// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package backup holds the SQLite snapshot and encrypted archive core used by
// the Unit 30 Admin API. Unit 32 can reuse this format for CLI verify/restore.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	archiveMagic = "TPOPENC1"
	chunkSize    = 64 * 1024
)

type Snapshotter interface {
	BackupTo(ctx context.Context, destPath string) error
}

type KeyWrapper interface {
	WrapArchiveKey(ctx context.Context, plaintextKey []byte) (keyID string, nonce, ciphertext []byte, err error)
}

type Receipt struct {
	BackupID       string `json:"backup_id"`
	ArchiveFilename string `json:"archive_filename"`
	SHA256Hex      string `json:"sha256_hex"`
	SizeBytes      int64  `json:"size_bytes"`
	CreatedAt      string `json:"created_at"`
}

type manifest struct {
	FormatVersion int    `json:"format_version"`
	CreatedAt     string `json:"created_at"`
	DatabaseSHA256 string `json:"database_sha256"`
	LockfileSHA256 string `json:"lockfile_sha256,omitempty"`
}

type envelopeHeader struct {
	FormatVersion   int    `json:"format_version"`
	BackupID        string `json:"backup_id"`
	CreatedAt       string `json:"created_at"`
	Compression     string `json:"compression"`
	KeyID           string `json:"key_id"`
	WrappedKeyNonce string `json:"wrapped_key_nonce"`
	WrappedKey      string `json:"wrapped_key"`
	NoncePrefix     string `json:"nonce_prefix"`
	ChunkSize       int    `json:"chunk_size"`
}

// CreateSQLite takes a consistent online SQLite snapshot, includes the lockfile
// when configured, writes a manifest, gzip-compresses the tar stream, and seals
// it in independently authenticated AES-GCM chunks under a random wrapped key.
func CreateSQLite(ctx context.Context, store Snapshotter, keys KeyWrapper, outputDir, lockfilePath string, nowMs int64) (Receipt, error) {
	if store == nil || keys == nil || strings.TrimSpace(outputDir) == "" {
		return Receipt{}, errors.New("backup: snapshotter, key wrapper, and output directory are required")
	}
	createdAt := time.UnixMilli(nowMs).UTC().Format(time.RFC3339Nano)
	backupID, err := randomHexID()
	if err != nil {
		return Receipt{}, err
	}
	outputRoot, err := openOutputRoot(outputDir)
	if err != nil {
		return Receipt{}, err
	}
	defer outputRoot.Close()
	rootPathFile, sqliteRootPath, err := pathForOpenRoot(outputRoot, outputDir)
	if err != nil {
		return Receipt{}, err
	}
	defer rootPathFile.Close()
	stageName := ".tanpopo-backup-" + backupID
	if err := outputRoot.Mkdir(stageName, 0o700); err != nil {
		return Receipt{}, fmt.Errorf("backup: create protected staging directory: %w", err)
	}
	stageRoot, err := outputRoot.OpenRoot(stageName)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: open protected staging directory: %w", err)
	}
	defer func() {
		for _, name := range []string{"database.sqlite", "tanpopo.lock", "payload.tar.gz"} {
			_ = stageRoot.Remove(name)
		}
		_ = stageRoot.Close()
		_ = outputRoot.Remove(stageName)
	}()
	stageInfo, err := stageRoot.Stat(".")
	entryInfo, entryErr := outputRoot.Lstat(stageName)
	if err != nil || entryErr != nil || entryInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(stageInfo, entryInfo) {
		return Receipt{}, errors.New("backup: staging directory changed during secure open")
	}
	stagePathFile, sqliteStagePath, err := pathForOpenRoot(stageRoot, filepath.Join(sqliteRootPath, stageName))
	if err != nil {
		return Receipt{}, err
	}
	defer stagePathFile.Close()
	if runtime.GOOS != "windows" {
		if err := os.Chmod(sqliteStagePath, 0o700); err != nil {
			return Receipt{}, fmt.Errorf("backup: protect staging directory: %w", err)
		}
	}
	dbRelPath := "database.sqlite"
	dbPath := filepath.Join(sqliteStagePath, dbRelPath)
	if err := store.BackupTo(ctx, dbPath); err != nil {
		return Receipt{}, fmt.Errorf("backup: create online SQLite snapshot: %w", err)
	}
	dbHash, err := fileSHA256(stageRoot, dbRelPath)
	if err != nil {
		return Receipt{}, err
	}
	lockfileCopy := ""
	lockfileHash := ""
	if strings.TrimSpace(lockfilePath) != "" {
		info, statErr := os.Lstat(lockfilePath)
		if statErr == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return Receipt{}, errors.New("backup: lockfile must be a regular non-symlink file")
			}
			lockfileCopy = "tanpopo.lock"
			if err := copyFile(ctx, lockfilePath, stageRoot, lockfileCopy, info); err != nil {
				return Receipt{}, fmt.Errorf("backup: stage extension lockfile: %w", err)
			}
			lockfileHash, err = fileSHA256(stageRoot, lockfileCopy)
			if err != nil {
				return Receipt{}, err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return Receipt{}, fmt.Errorf("backup: inspect extension lockfile: %w", statErr)
		}
	}

	payloadPath := "payload.tar.gz"
	m := manifest{FormatVersion: 1, CreatedAt: createdAt, DatabaseSHA256: dbHash, LockfileSHA256: lockfileHash}
	if err := writePayload(ctx, stageRoot, payloadPath, dbRelPath, lockfileCopy, m); err != nil {
		return Receipt{}, err
	}

	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return Receipt{}, fmt.Errorf("backup: generate archive data key: %w", err)
	}
	defer clear(dataKey)
	keyID, wrappedNonce, wrappedKey, err := keys.WrapArchiveKey(ctx, dataKey)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: wrap archive data key: %w", err)
	}
	noncePrefix := make([]byte, 4)
	if _, err := rand.Read(noncePrefix); err != nil {
		return Receipt{}, fmt.Errorf("backup: generate archive nonce prefix: %w", err)
	}
	header := envelopeHeader{
		FormatVersion: 1, BackupID: backupID, CreatedAt: createdAt, Compression: "gzip",
		KeyID: keyID, WrappedKeyNonce: base64.RawStdEncoding.EncodeToString(wrappedNonce),
		WrappedKey: base64.RawStdEncoding.EncodeToString(wrappedKey),
		NoncePrefix: base64.RawStdEncoding.EncodeToString(noncePrefix), ChunkSize: chunkSize,
	}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: encode archive header: %w", err)
	}
	finalName := "tanpopo-backup-" + backupID + ".tpop.enc"
	tempArchive := ".tanpopo-backup-" + backupID + ".partial"
	defer outputRoot.Remove(tempArchive)
	if err := encryptPayload(ctx, stageRoot, outputRoot, tempArchive, payloadPath, dataKey, noncePrefix, backupID, headerBytes); err != nil {
		return Receipt{}, err
	}
	if _, err := outputRoot.Lstat(finalName); err == nil {
		return Receipt{}, errors.New("backup: destination archive already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Receipt{}, fmt.Errorf("backup: inspect archive destination: %w", err)
	}
	if err := renameRootEntry(sqliteRootPath, tempArchive, finalName); err != nil {
		return Receipt{}, fmt.Errorf("backup: publish encrypted archive: %w", err)
	}
	archiveHash, err := fileSHA256(outputRoot, finalName)
	if err != nil {
		_ = outputRoot.Remove(finalName)
		return Receipt{}, err
	}
	info, err := outputRoot.Stat(finalName)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: inspect published archive: %w", err)
	}
	return Receipt{BackupID: backupID, ArchiveFilename: finalName, SHA256Hex: archiveHash, SizeBytes: info.Size(), CreatedAt: createdAt}, nil
}

func openOutputRoot(outputDir string) (*os.Root, error) {
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, fmt.Errorf("backup: resolve output directory: %w", err)
	}
	parentRoot, err := os.OpenRoot(filepath.Dir(absOutputDir))
	if err != nil {
		return nil, fmt.Errorf("backup: open output parent directory: %w", err)
	}
	defer parentRoot.Close()
	base := filepath.Base(absOutputDir)
	if base == "." || base == string(filepath.Separator) || base == ".." {
		return nil, errors.New("backup: invalid output directory name")
	}
	info, err := parentRoot.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		if err := parentRoot.Mkdir(base, 0o700); err != nil {
			return nil, fmt.Errorf("backup: create output directory: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("backup: inspect output directory: %w", err)
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("backup: output directory must be a real directory, not a symlink")
	}
	root, err := parentRoot.OpenRoot(base)
	if err != nil {
		return nil, fmt.Errorf("backup: open output directory: %w", err)
	}
	openedInfo, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("backup: inspect opened output directory: %w", err)
	}
	entryInfo, err := parentRoot.Lstat(base)
	if err != nil || entryInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(openedInfo, entryInfo) {
		_ = root.Close()
		return nil, errors.New("backup: output directory changed during secure open")
	}
	if runtime.GOOS != "windows" {
		rootFile, rootPath, pathErr := pathForOpenRoot(root, absOutputDir)
		if pathErr != nil {
			_ = root.Close()
			return nil, pathErr
		}
		chmodErr := os.Chmod(rootPath, 0o700)
		_ = rootFile.Close()
		if chmodErr != nil {
			_ = root.Close()
			return nil, fmt.Errorf("backup: protect output directory: %w", chmodErr)
		}
	}
	return root, nil
}

func renameRootEntry(rootPath, oldName, newName string) error {
	return os.Rename(filepath.Join(rootPath, oldName), filepath.Join(rootPath, newName))
}

func pathForOpenRoot(root *os.Root, fallback string) (*os.File, string, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, "", fmt.Errorf("backup: pin output directory: %w", err)
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		abs, err := filepath.Abs(fallback)
		if err != nil {
			_ = dir.Close()
			return nil, "", fmt.Errorf("backup: resolve output directory: %w", err)
		}
		return dir, abs, nil
	}
	conn, err := dir.SyscallConn()
	if err != nil {
		_ = dir.Close()
		return nil, "", fmt.Errorf("backup: pin output directory handle: %w", err)
	}
	var fd uintptr
	if err := conn.Control(func(raw uintptr) { fd = raw }); err != nil {
		_ = dir.Close()
		return nil, "", fmt.Errorf("backup: read output directory handle: %w", err)
	}
	procDir := "/proc/self/fd"
	if runtime.GOOS == "darwin" {
		procDir = "/dev/fd"
	}
	return dir, filepath.Join(procDir, strconv.FormatUint(uint64(fd), 10)), nil
}

func writePayload(ctx context.Context, root *os.Root, path, dbPath, lockfilePath string, m manifest) error {
	out, err := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("backup: create compressed payload: %w", err)
	}
	good := false
	defer func() {
		_ = out.Close()
		if !good {
			_ = root.Remove(path)
		}
	}()
	compressed := gzip.NewWriter(out)
	archive := tar.NewWriter(compressed)
	if err := addArchiveFile(ctx, archive, root, dbPath, "database.sqlite"); err != nil {
		return err
	}
	if lockfilePath != "" {
		if err := addArchiveFile(ctx, archive, root, lockfilePath, "tanpopo.lock"); err != nil {
			return err
		}
	}
	manifestBytes, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("backup: encode manifest: %w", err)
	}
	if err := archive.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifestBytes)), Typeflag: tar.TypeReg}); err != nil {
		return fmt.Errorf("backup: write manifest header: %w", err)
	}
	if _, err := archive.Write(manifestBytes); err != nil {
		return fmt.Errorf("backup: write manifest: %w", err)
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("backup: finalize tar payload: %w", err)
	}
	if err := compressed.Close(); err != nil {
		return fmt.Errorf("backup: finalize gzip payload: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("backup: sync compressed payload: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("backup: close compressed payload: %w", err)
	}
	good = true
	return nil
}

func addArchiveFile(ctx context.Context, tarWriter *tar.Writer, root *os.Root, path, name string) error {
	file, err := root.Open(path)
	if err != nil {
		return fmt.Errorf("backup: open staged file %q: %w", name, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("backup: staged file %q is not a regular file", name)
	}
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), Typeflag: tar.TypeReg}); err != nil {
		return fmt.Errorf("backup: write %q header: %w", name, err)
	}
	if _, err := io.Copy(tarWriter, contextReader{ctx: ctx, reader: file}); err != nil {
		return fmt.Errorf("backup: add %q: %w", name, err)
	}
	return nil
}

func encryptPayload(ctx context.Context, srcRoot, dstRoot *os.Root, destPath, payloadPath string, key, noncePrefix []byte, backupID string, header []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("backup: initialize archive cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("backup: initialize archive authentication: %w", err)
	}
	src, err := srcRoot.Open(payloadPath)
	if err != nil {
		return fmt.Errorf("backup: open compressed payload: %w", err)
	}
	defer src.Close()
	dst, err := dstRoot.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("backup: create encrypted archive: %w", err)
	}
	good := false
	defer func() {
		_ = dst.Close()
		if !good {
			_ = dstRoot.Remove(destPath)
		}
	}()
	if _, err := io.WriteString(dst, archiveMagic); err != nil {
		return fmt.Errorf("backup: write archive magic: %w", err)
	}
	if len(header) > 1<<20 {
		return errors.New("backup: archive header exceeds size limit")
	}
	if err := binary.Write(dst, binary.BigEndian, uint32(len(header))); err != nil {
		return fmt.Errorf("backup: write header length: %w", err)
	}
	if _, err := dst.Write(header); err != nil {
		return fmt.Errorf("backup: write archive header: %w", err)
	}
	buf := make([]byte, chunkSize)
	var index uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := io.ReadFull(src, buf)
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return fmt.Errorf("backup: read compressed payload: %w", readErr)
		}
		if n > 0 {
			nonce := make([]byte, aead.NonceSize())
			copy(nonce, noncePrefix)
			binary.BigEndian.PutUint64(nonce[len(noncePrefix):], index)
			aad := chunkAAD(backupID, index, header)
			sealed := aead.Seal(nil, nonce, buf[:n], aad)
			if err := binary.Write(dst, binary.BigEndian, uint32(len(sealed))); err != nil {
				return fmt.Errorf("backup: write encrypted frame length: %w", err)
			}
			if _, err := dst.Write(sealed); err != nil {
				return fmt.Errorf("backup: write encrypted frame: %w", err)
			}
			index++
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
	}
	if err := binary.Write(dst, binary.BigEndian, uint32(0)); err != nil {
		return fmt.Errorf("backup: write end marker: %w", err)
	}
	if err := dst.Sync(); err != nil {
		return fmt.Errorf("backup: sync encrypted archive: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("backup: close encrypted archive: %w", err)
	}
	good = true
	return nil
}

func chunkAAD(backupID string, index uint64, header []byte) []byte {
	headerDigest := sha256.Sum256(header)
	aad := make([]byte, 0, len(archiveMagic)+len(backupID)+8+len(headerDigest))
	aad = append(aad, archiveMagic...)
	aad = append(aad, backupID...)
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], index)
	aad = append(aad, counter[:]...)
	return append(aad, headerDigest[:]...)
}

func fileSHA256(root *os.Root, path string) (string, error) {
	f, err := root.Open(path)
	if err != nil {
		return "", fmt.Errorf("backup: open file for checksum: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("backup: checksum file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(ctx context.Context, src string, root *os.Root, dst string, expected os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		return errors.New("backup: lockfile changed while opening; refusing to follow a replacement path")
	}
	out, err := root.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, contextReader{ctx: ctx, reader: in})
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		_ = root.Remove(dst)
		return copyErr
	}
	if syncErr != nil {
		_ = root.Remove(dst)
		return syncErr
	}
	return closeErr
}

func randomHexID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("backup: generate backup ID: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

