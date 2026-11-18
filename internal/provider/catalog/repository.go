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
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/trust"
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
	InjectModeHostBlind      = broker.InjectModeHostBlind
	InjectModeHostCryptoSign = broker.InjectModeHostCryptoSign
	InjectModeMTLSHandshake  = "MTLS_HANDSHAKE"
)

// Canonical capability flag names governed by Capability Assurance Tiers.
var validCapabilityFlags = map[string]struct{}{
	"streaming":           {},
	"tool_calling":        {},
	"parallel_tool_calls": {},
	"strict_json_schema":  {},
	"json_object":         {},
	"vision":              {},
	"audio_input":         {},
	"document_input":      {},
	"reasoning":           {},
	"prompt_caching":      {},
	"embeddings":          {},
	"rerank":              {},
	"custom_dimensions":   {},
}

// Canonical catalog error sentinels.
var (
	ErrInvalidBillingMode     = fmt.Errorf("%w: invalid_billing_mode (must be COMMERCIAL_API or SELF_HOSTED_INFRA)", port.ErrInvalidInput)
	ErrInvalidStateTransition = fmt.Errorf("%w: invalid SM-02 connection state transition", port.ErrInvalidInput)
	ErrConnectionNotReady     = fmt.Errorf("%w: connection state does not permit egress", port.ErrInvalidInput)
	ErrSnapshotSyncFailed     = errors.New("catalog: committed to database but snapshot sync failed")
)

// SnapshotRefresher refreshes in-memory serving snapshots synchronously.
type SnapshotRefresher interface {
	Refresh(context.Context) error
}

// Config configures Repository fail-closed.
type Config struct {
	DB                *sql.DB
	NowMs             func() int64
	Snapshots         *snapshot.Manager
	SnapshotRefresher SnapshotRefresher
	// Keyring is the same trusted key configuration used by the extension installer.
	Keyring                   installer.TrustedKeyring
	AllowDetachedControlPlane bool
	Audit                     port.AuditSink
}

// Repository manages CRUD, optimistic locking, capability assurance metadata,
// secret bindings, SM-02 health transitions, and BlindBroker policy/slot lookup.
type Repository struct {
	db        *sql.DB
	nowMs     func() int64
	snapshots *snapshot.Manager
	snapSync  SnapshotRefresher
	keyring   installer.TrustedKeyring
	audit     port.TxAuditSink
}

// NewRepository validates dependencies and constructs a catalog Repository.
func NewRepository(cfg Config) (*Repository, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	if cfg.Audit == nil {
		return nil, fmt.Errorf("%w: non-nil port.TxAuditSink is required", port.ErrInvalidInput)
	}
	txAudit, ok := cfg.Audit.(port.TxAuditSink)
	if !ok {
		return nil, fmt.Errorf("%w: Audit must implement port.TxAuditSink", port.ErrInvalidInput)
	}
	syncer := cfg.SnapshotRefresher
	if syncer == nil && cfg.Snapshots != nil {
		syncer = cfg.Snapshots
	}
	if syncer == nil && !cfg.AllowDetachedControlPlane {
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
		snapSync:  syncer,
		keyring:   cfg.Keyring,
		audit:     txAudit,
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
	ExpectedActiveVersionID string
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

// UpdateProviderConnectionParams configures UpdateProviderConnection.
type UpdateProviderConnectionParams struct {
	OrgID           string
	ConnectionID    string
	ExpectedVersion int64
	Name            *string
	EndpointBaseURL *string
	AllowPrivateIP  *bool
	AllowedCIDRs    *[]string
	State           *string
	CredentialID    *string
	AuthMode        *string
	ReplaceBindings bool
	MergeBindings   bool
	SecretBindings  []SecretBindingInput
	AuditActorKind  string
	AuditActorID    string
	RequestID       string
	Emergency       bool
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

// CreateProviderAdapter registers a declarative or runtime provider adapter linked to an installation,
// strictly constrained by SM-01 approved permissions on the installation's active version.
func (r *Repository) CreateProviderAdapter(ctx context.Context, params CreateProviderAdapterParams) (*ProviderAdapter, error) {
	if strings.TrimSpace(params.InstallationID) == "" || strings.TrimSpace(params.AdapterSlug) == "" {
		return nil, fmt.Errorf("%w: installation_id and adapter_slug are required", port.ErrInvalidInput)
	}
	if len(params.AllowedHosts) == 0 {
		return nil, fmt.Errorf("%w: allowed_hosts must not be empty (default-deny CT-14)", port.ErrInvalidInput)
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
		gov, err := loadAdapterExtensionGovernance(ctx, tx, params.InstallationID)
		if err != nil {
			return err
		}
		if !isValidAdapterInstallState(gov.InstallState) {
			return fmt.Errorf("%w: extension installation %q state %s cannot back an adapter", port.ErrInvalidInput, params.InstallationID, gov.InstallState)
		}
		if gov.ActiveVersionID == "" {
			return fmt.Errorf("%w: extension installation %q has no active SM-01 version", port.ErrInvalidInput, params.InstallationID)
		}
		if params.ExpectedActiveVersionID != "" && gov.ActiveVersionID != params.ExpectedActiveVersionID {
			return port.ErrVersionConflict
		}
		if params.ExpectedActiveVersionID != "" {
			var manifestJSON string
			if err := tx.QueryRowContext(ctx, `SELECT manifest_json FROM extension_versions WHERE id = ? AND package_id = (SELECT package_id FROM extension_installations WHERE id = ?)`, gov.ActiveVersionID, params.InstallationID).Scan(&manifestJSON); err != nil {
				return mapDBError(err)
			}
			var manifest installer.ExtensionManifest
			if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
				return fmt.Errorf("%w: active provider adapter manifest is invalid", port.ErrInvalidInput)
			}
			declared := false
			for _, capability := range manifest.Capabilities {
				if capability.Type != "provider_adapter" || capability.AdapterSlug != params.AdapterSlug {
					continue
				}
				declaredPrefix := strings.TrimSpace(capability.NativePassthroughPrefix)
				requestedPrefix := ""
				if params.NativePrefix != nil {
					requestedPrefix = strings.TrimSpace(*params.NativePrefix)
				}
				if declaredPrefix != requestedPrefix || !sameStrings(capability.SupportedDialects, params.SupportedDialects) {
					return fmt.Errorf("%w: adapter metadata does not match its active manifest capability", port.ErrInvalidInput)
				}
				declared = true
				break
			}
			if !declared {
				return fmt.Errorf("%w: adapter slug is not declared by the active manifest", port.ErrInvalidInput)
			}
		}
		approved, err := decodeApprovedPermissionsStrict(gov.ApprovedPermissionsJSON)
		if err != nil {
			return fmt.Errorf("%w: %w", port.ErrInvalidInput, err)
		}
		for _, reqHost := range params.AllowedHosts {
			if !hostPatternPermittedByApproved(reqHost, approved.AllowedHosts) {
				return fmt.Errorf("%w: adapter allowed_host %q exceeds SM-01 approved networkEgress.allowedHosts", port.ErrInvalidInput, reqHost)
			}
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
		if err := appendChangelogTx(ctx, tx, nil, "provider_adapter", id, port.MutationUpsert, false, now); err != nil {
			return err
		}
		return r.emitAuditTx(ctx, tx, nil, "provider_adapter.created", "provider_adapter", id, map[string]any{
			"adapter_slug":    strings.TrimSpace(params.AdapterSlug),
			"installation_id": params.InstallationID,
		}, now)
	}); err != nil {
		return nil, err
	}

	if err := r.syncSnapshot(ctx); err != nil {
		return nil, err
	}
	return r.GetProviderAdapter(ctx, id)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
		if err := appendChangelogTx(ctx, tx, nil, "provider_adapter", id, port.MutationDelete, false, now); err != nil {
			return err
		}
		return r.emitAuditTx(ctx, tx, nil, "provider_adapter.deleted", "provider_adapter", id, nil, now)
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
		adapterHosts, gov, err := loadConnectionExtensionGovernanceByAdapter(ctx, tx, params.OrgID, params.AdapterID)
		if err != nil {
			return err
		}
		gov.HasChildProjectScope = ownerProjPtr != nil || len(sharedProjects) > 0
		if err := r.validateOrgAndActiveExtensionTrust(gov, false, port.ErrInvalidInput); err != nil {
			return err
		}
		approved, err := decodeApprovedPermissionsStrict(gov.ApprovedPermissionsJSON)
		if err != nil {
			return fmt.Errorf("%w: %w", port.ErrInvalidInput, err)
		}
		if err := validateEndpointAndPermissions(parsedEndpoint, params.AllowPrivateIP, adapterHosts, approved, port.ErrInvalidInput); err != nil {
			return err
		}
		for _, b := range bindings {
			if err := slotPermittedByApproved(b.SlotName, b.InjectMode, approved, port.ErrInvalidInput); err != nil {
				return err
			}
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
		if err := appendChangelogTx(ctx, tx, &params.OrgID, "provider_connection", id, port.MutationUpsert, false, now); err != nil {
			return err
		}
		return r.emitAuditTx(ctx, tx, &params.OrgID, "provider_connection.created", "provider_connection", id, map[string]any{
			"adapter_id":   params.AdapterID,
			"billing_mode": params.BillingMode,
			"auth_mode":    authMode,
			"state":        ConnStateDraft,
		}, now)
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

// UpdateProviderConnection atomically updates a ProviderConnection with optimistic locking,
// enforcing CIDR/SSRF/port/scheme/SM-01 permission parity, binding-mode checks, project secret
// ownership checks, config_changelog recording, transactional audit logging, and snapshot sync.
func (r *Repository) UpdateProviderConnection(ctx context.Context, params UpdateProviderConnectionParams) (*ProviderConnection, error) {
	if strings.TrimSpace(params.OrgID) == "" || strings.TrimSpace(params.ConnectionID) == "" || params.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id, connection_id, and expected_version >= 1 are required", port.ErrInvalidInput)
	}

	now := r.nowMs()
	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		var (
			ownerProj      sql.NullString
			adapterID      string
			curCredID      sql.NullString
			curName        string
			curEndpoint    string
			curAllowPriv   int
			curCIDRsJSON   string
			curAuthMode    string
			curState       string
			sharedProjJSON string
			curVersion     int64
		)
		err := tx.QueryRowContext(ctx, `
			SELECT owner_project_id, adapter_id, credential_id, name, endpoint_base_url,
			       allow_private_ip, allowed_cidrs_json, auth_mode, state, shared_project_ids_json, version
			FROM provider_connections
			WHERE org_id = ? AND id = ?`,
			params.OrgID, params.ConnectionID,
		).Scan(
			&ownerProj, &adapterID, &curCredID, &curName, &curEndpoint,
			&curAllowPriv, &curCIDRsJSON, &curAuthMode, &curState, &sharedProjJSON, &curVersion,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, params.ConnectionID)
		}
		if err != nil {
			return mapDBError(err)
		}
		if curVersion != params.ExpectedVersion {
			return port.ErrVersionConflict
		}

		curCIDRs, err := decodeStringSliceStrict(curCIDRsJSON, "allowed_cidrs_json")
		if err != nil {
			return err
		}
		sharedProjects, err := decodeStringSliceStrict(sharedProjJSON, "shared_project_ids_json")
		if err != nil {
			return err
		}

		nextName := curName
		if params.Name != nil {
			trimmed := strings.TrimSpace(*params.Name)
			if trimmed == "" || len(trimmed) > 128 {
				return fmt.Errorf("%w: connection name must be 1..128 characters", port.ErrInvalidInput)
			}
			nextName = trimmed
		}

		nextEndpoint := curEndpoint
		if params.EndpointBaseURL != nil {
			nextEndpoint = strings.TrimSpace(*params.EndpointBaseURL)
		}
		parsedEndpoint, err := url.Parse(strings.TrimSpace(nextEndpoint))
		if err != nil || (parsedEndpoint.Scheme != "http" && parsedEndpoint.Scheme != "https") || parsedEndpoint.Hostname() == "" || parsedEndpoint.User != nil {
			return fmt.Errorf("%w: invalid endpoint_base_url %q", port.ErrInvalidInput, nextEndpoint)
		}

		nextAllowPriv := curAllowPriv != 0
		if params.AllowPrivateIP != nil {
			nextAllowPriv = *params.AllowPrivateIP
		}
		nextCIDRs := curCIDRs
		if params.AllowedCIDRs != nil {
			nextCIDRs = append([]string(nil), (*params.AllowedCIDRs)...)
		}
		if err := validateCIDRs(nextCIDRs); err != nil {
			return err
		}
		if nextAllowPriv && len(nextCIDRs) == 0 {
			return fmt.Errorf("%w: allow_private_ip=true requires non-empty allowed_cidrs", port.ErrInvalidInput)
		}
		cidrsJSON, err := marshalStringSlice(nextCIDRs)
		if err != nil {
			return err
		}

		nextState := curState
		if params.State != nil {
			candidate := strings.TrimSpace(*params.State)
			switch candidate {
			case ConnStateDraft, ConnStateValidating, ConnStateActive, ConnStateDegraded, ConnStateAuthExpired, ConnStateSuspendedAdmin, ConnStateRevoked, ConnStateArchived:
				nextState = candidate
			default:
				return fmt.Errorf("%w: invalid connection state %q", port.ErrInvalidInput, candidate)
			}
		}

		if curState == ConnStateRevoked || curState == ConnStateArchived {
			return fmt.Errorf("%w: connection in state %s cannot be modified", ErrInvalidStateTransition, curState)
		}
		mutatingNetworkOrSecrets := params.EndpointBaseURL != nil ||
			params.AllowPrivateIP != nil ||
			params.AllowedCIDRs != nil ||
			params.ReplaceBindings || params.MergeBindings ||
			params.CredentialID != nil ||
			params.AuthMode != nil

		var (
			currentPrimaryCred    *string
			explicitPrimaryCred   *string
			nextPrimaryCred       *string
		)
		if curCredID.Valid && curCredID.String != "" {
			v := curCredID.String
			currentPrimaryCred = &v
			nextPrimaryCred = &v
		}
		if params.CredentialID != nil {
			if trimmed := strings.TrimSpace(*params.CredentialID); trimmed != "" {
			explicitPrimaryCred = &trimmed
				nextPrimaryCred = &trimmed
			} else {
				nextPrimaryCred = nil
			}
		}

		nextAuthMode := curAuthMode
		if params.AuthMode != nil {
			am := strings.TrimSpace(*params.AuthMode)
			if !isValidAuthMode(am) {
				return fmt.Errorf("%w: invalid auth_mode %q", port.ErrInvalidInput, am)
			}
			nextAuthMode = am
		}

		var (
			normalizedBindings []SecretBindingInput
			bindingsToValidate []SecretBindingInput
		)
		if params.ReplaceBindings || params.MergeBindings {
			requestedBindings := params.SecretBindings
			if params.MergeBindings {
				rows, err := tx.QueryContext(ctx, `SELECT slot_name, credential_id, inject_mode FROM connection_secret_bindings WHERE org_id = ? AND connection_id = ?`, params.OrgID, params.ConnectionID)
				if err != nil {
					return mapDBError(err)
				}
				bySlot := make(map[string]SecretBindingInput)
				for rows.Next() {
					var current SecretBindingInput
					if err := rows.Scan(&current.SlotName, &current.CredentialID, &current.InjectMode); err != nil {
						rows.Close()
						return mapDBError(err)
					}
					bySlot[current.SlotName] = current
				}
				if err := rows.Err(); err != nil {
					rows.Close()
					return mapDBError(err)
				}
				rows.Close()
				for _, binding := range params.SecretBindings {
					bySlot[binding.SlotName] = binding
				}
				requestedBindings = make([]SecretBindingInput, 0, len(bySlot))
				for _, binding := range bySlot {
					requestedBindings = append(requestedBindings, binding)
				}
				sort.Slice(requestedBindings, func(i, j int) bool {
					return requestedBindings[i].SlotName < requestedBindings[j].SlotName
				})
			}
			seenSlots := make(map[string]struct{}, len(requestedBindings)+1)
			for _, rawB := range requestedBindings {
				slot := strings.TrimSpace(rawB.SlotName)
				credID := strings.TrimSpace(rawB.CredentialID)
				if slot == "" || credID == "" {
					return fmt.Errorf("%w: secret binding slot_name and credential_id are required", port.ErrInvalidInput)
				}
				if _, dup := seenSlots[slot]; dup {
					return fmt.Errorf("%w: duplicate secret binding slot_name %q", port.ErrInvalidInput, slot)
				}
				mode := strings.TrimSpace(rawB.InjectMode)
				if mode == "" {
					mode = InjectModeHostBlind
				}
				if !isValidInjectMode(mode) {
					return fmt.Errorf("%w: invalid inject_mode %q", port.ErrInvalidInput, mode)
				}
				seenSlots[slot] = struct{}{}
				normalizedBindings = append(normalizedBindings, SecretBindingInput{
					SlotName:     slot,
					CredentialID: credID,
					InjectMode:   mode,
				})
			}
			nextPrimaryCred, err = selectPrimaryCredential(currentPrimaryCred, explicitPrimaryCred, params.CredentialID != nil, normalizedBindings)
			if err != nil {
				return err
			}
			if params.AuthMode == nil {
				if len(normalizedBindings) == 0 {
					nextAuthMode = AuthModeNone
				} else if nextAuthMode == AuthModeNone {
					nextAuthMode = AuthModeAPIKey
				}
			}
			bindingsToValidate = normalizedBindings
		} else {
			rows, qerr := tx.QueryContext(ctx, `
				SELECT slot_name, credential_id, inject_mode
				FROM connection_secret_bindings
				WHERE org_id = ? AND connection_id = ?
				ORDER BY slot_name ASC`, params.OrgID, params.ConnectionID)
			if qerr != nil {
				return mapDBError(qerr)
			}
			for rows.Next() {
				var b SecretBindingInput
				if serr := rows.Scan(&b.SlotName, &b.CredentialID, &b.InjectMode); serr != nil {
					_ = rows.Close()
					return mapDBError(serr)
				}
				bindingsToValidate = append(bindingsToValidate, b)
			}
			if rerr := rows.Err(); rerr != nil {
				_ = rows.Close()
				return mapDBError(rerr)
			}
			_ = rows.Close()
		}
		hasAPIKeySlot := false
		for _, binding := range bindingsToValidate {
			if binding.SlotName == "api_key" {
				hasAPIKeySlot = true
				if explicitPrimaryCred != nil && binding.CredentialID != *explicitPrimaryCred {
					return fmt.Errorf("%w: credential_id %q conflicts with secret_bindings api_key %q", port.ErrInvalidInput, *explicitPrimaryCred, binding.CredentialID)
				}
				break
			}
		}
		if !hasAPIKeySlot && nextPrimaryCred != nil && *nextPrimaryCred != "" {
			bindingsToValidate = append(bindingsToValidate, SecretBindingInput{
				SlotName:     "api_key",
				CredentialID: *nextPrimaryCred,
				InjectMode:   InjectModeHostBlind,
			})
		}

		if nextAuthMode == AuthModeNone && (nextPrimaryCred != nil || len(bindingsToValidate) > 0) {
			return fmt.Errorf("%w: auth_mode=NONE cannot bind credentials or secret slots", port.ErrInvalidInput)
		}

		if mutatingNetworkOrSecrets || nextState == ConnStateActive {
			adapterHosts, gov, err := loadConnectionExtensionGovernanceByAdapter(ctx, tx, params.OrgID, adapterID)
			if err != nil {
				return err
			}
			gov.HasChildProjectScope = ownerProj.Valid || len(sharedProjects) > 0
			if err := r.validateOrgAndActiveExtensionTrust(gov, false, port.ErrInvalidInput); err != nil {
				return err
			}
			approved, err := decodeApprovedPermissionsStrict(gov.ApprovedPermissionsJSON)
			if err != nil {
				return fmt.Errorf("%w: %w", port.ErrInvalidInput, err)
			}
			if err := validateEndpointAndPermissions(parsedEndpoint, nextAllowPriv, adapterHosts, approved, port.ErrInvalidInput); err != nil {
				return err
			}
			var ownerProjPtr *string
			if ownerProj.Valid {
				v := ownerProj.String
				ownerProjPtr = &v
			}
			for _, b := range bindingsToValidate {
				if err := slotPermittedByApproved(b.SlotName, b.InjectMode, approved, port.ErrInvalidInput); err != nil {
					return err
				}
				if err := ensureSecretCompatibleWithConnectionTx(ctx, tx, params.OrgID, ownerProjPtr, sharedProjects, b.CredentialID); err != nil {
					return err
				}
			}
		}

		if params.ReplaceBindings || params.MergeBindings {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM connection_secret_bindings WHERE org_id = ? AND connection_id = ?`,
				params.OrgID, params.ConnectionID,
			); err != nil {
				return mapDeleteDBError(err)
			}
			for _, b := range normalizedBindings {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO connection_secret_bindings (
						org_id, connection_id, slot_name, credential_id, inject_mode, created_at_ms
					) VALUES (?, ?, ?, ?, ?, ?)`,
					params.OrgID, params.ConnectionID, b.SlotName, b.CredentialID, b.InjectMode, now,
				); err != nil {
					return mapWriteDBError(err)
				}
			}
		}

		allowPrivInt := 0
		if nextAllowPriv {
			allowPrivInt = 1
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE provider_connections
			SET name = ?, endpoint_base_url = ?, allow_private_ip = ?, allowed_cidrs_json = ?,
			    credential_id = ?, auth_mode = ?, state = ?, version = version + 1, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND version = ?`,
			nextName, nextEndpoint, allowPrivInt, cidrsJSON,
			nullStrPtr(nextPrimaryCred), nextAuthMode, nextState, now,
			params.OrgID, params.ConnectionID, params.ExpectedVersion,
		)
		if err != nil {
			return mapWriteDBError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return port.ErrVersionConflict
		}

		emergency := params.Emergency || nextState == ConnStateSuspendedAdmin || nextState == ConnStateRevoked
		if err := appendChangelogTx(ctx, tx, &params.OrgID, "provider_connection", params.ConnectionID, port.MutationUpsert, emergency, now); err != nil {
			return err
		}

		actorKind := strings.TrimSpace(params.AuditActorKind)
		if actorKind == "" {
			actorKind = "SYSTEM"
		}
		actorID := strings.TrimSpace(params.AuditActorID)
		if actorID == "" {
			actorID = "catalog"
		}
		var reqIDPtr *string
		if trimmed := strings.TrimSpace(params.RequestID); trimmed != "" {
			reqIDPtr = &trimmed
		}
		auditID, err := idgen.New(idgen.PrefixAudit)
		if err != nil {
			return err
		}
		rawMeta, err := json.Marshal(map[string]any{
			"name":  nextName,
			"state": nextState,
		})
		if err != nil {
			return err
		}
		orgCopy := params.OrgID
		return r.audit.AppendTx(ctx, tx, port.AuditEvent{
			ID:           auditID,
			CreatedAtMs:  now,
			OrgID:        &orgCopy,
			ActorKind:    actorKind,
			ActorID:      actorID,
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "provider_connection.updated",
			TargetType:   "provider_connection",
			TargetID:     params.ConnectionID,
			RequestID:    reqIDPtr,
			MetadataJSON: string(rawMeta),
		})
	}); err != nil {
		return nil, err
	}

	if err := r.syncSnapshot(ctx); err != nil {
		return nil, err
	}
	return r.GetProviderConnection(ctx, params.OrgID, params.ConnectionID)
}

func selectPrimaryCredential(currentCredentialID, explicitCredentialID *string, explicitCredentialSet bool, bindings []SecretBindingInput) (*string, error) {
	var (
		apiKeyCredentialID   *string
		fallbackCredentialID *string
		fallbackSlot         string
	)
	for _, binding := range bindings {
		credentialID := binding.CredentialID
		if binding.SlotName == "api_key" {
			if explicitCredentialID != nil && credentialID != *explicitCredentialID {
				return nil, fmt.Errorf("%w: credential_id %q conflicts with secret_bindings api_key %q", port.ErrInvalidInput, *explicitCredentialID, credentialID)
			}
			apiKeyCredentialID = &credentialID
			continue
		}
		if fallbackCredentialID == nil || binding.SlotName < fallbackSlot {
			fallbackCredentialID = &credentialID
			fallbackSlot = binding.SlotName
		}
	}
	if apiKeyCredentialID != nil {
		return apiKeyCredentialID, nil
	}
	if explicitCredentialID != nil {
		return explicitCredentialID, nil
	}
	if !explicitCredentialSet && currentCredentialID != nil {
		return currentCredentialID, nil
	}
	return fallbackCredentialID, nil
}

// DeleteProviderConnection deletes an unreferenced ProviderConnection, returning port.ErrResourceInUse (409)
// if any ModelDeployment references it (NEG-3).
func (r *Repository) DeleteProviderConnection(ctx context.Context, orgID, connectionID string, expectedVersion int64) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(connectionID) == "" || expectedVersion < 1 {
		return fmt.Errorf("%w: org_id, connection_id, and expected_version >= 1 are required", port.ErrInvalidInput)
	}
	now := r.nowMs()
	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		var currentVersion int64
		if err := tx.QueryRowContext(ctx, `SELECT version FROM provider_connections WHERE org_id = ? AND id = ?`, orgID, connectionID).Scan(&currentVersion); err != nil {
			if errors.Is(err, sql.ErrNoRows) { return fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, connectionID) }
			return mapDBError(err)
		}
		if currentVersion != expectedVersion { return port.ErrVersionConflict }
		var inUse int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM model_deployments WHERE org_id = ? AND connection_id = ?`, orgID, connectionID).Scan(&inUse); err != nil {
			return mapDBError(err)
		}
		if inUse > 0 {
			return fmt.Errorf("%w: provider_connection %q is referenced by %d model_deployments", port.ErrResourceInUse, connectionID, inUse)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM provider_connections WHERE org_id = ? AND id = ? AND version = ?`, orgID, connectionID, expectedVersion)
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
		if err := appendChangelogTx(ctx, tx, &orgID, "provider_connection", connectionID, port.MutationDelete, false, now); err != nil {
			return err
		}
		return r.emitAuditTx(ctx, tx, &orgID, "provider_connection.deleted", "provider_connection", connectionID, nil, now)
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
		if err := appendChangelogTx(ctx, tx, params.OrgID, "logical_model", id, port.MutationUpsert, false, now); err != nil {
			return err
		}
		return r.emitAuditTx(ctx, tx, params.OrgID, "logical_model.created", "logical_model", id, map[string]any{
			"canonical_slug": strings.TrimSpace(params.CanonicalSlug),
			"modality_kind":  params.ModalityKind,
		}, now)
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
		if err := appendChangelogTx(ctx, tx, scopedOrgID, "logical_model", id, port.MutationDelete, false, now); err != nil {
			return err
		}
		return r.emitAuditTx(ctx, tx, scopedOrgID, "logical_model.deleted", "logical_model", id, nil, now)
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
	if hasNegativePricingRates(params.Pricing) {
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
		if err := appendChangelogTx(ctx, tx, &params.OrgID, "model_deployment", id, port.MutationUpsert, false, now); err != nil {
			return err
		}
		return r.emitAuditTx(ctx, tx, &params.OrgID, "model_deployment.created", "model_deployment", id, map[string]any{
			"connection_id":       params.ConnectionID,
			"logical_model_id":    params.LogicalModelID,
			"upstream_model_name": strings.TrimSpace(params.UpstreamModelName),
			"state":               state,
		}, now)
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
			if hasNegativePricingRates(*params.Pricing) {
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
		if err := appendChangelogTx(ctx, tx, &orgID, "model_deployment", deploymentID, port.MutationUpsert, false, now); err != nil {
			return err
		}
		return r.emitAuditTx(ctx, tx, &orgID, "model_deployment.updated", "model_deployment", deploymentID, map[string]any{
			"state":           state,
			"assurance_level": assurance,
		}, now)
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

func hasNegativePricingRates(p snapshot.PricingNanoUSD) bool {
	return p.InputPer1MNanoUSD < 0 ||
		p.OutputPer1MNanoUSD < 0 ||
		p.CacheReadPer1MNanoUSD < 0 ||
		p.CacheWritePer1MNanoUSD < 0 ||
		p.ReasoningPer1MNanoUSD < 0 ||
		p.ImagePerUnitNanoUSD < 0 ||
		p.ImagePer1MPixelsNanoUSD < 0 ||
		p.AudioInputPerSecNanoUSD < 0 ||
		p.AudioOutputPerSecNanoUSD < 0 ||
		p.DocumentPageNanoUSD < 0 ||
		p.RerankUnitNanoUSD < 0
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
	if hasNegativePricingRates(pricing) {
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
		if err := appendChangelogTx(ctx, tx, &orgID, "model_deployment", deploymentID, port.MutationDelete, false, now); err != nil {
			return err
		}
		return r.emitAuditTx(ctx, tx, &orgID, "model_deployment.deleted", "model_deployment", deploymentID, nil, now)
	}); err != nil {
		return err
	}
	return r.syncSnapshot(ctx)
}

// ResolveConnectionPolicy implements broker.ConnectionPolicyResolver fail-closed for both
// credentialed and zero-slot (auth_mode=NONE) connections (DEC-020), enforcing SM-01
// approved permissions, org disable policy, and TP-BR-0202 org minimum trust tier.
func (r *Repository) ResolveConnectionPolicy(ctx context.Context, orgID, connectionID string) (*egress.Policy, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(connectionID) == "" {
		return nil, fmt.Errorf("%w: orgID and connectionID are required", port.ErrInvalidInput)
	}
	var (
		endpointStr      string
		allowPriv        int
		cidrsJSON        string
		state            string
		connOwnerProj    sql.NullString
		sharedProjJSON   string
		adapterHostsJSON string
		gov              extensionGovernanceState
		activeVerID      sql.NullString
		entSigned        int
		orgOwnerOptIn    int
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT pc.endpoint_base_url, pc.allow_private_ip, pc.allowed_cidrs_json, pc.state,
		       pc.owner_project_id, pc.shared_project_ids_json,
		       pa.allowed_hosts_json, ei.state, ev.id, ei.approved_permissions_json,
		       ep.canonical_name, COALESCE(ev.signature_verified, 0), COALESCE(ev.signing_key_fingerprint, ''), COALESCE(ev.enterprise_signed, 0), COALESCE(ev.enterprise_key_fingerprint, ''),
		       o.min_plugin_trust, oep.enabled, COALESCE(oep.org_owner_opt_in, 0)
		FROM provider_connections pc
		JOIN organizations o ON o.id = pc.org_id
		JOIN provider_adapters pa ON pa.id = pc.adapter_id
		JOIN extension_installations ei ON ei.id = pa.installation_id
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions ev ON ev.id = ei.active_version_id AND ev.package_id = ei.package_id
		LEFT JOIN org_extension_policies oep ON oep.org_id = pc.org_id AND oep.package_id = ei.package_id
		WHERE pc.org_id = ? AND pc.id = ?`, orgID, connectionID,
	).Scan(
		&endpointStr, &allowPriv, &cidrsJSON, &state,
		&connOwnerProj, &sharedProjJSON,
		&adapterHostsJSON, &gov.InstallState, &activeVerID, &gov.ApprovedPermissionsJSON,
		&gov.CanonicalName, &gov.SignatureVerified, &gov.Fingerprint, &entSigned, &gov.EnterpriseKeyFingerprint,
		&gov.MinPluginTrust, &gov.OrgPolicyEnabled, &orgOwnerOptIn,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, connectionID)
	}
	if err != nil {
		return nil, mapDBError(err)
	}
	sharedProjects, err := decodeStringSliceStrict(sharedProjJSON, "shared_project_ids_json")
	if err != nil {
		return nil, err
	}
	if activeVerID.Valid {
		gov.ActiveVersionID = activeVerID.String
	}
	gov.EnterpriseSigned = entSigned != 0
	gov.OrgOwnerOptIn = orgOwnerOptIn != 0
	gov.HasChildProjectScope = connOwnerProj.Valid || len(sharedProjects) > 0
	if err := r.validateOrgAndActiveExtensionTrust(&gov, true, ErrConnectionNotReady); err != nil {
		return nil, err
	}
	approved, err := decodeApprovedPermissionsStrict(gov.ApprovedPermissionsJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnectionNotReady, err)
	}
	allowedCIDRs, err := decodeStringSliceStrict(cidrsJSON, "allowed_cidrs_json")
	if err != nil {
		return nil, err
	}
	adapterHosts, err := decodeStringSliceStrict(adapterHostsJSON, "adapter_allowed_hosts_json")
	if err != nil {
		return nil, err
	}
	return buildAuthoritativeEgressPolicy(endpointStr, allowPriv != 0, allowedCIDRs, adapterHosts, approved, state)
}

// ResolveSlot implements broker.CredentialBindingResolver fail-closed without ever reading
// or returning plaintext or ciphertext secret bytes (SPEC-SEC-03), enforcing SM-01 approved
// secret slots/modes/egress permissions, org disable policy, and TP-BR-0202 org min trust.
func (r *Repository) ResolveSlot(ctx context.Context, orgID, connectionID, slotName string) (*broker.ResolvedSlot, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(connectionID) == "" || strings.TrimSpace(slotName) == "" {
		return nil, fmt.Errorf("%w: orgID, connectionID, and slotName are required", port.ErrInvalidInput)
	}
	var (
		endpointStr       string
		allowPriv         int
		cidrsJSON         string
		state             string
		primaryCredID     sql.NullString
		connOwnerProj     sql.NullString
		sharedProjJSON    string
		adapterHostsJSON  string
		gov               extensionGovernanceState
		activeVerID       sql.NullString
		entSigned         int
		orgOwnerOptIn     int
		slotCredID        sql.NullString
		slotInjectMode    sql.NullString
		primaryHasBinding bool
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT pc.endpoint_base_url, pc.allow_private_ip, pc.allowed_cidrs_json, pc.state, pc.credential_id,
		       pc.owner_project_id, pc.shared_project_ids_json, pa.allowed_hosts_json,
		       ei.state, ev.id, ei.approved_permissions_json,
		       ep.canonical_name, COALESCE(ev.signature_verified, 0), COALESCE(ev.signing_key_fingerprint, ''), COALESCE(ev.enterprise_signed, 0), COALESCE(ev.enterprise_key_fingerprint, ''),
		       o.min_plugin_trust, oep.enabled, COALESCE(oep.org_owner_opt_in, 0), csb.credential_id, csb.inject_mode,
		       EXISTS (SELECT 1 FROM connection_secret_bindings b
		               WHERE b.org_id = pc.org_id AND b.connection_id = pc.id
		                 AND b.credential_id = pc.credential_id)
		FROM provider_connections pc
		JOIN organizations o ON o.id = pc.org_id
		JOIN provider_adapters pa ON pa.id = pc.adapter_id
		JOIN extension_installations ei ON ei.id = pa.installation_id
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions ev ON ev.id = ei.active_version_id AND ev.package_id = ei.package_id
		LEFT JOIN org_extension_policies oep ON oep.org_id = pc.org_id AND oep.package_id = ei.package_id
		LEFT JOIN connection_secret_bindings csb
		       ON csb.org_id = pc.org_id AND csb.connection_id = pc.id AND csb.slot_name = ?
		WHERE pc.org_id = ? AND pc.id = ?`,
		slotName, orgID, connectionID,
	).Scan(
		&endpointStr, &allowPriv, &cidrsJSON, &state, &primaryCredID,
		&connOwnerProj, &sharedProjJSON, &adapterHostsJSON,
		&gov.InstallState, &activeVerID, &gov.ApprovedPermissionsJSON,
		&gov.CanonicalName, &gov.SignatureVerified, &gov.Fingerprint, &entSigned, &gov.EnterpriseKeyFingerprint,
		&gov.MinPluginTrust, &gov.OrgPolicyEnabled, &orgOwnerOptIn, &slotCredID, &slotInjectMode, &primaryHasBinding,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, connectionID)
	}
	if err != nil {
		return nil, mapDBError(err)
	}
	sharedProjects, err := decodeStringSliceStrict(sharedProjJSON, "shared_project_ids_json")
	if err != nil {
		return nil, err
	}
	if activeVerID.Valid {
		gov.ActiveVersionID = activeVerID.String
	}
	gov.EnterpriseSigned = entSigned != 0
	gov.OrgOwnerOptIn = orgOwnerOptIn != 0
	gov.HasChildProjectScope = connOwnerProj.Valid || len(sharedProjects) > 0
	if err := r.validateOrgAndActiveExtensionTrust(&gov, true, ErrConnectionNotReady); err != nil {
		return nil, err
	}
	approved, err := decodeApprovedPermissionsStrict(gov.ApprovedPermissionsJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnectionNotReady, err)
	}

	credID := ""
	injectMode := InjectModeHostBlind
	if slotCredID.Valid && slotCredID.String != "" {
		credID = slotCredID.String
		if slotInjectMode.Valid && slotInjectMode.String != "" {
			injectMode = slotInjectMode.String
		}
	} else if slotName == "api_key" && primaryCredID.Valid && primaryCredID.String != "" && !primaryHasBinding {
		credID = primaryCredID.String
	} else {
		return nil, fmt.Errorf("%w: secret slot %q not bound on connection %q", port.ErrNotFound, slotName, connectionID)
	}
	if err := slotPermittedByApproved(slotName, injectMode, approved, ErrConnectionNotReady); err != nil {
		return nil, err
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
	policy, err := buildAuthoritativeEgressPolicy(endpointStr, allowPriv != 0, allowedCIDRs, adapterHosts, approved, state)
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
		InjectMode:     injectMode,
		BoundAudience:  audPtr,
		AllowedHosts:   policy.AllowedHosts,
		AllowPrivateIP: policy.AllowPrivateIP,
		AllowedCIDRs:   policy.AllowedCIDRs,
		AllowedPorts:   policy.AllowedPorts,
		RequireTLS:     policy.RequireTLS,
	}, nil
}

func buildAuthoritativeEgressPolicy(endpointStr string, allowPrivateIP bool, allowedCIDRs, adapterHosts []string, approved installer.PermissionSet, state string) (*egress.Policy, error) {
	switch state {
	case ConnStateValidating, ConnStateActive, ConnStateDegraded:
	default:
		return nil, fmt.Errorf("%w: connection state %q is blocked from egress", ErrConnectionNotReady, state)
	}
	u, err := url.Parse(strings.TrimSpace(endpointStr))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("%w: malformed endpoint_base_url %q", port.ErrInvalidInput, endpointStr)
	}
	if err := validateEndpointAndPermissions(u, allowPrivateIP, adapterHosts, approved, ErrConnectionNotReady); err != nil {
		return nil, err
	}
	effectiveAllowPrivate := allowPrivateIP && approved.AllowPrivateIP
	policy, err := buildEgressPolicyFromURL(endpointStr, effectiveAllowPrivate, allowedCIDRs)
	if err != nil {
		return nil, err
	}
	if approved.RequireTLS {
		policy.RequireTLS = true
	}
	return policy, nil
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

// SyncSnapshots triggers an immediate synchronous serving snapshot refresh.
func (r *Repository) SyncSnapshots(ctx context.Context) error {
	return r.syncSnapshot(ctx)
}

func (r *Repository) syncSnapshot(ctx context.Context) error {
	if r.snapSync == nil {
		return nil
	}
	if err := r.snapSync.Refresh(ctx); err != nil {
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

func (r *Repository) emitAuditTx(
	ctx context.Context,
	tx *sql.Tx,
	orgID *string,
	eventType, targetType, targetID string,
	meta map[string]any,
	nowMs int64,
) error {
	if r.audit == nil {
		return fmt.Errorf("%w: non-nil port.TxAuditSink is required", port.ErrInvalidInput)
	}
	auditID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return err
	}
	if meta == nil {
		meta = map[string]any{}
	}
	rawMeta, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	var normalizedOrg *string
	if orgID != nil && strings.TrimSpace(*orgID) != "" {
		trimmed := strings.TrimSpace(*orgID)
		normalizedOrg = &trimmed
	}
	return r.audit.AppendTx(ctx, tx, port.AuditEvent{
		ID:           auditID,
		CreatedAtMs:  nowMs,
		OrgID:        normalizedOrg,
		ActorKind:    "SYSTEM",
		ActorID:      "catalog",
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    eventType,
		TargetType:   targetType,
		TargetID:     targetID,
		MetadataJSON: string(rawMeta),
	})
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
	ctx context.Context, tx rowQuerier, orgID string,
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

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type extensionGovernanceState struct {
	InstallState             string
	ActiveVersionID          string
	ApprovedPermissionsJSON  string
	CanonicalName            string
	SignatureVerified        int
	Fingerprint              string
	EnterpriseSigned         bool
	EnterpriseKeyFingerprint string
	MinPluginTrust           string
	OrgPolicyEnabled         sql.NullInt64
	OrgOwnerOptIn            bool
	HasChildProjectScope     bool
}

func loadAdapterExtensionGovernance(ctx context.Context, q rowQuerier, installationID string) (*extensionGovernanceState, error) {
	var (
		gov         extensionGovernanceState
		activeVerID sql.NullString
		entSigned   int
	)
	err := q.QueryRowContext(ctx, `
		SELECT ei.state, ev.id, ei.approved_permissions_json,
		       ep.canonical_name, COALESCE(ev.signature_verified, 0), COALESCE(ev.signing_key_fingerprint, ''), COALESCE(ev.enterprise_signed, 0), COALESCE(ev.enterprise_key_fingerprint, '')
		FROM extension_installations ei
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions ev ON ev.id = ei.active_version_id AND ev.package_id = ei.package_id
		WHERE ei.id = ?`, installationID,
	).Scan(
		&gov.InstallState, &activeVerID, &gov.ApprovedPermissionsJSON,
		&gov.CanonicalName, &gov.SignatureVerified, &gov.Fingerprint, &entSigned, &gov.EnterpriseKeyFingerprint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: extension installation %q not found", port.ErrNotFound, installationID)
	}
	if err != nil {
		return nil, mapDBError(err)
	}
	if activeVerID.Valid {
		gov.ActiveVersionID = activeVerID.String
	}
	gov.EnterpriseSigned = entSigned != 0
	return &gov, nil
}

func loadConnectionExtensionGovernanceByAdapter(ctx context.Context, q rowQuerier, orgID, adapterID string) ([]string, *extensionGovernanceState, error) {
	var (
		gov              extensionGovernanceState
		activeVerID      sql.NullString
		entSigned        int
		orgOwnerOptIn    int
		adapterHostsJSON string
	)
	if err := q.QueryRowContext(ctx, `SELECT min_plugin_trust FROM organizations WHERE id = ?`, orgID).Scan(&gov.MinPluginTrust); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("%w: organization %q not found", port.ErrNotFound, orgID)
		}
		return nil, nil, mapDBError(err)
	}
	err := q.QueryRowContext(ctx, `
		SELECT pa.allowed_hosts_json, ei.state, ev.id, ei.approved_permissions_json,
		       ep.canonical_name, COALESCE(ev.signature_verified, 0), COALESCE(ev.signing_key_fingerprint, ''), COALESCE(ev.enterprise_signed, 0), COALESCE(ev.enterprise_key_fingerprint, ''),
		       oep.enabled, COALESCE(oep.org_owner_opt_in, 0)
		FROM provider_adapters pa
		JOIN extension_installations ei ON ei.id = pa.installation_id
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions ev ON ev.id = ei.active_version_id AND ev.package_id = ei.package_id
		LEFT JOIN org_extension_policies oep ON oep.org_id = ? AND oep.package_id = ei.package_id
		WHERE pa.id = ?`, orgID, adapterID,
	).Scan(
		&adapterHostsJSON, &gov.InstallState, &activeVerID, &gov.ApprovedPermissionsJSON,
		&gov.CanonicalName, &gov.SignatureVerified, &gov.Fingerprint, &entSigned, &gov.EnterpriseKeyFingerprint,
		&gov.OrgPolicyEnabled, &orgOwnerOptIn,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("%w: provider_adapter %q not found", port.ErrNotFound, adapterID)
	}
	if err != nil {
		return nil, nil, mapDBError(err)
	}
	adapterHosts, err := decodeStringSliceStrict(adapterHostsJSON, "provider_adapters.allowed_hosts_json")
	if err != nil {
		return nil, nil, err
	}
	if activeVerID.Valid {
		gov.ActiveVersionID = activeVerID.String
	}
	gov.EnterpriseSigned = entSigned != 0
	gov.OrgOwnerOptIn = orgOwnerOptIn != 0
	return adapterHosts, &gov, nil
}

func decodeApprovedPermissionsStrict(raw string) (installer.PermissionSet, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" || trimmed == "null" {
		return installer.PermissionSet{}, errors.New("SM-01 approved_permissions_json is empty or missing")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil || obj == nil {
		return installer.PermissionSet{}, fmt.Errorf("corrupt SM-01 approved_permissions_json: must be a JSON object")
	}
	var perms installer.PermissionSet
	if err := json.Unmarshal([]byte(trimmed), &perms); err != nil {
		return installer.PermissionSet{}, fmt.Errorf("corrupt SM-01 approved_permissions_json: %v", err)
	}
	perms = installer.NormalizePermissionSet(perms)
	if len(perms.AllowedHosts) == 0 {
		return installer.PermissionSet{}, errors.New("SM-01 approved networkEgress.allowedHosts is empty (default-deny CT-14)")
	}
	return perms, nil
}

type dbQuerier interface {
	rowQuerier
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (r *Repository) ensureConnectionAuthorized(ctx context.Context, q dbQuerier, orgID, connectionID string, wrapSentinel error) error {
	var (
		adapterID      string
		endpointStr    string
		allowPriv      int
		ownerProj      sql.NullString
		sharedProjJSON string
		primaryCredID  sql.NullString
	)
	err := q.QueryRowContext(ctx, `
		SELECT adapter_id, endpoint_base_url, allow_private_ip, owner_project_id, shared_project_ids_json, credential_id
		FROM provider_connections
		WHERE org_id = ? AND id = ?`, orgID, connectionID,
	).Scan(&adapterID, &endpointStr, &allowPriv, &ownerProj, &sharedProjJSON, &primaryCredID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, connectionID)
	}
	if err != nil {
		return mapDBError(err)
	}
	sharedProjects, err := decodeStringSliceStrict(sharedProjJSON, "shared_project_ids_json")
	if err != nil {
		return err
	}
	parsedEndpoint, perr := url.Parse(strings.TrimSpace(endpointStr))
	if perr != nil || parsedEndpoint.Hostname() == "" {
		return fmt.Errorf("%w: invalid endpoint_base_url %q", wrapSentinel, endpointStr)
	}
	adapterHosts, gov, err := loadConnectionExtensionGovernanceByAdapter(ctx, q, orgID, adapterID)
	if err != nil {
		return err
	}
	gov.HasChildProjectScope = ownerProj.Valid || len(sharedProjects) > 0
	if err := r.validateOrgAndActiveExtensionTrust(gov, true, wrapSentinel); err != nil {
		return err
	}
	approved, err := decodeApprovedPermissionsStrict(gov.ApprovedPermissionsJSON)
	if err != nil {
		return fmt.Errorf("%w: %w", wrapSentinel, err)
	}
	if err := validateEndpointAndPermissions(parsedEndpoint, allowPriv != 0, adapterHosts, approved, wrapSentinel); err != nil {
		return err
	}

	rows, err := q.QueryContext(ctx, `
		SELECT slot_name, credential_id, inject_mode
		FROM connection_secret_bindings
		WHERE org_id = ? AND connection_id = ?`, orgID, connectionID,
	)
	if err != nil {
		return mapDBError(err)
	}
	var bindings []SecretBindingInput
	hasAPIKeyBinding := false
	for rows.Next() {
		var b SecretBindingInput
		if serr := rows.Scan(&b.SlotName, &b.CredentialID, &b.InjectMode); serr != nil {
			_ = rows.Close()
			return mapDBError(serr)
		}
		if b.SlotName == "api_key" {
			hasAPIKeyBinding = true
		}
		bindings = append(bindings, b)
	}
	if rerr := rows.Err(); rerr != nil {
		_ = rows.Close()
		return mapDBError(rerr)
	}
	_ = rows.Close()

	if !hasAPIKeyBinding && primaryCredID.Valid && primaryCredID.String != "" {
		bindings = append(bindings, SecretBindingInput{
			SlotName:     "api_key",
			CredentialID: primaryCredID.String,
			InjectMode:   InjectModeHostBlind,
		})
	}
	var ownerProjPtr *string
	if ownerProj.Valid {
		v := ownerProj.String
		ownerProjPtr = &v
	}
	for _, b := range bindings {
		if err := slotPermittedByApproved(b.SlotName, b.InjectMode, approved, wrapSentinel); err != nil {
			return err
		}
		if err := ensureSecretCompatibleWithConnectionTx(ctx, q, orgID, ownerProjPtr, sharedProjects, b.CredentialID); err != nil {
			return fmt.Errorf("%w: %w", wrapSentinel, err)
		}
	}
	return nil
}

func (r *Repository) validateOrgAndActiveExtensionTrust(gov *extensionGovernanceState, requireUsableState bool, wrapSentinel error) error {
	if gov.ActiveVersionID == "" {
		return fmt.Errorf("%w: adapter extension has no active SM-01 version", wrapSentinel)
	}
	if requireUsableState {
		if !isUsableExtensionState(gov.InstallState) {
			return fmt.Errorf("%w: adapter extension installation state %q is not active", wrapSentinel, gov.InstallState)
		}
	} else if !isValidAdapterInstallState(gov.InstallState) {
		return fmt.Errorf("%w: adapter extension installation state %q cannot create connections", wrapSentinel, gov.InstallState)
	}
	verdict := trust.EvaluateOrgExtensionTrust(r.keyring, trust.OrgExtensionTrustInput{
		MinPluginTrust:           gov.MinPluginTrust,
		CanonicalName:            gov.CanonicalName,
		ActiveVersionID:          gov.ActiveVersionID,
		SignatureVerified:        gov.SignatureVerified != 0,
		SigningKeyFingerprint:    gov.Fingerprint,
		EnterpriseSigned:         gov.EnterpriseSigned,
		EnterpriseKeyFingerprint: gov.EnterpriseKeyFingerprint,
		OrgPolicyEnabled:         gov.OrgPolicyEnabled,
		OrgOwnerOptIn:            gov.OrgOwnerOptIn,
	})
	if verdict.DisabledByOrgPolicy {
		return fmt.Errorf("%w: adapter extension is disabled by organization policy", wrapSentinel)
	}
	if verdict.MinTrustErr != nil {
		return fmt.Errorf("%w: %w", wrapSentinel, verdict.MinTrustErr)
	}
	// TP-BR-0202 Exception: By default in organizations, unsigned unverified_external packages are unavailable
	// to child Project scopes unless explicitly enabled by ORG_OWNER in org_extension_policies (enabled=1, org_owner_opt_in=1).
	if gov.HasChildProjectScope && verdict.RequiresOrgOwnerChildOptIn {
		return fmt.Errorf("%w: %w: unsigned unverified_external package requires explicit ORG_OWNER enablement for child project scopes",
			wrapSentinel, installer.ErrOrgTrustPolicyDenied)
	}
	return nil
}

func validateEndpointAndPermissions(
	u *url.URL, allowPrivateIP bool, adapterHosts []string, approved installer.PermissionSet, wrapSentinel error,
) error {
	if len(adapterHosts) == 0 {
		return fmt.Errorf("%w: adapter allowed_hosts is empty (default-deny CT-14)", wrapSentinel)
	}
	if !urlHostPermittedByList(u, adapterHosts) {
		return fmt.Errorf("%w: endpoint host %q denied by adapter allowed_hosts", wrapSentinel, u.Hostname())
	}
	if !urlHostPermittedByList(u, approved.AllowedHosts) {
		return fmt.Errorf("%w: endpoint host %q denied by SM-01 approved networkEgress.allowedHosts", wrapSentinel, u.Hostname())
	}
	if approved.RequireTLS && u.Scheme != "https" {
		return fmt.Errorf("%w: endpoint scheme %q violates SM-01 approved requireTls=true", wrapSentinel, u.Scheme)
	}
	if allowPrivateIP && !approved.AllowPrivateIP {
		return fmt.Errorf("%w: allow_private_ip=true denied by SM-01 approved networkEgress.allowPrivateIp=false", wrapSentinel)
	}
	return nil
}

func slotPermittedByApproved(slotName, injectMode string, approved installer.PermissionSet, wrapSentinel error) error {
	trimmedSlot := strings.TrimSpace(slotName)
	slotAllowed := false
	for _, s := range approved.SecretSlots {
		if strings.TrimSpace(s) == trimmedSlot {
			slotAllowed = true
			break
		}
	}
	if !slotAllowed {
		return fmt.Errorf("%w: secret slot %q is not approved in SM-01 approved_permissions_json", wrapSentinel, trimmedSlot)
	}

	modeSpec, hasModeSpec := approved.SecretBindingModes[trimmedSlot]
	modeSpec = strings.TrimSpace(modeSpec)
	if !hasModeSpec || modeSpec == "" {
		// Unannotated slots allow HOST_BLIND_INJECT only; HOST_CRYPTO_SIGN / MTLS_HANDSHAKE require explicit manifest grant.
		if injectMode == InjectModeHostBlind {
			return nil
		}
		return fmt.Errorf("%w: inject_mode %q for slot %q requires explicit SM-01 binding mode permission", wrapSentinel, injectMode, trimmedSlot)
	}
	for _, tok := range strings.Split(modeSpec, ",") {
		if approvedModeMatchesInjectMode(strings.TrimSpace(tok), injectMode) {
			return nil
		}
	}
	return fmt.Errorf("%w: inject_mode %q for slot %q exceeds SM-01 approved mode %q", wrapSentinel, injectMode, trimmedSlot, modeSpec)
}

func approvedModeMatchesInjectMode(manifestMode, injectMode string) bool {
	mm := strings.ToLower(strings.TrimSpace(manifestMode))
	switch injectMode {
	case InjectModeHostBlind:
		return mm == "host_blind_inject" || mm == "header" || mm == "query" || mm == "query_param" || mm == "bearer"
	case InjectModeHostCryptoSign:
		return mm == "host_crypto_sign"
	case InjectModeMTLSHandshake:
		return mm == "mtls_handshake"
	default:
		return false
	}
}

func splitHostAndOptionalPort(raw string) (hostname, port string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}
	if strings.HasPrefix(trimmed, "[") {
		if strings.HasSuffix(trimmed, "]") {
			return strings.ToLower(trimmed[1 : len(trimmed)-1]), "", true
		}
		h, p, err := net.SplitHostPort(trimmed)
		if err != nil {
			return "", "", false
		}
		return strings.ToLower(h), p, true
	}
	if strings.ContainsRune(trimmed, ':') {
		h, p, err := net.SplitHostPort(trimmed)
		if err != nil {
			return "", "", false
		}
		return strings.ToLower(h), p, true
	}
	return strings.ToLower(trimmed), "", true
}

func hostPatternPermittedByApproved(candidate string, approved []string) bool {
	if len(approved) == 0 {
		return false
	}
	candHost, candPort, ok := splitHostAndOptionalPort(candidate)
	if !ok || candHost == "" {
		return false
	}
	for _, ap := range approved {
		apHost, apPort, aok := splitHostAndOptionalPort(ap)
		if !aok || apHost != candHost {
			continue
		}
		if apPort == "" || apPort == candPort {
			return true
		}
	}
	return false
}

func urlHostPermittedByList(u *url.URL, list []string) bool {
	if len(list) == 0 {
		return false
	}
	uHost := strings.ToLower(u.Hostname())
	uPort := u.Port()
	if uPort == "" {
		if u.Scheme == "https" {
			uPort = "443"
		} else if u.Scheme == "http" {
			uPort = "80"
		}
	}
	for _, entry := range list {
		apHost, apPort, ok := splitHostAndOptionalPort(entry)
		if !ok || apHost != uHost {
			continue
		}
		if apPort == "" || apPort == uPort {
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
	return trust.IsUsableExtensionState(s)
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
