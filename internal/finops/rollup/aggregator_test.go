// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package rollup_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/finops/rollup"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func TestHourlyRollupSegregationAndIdempotency(t *testing.T) {
	ctx := context.Background()
	clock := &atomic.Int64{}
	const hourStart int64 = (1_700_000_000_000 / rollup.HourMs) * rollup.HourMs
	clock.Store(hourStart + 10_000)

	dbPath := filepath.Join(t.TempDir(), "rollup.db")
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  clock.Load,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()

	_, err = st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
		ID:             "org_rollup_01",
		Slug:           "rollup-org",
		DisplayName:    "Rollup Org",
		MinPluginTrust: storageport.MinTrustOfficialOnly,
	})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	svc, err := ledger.NewService(ledger.Config{
		DB:    st.RawDB(),
		NowMs: clock.Load,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	agg, err := rollup.NewAggregator(st.RawDB(), clock.Load)
	if err != nil {
		t.Fatalf("NewAggregator: %v", err)
	}

	rate := pricing.RateCard{
		InputPer1MNanoUSD:  1_000_000_000,
		OutputPer1MNanoUSD: 2_000_000_000,
		BillingMode:        pricing.BillingModeCommercialAPI,
	}

	// 1 EXACT settlement -> 100*1 + 50*2 = 200 micro-USD
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:            "org_rollup_01",
		RequestID:        "req_r_1",
		ProjectID:        "prj_1",
		EnvID:            "env_1",
		VirtualKeyID:     "vky_1",
		LogicalModelSlug: "gpt-4o",
		Rate:             rate,
		Usage: pricing.UsageInput{
			InputTokens:          100,
			OutputTokens:         50,
			UpstreamUsagePresent: true,
			UpstreamAccuracy:     pricing.AccuracyExact,
		},
	}); err != nil {
		t.Fatalf("settle 1: %v", err)
	}

	// 1 ESTIMATED settlement -> 30*1 + 10*2 = 50 micro-USD
	clock.Add(5_000)
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:            "org_rollup_01",
		RequestID:        "req_r_2",
		ProjectID:        "prj_1",
		EnvID:            "env_1",
		VirtualKeyID:     "vky_1",
		LogicalModelSlug: "gpt-4o",
		Rate:             rate,
		Usage: pricing.UsageInput{
			InputTokens:          30,
			OutputTokens:         10,
			UpstreamUsagePresent: false,
		},
	}); err != nil {
		t.Fatalf("settle 2: %v", err)
	}

	// 1 UNMETERED_FLAGGED settlement -> 0 micro-USD, unmetered_request_count = 1
	clock.Add(5_000)
	if _, err := svc.Settle(ctx, ledger.SettleRequest{
		OrgID:            "org_rollup_01",
		RequestID:        "req_r_3",
		ProjectID:        "prj_1",
		EnvID:            "env_1",
		VirtualKeyID:     "vky_1",
		LogicalModelSlug: "gpt-4o",
		Rate:             pricing.RateCard{BillingMode: pricing.BillingModeSelfHostedInfra},
		Usage: pricing.UsageInput{
			InputTokens:          20,
			OutputTokens:         10,
			UpstreamUsagePresent: true,
		},
	}); err != nil {
		t.Fatalf("settle 3: %v", err)
	}

	// Run aggregation twice to verify strict idempotency (no double-counting!).
	for pass := 0; pass < 2; pass++ {
		if _, err := agg.AggregateHour(ctx, hourStart); err != nil {
			t.Fatalf("AggregateHour pass %d: %v", pass, err)
		}
	}

	rows, err := agg.QueryOrgRollups(ctx, "org_rollup_01", hourStart, hourStart+rollup.HourMs)
	if err != nil {
		t.Fatalf("QueryOrgRollups: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 rollup row, got %d (%+v)", len(rows), rows)
	}
	r := rows[0]
	if r.RequestCount != 3 || r.ExactMicroUSD != 200 || r.EstimatedMicroUSD != 50 || r.UnmeteredRequestCount != 1 || r.InputTokensSum != 150 || r.OutputTokensSum != 70 {
		t.Fatalf("unexpected segregated hourly rollup totals: %+v", r)
	}
}
