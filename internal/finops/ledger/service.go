// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package ledger implements Tanpopo's two-phase micro-USD budget reservation,
// idempotent settlement, negative balance carryover (AC-3), soft/unmetered
// alerting (ALT-BUDGET-*, ALT-UNMETERED-SPEND -> INC-01), and SM-07 orphan
// lease reclamation (TP-IMP-0021).
package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
)

// SM-07 BudgetReservation states matching 0012_budget_ledger.sql CHECK constraints.
const (
	StateReservedHold            = "RESERVED_HOLD"
	StateReleasedUnused          = "RELEASED_UNUSED"
	StateStreamingAccruing       = "STREAMING_ACCRUING"
	StateSettledExact            = "SETTLED_EXACT"
	StateSettledEstimated        = "SETTLED_ESTIMATED"
	StateSettledUnmeteredFlagged = "SETTLED_UNMETERED_FLAGGED"
	StateExpiredReclaimed        = "EXPIRED_RECLAIMED"
)

// Budget scope types and periods matching 0012_budget_ledger.sql CHECK constraints.
const (
	ScopeOrg        = "ORG"
	ScopeProject    = "PROJECT"
	ScopeEnv        = "ENV"
	ScopeVirtualKey = "VIRTUAL_KEY"

	PeriodDaily         = "DAILY"
	PeriodMonthly       = "MONTHLY"
	PeriodTotalLifetime = "TOTAL_LIFETIME"

	PlaneProvider = "PROVIDER"
	PlaneMCP      = "MCP"
	PlaneA2A      = "A2A"

	DefaultLeaseTTLMs int64 = 90_000
)

var (
	// ErrBudgetExceeded indicates insufficient remaining micro-USD headroom (HTTP 429 project_budget_exceeded).
	ErrBudgetExceeded = errors.New("429 project_budget_exceeded")
	// ErrNotFound indicates the requested ledger or reservation does not exist in the tenant org.
	ErrNotFound = storageport.ErrNotFound
	// ErrInvalidInput rejects missing required identifiers or negative monetary bounds.
	ErrInvalidInput = storageport.ErrInvalidInput
	// ErrInvalidStateTransition rejects invalid SM-07 reservation transitions.
	ErrInvalidStateTransition = errors.New("ledger: invalid reservation state transition")
	// ErrOverflow rejects signed 64-bit integer addition overflows in ledger accounting.
	ErrOverflow = errors.New("ledger: 64-bit integer overflow")
)

// BudgetExceededError carries structured diagnostics for 429 project_budget_exceeded responses.
type BudgetExceededError struct {
	LedgerID        string
	ScopeType       string
	ScopeID         string
	AvailableMicros int64
	RequestedMicros int64
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("%s: ledger=%s scope=%s:%s available=%d requested=%d",
		ErrBudgetExceeded.Error(), e.LedgerID, e.ScopeType, e.ScopeID, e.AvailableMicros, e.RequestedMicros)
}

func (e *BudgetExceededError) Unwrap() error { return ErrBudgetExceeded }

// AlertEvent represents a budget threshold or unmetered-spend alert linked to runbook INC-01.
type AlertEvent struct {
	Code           string
	Runbook        string
	OrgID          string
	LedgerID       string
	ScopeType      string
	ScopeID        string
	ThresholdPct   int
	UtilizationPct int
	RequestID      string
	SettlementID   string
	DeploymentID   string
	LogicalModel   string
	OccurredAtMs   int64
}

// AlertSink receives soft-threshold (ALT-BUDGET-*) and unmetered-spend (ALT-UNMETERED-SPEND) events.
type AlertSink interface {
	EmitBudgetAlert(ctx context.Context, ev AlertEvent)
}

// AlertSinkFunc adapts a function into an AlertSink.
type AlertSinkFunc func(ctx context.Context, ev AlertEvent)

// EmitBudgetAlert invokes f with ev.
func (f AlertSinkFunc) EmitBudgetAlert(ctx context.Context, ev AlertEvent) {
	if f != nil {
		f(ctx, ev)
	}
}

// Config configures the two-phase budget ledger Service.
type Config struct {
	DB          *sql.DB
	Audit       storageport.AuditSink
	Alerts      AlertSink
	AlertOutbox func(context.Context, *sql.Tx, AlertEvent) error
	NowMs       func() int64
	LeaseTTLMs  int64
}

// BudgetLedger mirrors a row in budget_ledgers.
type BudgetLedger struct {
	ID                      string
	OrgID                   string
	ScopeType               string
	ScopeID                 string
	PeriodKind              string
	PeriodStartMs           int64
	PeriodEndMs             int64
	HardLimitMicros         int64
	ReservedHoldMicros      int64
	SettledExactMicros      int64
	SettledEstimatedMicros  int64
	NegativeCarryoverMicros int64
	SoftAlertThresholdsPct  []int
	LastAlertedThresholdPct int
	Version                 int64
	CreatedAtMs             int64
	UpdatedAtMs             int64
}

// AvailableBalance returns remaining spendable headroom (may be negative under carryover AC-3).
func (b BudgetLedger) AvailableBalance() int64 {
	return b.HardLimitMicros - b.SettledExactMicros - b.SettledEstimatedMicros - b.ReservedHoldMicros
}

// UpsertLedgerParams configures creating or updating a scoped budget ledger.
type UpsertLedgerParams struct {
	ID                     string
	OrgID                  string
	ScopeType              string
	ScopeID                string
	PeriodKind             string
	PeriodStartMs          int64
	PeriodEndMs            int64
	HardLimitMicros        int64
	SoftAlertThresholdsPct []int
}

// ReserveRequest specifies Phase 1 pre-hold inputs across Org/Project/Env/VirtualKey scopes.
type ReserveRequest struct {
	OrgID        string
	ProjectID    string
	EnvID        string
	VirtualKeyID string
	RequestID    string
	HoldMicros   int64
	LeaseTTLMs   int64
}

// DeltaReserveRequest increases a pre-stream reservation hold during higher-cost failover (TP-IMP-0021 §4).
type DeltaReserveRequest struct {
	OrgID         string
	ReservationID string
	NewHoldMicros int64
	LeaseTTLMs    int64
}

// Reservation represents a row in budget_reservations plus its attached ledger IDs.
type Reservation struct {
	ID               string
	RequestID        string
	OrgID            string
	ProjectID        string
	EnvID            string
	VirtualKeyID     string
	State            string
	HoldMicros       int64
	LeaseExpiresAtMs int64
	LedgerIDs        []string
	CreatedAtMs      int64
	UpdatedAtMs      int64
}

// SettleRequest specifies Phase 2 settlement inputs.
type SettleRequest struct {
	OrgID            string
	ReservationID    string
	RequestID        string
	ProjectID        string
	EnvID            string
	VirtualKeyID     string
	Plane            string
	DeploymentID     string
	LogicalModelSlug string
	Rate             pricing.RateCard
	Usage            pricing.UsageInput
}

// SettlementResult reports the immutable settlement record and whether the call was an idempotent no-op.
type SettlementResult struct {
	SettlementID     string
	ReservationID    string
	RequestID        string
	SettledAtMs      int64
	Accuracy         string
	TotalCostMicros  int64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	UnmeteredFlagged bool
	AlreadySettled   bool
}

type dialectTx struct {
	tx      *sql.Tx
	dialect storageport.Dialect
}

func (d *dialectTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.tx.ExecContext(ctx, storageport.Rebind(d.dialect, query), args...)
}

func (d *dialectTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.tx.QueryContext(ctx, storageport.Rebind(d.dialect, query), args...)
}

func (d *dialectTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.tx.QueryRowContext(ctx, storageport.Rebind(d.dialect, query), args...)
}

func (d *dialectTx) forUpdateClause() string {
	if d.dialect == storageport.DialectPostgres {
		return " FOR UPDATE"
	}
	return ""
}

// Service manages atomic two-phase integer micro-USD budget accounting over SQLite/PostgreSQL.
type Service struct {
	db          *sql.DB
	dialect     storageport.Dialect
	audit       storageport.AuditSink
	alerts      AlertSink
	alertOutbox func(context.Context, *sql.Tx, AlertEvent) error
	nowMs       func() int64
	leaseTTLMs  int64

	txMu sync.Mutex

	unmeteredCount atomic.Int64
	softAlertCount atomic.Int64
}

// NewService constructs a validated two-phase budget ledger Service.
func NewService(cfg Config) (*Service, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: nil *sql.DB", ErrInvalidInput)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	ttl := cfg.LeaseTTLMs
	if ttl <= 0 {
		ttl = DefaultLeaseTTLMs
	}
	return &Service{
		db:          cfg.DB,
		dialect:     storageport.DetectDialect(context.Background(), cfg.DB),
		audit:       cfg.Audit,
		alerts:      cfg.Alerts,
		alertOutbox: cfg.AlertOutbox,
		nowMs:       nowFn,
		leaseTTLMs:  ttl,
	}, nil
}

// UnmeteredSpendCount returns total newly recorded UNMETERED_FLAGGED settlements (ALT-UNMETERED-SPEND).
func (s *Service) UnmeteredSpendCount() int64 {
	return s.unmeteredCount.Load()
}

// SoftThresholdAlertCount returns total newly emitted ALT-BUDGET-* soft threshold alerts.
func (s *Service) SoftThresholdAlertCount() int64 {
	return s.softAlertCount.Load()
}

// LeaseTTLMs returns the configured reservation lease duration in milliseconds.
func (s *Service) LeaseTTLMs() int64 {
	if s == nil || s.leaseTTLMs <= 0 {
		return DefaultLeaseTTLMs
	}
	return s.leaseTTLMs
}

// UpsertLedger creates or updates a scoped budget ledger, recalculating negative_carryover_micros
// immediately so budget top-ups clear carryover cleanly (AC-3).
func (s *Service) UpsertLedger(ctx context.Context, params UpsertLedgerParams) (*BudgetLedger, error) {
	if strings.TrimSpace(params.OrgID) == "" ||
		len(strings.TrimSpace(params.ID)) > 128 ||
		!isValidScopeType(params.ScopeType) ||
		strings.TrimSpace(params.ScopeID) == "" ||
		len(strings.TrimSpace(params.ScopeID)) > 128 ||
		(params.ScopeType == ScopeOrg && params.ScopeID != params.OrgID) ||
		!isValidPeriodKind(params.PeriodKind) ||
		params.PeriodEndMs <= params.PeriodStartMs ||
		params.HardLimitMicros < 0 {
		return nil, ErrInvalidInput
	}
	thresholds := normalizeThresholds(params.SoftAlertThresholdsPct)
	threshBytes, err := json.Marshal(thresholds)
	if err != nil {
		return nil, err
	}
	threshJSON := string(threshBytes)

	now := s.nowMs()
	var out *BudgetLedger
	err = s.withOrgTx(ctx, params.OrgID, func(tx *dialectTx) error {
		var existingID string
		var exactMicros, estMicros int64
		qErr := tx.QueryRowContext(ctx, `
			SELECT id, settled_exact_micros, settled_estimated_micros
			FROM budget_ledgers
			WHERE org_id = ? AND scope_type = ? AND scope_id = ? AND period_kind = ? AND period_start_ms = ?`+tx.forUpdateClause(),
			params.OrgID, params.ScopeType, params.ScopeID, params.PeriodKind, params.PeriodStartMs,
		).Scan(&existingID, &exactMicros, &estMicros)

		switch {
		case qErr == nil:
			totalSettled, ok := addInt64Checked(exactMicros, estMicros)
			if !ok {
				return ErrOverflow
			}
			carryover := computeCarryover(totalSettled, params.HardLimitMicros)
			// Allow re-arming higher soft thresholds if limit was topped up.
			newPct := computeUtilizationPct(totalSettled, params.HardLimitMicros)
			if _, uErr := tx.ExecContext(ctx, `
				UPDATE budget_ledgers
				SET period_end_ms = ?,
				    hard_limit_micros = ?,
				    negative_carryover_micros = ?,
				    soft_alert_thresholds_pct_json = ?,
				    last_alerted_threshold_pct = CASE WHEN ? < last_alerted_threshold_pct THEN ? ELSE last_alerted_threshold_pct END,
				    version = version + 1,
				    updated_at_ms = ?
				WHERE org_id = ? AND id = ?`,
				params.PeriodEndMs, params.HardLimitMicros, carryover, threshJSON,
				newPct, newPct, now, params.OrgID, existingID,
			); uErr != nil {
				return uErr
			}
		case errors.Is(qErr, sql.ErrNoRows):
			existingID = strings.TrimSpace(params.ID)
			if existingID == "" {
				existingID = idgen.MustNew(idgen.PrefixBudget)
			}
			settledExact, settledEst, sumErr := sumSettledForScopePeriodTx(ctx, tx, params.OrgID, params.ScopeType, params.ScopeID, params.PeriodStartMs, params.PeriodEndMs)
			if sumErr != nil {
				return sumErr
			}
			totalSettled, ok := addInt64Checked(settledExact, settledEst)
			if !ok {
				return ErrOverflow
			}
			carryover := computeCarryover(totalSettled, params.HardLimitMicros)
			if _, iErr := tx.ExecContext(ctx, `
				INSERT INTO budget_ledgers (
					id, org_id, scope_type, scope_id, period_kind, period_start_ms, period_end_ms,
					hard_limit_micros, settled_exact_micros, settled_estimated_micros, negative_carryover_micros,
					soft_alert_thresholds_pct_json, created_at_ms, updated_at_ms
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				existingID, params.OrgID, params.ScopeType, params.ScopeID, params.PeriodKind,
				params.PeriodStartMs, params.PeriodEndMs, params.HardLimitMicros,
				settledExact, settledEst, carryover, threshJSON, now, now,
			); iErr != nil {
				return iErr
			}
		default:
			return qErr
		}

		loaded, lErr := scanLedgerByIDTx(ctx, tx, params.OrgID, existingID)
		if lErr != nil {
			return lErr
		}
		out = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetLedger returns a tenant-isolated budget ledger row.
func (s *Service) GetLedger(ctx context.Context, orgID, ledgerID string) (*BudgetLedger, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(ledgerID) == "" {
		return nil, ErrInvalidInput
	}

	var out *BudgetLedger
	err := s.withOrgTx(ctx, orgID, func(tx *dialectTx) error {
		l, lErr := scanLedgerByIDTx(ctx, tx, orgID, ledgerID)
		if lErr != nil {
			return lErr
		}
		out = l
		return nil
	})
	return out, err
}

// GetReservation returns a tenant-isolated reservation record.
func (s *Service) GetReservation(ctx context.Context, orgID, reservationID string) (*Reservation, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(reservationID) == "" {
		return nil, ErrInvalidInput
	}

	var out *Reservation
	err := s.withOrgTx(ctx, orgID, func(tx *dialectTx) error {
		r, rErr := scanReservationByIDTx(ctx, tx, orgID, reservationID)
		if rErr != nil {
			return rErr
		}
		out = r
		return nil
	})
	return out, err
}

// Reserve atomically locks hold_micros across all active Org, Project, Env, and VirtualKey
// budget ledgers simultaneously (sorted deterministically by ledger_id ASC), rejecting with
// ErrBudgetExceeded (429 project_budget_exceeded) if any scope lacks headroom.
func (s *Service) Reserve(ctx context.Context, req ReserveRequest) (*Reservation, error) {
	if strings.TrimSpace(req.OrgID) == "" ||
		strings.TrimSpace(req.ProjectID) == "" ||
		strings.TrimSpace(req.EnvID) == "" ||
		strings.TrimSpace(req.VirtualKeyID) == "" ||
		strings.TrimSpace(req.RequestID) == "" ||
		req.HoldMicros < 0 {
		return nil, ErrInvalidInput
	}
	ttl := req.LeaseTTLMs
	if ttl <= 0 {
		ttl = s.leaseTTLMs
	}

	now := s.nowMs()
	expiresAt := addInt64Saturating(now, ttl)
	var out *Reservation

	err := s.withOrgTx(ctx, req.OrgID, func(tx *dialectTx) error {
		// Check for idempotent duplicate Reserve on same (org_id, request_id).
		var existingID, existingState, existingProj, existingEnv, existingKey string
		var existingHold int64
		dupErr := tx.QueryRowContext(ctx, `
			SELECT id, state, hold_micros, project_id, env_id, virtual_key_id
			FROM budget_reservations
			WHERE org_id = ? AND request_id = ?`+tx.forUpdateClause(),
			req.OrgID, req.RequestID,
		).Scan(&existingID, &existingState, &existingHold, &existingProj, &existingEnv, &existingKey)
		if dupErr == nil {
			if existingState == StateReservedHold &&
				existingHold == req.HoldMicros &&
				existingProj == req.ProjectID &&
				existingEnv == req.EnvID &&
				existingKey == req.VirtualKeyID {
				loaded, lErr := scanReservationByIDTx(ctx, tx, req.OrgID, existingID)
				if lErr != nil {
					return lErr
				}
				out = loaded
				return nil
			}
			return ErrInvalidStateTransition
		} else if !errors.Is(dupErr, sql.ErrNoRows) {
			return dupErr
		}

		ledgers, err := findActiveLedgersTx(ctx, tx, req.OrgID, req.ProjectID, req.EnvID, req.VirtualKeyID, now)
		if err != nil {
			return err
		}

		resID := idgen.MustNew(idgen.PrefixReservation)
		for _, l := range ledgers {
			if _, ok := addInt64Checked(l.ReservedHoldMicros, req.HoldMicros); !ok {
				return ErrOverflow
			}
			res, uErr := tx.ExecContext(ctx, `
				UPDATE budget_ledgers
				SET reserved_hold_micros = reserved_hold_micros + ?,
				    version = version + 1,
				    updated_at_ms = ?
				WHERE org_id = ? AND id = ?
				  AND settled_exact_micros <= hard_limit_micros
				  AND settled_estimated_micros <= hard_limit_micros - settled_exact_micros
				  AND reserved_hold_micros <= hard_limit_micros - settled_exact_micros - settled_estimated_micros
				  AND ? <= hard_limit_micros - settled_exact_micros - settled_estimated_micros - reserved_hold_micros`,
				req.HoldMicros, now, req.OrgID, l.ID, req.HoldMicros,
			)
			if uErr != nil {
				return uErr
			}
			rowsAffected, _ := res.RowsAffected()
			if rowsAffected == 0 {
				return &BudgetExceededError{
					LedgerID:        l.ID,
					ScopeType:       l.ScopeType,
					ScopeID:         l.ScopeID,
					AvailableMicros: safeAvailableBalance(l),
					RequestedMicros: req.HoldMicros,
				}
			}
		}

		if _, iErr := tx.ExecContext(ctx, `
			INSERT INTO budget_reservations (
				id, request_id, org_id, project_id, env_id, virtual_key_id,
				state, hold_micros, lease_expires_at_ms, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			resID, req.RequestID, req.OrgID, req.ProjectID, req.EnvID, req.VirtualKeyID,
			StateReservedHold, req.HoldMicros, expiresAt, now, now,
		); iErr != nil {
			return iErr
		}

		var ledgerIDs []string
		if len(ledgers) > 0 {
			ledgerIDs = make([]string, len(ledgers))
			for i, l := range ledgers {
				if _, jErr := tx.ExecContext(ctx, `
					INSERT INTO budget_reservation_ledgers (org_id, reservation_id, ledger_id, held_micros)
					VALUES (?, ?, ?, ?)`,
					req.OrgID, resID, l.ID, req.HoldMicros,
				); jErr != nil {
					return jErr
				}
				ledgerIDs[i] = l.ID
			}
			if len(ledgerIDs) > 1 {
				sort.Strings(ledgerIDs)
			}
		}

		out = &Reservation{
			ID:               resID,
			RequestID:        req.RequestID,
			OrgID:            req.OrgID,
			ProjectID:        req.ProjectID,
			EnvID:            req.EnvID,
			VirtualKeyID:     req.VirtualKeyID,
			State:            StateReservedHold,
			HoldMicros:       req.HoldMicros,
			LeaseExpiresAtMs: expiresAt,
			LedgerIDs:        ledgerIDs,
			CreatedAtMs:      now,
			UpdatedAtMs:      now,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeltaReserve increases the reserved hold on an active pre-stream RESERVED_HOLD
// reservation when pre-stream failover advances to a higher-cost target (TP-IMP-0021 §4).
func (s *Service) DeltaReserve(ctx context.Context, req DeltaReserveRequest) (*Reservation, error) {
	if strings.TrimSpace(req.OrgID) == "" ||
		strings.TrimSpace(req.ReservationID) == "" ||
		req.NewHoldMicros < 0 {
		return nil, ErrInvalidInput
	}
	ttl := req.LeaseTTLMs
	if ttl <= 0 {
		ttl = s.leaseTTLMs
	}

	now := s.nowMs()
	expiresAt := addInt64Saturating(now, ttl)
	var out *Reservation

	err := s.withOrgTx(ctx, req.OrgID, func(tx *dialectTx) error {
		cur, err := scanReservationByIDTx(ctx, tx, req.OrgID, req.ReservationID)
		if err != nil {
			return err
		}
		if cur.State != StateReservedHold {
			return ErrInvalidStateTransition
		}
		if req.NewHoldMicros <= cur.HoldMicros {
			if _, uErr := tx.ExecContext(ctx, `
				UPDATE budget_reservations
				SET lease_expires_at_ms = ?, updated_at_ms = ?
				WHERE org_id = ? AND id = ? AND state = ?`,
				expiresAt, now, req.OrgID, req.ReservationID, StateReservedHold,
			); uErr != nil {
				return uErr
			}
			cur.LeaseExpiresAtMs = expiresAt
			cur.UpdatedAtMs = now
			out = cur
			return nil
		}

		delta := req.NewHoldMicros - cur.HoldMicros
		for _, ledgerID := range cur.LedgerIDs {
			lCur, lCurErr := scanLedgerByIDTx(ctx, tx, req.OrgID, ledgerID)
			if lCurErr != nil {
				return lCurErr
			}
			if _, ok := addInt64Checked(lCur.ReservedHoldMicros, delta); !ok {
				return ErrOverflow
			}
			res, uErr := tx.ExecContext(ctx, `
				UPDATE budget_ledgers
				SET reserved_hold_micros = reserved_hold_micros + ?,
				    version = version + 1,
				    updated_at_ms = ?
				WHERE org_id = ? AND id = ?
				  AND settled_exact_micros <= hard_limit_micros
				  AND settled_estimated_micros <= hard_limit_micros - settled_exact_micros
				  AND reserved_hold_micros <= hard_limit_micros - settled_exact_micros - settled_estimated_micros
				  AND ? <= hard_limit_micros - settled_exact_micros - settled_estimated_micros - reserved_hold_micros`,
				delta, now, req.OrgID, ledgerID, delta,
			)
			if uErr != nil {
				return uErr
			}
			rowsAffected, _ := res.RowsAffected()
			if rowsAffected == 0 {
				return &BudgetExceededError{
					LedgerID:        lCur.ID,
					ScopeType:       lCur.ScopeType,
					ScopeID:         lCur.ScopeID,
					AvailableMicros: safeAvailableBalance(*lCur),
					RequestedMicros: delta,
				}
			}
		}

		if _, jErr := tx.ExecContext(ctx, `
			UPDATE budget_reservation_ledgers
			SET held_micros = held_micros + ?
			WHERE org_id = ? AND reservation_id = ?`,
			delta, req.OrgID, req.ReservationID,
		); jErr != nil {
			return jErr
		}

		if _, rErr := tx.ExecContext(ctx, `
			UPDATE budget_reservations
			SET hold_micros = ?, lease_expires_at_ms = ?, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND state = ?`,
			req.NewHoldMicros, expiresAt, now, req.OrgID, req.ReservationID, StateReservedHold,
		); rErr != nil {
			return rErr
		}

		loaded, lErr := scanReservationByIDTx(ctx, tx, req.OrgID, req.ReservationID)
		if lErr != nil {
			return lErr
		}
		out = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MarkStreaming transitions RESERVED_HOLD -> STREAMING_ACCRUING and extends the lease.
func (s *Service) MarkStreaming(ctx context.Context, orgID, reservationID string) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(reservationID) == "" {
		return ErrInvalidInput
	}

	now := s.nowMs()
	expiresAt := addInt64Saturating(now, s.leaseTTLMs)
	return s.withOrgTx(ctx, orgID, func(tx *dialectTx) error {
		var curState string
		err := tx.QueryRowContext(ctx, `
			SELECT state FROM budget_reservations WHERE org_id = ? AND id = ?`+tx.forUpdateClause(),
			orgID, reservationID,
		).Scan(&curState)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if curState == StateStreamingAccruing {
			_, uErr := tx.ExecContext(ctx, `
				UPDATE budget_reservations SET lease_expires_at_ms = ?, updated_at_ms = ?
				WHERE org_id = ? AND id = ?`, expiresAt, now, orgID, reservationID)
			return uErr
		}
		if curState != StateReservedHold {
			return ErrInvalidStateTransition
		}
		_, uErr := tx.ExecContext(ctx, `
			UPDATE budget_reservations
			SET state = ?, lease_expires_at_ms = ?, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND state = ?`,
			StateStreamingAccruing, expiresAt, now, orgID, reservationID, StateReservedHold,
		)
		return uErr
	})
}

// RenewLease extends lease_expires_at_ms for an in-flight RESERVED_HOLD or STREAMING_ACCRUING reservation.
func (s *Service) RenewLease(ctx context.Context, orgID, reservationID string) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(reservationID) == "" {
		return ErrInvalidInput
	}

	now := s.nowMs()
	expiresAt := addInt64Saturating(now, s.leaseTTLMs)
	return s.withOrgTx(ctx, orgID, func(tx *dialectTx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE budget_reservations
			SET lease_expires_at_ms = ?, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND state IN (?, ?)`,
			expiresAt, now, orgID, reservationID, StateReservedHold, StateStreamingAccruing,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			var state string
			qErr := tx.QueryRowContext(ctx, `SELECT state FROM budget_reservations WHERE org_id = ? AND id = ?`, orgID, reservationID).Scan(&state)
			if errors.Is(qErr, sql.ErrNoRows) {
				return ErrNotFound
			}
			if qErr != nil {
				return qErr
			}
			return ErrInvalidStateTransition
		}
		return nil
	})
}

// Release releases an unused RESERVED_HOLD or STREAMING_ACCRUING reservation back to ledger headroom.
// Double Release is an idempotent no-op (returns alreadyReleased=true, nil).
func (s *Service) Release(ctx context.Context, orgID, reservationID string) (bool, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(reservationID) == "" {
		return false, ErrInvalidInput
	}

	now := s.nowMs()
	alreadyReleased := false
	err := s.withOrgTx(ctx, orgID, func(tx *dialectTx) error {
		var curState string
		err := tx.QueryRowContext(ctx, `
			SELECT state FROM budget_reservations WHERE org_id = ? AND id = ?`+tx.forUpdateClause(),
			orgID, reservationID,
		).Scan(&curState)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if curState == StateReleasedUnused {
			alreadyReleased = true
			return nil
		}
		if curState != StateReservedHold && curState != StateStreamingAccruing {
			return ErrInvalidStateTransition
		}
		if err := releaseLinkedHoldsTx(ctx, tx, orgID, reservationID, now); err != nil {
			return err
		}
		_, uErr := tx.ExecContext(ctx, `
			UPDATE budget_reservations
			SET state = ?, updated_at_ms = ?
			WHERE org_id = ? AND id = ?`,
			StateReleasedUnused, now, orgID, reservationID,
		)
		return uErr
	})
	return alreadyReleased, err
}

// Settle commits Phase 2 billing into immutable usage_settlements, refunds unused hold or records
// Negative Balance Carryover (AC-3), emits ALT-BUDGET-* / ALT-UNMETERED-SPEND alerts, and is strictly
// idempotent on duplicate Settle(reservationID) calls (NEG-2).
func (s *Service) Settle(ctx context.Context, req SettleRequest) (*SettlementResult, error) {
	if strings.TrimSpace(req.OrgID) == "" || strings.TrimSpace(req.RequestID) == "" {
		return nil, ErrInvalidInput
	}
	plane := strings.ToUpper(strings.TrimSpace(req.Plane))
	if plane == "" {
		plane = PlaneProvider
	}
	if plane != PlaneProvider && plane != PlaneMCP && plane != PlaneA2A {
		return nil, ErrInvalidInput
	}
	billingMode := strings.ToUpper(strings.TrimSpace(req.Rate.BillingMode))
	if billingMode == "" {
		billingMode = pricing.BillingModeCommercialAPI
	}
	if billingMode != pricing.BillingModeCommercialAPI && billingMode != pricing.BillingModeSelfHostedInfra {
		return nil, ErrInvalidInput
	}
	req.Rate.BillingMode = billingMode

	breakdown, calcErr := pricing.Calculate(req.Rate, req.Usage)
	if calcErr != nil {
		return nil, calcErr
	}
	targetResState, err := reservationStateForAccuracy(breakdown.Accuracy)
	if err != nil {
		return nil, err
	}

	var (
		now                     = s.nowMs()
		out                     *SettlementResult
		alertsToEmit            []AlertEvent
		lateSettledAfterReclaim bool
	)

	err = s.withOrgTx(ctx, req.OrgID, func(tx *dialectTx) error {
		projectID := strings.TrimSpace(req.ProjectID)
		envID := strings.TrimSpace(req.EnvID)
		vkeyID := strings.TrimSpace(req.VirtualKeyID)

		if strings.TrimSpace(req.ReservationID) != "" {
			var curState string
			var rProj, rEnv, rKey, rReqID string
			qErr := tx.QueryRowContext(ctx, `
				SELECT state, project_id, env_id, virtual_key_id, request_id
				FROM budget_reservations
				WHERE org_id = ? AND id = ?`+tx.forUpdateClause(),
				req.OrgID, req.ReservationID,
			).Scan(&curState, &rProj, &rEnv, &rKey, &rReqID)
				if errors.Is(qErr, sql.ErrNoRows) {
					return ErrNotFound
				}
				if qErr != nil {
					return qErr
				}
				if rReqID != req.RequestID {
					return ErrInvalidInput
				}
				if (projectID != "" && projectID != rProj) ||
					(envID != "" && envID != rEnv) ||
					(vkeyID != "" && vkeyID != rKey) {
					return ErrInvalidInput
				}
				projectID = rProj
				envID = rEnv
				vkeyID = rKey

				if isSettledState(curState) {
					existing, eErr := findSettlementByReservationTx(ctx, tx, req.OrgID, req.ReservationID)
					if eErr != nil {
						return eErr
					}
					existing.AlreadySettled = true
					out = existing
					return nil
				}
				if existingByReq, reqErr := findSettlementByRequestIDTx(ctx, tx, req.OrgID, req.RequestID); reqErr == nil {
					existingByReq.AlreadySettled = true
					out = existingByReq
					return nil
				} else if !errors.Is(reqErr, ErrNotFound) {
					return reqErr
				}
				if curState == StateExpiredReclaimed {
					existing, eErr := findSettlementByReservationTx(ctx, tx, req.OrgID, req.ReservationID)
					if eErr == nil {
						existing.AlreadySettled = true
						out = existing
						return nil
					} else if !errors.Is(eErr, ErrNotFound) {
						return eErr
					}
					// Reservation lease expired and the reaper already released its held balance.
					// Prefer the reservation's originally linked ledgers (so period rollovers bill the
					// originating period ledgers accurately) with releaseHeld=0, falling back to currently
					// active ledgers if no linked holds existed. Keep EXPIRED_RECLAIMED terminal state.
					origLinks, lErr := loadLinkedHoldsTx(ctx, tx, req.OrgID, req.ReservationID)
					if lErr != nil {
						return lErr
					}
					if len(origLinks) > 0 {
						for _, lk := range origLinks {
							al, aErr := applyLedgerSettlementTx(ctx, tx, req.OrgID, lk.LedgerID, 0, breakdown, now, req.RequestID)
							if aErr != nil {
								return aErr
							}
							alertsToEmit = append(alertsToEmit, al...)
						}
					} else {
						ledgers, aErr := findActiveLedgersTx(ctx, tx, req.OrgID, projectID, envID, vkeyID, now)
						if aErr != nil {
							return aErr
						}
						for _, l := range ledgers {
							al, sErr := applyLedgerSettlementTx(ctx, tx, req.OrgID, l.ID, 0, breakdown, now, req.RequestID)
							if sErr != nil {
								return sErr
							}
							alertsToEmit = append(alertsToEmit, al...)
						}
					}
					lateSettledAfterReclaim = true
				} else if curState != StateReservedHold && curState != StateStreamingAccruing {
					return ErrInvalidStateTransition
				} else {
					// Release reserved hold from each linked ledger and apply settled spend + carryover.
					alerts, sErr := settleLinkedLedgersTx(ctx, tx, req.OrgID, req.ReservationID, breakdown, now, req.RequestID)
					if sErr != nil {
						return sErr
					}
					alertsToEmit = append(alertsToEmit, alerts...)

					updRes, uErr := tx.ExecContext(ctx, `
					UPDATE budget_reservations
					SET state = ?, updated_at_ms = ?
					WHERE org_id = ? AND id = ? AND state IN (?, ?)`,
						targetResState, now, req.OrgID, req.ReservationID, StateReservedHold, StateStreamingAccruing,
					)
					if uErr != nil {
						return uErr
					}
					if n, _ := updRes.RowsAffected(); n != 1 {
						return ErrInvalidStateTransition
					}
				}
			} else {
				if projectID == "" || envID == "" || vkeyID == "" {
					return ErrInvalidInput
				}
				if tx.dialect == storageport.DialectPostgres {
					if _, lErr := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('ledger:settle:' || ? || ':' || ?, 0))`, req.OrgID, req.RequestID); lErr != nil {
						return lErr
					}
				}
				// Idempotency guard when Settle is invoked directly without reservation_id.
				existing, eErr := findSettlementByRequestIDTx(ctx, tx, req.OrgID, req.RequestID)
				if eErr == nil {
					existing.AlreadySettled = true
					out = existing
					return nil
				} else if !errors.Is(eErr, ErrNotFound) {
					return eErr
				}

				ledgers, lErr := findActiveLedgersTx(ctx, tx, req.OrgID, projectID, envID, vkeyID, now)
				if lErr != nil {
					return lErr
				}
				for _, l := range ledgers {
					al, aErr := applyLedgerSettlementTx(ctx, tx, req.OrgID, l.ID, 0, breakdown, now, req.RequestID)
					if aErr != nil {
						return aErr
					}
					alertsToEmit = append(alertsToEmit, al...)
				}
			}

			setID := idgen.MustNew(idgen.PrefixSettlement)
			var resIDPtr *string
			if strings.TrimSpace(req.ReservationID) != "" {
				v := req.ReservationID
				resIDPtr = &v
			}
			var deplPtr *string
			if strings.TrimSpace(req.DeploymentID) != "" {
				v := strings.TrimSpace(req.DeploymentID)
				deplPtr = &v
			}
			var modelPtr *string
			if strings.TrimSpace(req.LogicalModelSlug) != "" {
				v := strings.TrimSpace(req.LogicalModelSlug)
				modelPtr = &v
			}
			eff := breakdown.EffectiveUsage
			if _, iErr := tx.ExecContext(ctx, `
			INSERT INTO usage_settlements (
				id, settled_at_ms, request_id, reservation_id, org_id, project_id, env_id, virtual_key_id,
				plane, deployment_id, logical_model_slug, billing_mode, usage_accuracy,
				input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
				media_usage_json, applied_pricing_snapshot_json, total_cost_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				setID, now, req.RequestID, resIDPtr, req.OrgID, projectID, envID, vkeyID,
				plane, deplPtr, modelPtr, billingMode, breakdown.Accuracy,
				eff.InputTokens, eff.OutputTokens, eff.CacheReadTokens, eff.CacheWriteTokens, eff.ReasoningTokens,
				eff.Media.ToJSON(), req.Rate.SnapshotJSON(), breakdown.TotalMicroUSD,
			); iErr != nil {
				return iErr
			}

			if breakdown.UnmeteredFlagged {
				alertsToEmit = append(alertsToEmit, AlertEvent{
					Code:         "ALT-UNMETERED-SPEND",
					Runbook:      "INC-01",
					OrgID:        req.OrgID,
					RequestID:    req.RequestID,
					SettlementID: setID,
					DeploymentID: req.DeploymentID,
					LogicalModel: req.LogicalModelSlug,
					OccurredAtMs: now,
				})
			}

			for i := range alertsToEmit {
				if alertsToEmit[i].SettlementID == "" {
					alertsToEmit[i].SettlementID = setID
				}
				if s.alertOutbox != nil && strings.HasPrefix(alertsToEmit[i].Code, "ALT-BUDGET-") {
					if err := s.alertOutbox(ctx, tx.tx, alertsToEmit[i]); err != nil {
						return fmt.Errorf("ledger: persist budget alert outbox: %w", err)
					}
				}
			}

			out = &SettlementResult{
				SettlementID:     setID,
				ReservationID:    req.ReservationID,
				RequestID:        req.RequestID,
				SettledAtMs:      now,
				Accuracy:         breakdown.Accuracy,
				TotalCostMicros:  breakdown.TotalMicroUSD,
				InputTokens:      eff.InputTokens,
				OutputTokens:     eff.OutputTokens,
				CacheReadTokens:  eff.CacheReadTokens,
				CacheWriteTokens: eff.CacheWriteTokens,
				ReasoningTokens:  eff.ReasoningTokens,
				UnmeteredFlagged: breakdown.UnmeteredFlagged,
				AlreadySettled:   false,
			}
			return nil
		})
	if err != nil {
		return nil, err
	}

	if out != nil && !out.AlreadySettled {
		if lateSettledAfterReclaim && s.audit != nil {
			meta, _ := json.Marshal(map[string]any{
				"reservation_id":    req.ReservationID,
				"settlement_id":     out.SettlementID,
				"total_cost_micros": out.TotalCostMicros,
			})
			orgCopy := req.OrgID
			reqCopy := req.RequestID
			_ = s.audit.Append(ctx, storageport.AuditEvent{
				ID:           idgen.MustNew(idgen.PrefixAudit),
				CreatedAtMs:  now,
				OrgID:        &orgCopy,
				ActorKind:    "system",
				ActorID:      "budget_ledger_service",
				EntryPoint:   "data_plane_guard",
				EventType:    "BUDGET_LATE_SETTLEMENT_AFTER_RECLAIM",
				TargetType:   "budget_reservation",
				TargetID:     req.ReservationID,
				RequestID:    &reqCopy,
				MetadataJSON: string(meta),
			})
		}
		for _, ev := range alertsToEmit {
			if ev.Code == "ALT-UNMETERED-SPEND" {
				s.unmeteredCount.Add(1)
			} else if strings.HasPrefix(ev.Code, "ALT-BUDGET-") {
				s.softAlertCount.Add(1)
			}
			if s.alerts != nil {
				s.alerts.EmitBudgetAlert(ctx, ev)
			}
		}
	}
	return out, nil
}

func (s *Service) withOrgTx(ctx context.Context, orgID string, fn func(tx *dialectTx) error) error {
	if s.dialect != storageport.DialectPostgres {
		s.txMu.Lock()
		defer s.txMu.Unlock()
	}
	return s.withTx(ctx, func(rawTx *sql.Tx) error {
		if s.dialect == storageport.DialectPostgres && orgID != "" {
			if _, err := rawTx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, orgID); err != nil {
				return fmt.Errorf("ledger: set app.current_org_id: %w", err)
			}
		}
		return fn(&dialectTx{tx: rawTx, dialect: s.dialect})
	})
}

func (s *Service) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	const maxBusyRetries = 5
	for attempt := 0; attempt <= maxBusyRetries; attempt++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			if isRetryableTxErr(err) && attempt < maxBusyRetries {
				time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
				continue
			}
			return err
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			if isRetryableTxErr(err) && attempt < maxBusyRetries {
				time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
				continue
			}
			return err
		}
		if err := tx.Commit(); err != nil {
			if isRetryableTxErr(err) && attempt < maxBusyRetries {
				time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
	return nil
}

func isRetryableTxErr(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "40001" || pgErr.Code == "40P01" {
			return true
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

func isValidScopeType(s string) bool {
	return s == ScopeOrg || s == ScopeProject || s == ScopeEnv || s == ScopeVirtualKey
}

func isValidPeriodKind(p string) bool {
	return p == PeriodDaily || p == PeriodMonthly || p == PeriodTotalLifetime
}

func isSettledState(s string) bool {
	return s == StateSettledExact || s == StateSettledEstimated || s == StateSettledUnmeteredFlagged
}

func reservationStateForAccuracy(accuracy string) (string, error) {
	switch accuracy {
	case pricing.AccuracyExact:
		return StateSettledExact, nil
	case pricing.AccuracyEstimated:
		return StateSettledEstimated, nil
	case pricing.AccuracyUnmeteredFlagged:
		return StateSettledUnmeteredFlagged, nil
	default:
		return "", ErrInvalidInput
	}
}

func computeCarryover(totalSettled, hardLimit int64) int64 {
	if totalSettled > hardLimit {
		return totalSettled - hardLimit
	}
	return 0
}

func computeUtilizationPct(totalSettled, hardLimit int64) int {
	if totalSettled <= 0 {
		return 0
	}
	if hardLimit <= 0 {
		return 100
	}
	if totalSettled >= hardLimit {
		return 100
	}
	// Avoid overflowing totalSettled * 100 when totalSettled is huge.
	pct := (totalSettled / hardLimit) * 100
	rem := totalSettled % hardLimit
	if rem > 0 {
		if rem <= 9_000_000_000_000_000 { // <= 9e15 avoids overflow in rem * 100
			pct += (rem * 100) / hardLimit
		} else {
			pct += (rem / (hardLimit / 100))
		}
	}
	if pct > 100 {
		pct = 100
	}
	return int(pct)
}

func normalizeThresholds(raw []int) []int {
	if len(raw) == 0 {
		return []int{50, 80, 100}
	}
	seen := make(map[int]struct{}, len(raw))
	out := make([]int, 0, len(raw))
	for _, v := range raw {
		if v <= 0 || v > 100 {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return []int{50, 80, 100}
	}
	sort.Ints(out)
	return out
}

func findActiveLedgersTx(ctx context.Context, tx *dialectTx, orgID, projectID, envID, vkeyID string, nowMs int64) ([]BudgetLedger, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, org_id, scope_type, scope_id, period_kind, period_start_ms, period_end_ms,
		       hard_limit_micros, reserved_hold_micros, settled_exact_micros, settled_estimated_micros,
		       negative_carryover_micros, soft_alert_thresholds_pct_json, last_alerted_threshold_pct,
		       version, created_at_ms, updated_at_ms
		FROM budget_ledgers
		WHERE org_id = ?
		  AND period_start_ms <= ?
		  AND period_end_ms > ?
		  AND (
		      (scope_type = 'ORG' AND scope_id = ?) OR
		      (scope_type = 'PROJECT' AND scope_id = ?) OR
		      (scope_type = 'ENV' AND scope_id = ?) OR
		      (scope_type = 'VIRTUAL_KEY' AND scope_id = ?)
		  )
		ORDER BY id ASC`+tx.forUpdateClause(),
		orgID, nowMs, nowMs, orgID, projectID, envID, vkeyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BudgetLedger
	for rows.Next() {
		l, sErr := scanLedgerRow(rows)
		if sErr != nil {
			return nil, sErr
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func scanLedgerByIDTx(ctx context.Context, tx *dialectTx, orgID, ledgerID string) (*BudgetLedger, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, org_id, scope_type, scope_id, period_kind, period_start_ms, period_end_ms,
		       hard_limit_micros, reserved_hold_micros, settled_exact_micros, settled_estimated_micros,
		       negative_carryover_micros, soft_alert_thresholds_pct_json, last_alerted_threshold_pct,
		       version, created_at_ms, updated_at_ms
		FROM budget_ledgers
		WHERE org_id = ? AND id = ?`+tx.forUpdateClause(),
		orgID, ledgerID,
	)
	l, err := scanLedgerRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLedgerRow(row rowScanner) (BudgetLedger, error) {
	var (
		l          BudgetLedger
		threshJSON string
	)
	if err := row.Scan(
		&l.ID, &l.OrgID, &l.ScopeType, &l.ScopeID, &l.PeriodKind, &l.PeriodStartMs, &l.PeriodEndMs,
		&l.HardLimitMicros, &l.ReservedHoldMicros, &l.SettledExactMicros, &l.SettledEstimatedMicros,
		&l.NegativeCarryoverMicros, &threshJSON, &l.LastAlertedThresholdPct,
		&l.Version, &l.CreatedAtMs, &l.UpdatedAtMs,
	); err != nil {
		return BudgetLedger{}, err
	}
	var parsed []int
	_ = json.Unmarshal([]byte(threshJSON), &parsed)
	l.SoftAlertThresholdsPct = normalizeThresholds(parsed)
	return l, nil
}

func scanReservationByIDTx(ctx context.Context, tx *dialectTx, orgID, reservationID string) (*Reservation, error) {
	var r Reservation
	err := tx.QueryRowContext(ctx, `
		SELECT id, request_id, org_id, project_id, env_id, virtual_key_id,
		       state, hold_micros, lease_expires_at_ms, created_at_ms, updated_at_ms
		FROM budget_reservations
		WHERE org_id = ? AND id = ?`+tx.forUpdateClause(),
		orgID, reservationID,
	).Scan(
		&r.ID, &r.RequestID, &r.OrgID, &r.ProjectID, &r.EnvID, &r.VirtualKeyID,
		&r.State, &r.HoldMicros, &r.LeaseExpiresAtMs, &r.CreatedAtMs, &r.UpdatedAtMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, lErr := tx.QueryContext(ctx, `
		SELECT ledger_id FROM budget_reservation_ledgers
		WHERE org_id = ? AND reservation_id = ?
		ORDER BY ledger_id ASC`,
		orgID, reservationID,
	)
	if lErr != nil {
		return nil, lErr
	}
	defer rows.Close()
	for rows.Next() {
		var lid string
		if sErr := rows.Scan(&lid); sErr != nil {
			return nil, sErr
		}
		r.LedgerIDs = append(r.LedgerIDs, lid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &r, nil
}

type linkedHold struct {
	LedgerID   string
	HeldMicros int64
}

func loadLinkedHoldsTx(ctx context.Context, tx *dialectTx, orgID, reservationID string) ([]linkedHold, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT ledger_id, held_micros
		FROM budget_reservation_ledgers
		WHERE org_id = ? AND reservation_id = ?
		ORDER BY ledger_id ASC`,
		orgID, reservationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []linkedHold
	for rows.Next() {
		var h linkedHold
		if sErr := rows.Scan(&h.LedgerID, &h.HeldMicros); sErr != nil {
			return nil, sErr
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func releaseLinkedHoldsTx(ctx context.Context, tx *dialectTx, orgID, reservationID string, nowMs int64) error {
	links, err := loadLinkedHoldsTx(ctx, tx, orgID, reservationID)
	if err != nil {
		return err
	}
	for _, lk := range links {
		if lk.HeldMicros == 0 {
			continue
		}
		if _, uErr := tx.ExecContext(ctx, `
			UPDATE budget_ledgers
			SET reserved_hold_micros = CASE WHEN reserved_hold_micros >= ? THEN reserved_hold_micros - ? ELSE 0 END,
			    version = version + 1,
			    updated_at_ms = ?
			WHERE org_id = ? AND id = ?`,
			lk.HeldMicros, lk.HeldMicros, nowMs, orgID, lk.LedgerID,
		); uErr != nil {
			return uErr
		}
	}
	return nil
}

func settleLinkedLedgersTx(
	ctx context.Context,
	tx *dialectTx,
	orgID, reservationID string,
	breakdown pricing.CostBreakdown,
	nowMs int64,
	requestID string,
) ([]AlertEvent, error) {
	links, err := loadLinkedHoldsTx(ctx, tx, orgID, reservationID)
	if err != nil {
		return nil, err
	}
	var allAlerts []AlertEvent
	for _, lk := range links {
		al, aErr := applyLedgerSettlementTx(ctx, tx, orgID, lk.LedgerID, lk.HeldMicros, breakdown, nowMs, requestID)
		if aErr != nil {
			return nil, aErr
		}
		allAlerts = append(allAlerts, al...)
	}
	return allAlerts, nil
}

func applyLedgerSettlementTx(
	ctx context.Context,
	tx *dialectTx,
	orgID, ledgerID string,
	releaseHeld int64,
	breakdown pricing.CostBreakdown,
	nowMs int64,
	requestID string,
) ([]AlertEvent, error) {
	l, err := scanLedgerByIDTx(ctx, tx, orgID, ledgerID)
	if err != nil {
		return nil, err
	}

	newReserved := l.ReservedHoldMicros - releaseHeld
	if newReserved < 0 {
		newReserved = 0
	}
	newExact := l.SettledExactMicros
	newEst := l.SettledEstimatedMicros
	var ok bool
	if breakdown.Accuracy == pricing.AccuracyExact {
		newExact, ok = addInt64Checked(newExact, breakdown.TotalMicroUSD)
	} else {
		newEst, ok = addInt64Checked(newEst, breakdown.TotalMicroUSD)
	}
	if !ok {
		return nil, ErrOverflow
	}
	totalSettled, ok := addInt64Checked(newExact, newEst)
	if !ok {
		return nil, ErrOverflow
	}
	carryover := computeCarryover(totalSettled, l.HardLimitMicros)

	utilPct := computeUtilizationPct(totalSettled, l.HardLimitMicros)
	lastAlerted := l.LastAlertedThresholdPct
	var alerts []AlertEvent
	for _, thresh := range l.SoftAlertThresholdsPct {
		if utilPct >= thresh && thresh > lastAlerted {
			lastAlerted = thresh
			alerts = append(alerts, AlertEvent{
				Code:           fmt.Sprintf("ALT-BUDGET-%d", thresh),
				Runbook:        "INC-01",
				OrgID:          orgID,
				LedgerID:       l.ID,
				ScopeType:      l.ScopeType,
				ScopeID:        l.ScopeID,
				ThresholdPct:   thresh,
				UtilizationPct: utilPct,
				RequestID:      requestID,
				OccurredAtMs:   nowMs,
			})
		}
	}

	if _, uErr := tx.ExecContext(ctx, `
		UPDATE budget_ledgers
		SET reserved_hold_micros = ?,
		    settled_exact_micros = ?,
		    settled_estimated_micros = ?,
		    negative_carryover_micros = ?,
		    last_alerted_threshold_pct = ?,
		    version = version + 1,
		    updated_at_ms = ?
		WHERE org_id = ? AND id = ?`,
		newReserved, newExact, newEst, carryover, lastAlerted, nowMs, orgID, ledgerID,
	); uErr != nil {
		return nil, uErr
	}
	return alerts, nil
}

func findSettlementByReservationTx(ctx context.Context, tx *dialectTx, orgID, reservationID string) (*SettlementResult, error) {
	var res SettlementResult
	var resID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT id, reservation_id, request_id, settled_at_ms, usage_accuracy, total_cost_micros,
		       input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens
		FROM usage_settlements
		WHERE org_id = ? AND reservation_id = ?
		ORDER BY settled_at_ms DESC
		LIMIT 1`,
		orgID, reservationID,
	).Scan(
		&res.SettlementID, &resID, &res.RequestID, &res.SettledAtMs, &res.Accuracy, &res.TotalCostMicros,
		&res.InputTokens, &res.OutputTokens, &res.CacheReadTokens, &res.CacheWriteTokens, &res.ReasoningTokens,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	res.ReservationID = resID.String
	res.UnmeteredFlagged = res.Accuracy == pricing.AccuracyUnmeteredFlagged
	return &res, nil
}

func findSettlementByRequestIDTx(ctx context.Context, tx *dialectTx, orgID, requestID string) (*SettlementResult, error) {
	var res SettlementResult
	var resID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT id, reservation_id, request_id, settled_at_ms, usage_accuracy, total_cost_micros,
		       input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens
		FROM usage_settlements
		WHERE org_id = ? AND request_id = ?
		ORDER BY settled_at_ms DESC
		LIMIT 1`,
		orgID, requestID,
	).Scan(
		&res.SettlementID, &resID, &res.RequestID, &res.SettledAtMs, &res.Accuracy, &res.TotalCostMicros,
		&res.InputTokens, &res.OutputTokens, &res.CacheReadTokens, &res.CacheWriteTokens, &res.ReasoningTokens,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	res.ReservationID = resID.String
	res.UnmeteredFlagged = res.Accuracy == pricing.AccuracyUnmeteredFlagged
	return &res, nil
}

func sumSettledForScopePeriodTx(
	ctx context.Context,
	tx *dialectTx,
	orgID, scopeType, scopeID string,
	periodStartMs, periodEndMs int64,
) (exactMicros, estMicros int64, err error) {
	var scopeCol string
	switch scopeType {
	case ScopeOrg:
		scopeCol = "org_id"
	case ScopeProject:
		scopeCol = "project_id"
	case ScopeEnv:
		scopeCol = "env_id"
	case ScopeVirtualKey:
		scopeCol = "virtual_key_id"
	default:
		return 0, 0, ErrInvalidInput
	}
	query := fmt.Sprintf(`
		SELECT COALESCE(SUM(CASE WHEN usage_accuracy = 'EXACT' THEN total_cost_micros ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN usage_accuracy = 'ESTIMATED' THEN total_cost_micros ELSE 0 END), 0)
		FROM usage_settlements
		WHERE org_id = ?
		  AND %s = ?
		  AND settled_at_ms >= ?
		  AND settled_at_ms < ?`, scopeCol)
	if qErr := tx.QueryRowContext(ctx, query, orgID, scopeID, periodStartMs, periodEndMs).Scan(&exactMicros, &estMicros); qErr != nil {
		return 0, 0, qErr
	}
	return exactMicros, estMicros, nil
}

func addInt64Checked(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, false
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, false
	}
	return a + b, true
}

func addInt64Saturating(a, b int64) int64 {
	if v, ok := addInt64Checked(a, b); ok {
		return v
	}
	if b > 0 {
		return math.MaxInt64
	}
	return math.MinInt64
}

func safeAvailableBalance(l BudgetLedger) int64 {
	s1, ok1 := addInt64Checked(l.SettledExactMicros, l.SettledEstimatedMicros)
	if !ok1 {
		return math.MinInt64
	}
	used, ok2 := addInt64Checked(s1, l.ReservedHoldMicros)
	if !ok2 {
		return math.MinInt64
	}
	avail, ok3 := addInt64Checked(l.HardLimitMicros, -used)
	if !ok3 {
		return math.MinInt64
	}
	return avail
}
