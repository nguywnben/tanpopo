// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package auth

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
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// DefaultEnrollmentTTL sets the lifetime of single-use local member enrollment tokens.
	DefaultEnrollmentTTL = 24 * time.Hour

	// MinLocalPasswordLength enforces minimum credential entropy on local password enrollment.
	MinLocalPasswordLength = 12
)

var (
	// ErrEnrollmentTokenInvalid indicates the enrollment token is missing, malformed, expired, or already used.
	ErrEnrollmentTokenInvalid = errors.New("auth: enrollment_token_invalid")

	// ErrWeakPassword indicates a supplied enrollment password does not satisfy minimum strength requirements.
	ErrWeakPassword = errors.New("auth: password must be at least 12 characters")
)

// CreateEnrollmentTokenTx invalidates prior unused enrollment tokens for principalID,
// persists a SHA-256 hash of a newly minted one-time token inside tx, and returns the raw
// token alongside its millisecond expiration timestamp.
func (s *LocalService) CreateEnrollmentTokenTx(
	ctx context.Context,
	tx *sql.Tx,
	orgID, principalID, issuedBy string,
	ttl time.Duration,
	nowMs int64,
) (string, int64, error) {
	if s == nil || tx == nil {
		return "", 0, fmt.Errorf("%w: non-nil auth service and tx required", port.ErrInvalidInput)
	}
	normOrg := strings.TrimSpace(orgID)
	normPrinc := strings.TrimSpace(principalID)
	normIssuer := strings.TrimSpace(issuedBy)
	if normOrg == "" || normPrinc == "" || normIssuer == "" {
		return "", 0, fmt.Errorf("%w: org_id, principal_id, and issued_by are required", port.ErrInvalidInput)
	}
	if ttl <= 0 {
		ttl = DefaultEnrollmentTTL
	}
	if nowMs <= 0 {
		nowMs = s.nowMs()
	}

	if _, err := tx.ExecContext(ctx, port.Rebind(s.dialect, `
		UPDATE member_enrollment_tokens
		SET used_at_ms = ?
		WHERE org_id = ? AND principal_id = ? AND used_at_ms IS NULL`),
		nowMs, normOrg, normPrinc,
	); err != nil {
		return "", 0, fmt.Errorf("auth: expire prior enrollment tokens: %w", err)
	}

	tokRand, err := randomURLToken(16)
	if err != nil {
		return "", 0, err
	}
	tokID := "enr_" + tokRand
	randPart, err := randomURLToken(32)
	if err != nil {
		return "", 0, err
	}
	rawToken := "tpen_" + randPart
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])
	expiresAtMs := nowMs + ttl.Milliseconds()

	if _, err := tx.ExecContext(ctx, port.Rebind(s.dialect, `
		INSERT INTO member_enrollment_tokens (
			id, org_id, principal_id, token_hash, issued_by, expires_at_ms, used_at_ms, created_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, NULL, ?)`),
		tokID, normOrg, normPrinc, tokenHash, normIssuer, expiresAtMs, nowMs,
	); err != nil {
		return "", 0, fmt.Errorf("auth: store member enrollment token: %w", err)
	}
	return rawToken, expiresAtMs, nil
}

// EnrollWithToken consumes a single-use enrollment token, stores a freshly derived Argon2id
// password on the target HUMAN_LOCAL principal, invalidates older sessions, records a canonical
// member.enrolled audit event, and mints a fresh authenticated Admin session.
func (s *LocalService) EnrollWithToken(
	ctx context.Context,
	clientIP, orgID, rawToken, newPassword, reqID string,
) (*SessionInfo, *http.Cookie, error) {
	if s == nil {
		return nil, nil, ErrUnauthenticated
	}
	normIP := strings.TrimSpace(clientIP)
	if normIP == "" {
		normIP = "unknown"
	}
	now := s.nowMs()
	ipKey := "ip:" + normIP
	enrollKey := "enroll:" + normIP
	checks := []rateBucketCheck{
		{key: ipKey, limit: s.rateCfg.MaxFailures},
		{key: enrollKey, limit: s.rateCfg.MaxFailures},
	}
	if retryAfter, limited := s.checkRateLimits(checks, now); limited {
		s.emitAudit(ctx, nil, "SYSTEM", normIP, "auth.enroll_rate_limited", "principal", "enrollment", `{"reason":"brute_force_throttled"}`)
		return nil, nil, &RateLimitError{RetryAfterSeconds: retryAfter}
	}

	token := strings.TrimSpace(rawToken)
	if token == "" || !strings.HasPrefix(token, "tpen_") || len(token) < 20 {
		if retryAfter, saturated := s.recordFailure(ctx, checks, normIP, "enrollment", now); saturated {
			return nil, nil, &RateLimitError{RetryAfterSeconds: retryAfter}
		}
		return nil, nil, ErrEnrollmentTokenInvalid
	}
	if len(newPassword) < MinLocalPasswordLength {
		return nil, nil, ErrWeakPassword
	}

	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])

	tokOrgID, tokID, principalID, err := s.lookupValidEnrollmentToken(ctx, strings.TrimSpace(orgID), tokenHash, now)
	if err != nil {
		if errors.Is(err, ErrEnrollmentTokenInvalid) {
			if retryAfter, saturated := s.recordFailure(ctx, checks, normIP, "enrollment", now); saturated {
				return nil, nil, &RateLimitError{RetryAfterSeconds: retryAfter}
			}
		}
		return nil, nil, err
	}

	pwHash, err := HashPasswordWithParams(newPassword, s.argon2)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: derive enrollment password hash: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: begin enrollment tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, s.dialect, tokOrgID); err != nil {
		return nil, nil, err
	}

	resTok, err := tx.ExecContext(ctx, port.Rebind(s.dialect, `
		UPDATE member_enrollment_tokens
		SET used_at_ms = ?
		WHERE id = ? AND org_id = ? AND used_at_ms IS NULL AND expires_at_ms > ?`),
		now, tokID, tokOrgID, now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: consume enrollment token: %w", err)
	}
	if affected, _ := resTok.RowsAffected(); affected != 1 {
		return nil, nil, ErrEnrollmentTokenInvalid
	}

	var (
		pKind   string
		pEmail  string
		pStatus string
		pVer    int64
	)
	if err := tx.QueryRowContext(ctx, port.Rebind(s.dialect, `
		SELECT kind, email_or_sub, status, version
		FROM principals
		WHERE id = ? AND org_id = ?`),
		principalID, tokOrgID,
	).Scan(&pKind, &pEmail, &pStatus, &pVer); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrEnrollmentTokenInvalid
		}
		return nil, nil, fmt.Errorf("auth: load enrolled principal: %w", err)
	}
	if pKind != PrincipalKindHumanLocal {
		return nil, nil, ErrEnrollmentTokenInvalid
	}
	if pStatus != PrincipalStatusActive {
		return nil, nil, ErrPrincipalInactive
	}

	resUpd, err := tx.ExecContext(ctx, port.Rebind(s.dialect, `
		UPDATE principals
		SET password_argon2id = ?, version = version + 1, last_login_at_ms = ?, updated_at_ms = ?
		WHERE id = ? AND org_id = ? AND version = ?`),
		pwHash, now, now, principalID, tokOrgID, pVer,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: persist enrolled password: %w", err)
	}
	if affected, _ := resUpd.RowsAffected(); affected != 1 {
		return nil, nil, ErrEnrollmentTokenInvalid
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return nil, nil, err
	}
	metaJSON, _ := json.Marshal(map[string]any{
		"enrollment_token_id": tokID,
		"email":               pEmail,
	})
	var reqPtr *string
	if strings.TrimSpace(reqID) != "" {
		v := strings.TrimSpace(reqID)
		reqPtr = &v
	}
	if err := s.auditSink.AppendTx(ctx, tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &tokOrgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      principalID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "member.enrolled",
		TargetType:   "principal",
		TargetID:     principalID,
		RequestID:    reqPtr,
		MetadataJSON: string(metaJSON),
	}); err != nil {
		return nil, nil, fmt.Errorf("auth: append enrollment audit event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("auth: commit enrollment tx: %w", err)
	}

	s.RevokePrincipalSessions(principalID)
	s.clearFailures([]string{enrollKey})

	bindings, roles, err := s.loadBindings(ctx, principalID, &tokOrgID)
	if err != nil {
		return nil, nil, err
	}
	return s.mintSessionForPrincipal(
		ctx,
		principalID,
		pVer+1,
		pEmail,
		&tokOrgID,
		rbac.CredentialPlaneAdminSession,
		roles,
		bindings,
		"auth.enrollment_login_succeeded",
		now,
	)
}

func (s *LocalService) lookupValidEnrollmentToken(ctx context.Context, orgHint, tokenHash string, nowMs int64) (string, string, string, error) {
	if s.dialect != port.DialectPostgres {
		var tokOrg, tokID, princID string
		var expiresAtMs int64
		var usedAtMs sql.NullInt64
		query := `
			SELECT id, org_id, principal_id, expires_at_ms, used_at_ms
			FROM member_enrollment_tokens
			WHERE token_hash = ?`
		args := []any{tokenHash}
		if orgHint != "" {
			query += ` AND org_id = ?`
			args = append(args, orgHint)
		}
		err := s.db.QueryRowContext(ctx, query, args...).Scan(&tokID, &tokOrg, &princID, &expiresAtMs, &usedAtMs)
		if errors.Is(err, sql.ErrNoRows) || usedAtMs.Valid || expiresAtMs <= nowMs {
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return "", "", "", fmt.Errorf("auth: lookup enrollment token: %w", err)
			}
			return "", "", "", ErrEnrollmentTokenInvalid
		}
		return tokOrg, tokID, princID, nil
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", "", "", fmt.Errorf("auth: begin enrollment lookup tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	orgCandidates := []string{}
	if orgHint != "" {
		orgCandidates = append(orgCandidates, orgHint)
	} else {
		orgIDs, oerr := port.ListOrgIDsTx(ctx, tx, s.dialect)
		if oerr != nil {
			return "", "", "", oerr
		}
		orgCandidates = orgIDs
	}

	for _, oid := range orgCandidates {
		if err := port.SetOrgRLSTx(ctx, tx, s.dialect, oid); err != nil {
			return "", "", "", err
		}
		var tokID, princID string
		var expiresAtMs int64
		var usedAtMs sql.NullInt64
		qerr := tx.QueryRowContext(ctx, port.Rebind(s.dialect, `
			SELECT id, principal_id, expires_at_ms, used_at_ms
			FROM member_enrollment_tokens
			WHERE org_id = ? AND token_hash = ?`),
			oid, tokenHash,
		).Scan(&tokID, &princID, &expiresAtMs, &usedAtMs)
		if errors.Is(qerr, sql.ErrNoRows) {
			continue
		}
		if qerr != nil {
			return "", "", "", fmt.Errorf("auth: lookup PG enrollment token: %w", qerr)
		}
		if usedAtMs.Valid || expiresAtMs <= nowMs {
			return "", "", "", ErrEnrollmentTokenInvalid
		}
		return oid, tokID, princID, nil
	}
	return "", "", "", ErrEnrollmentTokenInvalid
}
