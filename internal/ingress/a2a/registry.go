// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package a2a implements Tanpopo's Inter-Agent A2A Plane (/a2a/*) governed by
// dedicated state machine SM-09, monotonic privilege/residency/budget attenuation,
// hop_count <= 5 + cycle detection (CT-23), BlindHostCredentialBroker SSRF/credential
// isolation, and PostgreSQL FORCE RLS / SQLite dual-DB parity (TP-IMP-0042).
package a2a

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// MaxDelegationHopsCeiling enforces the hard CT-23 / SM-09 hop_count ceiling.
	MaxDelegationHopsCeiling = 5
	// AgentBrokerSlotName is the slot key used by BrokerResolver for A2A upstream auth.
	AgentBrokerSlotName = "a2a_agent"

	AgentStatusActive    = "ACTIVE"
	AgentStatusSuspended = "SUSPENDED"
	AgentStatusDisabled  = "DISABLED"
)

var (
	agentSlugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`)

	ErrInvalidAgentConfig = errors.New("a2a: invalid agent configuration")
	ErrAgentAlreadyExists = errors.New("a2a: agent slug already exists in project")
	ErrAgentNotFound      = port.ErrNotFound
)

// AgentSkill models a single published AgentCard skill entry matching openapi/dataplane-v1.yaml.
type AgentSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AgentCard models the OpenAPI A2aAgentCard representation returned by /a2a/agents and /.well-known/agent.json.
type AgentCard struct {
	AgentID           string       `json:"agent_id"`
	AgentSlug         string       `json:"agent_slug,omitempty"`
	Name              string       `json:"name"`
	Description       string       `json:"description,omitempty"`
	EndpointURL       string       `json:"endpoint_url"`
	DataRegion        string       `json:"data_region"`
	MaxDelegationHops int          `json:"max_delegation_hops"`
	Skills            []AgentSkill `json:"skills"`
	RequiredModels    []string     `json:"required_models,omitempty"`
	Status            string       `json:"status,omitempty"`
	ProjectID         string       `json:"project_id,omitempty"`
	Version           int64        `json:"version,omitempty"`
}

// AgentEndpoint mirrors a durable row in agent_endpoints.
type AgentEndpoint struct {
	ID                string
	OrgID             string
	ProjectID         string
	AgentSlug         string
	EndpointURL       string
	AllowPrivateIP    bool
	AllowedCIDRs      []string
	CredentialID      string
	DataRegion        string
	Card              AgentCard
	MaxDelegationHops int
	Status            string
	Version           int64
	CreatedAtMs       int64
	UpdatedAtMs       int64
}

func (a AgentEndpoint) EgressPolicy() (*egress.Policy, error) {
	parsed, err := url.Parse(a.EndpointURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("%w: invalid endpoint URL", ErrInvalidAgentConfig)
	}
	return &egress.Policy{
		AllowedHosts:          []string{parsed.Host},
		AllowPrivateIP:        a.AllowPrivateIP,
		AllowedCIDRs:          append([]string(nil), a.AllowedCIDRs...),
		RequireTLS:            parsed.Scheme == "https",
		RequirePrivateForHTTP: parsed.Scheme == "http",
	}, nil
}

// RegisterAgentParams carries validated Admin Control Plane registration fields.
type RegisterAgentParams struct {
	OrgID             string
	ProjectID         string
	ActorPrincipalID  string
	AgentSlug         string
	Name              string
	Description       string
	EndpointURL       string
	AllowPrivateIP    bool
	AllowedCIDRs      []string
	CredentialID      *string
	DataRegion        string
	MaxDelegationHops int
	Skills            []AgentSkill
	RequiredModels    []string
}

// Registry provides dual-DB (PostgreSQL FORCE RLS + SQLite) agent_endpoints persistence.
type Registry struct {
	db           *sql.DB
	dialect      port.Dialect
	audit        port.AuditSink
	now          func() time.Time
	hostResolver egress.HostResolver
	blockedPorts map[int]bool
}

// RegistryConfig configures a Registry instance.
type RegistryConfig struct {
	DB           *sql.DB
	Audit        port.AuditSink
	Now          func() time.Time
	HostResolver egress.HostResolver
	BlockedPorts []int
}

// NewRegistry constructs an A2A AgentEndpoint registry.
func NewRegistry(cfg RegistryConfig) (*Registry, error) {
	if cfg.DB == nil {
		return nil, errors.New("a2a: non-nil database required")
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	res := cfg.HostResolver
	if res == nil {
		res = egress.NewStdHostResolver()
	}
	bp := make(map[int]bool, len(cfg.BlockedPorts))
	for _, p := range cfg.BlockedPorts {
		if p > 0 && p <= 65535 {
			bp[p] = true
		}
	}
	return &Registry{
		db:           cfg.DB,
		dialect:      port.DetectDialect(context.Background(), cfg.DB),
		audit:        cfg.Audit,
		now:          nowFn,
		hostResolver: res,
		blockedPorts: bp,
	}, nil
}

// RegisterAgent persists a new agent_endpoints row with SSRF validation and hop bounds [1..5].
func (r *Registry) RegisterAgent(ctx context.Context, params RegisterAgentParams) (*AgentEndpoint, error) {
	orgID := strings.TrimSpace(params.OrgID)
	projID := strings.TrimSpace(params.ProjectID)
	name := strings.TrimSpace(params.Name)
	if orgID == "" || projID == "" || name == "" {
		return nil, fmt.Errorf("%w: org_id, project_id, and name are required", ErrInvalidAgentConfig)
	}
	slug := strings.TrimSpace(params.AgentSlug)
	if slug == "" {
		slug = deriveAgentSlug(name)
	}
	if !agentSlugRe.MatchString(slug) {
		return nil, fmt.Errorf("%w: invalid agent_slug %q", ErrInvalidAgentConfig, slug)
	}
	region, ok := normalizeDataRegion(params.DataRegion)
	if !ok {
		return nil, fmt.Errorf("%w: invalid data_region %q (expected EU, US, APAC, or ON_PREM)", ErrInvalidAgentConfig, params.DataRegion)
	}
	hops := params.MaxDelegationHops
	if hops == 0 {
		hops = MaxDelegationHopsCeiling
	}
	if hops < 1 || hops > MaxDelegationHopsCeiling {
		return nil, fmt.Errorf("%w: max_delegation_hops %d must be between 1 and %d", ErrInvalidAgentConfig, hops, MaxDelegationHopsCeiling)
	}
	rawURL := strings.TrimSpace(params.EndpointURL)
	parsedURL, parseErr := url.ParseRequestURI(rawURL)
	if parseErr != nil || parsedURL.Hostname() == "" || parsedURL.User != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return nil, fmt.Errorf("%w: endpoint_url must be an absolute HTTP(S) URL without userinfo", ErrInvalidAgentConfig)
	}
	if parsedURL.Scheme != "https" && !params.AllowPrivateIP {
		return nil, fmt.Errorf("%w: HTTP agent endpoints require allow_private_ip=true", ErrInvalidAgentConfig)
	}
	cidrs := normalizeStrings(params.AllowedCIDRs)
	if params.AllowPrivateIP && len(cidrs) == 0 {
		return nil, fmt.Errorf("%w: allowed_cidrs is required when allow_private_ip is true", ErrInvalidAgentConfig)
	}
	for _, rawCIDR := range cidrs {
		pfx, pfxErr := netip.ParsePrefix(rawCIDR)
		if pfxErr != nil || pfx.Bits() == 0 {
			return nil, fmt.Errorf("%w: invalid or wildcard CIDR %q in allowed_cidrs", ErrInvalidAgentConfig, rawCIDR)
		}
	}
	if err := egress.ValidateStaticEndpointURL(ctx, rawURL, params.AllowPrivateIP, r.hostResolver, r.blockedPorts); err != nil {
		return nil, fmt.Errorf("%w: endpoint_url failed SSRF guard: %v", ErrInvalidAgentConfig, err)
	}
	var credID string
	if params.CredentialID != nil {
		credID = strings.TrimSpace(*params.CredentialID)
	}

	cidrsJSONBytes, err := json.Marshal(cidrs)
	if err != nil {
		return nil, err
	}

	id, err := idgen.New(idgen.PrefixAgent)
	if err != nil {
		return nil, err
	}
	skills := params.Skills
	if skills == nil {
		skills = []AgentSkill{}
	}
	reqModels := normalizeStrings(params.RequiredModels)
	card := AgentCard{
		AgentID:           id,
		AgentSlug:         slug,
		Name:              name,
		Description:       strings.TrimSpace(params.Description),
		EndpointURL:       rawURL,
		DataRegion:        region,
		MaxDelegationHops: hops,
		Skills:            skills,
		RequiredModels:    reqModels,
		Status:            AgentStatusActive,
		ProjectID:         projID,
		Version:           1,
	}
	cardJSONBytes, err := json.Marshal(card)
	if err != nil {
		return nil, err
	}

	nowMs := r.now().UTC().UnixMilli()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("a2a: begin register tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, r.dialect, orgID); err != nil {
		return nil, err
	}

	var credArg any
	if credID != "" {
		credArg = credID
	}
	if _, err := tx.ExecContext(ctx, port.Rebind(r.dialect, `
		INSERT INTO agent_endpoints
			(id, org_id, project_id, agent_slug, endpoint_url, allow_private_ip, allowed_cidrs_json,
			 credential_id, data_region, agent_card_json, max_delegation_hops, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ACTIVE', 1, ?, ?)`),
		id, orgID, projID, slug, rawURL, port.BoolVal(r.dialect, params.AllowPrivateIP), string(cidrsJSONBytes),
		credArg, region, string(cardJSONBytes), hops, nowMs, nowMs,
	); err != nil {
		if isA2AUniqueViolation(err) {
			return nil, ErrAgentAlreadyExists
		}
		return nil, fmt.Errorf("a2a: insert agent_endpoints: %w", err)
	}

	if err := r.appendAudit(ctx, tx, orgID, params.ActorPrincipalID, "PRINCIPAL", port.AuditEntryPointAdminAPI,
		"a2a.agent.registered", "agent_endpoint", id, "", map[string]any{
			"agent_slug":          slug,
			"project_id":          projID,
			"data_region":         region,
			"max_delegation_hops": hops,
		}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("a2a: commit register agent: %w", err)
	}

	return &AgentEndpoint{
		ID:                id,
		OrgID:             orgID,
		ProjectID:         projID,
		AgentSlug:         slug,
		EndpointURL:       rawURL,
		AllowPrivateIP:    params.AllowPrivateIP,
		AllowedCIDRs:      cidrs,
		CredentialID:      credID,
		DataRegion:        region,
		Card:              card,
		MaxDelegationHops: hops,
		Status:            AgentStatusActive,
		Version:           1,
		CreatedAtMs:       nowMs,
		UpdatedAtMs:       nowMs,
	}, nil
}

// GetAgent fetches a single tenant-scoped agent endpoint by id or agent_slug.
func (r *Registry) GetAgent(ctx context.Context, orgID, agentIDOrSlug string) (*AgentEndpoint, error) {
	return r.GetAgentInProject(ctx, orgID, "", agentIDOrSlug)
}

// GetAgentInProject fetches a tenant-scoped agent endpoint; when projectID is non-empty,
// slug lookups are scoped strictly to that project to prevent cross-project slug collision.
func (r *Registry) GetAgentInProject(ctx context.Context, orgID, projectID, agentIDOrSlug string) (*AgentEndpoint, error) {
	if r == nil || r.db == nil || strings.TrimSpace(orgID) == "" || strings.TrimSpace(agentIDOrSlug) == "" {
		return nil, port.ErrInvalidInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, r.dialect, orgID); err != nil {
		return nil, err
	}
	var ep AgentEndpoint
	var allowPriv port.FlexBool
	var cidrsJSON, cardJSON string
	var credID sql.NullString
	cleanLookup := strings.TrimSpace(agentIDOrSlug)
	cleanProj := strings.TrimSpace(projectID)
	q := `SELECT id, org_id, project_id, agent_slug, endpoint_url, allow_private_ip, allowed_cidrs_json,
		       credential_id, data_region, agent_card_json, max_delegation_hops, status, version, created_at_ms, updated_at_ms
		FROM agent_endpoints
		WHERE org_id = ? AND (id = ? OR agent_slug = ?)
		LIMIT 1`
	args := []any{orgID, cleanLookup, cleanLookup}
	if cleanProj != "" {
		q = `SELECT id, org_id, project_id, agent_slug, endpoint_url, allow_private_ip, allowed_cidrs_json,
		       credential_id, data_region, agent_card_json, max_delegation_hops, status, version, created_at_ms, updated_at_ms
		FROM agent_endpoints
		WHERE org_id = ? AND project_id = ? AND (id = ? OR agent_slug = ?)
		LIMIT 1`
		args = []any{orgID, cleanProj, cleanLookup, cleanLookup}
	}
	err = tx.QueryRowContext(ctx, port.Rebind(r.dialect, q), args...).
		Scan(&ep.ID, &ep.OrgID, &ep.ProjectID, &ep.AgentSlug, &ep.EndpointURL, &allowPriv, &cidrsJSON,
			&credID, &ep.DataRegion, &cardJSON, &ep.MaxDelegationHops, &ep.Status, &ep.Version, &ep.CreatedAtMs, &ep.UpdatedAtMs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}
	ep.AllowPrivateIP = bool(allowPriv)
	if credID.Valid {
		ep.CredentialID = credID.String
	}
	_ = json.Unmarshal([]byte(cidrsJSON), &ep.AllowedCIDRs)
	_ = json.Unmarshal([]byte(cardJSON), &ep.Card)
	ep.Card.AgentID = ep.ID
	ep.Card.AgentSlug = ep.AgentSlug
	ep.Card.EndpointURL = ep.EndpointURL
	ep.Card.DataRegion = ep.DataRegion
	ep.Card.MaxDelegationHops = ep.MaxDelegationHops
	ep.Card.Status = ep.Status
	ep.Card.ProjectID = ep.ProjectID
	ep.Card.Version = ep.Version
	if ep.Card.Skills == nil {
		ep.Card.Skills = []AgentSkill{}
	}
	return &ep, nil
}

// ListAdminAgents returns tenant/project-scoped registered agent cards for Control Plane management.
func (r *Registry) ListAdminAgents(ctx context.Context, orgID string, projectIDs []string, orgWide bool, limit int) ([]AgentCard, error) {
	if r == nil || r.db == nil || strings.TrimSpace(orgID) == "" {
		return nil, port.ErrInvalidInput
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if !orgWide && len(projectIDs) == 0 {
		return []AgentCard{}, nil
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, r.dialect, orgID); err != nil {
		return nil, err
	}

	q := `SELECT id, project_id, agent_slug, endpoint_url, data_region, agent_card_json, max_delegation_hops, status, version
	      FROM agent_endpoints WHERE org_id = ?`
	args := []any{orgID}
	if !orgWide {
		q += ` AND project_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(projectIDs)), ",") + `)`
		for _, pid := range projectIDs {
			args = append(args, pid)
		}
	}
	q += ` ORDER BY agent_slug, id LIMIT ?`
	args = append(args, limit)

	rows, err := tx.QueryContext(ctx, port.Rebind(r.dialect, q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AgentCard{}
	for rows.Next() {
		var id, projID, slug, epURL, region, cardJSON, status string
		var hops int
		var ver int64
		if err := rows.Scan(&id, &projID, &slug, &epURL, &region, &cardJSON, &hops, &status, &ver); err != nil {
			return nil, err
		}
		var card AgentCard
		_ = json.Unmarshal([]byte(cardJSON), &card)
		card.AgentID = id
		card.ProjectID = projID
		card.AgentSlug = slug
		card.EndpointURL = epURL
		card.DataRegion = region
		card.MaxDelegationHops = hops
		card.Status = status
		card.Version = ver
		if card.Name == "" {
			card.Name = slug
		}
		if card.Skills == nil {
			card.Skills = []AgentSkill{}
		}
		out = append(out, card)
	}
	return out, rows.Err()
}

// AdminAgentPage holds a keyset-paginated slice of A2A AgentCards for Admin HTTP.
type AdminAgentPage struct {
	Items   []AgentCard
	HasMore bool
	LastMs  int64
	LastID  string
}

// ListAdminAgentsPage returns a keyset-paginated slice ordered by (created_at_ms DESC, id DESC).
func (r *Registry) ListAdminAgentsPage(ctx context.Context, orgID string, projectIDs []string, orgWide bool, limit int, cursorCreatedAtMs *int64, cursorID *string) (AdminAgentPage, error) {
	if r == nil || r.db == nil || strings.TrimSpace(orgID) == "" {
		return AdminAgentPage{}, port.ErrInvalidInput
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if !orgWide && len(projectIDs) == 0 {
		return AdminAgentPage{Items: []AgentCard{}}, nil
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminAgentPage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, r.dialect, orgID); err != nil {
		return AdminAgentPage{}, err
	}

	q := `SELECT id, project_id, agent_slug, endpoint_url, data_region, agent_card_json, max_delegation_hops, status, version, created_at_ms
	      FROM agent_endpoints WHERE org_id = ?`
	args := []any{orgID}
	if !orgWide {
		q += ` AND project_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(projectIDs)), ",") + `)`
		for _, pid := range projectIDs {
			args = append(args, pid)
		}
	}
	if cursorCreatedAtMs != nil && cursorID != nil && strings.TrimSpace(*cursorID) != "" {
		q += ` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`
		args = append(args, *cursorCreatedAtMs, *cursorCreatedAtMs, strings.TrimSpace(*cursorID))
	}
	q += ` ORDER BY created_at_ms DESC, id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := tx.QueryContext(ctx, port.Rebind(r.dialect, q), args...)
	if err != nil {
		return AdminAgentPage{}, err
	}
	defer rows.Close()

	out := []AgentCard{}
	var timestamps []int64
	for rows.Next() {
		var id, projID, slug, epURL, region, cardJSON, status string
		var hops int
		var ver, createdMs int64
		if err := rows.Scan(&id, &projID, &slug, &epURL, &region, &cardJSON, &hops, &status, &ver, &createdMs); err != nil {
			return AdminAgentPage{}, err
		}
		var card AgentCard
		_ = json.Unmarshal([]byte(cardJSON), &card)
		card.AgentID = id
		card.ProjectID = projID
		card.AgentSlug = slug
		card.EndpointURL = epURL
		card.DataRegion = region
		card.MaxDelegationHops = hops
		card.Status = status
		card.Version = ver
		if card.Name == "" {
			card.Name = slug
		}
		if card.Skills == nil {
			card.Skills = []AgentSkill{}
		}
		out = append(out, card)
		timestamps = append(timestamps, createdMs)
	}
	if err := rows.Err(); err != nil {
		return AdminAgentPage{}, err
	}
	hasMore := false
	if len(out) > limit {
		hasMore = true
		out = out[:limit]
		timestamps = timestamps[:limit]
	}
	page := AdminAgentPage{Items: out, HasMore: hasMore}
	if len(out) > 0 {
		page.LastMs = timestamps[len(out)-1]
		page.LastID = out[len(out)-1].AgentID
	}
	return page, nil
}

// DiscoverFilter bounds Data Plane A2A AgentCard discovery to authorized scope & residency.
type DiscoverFilter struct {
	OrgID              string
	ProjectID          string
	CallerRegion       string
	AllowedDataRegions []string
	AllowedModels      []string
	Limit              int
}

func regionAllowed(agentRegion, callerRegion string, enforcedRegions []string) bool {
	ar := strings.ToUpper(strings.TrimSpace(agentRegion))
	if cr := strings.ToUpper(strings.TrimSpace(callerRegion)); cr != "" && ar != cr {
		return false
	}
	if len(enforcedRegions) > 0 {
		matched := false
		for _, r := range enforcedRegions {
			trimmed := strings.ToUpper(strings.TrimSpace(r))
			if trimmed == "*" || trimmed == ar {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// ListDiscoverableAgents returns ACTIVE agents filtered strictly by project, caller/policy residency,
// and Virtual Key privilege intersection before cursor truncation (TP-FR-0405 / CT-23).
func (r *Registry) ListDiscoverableAgents(ctx context.Context, filter DiscoverFilter) ([]AgentCard, error) {
	all, err := r.ListAdminAgents(ctx, filter.OrgID, []string{filter.ProjectID}, false, 200)
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out := make([]AgentCard, 0, len(all))
	for _, card := range all {
		if card.Status != AgentStatusActive {
			continue
		}
		if !regionAllowed(card.DataRegion, filter.CallerRegion, filter.AllowedDataRegions) {
			continue
		}
		if !callerPrivilegeAllowsCard(filter.AllowedModels, card) {
			continue
		}
		out = append(out, card)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// callerPrivilegeAllowsCard enforces CT-23 monotonic privilege narrowing:
// empty AllowedModels denies all model-requiring agents unless explicitly wildcarded ("*").
func callerPrivilegeAllowsCard(callerAllowedModels []string, card AgentCard) bool {
	if len(card.RequiredModels) == 0 {
		return true
	}
	if len(callerAllowedModels) == 0 {
		return false
	}
	allowed := make(map[string]struct{}, len(callerAllowedModels))
	for _, m := range callerAllowedModels {
		if t := strings.TrimSpace(m); t != "" {
			allowed[t] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return false
	}
	if _, wildcard := allowed["*"]; wildcard {
		return true
	}
	for _, req := range card.RequiredModels {
		if _, ok := allowed[strings.TrimSpace(req)]; !ok {
			return false
		}
	}
	return true
}

// BrokerResolver resolves registered A2A agent egress policies and vault credentials for
// BlindHostCredentialBroker with strict audience matching and zero plaintext exposure.
type BrokerResolver struct {
	DB *sql.DB
}

var _ broker.CredentialBindingResolver = BrokerResolver{}
var _ broker.ConnectionPolicyResolver = BrokerResolver{}

func (r BrokerResolver) ResolveConnectionPolicy(ctx context.Context, orgID, agentID string) (*egress.Policy, error) {
	if r.DB == nil || strings.TrimSpace(orgID) == "" || strings.TrimSpace(agentID) == "" {
		return nil, port.ErrInvalidInput
	}
	reg, err := NewRegistry(RegistryConfig{DB: r.DB})
	if err != nil {
		return nil, err
	}
	ep, err := reg.GetAgent(ctx, orgID, agentID)
	if err != nil {
		return nil, err
	}
	return ep.EgressPolicy()
}

func (r BrokerResolver) ResolveSlot(ctx context.Context, orgID, agentID, slotName string) (*broker.ResolvedSlot, error) {
	if r.DB == nil || strings.TrimSpace(orgID) == "" || strings.TrimSpace(agentID) == "" || slotName != AgentBrokerSlotName {
		return nil, port.ErrInvalidInput
	}
	reg, err := NewRegistry(RegistryConfig{DB: r.DB})
	if err != nil {
		return nil, err
	}
	ep, err := reg.GetAgent(ctx, orgID, agentID)
	if err != nil {
		return nil, err
	}
	if ep.CredentialID == "" {
		return nil, port.ErrNotFound
	}
	parsed, err := url.Parse(ep.EndpointURL)
	if err != nil || parsed.Host == "" {
		return nil, port.ErrInvalidInput
	}
	dialect := port.DetectDialect(ctx, r.DB)
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, dialect, orgID); err != nil {
		return nil, err
	}
	var audience, ownerProj sql.NullString
	var kind string
	err = tx.QueryRowContext(ctx, port.Rebind(dialect, `
		SELECT bound_audience, owner_project_id, secret_kind
		FROM secret_credentials WHERE org_id = ? AND id = ?`), orgID, ep.CredentialID).
		Scan(&audience, &ownerProj, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	normalizedKind := strings.ToUpper(strings.TrimSpace(kind))
	if normalizedKind != "API_KEY" && normalizedKind != "OAUTH2_TOKEN_SET" {
		return nil, fmt.Errorf("%w: credential kind %q cannot be bound as an A2A Bearer secret", ErrInvalidAgentConfig, kind)
	}
	if !ownerProj.Valid || ownerProj.String != ep.ProjectID {
		return nil, port.ErrNotFound
	}
	if !audience.Valid || !strings.EqualFold(strings.TrimSpace(audience.String), strings.TrimSpace(parsed.Host)) {
		return nil, errors.New("a2a: credential bound_audience must exactly match agent endpoint host")
	}
	return &broker.ResolvedSlot{
		OrgID:          orgID,
		ConnectionID:   ep.ID,
		SlotName:       slotName,
		SecretID:       ep.CredentialID,
		InjectMode:     broker.InjectModeHostBlind,
		BoundAudience:  &audience.String,
		AllowedHosts:   []string{parsed.Host},
		AllowPrivateIP: ep.AllowPrivateIP,
		AllowedCIDRs:   append([]string(nil), ep.AllowedCIDRs...),
		RequireTLS:     parsed.Scheme == "https",
	}, nil
}

func (r *Registry) appendAudit(ctx context.Context, tx *sql.Tx, orgID, actorID, actorKind, entryPoint, eventType, targetType, targetID, reqID string, meta map[string]any) error {
	if r.audit == nil {
		return nil
	}
	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return err
	}
	rawMeta, _ := json.Marshal(meta)
	var reqPtr *string
	if strings.TrimSpace(reqID) != "" {
		v := reqID
		reqPtr = &v
	}
	orgCopy := orgID
	if actorID == "" {
		actorID = "a2a_plane"
	}
	ev := port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  r.now().UTC().UnixMilli(),
		OrgID:        &orgCopy,
		ActorKind:    actorKind,
		ActorID:      actorID,
		EntryPoint:   entryPoint,
		EventType:    eventType,
		TargetType:   targetType,
		TargetID:     targetID,
		RequestID:    reqPtr,
		MetadataJSON: string(rawMeta),
	}
	if txAudit, ok := r.audit.(port.TxAuditSink); ok && tx != nil {
		return txAudit.AppendTx(ctx, tx, ev)
	}
	return r.audit.Append(ctx, ev)
}

func normalizeDataRegion(raw string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "EU", "US", "APAC", "ON_PREM":
		return strings.ToUpper(strings.TrimSpace(raw)), true
	default:
		return "", false
	}
}

func normalizeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, s := range items {
		t := strings.TrimSpace(s)
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

func deriveAgentSlug(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		case !prevDash && b.Len() > 0:
			b.WriteByte('-')
			prevDash = true
		}
		if b.Len() >= 48 {
			break
		}
	}
	res := strings.Trim(b.String(), "-")
	if res == "" {
		return "agent"
	}
	return res
}

func isA2AUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
