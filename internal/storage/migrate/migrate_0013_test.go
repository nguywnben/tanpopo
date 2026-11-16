// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package migrate

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	sqlitemigrations "github.com/nguywnben/tanpopo/db/migrations/sqlite"
)

func TestSQLiteMigration0013TracesAndBlobsFreshAndUpgrade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Fresh DB applying all embedded migrations 0001..0013.
	dbFresh := openTestSQLite(t)
	sumFresh, err := Apply(ctx, dbFresh, sqlitemigrations.FS, func() int64 { return 1760000000000 })
	if err != nil {
		t.Fatalf("fresh Apply(0001..0013): %v", err)
	}
	if sumFresh.LatestVersion < 13 || sumFresh.NewlyApplied < 13 {
		t.Fatalf("expected >=13 migrations applied fresh, got %+v", sumFresh)
	}

	// 2. Existing DB upgrade path: apply 0001..0012 first, then apply full FS including 0013+.
	preFS := fstest.MapFS{}
	entries, err := fs.ReadDir(sqlitemigrations.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir sqlitemigrations.FS: %v", err)
	}
	for _, ent := range entries {
		if !strings.HasSuffix(ent.Name(), ".sql") || ent.Name() >= "0013_" {
			continue
		}
		data, rerr := fs.ReadFile(sqlitemigrations.FS, ent.Name())
		if rerr != nil {
			t.Fatalf("ReadFile(%s): %v", ent.Name(), rerr)
		}
		preFS[ent.Name()] = &fstest.MapFile{Data: data}
	}

	dbUpgrade := openTestSQLite(t)
	sumPre, err := Apply(ctx, dbUpgrade, preFS, func() int64 { return 1760000000000 })
	if err != nil {
		t.Fatalf("Apply(0001..0012): %v", err)
	}
	if sumPre.LatestVersion != 12 {
		t.Fatalf("expected pre-upgrade LatestVersion=12, got %+v", sumPre)
	}
	sumUp, err := Apply(ctx, dbUpgrade, sqlitemigrations.FS, func() int64 { return 1760000001000 })
	if err != nil {
		t.Fatalf("upgrade Apply(->0013): %v", err)
	}
	if sumUp.NewlyApplied < 1 || sumUp.AlreadyApplied != 12 || sumUp.LatestVersion < 13 {
		t.Fatalf("unexpected upgrade summary: %+v", sumUp)
	}

	// 3. Verify CHECK constraint <= 72h TTL and ON DELETE CASCADE on debug_payload_blobs.
	nowMs := int64(1760000000000)
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO organizations (id, slug, display_name, created_at_ms, updated_at_ms)
		VALUES ('org_m13', 'org-m13', 'Org M13', ?, ?)`, nowMs, nowMs); err != nil {
		t.Fatal(err)
	}
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO projects (id, org_id, slug, display_name, created_at_ms, updated_at_ms)
		VALUES ('prj_m13', 'org_m13', 'prj-m13', 'Proj M13', ?, ?)`, nowMs, nowMs); err != nil {
		t.Fatal(err)
	}
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO org_encryption_keys (org_id, kek_key_id, nonce, wrapped_org_dek, dek_version, key_generation, created_at_ms, rotated_at_ms)
		VALUES ('org_m13', 'kek_v1', X'02', X'01', 1, 1, ?, ?)`, nowMs, nowMs); err != nil {
		t.Fatal(err)
	}
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO project_encryption_keys (org_id, project_id, nonce, wrapped_project_dek, dek_version, key_generation, created_at_ms, rotated_at_ms)
		VALUES ('org_m13', 'prj_m13', X'02', X'01', 1, 1, ?, ?)`, nowMs, nowMs); err != nil {
		t.Fatal(err)
	}

	// Attempt inserting > 72h debug blob (259200001 ms) -> CHECK fails.
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO debug_payload_blobs (request_id, org_id, project_id, aead_alg, nonce, encrypted_payload_aead, created_at_ms, expires_at_ms)
		VALUES ('req_gt72h', 'org_m13', 'prj_m13', 'AES-256-GCM', X'01', X'02', ?, ?)`,
		nowMs, nowMs+259200001); err == nil {
		t.Fatalf("expected CHECK constraint failure for > 72h debug_payload_blobs TTL")
	}

	// Valid <= 72h debug blob succeeds and cascades when project_encryption_keys row deletes.
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO debug_payload_blobs (request_id, org_id, project_id, aead_alg, nonce, encrypted_payload_aead, created_at_ms, expires_at_ms)
		VALUES ('req_ok72h', 'org_m13', 'prj_m13', 'AES-256-GCM', X'01', X'02', ?, ?)`,
		nowMs, nowMs+259200000); err != nil {
		t.Fatalf("insert valid <=72h blob: %v", err)
	}
	if _, err := dbUpgrade.ExecContext(ctx, `DELETE FROM project_encryption_keys WHERE org_id = 'org_m13' AND project_id = 'prj_m13'`); err != nil {
		t.Fatalf("delete parent project_encryption_keys: %v", err)
	}
	var cnt int
	if err := dbUpgrade.QueryRowContext(ctx, `SELECT count(*) FROM debug_payload_blobs WHERE request_id = 'req_ok72h'`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("expected ON DELETE CASCADE to delete debug_payload_blobs row, got count=%d", cnt)
	}
}
