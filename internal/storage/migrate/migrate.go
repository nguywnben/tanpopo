// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package migrate executes sequential Expand-Only SQL migrations with SHA-256
// checksum verification and fail-closed dirty state detection (FMEA-06 -> exit 31).
//
// DAG Boundary (Tier 2): May import stdlib, internal/pkg, and internal/storage/port.
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// ExitCodeDirtyMigration is the canonical CLI exit code when schema_migrations
// is dirty or a previously applied migration checksum does not match (FMEA-06).
const ExitCodeDirtyMigration = 31

// Migration represents a parsed versioned SQL migration file.
type Migration struct {
	Version        int64
	Name           string
	SQL            string
	ChecksumSHA256 string
}

// AppliedRecord captures one persisted row in schema_migrations.
type AppliedRecord struct {
	Version        int64
	Name           string
	ChecksumSHA256 string
	Dirty          bool
	AppliedAtMs    int64
}

// Summary reports migration runner actions for observability and tests.
type Summary struct {
	AlreadyApplied int
	NewlyApplied   int
	LatestVersion  int64
}

// DirtyError wraps port.ErrDirtyMigration with exact diagnostic context and exit code 31.
type DirtyError struct {
	Version        int64
	Name           string
	Reason         string
	ExpectedSHA256 string
	ActualSHA256   string
	Cause          error
}

func (e *DirtyError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "migrate (exit %d): version=%d name=%q reason=%s", ExitCodeDirtyMigration, e.Version, e.Name, e.Reason)
	if e.ExpectedSHA256 != "" || e.ActualSHA256 != "" {
		fmt.Fprintf(&b, " expected_sha256=%s actual_sha256=%s", e.ExpectedSHA256, e.ActualSHA256)
	}
	if e.Cause != nil {
		fmt.Fprintf(&b, ": %v", e.Cause)
	}
	return b.String()
}

func (e *DirtyError) Unwrap() []error {
	if e.Cause != nil {
		if errors.Is(e.Cause, port.ErrDirtyMigration) {
			return []error{e.Cause}
		}
		return []error{port.ErrDirtyMigration, e.Cause}
	}
	return []error{port.ErrDirtyMigration}
}

// ExitCode returns ExitCodeDirtyMigration (31) if err wraps port.ErrDirtyMigration,
// 1 for any other non-nil error, or 0 for nil.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, port.ErrDirtyMigration) {
		return ExitCodeDirtyMigration
	}
	return 1
}

var migrationFileRe = regexp.MustCompile(`^([0-9]+)_([a-zA-Z0-9_-]+)\.sql$`)

// LoadMigrations discovers, validates, and SHA-256 hashes versioned *.sql files
// from the root of migrationsFS in strictly ascending version order.
func LoadMigrations(migrationsFS fs.FS) ([]Migration, error) {
	if migrationsFS == nil {
		return nil, fmt.Errorf("%w: migrations FS is nil", port.ErrInvalidInput)
	}
	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: read dir: %w", err)
	}

	var migrations []Migration
	seen := make(map[int64]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if path.Ext(name) != ".sql" {
			continue
		}
		m := migrationFileRe.FindStringSubmatch(name)
		if m == nil {
			return nil, fmt.Errorf("%w: invalid migration filename %q (expected <NNNN>_<name>.sql)", port.ErrInvalidInput, name)
		}
		ver, parseErr := strconv.ParseInt(m[1], 10, 64)
		if parseErr != nil || ver <= 0 {
			return nil, fmt.Errorf("%w: invalid migration version in %q", port.ErrInvalidInput, name)
		}
		if prev, dup := seen[ver]; dup {
			return nil, fmt.Errorf("%w: duplicate migration version %d (%q and %q)", port.ErrInvalidInput, ver, prev, name)
		}
		seen[ver] = name

		raw, readErr := fs.ReadFile(migrationsFS, name)
		if readErr != nil {
			return nil, fmt.Errorf("migrate: read %q: %w", name, readErr)
		}
		normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
		if strings.TrimSpace(normalized) == "" {
			return nil, fmt.Errorf("%w: migration %q is empty", port.ErrInvalidInput, name)
		}
		sum := sha256.Sum256([]byte(normalized))
		migrations = append(migrations, Migration{
			Version:        ver,
			Name:           m[2],
			SQL:            normalized,
			ChecksumSHA256: hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

const createSchemaMigrationsSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version         BIGINT PRIMARY KEY,
    name            VARCHAR(128) NOT NULL,
    checksum_sha256 CHAR(64) NOT NULL,
    dirty           BOOLEAN NOT NULL DEFAULT 0 CHECK (dirty IN (0, 1)),
    applied_at_ms   BIGINT NOT NULL
);`

// Apply verifies all previously applied migrations (halting with port.ErrDirtyMigration /
// exit code 31 on dirty state or SHA-256 checksum drift) and sequentially applies any
// pending migrations from migrationsFS.
func Apply(ctx context.Context, db *sql.DB, migrationsFS fs.FS, nowMsFn func() int64) (Summary, error) {
	var summary Summary
	if db == nil {
		return summary, fmt.Errorf("%w: nil *sql.DB", port.ErrInvalidInput)
	}
	if nowMsFn == nil {
		nowMsFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}

	migrations, err := LoadMigrations(migrationsFS)
	if err != nil {
		return summary, err
	}

	if _, err := db.ExecContext(ctx, createSchemaMigrationsSQL); err != nil {
		return summary, fmt.Errorf("migrate: ensure schema_migrations: %w", err)
	}

	applied, err := readAppliedRecords(ctx, db)
	if err != nil {
		return summary, err
	}

	byVersion := make(map[int64]Migration, len(migrations))
	for _, m := range migrations {
		byVersion[m.Version] = m
	}

	appliedSet := make(map[int64]bool, len(applied))
	var maxApplied int64
	for _, rec := range applied {
		if rec.Dirty {
			return summary, &DirtyError{
				Version:        rec.Version,
				Name:           rec.Name,
				Reason:         "dirty_state_requires_manual_recovery",
				ExpectedSHA256: rec.ChecksumSHA256,
				Cause:          port.ErrDirtyMigration,
			}
		}
		m, ok := byVersion[rec.Version]
		if !ok {
			return summary, &DirtyError{
				Version:        rec.Version,
				Name:           rec.Name,
				Reason:         "applied_migration_missing_from_source",
				ExpectedSHA256: rec.ChecksumSHA256,
				Cause:          port.ErrDirtyMigration,
			}
		}
		if rec.ChecksumSHA256 != m.ChecksumSHA256 {
			return summary, &DirtyError{
				Version:        rec.Version,
				Name:           rec.Name,
				Reason:         "checksum_sha256_mismatch",
				ExpectedSHA256: rec.ChecksumSHA256,
				ActualSHA256:   m.ChecksumSHA256,
				Cause:          port.ErrDirtyMigration,
			}
		}
		appliedSet[rec.Version] = true
		if rec.Version > maxApplied {
			maxApplied = rec.Version
		}
		summary.AlreadyApplied++
		summary.LatestVersion = maxApplied
	}

	for _, m := range migrations {
		if appliedSet[m.Version] {
			continue
		}
		if m.Version < maxApplied {
			return summary, &DirtyError{
				Version: m.Version,
				Name:    m.Name,
				Reason:  "out_of_order_unapplied_migration",
				Cause:   port.ErrDirtyMigration,
			}
		}

		// Record dirty=1 prior to executing DDL so mid-flight failure persists dirty state (FMEA-06).
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, checksum_sha256, dirty, applied_at_ms) VALUES (?, ?, ?, 1, ?)`,
			m.Version, m.Name, m.ChecksumSHA256, nowMsFn(),
		); err != nil {
			return summary, fmt.Errorf("migrate: record dirty start v%d: %w", m.Version, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return summary, &DirtyError{
				Version: m.Version,
				Name:    m.Name,
				Reason:  "begin_tx_failed_dirty_persisted",
				Cause:   errors.Join(port.ErrDirtyMigration, err),
			}
		}

		if _, execErr := tx.ExecContext(ctx, m.SQL); execErr != nil {
			_ = tx.Rollback()
			return summary, &DirtyError{
				Version: m.Version,
				Name:    m.Name,
				Reason:  "migration_sql_failed_dirty_persisted",
				Cause:   errors.Join(port.ErrDirtyMigration, execErr),
			}
		}

		if _, clearErr := tx.ExecContext(ctx, `UPDATE schema_migrations SET dirty = 0 WHERE version = ?`, m.Version); clearErr != nil {
			_ = tx.Rollback()
			return summary, &DirtyError{
				Version: m.Version,
				Name:    m.Name,
				Reason:  "clear_dirty_flag_failed",
				Cause:   errors.Join(port.ErrDirtyMigration, clearErr),
			}
		}

		if commitErr := tx.Commit(); commitErr != nil {
			return summary, &DirtyError{
				Version: m.Version,
				Name:    m.Name,
				Reason:  "commit_failed_dirty_persisted",
				Cause:   errors.Join(port.ErrDirtyMigration, commitErr),
			}
		}

		summary.NewlyApplied++
		summary.LatestVersion = m.Version
		maxApplied = m.Version
	}

	return summary, nil
}

// ReadApplied returns all recorded entries from schema_migrations ordered by version.
func ReadApplied(ctx context.Context, db *sql.DB) ([]AppliedRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: nil *sql.DB", port.ErrInvalidInput)
	}
	return readAppliedRecords(ctx, db)
}

func readAppliedRecords(ctx context.Context, db *sql.DB) ([]AppliedRecord, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, name, checksum_sha256, dirty, applied_at_ms FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return nil, fmt.Errorf("migrate: query schema_migrations: %w", err)
	}
	defer rows.Close()

	var out []AppliedRecord
	for rows.Next() {
		var rec AppliedRecord
		var dirtyInt int64
		if scanErr := rows.Scan(&rec.Version, &rec.Name, &rec.ChecksumSHA256, &dirtyInt, &rec.AppliedAtMs); scanErr != nil {
			return nil, fmt.Errorf("migrate: scan schema_migrations: %w", scanErr)
		}
		rec.Dirty = dirtyInt != 0
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrate: iterate schema_migrations: %w", err)
	}
	return out, nil
}
