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

func TestSQLiteMigration0014AuditAndKillSwitchFreshAndUpgrade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Fresh DB applying all embedded migrations 0001..0014.
	dbFresh := openTestSQLite(t)
	sumFresh, err := Apply(ctx, dbFresh, sqlitemigrations.FS, func() int64 { return 1760000000000 })
	if err != nil {
		t.Fatalf("fresh Apply(0001..0014): %v", err)
	}
	if sumFresh.LatestVersion < 14 || sumFresh.NewlyApplied < 14 {
		t.Fatalf("expected >=14 migrations applied fresh, got %+v", sumFresh)
	}

	// 2. Existing DB upgrade path: apply 0001..0013 first, then apply full FS including 0014+.
	preFS := fstest.MapFS{}
	entries, err := fs.ReadDir(sqlitemigrations.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir sqlitemigrations.FS: %v", err)
	}
	for _, ent := range entries {
		if !strings.HasSuffix(ent.Name(), ".sql") || ent.Name() >= "0014_" {
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
		t.Fatalf("Apply(0001..0013): %v", err)
	}
	if sumPre.LatestVersion != 13 {
		t.Fatalf("expected pre-upgrade LatestVersion=13, got %+v", sumPre)
	}
	sumUp, err := Apply(ctx, dbUpgrade, sqlitemigrations.FS, func() int64 { return 1760000001000 })
	if err != nil {
		t.Fatalf("upgrade Apply(->0014): %v", err)
	}
	if sumUp.NewlyApplied < 1 || sumUp.AlreadyApplied != 13 || sumUp.LatestVersion < 14 {
		t.Fatalf("unexpected upgrade summary: %+v", sumUp)
	}

	// 3. NEG-1: Verify audit_events immutability triggers forbid UPDATE and DELETE,
	//    and CHECK constraints enforce chain_key, entry_point, and kill-switch reason length.
	nowMs := int64(1760000000000)
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, created_at_ms, org_id, chain_key, seq_no,
			actor_kind, actor_id, entry_point, event_type, target_type, target_id,
			request_id, metadata_json, prev_hash, entry_hash
		) VALUES (
			'aud_01m14', ?, NULL, 'SYSTEM', 1,
			'SYSTEM', 'bootstrap', 'admin_api', 'instance.bootstrapped', 'instance', 'inst_1',
			NULL, '{}', '`+strings.Repeat("0", 64)+`', '`+strings.Repeat("a", 64)+`'
		)`, nowMs); err != nil {
		t.Fatalf("insert valid SYSTEM audit event: %v", err)
	}

	if _, err := dbUpgrade.ExecContext(ctx, `UPDATE audit_events SET actor_id = 'x' WHERE id = 'aud_01m14'`); err == nil || !strings.Contains(err.Error(), "audit_events_immutable") {
		t.Fatalf("expected UPDATE to abort with audit_events_immutable, got %v", err)
	}
	if _, err := dbUpgrade.ExecContext(ctx, `DELETE FROM audit_events WHERE id = 'aud_01m14'`); err == nil || !strings.Contains(err.Error(), "audit_events_immutable") {
		t.Fatalf("expected DELETE to abort with audit_events_immutable, got %v", err)
	}

	// Invalid entry_point must fail CHECK constraint.
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, created_at_ms, org_id, chain_key, seq_no,
			actor_kind, actor_id, entry_point, event_type, target_type, target_id,
			request_id, metadata_json, prev_hash, entry_hash
		) VALUES (
			'aud_02m14', ?, NULL, 'SYSTEM', 2,
			'SYSTEM', 'bootstrap', 'invalid_ep', 'instance.test', 'instance', 'inst_1',
			NULL, '{}', '`+strings.Repeat("a", 64)+`', '`+strings.Repeat("b", 64)+`'
		)`, nowMs+1); err == nil {
		t.Fatalf("expected invalid entry_point CHECK constraint failure")
	}

	// Verify emergency_kill_switches reason length CHECK (>= 5) and FK constraints.
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO organizations (id, slug, display_name, created_at_ms, updated_at_ms)
		VALUES ('org_m14', 'org-m14', 'Org M14', ?, ?)`, nowMs, nowMs); err != nil {
		t.Fatal(err)
	}
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ('prn_m14', 'org_m14', 'HUMAN_LOCAL', 'sec@example.com', 'ACTIVE', 1, ?, ?)`, nowMs, nowMs); err != nil {
		t.Fatal(err)
	}
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO emergency_kill_switches (
			id, org_id, level, target_id, active, abort_active_streams, reason,
			triggered_by_principal_id, version, triggered_at_ms
		) VALUES ('ksw_short', 'org_m14', 'VIRTUAL_KEY', 'vk_1', 1, 1, 'bad', 'prn_m14', 1, ?)`, nowMs); err == nil {
		t.Fatalf("expected CHECK failure for kill-switch reason shorter than 5 chars")
	}
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO emergency_kill_switches (
			id, org_id, level, target_id, active, abort_active_streams, reason,
			triggered_by_principal_id, version, triggered_at_ms
		) VALUES ('ksw_valid', 'org_m14', 'EXTENSION_INSTALLATION', 'ins_1', 1, 1, 'compromised CVE-2026-0001', 'prn_m14', 1, ?)`, nowMs); err != nil {
		t.Fatalf("insert valid emergency_kill_switches row: %v", err)
	}

	// Verify idx_killswitches_active_unique rejects duplicate active switch on same tuple (including NULL org_id).
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO emergency_kill_switches (
			id, org_id, level, target_id, active, abort_active_streams, reason,
			triggered_by_principal_id, version, triggered_at_ms
		) VALUES ('ksw_dup_active', 'org_m14', 'EXTENSION_INSTALLATION', 'ins_1', 1, 1, 'duplicate active switch', 'prn_m14', 1, ?)`, nowMs); err == nil {
		t.Fatalf("expected UNIQUE constraint failure on duplicate active kill switch")
	}
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO emergency_kill_switches (
			id, org_id, level, target_id, active, abort_active_streams, reason,
			triggered_by_principal_id, version, triggered_at_ms
		) VALUES ('ksw_inst_1', NULL, 'EXTENSION_INSTALLATION', 'ins_inst', 1, 1, 'instance quarantine', 'prn_m14', 1, ?)`, nowMs); err != nil {
		t.Fatalf("insert instance-scoped kill switch: %v", err)
	}
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO emergency_kill_switches (
			id, org_id, level, target_id, active, abort_active_streams, reason,
			triggered_by_principal_id, version, triggered_at_ms
		) VALUES ('ksw_inst_dup', NULL, 'EXTENSION_INSTALLATION', 'ins_inst', 1, 1, 'duplicate instance quarantine', 'prn_m14', 1, ?)`, nowMs); err == nil {
		t.Fatalf("expected UNIQUE constraint failure on duplicate active NULL-org kill switch")
	}

	// Verify clear_reason CHECK constraint (NULL or length >= 5).
	if _, err := dbUpgrade.ExecContext(ctx, `
		UPDATE emergency_kill_switches SET active = 0, clear_reason = 'no' WHERE id = 'ksw_valid'`); err == nil {
		t.Fatalf("expected CHECK constraint failure for clear_reason shorter than 5 chars")
	}
	if _, err := dbUpgrade.ExecContext(ctx, `
		UPDATE emergency_kill_switches SET active = 0, cleared_by_principal_id = 'prn_m14', clear_reason = 'verified clean', cleared_at_ms = ? WHERE id = 'ksw_valid'`, nowMs+5); err != nil {
		t.Fatalf("clear emergency_kill_switches with valid clear_reason: %v", err)
	}
	// Historical inactive row must allow re-triggering active=1 on same target.
	if _, err := dbUpgrade.ExecContext(ctx, `
		INSERT INTO emergency_kill_switches (
			id, org_id, level, target_id, active, abort_active_streams, reason,
			triggered_by_principal_id, version, triggered_at_ms
		) VALUES ('ksw_retrigger', 'org_m14', 'EXTENSION_INSTALLATION', 'ins_1', 1, 1, 're-triggered after clear', 'prn_m14', 1, ?)`, nowMs+10); err != nil {
		t.Fatalf("expected re-triggering cleared target to succeed: %v", err)
	}
}
