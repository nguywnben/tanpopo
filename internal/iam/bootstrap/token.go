// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package bootstrap manages first-run single-use Bootstrap Token minting (0600 file),
// atomic ownership provisioning (OPERATOR + ORG_OWNER, default Org/Project/Dev Env),
// and permanent sealing so subsequent attempts fail with 410 bootstrap_already_completed
// (AUTH-02 / TP-IMP-0009).
package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Bootstrap Token prefix & minimum raw length (matches openapi/admin-v1.yaml minLength: 32).
const (
	TokenPrefix          = "tp_boot_"
	MinBootstrapTokenLen = 32

	maxBootstrapTokenFailures  = 10
	bootstrapRateWindowMs      = int64(5 * 60 * 1000)
)

// Canonical bootstrap error sentinels.
var (
	ErrBootstrapAlreadyCompleted = errors.New("bootstrap: bootstrap_already_completed")
	ErrInvalidBootstrapToken     = errors.New("bootstrap: invalid_bootstrap_token")
	ErrBootstrapRateLimited      = errors.New("bootstrap: auth_rate_limited")
	ErrInsecureTokenPermissions  = errors.New("bootstrap: insecure token file permissions or symlink")
	ErrMasterKEKAckRequired      = errors.New("bootstrap: master_kek_backup_acknowledged must be true")
)

// CompleteRequest captures the first-run bootstrap ownership payload (openapi/admin-v1.yaml).
type CompleteRequest struct {
	BootstrapToken               string
	OwnerEmail                   string
	OwnerPassword                string
	OrgName                      string
	ProjectName                  string
	MasterKEKBackupAcknowledged  bool
}

// CompleteResult returns the atomically provisioned tenant/owner IDs.
type CompleteResult struct {
	OrgID            string
	ProjectID        string
	EnvID            string
	OwnerPrincipalID string
}

// ManagerConfig configures the one-time Bootstrap Token manager.
type ManagerConfig struct {
	DB        *sql.DB
	TokenPath string
	NowMs     func() int64
	Argon2    auth.Argon2Params
	AuditSink port.AuditSink
}

// Manager mints, persists to a 0600 file, verifies, and permanently burns the Bootstrap Token.
type Manager struct {
	db        *sql.DB
	tokenPath string
	nowMs     func() int64
	argon2    auth.Argon2Params
	auditSink port.AuditSink

	mu            sync.Mutex
	hasDigest     bool
	digest        [32]byte
	burned        bool
	failWindowMs  int64
	failCount     int
	blockedUntilMs int64
}

// NewManager validates configuration and constructs a Bootstrap Manager.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	trimmedPath := strings.TrimSpace(cfg.TokenPath)
	if trimmedPath == "" {
		return nil, fmt.Errorf("%w: non-empty TokenPath is required", port.ErrInvalidInput)
	}
	absPath, err := filepath.Abs(trimmedPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: resolve token path: %w", err)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	a2 := cfg.Argon2
	if a2 == (auth.Argon2Params{}) {
		a2 = auth.DefaultArgon2Params()
	}
	if err := auth.ValidateArgon2Params(a2); err != nil {
		return nil, err
	}

	return &Manager{
		db:        cfg.DB,
		tokenPath: absPath,
		nowMs:     nowFn,
		argon2:    a2,
		auditSink: cfg.AuditSink,
	}, nil
}

// IsBootstrapped reports whether initial instance ownership has already been claimed.
func (m *Manager) IsBootstrapped(ctx context.Context) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("%w: nil bootstrap Manager", port.ErrInvalidInput)
	}
	return isBootstrappedSQL(ctx, m.db)
}

func isBootstrappedSQL(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (bool, error) {
	var exists int
	err := q.QueryRowContext(ctx, `
		SELECT 1 FROM role_bindings
		WHERE role_name IN ('OPERATOR', 'ORG_OWNER')
		LIMIT 1`).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		var pExists int
		errP := q.QueryRowContext(ctx, `SELECT 1 FROM principals LIMIT 1`).Scan(&pExists)
		if errors.Is(errP, sql.ErrNoRows) {
			return false, nil
		}
		if errP != nil {
			return false, errP
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// EnsureToken mints a one-time 0600 Bootstrap Token file when the DB is unbootstrapped.
// If the database is already bootstrapped, any leftover token file is deleted and
// ErrBootstrapAlreadyCompleted is returned.
func (m *Manager) EnsureToken(ctx context.Context) (string, error) {
	if m == nil {
		return "", fmt.Errorf("%w: nil bootstrap Manager", port.ErrInvalidInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	booted, err := isBootstrappedSQL(ctx, m.db)
	if err != nil {
		return "", err
	}
	if booted || m.burned {
		m.burnLocked()
		return "", ErrBootstrapAlreadyCompleted
	}

	dir := filepath.Dir(m.tokenPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("bootstrap: create token directory: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", fmt.Errorf("bootstrap: chmod token directory: %w", err)
		}
	}

	if err := verifyNotSymlink(m.tokenPath); err != nil {
		return "", err
	}

	if existing, err := m.loadExistingTokenLocked(); err == nil {
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", fmt.Errorf("bootstrap: rand read: %w", err)
	}
	defer clear(rawBytes)
	token := TokenPrefix + base64.RawURLEncoding.EncodeToString(rawBytes)

	// Use O_EXCL to eliminate symlink/file-swap TOCTOU when minting a fresh token file.
	f, err := os.OpenFile(m.tokenPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return m.loadExistingTokenLocked()
		}
		return "", fmt.Errorf("bootstrap: open token file: %w", err)
	}
	if runtime.GOOS != "windows" {
		if chmodErr := f.Chmod(0o600); chmodErr != nil {
			_ = f.Close()
			_ = os.Remove(m.tokenPath)
			return "", fmt.Errorf("bootstrap: chmod 0600 token file: %w", chmodErr)
		}
	}
	if _, writeErr := f.WriteString(token + "\n"); writeErr != nil {
		_ = f.Close()
		_ = os.Remove(m.tokenPath)
		return "", fmt.Errorf("bootstrap: write token file: %w", writeErr)
	}
	if syncErr := f.Sync(); syncErr != nil {
		_ = f.Close()
		_ = os.Remove(m.tokenPath)
		return "", fmt.Errorf("bootstrap: sync token file: %w", syncErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		_ = os.Remove(m.tokenPath)
		return "", fmt.Errorf("bootstrap: close token file: %w", closeErr)
	}

	m.digest = sha256.Sum256([]byte(token))
	m.hasDigest = true
	return token, nil
}

func (m *Manager) loadExistingTokenLocked() (string, error) {
	info, err := os.Lstat(m.tokenPath)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", ErrInsecureTokenPermissions
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%w: mode %o exposes token beyond owner", ErrInsecureTokenPermissions, info.Mode().Perm())
	}
	raw, err := os.ReadFile(m.tokenPath)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	clear(raw)
	if len(token) < MinBootstrapTokenLen {
		return "", ErrInvalidBootstrapToken
	}
	m.digest = sha256.Sum256([]byte(token))
	m.hasDigest = true
	return token, nil
}

// Complete verifies the one-time Bootstrap Token in constant time, atomically creates the
// default Organization, Project, Dev Environment, Owner principal, and Personal Admin
// role bindings (OPERATOR + ORG_OWNER), and permanently burns the Bootstrap Token.
// Any replay or concurrent loser receives ErrBootstrapAlreadyCompleted (410).
func (m *Manager) Complete(ctx context.Context, req CompleteRequest) (*CompleteResult, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: nil bootstrap Manager", port.ErrInvalidInput)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.burned {
		m.burnLocked()
		return nil, ErrBootstrapAlreadyCompleted
	}

	booted, err := isBootstrappedSQL(ctx, m.db)
	if err != nil {
		return nil, err
	}
	if booted {
		m.burnLocked()
		return nil, ErrBootstrapAlreadyCompleted
	}

	now := m.nowMs()
	if m.blockedUntilMs > now {
		m.emitAudit(ctx, now, "SYSTEM", "bootstrap", "bootstrap.token_rate_limited", "instance", "bootstrap", `{"reason":"brute_force_throttled"}`)
		return nil, ErrBootstrapRateLimited
	}

	if !m.hasDigest {
		if _, loadErr := m.loadExistingTokenLocked(); loadErr != nil {
			m.recordFailedTokenLocked(ctx, now)
			return nil, ErrInvalidBootstrapToken
		}
	}

	presented := strings.TrimSpace(req.BootstrapToken)
	if len(presented) < MinBootstrapTokenLen {
		m.recordFailedTokenLocked(ctx, now)
		return nil, ErrInvalidBootstrapToken
	}
	presentedDigest := sha256.Sum256([]byte(presented))
	if subtle.ConstantTimeCompare(presentedDigest[:], m.digest[:]) != 1 {
		m.recordFailedTokenLocked(ctx, now)
		return nil, ErrInvalidBootstrapToken
	}

	email := strings.ToLower(strings.TrimSpace(req.OwnerEmail))
	if email == "" || !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
		return nil, fmt.Errorf("%w: valid owner_email is required", port.ErrInvalidInput)
	}
	if len(req.OwnerPassword) < auth.MinOwnerPasswordLength || strings.ContainsRune(req.OwnerPassword, '\x00') {
		return nil, fmt.Errorf("%w: owner_password must be at least %d characters", port.ErrInvalidInput, auth.MinOwnerPasswordLength)
	}
	if !req.MasterKEKBackupAcknowledged {
		return nil, ErrMasterKEKAckRequired
	}

	orgName := strings.TrimSpace(req.OrgName)
	if orgName == "" {
		orgName = "Default Organization"
	}
	projName := strings.TrimSpace(req.ProjectName)
	if projName == "" {
		projName = "default"
	}

	pwHash, err := auth.HashPasswordWithParams(req.OwnerPassword, m.argon2)
	if err != nil {
		return nil, err
	}

	orgID, err := idgen.New(idgen.PrefixOrg)
	if err != nil {
		return nil, err
	}
	projID, err := idgen.New(idgen.PrefixProject)
	if err != nil {
		return nil, err
	}
	envID, err := idgen.New(idgen.PrefixEnv)
	if err != nil {
		return nil, err
	}
	principalID, err := idgen.New(idgen.PrefixPrincipal)
	if err != nil {
		return nil, err
	}
	rbOpRaw, err := idgen.New(idgen.PrefixPrincipal)
	if err != nil {
		return nil, err
	}
	rbOwnerRaw, err := idgen.New(idgen.PrefixPrincipal)
	if err != nil {
		return nil, err
	}
	rbOpID := "rbd_" + rbOpRaw[len(idgen.PrefixPrincipal)+1:]
	rbOwnerID := "rbd_" + rbOwnerRaw[len(idgen.PrefixPrincipal)+1:]

	orgSlug := slugify(orgName, "default-org")
	projSlug := slugify(projName, "default")

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		if bErr, checkErr := isBootstrappedSQL(ctx, m.db); checkErr == nil && bErr {
			m.burnLocked()
			return nil, ErrBootstrapAlreadyCompleted
		}
		return nil, fmt.Errorf("bootstrap: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Re-verify inside serialized write transaction to guarantee single-winner atomicity.
	alreadyBooted, err := isBootstrappedSQL(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: check bootstrapped inside tx: %w", err)
	}
	if alreadyBooted {
		m.burnLocked()
		return nil, ErrBootstrapAlreadyCompleted
	}

	mapRaceOrErr := func(execErr error) error {
		_ = tx.Rollback()
		if b, checkErr := isBootstrappedSQL(ctx, m.db); checkErr == nil && b {
			m.burnLocked()
			return ErrBootstrapAlreadyCompleted
		}
		return fmt.Errorf("bootstrap: write tx: %w", execErr)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO organizations (
			id, slug, display_name, status, default_data_region,
			min_plugin_trust, require_four_eyes_approval, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, 'ACTIVE', NULL, 'ANY_APPROVED', 0, 1, ?, ?)`,
		orgID, orgSlug, orgName, now, now,
	); err != nil {
		return nil, mapRaceOrErr(err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO projects (
			id, org_id, slug, display_name, status, enforced_region, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, 'ACTIVE', NULL, 1, ?, ?)`,
		projID, orgID, projSlug, projName, now, now,
	); err != nil {
		return nil, mapRaceOrErr(err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO environments (
			id, org_id, project_id, slug, require_tested_caps, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, 'dev', 0, 1, ?, ?)`,
		envID, orgID, projID, now, now,
	); err != nil {
		return nil, mapRaceOrErr(err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO principals (
			id, org_id, kind, email_or_sub, password_argon2id, oidc_issuer,
			status, version, last_login_at_ms, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, NULL, 'ACTIVE', 1, NULL, ?, ?)`,
		principalID, orgID, auth.PrincipalKindHumanLocal, email, pwHash, now, now,
	); err != nil {
		return nil, mapRaceOrErr(err)
	}

	// Personal Admin mode grants both instance OPERATOR and organization ORG_OWNER bindings (ACT-01).
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES (?, NULL, ?, 'INSTANCE', 'instance', ?, 1, ?)`,
		rbOpID, principalID, rbac.RoleOperator, now,
	); err != nil {
		return nil, mapRaceOrErr(err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES (?, ?, ?, 'ORG', ?, ?, 1, ?)`,
		rbOwnerID, orgID, principalID, orgID, rbac.RoleOrgOwner, now,
	); err != nil {
		return nil, mapRaceOrErr(err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'organization', ?, 'UPSERT', 0, ?)`,
		orgID, orgID, now,
	); err != nil {
		return nil, mapRaceOrErr(err)
	}

	if err := tx.Commit(); err != nil {
		return nil, mapRaceOrErr(err)
	}

	m.burnLocked()

	if m.auditSink != nil {
		if audID, idErr := idgen.New(idgen.PrefixAudit); idErr == nil {
			_ = m.auditSink.Append(ctx, port.AuditEvent{
				ID:           audID,
				CreatedAtMs:  now,
				OrgID:        nil,
				ActorKind:    "SYSTEM",
				ActorID:      principalID,
				EntryPoint:   "bootstrap",
				EventType:    "instance.bootstrap_completed",
				TargetType:   "organization",
				TargetID:     orgID,
				MetadataJSON: `{"mode":"personal_admin_operator_org_owner"}`,
			})
		}
	}

	return &CompleteResult{
		OrgID:            orgID,
		ProjectID:        projID,
		EnvID:            envID,
		OwnerPrincipalID: principalID,
	}, nil
}

func (m *Manager) recordFailedTokenLocked(ctx context.Context, now int64) {
	if m.blockedUntilMs > 0 && now >= m.blockedUntilMs {
		m.failWindowMs = now
		m.failCount = 0
		m.blockedUntilMs = 0
	} else if m.failWindowMs == 0 || now-m.failWindowMs >= bootstrapRateWindowMs {
		m.failWindowMs = now
		m.failCount = 0
	}
	m.failCount++
	if m.failCount >= maxBootstrapTokenFailures {
		m.blockedUntilMs = now + bootstrapRateWindowMs
	}
	m.emitAudit(ctx, now, "SYSTEM", "bootstrap", "bootstrap.token_rejected", "instance", "bootstrap", `{}`)
}

func (m *Manager) emitAudit(ctx context.Context, now int64, actorKind, actorID, eventType, targetType, targetID, meta string) {
	if m.auditSink == nil {
		return
	}
	audID, idErr := idgen.New(idgen.PrefixAudit)
	if idErr != nil {
		return
	}
	_ = m.auditSink.Append(ctx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        nil,
		ActorKind:    actorKind,
		ActorID:      actorID,
		EntryPoint:   "bootstrap",
		EventType:    eventType,
		TargetType:   targetType,
		TargetID:     targetID,
		MetadataJSON: meta,
	})
}

func (m *Manager) burnLocked() {
	clear(m.digest[:])
	m.hasDigest = false
	m.burned = true

	if m.tokenPath == "" {
		return
	}
	if info, err := os.Lstat(m.tokenPath); err == nil && info.Mode()&os.ModeSymlink == 0 && !info.IsDir() {
		if f, oerr := os.OpenFile(m.tokenPath, os.O_WRONLY, 0o600); oerr == nil {
			zeros := make([]byte, info.Size())
			_, _ = f.Write(zeros)
			_ = f.Sync()
			_ = f.Close()
		}
	}
	_ = os.Remove(m.tokenPath)
}

func verifyNotSymlink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrInsecureTokenPermissions
	}
	return nil
}

func slugify(raw, fallback string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || r == ' ':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
		if b.Len() >= 48 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return fallback
	}
	return out
}
