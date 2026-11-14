// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func openBootstrapDB(t *testing.T, fk *clock.FakeClock) *sqlite.Store {
	t.Helper()
	st, err := sqlite.Open(context.Background(), sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "bootstrap.db"),
		NowMs:  func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestBootstrapOneTimeConsumeAndBurn(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openBootstrapDB(t, fk)
	ctx := context.Background()

	tokenPath := filepath.Join(t.TempDir(), "boot", "bootstrap.token")
	mgr, err := NewManager(ManagerConfig{
		DB:        st.RawDB(),
		TokenPath: tokenPath,
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2:    auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	token, err := mgr.EnsureToken(ctx)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	if !strings.HasPrefix(token, TokenPrefix) || len(token) < MinBootstrapTokenLen {
		t.Fatalf("unexpected bootstrap token format/len: len=%d", len(token))
	}

	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("stat tokenPath: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions on bootstrap token file, got %o", info.Mode().Perm())
	}

	// Wrong token rejected fail-closed without burning bootstrap.
	if _, err := mgr.Complete(ctx, CompleteRequest{
		BootstrapToken:              strings.Repeat("x", 40),
		OwnerEmail:                  "admin@tanpopo.local",
		OwnerPassword:               "strong-owner-password-2026",
		MasterKEKBackupAcknowledged: true,
	}); !errors.Is(err, ErrInvalidBootstrapToken) {
		t.Fatalf("wrong bootstrap token expected ErrInvalidBootstrapToken, got %v", err)
	}

	// Valid AC-1 completion succeeds once and provisions OPERATOR + ORG_OWNER.
	res, err := mgr.Complete(ctx, CompleteRequest{
		BootstrapToken:              token,
		OwnerEmail:                  "Admin@Tanpopo.Local",
		OwnerPassword:               "strong-owner-password-2026",
		OrgName:                     "Acme AI",
		ProjectName:                 "core-gateway",
		MasterKEKBackupAcknowledged: true,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if res.OrgID == "" || res.ProjectID == "" || res.EnvID == "" || res.OwnerPrincipalID == "" {
		t.Fatalf("expected non-empty provisioned IDs, got %+v", res)
	}

	// Token file must be permanently burned on disk.
	if _, statErr := os.Stat(tokenPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected token file deleted after bootstrap completion, stat err=%v", statErr)
	}

	// AC-1 / AUTH-02: Second call presenting the same Bootstrap Token returns ErrBootstrapAlreadyCompleted (410).
	if _, err := mgr.Complete(ctx, CompleteRequest{
		BootstrapToken:              token,
		OwnerEmail:                  "attacker@tanpopo.local",
		OwnerPassword:               "attacker-password-1234",
		MasterKEKBackupAcknowledged: true,
	}); !errors.Is(err, ErrBootstrapAlreadyCompleted) {
		t.Fatalf("replay Complete expected ErrBootstrapAlreadyCompleted, got %v", err)
	}
	if _, err := mgr.EnsureToken(ctx); !errors.Is(err, ErrBootstrapAlreadyCompleted) {
		t.Fatalf("EnsureToken after bootstrap expected ErrBootstrapAlreadyCompleted, got %v", err)
	}

	// Verify Personal Admin role bindings + local login work end-to-end.
	authSvc, err := auth.NewLocalService(auth.ServiceConfig{
		DB:     st.RawDB(),
		NowMs:  func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2: auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
	})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	sess, _, err := authSvc.Login(ctx, "127.0.0.1", "admin@tanpopo.local", "strong-owner-password-2026")
	if err != nil {
		t.Fatalf("owner login after bootstrap failed: %v", err)
	}
	actor := &rbac.Actor{
		PrincipalID:     sess.PrincipalID,
		SessionOrgID:    sess.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminSession,
		Bindings:        sess.Bindings,
	}
	if err := rbac.Authorize(actor, rbac.ActionOrgCreate, rbac.TargetScope{}); err != nil {
		t.Fatalf("bootstrapped owner lacks OPERATOR ActionOrgCreate: %v", err)
	}
	if err := rbac.Authorize(actor, rbac.ActionOrgDelete, rbac.TargetScope{OrgID: res.OrgID}); err != nil {
		t.Fatalf("bootstrapped owner lacks ORG_OWNER ActionOrgDelete: %v", err)
	}

	_ = port.ErrInvalidInput
}

func TestBootstrapConcurrentSingleWinnerRace(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openBootstrapDB(t, fk)
	ctx := context.Background()

	tokenPath := filepath.Join(t.TempDir(), "race.token")
	mgr, err := NewManager(ManagerConfig{
		DB:        st.RawDB(),
		TokenPath: tokenPath,
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2:    auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	token, err := mgr.EnsureToken(ctx)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	const workers = 6
	var (
		wins    atomic.Int32
		replays atomic.Int32
		wg      sync.WaitGroup
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			_, cErr := mgr.Complete(ctx, CompleteRequest{
				BootstrapToken:              token,
				OwnerEmail:                  "owner@race.local",
				OwnerPassword:               "strong-race-password-2026",
				MasterKEKBackupAcknowledged: true,
			})
			switch {
			case cErr == nil:
				wins.Add(1)
			case errors.Is(cErr, ErrBootstrapAlreadyCompleted):
				replays.Add(1)
			default:
				t.Errorf("worker %d unexpected err: %v", idx, cErr)
			}
		}(i)
	}
	wg.Wait()

	if wins.Load() != 1 || replays.Load() != workers-1 {
		t.Fatalf("expected 1 winner and %d replays, got wins=%d replays=%d", workers-1, wins.Load(), replays.Load())
	}
}

type captureAuditSink struct {
	events []port.AuditEvent
}

func (c *captureAuditSink) Append(_ context.Context, ev port.AuditEvent) error {
	c.events = append(c.events, ev)
	return nil
}

func TestBootstrapReloadValidationAndInsecurePermissions(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openBootstrapDB(t, fk)
	ctx := context.Background()

	if _, err := NewManager(ManagerConfig{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("nil DB expected ErrInvalidInput, got %v", err)
	}
	if _, err := NewManager(ManagerConfig{DB: st.RawDB()}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("empty TokenPath expected ErrInvalidInput, got %v", err)
	}

	tokenPath := filepath.Join(t.TempDir(), "persist.token")
	sink := &captureAuditSink{}
	mgr1, err := NewManager(ManagerConfig{
		DB:        st.RawDB(),
		TokenPath: tokenPath,
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2:    auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
		AuditSink: sink,
	})
	if err != nil {
		t.Fatalf("NewManager 1: %v", err)
	}
	tok1, err := mgr1.EnsureToken(ctx)
	if err != nil {
		t.Fatalf("EnsureToken 1: %v", err)
	}

	// Simulate process restart prior to bootstrap completion -> loads exact same 0600 token.
	mgr2, err := NewManager(ManagerConfig{
		DB:        st.RawDB(),
		TokenPath: tokenPath,
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2:    auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
		AuditSink: sink,
	})
	if err != nil {
		t.Fatalf("NewManager 2: %v", err)
	}
	if booted, err := mgr2.IsBootstrapped(ctx); err != nil || booted {
		t.Fatalf("expected unbootstrapped prior to completion, got booted=%v err=%v", booted, err)
	}
	tok2, err := mgr2.EnsureToken(ctx)
	if err != nil || tok2 != tok1 {
		t.Fatalf("expected persisted token reload match, got tok2=%q err=%v", tok2, err)
	}

	// Enforce master_kek_backup_acknowledged == true and valid email/password length.
	if _, err := mgr2.Complete(ctx, CompleteRequest{
		BootstrapToken:              tok2,
		OwnerEmail:                  "admin@tanpopo.local",
		OwnerPassword:               "strong-owner-password-2026",
		MasterKEKBackupAcknowledged: false,
	}); !errors.Is(err, ErrMasterKEKAckRequired) {
		t.Fatalf("unacknowledged KEK expected ErrMasterKEKAckRequired, got %v", err)
	}
	if _, err := mgr2.Complete(ctx, CompleteRequest{
		BootstrapToken:              tok2,
		OwnerEmail:                  "not-an-email",
		OwnerPassword:               "strong-owner-password-2026",
		MasterKEKBackupAcknowledged: true,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("bad email expected ErrInvalidInput, got %v", err)
	}

	// On Unix/macOS, chmod 0644 on token file must fail closed with ErrInsecureTokenPermissions.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tokenPath, 0o644); err != nil {
			t.Fatalf("chmod 0644: %v", err)
		}
		if _, err := mgr2.EnsureToken(ctx); !errors.Is(err, ErrInsecureTokenPermissions) {
			t.Fatalf("0644 token file expected ErrInsecureTokenPermissions, got %v", err)
		}
		_ = os.Chmod(tokenPath, 0o600)
	}

	if _, err := mgr2.Complete(ctx, CompleteRequest{
		BootstrapToken:              tok2,
		OwnerEmail:                  "admin@tanpopo.local",
		OwnerPassword:               "strong-owner-password-2026",
		MasterKEKBackupAcknowledged: true,
	}); err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if booted, err := mgr2.IsBootstrapped(ctx); err != nil || !booted {
		t.Fatalf("expected IsBootstrapped=true after completion, got %v err=%v", booted, err)
	}
	if len(sink.events) != 1 || sink.events[0].OrgID != nil || sink.events[0].EventType != "instance.bootstrap_completed" {
		t.Fatalf("expected SYSTEM bootstrap audit event, got %+v", sink.events)
	}

	// Verify nil-manager guards and short-password rejection on Complete.
	var nilMgr *Manager
	if _, err := nilMgr.IsBootstrapped(ctx); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("nil manager IsBootstrapped expected ErrInvalidInput, got %v", err)
	}
	if _, err := nilMgr.EnsureToken(ctx); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("nil manager EnsureToken expected ErrInvalidInput, got %v", err)
	}
	if _, err := nilMgr.Complete(ctx, CompleteRequest{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("nil manager Complete expected ErrInvalidInput, got %v", err)
	}

	// Verify fresh DB manager rejects short password (< 12 chars) without burning token.
	st3 := openBootstrapDB(t, fk)
	mgr3, err := NewManager(ManagerConfig{
		DB:        st3.RawDB(),
		TokenPath: filepath.Join(t.TempDir(), "short.token"),
		Argon2:    auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
	})
	if err != nil {
		t.Fatalf("NewManager 3: %v", err)
	}
	tok3, err := mgr3.EnsureToken(ctx)
	if err != nil {
		t.Fatalf("EnsureToken 3: %v", err)
	}
	if _, err := mgr3.Complete(ctx, CompleteRequest{
		BootstrapToken:              tok3,
		OwnerEmail:                  "short@tanpopo.local",
		OwnerPassword:               "shortpass",
		MasterKEKBackupAcknowledged: true,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("short owner_password expected ErrInvalidInput, got %v", err)
	}
	if _, err := mgr3.Complete(ctx, CompleteRequest{
		BootstrapToken:              tok3,
		OwnerEmail:                  "owner3@tanpopo.local",
		OwnerPassword:               "valid-owner-password-2026!",
		OrgName:                     "!!!",
		ProjectName:                 "---",
		MasterKEKBackupAcknowledged: true,
	}); err != nil {
		t.Fatalf("Complete with fallback slugs failed: %v", err)
	}
}

func TestBootstrapWrongTokenThrottlingAndMultiInstanceRace(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openBootstrapDB(t, fk)
	ctx := context.Background()

	sink := &captureAuditSink{}
	tokenPath := filepath.Join(t.TempDir(), "multi.token")
	mgrA, err := NewManager(ManagerConfig{
		DB:        st.RawDB(),
		TokenPath: tokenPath,
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2:    auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
		AuditSink: sink,
	})
	if err != nil {
		t.Fatalf("NewManager A: %v", err)
	}
	mgrB, err := NewManager(ManagerConfig{
		DB:        st.RawDB(),
		TokenPath: tokenPath,
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2:    auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
		AuditSink: sink,
	})
	if err != nil {
		t.Fatalf("NewManager B: %v", err)
	}

	tok, err := mgrA.EnsureToken(ctx)
	if err != nil {
		t.Fatalf("EnsureToken A: %v", err)
	}

	// Exhaust 10 invalid token guesses -> 11th triggers ErrBootstrapRateLimited with audit event.
	for i := 0; i < 10; i++ {
		if _, err := mgrA.Complete(ctx, CompleteRequest{
			BootstrapToken:              strings.Repeat("z", 40),
			OwnerEmail:                  "admin@tanpopo.local",
			OwnerPassword:               "valid-owner-password-2026!",
			MasterKEKBackupAcknowledged: true,
		}); !errors.Is(err, ErrInvalidBootstrapToken) {
			t.Fatalf("attempt %d expected ErrInvalidBootstrapToken, got %v", i, err)
		}
	}
	if _, err := mgrA.Complete(ctx, CompleteRequest{
		BootstrapToken:              tok,
		OwnerEmail:                  "admin@tanpopo.local",
		OwnerPassword:               "valid-owner-password-2026!",
		MasterKEKBackupAcknowledged: true,
	}); !errors.Is(err, ErrBootstrapRateLimited) {
		t.Fatalf("11th attempt expected ErrBootstrapRateLimited, got %v", err)
	}

	// Concurrent/multi-instance Complete using mgrB wins and mgrA replay returns ErrBootstrapAlreadyCompleted (410).
	if _, err := mgrB.Complete(ctx, CompleteRequest{
		BootstrapToken:              tok,
		OwnerEmail:                  "ownerB@tanpopo.local",
		OwnerPassword:               "valid-owner-password-2026!",
		MasterKEKBackupAcknowledged: true,
	}); err != nil {
		t.Fatalf("mgrB Complete failed: %v", err)
	}
	if _, err := mgrA.Complete(ctx, CompleteRequest{
		BootstrapToken:              tok,
		OwnerEmail:                  "ownerA@tanpopo.local",
		OwnerPassword:               "valid-owner-password-2026!",
		MasterKEKBackupAcknowledged: true,
	}); !errors.Is(err, ErrBootstrapAlreadyCompleted) {
		t.Fatalf("mgrA after mgrB Complete expected ErrBootstrapAlreadyCompleted, got %v", err)
	}
}
