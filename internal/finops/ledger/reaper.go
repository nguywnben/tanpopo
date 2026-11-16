// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
)

// OrphanReservationReaper periodically reclaims expired RESERVED_HOLD and
// STREAMING_ACCRUING budget reservations whose lease_expires_at_ms <= now (SM-07).
type OrphanReservationReaper struct {
	svc      *Service
	interval time.Duration
	batchMax int
}

// NewOrphanReservationReaper constructs an OrphanReservationReaper bound to svc.
func NewOrphanReservationReaper(svc *Service, sweepInterval time.Duration, batchLimit int) (*OrphanReservationReaper, error) {
	if svc == nil {
		return nil, ErrInvalidInput
	}
	if sweepInterval <= 0 {
		sweepInterval = 15 * time.Second
	}
	if batchLimit <= 0 {
		batchLimit = 200
	}
	return &OrphanReservationReaper{
		svc:      svc,
		interval: sweepInterval,
		batchMax: batchLimit,
	}, nil
}

// ReapOnce scans for expired active holds and transitions them atomically to
// EXPIRED_RECLAIMED, restoring held_micros on linked budget_ledgers and writing
// audited reclamation records.
func (r *OrphanReservationReaper) ReapOnce(ctx context.Context) (int, error) {
	if r == nil || r.svc == nil {
		return 0, ErrInvalidInput
	}

	r.svc.txMu.Lock()
	defer r.svc.txMu.Unlock()

	now := r.svc.nowMs()
	type expiredRow struct {
		ID         string
		OrgID      string
		RequestID  string
		PrevState  string
		HoldMicros int64
	}
	var reclaimed []expiredRow

	err := r.svc.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT id, org_id, request_id, state, hold_micros
			FROM budget_reservations
			WHERE state IN (?, ?) AND lease_expires_at_ms <= ?
			ORDER BY lease_expires_at_ms ASC, id ASC
			LIMIT ?`,
			StateReservedHold, StateStreamingAccruing, now, r.batchMax,
		)
		if err != nil {
			return err
		}
		var candidates []expiredRow
		for rows.Next() {
			var c expiredRow
			if sErr := rows.Scan(&c.ID, &c.OrgID, &c.RequestID, &c.PrevState, &c.HoldMicros); sErr != nil {
				_ = rows.Close()
				return sErr
			}
			candidates = append(candidates, c)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()

		for _, c := range candidates {
			res, uErr := tx.ExecContext(ctx, `
				UPDATE budget_reservations
				SET state = ?, updated_at_ms = ?
				WHERE org_id = ? AND id = ? AND state IN (?, ?)`,
				StateExpiredReclaimed, now, c.OrgID, c.ID, StateReservedHold, StateStreamingAccruing,
			)
			if uErr != nil {
				return uErr
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				continue
			}
			if rErr := releaseLinkedHoldsTx(ctx, tx, c.OrgID, c.ID, now); rErr != nil {
				return rErr
			}
			reclaimed = append(reclaimed, c)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	if r.svc.audit != nil {
		for _, c := range reclaimed {
			meta, _ := json.Marshal(map[string]any{
				"previous_state": c.PrevState,
				"reclaimed_at_ms": now,
				"hold_micros":    c.HoldMicros,
			})
			orgCopy := c.OrgID
			reqCopy := c.RequestID
			_ = r.svc.audit.Append(ctx, storageport.AuditEvent{
				ID:           idgen.MustNew(idgen.PrefixAudit),
				CreatedAtMs:  now,
				OrgID:        &orgCopy,
				ActorKind:    "system",
				ActorID:      "orphan_reservation_reaper",
				EntryPoint:   "system_reaper",
				EventType:    "BUDGET_RESERVATION_RECLAIMED",
				TargetType:   "budget_reservation",
				TargetID:     c.ID,
				RequestID:    &reqCopy,
				MetadataJSON: string(meta),
			})
		}
	}
	return len(reclaimed), nil
}

// Start runs the periodic reaper loop in a goroutine and returns a wait function
// that cancels cleanly when ctx is canceled.
func (r *OrphanReservationReaper) Start(ctx context.Context) func() {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if _, err := r.ReapOnce(runCtx); err != nil && !errors.Is(err, context.Canceled) {
					continue
				}
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}
