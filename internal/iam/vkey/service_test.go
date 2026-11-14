// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vkey

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type vkeyTestHarness struct {
	st      *sqlite.Store
	fk      *clock.FakeClock
	mgr     *snapshot.Manager
	svc     *Service
	dbPath  string
	orgAID  string
	orgBID  string
	projAID string
	envStg  string
	envProd string
}

func newVKeyTestHarness(t *testing.T) *vkeyTestHarness {
	t.Helper()
	ctx := context.Background()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	dbPath := filepath.Join(t.TempDir(), "vkey.db")
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:    st.RawDB(),
		NowMs: func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	svc, err := NewService(ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
		Snapshots: mgr,
	})
	if err != nil {
		t.Fatalf("vkey.NewService: %v", err)
	}

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-a", DisplayName: "Org A"})
	if err != nil {
		t.Fatalf("CreateOrganization A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-b", DisplayName: "Org B"})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}
	projA, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "proj-a", DisplayName: "Proj A"})
	if err != nil {
		t.Fatalf("CreateProject A: %v", err)
	}
	envStg, err := st.Tenancy().CreateEnvironment(ctx, orgA.ID, projA.ID, port.CreateEnvironmentParams{Slug: "staging"})
	if err != nil {
		t.Fatalf("CreateEnvironment staging: %v", err)
	}
	envProd, err := st.Tenancy().CreateEnvironment(ctx, orgA.ID, projA.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatalf("CreateEnvironment prod: %v", err)
	}
	_ = mgr.Refresh(ctx)

	return &vkeyTestHarness{
		st:      st,
		fk:      fk,
		mgr:     mgr,
		svc:     svc,
		dbPath:  dbPath,
		orgAID:  orgA.ID,
		orgBID:  orgB.ID,
		projAID: projA.ID,
		envStg:  envStg.ID,
		envProd: envProd.ID,
	}
}

func TestVirtualKeyCreationStorageFormatAndEnvironmentScope(t *testing.T) {
	t.Parallel()
	h := newVKeyTestHarness(t)
	ctx := context.Background()

	created, err := h.svc.Create(ctx, CreateParams{
		OrgID:     h.orgAID,
		ProjectID: h.projAID,
		EnvID:     h.envStg,
		Name:      "staging-app",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !strings.HasPrefix(created.SecretKey, "vk_tp_staging_") {
		t.Fatalf("expected vk_tp_staging_ prefix, got %q", created.SecretKey)
	}
	suffix := strings.TrimPrefix(created.SecretKey, "vk_tp_staging_")
	if len(suffix) != 32 {
		t.Fatalf("expected 32 base62 chars, got %d", len(suffix))
	}
	if created.Summary.LastFour != created.SecretKey[len(created.SecretKey)-4:] {
		t.Fatalf("last_four mismatch")
	}

	// Verify DB stores ONLY raw 32-byte SHA-256 hash and NEVER plaintext secret key bytes.
	var storedHash []byte
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT active_key_hash FROM virtual_keys WHERE id = ?`, created.Summary.ID).Scan(&storedHash); err != nil {
		t.Fatalf("select active_key_hash: %v", err)
	}
	sum := sha256.Sum256([]byte(created.SecretKey))
	if len(storedHash) != 32 || string(storedHash) != string(sum[:]) {
		t.Fatalf("stored hash does not match SHA-256(rawKey)")
	}
	rawDBBytes, err := os.ReadFile(h.dbPath)
	if err != nil {
		t.Fatalf("read sqlite file: %v", err)
	}
	if strings.Contains(string(rawDBBytes), created.SecretKey) {
		t.Fatalf("SECURITY VIOLATION: raw Virtual Key found on disk inside SQLite database")
	}

	// AUTH-04 / NEG-1: Valid on staging, rejected with 403 environment_scope_denied on prod.
	nowMs := h.fk.Now().UTC().UnixMilli()
	if _, err := h.mgr.Current().AuthenticateKey(created.SecretKey, h.envStg, nowMs); err != nil {
		t.Fatalf("staging key on staging env failed: %v", err)
	}
	if _, err := h.mgr.Current().AuthenticateKey(created.SecretKey, h.envProd, nowMs); !errors.Is(err, snapshot.ErrEnvironmentScopeDenied) {
		t.Fatalf("staging key on prod env expected ErrEnvironmentScopeDenied, got %v", err)
	}

	// AUTH-15: Org B probing Org A's key ID receives ErrNotFound.
	if _, err := h.svc.Get(ctx, h.orgBID, created.Summary.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org Get expected ErrNotFound, got %v", err)
	}
}

func TestVirtualKeyGraceRotationAndExpiry(t *testing.T) {
	t.Parallel()
	h := newVKeyTestHarness(t)
	ctx := context.Background()

	k1, err := h.svc.Create(ctx, CreateParams{
		OrgID:     h.orgAID,
		ProjectID: h.projAID,
		EnvID:     h.envProd,
		Name:      "prod-key",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// AC-2: Rotate with grace_seconds = 300.
	k2, err := h.svc.Rotate(ctx, RotateParams{
		OrgID:           h.orgAID,
		KeyID:           k1.Summary.ID,
		ExpectedVersion: 1,
		GraceSeconds:    300,
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if k2.Summary.State != snapshot.KeyStateRotatingDualValid || k2.Summary.Version != 2 {
		t.Fatalf("unexpected rotated state: %+v", k2.Summary)
	}
	if k2.SecretKey == k1.SecretKey {
		t.Fatalf("rotated secret must differ from previous secret")
	}

	// Both K1 and K2 succeed within 300s grace window (e.g. T + 150s).
	h.fk.Advance(150 * time.Second)
	t150 := h.fk.Now().UTC().UnixMilli()
	if res, err := h.mgr.Current().AuthenticateKey(k1.SecretKey, h.envProd, t150); err != nil || !res.MatchedPrevious {
		t.Fatalf("K1 expected valid during grace window (MatchedPrevious=true): res=%+v err=%v", res, err)
	}
	if res, err := h.mgr.Current().AuthenticateKey(k2.SecretKey, h.envProd, t150); err != nil || res.MatchedPrevious {
		t.Fatalf("K2 expected valid during grace window (MatchedPrevious=false): res=%+v err=%v", res, err)
	}

	// AC-2: FakeClock.Advance past 300s (total 301s) -> K1 rejected with ErrKeyExpired, K2 still valid.
	h.fk.Advance(151 * time.Second)
	t301 := h.fk.Now().UTC().UnixMilli()
	if _, err := h.mgr.Current().AuthenticateKey(k1.SecretKey, h.envProd, t301); !errors.Is(err, snapshot.ErrKeyExpired) {
		t.Fatalf("K1 after 301s expected ErrKeyExpired, got %v", err)
	}
	if _, err := h.mgr.Current().AuthenticateKey(k2.SecretKey, h.envProd, t301); err != nil {
		t.Fatalf("K2 after 301s expected valid, got %v", err)
	}

	// Sweeper cleanly clears previous_key_hash and returns state to CREATED_ACTIVE.
	finalized, err := h.svc.FinalizeExpiredRotations(ctx)
	if err != nil || finalized != 1 {
		t.Fatalf("FinalizeExpiredRotations expected 1, got %d err=%v", finalized, err)
	}
	afterSweep, err := h.svc.Get(ctx, h.orgAID, k1.Summary.ID)
	if err != nil || afterSweep.State != snapshot.KeyStateCreatedActive {
		t.Fatalf("expected CREATED_ACTIVE after sweep, got %+v err=%v", afterSweep, err)
	}

	// Stale optimistic lock version conflict returns ErrVersionConflict.
	if _, err := h.svc.Rotate(ctx, RotateParams{
		OrgID:           h.orgAID,
		KeyID:           k1.Summary.ID,
		ExpectedVersion: 1,
		GraceSeconds:    60,
	}); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("stale Rotate expected ErrVersionConflict, got %v", err)
	}
}

func TestQuotaPausedEmergencyRevokeAndNonRevival(t *testing.T) {
	t.Parallel()
	h := newVKeyTestHarness(t)
	ctx := context.Background()

	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.mgr.StartWatcher(watchCtx, 25*time.Millisecond)
	}()

	created, err := h.svc.Create(ctx, CreateParams{
		OrgID:     h.orgAID,
		ProjectID: h.projAID,
		EnvID:     h.envProd,
		Name:      "stream-key",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Transition key to QUOTA_PAUSED (AC-3 precondition).
	paused, err := h.svc.MarkQuotaPaused(ctx, h.orgAID, created.Summary.ID, 1)
	if err != nil || paused.State != snapshot.KeyStateQuotaPaused {
		t.Fatalf("MarkQuotaPaused failed: %+v err=%v", paused, err)
	}

	// Attach an active SSE stream context to verify emergency abort directly from QUOTA_PAUSED.
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	h.mgr.Streams().Register(h.orgAID, h.projAID, created.Summary.ID, cancelStream)

	// AC-3 / AUTH-06: Emergency revoke directly from QUOTA_PAUSED.
	revoked, err := h.svc.Revoke(ctx, RevokeParams{
		OrgID:           h.orgAID,
		KeyID:           created.Summary.ID,
		ExpectedVersion: paused.Version,
		Mode:            RevokeModeEmergency,
		ActorID:         "prc_sec_reviewer",
	})
	if err != nil || revoked.State != snapshot.KeyStateRevoked {
		t.Fatalf("Revoke from QUOTA_PAUSED failed: %+v err=%v", revoked, err)
	}
	if streamCtx.Err() == nil {
		t.Fatalf("expected emergency revoke to abort active SSE stream immediately")
	}

	// Subsequent requests receive ErrKeyRevoked.
	nowMs := h.fk.Now().UTC().UnixMilli()
	if _, err := h.mgr.Current().AuthenticateKey(created.SecretKey, h.envProd, nowMs); !errors.Is(err, snapshot.ErrKeyRevoked) {
		t.Fatalf("expected ErrKeyRevoked after revocation, got %v", err)
	}

	// AUTH-05 / SM-05 Mandatory Non-Revival Security Invariant:
	// Billing period rollover / ResumeFromQuotaPaused MUST NEVER revive a REVOKED key!
	if _, err := h.svc.ResumeFromQuotaPaused(ctx, h.orgAID, created.Summary.ID, revoked.Version); !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("SECURITY VIOLATION: ResumeFromQuotaPaused revived or accepted REVOKED key: err=%v", err)
	}

	var dbState string
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT state FROM virtual_keys WHERE id = ?`, created.Summary.ID).Scan(&dbState); err != nil {
		t.Fatalf("query db state: %v", err)
	}
	if dbState != snapshot.KeyStateRevoked {
		t.Fatalf("expected persisted state REVOKED, got %q", dbState)
	}
	_ = sql.ErrNoRows

	stopWatch()
	wg.Wait()
}

func TestVirtualKeySuspendReactivateTTLExpiryAndCanaryHygiene(t *testing.T) {
	t.Parallel()
	h := newVKeyTestHarness(t)
	ctx := context.Background()

	if _, err := NewService(ServiceConfig{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("nil DB expected ErrInvalidInput, got %v", err)
	}

	ttlMs := h.fk.Now().UTC().Add(1 * time.Hour).UnixMilli()
	k, err := h.svc.Create(ctx, CreateParams{
		OrgID:       h.orgAID,
		ProjectID:   h.projAID,
		EnvID:       h.envStg,
		Name:        "ttl-key",
		ExpiresAtMs: &ttlMs,
	})
	if err != nil {
		t.Fatalf("Create with TTL: %v", err)
	}

	// Suspend -> Reactivate succeeds while valid.
	susp, err := h.svc.Suspend(ctx, h.orgAID, k.Summary.ID, 1)
	if err != nil || susp.State != snapshot.KeyStateSuspended {
		t.Fatalf("Suspend failed: %+v err=%v", susp, err)
	}
	if _, err := h.mgr.Current().AuthenticateKey(k.SecretKey, h.envStg, h.fk.Now().UTC().UnixMilli()); !errors.Is(err, snapshot.ErrKeySuspended) {
		t.Fatalf("suspended key expected ErrKeySuspended, got %v", err)
	}
	reac, err := h.svc.ReactivateFromSuspended(ctx, h.orgAID, k.Summary.ID, susp.Version)
	if err != nil || reac.State != snapshot.KeyStateCreatedActive {
		t.Fatalf("ReactivateFromSuspended failed: %+v err=%v", reac, err)
	}

	// QUOTA_PAUSED -> CREATED_ACTIVE succeeds when key is NOT revoked/expired.
	paused, err := h.svc.MarkQuotaPaused(ctx, h.orgAID, k.Summary.ID, reac.Version)
	if err != nil {
		t.Fatalf("MarkQuotaPaused: %v", err)
	}
	resumed, err := h.svc.ResumeFromQuotaPaused(ctx, h.orgAID, k.Summary.ID, paused.Version)
	if err != nil || resumed.State != snapshot.KeyStateCreatedActive {
		t.Fatalf("ResumeFromQuotaPaused failed: %+v err=%v", resumed, err)
	}

	// Zero-grace rotation immediately invalidates old secret.
	zeroRot, err := h.svc.Rotate(ctx, RotateParams{
		OrgID:           h.orgAID,
		KeyID:           k.Summary.ID,
		ExpectedVersion: resumed.Version,
		GraceSeconds:    0,
	})
	if err != nil || zeroRot.Summary.State != snapshot.KeyStateCreatedActive {
		t.Fatalf("zero-grace Rotate failed: %+v err=%v", zeroRot, err)
	}
	nowMs := h.fk.Now().UTC().UnixMilli()
	if _, err := h.mgr.Current().AuthenticateKey(k.SecretKey, h.envStg, nowMs); !errors.Is(err, snapshot.ErrKeyNotFound) {
		t.Fatalf("old secret after zero-grace rotate expected ErrKeyNotFound, got %v", err)
	}

	// Advance clock past ExpiresAtMs -> ErrKeyExpired.
	h.fk.Advance(2 * time.Hour)
	expiredNowMs := h.fk.Now().UTC().UnixMilli()
	if _, err := h.mgr.Current().AuthenticateKey(zeroRot.SecretKey, h.envStg, expiredNowMs); !errors.Is(err, snapshot.ErrKeyExpired) {
		t.Fatalf("expired key expected ErrKeyExpired, got %v", err)
	}
	if _, err := h.svc.Rotate(ctx, RotateParams{
		OrgID: h.orgAID, KeyID: k.Summary.ID, ExpectedVersion: zeroRot.Summary.Version, GraceSeconds: 60,
	}); !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("rotating expired key expected ErrInvalidStateTransition, got %v", err)
	}

	// Standard revoke on an active key succeeds once and returns ErrAlreadyRevoked on repeat.
	k2, err := h.svc.Create(ctx, CreateParams{
		OrgID: h.orgAID, ProjectID: h.projAID, EnvID: h.envProd, Name: "std-revoke",
	})
	if err != nil {
		t.Fatalf("Create k2: %v", err)
	}
	rev2, err := h.svc.Revoke(ctx, RevokeParams{
		OrgID: h.orgAID, KeyID: k2.Summary.ID, ExpectedVersion: 1, Mode: RevokeModeStandard,
	})
	if err != nil || rev2.State != snapshot.KeyStateRevoked {
		t.Fatalf("Standard Revoke failed: %+v err=%v", rev2, err)
	}
	if _, err := h.svc.Revoke(ctx, RevokeParams{
		OrgID: h.orgAID, KeyID: k2.Summary.ID, ExpectedVersion: rev2.Version, Mode: RevokeModeStandard,
	}); !errors.Is(err, ErrAlreadyRevoked) {
		t.Fatalf("double Revoke expected ErrAlreadyRevoked, got %v", err)
	}

	// Verify AuditSink recording, RPM/TPM validation, CanaryScanner hygiene, and invalid input guards.
	var auditEvents []port.AuditEvent
	auditSink := auditRecorderFunc(func(_ context.Context, ev port.AuditEvent) error {
		auditEvents = append(auditEvents, ev)
		return nil
	})
	auditedSvc, err := NewService(ServiceConfig{
		DB:        h.st.RawDB(),
		NowMs:     func() int64 { return h.fk.Now().UTC().UnixMilli() },
		Snapshots: h.mgr,
		AuditSink: auditSink,
	})
	if err != nil {
		t.Fatalf("NewService audited: %v", err)
	}
	badLimit := int64(0)
	if _, err := auditedSvc.Create(ctx, CreateParams{
		OrgID: h.orgAID, ProjectID: h.projAID, EnvID: h.envProd, Name: "bad-rpm", RPMLimit: &badLimit,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("zero RPMLimit expected ErrInvalidInput, got %v", err)
	}
	if _, err := auditedSvc.Get(ctx, "", "k"); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("empty orgID Get expected ErrInvalidInput, got %v", err)
	}
	if _, err := auditedSvc.Revoke(ctx, RevokeParams{
		OrgID: h.orgAID, KeyID: k2.Summary.ID, ExpectedVersion: 1, Mode: "INVALID_MODE",
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("invalid revocation mode expected ErrInvalidInput, got %v", err)
	}

	rpm := int64(120)
	tpm := int64(50000)
	k3, err := auditedSvc.Create(ctx, CreateParams{
		OrgID: h.orgAID, ProjectID: h.projAID, EnvID: h.envProd, Name: "audited-key",
		RPMLimit: &rpm, TPMLimit: &tpm, AllowedModels: []string{"gpt-4o"}, AllowedRoutes: []string{"default"},
	})
	if err != nil {
		t.Fatalf("Create audited key: %v", err)
	}
	if _, err := auditedSvc.Revoke(ctx, RevokeParams{
		OrgID: h.orgAID, KeyID: k3.Summary.ID, ExpectedVersion: 1, Mode: RevokeModeEmergency, ActorID: "prc_op",
	}); err != nil {
		t.Fatalf("Revoke audited key: %v", err)
	}
	if len(auditEvents) != 1 || auditEvents[0].EventType != "virtual_key.revoked" {
		t.Fatalf("expected virtual_key.revoked audit event, got %+v", auditEvents)
	}

	// Cross-tenant owner_principal_id anti-oracle (AUTH-15): Org B principal or unknown principal
	// must both fail identically with port.ErrNotFound without binding Org B principal to Org A key.
	nowMs = h.fk.Now().UTC().UnixMilli()
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, password_argon2id, status, version, created_at_ms, updated_at_ms)
		VALUES ('prc_01orgbprincipal000000000001', ?, 'HUMAN_LOCAL', 'orgb@example.test', 'x', 'ACTIVE', 1, ?, ?)`,
		h.orgBID, nowMs, nowMs,
	); err != nil {
		t.Fatalf("insert Org B principal: %v", err)
	}
	orgBPrincipal := "prc_01orgbprincipal000000000001"
	unknownPrincipal := "prc_01unknownprincipal000000001"
	for _, probeOwner := range []*string{&orgBPrincipal, &unknownPrincipal} {
		if _, err := h.svc.Create(ctx, CreateParams{
			OrgID: h.orgAID, ProjectID: h.projAID, EnvID: h.envProd, Name: "probe", OwnerPrincipalID: probeOwner,
		}); !errors.Is(err, port.ErrNotFound) {
			t.Fatalf("owner_principal_id probe %s expected port.ErrNotFound, got %v", *probeOwner, err)
		}
	}

	// Pausing during ROTATING_DUAL_VALID and then resuming cleanly clears stale previous_key_hash/grace_expires_at_ms.
	kRot, err := h.svc.Create(ctx, CreateParams{OrgID: h.orgAID, ProjectID: h.projAID, EnvID: h.envStg, Name: "rot-pause"})
	if err != nil {
		t.Fatalf("Create rot-pause: %v", err)
	}
	kRot2, err := h.svc.Rotate(ctx, RotateParams{OrgID: h.orgAID, KeyID: kRot.Summary.ID, ExpectedVersion: 1, GraceSeconds: 300})
	if err != nil {
		t.Fatalf("Rotate rot-pause: %v", err)
	}
	pausedRot, err := h.svc.MarkQuotaPaused(ctx, h.orgAID, kRot.Summary.ID, kRot2.Summary.Version)
	if err != nil {
		t.Fatalf("MarkQuotaPaused rot-pause: %v", err)
	}
	resumedRot, err := h.svc.ResumeFromQuotaPaused(ctx, h.orgAID, kRot.Summary.ID, pausedRot.Version)
	if err != nil {
		t.Fatalf("ResumeFromQuotaPaused rot-pause: %v", err)
	}
	if resumedRot.GraceExpiresAtMs != nil {
		t.Fatalf("expected GraceExpiresAtMs cleared on CREATED_ACTIVE transition, got %v", *resumedRot.GraceExpiresAtMs)
	}

	// TTL-expired QUOTA_PAUSED key cannot be revived via ResumeFromQuotaPaused (AUTH-05).
	expTTL := h.fk.Now().UTC().Add(5 * time.Minute).UnixMilli()
	kExp, err := h.svc.Create(ctx, CreateParams{OrgID: h.orgAID, ProjectID: h.projAID, EnvID: h.envStg, Name: "exp-pause", ExpiresAtMs: &expTTL})
	if err != nil {
		t.Fatalf("Create exp-pause: %v", err)
	}
	pausedExp, err := h.svc.MarkQuotaPaused(ctx, h.orgAID, kExp.Summary.ID, 1)
	if err != nil {
		t.Fatalf("MarkQuotaPaused exp-pause: %v", err)
	}
	h.fk.Advance(10 * time.Minute)
	if _, err := h.svc.ResumeFromQuotaPaused(ctx, h.orgAID, kExp.Summary.ID, pausedExp.Version); !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("TTL-expired QUOTA_PAUSED key expected ErrInvalidStateTransition on resume, got %v", err)
	}

	// Invalid AllowedPlanes rejected fail-closed; valid CSV normalized uppercase.
	if _, err := h.svc.Create(ctx, CreateParams{
		OrgID: h.orgAID, ProjectID: h.projAID, EnvID: h.envStg, Name: "bad-planes", AllowedPlanes: "PROVIDER,ROOT",
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("invalid allowed_planes expected ErrInvalidInput, got %v", err)
	}
	createdPlanes, err := h.svc.Create(ctx, CreateParams{
		OrgID: h.orgAID, ProjectID: h.projAID, EnvID: h.envStg, Name: "multi-planes", AllowedPlanes: "provider, mcp, a2a, mcp",
	})
	if err != nil {
		t.Fatalf("Create normalized allowed_planes: %v", err)
	}
	if createdPlanes.Summary.AllowedPlanes != "PROVIDER,MCP,A2A" {
		t.Fatalf("expected normalized AllowedPlanes PROVIDER,MCP,A2A, got %q", createdPlanes.Summary.AllowedPlanes)
	}
}

func TestNewServiceRequiresSnapshotsUnlessDetachedOptIn(t *testing.T) {
	t.Parallel()
	h := newVKeyTestHarness(t)

	if _, err := NewService(ServiceConfig{DB: h.st.RawDB()}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("NewService with nil Snapshots and default config expected ErrInvalidInput, got %v", err)
	}
	detached, err := NewService(ServiceConfig{
		DB:                        h.st.RawDB(),
		AllowDetachedControlPlane: true,
	})
	if err != nil || detached == nil {
		t.Fatalf("NewService with explicit AllowDetachedControlPlane=true failed: %v", err)
	}
}

type syncedAuditRecorder struct {
	mu     sync.Mutex
	events []port.AuditEvent
}

func (r *syncedAuditRecorder) Append(_ context.Context, ev port.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}

func (r *syncedAuditRecorder) Count(eventType string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, ev := range r.events {
		if ev.EventType == eventType {
			n++
		}
	}
	return n
}

func TestRevokeAndTransitionsRefreshFailureFailClosedWithoutRollbackOrLostAudit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	dbPath := filepath.Join(t.TempDir(), "split-sync.db")

	dbWrite, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open write: %v", err)
	}
	t.Cleanup(func() { _ = dbWrite.Close() })

	// Separate reader pool modeling Control/Data snapshot reader connection so we can trigger a real
	// post-commit Refresh failure without mocks and without rolling back dbWrite.
	dbRead, err := sqlite.Open(ctx, sqlite.Config{
		DBPath:      dbPath,
		NowMs:       func() int64 { return fk.Now().UTC().UnixMilli() },
		SkipMigrate: true,
	})
	if err != nil {
		t.Fatalf("sqlite.Open read: %v", err)
	}
	defer func() { _ = dbRead.Close() }()

	org, err := dbWrite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "sync-org", DisplayName: "Sync Org"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	proj, err := dbWrite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "sync-proj", DisplayName: "Sync Proj"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	envProd, err := dbWrite.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:         dbRead.RawDB(),
		NowMs:      func() int64 { return fk.Now().UTC().UnixMilli() },
		MaxStaleMs: 10_000,
	})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	auditRec := &syncedAuditRecorder{}
	svc, err := NewService(ServiceConfig{
		DB:        dbWrite.RawDB(),
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
		Snapshots: mgr,
		AuditSink: auditRec,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	kRev, err := svc.Create(ctx, CreateParams{OrgID: org.ID, ProjectID: proj.ID, EnvID: envProd.ID, Name: "revoke-fail-sync"})
	if err != nil {
		t.Fatalf("Create kRev: %v", err)
	}
	kSusp, err := svc.Create(ctx, CreateParams{OrgID: org.ID, ProjectID: proj.ID, EnvID: envProd.ID, Name: "suspend-fail-sync"})
	if err != nil {
		t.Fatalf("Create kSusp: %v", err)
	}
	kZeroRot, err := svc.Create(ctx, CreateParams{OrgID: org.ID, ProjectID: proj.ID, EnvID: envProd.ID, Name: "zerorot-fail-sync"})
	if err != nil {
		t.Fatalf("Create kZeroRot: %v", err)
	}

	// Register 2 active SSE streams for kRev prior to failure.
	streamCtx1, cancelStream1 := context.WithCancel(ctx)
	defer cancelStream1()
	if _, _, err := mgr.RegisterAuthenticatedStream(kRev.SecretKey, envProd.ID, cancelStream1); err != nil {
		t.Fatalf("RegisterAuthenticatedStream 1: %v", err)
	}
	streamCtx2, cancelStream2 := context.WithCancel(ctx)
	defer cancelStream2()
	if _, _, err := mgr.RegisterAuthenticatedStream(kRev.SecretKey, envProd.ID, cancelStream2); err != nil {
		t.Fatalf("RegisterAuthenticatedStream 2: %v", err)
	}

	// Inject real snapshot Refresh failure by closing dbRead before DB write commit mutations run.
	_ = dbRead.Close()

	// 1. Emergency Revoke commits REVOKED in dbWrite, Refresh fails:
	// - returns committed summary + ErrSnapshotSyncFailed (never false nil success)
	// - aborts all existing active SSE streams (no broken stream cancellation)
	// - preserves audit event in AuditSink
	// - enforces fail-closed ErrKeyRevoked on mgr.Current() and RegisterAuthenticatedStream
	revSummary, revErr := svc.Revoke(ctx, RevokeParams{
		OrgID:           org.ID,
		KeyID:           kRev.Summary.ID,
		ExpectedVersion: kRev.Summary.Version,
		Mode:            RevokeModeEmergency,
		ActorID:         "prc_sec_lead",
	})
	if !errors.Is(revErr, ErrSnapshotSyncFailed) {
		t.Fatalf("Revoke with failing Refresh expected ErrSnapshotSyncFailed, got %v", revErr)
	}
	if revSummary == nil || revSummary.State != snapshot.KeyStateRevoked {
		t.Fatalf("Revoke must still return committed REVOKED summary without false rollback, got %+v", revSummary)
	}
	var dbState string
	if err := dbWrite.RawDB().QueryRowContext(ctx, `SELECT state FROM virtual_keys WHERE id = ?`, kRev.Summary.ID).Scan(&dbState); err != nil || dbState != snapshot.KeyStateRevoked {
		t.Fatalf("expected DB state REVOKED committed without rollback, got state=%q err=%v", dbState, err)
	}
	if auditRec.Count("virtual_key.revoked") != 1 {
		t.Fatalf("expected audit event virtual_key.revoked preserved, got count=%d", auditRec.Count("virtual_key.revoked"))
	}
	if streamCtx1.Err() == nil || streamCtx2.Err() == nil {
		t.Fatalf("expected all existing SSE streams aborted despite Refresh failure (s1=%v, s2=%v)", streamCtx1.Err(), streamCtx2.Err())
	}
	nowMs := fk.Now().UTC().UnixMilli()
	if _, err := mgr.Current().AuthenticateKey(kRev.SecretKey, envProd.ID, nowMs); !errors.Is(err, snapshot.ErrKeyRevoked) {
		t.Fatalf("revoked key MUST NOT authenticate from stale snapshot after Refresh failure, got %v", err)
	}
	_, cancelStream3 := context.WithCancel(ctx)
	defer cancelStream3()
	if _, _, err := mgr.RegisterAuthenticatedStream(kRev.SecretKey, envProd.ID, cancelStream3); !errors.Is(err, snapshot.ErrKeyRevoked) {
		t.Fatalf("post-revoke stream registration expected ErrKeyRevoked, got %v", err)
	}
	if mgr.LastError() == nil {
		t.Fatalf("expected mgr.LastError() to record the Refresh failure")
	}

	// 2. Suspend commits in dbWrite while Refresh fails -> returns ErrSnapshotSyncFailed AND locally fences key as SUSPENDED.
	suspSummary, suspErr := svc.Suspend(ctx, org.ID, kSusp.Summary.ID, kSusp.Summary.Version)
	if !errors.Is(suspErr, ErrSnapshotSyncFailed) || suspSummary == nil || suspSummary.State != snapshot.KeyStateSuspended {
		t.Fatalf("Suspend with failing Refresh expected summary SUSPENDED + ErrSnapshotSyncFailed, got summary=%+v err=%v", suspSummary, suspErr)
	}
	if _, err := mgr.Current().AuthenticateKey(kSusp.SecretKey, envProd.ID, nowMs); !errors.Is(err, snapshot.ErrKeySuspended) {
		t.Fatalf("expected suspended key blocked on local snapshot immediately, got %v", err)
	}

	// 3. Zero-grace Rotate commits in dbWrite while Refresh fails -> old secret invalidated immediately in local snapshot.
	rotRes, rotErr := svc.Rotate(ctx, RotateParams{
		OrgID:           org.ID,
		KeyID:           kZeroRot.Summary.ID,
		ExpectedVersion: kZeroRot.Summary.Version,
		GraceSeconds:    0,
	})
	if !errors.Is(rotErr, ErrSnapshotSyncFailed) || rotRes == nil || rotRes.SecretKey == "" {
		t.Fatalf("zero-grace Rotate expected new key + ErrSnapshotSyncFailed, got res=%+v err=%v", rotRes, rotErr)
	}
	if _, err := mgr.Current().AuthenticateKey(kZeroRot.SecretKey, envProd.ID, nowMs); !errors.Is(err, snapshot.ErrKeyNotFound) {
		t.Fatalf("old secret after failed-refresh zero-grace Rotate MUST NOT authenticate, got %v", err)
	}

	// Verify FakeClock advance past MaxStaleMs still trips IsStale (local fences do NOT forge heartbeat freshness).
	fk.Advance(11 * time.Second)
	if !mgr.IsStale() {
		t.Fatalf("expected IsStale=true when clock advances without real DB refresh")
	}
	if _, _, err := mgr.RegisterAuthenticatedStream(kRev.SecretKey, envProd.ID, cancelStream3); !errors.Is(err, snapshot.ErrSnapshotStale) {
		t.Fatalf("stale snapshot expected ErrSnapshotStale, got %v", err)
	}

	// Verify audit metadata explicitly flagged snapshot_sync_pending:true while preserving event.
	auditRec.mu.Lock()
	lastMeta := ""
	for _, ev := range auditRec.events {
		if ev.EventType == "virtual_key.revoked" && ev.TargetID == kRev.Summary.ID {
			lastMeta = ev.MetadataJSON
		}
	}
	auditRec.mu.Unlock()
	if !strings.Contains(lastMeta, `"snapshot_sync_pending":true`) {
		t.Fatalf("expected audit metadata to record snapshot_sync_pending:true, got %q", lastMeta)
	}

	// 4. FinalizeExpiredRotations commits in dbWrite while Refresh fails -> old grace secret invalidated immediately on RAM.
	// Setup separate live pair so we rotate with GraceSeconds=60, advance clock, close reader DB, then FinalizeExpiredRotations.
	dbPath2 := filepath.Join(t.TempDir(), "split-finalize.db")
	dbW2, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath2, NowMs: func() int64 { return fk.Now().UTC().UnixMilli() }})
	if err != nil {
		t.Fatalf("sqlite.Open w2: %v", err)
	}
	t.Cleanup(func() { _ = dbW2.Close() })
	dbR2, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath2, NowMs: func() int64 { return fk.Now().UTC().UnixMilli() }, SkipMigrate: true})
	if err != nil {
		t.Fatalf("sqlite.Open r2: %v", err)
	}
	defer func() { _ = dbR2.Close() }()
	org2, _ := dbW2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fin-org", DisplayName: "Fin Org"})
	proj2, _ := dbW2.Tenancy().CreateProject(ctx, org2.ID, port.CreateProjectParams{Slug: "fin-proj", DisplayName: "Fin Proj"})
	env2, _ := dbW2.Tenancy().CreateEnvironment(ctx, org2.ID, proj2.ID, port.CreateEnvironmentParams{Slug: "prod"})
	mgrFin, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: dbR2.RawDB(), NowMs: func() int64 { return fk.Now().UTC().UnixMilli() }})
	if err != nil {
		t.Fatalf("NewManager fin: %v", err)
	}
	svcFin, err := NewService(ServiceConfig{DB: dbW2.RawDB(), NowMs: func() int64 { return fk.Now().UTC().UnixMilli() }, Snapshots: mgrFin})
	if err != nil {
		t.Fatalf("NewService fin: %v", err)
	}
	kFin, err := svcFin.Create(ctx, CreateParams{OrgID: org2.ID, ProjectID: proj2.ID, EnvID: env2.ID, Name: "fin-key"})
	if err != nil {
		t.Fatalf("Create kFin: %v", err)
	}
	if _, err := svcFin.Rotate(ctx, RotateParams{OrgID: org2.ID, KeyID: kFin.Summary.ID, ExpectedVersion: 1, GraceSeconds: 60}); err != nil {
		t.Fatalf("Rotate kFin: %v", err)
	}
	fk.Advance(61 * time.Second)
	_ = dbR2.Close()
	if nFin, err := svcFin.FinalizeExpiredRotations(ctx); !errors.Is(err, ErrSnapshotSyncFailed) || nFin != 1 {
		t.Fatalf("FinalizeExpiredRotations expected n=1 + ErrSnapshotSyncFailed, got n=%d err=%v", nFin, err)
	}
	if _, err := mgrFin.Current().AuthenticateKey(kFin.SecretKey, env2.ID, fk.Now().UTC().UnixMilli()); !errors.Is(err, snapshot.ErrKeyNotFound) {
		t.Fatalf("expected old grace key stripped cleanly from local snapshot, got %v", err)
	}

	// 5. Consecutive Rotate(GraceSeconds > 0) from ROTATING_DUAL_VALID on Refresh failure immediately evicts oldest K1 digest.
	kSeq, err := svcFin.Create(ctx, CreateParams{OrgID: org2.ID, ProjectID: proj2.ID, EnvID: env2.ID, Name: "seq-rot-key"})
	if !errors.Is(err, ErrSnapshotSyncFailed) || kSeq == nil {
		t.Fatalf("Create under closed reader expected secret + ErrSnapshotSyncFailed, got %v", err)
	}
	// Re-verify using a live manager that rotates once (K1 -> K2), closes reader, then rotates (K2 -> K3) with GraceSeconds=120.
	dbPath3 := filepath.Join(t.TempDir(), "split-seqrot.db")
	dbW3, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath3, NowMs: func() int64 { return fk.Now().UTC().UnixMilli() }})
	if err != nil {
		t.Fatalf("sqlite.Open w3: %v", err)
	}
	t.Cleanup(func() { _ = dbW3.Close() })
	dbR3, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath3, NowMs: func() int64 { return fk.Now().UTC().UnixMilli() }, SkipMigrate: true})
	if err != nil {
		t.Fatalf("sqlite.Open r3: %v", err)
	}
	defer func() { _ = dbR3.Close() }()
	org3, _ := dbW3.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "seq-org", DisplayName: "Seq Org"})
	proj3, _ := dbW3.Tenancy().CreateProject(ctx, org3.ID, port.CreateProjectParams{Slug: "seq-proj", DisplayName: "Seq Proj"})
	env3, _ := dbW3.Tenancy().CreateEnvironment(ctx, org3.ID, proj3.ID, port.CreateEnvironmentParams{Slug: "prod"})
	mgrSeq, _ := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: dbR3.RawDB(), NowMs: func() int64 { return fk.Now().UTC().UnixMilli() }})
	svcSeq, _ := NewService(ServiceConfig{DB: dbW3.RawDB(), NowMs: func() int64 { return fk.Now().UTC().UnixMilli() }, Snapshots: mgrSeq})
	kSeq1, err := svcSeq.Create(ctx, CreateParams{OrgID: org3.ID, ProjectID: proj3.ID, EnvID: env3.ID, Name: "k-seq"})
	if err != nil {
		t.Fatalf("Create kSeq1: %v", err)
	}
	kSeq2, err := svcSeq.Rotate(ctx, RotateParams{OrgID: org3.ID, KeyID: kSeq1.Summary.ID, ExpectedVersion: 1, GraceSeconds: 120})
	if err != nil {
		t.Fatalf("Rotate kSeq1->kSeq2: %v", err)
	}
	_ = dbR3.Close()
	if _, err := svcSeq.Rotate(ctx, RotateParams{OrgID: org3.ID, KeyID: kSeq1.Summary.ID, ExpectedVersion: kSeq2.Summary.Version, GraceSeconds: 120}); !errors.Is(err, ErrSnapshotSyncFailed) {
		t.Fatalf("second Rotate expected ErrSnapshotSyncFailed, got %v", err)
	}
	if _, err := mgrSeq.Current().AuthenticateKey(kSeq1.SecretKey, env3.ID, fk.Now().UTC().UnixMilli()); !errors.Is(err, snapshot.ErrKeyNotFound) {
		t.Fatalf("oldest rotated K1 digest must be evicted immediately on second Rotate failure, got %v", err)
	}
}

type auditRecorderFunc func(ctx context.Context, ev port.AuditEvent) error

func (f auditRecorderFunc) Append(ctx context.Context, ev port.AuditEvent) error { return f(ctx, ev) }
