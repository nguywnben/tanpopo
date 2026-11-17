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
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: resolve output directory: %w", err)
	}
	if info, statErr := os.Lstat(absOutputDir); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return Receipt{}, errors.New("backup: output directory must be a real directory, not a symlink")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Receipt{}, fmt.Errorf("backup: inspect output directory: %w", statErr)
	}
	if err := os.MkdirAll(absOutputDir, 0o700); err != nil {
		return Receipt{}, fmt.Errorf("backup: create output directory: %w", err)
	}
	info, err := os.Lstat(absOutputDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Receipt{}, errors.New("backup: output directory changed or is not a real directory")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(absOutputDir, 0o700); err != nil {
			return Receipt{}, fmt.Errorf("backup: protect output directory: %w", err)
		}
	}
	workDir, err := os.MkdirTemp(absOutputDir, ".tanpopo-backup-")
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: create staging directory: %w", err)
	}
	defer os.RemoveAll(workDir)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(workDir, 0o700); err != nil {
			return Receipt{}, fmt.Errorf("backup: protect staging directory: %w", err)
		}
	}

	createdAt := time.UnixMilli(nowMs).UTC().Format(time.RFC3339Nano)
	backupID, err := randomHexID()
	if err != nil {
		return Receipt{}, err
	}
	dbPath := filepath.Join(workDir, "database.sqlite")
	if err := store.BackupTo(ctx, dbPath); err != nil {
		return Receipt{}, fmt.Errorf("backup: create online SQLite snapshot: %w", err)
	}
	dbHash, err := fileSHA256(dbPath)
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
			lockfileCopy = filepath.Join(workDir, "tanpopo.lock")
			if err := copyFile(ctx, lockfilePath, lockfileCopy, info); err != nil {
				return Receipt{}, fmt.Errorf("backup: stage extension lockfile: %w", err)
			}
			lockfileHash, err = fileSHA256(lockfileCopy)
			if err != nil {
				return Receipt{}, err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return Receipt{}, fmt.Errorf("backup: inspect extension lockfile: %w", statErr)
		}
	}

	payloadPath := filepath.Join(workDir, "payload.tar.gz")
	m := manifest{FormatVersion: 1, CreatedAt: createdAt, DatabaseSHA256: dbHash, LockfileSHA256: lockfileHash}
	if err := writePayload(ctx, payloadPath, dbPath, lockfileCopy, m); err != nil {
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
	tempArchive := filepath.Join(workDir, finalName+".partial")
	if err := encryptPayload(ctx, tempArchive, payloadPath, dataKey, noncePrefix, backupID, headerBytes); err != nil {
		return Receipt{}, err
	}
	finalPath := filepath.Join(outputDir, finalName)
	if _, err := os.Lstat(finalPath); err == nil {
		return Receipt{}, errors.New("backup: destination archive already exists")
	}
	if err := os.Rename(tempArchive, finalPath); err != nil {
		return Receipt{}, fmt.Errorf("backup: publish encrypted archive: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(finalPath, 0o600); err != nil {
			_ = os.Remove(finalPath)
			return Receipt{}, fmt.Errorf("backup: protect encrypted archive: %w", err)
		}
	}
	archiveHash, err := fileSHA256(finalPath)
	if err != nil {
		_ = os.Remove(finalPath)
		return Receipt{}, err
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: inspect published archive: %w", err)
	}
	return Receipt{BackupID: backupID, ArchiveFilename: finalName, SHA256Hex: archiveHash, SizeBytes: info.Size(), CreatedAt: createdAt}, nil
}

func writePayload(ctx context.Context, path, dbPath, lockfilePath string, m manifest) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("backup: create compressed payload: %w", err)
	}
	good := false
	defer func() {
		_ = out.Close()
		if !good {
			_ = os.Remove(path)
		}
	}()
	compressed := gzip.NewWriter(out)
	archive := tar.NewWriter(compressed)
	if err := addArchiveFile(ctx, archive, dbPath, "database.sqlite"); err != nil {
		return err
	}
	if lockfilePath != "" {
		if err := addArchiveFile(ctx, archive, lockfilePath, "tanpopo.lock"); err != nil {
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

func addArchiveFile(ctx context.Context, tarWriter *tar.Writer, path, name string) error {
	file, err := os.Open(path)
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

func encryptPayload(ctx context.Context, destPath, payloadPath string, key, noncePrefix []byte, backupID string, header []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("backup: initialize archive cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("backup: initialize archive authentication: %w", err)
	}
	src, err := os.Open(payloadPath)
	if err != nil {
		return fmt.Errorf("backup: open compressed payload: %w", err)
	}
	defer src.Close()
	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("backup: create encrypted archive: %w", err)
	}
	good := false
	defer func() {
		_ = dst.Close()
		if !good {
			_ = os.Remove(destPath)
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

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
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

func copyFile(ctx context.Context, src, dst string, expected os.FileInfo) error {
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
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, contextReader{ctx: ctx, reader: in})
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(dst)
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

