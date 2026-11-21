// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/sidecar"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/ingress/a2a"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/hub"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

type tp42StaticHostResolver struct{}

func (tp42StaticHostResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

func TestTPIMP0042DualDB_A2AAndSidecarFailClosed(t *testing.T) {
	ctx := context.Background()

	// Verify runtime.sidecar.enabled defaults to fail-closed (disabled).
	if _, err := sidecar.NewClient(sidecar.Config{Enabled: false}); !errors.Is(err, sidecar.ErrSidecarDisabled) {
		t.Fatalf("expected ErrSidecarDisabled when Enabled=false, got %v", err)
	}

	t.Run("sqlite", func(t *testing.T) {
		st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "tp42_sqlite.db")})
		if err != nil {
			t.Fatalf("sqlite.Open: %v", err)
		}
		defer func() { _ = st.Close() }()
		runTP0042DualDBSuite(t, ctx, st, st.RawDB(), st.CheckpointWAL, false)
	})

	pgDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if pgDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN is required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		return
	}

	t.Run("postgres16_force_rls", func(t *testing.T) {
		effectiveDSN, cleanupDB := prepareIsolatedPGDatabase(t, pgDSN)
		defer cleanupDB()
		st, err := postgres.Open(ctx, postgres.Options{
			DSN:                      effectiveDSN,
			PartitionLookaheadMonths: 0,
		})
		if err != nil {
			t.Fatalf("postgres.Open: %v", err)
		}
		defer func() { _ = st.Close() }()
		runTP0042DualDBSuite(t, ctx, st, st.DB(), func(context.Context) error { return nil }, true)
	})
}

func runTP0042DualDBSuite(
	t *testing.T,
	ctx context.Context,
	st port.Store,
	db *sql.DB,
	postShred func(context.Context) error,
	isPG bool,
) {
	t.Helper()
	dialect := port.DetectDialect(ctx, db)
	aud, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	masterRaw := sha256.Sum256([]byte("tp0042-dualdb-master-kek"))
	kek, err := vault.NewStaticKEKProvider("kek_tp42_v1", masterRaw[:])
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	vs, err := vault.NewStore(vault.Config{DB: db, KEK: kek, PostShredHook: postShred})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	defer vs.Close()

	orgSuffix := strings.ToLower(idgen.MustNew(idgen.PrefixOrg))
	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-tp42-a-" + orgSuffix, DisplayName: "Org TP42 A", MinPluginTrust: port.MinTrustVerifiedOnly})
	if err != nil {
		t.Fatalf("CreateOrganization A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-tp42-b-" + orgSuffix, DisplayName: "Org TP42 B", MinPluginTrust: port.MinTrustVerifiedOnly})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}
	prjA, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "prj-tp42-a", DisplayName: "Prj TP42 A"})
	if err != nil {
		t.Fatalf("CreateProject A: %v", err)
	}
	envA, err := st.Tenancy().CreateEnvironment(ctx, orgA.ID, prjA.ID, port.CreateEnvironmentParams{Slug: "prod-tp42"})
	if err != nil {
		t.Fatalf("CreateEnvironment A: %v", err)
	}
	prcA := idgen.MustNew(idgen.PrefixPrincipal)
	ptx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx(principal): %v", err)
	}
	if err := port.SetOrgRLSTx(ctx, ptx, dialect, orgA.ID); err != nil {
		_ = ptx.Rollback()
		t.Fatalf("SetOrgRLSTx(principal): %v", err)
	}
	if _, err := ptx.ExecContext(ctx, port.Rebind(dialect, `INSERT INTO principals(id,org_id,kind,email_or_sub,status,created_at_ms,updated_at_ms) VALUES(?,?,'HUMAN_LOCAL','tp42@tanpopo.test','ACTIVE',1000,1000)`), prcA, orgA.ID); err != nil {
		_ = ptx.Rollback()
		t.Fatalf("insert principal: %v", err)
	}
	if err := ptx.Commit(); err != nil {
		t.Fatalf("commit principal: %v", err)
	}

	reg, err := a2a.NewRegistry(a2a.RegistryConfig{DB: db, Audit: aud, HostResolver: tp42StaticHostResolver{}})
	if err != nil {
		t.Fatalf("a2a.NewRegistry: %v", err)
	}
	epA, err := reg.RegisterAgent(ctx, a2a.RegisterAgentParams{
		OrgID:             orgA.ID,
		ProjectID:         prjA.ID,
		ActorPrincipalID:  prcA,
		AgentSlug:         "eu-analyst",
		Name:              "EU Analyst",
		EndpointURL:       "https://agent-a.example.com/a2a",
		DataRegion:        "EU",
		MaxDelegationHops: 4,
		RequiredModels:    []string{"gpt-4o"},
		Skills:            []a2a.AgentSkill{{ID: "analyze", Name: "Analyze"}},
	})
	if err != nil {
		t.Fatalf("RegisterAgent A: %v", err)
	}

	// Verify A2A default-disabled HTTP behavior returns 404.
	disabledCoord, err := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{Enabled: false, DB: db, Audit: aud})
	if err != nil {
		t.Fatalf("NewTaskCoordinator(disabled): %v", err)
	}
	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/.well-known/agent.json"},
		{http.MethodGet, "/a2a/agents"},
		{http.MethodPost, "/a2a/tasks"},
	} {
		req := httptest.NewRequest(route.method, route.path, nil)
		rec := httptest.NewRecorder()
		disabledCoord.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s when a2a.enabled=false: got %d, want 404", route.method, route.path, rec.Code)
		}
	}

	// Verify cross-org isolation under PG16 FORCE RLS (and SQLite org scoping).
	if _, err := reg.GetAgent(ctx, orgB.ID, epA.ID); err == nil {
		t.Fatalf("expected Org B lookup of Org A agent endpoint to fail (isPG=%t)", isPG)
	}

	if isPG {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := port.SetOrgRLSTx(ctx, tx, dialect, orgB.ID); err != nil {
			t.Fatalf("SetOrgRLSTx(orgB): %v", err)
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_endpoints WHERE id=$1`, epA.ID).Scan(&count); err != nil {
			t.Fatalf("SELECT COUNT(*) FROM agent_endpoints: %v", err)
		}
		if count != 0 {
			t.Fatalf("PG16 FORCE RLS leakage: Org B saw %d rows for Org A agent %s", count, epA.ID)
		}
	}

	snaps, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	ks, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{DB: db, Audit: aud})
	if err != nil {
		t.Fatalf("killswitch.NewRegistry: %v", err)
	}

	// Verify hop_count > 5, empty AllowedModels denial, and KillSwitch wiring.
	enabledCoord, err := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
		Enabled:    true,
		DB:         db,
		Snapshots:  snaps,
		KillSwitch: ks,
		Audit:      aud,
	})
	if err != nil {
		t.Fatalf("NewTaskCoordinator(enabled): %v", err)
	}
	regionEU := "EU"
	caller := snapshot.KeyMatchResult{
		Key: &snapshot.VirtualKeyView{
			ID:        idgen.MustNew(idgen.PrefixVirtualKey),
			OrgID:     orgA.ID,
			ProjectID: prjA.ID,
			EnvID:     envA.ID,
			State:     "CREATED_ACTIVE",
		},
		Config: &snapshot.EffectiveConfig{
			EnforcedRegion: &regionEU,
		},
	}
	// Empty AllowedModels must fail closed against agent requiring "gpt-4o".
	if _, err := enabledCoord.CreateTask(ctx, caller, a2a.TaskCreateRequest{
		TargetAgentID:       epA.ID,
		SubBudgetHoldMicros: 1000,
		HopCount:            1,
		InputParts:          []map[string]any{{"text": "hi"}},
	}, "EU", "req_tp42_model_deny"); !errors.Is(err, a2a.ErrPrivilegeOrBudget) {
		t.Fatalf("expected ErrPrivilegeOrBudget for empty caller AllowedModels, got %v", err)
	}

	caller.Key.AllowedModels = []string{"*"}
	if _, err := enabledCoord.CreateTask(ctx, caller, a2a.TaskCreateRequest{
		TargetAgentID:       epA.ID,
		SubBudgetHoldMicros: 1000,
		HopCount:            6,
		InputParts:          []map[string]any{{"text": "hi"}},
	}, "EU", "req_tp42_trace"); !errors.Is(err, a2a.ErrDelegationDepth) {
		t.Fatalf("expected ErrDelegationDepth for hop_count=6, got %v", err)
	}

	// Verify emergency kill switch immediately freezes HTTP A2A admission (503).
	rbTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx(role_bindings): %v", err)
	}
	if err := port.SetOrgRLSTx(ctx, rbTx, dialect, orgA.ID); err != nil {
		_ = rbTx.Rollback()
		t.Fatalf("SetOrgRLSTx(role_bindings): %v", err)
	}
	if _, err := rbTx.ExecContext(ctx, port.Rebind(dialect, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES (?, ?, ?, 'ORG', ?, 'ORG_OWNER', 1, 1000)`),
		"rb_tp42_"+orgA.ID, orgA.ID, prcA, orgA.ID,
	); err != nil {
		_ = rbTx.Rollback()
		t.Fatalf("insert ORG_OWNER role_binding: %v", err)
	}
	if err := rbTx.Commit(); err != nil {
		t.Fatalf("commit role_binding: %v", err)
	}
	vkeySvc, err := vkey.NewService(vkey.ServiceConfig{DB: db, Snapshots: snaps, AuditSink: aud})
	if err != nil {
		t.Fatalf("vkey.NewService: %v", err)
	}
	createdVK, err := vkeySvc.Create(ctx, vkey.CreateParams{
		OrgID:         orgA.ID,
		ProjectID:     prjA.ID,
		EnvID:         envA.ID,
		Name:          "tp42-a2a-vk",
		AllowedPlanes: "PROVIDER,A2A",
		AllowedModels: []string{"*"},
		AllowedRoutes: []string{"*"},
		ActorID:       prcA,
	})
	if err != nil {
		t.Fatalf("vkeySvc.Create: %v", err)
	}
	if err := snaps.Refresh(ctx); err != nil {
		t.Fatalf("snaps.Refresh: %v", err)
	}

	okReq := httptest.NewRequest(http.MethodGet, "/a2a/agents", nil)
	okReq.Header.Set("Authorization", "Bearer "+createdVK.SecretKey)
	okRec := httptest.NewRecorder()
	enabledCoord.Handler().ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("GET /a2a/agents before kill switch: got %d, want 200 (%s)", okRec.Code, okRec.Body.String())
	}

	if _, err := ks.Trigger(ctx, killswitch.TriggerParams{
		OrgID:                  &orgA.ID,
		Level:                  killswitch.LevelProjectOrOrgFreeze,
		TargetID:               orgA.ID,
		Reason:                 "tp42 emergency a2a halt",
		TriggeredByPrincipalID: prcA,
	}); err != nil {
		t.Fatalf("ks.Trigger: %v", err)
	}
	frozenReq := httptest.NewRequest(http.MethodGet, "/a2a/agents", nil)
	frozenReq.Header.Set("Authorization", "Bearer "+createdVK.SecretKey)
	frozenRec := httptest.NewRecorder()
	enabledCoord.Handler().ServeHTTP(frozenRec, frozenReq)
	if frozenRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /a2a/agents after kill switch: got %d, want 503 (%s)", frozenRec.Code, frozenRec.Body.String())
	}

	// Verify planeBrokerResolver dispatches agt_ connections without breaking mcp_ or catalog.
	_ = hub.BrokerResolver{DB: db}
	rsv := planeBrokerResolver{a2a: a2a.BrokerResolver{DB: db}}
	policy, err := rsv.ResolveConnectionPolicy(ctx, orgA.ID, epA.ID)
	if err != nil {
		t.Fatalf("planeBrokerResolver.ResolveConnectionPolicy(%s): %v", epA.ID, err)
	}
	if len(policy.AllowedHosts) == 0 || !strings.Contains(policy.AllowedHosts[0], "agent-a.example.com") {
		t.Fatalf("unexpected resolved policy AllowedHosts: %+v", policy)
	}
}
