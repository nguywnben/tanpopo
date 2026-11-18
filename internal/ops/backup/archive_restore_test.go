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
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
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
	archivePath, keys := makeTestSQLiteArchive(t)
	if !PlatformSupported() {
		if _, err := Verify(context.Background(), archivePath, keys); err == nil || err.Error() != PlatformSupportUnavailableReason() {
			t.Fatalf("Verify did not fail closed on unsupported platform: %v", err)
		}
		if _, err := Restore(context.Background(), archivePath, filepath.Join(t.TempDir(), "restored"), keys); err == nil || err.Error() != PlatformSupportUnavailableReason() {
			t.Fatalf("Restore did not fail closed on unsupported platform: %v", err)
		}
		return
	}
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

func TestRestoreRejectsInvalidAuditChainBeforePublication(t *testing.T) {
	archivePath, keys := makeTestSQLiteArchiveWithTamperedAudit(t)
	if !PlatformSupported() {
		return
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(context.Background(), archivePath, destination, keys); err == nil {
		t.Fatal("Restore published an archive with a tampered audit chain")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid audit chain must fail before restore publication, stat err=%v", err)
	}
}

func TestRestoreAuditVerificationRejectsUnexpectedChainPartition(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "restore-audit.sqlite")
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	org, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "valid-chain-org", DisplayName: "Valid Chain Org"})
	if err != nil {
		t.Fatal(err)
	}
	logger, err := audit.NewLogger(audit.LoggerConfig{DB: store.RawDB()})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Append(ctx, port.AuditEvent{
		OrgID: &org.ID, ActorKind: "SYSTEM", ActorID: "restore-test", EntryPoint: "cli",
		EventType: "restore.test", TargetType: "organization", TargetID: org.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RawDB().ExecContext(ctx, `INSERT INTO audit_events
		(id, created_at_ms, org_id, chain_key, seq_no, actor_kind, actor_id, entry_point, event_type, target_type, target_id, metadata_json, prev_hash, entry_hash)
		VALUES ('aud_unexpected', 1, 'unexpected-chain', 'unexpected-chain', 1, 'SYSTEM', 'restore-test', 'cli', 'bad.partition', 'organization', 'none', '{}', printf('%064d', 0), printf('%064d', 0))`); err != nil {
		t.Fatalf("seed unexpected audit partition: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyRestoredAuditChain(ctx, dbPath); err == nil {
		t.Fatal("restore audit verification accepted a chain partition unrelated to an organization or SYSTEM")
	}
}

func TestRestoreSQLiteIntegrityRejectsForeignKeyViolations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "foreign-key-check.sqlite")
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	db := store.RawDB()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE restore_fk_parent (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE restore_fk_child (parent_id INTEGER REFERENCES restore_fk_parent(id))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO restore_fk_child(parent_id) VALUES (99)`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifySQLiteForeignKeys(ctx, dbPath); err == nil {
		t.Fatal("restore foreign-key verification accepted a broken reference")
	}
}

func TestVerifyRejectsTamperedEncryptedArchive(t *testing.T) {
	archivePath, keys := makeTestSQLiteArchive(t)
	if !PlatformSupported() {
		if _, err := Verify(context.Background(), archivePath, keys); err == nil || err.Error() != PlatformSupportUnavailableReason() {
			t.Fatalf("Verify did not fail closed on unsupported platform: %v", err)
		}
		return
	}
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
	return makeTestSQLiteArchiveVariant(t, false)
}

func makeTestSQLiteArchiveWithTamperedAudit(t *testing.T) (string, *archiveTestKeys) {
	return makeTestSQLiteArchiveVariant(t, true)
}

func makeTestSQLiteArchiveVariant(t *testing.T, tamperAudit bool) (string, *archiveTestKeys) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	store, err := sqlite.Open(context.Background(), sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RawDB().Exec(`CREATE TABLE backup_fixture (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO backup_fixture(value) VALUES ('verified snapshot')`); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	logger, err := audit.NewLogger(audit.LoggerConfig{DB: store.RawDB()})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := logger.Append(context.Background(), port.AuditEvent{
		ActorKind: "SYSTEM", ActorID: "backup-restore-test", EntryPoint: "cli",
		EventType: "backup.restore.fixture", TargetType: "backup", TargetID: "fixture",
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if tamperAudit {
		if _, err := store.RawDB().Exec(`DROP TRIGGER trg_audit_events_no_update; UPDATE audit_events SET entry_hash = printf('%064d', 0)`); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
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
