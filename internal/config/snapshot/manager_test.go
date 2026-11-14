// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package snapshot

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func openTestSQLite(t *testing.T, fk *clock.FakeClock) *sqlite.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "snapshot.db")
	st, err := sqlite.Open(context.Background(), sqlite.Config{
		DBPath: dbPath,
		NowMs: func() int64 {
			return fk.Now().UTC().UnixMilli()
		},
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func seedTenantAndKey(t *testing.T, st *sqlite.Store, fk *clock.FakeClock, rawKey, envSlug, state string) (orgID, projID, envID, keyID string) {
	t.Helper()
	ctx := context.Background()
	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		Slug:        "org-" + envSlug + "-" + state,
		DisplayName: "Org " + envSlug,
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{
		Slug:        "proj-" + envSlug,
		DisplayName: "Project " + envSlug,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, port.CreateEnvironmentParams{
		Slug: envSlug,
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	keyID = "vky_01jabcdef01234567890123456"
	if envSlug == "prod" {
		keyID = "vky_01jabcdef01234567890123457"
	}
	sum := sha256.Sum256([]byte(rawKey))
	nowMs := fk.Now().UTC().UnixMilli()
	_, err = st.RawDB().ExecContext(ctx, `
		INSERT INTO virtual_keys (
			id, org_id, project_id, env_id, owner_principal_id, name,
			key_prefix, last_four, active_key_hash, previous_key_hash, grace_expires_at_ms,
			state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			rpm_limit, tpm_limit, max_concurrent_streams, is_ephemeral_playground,
			expires_at_ms, revoked_at_ms, revocation_mode, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, NULL, NULL, ?, 'PROVIDER', '["gpt-4o"]', '["*"]', '[]', 60, 20000, 10, 0, NULL, NULL, NULL, 1, ?, ?)`,
		keyID, org.ID, proj.ID, env.ID, "Key "+envSlug, "vk_tp_"+envSlug+"_", rawKey[len(rawKey)-4:], sum[:], state, nowMs, nowMs,
	)
	if err != nil {
		t.Fatalf("insert virtual_key: %v", err)
	}
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID:        &org.ID,
		EntityKind:   "virtual_key",
		EntityID:     keyID,
		MutationType: port.MutationUpsert,
		CreatedAtMs:  nowMs,
	}); err != nil {
		t.Fatalf("Changelog.Append: %v", err)
	}
	return org.ID, proj.ID, env.ID, keyID
}

func TestConfigSnapshotLookupAndEvaluation(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openTestSQLite(t, fk)

	rawKey := "vk_tp_staging_0123456789abcdefABCDEF01234567"
	orgID, projID, envID, keyID := seedTenantAndKey(t, st, fk, rawKey, "staging", KeyStateCreatedActive)

	mgr, err := NewManager(context.Background(), ManagerConfig{
		DB: st.RawDB(),
		NowMs: func() int64 {
			return fk.Now().UTC().UnixMilli()
		},
		MaxStaleMs: 10_000,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	snap := mgr.Current()
	if snap == nil || snap.Epoch == 0 {
		t.Fatalf("expected non-empty snapshot with epoch > 0, got %+v", snap)
	}
	if mgr.IsStale() {
		t.Fatalf("expected fresh snapshot manager not to be stale")
	}

	// Matching env succeeds.
	res, err := snap.AuthenticateKey(rawKey, envID, fk.Now().UTC().UnixMilli())
	if err != nil {
		t.Fatalf("AuthenticateKey matching env: %v", err)
	}
	if res.Key.ID != keyID || res.Config.OrgID != orgID || res.Config.ProjectID != projID {
		t.Fatalf("unexpected match result: %+v", res)
	}

	// Cross-env fails closed with ErrEnvironmentScopeDenied (AUTH-04).
	if _, err := snap.AuthenticateKey(rawKey, "env_prod_other", fk.Now().UTC().UnixMilli()); !errors.Is(err, ErrEnvironmentScopeDenied) {
		t.Fatalf("expected ErrEnvironmentScopeDenied, got %v", err)
	}

	// Advance clock past 10s without polling -> IsStale must trip true (ADR-0006).
	fk.Advance(11 * time.Second)
	if !mgr.IsStale() {
		t.Fatalf("expected manager to report stale after 11s without poll")
	}
}

func TestConfigSnapshotEmergencyRevocationAbortsStreams(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openTestSQLite(t, fk)
	ctx := context.Background()

	rawKey := "vk_tp_live_0123456789abcdefABCDEF01234568"
	orgID, projID, _, keyID := seedTenantAndKey(t, st, fk, rawKey, "live", KeyStateCreatedActive)

	mgr, err := NewManager(ctx, ManagerConfig{
		DB: st.RawDB(),
		NowMs: func() int64 {
			return fk.Now().UTC().UnixMilli()
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mgr.StartWatcher(watchCtx, 50*time.Millisecond)
	}()

	// Register 10 active in-flight SSE streams (AUTH-06).
	streamCtxs := make([]context.Context, 10)
	for i := 0; i < 10; i++ {
		sctx, scancel := context.WithCancel(ctx)
		streamCtxs[i] = sctx
		mgr.Streams().Register(orgID, projID, keyID, scancel)
	}
	if got := mgr.Streams().ActiveCount(); got != 10 {
		t.Fatalf("expected 10 active streams, got %d", got)
	}

	nowMs := fk.Now().UTC().UnixMilli()
	if _, err := st.RawDB().ExecContext(ctx,
		`UPDATE virtual_keys SET state = ?, revoked_at_ms = ?, revocation_mode = 'EMERGENCY_SECURITY', version = version + 1 WHERE org_id = ? AND id = ?`,
		KeyStateRevoked, nowMs, orgID, keyID,
	); err != nil {
		t.Fatalf("revoke update: %v", err)
	}
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID:        &orgID,
		EntityKind:   "virtual_key",
		EntityID:     keyID,
		MutationType: port.MutationRevoke,
		Emergency:    true,
		CreatedAtMs:  nowMs,
	}); err != nil {
		t.Fatalf("Changelog.Append revoke: %v", err)
	}

	mgr.Notify()

	deadline := time.Now().Add(900 * time.Millisecond)
	for i, sctx := range streamCtxs {
		for sctx.Err() == nil && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if sctx.Err() == nil {
			t.Fatalf("stream %d was not aborted within 900ms", i)
		}
	}

	if mgr.Streams().ActiveCount() != 0 {
		t.Fatalf("expected 0 active streams remaining, got %d", mgr.Streams().ActiveCount())
	}
	if _, err := mgr.Current().AuthenticateKey(rawKey, "", nowMs); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("expected ErrKeyRevoked after swap, got %v", err)
	}

	cancelWatch()
	wg.Wait()
}

func TestConfigSnapshotStatesShredScopesAndErrorGuards(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openTestSQLite(t, fk)
	ctx := context.Background()

	if _, err := NewManager(ctx, ManagerConfig{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("nil DB expected ErrInvalidInput, got %v", err)
	}
	var nilSnap *ConfigSnapshot
	if _, err := nilSnap.AuthenticateKey("k", "", 0); !errors.Is(err, ErrSnapshotNotLoaded) {
		t.Fatalf("nil snapshot expected ErrSnapshotNotLoaded, got %v", err)
	}
	var nilMgr *Manager
	if !nilMgr.IsStale() || nilMgr.Current() != nil || nilMgr.Streams() != nil {
		t.Fatalf("nil manager guards failed")
	}

	rawKey := "vk_tp_prod_0123456789abcdefABCDEF01234570"
	orgID, projID, envID, keyID := seedTenantAndKey(t, st, fk, rawKey, "prod", KeyStateSuspended)
	mgr, err := NewManager(ctx, ManagerConfig{
		DB:    st.RawDB(),
		NowMs: func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	nowMs := fk.Now().UTC().UnixMilli()
	if _, err := mgr.Current().AuthenticateKey(rawKey, envID, nowMs); !errors.Is(err, ErrKeySuspended) {
		t.Fatalf("expected ErrKeySuspended, got %v", err)
	}
	if _, err := mgr.Current().AuthenticateKey("vk_tp_prod_nonexistent0000000000000000000000", envID, nowMs); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
	if v, ok := mgr.Current().LookupVirtualKeyByID(keyID); !ok || v.ID != keyID {
		t.Fatalf("LookupVirtualKeyByID failed: ok=%v v=%+v", ok, v)
	}

	// Verify QUOTA_PAUSED and EXPIRED states in snapshot evaluator.
	for tcState, wantErr := range map[string]error{
		KeyStateQuotaPaused:            ErrKeyQuotaPaused,
		KeyStateExpired:                ErrKeyExpired,
		KeyStatePurgedMetadataRetained: ErrKeyRevoked,
	} {
		if _, err := st.RawDB().ExecContext(ctx, `UPDATE virtual_keys SET state = ? WHERE id = ?`, tcState, keyID); err != nil {
			t.Fatalf("update state %s: %v", tcState, err)
		}
		if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
			OrgID: &orgID, EntityKind: "virtual_key", EntityID: keyID, MutationType: port.MutationUpsert, CreatedAtMs: nowMs,
		}); err != nil {
			t.Fatalf("Changelog.Append: %v", err)
		}
		if err := mgr.Refresh(ctx); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		if _, err := mgr.Current().AuthenticateKey(rawKey, envID, nowMs); !errors.Is(err, wantErr) {
			t.Fatalf("state %s expected %v, got %v", tcState, wantErr, err)
		}
	}

	// Verify inactive organization triggers ErrTenantSuspended.
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE virtual_keys SET state = 'CREATED_ACTIVE' WHERE id = ?`, keyID); err != nil {
		t.Fatalf("restore active state: %v", err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE organizations SET status = 'SUSPENDED' WHERE id = ?`, orgID); err != nil {
		t.Fatalf("suspend org: %v", err)
	}
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &orgID, EntityKind: "organization", EntityID: orgID, MutationType: port.MutationUpsert, CreatedAtMs: nowMs,
	}); err != nil {
		t.Fatalf("Changelog.Append org: %v", err)
	}
	_ = mgr.Refresh(ctx)
	if _, err := mgr.Current().AuthenticateKey(rawKey, envID, nowMs); !errors.Is(err, ErrTenantSuspended) {
		t.Fatalf("suspended org expected ErrTenantSuspended, got %v", err)
	}

	// Verify SHRED changelog aborts streams scoped to project and organization cleanly.
	sCtx1, cancel1 := context.WithCancel(ctx)
	defer cancel1()
	sCtx2, cancel2 := context.WithCancel(ctx)
	defer cancel2()
	h1 := mgr.Streams().Register(orgID, projID, keyID, cancel1)
	h2 := mgr.Streams().Register(orgID, "prj_other", "vky_other", cancel2)
	mgr.Streams().Unregister(keyID, h1)
	if sCtx1.Err() != nil {
		t.Fatalf("unregistered stream should not cancel early")
	}
	mgr.Streams().Register(orgID, projID, keyID, cancel1)
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &orgID, EntityKind: "project", EntityID: projID, MutationType: port.MutationShred, Emergency: true, CreatedAtMs: nowMs,
	}); err != nil {
		t.Fatalf("Changelog.Append project shred: %v", err)
	}
	_ = mgr.Refresh(ctx)
	if sCtx1.Err() == nil {
		t.Fatalf("project SHRED expected stream in projID canceled")
	}
	if sCtx2.Err() != nil {
		t.Fatalf("project SHRED should not cancel sibling project stream")
	}
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &orgID, EntityKind: "organization", EntityID: orgID, MutationType: port.MutationShred, Emergency: true, CreatedAtMs: nowMs,
	}); err != nil {
		t.Fatalf("Changelog.Append org shred: %v", err)
	}
	_ = mgr.Refresh(ctx)
	if sCtx2.Err() == nil {
		t.Fatalf("org SHRED expected remaining stream in orgID canceled")
	}
	mgr.Streams().Unregister("vky_other", h2)
	if mgr.LastError() != nil {
		t.Fatalf("unexpected LastError: %v", mgr.LastError())
	}

	// Verify AuthenticateDigest, empty-key HashRawVirtualKey, and non-existent ID lookup guards.
	if _, ok := HashRawVirtualKey(""); ok {
		t.Fatalf("expected empty key HashRawVirtualKey ok=false")
	}
	if sum, ok := HashRawVirtualKey(rawKey); !ok {
		t.Fatalf("HashRawVirtualKey failed")
	} else if _, err := mgr.Current().AuthenticateDigest(sum, envID, nowMs); !errors.Is(err, ErrTenantSuspended) {
		t.Fatalf("AuthenticateDigest suspended org expected ErrTenantSuspended, got %v", err)
	}
	if _, ok := mgr.Current().LookupVirtualKeyByID("vky_does_not_exist"); ok {
		t.Fatalf("expected missing key ID ok=false")
	}
	if _, ok := mgr.Current().LookupEffectiveConfig("org_x", "prj_y", "env_z"); ok {
		t.Fatalf("expected missing effective config ok=false")
	}

	// Verify expired QUOTA_PAUSED key reports ErrKeyExpired (SM-05).
	pastExp := nowMs - 1000
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE virtual_keys SET state = 'QUOTA_PAUSED', expires_at_ms = ? WHERE id = ?`, pastExp, keyID); err != nil {
		t.Fatalf("update expired quota_paused: %v", err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE organizations SET status = 'ACTIVE' WHERE id = ?`, orgID); err != nil {
		t.Fatalf("reactivate org: %v", err)
	}
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &orgID, EntityKind: "virtual_key", EntityID: keyID, MutationType: port.MutationUpsert, CreatedAtMs: nowMs,
	}); err != nil {
		t.Fatalf("append changelog: %v", err)
	}
	_ = mgr.Refresh(ctx)
	if _, err := mgr.Current().AuthenticateKey(rawKey, envID, nowMs); !errors.Is(err, ErrKeyExpired) {
		t.Fatalf("expired QUOTA_PAUSED key expected ErrKeyExpired, got %v", err)
	}
}

func TestRegisterAuthenticatedStreamZeroTOCTOU(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openTestSQLite(t, fk)
	ctx := context.Background()

	rawKey := "vk_tp_prod_0123456789abcdefABCDEF01234571"
	orgID, _, envID, keyID := seedTenantAndKey(t, st, fk, rawKey, "prod", KeyStateCreatedActive)
	mgr, err := NewManager(ctx, ManagerConfig{
		DB:    st.RawDB(),
		NowMs: func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	res, hid, err := mgr.RegisterAuthenticatedStream(rawKey, envID, scancel)
	if err != nil || hid == 0 || res.Key.ID != keyID {
		t.Fatalf("RegisterAuthenticatedStream failed: res=%+v hid=%d err=%v", res, hid, err)
	}

	nowMs := fk.Now().UTC().UnixMilli()
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE virtual_keys SET state = 'REVOKED' WHERE id = ?`, keyID); err != nil {
		t.Fatalf("revoke update: %v", err)
	}
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &orgID, EntityKind: "virtual_key", EntityID: keyID, MutationType: port.MutationRevoke, Emergency: true, CreatedAtMs: nowMs,
	}); err != nil {
		t.Fatalf("append revoke: %v", err)
	}
	_ = mgr.Refresh(ctx)

	if sctx.Err() == nil {
		t.Fatalf("expected active registered stream to abort on emergency revoke")
	}
	// Attempting to register another stream after revoke must fail closed and leave 0 streams leaked.
	sctx2, scancel2 := context.WithCancel(ctx)
	defer scancel2()
	if _, _, err := mgr.RegisterAuthenticatedStream(rawKey, envID, scancel2); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("post-revoke RegisterAuthenticatedStream expected ErrKeyRevoked, got %v", err)
	}
	if mgr.Streams().ActiveCount() != 0 {
		t.Fatalf("expected 0 leaked stream handles, got %d (sctx2=%v)", mgr.Streams().ActiveCount(), sctx2.Err())
	}
}

func BenchmarkSnapshotLookup(b *testing.B) {
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	dbPath := filepath.Join(b.TempDir(), "bench.db")
	st, err := sqlite.Open(context.Background(), sqlite.Config{
		DBPath: dbPath,
		NowMs: func() int64 {
			return fk.Now().UTC().UnixMilli()
		},
	})
	if err != nil {
		b.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "bench-org", DisplayName: "Bench Org"})
	if err != nil {
		b.Fatalf("CreateOrganization: %v", err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "bench-proj", DisplayName: "Bench Proj"})
	if err != nil {
		b.Fatalf("CreateProject: %v", err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		b.Fatalf("CreateEnvironment: %v", err)
	}

	rawKey := "vk_tp_prod_0123456789abcdefABCDEF98765432"
	sum := sha256.Sum256([]byte(rawKey))
	nowMs := fk.Now().UTC().UnixMilli()
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO virtual_keys (
			id, org_id, project_id, env_id, owner_principal_id, name,
			key_prefix, last_four, active_key_hash, previous_key_hash, grace_expires_at_ms,
			state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			rpm_limit, tpm_limit, max_concurrent_streams, is_ephemeral_playground,
			expires_at_ms, revoked_at_ms, revocation_mode, version, created_at_ms, updated_at_ms
		) VALUES ('vky_01jabcdef01234567890123499', ?, ?, ?, NULL, 'bench', 'vk_tp_prod_', '5432', ?, NULL, NULL, 'CREATED_ACTIVE', 'PROVIDER', '["*"]', '["*"]', '[]', NULL, NULL, 50, 0, NULL, NULL, NULL, 1, ?, ?)`,
		org.ID, proj.ID, env.ID, sum[:], nowMs, nowMs,
	); err != nil {
		b.Fatalf("insert key: %v", err)
	}
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &org.ID, EntityKind: "virtual_key", EntityID: "vky_01jabcdef01234567890123499", MutationType: port.MutationUpsert, CreatedAtMs: nowMs,
	}); err != nil {
		b.Fatalf("Append: %v", err)
	}

	mgr, err := NewManager(ctx, ManagerConfig{
		DB: st.RawDB(),
		NowMs: func() int64 {
			return fk.Now().UTC().UnixMilli()
		},
	})
	if err != nil {
		b.Fatalf("NewManager: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap := mgr.Current()
		res, authErr := snap.AuthenticateKey(rawKey, env.ID, nowMs)
		if authErr != nil || res.Key == nil || res.Config == nil {
			b.Fatalf("AuthenticateKey failed: res=%+v err=%v", res, authErr)
		}
	}
}
