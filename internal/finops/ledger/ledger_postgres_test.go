// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
)

var ledgerSchemaSeq atomic.Int64

func livePGForLedger(t *testing.T) *postgres.Store {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if baseDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN is required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		return nil
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("tp_ledger_pg_%d_%d", time.Now().UnixNano(), ledgerSchemaSeq.Add(1))
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

	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC).UnixMilli()
	pgStore, err := postgres.Open(ctx, postgres.Options{
		DSN:   u.String(),
		NowMs: func() int64 { return now },
	})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = pgStore.Close() })
	return pgStore
}

func TestPostgresLedgerContractCT29(t *testing.T) {
	pgStore := livePGForLedger(t)
	if pgStore == nil {
		return
	}
	ctx := context.Background()
	nowMs := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC).UnixMilli()
	var clock atomic.Int64
	clock.Store(nowMs)

	svc, err := ledger.NewService(ledger.Config{
		DB:         pgStore.DB(),
		NowMs:      func() int64 { return clock.Load() },
		LeaseTTLMs: 5000,
	})
	if err != nil {
		t.Fatalf("ledger.NewService: %v", err)
	}

	org, err := pgStore.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "pg-ledger-org", DisplayName: "PG Ledger",
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

	// Upsert org & project ledgers with tight $0.000500 (500 micros) cap on project.
	_, err = svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID: org.ID, ScopeType: ledger.ScopeOrg, ScopeID: org.ID,
		PeriodKind: ledger.PeriodMonthly, PeriodStartMs: nowMs - 10000, PeriodEndMs: nowMs + 86400000,
		HardLimitMicros: 10_000, SoftAlertThresholdsPct: []int{50, 80, 100},
	})
	if err != nil {
		t.Fatalf("UpsertLedger org: %v", err)
	}
	prjLedger, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID: org.ID, ScopeType: ledger.ScopeProject, ScopeID: prj.ID,
		PeriodKind: ledger.PeriodMonthly, PeriodStartMs: nowMs - 10000, PeriodEndMs: nowMs + 86400000,
		HardLimitMicros: 500, SoftAlertThresholdsPct: []int{50, 80, 100},
	})
	if err != nil {
		t.Fatalf("UpsertLedger project: %v", err)
	}

	// Race 8 concurrent reservations for 100 micros each against 500 micros limit -> exactly 5 succeed.
	const workers = 8
	var wg sync.WaitGroup
	var succeeded atomic.Int64
	var exceeded atomic.Int64
	type raceWin struct {
		idx int
		id  string
	}
	var resIDs sync.Map
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rerr, res := error(nil), (*ledger.Reservation)(nil)
			res, rerr = svc.Reserve(ctx, ledger.ReserveRequest{
				OrgID:        org.ID,
				ProjectID:    prj.ID,
				EnvID:        env.ID,
				VirtualKeyID: "vkey_pg_01",
				RequestID:    fmt.Sprintf("req_pg_race_%d", idx),
				HoldMicros:   100,
			})
			if rerr == nil {
				succeeded.Add(1)
				resIDs.Store(idx, raceWin{idx: idx, id: res.ID})
				return
			}
			var bErr *ledger.BudgetExceededError
			if errors.As(rerr, &bErr) {
				exceeded.Add(1)
				return
			}
			t.Errorf("unexpected Reserve error: %v", rerr)
		}(i)
	}
	wg.Wait()
	if succeeded.Load() != 5 || exceeded.Load() != 3 {
		t.Fatalf("concurrent Reserve counts = (%d succeeded, %d exceeded), want (5, 3)", succeeded.Load(), exceeded.Load())
	}

	// Settle one reservation with exact cost exceeding remaining hold headroom -> Negative Balance Carryover.
	var firstWin raceWin
	resIDs.Range(func(_, val any) bool {
		firstWin = val.(raceWin)
		return false
	})
	setRes, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:            org.ID,
		ReservationID:    firstWin.id,
		RequestID:        fmt.Sprintf("req_pg_race_%d", firstWin.idx),
		ProjectID:        prj.ID,
		EnvID:            env.ID,
		VirtualKeyID:     "vkey_pg_01",
		Plane:            ledger.PlaneProvider,
		LogicalModelSlug: "gpt-test",
		Usage:            pricing.UsageInput{UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact, InputTokens: 600},
		Rate:             pricing.RateCard{BillingMode: pricing.BillingModeCommercialAPI, InputPer1MNanoUSD: 1_000_000_000}, // 1 micro-USD per token -> 600 micros
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if setRes.TotalCostMicros != 600 {
		t.Fatalf("TotalCostMicros = %d, want 600", setRes.TotalCostMicros)
	}
	updatedLedger, err := svc.GetLedger(ctx, org.ID, prjLedger.ID)
	if err != nil {
		t.Fatalf("GetLedger: %v", err)
	}
	if updatedLedger.NegativeCarryoverMicros != 100 {
		t.Fatalf("NegativeCarryoverMicros = %d, want 100", updatedLedger.NegativeCarryoverMicros)
	}

	// Advance clock past lease expiry and run OrphanReservationReaper across PG RLS.
	clock.Store(nowMs + 10_000)
	reaper, err := ledger.NewOrphanReservationReaper(svc, 100*time.Millisecond, 100)
	if err != nil {
		t.Fatalf("NewOrphanReservationReaper: %v", err)
	}
	reclaimed, err := reaper.ReapOnce(ctx)
	if err != nil {
		t.Fatalf("ReapOnce: %v", err)
	}
	if reclaimed != 4 {
		t.Fatalf("reclaimed = %d, want 4", reclaimed)
	}
	afterReap, err := svc.GetLedger(ctx, org.ID, prjLedger.ID)
	if err != nil {
		t.Fatalf("GetLedger after reap: %v", err)
	}
	if afterReap.ReservedHoldMicros != 0 {
		t.Fatalf("ReservedHoldMicros after reap = %d, want 0", afterReap.ReservedHoldMicros)
	}

	// Verify concurrent direct Settle (ReservationID=="", same RequestID) bills once via advisory lock.
	const directWorkers = 6
	var dWG sync.WaitGroup
	var freshCount, dupCount atomic.Int64
	for i := 0; i < directWorkers; i++ {
		dWG.Add(1)
		go func() {
			defer dWG.Done()
			sr, serr := svc.Settle(ctx, ledger.SettleRequest{
				OrgID:            org.ID,
				RequestID:        "req_pg_direct_idem",
				ProjectID:        prj.ID,
				EnvID:            env.ID,
				VirtualKeyID:     "vkey_pg_01",
				Plane:            ledger.PlaneProvider,
				LogicalModelSlug: "gpt-test",
				Usage:            pricing.UsageInput{UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact, InputTokens: 50},
				Rate:             pricing.RateCard{BillingMode: pricing.BillingModeCommercialAPI, InputPer1MNanoUSD: 1_000_000_000},
			})
			if serr != nil {
				t.Errorf("concurrent direct Settle err: %v", serr)
				return
			}
			if sr.AlreadySettled {
				dupCount.Add(1)
			} else {
				freshCount.Add(1)
			}
		}()
	}
	dWG.Wait()
	if freshCount.Load() != 1 || dupCount.Load() != directWorkers-1 {
		t.Fatalf("concurrent direct Settle = (fresh=%d dup=%d), want (1, %d)", freshCount.Load(), dupCount.Load(), directWorkers-1)
	}
}
