// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// SystemChainKey partitions instance-wide events with org_id IS NULL (CR-06).
	SystemChainKey = "SYSTEM"
	// genesisPrefix binds each independent audit hash chain's seq_no = 1 anchor.
	genesisPrefix = "tanpopo:audit:genesis:v1:"
)

type secretRule struct {
	re   *regexp.Regexp
	repl string
}

var (
	// defaultSecretRules redacts common credential formats in audit strings if no external hook overrides it.
	defaultSecretRules = []secretRule{
		{re: regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^@/\s]+)@`), repl: "${1}[REDACTED]@"},
		{re: regexp.MustCompile(`vk_tp_[A-Za-z0-9_-]+`), repl: "[REDACTED]"},
		{re: regexp.MustCompile(`tp_adm_[A-Za-z0-9_-]{8,}`), repl: "[REDACTED]"},
		{re: regexp.MustCompile(`sk-tp-[A-Za-z0-9._-]+`), repl: "[REDACTED]"},
		{re: regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{8,}`), repl: "[REDACTED]"},
		{re: regexp.MustCompile(`sk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{12,}`), repl: "[REDACTED]"},
		{re: regexp.MustCompile(`AIza[0-9A-Za-z_-]{16,}`), repl: "[REDACTED]"},
		{re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{16,}`), repl: "[REDACTED]"},
		{re: regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{8,}`), repl: "Bearer [REDACTED]"},
		{re: regexp.MustCompile(`(?i)\bBasic\s+[A-Za-z0-9+/=]{6,}`), repl: "Basic [REDACTED]"},
	}

	sensitiveAuditKeys = map[string]bool{
		"authorization":         true,
		"proxy_authorization":   true,
		"x_authorization":       true,
		"www_authenticate":      true,
		"proxy_authenticate":    true,
		"x_api_key":             true,
		"x_goog_api_key":        true,
		"x_tanpopo_virtual_key": true,
		"cookie":                true,
		"set_cookie":            true,
		"set_cookie2":           true,
		"api_key":               true,
		"apikey":                true,
		"secret":                true,
		"client_secret":         true,
		"access_token":          true,
		"refresh_token":         true,
		"id_token":              true,
		"private_key":           true,
		"password":              true,
		"passwd":                true,
		"credential":            true,
		"credentials":           true,
		"bearer_token":          true,
		"x_auth_token":          true,
		"openai_api_key":        true,
		"anthropic_api_key":     true,
		"tanpopo_master_key":    true,
		"token":                 true,
	}
)

// GenesisHash returns SHA-256("tanpopo:audit:genesis:v1:" + chainKey) in lowercase 64-char hex.
func GenesisHash(chainKey string) string {
	sum := sha256.Sum256([]byte(genesisPrefix + chainKey))
	return hex.EncodeToString(sum[:])
}

// ComputeEntryHash calculates SHA-256(chainKey + ":" + seqNo + ":" + prevHash + ":" + JCS(envelope))
// per data-model.md Section 3.2.
func ComputeEntryHash(chainKey string, seqNo int64, prevHash string, canonicalEnvelope []byte) string {
	var msg bytes.Buffer
	msg.Grow(len(chainKey) + 32 + len(prevHash) + len(canonicalEnvelope))
	msg.WriteString(chainKey)
	msg.WriteByte(':')
	msg.WriteString(fmt.Sprintf("%d", seqNo))
	msg.WriteByte(':')
	msg.WriteString(prevHash)
	msg.WriteByte(':')
	msg.Write(canonicalEnvelope)
	sum := sha256.Sum256(msg.Bytes())
	return hex.EncodeToString(sum[:])
}

// CanonicalEnvelopeBytes constructs the canonical RFC 8785 JCS representation of the audit envelope
// { id, created_at_ms, org_id, actor_kind, actor_id, entry_point, event_type, target_type, target_id, request_id, metadata }.
func CanonicalEnvelopeBytes(ev port.AuditEvent, parsedMeta any) ([]byte, error) {
	var orgVal any
	if ev.OrgID != nil && strings.TrimSpace(*ev.OrgID) != "" {
		orgVal = strings.TrimSpace(*ev.OrgID)
	}
	var reqVal any
	if ev.RequestID != nil && strings.TrimSpace(*ev.RequestID) != "" {
		reqVal = strings.TrimSpace(*ev.RequestID)
	}
	if parsedMeta == nil {
		parsedMeta = map[string]any{}
	}
	envMap := map[string]any{
		"id":            ev.ID,
		"created_at_ms": ev.CreatedAtMs,
		"org_id":        orgVal,
		"actor_kind":    ev.ActorKind,
		"actor_id":      ev.ActorID,
		"entry_point":   ev.EntryPoint,
		"event_type":    ev.EventType,
		"target_type":   ev.TargetType,
		"target_id":     ev.TargetID,
		"request_id":    reqVal,
		"metadata":      parsedMeta,
	}
	return CanonicalizeValue(envMap)
}

// LoggerConfig configures the hash-chained SQLite AuditSink & TxAuditSink.
type LoggerConfig struct {
	DB          *sql.DB
	NowMs       func() int64
	ScrubString func(string) string // Optional redaction hook (e.g., privacy.Redactor.SanitizeString).
}

// Logger persists gapless, per-chain_key RFC 8785 JCS hash-chained audit events into audit_events.
// It implements both port.AuditSink and port.TxAuditSink for fail-closed transactional atomicity.
type Logger struct {
	db          *sql.DB
	nowMs       func() int64
	scrubString func(string) string
	mu          sync.Mutex // Serializes standalone Append transactions; never locked inside AppendTx.
}

var _ port.TxAuditSink = (*Logger)(nil)

// NewLogger constructs a validated audit Logger.
func NewLogger(cfg LoggerConfig) (*Logger, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	return &Logger{
		db:          cfg.DB,
		nowMs:       nowFn,
		scrubString: cfg.ScrubString,
	}, nil
}

// Append persists a single hash-chained audit event inside its own transaction.
func (l *Logger) Append(ctx context.Context, event port.AuditEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("audit: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := l.appendWithTx(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("audit: commit append: %w", err)
	}
	return nil
}

// AppendTx writes a hash-chained audit event inside a caller-managed *sql.Tx BEFORE tx.Commit().
// Callers MUST treat any error as fatal and roll back the enclosing domain transaction (NEG-2).
func (l *Logger) AppendTx(ctx context.Context, tx *sql.Tx, event port.AuditEvent) error {
	if tx == nil {
		return fmt.Errorf("%w: non-nil *sql.Tx is required for AppendTx", port.ErrInvalidInput)
	}
	return l.appendWithTx(ctx, tx, event)
}

func (l *Logger) appendWithTx(ctx context.Context, tx *sql.Tx, ev port.AuditEvent) error {
	normalized, parsedMeta, canonMeta, err := l.normalizeEvent(ev)
	if err != nil {
		return err
	}

	chainKey := SystemChainKey
	var orgArg any
	if normalized.OrgID != nil {
		chainKey = *normalized.OrgID
		orgArg = *normalized.OrgID
	}

	var reqArg any
	if normalized.RequestID != nil {
		reqArg = *normalized.RequestID
	}

	// Read current chain tip (MAX seq_no + its entry_hash) inside this transaction.
	var (
		lastSeq  sql.NullInt64
		lastHash sql.NullString
	)
	err = tx.QueryRowContext(ctx, `
		SELECT seq_no, entry_hash
		FROM audit_events
		WHERE chain_key = ?
		ORDER BY seq_no DESC
		LIMIT 1`,
		chainKey,
	).Scan(&lastSeq, &lastHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("audit: read chain tip for %q: %w", chainKey, err)
	}

	nextSeq := int64(1)
	prevHash := GenesisHash(chainKey)
	if lastSeq.Valid {
		nextSeq = lastSeq.Int64 + 1
		prevHash = lastHash.String
	}

	envelopeBytes, err := CanonicalEnvelopeBytes(normalized, parsedMeta)
	if err != nil {
		return fmt.Errorf("audit: canonicalize envelope: %w", err)
	}
	entryHash := ComputeEntryHash(chainKey, nextSeq, prevHash, envelopeBytes)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, created_at_ms, org_id, chain_key, seq_no,
			actor_kind, actor_id, entry_point, event_type, target_type, target_id,
			request_id, metadata_json, prev_hash, entry_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.ID, normalized.CreatedAtMs, orgArg, chainKey, nextSeq,
		normalized.ActorKind, normalized.ActorID, normalized.EntryPoint, normalized.EventType, normalized.TargetType, normalized.TargetID,
		reqArg, string(canonMeta), prevHash, entryHash,
	); err != nil {
		return fmt.Errorf("audit: insert event into %q seq=%d: %w", chainKey, nextSeq, err)
	}
	return nil
}

func (l *Logger) normalizeEvent(ev port.AuditEvent) (port.AuditEvent, any, []byte, error) {
	out := ev
	out.ID = strings.TrimSpace(out.ID)
	if out.ID == "" {
		minted, err := idgen.New(idgen.PrefixAudit)
		if err != nil {
			return out, nil, nil, fmt.Errorf("audit: mint event ID: %w", err)
		}
		out.ID = minted
	}
	if out.CreatedAtMs <= 0 {
		out.CreatedAtMs = l.nowMs()
	}

	if out.OrgID != nil {
		trimmed := strings.TrimSpace(*out.OrgID)
		if trimmed == "" || strings.EqualFold(trimmed, SystemChainKey) {
			out.OrgID = nil
		} else {
			out.OrgID = &trimmed
		}
	}
	if out.RequestID != nil {
		trimmed := strings.TrimSpace(*out.RequestID)
		if trimmed == "" {
			out.RequestID = nil
		} else {
			scrubbedReq := l.scrub(trimmed)
			out.RequestID = &scrubbedReq
		}
	}

	out.ActorKind = l.scrub(strings.TrimSpace(out.ActorKind))
	out.ActorID = l.scrub(strings.TrimSpace(out.ActorID))
	out.EntryPoint = strings.TrimSpace(out.EntryPoint)
	out.EventType = strings.TrimSpace(out.EventType)
	out.TargetType = strings.TrimSpace(out.TargetType)
	out.TargetID = l.scrub(strings.TrimSpace(out.TargetID))

	if out.ActorKind == "" || out.ActorID == "" || out.EventType == "" || out.TargetType == "" || out.TargetID == "" {
		return out, nil, nil, fmt.Errorf("%w: actor_kind, actor_id, event_type, target_type, and target_id are required", port.ErrInvalidInput)
	}
	if !isCanonicalEntryPoint(out.EntryPoint) {
		return out, nil, nil, fmt.Errorf("%w: unsupported audit entry_point %q", port.ErrInvalidInput, out.EntryPoint)
	}

	rawMeta := strings.TrimSpace(out.MetadataJSON)
	if rawMeta == "" {
		rawMeta = "{}"
	}
	parsedMeta, err := decodeStrictJSONValue([]byte(rawMeta))
	if err != nil {
		return out, nil, nil, fmt.Errorf("%w: metadata_json: %v", port.ErrInvalidInput, err)
	}
	scrubbedMeta := l.scrubJSONValue(parsedMeta)
	if scrubbedMeta == nil {
		scrubbedMeta = map[string]any{}
	}
	canonMeta, err := CanonicalizeValue(scrubbedMeta)
	if err != nil {
		return out, nil, nil, fmt.Errorf("%w: canonicalize metadata_json: %v", port.ErrInvalidInput, err)
	}
	out.MetadataJSON = string(canonMeta)
	return out, scrubbedMeta, canonMeta, nil
}

func (l *Logger) scrub(s string) string {
	if s == "" {
		return s
	}
	out := s
	if l.scrubString != nil {
		out = l.scrubString(out)
	}
	for _, rule := range defaultSecretRules {
		out = rule.re.ReplaceAllString(out, rule.repl)
	}
	return out
}

func (l *Logger) scrubJSONValue(v any) any {
	switch x := v.(type) {
	case string:
		return l.scrub(x)
	case []any:
		res := make([]any, len(x))
		for i, item := range x {
			res[i] = l.scrubJSONValue(item)
		}
		return res
	case map[string]any:
		res := make(map[string]any, len(x))
		for k, val := range x {
			sk := l.scrub(k)
			if isSensitiveAuditKey(sk) {
				res[sk] = "[REDACTED]"
				continue
			}
			res[sk] = l.scrubJSONValue(val)
		}
		return res
	default:
		return v
	}
}

func normalizeAuditKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	prevUnderscore := false
	var prevRune rune
	for i, r := range trimmed {
		if r == '-' || r == '_' || r == '.' || r == ' ' {
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
			prevRune = r
			continue
		}
		if unicode.IsUpper(r) && i > 0 && !prevUnderscore {
			if unicode.IsLower(prevRune) || unicode.IsDigit(prevRune) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
		prevUnderscore = false
		prevRune = r
	}
	return strings.Trim(b.String(), "_")
}

func isSensitiveAuditKey(key string) bool {
	nk := normalizeAuditKey(key)
	if nk == "" {
		return false
	}
	if sensitiveAuditKeys[nk] {
		return true
	}
	for _, substr := range []string{
		"secret",
		"password",
		"passwd",
		"credential",
		"private_key",
		"signing_key",
		"encryption_key",
		"access_key",
		"master_key",
		"api_key",
		"apikey",
		"auth_header",
		"bearer",
	} {
		if strings.Contains(nk, substr) {
			return true
		}
	}
	if strings.HasSuffix(nk, "_token") || strings.HasSuffix(nk, "_cookie") {
		if strings.HasSuffix(nk, "_tokens") || strings.Contains(nk, "token_count") || strings.Contains(nk, "max_tokens") || strings.Contains(nk, "total_tokens") {
			return false
		}
		return true
	}
	return false
}

func decodeStrictJSONValue(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var val any
	if err := dec.Decode(&val); err != nil {
		return nil, err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("trailing JSON tokens")
	}
	return val, nil
}

func isCanonicalEntryPoint(ep string) bool {
	switch ep {
	case port.AuditEntryPointAdminAPI,
		port.AuditEntryPointCLI,
		port.AuditEntryPointGitOpsBoot,
		port.AuditEntryPointSystemReaper,
		port.AuditEntryPointDataPlaneGuard:
		return true
	default:
		return false
	}
}

// StoredEvent represents a persisted row from audit_events including chain metadata.
type StoredEvent struct {
	ID           string  `json:"id"`
	CreatedAtMs  int64   `json:"created_at_ms"`
	OrgID        *string `json:"org_id,omitempty"`
	ChainKey     string  `json:"chain_key"`
	SeqNo        int64   `json:"seq_no"`
	ActorKind    string  `json:"actor_kind"`
	ActorID      string  `json:"actor_id"`
	EntryPoint   string  `json:"entry_point"`
	EventType    string  `json:"event_type"`
	TargetType   string  `json:"target_type"`
	TargetID     string  `json:"target_id"`
	RequestID    *string `json:"request_id,omitempty"`
	MetadataJSON string  `json:"metadata_json"`
	PrevHash     string  `json:"prev_hash"`
	EntryHash    string  `json:"entry_hash"`
}

// ListFilter scopes queries against audit_events without cross-tenant leakage.
type ListFilter struct {
	ChainKey   string
	EventType  string
	TargetType string
	TargetID   string
	FromMs     int64
	ToMs       int64
	Limit      int
}

// List returns audit events matching filter ordered newest-first (DESC by seq_no).
func (l *Logger) List(ctx context.Context, filter ListFilter) ([]StoredEvent, error) {
	return ListEvents(ctx, l.db, filter)
}

// ListEvents queries persisted audit_events filtered cleanly by ChainKey and optional selectors.
func ListEvents(ctx context.Context, db *sql.DB, filter ListFilter) ([]StoredEvent, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	chainKey := strings.TrimSpace(filter.ChainKey)
	if chainKey == "" {
		return nil, fmt.Errorf("%w: chain_key is required for audit listing", port.ErrInvalidInput)
	}

	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	query := `
		SELECT id, created_at_ms, org_id, chain_key, seq_no,
		       actor_kind, actor_id, entry_point, event_type, target_type, target_id,
		       request_id, metadata_json, prev_hash, entry_hash
		FROM audit_events
		WHERE chain_key = ?`
	args := []any{chainKey}

	if et := strings.TrimSpace(filter.EventType); et != "" {
		query += ` AND event_type = ?`
		args = append(args, et)
	}
	if tt := strings.TrimSpace(filter.TargetType); tt != "" {
		query += ` AND target_type = ?`
		args = append(args, tt)
	}
	if tid := strings.TrimSpace(filter.TargetID); tid != "" {
		query += ` AND target_id = ?`
		args = append(args, tid)
	}
	if filter.FromMs > 0 {
		query += ` AND created_at_ms >= ?`
		args = append(args, filter.FromMs)
	}
	if filter.ToMs > 0 {
		query += ` AND created_at_ms <= ?`
		args = append(args, filter.ToMs)
	}
	query += ` ORDER BY seq_no DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: list events: %w", err)
	}
	defer rows.Close()

	var events []StoredEvent
	for rows.Next() {
		ev, serr := scanStoredEvent(rows)
		if serr != nil {
			return nil, serr
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: iterate list events: %w", err)
	}
	return events, nil
}

func scanStoredEvent(s interface{ Scan(...any) error }) (StoredEvent, error) {
	var (
		ev    StoredEvent
		orgNS sql.NullString
		reqNS sql.NullString
	)
	if err := s.Scan(
		&ev.ID, &ev.CreatedAtMs, &orgNS, &ev.ChainKey, &ev.SeqNo,
		&ev.ActorKind, &ev.ActorID, &ev.EntryPoint, &ev.EventType, &ev.TargetType, &ev.TargetID,
		&reqNS, &ev.MetadataJSON, &ev.PrevHash, &ev.EntryHash,
	); err != nil {
		return StoredEvent{}, fmt.Errorf("audit: scan stored event: %w", err)
	}
	if orgNS.Valid {
		v := orgNS.String
		ev.OrgID = &v
	}
	if reqNS.Valid {
		v := reqNS.String
		ev.RequestID = &v
	}
	return ev, nil
}
