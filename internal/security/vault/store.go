// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Supported secret_kind constants matching sqlite_schema.sql and postgres_schema.sql CHECK constraints.
const (
	SecretKindAPIKey           = "API_KEY"
	SecretKindOAuth2TokenSet   = "OAUTH2_TOKEN_SET"
	SecretKindHMACSecret       = "HMAC_SECRET"
	SecretKindAWSSigV4Key      = "AWS_SIGV4_KEY"
	SecretKindMTLSClientCert   = "MTLS_CLIENT_CERT"
	SecretKindWebhookHMAC      = "WEBHOOK_HMAC_SECRET"
	SecretKindOCIRegistryAuth  = "OCI_REGISTRY_AUTH"
)

func isValidSecretKind(kind string) bool {
	switch kind {
	case SecretKindAPIKey,
		SecretKindOAuth2TokenSet,
		SecretKindHMACSecret,
		SecretKindAWSSigV4Key,
		SecretKindMTLSClientCert,
		SecretKindWebhookHMAC,
		SecretKindOCIRegistryAuth:
		return true
	default:
		return false
	}
}

// UnsealCapability is an unexported-field capability token authorizing UnsealWithCapability.
// A zero-value UnsealCapability{} fails closed inside UnsealWithCapability (NEG-2).
type UnsealCapability struct {
	granted bool
}

// MintUnsealCapabilityForBroker mints the sole production UnsealCapability intended exclusively
// for cmd/tanpopo wiring into BlindHostCredentialBroker. Production calls outside cmd/tanpopo
// are rejected by scripts/ci/check.go AST verification.
func MintUnsealCapabilityForBroker() UnsealCapability {
	return UnsealCapability{granted: true}
}

// SecretMetadata is the Control-Plane DTO returned on create, rotate, get, and list calls.
// It contains zero plaintext or ciphertext secret bytes (SPEC-SEC-03).
type SecretMetadata struct {
	ID             string  `json:"id"`
	OrgID          string  `json:"org_id"`
	OwnerProjectID *string `json:"owner_project_id,omitempty"`
	Name           string  `json:"name"`
	SecretKind     string  `json:"secret_kind"`
	BoundAudience  *string `json:"bound_audience,omitempty"`
	KeyFingerprint string  `json:"key_fingerprint"`
	Version        int64   `json:"version"`
	CreatedAtMs    int64   `json:"created_at_ms"`
	UpdatedAtMs    int64   `json:"updated_at_ms"`
}

// UnsealedSecret carries decrypted secret material exclusively to capability-authorized callers.
// Callers must invoke Zeroize() immediately when outbound header construction finishes.
type UnsealedSecret struct {
	ID             string
	OrgID          string
	OwnerProjectID *string
	SecretKind     string
	BoundAudience  *string
	Version        int64
	Plaintext      []byte
}

// Zeroize overwrites the unsealed plaintext buffer in RAM.
func (u *UnsealedSecret) Zeroize() {
	if u != nil && u.Plaintext != nil {
		clear(u.Plaintext)
		u.Plaintext = nil
	}
}

// SealSecretParams specifies parameters for sealing a new write-only secret.
type SealSecretParams struct {
	ID             string
	OrgID          string
	OwnerProjectID *string
	Name           string
	SecretKind     string
	BoundAudience  *string
	Plaintext      []byte
}

// RotateSecretParams specifies parameters for replacing plaintext under an optimistic lock.
type RotateSecretParams struct {
	Plaintext          []byte
	BoundAudience      *string
	ClearBoundAudience bool
}

// RewrapSummary reports how many organization DEKs were re-wrapped during Master KEK rotation.
type RewrapSummary struct {
	OldKEKID    string
	NewKEKID    string
	Rewrapped   int
	ConfigEpoch int64
}

// ShredRequest carries the idempotency token and monotonic DEK generation fence required
// to prevent late retries of completed shred operations from deleting newly recreated DEKs.
type ShredRequest struct {
	RequestID          string
	ExpectedGeneration int64
}

// OrgKeyMetadata exposes write-only metadata (zero key bytes) for an organization DEK,
// including its monotonic KeyGeneration used as a fencing token in ShredOrgDEK.
type OrgKeyMetadata struct {
	OrgID         string `json:"org_id"`
	KEKKeyID      string `json:"kek_key_id"`
	DEKVersion    int64  `json:"dek_version"`
	KeyGeneration int64  `json:"key_generation"`
	CreatedAtMs   int64  `json:"created_at_ms"`
	RotatedAtMs   int64  `json:"rotated_at_ms"`
}

// ProjectKeyMetadata exposes write-only metadata (zero key bytes) for a project DEK,
// including its monotonic KeyGeneration used as a fencing token in ShredProjectDEK.
type ProjectKeyMetadata struct {
	OrgID         string `json:"org_id"`
	ProjectID     string `json:"project_id"`
	OrgDEKVersion int64  `json:"org_dek_version"`
	DEKVersion    int64  `json:"dek_version"`
	KeyGeneration int64  `json:"key_generation"`
	CreatedAtMs   int64  `json:"created_at_ms"`
	RotatedAtMs   int64  `json:"rotated_at_ms"`
}

const (
	shredStatusPendingCleanup = "PENDING_CLEANUP"
	shredStatusCompleted      = "COMPLETED"
)

func validateShredRequest(req ShredRequest) error {
	trimmed := strings.TrimSpace(req.RequestID)
	if trimmed == "" || trimmed != req.RequestID || len(req.RequestID) > 128 || strings.ContainsAny(req.RequestID, "\x00\r\n") {
		return fmt.Errorf("%w: non-blank RequestID (<=128 chars) is required for fenced crypto-shredding", ErrInvalidInput)
	}
	if req.ExpectedGeneration < 1 {
		return fmt.Errorf("%w: ExpectedGeneration >= 1 is required for fenced crypto-shredding", ErrInvalidInput)
	}
	return nil
}

// Config configures the envelope encryption Vault Store.
type Config struct {
	DB            *sql.DB
	KEK           KEKProvider
	CacheTTL      time.Duration
	NowMs         func() int64
	PostShredHook func(ctx context.Context) error
}

// Store implements three-tier AES-256-GCM envelope encryption, write-only metadata APIs,
// online KEK re-wrapping, and Project/Org Crypto-Shredding.
type Store struct {
	db        *sql.DB
	kek       KEKProvider
	cache     *dekCache
	nowMs     func() int64
	postShred func(ctx context.Context) error
	shredSem  chan struct{}
}

// NewStore validates dependencies and constructs a Vault Store fail-closed.
// PostShredHook is mandatory so SQLite WAL truncation (sqlite.Store.CheckpointWAL)
// or an explicit non-WAL cleanup hook is never silently omitted.
func NewStore(cfg Config) (*Store, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", ErrInvalidInput)
	}
	if cfg.KEK == nil {
		return nil, fmt.Errorf("%w: non-nil KEKProvider is required", ErrInvalidInput)
	}
	if cfg.PostShredHook == nil {
		return nil, fmt.Errorf("%w: non-nil PostShredHook is required for fail-closed shred cleanup", ErrInvalidInput)
	}
	cache, err := newDEKCache(cfg.CacheTTL)
	if err != nil {
		return nil, err
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	return &Store{
		db:        cfg.DB,
		kek:       cfg.KEK,
		cache:     cache,
		nowMs:     nowFn,
		postShred: cfg.PostShredHook,
		shredSem:  make(chan struct{}, 1),
	}, nil
}

func orgPendingShredKey(orgID string) string {
	return "org:" + orgID
}

func projectPendingShredKey(orgID, projectID string) string {
	return "proj:" + orgID + ":" + projectID
}

func (s *Store) acquireShredSlot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.shredSem <- struct{}{}:
		return nil
	}
}

func (s *Store) releaseShredSlot() {
	<-s.shredSem
}

// PendingShredCleanups reports how many durably committed shreds still await verified
// PostShredHook completion across process restarts. Returns -1 if the backing DB query fails.
func (s *Store) PendingShredCleanups() int {
	n, err := s.PendingShredCleanupsContext(context.Background())
	if err != nil {
		return -1
	}
	return n
}

// PendingShredCleanupsContext returns the count of durably persisted pending shred WAL cleanups.
func (s *Store) PendingShredCleanupsContext(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vault_pending_shred_cleanups`).Scan(&count); err != nil {
		return 0, fmt.Errorf("vault: count pending shred cleanups: %w", err)
	}
	return count, nil
}

// FlushPendingShredCleanups executes PostShredHook to verify WAL truncation for any durably
// persisted pending shred cleanups recovered after a crash or previous PostShredHook failure.
func (s *Store) FlushPendingShredCleanups(ctx context.Context) error {
	if err := s.acquireShredSlot(ctx); err != nil {
		return err
	}
	defer s.releaseShredSlot()

	observed, err := queryPendingCleanupTuples(ctx, s.db,
		`SELECT scope_key, org_id, shred_epoch, target_generation FROM vault_pending_shred_cleanups ORDER BY shred_epoch ASC`,
	)
	if err != nil {
		return fmt.Errorf("vault: inspect pending shred cleanups: %w", err)
	}
	if len(observed) == 0 {
		return nil
	}

	s.cache.clearAll()
	if herr := s.postShred(ctx); herr != nil {
		return fmt.Errorf("%w: %w", ErrPostShredCleanupFailed, herr)
	}
	now := s.nowMs()
	return s.inPostShredCleanupTx(ctx, func(ctxTx *sql.Tx) error {
		return clearPendingTuplesTx(ctx, ctxTx, observed, now)
	})
}

// GetOrgKeyMetadata retrieves write-only key metadata (including monotonic KeyGeneration)
// scoped to orgID without ever returning key ciphertext or plaintext bytes.
func (s *Store) GetOrgKeyMetadata(ctx context.Context, orgID string) (*OrgKeyMetadata, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, fmt.Errorf("%w: org_id required", ErrInvalidInput)
	}
	var m OrgKeyMetadata
	err := s.db.QueryRowContext(ctx, `
		SELECT org_id, kek_key_id, dek_version, key_generation, created_at_ms, rotated_at_ms
		FROM org_encryption_keys
		WHERE org_id = ?`, orgID).Scan(
		&m.OrgID, &m.KEKKeyID, &m.DEKVersion, &m.KeyGeneration, &m.CreatedAtMs, &m.RotatedAtMs,
	)
	if err != nil {
		return nil, mapVaultSQLiteError(err, false)
	}
	return &m, nil
}

// GetProjectKeyMetadata retrieves write-only key metadata (including monotonic KeyGeneration)
// scoped to (orgID, projectID) without ever returning key ciphertext or plaintext bytes.
func (s *Store) GetProjectKeyMetadata(ctx context.Context, orgID, projectID string) (*ProjectKeyMetadata, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("%w: org_id and project_id required", ErrInvalidInput)
	}
	var m ProjectKeyMetadata
	err := s.db.QueryRowContext(ctx, `
		SELECT org_id, project_id, org_dek_version, dek_version, key_generation, created_at_ms, rotated_at_ms
		FROM project_encryption_keys
		WHERE org_id = ? AND project_id = ?`, orgID, projectID).Scan(
		&m.OrgID, &m.ProjectID, &m.OrgDEKVersion, &m.DEKVersion, &m.KeyGeneration, &m.CreatedAtMs, &m.RotatedAtMs,
	)
	if err != nil {
		return nil, mapVaultSQLiteError(err, false)
	}
	return &m, nil
}

// Close scrubs all cached DEKs in RAM without closing the shared SQL pool.
func (s *Store) Close() {
	if s.cache != nil {
		s.cache.clearAll()
	}
}

// EvictAllDEKs immediately zeroizes all in-memory cached DEKs.
func (s *Store) EvictAllDEKs() {
	if s.cache != nil {
		s.cache.clearAll()
	}
}

// SealSecret envelopes plaintext using AES-256-GCM under the Org DEK (when OwnerProjectID == nil)
// or the Project DEK (when OwnerProjectID != nil), binding the canonical AEAD AAD formula.
func (s *Store) SealSecret(ctx context.Context, params SealSecretParams) (*SecretMetadata, error) {
	if strings.TrimSpace(params.OrgID) == "" || strings.TrimSpace(params.Name) == "" {
		return nil, fmt.Errorf("%w: org_id and name are required", ErrInvalidInput)
	}
	if !isValidSecretKind(params.SecretKind) {
		return nil, fmt.Errorf("%w: invalid secret_kind %q", ErrInvalidInput, params.SecretKind)
	}
	if len(params.Plaintext) == 0 {
		return nil, fmt.Errorf("%w: secret plaintext cannot be empty", ErrInvalidInput)
	}

	id := params.ID
	if id == "" {
		var err error
		id, err = idgen.New(idgen.PrefixSecret)
		if err != nil {
			return nil, err
		}
	} else if err := validateAADSegment("secret_id", id); err != nil || !strings.HasPrefix(id, idgen.PrefixSecret+"_") || len(id) > 40 {
		return nil, fmt.Errorf("%w: invalid caller-supplied secret id %q", ErrInvalidInput, id)
	}

	// Copy caller buffer so vault zeroizes its own transient slice reliably.
	pt := make([]byte, len(params.Plaintext))
	copy(pt, params.Plaintext)
	defer clear(pt)

	var ownerProj *string
	if params.OwnerProjectID != nil {
		trimmed := strings.TrimSpace(*params.OwnerProjectID)
		if trimmed == "" {
			return nil, fmt.Errorf("%w: owner_project_id cannot be blank when provided", ErrInvalidInput)
		}
		ownerProj = &trimmed
	}

	var aad []byte
	var err error
	if ownerProj == nil {
		aad, err = OrgSecretAAD(params.OrgID, id, params.SecretKind)
	} else {
		aad, err = ProjectRecordAAD(params.OrgID, *ownerProj, RecordKindCred, id)
	}
	if err != nil {
		return nil, err
	}

	now := s.nowMs()
	fingerprint := computeKeyFingerprint(params.OrgID, id, pt)
	gen := s.cache.generation()
	var stage dekCacheStage
	defer stage.clear()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var dek []byte
	if ownerProj == nil {
		_, dek, err = s.ensureOrgDEKTx(ctx, tx, params.OrgID, now, &stage)
	} else {
		_, _, dek, err = s.ensureProjectDEKTx(ctx, tx, params.OrgID, *ownerProj, now, &stage)
	}
	if err != nil {
		return nil, err
	}
	defer clear(dek)

	nonce, ciphertext, err := sealAESGCM(dek, aad, pt)
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO secret_credentials (
			id, org_id, owner_project_id, name, secret_kind, aead_alg,
			nonce, ciphertext, bound_audience, key_fingerprint, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, 'AES-256-GCM', ?, ?, ?, ?, 1, ?, ?)`,
		id, params.OrgID, nullStringPtr(ownerProj), params.Name, params.SecretKind,
		nonce, ciphertext, nullStringPtr(params.BoundAudience), fingerprint, now, now,
	); err != nil {
		return nil, mapVaultSQLiteError(err, false)
	}

	if _, err := appendVaultChangelogTx(ctx, tx, params.OrgID, "secret_credential", id, port.MutationUpsert, false, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("vault: commit SealSecret: %w", err)
	}
	committed = true
	stage.commit(s.cache, now, gen)

	return &SecretMetadata{
		ID:             id,
		OrgID:          params.OrgID,
		OwnerProjectID: cloneStringPtr(ownerProj),
		Name:           params.Name,
		SecretKind:     params.SecretKind,
		BoundAudience:  cloneStringPtr(params.BoundAudience),
		KeyFingerprint: fingerprint,
		Version:        1,
		CreatedAtMs:    now,
		UpdatedAtMs:    now,
	}, nil
}

// RotateSecret replaces ciphertext using a fresh 96-bit CSPRNG nonce under optimistic concurrency control.
func (s *Store) RotateSecret(ctx context.Context, orgID, secretID string, expectedVersion int64, params RotateSecretParams) (*SecretMetadata, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(secretID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id, secret_id, and expected_version >= 1 required", ErrInvalidInput)
	}
	if len(params.Plaintext) == 0 {
		return nil, fmt.Errorf("%w: new plaintext cannot be empty", ErrInvalidInput)
	}

	pt := make([]byte, len(params.Plaintext))
	copy(pt, params.Plaintext)
	defer clear(pt)

	now := s.nowMs()
	fingerprint := computeKeyFingerprint(orgID, secretID, pt)
	gen := s.cache.generation()
	var stage dekCacheStage
	defer stage.clear()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var (
		name        string
		kind        string
		curVersion  int64
		createdAtMs int64
		projNull    sql.NullString
		audNull     sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT owner_project_id, name, secret_kind, bound_audience, version, created_at_ms
		FROM secret_credentials
		WHERE org_id = ? AND id = ?`,
		orgID, secretID,
	).Scan(&projNull, &name, &kind, &audNull, &curVersion, &createdAtMs); err != nil {
		return nil, mapVaultSQLiteError(err, false)
	}
	if curVersion != expectedVersion {
		return nil, port.ErrVersionConflict
	}

	var ownerProj *string
	if projNull.Valid {
		v := projNull.String
		ownerProj = &v
	}
	var nextAudience *string
	if audNull.Valid {
		v := audNull.String
		nextAudience = &v
	}
	if params.ClearBoundAudience {
		nextAudience = nil
	} else if params.BoundAudience != nil {
		nextAudience = cloneStringPtr(params.BoundAudience)
	}

	var aad []byte
	if ownerProj == nil {
		aad, err = OrgSecretAAD(orgID, secretID, kind)
	} else {
		aad, err = ProjectRecordAAD(orgID, *ownerProj, RecordKindCred, secretID)
	}
	if err != nil {
		return nil, err
	}

	var dek []byte
	if ownerProj == nil {
		_, dek, err = s.ensureOrgDEKTx(ctx, tx, orgID, now, &stage)
	} else {
		_, _, dek, err = s.ensureProjectDEKTx(ctx, tx, orgID, *ownerProj, now, &stage)
	}
	if err != nil {
		return nil, err
	}
	defer clear(dek)

	nonce, ciphertext, err := sealAESGCM(dek, aad, pt)
	if err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE secret_credentials
		SET nonce = ?, ciphertext = ?, bound_audience = ?, key_fingerprint = ?, version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`,
		nonce, ciphertext, nullStringPtr(nextAudience), fingerprint, now, orgID, secretID, expectedVersion,
	)
	if err != nil {
		return nil, mapVaultSQLiteError(err, false)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, port.ErrVersionConflict
	}

	if _, err := appendVaultChangelogTx(ctx, tx, orgID, "secret_credential", secretID, port.MutationUpsert, false, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("vault: commit RotateSecret: %w", err)
	}
	committed = true
	stage.commit(s.cache, now, gen)

	return &SecretMetadata{
		ID:             secretID,
		OrgID:          orgID,
		OwnerProjectID: cloneStringPtr(ownerProj),
		Name:           name,
		SecretKind:     kind,
		BoundAudience:  cloneStringPtr(nextAudience),
		KeyFingerprint: fingerprint,
		Version:        curVersion + 1,
		CreatedAtMs:    createdAtMs,
		UpdatedAtMs:    now,
	}, nil
}

// GetSecretMetadata fetches write-only secret metadata strictly scoped to orgID.
func (s *Store) GetSecretMetadata(ctx context.Context, orgID, secretID string) (*SecretMetadata, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(secretID) == "" {
		return nil, fmt.Errorf("%w: org_id and secret_id required", ErrInvalidInput)
	}
	return scanSecretMetadata(s.db.QueryRowContext(ctx, `
		SELECT id, org_id, owner_project_id, name, secret_kind, bound_audience, key_fingerprint, version, created_at_ms, updated_at_ms
		FROM secret_credentials
		WHERE org_id = ? AND id = ?`, orgID, secretID))
}

// ListSecrets returns write-only metadata for all secrets belonging to orgID (optionally filtered to projectID).
func (s *Store) ListSecrets(ctx context.Context, orgID string, projectID *string) ([]SecretMetadata, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, fmt.Errorf("%w: org_id required", ErrInvalidInput)
	}
	if projectID != nil && strings.TrimSpace(*projectID) == "" {
		return nil, fmt.Errorf("%w: project_id cannot be blank when provided", ErrInvalidInput)
	}
	var (
		rows *sql.Rows
		err  error
	)
	if projectID == nil {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, org_id, owner_project_id, name, secret_kind, bound_audience, key_fingerprint, version, created_at_ms, updated_at_ms
			FROM secret_credentials
			WHERE org_id = ?
			ORDER BY id ASC`, orgID)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, org_id, owner_project_id, name, secret_kind, bound_audience, key_fingerprint, version, created_at_ms, updated_at_ms
			FROM secret_credentials
			WHERE org_id = ? AND owner_project_id = ?
			ORDER BY id ASC`, orgID, *projectID)
	}
	if err != nil {
		return nil, mapVaultSQLiteError(err, false)
	}
	defer rows.Close()

	var out []SecretMetadata
	for rows.Next() {
		item, serr := scanSecretMetadata(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// UnsealWithCapability decrypts a stored secret strictly when presented a validly minted UnsealCapability.
// Callers MUST invoke returned.Zeroize() immediately after outbound header construction.
func (s *Store) UnsealWithCapability(ctx context.Context, cap UnsealCapability, orgID, secretID string) (*UnsealedSecret, error) {
	if !cap.granted {
		return nil, ErrMissingUnsealCapability
	}
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(secretID) == "" {
		return nil, fmt.Errorf("%w: org_id and secret_id required", ErrInvalidInput)
	}

	var (
		projNull   sql.NullString
		kind       string
		aeadAlg    string
		nonce      []byte
		ciphertext []byte
		audNull    sql.NullString
		version    int64
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT owner_project_id, secret_kind, aead_alg, nonce, ciphertext, bound_audience, version
		FROM secret_credentials
		WHERE org_id = ? AND id = ?`,
		orgID, secretID,
	).Scan(&projNull, &kind, &aeadAlg, &nonce, &ciphertext, &audNull, &version); err != nil {
		return nil, mapVaultSQLiteError(err, false)
	}
	if aeadAlg != "AES-256-GCM" {
		return nil, fmt.Errorf("%w: unsupported algorithm %q", ErrDecryptionFailed, aeadAlg)
	}

	var ownerProj *string
	if projNull.Valid {
		v := projNull.String
		ownerProj = &v
	}
	var boundAud *string
	if audNull.Valid {
		v := audNull.String
		boundAud = &v
	}

	var (
		aad []byte
		dek []byte
		err error
	)
	if ownerProj == nil {
		aad, err = OrgSecretAAD(orgID, secretID, kind)
		if err != nil {
			return nil, err
		}
		_, dek, err = s.loadOrgDEKReadOnly(ctx, orgID)
	} else {
		aad, err = ProjectRecordAAD(orgID, *ownerProj, RecordKindCred, secretID)
		if err != nil {
			return nil, err
		}
		_, _, dek, err = s.loadProjectDEKReadOnly(ctx, orgID, *ownerProj)
	}
	if err != nil {
		return nil, err
	}
	defer clear(dek)

	plaintext, err := openAESGCM(dek, aad, nonce, ciphertext)
	if err != nil {
		return nil, err
	}

	return &UnsealedSecret{
		ID:             secretID,
		OrgID:          orgID,
		OwnerProjectID: ownerProj,
		SecretKind:     kind,
		BoundAudience:  boundAud,
		Version:        version,
		Plaintext:      plaintext,
	}, nil
}

// RewrapAllOrgDEKs performs online Master KEK rotation from oldKEKID to newKEKID by atomically
// re-wrapping org_encryption_keys rows without touching project_encryption_keys or secret_credentials,
// appending KEY_ROTATE to config_changelog, and evicting old-KEK cached Org DEKs from RAM (DR-03 / AC-2).
func (s *Store) RewrapAllOrgDEKs(ctx context.Context, oldKEKID, newKEKID string) (RewrapSummary, error) {
	summary := RewrapSummary{OldKEKID: oldKEKID, NewKEKID: newKEKID}
	if err := validateValidKeyID(oldKEKID); err != nil {
		return summary, err
	}
	if err := validateValidKeyID(newKEKID); err != nil {
		return summary, err
	}
	if oldKEKID == newKEKID {
		return summary, fmt.Errorf("%w: newKEKID must differ from oldKEKID", ErrInvalidInput)
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return summary, fmt.Errorf("vault: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	type orgKeyRow struct {
		orgID      string
		dekVersion int64
		nonce      []byte
		wrapped    []byte
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT org_id, dek_version, nonce, wrapped_org_dek
		FROM org_encryption_keys
		WHERE kek_key_id = ?
		ORDER BY org_id ASC`, oldKEKID)
	if err != nil {
		return summary, mapVaultSQLiteError(err, false)
	}
	var pending []orgKeyRow
	for rows.Next() {
		var r orgKeyRow
		if scanErr := rows.Scan(&r.orgID, &r.dekVersion, &r.nonce, &r.wrapped); scanErr != nil {
			_ = rows.Close()
			return summary, scanErr
		}
		pending = append(pending, r)
	}
	if rerr := rows.Err(); rerr != nil {
		_ = rows.Close()
		return summary, rerr
	}
	_ = rows.Close()

	for _, r := range pending {
		if err := s.rewrapSingleOrgTx(ctx, tx, oldKEKID, newKEKID, r.orgID, r.dekVersion, r.nonce, r.wrapped, now); err != nil {
			return summary, err
		}
		summary.Rewrapped++
	}

	if summary.Rewrapped > 0 {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (NULL, 'master_kek', ?, ?, 0, ?)`,
			newKEKID, port.MutationKeyRotate, now,
		)
		if err != nil {
			return summary, mapVaultSQLiteError(err, false)
		}
		epoch, err := res.LastInsertId()
		if err != nil {
			return summary, err
		}
		summary.ConfigEpoch = epoch
	}

	if err := tx.Commit(); err != nil {
		return summary, fmt.Errorf("vault: commit RewrapAllOrgDEKs: %w", err)
	}
	committed = true

	// Pre-retirement RAM cache eviction for oldKEKID so subsequent unseals re-verify against newKEKID (DR-03).
	s.cache.evictByKEKID(oldKEKID)
	return summary, nil
}

func (s *Store) rewrapSingleOrgTx(ctx context.Context, tx *sql.Tx, oldKEKID, newKEKID, orgID string, dekVersion int64, oldNonce, oldWrapped []byte, now int64) error {
	orgDEK, err := s.kek.UnwrapDEK(ctx, oldKEKID, orgID, dekVersion, oldNonce, oldWrapped)
	if err != nil {
		return err
	}
	defer clear(orgDEK)

	newNonce, newWrapped, err := s.kek.WrapDEK(ctx, newKEKID, orgID, dekVersion, orgDEK)
	if err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE org_encryption_keys
		SET kek_key_id = ?, nonce = ?, wrapped_org_dek = ?, rotated_at_ms = ?
		WHERE org_id = ? AND kek_key_id = ? AND dek_version = ?`,
		newKEKID, newNonce, newWrapped, now, orgID, oldKEKID, dekVersion,
	)
	if err != nil {
		return mapVaultSQLiteError(err, false)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return port.ErrVersionConflict
	}
	return nil
}

// ShredProjectDEK cryptographically erases a single project's DEK pinned to req.ExpectedGeneration,
// immediately zeroizing cached DEK bytes in RAM, emitting an emergency SHRED changelog epoch,
// durably persisting crash-safe pending WAL cleanup + idempotency fencing inside the same DB transaction,
// and invoking the mandatory PostShredHook (e.g. sqlite WAL checkpoint truncation) fail-closed.
// Late retries of an old RequestID or older ExpectedGeneration can never delete a recreated Project DEK
// (failing closed with ErrStaleShredGeneration / port.ErrVersionConflict).
func (s *Store) ShredProjectDEK(ctx context.Context, orgID, projectID string, req ShredRequest) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("%w: org_id and project_id required", ErrInvalidInput)
	}
	if err := validateShredRequest(req); err != nil {
		return err
	}
	if err := s.acquireShredSlot(ctx); err != nil {
		return err
	}
	defer s.releaseShredSlot()

	projKey := projectPendingShredKey(orgID, projectID)
	orgKey := orgPendingShredKey(orgID)

	// Zeroize cache immediately before and after DB deletion.
	s.cache.evictProject(orgID, projectID)

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("vault: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	reqStatus, reqErr := lookupShredRequestTx(ctx, tx, orgID, req.RequestID, projKey, req.ExpectedGeneration)
	if reqErr != nil {
		return reqErr
	}
	if done, doneErr := isCompletedWithoutPendingTx(ctx, tx, reqStatus, projKey, req.ExpectedGeneration, req.RequestID); doneErr != nil {
		return doneErr
	} else if done {
		_ = tx.Rollback()
		committed = true
		return nil
	}

	res, err := tx.ExecContext(ctx,
		`DELETE FROM project_encryption_keys WHERE org_id = ? AND project_id = ? AND key_generation = ?`,
		orgID, projectID, req.ExpectedGeneration,
	)
	if err != nil {
		return mapVaultSQLiteError(err, true)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	var (
		clearEpoch     int64
		clearTargetGen int64
	)
	if affected > 0 {
		shredEpoch, cerr := appendVaultChangelogTx(ctx, tx, orgID, "project_encryption_key", projectID, port.MutationShred, true, now)
		if cerr != nil {
			return cerr
		}
		if perr := recordPendingShredTx(ctx, tx, projKey, orgID, &projectID, shredEpoch, req.ExpectedGeneration, now); perr != nil {
			return perr
		}
		if uerr := upsertShredRequestExec(ctx, tx, req.RequestID, projKey, orgID, &projectID, req.ExpectedGeneration, shredStatusPendingCleanup, now); uerr != nil {
			return uerr
		}
		if cerr := tx.Commit(); cerr != nil {
			return fmt.Errorf("vault: commit ShredProjectDEK: %w", cerr)
		}
		committed = true
		clearEpoch = shredEpoch
		clearTargetGen = req.ExpectedGeneration
	} else {
		var liveGen int64
		lerr := tx.QueryRowContext(ctx,
			`SELECT key_generation FROM project_encryption_keys WHERE org_id = ? AND project_id = ?`,
			orgID, projectID,
		).Scan(&liveGen)
		if lerr == nil {
			return fmt.Errorf("%w: current project DEK generation is %d, expected %d", ErrStaleShredGeneration, liveGen, req.ExpectedGeneration)
		}
		if !errors.Is(lerr, sql.ErrNoRows) {
			return mapVaultSQLiteError(lerr, false)
		}

		var (
			projEpoch        int64
			pendingTargetGen int64
		)
		qerr := tx.QueryRowContext(ctx,
			`SELECT shred_epoch, target_generation FROM vault_pending_shred_cleanups WHERE scope_key = ?`,
			projKey,
		).Scan(&projEpoch, &pendingTargetGen)
		switch {
		case qerr == nil:
			if pendingTargetGen != req.ExpectedGeneration {
				return fmt.Errorf("%w: pending project shred targets generation %d, expected %d", ErrStaleShredGeneration, pendingTargetGen, req.ExpectedGeneration)
			}
			_ = tx.Rollback()
			committed = true // prevent double rollback in defer; complete pending WAL cleanup below.
			clearEpoch = projEpoch
			clearTargetGen = pendingTargetGen
		case !errors.Is(qerr, sql.ErrNoRows):
			return mapVaultSQLiteError(qerr, false)
		default:
			if gerr := checkGenerationNotSupersededTx(ctx, tx, projKey, req.ExpectedGeneration, req.RequestID); gerr != nil {
				return gerr
			}
			var orgEpoch int64
			oerr := tx.QueryRowContext(ctx,
				`SELECT shred_epoch FROM vault_pending_shred_cleanups WHERE scope_key = ?`,
				orgKey,
			).Scan(&orgEpoch)
			switch {
			case oerr == nil:
				_ = tx.Rollback()
				committed = true
				s.cache.evictProject(orgID, projectID)
				if herr := s.postShred(ctx); herr != nil {
					return fmt.Errorf("%w: %w", ErrPostShredCleanupFailed, herr)
				}
				return port.ErrNotFound
			case !errors.Is(oerr, sql.ErrNoRows):
				return mapVaultSQLiteError(oerr, false)
			default:
				return port.ErrNotFound
			}
		}
	}

	s.cache.evictProject(orgID, projectID)
	if herr := s.postShred(ctx); herr != nil {
		return fmt.Errorf("%w: %w", ErrPostShredCleanupFailed, herr)
	}
	return s.inPostShredCleanupTx(ctx, func(ctxTx *sql.Tx) error {
		if _, cerr := ctxTx.ExecContext(ctx,
			`DELETE FROM vault_pending_shred_cleanups WHERE scope_key = ? AND shred_epoch = ?`,
			projKey, clearEpoch,
		); cerr != nil {
			return fmt.Errorf("vault: clear pending shred cleanup: %w", cerr)
		}
		return completeProjectShredTx(ctx, ctxTx, req.RequestID, projKey, orgID, &projectID, clearTargetGen, s.nowMs())
	})
}

// ShredOrgDEK cryptographically erases an organization's root Org DEK pinned to req.ExpectedGeneration,
// cascades all child Project DEKs, deletes all org-owned and project-owned secret_credentials rows,
// zeroizes all cached Org/Project DEKs in RAM, records an emergency SHRED epoch and durable pending
// cleanup + idempotency fencing atomically in DB, and executes PostShredHook fail-closed.
// Late retries of an old RequestID or older ExpectedGeneration never delete a recreated Org DEK or its secrets.
func (s *Store) ShredOrgDEK(ctx context.Context, orgID string, req ShredRequest) error {
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("%w: org_id required", ErrInvalidInput)
	}
	if err := validateShredRequest(req); err != nil {
		return err
	}
	if err := s.acquireShredSlot(ctx); err != nil {
		return err
	}
	defer s.releaseShredSlot()

	orgKey := orgPendingShredKey(orgID)
	s.cache.evictOrg(orgID)

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("vault: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	reqStatus, reqErr := lookupShredRequestTx(ctx, tx, orgID, req.RequestID, orgKey, req.ExpectedGeneration)
	if reqErr != nil {
		return reqErr
	}
	if done, doneErr := isCompletedWithoutPendingTx(ctx, tx, reqStatus, orgKey, req.ExpectedGeneration, req.RequestID); doneErr != nil {
		return doneErr
	} else if done {
		_ = tx.Rollback()
		committed = true
		return nil
	}

	// Execute fenced DELETE on org_encryption_keys FIRST so stale retries never touch secret_credentials!
	res, err := tx.ExecContext(ctx,
		`DELETE FROM org_encryption_keys WHERE org_id = ? AND key_generation = ?`,
		orgID, req.ExpectedGeneration,
	)
	if err != nil {
		return mapVaultSQLiteError(err, true)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	var clearMaxEpoch int64
	if affected > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM secret_credentials WHERE org_id = ?`, orgID); err != nil {
			return mapVaultSQLiteError(err, true)
		}
		shredEpoch, cerr := appendVaultChangelogTx(ctx, tx, orgID, "org_encryption_key", orgID, port.MutationShred, true, now)
		if cerr != nil {
			return cerr
		}
		if perr := recordPendingShredTx(ctx, tx, orgKey, orgID, nil, shredEpoch, req.ExpectedGeneration, now); perr != nil {
			return perr
		}
		if uerr := upsertShredRequestExec(ctx, tx, req.RequestID, orgKey, orgID, nil, req.ExpectedGeneration, shredStatusPendingCleanup, now); uerr != nil {
			return uerr
		}
		if cerr := tx.Commit(); cerr != nil {
			return fmt.Errorf("vault: commit ShredOrgDEK: %w", cerr)
		}
		committed = true
		clearMaxEpoch = shredEpoch
	} else {
		var liveGen int64
		lerr := tx.QueryRowContext(ctx,
			`SELECT key_generation FROM org_encryption_keys WHERE org_id = ?`,
			orgID,
		).Scan(&liveGen)
		if lerr == nil {
			return fmt.Errorf("%w: current org DEK generation is %d, expected %d", ErrStaleShredGeneration, liveGen, req.ExpectedGeneration)
		}
		if !errors.Is(lerr, sql.ErrNoRows) {
			return mapVaultSQLiteError(lerr, false)
		}

		var (
			orgEpoch         int64
			pendingTargetGen int64
		)
		qerr := tx.QueryRowContext(ctx,
			`SELECT shred_epoch, target_generation FROM vault_pending_shred_cleanups WHERE scope_key = ?`,
			orgKey,
		).Scan(&orgEpoch, &pendingTargetGen)
		switch {
		case qerr == nil:
			if pendingTargetGen != req.ExpectedGeneration {
				return fmt.Errorf("%w: pending org shred targets generation %d, expected %d", ErrStaleShredGeneration, pendingTargetGen, req.ExpectedGeneration)
			}
			// Include any older child project shred epochs under the same org.
			var maxEpoch sql.NullInt64
			if merr := tx.QueryRowContext(ctx,
				`SELECT MAX(shred_epoch) FROM vault_pending_shred_cleanups WHERE org_id = ?`,
				orgID,
			).Scan(&maxEpoch); merr != nil {
				return mapVaultSQLiteError(merr, false)
			}
			_ = tx.Rollback()
			committed = true
			clearMaxEpoch = orgEpoch
			if maxEpoch.Valid && maxEpoch.Int64 > clearMaxEpoch {
				clearMaxEpoch = maxEpoch.Int64
			}
		case !errors.Is(qerr, sql.ErrNoRows):
			return mapVaultSQLiteError(qerr, false)
		default:
			var maxEpoch sql.NullInt64
			merr := tx.QueryRowContext(ctx,
				`SELECT MAX(shred_epoch) FROM vault_pending_shred_cleanups WHERE org_id = ?`,
				orgID,
			).Scan(&maxEpoch)
			if merr != nil {
				return mapVaultSQLiteError(merr, false)
			}
			if maxEpoch.Valid {
				_ = tx.Rollback()
				committed = true
				s.cache.evictOrg(orgID)
				if herr := s.postShred(ctx); herr != nil {
					return fmt.Errorf("%w: %w", ErrPostShredCleanupFailed, herr)
				}
				return port.ErrNotFound
			}
			if gerr := checkGenerationNotSupersededTx(ctx, tx, orgKey, req.ExpectedGeneration, req.RequestID); gerr != nil {
				return gerr
			}
			return port.ErrNotFound
		}
	}

	s.cache.evictOrg(orgID)
	if herr := s.postShred(ctx); herr != nil {
		return fmt.Errorf("%w: %w", ErrPostShredCleanupFailed, herr)
	}
	return s.inPostShredCleanupTx(ctx, func(ctxTx *sql.Tx) error {
		cleared, rerr := queryPendingCleanupTuples(ctx, ctxTx,
			`SELECT scope_key, org_id, shred_epoch, target_generation FROM vault_pending_shred_cleanups WHERE org_id = ? AND shred_epoch <= ?`,
			orgID, clearMaxEpoch,
		)
		if rerr != nil {
			return fmt.Errorf("vault: inspect cleared org shred cleanups: %w", rerr)
		}
		nowDone := s.nowMs()
		if err := upsertShredRequestExec(ctx, ctxTx, req.RequestID, orgKey, orgID, nil, req.ExpectedGeneration, shredStatusCompleted, nowDone); err != nil {
			return err
		}
		return clearPendingTuplesTx(ctx, ctxTx, cleared, nowDone)
	})
}

type stagedOrgDEK struct {
	orgID         string
	kekKeyID      string
	dekVersion    int64
	keyGeneration int64
	dek           []byte
}

type stagedProjDEK struct {
	orgID             string
	projectID         string
	orgDEKVersion     int64
	projectDEKVersion int64
	keyGeneration     int64
	dek               []byte
}

type dekCacheStage struct {
	orgs  []stagedOrgDEK
	projs []stagedProjDEK
}

func (st *dekCacheStage) addOrg(orgID, kekKeyID string, dekVersion, keyGeneration int64, dek []byte) {
	cp := make([]byte, len(dek))
	copy(cp, dek)
	st.orgs = append(st.orgs, stagedOrgDEK{orgID: orgID, kekKeyID: kekKeyID, dekVersion: dekVersion, keyGeneration: keyGeneration, dek: cp})
}

func (st *dekCacheStage) addProj(orgID, projectID string, orgDEKVersion, projectDEKVersion, keyGeneration int64, dek []byte) {
	cp := make([]byte, len(dek))
	copy(cp, dek)
	st.projs = append(st.projs, stagedProjDEK{orgID: orgID, projectID: projectID, orgDEKVersion: orgDEKVersion, projectDEKVersion: projectDEKVersion, keyGeneration: keyGeneration, dek: cp})
}

func (st *dekCacheStage) commit(cache *dekCache, nowMs int64, gen uint64) {
	for _, o := range st.orgs {
		cache.putOrgDEKIfGen(o.orgID, o.kekKeyID, o.dekVersion, o.keyGeneration, o.dek, nowMs, gen)
	}
	for _, p := range st.projs {
		cache.putProjectDEKIfGen(p.orgID, p.projectID, p.orgDEKVersion, p.projectDEKVersion, p.keyGeneration, p.dek, nowMs, gen)
	}
}

func (st *dekCacheStage) clear() {
	for _, o := range st.orgs {
		clear(o.dek)
	}
	for _, p := range st.projs {
		clear(p.dek)
	}
	st.orgs = nil
	st.projs = nil
}

func (s *Store) ensureOrgDEKTx(ctx context.Context, tx *sql.Tx, orgID string, now int64, stage *dekCacheStage) (int64, []byte, error) {
	var (
		kekKeyID   string
		nonce      []byte
		wrapped    []byte
		dekVersion int64
		keyGen     int64
	)
	err := tx.QueryRowContext(ctx, `
		SELECT kek_key_id, nonce, wrapped_org_dek, dek_version, key_generation
		FROM org_encryption_keys
		WHERE org_id = ?`, orgID).Scan(&kekKeyID, &nonce, &wrapped, &dekVersion, &keyGen)
	if errors.Is(err, sql.ErrNoRows) {
		var pendingMarker int
		perr := tx.QueryRowContext(ctx,
			`SELECT 1 FROM vault_pending_shred_cleanups WHERE org_id = ? LIMIT 1`,
			orgID,
		).Scan(&pendingMarker)
		if perr == nil {
			return 0, nil, fmt.Errorf("%w: %w (org_id=%s)", ErrPostShredCleanupFailed, ErrPendingShredCleanup, orgID)
		}
		if !errors.Is(perr, sql.ErrNoRows) {
			return 0, nil, mapVaultSQLiteError(perr, false)
		}

		activeKEKID, kerr := s.kek.ActiveKeyID(ctx)
		if kerr != nil {
			return 0, nil, kerr
		}
		freshDEK, derr := generate256BitDEK()
		if derr != nil {
			return 0, nil, derr
		}
		wrapNonce, wrapCT, werr := s.kek.WrapDEK(ctx, activeKEKID, orgID, 1, freshDEK)
		if werr != nil {
			clear(freshDEK)
			return 0, nil, werr
		}
		var insertedGen int64
		won, ierr := inSavepointTx(ctx, tx, "sp_org_dek_mint", func() (bool, error) {
			nextGen, gerr := nextKeyGenerationTx(ctx, tx, orgPendingShredKey(orgID), orgID, nil, now)
			if gerr != nil {
				return false, gerr
			}
			res, err := tx.ExecContext(ctx, `
				INSERT INTO org_encryption_keys (
					org_id, kek_key_id, aead_alg, nonce, wrapped_org_dek, dek_version, key_generation, created_at_ms, rotated_at_ms
				) VALUES (?, ?, 'AES-256-GCM', ?, ?, 1, ?, ?, ?)
				ON CONFLICT(org_id) DO NOTHING`,
				orgID, activeKEKID, wrapNonce, wrapCT, nextGen, now, now,
			)
			if err != nil {
				return false, mapVaultSQLiteError(err, false)
			}
			affected, aerr := res.RowsAffected()
			if aerr != nil || affected == 0 {
				return false, aerr
			}
			insertedGen = nextGen
			return true, nil
		})
		if ierr != nil {
			clear(freshDEK)
			return 0, nil, ierr
		}
		if won {
			if stage != nil {
				stage.addOrg(orgID, activeKEKID, 1, insertedGen, freshDEK)
			}
			return 1, freshDEK, nil
		}
		clear(freshDEK)
		if rerr := tx.QueryRowContext(ctx, `
			SELECT kek_key_id, nonce, wrapped_org_dek, dek_version, key_generation
			FROM org_encryption_keys
			WHERE org_id = ?`, orgID).Scan(&kekKeyID, &nonce, &wrapped, &dekVersion, &keyGen); rerr != nil {
			return 0, nil, mapVaultSQLiteError(rerr, false)
		}
	} else if err != nil {
		return 0, nil, mapVaultSQLiteError(err, false)
	}

	// Verify cache against persisted (kekKeyID, dekVersion, keyGeneration) to reject stale or superseded DEKs.
	if cachedKEK, cachedVer, cachedKeyGen, cp, ok := s.cache.getOrgDEK(orgID, now); ok {
		if cachedKEK == kekKeyID && cachedVer == dekVersion && cachedKeyGen == keyGen {
			return dekVersion, cp, nil
		}
		clear(cp)
	}

	unwrapped, uerr := s.kek.UnwrapDEK(ctx, kekKeyID, orgID, dekVersion, nonce, wrapped)
	if uerr != nil {
		return 0, nil, uerr
	}
	if stage != nil {
		stage.addOrg(orgID, kekKeyID, dekVersion, keyGen, unwrapped)
	}
	return dekVersion, unwrapped, nil
}

func (s *Store) ensureProjectDEKTx(ctx context.Context, tx *sql.Tx, orgID, projectID string, now int64, stage *dekCacheStage) (int64, int64, []byte, error) {
	orgDEKVer, orgDEK, err := s.ensureOrgDEKTx(ctx, tx, orgID, now, stage)
	if err != nil {
		return 0, 0, nil, err
	}
	defer clear(orgDEK)

	var (
		rowOrgDEKVer int64
		nonce        []byte
		wrapped      []byte
		projDEKVer   int64
		projKeyGen   int64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT org_dek_version, nonce, wrapped_project_dek, dek_version, key_generation
		FROM project_encryption_keys
		WHERE org_id = ? AND project_id = ?`, orgID, projectID).Scan(&rowOrgDEKVer, &nonce, &wrapped, &projDEKVer, &projKeyGen)
	if errors.Is(err, sql.ErrNoRows) {
		var pendingMarker int
		perr := tx.QueryRowContext(ctx,
			`SELECT 1 FROM vault_pending_shred_cleanups WHERE scope_key IN (?, ?) LIMIT 1`,
			projectPendingShredKey(orgID, projectID),
			orgPendingShredKey(orgID),
		).Scan(&pendingMarker)
		if perr == nil {
			return 0, 0, nil, fmt.Errorf("%w: %w (org_id=%s project_id=%s)", ErrPostShredCleanupFailed, ErrPendingShredCleanup, orgID, projectID)
		}
		if !errors.Is(perr, sql.ErrNoRows) {
			return 0, 0, nil, mapVaultSQLiteError(perr, false)
		}

		freshPDEK, derr := generate256BitDEK()
		if derr != nil {
			return 0, 0, nil, derr
		}
		aad, aerr := projectDEKWrapAAD(orgID, projectID, orgDEKVer, 1)
		if aerr != nil {
			clear(freshPDEK)
			return 0, 0, nil, aerr
		}
		wrapNonce, wrapCT, werr := sealAESGCM(orgDEK, aad, freshPDEK)
		if werr != nil {
			clear(freshPDEK)
			return 0, 0, nil, werr
		}
		var insertedGen int64
		won, ierr := inSavepointTx(ctx, tx, "sp_proj_dek_mint", func() (bool, error) {
			nextGen, gerr := nextKeyGenerationTx(ctx, tx, projectPendingShredKey(orgID, projectID), orgID, &projectID, now)
			if gerr != nil {
				return false, gerr
			}
			res, err := tx.ExecContext(ctx, `
				INSERT INTO project_encryption_keys (
					org_id, project_id, org_dek_version, aead_alg, nonce, wrapped_project_dek, dek_version, key_generation, created_at_ms, rotated_at_ms
				) VALUES (?, ?, ?, 'AES-256-GCM', ?, ?, 1, ?, ?, ?)
				ON CONFLICT(org_id, project_id) DO NOTHING`,
				orgID, projectID, orgDEKVer, wrapNonce, wrapCT, nextGen, now, now,
			)
			if err != nil {
				return false, mapVaultSQLiteError(err, false)
			}
			affected, aerr := res.RowsAffected()
			if aerr != nil || affected == 0 {
				return false, aerr
			}
			insertedGen = nextGen
			return true, nil
		})
		if ierr != nil {
			clear(freshPDEK)
			return 0, 0, nil, ierr
		}
		if won {
			if stage != nil {
				stage.addProj(orgID, projectID, orgDEKVer, 1, insertedGen, freshPDEK)
			}
			return orgDEKVer, 1, freshPDEK, nil
		}
		clear(freshPDEK)
		if rerr := tx.QueryRowContext(ctx, `
			SELECT org_dek_version, nonce, wrapped_project_dek, dek_version, key_generation
			FROM project_encryption_keys
			WHERE org_id = ? AND project_id = ?`, orgID, projectID).Scan(&rowOrgDEKVer, &nonce, &wrapped, &projDEKVer, &projKeyGen); rerr != nil {
			return 0, 0, nil, mapVaultSQLiteError(rerr, false)
		}
	} else if err != nil {
		return 0, 0, nil, mapVaultSQLiteError(err, false)
	}

	if cachedOrgV, cachedProjV, cachedKeyGen, cp, ok := s.cache.getProjectDEK(orgID, projectID, now); ok {
		if cachedOrgV == rowOrgDEKVer && cachedProjV == projDEKVer && cachedKeyGen == projKeyGen {
			return cachedOrgV, cachedProjV, cp, nil
		}
		clear(cp)
	}

	aad, err := projectDEKWrapAAD(orgID, projectID, rowOrgDEKVer, projDEKVer)
	if err != nil {
		return 0, 0, nil, err
	}
	unwrapped, err := openAESGCM(orgDEK, aad, nonce, wrapped)
	if err != nil {
		return 0, 0, nil, err
	}
	if err := validate256BitKey(unwrapped); err != nil {
		clear(unwrapped)
		return 0, 0, nil, err
	}
	if stage != nil {
		stage.addProj(orgID, projectID, rowOrgDEKVer, projDEKVer, projKeyGen, unwrapped)
	}
	return rowOrgDEKVer, projDEKVer, unwrapped, nil
}

func (s *Store) loadOrgDEKReadOnly(ctx context.Context, orgID string) (int64, []byte, error) {
	now := s.nowMs()
	gen := s.cache.generation()
	var (
		kekKeyID   string
		nonce      []byte
		wrapped    []byte
		dekVersion int64
		keyGen     int64
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT kek_key_id, nonce, wrapped_org_dek, dek_version, key_generation
		FROM org_encryption_keys
		WHERE org_id = ?`, orgID).Scan(&kekKeyID, &nonce, &wrapped, &dekVersion, &keyGen); err != nil {
		return 0, nil, mapVaultSQLiteError(err, false)
	}
	if cachedKEK, cachedVer, cachedKeyGen, cp, ok := s.cache.getOrgDEK(orgID, now); ok {
		if cachedKEK == kekKeyID && cachedVer == dekVersion && cachedKeyGen == keyGen {
			return dekVersion, cp, nil
		}
		clear(cp)
	}
	unwrapped, err := s.kek.UnwrapDEK(ctx, kekKeyID, orgID, dekVersion, nonce, wrapped)
	if err != nil {
		return 0, nil, err
	}
	s.cache.putOrgDEKIfGen(orgID, kekKeyID, dekVersion, keyGen, unwrapped, now, gen)
	return dekVersion, unwrapped, nil
}

func (s *Store) loadProjectDEKReadOnly(ctx context.Context, orgID, projectID string) (int64, int64, []byte, error) {
	now := s.nowMs()
	gen := s.cache.generation()
	var (
		rowOrgDEKVer int64
		nonce        []byte
		wrapped      []byte
		projDEKVer   int64
		projKeyGen   int64
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT org_dek_version, nonce, wrapped_project_dek, dek_version, key_generation
		FROM project_encryption_keys
		WHERE org_id = ? AND project_id = ?`, orgID, projectID).Scan(&rowOrgDEKVer, &nonce, &wrapped, &projDEKVer, &projKeyGen); err != nil {
		return 0, 0, nil, mapVaultSQLiteError(err, false)
	}
	if cachedOrgV, cachedProjV, cachedKeyGen, cp, ok := s.cache.getProjectDEK(orgID, projectID, now); ok {
		if cachedOrgV == rowOrgDEKVer && cachedProjV == projDEKVer && cachedKeyGen == projKeyGen {
			return cachedOrgV, cachedProjV, cp, nil
		}
		clear(cp)
	}

	_, orgDEK, err := s.loadOrgDEKReadOnly(ctx, orgID)
	if err != nil {
		return 0, 0, nil, err
	}
	defer clear(orgDEK)

	aad, err := projectDEKWrapAAD(orgID, projectID, rowOrgDEKVer, projDEKVer)
	if err != nil {
		return 0, 0, nil, err
	}
	unwrapped, err := openAESGCM(orgDEK, aad, nonce, wrapped)
	if err != nil {
		return 0, 0, nil, err
	}
	if err := validate256BitKey(unwrapped); err != nil {
		clear(unwrapped)
		return 0, 0, nil, err
	}
	s.cache.putProjectDEKIfGen(orgID, projectID, rowOrgDEKVer, projDEKVer, projKeyGen, unwrapped, now, gen)
	return rowOrgDEKVer, projDEKVer, unwrapped, nil
}

func checkGenerationNotSupersededTx(ctx context.Context, tx *sql.Tx, scopeKey string, expectedGen int64, requestID string) error {
	var lastGen int64
	err := tx.QueryRowContext(ctx,
		`SELECT last_generation FROM vault_key_generations WHERE scope_key = ?`,
		scopeKey,
	).Scan(&lastGen)
	if err == nil && lastGen > expectedGen {
		return fmt.Errorf("%w: DEK generation superseded at %d (stale request %q targeted %d)", ErrStaleShredGeneration, lastGen, requestID, expectedGen)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return mapVaultSQLiteError(err, false)
	}
	return nil
}

func appendVaultChangelogTx(ctx context.Context, tx *sql.Tx, orgID, entityKind, entityID, mutationType string, emergency bool, now int64) (int64, error) {
	emInt := 0
	if emergency {
		emInt = 1
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, ?, ?, ?, ?, ?)`,
		orgID, entityKind, entityID, mutationType, emInt, now,
	)
	if err != nil {
		return 0, mapVaultSQLiteError(err, false)
	}
	epoch, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("vault: read config_changelog epoch: %w", err)
	}
	return epoch, nil
}

func nextKeyGenerationTx(ctx context.Context, tx *sql.Tx, scopeKey, orgID string, projectID *string, now int64) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO vault_key_generations (scope_key, org_id, project_id, last_generation, updated_at_ms)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(scope_key) DO UPDATE SET
			last_generation = vault_key_generations.last_generation + 1,
			updated_at_ms   = excluded.updated_at_ms`,
		scopeKey, orgID, nullStringPtr(projectID), now,
	); err != nil {
		return 0, mapVaultSQLiteError(err, false)
	}
	var gen int64
	if err := tx.QueryRowContext(ctx,
		`SELECT last_generation FROM vault_key_generations WHERE scope_key = ?`,
		scopeKey,
	).Scan(&gen); err != nil {
		return 0, mapVaultSQLiteError(err, false)
	}
	return gen, nil
}

func inSavepointTx(ctx context.Context, tx *sql.Tx, spName string, fn func() (keep bool, err error)) (bool, error) {
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+spName); err != nil {
		return false, mapVaultSQLiteError(err, false)
	}
	keep, err := fn()
	if err != nil || !keep {
		_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+spName)
		_, _ = tx.ExecContext(ctx, "RELEASE SAVEPOINT "+spName)
		return false, err
	}
	if _, rerr := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+spName); rerr != nil {
		return false, mapVaultSQLiteError(rerr, false)
	}
	return true, nil
}

type pendingCleanupTuple struct {
	scopeKey         string
	orgID            string
	epoch            int64
	targetGeneration int64
}

type sqlQueryExecutor interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryPendingCleanupTuples(ctx context.Context, q sqlQueryExecutor, query string, args ...any) ([]pendingCleanupTuple, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var out []pendingCleanupTuple
	for rows.Next() {
		var pt pendingCleanupTuple
		if serr := rows.Scan(&pt.scopeKey, &pt.orgID, &pt.epoch, &pt.targetGeneration); serr != nil {
			_ = rows.Close()
			return nil, serr
		}
		out = append(out, pt)
	}
	if rerr := rows.Err(); rerr != nil {
		_ = rows.Close()
		return nil, rerr
	}
	_ = rows.Close()
	return out, nil
}

func clearPendingTuplesTx(ctx context.Context, tx *sql.Tx, tuples []pendingCleanupTuple, now int64) error {
	for _, pt := range tuples {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM vault_pending_shred_cleanups WHERE scope_key = ? AND shred_epoch = ?`,
			pt.scopeKey, pt.epoch,
		); err != nil {
			return mapVaultSQLiteError(err, false)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE vault_shred_requests
			SET status = ?, updated_at_ms = ?
			WHERE org_id = ? AND scope_key = ? AND target_generation = ? AND status = ?`,
			shredStatusCompleted, now, pt.orgID, pt.scopeKey, pt.targetGeneration, shredStatusPendingCleanup,
		); err != nil {
			return mapVaultSQLiteError(err, false)
		}
	}
	return nil
}

func (s *Store) inPostShredCleanupTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	ctxTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("vault: begin post-shred cleanup tx: %w", err)
	}
	done := false
	defer func() {
		if !done {
			_ = ctxTx.Rollback()
		}
	}()
	if err := fn(ctxTx); err != nil {
		return err
	}
	if err := ctxTx.Commit(); err != nil {
		return fmt.Errorf("vault: commit post-shred cleanup tx: %w", err)
	}
	done = true
	return nil
}

func isCompletedWithoutPendingTx(ctx context.Context, tx *sql.Tx, reqStatus, scopeKey string, expectedGen int64, requestID string) (bool, error) {
	if reqStatus != shredStatusCompleted {
		return false, nil
	}
	if err := checkGenerationNotSupersededTx(ctx, tx, scopeKey, expectedGen, requestID); err != nil {
		return false, err
	}
	var marker int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM vault_pending_shred_cleanups WHERE scope_key = ? AND target_generation = ? LIMIT 1`,
		scopeKey, expectedGen,
	).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return false, mapVaultSQLiteError(err, false)
}

func lookupShredRequestTx(ctx context.Context, tx *sql.Tx, orgID, requestID, scopeKey string, expectedGen int64) (string, error) {
	var (
		recScope     string
		recTargetGen int64
		recStatus    string
	)
	err := tx.QueryRowContext(ctx,
		`SELECT scope_key, target_generation, status FROM vault_shred_requests WHERE org_id = ? AND request_id = ?`,
		orgID, requestID,
	).Scan(&recScope, &recTargetGen, &recStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", mapVaultSQLiteError(err, false)
	}
	if recScope != scopeKey || recTargetGen != expectedGen {
		return "", fmt.Errorf("%w: request_id %q already bound to %s@gen=%d (got %s@gen=%d)",
			ErrInvalidInput, requestID, recScope, recTargetGen, scopeKey, expectedGen)
	}
	return recStatus, nil
}

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func upsertShredRequestExec(ctx context.Context, exec sqlExecutor, requestID, scopeKey, orgID string, projectID *string, targetGen int64, status string, now int64) error {
	if _, err := exec.ExecContext(ctx, `
		INSERT INTO vault_shred_requests (
			org_id, request_id, scope_key, project_id, target_generation, status, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(org_id, request_id) DO UPDATE SET
			status        = excluded.status,
			updated_at_ms = excluded.updated_at_ms`,
		orgID, requestID, scopeKey, nullStringPtr(projectID), targetGen, status, now, now,
	); err != nil {
		return mapVaultSQLiteError(err, false)
	}
	return nil
}

func completeProjectShredTx(ctx context.Context, tx *sql.Tx, requestID, scopeKey, orgID string, projectID *string, targetGen, now int64) error {
	if err := upsertShredRequestExec(ctx, tx, requestID, scopeKey, orgID, projectID, targetGen, shredStatusCompleted, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE vault_shred_requests
		SET status = ?, updated_at_ms = ?
		WHERE org_id = ? AND scope_key = ? AND target_generation = ? AND status = ?`,
		shredStatusCompleted, now, orgID, scopeKey, targetGen, shredStatusPendingCleanup,
	)
	return mapVaultSQLiteError(err, false)
}

func recordPendingShredTx(ctx context.Context, tx *sql.Tx, scopeKey, orgID string, projectID *string, shredEpoch, targetGen, now int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO vault_pending_shred_cleanups (
			scope_key, org_id, project_id, shred_epoch, target_generation, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope_key) DO UPDATE SET
			shred_epoch       = excluded.shred_epoch,
			target_generation = excluded.target_generation,
			updated_at_ms     = excluded.updated_at_ms
		WHERE excluded.shred_epoch > vault_pending_shred_cleanups.shred_epoch`,
		scopeKey, orgID, nullStringPtr(projectID), shredEpoch, targetGen, now, now,
	)
	if err != nil {
		return mapVaultSQLiteError(err, false)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSecretMetadata(row rowScanner) (*SecretMetadata, error) {
	var (
		m        SecretMetadata
		projNull sql.NullString
		audNull  sql.NullString
	)
	if err := row.Scan(
		&m.ID, &m.OrgID, &projNull, &m.Name, &m.SecretKind,
		&audNull, &m.KeyFingerprint, &m.Version, &m.CreatedAtMs, &m.UpdatedAtMs,
	); err != nil {
		return nil, mapVaultSQLiteError(err, false)
	}
	if projNull.Valid {
		v := projNull.String
		m.OwnerProjectID = &v
	}
	if audNull.Valid {
		v := audNull.String
		m.BoundAudience = &v
	}
	return &m, nil
}

func nullStringPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func cloneStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

func mapVaultSQLiteError(err error, isDelete bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return port.ErrNotFound
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "UNIQUE constraint failed"):
		return fmt.Errorf("%w: %v", port.ErrAlreadyExists, err)
	case strings.Contains(msg, "FOREIGN KEY constraint failed"):
		if isDelete {
			return fmt.Errorf("%w: %v", port.ErrResourceInUse, err)
		}
		return fmt.Errorf("%w: %v", port.ErrNotFound, err)
	case strings.Contains(msg, "CHECK constraint failed"):
		return fmt.Errorf("%w: %w: %v", port.ErrInvalidInput, ErrInvalidInput, err)
	default:
		return err
	}
}
