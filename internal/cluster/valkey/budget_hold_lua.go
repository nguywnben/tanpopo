// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package valkey

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	DefaultCheckpointInterval   = 1 * time.Second
	DefaultCheckpointStaleAfter = 15 * time.Second
	defaultBudgetHoldTTLMs      = int64(300_000)
)

var (
	// ErrBudgetCeilingExceeded indicates the distributed ceiling check rejected the pre-hold.
	ErrBudgetCeilingExceeded = ledger.ErrBudgetExceeded
)

// HoldRequest describes a distributed budget hold check under {org_id}.
type HoldRequest struct {
	OrgID               string
	LedgerID            string
	ReservationID       string
	CeilingMicroUSD     int64
	PersistedSpentMicro int64
	EstimatedMicroUSD   int64
	HoldTTLMs           int64
}

// HoldOutcome summarizes the Valkey or PG fallback hold admission decision.
type HoldOutcome struct {
	Allowed             bool
	ActiveHoldMicroUSD  int64
	UsedPostgresFallback bool
}

// BudgetCoordinatorConfig wires Valkey Lua budget holds + PostgreSQL valkey_hold_checkpoints.
type BudgetCoordinatorConfig struct {
	Client               *Client
	DB                   *sql.DB
	Ledger               *ledger.Service
	NodeID               string
	CheckpointInterval   time.Duration
	CheckpointStaleAfter time.Duration
	SafetyMarginMicroUSD int64 // Conservative per-active-node safety margin during Valkey outages.
	Now                  func() time.Time
}

type nodeLedgerKey struct {
	orgID    string
	ledgerID string
}

type localHoldEntry struct {
	activeMicroUSD  int64
	inflightStreams int64
}

// BudgetCoordinator executes cluster-wide budget pre-holds under tp:v1:{<org_id>}:budget:*
// with 1-second valkey_hold_checkpoints persistence to PostgreSQL and PG failover fallback.
type BudgetCoordinator struct {
	client       *Client
	db           *sql.DB
	dialect      port.Dialect
	ledgerSvc    *ledger.Service
	nodeID       string
	interval     time.Duration
	staleAfter   time.Duration
	safetyMargin int64
	now          func() time.Time

	mu          sync.Mutex
	localHolds  map[nodeLedgerKey]*localHoldEntry
	degraded    atomic.Bool
}

// NewBudgetCoordinator initializes a BudgetCoordinator.
func NewBudgetCoordinator(cfg BudgetCoordinatorConfig) (*BudgetCoordinator, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("valkey: budget coordinator requires non-nil DB")
	}
	if cfg.NodeID == "" {
		cfg.NodeID = "node_default"
	}
	interval := cfg.CheckpointInterval
	if interval <= 0 {
		interval = DefaultCheckpointInterval
	}
	staleAfter := cfg.CheckpointStaleAfter
	if staleAfter <= 0 {
		staleAfter = DefaultCheckpointStaleAfter
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &BudgetCoordinator{
		client:       cfg.Client,
		db:           cfg.DB,
		dialect:      port.DetectDialect(context.Background(), cfg.DB),
		ledgerSvc:    cfg.Ledger,
		nodeID:       cfg.NodeID,
		interval:     interval,
		staleAfter:   staleAfter,
		safetyMargin: cfg.SafetyMarginMicroUSD,
		now:          nowFn,
		localHolds:   make(map[nodeLedgerKey]*localHoldEntry),
	}, nil
}

// IsDegraded reports whether budget holds are currently falling back to PostgreSQL.
func (b *BudgetCoordinator) IsDegraded() bool { return b != nil && b.degraded.Load() }

// Lua script atomically reserving estMicroUSD under {org_id}.
// KEYS: [1] budget_hold_key (tp:v1:{org}:budget:hold:<ledger>), [2] budget_req_key (tp:v1:{org}:budget:req:<resID>)
// ARGV: [1] ceiling, [2] persisted_spent, [3] est_micro, [4] ttl_ms
// Returns: {allowed (0|1), active_hold_micro_usd}
const luaAcquireHoldScript = `
local holdKey = KEYS[1]
local reqKey = KEYS[2]
local ceiling = tonumber(ARGV[1])
local persisted = tonumber(ARGV[2])
local est = tonumber(ARGV[3])
local ttlMs = tonumber(ARGV[4])

local existing = tonumber(redis.call('GET', reqKey))
local curHold = tonumber(redis.call('HGET', holdKey, 'active_micro_usd')) or 0
if curHold < 0 then curHold = 0 end
if existing then
	return {1, curHold}
end
if persisted + curHold + est > ceiling then
	return {0, curHold}
end
local nextHold = redis.call('HINCRBY', holdKey, 'active_micro_usd', est)
redis.call('SET', reqKey, tostring(est), 'PX', ttlMs)
redis.call('PEXPIRE', holdKey, ttlMs * 2)
return {1, nextHold}
`

// Lua script atomically releasing an active budget hold under {org_id}.
// KEYS: [1] budget_hold_key, [2] budget_req_key
// Returns: {released_amount, remaining_hold_micro_usd}
const luaReleaseHoldScript = `
local holdKey = KEYS[1]
local reqKey = KEYS[2]
local amt = tonumber(redis.call('GET', reqKey))
if not amt then
	local cur = tonumber(redis.call('HGET', holdKey, 'active_micro_usd')) or 0
	return {0, math.max(0, cur)}
end
redis.call('DEL', reqKey)
local nextHold = redis.call('HINCRBY', holdKey, 'active_micro_usd', -amt)
if nextHold < 0 then
	redis.call('HSET', holdKey, 'active_micro_usd', '0')
	nextHold = 0
end
return {amt, nextHold}
`

// AcquireHold reserves estMicroUSD atomically in Valkey under {org_id}, failing over
// directly to PostgreSQL serialized ceiling + valkey_hold_checkpoints margins on outage.
func (b *BudgetCoordinator) AcquireHold(ctx context.Context, req HoldRequest) (HoldOutcome, error) {
	if req.OrgID == "" || req.LedgerID == "" || req.ReservationID == "" {
		return HoldOutcome{}, fmt.Errorf("%w: org, ledger, and reservation IDs are required", ErrInvalidKeyComponent)
	}
	if req.EstimatedMicroUSD < 0 {
		return HoldOutcome{}, fmt.Errorf("%w: estimated micro-USD must be >= 0", ErrInvalidKeyComponent)
	}
	ttlMs := req.HoldTTLMs
	if ttlMs <= 0 {
		ttlMs = defaultBudgetHoldTTLMs
	}
	holdKey, err := Key(req.OrgID, "budget", "hold:"+req.LedgerID)
	if err != nil {
		return HoldOutcome{}, err
	}
	reqKey, err := Key(req.OrgID, "budget", "req:"+req.ReservationID)
	if err != nil {
		return HoldOutcome{}, err
	}
	if b.client != nil && b.client.raw != nil {
		res, lerr := b.client.EvalLua(
			ctx,
			luaAcquireHoldScript,
			[]string{holdKey, reqKey},
			strconv.FormatInt(req.CeilingMicroUSD, 10),
			strconv.FormatInt(req.PersistedSpentMicro, 10),
			strconv.FormatInt(req.EstimatedMicroUSD, 10),
			strconv.FormatInt(ttlMs, 10),
		)
		if lerr == nil {
			arr, perr := res.AsIntSlice()
			if perr == nil && len(arr) >= 2 {
				b.degraded.Store(false)
				if arr[0] == 0 {
					return HoldOutcome{Allowed: false, ActiveHoldMicroUSD: arr[1]}, ErrBudgetCeilingExceeded
				}
				b.adjustLocalHold(req.OrgID, req.LedgerID, req.EstimatedMicroUSD, 1)
				return HoldOutcome{Allowed: true, ActiveHoldMicroUSD: arr[1]}, nil
			}
			lerr = fmt.Errorf("unexpected lua AcquireHold reply: %w", perr)
		}
		if IsCrossSlotError(lerr) {
			return HoldOutcome{}, fmt.Errorf("%w: %v", ErrCrossOrgKeyViolation, lerr)
		}
		b.degraded.Store(true)
	} else {
		b.degraded.Store(true)
	}

	// Safe PostgreSQL fallback using persisted ledger spend + active cluster valkey_hold_checkpoints.
	return b.acquireHoldPostgresFallback(ctx, req)
}

// ReleaseHold releases a Valkey hold on settlement or cancellation and updates local checkpoint tracking.
func (b *BudgetCoordinator) ReleaseHold(ctx context.Context, orgID, ledgerID, reservationID string, fallbackEstMicro int64) error {
	holdKey, err := Key(orgID, "budget", "hold:"+ledgerID)
	if err != nil {
		return err
	}
	reqKey, err := Key(orgID, "budget", "req:"+reservationID)
	if err != nil {
		return err
	}
	released := fallbackEstMicro
	if b.client != nil && b.client.raw != nil {
		res, lerr := b.client.EvalLua(ctx, luaReleaseHoldScript, []string{holdKey, reqKey})
		if lerr == nil {
			if arr, perr := res.AsIntSlice(); perr == nil && len(arr) >= 2 {
				released = arr[0]
				b.degraded.Store(false)
			}
		} else if IsCrossSlotError(lerr) {
			return fmt.Errorf("%w: %v", ErrCrossOrgKeyViolation, lerr)
		}
	}
	b.adjustLocalHold(orgID, ledgerID, -released, -1)
	return nil
}

func (b *BudgetCoordinator) adjustLocalHold(orgID, ledgerID string, deltaMicro, deltaStreams int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := nodeLedgerKey{orgID: orgID, ledgerID: ledgerID}
	entry, ok := b.localHolds[k]
	if !ok {
		entry = &localHoldEntry{}
		b.localHolds[k] = entry
	}
	entry.activeMicroUSD += deltaMicro
	if entry.activeMicroUSD < 0 {
		entry.activeMicroUSD = 0
	}
	entry.inflightStreams += deltaStreams
	if entry.inflightStreams < 0 {
		entry.inflightStreams = 0
	}
}

func (b *BudgetCoordinator) acquireHoldPostgresFallback(ctx context.Context, req HoldRequest) (HoldOutcome, error) {
	nowMs := b.now().UnixMilli()
	cutoffMs := nowMs - b.staleAfter.Milliseconds()
	tx, err := b.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return HoldOutcome{UsedPostgresFallback: true}, fmt.Errorf("valkey: begin pg fallback tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if b.dialect == port.DialectPostgres {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, req.OrgID); err != nil {
			return HoldOutcome{UsedPostgresFallback: true}, fmt.Errorf("valkey: set app.current_org_id: %w", err)
		}
	}

	var hardCap, reserved, settled sql.NullInt64
	err = tx.QueryRowContext(ctx, port.Rebind(b.dialect, `
		SELECT hard_limit_micros, reserved_hold_micros,
		       (settled_exact_micros + settled_estimated_micros + negative_carryover_micros)
		FROM budget_ledgers
		WHERE org_id = ? AND id = ?`), req.OrgID, req.LedgerID).Scan(&hardCap, &reserved, &settled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return HoldOutcome{UsedPostgresFallback: true}, fmt.Errorf("valkey: read ledger fallback: %w", err)
	}

	var checkpointHold, activeNodes int64
	if err := tx.QueryRowContext(ctx, port.Rebind(b.dialect, `
		SELECT COALESCE(SUM(active_hold_micro_usd), 0), COUNT(*)
		FROM valkey_hold_checkpoints
		WHERE org_id = ? AND ledger_id = ? AND updated_at_ms >= ?`),
		req.OrgID, req.LedgerID, cutoffMs,
	).Scan(&checkpointHold, &activeNodes); err != nil {
		return HoldOutcome{UsedPostgresFallback: true}, fmt.Errorf("valkey: sum checkpoints: %w", err)
	}

	ceiling := req.CeilingMicroUSD
	persistedSpent := req.PersistedSpentMicro
	if hardCap.Valid {
		ceiling = hardCap.Int64
		persistedSpent = reserved.Int64 + settled.Int64
	}
	margin := activeNodes * b.safetyMargin
	effectiveUsed := persistedSpent + checkpointHold + margin + req.EstimatedMicroUSD
	if effectiveUsed > ceiling {
		return HoldOutcome{
			Allowed:              false,
			ActiveHoldMicroUSD:   checkpointHold + margin,
			UsedPostgresFallback: true,
		}, ErrBudgetCeilingExceeded
	}
	return HoldOutcome{
		Allowed:              true,
		ActiveHoldMicroUSD:   checkpointHold,
		UsedPostgresFallback: true,
	}, nil
}

// StartCheckpointLoop periodically flushes this node's active Valkey holds into
// valkey_hold_checkpoints until ctx is canceled.
func (b *BudgetCoordinator) StartCheckpointLoop(ctx context.Context) {
	if b == nil {
		return
	}
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = b.FlushCheckpoints(flushCtx)
			cancel()
			return
		case <-ticker.C:
			_ = b.FlushCheckpoints(ctx)
		}
	}
}

// FlushCheckpoints persists current node active holds per (org_id, ledger_id) into PostgreSQL/SQLite.
func (b *BudgetCoordinator) FlushCheckpoints(ctx context.Context) error {
	if b == nil || b.db == nil {
		return nil
	}
	b.mu.Lock()
	snapshot := make(map[nodeLedgerKey]localHoldEntry, len(b.localHolds))
	for k, v := range b.localHolds {
		snapshot[k] = *v
	}
	b.mu.Unlock()
	if len(snapshot) == 0 {
		return nil
	}
	nowMs := b.now().UnixMilli()
	for k, entry := range snapshot {
		if err := b.upsertCheckpoint(ctx, k.orgID, k.ledgerID, entry, nowMs); err != nil {
			return err
		}
	}
	return nil
}

func (b *BudgetCoordinator) upsertCheckpoint(ctx context.Context, orgID, ledgerID string, entry localHoldEntry, nowMs int64) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("valkey: begin checkpoint tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if b.dialect == port.DialectPostgres {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, orgID); err != nil {
			return fmt.Errorf("valkey: set app.current_org_id for checkpoint: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, port.Rebind(b.dialect, `
		INSERT INTO valkey_hold_checkpoints
			(org_id, ledger_id, node_id, active_hold_micro_usd, active_inflight_streams, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (org_id, ledger_id, node_id) DO UPDATE SET
			active_hold_micro_usd   = excluded.active_hold_micro_usd,
			active_inflight_streams = excluded.active_inflight_streams,
			updated_at_ms           = excluded.updated_at_ms`),
		orgID, ledgerID, b.nodeID, entry.activeMicroUSD, entry.inflightStreams, nowMs,
	); err != nil {
		return fmt.Errorf("valkey: upsert valkey_hold_checkpoints: %w", err)
	}
	return tx.Commit()
}

// RebuildLedgerHoldsFromPostgres reconstructs Valkey budget hold state (`tp:v1:{<org_id>}:budget:*`)
// cleanly from open reservations and checkpoints after a Valkey flush/failover (DR-02 / AC-3).
func RebuildLedgerHoldsFromPostgres(ctx context.Context, db *sql.DB, client *Client, nowMs int64) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("valkey: rebuild requires non-nil DB")
	}
	if client == nil || client.raw == nil {
		return 0, fmt.Errorf("valkey: rebuild requires connected Valkey client")
	}
	dialect := port.DetectDialect(ctx, db)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, fmt.Errorf("valkey: begin rebuild tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if dialect == port.DialectPostgres {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'true', true)`); err != nil {
			return 0, fmt.Errorf("valkey: set operator RLS: %w", err)
		}
	}
	orgRows, err := tx.QueryContext(ctx, `SELECT id FROM organizations ORDER BY id ASC`)
	if err != nil {
		return 0, fmt.Errorf("valkey: list orgs for rebuild: %w", err)
	}
	var orgIDs []string
	for orgRows.Next() {
		var id string
		if serr := orgRows.Scan(&id); serr != nil {
			_ = orgRows.Close()
			return 0, serr
		}
		orgIDs = append(orgIDs, id)
	}
	if rerr := orgRows.Err(); rerr != nil {
		_ = orgRows.Close()
		return 0, rerr
	}
	_ = orgRows.Close()

	rebuilt := 0
	for _, orgID := range orgIDs {
		if dialect == port.DialectPostgres {
			if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, orgID); serr != nil {
				return rebuilt, fmt.Errorf("valkey: set org RLS for rebuild: %w", serr)
			}
		}
		type openRes struct {
			id, ledgerID string
			reserved     int64
			expiresAtMs  int64
		}
		rows, rerr := tx.QueryContext(ctx, port.Rebind(dialect, `
			SELECT br.id, brl.ledger_id, brl.held_micros, br.lease_expires_at_ms
			FROM budget_reservations br
			JOIN budget_reservation_ledgers brl ON brl.org_id = br.org_id AND brl.reservation_id = br.id
			WHERE br.org_id = ? AND br.state IN ('RESERVED_HOLD', 'STREAMING_ACCRUING')
			ORDER BY br.id ASC, brl.ledger_id ASC`), orgID)
		if rerr != nil {
			return rebuilt, fmt.Errorf("valkey: list open reservations for %s: %w", orgID, rerr)
		}
		var list []openRes
		for rows.Next() {
			var r openRes
			if serr := rows.Scan(&r.id, &r.ledgerID, &r.reserved, &r.expiresAtMs); serr != nil {
				_ = rows.Close()
				return rebuilt, serr
			}
			list = append(list, r)
		}
		if ierr := rows.Err(); ierr != nil {
			_ = rows.Close()
			return rebuilt, ierr
		}
		_ = rows.Close()

		byLedger := make(map[string]int64)
		for _, r := range list {
			holdKey, kerr := Key(orgID, "budget", "hold:"+r.ledgerID)
			if kerr != nil {
				return rebuilt, kerr
			}
			reqKey, kerr := Key(orgID, "budget", "req:"+r.id)
			if kerr != nil {
				return rebuilt, kerr
			}
			ttlMs := r.expiresAtMs - nowMs
			if ttlMs < 5000 {
				ttlMs = defaultBudgetHoldTTLMs
			}
			if _, aerr := client.EvalLua(
				ctx,
				luaAcquireHoldScript,
				[]string{holdKey, reqKey},
				strconv.FormatInt(1<<60, 10),
				"0",
				strconv.FormatInt(r.reserved, 10),
				strconv.FormatInt(ttlMs, 10),
			); aerr != nil {
				return rebuilt, fmt.Errorf("valkey: restore hold %s: %w", r.id, aerr)
			}
			byLedger[r.ledgerID] += r.reserved
			rebuilt++
		}
		// Ensure ledger hold hashes also reflect exact aggregate active holds.
		for lid, sum := range byLedger {
			holdKey, _ := Key(orgID, "budget", "hold:"+lid)
			if err := client.raw.Do(ctx, client.raw.B().Hset().Key(holdKey).FieldValue().FieldValue("active_micro_usd", strconv.FormatInt(sum, 10)).Build()).Error(); err != nil {
				return rebuilt, fmt.Errorf("valkey: sync hold total for %s: %w", lid, err)
			}
		}
	}
	return rebuilt, nil
}
