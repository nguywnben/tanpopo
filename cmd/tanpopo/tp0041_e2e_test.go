// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/ingress/cache"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/foureyes"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func TestTPIMP0041DualDB_FourEyesAndResponseCacheForceRLS(t *testing.T) {
	ctx := context.Background()

	t.Run("sqlite", func(t *testing.T) {
		st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "tp41_sqlite.db")})
		if err != nil {
			t.Fatalf("sqlite.Open: %v", err)
		}
		defer func() { _ = st.Close() }()
		runTP0041DualDBSuite(t, ctx, st, st.RawDB(), st.CheckpointWAL, false)
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
		runTP0041DualDBSuite(t, ctx, st, st.DB(), func(context.Context) error { return nil }, true)
	})
}

func runTP0041DualDBSuite(
	t *testing.T,
	ctx context.Context,
	st port.Store,
	db *sql.DB,
	postShred func(context.Context) error,
	isPG bool,
) {
	t.Helper()

	aud, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	masterRaw := sha256.Sum256([]byte("tp0041-dualdb-master-kek"))
	kek, err := vault.NewStaticKEKProvider("kek_tp41_v1", masterRaw[:])
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	vs, err := vault.NewStore(vault.Config{DB: db, KEK: kek, PostShredHook: postShred})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	feSvc, err := foureyes.NewService(foureyes.Config{DB: db, AuditSink: aud})
	if err != nil {
		t.Fatalf("foureyes.NewService: %v", err)
	}
	ec, err := cache.NewExactCache(cache.Config{DB: db, Vault: vs})
	if err != nil {
		t.Fatalf("cache.NewExactCache: %v", err)
	}

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "tp41-org-a", DisplayName: "Org A"})
	if err != nil {
		t.Fatalf("CreateOrganization A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "tp41-org-b", DisplayName: "Org B"})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}
	pA1, _ := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "P1"})
	pA2, _ := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p2", DisplayName: "P2"})
	pB1, _ := st.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "pb", DisplayName: "PB"})
	eA1, _ := st.Tenancy().CreateEnvironment(ctx, orgA.ID, pA1.ID, port.CreateEnvironmentParams{Slug: "prod"})
	eA2, _ := st.Tenancy().CreateEnvironment(ctx, orgA.ID, pA2.ID, port.CreateEnvironmentParams{Slug: "prod"})
	eB1, _ := st.Tenancy().CreateEnvironment(ctx, orgB.ID, pB1.ID, port.CreateEnvironmentParams{Slug: "prod"})

	// Insert principals in Org A under RLS.
	admin1 := idgen.MustNew(idgen.PrefixPrincipal)
	admin2 := idgen.MustNew(idgen.PrefixPrincipal)
	dialect := port.DetectDialect(ctx, db)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := port.SetOrgRLSTx(ctx, tx, dialect, orgA.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("SetOrgRLSTx: %v", err)
	}
	for i, pid := range []string{admin1, admin2} {
		email := "dual-a1@example.test"
		if i == 1 {
			email = "dual-a2@example.test"
		}
		if _, err := tx.ExecContext(ctx, port.Rebind(dialect, `
			INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms)
			VALUES (?, ?, 'HUMAN_LOCAL', ?, 'ACTIVE', 1000, 1000)`),
			pid, orgA.ID, email,
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert principal: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit principals: %v", err)
	}

	// 1. Four-Eyes Change Requests across Org A vs Org B under FORCE RLS.
	cr, err := feSvc.Initiate(ctx, foureyes.InitiateParams{
		OrgID: &orgA.ID, ActionType: foureyes.ActionKillSwitchClear,
		TargetType: "kill_switch", TargetID: "ksw_dual_1",
		PayloadJSON: json.RawMessage(`{"clear_reason":"verified dual db"}`), InitiatorPrincipalID: admin1,
	})
	if err != nil {
		t.Fatalf("foureyes.Initiate: %v", err)
	}
	if _, err := feSvc.Get(ctx, &orgB.ID, cr.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org Get leaked change request: %v", err)
	}
	listB, err := feSvc.List(ctx, &orgB.ID, "", 10)
	if err != nil || len(listB) != 0 {
		t.Fatalf("cross-org List leaked change requests: %+v err=%v", listB, err)
	}
	if isPG {
		// Direct RLS probe: setting Org B RLS context must yield 0 rows from admin_change_requests.
		rtx, _ := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		_ = port.SetOrgRLSTx(ctx, rtx, dialect, orgB.ID)
		var leaked int
		_ = rtx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_change_requests WHERE id = $1`, cr.ID).Scan(&leaked)
		_ = rtx.Rollback()
		if leaked != 0 {
			t.Fatalf("PG16 FORCE RLS violation on admin_change_requests: Org B saw %d rows", leaked)
		}
	}
	if _, err := feSvc.Decide(ctx, foureyes.DecideParams{
		OrgID: &orgA.ID, RequestID: cr.ID, ApproverPrincipalID: admin1, Decision: foureyes.DecisionApprove, ExpectedVersion: 1,
	}, nil); !errors.Is(err, foureyes.ErrSelfApprovalForbidden) {
		t.Fatalf("expected ErrSelfApprovalForbidden, got %v", err)
	}
	if _, err := feSvc.Decide(ctx, foureyes.DecideParams{
		OrgID: &orgA.ID, RequestID: cr.ID, ApproverPrincipalID: admin2, Decision: foureyes.DecisionApprove, ExpectedVersion: 1,
	}, nil); err != nil {
		t.Fatalf("admin2 approve failed: %v", err)
	}

	// 2. Response Cache isolation under Dual-DB + PG16 FORCE RLS (CT-28 / AC-3).
	ttl := int64(300)
	effEnabled := engine.EffectivePolicy{HasResponseCache: true, ResponseCacheEnabled: true, ResponseCacheTTLSeconds: &ttl}
	promptQ := []byte(`{"model":"gpt-4o","temperature":0,"messages":[{"role":"user","content":"dual db isolation check"}]}`)
	el1, err := ec.EvaluateEligibility(http.Header{}, effEnabled, cache.KeyInput{
		OrgID: orgA.ID, ProjectID: pA1.ID, EnvID: eA1.ID, PolicyRevision: "r1", ModelDeploymentID: "dpl_1", RequestBody: promptQ,
	})
	if err != nil || !el1.Eligible {
		t.Fatalf("EvaluateEligibility pA1: %+v err=%v", el1, err)
	}
	if err := ec.Store(ctx, orgA.ID, pA1.ID, eA1.ID, "dpl_1", el1.CacheKeySHA256, []byte(`{"choices":[{"message":{"content":"orgA-p1"}}]}`), ttl); err != nil {
		t.Fatalf("ec.Store pA1: %v", err)
	}
	if got, err := ec.Lookup(ctx, orgA.ID, pA1.ID, eA1.ID, el1.CacheKeySHA256); err != nil || !strings.Contains(string(got), "orgA-p1") {
		t.Fatalf("ec.Lookup pA1 HIT expected, got err=%v body=%q", err, string(got))
	}
	el2, _ := ec.EvaluateEligibility(http.Header{}, effEnabled, cache.KeyInput{
		OrgID: orgA.ID, ProjectID: pA2.ID, EnvID: eA2.ID, PolicyRevision: "r1", ModelDeploymentID: "dpl_1", RequestBody: promptQ,
	})
	elB, _ := ec.EvaluateEligibility(http.Header{}, effEnabled, cache.KeyInput{
		OrgID: orgB.ID, ProjectID: pB1.ID, EnvID: eB1.ID, PolicyRevision: "r1", ModelDeploymentID: "dpl_1", RequestBody: promptQ,
	})
	if _, err := ec.Lookup(ctx, orgA.ID, pA2.ID, eA2.ID, el2.CacheKeySHA256); !errors.Is(err, cache.ErrCacheMiss) {
		t.Fatalf("expected pA2 cache MISS, got %v", err)
	}
	if _, err := ec.Lookup(ctx, orgB.ID, pB1.ID, eB1.ID, elB.CacheKeySHA256); !errors.Is(err, cache.ErrCacheMiss) {
		t.Fatalf("expected orgB cache MISS, got %v", err)
	}

	// Shred pA1 Project DEK -> cascades / renders cached entry permanently unusable.
	meta, err := vs.EnsureProjectDEK(ctx, orgA.ID, pA1.ID)
	if err != nil {
		t.Fatalf("EnsureProjectDEK: %v", err)
	}
	if err := vs.ShredProjectDEK(ctx, orgA.ID, pA1.ID, vault.ShredRequest{RequestID: "dual-shred-p1", ExpectedGeneration: meta.KeyGeneration}); err != nil {
		t.Fatalf("ShredProjectDEK: %v", err)
	}
	if _, err := ec.Lookup(ctx, orgA.ID, pA1.ID, eA1.ID, el1.CacheKeySHA256); err == nil {
		t.Fatalf("expected cache lookup failure after Project DEK shred")
	}
}
