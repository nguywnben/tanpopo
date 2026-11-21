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

	now := r.svc.nowMs()
	type expiredRow struct {
		ID         string
		OrgID      string
		RequestID  string
		PrevState  string
		HoldMicros int64
	}
	var reclaimed []expiredRow

	reapInOrg := func(orgScope string, maxRows int) error {
		var orgReclaimed []expiredRow
		txErr := r.svc.withOrgTx(ctx, orgScope, func(tx *dialectTx) error {
			orgReclaimed = orgReclaimed[:0]
			lockClause := ""
			if tx.dialect == storageport.DialectPostgres {
				lockClause = " FOR UPDATE SKIP LOCKED"
			}
			rows, err := tx.QueryContext(ctx, `
				SELECT id, org_id, request_id, state, hold_micros
				FROM budget_reservations
				WHERE state IN (?, ?) AND lease_expires_at_ms <= ?
				ORDER BY lease_expires_at_ms ASC, id ASC
				LIMIT ?`+lockClause,
				StateReservedHold, StateStreamingAccruing, now, maxRows,
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
				orgReclaimed = append(orgReclaimed, c)
			}
			return nil
		})
		if txErr != nil {
			return txErr
		}
		reclaimed = append(reclaimed, orgReclaimed...)
		return nil
	}

	var err error
	if r.svc.dialect == storageport.DialectPostgres {
		var orgIDs []string
		oErr := r.svc.withTx(ctx, func(rawTx *sql.Tx) error {
			if _, serr := rawTx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'true', true)`); serr != nil {
				return serr
			}
			orgRows, qErr := rawTx.QueryContext(ctx, `SELECT id FROM organizations ORDER BY id ASC`)
			if qErr != nil {
				return qErr
			}
			for orgRows.Next() {
				var oid string
				if sErr := orgRows.Scan(&oid); sErr != nil {
					_ = orgRows.Close()
					return sErr
				}
				orgIDs = append(orgIDs, oid)
			}
			iErr := orgRows.Err()
			_ = orgRows.Close()
			return iErr
		})
		if oErr != nil {
			err = oErr
		} else {

			for _, oid := range orgIDs {
				remaining := r.batchMax - len(reclaimed)
				if remaining <= 0 {
					break
				}
				if rErr := reapInOrg(oid, remaining); rErr != nil {
					err = rErr
					break
				}
			}
		}
	} else {
		err = reapInOrg("", r.batchMax)
	}
	if r.svc.audit != nil {
		for _, c := range reclaimed {
			meta, _ := json.Marshal(map[string]any{
				"previous_state":  c.PrevState,
				"reclaimed_at_ms": now,
				"hold_micros":     c.HoldMicros,
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
	if err != nil {
		return len(reclaimed), err
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
