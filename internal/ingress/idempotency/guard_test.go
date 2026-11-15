// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package idempotency_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestValidateKeyAndDigest(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	if _, err := idempotency.ValidateKey(""); !errors.Is(err, idempotency.ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey on empty key, got %v", err)
	}
	if _, err := idempotency.ValidateKey(" key-with-space"); !errors.Is(err, idempotency.ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey on whitespace, got %v", err)
	}
	if _, err := idempotency.ValidateKey("bad\nnewline"); !errors.Is(err, idempotency.ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey on control char, got %v", err)
	}
	if _, err := idempotency.ValidateKey(string(make([]byte, idempotency.MaxKeyLength+1))); !errors.Is(err, idempotency.ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey on >128 char key, got %v", err)
	}
	k, err := idempotency.ValidateKey("req-123_abc:XYZ")
	if err != nil || k != "req-123_abc:XYZ" {
		t.Fatalf("expected valid key acceptance, got %q err=%v", k, err)
	}

	d1 := idempotency.ComputePayloadSHA256("POST", "/v1/responses", []byte(`{"model":"a"}`))
	d2 := idempotency.ComputePayloadSHA256("POST", "/v1/embeddings", []byte(`{"model":"a"}`))
	if len(d1) != 64 || d1 == d2 {
		t.Fatalf("expected route-bound 64-hex digests, got d1=%q d2=%q", d1, d2)
	}
}

func TestGuardMemoryAndSQLiteLifecycleAndIsolation(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760001000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	tmp := t.TempDir()
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "idem.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-a", DisplayName: "Org A"})
	if err != nil {
		t.Fatal(err)
	}
	projA, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "proj-a", DisplayName: "Proj A"})
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-b", DisplayName: "Org B"})
	if err != nil {
		t.Fatal(err)
	}
	projB, err := st.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "proj-b", DisplayName: "Proj B"})
	if err != nil {
		t.Fatal(err)
	}

	envA, err := st.Tenancy().CreateEnvironment(ctx, orgA.ID, projA.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	envB, err := st.Tenancy().CreateEnvironment(ctx, orgB.ID, projB.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}

	insertVK := func(id, orgID, projID, envID string, hash []byte) {
		if _, err := st.RawDB().ExecContext(ctx, `
			INSERT INTO virtual_keys (
				id, org_id, project_id, env_id, name, key_prefix, last_four,
				active_key_hash, state, version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, 'k', 'vk_tp_test', '1234', ?, 'CREATED_ACTIVE', 1, ?, ?)
		`, id, orgID, projID, envID, hash, nowFn(), nowFn()); err != nil {
			t.Fatal(err)
		}
	}
	insertVK("vk_a1", orgA.ID, projA.ID, envA.ID, []byte("0123456789abcdef0123456789abcdef"))
	insertVK("vk_b1", orgB.ID, projB.ID, envB.ID, []byte("fedcba9876543210fedcba9876543210"))

	guard := idempotency.NewGuard(idempotency.Config{
		DB:    st.RawDB(),
		TTL:   24 * time.Hour,
		NowMs: nowFn,
	})

	digestA := idempotency.ComputePayloadSHA256("POST", "/v1/responses", []byte(`{"model":"gpt-4o","input":"hi"}`))
	digestModified := idempotency.ComputePayloadSHA256("POST", "/v1/responses", []byte(`{"model":"gpt-4o","input":"different"}`))

	scopeA := idempotency.ScopeKey{OrgID: orgA.ID, ProjectID: projA.ID, VirtualKeyID: "vk_a1", IdempotencyKey: "req-123"}
	scopeB := idempotency.ScopeKey{OrgID: orgB.ID, ProjectID: projB.ID, VirtualKeyID: "vk_b1", IdempotencyKey: "req-123"}

	// 1. Acquire initial claim for Org A.
	res1, err := guard.Begin(ctx, scopeA, digestA)
	if err != nil || res1.Outcome != idempotency.OutcomeAcquired || res1.Claim == nil {
		t.Fatalf("expected OutcomeAcquired for Org A, got %+v err=%v", res1, err)
	}

	// 2. Concurrent request while IN_FLIGHT -> OutcomeInFlight.
	resConcurrent, err := guard.Begin(ctx, scopeA, digestA)
	if err != nil || resConcurrent.Outcome != idempotency.OutcomeInFlight {
		t.Fatalf("expected OutcomeInFlight for concurrent request, got %+v err=%v", resConcurrent, err)
	}

	// 3. Same key + different body while IN_FLIGHT -> OutcomeMismatch.
	resMismatch, err := guard.Begin(ctx, scopeA, digestModified)
	if err != nil || resMismatch.Outcome != idempotency.OutcomeMismatch {
		t.Fatalf("expected OutcomeMismatch for modified body, got %+v err=%v", resMismatch, err)
	}

	// 4. NEG-3 Cross-tenant isolation: Org B using identical Idempotency-Key acquires cleanly.
	resB, err := guard.Begin(ctx, scopeB, digestModified)
	if err != nil || resB.Outcome != idempotency.OutcomeAcquired || resB.Claim == nil {
		t.Fatalf("expected Org B to acquire independently, got %+v err=%v", resB, err)
	}
	if err := resB.Claim.Complete(ctx, http.StatusOK, map[string]string{"X-Tenant": "B"}, []byte(`{"org":"B"}`)); err != nil {
		t.Fatal(err)
	}

	// 5. Complete Org A's claim and verify deterministic SQLite replay even from a fresh Guard instance.
	if err := res1.Claim.Complete(ctx, http.StatusOK, map[string]string{"X-Tenant": "A"}, []byte(`{"org":"A"}`)); err != nil {
		t.Fatal(err)
	}
	guardRestarted := idempotency.NewGuard(idempotency.Config{
		DB:    st.RawDB(),
		TTL:   24 * time.Hour,
		NowMs: nowFn,
	})
	replayA, err := guardRestarted.Begin(ctx, scopeA, digestA)
	if err != nil || replayA.Outcome != idempotency.OutcomeReplay || replayA.Replay == nil {
		t.Fatalf("expected OutcomeReplay across guard restart, got %+v err=%v", replayA, err)
	}
	if string(replayA.Replay.Body) != `{"org":"A"}` || replayA.Replay.Headers["X-Tenant"] != "A" {
		t.Fatalf("unexpected replayed Org A response: %+v", replayA.Replay)
	}
	replayB, err := guardRestarted.Begin(ctx, scopeB, digestModified)
	if err != nil || replayB.Outcome != idempotency.OutcomeReplay || string(replayB.Replay.Body) != `{"org":"B"}` {
		t.Fatalf("unexpected Org B replay: %+v err=%v", replayB, err)
	}

	// 6. TTL expiry allows fresh acquisition.
	clk.Advance(24*time.Hour + time.Second)
	expiredRetry, err := guardRestarted.Begin(ctx, scopeA, digestModified)
	if err != nil || expiredRetry.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("expected OutcomeAcquired after 24h TTL expiry, got %+v err=%v", expiredRetry, err)
	}
	expiredRetry.Claim.Abort(ctx)

	// After Abort of IN_FLIGHT claim, same key can be re-acquired cleanly.
	reacquired, err := guardRestarted.Begin(ctx, scopeA, digestModified)
	if err != nil || reacquired.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("expected OutcomeAcquired after Abort, got %+v err=%v", reacquired, err)
	}
}

func TestGuardBoundedMemoryEvictionAndBodyCap(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760002000000))
	guard := idempotency.NewGuard(idempotency.Config{
		TTL:                time.Hour,
		NowMs:              func() int64 { return clk.Now().UnixMilli() },
		MaxEntries:         2,
		MaxTotalBytes:      32,
		MaxRecordBodyBytes: 24,
	})

	d := idempotency.ComputePayloadSHA256("POST", "/v1/responses", []byte(`{"model":"a","input":"1"}`))
	s1 := idempotency.ScopeKey{OrgID: "o1", ProjectID: "p1", VirtualKeyID: "k1", IdempotencyKey: "k-1"}
	s2 := idempotency.ScopeKey{OrgID: "o1", ProjectID: "p1", VirtualKeyID: "k1", IdempotencyKey: "k-2"}
	s3 := idempotency.ScopeKey{OrgID: "o1", ProjectID: "p1", VirtualKeyID: "k1", IdempotencyKey: "k-3"}
	sBig := idempotency.ScopeKey{OrgID: "o1", ProjectID: "p1", VirtualKeyID: "k1", IdempotencyKey: "k-big"}

	r1, err := guard.Begin(ctx, s1, d)
	if err != nil || r1.Outcome != idempotency.OutcomeAcquired {
		t.Fatal(err, r1)
	}
	if err := r1.Claim.Complete(ctx, http.StatusOK, nil, []byte(`0123456789abcdef`)); err != nil {
		t.Fatal(err)
	}
	r2, err := guard.Begin(ctx, s2, d)
	if err != nil || r2.Outcome != idempotency.OutcomeAcquired {
		t.Fatal(err, r2)
	}
	if err := r2.Claim.Complete(ctx, http.StatusOK, nil, []byte(`fedcba9876543210`)); err != nil {
		t.Fatal(err)
	}

	// Adding s3 (total bytes > 32 & entries > 2) must evict oldest completed entry s1.
	r3, err := guard.Begin(ctx, s3, d)
	if err != nil || r3.Outcome != idempotency.OutcomeAcquired {
		t.Fatal(err, r3)
	}
	if err := r3.Claim.Complete(ctx, http.StatusOK, nil, []byte(`0123456789abcdef`)); err != nil {
		t.Fatal(err)
	}
	r1After, err := guard.Begin(ctx, s1, d)
	if err != nil || r1After.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("expected evicted s1 to require re-acquisition, got %+v err=%v", r1After, err)
	}
	r1After.Claim.Abort(ctx)

	// Response body > MaxRecordBodyBytes skips caching and releases IN_FLIGHT cleanly.
	rBig, err := guard.Begin(ctx, sBig, d)
	if err != nil || rBig.Outcome != idempotency.OutcomeAcquired {
		t.Fatal(err, rBig)
	}
	if err := rBig.Claim.Complete(ctx, http.StatusOK, nil, []byte("this-response-exceeds-24-bytes")); err != nil {
		t.Fatal(err)
	}
	rBigRetry, err := guard.Begin(ctx, sBig, d)
	if err != nil || rBigRetry.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("expected oversized response not to stick in cache/IN_FLIGHT, got %+v err=%v", rBigRetry, err)
	}
	rBigRetry.Claim.Abort(ctx)
}

func TestGuardSQLiteCompleteFailureRecoveryAndBatchPrune(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760003000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	tmp := t.TempDir()
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "idem_recover.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-rec", DisplayName: "Org Rec"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "proj-rec", DisplayName: "Proj Rec"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO virtual_keys (
			id, org_id, project_id, env_id, name, key_prefix, last_four,
			active_key_hash, state, version, created_at_ms, updated_at_ms
		) VALUES ('vk_rec', ?, ?, ?, 'k', 'vk_tp_test', '1234', ?, 'CREATED_ACTIVE', 1, ?, ?)
	`, org.ID, proj.ID, env.ID, []byte("0123456789abcdef0123456789abcdef"), nowFn(), nowFn()); err != nil {
		t.Fatal(err)
	}

	guard := idempotency.NewGuard(idempotency.Config{
		DB:    st.RawDB(),
		TTL:   time.Hour,
		NowMs: nowFn,
	})
	digest := idempotency.ComputePayloadSHA256("POST", "/v1/responses", []byte(`{"model":"gpt-4o","input":"hi"}`))
	scope := idempotency.ScopeKey{OrgID: org.ID, ProjectID: proj.ID, VirtualKeyID: "vk_rec", IdempotencyKey: "k-recover"}

	// 1. If Complete fails (e.g. canceled context), Abort must still clear IN_FLIGHT so retries do not wedge.
	claimRes, err := guard.Begin(ctx, scope, digest)
	if err != nil || claimRes.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("expected OutcomeAcquired, got %+v err=%v", claimRes, err)
	}
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := claimRes.Claim.Complete(canceledCtx, http.StatusOK, nil, []byte(`{"ok":true}`)); err == nil {
		t.Fatalf("expected error on Complete with canceled context")
	}
	claimRes.Claim.Abort(ctx)
	retryRes, err := guard.Begin(ctx, scope, digest)
	if err != nil || retryRes.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("expected retry OutcomeAcquired after failed Complete + Abort, got %+v err=%v", retryRes, err)
	}
	retryRes.Claim.Abort(ctx)

	// 2. Seed > 512 expired rows and verify consecutive Begin calls within < 60s drain the backlog.
	expiredAt := nowFn() - 1000
	tx, err := st.RawDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO idempotency_records (
			org_id, project_id, virtual_key_id, idempotency_key,
			payload_sha256, state, created_at_ms, expires_at_ms
		) VALUES (?, ?, 'vk_rec', ?, ?, 'COMPLETED', ?, ?)
	`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 650; i++ {
		if _, err := stmt.ExecContext(ctx, org.ID, proj.ID, fmt.Sprintf("exp-%04d", i), digest, expiredAt-1000, expiredAt); err != nil {
			t.Fatal(err)
		}
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	clk.Advance(61 * time.Second)
	b1, err := guard.Begin(ctx, idempotency.ScopeKey{OrgID: org.ID, ProjectID: proj.ID, VirtualKeyID: "vk_rec", IdempotencyKey: "k-drain-1"}, digest)
	if err != nil || b1.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("drain 1 failed: %+v err=%v", b1, err)
	}
	b1.Claim.Abort(ctx)
	b2, err := guard.Begin(ctx, idempotency.ScopeKey{OrgID: org.ID, ProjectID: proj.ID, VirtualKeyID: "vk_rec", IdempotencyKey: "k-drain-2"}, digest)
	if err != nil || b2.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("drain 2 failed: %+v err=%v", b2, err)
	}
	b2.Claim.Abort(ctx)

	var remainingExpired int
	if err := st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM idempotency_records WHERE expires_at_ms <= ?`, nowFn()).Scan(&remainingExpired); err != nil {
		t.Fatal(err)
	}
	if remainingExpired != 0 {
		t.Fatalf("expected consecutive Begin calls to drain >512 expired rows, still remaining=%d", remainingExpired)
	}
}


