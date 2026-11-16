// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package ledger_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

// FuzzLedgerInvariants verifies FZ-06 accounting conservation law across randomized
// sequences of Reserve, DeltaReserve, MarkStreaming, Settle, Release, ReapOnce, and TopUp:
//
//	Spent + ActiveHolds + max(0, AvailableBalance) == Limit + NegativeCarryover
//
// AND settled_exact + settled_estimated equals exact SUM(usage_settlements.total_cost_micros).
func FuzzLedgerInvariants(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 10, 20, 30, 40})
	f.Add([]byte{1, 1, 1, 1, 1, 3, 4, 2, 5, 0, 6})

	f.Fuzz(func(t *testing.T, ops []byte) {
		ctx := context.Background()
		clock := &atomic.Int64{}
		const startMs int64 = 1_700_000_000_000
		clock.Store(startMs)

		dbPath := filepath.Join(t.TempDir(), "fuzz_ledger.db")
		st, err := sqlite.Open(ctx, sqlite.Config{
			DBPath: dbPath,
			NowMs:  clock.Load,
		})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer st.Close()

		_, err = st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
			ID:             "org_fuzz",
			Slug:           "fuzz-org",
			DisplayName:    "Fuzz Org",
			MinPluginTrust: storageport.MinTrustOfficialOnly,
		})
		if err != nil {
			t.Fatalf("create org: %v", err)
		}

		svc, err := ledger.NewService(ledger.Config{
			DB:         st.RawDB(),
			NowMs:      clock.Load,
			LeaseTTLMs: 1_000,
		})
		if err != nil {
			t.Fatalf("NewService: %v", err)
		}
		reaper, err := ledger.NewOrphanReservationReaper(svc, 1, 50)
		if err != nil {
			t.Fatalf("NewOrphanReservationReaper: %v", err)
		}

		bdg, err := svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
			OrgID:           "org_fuzz",
			ScopeType:       ledger.ScopeProject,
			ScopeID:         "prj_fuzz",
			PeriodKind:      ledger.PeriodTotalLifetime,
			PeriodStartMs:   startMs - 1_000,
			PeriodEndMs:     startMs + 1_000_000_000,
			HardLimitMicros: 5_000,
		})
		if err != nil {
			t.Fatalf("UpsertLedger: %v", err)
		}

		var (
			activeIDs []string
			settledID []string
			seq       int
			curLimit  int64 = 5_000
		)

		rate := pricing.RateCard{
			InputPer1MNanoUSD: 1_000_000_000, // 1 micro-USD per token
			BillingMode:       pricing.BillingModeCommercialAPI,
		}

		for i := 0; i+1 < len(ops); i += 2 {
			action := ops[i] % 7
			param := int64(ops[i+1]) + 1
			seq++

			switch action {
			case 0, 1: // Reserve
				hold := param * 20
				rsv, rErr := svc.Reserve(ctx, ledger.ReserveRequest{
					OrgID:        "org_fuzz",
					ProjectID:    "prj_fuzz",
					EnvID:        "env_fuzz",
					VirtualKeyID: "vky_fuzz",
					RequestID:    fmt.Sprintf("req_%d", seq),
					HoldMicros:   hold,
				})
				if rErr == nil {
					activeIDs = append(activeIDs, rsv.ID)
				}
			case 2: // DeltaReserve or MarkStreaming on an active hold
				if len(activeIDs) > 0 {
					id := activeIDs[int(param)%len(activeIDs)]
					_, _ = svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{
						OrgID:         "org_fuzz",
						ReservationID: id,
						NewHoldMicros: param * 30,
					})
					_ = svc.MarkStreaming(ctx, "org_fuzz", id)
				}
			case 3: // Settle (plus duplicate Settle NEG-2 check)
				if len(activeIDs) > 0 {
					idx := int(param) % len(activeIDs)
					id := activeIDs[idx]
					activeIDs = append(activeIDs[:idx], activeIDs[idx+1:]...)

					rsv, _ := svc.GetReservation(ctx, "org_fuzz", id)
					acc := pricing.AccuracyExact
					var inTok int64 = param * 20
					if param%3 == 0 {
						acc = pricing.AccuracyEstimated
					}
					useRate := rate
					if param%7 == 0 {
						useRate = pricing.RateCard{BillingMode: pricing.BillingModeSelfHostedInfra}
						inTok = 0
					}
					s1, sErr := svc.Settle(ctx, ledger.SettleRequest{
						OrgID:         "org_fuzz",
						ReservationID: id,
						RequestID:     rsv.RequestID,
						Rate:          useRate,
						Usage: pricing.UsageInput{
							InputTokens:          inTok,
							UpstreamUsagePresent: true,
							UpstreamAccuracy:     acc,
						},
					})
					if sErr == nil {
						settledID = append(settledID, id)
						// Verify idempotent re-settle (NEG-2)
						s2, s2Err := svc.Settle(ctx, ledger.SettleRequest{
							OrgID:         "org_fuzz",
							ReservationID: id,
							RequestID:     rsv.RequestID,
							Rate:          useRate,
							Usage: pricing.UsageInput{
								InputTokens:          inTok,
								UpstreamUsagePresent: true,
								UpstreamAccuracy:     acc,
							},
						})
						if s2Err != nil || !s2.AlreadySettled || s2.SettlementID != s1.SettlementID {
							t.Fatalf("double Settle invariant broken: s1=%+v s2=%+v err=%v", s1, s2, s2Err)
						}
					}
				}
			case 4: // Release (plus duplicate Release NEG-2 check)
				if len(activeIDs) > 0 {
					idx := int(param) % len(activeIDs)
					id := activeIDs[idx]
					activeIDs = append(activeIDs[:idx], activeIDs[idx+1:]...)
					already1, r1Err := svc.Release(ctx, "org_fuzz", id)
					if r1Err == nil && !already1 {
						already2, r2Err := svc.Release(ctx, "org_fuzz", id)
						if r2Err != nil || !already2 {
							t.Fatalf("double Release invariant broken: already2=%v err=%v", already2, r2Err)
						}
					}
				}
			case 5: // Advance clock + OrphanReservationReaper
				clock.Add(param * 100)
				_, _ = reaper.ReapOnce(ctx)
				activeIDs = filterActiveReservations(ctx, svc, "org_fuzz", activeIDs)
			case 6: // Re-settle old or top-up limit
				if len(settledID) > 0 && param%2 == 0 {
					id := settledID[int(param)%len(settledID)]
					rsv, _ := svc.GetReservation(ctx, "org_fuzz", id)
					s2, s2Err := svc.Settle(ctx, ledger.SettleRequest{
						OrgID:         "org_fuzz",
						ReservationID: id,
						RequestID:     rsv.RequestID,
						Rate:          rate,
						Usage:         pricing.UsageInput{InputTokens: 1, UpstreamUsagePresent: true},
					})
					if s2Err != nil || !s2.AlreadySettled {
						t.Fatalf("re-settle failed: %+v %v", s2, s2Err)
					}
				} else {
					curLimit += param * 10
					_, _ = svc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
						OrgID:           "org_fuzz",
						ScopeType:       ledger.ScopeProject,
						ScopeID:         "prj_fuzz",
						PeriodKind:      ledger.PeriodTotalLifetime,
						PeriodStartMs:   startMs - 1_000,
						PeriodEndMs:     startMs + 1_000_000_000,
						HardLimitMicros: curLimit,
					})
				}
			}
		}

		// Final FZ-06 accounting law assertion:
		l, err := svc.GetLedger(ctx, "org_fuzz", bdg.ID)
		if err != nil {
			t.Fatalf("GetLedger: %v", err)
		}
		if l.ReservedHoldMicros < 0 || l.SettledExactMicros < 0 || l.SettledEstimatedMicros < 0 || l.NegativeCarryoverMicros < 0 {
			t.Fatalf("negative counter on ledger: %+v", l)
		}
		spent := l.SettledExactMicros + l.SettledEstimatedMicros
		availPos := l.AvailableBalance()
		if availPos < 0 {
			availPos = 0
		}
		lhs := spent + l.ReservedHoldMicros + availPos
		rhs := l.HardLimitMicros + l.NegativeCarryoverMicros
		if lhs != rhs {
			t.Fatalf("FZ-06 CONSERVATION VIOLATION: spent(%d)+holds(%d)+max(0,avail)(%d)=%d != limit(%d)+carryover(%d)=%d (ledger=%+v)",
				spent, l.ReservedHoldMicros, availPos, lhs, l.HardLimitMicros, l.NegativeCarryoverMicros, rhs, l)
		}

		// Cross-check ledger totals against raw immutable usage_settlements rows and active reservations!
		var sumSettlements, sumActiveHolds int64
		_ = st.RawDB().QueryRow(`SELECT COALESCE(SUM(total_cost_micros), 0) FROM usage_settlements WHERE org_id = 'org_fuzz'`).Scan(&sumSettlements)
		_ = st.RawDB().QueryRow(`SELECT COALESCE(SUM(hold_micros), 0) FROM budget_reservations WHERE org_id = 'org_fuzz' AND state IN ('RESERVED_HOLD','STREAMING_ACCRUING')`).Scan(&sumActiveHolds)
		if spent != sumSettlements {
			t.Fatalf("ledger spent %d != SUM(usage_settlements) %d", spent, sumSettlements)
		}
		if l.ReservedHoldMicros != sumActiveHolds {
			t.Fatalf("ledger reserved_hold_micros %d != SUM(active reservations) %d", l.ReservedHoldMicros, sumActiveHolds)
		}
	})
}

func filterActiveReservations(ctx context.Context, svc *ledger.Service, orgID string, ids []string) []string {
	out := ids[:0]
	for _, id := range ids {
		r, err := svc.GetReservation(ctx, orgID, id)
		if err == nil && (r.State == ledger.StateReservedHold || r.State == ledger.StateStreamingAccruing) {
			out = append(out, id)
		}
	}
	return out
}
