// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type partitionedTableSpec struct {
	name            string
	allowSystemExpr bool
}

var canonicalPartitionedTables = []partitionedTableSpec{
	{name: "usage_settlements", allowSystemExpr: false},
	{name: "request_traces_meta", allowSystemExpr: false},
	{name: "audit_events", allowSystemExpr: true},
}

// PartitionManager manages monthly UTC RANGE partitions for usage_settlements,
// request_traces_meta, and audit_events, ensuring FORCE RLS policies match parents.
type PartitionManager struct {
	db *sql.DB
}

// NewPartitionManager constructs a PartitionManager over a PostgreSQL *sql.DB.
func NewPartitionManager(db *sql.DB) *PartitionManager {
	return &PartitionManager{db: db}
}

// MonthBoundsUTC returns the canonical partition suffix YYYY_MM and [startMs, endMs)
// UTC Unix millisecond bounds for the month containing t.
func MonthBoundsUTC(t time.Time) (suffix string, startMs int64, endMs int64) {
	u := t.UTC()
	start := time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	suffix = fmt.Sprintf("%04d_%02d", start.Year(), int(start.Month()))
	return suffix, start.UnixMilli(), end.UnixMilli()
}

// EnsureMonth idempotently creates the monthly partitions covering monthTime.UTC()
// across usage_settlements, request_traces_meta, and audit_events and enforces RLS.
func (pm *PartitionManager) EnsureMonth(ctx context.Context, monthTime time.Time) error {
	if pm == nil || pm.db == nil {
		return fmt.Errorf("%w: nil partition manager db", port.ErrInvalidInput)
	}
	suffix, startMs, endMs := MonthBoundsUTC(monthTime)

	tx, err := pm.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres partitions: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tanpopo:partitions', 0))`); err != nil {
		return fmt.Errorf("postgres partitions: advisory xact lock: %w", err)
	}

	for _, tbl := range canonicalPartitionedTables {
		child := fmt.Sprintf("%s_%s", tbl.name, suffix)
		createDDL := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%d) TO (%d)`,
			child, tbl.name, startMs, endMs,
		)
		if _, err := tx.ExecContext(ctx, createDDL); err != nil {
			return fmt.Errorf("postgres partitions: create %s: %w", child, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ENABLE ROW LEVEL SECURITY`, child)); err != nil {
			return fmt.Errorf("postgres partitions: enable RLS on %s: %w", child, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s FORCE ROW LEVEL SECURITY`, child)); err != nil {
			return fmt.Errorf("postgres partitions: force RLS on %s: %w", child, err)
		}

		var exists int
		if err := tx.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM pg_policies WHERE schemaname = current_schema() AND tablename = $1 AND policyname = 'tenant_isolation'`,
			child,
		).Scan(&exists); err != nil {
			return fmt.Errorf("postgres partitions: check policy on %s: %w", child, err)
		}
		if exists == 0 {
			expr := `org_id = current_setting('app.current_org_id', true)`
			if tbl.allowSystemExpr {
				expr = `org_id = current_setting('app.current_org_id', true) OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')`
			}
			policyDDL := fmt.Sprintf(
				`CREATE POLICY tenant_isolation ON %s USING (%s) WITH CHECK (%s)`,
				child, expr, expr,
			)
			if _, err := tx.ExecContext(ctx, policyDDL); err != nil {
				return fmt.Errorf("postgres partitions: create RLS policy on %s: %w", child, err)
			}
		}
	}

	return tx.Commit()
}

// EnsureLookahead pre-creates the partition for ref's month plus aheadMonths future months.
func (pm *PartitionManager) EnsureLookahead(ctx context.Context, ref time.Time, aheadMonths int) error {
	if aheadMonths < 0 {
		aheadMonths = 0
	}
	base := time.Date(ref.UTC().Year(), ref.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= aheadMonths; i++ {
		if err := pm.EnsureMonth(ctx, base.AddDate(0, i, 0)); err != nil {
			return err
		}
	}
	return nil
}

// EnsureTimestampCovered ensures monthly partitions exist for a UTC epoch ms timestamp.
func (pm *PartitionManager) EnsureTimestampCovered(ctx context.Context, tsMs int64) error {
	return pm.EnsureMonth(ctx, time.UnixMilli(tsMs).UTC())
}

// ListPartitions lists existing child partition table names for a whitelisted parent table.
func (pm *PartitionManager) ListPartitions(ctx context.Context, parentTable string) ([]string, error) {
	if !isAllowedPartitionParent(parentTable) {
		return nil, fmt.Errorf("%w: unsupported partitioned table %q", port.ErrInvalidInput, parentTable)
	}
	rows, err := pm.db.QueryContext(
		ctx,
		`SELECT c.relname
		 FROM pg_inherits i
		 JOIN pg_class p ON p.oid = i.inhparent
		 JOIN pg_class c ON c.oid = i.inhrelid
		 JOIN pg_namespace n ON n.oid = p.relnamespace
		 WHERE n.nspname = current_schema() AND p.relname = $1
		 ORDER BY c.relname ASC`,
		parentTable,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres partitions: list %s: %w", parentTable, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// DetachAndDropExpired detaches and drops monthly partitions of parentTable whose
// exclusive upper bound end_ms <= cutoffMs without triggering row delete triggers.
func (pm *PartitionManager) DetachAndDropExpired(ctx context.Context, parentTable string, cutoffMs int64) ([]string, error) {
	if !isAllowedPartitionParent(parentTable) {
		return nil, fmt.Errorf("%w: unsupported partitioned table %q", port.ErrInvalidInput, parentTable)
	}
	children, err := pm.ListPartitions(ctx, parentTable)
	if err != nil {
		return nil, err
	}

	var dropped []string
	strictRe := regexp.MustCompile(`^` + regexp.QuoteMeta(parentTable) + `_(\d{4})_(\d{2})$`)
	quotedParent := pgx.Identifier{parentTable}.Sanitize()
	for _, child := range children {
		m := strictRe.FindStringSubmatch(child)
		if m == nil {
			continue
		}
		yr, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		if mo < 1 || mo > 12 {
			continue
		}
		_, _, endMs := MonthBoundsUTC(time.Date(yr, time.Month(mo), 1, 0, 0, 0, 0, time.UTC))
		if endMs > cutoffMs {
			continue
		}

		quotedChild := pgx.Identifier{child}.Sanitize()
		tx, err := pm.db.BeginTx(ctx, nil)
		if err != nil {
			return dropped, fmt.Errorf("postgres partitions: begin detach %s: %w", child, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s DETACH PARTITION %s`, quotedParent, quotedChild)); err != nil {
			_ = tx.Rollback()
			return dropped, fmt.Errorf("postgres partitions: detach %s: %w", child, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, quotedChild)); err != nil {
			_ = tx.Rollback()
			return dropped, fmt.Errorf("postgres partitions: drop %s: %w", child, err)
		}
		if err := tx.Commit(); err != nil {
			return dropped, fmt.Errorf("postgres partitions: commit drop %s: %w", child, err)
		}
		dropped = append(dropped, child)
	}
	return dropped, nil
}

func isAllowedPartitionParent(parent string) bool {
	for _, tbl := range canonicalPartitionedTables {
		if tbl.name == parent {
			return true
		}
	}
	return false
}
