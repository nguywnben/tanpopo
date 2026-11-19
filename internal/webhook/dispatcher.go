// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package webhook persists privacy-redacted event deliveries and dispatches them
// through Tanpopo's connect-time SSRF guarded HTTP client.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/telemetry/privacy"
)

const (
	workerDatabaseRole   = "tanpopo_webhook_worker"
	statusPending        = "PENDING"
	statusRetry          = "RETRY_SCHEDULED"
	statusDelivered      = "DELIVERED"
	statusDead           = "DEAD_LETTER"
	maxWorkers           = 64
	maxPayloadBytes      = 4 * 1024 * 1024
	maxOutstandingPerOrg = 5000
	maxRequestTime       = 2 * time.Minute
	maxBackoffTime       = 24 * time.Hour
)

// SecretUnsealer returns credential plaintext and the function that must erase it.
// Implementations must bind unsealing to both orgID and credentialID.
type SecretUnsealer func(ctx context.Context, orgID, credentialID string) (plaintext []byte, zeroizer func(), err error)

// Limits bounds memory, network time, concurrency, poll frequency, attempts, and retry delay.
type Limits struct {
	Workers           int
	MaxAttempts       int
	MaxBodyBytes      int
	RequestTimeout    time.Duration
	PollInterval      time.Duration
	BaseBackoff       time.Duration
	MaxBackoff        time.Duration
	LeaseSafety       time.Duration
	TerminalRetention time.Duration
}

// Config contains the trusted database, secret, egress, time, and resource dependencies.
// DB must be a dedicated internal worker handle with access to all queued tenant rows;
// never reuse a request-scoped tenant handle. In a Postgres RLS deployment, provision
// the worker role explicitly for queue-wide access. Postgres selects numbered placeholders.
type Config struct {
	DB       *sql.DB
	Postgres bool
	Unseal   SecretUnsealer
	Resolver egress.HostResolver
	Now      func() time.Time
	Limits   Limits
}

type dispatcherDB interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Event is one tenant-scoped event to fan out to enabled subscriptions.
type Event struct {
	ID        string
	OrgID     string
	ProjectID string
	Type      string
	Payload   json.RawMessage
}

// Dispatcher is a persistent bounded webhook queue and worker pool.
type Dispatcher struct {
	cfg         Config
	db          dispatcherDB
	workerConn  *sql.Conn
	pinned      bool
	redactor    *privacy.Redactor
	mu          sync.Mutex
	dbMu        sync.Mutex
	cleanupMu   sync.Mutex
	lastCleanup time.Time
	running     bool
}

var (
	ErrInvalidConfig = errors.New("webhook: invalid configuration")
	ErrInvalidEvent  = errors.New("webhook: invalid event")
)

// New constructs a dispatcher. All zero-valued limits receive conservative defaults.
func New(cfg Config) (*Dispatcher, error) {
	if cfg.DB == nil || cfg.Unseal == nil || cfg.Resolver == nil {
		return nil, ErrInvalidConfig
	}
	var database dispatcherDB = cfg.DB
	var workerConn *sql.Conn
	if cfg.Postgres {
		checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := cfg.DB.Conn(checkCtx)
		if err != nil {
			return nil, fmt.Errorf("%w: acquire dedicated postgres dispatcher connection: %v", ErrInvalidConfig, err)
		}
		if _, err := conn.ExecContext(checkCtx, `SET ROLE tanpopo_webhook_worker`); err != nil {
			discardWorkerConn(conn)
			return nil, fmt.Errorf("%w: assume postgres dispatcher role: %v", ErrInvalidConfig, err)
		}
		var currentRole string
		if err := conn.QueryRowContext(checkCtx, `SELECT current_user`).Scan(&currentRole); err != nil || currentRole != workerDatabaseRole {
			discardWorkerConn(conn)
			return nil, fmt.Errorf("%w: postgres dispatcher connection must assume %s", ErrInvalidConfig, workerDatabaseRole)
		}
		workerConn = conn
		database = conn
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Limits.Workers <= 0 {
		cfg.Limits.Workers = 4
	} else if cfg.Limits.Workers > maxWorkers {
		cfg.Limits.Workers = maxWorkers
	}
	if cfg.Limits.MaxAttempts <= 0 {
		cfg.Limits.MaxAttempts = 8
	}
	if cfg.Limits.MaxAttempts > 32767 {
		cfg.Limits.MaxAttempts = 32767
	}
	if cfg.Limits.MaxBodyBytes <= 0 {
		cfg.Limits.MaxBodyBytes = 256 * 1024
	} else if cfg.Limits.MaxBodyBytes > maxPayloadBytes {
		cfg.Limits.MaxBodyBytes = maxPayloadBytes
	}
	if cfg.Limits.RequestTimeout <= 0 {
		cfg.Limits.RequestTimeout = 10 * time.Second
	} else if cfg.Limits.RequestTimeout > maxRequestTime {
		cfg.Limits.RequestTimeout = maxRequestTime
	}
	if cfg.Limits.PollInterval <= 0 {
		cfg.Limits.PollInterval = 250 * time.Millisecond
	}
	if cfg.Limits.BaseBackoff <= 0 {
		cfg.Limits.BaseBackoff = time.Second
	}
	if cfg.Limits.MaxBackoff <= 0 {
		cfg.Limits.MaxBackoff = 5 * time.Minute
	}
	if cfg.Limits.MaxBackoff < cfg.Limits.BaseBackoff {
		cfg.Limits.MaxBackoff = cfg.Limits.BaseBackoff
	}
	if cfg.Limits.MaxBackoff > maxBackoffTime {
		cfg.Limits.MaxBackoff = maxBackoffTime
		if cfg.Limits.BaseBackoff > cfg.Limits.MaxBackoff {
			cfg.Limits.BaseBackoff = cfg.Limits.MaxBackoff
		}
	}
	if cfg.Limits.LeaseSafety <= 0 {
		cfg.Limits.LeaseSafety = 5 * time.Second
	}
	if cfg.Limits.TerminalRetention <= 0 {
		cfg.Limits.TerminalRetention = 30 * 24 * time.Hour
	}
	return &Dispatcher{cfg: cfg, db: database, workerConn: workerConn, pinned: workerConn != nil, redactor: privacy.NewDefaultRedactor()}, nil
}

func discardWorkerConn(conn *sql.Conn) {
	if conn == nil {
		return
	}
	// A connection whose role state cannot be proven must never return to the
	// general pool, where a later tenant-facing operation could inherit it.
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

// Close releases the pinned PostgreSQL worker connection, if configured.
func (d *Dispatcher) Close() error {
	if d == nil {
		return nil
	}
	d.dbMu.Lock()
	defer d.dbMu.Unlock()
	if d.workerConn == nil {
		return nil
	}
	_, resetErr := d.workerConn.ExecContext(context.Background(), `RESET ROLE`)
	if resetErr != nil {
		// Do not return a connection with an unknown role state to the shared pool.
		_ = d.workerConn.Raw(func(any) error { return driver.ErrBadConn })
	}
	err := errors.Join(resetErr, d.workerConn.Close())
	d.workerConn = nil
	return err
}

// lockDatabase serializes complete transactions when PostgreSQL mode pins the
// dispatcher to one verified connection. SQLite and ordinary pools retain their
// normal database/sql concurrency.
func (d *Dispatcher) lockDatabase() func() {
	if !d.pinned {
		return func() {}
	}
	d.dbMu.Lock()
	return d.dbMu.Unlock
}

// StableEventID derives a bounded opaque event ID for transactional sources
// that must safely retry fan-out after process recovery.
func StableEventID(key string) string {
	digest := sha256.Sum256([]byte(key))
	return "evt_" + hex.EncodeToString(digest[:16])
}

// Enqueue redacts the payload before persistence and fans it out to subscriptions
// matching the event type and tenant/project scope. It returns the inserted row count.
func (d *Dispatcher) Enqueue(ctx context.Context, event Event) (int, error) {
	unlock := d.lockDatabase()
	defer unlock()
	for attempt := 0; ; attempt++ {
		conn := d.workerConn
		ownedConn := false
		var err error
		if conn == nil {
			conn, err = d.cfg.DB.Conn(ctx)
			ownedConn = true
		}
		if err != nil {
			if retryAdmission(ctx, attempt, err) {
				continue
			}
			return 0, fmt.Errorf("webhook: acquire enqueue connection: %w", err)
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			if ownedConn {
				_ = conn.Close()
			}
			if retryAdmission(ctx, attempt, err) {
				continue
			}
			return 0, fmt.Errorf("webhook: begin enqueue: %w", err)
		}
		inserted, err := d.EnqueueTx(ctx, tx, event)
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				discardWorkerConn(conn)
			}
			if ownedConn {
				_ = conn.Close()
			}
			if retryAdmission(ctx, attempt, err) {
				continue
			}
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			// Commit errors may leave SQLite's transaction active. The transaction
			// handle is finished by database/sql, so clean the exact underlying
			// connection before it can be reused or retried.
			if _, rollbackErr := conn.ExecContext(ctx, `ROLLBACK`); rollbackErr != nil {
				discardWorkerConn(conn)
				if ownedConn {
					_ = conn.Close()
				}
				return 0, fmt.Errorf("webhook: commit enqueue: %w (rollback failed; connection discarded: %v)", err, rollbackErr)
			}
			if ownedConn {
				_ = conn.Close()
			}
			if retryAdmission(ctx, attempt, err) {
				continue
			}
			return 0, fmt.Errorf("webhook: commit enqueue: %w", err)
		}
		if ownedConn {
			_ = conn.Close()
		}
		return inserted, nil
	}
}

const maxSQLiteAdmissionRetries = 8

func retryAdmission(ctx context.Context, attempt int, err error) bool {
	if attempt >= maxSQLiteAdmissionRetries || !isSQLiteBusyError(err) || ctx.Err() != nil {
		return false
	}
	delay := time.Duration(attempt+1) * 10 * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "SQLITE_BUSY") || strings.Contains(message, "database is locked")
}

// RecordEventTx persists a tenant-scoped event in the transactional outbox.
// Call it in the same database transaction as the business state transition.
func (d *Dispatcher) RecordEventTx(ctx context.Context, tx *sql.Tx, event Event) error {
	if tx == nil || event.OrgID == "" || event.ID == "" || event.Type == "" || len(event.ID) > 40 || len(event.OrgID) > 40 || len(event.ProjectID) > 40 || len(event.Type) > 64 || len(event.Payload) == 0 || !json.Valid(event.Payload) {
		return ErrInvalidEvent
	}
	clean := d.redactor.SanitizeDebugPayload(event.Payload, d.cfg.Limits.MaxBodyBytes)
	if len(clean) == 0 || len(clean) > d.cfg.Limits.MaxBodyBytes || !json.Valid(clean) {
		return ErrInvalidEvent
	}
	if err := d.lockOrganizationAdmission(ctx, tx, event.OrgID); err != nil {
		return err
	}
	var existing int
	if err := tx.QueryRowContext(ctx, d.bind(`SELECT 1 FROM webhook_event_outbox WHERE id=?`), event.ID).Scan(&existing); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("webhook: check transactional outbox id: %w", err)
	}
	var pending int
	if err := tx.QueryRowContext(ctx, d.bind(`SELECT COUNT(*) FROM webhook_event_outbox WHERE org_id=? AND dispatched_at_ms IS NULL`), event.OrgID).Scan(&pending); err != nil {
		return fmt.Errorf("webhook: count pending outbox events: %w", err)
	}
	if pending >= maxOutstandingPerOrg {
		return fmt.Errorf("webhook: organization outbox capacity reached")
	}
	_, err := tx.ExecContext(ctx, d.bind(`INSERT INTO webhook_event_outbox(id,org_id,project_id,event_type,payload_json,created_at_ms)
		VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`), event.ID, event.OrgID, nullable(event.ProjectID), event.Type, string(clean), d.cfg.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("webhook: persist transactional outbox event: %w", err)
	}
	return nil
}

// EnqueueTx writes matching deliveries within the caller's business transaction.
func (d *Dispatcher) EnqueueTx(ctx context.Context, tx *sql.Tx, event Event) (int, error) {
	if tx == nil {
		return 0, ErrInvalidEvent
	}
	if event.OrgID == "" || event.Type == "" || len(event.Type) > 64 || len(event.Payload) == 0 || !json.Valid(event.Payload) {
		return 0, ErrInvalidEvent
	}
	if event.ID == "" {
		var err error
		event.ID, err = idgen.New(idgen.PrefixTrace)
		if err != nil {
			return 0, fmt.Errorf("webhook: generate event id: %w", err)
		}
	}
	if len(event.ID) > 40 || len(event.OrgID) > 40 || len(event.ProjectID) > 40 {
		return 0, ErrInvalidEvent
	}
	if err := d.lockOrganizationAdmission(ctx, tx, event.OrgID); err != nil {
		return 0, err
	}
	clean := d.redactor.SanitizeDebugPayload(event.Payload, d.cfg.Limits.MaxBodyBytes)
	if len(clean) == 0 || len(clean) > d.cfg.Limits.MaxBodyBytes || !json.Valid(clean) {
		return 0, ErrInvalidEvent
	}

	q := d.bind
	rows, err := tx.QueryContext(ctx, q(`SELECT id, event_types_json FROM webhook_subscriptions
		WHERE org_id = ? AND enabled = ? AND (project_id IS NULL OR project_id = ?) ORDER BY id`), event.OrgID, true, nullable(event.ProjectID))
	if err != nil {
		return 0, fmt.Errorf("webhook: load subscriptions: %w", err)
	}
	type subscription struct{ id, eventTypes string }
	var subs []subscription
	for rows.Next() {
		var s subscription
		if err := rows.Scan(&s.id, &s.eventTypes); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("webhook: scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("webhook: read subscriptions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("webhook: close subscriptions: %w", err)
	}
	type candidate struct{ subscriptionID string }
	var candidates []candidate
	for _, sub := range subs {
		var eventTypes []string
		if json.Unmarshal([]byte(sub.eventTypes), &eventTypes) != nil || !contains(eventTypes, event.Type) {
			continue
		}
		var existing int
		err := tx.QueryRowContext(ctx, q(`SELECT 1 FROM webhook_deliveries WHERE org_id=? AND subscription_id=? AND event_id=?`), event.OrgID, sub.id, event.ID).Scan(&existing)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("webhook: check event delivery idempotency: %w", err)
		}
		candidates = append(candidates, candidate{subscriptionID: sub.id})
	}
	var outstanding int
	if err := tx.QueryRowContext(ctx, q(`SELECT COUNT(*) FROM webhook_deliveries WHERE org_id=? AND status IN (?,?)`), event.OrgID, statusPending, statusRetry).Scan(&outstanding); err != nil {
		return 0, fmt.Errorf("webhook: count outstanding deliveries: %w", err)
	}
	if outstanding+len(candidates) > maxOutstandingPerOrg {
		return 0, fmt.Errorf("webhook: organization delivery capacity reached")
	}
	created := d.cfg.Now().UnixMilli()
	inserted := 0
	for _, sub := range candidates {
		id, err := idgen.New(idgen.PrefixWebhook)
		if err != nil {
			return 0, fmt.Errorf("webhook: generate delivery id: %w", err)
		}
		result, err := tx.ExecContext(ctx, q(`INSERT INTO webhook_deliveries
			(id, org_id, subscription_id, event_id, event_type, payload_json, attempt_count, status, next_attempt_at_ms, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?) ON CONFLICT(org_id,subscription_id,event_id) DO NOTHING`), id, event.OrgID, sub.subscriptionID, event.ID, event.Type, string(clean), statusPending, created, created)
		if err != nil {
			return 0, fmt.Errorf("webhook: persist delivery: %w", err)
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
			return 0, fmt.Errorf("webhook: count inserted delivery: %w", rowsErr)
		} else if affected == 1 {
			inserted++
		}
	}
	return inserted, nil
}

// lockOrganizationAdmission serializes bounded queue admission within a tenant.
// PostgreSQL uses a transaction advisory lock so the least-privileged worker
// needs no UPDATE grant on tenant records. SQLite takes its normal write lock
// through a no-op update to the existing organization row.
func (d *Dispatcher) lockOrganizationAdmission(ctx context.Context, tx *sql.Tx, orgID string) error {
	if d.cfg.Postgres {
		rows, err := tx.QueryContext(ctx, d.bind(`SELECT pg_advisory_xact_lock(hashtext(?))`), orgID)
		if err != nil {
			return fmt.Errorf("webhook: lock organization queue admission: %w", err)
		}
		if !rows.Next() {
			err = rows.Err()
			_ = rows.Close()
			if err == nil {
				err = errors.New("webhook: organization admission lock returned no row")
			}
			return err
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	}
	result, err := tx.ExecContext(ctx, `UPDATE organizations SET id=id WHERE id=?`, orgID)
	if err != nil {
		return fmt.Errorf("webhook: lock organization queue admission: %w", err)
	}
	locked, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("webhook: verify organization queue lock: %w", err)
	}
	if locked != 1 {
		return fmt.Errorf("webhook: organization not found for queue admission")
	}
	return nil
}

func (d *Dispatcher) dispatchOutbox(ctx context.Context) {
	d.cleanupTerminal(ctx)
	type pending struct{ id, org, project, eventType, payload string }
	var events []pending
	func() {
		unlock := d.lockDatabase()
		defer unlock()
		rows, err := d.db.QueryContext(ctx, d.bind(`SELECT id,org_id,COALESCE(project_id,''),event_type,payload_json
			FROM webhook_event_outbox WHERE dispatched_at_ms IS NULL ORDER BY created_at_ms,id LIMIT 64`))
		if err != nil {
			return
		}
		for rows.Next() {
			var event pending
			if rows.Scan(&event.id, &event.org, &event.project, &event.eventType, &event.payload) != nil {
				_ = rows.Close()
				events = nil
				return
			}
			events = append(events, event)
		}
		rowErr := rows.Err()
		closeErr := rows.Close()
		if rowErr != nil || closeErr != nil {
			events = nil
		}
	}()
	for _, event := range events {
		if _, err := d.Enqueue(ctx, Event{ID: event.id, OrgID: event.org, ProjectID: event.project, Type: event.eventType, Payload: json.RawMessage(event.payload)}); err != nil {
			continue
		}
		unlock := d.lockDatabase()
		_, _ = d.db.ExecContext(ctx, d.bind(`UPDATE webhook_event_outbox SET dispatched_at_ms=? WHERE id=? AND dispatched_at_ms IS NULL`), d.cfg.Now().UnixMilli(), event.id)
		unlock()
	}
}

func (d *Dispatcher) cleanupTerminal(ctx context.Context) {
	d.cleanupMu.Lock()
	defer d.cleanupMu.Unlock()
	unlock := d.lockDatabase()
	defer unlock()
	now := d.cfg.Now()
	if !d.lastCleanup.IsZero() && now.Sub(d.lastCleanup) < time.Hour {
		return
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	cutoff := now.Add(-d.cfg.Limits.TerminalRetention).UnixMilli()
	if _, err := tx.ExecContext(ctx, d.bind(`DELETE FROM webhook_deliveries WHERE status IN (?,?) AND terminal_at_ms IS NOT NULL AND terminal_at_ms<?`), statusDelivered, statusDead, cutoff); err != nil {
		return
	}
	if _, err := tx.ExecContext(ctx, d.bind(`DELETE FROM webhook_event_outbox WHERE dispatched_at_ms IS NOT NULL AND dispatched_at_ms<?`), cutoff); err != nil {
		return
	}
	if err := tx.Commit(); err == nil {
		d.lastCleanup = now
	}
}

// Run starts the bounded worker pool and runs until ctx is canceled. A dispatcher
// instance may only have one active Run call at a time.
func (d *Dispatcher) Run(ctx context.Context) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return errors.New("webhook: dispatcher already running")
	}
	d.running = true
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.running = false; d.mu.Unlock() }()
	var workers sync.WaitGroup
	workers.Add(d.cfg.Limits.Workers)
	for i := 0; i < d.cfg.Limits.Workers; i++ {
		go func() { defer workers.Done(); d.worker(ctx) }()
	}
	<-ctx.Done()
	workers.Wait()
	return nil
}

type delivery struct {
	id, orgID, subscriptionID, eventID, eventType, payload                         string
	attempt                                                                        int
	target, credential, targetCredential, projectID, boundAudience, targetAudience string
	allowPrivate                                                                   bool
	allowedCIDRs                                                                   string
}

func (d *Dispatcher) worker(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.Limits.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		d.dispatchOutbox(ctx)
		item, err := d.claim(ctx)
		if err == nil && item != nil {
			d.deliver(ctx, *item)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// claim atomically advances the queue lease and attempt count. Expired leases are
// eligible again after process loss, so the persistent queue remains recoverable.
func (d *Dispatcher) claim(ctx context.Context) (*delivery, error) {
	unlock := d.lockDatabase()
	defer unlock()
	now := d.cfg.Now().UnixMilli()
	lease := now + (d.cfg.Limits.RequestTimeout + d.cfg.Limits.LeaseSafety).Milliseconds()
	_, _ = d.db.ExecContext(ctx, d.bind(`UPDATE webhook_deliveries SET status = ?, last_error = ?, terminal_at_ms = ?
		WHERE attempt_count >= ? AND status IN (?, ?) AND next_attempt_at_ms <= ?`),
		statusDead, "attempt limit exhausted", now, d.cfg.Limits.MaxAttempts, statusPending, statusRetry, now)
	row := d.db.QueryRowContext(ctx, d.bind(`UPDATE webhook_deliveries SET attempt_count = attempt_count + 1, next_attempt_at_ms = ?
		WHERE id = (SELECT q.id FROM webhook_deliveries q JOIN webhook_subscriptions s
		ON s.org_id=q.org_id AND s.id=q.subscription_id WHERE s.enabled = ? AND q.status IN (?, ?)
		AND q.attempt_count < ? AND q.next_attempt_at_ms <= ? ORDER BY q.next_attempt_at_ms, q.created_at_ms, q.id LIMIT 1)
		AND status IN (?, ?) AND next_attempt_at_ms <= ? RETURNING id, org_id, subscription_id, event_id, event_type, payload_json, attempt_count`),
		lease, true, statusPending, statusRetry, d.cfg.Limits.MaxAttempts, now, statusPending, statusRetry, now)
	var item delivery
	if err := row.Scan(&item.id, &item.orgID, &item.subscriptionID, &item.eventID, &item.eventType, &item.payload, &item.attempt); err != nil {
		if errors.Is(err, sql.ErrNoRows) || ctx.Err() != nil {
			return nil, nil
		}
		return nil, err
	}
	err := d.db.QueryRowContext(ctx, d.bind(`SELECT s.target_url, s.signing_secret_credential_id, s.target_url_credential_id, s.allow_private_ip, s.allowed_cidrs_json,
		COALESCE(s.project_id,''), COALESCE(c.bound_audience,''), COALESCE(t.bound_audience,'')
		FROM webhook_subscriptions s JOIN secret_credentials c ON c.org_id=s.org_id AND c.id=s.signing_secret_credential_id
		JOIN secret_credentials t ON t.org_id=s.org_id AND t.id=s.target_url_credential_id
		WHERE s.org_id = ? AND s.id = ? AND s.enabled = ?
		AND (c.owner_project_id=s.project_id OR (c.owner_project_id IS NULL AND s.project_id IS NULL))
		AND (t.owner_project_id=s.project_id OR (t.owner_project_id IS NULL AND s.project_id IS NULL))`), item.orgID, item.subscriptionID, true).Scan(&item.target, &item.credential, &item.targetCredential, &item.allowPrivate, &item.allowedCIDRs, &item.projectID, &item.boundAudience, &item.targetAudience)
	if err != nil {
		d.fail(ctx, item, "subscription unavailable", 0)
		return nil, nil
	}
	return &item, nil
}

func (d *Dispatcher) deliver(parent context.Context, item delivery) {
	ctx, cancel := context.WithTimeout(parent, d.cfg.Limits.RequestTimeout)
	defer cancel()
	targetBytes, targetZero, err := d.cfg.Unseal(ctx, item.orgID, item.targetCredential)
	if err != nil || len(targetBytes) == 0 || targetZero == nil {
		if targetZero != nil {
			targetZero()
		}
		d.fail(parent, item, "webhook target unavailable", 0)
		return
	}
	defer targetZero()
	targetRaw := string(targetBytes)
	target, err := url.Parse(targetRaw)
	if err != nil || target == nil || target.Hostname() == "" || target.User != nil || target.Fragment != "" || target.Scheme != "http" && target.Scheme != "https" {
		d.fail(parent, item, "invalid webhook target", 0)
		return
	}
	if target.Scheme+"://"+target.Host != item.target {
		d.fail(parent, item, "sealed webhook target origin mismatch", 0)
		return
	}
	if item.targetAudience == "" {
		d.fail(parent, item, "webhook target audience unavailable", 0)
		return
	}
	targetAudience := item.targetAudience
	if matches, _ := broker.AudienceMatches(target, &targetAudience); !matches {
		d.fail(parent, item, "sealed webhook target audience mismatch", 0)
		return
	}
	var audience *string
	if item.boundAudience != "" {
		audience = &item.boundAudience
	}
	if matches, _ := broker.AudienceMatches(target, audience); !matches {
		d.fail(parent, item, "signing credential audience changed", 0)
		return
	}
	var cidrs []string
	if json.Unmarshal([]byte(item.allowedCIDRs), &cidrs) != nil {
		d.fail(parent, item, "invalid egress policy", 0)
		return
	}
	policy := egress.Policy{
		AllowedHosts:   []string{net.JoinHostPort(target.Hostname(), targetPort(target))},
		AllowPrivateIP: item.allowPrivate, AllowedCIDRs: cidrs,
		RequireTLS: target.Scheme == "https", RequirePrivateForHTTP: target.Scheme == "http",
	}
	client, err := egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy: policy, Timeout: d.cfg.Limits.RequestTimeout, Resolver: d.cfg.Resolver,
		DenyCrossOriginRedirects: true, MaxRedirects: -1,
	})
	if err != nil {
		d.fail(parent, item, "egress policy rejected", 0)
		return
	}
	defer client.CloseIdleConnections()
	secret, zero, err := d.cfg.Unseal(ctx, item.orgID, item.credential)
	if err != nil || len(secret) == 0 || zero == nil {
		if zero != nil {
			zero()
		}
		d.fail(parent, item, "signing credential unavailable", 0)
		return
	}
	defer zero()
	body := []byte(item.payload)
	if len(body) > d.cfg.Limits.MaxBodyBytes || !json.Valid(body) {
		d.fail(parent, item, "stored payload invalid", 0)
		return
	}
	timestamp := strconv.FormatInt(d.cfg.Now().Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		d.fail(parent, item, "request construction failed", 0)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tanpopo-Signature-256", signature)
	req.Header.Set("X-Tanpopo-Timestamp", timestamp)
	req.Header.Set("User-Agent", "Tanpopo-Webhook/1")
	resp, err := client.Do(req)
	if err != nil {
		d.fail(parent, item, "transport failure", 0)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		d.fail(parent, item, "upstream returned non-success status", resp.StatusCode)
		return
	}
	terminalAt := d.cfg.Now().UnixMilli()
	unlock := d.lockDatabase()
	_, _ = d.db.ExecContext(parent, d.bind(`UPDATE webhook_deliveries SET status = ?, last_http_status = ?, last_error = NULL, delivered_at_ms = ?, terminal_at_ms = ? WHERE org_id = ? AND id = ?`), statusDelivered, resp.StatusCode, terminalAt, terminalAt, item.orgID, item.id)
	unlock()
}

func (d *Dispatcher) fail(ctx context.Context, item delivery, safeError string, status int) {
	if status < 0 || status > 999 {
		status = 0
	}
	if item.attempt >= d.cfg.Limits.MaxAttempts {
		unlock := d.lockDatabase()
		_, _ = d.db.ExecContext(ctx, d.bind(`UPDATE webhook_deliveries SET status = ?, last_http_status = ?, last_error = ?, terminal_at_ms = ? WHERE org_id = ? AND id = ?`), statusDead, nullableInt(status), safeError, d.cfg.Now().UnixMilli(), item.orgID, item.id)
		unlock()
		return
	}
	delay := d.cfg.Limits.BaseBackoff
	for n := 1; n < item.attempt && delay < d.cfg.Limits.MaxBackoff; n++ {
		if delay > d.cfg.Limits.MaxBackoff/2 {
			delay = d.cfg.Limits.MaxBackoff
			break
		}
		delay *= 2
	}
	if delay > d.cfg.Limits.MaxBackoff {
		delay = d.cfg.Limits.MaxBackoff
	}
	unlock := d.lockDatabase()
	_, _ = d.db.ExecContext(ctx, d.bind(`UPDATE webhook_deliveries SET status = ?, next_attempt_at_ms = ?, last_http_status = ?, last_error = ? WHERE org_id = ? AND id = ?`), statusRetry, d.cfg.Now().Add(delay).UnixMilli(), nullableInt(status), safeError, item.orgID, item.id)
	unlock()
}

func (d *Dispatcher) bind(query string) string {
	if !d.cfg.Postgres {
		return query
	}
	var b strings.Builder
	index := 0
	for _, r := range query {
		if r == '?' {
			index++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(index))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func nullableInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}
func targetPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
