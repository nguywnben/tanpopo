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

type auditRecorderFunc func(ctx context.Context, ev port.AuditEvent) error

func (f auditRecorderFunc) Append(ctx context.Context, ev port.AuditEvent) error { return f(ctx, ev) }
