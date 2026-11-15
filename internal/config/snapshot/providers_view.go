// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package snapshot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nguywnben/tanpopo/internal/security/trust"
)

// ProviderConnection SM-02 state constants mirrored in snapshot views (no Tier-5 import).
const (
	ConnectionStateDraft          = "DRAFT"
	ConnectionStateValidating     = "VALIDATING"
	ConnectionStateActive         = "ACTIVE"
	ConnectionStateDegraded       = "DEGRADED"
	ConnectionStateAuthExpired    = "AUTH_EXPIRED_REAUTH_REQUIRED"
	ConnectionStateSuspendedAdmin = "SUSPENDED_BY_ADMIN"
	ConnectionStateRevoked        = "REVOKED"
	ConnectionStateArchived       = "ARCHIVED"
)

// LogicalModelKey indexes logical models or deployments by tenant org_id (empty = global) and slug.
type LogicalModelKey struct {
	OrgID string
	Slug  string
}

// ProviderAdapterView is an immutable O(1) snapshot record of provider_adapters.
type ProviderAdapterView struct {
	ID                string
	InstallationID    string
	AdapterSlug       string
	NativePrefix      string
	SupportedDialects []string
	AllowedHosts      []string
	Version           int64
	CreatedAtMs       int64
}

func (a *ProviderAdapterView) clone() *ProviderAdapterView {
	if a == nil {
		return nil
	}
	cp := *a
	cp.SupportedDialects = append([]string(nil), a.SupportedDialects...)
	cp.AllowedHosts = append([]string(nil), a.AllowedHosts...)
	return &cp
}

// ConnectionSecretBindingView is an immutable snapshot record of connection_secret_bindings.
type ConnectionSecretBindingView struct {
	OrgID        string
	ConnectionID string
	SlotName     string
	CredentialID string
	InjectMode   string
	CreatedAtMs  int64
}

// ProviderConnectionView is an immutable O(1) snapshot record of provider_connections.
type ProviderConnectionView struct {
	ID                            string
	OrgID                         string
	OwnerProjectID                *string
	AdapterID                     string
	AdapterSlug                   string
	CredentialID                  *string
	Name                          string
	EndpointBaseURL               string
	AllowPrivateIP                bool
	AllowedCIDRs                  []string
	DataRegion                    string
	BillingMode                   string
	AuthMode                      string
	State                         string
	SharedProjectIDs              []string
	sharedProjectSet              map[string]struct{}
	AdapterInstallUsable          bool
	DerivedPublicTrust            string
	ChildProjectBlockedByOrgTrust bool
	BlockedByOrgTrustPolicy       bool
	DisabledByOrgPolicy           bool
	ConfigJSON                    string
	SecretBindings                map[string]*ConnectionSecretBindingView
	Version                       int64
	CreatedAtMs                   int64
	UpdatedAtMs                   int64
}

func (c *ProviderConnectionView) clone() *ProviderConnectionView {
	if c == nil {
		return nil
	}
	cp := *c
	cp.OwnerProjectID = cloneStrPtr(c.OwnerProjectID)
	cp.CredentialID = cloneStrPtr(c.CredentialID)
	cp.AllowedCIDRs = append([]string(nil), c.AllowedCIDRs...)
	cp.SharedProjectIDs = append([]string(nil), c.SharedProjectIDs...)
	cp.SecretBindings = make(map[string]*ConnectionSecretBindingView, len(c.SecretBindings))
	for k, v := range c.SecretBindings {
		if v != nil {
			vb := *v
			cp.SecretBindings[k] = &vb
		}
	}
	return &cp
}

// VisibleToProject reports whether projectID inside orgID can access
// this connection according to owner_project_id and downward shared_project_ids.
func (c *ProviderConnectionView) VisibleToProject(orgID, projectID string) bool {
	if c == nil || c.OrgID != orgID || !c.AdapterInstallUsable || c.DisabledByOrgPolicy || c.BlockedByOrgTrustPolicy {
		return false
	}
	// TP-BR-0202: Unsigned unverified_external extensions are unavailable to child Project
	// scopes unless explicitly enabled by ORG_OWNER via org_extension_policies (enabled=1, org_owner_opt_in=1),
	// even when owner_project_id IS NULL and shared_project_ids is empty.
	if projectID != "" && c.ChildProjectBlockedByOrgTrust {
		return false
	}
	if c.OwnerProjectID == nil {
		// Org-owned connection: accessible organization-wide when shared_project_ids is empty;
		// otherwise strictly limited to projects in shared_project_ids.
		if len(c.sharedProjectSet) == 0 {
			return true
		}
		if projectID == "" {
			return false
		}
		_, ok := c.sharedProjectSet[projectID]
		return ok
	}
	if projectID == "" {
		return false
	}
	if *c.OwnerProjectID == projectID {
		return true
	}
	_, ok := c.sharedProjectSet[projectID]
	return ok
}

// LogicalModelView is an immutable O(1) snapshot record of logical_models.
type LogicalModelView struct {
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

func (m *LogicalModelView) clone() *LogicalModelView {
	if m == nil {
		return nil
	}
	cp := *m
	cp.OrgID = cloneStrPtr(m.OrgID)
	cp.BaseCapabilities = cloneStringMap(m.BaseCapabilities)
	return &cp
}

// PricingNanoUSD holds strict integer nano-USD pricing rates per 1M units (zero floats).
type PricingNanoUSD struct {
	InputPer1MNanoUSD      int64 `json:"input_per_1m_nano_usd"`
	OutputPer1MNanoUSD     int64 `json:"output_per_1m_nano_usd"`
	CacheReadPer1MNanoUSD  int64 `json:"cache_read_per_1m_nano_usd,omitempty"`
	CacheWritePer1MNanoUSD int64 `json:"cache_write_per_1m_nano_usd,omitempty"`
	ReasoningPer1MNanoUSD  int64 `json:"reasoning_per_1m_nano_usd,omitempty"`
}

// ModelDeploymentView is an immutable O(1) snapshot record of model_deployments.
type ModelDeploymentView struct {
	ID                  string
	OrgID               string
	ConnectionID        string
	LogicalModelID      string
	CanonicalModelSlug  string
	DataRegion          string
	UpstreamModelName   string
	NativeDialect       string
	CapabilityOverrides map[string]string
	AssuranceLevel      string
	Pricing             PricingNanoUSD
	PricingJSON         string
	State               string
	CooldownUntilMs     *int64
	ConnectTimeoutMs    int64
	FirstByteTimeoutMs  int64
	IdleChunkTimeoutMs  int64
	Version             int64
	CreatedAtMs         int64
	UpdatedAtMs         int64
}

func (d *ModelDeploymentView) clone() *ModelDeploymentView {
	if d == nil {
		return nil
	}
	cp := *d
	if d.CooldownUntilMs != nil {
		v := *d.CooldownUntilMs
		cp.CooldownUntilMs = &v
	}
	cp.CapabilityOverrides = cloneStringMap(d.CapabilityOverrides)
	return &cp
}

// ProviderCatalogView bundles immutable O(1) indexes over the 4-entity provider hierarchy.
type ProviderCatalogView struct {
	adaptersByID         map[string]*ProviderAdapterView
	connectionsByID      map[string]*ProviderConnectionView
	logicalModelsByID    map[string]*LogicalModelView
	logicalModelsBySlug  map[LogicalModelKey]*LogicalModelView
	deploymentsByID      map[string]*ModelDeploymentView
	deploymentsByOrgSlug map[LogicalModelKey][]*ModelDeploymentView
}

// LookupProviderAdapter performs an O(1) snapshot lookup of a ProviderAdapter by ID.
func (s *ConfigSnapshot) LookupProviderAdapter(adapterID string) (*ProviderAdapterView, bool) {
	if s == nil || s.providers == nil || adapterID == "" {
		return nil, false
	}
	v, ok := s.providers.adaptersByID[adapterID]
	if !ok {
		return nil, false
	}
	return v.clone(), true
}

// LookupProviderConnection performs an O(1) tenant-isolated lookup of a ProviderConnection.
func (s *ConfigSnapshot) LookupProviderConnection(orgID, connectionID string) (*ProviderConnectionView, bool) {
	if s == nil || s.providers == nil || orgID == "" || connectionID == "" {
		return nil, false
	}
	v, ok := s.providers.connectionsByID[connectionID]
	if !ok || v.OrgID != orgID {
		return nil, false
	}
	return v.clone(), true
}

// LookupLogicalModelByID performs an O(1) tenant-isolated snapshot lookup of a LogicalModel by ID.
// Global models (OrgID == nil) are visible to any orgID; org-scoped models require matching orgID.
func (s *ConfigSnapshot) LookupLogicalModelByID(orgID, id string) (*LogicalModelView, bool) {
	if s == nil || s.providers == nil || id == "" {
		return nil, false
	}
	v, ok := s.providers.logicalModelsByID[id]
	if !ok {
		return nil, false
	}
	if v.OrgID != nil && *v.OrgID != orgID {
		return nil, false
	}
	return v.clone(), true
}

// LookupLogicalModelBySlug resolves an org-scoped LogicalModel override first, falling back
// to the global (org_id IS NULL) LogicalModel definition.
func (s *ConfigSnapshot) LookupLogicalModelBySlug(orgID, canonicalSlug string) (*LogicalModelView, bool) {
	if s == nil || s.providers == nil || canonicalSlug == "" {
		return nil, false
	}
	if orgID != "" {
		if v, ok := s.providers.logicalModelsBySlug[LogicalModelKey{OrgID: orgID, Slug: canonicalSlug}]; ok {
			return v.clone(), true
		}
	}
	v, ok := s.providers.logicalModelsBySlug[LogicalModelKey{OrgID: "", Slug: canonicalSlug}]
	if !ok {
		return nil, false
	}
	return v.clone(), true
}

// LookupModelDeployment performs an O(1) tenant-isolated lookup of a ModelDeployment.
func (s *ConfigSnapshot) LookupModelDeployment(orgID, deploymentID string) (*ModelDeploymentView, bool) {
	if s == nil || s.providers == nil || orgID == "" || deploymentID == "" {
		return nil, false
	}
	v, ok := s.providers.deploymentsByID[deploymentID]
	if !ok || v.OrgID != orgID {
		return nil, false
	}
	return v.clone(), true
}

// LookupDeploymentsForSlug performs an O(1) lookup of ModelDeployments for (orgID, canonicalSlug).
func (s *ConfigSnapshot) LookupDeploymentsForSlug(orgID, canonicalSlug string) []*ModelDeploymentView {
	if s == nil || s.providers == nil || orgID == "" || canonicalSlug == "" {
		return nil
	}
	src := s.providers.deploymentsByOrgSlug[LogicalModelKey{OrgID: orgID, Slug: canonicalSlug}]
	if len(src) == 0 {
		return nil
	}
	out := make([]*ModelDeploymentView, len(src))
	for i, d := range src {
		out[i] = d.clone()
	}
	return out
}

func loadProviderCatalogSQL(ctx context.Context, tx *sql.Tx, keyring trust.TrustedKeyring) (*ProviderCatalogView, error) {
	view := &ProviderCatalogView{
		adaptersByID:         make(map[string]*ProviderAdapterView),
		connectionsByID:      make(map[string]*ProviderConnectionView),
		logicalModelsByID:    make(map[string]*LogicalModelView),
		logicalModelsBySlug:  make(map[LogicalModelKey]*LogicalModelView),
		deploymentsByID:      make(map[string]*ModelDeploymentView),
		deploymentsByOrgSlug: make(map[LogicalModelKey][]*ModelDeploymentView),
	}

	if err := loadAdaptersSQL(ctx, tx, view); err != nil {
		return nil, err
	}
	if err := loadConnectionsSQL(ctx, tx, view, keyring); err != nil {
		return nil, err
	}
	if err := loadConnectionBindingsSQL(ctx, tx, view); err != nil {
		return nil, err
	}
	if err := loadLogicalModelsSQL(ctx, tx, view); err != nil {
		return nil, err
	}
	if err := loadModelDeploymentsSQL(ctx, tx, view); err != nil {
		return nil, err
	}
	return view, nil
}

func loadAdaptersSQL(ctx context.Context, tx *sql.Tx, view *ProviderCatalogView) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, installation_id, adapter_slug, native_prefix,
		       supported_dialects_json, allowed_hosts_json, version, created_at_ms
		FROM provider_adapters
	`)
	if err != nil {
		return fmt.Errorf("snapshot: query provider_adapters: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			a            ProviderAdapterView
			nativePrefix sql.NullString
			dialectsJSON string
			hostsJSON    string
		)
		if err := rows.Scan(
			&a.ID, &a.InstallationID, &a.AdapterSlug, &nativePrefix,
			&dialectsJSON, &hostsJSON, &a.Version, &a.CreatedAtMs,
		); err != nil {
			return fmt.Errorf("snapshot: scan provider_adapters: %w", err)
		}
		if nativePrefix.Valid {
			a.NativePrefix = nativePrefix.String
		}
		if a.SupportedDialects, err = parseJSONStringSliceStrict(dialectsJSON); err != nil {
			return fmt.Errorf("snapshot: decode adapter %q supported_dialects_json: %w", a.ID, err)
		}
		if a.AllowedHosts, err = parseJSONStringSliceStrict(hostsJSON); err != nil {
			return fmt.Errorf("snapshot: decode adapter %q allowed_hosts_json: %w", a.ID, err)
		}
		view.adaptersByID[a.ID] = &a
	}
	return rows.Err()
}

func loadConnectionsSQL(ctx context.Context, tx *sql.Tx, view *ProviderCatalogView, keyring trust.TrustedKeyring) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT pc.id, pc.org_id, pc.owner_project_id, pc.adapter_id, pc.credential_id, pc.name,
		       pc.endpoint_base_url, pc.allow_private_ip, pc.allowed_cidrs_json, pc.data_region,
		       pc.billing_mode, pc.auth_mode, pc.state, pc.shared_project_ids_json, pc.config_json,
		       pc.version, pc.created_at_ms, pc.updated_at_ms,
		       COALESCE(o.min_plugin_trust, ''),
		       COALESCE(ei.state, ''),
		       COALESCE(ep.canonical_name, ''),
		       COALESCE(ev.signature_verified, 0),
		       COALESCE(ev.signing_key_fingerprint, ''),
		       COALESCE(ev.enterprise_signed, 0),
		       COALESCE(ev.enterprise_key_fingerprint, ''),
		       ev.id,
		       oep.enabled,
		       COALESCE(oep.org_owner_opt_in, 0)
		FROM provider_connections pc
		LEFT JOIN organizations o ON o.id = pc.org_id
		LEFT JOIN provider_adapters pa ON pa.id = pc.adapter_id
		LEFT JOIN extension_installations ei ON ei.id = pa.installation_id
		LEFT JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions ev ON ev.id = ei.active_version_id AND ev.package_id = ei.package_id
		LEFT JOIN org_extension_policies oep ON oep.org_id = pc.org_id AND oep.package_id = ep.id
	`)
	if err != nil {
		return fmt.Errorf("snapshot: query provider_connections: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			c                ProviderConnectionView
			ownerProj        sql.NullString
			credID           sql.NullString
			allowPriv        int
			cidrsJSON        string
			sharedJSON       string
			minPluginTrust   string
			installState     string
			canonicalName    string
			sigVerified      int
			fingerprint      string
			entSigned        int
			entFingerprint   string
			activeVerID      sql.NullString
			orgPolicyEnabled sql.NullInt64
			orgOwnerOptIn    int
		)
		if err := rows.Scan(
			&c.ID, &c.OrgID, &ownerProj, &c.AdapterID, &credID, &c.Name,
			&c.EndpointBaseURL, &allowPriv, &cidrsJSON, &c.DataRegion,
			&c.BillingMode, &c.AuthMode, &c.State, &sharedJSON, &c.ConfigJSON,
			&c.Version, &c.CreatedAtMs, &c.UpdatedAtMs,
			&minPluginTrust, &installState, &canonicalName, &sigVerified, &fingerprint, &entSigned, &entFingerprint,
			&activeVerID, &orgPolicyEnabled, &orgOwnerOptIn,
		); err != nil {
			return fmt.Errorf("snapshot: scan provider_connections: %w", err)
		}
		verdict := trust.EvaluateOrgExtensionTrust(keyring, trust.OrgExtensionTrustInput{
			MinPluginTrust:           minPluginTrust,
			CanonicalName:            canonicalName,
			ActiveVersionID:          activeVerID.String,
			SignatureVerified:        sigVerified != 0,
			SigningKeyFingerprint:    fingerprint,
			EnterpriseSigned:         entSigned != 0,
			EnterpriseKeyFingerprint: entFingerprint,
			OrgPolicyEnabled:         orgPolicyEnabled,
			OrgOwnerOptIn:            orgOwnerOptIn != 0,
		})
		c.AdapterInstallUsable = trust.IsUsableExtensionState(installState)
		c.DerivedPublicTrust = verdict.DerivedPublicTrust
		c.DisabledByOrgPolicy = verdict.DisabledByOrgPolicy
		c.BlockedByOrgTrustPolicy = verdict.MinTrustDenied
		c.ChildProjectBlockedByOrgTrust = verdict.RequiresOrgOwnerChildOptIn
		if ownerProj.Valid {
			v := ownerProj.String
			c.OwnerProjectID = &v
		}
		if credID.Valid {
			v := credID.String
			c.CredentialID = &v
		}
		if adp, ok := view.adaptersByID[c.AdapterID]; ok {
			c.AdapterSlug = adp.AdapterSlug
		}
		c.AllowPrivateIP = allowPriv != 0
		if c.AllowedCIDRs, err = parseJSONStringSliceStrict(cidrsJSON); err != nil {
			return fmt.Errorf("snapshot: decode connection %q allowed_cidrs_json: %w", c.ID, err)
		}
		if c.SharedProjectIDs, err = parseJSONStringSliceStrict(sharedJSON); err != nil {
			return fmt.Errorf("snapshot: decode connection %q shared_project_ids_json: %w", c.ID, err)
		}
		c.sharedProjectSet = make(map[string]struct{}, len(c.SharedProjectIDs))
		for _, pid := range c.SharedProjectIDs {
			c.sharedProjectSet[pid] = struct{}{}
		}
		c.SecretBindings = make(map[string]*ConnectionSecretBindingView)
		view.connectionsByID[c.ID] = &c
	}
	return rows.Err()
}

func loadConnectionBindingsSQL(ctx context.Context, tx *sql.Tx, view *ProviderCatalogView) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT org_id, connection_id, slot_name, credential_id, inject_mode, created_at_ms
		FROM connection_secret_bindings
	`)
	if err != nil {
		return fmt.Errorf("snapshot: query connection_secret_bindings: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var b ConnectionSecretBindingView
		if err := rows.Scan(&b.OrgID, &b.ConnectionID, &b.SlotName, &b.CredentialID, &b.InjectMode, &b.CreatedAtMs); err != nil {
			return fmt.Errorf("snapshot: scan connection_secret_bindings: %w", err)
		}
		if conn, ok := view.connectionsByID[b.ConnectionID]; ok && conn.OrgID == b.OrgID {
			conn.SecretBindings[b.SlotName] = &b
		}
	}
	return rows.Err()
}

func loadLogicalModelsSQL(ctx context.Context, tx *sql.Tx, view *ProviderCatalogView) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, org_id, canonical_slug, modality_kind, context_window,
		       max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms
		FROM logical_models
	`)
	if err != nil {
		return fmt.Errorf("snapshot: query logical_models: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			m        LogicalModelView
			orgID    sql.NullString
			capsJSON string
		)
		if err := rows.Scan(
			&m.ID, &orgID, &m.CanonicalSlug, &m.ModalityKind, &m.ContextWindow,
			&m.MaxOutputTokens, &capsJSON, &m.Version, &m.CreatedAtMs, &m.UpdatedAtMs,
		); err != nil {
			return fmt.Errorf("snapshot: scan logical_models: %w", err)
		}
		var orgKey string
		if orgID.Valid {
			v := orgID.String
			m.OrgID = &v
			orgKey = v
		}
		if m.BaseCapabilities, err = parseStringMapStrict(capsJSON); err != nil {
			return fmt.Errorf("snapshot: decode logical_model %q base_capabilities_json: %w", m.ID, err)
		}
		view.logicalModelsByID[m.ID] = &m
		view.logicalModelsBySlug[LogicalModelKey{OrgID: orgKey, Slug: m.CanonicalSlug}] = &m
	}
	return rows.Err()
}

func loadModelDeploymentsSQL(ctx context.Context, tx *sql.Tx, view *ProviderCatalogView) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, org_id, connection_id, logical_model_id, upstream_model_name,
		       native_dialect, capability_overrides_json, assurance_level,
		       pricing_nano_usd_json, state, cooldown_until_ms,
		       connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms,
		       version, created_at_ms, updated_at_ms
		FROM model_deployments
	`)
	if err != nil {
		return fmt.Errorf("snapshot: query model_deployments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			d             ModelDeploymentView
			nativeDialect sql.NullString
			capsJSON      string
			cooldown      sql.NullInt64
		)
		if err := rows.Scan(
			&d.ID, &d.OrgID, &d.ConnectionID, &d.LogicalModelID, &d.UpstreamModelName,
			&nativeDialect, &capsJSON, &d.AssuranceLevel,
			&d.PricingJSON, &d.State, &cooldown,
			&d.ConnectTimeoutMs, &d.FirstByteTimeoutMs, &d.IdleChunkTimeoutMs,
			&d.Version, &d.CreatedAtMs, &d.UpdatedAtMs,
		); err != nil {
			return fmt.Errorf("snapshot: scan model_deployments: %w", err)
		}
		if nativeDialect.Valid {
			d.NativeDialect = nativeDialect.String
		}
		if cooldown.Valid {
			v := cooldown.Int64
			d.CooldownUntilMs = &v
		}
		if d.CapabilityOverrides, err = parseStringMapStrict(capsJSON); err != nil {
			return fmt.Errorf("snapshot: decode deployment %q capability_overrides_json: %w", d.ID, err)
		}
		if err := parsePricingNanoUSDStrict(d.PricingJSON, &d.Pricing); err != nil {
			return fmt.Errorf("snapshot: decode deployment %q pricing_nano_usd_json: %w", d.ID, err)
		}

		if conn, ok := view.connectionsByID[d.ConnectionID]; ok && conn.OrgID == d.OrgID {
			d.DataRegion = conn.DataRegion
		}
		if lm, ok := view.logicalModelsByID[d.LogicalModelID]; ok {
			if lm.OrgID == nil || *lm.OrgID == d.OrgID {
				d.CanonicalModelSlug = lm.CanonicalSlug
			}
		}
		view.deploymentsByID[d.ID] = &d
		if d.CanonicalModelSlug != "" {
			key := LogicalModelKey{OrgID: d.OrgID, Slug: d.CanonicalModelSlug}
			view.deploymentsByOrgSlug[key] = append(view.deploymentsByOrgSlug[key], &d)
		}
	}
	return rows.Err()
}

func parseJSONStringSliceStrict(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	if trimmed == "null" {
		return nil, fmt.Errorf("snapshot: expected JSON string array, got null")
	}
	var out []string
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil || out == nil {
		return nil, fmt.Errorf("snapshot: expected JSON string array: %v", err)
	}
	return out, nil
}

func parsePricingNanoUSDStrict(raw string, out *PricingNanoUSD) error {
	trimmed := strings.TrimSpace(raw)
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil || obj == nil {
		return fmt.Errorf("expected JSON object for pricing_nano_usd_json")
	}
	if err := json.Unmarshal([]byte(trimmed), out); err != nil {
		return err
	}
	if out.InputPer1MNanoUSD < 0 || out.OutputPer1MNanoUSD < 0 ||
		out.CacheReadPer1MNanoUSD < 0 || out.CacheWritePer1MNanoUSD < 0 ||
		out.ReasoningPer1MNanoUSD < 0 {
		return fmt.Errorf("negative pricing rate in pricing_nano_usd_json")
	}
	return nil
}

func cloneStrPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func parseStringMapStrict(raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return map[string]string{}, nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("snapshot: expected JSON object, got null")
	}
	return out, nil
}

func cloneStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return map[string]string{}
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
