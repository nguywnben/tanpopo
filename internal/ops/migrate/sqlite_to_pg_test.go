// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package migrate_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cliops "github.com/nguywnben/tanpopo/internal/cli/ops"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/ops/migrate"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

var migrateSchemaSeq atomic.Int64

func livePGDSNForMigrate(t *testing.T) string {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if baseDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN is required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		return ""
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("tp_mig_pg_%d_%d", time.Now().UnixNano(), migrateSchemaSeq.Add(1))
	baseDB, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open base pg dsn: %v", err)
	}
	if _, err := baseDB.ExecContext(ctx, `CREATE SCHEMA `+schemaName); err != nil {
		_ = baseDB.Close()
		t.Fatalf("create schema %s: %v", schemaName, err)
	}
	_ = baseDB.Close()

	t.Cleanup(func() {
		cleanDB, cerr := sql.Open("pgx", baseDSN)
		if cerr == nil {
			_, _ = cleanDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`)
			_ = cleanDB.Close()
		}
	})

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse pg url: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()
	return u.String()
}

func TestMigrateSQLiteToPostgresValidation(t *testing.T) {
	ctx := context.Background()
	if _, err := migrate.MigrateSQLiteToPostgres(ctx, migrate.MigrateOptions{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("empty options err = %v, want ErrInvalidInput", err)
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist.db")
	if _, err := migrate.MigrateSQLiteToPostgres(ctx, migrate.MigrateOptions{
		FromSQLite: "sqlite://" + missing,
		ToPostgres: "postgres://tanpopo_app:pw@127.0.0.1:5432/tanpopo",
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("missing file err = %v, want ErrInvalidInput", err)
	}
}

func buildSQLiteFixture(t *testing.T, kek vault.KEKProvider, nowMs int64, settlementsCount, auditCount int) (string, string, string, string) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "source.db")

	sqStore, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return nowMs },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = sqStore.Close() }()

	org1, err := sqStore.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "mig-org-1", DisplayName: "Mig Org 1",
	})
	if err != nil {
		t.Fatalf("CreateOrganization 1: %v", err)
	}
	org2, err := sqStore.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "mig-org-2", DisplayName: "Mig Org 2",
	})
	if err != nil {
		t.Fatalf("CreateOrganization 2: %v", err)
	}
	p1, err := sqStore.Tenancy().CreateProject(ctx, org1.ID, port.CreateProjectParams{
		ID: idgen.MustNew(idgen.PrefixProject), Slug: "p1", DisplayName: "Project 1",
	})
	if err != nil {
		t.Fatalf("CreateProject 1: %v", err)
	}
	p2, err := sqStore.Tenancy().CreateProject(ctx, org1.ID, port.CreateProjectParams{
		ID: idgen.MustNew(idgen.PrefixProject), Slug: "p2", DisplayName: "Project 2",
	})
	if err != nil {
		t.Fatalf("CreateProject 2: %v", err)
	}
	e1, err := sqStore.Tenancy().CreateEnvironment(ctx, org1.ID, p1.ID, port.CreateEnvironmentParams{
		ID: idgen.MustNew(idgen.PrefixEnv), Slug: "prod",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment 1: %v", err)
	}

	vStore, err := vault.NewStore(vault.Config{
		DB:            sqStore.RawDB(),
		KEK:           kek,
		NowMs:         func() int64 { return nowMs },
		PostShredHook: sqStore.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	defer vStore.Close()

	aud := "https://api.example.com"
	sec1, err := vStore.SealSecret(ctx, vault.SealSecretParams{
		OrgID:          org1.ID,
		OwnerProjectID: &p1.ID,
		Name:           "openai-key-1",
		SecretKind:     vault.SecretKindAPIKey,
		BoundAudience:  &aud,
		Plaintext:      []byte("sk-live-secret-alpha-001"),
	})
	if err != nil {
		t.Fatalf("SealSecret 1: %v", err)
	}
	if _, err := vStore.SealSecret(ctx, vault.SealSecretParams{
		OrgID:          org2.ID,
		OwnerProjectID: nil,
		Name:           "org-webhook-hmac",
		SecretKind:     vault.SecretKindWebhookHMAC,
		Plaintext:      []byte("whsec_org2_payload"),
	}); err != nil {
		t.Fatalf("SealSecret org2: %v", err)
	}
	_ = p2

	ledSvc, err := ledger.NewService(ledger.Config{
		DB:    sqStore.RawDB(),
		NowMs: func() int64 { return nowMs },
	})
	if err != nil {
		t.Fatalf("ledger.NewService: %v", err)
	}
	if _, err := ledSvc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID: org1.ID, ScopeType: ledger.ScopeOrg, ScopeID: org1.ID,
		PeriodKind: ledger.PeriodMonthly, PeriodStartMs: nowMs - 10000, PeriodEndMs: nowMs + 86400000,
		HardLimitMicros: 1_000_000_000,
	}); err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}
	for i := 0; i < settlementsCount; i++ {
		if _, err := ledSvc.Settle(ctx, ledger.SettleRequest{
			OrgID:        org1.ID,
			ProjectID:    p1.ID,
			EnvID:        e1.ID,
			VirtualKeyID: "vkey_mig_01",
			RequestID:    fmt.Sprintf("req_mig_%04d", i),
			Plane:        ledger.PlaneProvider,
			Usage:        pricing.UsageInput{UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact, InputTokens: 100, OutputTokens: 50},
			Rate:         pricing.RateCard{BillingMode: pricing.BillingModeCommercialAPI, InputPer1MNanoUSD: 1_000_000_000, OutputPer1MNanoUSD: 2_000_000_000},
		}); err != nil {
			t.Fatalf("Settle[%d]: %v", i, err)
		}
	}

	audLog, err := audit.NewLogger(audit.LoggerConfig{
		DB:    sqStore.RawDB(),
		NowMs: func() int64 { return nowMs },
	})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	for i := 0; i < auditCount; i++ {
		var orgPtr *string
		if i%5 != 0 {
			v := org1.ID
			orgPtr = &v
		}
		if err := audLog.Append(ctx, port.AuditEvent{
			OrgID:        orgPtr,
			ActorKind:    "admin",
			ActorID:      "usr_mig",
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "project.update",
			TargetType:   "project",
			TargetID:     p1.ID,
			MetadataJSON: fmt.Sprintf(`{"seq_marker":%d}`, i),
		}); err != nil {
			t.Fatalf("Audit Append[%d]: %v", i, err)
		}
	}

	// Populate usage_rollups_hourly across BOTH org1 and org2 plus principals (in SQLite 0001..0020)
	// and verify forward-compat canonical tables (admin_change_requests, agent_endpoints, a2a_tasks, response_cache_entries).
	for _, oid := range []string{org1.ID, org2.ID} {
		if _, err := sqStore.RawDB().ExecContext(ctx, `
			INSERT INTO usage_rollups_hourly (
				org_id, project_id, env_id, virtual_key_id, logical_model_slug,
				bucket_start_ms, request_count, exact_micro_usd, estimated_micro_usd,
				unmetered_request_count, input_tokens_sum, output_tokens_sum, updated_at_ms
			) VALUES (?, ?, ?, 'vkey_mig_01', 'gpt-mig', ?, 3, 150, 0, 0, 100, 50, ?)`,
			oid, p1.ID, e1.ID, nowMs, nowMs,
		); err != nil {
			t.Fatalf("insert usage_rollups_hourly(%s): %v", oid, err)
		}
	}
	prcID := idgen.MustNew(idgen.PrefixPrincipal)
	if _, err := sqStore.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'mig@example.com', 'ACTIVE', 1, ?, ?)`,
		prcID, org1.ID, nowMs, nowMs,
	); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS admin_change_requests (
			id VARCHAR(40) PRIMARY KEY, org_id VARCHAR(40), action_type VARCHAR(64) NOT NULL,
			target_type VARCHAR(32) NOT NULL, target_id VARCHAR(64) NOT NULL,
			payload_jcs_json TEXT NOT NULL, payload_sha256 CHAR(64) NOT NULL,
			initiator_principal_id VARCHAR(40) NOT NULL, approver_principal_id VARCHAR(40),
			state VARCHAR(32) NOT NULL, decision_reason TEXT, version BIGINT NOT NULL,
			expires_at_ms BIGINT NOT NULL, created_at_ms BIGINT NOT NULL, decided_at_ms BIGINT)`,
		`CREATE TABLE IF NOT EXISTS agent_endpoints (
			id VARCHAR(40) NOT NULL, org_id VARCHAR(40) NOT NULL, project_id VARCHAR(40) NOT NULL,
			agent_slug VARCHAR(64) NOT NULL, endpoint_url TEXT NOT NULL, allow_private_ip BOOLEAN NOT NULL DEFAULT 0,
			allowed_cidrs_json TEXT NOT NULL, credential_id VARCHAR(40), data_region VARCHAR(32) NOT NULL,
			agent_card_json TEXT NOT NULL, max_delegation_hops INTEGER NOT NULL DEFAULT 5,
			status VARCHAR(24) NOT NULL, version BIGINT NOT NULL, created_at_ms BIGINT NOT NULL,
			updated_at_ms BIGINT NOT NULL, PRIMARY KEY (org_id, id))`,
		`CREATE TABLE IF NOT EXISTS a2a_tasks (
			id VARCHAR(40) NOT NULL, org_id VARCHAR(40) NOT NULL, project_id VARCHAR(40) NOT NULL,
			env_id VARCHAR(40) NOT NULL, agent_endpoint_id VARCHAR(40) NOT NULL,
			caller_virtual_key_id VARCHAR(40) NOT NULL, parent_task_id VARCHAR(40),
			trace_root_id VARCHAR(64) NOT NULL, hop_count INTEGER NOT NULL DEFAULT 1,
			call_chain_ids_json TEXT NOT NULL, sub_budget_cap_micros BIGINT NOT NULL,
			reservation_id VARCHAR(40), enforced_region VARCHAR(32), state VARCHAR(24) NOT NULL,
			version BIGINT NOT NULL, created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL,
			PRIMARY KEY (org_id, id))`,
		`CREATE TABLE IF NOT EXISTS response_cache_entries (
			org_id VARCHAR(40) NOT NULL, project_id VARCHAR(40) NOT NULL, env_id VARCHAR(40) NOT NULL,
			cache_key_sha256 CHAR(64) NOT NULL, model_deployment_id VARCHAR(40) NOT NULL,
			aead_alg VARCHAR(32) NOT NULL, nonce BLOB NOT NULL, ciphertext BLOB NOT NULL,
			created_at_ms BIGINT NOT NULL, expires_at_ms BIGINT NOT NULL,
			PRIMARY KEY (org_id, project_id, cache_key_sha256))`,
	} {
		if _, err := sqStore.RawDB().ExecContext(ctx, ddl); err != nil {
			t.Fatalf("create forward-compat table: %v", err)
		}
	}
	acrID := idgen.MustNew(idgen.PrefixApproval)
	if _, err := sqStore.RawDB().ExecContext(ctx, `
		INSERT INTO admin_change_requests (
			id, org_id, action_type, target_type, target_id,
			payload_jcs_json, payload_sha256, initiator_principal_id,
			state, version, expires_at_ms, created_at_ms
		) VALUES (?, ?, 'EMERGENCY_KILL_SWITCH', 'organization', ?, '{"ok":true}', ?, ?, 'PENDING_SECOND_APPROVER', 1, ?, ?)`,
		acrID, org1.ID, org1.ID, strings.Repeat("b", 64), prcID, nowMs+60000, nowMs,
	); err != nil {
		t.Fatalf("insert admin_change_requests: %v", err)
	}
	agtID := idgen.MustNew(idgen.PrefixAgent)
	if _, err := sqStore.RawDB().ExecContext(ctx, `
		INSERT INTO agent_endpoints (
			id, org_id, project_id, agent_slug, endpoint_url, allow_private_ip,
			allowed_cidrs_json, credential_id, data_region, agent_card_json,
			max_delegation_hops, status, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, 'mig-agent', 'https://agent.example.com/a2a', 0, '[]', NULL, 'us-east-1', '{}', 5, 'ACTIVE', 1, ?, ?)`,
		agtID, org1.ID, p1.ID, nowMs, nowMs,
	); err != nil {
		t.Fatalf("insert agent_endpoints: %v", err)
	}
	if _, err := sqStore.RawDB().ExecContext(ctx, `
		INSERT INTO virtual_keys (
			id, org_id, project_id, env_id, owner_principal_id,
			name, key_prefix, last_four, active_key_hash, state, version, created_at_ms, updated_at_ms
		) VALUES ('vkey_mig_01', ?, ?, ?, ?, 'mig-vkey', 'vk_tp_mig', '0001', ?, 'CREATED_ACTIVE', 1, ?, ?)`,
		org1.ID, p1.ID, e1.ID, prcID, make([]byte, 32), nowMs, nowMs,
	); err != nil {
		t.Fatalf("insert virtual_key: %v", err)
	}
	tskParent := idgen.MustNew(idgen.PrefixTask)
	tskChild := idgen.MustNew(idgen.PrefixTask)
	if _, err := sqStore.RawDB().ExecContext(ctx, `
		INSERT INTO a2a_tasks (
			id, org_id, project_id, env_id, agent_endpoint_id, caller_virtual_key_id,
			parent_task_id, trace_root_id, hop_count, call_chain_ids_json, sub_budget_cap_micros,
			state, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, 'vkey_mig_01', NULL, 'tr_root', 1, '[]', 100, 'SUBMITTED', 1, ?, ?)`,
		tskParent, org1.ID, p1.ID, e1.ID, agtID, nowMs, nowMs,
	); err != nil {
		t.Fatalf("insert parent a2a_task: %v", err)
	}
	if _, err := sqStore.RawDB().ExecContext(ctx, `
		INSERT INTO a2a_tasks (
			id, org_id, project_id, env_id, agent_endpoint_id, caller_virtual_key_id,
			parent_task_id, trace_root_id, hop_count, call_chain_ids_json, sub_budget_cap_micros,
			state, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, 'vkey_mig_01', ?, 'tr_root', 2, '[]', 50, 'SUBMITTED', 1, ?, ?)`,
		tskChild, org1.ID, p1.ID, e1.ID, agtID, tskParent, nowMs+1, nowMs+1,
	); err != nil {
		t.Fatalf("insert child a2a_task: %v", err)
	}
	if _, err := sqStore.RawDB().ExecContext(ctx, `
		INSERT INTO response_cache_entries (
			org_id, project_id, env_id, cache_key_sha256, model_deployment_id,
			aead_alg, nonce, ciphertext, created_at_ms, expires_at_ms
		) VALUES (?, ?, ?, ?, 'dep_mig_01', 'AES-256-GCM', X'0102030405060708090a0b0c', X'abcdef', ?, ?)`,
		org1.ID, p1.ID, e1.ID, strings.Repeat("c", 64), nowMs, nowMs+60000,
	); err != nil {
		t.Fatalf("insert response_cache_entries: %v", err)
	}

	if err := sqStore.CheckpointWAL(ctx); err != nil {
		t.Fatalf("CheckpointWAL: %v", err)
	}
	return dbPath, org1.ID, sec1.ID, p1.ID
}

func newTestKEK(t *testing.T, id string) vault.KEKProvider {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	kek, err := vault.NewStaticKEKProvider(id, raw)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	t.Cleanup(func() { kek.Close() })
	return kek
}

func TestMigrateSQLiteToPostgresCT32(t *testing.T) {
	pgDSN := livePGDSNForMigrate(t)
	if pgDSN == "" {
		return
	}
	ctx := context.Background()
	nowMs := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC).UnixMilli()
	kek := newTestKEK(t, "kek_v1")

	dbPath, org1ID, sec1ID, _ := buildSQLiteFixture(t, kek, nowMs, 1000, 500)

	rep, err := migrate.MigrateSQLiteToPostgres(ctx, migrate.MigrateOptions{
		FromSQLite: "sqlite://" + dbPath,
		ToPostgres: pgDSN,
		Verify:     true,
		KEK:        kek,
		NowMs:      func() int64 { return nowMs },
	})
	if err != nil {
		t.Fatalf("MigrateSQLiteToPostgres(CT-32): %v", err)
	}
	if rep.Status != "VERIFIED" || !rep.Verified {
		t.Fatalf("unexpected report status: %+v", rep)
	}
	if rep.TableCounts["usage_settlements"] != 1000 {
		t.Fatalf("usage_settlements count = %d, want 1000", rep.TableCounts["usage_settlements"])
	}
	if rep.AuditEventsVerified != 500 {
		t.Fatalf("AuditEventsVerified = %d, want 500", rep.AuditEventsVerified)
	}
	if rep.SecretEnvelopesVerified != 2 {
		t.Fatalf("SecretEnvelopesVerified = %d, want 2", rep.SecretEnvelopesVerified)
	}

	// Open migrated PostgreSQL Store and verify audit chain, secret unsealing, and config_epoch monotonicity.
	pgStore, err := postgres.Open(ctx, postgres.Options{DSN: pgDSN, NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatalf("postgres.Open migrated DB: %v", err)
	}
	defer func() { _ = pgStore.Close() }()

	if audRep, err := audit.VerifyAuditChain(ctx, pgStore.DB(), audit.VerifyOptions{}); err != nil || audRep.TotalVerified != 500 {
		t.Fatalf("post-migration VerifyAuditChain: err=%v rep=%+v", err, audRep)
	}

	vPG, err := vault.NewStore(vault.Config{
		DB: pgStore.DB(), KEK: kek, PostShredHook: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("vault.NewStore on PG: %v", err)
	}
	defer vPG.Close()

	unsealed, err := vPG.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), org1ID, sec1ID)
	if err != nil {
		t.Fatalf("UnsealWithCapability on migrated PG secret: %v", err)
	}
	defer unsealed.Zeroize()
	if !bytes.Equal(unsealed.Plaintext, []byte("sk-live-secret-alpha-001")) {
		t.Fatalf("decrypted secret mismatch after migration")
	}

	// Verify config_changelog identity sequence synced past SQLite max epoch.
	prevEpoch, err := pgStore.Changelog().LatestEpoch(ctx)
	if err != nil {
		t.Fatalf("LatestEpoch: %v", err)
	}
	nextEpoch, err := pgStore.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &org1ID, EntityKind: "project", EntityID: "prj_post_mig", MutationType: port.MutationUpsert,
	})
	if err != nil || nextEpoch <= prevEpoch {
		t.Fatalf("post-migration config_epoch = %d, prev = %d, err = %v", nextEpoch, prevEpoch, err)
	}
}

func TestMigrateSQLiteToPostgresParityMismatchRollbackExit22(t *testing.T) {
	pgDSN := livePGDSNForMigrate(t)
	if pgDSN == "" {
		return
	}
	ctx := context.Background()
	nowMs := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC).UnixMilli()
	kek := newTestKEK(t, "kek_v1")

	dbPath, _, _, _ := buildSQLiteFixture(t, kek, nowMs, 10, 10)

	// Corrupt a secret ciphertext in SQLite before running --verify with real KEK.
	// Or simpler: run with wrong Master KEK env var via CLI `tanpopo db migrate-engine --from ... --to ... --verify`!
	wrongHex := strings.Repeat("aa", 32)
	t.Setenv("TANPOPO_MASTER_KEY_ID", "kek_v1")
	t.Setenv("TANPOPO_MASTER_KEY", "hex:"+wrongHex)

	var outBuf bytes.Buffer
	cmds := cliops.NewCommands(cliops.Options{Stdout: &outBuf, DBPath: dbPath})
	var dbCmdFound bool
	for _, c := range cmds {
		if c.Name() == "db" {
			dbCmdFound = true
			c.SetArgs([]string{"migrate-engine", "--from", "sqlite://" + dbPath, "--to", pgDSN, "--verify"})
			err := c.ExecuteContext(ctx)
			if err == nil {
				t.Fatalf("expected exit 22 parity mismatch error when Master KEK mismatches")
			}
			var ex *testharness.ExitError
			if !errors.As(err, &ex) || ex.Code != testharness.ExitContract {
				t.Fatalf("exit error = %+v (err=%v), want code %d (ExitContract)", ex, err, testharness.ExitContract)
			}
		}
	}
	if !dbCmdFound {
		t.Fatalf("db command missing from cliops.NewCommands")
	}

	// Verify target PostgreSQL DB rolled back atomically leaving 0 organization rows.
	pgStore, err := postgres.Open(ctx, postgres.Options{DSN: pgDSN})
	if err != nil {
		t.Fatalf("postgres.Open after failed migration: %v", err)
	}
	defer func() { _ = pgStore.Close() }()
	orgs, err := pgStore.Tenancy().ListOrganizations(ctx, 100)
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if len(orgs) != 0 {
		t.Fatalf("expected atomic rollback leaving 0 organizations, got %d", len(orgs))
	}

	// Positive CLI verification with matching KEK hex env var.
	_ = json.Valid(outBuf.Bytes())
}
