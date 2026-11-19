// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package migrate

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	sqlitemigrations "github.com/nguywnben/tanpopo/db/migrations/sqlite"
)

func TestSQLiteMigration0016OAuthStateFreshAndUpgrade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	all, err := LoadMigrations(sqlitemigrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 16 || all[len(all)-1].Version != 16 {
		t.Fatalf("expected ordered migrations through 0016, got %d entries; last=%+v", len(all), all[len(all)-1])
	}

	fresh := openTestSQLite(t)
	if _, err := Apply(ctx, fresh, sqlitemigrations.FS, func() int64 { return 1760000000000 }); err != nil {
		t.Fatalf("fresh Apply through 0016: %v", err)
	}
	assertOAuthMigrationObjects(t, fresh)

	preFS := fstest.MapFS{}
	entries, err := fs.ReadDir(sqlitemigrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if strings.HasSuffix(ent.Name(), ".sql") && ent.Name() < "0016_" {
			data, err := fs.ReadFile(sqlitemigrations.FS, ent.Name())
			if err != nil {
				t.Fatal(err)
			}
			preFS[ent.Name()] = &fstest.MapFile{Data: data}
		}
	}
	upgrade := openTestSQLite(t)
	pre, err := Apply(ctx, upgrade, preFS, func() int64 { return 1760000000000 })
	if err != nil || pre.LatestVersion != 15 {
		t.Fatalf("pre-upgrade summary = %+v, err=%v", pre, err)
	}
	post, err := Apply(ctx, upgrade, sqlitemigrations.FS, func() int64 { return 1760000001000 })
	if err != nil || post.LatestVersion != 16 || post.NewlyApplied != 1 {
		t.Fatalf("upgrade summary = %+v, err=%v", post, err)
	}
	assertOAuthMigrationObjects(t, upgrade)
	if _, err := upgrade.ExecContext(ctx, `INSERT INTO organizations (id, slug, display_name, created_at_ms, updated_at_ms) VALUES ('org_seed', 'oauth-org', 'OAuth Org', ?, ?)`, int64(1760000000000), int64(1760000000000)); err != nil {
		t.Fatal(err)
	}
	if _, err := upgrade.ExecContext(ctx, `INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms) VALUES ('prc_seed', 'org_seed', 'HUMAN_LOCAL', 'owner@example.invalid', 'ACTIVE', 1, ?, ?)`, int64(1760000000000), int64(1760000000000)); err != nil {
		t.Fatal(err)
	}

	// Reject PKCE downgrade, invalid states/grants and malformed pending rows.
	now := int64(1760000002000)
	for _, stmt := range []string{
		`INSERT INTO oauth_authorization_sessions (id, org_id, principal_id, browser_session_hash, target_kind, target_id, grant_type, state, code_challenge_method, session_status, expires_at_ms, created_at_ms) VALUES ('oas_bad_plain', 'org_seed', 'prc_seed', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'PROVIDER_CONNECTION', 'con_seed', 'AUTHORIZATION_CODE_PKCE', 'state-plain', 'plain', 'PENDING', ?, ?)`,
		`INSERT INTO oauth_authorization_sessions (id, org_id, principal_id, browser_session_hash, target_kind, target_id, grant_type, state, session_status, expires_at_ms, created_at_ms) VALUES ('oas_bad_grant', 'org_seed', 'prc_seed', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'PROVIDER_CONNECTION', 'con_seed', 'IMPLICIT', 'state-grant', 'PENDING', ?, ?)`,
		`INSERT INTO oauth_authorization_sessions (id, org_id, principal_id, browser_session_hash, target_kind, target_id, grant_type, state, sealed_code_verifier, verifier_nonce, device_code, session_status, expires_at_ms, created_at_ms) VALUES ('oas_bad_device', 'org_seed', 'prc_seed', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'PROVIDER_CONNECTION', 'con_seed', 'DEVICE_CODE', 'state-device', x'01', x'02', 'raw-device-code', 'PENDING', ?, ?)`,
	} {
		if _, err := upgrade.ExecContext(ctx, stmt, now+60000, now); err == nil {
			t.Fatalf("expected contract constraint failure for %s", stmt)
		}
	}
	if _, err := upgrade.ExecContext(ctx, `INSERT INTO oauth_authorization_sessions (id, org_id, principal_id, browser_session_hash, target_kind, target_id, grant_type, state, sealed_code_verifier, verifier_nonce, session_status, expires_at_ms, created_at_ms) VALUES ('oas_valid', 'org_seed', 'prc_seed', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'PROVIDER_CONNECTION', 'con_seed', 'AUTHORIZATION_CODE_PKCE', 'state-valid', x'01', x'02', 'PENDING', ?, ?)`, now+60000, now); err != nil {
		t.Fatalf("valid S256 session insert failed: %v", err)
	}
}

func assertOAuthMigrationObjects(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, name := range []string{"oauth_authorization_sessions", "oauth_token_states", "oauth_token_leases", "idx_oauth_sessions_pending_expiry", "idx_oauth_token_states_refresh"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&count); err != nil || count != 1 {
			t.Fatalf("SQLite object %q count=%d err=%v", name, count, err)
		}
	}
}
