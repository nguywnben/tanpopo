// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func TestVerifyStateSQLiteAuditReclaimAndTamperDetection(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "verify_state.db")
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	db := store.RawDB()

	nowMs := int64(1_700_000_000_000)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO organizations (id, slug, display_name, status, version, created_at_ms, updated_at_ms)
		VALUES ('org_vs', 'vs', 'VS Org', 'ACTIVE', 1, ?, ?)`, nowMs, nowMs); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	ledgerSvc, err := ledger.NewService(ledger.Config{DB: db, Audit: auditLog, NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatalf("ledger.NewService: %v", err)
	}
	if _, err := ledgerSvc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		ID:              "bdg_vs",
		OrgID:           "org_vs",
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         "org_vs",
		PeriodKind:      ledger.PeriodMonthly,
		PeriodStartMs:   nowMs - 1000,
		PeriodEndMs:     nowMs + 86_400_000,
		HardLimitMicros: 5_000_000,
	}); err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, org_id, slug, display_name, status, version, created_at_ms, updated_at_ms)
		VALUES ('prj_vs', 'org_vs', 'vs-proj', 'VS Project', 'ACTIVE', 1, ?, ?)`, nowMs, nowMs); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO environments (id, org_id, project_id, slug, version, created_at_ms, updated_at_ms)
		VALUES ('env_vs', 'org_vs', 'prj_vs', 'prod', 1, ?, ?)`, nowMs, nowMs); err != nil {
		t.Fatalf("insert env: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO virtual_keys (id, org_id, project_id, env_id, name, key_prefix, last_four, active_key_hash, state, version, created_at_ms, updated_at_ms)
		VALUES ('vk_vs', 'org_vs', 'prj_vs', 'env_vs', 'vs-key', 'vk_tp_test_', '1234', ?, 'CREATED_ACTIVE', 1, ?, ?)`, make([]byte, 32), nowMs, nowMs); err != nil {
		t.Fatalf("insert vkey: %v", err)
	}
	// Expired orphan hold (nowMs is 1_700_000_000_000 with 1ms TTL, expired vs real wall-clock time).
	if _, err := ledgerSvc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_vs",
		ProjectID:    "prj_vs",
		EnvID:        "env_vs",
		VirtualKeyID: "vk_vs",
		RequestID:    "req_orphan",
		HoldMicros:   250_000,
		LeaseTTLMs:   1,
	}); err != nil {
		t.Fatalf("Reserve orphan hold: %v", err)
	}
	orgID := "org_vs"
	if err := auditLog.Append(ctx, port.AuditEvent{
		ID:          "aud_vs_1",
		CreatedAtMs: nowMs,
		OrgID:       &orgID,
		ActorKind:   "SYSTEM",
		ActorID:     "test",
		EntryPoint:  port.AuditEntryPointAdminAPI,
		EventType:   "test.verify",
		TargetType:  "organization",
		TargetID:    "org_vs",
	}); err != nil {
		t.Fatalf("append audit event: %v", err)
	}
	_ = store.Close()

	var stdout bytes.Buffer
	outFmt := "json"
	root := NewCommandWithLocale(&stdout, i18n.LocaleEn, &outFmt)
	root.SetArgs([]string{"verify-state", "--db", dbPath})
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("verify-state failed: %v (output=%s)", err, stdout.String())
	}

	var rep VerifyStateReport
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if rep.Status != "ok" || !rep.AuditChainValid || rep.AuditEventsVerified < 1 || rep.OrphansReconciled != 1 {
		t.Fatalf("unexpected VerifyStateReport: %+v", rep)
	}

	// Verify tamper injection fails closed with ExitSecurity.
	tamperStore, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	if _, err := tamperStore.RawDB().ExecContext(ctx, `
		INSERT INTO audit_events (id, seq_no, created_at_ms, chain_key, org_id, actor_kind, actor_id, entry_point, event_type, target_type, target_id, metadata_json, prev_hash, entry_hash)
		VALUES ('aud_vs_forged', 99, ?, 'org_vs', 'org_vs', 'SYSTEM', 'attacker', 'admin_api', 'forged', 'organization', 'org_vs', '{}', '0000', 'badhash')`, nowMs); err != nil {
		t.Fatalf("tamper audit event: %v", err)
	}
	_ = tamperStore.Close()

	stdout.Reset()
	root2 := NewCommandWithLocale(&stdout, i18n.LocaleEn, &outFmt)
	root2.SetArgs([]string{"verify-state", "--db", dbPath})
	err = root2.ExecuteContext(ctx)
	if err == nil {
		t.Fatalf("expected verify-state to fail closed on tampered audit chain")
	}
	var ee *testharness.ExitError
	if !errors.As(err, &ee) || ee.Code != testharness.ExitSecurity {
		t.Fatalf("expected ExitSecurity (code=%d), got %T (%v)", testharness.ExitSecurity, err, err)
	}
}
