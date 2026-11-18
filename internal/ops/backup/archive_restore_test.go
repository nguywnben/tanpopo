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
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

type archiveTestKeys struct{ kek []byte }

func newArchiveTestKeys() *archiveTestKeys {
	return &archiveTestKeys{kek: []byte("0123456789abcdef0123456789abcdef")}
}

func (k *archiveTestKeys) WrapArchiveKey(_ context.Context, dataKey []byte) (string, []byte, []byte, error) {
	block, err := aes.NewCipher(k.kek)
	if err != nil {
		return "", nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", nil, nil, err
	}
	return "test-kek", nonce, aead.Seal(nil, nonce, dataKey, []byte("test-kek")), nil
}

func (k *archiveTestKeys) UnwrapArchiveKey(_ context.Context, keyID string, nonce, wrapped []byte) ([]byte, error) {
	if keyID != "test-kek" {
		return nil, errors.New("unexpected KEK id")
	}
	block, err := aes.NewCipher(k.kek)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, wrapped, []byte("test-kek"))
}

func TestVerifyAndRestoreSQLiteArchive(t *testing.T) {
	if !PlatformSupported() {
		t.Skip(PlatformSupportUnavailableReason())
	}
	archivePath, keys := makeTestSQLiteArchive(t)
	verified, err := Verify(context.Background(), archivePath, keys)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.BackupID == "" || verified.CreatedAt == "" || verified.Files != 5 {
		t.Fatalf("unexpected verification receipt: %+v", verified)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	receipt, err := Restore(context.Background(), archivePath, destination, keys)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if receipt.BackupID != verified.BackupID || receipt.Path != destination {
		t.Fatalf("unexpected restore receipt: %+v", receipt)
	}
	for _, name := range []string{"database.sqlite", "tanpopo.lock", "tanpopo.yaml", "extensions/sample.wasm"} {
		if info, err := os.Lstat(filepath.Join(destination, name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("restored %s missing or non-regular: info=%v err=%v", name, info, err)
		}
	}
	if err := verifySQLiteIntegrity(context.Background(), filepath.Join(destination, "database.sqlite")); err != nil {
		t.Fatalf("restored SQLite integrity: %v", err)
	}
	if _, err := Restore(context.Background(), archivePath, destination, keys); err == nil {
		t.Fatal("Restore replaced an existing destination")
	}
}

func TestVerifyRejectsTamperedEncryptedArchive(t *testing.T) {
	if !PlatformSupported() {
		t.Skip(PlatformSupportUnavailableReason())
	}
	archivePath, keys := makeTestSQLiteArchive(t)
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	headerLength := int(archive[len(archiveMagic)])<<24 | int(archive[len(archiveMagic)+1])<<16 | int(archive[len(archiveMagic)+2])<<8 | int(archive[len(archiveMagic)+3])
	frameLengthOffset := len(archiveMagic) + 4 + headerLength
	if frameLengthOffset+5 >= len(archive) {
		t.Fatal("generated archive has no encrypted frame")
	}
	archive[frameLengthOffset+4] ^= 0x40
	tampered := filepath.Join(t.TempDir(), "tampered.tpop.enc")
	if err := os.WriteFile(tampered, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(context.Background(), tampered, keys); err == nil {
		t.Fatal("Verify accepted a modified AES-GCM frame")
	}
}

func TestArchiveEntryValidationRejectsTraversalLinksAndOversize(t *testing.T) {
	for _, test := range []struct {
		name   string
		header tar.Header
	}{
		{name: "traversal", header: tar.Header{Name: "../outside", Mode: 0o600, Typeflag: tar.TypeReg}},
		{name: "symlink", header: tar.Header{Name: "database.sqlite", Linkname: "../outside", Typeflag: tar.TypeSymlink}},
		{name: "oversize", header: tar.Header{Name: "database.sqlite", Mode: 0o600, Size: maxDatabaseBytes + 1, Typeflag: tar.TypeReg}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var payload bytes.Buffer
			writer := tar.NewWriter(&payload)
			if err := writer.WriteHeader(&test.header); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if _, err := extractTar(context.Background(), bytes.NewReader(payload.Bytes()), root); err == nil {
				t.Fatal("extractTar accepted an unsafe archive entry")
			}
		})
	}
}

func makeTestSQLiteArchive(t *testing.T) (string, *archiveTestKeys) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE backup_fixture (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO backup_fixture(value) VALUES ('verified snapshot')`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	databaseBytes, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	lockfile := filepath.Join(root, "tanpopo.lock")
	config := filepath.Join(root, "tanpopo.yaml")
	artifactDir := filepath.Join(root, "extensions")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for filename, data := range map[string][]byte{
		lockfile: []byte("lock-version: 1\n"),
		config:   []byte("data_dir: ./data\n"),
		filepath.Join(artifactDir, "sample.wasm"): []byte("extension artifact"),
	} {
		if err := os.WriteFile(filename, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keys := newArchiveTestKeys()
	receipt, err := createSQLiteMemory(context.Background(), testSnapshotter{content: databaseBytes}, keys, filepath.Join(root, "archives"), lockfile, config, artifactDir, 1_795_000_000_000)
	if err != nil {
		t.Fatalf("create archive fixture: %v", err)
	}
	if _, err := hex.DecodeString(receipt.SHA256Hex); err != nil {
		t.Fatalf("invalid archive fixture checksum: %v", err)
	}
	return filepath.Join(root, "archives", receipt.ArchiveFilename), keys
}
