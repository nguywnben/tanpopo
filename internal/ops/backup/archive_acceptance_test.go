// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
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
