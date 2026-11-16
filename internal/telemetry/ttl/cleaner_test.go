// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package ttl_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/telemetry/ttl"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestTTLCleanerMicroBatchPurgeAndLifecycle(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	ctx := context.Background()
	fc := clock.NewFakeClock(time.UnixMilli(1760000000000))
	nowFn := func() int64 { return fc.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "ttl.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "ttl-org", DisplayName: "TTL Org"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	prj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "ttl-prj", DisplayName: "TTL Proj"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	db := st.RawDB()
	nowMs := nowFn()

	// Seed parent org/project DEK rows to satisfy debug_payload_blobs FK.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO org_encryption_keys (org_id, kek_key_id, nonce, wrapped_org_dek, dek_version, key_generation, created_at_ms, rotated_at_ms)
		VALUES (?, 'kek_v1', X'0304', X'0102', 1, 1, ?, ?)`, org.ID, nowMs, nowMs); err != nil {
		t.Fatalf("insert org_encryption_keys: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_encryption_keys (org_id, project_id, nonce, wrapped_project_dek, dek_version, key_generation, created_at_ms, rotated_at_ms)
		VALUES (?, ?, X'0304', X'0102', 1, 1, ?, ?)`, org.ID, prj.ID, nowMs, nowMs); err != nil {
		t.Fatalf("insert project_encryption_keys: %v", err)
	}

	// Validation check on TraceRetentionDays bounds (7..90 days).
	if _, err := ttl.NewCleaner(ttl.Config{DB: db, TraceRetentionDays: 3}); err == nil {
		t.Fatalf("expected error for retention < 7 days")
	}
	if _, err := ttl.NewCleaner(ttl.Config{DB: db, TraceRetentionDays: 120}); err == nil {
		t.Fatalf("expected error for retention > 90 days")
	}

	cleaner, err := ttl.NewCleaner(ttl.Config{
		DB:                 db,
		SweepInterval:      10 * time.Millisecond,
		TraceRetentionDays: 30,
		BatchSize:          2,
		NowMs:              nowFn,
	})
	if err != nil {
		t.Fatalf("NewCleaner: %v", err)
	}

	// Insert 5 expired blobs and 2 valid future blobs (within 72h CHECK cap).
	for i := 0; i < 5; i++ {
		created := nowMs - int64(2*time.Hour/time.Millisecond)
		expires := nowMs - int64(i)*1000 // expired
		reqID := "req_expired_" + string(rune('a'+i))
		if _, err := db.ExecContext(ctx, `
			INSERT INTO debug_payload_blobs (request_id, org_id, project_id, aead_alg, nonce, encrypted_payload_aead, created_at_ms, expires_at_ms)
			VALUES (?, ?, ?, 'AES-256-GCM', X'0102030405060708090a0b0c', X'abcdef', ?, ?)`,
			reqID, org.ID, prj.ID, created, expires); err != nil {
			t.Fatalf("insert expired blob %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		created := nowMs
		expires := nowMs + int64(time.Hour/time.Millisecond)
		reqID := "req_active_" + string(rune('a'+i))
		if _, err := db.ExecContext(ctx, `
			INSERT INTO debug_payload_blobs (request_id, org_id, project_id, aead_alg, nonce, encrypted_payload_aead, created_at_ms, expires_at_ms)
			VALUES (?, ?, ?, 'AES-256-GCM', X'0102030405060708090a0b0c', X'abcdef', ?, ?)`,
			reqID, org.ID, prj.ID, created, expires); err != nil {
			t.Fatalf("insert active blob %d: %v", i, err)
		}
	}

	// Insert 3 aged trace metadata rows (31 days old) and 1 recent row (1 day old).
	dayMs := int64(24 * time.Hour / time.Millisecond)
	for i := 0; i < 3; i++ {
		ts := nowMs - 31*dayMs - int64(i)*1000
		if _, err := db.ExecContext(ctx, `
			INSERT INTO request_traces_meta (request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, entry_point, http_status, proxy_overhead_us, total_duration_ms, waterfall_stages_json)
			VALUES (?, ?, ?, ?, ?, 'env_1', 'unified_http', 200, 120, 45, '[]')`,
			"req_old_"+string(rune('a'+i)), ts, ts, org.ID, prj.ID); err != nil {
			t.Fatalf("insert aged trace %d: %v", i, err)
		}
	}
	recentTs := nowMs - dayMs
	if _, err := db.ExecContext(ctx, `
		INSERT INTO request_traces_meta (request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, entry_point, http_status, proxy_overhead_us, total_duration_ms, waterfall_stages_json)
		VALUES ('req_recent', ?, ?, ?, ?, 'env_1', 'native_http', 200, 90, 22, '[]')`,
		recentTs, recentTs, org.ID, prj.ID); err != nil {
		t.Fatalf("insert recent trace: %v", err)
	}

	sum, err := cleaner.PurgeOnce(ctx)
	if err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if sum.DeletedDebugBlobs != 5 || sum.DeletedTracesMeta != 3 {
		t.Fatalf("unexpected purge summary: %+v", sum)
	}

	var remBlobs, remTraces int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM debug_payload_blobs`).Scan(&remBlobs); err != nil {
		t.Fatalf("count blobs: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM request_traces_meta`).Scan(&remTraces); err != nil {
		t.Fatalf("count traces: %v", err)
	}
	if remBlobs != 2 || remTraces != 1 {
		t.Fatalf("expected 2 blobs & 1 trace remaining, got blobs=%d traces=%d", remBlobs, remTraces)
	}

	// Start background loop, advance fake clock past future blob expiry, verify background purge & clean stop.
	stop := cleaner.Start(ctx)
	fc.Advance(2 * time.Hour)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = db.QueryRowContext(ctx, `SELECT count(*) FROM debug_payload_blobs`).Scan(&remBlobs)
		if remBlobs == 0 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	stop()

	if remBlobs != 0 {
		t.Fatalf("expected background TTL cleaner to purge expired blobs, %d remained", remBlobs)
	}
}
