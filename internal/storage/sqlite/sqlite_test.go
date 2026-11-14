// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	sqlitemigrations "github.com/nguywnben/tanpopo/db/migrations/sqlite"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/migrate"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func openTempStore(t *testing.T, fc *clock.FakeClock) *Store {
	t.Helper()
	if fc == nil {
		fc = clock.NewFakeClock(time.Time{})
	}
	dbPath := filepath.Join(t.TempDir(), "data", "tanpopo.db")
	st, err := Open(context.Background(), Config{
		DBPath: dbPath,
		NowMs: func() int64 {
			return fc.Now().UnixMilli()
		},
	})
	if err != nil {
		t.Fatalf("Open(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSQLiteOpenWALPragmasMigrationAndChangelogAC1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fc := clock.NewFakeClock(time.Date(2026, 11, 14, 18, 0, 0, 0, time.UTC))

	dbPath := filepath.Join(t.TempDir(), "empty", "tanpopo.db")
	start := time.Now()
	st, err := Open(ctx, Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Open empty dir: %v", err)
	}
	defer st.Close()

	if err := st.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	t.Logf("AC-1 cold SQLite WAL open + 0001_foundation.sql applied in %v", elapsed)

	// Verify active SQLite pragmas.
	var jm string
	if err := st.RawDB().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&jm); err != nil || !strings.EqualFold(jm, "wal") {
		t.Fatalf("expected journal_mode=wal, got %q (err=%v)", jm, err)
	}
	var fk int
	if err := st.RawDB().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d (err=%v)", fk, err)
	}
	var secDel int
	if err := st.RawDB().QueryRowContext(ctx, "PRAGMA secure_delete").Scan(&secDel); err != nil || secDel != 1 {
		t.Fatalf("expected secure_delete=1, got %d (err=%v)", secDel, err)
	}

	// Create Org -> Project -> Env and confirm config_epoch increments atomically.
	reg := "EU"
	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		Slug:                    "acme-ai",
		DisplayName:             "Acme AI",
		DefaultDataRegion:       &reg,
		MinPluginTrust:          port.MinTrustVerifiedOrEnterpriseSigned,
		RequireFourEyesApproval: true,
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	prj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{
		Slug:           "core-gateway",
		DisplayName:    "Core Gateway",
		EnforcedRegion: &reg,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, prj.ID, port.CreateEnvironmentParams{
		Slug:              "prod",
		RequireTestedCaps: true,
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if !env.RequireTestedCaps || env.Version != 1 {
		t.Fatalf("unexpected created env: %+v", env)
	}

	epoch, err := st.Changelog().LatestEpoch(ctx)
	if err != nil || epoch != 3 {
		t.Fatalf("expected LatestEpoch=3 after Org->Proj->Env, got %d (err=%v)", epoch, err)
	}
	entries, err := st.Changelog().ListSince(ctx, 0, 10)
	if err != nil || len(entries) != 3 {
		t.Fatalf("expected 3 changelog entries, got %d (err=%v)", len(entries), err)
	}
	if entries[0].EntityKind != "organization" || entries[1].EntityKind != "project" || entries[2].EntityKind != "environment" {
		t.Fatalf("unexpected changelog kinds: %+v", entries)
	}
}

func TestSQLiteOptimisticConcurrencyAndZeroCrossOrgLeakageAC3(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTempStore(t, nil)

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-a", DisplayName: "Org A"})
	if err != nil {
		t.Fatalf("CreateOrg A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-b", DisplayName: "Org B"})
	if err != nil {
		t.Fatalf("CreateOrg B: %v", err)
	}

	projA, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "alpha", DisplayName: "Alpha"})
	if err != nil {
		t.Fatalf("CreateProject A: %v", err)
	}
	projB, err := st.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "beta", DisplayName: "Beta"})
	if err != nil {
		t.Fatalf("CreateProject B: %v", err)
	}
	envB, err := st.Tenancy().CreateEnvironment(ctx, orgB.ID, projB.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatalf("CreateEnv B: %v", err)
	}

	// Concurrent UpdateProject with expectedVersion=1 -> exactly 1 succeeds, 1 gets ErrVersionConflict.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	updated := make([]*port.Project, 2)
	startBarrier := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startBarrier
			name := "Alpha Updated"
			updated[idx], errs[idx] = st.Tenancy().UpdateProject(ctx, orgA.ID, projA.ID, 1, port.UpdateProjectParams{
				DisplayName: &name,
			})
		}(i)
	}
	close(startBarrier)
	wg.Wait()

	successCount := 0
	conflictCount := 0
	for i := 0; i < 2; i++ {
		switch {
		case errs[i] == nil:
			successCount++
			if updated[i].Version != 2 {
				t.Fatalf("expected winning UpdateProject version=2, got %d", updated[i].Version)
			}
		case errors.Is(errs[i], port.ErrVersionConflict):
			conflictCount++
		default:
			t.Fatalf("unexpected concurrent update error[%d]: %v", i, errs[i])
		}
	}
	if successCount != 1 || conflictCount != 1 {
		t.Fatalf("expected 1 success + 1 ErrVersionConflict, got success=%d conflict=%d errs=%v", successCount, conflictCount, errs)
	}

	// Cross-tenant lookups and mutations uniformly return ErrNotFound (0 cross-org leakage).
	if _, err := st.Tenancy().GetProject(ctx, orgA.ID, projB.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org GetProject expected ErrNotFound, got %v", err)
	}
	if _, err := st.Tenancy().GetProjectBySlug(ctx, orgA.ID, projB.Slug); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org GetProjectBySlug expected ErrNotFound, got %v", err)
	}
	badName := "Hijack"
	if _, err := st.Tenancy().UpdateProject(ctx, orgA.ID, projB.ID, 1, port.UpdateProjectParams{DisplayName: &badName}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org UpdateProject expected ErrNotFound, got %v", err)
	}
	if err := st.Tenancy().DeleteProject(ctx, orgA.ID, projB.ID, 1); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org DeleteProject expected ErrNotFound, got %v", err)
	}
	if _, err := st.Tenancy().GetEnvironment(ctx, orgA.ID, envB.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org GetEnvironment expected ErrNotFound, got %v", err)
	}
	newSlug := "staging"
	if _, err := st.Tenancy().UpdateEnvironment(ctx, orgA.ID, envB.ID, 1, port.UpdateEnvironmentParams{Slug: &newSlug}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org UpdateEnvironment expected ErrNotFound, got %v", err)
	}
	if err := st.Tenancy().DeleteEnvironment(ctx, orgA.ID, envB.ID, 1); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org DeleteEnvironment expected ErrNotFound, got %v", err)
	}
}

func TestSQLiteCompositeForeignKeyPreventsCrossOrgWiringNEG1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTempStore(t, nil)

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fk-org-a", DisplayName: "FK A"})
	if err != nil {
		t.Fatalf("CreateOrg A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fk-org-b", DisplayName: "FK B"})
	if err != nil {
		t.Fatalf("CreateOrg B: %v", err)
	}
	projB, err := st.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "proj-b", DisplayName: "Proj B"})
	if err != nil {
		t.Fatalf("CreateProject B: %v", err)
	}

	// 1. Repository attempt to insert Environment in Org_A referencing Proj_B must fail with ErrNotFound (zero oracle).
	if _, err := st.Tenancy().CreateEnvironment(ctx, orgA.ID, projB.ID, port.CreateEnvironmentParams{Slug: "prod"}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected repository CreateEnvironment with mismatched org_id/project_id to return ErrNotFound, got %v", err)
	}

	// 2. Direct raw SQL bypassing Go repositories MUST also be rejected at DB layer by composite FK (org_id, project_id).
	envID := idgen.MustNew(idgen.PrefixEnv)
	_, rawErr := st.RawDB().ExecContext(ctx,
		`INSERT INTO environments (id, org_id, project_id, slug, require_tested_caps, version, created_at_ms, updated_at_ms) VALUES (?, ?, ?, 'prod', 0, 1, 1000, 1000)`,
		envID, orgA.ID, projB.ID,
	)
	if rawErr == nil || !strings.Contains(rawErr.Error(), "FOREIGN KEY constraint failed") {
		t.Fatalf("expected DB composite FOREIGN KEY constraint error, got: %v", rawErr)
	}
}

func TestSQLiteMigrationTamperAndDirtyExit31NEG2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "neg2.db")

	st, err := Open(ctx, Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("initial Open: %v", err)
	}
	_ = st.Close()

	// 1. Mutate 1 byte in 0001_foundation.sql -> Open halts with ErrDirtyMigration (exit 31).
	origBytes, err := sqlitemigrations.FS.ReadFile("0001_foundation.sql")
	if err != nil {
		t.Fatalf("read embedded 0001_foundation.sql: %v", err)
	}
	mutatedFS := fstest.MapFS{
		"0001_foundation.sql": &fstest.MapFile{
			Data: append(append([]byte{}, origBytes...), ' '),
		},
	}
	_, tamperErr := Open(ctx, Config{DBPath: dbPath, MigrationsFS: mutatedFS})
	if !errors.Is(tamperErr, port.ErrDirtyMigration) || migrate.ExitCode(tamperErr) != 31 {
		t.Fatalf("expected ErrDirtyMigration (exit 31) on 1-byte mutation, got %v (exit=%d)", tamperErr, migrate.ExitCode(tamperErr))
	}

	// 2. Set dirty=1 directly in schema_migrations -> Open halts with exit 31.
	stSkip, err := Open(ctx, Config{DBPath: dbPath, SkipMigrate: true})
	if err != nil {
		t.Fatalf("Open SkipMigrate: %v", err)
	}
	if _, err := stSkip.RawDB().ExecContext(ctx, `UPDATE schema_migrations SET dirty = 1 WHERE version = 1`); err != nil {
		t.Fatalf("mark dirty=1: %v", err)
	}
	_ = stSkip.Close()

	_, dirtyErr := Open(ctx, Config{DBPath: dbPath})
	if !errors.Is(dirtyErr, port.ErrDirtyMigration) || migrate.ExitCode(dirtyErr) != 31 {
		t.Fatalf("expected ErrDirtyMigration (exit 31) on dirty=1, got %v (exit=%d)", dirtyErr, migrate.ExitCode(dirtyErr))
	}
}

func TestSQLiteZeroFloatRealAndPortableSQLNEG3(t *testing.T) {
	t.Parallel()

	sqlRaw, err := sqlitemigrations.FS.ReadFile("0001_foundation.sql")
	if err != nil {
		t.Fatalf("read 0001_foundation.sql: %v", err)
	}
	sqlText := string(sqlRaw)
	if regexp.MustCompile(`(?i)\b(FLOAT|REAL|DOUBLE)\b`).MatchString(sqlText) {
		t.Fatalf("NEG-3 violation: 0001_foundation.sql contains forbidden floating-point column type")
	}
	if regexp.MustCompile(`(?i)\b(datetime|strftime|randomblob|json_extract|julianday)\s*\(`).MatchString(sqlText) {
		t.Fatalf("NEG-3 violation: 0001_foundation.sql contains vendor-specific SQL function")
	}

	// Also check port and migrate Go source files do not embed FLOAT/REAL DDL or SQLite functions.
	for _, goFile := range []string{
		filepath.Join("..", "port", "port.go"),
		filepath.Join("..", "migrate", "migrate.go"),
	} {
		b, rerr := os.ReadFile(goFile)
		if rerr != nil {
			t.Fatalf("read %s: %v", goFile, rerr)
		}
		if regexp.MustCompile(`(?i)\b(FLOAT|REAL|DOUBLE)\b`).MatchString(string(b)) {
			t.Fatalf("NEG-3 violation: %s contains FLOAT/REAL/DOUBLE", goFile)
		}
	}
}

func TestSQLiteNetworkFSRejectionBackupToCrashReopenLeasesAndCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Network filesystem rejection (UNC paths + simulated NFS/SMB detector).
	for _, unc := range []string{"", ":memory:", `\\fileserver\share\tanpopo.db`, `//nas.local/share/tanpopo.db`} {
		if _, err := Open(ctx, Config{DBPath: unc}); err == nil {
			t.Fatalf("expected error opening %q", unc)
		}
	}
	nfsPath := filepath.Join(t.TempDir(), "nfs.db")
	_, nfsErr := Open(ctx, Config{
		DBPath:    nfsPath,
		FSChecker: func(string) (string, error) { return "nfs", nil },
	})
	if !errors.Is(nfsErr, port.ErrNetworkFilesystem) {
		t.Fatalf("expected ErrNetworkFilesystem for NFS mount, got %v", nfsErr)
	}

	// 2. Full CRUD + InTx rollback/commit + Crash-Reopen + VACUUM INTO hot backup + Leases.
	fc := clock.NewFakeClock(time.Date(2026, 11, 14, 18, 30, 0, 0, time.UTC))
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "store", "tanpopo.db")
	st, err := Open(ctx, Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// InTx rollback discards uncommitted mutation.
	rollbackSentinel := errors.New("abort tx")
	if err := st.InTx(ctx, func(tx port.Tx) error {
		if _, err := tx.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "rolled-back", DisplayName: "RB"}); err != nil {
			return err
		}
		return rollbackSentinel
	}); !errors.Is(err, rollbackSentinel) {
		t.Fatalf("expected rollbackSentinel, got %v", err)
	}
	if _, err := st.Tenancy().GetOrganizationBySlug(ctx, "rolled-back"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected rolled-back org absent, got %v", err)
	}

	// Committed CRUD across Org, Project, Environment.
	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "main-org", DisplayName: "Main Org"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if gotOrg, err := st.Tenancy().GetOrganization(ctx, org.ID); err != nil || gotOrg.Slug != "main-org" {
		t.Fatalf("GetOrganization: %+v err=%v", gotOrg, err)
	}
	if orgs, err := st.Tenancy().ListOrganizations(ctx, 0); err != nil || len(orgs) != 1 {
		t.Fatalf("ListOrganizations: %+v err=%v", orgs, err)
	}

	newDisplay := "Main Org Renamed"
	newStatus := port.OrgStatusSuspended
	newReg := "US"
	newTrust := port.MinTrustOfficialOnly
	fourEyes := true
	orgV2, err := st.Tenancy().UpdateOrganization(ctx, org.ID, 1, port.UpdateOrgParams{
		DisplayName:             &newDisplay,
		Status:                  &newStatus,
		DefaultDataRegion:       &newReg,
		MinPluginTrust:          &newTrust,
		RequireFourEyesApproval: &fourEyes,
	})
	if err != nil || orgV2.Version != 2 || orgV2.Status != port.OrgStatusSuspended {
		t.Fatalf("UpdateOrganization: %+v err=%v", orgV2, err)
	}
	if _, err := st.Tenancy().UpdateOrganization(ctx, org.ID, 1, port.UpdateOrgParams{ClearDefaultDataRegion: true}); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("stale UpdateOrganization expected ErrVersionConflict, got %v", err)
	}

	prj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "P1"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if prjs, err := st.Tenancy().ListProjects(ctx, org.ID, 0); err != nil || len(prjs) != 1 {
		t.Fatalf("ListProjects: %+v err=%v", prjs, err)
	}
	if gotPrj, err := st.Tenancy().GetProjectBySlug(ctx, org.ID, "p1"); err != nil || gotPrj.ID != prj.ID {
		t.Fatalf("GetProjectBySlug: %+v err=%v", gotPrj, err)
	}
	// Duplicate slug fails cleanly with ErrAlreadyExists.
	if _, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "Dup"}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists on dup project slug, got %v", err)
	}

	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, prj.ID, port.CreateEnvironmentParams{Slug: "staging"})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if envs, err := st.Tenancy().ListEnvironments(ctx, org.ID, prj.ID); err != nil || len(envs) != 1 {
		t.Fatalf("ListEnvironments: %+v err=%v", envs, err)
	}
	envSlug := "prod"
	reqCaps := true
	envV2, err := st.Tenancy().UpdateEnvironment(ctx, org.ID, env.ID, 1, port.UpdateEnvironmentParams{
		Slug:              &envSlug,
		RequireTestedCaps: &reqCaps,
	})
	if err != nil || envV2.Version != 2 || envV2.Slug != "prod" {
		t.Fatalf("UpdateEnvironment: %+v err=%v", envV2, err)
	}
	if _, err := st.Tenancy().UpdateEnvironment(ctx, org.ID, env.ID, 1, port.UpdateEnvironmentParams{}); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("stale UpdateEnvironment expected ErrVersionConflict, got %v", err)
	}

	// Distributed Leases acquire, contend, renew, expire, release.
	lease1, acquired1, err := st.Leases().AcquireOrRenew(ctx, "orphan_reaper", "node-a", 1000, 5000)
	if err != nil || !acquired1 || lease1.FencingToken != 1 {
		t.Fatalf("node-a acquire failed: lease=%+v acq=%v err=%v", lease1, acquired1, err)
	}
	_, acquiredB, err := st.Leases().AcquireOrRenew(ctx, "orphan_reaper", "node-b", 2000, 5000)
	if err != nil || acquiredB {
		t.Fatalf("node-b should not steal active lease: acq=%v err=%v", acquiredB, err)
	}
	leaseExp, acquiredB2, err := st.Leases().AcquireOrRenew(ctx, "orphan_reaper", "node-b", 7000, 5000)
	if err != nil || !acquiredB2 || leaseExp.FencingToken != 2 {
		t.Fatalf("node-b acquire after expiry failed: lease=%+v acq=%v err=%v", leaseExp, acquiredB2, err)
	}
	if err := st.Leases().Release(ctx, "orphan_reaper", "node-b", leaseExp.FencingToken); err != nil {
		t.Fatalf("lease release: %v", err)
	}

	// Hot online backup via VACUUM INTO + verify backup opens cleanly and contains persisted rows.
	backupPath := filepath.Join(dbDir, "backup", "hot-backup.db")
	if err := st.BackupTo(ctx, backupPath); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}
	if err := st.BackupTo(ctx, backupPath); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("BackupTo existing file expected ErrAlreadyExists, got %v", err)
	}

	// Verify Audit() & Append changelog direct calls.
	if err := st.Audit().Append(ctx, port.AuditEvent{ID: idgen.MustNew(idgen.PrefixAudit)}); err != nil {
		t.Fatalf("Audit.Append: %v", err)
	}
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &org.ID, EntityKind: "policy", EntityID: idgen.MustNew(idgen.PrefixPolicy), MutationType: port.MutationUpsert,
	}); err != nil {
		t.Fatalf("Changelog.Append: %v", err)
	}

	// DeleteEnvironment & DeleteProject version-check + clean removal.
	if err := st.Tenancy().DeleteEnvironment(ctx, org.ID, env.ID, 1); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("stale DeleteEnvironment expected ErrVersionConflict, got %v", err)
	}
	if err := st.Tenancy().DeleteEnvironment(ctx, org.ID, env.ID, 2); err != nil {
		t.Fatalf("DeleteEnvironment v2: %v", err)
	}
	if err := st.Tenancy().DeleteProject(ctx, org.ID, prj.ID, 1); err != nil {
		t.Fatalf("DeleteProject v1: %v", err)
	}

	// Crash-reopen verification on main DB and restored backup DB.
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("second Close should be idempotent, got %v", err)
	}

	reopenedBackup, err := Open(ctx, Config{DBPath: backupPath})
	if err != nil {
		t.Fatalf("Open hot backup: %v", err)
	}
	defer reopenedBackup.Close()
	if gotEnv, err := reopenedBackup.Tenancy().GetEnvironment(ctx, org.ID, env.ID); err != nil || gotEnv.Slug != "prod" {
		t.Fatalf("hot backup missing env prior to delete: got=%+v err=%v", gotEnv, err)
	}
}

func TestSQLiteEdgeCasesTransactionsValidationAndErrorMapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. FSChecker error + BackupTo remote path rejection.
	probePath := filepath.Join(t.TempDir(), "probe.db")
	probeErr := errors.New("statfs failed")
	if _, err := Open(ctx, Config{
		DBPath:    probePath,
		FSChecker: func(string) (string, error) { return "", probeErr },
	}); !errors.Is(err, probeErr) {
		t.Fatalf("expected wrapped FSChecker error, got %v", err)
	}

	// 2. verifyWALPragmas rejects non-WAL / foreign_keys=0 databases.
	rawPath := filepath.Join(t.TempDir(), "raw.db")
	rawDB, err := sql.Open("sqlite", rawPath)
	if err != nil {
		t.Fatalf("sql.Open raw: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	if pragmaErr := verifyWALPragmas(ctx, rawDB); pragmaErr == nil {
		t.Fatalf("expected verifyWALPragmas to fail on raw journal_mode=delete DB")
	}
	if _, err := rawDB.ExecContext(ctx, "PRAGMA journal_mode=WAL; PRAGMA foreign_keys=0;"); err != nil {
		t.Fatalf("set WAL+fk0: %v", err)
	}
	if pragmaErr := verifyWALPragmas(ctx, rawDB); pragmaErr == nil || !strings.Contains(pragmaErr.Error(), "foreign_keys") {
		t.Fatalf("expected foreign_keys pragma failure, got %v", pragmaErr)
	}

	// 3. Open healthy store and exercise all tx repos, Updates, Deletes, Leases, and input errors.
	st := openTempStore(t, nil)

	if err := st.BackupTo(ctx, `\\remote\share\backup.db`); !errors.Is(err, port.ErrNetworkFilesystem) {
		t.Fatalf("expected BackupTo to reject UNC path, got %v", err)
	}

	// Explicit BeginTx + tx.{Tenancy,Changelog,Leases,Audit} + Commit.
	tx, err := st.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	org, err := tx.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "tx-org", DisplayName: "Tx Org"})
	if err != nil {
		t.Fatalf("tx.CreateOrganization: %v", err)
	}
	prj, err := tx.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "tx-prj", DisplayName: "Tx Prj"})
	if err != nil {
		t.Fatalf("tx.CreateProject: %v", err)
	}
	env, err := tx.Tenancy().CreateEnvironment(ctx, org.ID, prj.ID, port.CreateEnvironmentParams{Slug: "dev"})
	if err != nil {
		t.Fatalf("tx.CreateEnvironment: %v", err)
	}
	if _, err := tx.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &org.ID, EntityKind: "policy", EntityID: idgen.MustNew(idgen.PrefixPolicy), MutationType: port.MutationUpsert, Emergency: true,
	}); err != nil {
		t.Fatalf("tx.Changelog.Append: %v", err)
	}
	if _, _, err := tx.Leases().AcquireOrRenew(ctx, "tx-lease", "node-1", 1000, 5000); err != nil {
		t.Fatalf("tx.Leases.AcquireOrRenew: %v", err)
	}
	if err := tx.Audit().Append(ctx, port.AuditEvent{ID: idgen.MustNew(idgen.PrefixAudit)}); err != nil {
		t.Fatalf("tx.Audit.Append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit: %v", err)
	}

	// UpdateOrganization ClearDefaultDataRegion + duplicate org slug ErrAlreadyExists.
	if _, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "tx-org", DisplayName: "Dup"}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists on duplicate org slug, got %v", err)
	}
	orgV2, err := st.Tenancy().UpdateOrganization(ctx, org.ID, 1, port.UpdateOrgParams{ClearDefaultDataRegion: true})
	if err != nil || orgV2.DefaultDataRegion != nil || orgV2.Version != 2 {
		t.Fatalf("expected cleared region and version=2, got %+v err=%v", orgV2, err)
	}

	// UpdateProject Status + EnforcedRegion + ClearEnforcedRegion + CHECK constraint invalid status.
	eu := "EU"
	halted := port.ProjectStatusEmergencyHalted
	prjV2, err := st.Tenancy().UpdateProject(ctx, org.ID, prj.ID, 1, port.UpdateProjectParams{
		Status:         &halted,
		EnforcedRegion: &eu,
	})
	if err != nil || prjV2.Status != port.ProjectStatusEmergencyHalted || prjV2.EnforcedRegion == nil || *prjV2.EnforcedRegion != "EU" {
		t.Fatalf("UpdateProject status/region failed: %+v err=%v", prjV2, err)
	}
	prjV3, err := st.Tenancy().UpdateProject(ctx, org.ID, prj.ID, 2, port.UpdateProjectParams{ClearEnforcedRegion: true})
	if err != nil || prjV3.EnforcedRegion != nil || prjV3.Version != 3 {
		t.Fatalf("ClearEnforcedRegion failed: %+v err=%v", prjV3, err)
	}
	badStatus := "BOGUS"
	if _, err := st.Tenancy().UpdateProject(ctx, org.ID, prj.ID, 3, port.UpdateProjectParams{Status: &badStatus}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput from CHECK constraint on bad status, got %v", err)
	}

	// Wrong version DeleteProject on existing project -> ErrVersionConflict.
	if err := st.Tenancy().DeleteProject(ctx, org.ID, prj.ID, 1); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict deleting project with stale version, got %v", err)
	}
	// DeleteProject succeeds with matching version and cascades child environments per canonical DDL.
	if err := st.Tenancy().DeleteProject(ctx, org.ID, prj.ID, 3); err != nil {
		t.Fatalf("DeleteProject v3: %v", err)
	}
	if _, err := st.Tenancy().GetEnvironment(ctx, org.ID, env.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected cascaded delete of env, got %v", err)
	}
	// Verify mapSQLiteError translates FOREIGN KEY errors by operation (ErrNotFound on insert vs ErrResourceInUse on delete).
	if err := mapSQLiteError(errors.New("FOREIGN KEY constraint failed")); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound mapping for insert FK error, got %v", err)
	}
	if err := mapSQLiteErrorForDelete(errors.New("FOREIGN KEY constraint failed")); !errors.Is(err, port.ErrResourceInUse) {
		t.Fatalf("expected ErrResourceInUse mapping for delete FK error, got %v", err)
	}
	if mapSQLiteError(nil) != nil {
		t.Fatalf("expected nil on mapSQLiteError(nil)")
	}
	// Missing env inside same org returns ErrNotFound on DeleteEnvironment.
	if err := st.Tenancy().DeleteEnvironment(ctx, org.ID, "env_does_not_exist", 1); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting nonexistent env in same org, got %v", err)
	}

	// Same-node lease renewal + wrong-holder / stale-token release + monotonic fencing_token across Release -> Get -> Reacquire.
	renewed, ok, err := st.Leases().AcquireOrRenew(ctx, "tx-lease", "node-1", 2000, 5000)
	if err != nil || !ok || renewed.FencingToken != 2 {
		t.Fatalf("expected same-node renewal fencing_token=2, got %+v ok=%v err=%v", renewed, ok, err)
	}
	if err := st.Leases().Release(ctx, "tx-lease", "wrong-node", renewed.FencingToken); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict releasing lease by wrong holder, got %v", err)
	}
	if err := st.Leases().Release(ctx, "tx-lease", "node-1", renewed.FencingToken); err != nil {
		t.Fatalf("release lease: %v", err)
	}
	if _, err := st.Leases().Get(ctx, "tx-lease"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after lease release, got %v", err)
	}
	if err := st.Leases().Release(ctx, "tx-lease", "node-1", renewed.FencingToken); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double-release with same token, got %v", err)
	}
	reacquired, ok, err := st.Leases().AcquireOrRenew(ctx, "tx-lease", "node-2", 2500, 5000)
	if err != nil || !ok || reacquired.FencingToken != 3 {
		t.Fatalf("expected monotonic fencing_token=3 after release+reacquire, got %+v ok=%v err=%v", reacquired, ok, err)
	}
	if _, err := st.Leases().Get(ctx, "missing-lease"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing lease, got %v", err)
	}

	// InTx panic rolls back uncommitted work cleanly before propagating panic.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("expected panic from InTx to propagate after rollback")
			}
		}()
		_ = st.InTx(ctx, func(ptx port.Tx) error {
			if _, err := ptx.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "panicked-org", DisplayName: "Panic"}); err != nil {
				return err
			}
			panic("simulated tx panic")
		})
	}()
	if _, err := st.Tenancy().GetOrganizationBySlug(ctx, "panicked-org"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected panicked InTx org to be rolled back, got %v", err)
	}

	// Explicit BeginTx Update/Delete exercise + concurrent Close() idempotency.
	tx2, err := st.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 2: %v", err)
	}
	txName := "Tx Org Updated"
	if _, err := tx2.Tenancy().UpdateOrganization(ctx, org.ID, 2, port.UpdateOrgParams{DisplayName: &txName}); err != nil {
		t.Fatalf("tx2.UpdateOrganization: %v", err)
	}
	txPrj, err := tx2.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "tx-p2", DisplayName: "P2"})
	if err != nil {
		t.Fatalf("tx2.CreateProject: %v", err)
	}
	txPrjName := "P2 Renamed"
	if _, err := tx2.Tenancy().UpdateProject(ctx, org.ID, txPrj.ID, 1, port.UpdateProjectParams{DisplayName: &txPrjName}); err != nil {
		t.Fatalf("tx2.UpdateProject: %v", err)
	}
	txEnv, err := tx2.Tenancy().CreateEnvironment(ctx, org.ID, txPrj.ID, port.CreateEnvironmentParams{Slug: "qa"})
	if err != nil {
		t.Fatalf("tx2.CreateEnvironment: %v", err)
	}
	txEnvSlug := "qa-v2"
	if _, err := tx2.Tenancy().UpdateEnvironment(ctx, org.ID, txEnv.ID, 1, port.UpdateEnvironmentParams{Slug: &txEnvSlug}); err != nil {
		t.Fatalf("tx2.UpdateEnvironment: %v", err)
	}
	if err := tx2.Tenancy().DeleteEnvironment(ctx, org.ID, txEnv.ID, 2); err != nil {
		t.Fatalf("tx2.DeleteEnvironment: %v", err)
	}
	if err := tx2.Tenancy().DeleteProject(ctx, org.ID, txPrj.ID, 2); err != nil {
		t.Fatalf("tx2.DeleteProject: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("tx2.Commit: %v", err)
	}

	// NUL byte path rejection + optional OS symlink/dangling-symlink rejection when symlinks are permitted.
	if _, err := Open(ctx, Config{DBPath: filepath.Join(t.TempDir(), "bad\x00.db")}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on NUL path, got %v", err)
	}
	symDir := t.TempDir()
	symLinkPath := filepath.Join(symDir, "linked.db")
	if symErr := os.Symlink(filepath.Join(symDir, "missing-target.db"), symLinkPath); symErr == nil {
		if _, err := Open(ctx, Config{DBPath: symLinkPath}); !errors.Is(err, port.ErrNetworkFilesystem) {
			t.Fatalf("expected ErrNetworkFilesystem rejecting dangling symlink %q, got %v", symLinkPath, err)
		}
	}

	// Cancelled context returns context.Canceled cleanly.
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := st.Tenancy().ListOrganizations(cancelledCtx, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on cancelled context, got %v", err)
	}

	// Caller-supplied ID & Update blank-string validation checks.
	if _, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{ID: "prj_wrongprefix", Slug: "s", DisplayName: "D"}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on wrong ID prefix, got %v", err)
	}
	if _, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{ID: "prj with space", Slug: "s", DisplayName: "D"}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on whitespace project ID, got %v", err)
	}
	if _, err := st.Tenancy().CreateEnvironment(ctx, org.ID, "prj_any", port.CreateEnvironmentParams{ID: "env_", Slug: "s"}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on empty suffix env ID, got %v", err)
	}
	blank := "   "
	if _, err := st.Tenancy().UpdateOrganization(ctx, org.ID, 3, port.UpdateOrgParams{DisplayName: &blank}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on blank Org DisplayName update, got %v", err)
	}
	if _, err := st.Tenancy().UpdateProject(ctx, org.ID, "prj_x", 1, port.UpdateProjectParams{DisplayName: &blank}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on blank Project DisplayName update, got %v", err)
	}
	if _, err := st.Tenancy().UpdateEnvironment(ctx, org.ID, "env_x", 1, port.UpdateEnvironmentParams{Slug: &blank}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on blank Environment Slug update, got %v", err)
	}

	// Changelog ListSince defaults & entries check.
	changes, err := st.Changelog().ListSince(ctx, 0, 0)
	if err != nil || len(changes) < 5 {
		t.Fatalf("expected changelog entries >= 5, got %d err=%v", len(changes), err)
	}

	// Invalid input validation checks across store/repo methods.
	if _, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateOrganization empty: got %v", err)
	}
	if _, err := st.Tenancy().GetOrganization(ctx, ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetOrganization empty: got %v", err)
	}
	if _, err := st.Tenancy().GetOrganizationBySlug(ctx, ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetOrganizationBySlug empty: got %v", err)
	}
	if _, err := st.Tenancy().UpdateOrganization(ctx, "", 1, port.UpdateOrgParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("UpdateOrganization invalid: got %v", err)
	}
	if _, err := st.Tenancy().UpdateOrganization(ctx, "org_missing", 1, port.UpdateOrgParams{}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdateOrganization missing: got %v", err)
	}
	if _, err := st.Tenancy().CreateProject(ctx, "", port.CreateProjectParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateProject invalid: got %v", err)
	}
	if _, err := st.Tenancy().GetProject(ctx, "", "p"); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetProject invalid: got %v", err)
	}
	if _, err := st.Tenancy().GetProjectBySlug(ctx, "", "p"); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetProjectBySlug invalid: got %v", err)
	}
	if _, err := st.Tenancy().ListProjects(ctx, "", 10); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("ListProjects invalid: got %v", err)
	}
	if _, err := st.Tenancy().UpdateProject(ctx, "", "p", 1, port.UpdateProjectParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("UpdateProject invalid: got %v", err)
	}
	if err := st.Tenancy().DeleteProject(ctx, "", "p", 1); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("DeleteProject invalid: got %v", err)
	}
	if _, err := st.Tenancy().CreateEnvironment(ctx, "", "p", port.CreateEnvironmentParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateEnvironment invalid: got %v", err)
	}
	if _, err := st.Tenancy().GetEnvironment(ctx, "", "e"); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetEnvironment invalid: got %v", err)
	}
	if _, err := st.Tenancy().ListEnvironments(ctx, "", "p"); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("ListEnvironments invalid: got %v", err)
	}
	if _, err := st.Tenancy().UpdateEnvironment(ctx, "", "e", 1, port.UpdateEnvironmentParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("UpdateEnvironment invalid: got %v", err)
	}
	if err := st.Tenancy().DeleteEnvironment(ctx, "", "e", 1); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("DeleteEnvironment invalid: got %v", err)
	}
	if _, _, err := st.Leases().AcquireOrRenew(ctx, "", "h", 1, 0); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("AcquireOrRenew invalid: got %v", err)
	}
	if err := st.Leases().Release(ctx, "", "h", 1); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("Release invalid: got %v", err)
	}
	if err := st.Leases().Release(ctx, "tx-lease", "h", 0); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("Release invalid token 0: got %v", err)
	}
	if _, err := st.Leases().Get(ctx, ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("Leases.Get invalid: got %v", err)
	}
}

func TestSQLiteLeaseStaleReleaseAndExpiredGetSemantics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTempStore(t, nil)

	// 1. Acquire lease on node-1 (fencing_token = 1), then same node renews (fencing_token = 2).
	l1, ok, err := st.Leases().AcquireOrRenew(ctx, "singleton_worker", "node-1", 1000, 5000)
	if err != nil || !ok || l1.FencingToken != 1 {
		t.Fatalf("acquire l1: lease=%+v ok=%v err=%v", l1, ok, err)
	}
	l2, ok, err := st.Leases().AcquireOrRenew(ctx, "singleton_worker", "node-1", 2000, 5000)
	if err != nil || !ok || l2.FencingToken != 2 {
		t.Fatalf("renew l2: lease=%+v ok=%v err=%v", l2, ok, err)
	}

	// Regression: stalled worker on the SAME holderNodeID holding stale token=1 MUST NOT release token=2.
	if err := st.Leases().Release(ctx, "singleton_worker", "node-1", l1.FencingToken); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("stale release same holder after renew expected ErrVersionConflict, got %v", err)
	}
	cur, err := st.Leases().Get(ctx, "singleton_worker")
	if err != nil || cur.HolderNodeID != "node-1" || cur.FencingToken != 2 {
		t.Fatalf("active lease should remain intact after stale release: cur=%+v err=%v", cur, err)
	}

	// 2. Lease expires (nowMs=7000 == l2.ExpiresAtMs), node-2 steals (token=3), then expires (nowMs=12000),
	// and SAME holderNodeID node-1 reacquires (token=4). Stale token=1 MUST STILL NOT release token=4.
	l3, ok, err := st.Leases().AcquireOrRenew(ctx, "singleton_worker", "node-2", 7000, 5000)
	if err != nil || !ok || l3.FencingToken != 3 {
		t.Fatalf("steal l3 after expiry: lease=%+v ok=%v err=%v", l3, ok, err)
	}
	l4, ok, err := st.Leases().AcquireOrRenew(ctx, "singleton_worker", "node-1", 12000, 5000)
	if err != nil || !ok || l4.FencingToken != 4 {
		t.Fatalf("reacquire l4 on node-1: lease=%+v ok=%v err=%v", l4, ok, err)
	}
	if err := st.Leases().Release(ctx, "singleton_worker", "node-1", l1.FencingToken); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("stale release same holder after reacquire expected ErrVersionConflict, got %v", err)
	}
	cur2, err := st.Leases().Get(ctx, "singleton_worker")
	if err != nil || cur2.HolderNodeID != "node-1" || cur2.FencingToken != 4 {
		t.Fatalf("reacquired lease must remain intact after stale token release: cur=%+v err=%v", cur2, err)
	}

	// 3. Expired lease Get() semantics: Get returns the un-released record (so liveness is checked via IsExpired(nowMs)).
	if cur2.IsExpired(16999) {
		t.Fatalf("expected lease not expired at 16999 (expiresAt=%d)", cur2.ExpiresAtMs)
	}
	if !cur2.IsExpired(17000) || !cur2.IsExpired(20000) {
		t.Fatalf("expected IsExpired=true at >= expiresAt (%d)", cur2.ExpiresAtMs)
	}

	// 4. Missing lease Release -> ErrNotFound.
	if err := st.Leases().Release(ctx, "never_created", "node-1", 1); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound releasing nonexistent lease, got %v", err)
	}

	// 5. Release with active matching token=4 succeeds -> Get returns ErrNotFound -> stale token=1 rejected -> next acquire=5.
	if err := st.Leases().Release(ctx, "singleton_worker", "node-1", l4.FencingToken); err != nil {
		t.Fatalf("release current token l4: %v", err)
	}
	if _, err := st.Leases().Get(ctx, "singleton_worker"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after matching release, got %v", err)
	}
	if err := st.Leases().Release(ctx, "singleton_worker", "node-1", l1.FencingToken); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("stale token=1 on released token=4 row expected ErrVersionConflict, got %v", err)
	}
	l5, ok, err := st.Leases().AcquireOrRenew(ctx, "singleton_worker", "node-3", 20000, 5000)
	if err != nil || !ok || l5.FencingToken != 5 {
		t.Fatalf("expected monotonic token=5 on next acquire, got %+v ok=%v err=%v", l5, ok, err)
	}
}
