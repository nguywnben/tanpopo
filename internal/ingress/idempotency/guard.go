// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package idempotency enforces atomic per-(org_id, project_id, virtual_key_id,
// idempotency_key) replay deduplication and payload digest verification for
// non-streaming Data Plane POST endpoints (TP-IMP-0016 / SPEC-EXT-06).
package idempotency

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// MaxKeyLength bounds Idempotency-Key header length to match idempotency_records VARCHAR(128).
	MaxKeyLength = 128
	// DefaultTTL sets the 24-hour idempotency replay window (SPEC-EXT-06).
	DefaultTTL = 24 * time.Hour
	// DefaultMaxEntries bounds in-memory cached idempotency records.
	DefaultMaxEntries = 10000
	// DefaultMaxTotalBytes bounds total cached response body bytes held in memory (64 MiB).
	DefaultMaxTotalBytes int64 = 64 << 20
	// DefaultMaxRecordBodyBytes caps single-response caching (4 MiB) so oversized responses never bloat storage.
	DefaultMaxRecordBodyBytes = 4 << 20

	sqlitePruneIntervalMs int64 = 60_000
	sqlitePruneBatchLimit int64 = 512

	stateInFlight  = "IN_FLIGHT"
	stateCompleted = "COMPLETED"
)

var (
	// ErrInvalidKey indicates a malformed or oversized Idempotency-Key header.
	ErrInvalidKey = errors.New("idempotency: invalid Idempotency-Key header")
)

// Outcome describes the atomic state transition after claiming an Idempotency-Key.
type Outcome int

const (
	// OutcomeAcquired indicates the caller now owns the IN_FLIGHT claim.
	OutcomeAcquired Outcome = iota + 1
	// OutcomeReplay indicates a matching completed response was found and should be replayed.
	OutcomeReplay
	// OutcomeInFlight indicates another concurrent request holds an active IN_FLIGHT claim.
	OutcomeInFlight
	// OutcomeMismatch indicates the key was already used with a different request payload digest.
	OutcomeMismatch
)

// ScopeKey isolates Idempotency-Key namespaces strictly per tenant, project, and Virtual Key.
type ScopeKey struct {
	OrgID          string
	ProjectID      string
	VirtualKeyID   string
	IdempotencyKey string
}

// CachedResponse captures a settled HTTP response safe for deterministic replay.
type CachedResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// BeginResult returns either an active claim handle or an immediate replay/conflict/mismatch decision.
type BeginResult struct {
	Outcome Outcome
	Claim   *Claim
	Replay  *CachedResponse
}

// Config configures an Idempotency Guard backed by SQLite or in-memory state.
type Config struct {
	DB                 *sql.DB
	TTL                time.Duration
	NowMs              func() int64
	MaxEntries         int
	MaxTotalBytes      int64
	MaxRecordBodyBytes int
}

// Guard provides atomic Idempotency-Key claiming, replay, and cleanup.
type Guard struct {
	db                 *sql.DB
	dialect            port.Dialect
	ttlMs              int64
	nowMs              func() int64
	maxEntries         int
	maxTotalBytes      int64
	maxRecordBodyBytes int

	mu             sync.Mutex
	records        map[ScopeKey]*memRecord
	memTotalBytes  int64
	nextSeq        uint64
	lastSQLPruneMs atomic.Int64
}

type memRecord struct {
	payloadSHA256 string
	state         string
	httpStatus    int
	headers       map[string]string
	body          []byte
	expiresAtMs   int64
	seq           uint64
}

// Claim represents ownership of an active IN_FLIGHT idempotency entry.
type Claim struct {
	guard         *Guard
	scope         ScopeKey
	payloadSHA256 string
	mu            sync.Mutex
	done          bool
}

// NewGuard constructs an Idempotency Guard. When cfg.DB is non-nil it persists
// across requests via idempotency_records; otherwise it operates safely in RAM.
func NewGuard(cfg Config) *Guard {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	maxEntries := cfg.MaxEntries
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	maxTotalBytes := cfg.MaxTotalBytes
	if maxTotalBytes <= 0 {
		maxTotalBytes = DefaultMaxTotalBytes
	}
	maxBodyBytes := cfg.MaxRecordBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = DefaultMaxRecordBodyBytes
	}
	var dialect port.Dialect
	if cfg.DB != nil {
		dialect = port.DetectDialect(context.Background(), cfg.DB)
	}
	return &Guard{
		db:                 cfg.DB,
		dialect:            dialect,
		ttlMs:              ttl.Milliseconds(),
		nowMs:              nowFn,
		maxEntries:         maxEntries,
		maxTotalBytes:      maxTotalBytes,
		maxRecordBodyBytes: maxBodyBytes,
		records:            make(map[ScopeKey]*memRecord),
	}
}

// ValidateKey validates raw Idempotency-Key header syntax (1..128 printable ASCII chars).
func ValidateKey(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%w: empty header value", ErrInvalidKey)
	}
	if len(raw) > MaxKeyLength {
		return "", fmt.Errorf("%w: exceeds %d characters", ErrInvalidKey, MaxKeyLength)
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed != raw {
		return "", fmt.Errorf("%w: leading or trailing whitespace is prohibited", ErrInvalidKey)
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c < 0x21 || c > 0x7e {
			return "", fmt.Errorf("%w: non-printable ASCII or whitespace character prohibited", ErrInvalidKey)
		}
	}
	return raw, nil
}

// ComputePayloadSHA256 computes a canonical 64-hex SHA-256 digest bound to HTTP method, route, and body.
func ComputePayloadSHA256(method, routePath string, body []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(strings.ToUpper(strings.TrimSpace(method))))
	_, _ = h.Write([]byte{'\n'})
	_, _ = h.Write([]byte(strings.TrimSpace(routePath)))
	_, _ = h.Write([]byte{'\n'})
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// Begin atomically claims scope+payloadSHA256 or reports Replay / InFlight / Mismatch.
func (g *Guard) Begin(ctx context.Context, scope ScopeKey, payloadSHA256 string) (*BeginResult, error) {
	if g == nil {
		return &BeginResult{Outcome: OutcomeAcquired}, nil
	}
	if scope.OrgID == "" || scope.ProjectID == "" || scope.VirtualKeyID == "" || scope.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: incomplete idempotency scope", ErrInvalidKey)
	}
	if len(payloadSHA256) != 64 {
		return nil, fmt.Errorf("idempotency: invalid payload digest length %d", len(payloadSHA256))
	}

	now := g.nowMs()
	if g.db != nil {
		return g.beginSQL(ctx, scope, payloadSHA256, now)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.beginMemory(scope, payloadSHA256, now), nil
}

func (g *Guard) deleteMemoryRecordLocked(k ScopeKey) {
	if rec, ok := g.records[k]; ok {
		g.memTotalBytes -= int64(len(rec.body))
		if g.memTotalBytes < 0 {
			g.memTotalBytes = 0
		}
		delete(g.records, k)
	}
}

func (g *Guard) evictMemoryLocked(now int64) {
	for k, rec := range g.records {
		if rec.expiresAtMs <= now {
			g.deleteMemoryRecordLocked(k)
		}
	}
	for len(g.records) > g.maxEntries || g.memTotalBytes > g.maxTotalBytes {
		var oldestKey ScopeKey
		var oldestSeq uint64
		foundCompleted := false
		for k, rec := range g.records {
			if rec.state != stateCompleted {
				continue
			}
			if !foundCompleted || rec.seq < oldestSeq {
				oldestKey = k
				oldestSeq = rec.seq
				foundCompleted = true
			}
		}
		if foundCompleted {
			g.deleteMemoryRecordLocked(oldestKey)
			continue
		}
		// Fallback only if exclusively IN_FLIGHT records exceed cap: evict oldest entry.
		var oldestAny ScopeKey
		var oldestAnySeq uint64
		foundAny := false
		for k, rec := range g.records {
			if !foundAny || rec.seq < oldestAnySeq {
				oldestAny = k
				oldestAnySeq = rec.seq
				foundAny = true
			}
		}
		if !foundAny {
			break
		}
		g.deleteMemoryRecordLocked(oldestAny)
	}
}

func (g *Guard) beginMemory(scope ScopeKey, payloadSHA256 string, now int64) *BeginResult {
	g.evictMemoryLocked(now)
	existing, ok := g.records[scope]
	if ok {
		if existing.payloadSHA256 != payloadSHA256 {
			return &BeginResult{Outcome: OutcomeMismatch}
		}
		if existing.state == stateInFlight {
			return &BeginResult{Outcome: OutcomeInFlight}
		}
		hdrsCopy := make(map[string]string, len(existing.headers))
		for k, v := range existing.headers {
			hdrsCopy[k] = v
		}
		return &BeginResult{
			Outcome: OutcomeReplay,
			Replay: &CachedResponse{
				StatusCode: existing.httpStatus,
				Headers:    hdrsCopy,
				Body:       append([]byte(nil), existing.body...),
			},
		}
	}

	g.nextSeq++
	g.records[scope] = &memRecord{
		payloadSHA256: payloadSHA256,
		state:         stateInFlight,
		expiresAtMs:   now + g.ttlMs,
		seq:           g.nextSeq,
	}
	g.evictMemoryLocked(now)
	return &BeginResult{
		Outcome: OutcomeAcquired,
		Claim: &Claim{
			guard:         g,
			scope:         scope,
			payloadSHA256: payloadSHA256,
		},
	}
}

func (g *Guard) beginSQL(ctx context.Context, scope ScopeKey, payloadSHA256 string, now int64) (*BeginResult, error) {
	tx, err := g.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, g.dialect, scope.OrgID); err != nil {
		return nil, err
	}

	pruneCaughtUp := false
	lastPrune := g.lastSQLPruneMs.Load()
	if lastPrune == 0 || now-lastPrune >= sqlitePruneIntervalMs || now < lastPrune {
		var (
			pruneRes sql.Result
			perr     error
		)
		if g.dialect == port.DialectPostgres {
			pruneRes, perr = tx.ExecContext(ctx, port.Rebind(g.dialect, `
				DELETE FROM idempotency_records
				WHERE org_id = ? AND ctid IN (
					SELECT ctid FROM idempotency_records
					WHERE org_id = ? AND expires_at_ms <= ?
					LIMIT ?
				)
			`), scope.OrgID, scope.OrgID, now, sqlitePruneBatchLimit)
		} else {
			pruneRes, perr = tx.ExecContext(ctx, `
				DELETE FROM idempotency_records
				WHERE rowid IN (
					SELECT rowid FROM idempotency_records
					WHERE expires_at_ms <= ?
					LIMIT ?
				)
			`, now, sqlitePruneBatchLimit)
		}
		if perr != nil {
			return nil, perr
		}
		if deleted, _ := pruneRes.RowsAffected(); deleted < sqlitePruneBatchLimit {
			pruneCaughtUp = true
		}
	}

	if _, err := tx.ExecContext(ctx, port.Rebind(g.dialect, `
		DELETE FROM idempotency_records
		WHERE org_id = ? AND project_id = ? AND virtual_key_id = ? AND idempotency_key = ? AND expires_at_ms <= ?
	`), scope.OrgID, scope.ProjectID, scope.VirtualKeyID, scope.IdempotencyKey, now); err != nil {
		return nil, err
	}

	var (
		existingSHA string
		state       string
		statusNull  sql.NullInt64
		hdrsNull    sql.NullString
		bodyBytes   []byte
	)
	row := tx.QueryRowContext(ctx, port.Rebind(g.dialect, `
		SELECT payload_sha256, state, http_status, response_headers_json, response_body_bytes
		FROM idempotency_records
		WHERE org_id = ? AND project_id = ? AND virtual_key_id = ? AND idempotency_key = ?
	`), scope.OrgID, scope.ProjectID, scope.VirtualKeyID, scope.IdempotencyKey)
	scanErr := row.Scan(&existingSHA, &state, &statusNull, &hdrsNull, &bodyBytes)
	switch {
	case scanErr == nil:
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		if pruneCaughtUp {
			g.lastSQLPruneMs.Store(now)
		}
		if existingSHA != payloadSHA256 {
			return &BeginResult{Outcome: OutcomeMismatch}, nil
		}
		if state == stateInFlight {
			return &BeginResult{Outcome: OutcomeInFlight}, nil
		}
		hdrs := make(map[string]string)
		if hdrsNull.Valid && strings.TrimSpace(hdrsNull.String) != "" {
			if jerr := json.Unmarshal([]byte(hdrsNull.String), &hdrs); jerr != nil {
				return nil, fmt.Errorf("idempotency: decode cached headers: %w", jerr)
			}
		}
		status := http.StatusOK
		if statusNull.Valid && statusNull.Int64 > 0 {
			status = int(statusNull.Int64)
		}
		return &BeginResult{
			Outcome: OutcomeReplay,
			Replay: &CachedResponse{
				StatusCode: status,
				Headers:    hdrs,
				Body:       append([]byte(nil), bodyBytes...),
			},
		}, nil
	case !errors.Is(scanErr, sql.ErrNoRows):
		return nil, scanErr
	}

	res, err := tx.ExecContext(ctx, port.Rebind(g.dialect, `
		INSERT INTO idempotency_records (
			org_id, project_id, virtual_key_id, idempotency_key,
			payload_sha256, state, created_at_ms, expires_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(org_id, project_id, virtual_key_id, idempotency_key) DO NOTHING
	`), scope.OrgID, scope.ProjectID, scope.VirtualKeyID, scope.IdempotencyKey, payloadSHA256, stateInFlight, now, now+g.ttlMs)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_ = tx.Rollback()
		return &BeginResult{Outcome: OutcomeInFlight}, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if pruneCaughtUp {
		g.lastSQLPruneMs.Store(now)
	}
	return &BeginResult{
		Outcome: OutcomeAcquired,
		Claim: &Claim{
			guard:         g,
			scope:         scope,
			payloadSHA256: payloadSHA256,
		},
	}, nil
}

func (g *Guard) execOrgScoped(ctx context.Context, orgID, query string, args ...any) (sql.Result, error) {
	q := port.Rebind(g.dialect, query)
	if g.dialect != port.DialectPostgres {
		return g.db.ExecContext(ctx, q, args...)
	}
	tx, err := g.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, g.dialect, orgID); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

// Complete persists the settled response so subsequent matching retries replay cleanly.
// Responses larger than MaxRecordBodyBytes release the IN_FLIGHT claim without caching.
func (c *Claim) Complete(ctx context.Context, statusCode int, headers map[string]string, body []byte) error {
	if c == nil || c.guard == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return nil
	}

	g := c.guard
	if len(body) > g.maxRecordBodyBytes || int64(len(body)) > g.maxTotalBytes {
		if g.db != nil {
			if _, err := g.execOrgScoped(ctx, c.scope.OrgID, `
				DELETE FROM idempotency_records
				WHERE org_id = ? AND project_id = ? AND virtual_key_id = ? AND idempotency_key = ? AND payload_sha256 = ? AND state = ?
			`, c.scope.OrgID, c.scope.ProjectID, c.scope.VirtualKeyID, c.scope.IdempotencyKey, c.payloadSHA256, stateInFlight); err != nil {
				return err
			}
		} else {
			g.mu.Lock()
			g.deleteMemoryRecordLocked(c.scope)
			g.mu.Unlock()
		}
		c.done = true
		return nil
	}

	now := g.nowMs()
	hdrCopy := make(map[string]string, len(headers))
	for k, v := range headers {
		hdrCopy[k] = v
	}

	if g.db != nil {
		hdrJSON, err := json.Marshal(hdrCopy)
		if err != nil {
			return err
		}
		res, err := g.execOrgScoped(ctx, c.scope.OrgID, `
			UPDATE idempotency_records
			SET state = ?, http_status = ?, response_headers_json = ?, response_body_bytes = ?, expires_at_ms = ?
			WHERE org_id = ? AND project_id = ? AND virtual_key_id = ? AND idempotency_key = ? AND payload_sha256 = ? AND state = ?
		`, stateCompleted, statusCode, string(hdrJSON), body, now+g.ttlMs,
			c.scope.OrgID, c.scope.ProjectID, c.scope.VirtualKeyID, c.scope.IdempotencyKey, c.payloadSHA256, stateInFlight)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return errors.New("idempotency: claim missing or expired before completion")
		}
		c.done = true
		return nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.deleteMemoryRecordLocked(c.scope)
	g.nextSeq++
	bodyCopy := append([]byte(nil), body...)
	g.records[c.scope] = &memRecord{
		payloadSHA256: c.payloadSHA256,
		state:         stateCompleted,
		httpStatus:    statusCode,
		headers:       hdrCopy,
		body:          bodyCopy,
		expiresAtMs:   now + g.ttlMs,
		seq:           g.nextSeq,
	}
	g.memTotalBytes += int64(len(bodyCopy))
	g.evictMemoryLocked(now)
	c.done = true
	return nil
}

// Abort removes an unsettled IN_FLIGHT claim (e.g. transient upstream failure) so retries can proceed.
func (c *Claim) Abort(ctx context.Context) {
	if c == nil || c.guard == nil {
		return
	}
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return
	}
	c.done = true
	c.mu.Unlock()

	g := c.guard
	if g.db != nil {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}
		_, _ = g.execOrgScoped(ctx, c.scope.OrgID, `
			DELETE FROM idempotency_records
			WHERE org_id = ? AND project_id = ? AND virtual_key_id = ? AND idempotency_key = ? AND payload_sha256 = ? AND state = ?
		`, c.scope.OrgID, c.scope.ProjectID, c.scope.VirtualKeyID, c.scope.IdempotencyKey, c.payloadSHA256, stateInFlight)
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if rec, ok := g.records[c.scope]; ok && rec.state == stateInFlight && rec.payloadSHA256 == c.payloadSHA256 {
		g.deleteMemoryRecordLocked(c.scope)
	}
}
