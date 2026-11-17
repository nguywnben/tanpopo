// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type testSnapshotter struct{ content []byte }

func (s testSnapshotter) BackupTo(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.WriteFile(path, s.content, 0o600)
}

type testKeyWrapper struct{ key []byte }

func (w *testKeyWrapper) WrapArchiveKey(ctx context.Context, key []byte) (string, []byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, nil, err
	}
	w.key = append([]byte(nil), key...)
	return "test-kek-v1", []byte("wrapped-nonce"), []byte("wrapped-key-ciphertext"), nil
}

func TestCreateSQLiteProducesProtectedEncryptedArchive(t *testing.T) {
	ctx := context.Background()
	if runtime.GOOS != "linux" {
		if _, err := CreateSQLite(ctx, testSnapshotter{content: []byte("snapshot")}, &testKeyWrapper{}, t.TempDir(), "", 1_795_000_000_000); err == nil {
			t.Fatal("CreateSQLite returned success without a directory-handle-pinned SQLite path")
		}
		return
	}
	root := t.TempDir()
	outputDir := filepath.Join(root, "backups")
	stagingRoot, cleanupStagingRoot, err := openStagingRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanupStagingRoot(); err != nil {
			t.Errorf("clean test staging root: %v", err)
		}
	})
	staleStage := ".tanpopo-backup-0123456789abcdef0123456789abcdef"
	if err := stagingRoot.Mkdir(staleStage, 0o700); err != nil {
		t.Fatal(err)
	}
	staleDB, err := stagingRoot.OpenFile(filepath.Join(staleStage, "database.sqlite"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staleDB.Write([]byte("stale plaintext snapshot")); err != nil {
		_ = staleDB.Close()
		t.Fatal(err)
	}
	if err := staleDB.Close(); err != nil {
		t.Fatal(err)
	}
	stalePartial := ".tanpopo-backup-fedcba9876543210fedcba9876543210.partial"
	partial, err := stagingRoot.OpenFile(stalePartial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := partial.Write([]byte("partial encrypted archive")); err != nil {
		_ = partial.Close()
		t.Fatal(err)
	}
	if err := partial.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cleanupStaleStaging(stagingRoot); err != nil {
		t.Fatalf("cleanup abandoned staging data: %v", err)
	}
	lockfile := filepath.Join(root, "tanpopo.lock")
	if err := os.WriteFile(lockfile, []byte("extension lock manifest"), 0o600); err != nil {
		t.Fatal(err)
	}
	plainDB := []byte("sqlite snapshot contains sensitive row data")
	keys := &testKeyWrapper{}
	receipt, err := CreateSQLite(ctx, testSnapshotter{content: plainDB}, keys, outputDir, lockfile, 1_795_000_000_000)
	if err != nil {
		t.Fatalf("CreateSQLite: %v", err)
	}
	if receipt.ArchiveFilename == "" || len(receipt.SHA256Hex) != 64 || receipt.SizeBytes <= 0 || len(keys.key) != 32 {
		t.Fatalf("incomplete encrypted backup receipt/key: receipt=%+v key-bytes=%d", receipt, len(keys.key))
	}
	archivePath := filepath.Join(outputDir, receipt.ArchiveFilename)
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.HasPrefix(archive, []byte(archiveMagic)) {
		t.Fatalf("archive does not have the canonical encrypted format header")
	}
	if bytes.Contains(archive, plainDB) || bytes.Contains(archive, keys.key) {
		t.Fatal("archive contains plaintext database content or the raw archive data key")
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode = %o, want 600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("backup directory mode = %o, want 700", dirInfo.Mode().Perm())
	}
	if _, err := stagingRoot.Lstat(staleStage); !os.IsNotExist(err) {
		t.Fatalf("abandoned plaintext staging directory remains: stat err=%v", err)
	}
	if _, err := stagingRoot.Lstat(stalePartial); !os.IsNotExist(err) {
		t.Fatalf("abandoned partial archive remains: stat err=%v", err)
	}
}

func TestChunkAADAuthenticatesEnvelopeHeader(t *testing.T) {
	first := chunkAAD("backup-id", 3, []byte(`{"format_version":1,"compression":"zstd"}`))
	modified := chunkAAD("backup-id", 3, []byte(`{"format_version":1,"compression":"other"}`))
	if bytes.Equal(first, modified) {
		t.Fatal("changing envelope metadata did not change the chunk authentication data")
	}
}
