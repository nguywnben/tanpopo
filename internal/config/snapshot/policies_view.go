// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package snapshot

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// PolicyScopeType constants matching policies.scope_type CHECK constraint.
const (
	PolicyScopeOrg        = "ORG"
	PolicyScopeProject    = "PROJECT"
	PolicyScopeEnv        = "ENV"
	PolicyScopeVirtualKey = "VIRTUAL_KEY"
)

// PolicyType constants matching policies.policy_type CHECK constraint.
const (
	PolicyTypeModelAllowlist = "MODEL_ALLOWLIST"
	PolicyTypeDataResidency  = "DATA_RESIDENCY"
	PolicyTypeRateLimit      = "RATE_LIMIT"
	PolicyTypeGuardrail      = "GUARDRAIL"
	PolicyTypeMCPGovernance  = "MCP_GOVERNANCE"
	PolicyTypeResponseCache  = "RESPONSE_CACHE"
)

// PolicyFailureMode constants matching policies.failure_mode CHECK constraint.
const (
	PolicyFailureClosed = "FAIL_CLOSED"
	PolicyFailureOpen   = "FAIL_OPEN"
)

// ErrPolicyConfigCorrupted indicates a policy snapshot load or parse failure requiring Fail-Closed rejection.
var ErrPolicyConfigCorrupted = errors.New("snapshot: policy configuration corrupted or unavailable")

// PolicyScopeKey indexes policies by (org_id, scope_type, scope_id).
type PolicyScopeKey struct {
	OrgID     string
	ScopeType string
	ScopeID   string
}

// GuardrailHookSpec declares a guardrail extension hook and its failure posture.
type GuardrailHookSpec struct {
	ExtensionID    string `json:"extension_id"`
	FailurePosture string `json:"failure_posture"`
}

// PolicyRulesView holds parsed, normalized rules from policies.rules_json.
type PolicyRulesView struct {
	HasAllowedModels       bool
	AllowedModels          []string
	DeniedModels           []string
	HasAllowedAdapters     bool
	AllowedAdapters        []string
	DeniedAdapters         []string
	HasAllowedDataRegions  bool
	AllowedDataRegions     []string
	MinPluginTrust         string
	MinCapabilityAssurance string
	RPMLimit               *int64
	TPMLimit               *int64
	MaxConcurrentStreams   *int64
	MaxContextTokens       *int64
	MaxRequestBodyBytes    *int64
	GuardrailHooks         []GuardrailHookSpec
}

// Clone returns a deep copy of PolicyRulesView.
func (r PolicyRulesView) Clone() PolicyRulesView {
	cp := r
	if r.AllowedModels != nil {
		cp.AllowedModels = append([]string(nil), r.AllowedModels...)
	}
	if r.DeniedModels != nil {
		cp.DeniedModels = append([]string(nil), r.DeniedModels...)
	}
	if r.AllowedAdapters != nil {
		cp.AllowedAdapters = append([]string(nil), r.AllowedAdapters...)
	}
	if r.DeniedAdapters != nil {
		cp.DeniedAdapters = append([]string(nil), r.DeniedAdapters...)
	}
	if r.AllowedDataRegions != nil {
		cp.AllowedDataRegions = append([]string(nil), r.AllowedDataRegions...)
	}
	cp.RPMLimit = cloneInt64Ptr(r.RPMLimit)
	cp.TPMLimit = cloneInt64Ptr(r.TPMLimit)
	cp.MaxConcurrentStreams = cloneInt64Ptr(r.MaxConcurrentStreams)
	cp.MaxContextTokens = cloneInt64Ptr(r.MaxContextTokens)
	cp.MaxRequestBodyBytes = cloneInt64Ptr(r.MaxRequestBodyBytes)
	if len(r.GuardrailHooks) > 0 {
		cp.GuardrailHooks = append([]GuardrailHookSpec(nil), r.GuardrailHooks...)
	}
	return cp
}

// PolicyRecordView is an immutable snapshot record of an enabled policies row.
type PolicyRecordView struct {
	ID          string
	OrgID       string
	ScopeType   string
	ScopeID     string
	PolicyType  string
	FailureMode string
	RulesJSON   string
	ParsedRules PolicyRulesView
	Enabled     bool
	Version     int64
	CreatedAtMs int64
	UpdatedAtMs int64
}

func (p *PolicyRecordView) clone() *PolicyRecordView {
	if p == nil {
		return nil
	}
	cp := *p
	cp.ParsedRules = p.ParsedRules.Clone()
	return &cp
}

// PolicyCatalogView stores immutable indexed policies loaded atomically in a snapshot transaction.
type PolicyCatalogView struct {
	byScope    map[PolicyScopeKey][]*PolicyRecordView
	corruptErr error
}

// LookupPoliciesForScope returns cloned immutable PolicyRecordView entries for (orgID, scopeType, scopeID),
// or returns ErrPolicyConfigCorrupted if any policy row in the snapshot failed to load or parse.
func (s *ConfigSnapshot) LookupPoliciesForScope(orgID, scopeType, scopeID string) ([]*PolicyRecordView, error) {
	if s == nil || s.policies == nil {
		return nil, ErrPolicyConfigCorrupted
	}
	if s.policies.corruptErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrPolicyConfigCorrupted, s.policies.corruptErr)
	}
	if orgID == "" || scopeType == "" || scopeID == "" {
		return nil, nil
	}
	src := s.policies.byScope[PolicyScopeKey{
		OrgID:     orgID,
		ScopeType: scopeType,
		ScopeID:   scopeID,
	}]
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]*PolicyRecordView, len(src))
	for i, r := range src {
		out[i] = r.clone()
	}
	return out, nil
}

// PoliciesHealthErr returns nil when the snapshot policy catalog loaded cleanly,
// or ErrPolicyConfigCorrupted if loading or JSON decoding failed.
func (s *ConfigSnapshot) PoliciesHealthErr() error {
	if s == nil || s.policies == nil {
		return ErrPolicyConfigCorrupted
	}
	if s.policies.corruptErr != nil {
		return fmt.Errorf("%w: %v", ErrPolicyConfigCorrupted, s.policies.corruptErr)
	}
	return nil
}

type rawPolicyRulesJSON struct {
	AllowedModels          json.RawMessage     `json:"allowed_models"`
	DeniedModels           json.RawMessage     `json:"denied_models"`
	AllowedAdapters        json.RawMessage     `json:"allowed_adapters"`
	DeniedAdapters         json.RawMessage     `json:"denied_adapters"`
	AllowedDataRegions     json.RawMessage     `json:"allowed_data_regions"`
	AllowedRegionsAlias    json.RawMessage     `json:"allowed_regions"`
	MinPluginTrust         string              `json:"min_plugin_trust"`
	MinCapabilityAssurance string              `json:"min_capability_assurance"`
	RPMLimit               *int64              `json:"rpm_limit"`
	RPMCeilingAlias        *int64              `json:"rpm_ceiling"`
	TPMLimit               *int64              `json:"tpm_limit"`
	TPMCeilingAlias        *int64              `json:"tpm_ceiling"`
	MaxConcurrentStreams   *int64              `json:"max_concurrent_streams"`
	MaxContextTokens       *int64              `json:"max_context_tokens"`
	MaxRequestBodyBytes    *int64              `json:"max_request_body_bytes"`
	GuardrailHooks         []GuardrailHookSpec `json:"guardrail_hooks"`
}

// ParsePolicyRulesJSONStrict validates and decodes a policies.rules_json document fail-closed.
func ParsePolicyRulesJSONStrict(raw string) (PolicyRulesView, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return PolicyRulesView{}, fmt.Errorf("rules_json must be a JSON object")
	}

	var doc rawPolicyRulesJSON
	dec := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return PolicyRulesView{}, fmt.Errorf("decode rules_json: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return PolicyRulesView{}, fmt.Errorf("rules_json contains trailing data")
	}

	var out PolicyRulesView
	var err error
	if len(doc.AllowedModels) > 0 {
		out.HasAllowedModels = true
		out.AllowedModels, err = decodeNonNullStringArray(doc.AllowedModels, "allowed_models")
		if err != nil {
			return PolicyRulesView{}, err
		}
	}
	if len(doc.DeniedModels) > 0 {
		out.DeniedModels, err = decodeNonNullStringArray(doc.DeniedModels, "denied_models")
		if err != nil {
			return PolicyRulesView{}, err
		}
	}
	if len(doc.AllowedAdapters) > 0 {
		out.HasAllowedAdapters = true
		out.AllowedAdapters, err = decodeNonNullStringArray(doc.AllowedAdapters, "allowed_adapters")
		if err != nil {
			return PolicyRulesView{}, err
		}
	}
	if len(doc.DeniedAdapters) > 0 {
		out.DeniedAdapters, err = decodeNonNullStringArray(doc.DeniedAdapters, "denied_adapters")
		if err != nil {
			return PolicyRulesView{}, err
		}
	}

	if len(doc.AllowedDataRegions) > 0 && len(doc.AllowedRegionsAlias) > 0 {
		return PolicyRulesView{}, fmt.Errorf("specify either allowed_data_regions or allowed_regions, not both")
	}
	regionRaw := doc.AllowedDataRegions
	if len(regionRaw) == 0 {
		regionRaw = doc.AllowedRegionsAlias
	}
	if len(regionRaw) > 0 {
		out.HasAllowedDataRegions = true
		out.AllowedDataRegions, err = decodeNonNullStringArray(regionRaw, "allowed_data_regions")
		if err != nil {
			return PolicyRulesView{}, err
		}
	}

	if strings.TrimSpace(doc.MinPluginTrust) != "" {
		t := strings.ToUpper(strings.TrimSpace(doc.MinPluginTrust))
		switch t {
		case "OFFICIAL_ONLY", "VERIFIED_ONLY", "VERIFIED_OR_ENTERPRISE_SIGNED", "ANY_APPROVED":
			out.MinPluginTrust = t
		default:
			return PolicyRulesView{}, fmt.Errorf("invalid min_plugin_trust %q", doc.MinPluginTrust)
		}
	}
	if strings.TrimSpace(doc.MinCapabilityAssurance) != "" {
		a := strings.ToUpper(strings.TrimSpace(doc.MinCapabilityAssurance))
		switch a {
		case "VERIFIED", "TESTED", "DECLARED":
			out.MinCapabilityAssurance = a
		default:
			return PolicyRulesView{}, fmt.Errorf("invalid min_capability_assurance %q", doc.MinCapabilityAssurance)
		}
	}

	out.RPMLimit, err = coalesceNonNegativeLimit(doc.RPMLimit, doc.RPMCeilingAlias, "rpm_limit")
	if err != nil {
		return PolicyRulesView{}, err
	}
	out.TPMLimit, err = coalesceNonNegativeLimit(doc.TPMLimit, doc.TPMCeilingAlias, "tpm_limit")
	if err != nil {
		return PolicyRulesView{}, err
	}
	out.MaxConcurrentStreams, err = validateNonNegativeLimit(doc.MaxConcurrentStreams, "max_concurrent_streams")
	if err != nil {
		return PolicyRulesView{}, err
	}
	out.MaxContextTokens, err = validatePositiveLimit(doc.MaxContextTokens, "max_context_tokens")
	if err != nil {
		return PolicyRulesView{}, err
	}
	out.MaxRequestBodyBytes, err = validatePositiveLimit(doc.MaxRequestBodyBytes, "max_request_body_bytes")
	if err != nil {
		return PolicyRulesView{}, err
	}

	for _, h := range doc.GuardrailHooks {
		extID := strings.TrimSpace(h.ExtensionID)
		if extID == "" {
			return PolicyRulesView{}, fmt.Errorf("guardrail_hooks entry missing extension_id")
		}
		posture := strings.ToUpper(strings.TrimSpace(h.FailurePosture))
		if posture == "" {
			posture = PolicyFailureClosed
		}
		if posture != PolicyFailureClosed && posture != PolicyFailureOpen {
			return PolicyRulesView{}, fmt.Errorf("guardrail_hooks entry invalid failure_posture %q", h.FailurePosture)
		}
		out.GuardrailHooks = append(out.GuardrailHooks, GuardrailHookSpec{
			ExtensionID:    extID,
			FailurePosture: posture,
		})
	}
	return out, nil
}

func decodeNonNullStringArray(raw json.RawMessage, field string) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, fmt.Errorf("%s must be a JSON string array, not null", field)
	}
	var vals []string
	if err := json.Unmarshal(raw, &vals); err != nil || vals == nil {
		return nil, fmt.Errorf("%s must be a JSON string array", field)
	}
	return normalizeNonEmptyTokens(vals), nil
}

func normalizeNonEmptyTokens(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if tr := strings.TrimSpace(s); tr != "" {
			out = append(out, tr)
		}
	}
	return out
}

func coalesceNonNegativeLimit(primary, alias *int64, field string) (*int64, error) {
	if primary != nil && alias != nil && *primary != *alias {
		return nil, fmt.Errorf("conflicting values for %s and ceiling alias", field)
	}
	v := primary
	if v == nil {
		v = alias
	}
	return validateNonNegativeLimit(v, field)
}

func validateNonNegativeLimit(v *int64, field string) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	if *v < 0 {
		return nil, fmt.Errorf("%s cannot be negative (%d)", field, *v)
	}
	cp := *v
	return &cp, nil
}

func validatePositiveLimit(v *int64, field string) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	if *v <= 0 {
		return nil, fmt.Errorf("%s must be positive (%d)", field, *v)
	}
	cp := *v
	return &cp, nil
}

func cloneInt64Ptr(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// loadPoliciesSQL loads enabled policies atomically. On query or rule parse errors,
// it records corruptErr on the returned PolicyCatalogView rather than skipping bad rows
// so runtime evaluation fails closed with 503 policy_engine_unavailable.
func loadPoliciesSQL(ctx context.Context, tx *sql.Tx) *PolicyCatalogView {
	cat := &PolicyCatalogView{
		byScope: make(map[PolicyScopeKey][]*PolicyRecordView),
	}
	loadPoliciesIntoSQL(ctx, tx, port.DialectSQLite, cat)
	return cat
}

func loadPoliciesIntoSQL(ctx context.Context, tx *sql.Tx, d port.Dialect, cat *PolicyCatalogView) {
	if cat.corruptErr != nil {
		return
	}
	query := fmt.Sprintf(`
		SELECT id, org_id, scope_type, scope_id, policy_type,
		       failure_mode, rules_json, enabled, version, created_at_ms, updated_at_ms
		FROM policies
		WHERE enabled = %s
		ORDER BY created_at_ms ASC, id ASC
	`, sqlBoolExpr(d, true))
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		cat.corruptErr = fmt.Errorf("query policies: %w", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			p           PolicyRecordView
			enabledBool flexBool
		)
		if scanErr := rows.Scan(
			&p.ID, &p.OrgID, &p.ScopeType, &p.ScopeID, &p.PolicyType,
			&p.FailureMode, &p.RulesJSON, &enabledBool, &p.Version, &p.CreatedAtMs, &p.UpdatedAtMs,
		); scanErr != nil {
			cat.corruptErr = fmt.Errorf("scan policy row: %w", scanErr)
			return
		}
		p.Enabled = bool(enabledBool)
		parsed, parseErr := ParsePolicyRulesJSONStrict(p.RulesJSON)
		if parseErr != nil {
			cat.corruptErr = fmt.Errorf("parse policy %q rules_json: %w", p.ID, parseErr)
			return
		}
		p.ParsedRules = parsed

		key := PolicyScopeKey{
			OrgID:     p.OrgID,
			ScopeType: p.ScopeType,
			ScopeID:   p.ScopeID,
		}
		cat.byScope[key] = append(cat.byScope[key], &p)
	}
	if err := rows.Err(); err != nil {
		cat.corruptErr = fmt.Errorf("iterate policies: %w", err)
		return
	}
}
