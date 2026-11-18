// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func TestOnlineBackupRestoresLedgerAndAuditDuringConcurrentWriters(t *testing.T) {
	if !PlatformSupported() {
		if _, err := Verify(context.Background(), "unsupported-platform-probe", newArchiveTestKeys()); err == nil || err.Error() != PlatformSupportUnavailableReason() {
			t.Fatalf("backup platform must fail closed: %v", err)
		}
		return
	}
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "tanpopo.db")
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger, err := audit.NewLogger(audit.LoggerConfig{DB: store.RawDB()})
	if err != nil {
		t.Fatal(err)
	}
	ledgerService, err := ledger.NewService(ledger.Config{DB: store.RawDB(), Audit: logger})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "tanpopo.lock")
	if err := lockfile.WriteAtomic(lockPath, lockfile.NewEmpty(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "tanpopo.yaml")
	if err := os.WriteFile(configPath, []byte("data_dir: ./data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifactDir := filepath.Join(root, "plugins")
	if err := os.Mkdir(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}

	const workers = 20
	start := make(chan struct{})
	ready := make(chan struct{}, workers)
	firstWrite := make(chan error, workers)
	stop := make(chan struct{})
	var writers sync.WaitGroup
	var failures sync.Map
	for worker := 0; worker < workers; worker++ {
		writers.Add(1)
		go func(worker int) {
			defer writers.Done()
			ready <- struct{}{}
			<-start
			for write := 0; ; write++ {
				select {
				case <-stop:
					return
				default:
				}
				requestID := fmt.Sprintf("bk_%02d_%02d", worker, write)
				_, settleErr := ledgerService.Settle(ctx, ledger.SettleRequest{
					OrgID: "org_backup_ac1", RequestID: requestID, ProjectID: "prj_backup_ac1",
					EnvID: "env_backup_ac1", VirtualKeyID: "vky_backup_ac1", Plane: ledger.PlaneProvider,
					Rate: pricing.RateCard{InputPer1MNanoUSD: 1_000_000, OutputPer1MNanoUSD: 2_000_000, BillingMode: pricing.BillingModeCommercialAPI},
					Usage: pricing.UsageInput{InputTokens: 100, OutputTokens: 25, UpstreamUsagePresent: true, UpstreamAccuracy: pricing.AccuracyExact},
				})
				if settleErr != nil {
					failures.Store(requestID, settleErr)
					if write == 0 {
						firstWrite <- settleErr
					}
					return
				}
				orgID, auditRequest := "org_backup_ac1", requestID
				if auditErr := logger.Append(ctx, port.AuditEvent{
					OrgID: &orgID, ActorKind: "OPERATOR", ActorID: "backup-acceptance",
					EntryPoint: "cli", EventType: "backup.acceptance.write", TargetType: "settlement",
					TargetID: requestID, RequestID: &auditRequest,
				}); auditErr != nil {
					failures.Store(requestID, auditErr)
					if write == 0 {
						firstWrite <- auditErr
					}
					return
				}
				if write == 0 {
					firstWrite <- nil
				}
				select {
				case <-stop:
					return
				default:
				}
			}
		}(worker)
	}
	for i := 0; i < workers; i++ {
		<-ready
	}
	close(start)
	for i := 0; i < workers; i++ {
		if writeErr := <-firstWrite; writeErr != nil {
			close(stop)
			writers.Wait()
			t.Fatalf("concurrent writer failed before online snapshot: %v", writeErr)
		}
	}

	archiveDir := filepath.Join(root, "backups")
	receipt, err := CreateSQLite(ctx, store, newArchiveTestKeys(), archiveDir, lockPath, configPath, artifactDir, time.Now().UTC().UnixMilli())
	close(stop)
	writers.Wait()
	if err != nil {
		t.Fatalf("online CreateSQLite: %v", err)
	}
	failures.Range(func(key, value any) bool {
		t.Errorf("concurrent writer %v failed: %v", key, value)
		return true
	})

	archivePath := filepath.Join(archiveDir, receipt.ArchiveFilename)
	if _, err := Verify(ctx, archivePath, newArchiveTestKeys()); err != nil {
		t.Fatalf("Verify created archive: %v", err)
	}
	destination := filepath.Join(root, "restored")
	if _, err := Restore(ctx, archivePath, destination, newArchiveTestKeys()); err != nil {
		t.Fatalf("Restore created archive: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(destination, "database.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var settlements, audits int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_settlements").Scan(&settlements); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if settlements < workers || audits < workers {
		t.Fatalf("restored concurrent snapshot lost committed ledger/audit rows: settlements=%d audit_events=%d want at least %d of each", settlements, audits, workers)
	}
	verified, err := audit.VerifyAuditChain(ctx, db, audit.VerifyOptions{})
	if err != nil || !verified.Valid {
		t.Fatalf("restored audit chain is invalid: report=%+v err=%v", verified, err)
	}
}

func TestEncryptedArchiveRestoreTracksProjectShredAndKEKRetirement(t *testing.T) {
	if !PlatformSupported() {
		return
	}
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(root, "tanpopo.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	org, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "archive-shred-org", DisplayName: "Archive Shred Org"})
	if err != nil {
		t.Fatal(err)
	}
	p1, err := store.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "archive-shred-p1", DisplayName: "Project P1"})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := store.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "archive-shred-p2", DisplayName: "Project P2"})
	if err != nil {
		t.Fatal(err)
	}
	kekV1, err := vault.NewStaticKEKProvider("kek_v1", []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	defer kekV1.Close()
	keyVault, err := vault.NewStore(vault.Config{DB: store.RawDB(), KEK: kekV1, PostShredHook: store.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer keyVault.Close()
	p1Key, err := keyVault.EnsureProjectDEK(ctx, org.ID, p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keyVault.EnsureProjectDEK(ctx, org.ID, p2.ID); err != nil {
		t.Fatal(err)
	}
	p1Secret, err := keyVault.SealSecret(ctx, vault.SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &p1.ID, Name: "P1 credential", SecretKind: "API_KEY", Plaintext: []byte("p1-sensitive-value"),
	})
	if err != nil {
		t.Fatal(err)
	}
	p2Secret, err := keyVault.SealSecret(ctx, vault.SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &p2.ID, Name: "P2 credential", SecretKind: "API_KEY", Plaintext: []byte("p2-survives"),
	})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "tanpopo.lock")
	if err := lockfile.WriteAtomic(lockPath, lockfile.NewEmpty(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "tanpopo.yaml")
	if err := os.WriteFile(configPath, []byte("data_dir: ./data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifactDir := filepath.Join(root, "plugins")
	if err := os.Mkdir(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keys := keyVault
	preDir := filepath.Join(root, "pre-shred-backups")
	pre, err := CreateSQLite(ctx, store, keys, preDir, lockPath, configPath, artifactDir, time.Now().UTC().UnixMilli())
	if err != nil {
		t.Fatalf("create pre-shred encrypted archive: %v", err)
	}
	started := time.Now()
	if err := keyVault.ShredProjectDEK(ctx, org.ID, p1.ID, vault.ShredRequest{RequestID: "archive-shred-p1", ExpectedGeneration: p1Key.KeyGeneration}); err != nil {
		t.Fatalf("shred Project P1: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 60*time.Second {
		t.Fatalf("project shred exceeded the 60-second bound: %s", elapsed)
	}
	if _, err := keyVault.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), org.ID, p1Secret.ID); err == nil {
		t.Fatal("live P1 secret remained readable after project DEK shred")
	}
	openedP2, err := keyVault.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), org.ID, p2Secret.ID)
	if err != nil {
		t.Fatalf("sibling Project P2 secret failed after P1 shred: %v", err)
	}
	if !bytes.Equal(openedP2.Plaintext, []byte("p2-survives")) {
		t.Fatal("sibling Project P2 secret plaintext changed after P1 shred")
	}
	openedP2.Zeroize()

	postDir := filepath.Join(root, "post-shred-backups")
	post, err := CreateSQLite(ctx, store, keys, postDir, lockPath, configPath, artifactDir, time.Now().UTC().UnixMilli())
	if err != nil {
		t.Fatalf("create post-shred encrypted archive: %v", err)
	}
	postArchive := filepath.Join(postDir, post.ArchiveFilename)
	if _, err := Verify(ctx, postArchive, keys); err != nil {
		t.Fatalf("verify post-shred archive: %v", err)
	}
	postRestore := filepath.Join(root, "post-shred-restore")
	if _, err := Restore(ctx, postArchive, postRestore, keys); err != nil {
		t.Fatalf("restore post-shred archive: %v", err)
	}
	restoredStore, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(postRestore, "database.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer restoredStore.Close()
	restoredVault, err := vault.NewStore(vault.Config{DB: restoredStore.RawDB(), KEK: kekV1, PostShredHook: restoredStore.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer restoredVault.Close()
	if _, err := restoredVault.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), org.ID, p1Secret.ID); err == nil {
		t.Fatal("restored post-shred archive contains a readable P1 secret")
	}
	restoredP2, err := restoredVault.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), org.ID, p2Secret.ID)
	if err != nil {
		t.Fatalf("restored sibling P2 secret is unreadable: %v", err)
	}
	restoredP2.Zeroize()

	if err := kekV1.RegisterKey("kek_v2", []byte("abcdef0123456789abcdef0123456789"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := keyVault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2"); err != nil {
		t.Fatalf("rewrap organization DEKs for DR-03: %v", err)
	}
	if err := kekV1.RetireKey("kek_v1"); err != nil {
		t.Fatalf("retire old KEK: %v", err)
	}
	if _, err := Verify(ctx, filepath.Join(preDir, pre.ArchiveFilename), keys); err == nil {
		t.Fatal("historical pre-shred archive remained verifiable after old KEK retirement")
	}
	rotatedP2, err := keyVault.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), org.ID, p2Secret.ID)
	if err != nil {
		t.Fatalf("P2 live secret failed after KEK rotation and retirement: %v", err)
	}
	rotatedP2.Zeroize()
}
