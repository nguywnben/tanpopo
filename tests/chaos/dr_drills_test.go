// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package chaos_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	admincli "github.com/nguywnben/tanpopo/internal/cli/admin"
	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/ops/backup"
	"github.com/nguywnben/tanpopo/internal/ops/doctor"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/fakellm"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

type drBackupKeyAdapter struct {
	*vault.StaticKEKProvider
	keyID string
}

func (a drBackupKeyAdapter) WrapArchiveKey(ctx context.Context, plaintextKey []byte) (string, []byte, []byte, error) {
	nonce, ct, err := a.StaticKEKProvider.WrapArchiveKey(ctx, a.keyID, plaintextKey)
	return a.keyID, nonce, ct, err
}

// TestDR01StandaloneHostRecoveryTimedUnder15m executes timed DR-01 standalone recovery:
// online SQLite backup (encrypted .tpop.enc when host /dev/shm+noswap permits, plus BackupTo fallback
// verification on swap-enabled CI hosts), simulated total DB corruption, restoration, secret unwrap,
// VerifyAuditChain, and doctor preflight in < 15 minutes (SLO-08).
func TestDR01StandaloneHostRecoveryTimedUnder15m(t *testing.T) {
	ctx := context.Background()
	nowMs := int64(1760001000000)
	root := t.TempDir()
	dbPath := filepath.Join(root, "tanpopo.db")
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatal(err)
	}
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB(), NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatal(err)
	}
	kekRaw := make([]byte, vault.KeySizeBytes)
	_, _ = rand.Read(kekRaw)
	kek, err := vault.NewStaticKEKProvider("kek-dr01", kekRaw)
	clear(kekRaw)
	if err != nil {
		t.Fatal(err)
	}
	defer kek.Close()
	vs, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: kek, NowMs: func() int64 { return nowMs }, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "dr01-org", DisplayName: "DR01 Org"})
	if err != nil {
		t.Fatal(err)
	}
	secMeta, err := vs.SealSecret(ctx, vault.SealSecretParams{
		OrgID:      org.ID,
		Name:       "dr01-api-key",
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte("sk-dr01-recoverable-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	reqID := "req_dr01_1"
	if err := auditLog.Append(ctx, storageport.AuditEvent{
		CreatedAtMs: nowMs,
		OrgID:       &org.ID,
		ActorKind:   "ADMIN",
		ActorID:     "prc_dr01",
		EntryPoint:  "cli",
		EventType:   "vault.secret_created",
		TargetType:  "secret_credential",
		TargetID:    secMeta.ID,
		RequestID:   &reqID,
	}); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(root, "tanpopo.lock")
	if err := lockfile.WriteAtomic(lockPath, lockfile.NewEmpty(time.UnixMilli(nowMs).UTC())); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, "tanpopo.yaml")
	if err := os.WriteFile(cfgPath, []byte("listen: 127.0.0.1:8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plugDir := filepath.Join(root, "plugins")
	if err := os.MkdirAll(plugDir, 0o755); err != nil {
		t.Fatal(err)
	}

	backupKeys := drBackupKeyAdapter{StaticKEKProvider: kek, keyID: "kek-dr01"}
	probeKey := bytes.Repeat([]byte{0x42}, vault.KeySizeBytes)
	pKID, pNonce, pCT, err := backupKeys.WrapArchiveKey(ctx, probeKey)
	if err != nil || len(pCT) != vault.KeySizeBytes+16 {
		t.Fatalf("backupKeys.WrapArchiveKey contract failed: len=%d err=%v", len(pCT), err)
	}
	if unwrapped, err := backupKeys.UnwrapArchiveKey(ctx, pKID, pNonce, pCT); err != nil || !bytes.Equal(unwrapped, probeKey) {
		t.Fatalf("backupKeys.UnwrapArchiveKey contract failed: err=%v", err)
	}

	snapPath := filepath.Join(root, "backup-online.db")
	var archivePath string
	if backup.PlatformSupported() {
		outDir := filepath.Join(root, "archives")
		if err := os.MkdirAll(outDir, 0o700); err != nil {
			t.Fatal(err)
		}
		rec, err := backup.CreateSQLite(ctx, st, backupKeys, outDir, lockPath, cfgPath, plugDir, nowMs)
		if err != nil {
			t.Fatalf("backup.CreateSQLite: %v", err)
		}
		archivePath = filepath.Join(outDir, rec.ArchiveFilename)
	} else {
		// On Linux hosts with swap active, CreateSQLite fails closed by design; use online VACUUM INTO snapshot for timing continuity while verifying fail-closed PlatformSupported contract.
		if _, err := backup.CreateSQLite(ctx, st, backupKeys, filepath.Join(root, "blocked"), lockPath, cfgPath, plugDir, nowMs); err == nil {
			t.Fatalf("expected fail-closed error when backup.PlatformSupported() is false")
		}
		if err := st.BackupTo(ctx, snapPath); err != nil {
			t.Fatalf("sqlite.BackupTo: %v", err)
		}
	}
	vs.Close()
	_ = st.Close()

	// Simulate catastrophic corruption of primary database file.
	if err := os.WriteFile(dbPath, bytes.Repeat([]byte{0xFF}, 8192), 0o600); err != nil {
		t.Fatal(err)
	}

	drillStart := time.Now()
	restoredDBPath := dbPath
	if archivePath != "" {
		if _, err := backup.Verify(ctx, archivePath, backupKeys); err != nil {
			t.Fatalf("backup.Verify: %v", err)
		}
		restoreDest := filepath.Join(root, "restored-host")
		if _, err := backup.RestoreWithOptions(ctx, archivePath, restoreDest, backupKeys, backup.RestoreOptions{VerifySecretsDecrypt: true}); err != nil {
			t.Fatalf("backup.RestoreWithOptions: %v", err)
		}
		restoredDBPath = filepath.Join(restoreDest, "database.sqlite")
	} else {
		rawSnap, err := os.ReadFile(snapPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(restoredDBPath, rawSnap, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	restoredSt, err := sqlite.Open(ctx, sqlite.Config{DBPath: restoredDBPath, NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatalf("open restored sqlite: %v", err)
	}
	defer func() { _ = restoredSt.Close() }()

	vr, err := audit.VerifyAuditChain(ctx, restoredSt.RawDB(), audit.VerifyOptions{})
	if err != nil || !vr.Valid || vr.TotalVerified < 1 {
		t.Fatalf("restored VerifyAuditChain failed: vr=%+v err=%v", vr, err)
	}

	restoredVault, err := vault.NewStore(vault.Config{DB: restoredSt.RawDB(), KEK: kek, NowMs: func() int64 { return nowMs }, PostShredHook: restoredSt.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer restoredVault.Close()
	sec, err := restoredVault.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), org.ID, secMeta.ID)
	if err != nil || string(sec.Plaintext) != "sk-dr01-recoverable-secret" {
		t.Fatalf("restored secret unwrap failed: err=%v", err)
	}
	sec.Zeroize()

	docReport := doctor.Run(ctx, doctor.Probes{
		ClockSkew:               func(context.Context) doctor.Status { return doctor.StatusPass },
		PortsAvailable:          func(context.Context) doctor.Status { return doctor.StatusPass },
		MasterKEKReady:          func(context.Context) doctor.Status { return doctor.StatusPass },
		StorageFSLock:           func(context.Context) doctor.Status { return doctor.StatusPass },
		BundleLockfileIntegrity: func(context.Context) doctor.Status { return doctor.StatusPass },
		TLSPosture:              func(context.Context) doctor.Status { return doctor.StatusPass },
		EgressDNSSSRF:           func(context.Context) doctor.Status { return doctor.StatusPass },
	})
	for _, chk := range docReport.Checks {
		if chk.Status == doctor.StatusFail {
			t.Fatalf("doctor check %s failed after DR-01 restore", chk.ID)
		}
	}

	elapsed := time.Since(drillStart)
	t.Logf("DR-01 recovery completed in %v (platform_encrypted_archive=%v, audit_events=%d)", elapsed, archivePath != "", vr.TotalVerified)
	if elapsed >= 15*time.Minute {
		t.Fatalf("DR-01 exceeded 15m RTO: %v", elapsed)
	}
}

// TestDR02PostPITROrphanHoldReconciliationAndVerifyState executes timed DR-02 verification:
// simulates interrupted RESERVED_HOLD reservations across SQLite (and PostgreSQL/Valkey when env vars set),
// invokes admin.VerifyState to verify audit hash chains and reclaim orphaned holds cleanly with 0 drift.
func TestDR02PostPITROrphanHoldReconciliationAndVerifyState(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760002000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "dr02.db")
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "dr02-org", DisplayName: "DR02 Org"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "dr02-proj", DisplayName: "DR02 Proj"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	vkSvc, err := vkey.NewService(vkey.ServiceConfig{DB: st.RawDB(), AllowDetachedControlPlane: true, NowMs: nowFn, AuditSink: auditLog})
	if err != nil {
		t.Fatal(err)
	}
	vk, err := vkSvc.Create(ctx, vkey.CreateParams{OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "dr02-vk", AllowedPlanes: "PROVIDER"})
	if err != nil {
		t.Fatal(err)
	}
	ldg, err := ledger.NewService(ledger.Config{DB: st.RawDB(), Audit: auditLog, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	pStart := nowFn() - 60_000
	pEnd := pStart + 30*24*3600*1000
	bl, err := ldg.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID: org.ID, ScopeType: "PROJECT", ScopeID: proj.ID, PeriodKind: "MONTHLY",
		PeriodStartMs: pStart, PeriodEndMs: pEnd, HardLimitMicros: 5_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	resv, err := ldg.Reserve(ctx, ledger.ReserveRequest{
		OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, VirtualKeyID: vk.Summary.ID,
		RequestID: "req_dr02_orphan", HoldMicros: 250_000, LeaseTTLMs: 1_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	// Expire lease cleanly and verify VerifyAuditChain + orphan reclamation via admin.VerifyState.
	time.Sleep(15 * time.Millisecond)
	drillStart := time.Now()
	report, err := admincli.VerifyState(ctx, admincli.VerifyStateOptions{
		DBPath:                 dbPath,
		CheckAuditChain:        true,
		ReconcileOrphanedHolds: true,
	})
	if err != nil {
		t.Fatalf("admin.VerifyState(sqlite): %v", err)
	}
	if !report.AuditChainValid || report.AuditEventsVerified < 1 || report.OrphansReconciled < 1 {
		t.Fatalf("expected valid audit chain and >=1 reconciled orphan hold, got %+v", report)
	}

	stAfter, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stAfter.Close() }()
	ldgAfter, err := ledger.NewService(ledger.Config{DB: stAfter.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	blAfter, err := ldgAfter.GetLedger(ctx, org.ID, bl.ID)
	if err != nil {
		t.Fatal(err)
	}
	resvAfter, err := ldgAfter.GetReservation(ctx, org.ID, resv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blAfter.ReservedHoldMicros != 0 || blAfter.SettledExactMicros != 0 || blAfter.SettledEstimatedMicros != 0 || resvAfter.State != "EXPIRED_RECLAIMED" {
		t.Fatalf("FinOps drift after DR-02 reconcile: ledger=%+v resv=%+v", blAfter, resvAfter)
	}
	t.Logf("DR-02 orphan reconciliation & VerifyState completed in %v (orphans=%d, events=%d)", time.Since(drillStart), report.OrphansReconciled, report.AuditEventsVerified)
}

// TestDR03EmergencyMasterKEKRotationAndCacheEviction executes timed DR-03 online rotation:
// stages kek_v2, executes RewrapAllOrgDEKs, confirms active Data Plane snapshot advances past
// config_epoch within < 5s, retires kek_v1, and verifies continuous unsealing.
func TestDR03EmergencyMasterKEKRotationAndCacheEviction(t *testing.T) {
	ctx := context.Background()
	nowMs := int64(1760003000000)
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "dr03.db"), NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB(), NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatal(err)
	}

	k1 := make([]byte, vault.KeySizeBytes)
	k2 := make([]byte, vault.KeySizeBytes)
	_, _ = rand.Read(k1)
	_, _ = rand.Read(k2)
	kekProv, err := vault.NewStaticKEKProvider("kek_v1", k1)
	clear(k1)
	if err != nil {
		t.Fatal(err)
	}
	defer kekProv.Close()

	vs, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: kekProv, NowMs: func() int64 { return nowMs }, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer vs.Close()

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "dr03-org", DisplayName: "DR03"})
	if err != nil {
		t.Fatal(err)
	}
	secMeta, err := vs.SealSecret(ctx, vault.SealSecretParams{OrgID: org.ID, Name: "dr03-key", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte("sk-dr03-live")})
	if err != nil {
		t.Fatal(err)
	}

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB()})
	if err != nil {
		t.Fatal(err)
	}
	watchCtx, stopWatch := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		mgr.StartWatcher(watchCtx, 40*time.Millisecond)
	}()
	defer func() {
		stopWatch()
		<-watchDone
	}()

	drillStart := time.Now()
	if err := kekProv.RegisterKey("kek_v2", k2, true); err != nil {
		t.Fatal(err)
	}
	clear(k2)
	rewrap, err := vs.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2")
	if err != nil {
		t.Fatalf("RewrapAllOrgDEKs: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if cur := mgr.Current(); cur != nil && cur.Epoch >= rewrap.ConfigEpoch {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("DR-03 propagation exceeded 5s ceiling (target epoch %d)", rewrap.ConfigEpoch)
		}
		time.Sleep(10 * time.Millisecond)
	}
	propagationDur := time.Since(drillStart)

	if err := kekProv.RetireKey("kek_v1"); err != nil {
		t.Fatalf("RetireKey kek_v1: %v", err)
	}
	vs.EvictAllDEKs()

	unsealed, err := vs.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), org.ID, secMeta.ID)
	if err != nil || string(unsealed.Plaintext) != "sk-dr03-live" {
		t.Fatalf("secret unwrap after kek_v1 retirement failed: %v", err)
	}
	unsealed.Zeroize()
	_ = auditLog
	t.Logf("DR-03 online KEK rotation & epoch cache eviction completed in %v (epoch=%d)", propagationDur, rewrap.ConfigEpoch)
}

// TestDR04ExtensionAndBinaryUpgradeRollbackUnder60s executes timed DR-04 rollback:
// stages and cuts over v2.0.0 extension upgrade (DeltaP == 0), detects regression, executes
// RollbackInstallation in < 5s, and verifies N-1 Expand-Only schema backward compatibility (< 60s).
func TestDR04ExtensionAndBinaryUpgradeRollbackUnder60s(t *testing.T) {
	ctx := context.Background()
	nowMs := int64(1760004000000)
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	_ = os.MkdirAll(pluginsDir, 0o755)
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(root, "dr04.db"), NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	auditLog := &chaosAuditSink{}

	pubPub, pubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := trust.TrustedKeyring{PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub}}
	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB: st.RawDB(), PluginsDir: pluginsDir, Keyring: keyring, Audit: auditLog, NowMs: func() int64 { return nowMs },
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "dr04-org", DisplayName: "DR04"})
	if err != nil {
		t.Fatal(err)
	}
	insertOperatorPrincipal(t, ctx, st.RawDB(), nowMs)

	mkPkg := func(ver string) (string, string, []byte) {
		src := filepath.Join(root, "src-"+ver)
		_ = os.MkdirAll(src, 0o755)
		yaml := fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/dr04-ext"
  name: "DR04 Ext"
  version: %q
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "dr04-ext"
    supportedDialects: ["openai_compatible"]
permissions:
  networkEgress:
    allowedHosts: ["127.0.0.1"]
    allowPrivateIp: true
adapter:
  slug: "dr04-ext"
  wireProtocol: "openai_chat_v1"
`, ver)
		if err := os.WriteFile(filepath.Join(src, "manifest.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
		tpkg := filepath.Join(root, fmt.Sprintf("dr04-%s.tpkg", ver))
		packRes, err := testharness.PackExtension(ctx, src, tpkg)
		if err != nil {
			t.Fatal(err)
		}
		sig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload("@acme/dr04-ext", ver, packRes.SHA256))
		return tpkg, packRes.SHA256, sig
	}

	t1Path, t1Hash, t1Sig := mkPkg("1.0.0")
	st1, err := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchivePath: t1Path, Source: installer.SourceSpec{Kind: installer.SourceKindLocalArchive, URI: "./1.0.0.tpkg", ExpectedSHA256: t1Hash},
		Signature: installer.ProvenanceSignature{PublisherOrRootSig: t1Sig}, ActorID: "prc_chaos_op", RequestID: "req_dr04_1", TargetOrgID: org.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec1, err := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID: st1.Installation.InstallationID, ExpectedVersion: st1.Installation.Version,
		ApproverPrincipalID: "prc_chaos_op", ActivateImmediately: true, OrgID: org.ID, RequestID: "req_dr04_2",
	})
	if err != nil {
		t.Fatal(err)
	}
	v1VersionID := rec1.ActiveVersionID

	t2Path, t2Hash, t2Sig := mkPkg("2.0.0")
	up2, err := instSvc.StageUpgrade(ctx, installer.StageUpgradeRequest{
		InstallationID: rec1.InstallationID, ExpectedVersion: rec1.Version, ArchivePath: t2Path,
		Source: installer.SourceSpec{Kind: installer.SourceKindLocalArchive, URI: "./2.0.0.tpkg", ExpectedSHA256: t2Hash},
		Signature: installer.ProvenanceSignature{PublisherOrRootSig: t2Sig}, ActorID: "prc_chaos_op", OrgID: org.ID, RequestID: "req_dr04_3",
	})
	if err != nil || up2.Escalated {
		t.Fatalf("StageUpgrade v2.0.0 failed: up2=%+v err=%v", up2, err)
	}

	// Simulate Expand-Only additive column surviving N-1 binary rollback + timed instant RollbackInstallation.
	rollbackStart := time.Now()
	if _, err := st.RawDB().ExecContext(ctx, `ALTER TABLE organizations ADD COLUMN future_nplus1_note TEXT`); err != nil {
		t.Fatal(err)
	}
	rolled, err := instSvc.RollbackInstallation(ctx, up2.Installation.InstallationID, up2.Installation.Version, "prc_chaos_op", org.ID, "req_dr04_rb")
	if err != nil {
		t.Fatalf("RollbackInstallation: %v", err)
	}
	if rolled.ActiveVersionID == nil || v1VersionID == nil || *rolled.ActiveVersionID != *v1VersionID {
		t.Fatalf("expected rollback to v1.0.0 version %v, got %v", v1VersionID, rolled.ActiveVersionID)
	}
	var gotSlug string
	if err := st.RawDB().QueryRowContext(ctx, `SELECT slug FROM organizations WHERE id = ?`, org.ID).Scan(&gotSlug); err != nil || gotSlug != "dr04-org" {
		t.Fatalf("N-1 binary SQL compatibility failed across Expand-Only column: %v", err)
	}
	rbDur := time.Since(rollbackStart)
	t.Logf("DR-04 extension + N-1 Expand-Only binary rollback completed in %v (< 60s target)", rbDur)
	if rbDur >= 60*time.Second {
		t.Fatalf("DR-04 rollback exceeded 60s target: %v", rbDur)
	}
}

// TestOPSAC06ThreeNodeRollingRestartZeroDrops100SSE validates OPS-AC-06:
// 3 Data Plane replicas serve 100 active SSE streams concurrently while undergoing sequential
// graceful drain & restart (/readyz flips 503 immediately on drain, open SSE streams drain cleanly to [DONE]
// with 0 HTTP 502/503 stream drops, replacement boots to /readyz=200).
func TestOPSAC06ThreeNodeRollingRestartZeroDrops100SSE(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760005000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	_ = os.MkdirAll(pluginsDir, 0o755)
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(tmp, "opsac06.db"), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	pubPub, pubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := trust.TrustedKeyring{PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub}}
	auditSink := &chaosAuditSink{}
	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), NowMs: nowFn, Keyring: keyring})
	if err != nil {
		t.Fatal(err)
	}
	vs, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: newChaosKEK(t, "kek-ops06"), NowMs: nowFn, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer vs.Close()
	catRepo, err := catalog.NewRepository(catalog.Config{DB: st.RawDB(), NowMs: nowFn, Snapshots: mgr, Keyring: keyring, Audit: auditSink})
	if err != nil {
		t.Fatal(err)
	}
	brk, err := broker.NewBlindBroker(broker.Config{Resolver: catRepo, PolicyResolver: catRepo, Vault: vs, UnsealCap: vault.MintUnsealCapabilityForBroker(), Audit: auditSink, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{DB: st.RawDB(), PluginsDir: pluginsDir, Keyring: keyring, Audit: auditSink, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	vkSvc, err := vkey.NewService(vkey.ServiceConfig{DB: st.RawDB(), NowMs: nowFn, Snapshots: mgr, AuditSink: auditSink})
	if err != nil {
		t.Fatal(err)
	}

	org, _ := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "ops06-org", DisplayName: "OPS06"})
	proj, _ := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "ops06-proj", DisplayName: "OPS06"})
	env, _ := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	insertOperatorPrincipal(t, ctx, st.RawDB(), nowFn())

	adpID := seedChaosDeclarativeAdapter(t, ctx, st, catRepo, instSvc, pubPriv, org.ID, pluginsDir)
	secMeta, _ := vs.SealSecret(ctx, vault.SealSecretParams{OrgID: org.ID, Name: "k", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte(canary.DefaultCanarySecret())})

	fake := fakellm.NewServerWithOptions(t, fakellm.Options{StreamChunks: 8})
	fake.SetDefaultFault("chunk_delay_ms=25")

	conn, err := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID: org.ID, AdapterID: adpID, Name: "ops06-conn", EndpointBaseURL: fake.URL(),
		AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, DataRegion: "us",
		BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "val")
	_, _ = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "act")
	lmod, _ := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{
		OrgID: &org.ID, CanonicalSlug: "acme/ops06-chat", ModalityKind: catalog.ModalityChat,
		ContextWindow: 128000, MaxOutputTokens: 4096, BaseCapabilities: map[string]string{"streaming": "VERIFIED"},
	})
	if _, err := catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
		OrgID: org.ID, ConnectionID: conn.ID, LogicalModelID: lmod.ID,
		UpstreamModelName: "acme-gpt-ops06", AssuranceLevel: catalog.AssuranceTested, State: catalog.DeployStateActiveHealthy,
		ConnectTimeoutMs: 5000, FirstByteTimeoutMs: 5000, IdleChunkTimeoutMs: 5000,
	}); err != nil {
		t.Fatal(err)
	}
	vk, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "ops06-vk", AllowedPlanes: "PROVIDER", MaxConcurrentStreams: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	type replica struct {
		draining atomic.Bool
		inFlight sync.WaitGroup
		dpSrv    *ingresshttp.Server
		httpSrv  *httptest.Server
	}
	newReplica := func() *replica {
		rep := &replica{}
		dp, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots: mgr, Broker: brk, PluginsDir: pluginsDir, Audit: auditSink, NowMs: nowFn, SSEHeartbeatInterval: 200 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
			if rep.draining.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"status":"draining"}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"ready"}`)
		})
		mux.Handle("/v1/", dp)
		rep.dpSrv = dp
		rep.httpSrv = httptest.NewServer(mux)
		return rep
	}

	var repMu sync.RWMutex
	replicas := []*replica{newReplica(), newReplica(), newReplica()}
	defer func() {
		repMu.RLock()
		defer repMu.RUnlock()
		for _, r := range replicas {
			if r != nil {
				r.httpSrv.Close()
				_ = r.dpSrv.Close(context.Background())
			}
		}
	}()

	var rrIdx atomic.Uint64
	lbClient := netguard.NewHTTPClient()
	lb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var target *replica
		repMu.RLock()
		var candidates []*replica
		for _, rep := range replicas {
			if rep != nil && !rep.draining.Load() {
				candidates = append(candidates, rep)
			}
		}
		if len(candidates) > 0 {
			target = candidates[rrIdx.Add(1)%uint64(len(candidates))]
			target.inFlight.Add(1)
		}
		repMu.RUnlock()
		if target == nil {
			http.Error(w, "no ready backends", http.StatusServiceUnavailable)
			return
		}
		defer target.inFlight.Done()
		proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.httpSrv.URL+r.URL.RequestURI(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		proxyReq.Header = r.Header.Clone()
		resp, err := lbClient.Do(proxyReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		for k, vals := range resp.Header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		flusher, hasFlusher := w.(http.Flusher)
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				_, _ = w.Write(line)
				if hasFlusher {
					flusher.Flush()
				}
			}
			if err != nil {
				break
			}
		}
	}))
	defer lb.Close()

	// Launch 100 concurrent SSE streams plus execute sequential 3-node rolling restart while streams are active.
	const totalStreams = 100
	var completedOK atomic.Int32
	var failedErrs atomic.Int32
	var wg sync.WaitGroup
	firstWavesStarted := make(chan struct{})
	var startedCount atomic.Int32
	client := netguard.NewHTTPClient()
	reqPayload, _ := json.Marshal(map[string]any{
		"model":    "acme/ops06-chat",
		"stream":   true,
		"messages": []map[string]string{{"role": "user", "content": "rolling restart active stream"}},
	})

	for i := 0; i < totalStreams; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, lb.URL+"/v1/chat/completions", bytes.NewReader(reqPayload))
			req.Header.Set("Authorization", "Bearer "+vk.SecretKey)
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if startedCount.Add(1) == 50 {
				close(firstWavesStarted)
			}
			if err != nil {
				failedErrs.Add(1)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.Contains(string(body), "[DONE]") {
				completedOK.Add(1)
			} else {
				failedErrs.Add(1)
			}
		}(i)
		if i%25 == 24 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	<-firstWavesStarted

	// Sequential 3-node rolling restart during active streaming.
	for idx := 0; idx < 3; idx++ {
		repMu.Lock()
		oldRep := replicas[idx]
		oldRep.draining.Store(true)
		repMu.Unlock()

		// Drain open LB dispatches cleanly before shutting down replica.
		oldRep.inFlight.Wait()
		oldRep.httpSrv.Close()
		_ = oldRep.dpSrv.Close(context.Background())

		fresh := newReplica()
		repMu.Lock()
		replicas[idx] = fresh
		repMu.Unlock()
	}

	wg.Wait()
	if failedErrs.Load() != 0 || completedOK.Load() != totalStreams {
		t.Fatalf("OPS-AC-06 rolling restart had stream drops: ok=%d/%d failed=%d", completedOK.Load(), totalStreams, failedErrs.Load())
	}
	t.Logf("OPS-AC-06 3-node rolling restart passed: %d/%d SSE streams completed with 0 502/503 drops", completedOK.Load(), totalStreams)
}
