// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"archive/tar"
	"bytes"
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
	"time"

	"gopkg.in/yaml.v3"
)

type memorySnapshotter interface {
	BackupBytes(context.Context) ([]byte, error)
}

// createSQLiteMemory avoids plaintext staging files on macOS and Windows.
// The SQLite online snapshot and small config inputs stay in memory; artifact
// files stream into tar, Zstandard, and authenticated encryption in that order.
func createSQLiteMemory(ctx context.Context, store Snapshotter, keys KeyWrapper, outputDir, lockfilePath, configPath, artifactDir string, nowMs int64) (Receipt, error) {
	snapshotter, ok := store.(memorySnapshotter)
	if !ok {
		return Receipt{}, errors.New("backup: storage backend does not support secure in-memory SQLite snapshots")
	}
	backupMu.Lock()
	defer backupMu.Unlock()
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
	unlockBackup, err := lockBackup(outputRoot)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: lock output directory: %w", err)
	}
	defer unlockBackup()
	if err := cleanupStaleStaging(outputRoot); err != nil {
		return Receipt{}, err
	}
	lockfile, err := readStableInput(ctx, lockfilePath, 32<<20)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: read extension lockfile: %w", err)
	}
	defer clear(lockfile)
	config, err := readStableInput(ctx, configPath, 4<<20)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: read server config: %w", err)
	}
	defer clear(config)
	if err := validateBackupConfigBytes(config); err != nil {
		return Receipt{}, err
	}
	artifactRoot, err := os.OpenRoot(artifactDir)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: open extension artifact root: %w", err)
	}
	defer artifactRoot.Close()
	artifactInfo, err := artifactRoot.Stat(".")
	if err != nil || !artifactInfo.IsDir() {
		return Receipt{}, errors.New("backup: extension artifact root must be a directory")
	}
	artifacts, err := collectArtifactFiles(ctx, artifactRoot)
	if err != nil {
		return Receipt{}, err
	}
	database, err := snapshotter.BackupBytes(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("backup: create in-memory online SQLite snapshot: %w", err)
	}
	defer clear(database)
	if len(database) == 0 {
		return Receipt{}, errors.New("backup: online SQLite snapshot is empty")
	}
	databaseHash := sha256.Sum256(database)
	lockfileHash := sha256.Sum256(lockfile)
	configHash := sha256.Sum256(config)
	artifactDigests := make([]artifactDigest, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifactDigests = append(artifactDigests, artifactDigest{Path: "extensions/" + artifact.Path, SHA256Hex: artifact.SHA256Hex})
	}
	m := manifest{FormatVersion: 1, CreatedAt: createdAt, DatabaseSHA256: hex.EncodeToString(databaseHash[:]), LockfileSHA256: hex.EncodeToString(lockfileHash[:]), ConfigSHA256: hex.EncodeToString(configHash[:]), Artifacts: artifactDigests}
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return Receipt{}, fmt.Errorf("backup: generate archive data key: %w", err)
	}
	defer clear(dataKey)
	keyID, wrappedNonce, wrappedKey, err := keys.WrapArchiveKey(ctx, dataKey)
	if err != nil {
		return Receipt{}, err
	}
	noncePrefix := make([]byte, 4)
	if _, err := rand.Read(noncePrefix); err != nil {
		return Receipt{}, err
	}
	header := envelopeHeader{FormatVersion: 1, BackupID: backupID, CreatedAt: createdAt, Compression: "zstd", KeyID: keyID, WrappedKeyNonce: base64.RawStdEncoding.EncodeToString(wrappedNonce), WrappedKey: base64.RawStdEncoding.EncodeToString(wrappedKey), NoncePrefix: base64.RawStdEncoding.EncodeToString(noncePrefix), ChunkSize: chunkSize}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return Receipt{}, err
	}
	finalName := "tanpopo-backup-" + backupID + ".tpop.enc"
	if err := writeMemoryPayload(ctx, database, lockfile, config, artifactRoot, artifacts, m, outputRoot, finalName, dataKey, noncePrefix, backupID, headerBytes); err != nil {
		return Receipt{}, err
	}
	archiveHash, err := fileSHA256(outputRoot, finalName)
	if err != nil {
		_ = outputRoot.Remove(finalName)
		return Receipt{}, err
	}
	info, err := outputRoot.Stat(finalName)
	if err != nil {
		_ = outputRoot.Remove(finalName)
		return Receipt{}, err
	}
	return Receipt{BackupID: backupID, ArchiveFilename: finalName, SHA256Hex: archiveHash, SizeBytes: info.Size(), CreatedAt: createdAt}, nil
}

func readStableInput(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxBytes {
		return nil, errors.New("input must be a regular non-symlink file within the size limit")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("input changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: f}, maxBytes+1))
	if err != nil {
		clear(b)
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		clear(b)
		return nil, errors.New("input exceeds size limit")
	}
	return b, nil
}

func validateBackupConfigBytes(data []byte) error {
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&doc); err != nil {
		return fmt.Errorf("backup: parse server config: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("backup: server config must contain exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("backup: parse server config: %w", err)
	}
	if containsInlineMasterKEK(&doc, make(map[*yaml.Node]bool)) {
		return errors.New("backup: refusing to archive config containing inline Master KEK material; use TANPOPO_MASTER_KEY or TANPOPO_MASTER_KEY_FILE")
	}
	return nil
}

func writeMemoryPayload(ctx context.Context, database, lockfile, config []byte, artifactRoot *os.Root, artifacts []artifactFile, m manifest, dstRoot *os.Root, destPath string, key, noncePrefix []byte, backupID string, header []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	dst, err := dstRoot.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		_ = dst.Close()
		if !complete {
			_ = dstRoot.Remove(destPath)
		}
	}()
	if _, err := io.WriteString(dst, archiveMagic); err != nil {
		return err
	}
	if err := binary.Write(dst, binary.BigEndian, uint32(len(header))); err != nil {
		return err
	}
	if _, err := dst.Write(header); err != nil {
		return err
	}
	encrypted := &encryptedFrameWriter{ctx: ctx, dst: dst, aead: aead, noncePrefix: noncePrefix, backupID: backupID, header: header, buffer: make([]byte, 0, chunkSize)}
	compressed, err := newZstdWriter(encrypted)
	if err != nil {
		return err
	}
	archive := tar.NewWriter(compressed)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"database.sqlite", database}, {"tanpopo.lock", lockfile}, {"tanpopo.yaml", config}} {
		if err := addArchiveBytes(ctx, archive, entry.name, entry.data); err != nil {
			return err
		}
	}
	for _, artifact := range artifacts {
		if err := addArchiveFile(ctx, archive, artifactRoot, artifact.Path, "extensions/"+artifact.Path, artifact.SHA256Hex); err != nil {
			return err
		}
	}
	manifestBytes, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := archive.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifestBytes)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := archive.Write(manifestBytes); err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := compressed.Close(); err != nil {
		return err
	}
	if err := encrypted.Close(); err != nil {
		return err
	}
	if err := dst.Sync(); err != nil {
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func addArchiveBytes(ctx context.Context, archive *tar.Writer, name string, data []byte) error {
	if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := io.Copy(archive, contextReader{ctx: ctx, reader: bytes.NewReader(data)}); err != nil {
		return fmt.Errorf("backup: add %q: %w", name, err)
	}
	return nil
}
