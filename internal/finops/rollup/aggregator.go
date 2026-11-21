// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package rollup aggregates immutable usage_settlements records into idempotent
// hourly buckets in usage_rollups_hourly, maintaining segregated exact_micro_usd,
// estimated_micro_usd, and unmetered_request_count columns (TP-IMP-0021).
package rollup

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// HourMs is the fixed 1-hour bucket duration in milliseconds.
	HourMs int64 = 3_600_000
)

// HourlyRow represents one bucket in usage_rollups_hourly.
type HourlyRow struct {
	OrgID                 string
	ProjectID             string
	EnvID                 string
	VirtualKeyID          string
	LogicalModelSlug      string
	BucketStartMs         int64
	RequestCount          int64
	ExactMicroUSD         int64
	EstimatedMicroUSD     int64
	UnmeteredRequestCount int64
	InputTokensSum        int64
	OutputTokensSum       int64
	UpdatedAtMs           int64
}

// Aggregator computes deterministic hourly usage rollups from usage_settlements.
type Aggregator struct {
	db      *sql.DB
	dialect port.Dialect
	nowMs   func() int64
}

// NewAggregator constructs an hourly usage rollup Aggregator.
func NewAggregator(db *sql.DB, nowMsFn func() int64) (*Aggregator, error) {
	if db == nil {
		return nil, errors.New("rollup: nil *sql.DB")
	}
	if nowMsFn == nil {
		nowMsFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	return &Aggregator{
		db:      db,
		dialect: port.DetectDialect(context.Background(), db),
		nowMs:   nowMsFn,
	}, nil
}

// AggregateHour idempotently recomputes all rollup buckets for the hour containing anyTimestampMs.
func (a *Aggregator) AggregateHour(ctx context.Context, anyTimestampMs int64) (int, error) {
	start := FloorHourMs(anyTimestampMs)
	return a.AggregateRange(ctx, start, start+HourMs)
}

// AggregateRange idempotently computes and upserts all hourly buckets intersecting [fromMs, toMs).
func (a *Aggregator) AggregateRange(ctx context.Context, fromMs, toMs int64) (int, error) {
	if fromMs < 0 || toMs <= fromMs {
		return 0, errors.New("rollup: invalid time window")
	}
	startBucket := FloorHourMs(fromMs)
	endExclusive := FloorHourMs(toMs-1) + HourMs
	now := a.nowMs()

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	aggregateScope := func(orgScope string) (int, error) {
		if a.dialect == port.DialectPostgres {
			if err := port.SetOrgRLSTx(ctx, tx, a.dialect, orgScope); err != nil {
				return 0, err
			}
		}
		var filterSQL string
		args := []any{HourMs, HourMs, startBucket, endExclusive}
		if orgScope != "" {
			filterSQL = ` AND org_id = ?`
			args = append(args, orgScope)
		}
		rows, qerr := tx.QueryContext(ctx, port.Rebind(a.dialect, `
			SELECT
				org_id,
				project_id,
				env_id,
				virtual_key_id,
				COALESCE(NULLIF(TRIM(logical_model_slug), ''), 'unknown') AS model_slug,
				(settled_at_ms / ?) * ? AS bucket_start_ms,
				COUNT(*) AS request_count,
				SUM(CASE WHEN usage_accuracy = 'EXACT' THEN total_cost_micros ELSE 0 END) AS exact_micro_usd,
				SUM(CASE WHEN usage_accuracy = 'ESTIMATED' THEN total_cost_micros ELSE 0 END) AS estimated_micro_usd,
				SUM(CASE WHEN usage_accuracy = 'UNMETERED_FLAGGED' THEN 1 ELSE 0 END) AS unmetered_request_count,
				SUM(input_tokens) AS input_tokens_sum,
				SUM(output_tokens) AS output_tokens_sum
			FROM usage_settlements
			WHERE settled_at_ms >= ? AND settled_at_ms < ?`+filterSQL+`
			GROUP BY org_id, project_id, env_id, virtual_key_id, model_slug, bucket_start_ms
			ORDER BY bucket_start_ms ASC, org_id ASC, project_id ASC, env_id ASC, virtual_key_id ASC, model_slug ASC`),
			args...,
		)
		if qerr != nil {
			return 0, qerr
		}
		var computed []HourlyRow
		for rows.Next() {
			var r HourlyRow
			if sErr := rows.Scan(
				&r.OrgID, &r.ProjectID, &r.EnvID, &r.VirtualKeyID, &r.LogicalModelSlug,
				&r.BucketStartMs, &r.RequestCount, &r.ExactMicroUSD, &r.EstimatedMicroUSD,
				&r.UnmeteredRequestCount, &r.InputTokensSum, &r.OutputTokensSum,
			); sErr != nil {
				_ = rows.Close()
				return 0, sErr
			}
			r.UpdatedAtMs = now
			computed = append(computed, r)
		}
		if rerr := rows.Err(); rerr != nil {
			_ = rows.Close()
			return 0, rerr
		}
		_ = rows.Close()

		upsertSQL := port.Rebind(a.dialect, `
			INSERT INTO usage_rollups_hourly (
				org_id, project_id, env_id, virtual_key_id, logical_model_slug, bucket_start_ms,
				request_count, exact_micro_usd, estimated_micro_usd, unmetered_request_count,
				input_tokens_sum, output_tokens_sum, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (org_id, project_id, env_id, virtual_key_id, logical_model_slug, bucket_start_ms)
			DO UPDATE SET
				request_count           = excluded.request_count,
				exact_micro_usd         = excluded.exact_micro_usd,
				estimated_micro_usd     = excluded.estimated_micro_usd,
				unmetered_request_count = excluded.unmetered_request_count,
				input_tokens_sum        = excluded.input_tokens_sum,
				output_tokens_sum       = excluded.output_tokens_sum,
				updated_at_ms           = excluded.updated_at_ms`)
		for _, r := range computed {
			if _, uErr := tx.ExecContext(ctx, upsertSQL,
				r.OrgID, r.ProjectID, r.EnvID, r.VirtualKeyID, r.LogicalModelSlug, r.BucketStartMs,
				r.RequestCount, r.ExactMicroUSD, r.EstimatedMicroUSD, r.UnmeteredRequestCount,
				r.InputTokensSum, r.OutputTokensSum, r.UpdatedAtMs,
			); uErr != nil {
				return 0, uErr
			}
		}
		return len(computed), nil
	}

	total := 0
	if a.dialect == port.DialectPostgres {
		orgIDs, lerr := port.ListOrgIDsTx(ctx, tx, a.dialect)
		if lerr != nil {
			return 0, lerr
		}
		for _, orgID := range orgIDs {
			n, aerr := aggregateScope(orgID)
			if aerr != nil {
				return 0, aerr
			}
			total += n
		}
	} else {
		n, aerr := aggregateScope("")
		if aerr != nil {
			return 0, aerr
		}
		total = n
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}

// QueryOrgRollups lists aggregated hourly rollups for orgID over [fromMs, toMs).
func (a *Aggregator) QueryOrgRollups(ctx context.Context, orgID string, fromMs, toMs int64) ([]HourlyRow, error) {
	if strings.TrimSpace(orgID) == "" || fromMs < 0 || toMs <= fromMs {
		return nil, errors.New("rollup: invalid query arguments")
	}
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, a.dialect, orgID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, port.Rebind(a.dialect, `
		SELECT org_id, project_id, env_id, virtual_key_id, logical_model_slug, bucket_start_ms,
		       request_count, exact_micro_usd, estimated_micro_usd, unmetered_request_count,
		       input_tokens_sum, output_tokens_sum, updated_at_ms
		FROM usage_rollups_hourly
		WHERE org_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
		ORDER BY bucket_start_ms ASC, project_id ASC, logical_model_slug ASC`),
		orgID, FloorHourMs(fromMs), toMs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HourlyRow
	for rows.Next() {
		var r HourlyRow
		if sErr := rows.Scan(
			&r.OrgID, &r.ProjectID, &r.EnvID, &r.VirtualKeyID, &r.LogicalModelSlug, &r.BucketStartMs,
			&r.RequestCount, &r.ExactMicroUSD, &r.EstimatedMicroUSD, &r.UnmeteredRequestCount,
			&r.InputTokensSum, &r.OutputTokensSum, &r.UpdatedAtMs,
		); sErr != nil {
			return nil, sErr
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FloorHourMs truncates tsMs down to the containing UTC hour boundary.
func FloorHourMs(tsMs int64) int64 {
	if tsMs <= 0 {
		return 0
	}
	return (tsMs / HourMs) * HourMs
}
