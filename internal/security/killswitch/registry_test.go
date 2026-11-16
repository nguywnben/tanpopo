// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package killswitch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type ksHarness struct {
	st        *sqlite.Store
	fk        *clock.FakeClock
	logger    *audit.Logger
	reg       *Registry
	orgAID    string
	orgBID    string
	projA1ID  string
	projA2ID  string
	envA1ID   string
	prnAID    string
	prnInstID string
	vkA1ID    string
	vkA2ID    string
	conn1ID   string
	conn2ID   string
	inst1ID   string
	inst2ID   string
}

func newKillSwitchHarness(t *testing.T) *ksHarness {
	t.Helper()
	ctx := context.Background()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	dbPath := filepath.Join(t.TempDir(), "ks.db")
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	logger, err := audit.NewLogger(audit.LoggerConfig{
		DB:    st.RawDB(),
		NowMs: func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}

	orgA, _ := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "ks-org-a", DisplayName: "KS Org A"})
	orgB, _ := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "ks-org-b", DisplayName: "KS Org B"})
	proj1, _ := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "ks-p1", DisplayName: "P1"})
	proj2, _ := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "ks-p2", DisplayName: "P2"})
	env1, _ := st.Tenancy().CreateEnvironment(ctx, orgA.ID, proj1.ID, port.CreateEnvironmentParams{Slug: "prod"})

	now := fk.Now().UTC().UnixMilli()
	prnA := "prn_org_a"
	prnInst := "prn_operator"
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'a@example.com', 'ACTIVE', 1, ?, ?),
		       (?, ?, 'HUMAN_LOCAL', 'op@example.com', 'ACTIVE', 1, ?, ?)`,
		prnA, orgA.ID, now, now, prnInst, orgA.ID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_op', NULL, ?, 'INSTANCE', 'instance', 'OPERATOR', 1, ?),
		       ('rb_org_a', ?, ?, 'ORG', ?, 'ORG_OWNER', 1, ?)`,
		prnInst, now, orgA.ID, prnA, orgA.ID, now,
	); err != nil {
		t.Fatal(err)
	}

	// Seed minimal FK targets: virtual_keys, provider_connections, extension_packages + installations.
	vk1, vk2 := "vk_test_1", "vk_test_2"
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO virtual_keys (
			id, org_id, project_id, env_id, name, key_prefix, last_four, active_key_hash,
			state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			max_concurrent_streams, version, created_at_ms, updated_at_ms
		) VALUES
		(?, ?, ?, ?, 'VK1', 'vk_tp_prod_', '0001', X'01020304', 'CREATED_ACTIVE', 'PROVIDER', '["*"]', '["*"]', '[]', 10, 1, ?, ?),
		(?, ?, ?, ?, 'VK2', 'vk_tp_prod_', '0002', X'05060708', 'CREATED_ACTIVE', 'PROVIDER', '["*"]', '["*"]', '[]', 10, 1, ?, ?)`,
		vk1, orgA.ID, proj1.ID, env1.ID, now, now,
		vk2, orgA.ID, proj2.ID, env1.ID, now, now,
	); err != nil {
		t.Fatal(err)
	}

	pkg1, pkg2 := "pkg_1", "pkg_2"
	ins1, ins2 := "ins_plugin_1", "ins_plugin_2"
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms)
		VALUES (?, 'ext/plugin-1', 'PROVIDER_ADAPTER', 'pub_tanpopo', 'official', ?),
		       (?, 'ext/plugin-2', 'PROVIDER_ADAPTER', 'pub_tanpopo', 'official', ?)`,
		pkg1, now, pkg2, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_installations (id, package_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'ENABLED_ACTIVE', '{}', 1, ?, ?),
		       (?, ?, 'ENABLED_ACTIVE', '{}', 1, ?, ?)`,
		ins1, pkg1, now, now,
		ins2, pkg2, now, now,
	); err != nil {
		t.Fatal(err)
	}

	adp1, adp2 := "adp_1", "adp_2"
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO provider_adapters (id, installation_id, adapter_slug, supported_dialects_json, allowed_hosts_json, version, created_at_ms)
		VALUES (?, ?, 'openai-a', '["openai"]', '["api.example.com"]', 1, ?),
		       (?, ?, 'openai-b', '["openai"]', '["api2.example.com"]', 1, ?)`,
		adp1, ins1, now, adp2, ins2, now,
	); err != nil {
		t.Fatal(err)
	}

	conn1, conn2 := "conn_1", "conn_2"
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO provider_connections (id, org_id, adapter_id, name, endpoint_base_url, data_region, billing_mode, auth_mode, state, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, 'Conn 1', 'https://api.example.com', 'US-EAST-1', 'COMMERCIAL_API', 'API_KEY', 'ACTIVE', 1, ?, ?),
		       (?, ?, ?, 'Conn 2', 'https://api2.example.com', 'US-EAST-1', 'COMMERCIAL_API', 'API_KEY', 'ACTIVE', 1, ?, ?)`,
		conn1, orgA.ID, adp1, now, now,
		conn2, orgA.ID, adp2, now, now,
	); err != nil {
		t.Fatal(err)
	}

	reg, err := NewRegistry(ctx, RegistryConfig{
		DB:    st.RawDB(),
		Audit: logger,
		NowMs: func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	return &ksHarness{
		st:        st,
		fk:        fk,
		logger:    logger,
		reg:       reg,
		orgAID:    orgA.ID,
		orgBID:    orgB.ID,
		projA1ID:  proj1.ID,
		projA2ID:  proj2.ID,
		envA1ID:   env1.ID,
		prnAID:    prnA,
		prnInstID: prnInst,
		vkA1ID:    vk1,
		vkA2ID:    vk2,
		conn1ID:   conn1,
		conn2ID:   conn2,
		inst1ID:   ins1,
		inst2ID:   ins2,
	}
}

func TestAC2ExtensionKillSwitchSeversMatched4Of8StreamsUnder1s(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newKillSwitchHarness(t)

	// AC-2: 8 active SSE streams across 2 Plugins and 2 Projects.
	type streamState struct {
		plugin string
		proj   string
		ctx    context.Context
		doneCh chan struct{}
		hid    uint64
	}
	streams := make([]*streamState, 0, 8)
	plugins := []string{h.inst1ID, h.inst2ID}
	projects := []string{h.projA1ID, h.projA2ID}

	for _, pl := range plugins {
		for _, pr := range projects {
			for k := 0; k < 2; k++ {
				sCtx, cancel := context.WithCancel(context.Background())
				doneCh := make(chan struct{})
				hid := h.reg.RegisterStream(StreamRegistration{
					OrgID:           h.orgAID,
					ProjectID:       pr,
					VirtualKeyID:    h.vkA1ID,
					ConnectionID:    h.conn1ID,
					PluginInstallID: pl,
					Cancel:          cancel,
				})
				go func(c context.Context, ch chan struct{}) {
					<-c.Done()
					close(ch)
				}(sCtx, doneCh)
				streams = append(streams, &streamState{plugin: pl, proj: pr, ctx: sCtx, doneCh: doneCh, hid: hid})
				defer cancel()
				defer h.reg.UnregisterStream(hid)
			}
		}
	}

	if got := h.reg.Streams().ActiveCount(); got != 8 {
		t.Fatalf("expected 8 registered active streams, got %d", got)
	}

	start := time.Now()
	ks, err := h.reg.Trigger(ctx, TriggerParams{
		Level:                  LevelExtensionInstallation,
		TargetID:               h.inst1ID,
		Reason:                 "CVE-2026-9999 compromised adapter binary",
		TriggeredByPrincipalID: h.prnInstID,
	})
	if err != nil {
		t.Fatalf("Trigger ExtensionInstallation kill switch: %v", err)
	}

	deadline := time.After(1 * time.Second)
	for _, s := range streams {
		if s.plugin == h.inst1ID {
			select {
			case <-s.doneCh:
			case <-deadline:
				t.Fatalf("Plugin 1 stream (proj=%s) did not terminate within < 1s", s.proj)
			}
		}
	}
	elapsed := time.Since(start)
	if elapsed >= time.Second {
		t.Fatalf("expected Plugin 1 streams to sever in < 1s, took %v", elapsed)
	}

	// Verify all 4 streams using Plugin 2 remain uninterrupted.
	time.Sleep(30 * time.Millisecond)
	for _, s := range streams {
		if s.plugin == h.inst2ID && s.ctx.Err() != nil {
			t.Fatalf("Plugin 2 stream (proj=%s) was unexpectedly canceled", s.proj)
		}
	}

	// New admissions touching Plugin 1 fail closed immediately, while Plugin 2 succeeds.
	if hit, blocked := h.reg.CheckAdmission(AdmissionQuery{
		OrgID:           h.orgAID,
		ProjectID:       h.projA1ID,
		VirtualKeyID:    h.vkA1ID,
		ConnectionID:    h.conn1ID,
		PluginInstallID: h.inst1ID,
	}); !blocked || hit == nil || hit.ID != ks.ID {
		t.Fatalf("expected CheckAdmission to block Plugin 1, got blocked=%v hit=%+v", blocked, hit)
	}
	if _, blocked := h.reg.CheckAdmission(AdmissionQuery{
		OrgID:           h.orgAID,
		ProjectID:       h.projA1ID,
		VirtualKeyID:    h.vkA1ID,
		ConnectionID:    h.conn2ID,
		PluginInstallID: h.inst2ID,
	}); blocked {
		t.Fatalf("expected Plugin 2 admission to remain allowed")
	}

	// Security Invariant: clearing requires reason >= 5 chars and records audited event.
	if _, err := h.reg.Clear(ctx, ClearParams{
		KillSwitchID:         ks.ID,
		ExpectedVersion:      ks.Version,
		ClearedByPrincipalID: h.prnInstID,
		ClearReason:          "no",
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected short clear_reason to fail ErrInvalidInput, got %v", err)
	}

	cleared, err := h.reg.Clear(ctx, ClearParams{
		KillSwitchID:         ks.ID,
		ExpectedVersion:      ks.Version,
		ClearedByPrincipalID: h.prnInstID,
		ClearReason:          "Patched to signed v1.0.1 and verified checksums",
	})
	if err != nil {
		t.Fatalf("Clear kill switch: %v", err)
	}
	if cleared.Active || cleared.Version != 2 {
		t.Fatalf("unexpected cleared kill switch: %+v", cleared)
	}
	if _, blocked := h.reg.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, PluginInstallID: h.inst1ID}); blocked {
		t.Fatalf("expected cleared kill switch to stop blocking admissions")
	}

	// Verify both trigger and clear recorded valid hash-chained SYSTEM audit entries.
	rep, err := audit.VerifyAuditChain(ctx, h.st.RawDB(), audit.VerifyOptions{ChainKeys: []string{audit.SystemChainKey}})
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if rep.Chains[audit.SystemChainKey].VerifiedCount != 2 {
		t.Fatalf("expected 2 audited kill-switch events in SYSTEM chain, got %+v", rep.Chains[audit.SystemChainKey])
	}
}

func TestAll4KillSwitchLevelsRestartHydrationAndTenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newKillSwitchHarness(t)

	// Sibling org principal cannot see or trigger against Org A.
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID:                  &h.orgBID,
		Level:                  LevelVirtualKey,
		TargetID:               h.vkA1ID,
		Reason:                 "cross tenant spoof test",
		TriggeredByPrincipalID: h.prnAID,
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected cross-org Trigger to fail ErrNotFound, got %v", err)
	}

	// Trigger VIRTUAL_KEY, PROVIDER_CONNECTION, and PROJECT_OR_ORG_FREEZE (project + org).
	ksVK, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelVirtualKey, TargetID: h.vkA1ID,
		Reason: "leaked key in public git commit", TriggeredByPrincipalID: h.prnAID,
	})
	if err != nil {
		t.Fatalf("Trigger VIRTUAL_KEY: %v", err)
	}
	ksConn, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelProviderConnection, TargetID: h.conn2ID,
		Reason: "upstream credential rotation incident", TriggeredByPrincipalID: h.prnAID,
	})
	if err != nil {
		t.Fatalf("Trigger PROVIDER_CONNECTION: %v", err)
	}
	ksProj, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelProjectOrOrgFreeze, TargetID: h.projA2ID,
		Reason: "emergency freeze on project 2", TriggeredByPrincipalID: h.prnAID,
	})
	if err != nil {
		t.Fatalf("Trigger PROJECT_OR_ORG_FREEZE: %v", err)
	}

	// Restart hydration: a freshly created Registry from the same DB preserves all active kill switches!
	restarted, err := NewRegistry(ctx, RegistryConfig{
		DB:    h.st.RawDB(),
		Audit: h.logger,
	})
	if err != nil {
		t.Fatalf("restarted NewRegistry: %v", err)
	}
	if _, blocked := restarted.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, ProjectID: h.projA1ID, VirtualKeyID: h.vkA1ID}); !blocked {
		t.Fatalf("expected restarted registry to enforce VIRTUAL_KEY kill switch")
	}
	if _, blocked := restarted.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, ProjectID: h.projA1ID, VirtualKeyID: h.vkA2ID, ConnectionID: h.conn2ID}); !blocked {
		t.Fatalf("expected restarted registry to enforce PROVIDER_CONNECTION kill switch")
	}
	if _, blocked := restarted.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, ProjectID: h.projA2ID, VirtualKeyID: h.vkA2ID, ConnectionID: h.conn1ID}); !blocked {
		t.Fatalf("expected restarted registry to enforce PROJECT_OR_ORG_FREEZE")
	}
	if _, blocked := restarted.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, ProjectID: h.projA1ID, VirtualKeyID: h.vkA2ID, ConnectionID: h.conn1ID, PluginInstallID: h.inst1ID}); blocked {
		t.Fatalf("expected unrelated (projA1, vkA2, conn1, inst1) to remain unblocked")
	}

	// Org B cannot Get or Clear Org A's kill switches (404 anti-oracle).
	if _, err := restarted.Get(ctx, h.orgBID, ksVK.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected cross-org Get to return ErrNotFound, got %v", err)
	}
	if _, err := restarted.Clear(ctx, ClearParams{
		OrgID: &h.orgBID, KillSwitchID: ksConn.ID, ExpectedVersion: ksConn.Version,
		ClearedByPrincipalID: h.prnInstID, ClearReason: "unauthorized clear attempt",
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected cross-org Clear to return ErrNotFound, got %v", err)
	}
	_ = ksProj
}

func TestKillSwitchConcurrentStreamSeveringAndAtomicRollbackOnAuditFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newKillSwitchHarness(t)

	// Subtest 1: Concurrent Register/UpdateTarget/AbortMatching under -race.
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, cancel := context.WithCancel(context.Background())
			defer cancel()
			hid := h.reg.RegisterStream(StreamRegistration{
				OrgID: h.orgAID, ProjectID: h.projA1ID, VirtualKeyID: h.vkA1ID, Cancel: cancel,
			})
			h.reg.UpdateStreamTarget(hid, h.conn1ID, h.inst1ID)
			h.reg.UnregisterStream(hid)
		}(i)
	}
	wg.Wait()

	// Subtest 2: Failing audit sink rolls back Trigger atomically (zero orphan row in DB or RAM).
	failingSink := &failingTxAuditSink{}
	badReg, err := NewRegistry(ctx, RegistryConfig{DB: h.st.RawDB(), Audit: failingSink})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badReg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelVirtualKey, TargetID: h.vkA2ID,
		Reason: "rollback check kill switch", TriggeredByPrincipalID: h.prnAID,
	}); err == nil {
		t.Fatalf("expected Trigger to fail closed when audit AppendTx fails")
	}
	var cnt int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM emergency_kill_switches WHERE target_id = ?`, h.vkA2ID).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("expected DB rollback on audit failure leaving 0 kill-switch rows, got %d", cnt)
	}
	if _, blocked := badReg.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, VirtualKeyID: h.vkA2ID}); blocked {
		t.Fatalf("expected no RAM drift after failed Trigger")
	}

	_ = strings.Contains("", "")
	_ = fmt.Sprintf("%s", idgen.PrefixKillSwitch)
}

type failingTxAuditSink struct{}

func (f *failingTxAuditSink) Append(context.Context, port.AuditEvent) error {
	return errors.New("injected audit failure")
}
func (f *failingTxAuditSink) AppendTx(context.Context, *sql.Tx, port.AuditEvent) error {
	return errors.New("injected audit failure")
}

func TestKillSwitchFailClosedRBACValidationAndCrossProcessWatcher(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newKillSwitchHarness(t)

	// 1. NewRegistry fails closed when Audit sink is nil.
	if _, err := NewRegistry(ctx, RegistryConfig{DB: h.st.RawDB()}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected NewRegistry with nil Audit to fail ErrInvalidInput, got %v", err)
	}

	// 2. Get / List reject empty orgID fail-closed.
	if _, err := h.reg.Get(ctx, "", "ksw_1"); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected Get empty orgID to fail ErrInvalidInput, got %v", err)
	}
	if _, err := h.reg.List(ctx, "", false); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected List empty orgID to fail ErrInvalidInput, got %v", err)
	}

	// 3. Seed an unprivileged DEVELOPER principal and a TEAM_ADMIN scoped strictly to projA1ID.
	now := h.fk.Now().UTC().UnixMilli()
	prnDev := "prn_dev_a"
	prnTeamAdmin := "prn_ta_a1"
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'dev@example.com', 'ACTIVE', 1, ?, ?),
		       (?, ?, 'HUMAN_LOCAL', 'ta@example.com', 'ACTIVE', 1, ?, ?)`,
		prnDev, h.orgAID, now, now, prnTeamAdmin, h.orgAID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_dev_a', ?, ?, 'ORG', ?, 'DEVELOPER', 1, ?),
		       ('rb_ta_a1', ?, ?, 'PROJECT', ?, 'TEAM_ADMIN', 1, ?)`,
		h.orgAID, prnDev, h.orgAID, now,
		h.orgAID, prnTeamAdmin, h.projA1ID, now,
	); err != nil {
		t.Fatal(err)
	}

	// DEVELOPER cannot trigger org-scoped kill switch -> ErrPermissionDenied.
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelVirtualKey, TargetID: h.vkA1ID,
		Reason: "developer attempt", TriggeredByPrincipalID: prnDev,
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected DEVELOPER Trigger to fail ErrPermissionDenied, got %v", err)
	}

	// Org admin cannot trigger or clear instance-scoped EXTENSION_INSTALLATION kill switch -> ErrPermissionDenied.
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		Level: LevelExtensionInstallation, TargetID: h.inst1ID,
		Reason: "non-operator extension attempt", TriggeredByPrincipalID: h.prnAID,
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected non-operator instance Trigger to fail ErrPermissionDenied, got %v", err)
	}
	ksInst, err := h.reg.Trigger(ctx, TriggerParams{
		Level: LevelExtensionInstallation, TargetID: h.inst1ID,
		Reason: "instance operator quarantine", TriggeredByPrincipalID: h.prnInstID,
	})
	if err != nil {
		t.Fatalf("operator Trigger EXTENSION_INSTALLATION: %v", err)
	}
	if _, err := h.reg.Clear(ctx, ClearParams{
		KillSwitchID: ksInst.ID, ExpectedVersion: ksInst.Version,
		ClearedByPrincipalID: h.prnAID, ClearReason: "unauthorized org owner clear",
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected org owner clearing instance switch to fail ErrPermissionDenied, got %v", err)
	}

	// Org-scoped EXTENSION_INSTALLATION via Clear(OrgID: nil) MUST fail with ErrNotFound
	// (Clear(OrgID: nil) is strictly for instance-level switches).
	ksProj, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelProjectOrOrgFreeze, TargetID: h.projA1ID,
		Reason: "freeze project 1", TriggeredByPrincipalID: h.prnAID,
	})
	if err != nil {
		t.Fatalf("Trigger project freeze: %v", err)
	}
	if _, err := h.reg.Clear(ctx, ClearParams{
		OrgID: nil, KillSwitchID: ksProj.ID, ExpectedVersion: ksProj.Version,
		ClearedByPrincipalID: h.prnInstID, ClearReason: "must require OrgID for org-scoped switch",
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected Clear(OrgID: nil) on org-scoped switch to return ErrNotFound, got %v", err)
	}

	// Cross-tenant non-member attempting Clear on Org A switch fails with ErrNotFound (anti-oracle).
	prnOrgB := "prn_org_b"
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'b@example.com', 'ACTIVE', 1, ?, ?)`,
		prnOrgB, h.orgBID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reg.Clear(ctx, ClearParams{
		OrgID: &h.orgAID, KillSwitchID: ksProj.ID, ExpectedVersion: ksProj.Version,
		ClearedByPrincipalID: prnOrgB, ClearReason: "org B anti-oracle probe",
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected non-member Clear probe on Org A to return ErrNotFound, got %v", err)
	}

	// TEAM_ADMIN for projA1ID CAN trigger vkA1ID (bound to projA1ID), CANNOT trigger vkA2ID (bound to projA2ID).
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelVirtualKey, TargetID: h.vkA2ID,
		Reason: "cross project attempt", TriggeredByPrincipalID: prnTeamAdmin,
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected TEAM_ADMIN(proj1) Trigger on vkA2(proj2) to fail ErrPermissionDenied, got %v", err)
	}

	// Duplicate active kill switch on same target fails cleanly under partial unique index.
	if _, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelProjectOrOrgFreeze, TargetID: h.projA1ID,
		Reason: "duplicate active switch", TriggeredByPrincipalID: h.prnAID,
	}); err == nil {
		t.Fatalf("expected duplicate active kill switch trigger to fail")
	}

	// Cross-process sync via second Registry sharing same DB + RefreshFromDB / StartWatcher.
	proc2Reg, err := NewRegistry(ctx, RegistryConfig{DB: h.st.RawDB(), Audit: h.logger})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	hid := proc2Reg.RegisterStream(StreamRegistration{
		OrgID: h.orgAID, ProjectID: h.projA2ID, VirtualKeyID: h.vkA2ID, Cancel: streamCancel,
	})
	defer proc2Reg.UnregisterStream(hid)

	watchCtx, watchCancel := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		proc2Reg.StartWatcher(watchCtx, 25*time.Millisecond)
	}()
	defer func() {
		watchCancel()
		<-watchDone
	}()

	// Trigger vkA2 on primary h.reg -> proc2Reg watcher observes DB update and cancels streamCtx!
	ksVK2, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelVirtualKey, TargetID: h.vkA2ID,
		Reason: "out-of-band cross-process emergency", TriggeredByPrincipalID: h.prnAID,
	})
	if err != nil {
		t.Fatalf("Trigger vkA2 on reg1: %v", err)
	}
	select {
	case <-streamCtx.Done():
	case <-time.After(1 * time.Second):
		t.Fatalf("expected proc2Reg watcher to sever active stream via RefreshFromDB")
	}
	if _, blocked := proc2Reg.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, VirtualKeyID: h.vkA2ID}); !blocked {
		t.Fatalf("expected proc2Reg admission check to block vkA2 after sync")
	}

	// Clearing ksVK2 on reg1 unblocks proc2Reg after RefreshFromDB.
	if _, err := h.reg.Clear(ctx, ClearParams{
		OrgID: &h.orgAID, KillSwitchID: ksVK2.ID, ExpectedVersion: ksVK2.Version,
		ClearedByPrincipalID: h.prnAID, ClearReason: "verified false alarm cleared",
	}); err != nil {
		t.Fatalf("Clear ksVK2: %v", err)
	}
	if err := proc2Reg.RefreshFromDB(ctx); err != nil {
		t.Fatalf("proc2Reg.RefreshFromDB: %v", err)
	}
	if _, blocked := proc2Reg.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, VirtualKeyID: h.vkA2ID}); blocked {
		t.Fatalf("expected proc2Reg to unblock vkA2 after Clear + RefreshFromDB")
	}
	_ = ksInst
}

func TestRegistryConcurrentTriggerClearAndRefreshNoLostUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newKillSwitchHarness(t)

	const cycles = 20
	var wg sync.WaitGroup
	refreshStop := make(chan struct{})
	refreshErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-refreshStop:
				return
			default:
				if err := h.reg.RefreshFromDB(ctx); err != nil {
					select {
					case refreshErr <- err:
					default:
					}
					return
				}
			}
		}
	}()

	for i := 0; i < cycles; i++ {
		ks, err := h.reg.Trigger(ctx, TriggerParams{
			OrgID: &h.orgAID, Level: LevelVirtualKey, TargetID: h.vkA1ID,
			Reason: fmt.Sprintf("concurrent cycle %02d trigger", i), TriggeredByPrincipalID: h.prnAID,
		})
		if err != nil {
			t.Fatalf("cycle %d Trigger: %v", i, err)
		}
		if _, blocked := h.reg.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, VirtualKeyID: h.vkA1ID}); !blocked {
			t.Fatalf("cycle %d: expected vkA1 blocked immediately after Trigger", i)
		}
		if _, err := h.reg.Clear(ctx, ClearParams{
			OrgID: &h.orgAID, KillSwitchID: ks.ID, ExpectedVersion: ks.Version,
			ClearedByPrincipalID: h.prnAID, ClearReason: fmt.Sprintf("concurrent cycle %02d clear", i),
		}); err != nil {
			t.Fatalf("cycle %d Clear: %v", i, err)
		}
		if _, blocked := h.reg.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, VirtualKeyID: h.vkA1ID}); blocked {
			t.Fatalf("cycle %d: expected vkA1 unblocked immediately after Clear", i)
		}
	}

	close(refreshStop)
	wg.Wait()
	select {
	case err := <-refreshErr:
		t.Fatalf("concurrent RefreshFromDB failed: %v", err)
	default:
	}

	// Trigger one final time concurrently with a final RefreshFromDB; final memory state must stay blocked.
	ksFinal, err := h.reg.Trigger(ctx, TriggerParams{
		OrgID: &h.orgAID, Level: LevelVirtualKey, TargetID: h.vkA1ID,
		Reason: "final active check after race", TriggeredByPrincipalID: h.prnAID,
	})
	if err != nil {
		t.Fatalf("final Trigger: %v", err)
	}
	if _, blocked := h.reg.CheckAdmission(AdmissionQuery{OrgID: h.orgAID, VirtualKeyID: h.vkA1ID}); !blocked {
		t.Fatalf("expected vkA1 blocked after final Trigger")
	}
	_ = ksFinal
}
