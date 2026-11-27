// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package killswitch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

var ksPGSchemaSeq atomic.Int64

type ksPGHarness struct {
	st        *postgres.Store
	fk        *clock.FakeClock
	logger    *audit.Logger
	reg       *Registry
	orgAID    string
	orgBID    string
	projA1ID  string
	envA1ID   string
	prnAID    string
	prnBootOp string
	prnNullOp string
	vkA1ID    string
	inst1ID   string
	inst2ID   string
}

func newKillSwitchPGHarness(t *testing.T) *ksPGHarness {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if baseDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		t.Log("TANPOPO_TEST_PG_DSN not set; skipping live Postgres killswitch RLS tests")
		return nil
	}

	ctx := context.Background()
	schemaName := fmt.Sprintf("tp_ks_pg_%d_%d", time.Now().UnixNano(), ksPGSchemaSeq.Add(1))
	baseDB, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open base pg: %v", err)
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

	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	nowMs := func() int64 { return fk.Now().UTC().UnixMilli() }
	st, err := postgres.Open(ctx, postgres.Options{DSN: u.String(), NowMs: nowMs})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	logger, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB(), NowMs: nowMs})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "ks-pg-a", DisplayName: "KS PG A"})
	if err != nil {
		t.Fatalf("create orgA: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "ks-pg-b", DisplayName: "KS PG B"})
	if err != nil {
		t.Fatalf("create orgB: %v", err)
	}
	proj1, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "ks-pg-p1", DisplayName: "PG P1"})
	if err != nil {
		t.Fatalf("create proj1: %v", err)
	}
	env1, err := st.Tenancy().CreateEnvironment(ctx, orgA.ID, proj1.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatalf("create env1: %v", err)
	}

	now := nowMs()
	prnOrgOwnerA := "prn_pg_owner_a"
	prnBootOp := "prn_pg_boot_op" // Exact bootstrap/setup shape: org_id IS NOT NULL + INSTANCE OPERATOR binding
	prnNullOp := "prn_pg_null_op" // Pure instance operator: org_id IS NULL + INSTANCE OPERATOR binding
	vk1 := "vk_pg_1"
	pkg1, pkg2 := "pkg_pg_1", "pkg_pg_2"
	ins1, ins2 := "ins_pg_1", "ins_pg_2"

	tx, err := st.RawDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := port.SetOrgRLSTx(ctx, tx, port.DialectPostgres, orgA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ($1, $2, 'HUMAN_LOCAL', 'owner-a@pg.local', 'ACTIVE', 1, $3, $3),
		       ($4, $2, 'HUMAN_LOCAL', 'boot-op@pg.local', 'ACTIVE', 1, $3, $3)`,
		prnOrgOwnerA, orgA.ID, now, prnBootOp,
	); err != nil {
		t.Fatalf("insert orgA principals: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_pg_owner_a', $1, $2, 'ORG', $1, 'ORG_OWNER', 1, $3),
		       ('rb_pg_boot_owner', $1, $4, 'ORG', $1, 'ORG_OWNER', 1, $3)`,
		orgA.ID, prnOrgOwnerA, now, prnBootOp,
	); err != nil {
		t.Fatalf("insert orgA bindings: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO virtual_keys (
			id, org_id, project_id, env_id, name, key_prefix, last_four, active_key_hash,
			state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			max_concurrent_streams, version, created_at_ms, updated_at_ms
		) VALUES ($1, $2, $3, $4, 'VK PG 1', 'vk_tp_pg_', '0001', '\x01020304', 'CREATED_ACTIVE', 'PROVIDER', '["*"]', '["*"]', '[]', 10, 1, $5, $5)`,
		vk1, orgA.ID, proj1.ID, env1.ID, now,
	); err != nil {
		t.Fatalf("insert vk1: %v", err)
	}

	if err := port.SetOperatorRLSTx(ctx, tx, port.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ($1, NULL, 'HUMAN_LOCAL', 'null-op@pg.local', 'ACTIVE', 1, $2, $2)`,
		prnNullOp, now,
	); err != nil {
		t.Fatalf("insert null-org operator: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_pg_boot_op', NULL, $1, 'INSTANCE', 'instance', 'OPERATOR', 1, $3),
		       ('rb_pg_null_op', NULL, $2, 'INSTANCE', 'instance', 'OPERATOR', 1, $3)`,
		prnBootOp, prnNullOp, now,
	); err != nil {
		t.Fatalf("insert operator role_bindings: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms)
		VALUES ($1, '@tanpopo/openai-compatible', 'PROVIDER_ADAPTER', 'pub_tanpopo', 'official', $3),
		       ($2, '@tanpopo/ollama', 'PROVIDER_ADAPTER', 'pub_tanpopo', 'official', $3)`,
		pkg1, pkg2, now,
	); err != nil {
		t.Fatalf("insert extension_packages: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO extension_installations (id, package_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms)
		VALUES ($1, $2, 'ENABLED_ACTIVE', '{}', 1, $5, $5),
		       ($3, $4, 'ENABLED_ACTIVE', '{}', 1, $5, $5)`,
		ins1, pkg1, ins2, pkg2, now,
	); err != nil {
		t.Fatalf("insert extension_installations: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	reg, err := NewRegistry(ctx, RegistryConfig{DB: st.RawDB(), Audit: logger, NowMs: nowMs})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	return &ksPGHarness{
		st:        st,
		fk:        fk,
		logger:    logger,
		reg:       reg,
		orgAID:    orgA.ID,
		orgBID:    orgB.ID,
		projA1ID:  proj1.ID,
		envA1ID:   env1.ID,
		prnAID:    prnOrgOwnerA,
		prnBootOp: prnBootOp,
		prnNullOp: prnNullOp,
		vkA1ID:    vk1,
		inst1ID:   ins1,
		inst2ID:   ins2,
	}
}

func TestDEF081_PostgresForcedRLSGlobalQuarantineAndClearOperatorResolution(t *testing.T) {
	ctx := context.Background()
	h := newKillSwitchPGHarness(t)
	if h == nil {
		return
	}

	// Confirm the exact CP125 schema precondition on PostgreSQL: bootstrap operator principal
	// has non-null org_id, so under bare instance-operator RLS `principals` hides it.
	tx, err := h.st.RawDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := port.SetOperatorRLSTx(ctx, tx, port.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	var bareVisible int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM principals WHERE id = $1`, h.prnBootOp).Scan(&bareVisible); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if bareVisible != 0 {
		t.Fatalf("expected bare instance-operator RLS to hide org-bound operator %s (got %d)", h.prnBootOp, bareVisible)
	}

	// Register live SSE streams: two bound to inst1 (OpenAI-compatible) and inst2 (Ollama).
	stream1Ctx, stream1Cancel := context.WithCancel(context.Background())
	defer stream1Cancel()
	stream2Ctx, stream2Cancel := context.WithCancel(context.Background())
	defer stream2Cancel()
	h1 := h.reg.RegisterStream(StreamRegistration{OrgID: h.orgAID, ProjectID: h.projA1ID, VirtualKeyID: h.vkA1ID, PluginInstallID: h.inst1ID, Cancel: stream1Cancel})
	defer h.reg.UnregisterStream(h1)
	h2 := h.reg.RegisterStream(StreamRegistration{OrgID: h.orgAID, ProjectID: h.projA1ID, VirtualKeyID: h.vkA1ID, PluginInstallID: h.inst2ID, Cancel: stream2Cancel})
	defer h.reg.UnregisterStream(h2)

	abortTrue := true
	// Bootstrapped org-bound operator triggers global quarantine on inst1 and inst2 -> must succeed, sever stream, and persist switch.
	ks1, severed1, err := h.reg.TriggerWithSeveredCount(ctx, TriggerParams{
		Level:                  LevelExtensionInstallation,
		TargetID:               h.inst1ID,
		Reason:                 "emergency quarantine on openai-compatible",
		TriggeredByPrincipalID: h.prnBootOp,
		AbortActiveStreams:     &abortTrue,
	})
	if err != nil || severed1 != 1 || !ks1.Active {
		t.Fatalf("bootstrap operator Trigger inst1 = (%v, severed=%d, err=%v), want active & severed=1", ks1, severed1, err)
	}
	select {
	case <-stream1Ctx.Done():
	case <-time.After(time.Second):
		t.Fatalf("expected inst1 stream to be aborted")
	}

	ks2, severed2, err := h.reg.TriggerWithSeveredCount(ctx, TriggerParams{
		Level:                  LevelExtensionInstallation,
		TargetID:               h.inst2ID,
		Reason:                 "emergency quarantine on ollama",
		TriggeredByPrincipalID: h.prnBootOp,
		AbortActiveStreams:     &abortTrue,
	})
	if err != nil || severed2 != 1 || !ks2.Active {
		t.Fatalf("bootstrap operator Trigger inst2 = (%v, severed=%d, err=%v), want active & severed=1", ks2, severed2, err)
	}
	select {
	case <-stream2Ctx.Done():
	case <-time.After(time.Second):
		t.Fatalf("expected inst2 stream to be aborted")
	}

	if _, err := h.reg.EnsureActiveAbortBarrier(ctx, LevelExtensionInstallation, h.inst1ID, nil); err != nil {
		t.Fatalf("EnsureActiveAbortBarrier: %v", err)
	}
	if _, blocked := h.reg.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, PluginInstallID: h.inst1ID}); !blocked {
		t.Fatalf("expected CheckAdmission to block inst1")
	}

	// Bootstrapped org-bound operator can also trigger/clear an org-scoped switch in orgB (cross-org instance operator scope).
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgBID, Level: LevelProjectOrOrgFreeze, TargetID: h.orgBID,
		Reason: "instance operator cross-org freeze", TriggeredByPrincipalID: h.prnBootOp,
	}); err != nil {
		t.Fatalf("bootstrap operator cross-org trigger on orgB failed: %v", err)
	}

	// Restart hydration preserves both global switches, and bootstrap operator can clear inst1/inst2.
	restarted, err := NewRegistry(ctx, RegistryConfig{DB: h.st.RawDB(), Audit: h.logger, NowMs: func() int64 { return h.fk.Now().UTC().UnixMilli() }})
	if err != nil {
		t.Fatalf("restart NewRegistry: %v", err)
	}
	if _, blocked := restarted.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, PluginInstallID: h.inst1ID}); !blocked {
		t.Fatalf("expected restarted Registry to block inst1")
	}
	if _, err := restarted.Clear(ctx, ClearParams{
		KillSwitchID: ks1.ID, ExpectedVersion: ks1.Version,
		ClearedByPrincipalID: h.prnBootOp, ClearReason: "verified clear after restart",
	}); err != nil {
		t.Fatalf("bootstrap operator Clear inst1: %v", err)
	}
	if _, blocked := restarted.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, PluginInstallID: h.inst1ID}); blocked {
		t.Fatalf("expected inst1 admission unblocked after Clear")
	}
	// Pure org_id=NULL operator also continues working cleanly.
	if _, err := restarted.Clear(ctx, ClearParams{
		KillSwitchID: ks2.ID, ExpectedVersion: ks2.Version,
		ClearedByPrincipalID: h.prnNullOp, ClearReason: "null-org operator clear inst2",
	}); err != nil {
		t.Fatalf("null-org operator Clear inst2: %v", err)
	}
}

func TestDEF081_PostgresForcedRLSNegativeAuthorizationAndIsolation(t *testing.T) {
	ctx := context.Background()
	h := newKillSwitchPGHarness(t)
	if h == nil {
		return
	}
	now := h.fk.Now().UTC().UnixMilli()

	// Seed: orgB non-operator, forged org-scoped OPERATOR binding inside orgA, and SUSPENDED instance operator.
	prnOrgB := "prn_pg_org_b"
	prnForgedOp := "prn_pg_forged_op"
	prnSuspOp := "prn_pg_susp_op"
	tx, err := h.st.RawDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := port.SetOrgRLSTx(ctx, tx, port.DialectPostgres, h.orgBID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ($1, $2, 'HUMAN_LOCAL', 'b@pg.local', 'ACTIVE', 1, $3, $3)`,
		prnOrgB, h.orgBID, now,
	); err != nil {
		t.Fatal(err)
	}

	if err := port.SetOrgRLSTx(ctx, tx, port.DialectPostgres, h.orgAID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ($1, $2, 'HUMAN_LOCAL', 'forged@pg.local', 'ACTIVE', 1, $3, $3),
		       ($4, $2, 'HUMAN_LOCAL', 'susp-op@pg.local', 'SUSPENDED', 1, $3, $3)`,
		prnForgedOp, h.orgAID, now, prnSuspOp,
	); err != nil {
		t.Fatal(err)
	}
	// Org-level SECURITY_REVIEWER inside orgA cannot escalate to instance-global OPERATOR privileges.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_forged_1', $1, $2, 'ORG', $1, 'SECURITY_REVIEWER', 1, $3)`,
		h.orgAID, prnForgedOp, now,
	); err != nil {
		t.Fatal(err)
	}

	if err := port.SetOperatorRLSTx(ctx, tx, port.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_susp_op', NULL, $1, 'INSTANCE', 'instance', 'OPERATOR', 1, $2)`,
		prnSuspOp, now,
	); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 1. Non-operator ORG_OWNER in orgA denied global Trigger/Clear -> ErrPermissionDenied.
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		Level: LevelExtensionInstallation, TargetID: h.inst1ID,
		Reason: "org owner global quarantine attempt", TriggeredByPrincipalID: h.prnAID,
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected non-operator global Trigger = ErrPermissionDenied, got %v", err)
	}

	// 2. Forged org-bound OPERATOR binding MUST NOT escalate to global privilege -> ErrPermissionDenied.
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		Level: LevelExtensionInstallation, TargetID: h.inst1ID,
		Reason: "forged operator global quarantine attempt", TriggeredByPrincipalID: prnForgedOp,
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected forged org-bound OPERATOR global Trigger = ErrPermissionDenied, got %v", err)
	}

	// 3. Suspended org-bound INSTANCE OPERATOR denied fail-closed -> ErrInvalidInput.
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		Level: LevelExtensionInstallation, TargetID: h.inst1ID,
		Reason: "suspended operator global quarantine attempt", TriggeredByPrincipalID: prnSuspOp,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected suspended OPERATOR Trigger = ErrInvalidInput, got %v", err)
	}

	// 4. Non-existent / forged actor identifier denied fail-closed -> ErrPermissionDenied.
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		Level: LevelExtensionInstallation, TargetID: h.inst1ID,
		Reason: "forged actor id global quarantine attempt", TriggeredByPrincipalID: "prn_does_not_exist",
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected nonexistent actor global Trigger = ErrPermissionDenied, got %v", err)
	}

	// 5. Cross-tenant non-member probing Clear on orgA switch -> ErrNotFound (anti-oracle).
	ksOrgA, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelVirtualKey, TargetID: h.vkA1ID,
		Reason: "orgA legit switch", TriggeredByPrincipalID: h.prnAID,
	})
	if err != nil {
		t.Fatalf("Trigger orgA vk1: %v", err)
	}
	if _, err := h.reg.Clear(ctx, ClearParams{
		OrgID: &h.orgAID, KillSwitchID: ksOrgA.ID, ExpectedVersion: ksOrgA.Version,
		ClearedByPrincipalID: prnOrgB, ClearReason: "cross-tenant probe",
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected cross-tenant Clear probe = ErrNotFound, got %v", err)
	}
}

func TestDEF081_PostgresListWithFilterAndEmptyOrgEdgeCases(t *testing.T) {
	ctx := context.Background()
	h := newKillSwitchPGHarness(t)
	if h == nil {
		return
	}

	// Trigger orgA switch, orgB switch, and global switch separated in time for sort verification.
	ksA, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelVirtualKey, TargetID: h.vkA1ID,
		Reason: "orgA filter check", TriggeredByPrincipalID: h.prnAID,
	})
	if err != nil {
		t.Fatalf("Trigger orgA: %v", err)
	}
	h.fk.Advance(10 * time.Millisecond)
	ksB, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgBID, Level: LevelProjectOrOrgFreeze, TargetID: h.orgBID,
		Reason: "orgB filter check", TriggeredByPrincipalID: h.prnBootOp,
	})
	if err != nil {
		t.Fatalf("Trigger orgB: %v", err)
	}
	h.fk.Advance(10 * time.Millisecond)
	ksGlobal, err := h.reg.Trigger(ctx, TriggerParams{
		Level: LevelExtensionInstallation, TargetID: h.inst1ID,
		Reason: "global filter check", TriggeredByPrincipalID: h.prnBootOp,
	})
	if err != nil {
		t.Fatalf("Trigger global: %v", err)
	}

	// 1. AllowInstanceScope=true, OrgID="" -> returns all 3 switches ordered DESC by TriggeredAtMs, deduplicated.
	allSwitches, err := h.reg.ListWithFilter(ctx, ListFilter{ActiveOnly: true, AllowInstanceScope: true})
	if err != nil {
		t.Fatalf("ListWithFilter instance all: %v", err)
	}
	if len(allSwitches) != 3 {
		t.Fatalf("expected 3 switches across orgs + instance, got %d (%+v)", len(allSwitches), allSwitches)
	}
	if allSwitches[0].ID != ksGlobal.ID || allSwitches[1].ID != ksB.ID || allSwitches[2].ID != ksA.ID {
		t.Fatalf("unexpected sort order for multi-org ListWithFilter: got [%s, %s, %s], want [%s, %s, %s]",
			allSwitches[0].ID, allSwitches[1].ID, allSwitches[2].ID, ksGlobal.ID, ksB.ID, ksA.ID)
	}

	// 2. AllowInstanceScope=true, OrgID=orgA -> includes orgA + global ONLY (no orgB leakage!).
	orgAPlusGlobal, err := h.reg.ListWithFilter(ctx, ListFilter{OrgID: h.orgAID, ActiveOnly: true, AllowInstanceScope: true})
	if err != nil {
		t.Fatalf("ListWithFilter orgA+instance: %v", err)
	}
	if len(orgAPlusGlobal) != 2 {
		t.Fatalf("expected 2 switches (orgA + global, no orgB leak), got %d (%+v)", len(orgAPlusGlobal), orgAPlusGlobal)
	}
	for _, it := range orgAPlusGlobal {
		if it.ID == ksB.ID {
			t.Fatalf("tenant isolation failure: orgB switch %s leaked into orgA ListWithFilter", ksB.ID)
		}
	}

	// 3. AllowInstanceScope=false, OrgID=orgA -> excludes BOTH orgB AND global switches!
	orgAOnly, err := h.reg.ListWithFilter(ctx, ListFilter{OrgID: h.orgAID, ActiveOnly: true, AllowInstanceScope: false})
	if err != nil {
		t.Fatalf("ListWithFilter orgA only: %v", err)
	}
	if len(orgAOnly) != 1 || orgAOnly[0].ID != ksA.ID {
		t.Fatalf("expected strictly [%s] for orgA-only filter, got %+v", ksA.ID, orgAOnly)
	}

	// 4. Fresh PG schema with ZERO organizations -> RefreshFromDB & ListWithFilter both succeed cleanly!
	baseDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	zeroSchema := fmt.Sprintf("tp_ks_zero_%d_%d", time.Now().UnixNano(), ksPGSchemaSeq.Add(1))
	baseDB, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baseDB.ExecContext(ctx, `CREATE SCHEMA `+zeroSchema); err != nil {
		_ = baseDB.Close()
		t.Fatal(err)
	}
	_ = baseDB.Close()
	defer func() {
		if cleanDB, cerr := sql.Open("pgx", baseDSN); cerr == nil {
			_, _ = cleanDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+zeroSchema+` CASCADE`)
			_ = cleanDB.Close()
		}
	}()
	u, _ := url.Parse(baseDSN)
	q := u.Query()
	q.Set("search_path", zeroSchema)
	u.RawQuery = q.Encode()
	zeroSt, err := postgres.Open(ctx, postgres.Options{DSN: u.String(), NowMs: func() int64 { return h.fk.Now().UTC().UnixMilli() }})
	if err != nil {
		t.Fatalf("open zero-org schema: %v", err)
	}
	defer func() { _ = zeroSt.Close() }()
	zeroAud, err := audit.NewLogger(audit.LoggerConfig{DB: zeroSt.RawDB()})
	if err != nil {
		t.Fatal(err)
	}
	zeroReg, err := NewRegistry(ctx, RegistryConfig{DB: zeroSt.RawDB(), Audit: zeroAud, NowMs: func() int64 { return h.fk.Now().UTC().UnixMilli() }})
	if err != nil {
		t.Fatalf("NewRegistry with 0 orgs: %v", err)
	}
	if err := zeroReg.RefreshFromDB(ctx); err != nil {
		t.Fatalf("RefreshFromDB with 0 orgs: %v", err)
	}
	zeroPage, err := zeroReg.ListWithFilter(ctx, ListFilter{ActiveOnly: false, AllowInstanceScope: true})
	if err != nil || len(zeroPage) != 0 {
		t.Fatalf("ListWithFilter with 0 orgs expected empty without error, got (%d, %v)", len(zeroPage), err)
	}
}
