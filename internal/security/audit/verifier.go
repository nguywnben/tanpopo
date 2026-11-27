// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// AlertAuditTamper is the canonical P0 monitoring alert code raised when hash-chain verification fails (OPS-AC-08).
const AlertAuditTamper = "ALT-AUDIT-TAMPER"

// ErrAuditTampered is returned by VerifyAuditChain whenever any chain breaks hash, genesis, or sequence continuity.
var ErrAuditTampered = errors.New("audit: tamper detected in immutable audit hash chain")

// TamperKind classifies the exact invariant violation observed during chain verification.
type TamperKind string

const (
	TamperMissingSequence   TamperKind = "MISSING_SEQUENCE"
	TamperGenesisMismatch   TamperKind = "GENESIS_HASH_MISMATCH"
	TamperPrevHashMismatch  TamperKind = "PREV_HASH_MISMATCH"
	TamperEntryHashMismatch TamperKind = "ENTRY_HASH_MISMATCH"
	TamperMalformedRecord   TamperKind = "MALFORMED_RECORD"
)

// TamperFinding pinpoints the exact chain and sequence number where integrity verification failed.
type TamperFinding struct {
	AlertCode    string     `json:"alert_code"`
	ChainKey     string     `json:"chain_key"`
	SeqNo        int64      `json:"seq_no"`
	EventID      string     `json:"event_id,omitempty"`
	Kind         TamperKind `json:"kind"`
	Detail       string     `json:"detail"`
	ExpectedHash string     `json:"expected_hash,omitempty"`
	ActualHash   string     `json:"actual_hash,omitempty"`
}

// ChainReport summarizes verification results for one partitioned audit chain (org_id or SYSTEM).
type ChainReport struct {
	ChainKey      string          `json:"chain_key"`
	VerifiedCount int             `json:"verified_count"`
	LastSeqNo     int64           `json:"last_seq_no"`
	LastHash      string          `json:"last_hash,omitempty"`
	Valid         bool            `json:"valid"`
	Findings      []TamperFinding `json:"findings,omitempty"`
}

// VerifyReport aggregates verification verdicts across all inspected audit chains.
type VerifyReport struct {
	TotalVerified int                    `json:"total_verified"`
	Valid         bool                   `json:"valid"`
	Chains        map[string]ChainReport `json:"chains"`
	Findings      []TamperFinding        `json:"findings,omitempty"`
}

// VerifyOptions configures optional chain filtering and tamper alert hooks.
type VerifyOptions struct {
	ChainKeys []string            // Optional restrict list (e.g., single org_id or []string{"SYSTEM"}); empty checks all chains.
	OnTamper  func(TamperFinding) // Invoked immediately upon detecting any tamper finding (ALT-AUDIT-TAMPER).
}

// VerifyAuditChain walks every selected partitioned audit chain in ascending seq_no order,
// validating gapless sequence numbering, genesis prev_hash, link continuity, and RFC 8785 JCS entry_hash digests.
// Returns both the detailed VerifyReport and ErrAuditTampered if any finding is detected (CT-30 / OPS-AC-08).
func VerifyAuditChain(ctx context.Context, db *sql.DB, opts VerifyOptions) (*VerifyReport, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}

	chainKeys, err := resolveChainKeysToVerify(ctx, db, opts.ChainKeys)
	if err != nil {
		return nil, err
	}

	report := &VerifyReport{
		Valid:  true,
		Chains: make(map[string]ChainReport, len(chainKeys)),
	}

	for _, ck := range chainKeys {
		cr, err := verifySingleChain(ctx, db, ck, opts.OnTamper)
		if err != nil {
			return nil, err
		}
		report.Chains[ck] = cr
		report.TotalVerified += cr.VerifiedCount
		if !cr.Valid {
			report.Valid = false
			report.Findings = append(report.Findings, cr.Findings...)
		}
	}

	if !report.Valid {
		return report, fmt.Errorf("%w: %d finding(s) across %d chain(s)", ErrAuditTampered, len(report.Findings), len(chainKeys))
	}
	return report, nil
}

// VerifyAuditChainTx executes VerifyAuditChain against a caller-managed *sql.Tx
// (such as an uncommitted PostgreSQL migration transaction), closing cursors
// cleanly between chains and enforcing FORCE RLS scoping per chain_key.
func VerifyAuditChainTx(ctx context.Context, tx *sql.Tx, dialect port.Dialect, opts VerifyOptions) (*VerifyReport, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.Tx is required", port.ErrInvalidInput)
	}
	chainKeys, err := resolveChainKeysToVerifyTx(ctx, tx, dialect, opts.ChainKeys)
	if err != nil {
		return nil, err
	}
	report := &VerifyReport{
		Valid:  true,
		Chains: make(map[string]ChainReport, len(chainKeys)),
	}
	for _, ck := range chainKeys {
		if dialect == port.DialectPostgres {
			if err := setAuditRLSTx(ctx, tx, ck); err != nil {
				return nil, err
			}
		}
		query := port.Rebind(dialect, `
			SELECT id, created_at_ms, org_id, chain_key, seq_no,
			       actor_kind, actor_id, entry_point, event_type, target_type, target_id,
			       request_id, metadata_json, prev_hash, entry_hash
			FROM audit_events
			WHERE chain_key = ?
			ORDER BY seq_no ASC`)
		rows, err := tx.QueryContext(ctx, query, ck)
		if err != nil {
			return nil, fmt.Errorf("audit: query chain %q: %w", ck, err)
		}
		cr, vErr := verifyChainRows(rows, ck, opts.OnTamper)
		_ = rows.Close()
		if vErr != nil {
			return nil, vErr
		}
		report.Chains[ck] = cr
		report.TotalVerified += cr.VerifiedCount
		if !cr.Valid {
			report.Valid = false
			report.Findings = append(report.Findings, cr.Findings...)
		}
	}
	if !report.Valid {
		return report, fmt.Errorf("%w: %d finding(s) across %d chain(s)", ErrAuditTampered, len(report.Findings), len(chainKeys))
	}
	return report, nil
}

func resolveChainKeysToVerifyTx(ctx context.Context, tx *sql.Tx, dialect port.Dialect, requested []string) ([]string, error) {
	if len(requested) > 0 {
		seen := make(map[string]bool, len(requested))
		out := make([]string, 0, len(requested))
		for _, k := range requested {
			trimmed := strings.TrimSpace(k)
			if trimmed != "" && !seen[trimmed] {
				seen[trimmed] = true
				out = append(out, trimmed)
			}
		}
		sort.Strings(out)
		return out, nil
	}
	if dialect == port.DialectPostgres {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'true', true)`); err != nil {
			return nil, fmt.Errorf("audit: set app.is_instance_operator: %w", err)
		}
		orgRows, err := tx.QueryContext(ctx, `SELECT id FROM organizations ORDER BY id ASC`)
		if err != nil {
			return nil, fmt.Errorf("audit: list organizations for chain verification: %w", err)
		}
		candidates := []string{SystemChainKey}
		for orgRows.Next() {
			var orgID string
			if serr := orgRows.Scan(&orgID); serr != nil {
				_ = orgRows.Close()
				return nil, fmt.Errorf("audit: scan org id: %w", serr)
			}
			candidates = append(candidates, orgID)
		}
		if err := orgRows.Err(); err != nil {
			_ = orgRows.Close()
			return nil, fmt.Errorf("audit: iterate organizations: %w", err)
		}
		_ = orgRows.Close()

		var keys []string
		for _, ck := range candidates {
			if err := setAuditRLSTx(ctx, tx, ck); err != nil {
				return nil, err
			}
			var one int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM audit_events WHERE chain_key = $1 LIMIT 1`, ck).Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("audit: probe chain %q: %w", ck, err)
			}
			keys = append(keys, ck)
		}
		sort.Strings(keys)
		return keys, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT chain_key FROM audit_events ORDER BY chain_key ASC`)
	if err != nil {
		return nil, fmt.Errorf("audit: list chain keys: %w", err)
	}
	var keys []string
	for rows.Next() {
		var k string
		if serr := rows.Scan(&k); serr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("audit: scan chain key: %w", serr)
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("audit: iterate chain keys: %w", err)
	}
	return keys, nil
}

func resolveChainKeysToVerify(ctx context.Context, db *sql.DB, requested []string) ([]string, error) {
	if len(requested) > 0 {
		seen := make(map[string]bool, len(requested))
		out := make([]string, 0, len(requested))
		for _, k := range requested {
			trimmed := strings.TrimSpace(k)
			if trimmed != "" && !seen[trimmed] {
				seen[trimmed] = true
				out = append(out, trimmed)
			}
		}
		sort.Strings(out)
		return out, nil
	}

	dialect := port.DetectDialect(ctx, db)
	if dialect == port.DialectPostgres {
		orgTx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, fmt.Errorf("audit: begin organization discovery tx: %w", err)
		}
		defer func() { _ = orgTx.Rollback() }()
		if _, err := orgTx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'true', true)`); err != nil {
			return nil, fmt.Errorf("audit: set app.is_instance_operator: %w", err)
		}
		orgRows, err := orgTx.QueryContext(ctx, `SELECT id FROM organizations ORDER BY id ASC`)
		if err != nil {
			return nil, fmt.Errorf("audit: list organizations for chain verification: %w", err)
		}
		candidates := []string{SystemChainKey}
		for orgRows.Next() {
			var orgID string
			if serr := orgRows.Scan(&orgID); serr != nil {
				_ = orgRows.Close()
				return nil, fmt.Errorf("audit: scan org id: %w", serr)
			}
			candidates = append(candidates, orgID)
		}
		if err := orgRows.Err(); err != nil {
			_ = orgRows.Close()
			return nil, fmt.Errorf("audit: iterate organizations: %w", err)
		}
		_ = orgRows.Close()
		_ = orgTx.Rollback()

		var keys []string
		for _, ck := range candidates {
			hasEvents, err := chainHasEventsPG(ctx, db, ck)
			if err != nil {
				return nil, err
			}
			if hasEvents {
				keys = append(keys, ck)
			}
		}
		sort.Strings(keys)
		return keys, nil
	}

	rows, err := db.QueryContext(ctx, `SELECT DISTINCT chain_key FROM audit_events ORDER BY chain_key ASC`)
	if err != nil {
		return nil, fmt.Errorf("audit: list chain keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if serr := rows.Scan(&k); serr != nil {
			return nil, fmt.Errorf("audit: scan chain key: %w", serr)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: iterate chain keys: %w", err)
	}
	return keys, nil
}

func setAuditRLSTx(ctx context.Context, tx *sql.Tx, chainKey string) error {
	if chainKey == SystemChainKey {
		if err := port.SetOperatorRLSTx(ctx, tx, port.DialectPostgres); err != nil {
			return fmt.Errorf("audit: set operator RLS context: %w", err)
		}
		return nil
	}
	if err := port.SetOrgRLSTx(ctx, tx, port.DialectPostgres, chainKey); err != nil {
		return fmt.Errorf("audit: set org RLS context: %w", err)
	}
	return nil
}

func chainHasEventsPG(ctx context.Context, db *sql.DB, chainKey string) (bool, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, fmt.Errorf("audit: begin chain key probe tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := setAuditRLSTx(ctx, tx, chainKey); err != nil {
		return false, err
	}
	var one int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM audit_events WHERE chain_key = $1 LIMIT 1`, chainKey).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("audit: probe chain %q: %w", chainKey, err)
	}
	return true, nil
}

func verifySingleChain(ctx context.Context, db *sql.DB, chainKey string, onTamper func(TamperFinding)) (ChainReport, error) {
	dialect := port.DetectDialect(ctx, db)
	var (
		rows *sql.Rows
		tx   *sql.Tx
		err  error
	)
	query := port.Rebind(dialect, `
		SELECT id, created_at_ms, org_id, chain_key, seq_no,
		       actor_kind, actor_id, entry_point, event_type, target_type, target_id,
		       request_id, metadata_json, prev_hash, entry_hash
		FROM audit_events
		WHERE chain_key = ?
		ORDER BY seq_no ASC`)
	if dialect == port.DialectPostgres {
		tx, err = db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return ChainReport{}, fmt.Errorf("audit: begin verify tx for %q: %w", chainKey, err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := setAuditRLSTx(ctx, tx, chainKey); err != nil {
			return ChainReport{}, err
		}
		rows, err = tx.QueryContext(ctx, query, chainKey)
	} else {
		rows, err = db.QueryContext(ctx, query, chainKey)
	}
	if err != nil {
		return ChainReport{}, fmt.Errorf("audit: query chain %q: %w", chainKey, err)
	}
	defer rows.Close()
	return verifyChainRows(rows, chainKey, onTamper)
}

func verifyChainRows(rows *sql.Rows, chainKey string, onTamper func(TamperFinding)) (ChainReport, error) {

	cr := ChainReport{
		ChainKey: chainKey,
		Valid:    true,
	}

	expectedSeq := int64(1)
	expectedPrev := GenesisHash(chainKey)
	chainBroken := false

	emit := func(f TamperFinding) {
		f.AlertCode = AlertAuditTamper
		cr.Valid = false
		cr.Findings = append(cr.Findings, f)
		if onTamper != nil {
			onTamper(f)
		}
	}

	for rows.Next() {
		ev, serr := scanStoredEvent(rows)
		if serr != nil {
			return ChainReport{}, serr
		}

		// Verify chain_key invariant matches org_id partitioning (NULL -> SYSTEM, non-NULL -> org_id).
		expectedChainKey := SystemChainKey
		invalidOrg := false
		if ev.OrgID != nil {
			if *ev.OrgID == "" {
				invalidOrg = true
			} else {
				expectedChainKey = *ev.OrgID
			}
		}
		if invalidOrg || ev.ChainKey != expectedChainKey || ev.ChainKey != chainKey {
			if !chainBroken {
				emit(TamperFinding{
					ChainKey: chainKey,
					SeqNo:    ev.SeqNo,
					EventID:  ev.ID,
					Kind:     TamperMalformedRecord,
					Detail:   fmt.Sprintf("chain_key/org_id mismatch at chain %q seq_no=%d (chain_key=%q, expected=%q)", chainKey, ev.SeqNo, ev.ChainKey, expectedChainKey),
				})
				chainBroken = true
			}
		}

		// Pinpoint missing sequence numbers (e.g., direct SQL DELETE of seq_no = 250).
		if ev.SeqNo != expectedSeq {
			if !chainBroken {
				emit(TamperFinding{
					ChainKey: chainKey,
					SeqNo:    expectedSeq,
					Kind:     TamperMissingSequence,
					Detail:   fmt.Sprintf("gap in audit chain %q: expected seq_no=%d before observed seq_no=%d", chainKey, expectedSeq, ev.SeqNo),
				})
				chainBroken = true
			}
		}

		// Check prev_hash anchor/link continuity.
		if !chainBroken && ev.PrevHash != expectedPrev {
			kind := TamperPrevHashMismatch
			if ev.SeqNo == 1 {
				kind = TamperGenesisMismatch
			}
			emit(TamperFinding{
				ChainKey:     chainKey,
				SeqNo:        ev.SeqNo,
				EventID:      ev.ID,
				Kind:         kind,
				Detail:       fmt.Sprintf("prev_hash mismatch at chain %q seq_no=%d", chainKey, ev.SeqNo),
				ExpectedHash: expectedPrev,
				ActualHash:   ev.PrevHash,
			})
			chainBroken = true
		}

		// Validate metadata JSON + recompute JCS canonical envelope digest.
		parsedMeta, jerr := decodeStrictJSONValue([]byte(ev.MetadataJSON))
		if jerr != nil {
			if !chainBroken {
				emit(TamperFinding{
					ChainKey: chainKey,
					SeqNo:    ev.SeqNo,
					EventID:  ev.ID,
					Kind:     TamperMalformedRecord,
					Detail:   fmt.Sprintf("invalid metadata_json at chain %q seq_no=%d: %v", chainKey, ev.SeqNo, jerr),
				})
				chainBroken = true
			}
			expectedSeq = ev.SeqNo + 1
			expectedPrev = ev.EntryHash
			continue
		}

		portEv := port.AuditEvent{
			ID:           ev.ID,
			CreatedAtMs:  ev.CreatedAtMs,
			OrgID:        ev.OrgID,
			ActorKind:    ev.ActorKind,
			ActorID:      ev.ActorID,
			EntryPoint:   ev.EntryPoint,
			EventType:    ev.EventType,
			TargetType:   ev.TargetType,
			TargetID:     ev.TargetID,
			RequestID:    ev.RequestID,
			MetadataJSON: ev.MetadataJSON,
		}
		envelopeBytes, cerr := CanonicalEnvelopeBytes(portEv, parsedMeta)
		if cerr != nil {
			if !chainBroken {
				emit(TamperFinding{
					ChainKey: chainKey,
					SeqNo:    ev.SeqNo,
					EventID:  ev.ID,
					Kind:     TamperMalformedRecord,
					Detail:   fmt.Sprintf("canonicalization failure at chain %q seq_no=%d: %v", chainKey, ev.SeqNo, cerr),
				})
				chainBroken = true
			}
			expectedSeq = ev.SeqNo + 1
			expectedPrev = ev.EntryHash
			continue
		}

		computedEntry := ComputeEntryHash(ev.ChainKey, ev.SeqNo, ev.PrevHash, envelopeBytes)
		if computedEntry != ev.EntryHash {
			if !chainBroken {
				emit(TamperFinding{
					ChainKey:     chainKey,
					SeqNo:        ev.SeqNo,
					EventID:      ev.ID,
					Kind:         TamperEntryHashMismatch,
					Detail:       fmt.Sprintf("entry_hash mismatch at chain %q seq_no=%d", chainKey, ev.SeqNo),
					ExpectedHash: computedEntry,
					ActualHash:   ev.EntryHash,
				})
				chainBroken = true
			}
		} else if !chainBroken {
			cr.VerifiedCount++
		}

		cr.LastSeqNo = ev.SeqNo
		cr.LastHash = ev.EntryHash
		expectedSeq = ev.SeqNo + 1
		expectedPrev = ev.EntryHash
	}

	if err := rows.Err(); err != nil {
		return ChainReport{}, fmt.Errorf("audit: iterate chain %q: %w", chainKey, err)
	}
	return cr, nil
}
