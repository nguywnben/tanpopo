// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package catalog implements the 4-entity Provider Catalog (ProviderAdapter ->
// ProviderConnection -> LogicalModel -> ModelDeployment), multi-slot
// ConnectionSecretBinding persistence, canonical SM-02 connection lifecycle,
// and dual CredentialBindingResolver + ConnectionPolicyResolver interfaces for
// BlindHostCredentialBroker (TP-IMP-0012 / DEC-020).
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Compile-time assertions: Repository satisfies BOTH broker resolver interfaces (DEC-020).
var (
	_ broker.CredentialBindingResolver = (*Repository)(nil)
	_ broker.ConnectionPolicyResolver  = (*Repository)(nil)
)

// Canonical billing modes (DEC-004 / ADR-0005).
const (
	BillingModeCommercialAPI   = "COMMERCIAL_API"
	BillingModeSelfHostedInfra = "SELF_HOSTED_INFRA"
)

// Canonical authentication modes.
const (
	AuthModeNone             = "NONE"
	AuthModeAPIKey           = "API_KEY"
	AuthModeOAuth2           = "OAUTH2"
	AuthModeAWSSigV4         = "AWS_SIGV4"
	AuthModeWorkloadIdentity = "WORKLOAD_IDENTITY"
	AuthModeMTLS             = "MTLS"
	AuthModeCustomSign       = "CUSTOM_SIGN"
)

// Canonical SM-02 ProviderConnection states.
const (
	ConnStateDraft          = "DRAFT"
	ConnStateValidating     = "VALIDATING"
	ConnStateActive         = "ACTIVE"
	ConnStateDegraded       = "DEGRADED"
	ConnStateAuthExpired    = "AUTH_EXPIRED_REAUTH_REQUIRED"
	ConnStateSuspendedAdmin = "SUSPENDED_BY_ADMIN"
	ConnStateRevoked        = "REVOKED"
	ConnStateArchived       = "ARCHIVED"
)

// Canonical SM-04 ModelDeployment states.
const (
	DeployStateProvisioned       = "PROVISIONED"
	DeployStateActiveHealthy     = "ACTIVE_HEALTHY"
	DeployStateThrottledCooldown = "THROTTLED_COOLDOWN"
	DeployStateCircuitOpen       = "CIRCUIT_OPEN_UNHEALTHY"
	DeployStateHalfOpenProbing   = "HALF_OPEN_PROBING"
	DeployStateDraining          = "DRAINING_MAINTENANCE"
	DeployStateDisabled          = "DISABLED"
)

// Canonical LogicalModel modality kinds.
const (
	ModalityChat       = "CHAT"
	ModalityEmbedding  = "EMBEDDING"
	ModalityRerank     = "RERANK"
	ModalityMultimodal = "MULTIMODAL"
)

// Canonical Capability Assurance Tiers (SPEC-EXT-02 / TP-IMP-0012).
const (
	AssuranceDeclared = "DECLARED"
	AssuranceTested   = "TESTED"
	AssuranceVerified = "VERIFIED"
)

// Canonical secret injection modes (connection_secret_bindings.inject_mode).
const (
	InjectModeHostBlind      = "HOST_BLIND_INJECT"
	InjectModeHostCryptoSign = "HOST_CRYPTO_SIGN"
	InjectModeMTLSHandshake  = "MTLS_HANDSHAKE"
)

// Canonical capability flag names governed by Capability Assurance Tiers.
var validCapabilityFlags = map[string]struct{}{
	"streaming":          {},
	"tool_calling":       {},
	"vision":             {},
	"audio_input":        {},
	"document_input":     {},
	"strict_json_schema": {},
	"reasoning":          {},
}

// Canonical catalog error sentinels.
var (
	ErrInvalidBillingMode     = fmt.Errorf("%w: invalid_billing_mode (must be COMMERCIAL_API or SELF_HOSTED_INFRA)", port.ErrInvalidInput)
	ErrInvalidStateTransition = fmt.Errorf("%w: invalid SM-02 connection state transition", port.ErrInvalidInput)
	ErrConnectionNotReady     = fmt.Errorf("%w: connection state does not permit egress", port.ErrInvalidInput)
	ErrSnapshotSyncFailed     = errors.New("catalog: committed to database but snapshot sync failed")
)

// Config configures Repository fail-closed.
type Config struct {
	DB                        *sql.DB
	NowMs                     func() int64
	Snapshots                 *snapshot.Manager
	AllowDetachedControlPlane bool
	Audit                     port.AuditSink
}

// Repository manages CRUD, optimistic locking, capability assurance metadata,
// secret bindings, SM-02 health transitions, and BlindBroker policy/slot lookup.
type Repository struct {
	db        *sql.DB
	nowMs     func() int64
	snapshots *snapshot.Manager
	audit     port.AuditSink
}

// NewRepository validates dependencies and constructs a catalog Repository.
func NewRepository(cfg Config) (*Repository, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	if cfg.Snapshots == nil && !cfg.AllowDetachedControlPlane {
		return nil, fmt.Errorf("%w: non-nil SnapshotManager is required unless AllowDetachedControlPlane=true", port.ErrInvalidInput)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UnixMilli() }
	}
	return &Repository{
		db:        cfg.DB,
		nowMs:     nowFn,
		snapshots: cfg.Snapshots,
		audit:     cfg.Audit,
	}, nil
}

// ProviderAdapter represents row data for provider_adapters.
type ProviderAdapter struct {
	ID                string
	InstallationID    string
	AdapterSlug       string
	NativePrefix      *string
	SupportedDialects []string
	AllowedHosts      []string
	Version           int64
	CreatedAtMs       int64
}

// CreateProviderAdapterParams configures CreateProviderAdapter.
type CreateProviderAdapterParams struct {
	ID                string
	InstallationID    string
	AdapterSlug       string
	NativePrefix      *string
	SupportedDialects []string
	AllowedHosts      []string
}

// SecretBindingInput declares one slot binding in connection_secret_bindings.
type SecretBindingInput struct {
	SlotName     string
	CredentialID string
	InjectMode   string
}

// ConnectionSecretBinding represents row data for connection_secret_bindings.
type ConnectionSecretBinding struct {
	OrgID        string
	ConnectionID string
	SlotName     string
	CredentialID string
	InjectMode   string
	CreatedAtMs  int64
}

// ProviderConnection represents row data for provider_connections plus bound secret slots.
type ProviderConnection struct {
	ID               string
	OrgID            string
	OwnerProjectID   *string
	AdapterID        string
	CredentialID     *string
	Name             string
	EndpointBaseURL  string
	AllowPrivateIP   bool
	AllowedCIDRs     []string
	DataRegion       string
	BillingMode      string
	AuthMode         string
	State            string
	SharedProjectIDs []string
	ConfigJSON       string
	SecretBindings   []ConnectionSecretBinding
	Version          int64
	CreatedAtMs      int64
	UpdatedAtMs      int64
}

// CreateProviderConnectionParams configures CreateProviderConnection.
type CreateProviderConnectionParams struct {
	ID               string
	OrgID            string
	OwnerProjectID   *string
	AdapterID        string
	CredentialID     *string
	Name             string
	EndpointBaseURL  string
	AllowPrivateIP   bool
	AllowedCIDRs     []string
	DataRegion       string
	BillingMode      string
	AuthMode         string
	SharedProjectIDs []string
	SecretBindings   []SecretBindingInput
	ConfigJSON       string
}

// LogicalModel represents row data for logical_models.
type LogicalModel struct {
	ID               string
	OrgID            *string
	CanonicalSlug    string
	ModalityKind     string
	ContextWindow    int64
	MaxOutputTokens  int64
	BaseCapabilities map[string]string
	Version          int64
	CreatedAtMs      int64
	UpdatedAtMs      int64
}

// CreateLogicalModelParams configures CreateLogicalModel.
type CreateLogicalModelParams struct {
	ID               string
	OrgID            *string
	CanonicalSlug    string
	ModalityKind     string
	ContextWindow    int64
	MaxOutputTokens  int64
	BaseCapabilities map[string]string
}

// ModelDeployment represents row data for model_deployments.
type ModelDeployment struct {
	ID                  string
	OrgID               string
	ConnectionID        string
	LogicalModelID      string
	UpstreamModelName   string
	NativeDialect       *string
	CapabilityOverrides map[string]string
	AssuranceLevel      string
	Pricing             snapshot.PricingNanoUSD
	State               string
	CooldownUntilMs     *int64
	ConnectTimeoutMs    int64
	FirstByteTimeoutMs  int64
	IdleChunkTimeoutMs  int64
	Version             int64
	CreatedAtMs         int64
	UpdatedAtMs         int64
}

// CreateModelDeploymentParams configures CreateModelDeployment.
type CreateModelDeploymentParams struct {
	ID                  string
	OrgID               string
	ConnectionID        string
	LogicalModelID      string
	UpstreamModelName   string
	NativeDialect       *string
	CapabilityOverrides map[string]string
	AssuranceLevel      string
	Pricing             snapshot.PricingNanoUSD
	State               string
	ConnectTimeoutMs    int64
	FirstByteTimeoutMs  int64
	IdleChunkTimeoutMs  int64
}

// CreateProviderAdapter registers a declarative or runtime provider adapter linked to an installation.
func (r *Repository) CreateProviderAdapter(ctx context.Context, params CreateProviderAdapterParams) (*ProviderAdapter, error) {
	if strings.TrimSpace(params.InstallationID) == "" || strings.TrimSpace(params.AdapterSlug) == "" {
		return nil, fmt.Errorf("%w: installation_id and adapter_slug are required", port.ErrInvalidInput)
	}
	id, err := resolveID(params.ID, idgen.PrefixAdapter)
	if err != nil {
		return nil, err
	}
	for _, h := range params.AllowedHosts {
		if err := validateHostEntry(h); err != nil {
			return nil, err
		}
	}
	dialectsJSON, err := marshalStringSlice(params.SupportedDialects)
	if err != nil {
		return nil, err
	}
	hostsJSON, err := marshalStringSlice(params.AllowedHosts)
	if err != nil {
		return nil, err
	}
	now := r.nowMs()

	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		var installState string
		err := tx.QueryRowContext(ctx, `SELECT state FROM extension_installations WHERE id = ?`, params.InstallationID).Scan(&installState)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: extension installation %q not found", port.ErrNotFound, params.InstallationID)
		}
		if err != nil {
			return mapDBError(err)
		}
		if !isValidAdapterInstallState(installState) {
			return fmt.Errorf("%w: extension installation %q state %s cannot back an adapter", port.ErrInvalidInput, params.InstallationID, installState)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO provider_adapters (
				id, installation_id, adapter_slug, native_prefix,
				supported_dialects_json, allowed_hosts_json, version, created_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
			id, params.InstallationID, strings.TrimSpace(params.AdapterSlug), nullStrPtr(params.NativePrefix),
			dialectsJSON, hostsJSON, now,
		)
		if err != nil {
			return mapWriteDBError(err)
		}
		return appendChangelogTx(ctx, tx, nil, "provider_adapter", id, port.MutationUpsert, false, now)
	}); err != nil {
		return nil, err
	}

	if err := r.syncSnapshot(ctx); err != nil {
		return nil, err
	}
	return r.GetProviderAdapter(ctx, id)
}

// GetProviderAdapter fetches a ProviderAdapter by ID.
func (r *Repository) GetProviderAdapter(ctx context.Context, id string) (*ProviderAdapter, error) {
	var (
		out          ProviderAdapter
		nativePrefix sql.NullString
		dialectsJSON string
		hostsJSON    string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT id, installation_id, adapter_slug, native_prefix,
		       supported_dialects_json, allowed_hosts_json, version, created_at_ms
		FROM provider_adapters WHERE id = ?`, id,
	).Scan(
		&out.ID, &out.InstallationID, &out.AdapterSlug, &nativePrefix,
		&dialectsJSON, &hostsJSON, &out.Version, &out.CreatedAtMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: provider_adapter %q", port.ErrNotFound, id)
	}
	if err != nil {
		return nil, mapDBError(err)
	}
	if nativePrefix.Valid {
		v := nativePrefix.String
		out.NativePrefix = &v
	}
	out.SupportedDialects, err = decodeStringSliceStrict(dialectsJSON, "supported_dialects_json")
	if err != nil {
		return nil, err
	}
	out.AllowedHosts, err = decodeStringSliceStrict(hostsJSON, "allowed_hosts_json")
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProviderAdapter deletes an unreferenced ProviderAdapter, failing with port.ErrResourceInUse (409)
// when referenced by any ProviderConnection (NEG-3).
func (r *Repository) DeleteProviderAdapter(ctx context.Context, id string) error {
	now := r.nowMs()
	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		var inUse int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM provider_connections WHERE adapter_id = ?`, id).Scan(&inUse); err != nil {
			return mapDBError(err)
		}
		if inUse > 0 {
			return fmt.Errorf("%w: provider_adapter %q is referenced by %d provider_connections", port.ErrResourceInUse, id, inUse)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM provider_adapters WHERE id = ?`, id)
		if err != nil {
			return mapDeleteDBError(err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: provider_adapter %q", port.ErrNotFound, id)
		}
		return appendChangelogTx(ctx, tx, nil, "provider_adapter", id, port.MutationDelete, false, now)
	}); err != nil {
		return err
	}
	return r.syncSnapshot(ctx)
}

// CreateProviderConnection persists a tenant-isolated ProviderConnection in DRAFT state and
// attaches zero or more ConnectionSecretBindings.
func (r *Repository) CreateProviderConnection(ctx context.Context, params CreateProviderConnectionParams) (*ProviderConnection, error) {
	if strings.TrimSpace(params.OrgID) == "" || strings.TrimSpace(params.AdapterID) == "" || strings.TrimSpace(params.Name) == "" {
		return nil, fmt.Errorf("%w: org_id, adapter_id, and name are required", port.ErrInvalidInput)
	}
	if strings.TrimSpace(params.DataRegion) == "" {
		return nil, fmt.Errorf("%w: data_region is required", port.ErrInvalidInput)
	}
	if params.BillingMode != BillingModeCommercialAPI && params.BillingMode != BillingModeSelfHostedInfra {
		return nil, ErrInvalidBillingMode
	}
	authMode := params.AuthMode
	if authMode == "" {
		authMode = AuthModeAPIKey
	}
	if !isValidAuthMode(authMode) {
		return nil, fmt.Errorf("%w: invalid auth_mode %q", port.ErrInvalidInput, authMode)
	}

	parsedEndpoint, err := url.Parse(strings.TrimSpace(params.EndpointBaseURL))
	if err != nil || (parsedEndpoint.Scheme != "http" && parsedEndpoint.Scheme != "https") || parsedEndpoint.Hostname() == "" || parsedEndpoint.User != nil {
		return nil, fmt.Errorf("%w: invalid endpoint_base_url %q", port.ErrInvalidInput, params.EndpointBaseURL)
	}
	if err := validateCIDRs(params.AllowedCIDRs); err != nil {
		return nil, err
	}
	if params.AllowPrivateIP && len(params.AllowedCIDRs) == 0 {
		return nil, fmt.Errorf("%w: allow_private_ip=true requires non-empty allowed_cidrs", port.ErrInvalidInput)
	}

	var ownerProjPtr *string
	if params.OwnerProjectID != nil {
		if trimmed := strings.TrimSpace(*params.OwnerProjectID); trimmed != "" {
			ownerProjPtr = &trimmed
		}
	}
	sharedProjects := make([]string, 0, len(params.SharedProjectIDs))
	for _, sp := range params.SharedProjectIDs {
		if trimmed := strings.TrimSpace(sp); trimmed != "" {
			sharedProjects = append(sharedProjects, trimmed)
		}
	}

	id, err := resolveID(params.ID, idgen.PrefixConnection)
	if err != nil {
		return nil, err
	}
	cidrsJSON, err := marshalStringSlice(params.AllowedCIDRs)
	if err != nil {
		return nil, err
	}
	sharedJSON, err := marshalStringSlice(sharedProjects)
	if err != nil {
		return nil, err
	}
	cfgJSON := strings.TrimSpace(params.ConfigJSON)
	if cfgJSON == "" {
		cfgJSON = "{}"
	} else {
		var cfgObj map[string]any
		dec := json.NewDecoder(strings.NewReader(cfgJSON))
		if err := dec.Decode(&cfgObj); err != nil || cfgObj == nil {
			return nil, fmt.Errorf("%w: config_json must be a valid JSON object", port.ErrInvalidInput)
		}
		var trailing any
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: config_json must contain a single JSON object", port.ErrInvalidInput)
		}
	}

	var credIDPtr *string
	if params.CredentialID != nil {
		if trimmed := strings.TrimSpace(*params.CredentialID); trimmed != "" {
			credIDPtr = &trimmed
		}
	}

	bindings := make([]SecretBindingInput, 0, len(params.SecretBindings)+1)
	seenSlots := make(map[string]struct{}, len(params.SecretBindings)+1)
	hasDefaultSlot := false
	for _, rawB := range params.SecretBindings {
		slot := strings.TrimSpace(rawB.SlotName)
		credID := strings.TrimSpace(rawB.CredentialID)
		if slot == "" || credID == "" {
			return nil, fmt.Errorf("%w: secret binding slot_name and credential_id are required", port.ErrInvalidInput)
		}
		if _, dup := seenSlots[slot]; dup {
			return nil, fmt.Errorf("%w: duplicate secret binding slot_name %q", port.ErrInvalidInput, slot)
		}
		if slot == "api_key" && credIDPtr != nil && credID != *credIDPtr {
			return nil, fmt.Errorf("%w: credential_id %q conflicts with secret_bindings api_key %q", port.ErrInvalidInput, *credIDPtr, credID)
		}
		mode := rawB.InjectMode
		if mode == "" {
			mode = InjectModeHostBlind
		}
		if !isValidInjectMode(mode) {
			return nil, fmt.Errorf("%w: invalid inject_mode %q", port.ErrInvalidInput, mode)
		}
		seenSlots[slot] = struct{}{}
		if slot == "api_key" || (credIDPtr != nil && credID == *credIDPtr) {
			hasDefaultSlot = true
		}
		bindings = append(bindings, SecretBindingInput{
			SlotName:     slot,
			CredentialID: credID,
			InjectMode:   mode,
		})
	}
	if credIDPtr != nil && !hasDefaultSlot {
		bindings = append(bindings, SecretBindingInput{
			SlotName:     "api_key",
			CredentialID: *credIDPtr,
			InjectMode:   InjectModeHostBlind,
		})
	}
	if authMode == AuthModeNone && (credIDPtr != nil || len(bindings) > 0) {
		return nil, fmt.Errorf("%w: auth_mode=NONE cannot bind credentials or secret slots", port.ErrInvalidInput)
	}

	now := r.nowMs()
	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		var orgStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM organizations WHERE id = ?`, params.OrgID).Scan(&orgStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: organization %q not found", port.ErrNotFound, params.OrgID)
			}
			return mapDBError(err)
		}

		// Verify adapter exists & endpoint host falls inside adapter allowed_hosts when non-empty.
		var adapterHostsJSON string
		if err := tx.QueryRowContext(ctx, `SELECT allowed_hosts_json FROM provider_adapters WHERE id = ?`, params.AdapterID).Scan(&adapterHostsJSON); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: provider_adapter %q not found", port.ErrNotFound, params.AdapterID)
			}
			return mapDBError(err)
		}
		adapterHosts, err := decodeStringSliceStrict(adapterHostsJSON, "provider_adapters.allowed_hosts_json")
		if err != nil {
			return err
		}
		if !hostPermittedByAdapter(parsedEndpoint, adapterHosts) {
			return fmt.Errorf("%w: endpoint host %q rejected by adapter allowed_hosts", port.ErrInvalidInput, parsedEndpoint.Hostname())
		}

		if ownerProjPtr != nil {
			if err := ensureProjectOwnedByOrgTx(ctx, tx, params.OrgID, *ownerProjPtr); err != nil {
				return err
			}
		}
		for _, spID := range sharedProjects {
			if err := ensureProjectOwnedByOrgTx(ctx, tx, params.OrgID, spID); err != nil {
				return err
			}
		}
		if credIDPtr != nil {
			if err := ensureSecretCompatibleWithConnectionTx(ctx, tx, params.OrgID, ownerProjPtr, sharedProjects, *credIDPtr); err != nil {
				return err
			}
		}
		for _, b := range bindings {
			if err := ensureSecretCompatibleWithConnectionTx(ctx, tx, params.OrgID, ownerProjPtr, sharedProjects, b.CredentialID); err != nil {
				return err
			}
		}

		allowPrivInt := 0
		if params.AllowPrivateIP {
			allowPrivInt = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO provider_connections (
				id, org_id, owner_project_id, adapter_id, credential_id, name,
				endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region,
				billing_mode, auth_mode, state, shared_project_ids_json, config_json,
				version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			id, params.OrgID, nullStrPtr(ownerProjPtr), params.AdapterID, nullStrPtr(credIDPtr),
			strings.TrimSpace(params.Name), strings.TrimSpace(params.EndpointBaseURL), allowPrivInt, cidrsJSON,
			strings.TrimSpace(params.DataRegion), params.BillingMode, authMode, ConnStateDraft, sharedJSON, cfgJSON,
			now, now,
		); err != nil {
			return mapWriteDBError(err)
		}

		for _, b := range bindings {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO connection_secret_bindings (
					org_id, connection_id, slot_name, credential_id, inject_mode, created_at_ms
				) VALUES (?, ?, ?, ?, ?, ?)`,
				params.OrgID, id, b.SlotName, b.CredentialID, b.InjectMode, now,
			); err != nil {
				return mapWriteDBError(err)
			}
		}
		return appendChangelogTx(ctx, tx, &params.OrgID, "provider_connection", id, port.MutationUpsert, false, now)
	}); err != nil {
		return nil, err
	}

	if err := r.syncSnapshot(ctx); err != nil {
		return nil, err
	}
	return r.GetProviderConnection(ctx, params.OrgID, id)
}

// GetProviderConnection fetches a tenant-isolated ProviderConnection by (orgID, id).
func (r *Repository) GetProviderConnection(ctx context.Context, orgID, connectionID string) (*ProviderConnection, error) {
	var (
		out        ProviderConnection
		ownerProj  sql.NullString
		credID     sql.NullString
		allowPriv  int
		cidrsJSON  string
		sharedJSON string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT id, org_id, owner_project_id, adapter_id, credential_id, name,
		       endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region,
		       billing_mode, auth_mode, state, shared_project_ids_json, config_json,
		       version, created_at_ms, updated_at_ms
		FROM provider_connections
		WHERE org_id = ? AND id = ?`, orgID, connectionID,
	).Scan(
		&out.ID, &out.OrgID, &ownerProj, &out.AdapterID, &credID, &out.Name,
		&out.EndpointBaseURL, &allowPriv, &cidrsJSON, &out.DataRegion,
		&out.BillingMode, &out.AuthMode, &out.State, &sharedJSON, &out.ConfigJSON,
		&out.Version, &out.CreatedAtMs, &out.UpdatedAtMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, connectionID)
	}
	if err != nil {
		return nil, mapDBError(err)
	}
	if ownerProj.Valid {
		v := ownerProj.String
		out.OwnerProjectID = &v
	}
	if credID.Valid {
		v := credID.String
		out.CredentialID = &v
	}
	out.AllowPrivateIP = allowPriv != 0
	out.AllowedCIDRs, err = decodeStringSliceStrict(cidrsJSON, "allowed_cidrs_json")
	if err != nil {
		return nil, err
	}
	out.SharedProjectIDs, err = decodeStringSliceStrict(sharedJSON, "shared_project_ids_json")
	if err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT org_id, connection_id, slot_name, credential_id, inject_mode, created_at_ms
		FROM connection_secret_bindings
		WHERE org_id = ? AND connection_id = ?
		ORDER BY slot_name ASC`, orgID, connectionID)
	if err != nil {
		return nil, mapDBError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var b ConnectionSecretBinding
		if err := rows.Scan(&b.OrgID, &b.ConnectionID, &b.SlotName, &b.CredentialID, &b.InjectMode, &b.CreatedAtMs); err != nil {
			return nil, mapDBError(err)
		}
		out.SecretBindings = append(out.SecretBindings, b)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBError(err)
	}
	return &out, nil
}

// DeleteProviderConnection deletes an unreferenced ProviderConnection, returning port.ErrResourceInUse (409)
// if any ModelDeployment references it (NEG-3).
func (r *Repository) DeleteProviderConnection(ctx context.Context, orgID, connectionID string) error {
	now := r.nowMs()
	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		var inUse int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM model_deployments WHERE org_id = ? AND connection_id = ?`, orgID, connectionID).Scan(&inUse); err != nil {
			return mapDBError(err)
		}
		if inUse > 0 {
			return fmt.Errorf("%w: provider_connection %q is referenced by %d model_deployments", port.ErrResourceInUse, connectionID, inUse)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM provider_connections WHERE org_id = ? AND id = ?`, orgID, connectionID)
		if err != nil {
			return mapDeleteDBError(err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, connectionID)
		}
		return appendChangelogTx(ctx, tx, &orgID, "provider_connection", connectionID, port.MutationDelete, false, now)
	}); err != nil {
		return err
	}
	return r.syncSnapshot(ctx)
}

// CreateLogicalModel creates a global (OrgID == nil) or org-scoped LogicalModel with
// explicit Capability Assurance Tier tracking (`DECLARED | TESTED | VERIFIED`).
func (r *Repository) CreateLogicalModel(ctx context.Context, params CreateLogicalModelParams) (*LogicalModel, error) {
	if strings.TrimSpace(params.CanonicalSlug) == "" {
		return nil, fmt.Errorf("%w: canonical_slug is required", port.ErrInvalidInput)
	}
	if !isValidModality(params.ModalityKind) {
		return nil, fmt.Errorf("%w: invalid modality_kind %q", port.ErrInvalidInput, params.ModalityKind)
	}
	if params.ContextWindow <= 0 || params.MaxOutputTokens < 0 {
		return nil, fmt.Errorf("%w: context_window must be > 0 and max_output_tokens >= 0", port.ErrInvalidInput)
	}
	capsJSON, err := validateAndMarshalCapabilityMap(params.BaseCapabilities)
	if err != nil {
		return nil, err
	}
	id, err := resolveID(params.ID, idgen.PrefixLogicalModel)
	if err != nil {
		return nil, err
	}
	now := r.nowMs()

	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		if params.OrgID != nil {
			var orgStatus string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM organizations WHERE id = ?`, *params.OrgID).Scan(&orgStatus); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("%w: organization %q not found", port.ErrNotFound, *params.OrgID)
				}
				return mapDBError(err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO logical_models (
				id, org_id, canonical_slug, modality_kind, context_window,
				max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			id, nullStrPtr(params.OrgID), strings.TrimSpace(params.CanonicalSlug), params.ModalityKind,
			params.ContextWindow, params.MaxOutputTokens, capsJSON, now, now,
		); err != nil {
			return mapDBError(err)
		}
		return appendChangelogTx(ctx, tx, params.OrgID, "logical_model", id, port.MutationUpsert, false, now)
	}); err != nil {
		return nil, err
	}

	if err := r.syncSnapshot(ctx); err != nil {
		return nil, err
	}
	return r.GetLogicalModel(ctx, params.OrgID, id)
}

// GetLogicalModel retrieves a LogicalModel by ID within tenant scope:
// when orgID is non-nil, only models belonging to *orgID or global (org_id IS NULL) are visible;
// when orgID is nil, only global (org_id IS NULL) models are returned.
func (r *Repository) GetLogicalModel(ctx context.Context, orgID *string, id string) (*LogicalModel, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: logical_model id is required", port.ErrInvalidInput)
	}
	var (
		out      LogicalModel
		rowOrgID sql.NullString
		capsJSON string
		query    string
		args     []any
	)
	if orgID != nil && strings.TrimSpace(*orgID) != "" {
		query = `
		SELECT id, org_id, canonical_slug, modality_kind, context_window,
		       max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms
		FROM logical_models WHERE id = ? AND (org_id = ? OR org_id IS NULL)`
		args = []any{id, strings.TrimSpace(*orgID)}
	} else {
		query = `
		SELECT id, org_id, canonical_slug, modality_kind, context_window,
		       max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms
		FROM logical_models WHERE id = ? AND org_id IS NULL`
		args = []any{id}
	}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&out.ID, &rowOrgID, &out.CanonicalSlug, &out.ModalityKind, &out.ContextWindow,
		&out.MaxOutputTokens, &capsJSON, &out.Version, &out.CreatedAtMs, &out.UpdatedAtMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: logical_model %q", port.ErrNotFound, id)
	}
	if err != nil {
		return nil, mapDBError(err)
	}
	if rowOrgID.Valid {
		v := rowOrgID.String
		out.OrgID = &v
	}
	out.BaseCapabilities, err = decodeStringMapStrict(capsJSON, "base_capabilities_json")
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteLogicalModel deletes an unreferenced LogicalModel within tenant scope, returning
// port.ErrResourceInUse if deployed. When orgID is non-nil, only org-owned models of *orgID can be deleted;
// global models (org_id IS NULL) require orgID == nil.
func (r *Repository) DeleteLogicalModel(ctx context.Context, orgID *string, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: logical_model id is required", port.ErrInvalidInput)
	}
	var scopedOrgID *string
	if orgID != nil && strings.TrimSpace(*orgID) != "" {
		trimmed := strings.TrimSpace(*orgID)
		scopedOrgID = &trimmed
	}
	now := r.nowMs()
	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		var (
			ownerCheckQuery string
			ownerCheckArgs  []any
		)
		if scopedOrgID != nil {
			ownerCheckQuery = `SELECT COUNT(1) FROM logical_models WHERE id = ? AND org_id = ?`
			ownerCheckArgs = []any{id, *scopedOrgID}
		} else {
			ownerCheckQuery = `SELECT COUNT(1) FROM logical_models WHERE id = ? AND org_id IS NULL`
			ownerCheckArgs = []any{id}
		}
		var exists int
		if err := tx.QueryRowContext(ctx, ownerCheckQuery, ownerCheckArgs...).Scan(&exists); err != nil {
			return mapDBError(err)
		}
		if exists == 0 {
			return fmt.Errorf("%w: logical_model %q", port.ErrNotFound, id)
		}

		var inUse int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM model_deployments WHERE logical_model_id = ?`, id).Scan(&inUse); err != nil {
			return mapDBError(err)
		}
		if inUse > 0 {
			return fmt.Errorf("%w: logical_model %q is referenced by %d model_deployments", port.ErrResourceInUse, id, inUse)
		}

		var (
			res sql.Result
			err error
		)
		if scopedOrgID != nil {
			res, err = tx.ExecContext(ctx, `DELETE FROM logical_models WHERE id = ? AND org_id = ?`, id, *scopedOrgID)
		} else {
			res, err = tx.ExecContext(ctx, `DELETE FROM logical_models WHERE id = ? AND org_id IS NULL`, id)
		}
		if err != nil {
			return mapDeleteDBError(err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: logical_model %q", port.ErrNotFound, id)
		}
		return appendChangelogTx(ctx, tx, scopedOrgID, "logical_model", id, port.MutationDelete, false, now)
	}); err != nil {
		return err
	}
	return r.syncSnapshot(ctx)
}

// CreateModelDeployment binds a LogicalModel to a same-org ProviderConnection.
func (r *Repository) CreateModelDeployment(ctx context.Context, params CreateModelDeploymentParams) (*ModelDeployment, error) {
	if strings.TrimSpace(params.OrgID) == "" || strings.TrimSpace(params.ConnectionID) == "" || strings.TrimSpace(params.LogicalModelID) == "" || strings.TrimSpace(params.UpstreamModelName) == "" {
		return nil, fmt.Errorf("%w: org_id, connection_id, logical_model_id, and upstream_model_name are required", port.ErrInvalidInput)
	}
	assurance := params.AssuranceLevel
	if assurance == "" {
		assurance = AssuranceDeclared
	}
	if !isValidAssuranceTier(assurance) {
		return nil, fmt.Errorf("%w: invalid assurance_level %q", port.ErrInvalidInput, assurance)
	}
	state := params.State
	if state == "" {
		state = DeployStateProvisioned
	}
	if !isValidDeploymentState(state) {
		return nil, fmt.Errorf("%w: invalid deployment state %q", port.ErrInvalidInput, state)
	}
	capsJSON, err := validateAndMarshalCapabilityMap(params.CapabilityOverrides)
	if err != nil {
		return nil, err
	}
	if params.Pricing.InputPer1MNanoUSD < 0 || params.Pricing.OutputPer1MNanoUSD < 0 ||
		params.Pricing.CacheReadPer1MNanoUSD < 0 || params.Pricing.CacheWritePer1MNanoUSD < 0 ||
		params.Pricing.ReasoningPer1MNanoUSD < 0 {
		return nil, fmt.Errorf("%w: nano-USD pricing rates cannot be negative", port.ErrInvalidInput)
	}
	pricingBytes, err := json.Marshal(params.Pricing)
	if err != nil {
		return nil, err
	}
	connTO := params.ConnectTimeoutMs
	if connTO <= 0 {
		connTO = 5000
	}
	fbTO := params.FirstByteTimeoutMs
	if fbTO <= 0 {
		fbTO = 30000
	}
	idleTO := params.IdleChunkTimeoutMs
	if idleTO <= 0 {
		idleTO = 15000
	}
	id, err := resolveID(params.ID, idgen.PrefixDeployment)
	if err != nil {
		return nil, err
	}
	now := r.nowMs()

	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		var connOrg string
		if err := tx.QueryRowContext(ctx, `SELECT org_id FROM provider_connections WHERE org_id = ? AND id = ?`, params.OrgID, params.ConnectionID).Scan(&connOrg); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: provider_connection %q not found in org %q", port.ErrNotFound, params.ConnectionID, params.OrgID)
			}
			return mapDBError(err)
		}
		var modelOrg sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT org_id FROM logical_models WHERE id = ?`, params.LogicalModelID).Scan(&modelOrg); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: logical_model %q not found", port.ErrNotFound, params.LogicalModelID)
			}
			return mapDBError(err)
		}
		if modelOrg.Valid && modelOrg.String != params.OrgID {
			return fmt.Errorf("%w: logical_model %q belongs to another organization", port.ErrNotFound, params.LogicalModelID)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO model_deployments (
				id, org_id, connection_id, logical_model_id, upstream_model_name,
				native_dialect, capability_overrides_json, assurance_level,
				pricing_nano_usd_json, state, cooldown_until_ms,
				connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms,
				version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, 1, ?, ?)`,
			id, params.OrgID, params.ConnectionID, params.LogicalModelID, strings.TrimSpace(params.UpstreamModelName),
			nullStrPtr(params.NativeDialect), capsJSON, assurance,
			string(pricingBytes), state, connTO, fbTO, idleTO, now, now,
		); err != nil {
			return mapWriteDBError(err)
		}
		return appendChangelogTx(ctx, tx, &params.OrgID, "model_deployment", id, port.MutationUpsert, false, now)
	}); err != nil {
		return nil, err
	}

	if err := r.syncSnapshot(ctx); err != nil {
		return nil, err
	}
	return r.GetModelDeployment(ctx, params.OrgID, id)
}

const modelDeploymentSelectSQL = `
	SELECT id, org_id, connection_id, logical_model_id, upstream_model_name,
	       native_dialect, capability_overrides_json, assurance_level,
	       pricing_nano_usd_json, state, cooldown_until_ms,
	       connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms,
	       version, created_at_ms, updated_at_ms
	FROM model_deployments WHERE org_id = ? AND id = ?`

// GetModelDeployment fetches a tenant-isolated ModelDeployment by (orgID, deploymentID).
func (r *Repository) GetModelDeployment(ctx context.Context, orgID, deploymentID string) (*ModelDeployment, error) {
	return scanModelDeployment(r.db.QueryRowContext(ctx, modelDeploymentSelectSQL, orgID, deploymentID), deploymentID)
}

// UpdateModelDeployment updates mutable deployment attributes (AssuranceLevel, CapabilityOverrides,
// State, CooldownUntilMs, Pricing, timeouts) under an optimistic version lock.
type UpdateModelDeploymentParams struct {
	ExpectedVersion     int64
	AssuranceLevel      *string
	CapabilityOverrides map[string]string
	Pricing             *snapshot.PricingNanoUSD
	State               *string
	CooldownUntilMs     *int64
	ClearCooldown       bool
}

// UpdateModelDeployment updates a tenant-scoped ModelDeployment optimistically and refreshes snapshots.
func (r *Repository) UpdateModelDeployment(ctx context.Context, orgID, deploymentID string, params UpdateModelDeploymentParams) (*ModelDeployment, error) {
	if params.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: expected_version >= 1 required", port.ErrInvalidInput)
	}
	now := r.nowMs()
	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := r.getDeploymentTx(ctx, tx, orgID, deploymentID)
		if err != nil {
			return err
		}
		if cur.Version != params.ExpectedVersion {
			return fmt.Errorf("%w: model_deployment %q version=%d expected=%d", port.ErrVersionConflict, deploymentID, cur.Version, params.ExpectedVersion)
		}
		assurance := cur.AssuranceLevel
		if params.AssuranceLevel != nil {
			if !isValidAssuranceTier(*params.AssuranceLevel) {
				return fmt.Errorf("%w: invalid assurance_level %q", port.ErrInvalidInput, *params.AssuranceLevel)
			}
			assurance = *params.AssuranceLevel
		}
		capsMap := cur.CapabilityOverrides
		if params.CapabilityOverrides != nil {
			capsMap = params.CapabilityOverrides
		}
		capsJSON, err := validateAndMarshalCapabilityMap(capsMap)
		if err != nil {
			return err
		}
		pricing := cur.Pricing
		if params.Pricing != nil {
			if params.Pricing.InputPer1MNanoUSD < 0 || params.Pricing.OutputPer1MNanoUSD < 0 ||
				params.Pricing.CacheReadPer1MNanoUSD < 0 || params.Pricing.CacheWritePer1MNanoUSD < 0 ||
				params.Pricing.ReasoningPer1MNanoUSD < 0 {
				return fmt.Errorf("%w: nano-USD pricing rates cannot be negative", port.ErrInvalidInput)
			}
			pricing = *params.Pricing
		}
		pricingBytes, err := json.Marshal(pricing)
		if err != nil {
			return err
		}
		state := cur.State
		if params.State != nil {
			if !isValidDeploymentState(*params.State) {
				return fmt.Errorf("%w: invalid deployment state %q", port.ErrInvalidInput, *params.State)
			}
			state = *params.State
		}
		cooldown := cur.CooldownUntilMs
		if params.ClearCooldown {
			cooldown = nil
		} else if params.CooldownUntilMs != nil {
			cooldown = params.CooldownUntilMs
		}
		var cooldownArg any
		if cooldown != nil {
			cooldownArg = *cooldown
		}

		res, err := tx.ExecContext(ctx, `
			UPDATE model_deployments
			SET assurance_level = ?, capability_overrides_json = ?, pricing_nano_usd_json = ?,
			    state = ?, cooldown_until_ms = ?, version = version + 1, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND version = ?`,
			assurance, capsJSON, string(pricingBytes), state, cooldownArg, now,
			orgID, deploymentID, params.ExpectedVersion,
		)
		if err != nil {
			return mapDBError(err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: concurrent update on model_deployment %q", port.ErrVersionConflict, deploymentID)
		}
		return appendChangelogTx(ctx, tx, &orgID, "model_deployment", deploymentID, port.MutationUpsert, false, now)
	}); err != nil {
		return nil, err
	}
	if err := r.syncSnapshot(ctx); err != nil {
		return nil, err
	}
	return r.GetModelDeployment(ctx, orgID, deploymentID)
}

func (r *Repository) getDeploymentTx(ctx context.Context, tx *sql.Tx, orgID, deploymentID string) (*ModelDeployment, error) {
	return scanModelDeployment(tx.QueryRowContext(ctx, modelDeploymentSelectSQL, orgID, deploymentID), deploymentID)
}

func scanModelDeployment(row interface{ Scan(dest ...any) error }, deploymentID string) (*ModelDeployment, error) {
	var (
		out           ModelDeployment
		nativeDialect sql.NullString
		capsJSON      string
		pricingJSON   string
		cooldown      sql.NullInt64
	)
	err := row.Scan(
		&out.ID, &out.OrgID, &out.ConnectionID, &out.LogicalModelID, &out.UpstreamModelName,
		&nativeDialect, &capsJSON, &out.AssuranceLevel,
		&pricingJSON, &out.State, &cooldown,
		&out.ConnectTimeoutMs, &out.FirstByteTimeoutMs, &out.IdleChunkTimeoutMs,
		&out.Version, &out.CreatedAtMs, &out.UpdatedAtMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: model_deployment %q", port.ErrNotFound, deploymentID)
	}
	if err != nil {
		return nil, mapDBError(err)
	}
	if nativeDialect.Valid {
		v := nativeDialect.String
		out.NativeDialect = &v
	}
	if cooldown.Valid {
		v := cooldown.Int64
		out.CooldownUntilMs = &v
	}
	out.CapabilityOverrides, err = decodeStringMapStrict(capsJSON, "capability_overrides_json")
	if err != nil {
		return nil, err
	}
	out.Pricing, err = decodePricingNanoUSDStrict(pricingJSON)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func decodePricingNanoUSDStrict(raw string) (snapshot.PricingNanoUSD, error) {
	trimmed := strings.TrimSpace(raw)
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil || obj == nil {
		return snapshot.PricingNanoUSD{}, fmt.Errorf("%w: corrupt pricing_nano_usd_json: must be a JSON object", port.ErrInvalidInput)
	}
	var pricing snapshot.PricingNanoUSD
	if err := json.Unmarshal([]byte(trimmed), &pricing); err != nil {
		return snapshot.PricingNanoUSD{}, fmt.Errorf("%w: corrupt pricing_nano_usd_json: %v", port.ErrInvalidInput, err)
	}
	if pricing.InputPer1MNanoUSD < 0 || pricing.OutputPer1MNanoUSD < 0 ||
		pricing.CacheReadPer1MNanoUSD < 0 || pricing.CacheWritePer1MNanoUSD < 0 ||
		pricing.ReasoningPer1MNanoUSD < 0 {
		return snapshot.PricingNanoUSD{}, fmt.Errorf("%w: corrupt pricing_nano_usd_json: negative rate", port.ErrInvalidInput)
	}
	return pricing, nil
}

// DeleteModelDeployment deletes a ModelDeployment within orgID.
func (r *Repository) DeleteModelDeployment(ctx context.Context, orgID, deploymentID string) error {
	now := r.nowMs()
	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM model_deployments WHERE org_id = ? AND id = ?`, orgID, deploymentID)
		if err != nil {
			return mapDeleteDBError(err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: model_deployment %q", port.ErrNotFound, deploymentID)
		}
		return appendChangelogTx(ctx, tx, &orgID, "model_deployment", deploymentID, port.MutationDelete, false, now)
	}); err != nil {
		return err
	}
	return r.syncSnapshot(ctx)
}

// ResolveConnectionPolicy implements broker.ConnectionPolicyResolver fail-closed for both
// credentialed and zero-slot (auth_mode=NONE) connections (DEC-020).
func (r *Repository) ResolveConnectionPolicy(ctx context.Context, orgID, connectionID string) (*egress.Policy, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(connectionID) == "" {
		return nil, fmt.Errorf("%w: orgID and connectionID are required", port.ErrInvalidInput)
	}
	var (
		endpointStr      string
		allowPriv        int
		cidrsJSON        string
		state            string
		adapterHostsJSON string
		installState     string
		oepEnabled       sql.NullInt64
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT pc.endpoint_base_url, pc.allow_private_ip, pc.allowed_cidrs_json, pc.state,
		       pa.allowed_hosts_json, ei.state, oep.enabled
		FROM provider_connections pc
		JOIN provider_adapters pa ON pa.id = pc.adapter_id
		JOIN extension_installations ei ON ei.id = pa.installation_id
		LEFT JOIN org_extension_policies oep ON oep.org_id = pc.org_id AND oep.package_id = ei.package_id
		WHERE pc.org_id = ? AND pc.id = ?`, orgID, connectionID,
	).Scan(&endpointStr, &allowPriv, &cidrsJSON, &state, &adapterHostsJSON, &installState, &oepEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, connectionID)
	}
	if err != nil {
		return nil, mapDBError(err)
	}
	if !isUsableExtensionState(installState) {
		return nil, fmt.Errorf("%w: adapter extension installation state %q is not active", ErrConnectionNotReady, installState)
	}
	if oepEnabled.Valid && oepEnabled.Int64 == 0 {
		return nil, fmt.Errorf("%w: adapter extension is disabled by organization policy", ErrConnectionNotReady)
	}
	allowedCIDRs, err := decodeStringSliceStrict(cidrsJSON, "allowed_cidrs_json")
	if err != nil {
		return nil, err
	}
	adapterHosts, err := decodeStringSliceStrict(adapterHostsJSON, "adapter_allowed_hosts_json")
	if err != nil {
		return nil, err
	}
	return buildAuthoritativeEgressPolicy(endpointStr, allowPriv != 0, allowedCIDRs, adapterHosts, state)
}

// ResolveSlot implements broker.CredentialBindingResolver fail-closed without ever reading
// or returning plaintext or ciphertext secret bytes (SPEC-SEC-03).
func (r *Repository) ResolveSlot(ctx context.Context, orgID, connectionID, slotName string) (*broker.ResolvedSlot, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(connectionID) == "" || strings.TrimSpace(slotName) == "" {
		return nil, fmt.Errorf("%w: orgID, connectionID, and slotName are required", port.ErrInvalidInput)
	}
	var (
		endpointStr      string
		allowPriv        int
		cidrsJSON        string
		state            string
		primaryCredID    sql.NullString
		connOwnerProj    sql.NullString
		sharedProjJSON   string
		adapterHostsJSON string
		installState     string
		oepEnabled       sql.NullInt64
		slotCredID       sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT pc.endpoint_base_url, pc.allow_private_ip, pc.allowed_cidrs_json, pc.state, pc.credential_id,
		       pc.owner_project_id, pc.shared_project_ids_json, pa.allowed_hosts_json, ei.state, oep.enabled, csb.credential_id
		FROM provider_connections pc
		JOIN provider_adapters pa ON pa.id = pc.adapter_id
		JOIN extension_installations ei ON ei.id = pa.installation_id
		LEFT JOIN org_extension_policies oep ON oep.org_id = pc.org_id AND oep.package_id = ei.package_id
		LEFT JOIN connection_secret_bindings csb
		       ON csb.org_id = pc.org_id AND csb.connection_id = pc.id AND csb.slot_name = ?
		WHERE pc.org_id = ? AND pc.id = ?`,
		slotName, orgID, connectionID,
	).Scan(&endpointStr, &allowPriv, &cidrsJSON, &state, &primaryCredID,
		&connOwnerProj, &sharedProjJSON, &adapterHostsJSON, &installState, &oepEnabled, &slotCredID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, connectionID)
	}
	if err != nil {
		return nil, mapDBError(err)
	}
	if !isUsableExtensionState(installState) {
		return nil, fmt.Errorf("%w: adapter extension installation state %q is not active", ErrConnectionNotReady, installState)
	}
	if oepEnabled.Valid && oepEnabled.Int64 == 0 {
		return nil, fmt.Errorf("%w: adapter extension is disabled by organization policy", ErrConnectionNotReady)
	}

	credID := ""
	if slotCredID.Valid && slotCredID.String != "" {
		credID = slotCredID.String
	} else if slotName == "api_key" && primaryCredID.Valid && primaryCredID.String != "" {
		credID = primaryCredID.String
	} else {
		return nil, fmt.Errorf("%w: secret slot %q not bound on connection %q", port.ErrNotFound, slotName, connectionID)
	}

	var (
		boundAudience   sql.NullString
		secretOwnerProj sql.NullString
	)
	if err := r.db.QueryRowContext(ctx, `
		SELECT bound_audience, owner_project_id FROM secret_credentials WHERE org_id = ? AND id = ?`,
		orgID, credID,
	).Scan(&boundAudience, &secretOwnerProj); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: bound secret_credential %q not found", port.ErrNotFound, credID)
		}
		return nil, mapDBError(err)
	}

	sharedProjects, err := decodeStringSliceStrict(sharedProjJSON, "shared_project_ids_json")
	if err != nil {
		return nil, err
	}
	if secretOwnerProj.Valid {
		if !connOwnerProj.Valid || connOwnerProj.String != secretOwnerProj.String || len(sharedProjects) > 0 {
			return nil, fmt.Errorf("%w: project-owned secret %q cannot be resolved on org-shared or cross-project connection",
				ErrConnectionNotReady, credID)
		}
	}

	allowedCIDRs, err := decodeStringSliceStrict(cidrsJSON, "allowed_cidrs_json")
	if err != nil {
		return nil, err
	}
	adapterHosts, err := decodeStringSliceStrict(adapterHostsJSON, "adapter_allowed_hosts_json")
	if err != nil {
		return nil, err
	}
	policy, err := buildAuthoritativeEgressPolicy(endpointStr, allowPriv != 0, allowedCIDRs, adapterHosts, state)
	if err != nil {
		return nil, err
	}

	var audPtr *string
	if boundAudience.Valid && boundAudience.String != "" {
		v := boundAudience.String
		audPtr = &v
	}

	return &broker.ResolvedSlot{
		OrgID:          orgID,
		ConnectionID:   connectionID,
		SlotName:       slotName,
		SecretID:       credID,
		BoundAudience:  audPtr,
		AllowedHosts:   policy.AllowedHosts,
		AllowPrivateIP: policy.AllowPrivateIP,
		AllowedCIDRs:   policy.AllowedCIDRs,
		AllowedPorts:   policy.AllowedPorts,
		RequireTLS:     policy.RequireTLS,
	}, nil
}

func buildAuthoritativeEgressPolicy(endpointStr string, allowPrivateIP bool, allowedCIDRs, adapterHosts []string, state string) (*egress.Policy, error) {
	switch state {
	case ConnStateValidating, ConnStateActive, ConnStateDegraded:
	default:
		return nil, fmt.Errorf("%w: connection state %q is blocked from egress", ErrConnectionNotReady, state)
	}
	u, err := url.Parse(strings.TrimSpace(endpointStr))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("%w: malformed endpoint_base_url %q", port.ErrInvalidInput, endpointStr)
	}
	if !hostPermittedByAdapter(u, adapterHosts) {
		return nil, fmt.Errorf("%w: endpoint host %q denied by adapter allowed_hosts", port.ErrInvalidInput, u.Hostname())
	}
	return buildEgressPolicyFromURL(endpointStr, allowPrivateIP, allowedCIDRs)
}

func buildEgressPolicyFromURL(endpointStr string, allowPrivateIP bool, allowedCIDRs []string) (*egress.Policy, error) {
	if allowPrivateIP && len(allowedCIDRs) == 0 {
		return nil, fmt.Errorf("%w: allow_private_ip=true requires non-empty allowed_cidrs", port.ErrInvalidInput)
	}
	u, err := url.Parse(strings.TrimSpace(endpointStr))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("%w: malformed endpoint_base_url %q", port.ErrInvalidInput, endpointStr)
	}
	portNum := 80
	if u.Scheme == "https" {
		portNum = 443
	}
	if pStr := u.Port(); pStr != "" {
		parsedPort, perr := strconv.Atoi(pStr)
		if perr != nil || parsedPort < 1 || parsedPort > 65535 {
			return nil, fmt.Errorf("%w: invalid endpoint port %q", port.ErrInvalidInput, pStr)
		}
		portNum = parsedPort
	}
	return &egress.Policy{
		AllowedHosts:   []string{strings.ToLower(u.Hostname())},
		AllowPrivateIP: allowPrivateIP,
		AllowedCIDRs:   append([]string(nil), allowedCIDRs...),
		AllowedPorts:   []int{portNum},
		RequireTLS:     u.Scheme == "https",
	}, nil
}

func (r *Repository) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return mapDBError(err)
	}
	committed = true
	return nil
}

func (r *Repository) syncSnapshot(ctx context.Context) error {
	if r.snapshots == nil {
		return nil
	}
	if err := r.snapshots.Refresh(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrSnapshotSyncFailed, err)
	}
	return nil
}

func appendChangelogTx(ctx context.Context, tx *sql.Tx, orgID *string, entityKind, entityID, mutationType string, emergency bool, now int64) error {
	emInt := 0
	if emergency {
		emInt = 1
	}
	var orgArg any
	if orgID != nil && *orgID != "" {
		orgArg = *orgID
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, ?, ?, ?, ?, ?)`,
		orgArg, entityKind, entityID, mutationType, emInt, now,
	)
	return mapDBError(err)
}

func ensureProjectOwnedByOrgTx(ctx context.Context, tx *sql.Tx, orgID, projectID string) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM projects WHERE org_id = ? AND id = ?`, orgID, projectID).Scan(&exists); err != nil {
		return mapDBError(err)
	}
	if exists == 0 {
		return fmt.Errorf("%w: project %q not found in organization %q", port.ErrNotFound, projectID, orgID)
	}
	return nil
}

func ensureSecretCompatibleWithConnectionTx(
	ctx context.Context, tx *sql.Tx, orgID string,
	connOwnerProjectID *string, connSharedProjectIDs []string, secretID string,
) error {
	var secretOwnerProj sql.NullString
	err := tx.QueryRowContext(ctx,
		`SELECT owner_project_id FROM secret_credentials WHERE org_id = ? AND id = ?`,
		orgID, secretID,
	).Scan(&secretOwnerProj)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: secret_credential %q not found in organization %q", port.ErrNotFound, secretID, orgID)
	}
	if err != nil {
		return mapDBError(err)
	}
	if !secretOwnerProj.Valid {
		return nil
	}
	if connOwnerProjectID == nil || *connOwnerProjectID != secretOwnerProj.String || len(connSharedProjectIDs) > 0 {
		return fmt.Errorf("%w: project-owned secret %q cannot be bound to org-shared or cross-project connection",
			port.ErrInvalidInput, secretID)
	}
	return nil
}

func resolveID(callerID, prefix string) (string, error) {
	trimmed := strings.TrimSpace(callerID)
	if trimmed == "" {
		return idgen.New(prefix)
	}
	if strings.ContainsAny(trimmed, " \t\r\n\x00") || len(trimmed) > 40 || !strings.HasPrefix(trimmed, prefix+"_") {
		return "", fmt.Errorf("%w: id %q must start with %q_ and be <= 40 chars", port.ErrInvalidInput, callerID, prefix)
	}
	return trimmed, nil
}

func validateHostEntry(raw string) error {
	h := strings.TrimSpace(raw)
	if h == "" || strings.ContainsAny(h, "*?/@\\ \t\r\n\x00") {
		return fmt.Errorf("%w: invalid allowed_hosts entry %q", port.ErrInvalidInput, raw)
	}
	return nil
}

func hostPermittedByAdapter(u *url.URL, adapterHosts []string) bool {
	if len(adapterHosts) == 0 {
		return true
	}
	hostname := strings.ToLower(u.Hostname())
	hostPort := strings.ToLower(u.Host)
	for _, ah := range adapterHosts {
		norm := strings.ToLower(strings.TrimSpace(ah))
		if norm == hostname || norm == hostPort {
			return true
		}
	}
	return false
}

func validateCIDRs(cidrs []string) error {
	for _, c := range cidrs {
		pfx, err := netip.ParsePrefix(strings.TrimSpace(c))
		if err != nil {
			return fmt.Errorf("%w: invalid allowed_cidrs entry %q: %v", port.ErrInvalidInput, c, err)
		}
		if pfx.Bits() == 0 {
			return fmt.Errorf("%w: unrestricted 0.0.0.0/0 or ::/0 CIDR is forbidden", port.ErrInvalidInput)
		}
	}
	return nil
}

func validateAndMarshalCapabilityMap(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	clean := make(map[string]string, len(m))
	for k, v := range m {
		if _, ok := validCapabilityFlags[k]; !ok {
			return "", fmt.Errorf("%w: unknown capability key %q", port.ErrInvalidInput, k)
		}
		if !isValidAssuranceTier(v) {
			return "", fmt.Errorf("%w: invalid capability assurance tier %q for %q", port.ErrInvalidInput, v, k)
		}
		clean[k] = v
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func isValidAssuranceTier(t string) bool {
	switch t {
	case AssuranceDeclared, AssuranceTested, AssuranceVerified:
		return true
	default:
		return false
	}
}

func isValidModality(m string) bool {
	switch m {
	case ModalityChat, ModalityEmbedding, ModalityRerank, ModalityMultimodal:
		return true
	default:
		return false
	}
}

func isValidAuthMode(m string) bool {
	switch m {
	case AuthModeNone, AuthModeAPIKey, AuthModeOAuth2, AuthModeAWSSigV4, AuthModeWorkloadIdentity, AuthModeMTLS, AuthModeCustomSign:
		return true
	default:
		return false
	}
}

func isValidInjectMode(m string) bool {
	switch m {
	case InjectModeHostBlind, InjectModeHostCryptoSign, InjectModeMTLSHandshake:
		return true
	default:
		return false
	}
}

func isValidDeploymentState(s string) bool {
	switch s {
	case DeployStateProvisioned, DeployStateActiveHealthy, DeployStateThrottledCooldown,
		DeployStateCircuitOpen, DeployStateHalfOpenProbing, DeployStateDraining, DeployStateDisabled:
		return true
	default:
		return false
	}
}

func isValidAdapterInstallState(s string) bool {
	switch s {
	case "ENABLED_ACTIVE", "INSTALLED_DISABLED", "UPDATING_PENDING_RECONSENT", "ROLLED_BACK":
		return true
	default:
		return false
	}
}

func isUsableExtensionState(s string) bool {
	switch s {
	case "ENABLED_ACTIVE", "ROLLED_BACK", "UPDATING_PENDING_RECONSENT":
		return true
	default:
		return false
	}
}

func marshalStringSlice(items []string) (string, error) {
	if len(items) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeStringSliceStrict(raw, fieldName string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	if trimmed == "null" {
		return nil, fmt.Errorf("%w: corrupt %s JSON (unexpected null)", port.ErrInvalidInput, fieldName)
	}
	var out []string
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, fmt.Errorf("%w: corrupt %s JSON: %v", port.ErrInvalidInput, fieldName, err)
	}
	return out, nil
}

func decodeStringMapStrict(raw, fieldName string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return map[string]string{}, nil
	}
	if trimmed == "null" {
		return nil, fmt.Errorf("%w: corrupt %s JSON (unexpected null)", port.ErrInvalidInput, fieldName)
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil || out == nil {
		return nil, fmt.Errorf("%w: corrupt %s JSON: %v", port.ErrInvalidInput, fieldName, err)
	}
	return out, nil
}

func nullStrPtr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func mapDBError(err error) error {
	return mapDBErrorWithMode(err, false)
}

func mapWriteDBError(err error) error {
	return mapDBErrorWithMode(err, false)
}

func mapDeleteDBError(err error) error {
	return mapDBErrorWithMode(err, true)
}

func mapDBErrorWithMode(err error, onDelete bool) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "UNIQUE constraint failed"):
		return fmt.Errorf("%w: %v", port.ErrAlreadyExists, err)
	case strings.Contains(msg, "FOREIGN KEY constraint failed"):
		if onDelete {
			return fmt.Errorf("%w: %v", port.ErrResourceInUse, err)
		}
		return fmt.Errorf("%w: referenced foreign key missing (%v)", port.ErrNotFound, err)
	case strings.Contains(msg, "CHECK constraint failed") && strings.Contains(msg, "billing_mode"):
		return fmt.Errorf("%w: %v", ErrInvalidBillingMode, err)
	case strings.Contains(msg, "CHECK constraint failed"):
		return fmt.Errorf("%w: %v", port.ErrInvalidInput, err)
	default:
		return err
	}
}
