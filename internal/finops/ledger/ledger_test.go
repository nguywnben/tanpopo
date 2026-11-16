// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	sqlitemigrations "github.com/nguywnben/tanpopo/db/migrations/sqlite"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/storage/migrate"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

type spyAuditSink struct {
	mu     sync.Mutex
	db     *sql.DB
	seq    int64
	events []storageport.AuditEvent
	err    error
}

func (s *spyAuditSink) attachDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_events (
			id            VARCHAR(40) NOT NULL,
			created_at_ms BIGINT NOT NULL,
			org_id        VARCHAR(40),
			chain_key     VARCHAR(40) NOT NULL,
			seq_no        BIGINT NOT NULL CHECK (seq_no >= 1),
			actor_kind    VARCHAR(24) NOT NULL,
			actor_id      VARCHAR(64) NOT NULL,
			entry_point   VARCHAR(24) NOT NULL
			              CHECK (entry_point IN ('admin_api', 'cli', 'gitops_boot', 'system_reaper', 'data_plane_guard')),
			event_type    VARCHAR(64) NOT NULL,
			target_type   VARCHAR(32) NOT NULL,
			target_id     VARCHAR(64) NOT NULL,
			request_id    VARCHAR(40),
			metadata_json TEXT NOT NULL,
			prev_hash     CHAR(64) NOT NULL,
			entry_hash    CHAR(64) NOT NULL,
			CHECK ((org_id IS NULL AND chain_key = 'SYSTEM') OR (org_id IS NOT NULL AND chain_key = org_id)),
			PRIMARY KEY (chain_key, seq_no, created_at_ms),
			UNIQUE (chain_key, seq_no),
			UNIQUE (id, created_at_ms)
		)`); err != nil {
		t.Fatalf("create audit_events table: %v", err)
	}
	s.mu.Lock()
	s.db = db
	s.mu.Unlock()
}

func (s *spyAuditSink) Append(ctx context.Context, ev storageport.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		s.seq++
		chainKey := "SYSTEM"
		var orgArg any
		if ev.OrgID != nil && *ev.OrgID != "" {
			chainKey = *ev.OrgID
			orgArg = *ev.OrgID
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO audit_events (
				id, created_at_ms, org_id, chain_key, seq_no,
				actor_kind, actor_id, entry_point, event_type,
				target_type, target_id, request_id, metadata_json,
				prev_hash, entry_hash
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ev.ID, ev.CreatedAtMs, orgArg, chainKey, s.seq,
			ev.ActorKind, ev.ActorID, ev.EntryPoint, ev.EventType,
			ev.TargetType, ev.TargetID, ev.RequestID, ev.MetadataJSON,
			strings.Repeat("0", 64), strings.Repeat("a", 64),
		); err != nil {
			s.err = err
			return err
		}
	}
	s.events = append(s.events, ev)
	return nil
}

func (s *spyAuditSink) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *spyAuditSink) Events() []storageport.AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]storageport.AuditEvent, len(s.events))
	copy(out, s.events)
	return out
}

type spyAlertSink struct {
	mu     sync.Mutex
	events []ledger.AlertEvent
}

func (s *spyAlertSink) EmitBudgetAlert(_ context.Context, ev ledger.AlertEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *spyAlertSink) Events() []ledger.AlertEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ledger.AlertEvent, len(s.events))
	copy(out, s.events)
	return out
}

func openLedgerFixture(t *testing.T, initialMs int64) (*sqlite.Store, *ledger.Service, *atomic.Int64, *spyAuditSink, *spyAlertSink) {
	t.Helper()
	clock := &atomic.Int64{}
	clock.Store(initialMs)
	auditSink := &spyAuditSink{}
	alertSink := &spyAlertSink{}

	dbPath := filepath.Join(t.TempDir(), "finops_ledger.db")
	st, err := sqlite.Open(context.Background(), sqlite.Config{
		DBPath:    dbPath,
		NowMs:     clock.Load,
		AuditSink: auditSink,
	})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	auditSink.attachDB(t, st.RawDB())
	t.Cleanup(func() {
		if err := auditSink.LastError(); err != nil {
			t.Errorf("audit_events SQLite constraint failure: %v", err)
		}
	})

	_, err = st.Tenancy().CreateOrganization(context.Background(), storageport.CreateOrgParams{
		ID:             "org_finops_01",
		Slug:           "finops-acme",
		DisplayName:    "FinOps Acme",
		MinPluginTrust: storageport.MinTrustOfficialOnly,
	})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	svc, err := ledger.NewService(ledger.Config{
		DB:         st.RawDB(),
		Audit:      auditSink,
		Alerts:     alertSink,
		NowMs:      clock.Load,
		LeaseTTLMs: 90_000,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return st, svc, clock, auditSink, alertSink
}

func TestFreshAndUpgradeMigration0011To0012(t *testing.T) {
	ctx := context.Background()
	entries, err := fs.ReadDir(sqlitemigrations.FS, ".")
	if err != nil {
		t.Fatalf("read migrations fs: %v", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	pre0012FS := fstest.MapFS{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		if entry.Name() >= "0012_" {
			continue
		}
		raw, rErr := fs.ReadFile(sqlitemigrations.FS, entry.Name())
		if rErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), rErr)
		}
		pre0012FS[entry.Name()] = &fstest.MapFile{Data: raw}
	}

	dbPath := filepath.Join(t.TempDir(), "upgrade_0011_to_0012.db")
	stOld, err := sqlite.Open(ctx, sqlite.Config{
		DBPath:       dbPath,
		MigrationsFS: pre0012FS,
	})
	if err != nil {
		t.Fatalf("open DB at 0011: %v", err)
	}
	_, err = stOld.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
		ID:             "org_upgrade_01",
		Slug:           "upgrade-org",
		DisplayName:    "Upgrade Org",
		MinPluginTrust: storageport.MinTrustOfficialOnly,
	})
	if err != nil {
		t.Fatalf("seed org on 0011: %v", err)
	}
	if err := stOld.Close(); err != nil {
		t.Fatalf("close old store: %v", err)
	}

	// Reopen with full embedded migrations (0001..0012) to verify live upgrade preserves existing rows.
	stUpgraded, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
	})
	if err != nil {
		t.Fatalf("upgrade DB to 0012: %v", err)
	}
	defer stUpgraded.Close()

	recs, err := migrate.ReadApplied(ctx, stUpgraded.RawDB())
	if err != nil {
		t.Fatalf("ReadApplied: %v", err)
	}
	if len(recs) == 0 || recs[len(recs)-1].Version != 12 {
		t.Fatalf("expected latest migration version 12, got %+v", recs)
	}
	org, err := stUpgraded.Tenancy().GetOrganization(ctx, "org_upgrade_01")
	if err != nil || org.Slug != "upgrade-org" {
		t.Fatalf("expected preserved org across 0011->0012 upgrade, got org=%+v err=%v", org, err)
	}

	// Verify immutability triggers reject UPDATE and DELETE on usage_settlements.
	svc, err := ledger.NewService(ledger.Config{DB: stUpgraded.RawDB()})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	res, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:        "org_upgrade_01",
		RequestID:    "req_immut_test",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_01",
		Plane:        ledger.PlaneProvider,
		Rate: pricing.RateCard{
			InputPer1MNanoUSD: 1_000_000_000,
			BillingMode:       pricing.BillingModeCommercialAPI,
		},
		Usage: pricing.UsageInput{
			InputTokens:          100,
			UpstreamUsagePresent: true,
			UpstreamAccuracy:     pricing.AccuracyExact,
		},
	})
	if err != nil {
		t.Fatalf("Settle on upgraded DB: %v", err)
	}
	if _, uErr := stUpgraded.RawDB().ExecContext(ctx, `UPDATE usage_settlements SET total_cost_micros = 0 WHERE id = ?`, res.SettlementID); uErr == nil {
		t.Fatalf("expected immutability trigger to reject UPDATE on usage_settlements")
	}
	if _, dErr := stUpgraded.RawDB().ExecContext(ctx, `DELETE FROM usage_settlements WHERE id = ?`, res.SettlementID); dErr == nil {
		t.Fatalf("expected immutability trigger to reject DELETE on usage_settlements")
	}
}

// TestCT29ConcurrentRaceUnderTightBudget verifies AC-1: 50 goroutines against 10,000 micro-USD
// budget with 2,500 micro-USD pre-hold -> at most 4 admitted, >= 46 fail fast with 429 project_budget_exceeded.
func TestCT29ConcurrentRaceUnderTightBudget(t *testing.T) {
	ctx := context.Background()
	const baseMs = 1_700_000_000_000
	_, svc, _, _, _ := openLedgerFixture(t, baseMs)

	bdg, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeProject,
		ScopeID:         "prj_tight_01",
		PeriodKind:      ledger.PeriodMonthly,
		PeriodStartMs:   baseMs - 10_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: 10_000,
	})
	if err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}

	const workers = 50
	var (
		admitted atomic.Int32
		rejected atomic.Int32
		otherErr atomic.Int32
	)
	startGate := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startGate
			_, rErr := svc.Reserve(ctx, ledger.ReserveRequest{
				OrgID:        "org_finops_01",
				ProjectID:    "prj_tight_01",
				EnvID:        "env_01",
				VirtualKeyID: "vky_01",
				RequestID:    fmt.Sprintf("req_ct29_%02d", idx),
				HoldMicros:   2_500,
			})
			if rErr == nil {
				admitted.Add(1)
			} else if errors.Is(rErr, ledger.ErrBudgetExceeded) {
				rejected.Add(1)
			} else {
				otherErr.Add(1)
				t.Logf("unexpected reserve err on worker %d: %v", idx, rErr)
			}
		}(i)
	}
	close(startGate)
	wg.Wait()

	if otherErr.Load() != 0 {
		t.Fatalf("unexpected non-budget errors: %d", otherErr.Load())
	}
	if admitted.Load() != 4 || rejected.Load() != 46 {
		t.Fatalf("CT-29 AC-1 FAILED: expected 4 admitted / 46 rejected (429), got admitted=%d rejected=%d", admitted.Load(), rejected.Load())
	}

	updated, err := svc.GetLedger(ctx, "org_finops_01", bdg.ID)
	if err != nil {
		t.Fatalf("GetLedger: %v", err)
	}
	if updated.ReservedHoldMicros != 10_000 || updated.AvailableBalance() != 0 {
		t.Fatalf("expected reserved=10000 available=0, got reserved=%d available=%d", updated.ReservedHoldMicros, updated.AvailableBalance())
	}
}

func TestNegativeCarryoverTopUpAndDeltaReserveAndNEG2Idempotency(t *testing.T) {
	ctx := context.Background()
	const baseMs = 1_700_000_000_000
	_, svc, clock, _, alertSink := openLedgerFixture(t, baseMs)

	// Multi-scope hierarchy: Org limit (50_000) + Project limit (10_000).
	orgBdg, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         "org_finops_01",
		PeriodKind:      ledger.PeriodMonthly,
		PeriodStartMs:   baseMs - 10_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: 50_000,
	})
	if err != nil {
		t.Fatalf("UpsertLedger org: %v", err)
	}
	projBdg, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeProject,
		ScopeID:         "prj_carry_01",
		PeriodKind:      ledger.PeriodMonthly,
		PeriodStartMs:   baseMs - 10_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: 10_000,
	})
	if err != nil {
		t.Fatalf("UpsertLedger proj: %v", err)
	}

	// Phase 1 hold for primary target: 8,000 micro-USD, then DeltaReserve on failover to 9,500 micro-USD.
	rsv, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_carry_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_01",
		RequestID:    "req_carry_01",
		HoldMicros:   8_000,
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	rsv, err = svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{
		OrgID:         "org_finops_01",
		ReservationID: rsv.ID,
		NewHoldMicros: 9_500,
	})
	if err != nil {
		t.Fatalf("DeltaReserve: %v", err)
	}
	if rsv.HoldMicros != 9_500 {
		t.Fatalf("expected updated hold=9500, got %d", rsv.HoldMicros)
	}

	if err := svc.MarkStreaming(ctx, "org_finops_01", rsv.ID); err != nil {
		t.Fatalf("MarkStreaming: %v", err)
	}

	// Stream slightly overruns budget: settles 10,050 micro-USD (10_050 output tokens @ $1/1M -> 10_050 micro-USD).
	rate := pricing.RateCard{
		InputPer1MNanoUSD:  1_000_000_000,
		OutputPer1MNanoUSD: 1_000_000_000,
		BillingMode:        pricing.BillingModeCommercialAPI,
	}
	set1, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:            "org_finops_01",
		ReservationID:    rsv.ID,
		RequestID:        "req_carry_01",
		LogicalModelSlug: "gpt-4o",
		Rate:             rate,
		Usage: pricing.UsageInput{
			InputTokens:          50,
			OutputTokens:         10_000,
			UpstreamUsagePresent: true,
			UpstreamAccuracy:     pricing.AccuracyExact,
		},
	})
	if err != nil {
		t.Fatalf("Settle 1: %v", err)
	}
	if set1.TotalCostMicros != 10_050 || set1.AlreadySettled {
		t.Fatalf("unexpected settlement 1: %+v", set1)
	}

	// NEG-2: Second Settle(reservationID) must be an idempotent no-op without double-charging!
	clock.Add(100)
	set2, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:            "org_finops_01",
		ReservationID:    rsv.ID,
		RequestID:        "req_carry_01",
		LogicalModelSlug: "gpt-4o",
		Rate:             rate,
		Usage: pricing.UsageInput{
			InputTokens:          50,
			OutputTokens:         10_000,
			UpstreamUsagePresent: true,
			UpstreamAccuracy:     pricing.AccuracyExact,
		},
	})
	if err != nil {
		t.Fatalf("Settle 2 (NEG-2): %v", err)
	}
	if !set2.AlreadySettled || set2.SettlementID != set1.SettlementID {
		t.Fatalf("expected NEG-2 idempotent no-op matching settlement 1, got %+v", set2)
	}

	// Verify AC-3: -50 available balance, carryover=50, alerts emitted (50/80/100).
	projAfter, err := svc.GetLedger(ctx, "org_finops_01", projBdg.ID)
	if err != nil {
		t.Fatalf("GetLedger proj: %v", err)
	}
	if projAfter.SettledExactMicros != 10_050 || projAfter.NegativeCarryoverMicros != 50 || projAfter.AvailableBalance() != -50 {
		t.Fatalf("AC-3 carryover mismatch: %+v (avail=%d)", projAfter, projAfter.AvailableBalance())
	}
	orgAfter, _ := svc.GetLedger(ctx, "org_finops_01", orgBdg.ID)
	if orgAfter.SettledExactMicros != 10_050 || orgAfter.ReservedHoldMicros != 0 {
		t.Fatalf("expected org ledger synchronized to 10050 exact and 0 reserved, got %+v", orgAfter)
	}

	// Next request MUST fail immediately with 429 project_budget_exceeded!
	if _, rErr := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_carry_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_01",
		RequestID:    "req_blocked_carryover",
		HoldMicros:   1,
	}); !errors.Is(rErr, ledger.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded while negative carryover active, got %v", rErr)
	}

	// Admin tops up limit from 10,000 -> 10,100: carryover clears and +50 becomes available.
	projTopped, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeProject,
		ScopeID:         "prj_carry_01",
		PeriodKind:      ledger.PeriodMonthly,
		PeriodStartMs:   baseMs - 10_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: 10_100,
	})
	if err != nil {
		t.Fatalf("top-up UpsertLedger: %v", err)
	}
	if projTopped.NegativeCarryoverMicros != 0 || projTopped.AvailableBalance() != 50 {
		t.Fatalf("expected cleared carryover and +50 available after top-up, got %+v", projTopped)
	}

	// Verify ALT-BUDGET-80 alert fired linked to runbook INC-01.
	sawBudget80 := false
	for _, ev := range alertSink.Events() {
		if ev.Code == "ALT-BUDGET-80" && ev.Runbook == "INC-01" {
			sawBudget80 = true
		}
	}
	if !sawBudget80 {
		t.Fatalf("expected ALT-BUDGET-80 alert with Runbook=INC-01, got %+v", alertSink.Events())
	}
}

func TestAC2UnmeteredFlaggedAlertAndOrphanReaperAndDoubleRelease(t *testing.T) {
	ctx := context.Background()
	const baseMs = 1_700_000_000_000
	_, svc, clock, auditSink, alertSink := openLedgerFixture(t, baseMs)

	bdg, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeVirtualKey,
		ScopeID:         "vky_unmetered",
		PeriodKind:      ledger.PeriodDaily,
		PeriodStartMs:   baseMs - 10_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: 5_000,
	})
	if err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}

	// AC-2.2: Unpriced self-hosted model settles 0 micro-USD stamped UNMETERED_FLAGGED + ALT-UNMETERED-SPEND.
	rsvUnmetered, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_unmetered",
		RequestID:    "req_unmetered_01",
		HoldMicros:   0,
	})
	if err != nil {
		t.Fatalf("Reserve unmetered: %v", err)
	}
	setUnmetered, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:            "org_finops_01",
		ReservationID:    rsvUnmetered.ID,
		RequestID:        "req_unmetered_01",
		DeploymentID:     "dpl_selfhosted_01",
		LogicalModelSlug: "llama-local",
		Rate: pricing.RateCard{
			BillingMode: pricing.BillingModeSelfHostedInfra,
		},
		Usage: pricing.UsageInput{
			InputTokens:          500,
			OutputTokens:         200,
			UpstreamUsagePresent: true,
		},
	})
	if err != nil {
		t.Fatalf("Settle unmetered: %v", err)
	}
	if setUnmetered.Accuracy != pricing.AccuracyUnmeteredFlagged || setUnmetered.TotalCostMicros != 0 || !setUnmetered.UnmeteredFlagged {
		t.Fatalf("expected UNMETERED_FLAGGED with 0 micro-USD, got %+v", setUnmetered)
	}
	if svc.UnmeteredSpendCount() != 1 {
		t.Fatalf("expected UnmeteredSpendCount=1, got %d", svc.UnmeteredSpendCount())
	}
	sawUnmeteredAlert := false
	for _, ev := range alertSink.Events() {
		if ev.Code == "ALT-UNMETERED-SPEND" && ev.Runbook == "INC-01" {
			sawUnmeteredAlert = true
		}
	}
	if !sawUnmeteredAlert {
		t.Fatalf("expected ALT-UNMETERED-SPEND alert linked to INC-01, got %+v", alertSink.Events())
	}

	// NEG-2: Double Release idempotency check.
	rsvRel, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_unmetered",
		RequestID:    "req_double_release",
		HoldMicros:   1_000,
	})
	if err != nil {
		t.Fatalf("Reserve double release: %v", err)
	}
	if already, rErr := svc.Release(ctx, "org_finops_01", rsvRel.ID); rErr != nil || already {
		t.Fatalf("first Release failed: already=%v err=%v", already, rErr)
	}
	if already, rErr := svc.Release(ctx, "org_finops_01", rsvRel.ID); rErr != nil || !already {
		t.Fatalf("second Release should be idempotent no-op: already=%v err=%v", already, rErr)
	}

	// OrphanReservationReaper: reserve STREAMING_ACCRUING + RESERVED_HOLD across period rollover, expire leases, reclaim.
	rsvOrphan1, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_unmetered",
		RequestID:    "req_orphan_01",
		HoldMicros:   2_000,
	})
	if err != nil {
		t.Fatalf("Reserve orphan 1: %v", err)
	}
	if err := svc.MarkStreaming(ctx, "org_finops_01", rsvOrphan1.ID); err != nil {
		t.Fatalf("MarkStreaming orphan: %v", err)
	}

	reaper, err := ledger.NewOrphanReservationReaper(svc, 5*time.Millisecond, 50)
	if err != nil {
		t.Fatalf("NewOrphanReservationReaper: %v", err)
	}

	// Advance clock past lease AND past period_end_ms to verify cross-period reclamation works cleanly!
	clock.Store(baseMs + 90_000_000)
	reapedCount, err := reaper.ReapOnce(ctx)
	if err != nil {
		t.Fatalf("ReapOnce: %v", err)
	}
	if reapedCount != 1 {
		t.Fatalf("expected 1 orphan hold reclaimed, got %d", reapedCount)
	}

	reclaimedRes, err := svc.GetReservation(ctx, "org_finops_01", rsvOrphan1.ID)
	if err != nil || reclaimedRes.State != ledger.StateExpiredReclaimed {
		t.Fatalf("expected state EXPIRED_RECLAIMED, got %+v err=%v", reclaimedRes, err)
	}
	afterReap, err := svc.GetLedger(ctx, "org_finops_01", bdg.ID)
	if err != nil || afterReap.ReservedHoldMicros != 0 {
		t.Fatalf("expected reserved_hold_micros=0 after reclaim, got %+v err=%v", afterReap, err)
	}
	events := auditSink.Events()
	if len(events) == 0 || events[len(events)-1].EventType != "BUDGET_RESERVATION_RECLAIMED" {
		t.Fatalf("expected BUDGET_RESERVATION_RECLAIMED audit event, got %+v", events)
	}
}

func TestLedgerLifecycleValidationRenewAndTenantIsolation(t *testing.T) {
	ctx := context.Background()
	const baseMs = 1_700_000_000_000
	_, svc, clock, _, _ := openLedgerFixture(t, baseMs)

	// Invalid constructors & input guards.
	if _, err := ledger.NewService(ledger.Config{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil DB, got %v", err)
	}
	if _, err := ledger.NewOrphanReservationReaper(nil, 0, 0); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil svc reaper, got %v", err)
	}
	if _, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on empty UpsertLedger, got %v", err)
	}
	if _, err := svc.GetLedger(ctx, "", "led_x"); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on empty GetLedger, got %v", err)
	}
	if _, err := svc.GetReservation(ctx, "", "rsv_x"); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on empty GetReservation, got %v", err)
	}

	bdg, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         "org_finops_01",
		PeriodKind:      ledger.PeriodTotalLifetime,
		PeriodStartMs:   baseMs - 10_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: 5_000,
	})
	if err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}

	// Cross-org tenant isolation checks.
	if _, err := svc.GetLedger(ctx, "org_other", bdg.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-org GetLedger, got %v", err)
	}

	// Reserve validation + idempotent replay on identical (org_id, request_id, hold_micros).
	if _, err := svc.Reserve(ctx, ledger.ReserveRequest{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on invalid Reserve, got %v", err)
	}
	rsv, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_01",
		RequestID:    "req_idem_res_1",
		HoldMicros:   1_000,
	})
	if err != nil {
		t.Fatalf("Reserve 1: %v", err)
	}
	rsvReplay, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_01",
		RequestID:    "req_idem_res_1",
		HoldMicros:   1_000,
	})
	if err != nil || rsvReplay.ID != rsv.ID {
		t.Fatalf("expected idempotent Reserve replay, got rsv=%+v err=%v", rsvReplay, err)
	}
	if _, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_01",
		RequestID:    "req_idem_res_1",
		HoldMicros:   2_000,
	}); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition on conflicting hold replay, got %v", err)
	}

	// Cross-org reservation lookup fails.
	if _, err := svc.GetReservation(ctx, "org_other", rsv.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on cross-org GetReservation, got %v", err)
	}

	// DeltaReserve: downward/equal is a lease-extending no-op; exceeding limit fails with BudgetExceededError.
	sameRsv, err := svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{
		OrgID:         "org_finops_01",
		ReservationID: rsv.ID,
		NewHoldMicros: 500,
	})
	if err != nil || sameRsv.HoldMicros != 1_000 {
		t.Fatalf("expected unchanged HoldMicros on downward DeltaReserve, got %+v err=%v", sameRsv, err)
	}
	var bErr *ledger.BudgetExceededError
	if _, err := svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{
		OrgID:         "org_finops_01",
		ReservationID: rsv.ID,
		NewHoldMicros: 9_000,
	}); !errors.As(err, &bErr) {
		t.Fatalf("expected *BudgetExceededError on exceeding DeltaReserve, got %v (msg=%q)", err, fmt.Sprint(err))
	}

	// RenewLease on active RESERVED_HOLD and STREAMING_ACCRUING, plus MarkStreaming idempotency.
	clock.Add(5_000)
	if err := svc.RenewLease(ctx, "org_finops_01", rsv.ID); err != nil {
		t.Fatalf("RenewLease RESERVED_HOLD: %v", err)
	}
	if err := svc.MarkStreaming(ctx, "org_finops_01", rsv.ID); err != nil {
		t.Fatalf("MarkStreaming: %v", err)
	}
	if err := svc.MarkStreaming(ctx, "org_finops_01", rsv.ID); err != nil {
		t.Fatalf("MarkStreaming idempotent extend: %v", err)
	}
	if _, err := svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{
		OrgID:         "org_finops_01",
		ReservationID: rsv.ID,
		NewHoldMicros: 1_500,
	}); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition for DeltaReserve during streaming, got %v", err)
	}

	// Settle with valid cost >= 80% triggers SoftThresholdAlertCount > 0; subsequent RenewLease/Release reject cleanly.
	rate := pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000, OutputPer1MNanoUSD: 1_000_000_000, BillingMode: pricing.BillingModeCommercialAPI}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:         "org_finops_01",
		ReservationID: rsv.ID,
		RequestID:     "req_idem_res_1",
		Plane:         ledger.PlaneMCP,
		Rate:          rate,
		Usage:         pricing.UsageInput{InputTokens: 4_500, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
	}); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if svc.SoftThresholdAlertCount() == 0 {
		t.Fatalf("expected SoftThresholdAlertCount > 0 at >=80%% utilization")
	}
	if err := svc.RenewLease(ctx, "org_finops_01", rsv.ID); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition on settled RenewLease, got %v", err)
	}
	if _, err := svc.Release(ctx, "org_finops_01", rsv.ID); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition on settled Release, got %v", err)
	}
	if err := svc.RenewLease(ctx, "org_finops_01", "rsv_missing"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing RenewLease, got %v", err)
	}
	if _, err := svc.Release(ctx, "org_finops_01", "rsv_missing"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing Release, got %v", err)
	}

	// Verify Start/Stop lifecycle of OrphanReservationReaper cleanly shuts down without leaks.
	reaper, err := ledger.NewOrphanReservationReaper(svc, 5*time.Millisecond, 10)
	if err != nil {
		t.Fatalf("NewOrphanReservationReaper: %v", err)
	}
	stopReaper := reaper.Start(ctx)
	time.Sleep(15 * time.Millisecond)
	stopReaper()

	// Exercise AlertSinkFunc, ScopeEnv, ScopeVirtualKey, PeriodDaily, Settle with no reservation (and RequestID idempotency),
	// ESTIMATED settlement, and invalid-input guards across Settle/DeltaReserve/MarkStreaming.
	var fnAlerts int
	stFn, svcFn, _, _, _ := openLedgerFixture(t, baseMs)
	svcDirect, err := ledger.NewService(ledger.Config{
		DB:     stFn.RawDB(),
		Alerts: ledger.AlertSinkFunc(func(_ context.Context, _ ledger.AlertEvent) { fnAlerts++ }),
		NowMs:  func() int64 { return baseMs },
	})
	if err != nil {
		t.Fatalf("NewService direct: %v", err)
	}
	if _, err := svcDirect.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:                  "org_finops_01",
		ScopeType:              ledger.ScopeEnv,
		ScopeID:                "env_direct",
		PeriodKind:             ledger.PeriodDaily,
		PeriodStartMs:          baseMs - 10_000,
		PeriodEndMs:            baseMs + 86_400_000,
		HardLimitMicros:        100,
		SoftAlertThresholdsPct: []int{50, 80, 100},
	}); err != nil {
		t.Fatalf("UpsertLedger env: %v", err)
	}
	setDirect, err := svcDirect.Settle(ctx, ledger.SettleRequest{
		OrgID:        "org_finops_01",
		RequestID:    "req_direct_est_1",
		ProjectID:    "prj_01",
		EnvID:        "env_direct",
		VirtualKeyID: "vky_01",
		Plane:        ledger.PlaneA2A,
		Rate:         rate,
		Usage: pricing.UsageInput{
			FallbackTextBytes:    300,
			FallbackOutputBytes:  300,
			UpstreamUsagePresent: false,
		},
	})
	if err != nil || setDirect.Accuracy != pricing.AccuracyEstimated || fnAlerts == 0 {
		t.Fatalf("expected ESTIMATED direct settlement triggering AlertSinkFunc, got set=%+v alerts=%d err=%v", setDirect, fnAlerts, err)
	}
	// Duplicate Settle by RequestID (no ReservationID) must replay idempotently.
	setDirectDup, err := svcDirect.Settle(ctx, ledger.SettleRequest{
		OrgID:        "org_finops_01",
		RequestID:    "req_direct_est_1",
		ProjectID:    "prj_01",
		EnvID:        "env_direct",
		VirtualKeyID: "vky_01",
		Plane:        ledger.PlaneA2A,
		Rate:         rate,
		Usage: pricing.UsageInput{
			FallbackTextBytes:    300,
			FallbackOutputBytes:  300,
			UpstreamUsagePresent: false,
		},
	})
	if err != nil || !setDirectDup.AlreadySettled || setDirectDup.SettlementID != setDirect.SettlementID {
		t.Fatalf("expected duplicate RequestID Settle replay, got %+v err=%v", setDirectDup, err)
	}
	if err := svcFn.MarkStreaming(ctx, "", "rsv"); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty orgID MarkStreaming")
	}
	if err := svcFn.MarkStreaming(ctx, "org_finops_01", "rsv_missing"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing MarkStreaming")
	}
	if err := svc.MarkStreaming(ctx, "org_finops_01", rsv.ID); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition on settled MarkStreaming, got %v", err)
	}
	if _, err := svcFn.DeltaReserve(ctx, ledger.DeltaReserveRequest{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty DeltaReserve")
	}
	if _, err := svcFn.DeltaReserve(ctx, ledger.DeltaReserveRequest{OrgID: "org_finops_01", ReservationID: "rsv_missing", NewHoldMicros: 10}); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing DeltaReserve")
	}
	if _, err := svcFn.Settle(ctx, ledger.SettleRequest{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty Settle")
	}
	if _, err := svcFn.Settle(ctx, ledger.SettleRequest{OrgID: "org_finops_01", RequestID: "r", Plane: "BAD"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for bad plane Settle")
	}
	if _, err := svcFn.Settle(ctx, ledger.SettleRequest{OrgID: "org_finops_01", RequestID: "r2", ReservationID: "rsv_missing"}); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing reservation Settle")
	}
	if _, err := svcFn.Settle(ctx, ledger.SettleRequest{OrgID: "org_finops_01", RequestID: "r3"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput when ReservationID and project/env/key omitted")
	}
}

func TestReserveScopeMismatchAndLateSettlementAfterReclaim(t *testing.T) {
	ctx := context.Background()
	const baseMs = 1_700_000_000_000
	_, svc, clock, auditSink, _ := openLedgerFixture(t, baseMs)

	bdg, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         "org_finops_01",
		PeriodKind:      ledger.PeriodTotalLifetime,
		PeriodStartMs:   baseMs - 10_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: 20_000,
	})
	if err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}

	// 1. Reserve duplicate (org_id, request_id, hold_micros) MUST reject mismatched project/env/vkey scopes.
	orig, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_01",
		RequestID:    "req_scope_check",
		HoldMicros:   1_000,
	})
	if err != nil {
		t.Fatalf("Reserve orig: %v", err)
	}
	for _, mismatch := range []ledger.ReserveRequest{
		{OrgID: "org_finops_01", ProjectID: "prj_OTHER", EnvID: "env_01", VirtualKeyID: "vky_01", RequestID: "req_scope_check", HoldMicros: 1_000},
		{OrgID: "org_finops_01", ProjectID: "prj_01", EnvID: "env_OTHER", VirtualKeyID: "vky_01", RequestID: "req_scope_check", HoldMicros: 1_000},
		{OrgID: "org_finops_01", ProjectID: "prj_01", EnvID: "env_01", VirtualKeyID: "vky_OTHER", RequestID: "req_scope_check", HoldMicros: 1_000},
	} {
		if _, err := svc.Reserve(ctx, mismatch); !errors.Is(err, ledger.ErrInvalidStateTransition) {
			t.Fatalf("expected ErrInvalidStateTransition for mismatched Reserve scope %+v, got %v", mismatch, err)
		}
	}

	// Settle orig cleanly so only subsequent reservation remains open.
	rate := pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000, OutputPer1MNanoUSD: 1_000_000_000, BillingMode: pricing.BillingModeCommercialAPI}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:         "org_finops_01",
		ReservationID: orig.ID,
		RequestID:     "req_scope_check",
		Rate:          rate,
		Usage:         pricing.UsageInput{InputTokens: 100, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
	}); err != nil {
		t.Fatalf("Settle orig: %v", err)
	}

	// 2. Reservation expires and gets reaped to EXPIRED_RECLAIMED; late Settle MUST bill accurately,
	// emit BUDGET_LATE_SETTLEMENT_AFTER_RECLAIM, preserve EXPIRED_RECLAIMED state, and stay idempotent on retry.
	lateRsv, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_01",
		RequestID:    "req_late_after_reclaim",
		HoldMicros:   2_000,
	})
	if err != nil {
		t.Fatalf("Reserve lateRsv: %v", err)
	}
	reaper, err := ledger.NewOrphanReservationReaper(svc, 5*time.Millisecond, 50)
	if err != nil {
		t.Fatalf("NewOrphanReservationReaper: %v", err)
	}
	clock.Add(ledger.DefaultLeaseTTLMs + 10_000)
	reaped, err := reaper.ReapOnce(ctx)
	if err != nil || reaped != 1 {
		t.Fatalf("expected 1 reaped, got reaped=%d err=%v", reaped, err)
	}

	// Validate RequestID mismatch fails Settle cleanly.
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:         "org_finops_01",
		ReservationID: lateRsv.ID,
		RequestID:     "wrong_req_id",
		Rate:          rate,
		Usage:         pricing.UsageInput{InputTokens: 500, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
	}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for mismatched RequestID in Settle, got %v", err)
	}

	// Validate late settlement succeeds and bills 500 micro-USD without double-releasing hold.
	lateSet, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:         "org_finops_01",
		ReservationID: lateRsv.ID,
		RequestID:     "req_late_after_reclaim",
		Rate:          rate,
		Usage:         pricing.UsageInput{InputTokens: 200, OutputTokens: 300, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
	})
	if err != nil || lateSet.AlreadySettled || lateSet.TotalCostMicros != 500 {
		t.Fatalf("expected late Settle 500 micro-USD, got %+v err=%v", lateSet, err)
	}
	reloadedRsv, err := svc.GetReservation(ctx, "org_finops_01", lateRsv.ID)
	if err != nil || reloadedRsv.State != ledger.StateExpiredReclaimed {
		t.Fatalf("expected reservation state preserved as EXPIRED_RECLAIMED, got %+v err=%v", reloadedRsv, err)
	}
	reloadedLed, err := svc.GetLedger(ctx, "org_finops_01", bdg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedLed.SettledExactMicros != 600 || reloadedLed.ReservedHoldMicros != 0 {
		t.Fatalf("expected settled_exact=600 reserved=0 after late settle, got %+v", reloadedLed)
	}

	// Retry late Settle MUST replay idempotently without double billing.
	lateDup, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:         "org_finops_01",
		ReservationID: lateRsv.ID,
		RequestID:     "req_late_after_reclaim",
		Rate:          rate,
		Usage:         pricing.UsageInput{InputTokens: 200, OutputTokens: 300, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
	})
	if err != nil || !lateDup.AlreadySettled || lateDup.SettlementID != lateSet.SettlementID {
		t.Fatalf("expected idempotent replay on second late Settle, got %+v err=%v", lateDup, err)
	}
	events := auditSink.Events()
	foundLateAudit := false
	for _, ev := range events {
		if ev.EventType == "BUDGET_LATE_SETTLEMENT_AFTER_RECLAIM" && ev.TargetID == lateRsv.ID {
			foundLateAudit = true
			break
		}
	}
	if !foundLateAudit {
		t.Fatalf("expected BUDGET_LATE_SETTLEMENT_AFTER_RECLAIM audit event, got %+v", events)
	}

	// 3. LeaseTTLMs, zero hard-limit utilization, threshold normalization, invalid BillingMode, and input guards.
	if got := svc.LeaseTTLMs(); got != ledger.DefaultLeaseTTLMs {
		t.Fatalf("expected DefaultLeaseTTLMs=%d, got %d", ledger.DefaultLeaseTTLMs, got)
	}
	var nilSvc *ledger.Service
	if got := nilSvc.LeaseTTLMs(); got != ledger.DefaultLeaseTTLMs {
		t.Fatalf("expected nilSvc.LeaseTTLMs()=%d, got %d", ledger.DefaultLeaseTTLMs, got)
	}
	if _, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:                  "org_finops_01",
		ScopeType:              ledger.ScopeVirtualKey,
		ScopeID:                "vky_zero",
		PeriodKind:             ledger.PeriodMonthly,
		PeriodStartMs:          baseMs - 10_000,
		PeriodEndMs:            baseMs + 86_400_000,
		HardLimitMicros:        0,
		SoftAlertThresholdsPct: []int{-5, 80, 80, 150, 90},
	}); err != nil {
		t.Fatalf("UpsertLedger vkey zero limit: %v", err)
	}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_zero",
		RequestID:    "req_zero_limit",
		Plane:        ledger.PlaneProvider,
		Rate:         rate,
		Usage:        pricing.UsageInput{InputTokens: 100, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
	}); err != nil {
		t.Fatalf("Settle zero limit vkey: %v", err)
	}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_01",
		EnvID:        "env_01",
		VirtualKeyID: "vky_zero",
		RequestID:    "req_bad_billing",
		Rate:         pricing.RateCard{BillingMode: "CONSUMER_SUBSCRIPTION"},
	}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for bad BillingMode, got %v", err)
	}
	if err := svc.RenewLease(ctx, "", ""); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty RenewLease, got %v", err)
	}
	if _, err := svc.Release(ctx, "", ""); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty Release, got %v", err)
	}
}

func TestLedgerOverflowProtection(t *testing.T) {
	ctx := context.Background()
	const baseMs = 1_700_000_000_000
	st, svc, _, _, _ := openLedgerFixture(t, baseMs)

	led, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         "org_finops_01",
		PeriodKind:      ledger.PeriodTotalLifetime,
		PeriodStartMs:   baseMs - 10_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: math.MaxInt64,
	})
	if err != nil {
		t.Fatalf("UpsertLedger MaxInt64: %v", err)
	}

	r1, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_ov",
		EnvID:        "env_ov",
		VirtualKeyID: "vky_ov",
		RequestID:    "req_ov_1",
		HoldMicros:   math.MaxInt64 - 10,
	})
	if err != nil {
		t.Fatalf("Reserve near-MaxInt64 failed: %v", err)
	}

	// Second Reserve of 20 would overflow reserved_hold_micros (MaxInt64-10 + 20 > MaxInt64) -> MUST return ErrBudgetExceeded or ErrOverflow cleanly (never wrap negative).
	if _, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_ov",
		EnvID:        "env_ov",
		VirtualKeyID: "vky_ov",
		RequestID:    "req_ov_2",
		HoldMicros:   20,
	}); !errors.Is(err, ledger.ErrOverflow) && !errors.Is(err, ledger.ErrBudgetExceeded) {
		t.Fatalf("expected ErrOverflow or ErrBudgetExceeded on near-MaxInt64 Reserve, got %v", err)
	}

	// DeltaReserve increasing hold to MaxInt64 succeeds; release restores 0 hold.
	if _, err := svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{
		OrgID:         "org_finops_01",
		ReservationID: r1.ID,
		NewHoldMicros: math.MaxInt64,
	}); err != nil {
		t.Fatalf("DeltaReserve to exact MaxInt64 failed: %v", err)
	}
	if _, err := svc.Release(ctx, "org_finops_01", r1.ID); err != nil {
		t.Fatalf("Release r1: %v", err)
	}

	// Pre-seed accumulated settled_exact_micros near MaxInt64, then verify Settle overflowing settled_exact_micros returns ErrOverflow.
	if _, err := st.RawDB().ExecContext(ctx, `
		UPDATE budget_ledgers SET settled_exact_micros = ? WHERE id = ?
	`, int64(math.MaxInt64-50), led.ID); err != nil {
		t.Fatalf("seed near-MaxInt64 settled_exact_micros: %v", err)
	}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:        "org_finops_01",
		ProjectID:    "prj_ov",
		EnvID:        "env_ov",
		VirtualKeyID: "vky_ov",
		RequestID:    "req_ov_settle_overflow",
		Plane:        ledger.PlaneProvider,
		Rate: pricing.RateCard{
			InputPer1MNanoUSD: 1_000_000_000, // 1 micro-USD/tok
			BillingMode:       pricing.BillingModeCommercialAPI,
		},
		Usage: pricing.UsageInput{
			InputTokens:          100, // 100 micro-USD + (MaxInt64-50) > MaxInt64
			UpstreamUsagePresent: true,
			UpstreamAccuracy:     pricing.AccuracyExact,
		},
	}); !errors.Is(err, ledger.ErrOverflow) {
		t.Fatalf("expected ErrOverflow when cumulative settled spend exceeds MaxInt64, got %v", err)
	}
}

type reentrantSink struct {
	svc    *ledger.Service
	events atomic.Int64
}

func (s *reentrantSink) EmitBudgetAlert(ctx context.Context, ev ledger.AlertEvent) {
	s.events.Add(1)
	if s.svc != nil {
		_, _ = s.svc.Reserve(ctx, ledger.ReserveRequest{
			OrgID:        ev.OrgID,
			ProjectID:    "prj_cb",
			EnvID:        "env_cb",
			VirtualKeyID: "vky_cb",
			RequestID:    fmt.Sprintf("req_cb_alert_%d", s.events.Load()),
			HoldMicros:   1,
		})
	}
}

func (s *reentrantSink) Append(ctx context.Context, ev storageport.AuditEvent) error {
	s.events.Add(1)
	if s.svc != nil && ev.OrgID != nil {
		_, _ = s.svc.GetReservation(ctx, *ev.OrgID, ev.TargetID)
	}
	return nil
}

func TestSinkReentrancyAndSettleScopeValidation(t *testing.T) {
	ctx := context.Background()
	var now atomic.Int64
	now.Store(1_700_000_000_000)

	dbPath := filepath.Join(t.TempDir(), "reentrant_ledger.db")
	sink := &reentrantSink{}
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath:    dbPath,
		NowMs:     now.Load,
		AuditSink: sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
		ID:             "org_reent",
		Slug:           "reent-acme",
		DisplayName:    "Reent Acme",
		MinPluginTrust: storageport.MinTrustOfficialOnly,
	}); err != nil {
		t.Fatal(err)
	}

	svc, err := ledger.NewService(ledger.Config{
		DB:         st.RawDB(),
		Audit:      sink,
		Alerts:     sink,
		NowMs:      now.Load,
		LeaseTTLMs: 1_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	sink.svc = svc

	_, err = svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:                  "org_reent",
		ScopeType:              ledger.ScopeProject,
		ScopeID:                "prj_1",
		PeriodKind:             ledger.PeriodDaily,
		PeriodStartMs:          now.Load() - 1_000,
		PeriodEndMs:            now.Load() + 86_400_000,
		HardLimitMicros:        1_000,
		SoftAlertThresholdsPct: []int{50, 80, 100},
	})
	if err != nil {
		t.Fatal(err)
	}

	r1, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_reent",
		ProjectID:    "prj_1",
		EnvID:        "env_1",
		VirtualKeyID: "vky_1",
		RequestID:    "req_reent_1",
		HoldMicros:   100,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mismatched non-empty scope IDs on Settle MUST fail with ErrInvalidInput without consuming reservation.
	for _, tc := range []ledger.SettleRequest{
		{OrgID: "org_reent", ReservationID: r1.ID, RequestID: "req_reent_1", ProjectID: "prj_WRONG", Rate: pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000}},
		{OrgID: "org_reent", ReservationID: r1.ID, RequestID: "req_reent_1", EnvID: "env_WRONG", Rate: pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000}},
		{OrgID: "org_reent", ReservationID: r1.ID, RequestID: "req_reent_1", VirtualKeyID: "vky_WRONG", Rate: pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000}},
	} {
		if _, err := svc.Settle(ctx, tc); !errors.Is(err, ledger.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for mismatched scope %+v, got %v", tc, err)
		}
	}

	// Settling >50% triggers synchronous AlertSink reentering svc.Reserve; MUST NOT deadlock.
	done := make(chan error, 1)
	go func() {
		_, sErr := svc.Settle(ctx, ledger.SettleRequest{
			OrgID:         "org_reent",
			ReservationID: r1.ID,
			RequestID:     "req_reent_1",
			Plane:         ledger.PlaneProvider,
			Rate:          pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000, BillingMode: pricing.BillingModeCommercialAPI},
			Usage:         pricing.UsageInput{InputTokens: 600, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
		})
		done <- sErr
	}()
	select {
	case sErr := <-done:
		if sErr != nil {
			t.Fatalf("Settle with reentrant AlertSink failed: %v", sErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Settle deadlocked while invoking reentrant AlertSink")
	}

	// ReapOnce with reentrant AuditSink calling svc.GetReservation MUST NOT deadlock.
	r2, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        "org_reent",
		ProjectID:    "prj_1",
		EnvID:        "env_1",
		VirtualKeyID: "vky_1",
		RequestID:    "req_reent_2",
		HoldMicros:   50,
	})
	if err != nil {
		t.Fatal(err)
	}
	now.Add(2_000)

	reaper, err := ledger.NewOrphanReservationReaper(svc, time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	reapDone := make(chan error, 1)
	go func() {
		_, rErr := reaper.ReapOnce(ctx)
		reapDone <- rErr
	}()
	select {
	case rErr := <-reapDone:
		if rErr != nil {
			t.Fatalf("ReapOnce with reentrant AuditSink failed: %v", rErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReapOnce deadlocked while invoking reentrant AuditSink")
	}

	// Late settle after reclaim also invokes reentrant AuditSink + AlertSink safely.
	lateDone := make(chan error, 1)
	go func() {
		_, lErr := svc.Settle(ctx, ledger.SettleRequest{
			OrgID:         "org_reent",
			ReservationID: r2.ID,
			RequestID:     "req_reent_2",
			Plane:         ledger.PlaneProvider,
			Rate:          pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000, BillingMode: pricing.BillingModeCommercialAPI},
			Usage:         pricing.UsageInput{InputTokens: 300, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
		})
		lateDone <- lErr
	}()
	select {
	case lErr := <-lateDone:
		if lErr != nil {
			t.Fatalf("late Settle with reentrant AuditSink+AlertSink failed: %v", lErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("late Settle deadlocked while invoking reentrant sinks")
	}
	if sink.events.Load() < 3 {
		t.Fatalf("expected >= 3 sink callbacks invoked, got %d", sink.events.Load())
	}
}

func TestLedgerValidationAndEdgePaths(t *testing.T) {
	ctx := context.Background()
	const baseMs = 1_700_000_000_000
	_, svc, _, _, _ := openLedgerFixture(t, baseMs)

	// Reaper invalid and default arg validation.
	if _, err := ledger.NewOrphanReservationReaper(nil, 0, 0); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil svc, got %v", err)
	}
	var nilReaper *ledger.OrphanReservationReaper
	if _, err := nilReaper.ReapOnce(ctx); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil reaper, got %v", err)
	}
	if _, err := ledger.NewOrphanReservationReaper(svc, 0, 0); err != nil {
		t.Fatalf("NewOrphanReservationReaper with defaults: %v", err)
	}

	// UpsertLedger invalid inputs + update path.
	for _, bad := range []ledger.UpsertLedgerParams{
		{OrgID: "", ScopeType: ledger.ScopeOrg, ScopeID: "o", PeriodKind: ledger.PeriodDaily, PeriodStartMs: 1, PeriodEndMs: 2},
		{OrgID: "org_finops_01", ScopeType: "BAD", ScopeID: "o", PeriodKind: ledger.PeriodDaily, PeriodStartMs: 1, PeriodEndMs: 2},
		{OrgID: "org_finops_01", ScopeType: ledger.ScopeOrg, ScopeID: "o", PeriodKind: "HOURLY", PeriodStartMs: 1, PeriodEndMs: 2},
		{OrgID: "org_finops_01", ScopeType: ledger.ScopeOrg, ScopeID: "o", PeriodKind: ledger.PeriodDaily, PeriodStartMs: 10, PeriodEndMs: 10},
		{OrgID: "org_finops_01", ScopeType: ledger.ScopeOrg, ScopeID: "o", PeriodKind: ledger.PeriodDaily, PeriodStartMs: 1, PeriodEndMs: 10, HardLimitMicros: -1},
	} {
		if _, err := svc.UpsertLedger(ctx, bad); !errors.Is(err, ledger.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for %+v, got %v", bad, err)
		}
	}

	led, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         "org_finops_01",
		PeriodKind:      ledger.PeriodDaily,
		PeriodStartMs:   baseMs - 1_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: 5_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Update limit upwards on existing row.
	updated, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           "org_finops_01",
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         "org_finops_01",
		PeriodKind:      ledger.PeriodDaily,
		PeriodStartMs:   baseMs - 1_000,
		PeriodEndMs:     baseMs + 86_400_000,
		HardLimitMicros: 10_000,
	})
	if err != nil || updated.ID != led.ID || updated.HardLimitMicros != 10_000 {
		t.Fatalf("expected updated limit 10000 on same ledger, got %+v err=%v", updated, err)
	}

	// Settle error guards: not-found reservation, missing scopes without reservation, and settling released reservation.
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID: "org_finops_01", ReservationID: "rsv_missing", RequestID: "req_miss",
		Rate: pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000},
	}); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing reservation, got %v", err)
	}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID: "org_finops_01", RequestID: "req_no_scopes",
		Rate: pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000},
	}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for Settle missing scope IDs, got %v", err)
	}
	rRel, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID: "org_finops_01", ProjectID: "p1", EnvID: "e1", VirtualKeyID: "k1",
		RequestID: "req_rel_settle", HoldMicros: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Release(ctx, "org_finops_01", rRel.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID: "org_finops_01", ReservationID: rRel.ID, RequestID: "req_rel_settle",
		Rate: pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000},
	}); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition settling released reservation, got %v", err)
	}

	// BudgetLedger.AvailableBalance and AlertError methods.
	b := ledger.BudgetLedger{HardLimitMicros: 100, SettledExactMicros: 40, ReservedHoldMicros: 30}
	if b.AvailableBalance() != 30 {
		t.Fatalf("expected AvailableBalance=30, got %d", b.AvailableBalance())
	}
	bZero := ledger.BudgetLedger{HardLimitMicros: 100, SettledExactMicros: 150}
	if bZero.AvailableBalance() != -50 {
		t.Fatalf("expected AvailableBalance=-50 when overspent, got %d", bZero.AvailableBalance())
	}
	if _, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID: "org_finops_01", ScopeType: ledger.ScopeOrg, ScopeID: "org_finops_01",
		PeriodKind: ledger.PeriodDaily, PeriodStartMs: baseMs - 1_000, PeriodEndMs: baseMs + 86_400_000,
		HardLimitMicros: 8_000, SoftAlertThresholdsPct: []int{25, 75, 100},
	}); err != nil {
		t.Fatalf("UpsertLedger update thresholds: %v", err)
	}
	if _, err := svc.GetLedger(ctx, "org_finops_01", "bdg_missing"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for GetLedger, got %v", err)
	}
	if _, err := svc.GetReservation(ctx, "org_finops_01", "rsv_missing"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for GetReservation, got %v", err)
	}
	if _, err := svc.GetLedger(ctx, "", ""); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty GetLedger, got %v", err)
	}
	if _, err := svc.GetReservation(ctx, "", ""); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty GetReservation, got %v", err)
	}
	if _, err := ledger.NewService(ledger.Config{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for NewService nil DB, got %v", err)
	}
	if err := svc.MarkStreaming(ctx, "", ""); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty MarkStreaming, got %v", err)
	}
	if err := svc.MarkStreaming(ctx, "org_finops_01", "rsv_missing"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for MarkStreaming, got %v", err)
	}
	if err := svc.MarkStreaming(ctx, "org_finops_01", rRel.ID); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition for MarkStreaming on released reservation, got %v", err)
	}
	if err := svc.RenewLease(ctx, "org_finops_01", "rsv_missing"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for RenewLease, got %v", err)
	}
	if err := svc.RenewLease(ctx, "org_finops_01", rRel.ID); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition for RenewLease on released reservation, got %v", err)
	}
	if _, err := svc.Release(ctx, "org_finops_01", "rsv_missing"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for Release, got %v", err)
	}
	if _, err := svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty DeltaReserve, got %v", err)
	}
	if _, err := svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{OrgID: "org_finops_01", ReservationID: rRel.ID, NewHoldMicros: 200}); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition for DeltaReserve on released reservation, got %v", err)
	}
	rHold, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID: "org_finops_01", ProjectID: "p1", EnvID: "e1", VirtualKeyID: "k1",
		RequestID: "req_delta_shrink", HoldMicros: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{OrgID: "org_finops_01", ReservationID: rHold.ID, NewHoldMicros: 150}); err != nil || got.HoldMicros != 200 {
		t.Fatalf("expected DeltaReserve with smaller NewHoldMicros to preserve HoldMicros=200 and renew lease, got %+v err=%v", got, err)
	}
	if err := svc.MarkStreaming(ctx, "org_finops_01", rHold.ID); err != nil {
		t.Fatal(err)
	}
	// Idempotent second MarkStreaming renews lease cleanly.
	if err := svc.MarkStreaming(ctx, "org_finops_01", rHold.ID); err != nil {
		t.Fatal(err)
	}
	// Duplicate Reserve with identical request_id + hold replays idempotently; differing hold returns ErrInvalidStateTransition.
	rDup, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID: "org_finops_01", ProjectID: "p1", EnvID: "e1", VirtualKeyID: "k1",
		RequestID: "req_dup_check", HoldMicros: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	rReplay, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID: "org_finops_01", ProjectID: "p1", EnvID: "e1", VirtualKeyID: "k1",
		RequestID: "req_dup_check", HoldMicros: 200,
	})
	if err != nil || rReplay.ID != rDup.ID {
		t.Fatalf("expected idempotent Reserve replay, got %+v err=%v", rReplay, err)
	}
	if _, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID: "org_finops_01", ProjectID: "p1", EnvID: "e1", VirtualKeyID: "k1",
		RequestID: "req_dup_check", HoldMicros: 300,
	}); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition on conflicting duplicate Reserve, got %v", err)
	}
	if _, err := svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{
		OrgID: "org_finops_01", ReservationID: rDup.ID, NewHoldMicros: 99_999_999,
	}); !errors.Is(err, ledger.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded on huge DeltaReserve, got %v", err)
	} else if !strings.Contains(err.Error(), "project_budget_exceeded") {
		t.Fatalf("unexpected BudgetExceededError string: %q", err.Error())
	}
	if err := svc.RenewLease(ctx, "org_finops_01", rHold.ID); err != nil {
		t.Fatalf("RenewLease active streaming reservation: %v", err)
	}
	if rel1, err := svc.Release(ctx, "org_finops_01", rHold.ID); err != nil || rel1 {
		t.Fatalf("expected first Release on streaming hold to return alreadyReleased=false, got %v err=%v", rel1, err)
	}
	if rel2, err := svc.Release(ctx, "org_finops_01", rHold.ID); err != nil || !rel2 {
		t.Fatalf("expected second Release to return alreadyReleased=true, got %v err=%v", rel2, err)
	}
	if _, err := svc.Reserve(ctx, ledger.ReserveRequest{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on empty Reserve, got %v", err)
	}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on empty Settle, got %v", err)
	}
	rSet, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID: "org_finops_01", ProjectID: "p1", EnvID: "e1", VirtualKeyID: "k1",
		RequestID: "req_rel_after_settle", HoldMicros: 50, LeaseTTLMs: math.MaxInt64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID: "org_finops_01", ReservationID: rSet.ID, RequestID: "req_rel_after_settle",
		Rate: pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000, BillingMode: pricing.BillingModeCommercialAPI},
		Usage: pricing.UsageInput{InputTokens: 10, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Release(ctx, "org_finops_01", rSet.ID); !errors.Is(err, ledger.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition on Release after Settle, got %v", err)
	}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID: "org_finops_01", ReservationID: rSet.ID, RequestID: "req_rel_after_settle",
		Rate: pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000, BillingMode: pricing.BillingModeCommercialAPI},
		Usage: pricing.UsageInput{InputTokens: 10, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyEstimated},
	}); err != nil {
		t.Fatal(err)
	}
	// Zero-hold reservation Release hits HeldMicros == 0 branch; huge HardLimit Settle exercises >9e15 utilization path.
	rZero, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID: "org_finops_01", ProjectID: "p1", EnvID: "e1", VirtualKeyID: "k1",
		RequestID: "req_zero_hold_rel", HoldMicros: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Release(ctx, "org_finops_01", rZero.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID: "org_finops_01", ScopeType: ledger.ScopeProject, ScopeID: "prj_huge",
		PeriodKind: ledger.PeriodTotalLifetime, PeriodStartMs: 0, PeriodEndMs: baseMs + 86_400_000,
		HardLimitMicros: math.MaxInt64,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID: "org_finops_01", ProjectID: "prj_huge", EnvID: "e_h", VirtualKeyID: "k_h",
		RequestID: "req_huge_util", Plane: ledger.PlaneProvider,
		Rate:  pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000_000_000, BillingMode: pricing.BillingModeCommercialAPI},
		Usage: pricing.UsageInput{InputTokens: 20_000_000, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
	}); err != nil {
		t.Fatal(err)
	}
	// Reserve on overspent ledger hits safeAvailableBalance clamp to 0 and BudgetExceededError.Unwrap.
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID: "org_finops_01", ProjectID: "p1", EnvID: "e1", VirtualKeyID: "k1",
		RequestID: "req_overspend_ledger", Plane: ledger.PlaneProvider,
		Rate:  pricing.RateCard{InputPer1MNanoUSD: 1_000_000_000, BillingMode: pricing.BillingModeCommercialAPI},
		Usage: pricing.UsageInput{InputTokens: 20_000, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID: "org_finops_01", ProjectID: "p1", EnvID: "e1", VirtualKeyID: "k1",
		RequestID: "req_blocked_overspent", HoldMicros: 10,
	}); !errors.Is(err, ledger.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded after overspend, got %v", err)
	} else {
		var be *ledger.BudgetExceededError
		if !errors.As(err, &be) || !errors.Is(be.Unwrap(), ledger.ErrBudgetExceeded) || be.AvailableMicros >= 0 {
			t.Fatalf("expected BudgetExceededError with negative AvailableMicros, got %+v", be)
		}
	}
	// UpsertLedger with all-out-of-range thresholds normalizes to default [50,80,100],
	// and Reaper.ReapOnce with canceled context returns context error.
	defThresh, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID: "org_finops_01", ScopeType: ledger.ScopeEnv, ScopeID: "env_thresh",
		PeriodKind: ledger.PeriodMonthly, PeriodStartMs: 0, PeriodEndMs: baseMs + 86_400_000,
		HardLimitMicros: 1_000, SoftAlertThresholdsPct: []int{-1, 0, 101},
	})
	if err != nil || len(defThresh.SoftAlertThresholdsPct) != 3 {
		t.Fatalf("expected default thresholds [50,80,100], got %+v err=%v", defThresh, err)
	}
	reap, err := ledger.NewOrphanReservationReaper(svc, time.Millisecond, 5)
	if err != nil {
		t.Fatal(err)
	}
	cctx, ccancel := context.WithCancel(ctx)
	ccancel()
	if _, err := reap.ReapOnce(cctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from ReapOnce, got %v", err)
	}
}

