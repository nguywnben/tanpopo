// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package valkey

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/ratelimit"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestKeyspaceAndHashTagInvariantsCR07(t *testing.T) {
	t.Parallel()
	k1, err := Key("org_alpha", "rl", "rpm:prj_1:env_1:vk_1")
	if err != nil {
		t.Fatalf("Key(k1): %v", err)
	}
	if k1 != "tp:v1:{org_alpha}:rl:rpm:prj_1:env_1:vk_1" {
		t.Fatalf("unexpected canonical key %q", k1)
	}
	k2, err := Key("org_alpha", "budget", "hold:led_1")
	if err != nil {
		t.Fatalf("Key(k2): %v", err)
	}
	if err := AssertSameHashTag(k1, k2); err != nil {
		t.Fatalf("AssertSameHashTag(same org) unexpected error: %v", err)
	}

	kOther, err := Key("org_beta", "budget", "hold:led_2")
	if err != nil {
		t.Fatalf("Key(kOther): %v", err)
	}
	if err := AssertSameHashTag(k1, kOther); !errors.Is(err, ErrCrossOrgKeyViolation) {
		t.Fatalf("expected ErrCrossOrgKeyViolation for cross-org keys, got %v", err)
	}
	if err := AssertSameHashTag("tp:v1:missing_tag:k"); !errors.Is(err, ErrCrossOrgKeyViolation) {
		t.Fatalf("expected ErrCrossOrgKeyViolation for missing hash tag, got %v", err)
	}

	for _, bad := range []string{"", "org{inject}", "org}bad", "org:bad", "org space", "org\nbad"} {
		if err := ValidateOrgID(bad); !errors.Is(err, ErrInvalidKeyComponent) {
			t.Fatalf("ValidateOrgID(%q) = %v, want ErrInvalidKeyComponent", bad, err)
		}
	}
}

func TestRateLimiterDegradedLocalFallbackQuotient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fc := clock.NewFakeClock(time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC))
	lim, err := NewRateLimiter(RateLimiterConfig{
		Client:                nil, // Force degraded Valkey unavailable path.
		EstimatedReplicaCount: 3,
		Now:                   fc.Now,
	})
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}
	scope := ratelimit.LimitScope{
		OrgID: "org_1", ProjectID: "prj_1", EnvID: "env_1", VirtualKeyID: "vk_1",
	}
	rpm := int64(6) // 6 / 3 replicas = 2 per node
	tpm := int64(300)
	maxStreams := int64(3) // 3 / 3 = 1 per node
	limits := ratelimit.Limits{RPMLimit: &rpm, TPMLimit: &tpm, MaxConcurrentStreams: &maxStreams}

	for i := 0; i < 2; i++ {
		res, aerr := lim.AllowRequest(ctx, scope, 50, limits)
		if aerr != nil || !res.Allowed {
			t.Fatalf("AllowRequest %d failed: %+v err=%v", i, res, aerr)
		}
	}
	if !lim.IsDegraded() || lim.DegradedFallbacks() < 2 {
		t.Fatalf("expected degraded state and fallback count >= 2, got degraded=%v count=%d", lim.IsDegraded(), lim.DegradedFallbacks())
	}
	if _, aerr := lim.AllowRequest(ctx, scope, 50, limits); !errors.Is(aerr, ratelimit.ErrRateLimitExceeded) {
		t.Fatalf("expected ErrRateLimitExceeded after local fallback quotient exhausted, got %v", aerr)
	}

	lease, _, serr := lim.AcquireStreamSlot(ctx, scope, maxStreams)
	if serr != nil || lease == nil {
		t.Fatalf("AcquireStreamSlot fallback 1 failed: %v", serr)
	}
	if _, _, serr2 := lim.AcquireStreamSlot(ctx, scope, maxStreams); !errors.Is(serr2, ratelimit.ErrConcurrencyLimitExceeded) {
		t.Fatalf("expected ErrConcurrencyLimitExceeded on 2nd stream under 1/node fallback, got %v", serr2)
	}
	lease.Release()
}

func TestBudgetCoordinatorFallbackMarginOnSQLite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fc := clock.NewFakeClock(time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC))
	dbPath := filepath.Join(t.TempDir(), "tanpopo.db")
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, NowMs: func() int64 { return fc.Now().UnixMilli() }})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "vk-fb", DisplayName: "VK FB",
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	lSvc, err := ledger.NewService(ledger.Config{DB: st.RawDB(), NowMs: func() int64 { return fc.Now().UnixMilli() }})
	if err != nil {
		t.Fatalf("ledger.NewService: %v", err)
	}
	led, err := lSvc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID: org.ID, ScopeType: ledger.ScopeOrg, ScopeID: org.ID,
		PeriodKind: ledger.PeriodMonthly, HardLimitMicros: 1000,
		PeriodStartMs: fc.Now().UnixMilli() - 1000, PeriodEndMs: fc.Now().UnixMilli() + 86_400_000,
	})
	if err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO valkey_hold_checkpoints (org_id, ledger_id, node_id, active_hold_micro_usd, active_inflight_streams, updated_at_ms)
		VALUES (?, ?, 'node_peer', 600, 1, ?)`, org.ID, led.ID, fc.Now().UnixMilli()); err != nil {
		t.Fatalf("insert peer checkpoint: %v", err)
	}

	coord, err := NewBudgetCoordinator(BudgetCoordinatorConfig{
		Client:               nil, // Force PG/SQLite fallback path.
		DB:                   st.RawDB(),
		NodeID:               "node_self",
		SafetyMarginMicroUSD: 150,
		Now:                  fc.Now,
	})
	if err != nil {
		t.Fatalf("NewBudgetCoordinator: %v", err)
	}

	// 600 peer hold + 150 safety margin + 200 est = 950 <= 1000 (ALLOWED)
	okOut, err := coord.AcquireHold(ctx, HoldRequest{
		OrgID: org.ID, LedgerID: led.ID, ReservationID: "res_1", CeilingMicroUSD: 1000, EstimatedMicroUSD: 200,
	})
	if err != nil || !okOut.Allowed || !okOut.UsedPostgresFallback {
		t.Fatalf("expected allowed fallback hold, got %+v err=%v", okOut, err)
	}
	// 600 + 150 + 300 = 1050 > 1000 (REJECTED)
	if _, err := coord.AcquireHold(ctx, HoldRequest{
		OrgID: org.ID, LedgerID: led.ID, ReservationID: "res_2", CeilingMicroUSD: 1000, EstimatedMicroUSD: 300,
	}); !errors.Is(err, ErrBudgetCeilingExceeded) {
		t.Fatalf("expected ErrBudgetCeilingExceeded, got %v", err)
	}
}

func TestLiveThreeShardValkeyClusterAndDR02RebuildPG(t *testing.T) {
	clusterEnv := strings.TrimSpace(os.Getenv("TANPOPO_TEST_VALKEY_CLUSTER_ADDRS"))
	pgDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if clusterEnv == "" {
		if os.Getenv("TANPOPO_REQUIRE_VALKEY_CLUSTER_TESTS") == "1" {
			t.Fatal("TANPOPO_TEST_VALKEY_CLUSTER_ADDRS is required")
		}
		return
	}
	ctx := context.Background()
	addrs := strings.Split(clusterEnv, ",")
	client, err := Dial(Config{Addrs: addrs, DisableCache: true})
	if err != nil {
		t.Fatalf("Dial cluster: %v", err)
	}
	defer client.Close()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping cluster: %v", err)
	}

	lim, err := NewRateLimiter(RateLimiterConfig{Client: client, EstimatedReplicaCount: 3})
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}

	// Exercise 12 distinct org hash tags so keys fan out across all 3 cluster shards with 0 CROSSSLOT errors.
	rpm := int64(5)
	tpm := int64(1000)
	maxStreams := int64(2)
	limits := ratelimit.Limits{RPMLimit: &rpm, TPMLimit: &tpm, MaxConcurrentStreams: &maxStreams}
	for i := 0; i < 12; i++ {
		orgID := fmt.Sprintf("org_cluster_%02d_%d", i, time.Now().UnixNano())
		scope := ratelimit.LimitScope{OrgID: orgID, ProjectID: "prj_1", EnvID: "env_1", VirtualKeyID: "vk_1"}
		res, aerr := lim.AllowRequest(ctx, scope, 250, limits)
		if aerr != nil || !res.Allowed {
			t.Fatalf("AllowRequest org %s failed: err=%v res=%+v", orgID, aerr, res)
		}
		if err := lim.ReconcileTokens(ctx, scope, 250, 100, limits); err != nil {
			t.Fatalf("ReconcileTokens org %s: %v", orgID, err)
		}
		rem, rerr := lim.Remaining(ctx, scope, limits)
		if rerr != nil || rem.RemainingTokens < 850 {
			t.Fatalf("Remaining org %s = %+v err=%v", orgID, rem, rerr)
		}
		lease, _, serr := lim.AcquireStreamSlot(ctx, scope, maxStreams)
		if serr != nil {
			t.Fatalf("AcquireStreamSlot org %s: %v", orgID, serr)
		}
		lease.Release()
	}

	if pgDSN == "" {
		return
	}

	// Live PostgreSQL + 3-shard Valkey Cluster checkpoint flush & FLUSHALL -> DR-02 rebuild parity.
	schema := fmt.Sprintf("tp_vk_%d", time.Now().UnixNano())
	rawPG, err := sql.Open("pgx", pgDSN)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	defer func() { _ = rawPG.Close() }()
	if _, err := rawPG.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = rawPG.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schema))
	})
	sep := "?"
	if strings.Contains(pgDSN, "?") {
		sep = "&"
	}
	isoDSN := fmt.Sprintf("%s%soptions=-c%%20search_path%%3D%s", pgDSN, sep, schema)
	pgStore, err := postgres.Open(ctx, postgres.Options{DSN: isoDSN})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	defer func() { _ = pgStore.Close() }()

	org, err := pgStore.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "vk-pg-dr02", DisplayName: "VK PG DR02",
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	prj, err := pgStore.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{
		ID: idgen.MustNew(idgen.PrefixProject), Slug: "p1", DisplayName: "P1",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	env, err := pgStore.Tenancy().CreateEnvironment(ctx, org.ID, prj.ID, port.CreateEnvironmentParams{
		ID: idgen.MustNew(idgen.PrefixEnv), Slug: "prod",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	lSvc, err := ledger.NewService(ledger.Config{DB: pgStore.DB()})
	if err != nil {
		t.Fatalf("ledger.NewService: %v", err)
	}
	nowMs := time.Now().UnixMilli()
	led, err := lSvc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID: org.ID, ScopeType: ledger.ScopeOrg, ScopeID: org.ID,
		PeriodKind: ledger.PeriodMonthly, HardLimitMicros: 1000,
		PeriodStartMs: nowMs - 1000, PeriodEndMs: nowMs + 86_400_000,
	})
	if err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}
	resv, err := lSvc.Reserve(ctx, ledger.ReserveRequest{
		OrgID: org.ID, ProjectID: prj.ID, EnvID: env.ID, VirtualKeyID: "vkey_cluster_1",
		RequestID: fmt.Sprintf("req_cluster_%d", nowMs), HoldMicros: 700,
	})
	if err != nil || resv == nil {
		t.Fatalf("ledger Reserve: res=%v err=%v", resv, err)
	}

	coord, err := NewBudgetCoordinator(BudgetCoordinatorConfig{
		Client:               client,
		DB:                   pgStore.DB(),
		Ledger:               lSvc,
		NodeID:               "node_1",
		CheckpointInterval:   50 * time.Millisecond,
		SafetyMarginMicroUSD: 50,
	})
	if err != nil {
		t.Fatalf("NewBudgetCoordinator: %v", err)
	}
	if _, err := coord.AcquireHold(ctx, HoldRequest{
		OrgID: org.ID, LedgerID: led.ID, ReservationID: resv.ID, CeilingMicroUSD: 1000, EstimatedMicroUSD: 700,
	}); err != nil {
		t.Fatalf("AcquireHold: %v", err)
	}
	if err := coord.FlushCheckpoints(ctx); err != nil {
		t.Fatalf("FlushCheckpoints: %v", err)
	}

	// Simulate total Valkey flush across every shard primary, then verify DR-02 100% rebuild.
	for _, addr := range addrs {
		sc, serr := Dial(Config{Addrs: []string{strings.TrimSpace(addr)}, DisableCache: true})
		if serr == nil {
			_ = sc.Raw().Do(ctx, sc.Raw().B().Flushall().Build()).Error()
			sc.Close()
		}
	}
	rebuilt, err := RebuildLedgerHoldsFromPostgres(ctx, pgStore.DB(), client, nowMs)
	if err != nil || rebuilt != 1 {
		t.Fatalf("RebuildLedgerHoldsFromPostgres = (%d, %v), want (1, nil)", rebuilt, err)
	}
	// After rebuild, requesting +400 on ceiling 1000 (with 700 rebuilt hold) MUST reject cleanly!
	if _, err := coord.AcquireHold(ctx, HoldRequest{
		OrgID: org.ID, LedgerID: led.ID, ReservationID: "res_after_rebuild", CeilingMicroUSD: 1000, EstimatedMicroUSD: 400,
	}); !errors.Is(err, ErrBudgetCeilingExceeded) {
		t.Fatalf("expected ErrBudgetCeilingExceeded after DR-02 Valkey rebuild, got %v", err)
	}
}
