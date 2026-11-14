// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"

	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "migrate_test.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("sql.Open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateApplyIdempotentAndNEG2DirtyChecksumExit31(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestSQLite(t)

	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE sample_items (id TEXT PRIMARY KEY, val BIGINT NOT NULL);\n"),
		},
		"0002_second.sql": &fstest.MapFile{
			Data: []byte("ALTER TABLE sample_items ADD COLUMN note TEXT;\n"),
		},
	}

	sum, err := Apply(ctx, db, mfs, func() int64 { return 1700000000000 })
	if err != nil {
		t.Fatalf("initial Apply: %v", err)
	}
	if sum.NewlyApplied != 2 || sum.AlreadyApplied != 0 || sum.LatestVersion != 2 {
		t.Fatalf("unexpected summary: %+v", sum)
	}
	if ExitCode(err) != 0 {
		t.Fatalf("expected ExitCode 0, got %d", ExitCode(err))
	}

	// Re-running Apply with identical files is idempotent.
	sum2, err := Apply(ctx, db, mfs, nil)
	if err != nil {
		t.Fatalf("idempotent Apply: %v", err)
	}
	if sum2.NewlyApplied != 0 || sum2.AlreadyApplied != 2 || sum2.LatestVersion != 2 {
		t.Fatalf("unexpected idempotent summary: %+v", sum2)
	}

	// NEG-2a: Mutate 1 byte in an already-applied migration -> ErrDirtyMigration & exit code 31!
	tamperedFS := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE sample_items (id TEXT PRIMARY KEY, val BIGINT NOT NULL );\n"),
		},
		"0002_second.sql": mfs["0002_second.sql"],
	}
	_, tamperErr := Apply(ctx, db, tamperedFS, nil)
	if !errors.Is(tamperErr, port.ErrDirtyMigration) || ExitCode(tamperErr) != ExitCodeDirtyMigration {
		t.Fatalf("expected ErrDirtyMigration (exit 31) on tampered migration, got err=%v exit=%d", tamperErr, ExitCode(tamperErr))
	}

	// NEG-2b: Applied migration missing from FS -> exit code 31.
	missingFS := fstest.MapFS{
		"0001_init.sql": mfs["0001_init.sql"],
	}
	_, missErr := Apply(ctx, db, missingFS, nil)
	if !errors.Is(missErr, port.ErrDirtyMigration) || ExitCode(missErr) != 31 {
		t.Fatalf("expected exit 31 on missing applied migration, got %v (%d)", missErr, ExitCode(missErr))
	}

	// NEG-2c: Out-of-order migration (< maxApplied) -> exit code 31.
	// Mark version 3 applied, then try adding version 2 below it cleanly on fresh db.
	dbOOO := openTestSQLite(t)
	if _, err := Apply(ctx, dbOOO, fstest.MapFS{
		"0003_three.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t3 (id TEXT PRIMARY KEY);\n")},
	}, nil); err != nil {
		t.Fatalf("apply v3: %v", err)
	}
	_, oooErr := Apply(ctx, dbOOO, fstest.MapFS{
		"0002_two.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE t2 (id TEXT PRIMARY KEY);\n")},
		"0003_three.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t3 (id TEXT PRIMARY KEY);\n")},
	}, nil)
	if !errors.Is(oooErr, port.ErrDirtyMigration) || ExitCode(oooErr) != 31 {
		t.Fatalf("expected ErrDirtyMigration on out-of-order migration, got %v", oooErr)
	}

	// NEG-2d: Explicit dirty = 1 row halts startup with exit code 31.
	if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET dirty = 1 WHERE version = 2`); err != nil {
		t.Fatalf("set dirty=1: %v", err)
	}
	_, dirtyErr := Apply(ctx, db, mfs, nil)
	if !errors.Is(dirtyErr, port.ErrDirtyMigration) || ExitCode(dirtyErr) != 31 {
		t.Fatalf("expected ErrDirtyMigration (exit 31) on dirty=1, got %v (%d)", dirtyErr, ExitCode(dirtyErr))
	}

	recs, err := ReadApplied(ctx, db)
	if err != nil || len(recs) != 2 || !recs[1].Dirty {
		t.Fatalf("unexpected ReadApplied records: recs=%+v err=%v", recs, err)
	}
}

func TestMigrateFailedSQLPersistsDirtyStateAndRejectsBadInputs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestSQLite(t)

	badSQLFS := fstest.MapFS{
		"0001_broken.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE broken (id TEXT); SYNTAX ERROR HERE;\n"),
		},
	}
	_, failErr := Apply(ctx, db, badSQLFS, nil)
	if !errors.Is(failErr, port.ErrDirtyMigration) || ExitCode(failErr) != 31 {
		t.Fatalf("expected ErrDirtyMigration on failed SQL, got %v", failErr)
	}

	// Reopening and running Apply (even with fixed SQL) must refuse to start while dirty=1.
	fixedFS := fstest.MapFS{
		"0001_broken.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE broken (id TEXT PRIMARY KEY);\n"),
		},
	}
	_, reopenErr := Apply(ctx, db, fixedFS, nil)
	if !errors.Is(reopenErr, port.ErrDirtyMigration) || ExitCode(reopenErr) != 31 {
		t.Fatalf("expected fail-closed dirty halt on reopen, got %v", reopenErr)
	}

	// Cross-OS CRLF vs LF produces identical SHA-256 checksums.
	lfMigs, err := LoadMigrations(fstest.MapFS{
		"0001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (x INT);\n")},
	})
	if err != nil {
		t.Fatalf("LoadMigrations LF: %v", err)
	}
	crlfMigs, err := LoadMigrations(fstest.MapFS{
		"0001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (x INT);\r\n")},
	})
	if err != nil {
		t.Fatalf("LoadMigrations CRLF: %v", err)
	}
	if lfMigs[0].ChecksumSHA256 != crlfMigs[0].ChecksumSHA256 {
		t.Fatalf("CRLF vs LF checksum drift: %s vs %s", lfMigs[0].ChecksumSHA256, crlfMigs[0].ChecksumSHA256)
	}

	// Nil/invalid argument handling.
	if ExitCode(errors.New("generic")) != 1 {
		t.Fatalf("expected generic ExitCode 1")
	}
	if _, err := LoadMigrations(nil); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on nil FS, got %v", err)
	}
	if _, err := ReadApplied(ctx, nil); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on nil DB, got %v", err)
	}
	if _, err := Apply(ctx, nil, fixedFS, nil); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on nil DB Apply, got %v", err)
	}

	for name, mfs := range map[string]fstest.MapFS{
		"bad_filename": {"bad.sql": {Data: []byte("SELECT 1;")}},
		"zero_ver":     {"0000_zero.sql": {Data: []byte("SELECT 1;")}},
		"dup_ver": {
			"0001_a.sql": {Data: []byte("SELECT 1;")},
			"001_b.sql":  {Data: []byte("SELECT 1;")},
		},
		"empty_sql": {"0001_empty.sql": {Data: []byte("   \n")}},
	} {
		if _, err := LoadMigrations(mfs); !errors.Is(err, port.ErrInvalidInput) {
			t.Fatalf("case %s expected ErrInvalidInput, got %v", name, err)
		}
	}
	// Verify non-.sql files and subdirectories are ignored cleanly.
	withExtrasFS := fstest.MapFS{
		"README.md":       {Data: []byte("ignore me")},
		"sub/0002_s.sql":  {Data: []byte("SELECT 1;")},
		"0001_valid.sql":  {Data: []byte("CREATE TABLE v (id TEXT);")},
	}
	loadedExtras, err := LoadMigrations(withExtrasFS)
	if err != nil || len(loadedExtras) != 1 || loadedExtras[0].Version != 1 {
		t.Fatalf("expected 1 loaded migration, got %+v err=%v", loadedExtras, err)
	}
	de := &DirtyError{
		Version:        1,
		Name:           "x",
		Reason:         "r",
		ExpectedSHA256: "abc",
		ActualSHA256:   "def",
		Cause:          errors.New("underlying cause"),
	}
	if !strings.Contains(de.Error(), "exit 31") || !strings.Contains(de.Error(), "expected_sha256=abc") {
		t.Fatalf("unexpected DirtyError string: %q", de.Error())
	}
	if !errors.Is(de, port.ErrDirtyMigration) {
		t.Fatalf("expected DirtyError to unwrap to ErrDirtyMigration")
	}
}
