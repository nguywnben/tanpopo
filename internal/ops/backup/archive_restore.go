// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)

const (
	maxArchiveHeaderBytes = 1 << 20
	maxManifestBytes      = 8 << 20
	maxDatabaseBytes      = 8 << 30
	maxSmallEntryBytes    = 32 << 20
	maxArtifactBytes      = 256 << 20
	maxExpandedBytes      = 16 << 30
	maxArchiveFiles       = 10000
)

// Verification is metadata for a successfully authenticated and structurally
// validated archive. It contains no key material or archive contents.
type Verification struct {
	BackupID  string `json:"backup_id"`
	CreatedAt string `json:"created_at"`
	Files     int    `json:"files"`
}

// RestoreReceipt identifies an atomically published restore directory.
type RestoreReceipt struct {
	BackupID  string `json:"backup_id"`
	CreatedAt string `json:"created_at"`
	Path      string `json:"path"`
}

// Verify authenticates, decrypts, decompresses, and validates a v1 .tpop.enc
// archive, including its manifest hashes and SQLite integrity. The supplied
// key service unwraps only the archive data key; the Master KEK stays in its
// original custody.
func Verify(ctx context.Context, archivePath string, keys KeyUnwrapper) (Verification, error) {
	if ctx == nil || strings.TrimSpace(archivePath) == "" || keys == nil {
		return Verification{}, errors.New("backup: context, archive path, and key unwrapper are required")
	}
	if !PlatformSupported() {
		return Verification{}, errors.New(PlatformSupportUnavailableReason())
	}
	stage, cleanup, err := openRestoreScratch()
	if err != nil {
		return Verification{}, err
	}
	defer cleanup()
	return decodeAndValidate(ctx, archivePath, keys, stage)
}

// Restore verifies an archive completely before atomically publishing its
// files into a new destination directory. The destination must not exist.
func Restore(ctx context.Context, archivePath, destination string, keys KeyUnwrapper) (RestoreReceipt, error) {
	if ctx == nil || strings.TrimSpace(archivePath) == "" || strings.TrimSpace(destination) == "" || keys == nil {
		return RestoreReceipt{}, errors.New("backup: context, archive path, destination, and key unwrapper are required")
	}
	if !PlatformSupported() {
		return RestoreReceipt{}, errors.New(PlatformSupportUnavailableReason())
	}
	absDestination, err := filepath.Abs(destination)
	if err != nil {
		return RestoreReceipt{}, fmt.Errorf("backup: resolve restore destination: %w", err)
	}
	parent := filepath.Dir(absDestination)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return RestoreReceipt{}, errors.New("backup: restore destination parent must be an existing real directory")
	}
	if _, err := os.Lstat(absDestination); err == nil {
		return RestoreReceipt{}, errors.New("backup: restore destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return RestoreReceipt{}, fmt.Errorf("backup: inspect restore destination: %w", err)
	}
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return RestoreReceipt{}, fmt.Errorf("backup: open restore destination parent: %w", err)
	}
	defer parentRoot.Close()
	openedParent, err := parentRoot.Stat(".")
	if err != nil || !os.SameFile(parentInfo, openedParent) {
		return RestoreReceipt{}, errors.New("backup: restore destination parent changed during secure open")
	}

	scratch, cleanupScratch, err := openRestoreScratch()
	if err != nil {
		return RestoreReceipt{}, err
	}
	defer cleanupScratch()
	verified, err := decodeAndValidate(ctx, archivePath, keys, scratch)
	if err != nil {
		return RestoreReceipt{}, err
	}

	stageID, err := randomHexID()
	if err != nil {
		return RestoreReceipt{}, fmt.Errorf("backup: create restore staging identifier: %w", err)
	}
	stageName := ".tanpopo-restore-" + stageID
	if err := parentRoot.Mkdir(stageName, 0o700); err != nil {
		return RestoreReceipt{}, fmt.Errorf("backup: create private restore publication directory: %w", err)
	}
	stageExists := true
	defer func() {
		if stageExists {
			_ = removeTree(parentRoot, stageName)
		}
	}()
	stageRoot, err := parentRoot.OpenRoot(stageName)
	if err != nil {
		return RestoreReceipt{}, err
	}
	if err := secureOpenedOutputRoot(stageRoot); err != nil {
		_ = stageRoot.Close()
		return RestoreReceipt{}, err
	}
	if err := copyRestoreTree(ctx, scratch, stageRoot); err != nil {
		_ = stageRoot.Close()
		return RestoreReceipt{}, err
	}
	if err := stageRoot.Close(); err != nil {
		return RestoreReceipt{}, err
	}
	if err := renameRootDirectoryNoReplace(parentRoot, stageName, filepath.Base(absDestination)); err != nil {
		return RestoreReceipt{}, fmt.Errorf("backup: publish verified restore atomically: %w", err)
	}
	stageExists = false
	return RestoreReceipt{BackupID: verified.BackupID, CreatedAt: verified.CreatedAt, Path: absDestination}, nil
}

func decodeAndValidate(ctx context.Context, archivePath string, keys KeyUnwrapper, stage *os.Root) (Verification, error) {
	if err := ctx.Err(); err != nil {
		return Verification{}, err
	}
	info, err := os.Lstat(archivePath)
	if err != nil {
		return Verification{}, fmt.Errorf("backup: inspect archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Verification{}, errors.New("backup: archive must be a regular non-symlink file")
	}
	if info.Size() <= int64(len(archiveMagic)+4) || info.Size() > maxExpandedBytes+(16<<20) {
		return Verification{}, errors.New("backup: archive size is outside supported limits")
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return Verification{}, err
	}
	defer archive.Close()
	opened, err := archive.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return Verification{}, errors.New("backup: archive changed while opening")
	}
	reader := bufio.NewReaderSize(archive, 64*1024)
	magic := make([]byte, len(archiveMagic))
	if _, err := io.ReadFull(reader, magic); err != nil || string(magic) != archiveMagic {
		return Verification{}, errors.New("backup: invalid encrypted archive magic")
	}
	var headerLength uint32
	if err := binary.Read(reader, binary.BigEndian, &headerLength); err != nil || headerLength == 0 || headerLength > maxArchiveHeaderBytes {
		return Verification{}, errors.New("backup: invalid encrypted archive header length")
	}
	headerBytes := make([]byte, headerLength)
	if _, err := io.ReadFull(reader, headerBytes); err != nil {
		return Verification{}, fmt.Errorf("backup: read archive header: %w", err)
	}
	var header envelopeHeader
	decoder := json.NewDecoder(strings.NewReader(string(headerBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&header); err != nil {
		return Verification{}, fmt.Errorf("backup: decode archive header: %w", err)
	}
	var headerTrailing any
	if err := decoder.Decode(&headerTrailing); !errors.Is(err, io.EOF) {
		return Verification{}, errors.New("backup: archive header contains trailing data")
	}
	if err := validateEnvelopeHeader(header); err != nil {
		return Verification{}, err
	}
	noncePrefix, _ := base64.RawStdEncoding.DecodeString(header.NoncePrefix)
	wrappedNonce, _ := base64.RawStdEncoding.DecodeString(header.WrappedKeyNonce)
	wrappedKey, _ := base64.RawStdEncoding.DecodeString(header.WrappedKey)
	dataKey, err := keys.UnwrapArchiveKey(ctx, header.KeyID, wrappedNonce, wrappedKey)
	if err != nil {
		return Verification{}, fmt.Errorf("backup: unwrap archive data key: %w", err)
	}
	defer clear(dataKey)
	if len(dataKey) != 32 {
		return Verification{}, errors.New("backup: unwrapped archive data key has invalid length")
	}
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return Verification{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Verification{}, err
	}
	frames := &encryptedFrameReader{ctx: ctx, src: reader, aead: aead, noncePrefix: noncePrefix, backupID: header.BackupID, header: headerBytes}
	defer frames.clear()
	zstdReader, err := zstd.NewReader(frames,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxMemory(1<<30),
		zstd.WithDecoderMaxWindow(1<<27),
	)
	if err != nil {
		return Verification{}, fmt.Errorf("backup: initialize zstd decoder: %w", err)
	}
	defer zstdReader.Close()
	extracted, err := extractTar(ctx, zstdReader, stage)
	if err != nil {
		return Verification{}, err
	}
	// tar.Reader stops at tar end blocks; consume the zstd stream so every AES-GCM
	// frame and the explicit encrypted end marker are authenticated as well.
	trailing, err := io.Copy(io.Discard, contextReader{ctx: ctx, reader: zstdReader})
	if err != nil {
		return Verification{}, fmt.Errorf("backup: finish authenticated archive stream: %w", err)
	}
	if trailing != 0 {
		return Verification{}, errors.New("backup: archive contains data after tar end marker")
	}
	if err := frames.finish(); err != nil {
		return Verification{}, err
	}
	manifestValue, err := validateManifestAndFiles(ctx, stage, extracted)
	if err != nil {
		return Verification{}, err
	}
	if manifestValue.CreatedAt != header.CreatedAt {
		return Verification{}, errors.New("backup: manifest creation time does not match archive header")
	}
	databasePath := filepath.Join(stage.Name(), "database.sqlite")
	if err := verifySQLiteIntegrity(ctx, databasePath); err != nil {
		return Verification{}, err
	}
	return Verification{BackupID: header.BackupID, CreatedAt: header.CreatedAt, Files: extracted.count}, nil
}

func validateEnvelopeHeader(h envelopeHeader) error {
	if h.FormatVersion != 1 || h.Compression != "zstd" || h.ChunkSize != chunkSize || h.BackupID == "" || h.KeyID == "" {
		return errors.New("backup: unsupported or invalid archive header")
	}
	backupID, err := hex.DecodeString(h.BackupID)
	if err != nil || len(backupID) != 16 || h.BackupID != strings.ToLower(h.BackupID) {
		return errors.New("backup: invalid backup identifier")
	}
	if _, err := time.Parse(time.RFC3339Nano, h.CreatedAt); err != nil {
		return errors.New("backup: invalid archive creation time")
	}
	noncePrefix, err := base64.RawStdEncoding.DecodeString(h.NoncePrefix)
	if err != nil || len(noncePrefix) != 4 {
		return errors.New("backup: invalid archive nonce prefix")
	}
	wrappedNonce, err := base64.RawStdEncoding.DecodeString(h.WrappedKeyNonce)
	if err != nil || len(wrappedNonce) != 12 {
		return errors.New("backup: invalid key wrapping nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(h.WrappedKey)
	if err != nil || len(ciphertext) != 32+16 {
		return errors.New("backup: invalid wrapped archive key")
	}
	return nil
}

func (r *encryptedFrameReader) Read(p []byte) (int, error) { return r.read(p) }

type encryptedFrameReader struct {
	ctx         context.Context
	src         io.Reader
	aead        cipher.AEAD
	noncePrefix []byte
	backupID    string
	header      []byte
	index       uint64
	plain       []byte
	ended       bool
}

func (r *encryptedFrameReader) read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.plain) == 0 {
		if r.ended {
			return 0, io.EOF
		}
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		var frameLength uint32
		if err := binary.Read(r.src, binary.BigEndian, &frameLength); err != nil {
			return 0, fmt.Errorf("backup: read encrypted frame length: %w", err)
		}
		if frameLength == 0 {
			var extra [1]byte
			n, err := r.src.Read(extra[:])
			if n != 0 || err != io.EOF {
				return 0, errors.New("backup: trailing bytes after encrypted archive end marker")
			}
			r.ended = true
			return 0, io.EOF
		}
		if frameLength <= uint32(r.aead.Overhead()) || frameLength > chunkSize+uint32(r.aead.Overhead()) {
			return 0, errors.New("backup: encrypted frame length is outside limits")
		}
		sealed := make([]byte, int(frameLength))
		if _, err := io.ReadFull(r.src, sealed); err != nil {
			return 0, fmt.Errorf("backup: read encrypted frame: %w", err)
		}
		nonce := make([]byte, r.aead.NonceSize())
		copy(nonce, r.noncePrefix)
		binary.BigEndian.PutUint64(nonce[len(r.noncePrefix):], r.index)
		plain, err := r.aead.Open(nil, nonce, sealed, chunkAAD(r.backupID, r.index, r.header))
		clear(sealed)
		if err != nil {
			return 0, fmt.Errorf("backup: authenticate encrypted frame %d: %w", r.index, err)
		}
		r.index++
		r.plain = plain
	}
	n := copy(p, r.plain)
	clear(r.plain[:n])
	r.plain = r.plain[n:]
	return n, nil
}

func (r *encryptedFrameReader) finish() error {
	if len(r.plain) != 0 {
		return errors.New("backup: compressed payload ended before encrypted frame data")
	}
	var scratch [32 * 1024]byte
	for {
		n, err := r.read(scratch[:])
		if n > 0 {
			clear(scratch[:n])
			return errors.New("backup: encrypted archive contains trailing compressed data")
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("backup: finish authenticated archive frames: %w", err)
		}
	}
}

func (r *encryptedFrameReader) clear() {
	clear(r.plain)
	r.plain = nil
}

type extractedArchive struct {
	manifest manifest
	files    map[string]string
	count    int
}

func extractTar(ctx context.Context, src io.Reader, root *os.Root) (extractedArchive, error) {
	result := extractedArchive{files: make(map[string]string)}
	tr := tar.NewReader(contextReader{ctx: ctx, reader: src})
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return result, fmt.Errorf("backup: read tar entry: %w", err)
		}
		if result.count >= maxArchiveFiles || header.Typeflag != tar.TypeReg || header.Size < 0 {
			return result, errors.New("backup: archive contains unsupported or excessive entries")
		}
		name, err := cleanArchivePath(header.Name)
		if err != nil {
			return result, err
		}
		if _, exists := result.files[name]; exists {
			return result, fmt.Errorf("backup: duplicate archive entry %q", name)
		}
		limit, err := archiveEntryLimit(name)
		if err != nil || header.Size > limit || total > maxExpandedBytes-header.Size {
			return result, fmt.Errorf("backup: archive entry %q exceeds size limits", name)
		}
		if name != "manifest.json" && name != "database.sqlite" && name != "tanpopo.lock" && name != "tanpopo.yaml" && !strings.HasPrefix(name, "extensions/") {
			return result, fmt.Errorf("backup: unexpected archive entry %q", name)
		}
		total += header.Size
		result.count++
		dir := path.Dir(name)
		if dir != "." {
			if err := mkdirRestoreParents(root, filepath.FromSlash(dir)); err != nil {
				return result, err
			}
		}
		file, err := root.OpenFile(filepath.FromSlash(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return result, fmt.Errorf("backup: create extracted entry %q: %w", name, err)
		}
		h := sha256.New()
		written, copyErr := io.CopyN(io.MultiWriter(file, h), contextReader{ctx: ctx, reader: tr}, header.Size)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != header.Size {
			return result, fmt.Errorf("backup: extract entry %q: %w", name, errors.Join(copyErr, closeErr, io.ErrUnexpectedEOF))
		}
		if err := secureOutputFile(root, filepath.FromSlash(name)); err != nil {
			return result, fmt.Errorf("backup: secure extracted entry %q: %w", name, err)
		}
		result.files[name] = hex.EncodeToString(h.Sum(nil))
	}
}

func cleanArchivePath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == "." || strings.Contains(name, ":") {
		return "", fmt.Errorf("backup: unsafe archive path %q", name)
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || component == "." || component == ".." {
			return "", fmt.Errorf("backup: unsafe archive path %q", name)
		}
	}
	return name, nil
}

func archiveEntryLimit(name string) (int64, error) {
	switch name {
	case "manifest.json":
		return maxManifestBytes, nil
	case "database.sqlite":
		return maxDatabaseBytes, nil
	case "tanpopo.lock", "tanpopo.yaml":
		return maxSmallEntryBytes, nil
	default:
		if strings.HasPrefix(name, "extensions/") && len(name) > len("extensions/") {
			return maxArtifactBytes, nil
		}
		return 0, errors.New("unknown archive entry")
	}
}

func validateManifestAndFiles(ctx context.Context, root *os.Root, extracted extractedArchive) (manifest, error) {
	manifestFile, err := root.Open("manifest.json")
	if err != nil {
		return manifest{}, errors.New("backup: archive has no manifest")
	}
	defer manifestFile.Close()
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: manifestFile}, maxManifestBytes+1))
	if err != nil || int64(len(data)) > maxManifestBytes {
		return manifest{}, errors.New("backup: manifest exceeds size limit or cannot be read")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&extracted.manifest); err != nil {
		return manifest{}, fmt.Errorf("backup: decode manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return manifest{}, errors.New("backup: manifest contains trailing data")
	}
	m := extracted.manifest
	if m.FormatVersion != 1 || m.CreatedAt == "" || !validSHA256(m.DatabaseSHA256) || !validSHA256(m.LockfileSHA256) || !validSHA256(m.ConfigSHA256) {
		return manifest{}, errors.New("backup: manifest metadata is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, m.CreatedAt); err != nil {
		return manifest{}, errors.New("backup: manifest creation time is invalid")
	}
	for _, fixed := range []struct{ name, digest string }{{"database.sqlite", m.DatabaseSHA256}, {"tanpopo.lock", m.LockfileSHA256}, {"tanpopo.yaml", m.ConfigSHA256}} {
		if extracted.files[fixed.name] != fixed.digest {
			return manifest{}, fmt.Errorf("backup: %s is absent or does not match the manifest", fixed.name)
		}
	}
	listed := map[string]bool{"manifest.json": true, "database.sqlite": true, "tanpopo.lock": true, "tanpopo.yaml": true}
	for _, artifact := range m.Artifacts {
		name, err := cleanArchivePath(artifact.Path)
		if err != nil || !strings.HasPrefix(name, "extensions/") || !validSHA256(artifact.SHA256Hex) || listed[name] {
			return manifest{}, errors.New("backup: manifest contains an invalid or duplicate extension path")
		}
		listed[name] = true
		if extracted.files[name] != artifact.SHA256Hex {
			return manifest{}, fmt.Errorf("backup: extension %q is absent or does not match the manifest", name)
		}
	}
	if len(listed) != len(extracted.files) {
		return manifest{}, errors.New("backup: archive and manifest file sets differ")
	}
	return m, nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func verifySQLiteIntegrity(ctx context.Context, dbPath string) error {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return err
	}
	urlPath := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" {
		urlPath = "/" + urlPath
	}
	dsn := (&url.URL{Scheme: "file", Path: urlPath}).String() + "?mode=ro&immutable=1"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("backup: open restored SQLite snapshot: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("backup: run SQLite integrity check: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return err
		}
		count++
		if result != "ok" {
			return fmt.Errorf("backup: SQLite integrity check failed: %s", result)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("backup: SQLite integrity check returned %d rows, want one", count)
	}
	return nil
}

func copyRestoreTree(ctx context.Context, src, dst *os.Root) error {
	return filepath.WalkDir(src.Name(), func(filename string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if filename == src.Name() {
			return nil
		}
		rel, err := filepath.Rel(src.Name(), filename)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)
		info, err := os.Lstat(filename)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("backup: restore staging tree contains an unsafe entry")
		}
		if info.IsDir() {
			if err := mkdirRestoreParents(dst, rel); err != nil {
				return err
			}
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > maxArtifactBytes && rel != "database.sqlite" {
			return errors.New("backup: restore staging tree contains an invalid file")
		}
		if dir := filepath.Dir(rel); dir != "." {
			if err := mkdirRestoreParents(dst, dir); err != nil {
				return err
			}
		}
		in, err := src.Open(rel)
		if err != nil {
			return err
		}
		out, err := dst.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, contextReader{ctx: ctx, reader: in})
		closeErr := errors.Join(in.Close(), out.Close())
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		return secureOutputFile(dst, rel)
	})
}

func mkdirRestoreParents(root *os.Root, directory string) error {
	clean := filepath.Clean(directory)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("backup: invalid restore directory path")
	}
	current := ""
	for _, component := range strings.Split(filepath.ToSlash(clean), "/") {
		if component == "" || component == "." || component == ".." {
			return errors.New("backup: invalid restore directory component")
		}
		current = filepath.Join(current, component)
		if err := root.Mkdir(current, 0o700); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return err
			}
			info, statErr := root.Lstat(current)
			if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("backup: restore path component is not a real directory")
			}
		}
	}
	return nil
}
