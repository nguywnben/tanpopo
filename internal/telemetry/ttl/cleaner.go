// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package ttl implements Tanpopo's background retention cleaner for expired
// Project-DEK-encrypted debug_payload_blobs (strict <= 72h TTL) and historical
// request_traces_meta metadata records (SPEC-OPS-02 Section 2 / TP-IMP-0022).
package ttl

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// DefaultSweepInterval controls background purge cadence (SPEC-OPS-02: 10 minutes).
	DefaultSweepInterval = 10 * time.Minute
	// DefaultBatchSize bounds each SQLite micro-batch purge to prevent WAL writer stalls.
	DefaultBatchSize = 500
	// DefaultTraceRetentionDays retains metadata-only trace rows for 30 days (configurable 7..90 days).
	DefaultTraceRetentionDays = 30
	// MinTraceRetentionDays enforces minimum trace metadata retention (SPEC-OPS-02).
	MinTraceRetentionDays = 7
	// MaxTraceRetentionDays enforces maximum trace metadata retention (SPEC-OPS-02).
	MaxTraceRetentionDays = 90
	// MaxDebugBlobTTLMillis enforces the strict 72-hour maximum on debug_payload_blobs.
	MaxDebugBlobTTLMillis int64 = 72 * 60 * 60 * 1000
)

// Config configures the telemetry retention cleaner.
type Config struct {
	DB                 *sql.DB
	SweepInterval      time.Duration
	TraceRetentionDays int
	BatchSize          int
	NowMs              func() int64
	OnError            func(error)
}

// PurgeSummary reports deleted row counts from a single purge cycle.
type PurgeSummary struct {
	DeletedDebugBlobs int64
	DeletedTracesMeta int64
}

// Cleaner reaps expired debug_payload_blobs and aged request_traces_meta rows in yielded micro-batches.
type Cleaner struct {
	db             *sql.DB
	dialect        port.Dialect
	sweepInterval  time.Duration
	traceRetention time.Duration
	batchSize      int
	nowMs          func() int64
	onError        func(error)
}

// NewCleaner validates configuration and constructs a telemetry TTL Cleaner.
func NewCleaner(cfg Config) (*Cleaner, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("ttl: non-nil *sql.DB is required")
	}
	interval := cfg.SweepInterval
	if interval <= 0 {
		interval = DefaultSweepInterval
	}
	retDays := cfg.TraceRetentionDays
	if retDays <= 0 {
		retDays = DefaultTraceRetentionDays
	}
	if retDays < MinTraceRetentionDays || retDays > MaxTraceRetentionDays {
		return nil, fmt.Errorf("ttl: trace retention days %d must be between %d and %d", retDays, MinTraceRetentionDays, MaxTraceRetentionDays)
	}
	batch := cfg.BatchSize
	if batch <= 0 {
		batch = DefaultBatchSize
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	return &Cleaner{
		db:             cfg.DB,
		dialect:        port.DetectDialect(context.Background(), cfg.DB),
		sweepInterval:  interval,
		traceRetention: time.Duration(retDays) * 24 * time.Hour,
		batchSize:      batch,
		nowMs:          nowFn,
		onError:        cfg.OnError,
	}, nil
}

// PurgeOnce executes yielded micro-batch deletions of expired debug_payload_blobs
// (expires_at_ms <= nowMs) and aged request_traces_meta rows (created_at_ms <= cutoffMs).
func (c *Cleaner) PurgeOnce(ctx context.Context) (PurgeSummary, error) {
	if c == nil || c.db == nil {
		return PurgeSummary{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	nowMs := c.nowMs()
	traceCutoffMs := nowMs - c.traceRetention.Milliseconds()
	var sum PurgeSummary

	if c.dialect == port.DialectPostgres {
		return c.purgeOncePostgres(ctx, nowMs, traceCutoffMs)
	}

	for {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		res, err := c.db.ExecContext(ctx, `
			DELETE FROM debug_payload_blobs
			WHERE rowid IN (
				SELECT rowid FROM debug_payload_blobs
				WHERE expires_at_ms <= ?
				LIMIT ?
			)`, nowMs, c.batchSize)
		if err != nil {
			return sum, fmt.Errorf("ttl: purge expired debug_payload_blobs: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return sum, fmt.Errorf("ttl: count deleted debug_payload_blobs: %w", err)
		}
		sum.DeletedDebugBlobs += n
		if n < int64(c.batchSize) {
			break
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		res, err := c.db.ExecContext(ctx, `
			DELETE FROM request_traces_meta
			WHERE rowid IN (
				SELECT rowid FROM request_traces_meta
				WHERE created_at_ms <= ?
				LIMIT ?
			)`, traceCutoffMs, c.batchSize)
		if err != nil {
			return sum, fmt.Errorf("ttl: purge aged request_traces_meta: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return sum, fmt.Errorf("ttl: count deleted request_traces_meta: %w", err)
		}
		sum.DeletedTracesMeta += n
		if n < int64(c.batchSize) {
			break
		}
	}

	return sum, nil
}

func (c *Cleaner) purgeOncePostgres(ctx context.Context, nowMs, traceCutoffMs int64) (PurgeSummary, error) {
	var sum PurgeSummary
	listTx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return sum, fmt.Errorf("ttl: begin list orgs tx: %w", err)
	}
	orgIDs, listErr := port.ListOrgIDsTx(ctx, listTx, c.dialect)
	_ = listTx.Rollback()
	if listErr != nil {
		return sum, listErr
	}

	delBlobsSQL := port.Rebind(c.dialect, `
		DELETE FROM debug_payload_blobs
		WHERE (org_id, project_id, request_id) IN (
			SELECT org_id, project_id, request_id FROM debug_payload_blobs
			WHERE org_id = ? AND expires_at_ms <= ?
			LIMIT ?
		)`)
	delTracesSQL := port.Rebind(c.dialect, `
		DELETE FROM request_traces_meta
		WHERE (org_id, request_id, created_at_ms) IN (
			SELECT org_id, request_id, created_at_ms FROM request_traces_meta
			WHERE org_id = ? AND created_at_ms <= ?
			LIMIT ?
		)`)

	for _, orgID := range orgIDs {
		for {
			if err := ctx.Err(); err != nil {
				return sum, err
			}
			n, err := c.execOrgBatchDelete(ctx, orgID, delBlobsSQL, orgID, nowMs, c.batchSize)
			if err != nil {
				return sum, fmt.Errorf("ttl: purge expired debug_payload_blobs: %w", err)
			}
			sum.DeletedDebugBlobs += n
			if n < int64(c.batchSize) {
				break
			}
		}
		for {
			if err := ctx.Err(); err != nil {
				return sum, err
			}
			n, err := c.execOrgBatchDelete(ctx, orgID, delTracesSQL, orgID, traceCutoffMs, c.batchSize)
			if err != nil {
				return sum, fmt.Errorf("ttl: purge aged request_traces_meta: %w", err)
			}
			sum.DeletedTracesMeta += n
			if n < int64(c.batchSize) {
				break
			}
		}
	}
	return sum, nil
}

func (c *Cleaner) execOrgBatchDelete(ctx context.Context, orgID, query string, args ...any) (int64, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, c.dialect, orgID); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// Start launches the background purge loop until ctx is canceled and returns a stop
// waiter function that blocks until the background goroutine exits cleanly (goleak-safe).
func (c *Cleaner) Start(ctx context.Context) (stop func()) {
	if c == nil {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	loopCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(c.sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				if _, err := c.PurgeOnce(loopCtx); err != nil && loopCtx.Err() == nil && c.onError != nil {
					c.onError(err)
				}
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}
