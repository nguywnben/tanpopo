// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package engine implements Tanpopo's 4-tier hierarchical policy merge algebra
// (Org ∩ Project ∩ Env ∩ VirtualKey) with Monotonic Restrictive Narrowing,
// write-time child relaxation prevention (422 policy_relaxation_forbidden),
// and < 0.2ms Fail-Closed runtime evaluation atop ConfigSnapshot (SPEC-CORE-01 / TP-IMP-0019).
package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
)

// Policy engine error sentinels mapped to canonical HTTP error responses.
var (
	ErrPolicyRelaxationForbidden = errors.New("policy: policy_relaxation_forbidden")
	ErrPolicyEngineUnavailable   = errors.New("policy: policy_engine_unavailable")
	ErrPolicyDeniedModel         = errors.New("policy: policy_denied_model")
	ErrPolicyDeniedRegion        = errors.New("policy: policy_denied_region")
	ErrPolicyDeniedMaxContext    = errors.New("policy: policy_denied_max_context")
	ErrPolicyDeniedTrust         = errors.New("policy: policy_denied_trust")
	ErrPolicyPayloadTooLarge     = errors.New("policy: payload_too_large")
	ErrInvalidPolicyInput        = errors.New("policy: invalid policy input")
)

// Trust tier constants matching organizations.min_plugin_trust CHECK constraint.
const (
	MinTrustOfficialOnly               = "OFFICIAL_ONLY"
	MinTrustVerifiedOnly               = "VERIFIED_ONLY"
	MinTrustVerifiedOrEnterpriseSigned = "VERIFIED_OR_ENTERPRISE_SIGNED"
	MinTrustAnyApproved                = "ANY_APPROVED"
)

// Capability assurance constants ordered by strictness: VERIFIED > TESTED > DECLARED.
const (
	AssuranceVerified = "VERIFIED"
	AssuranceTested   = "TESTED"
	AssuranceDeclared = "DECLARED"
)

// ScopedLayer captures policy rules contributed by a single hierarchy tier
// (ORG, PROJECT, ENV, or VIRTUAL_KEY).
type ScopedLayer struct {
	ScopeType string
	ScopeID   string
	Baseline  bool
	Rules     snapshot.PolicyRulesView
}

// EffectivePolicy holds the deterministically merged intersection across
// Org ∩ Project ∩ Env ∩ VirtualKey.
type EffectivePolicy struct {
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
	GuardrailHooks         []snapshot.GuardrailHookSpec

	allowedModelsSet         map[string]struct{}
	deniedModelsSet          map[string]struct{}
	allowedAdapterSet        map[string]struct{}
	deniedAdapterSet         map[string]struct{}
	allowedRegionSet         map[string]struct{}
	wildcardModels           bool
	wildcardAdapters         bool
	wildcardRegions          bool
	hasAncestorAllowedModels bool
	ancestorWildcardModels   bool
	ancestorAllowedModelsSet map[string]struct{}
}

// TrustStrictnessRank returns monotonic strictness order:
// OFFICIAL_ONLY (4) > VERIFIED_ONLY (3) > VERIFIED_OR_ENTERPRISE_SIGNED (2) > ANY_APPROVED (1).
func TrustStrictnessRank(tier string) int {
	switch strings.ToUpper(strings.TrimSpace(tier)) {
	case MinTrustOfficialOnly:
		return 4
	case MinTrustVerifiedOnly:
		return 3
	case MinTrustVerifiedOrEnterpriseSigned:
		return 2
	case MinTrustAnyApproved:
		return 1
	default:
		return 0
	}
}

// AssuranceStrictnessRank returns monotonic strictness order:
// VERIFIED (3) > TESTED (2) > DECLARED (1).
func AssuranceStrictnessRank(level string) int {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case AssuranceVerified:
		return 3
	case AssuranceTested:
		return 2
	case AssuranceDeclared:
		return 1
	default:
		return 0
	}
}

// ScopeHierarchyRank returns hierarchy depth: ORG(1) < PROJECT(2) < ENV(3) < VIRTUAL_KEY(4).
func ScopeHierarchyRank(scopeType string) int {
	switch strings.ToUpper(strings.TrimSpace(scopeType)) {
	case snapshot.PolicyScopeOrg:
		return 1
	case snapshot.PolicyScopeProject:
		return 2
	case snapshot.PolicyScopeEnv:
		return 3
	case snapshot.PolicyScopeVirtualKey:
		return 4
	default:
		return 0
	}
}

// MergeLayers applies Monotonic Restrictive Narrowing across ordered layers (Org -> Project -> Env -> VirtualKey).
func MergeLayers(layers ...ScopedLayer) EffectivePolicy {
	var out EffectivePolicy
	out.allowedModelsSet = make(map[string]struct{})
	out.ancestorAllowedModelsSet = make(map[string]struct{})
	out.deniedModelsSet = make(map[string]struct{})
	out.allowedAdapterSet = make(map[string]struct{})
	out.deniedAdapterSet = make(map[string]struct{})
	out.allowedRegionSet = make(map[string]struct{})

	// Sort defensively by hierarchy rank while preserving relative order inside same scope.
	sorted := append([]ScopedLayer(nil), layers...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return ScopeHierarchyRank(sorted[i].ScopeType) < ScopeHierarchyRank(sorted[j].ScopeType)
	})

	hookIndex := make(map[string]int)

	for _, layer := range sorted {
		r := layer.Rules

		// 1. Denylists: union across all tiers.
		for _, dm := range r.DeniedModels {
			tok := strings.TrimSpace(dm)
			if tok != "" {
				out.deniedModelsSet[tok] = struct{}{}
			}
		}
		for _, da := range r.DeniedAdapters {
			tok := strings.TrimSpace(da)
			if tok != "" {
				out.deniedAdapterSet[tok] = struct{}{}
			}
		}

		// 2. Allowlists: intersection across all configured tiers.
		if r.HasAllowedModels {
			out.HasAllowedModels, out.wildcardModels, out.allowedModelsSet = intersectAllowlist(
				out.HasAllowedModels, out.wildcardModels, out.allowedModelsSet, r.AllowedModels, false,
			)
			if !strings.EqualFold(strings.TrimSpace(layer.ScopeType), snapshot.PolicyScopeVirtualKey) {
				out.hasAncestorAllowedModels, out.ancestorWildcardModels, out.ancestorAllowedModelsSet = intersectAllowlist(
					out.hasAncestorAllowedModels, out.ancestorWildcardModels, out.ancestorAllowedModelsSet, r.AllowedModels, false,
				)
			}
		}
		if r.HasAllowedAdapters {
			out.HasAllowedAdapters, out.wildcardAdapters, out.allowedAdapterSet = intersectAllowlist(
				out.HasAllowedAdapters, out.wildcardAdapters, out.allowedAdapterSet, r.AllowedAdapters, false,
			)
		}
		if r.HasAllowedDataRegions {
			out.HasAllowedDataRegions, out.wildcardRegions, out.allowedRegionSet = intersectAllowlist(
				out.HasAllowedDataRegions, out.wildcardRegions, out.allowedRegionSet, r.AllowedDataRegions, true,
			)
		}

		// 3. Monotonic Trust & Assurance maximum strictness.
		if TrustStrictnessRank(r.MinPluginTrust) > TrustStrictnessRank(out.MinPluginTrust) {
			out.MinPluginTrust = strings.ToUpper(strings.TrimSpace(r.MinPluginTrust))
		}
		if AssuranceStrictnessRank(r.MinCapabilityAssurance) > AssuranceStrictnessRank(out.MinCapabilityAssurance) {
			out.MinCapabilityAssurance = strings.ToUpper(strings.TrimSpace(r.MinCapabilityAssurance))
		}

		// 4. Numeric ceilings: minimum across all configured tiers.
		out.RPMLimit = minPtr(out.RPMLimit, r.RPMLimit)
		out.TPMLimit = minPtr(out.TPMLimit, r.TPMLimit)
		out.MaxConcurrentStreams = minPtr(out.MaxConcurrentStreams, r.MaxConcurrentStreams)
		out.MaxContextTokens = minPtr(out.MaxContextTokens, r.MaxContextTokens)
		out.MaxRequestBodyBytes = minPtr(out.MaxRequestBodyBytes, r.MaxRequestBodyBytes)

		// 5. Ordered GuardrailHooks concatenation: ancestor hooks cannot be stripped;
		// child mentioning the same extension can only tighten posture FAIL_OPEN -> FAIL_CLOSED.
		for _, h := range r.GuardrailHooks {
			extID := strings.TrimSpace(h.ExtensionID)
			if extID == "" {
				continue
			}
			posture := strings.ToUpper(strings.TrimSpace(h.FailurePosture))
			if posture == "" {
				posture = snapshot.PolicyFailureClosed
			}
			if idx, exists := hookIndex[extID]; exists {
				if out.GuardrailHooks[idx].FailurePosture == snapshot.PolicyFailureOpen && posture == snapshot.PolicyFailureClosed {
					out.GuardrailHooks[idx].FailurePosture = snapshot.PolicyFailureClosed
				}
				continue
			}
			hookIndex[extID] = len(out.GuardrailHooks)
			out.GuardrailHooks = append(out.GuardrailHooks, snapshot.GuardrailHookSpec{
				ExtensionID:    extID,
				FailurePosture: posture,
			})
		}
	}

	// Subtract denylists from explicit allowlists after full hierarchical intersection.
	for dm := range out.deniedModelsSet {
		delete(out.allowedModelsSet, dm)
		delete(out.ancestorAllowedModelsSet, dm)
	}
	for da := range out.deniedAdapterSet {
		delete(out.allowedAdapterSet, da)
	}

	out.DeniedModels = sortedKeys(out.deniedModelsSet)
	out.DeniedAdapters = sortedKeys(out.deniedAdapterSet)

	if out.HasAllowedModels {
		if out.wildcardModels {
			out.AllowedModels = []string{"*"}
		} else {
			out.AllowedModels = sortedKeys(out.allowedModelsSet)
		}
	}
	if out.HasAllowedAdapters {
		if out.wildcardAdapters {
			out.AllowedAdapters = []string{"*"}
		} else {
			out.AllowedAdapters = sortedKeys(out.allowedAdapterSet)
		}
	}
	if out.HasAllowedDataRegions {
		if out.wildcardRegions {
			out.AllowedDataRegions = []string{"*"}
		} else {
			out.AllowedDataRegions = sortedKeys(out.allowedRegionSet)
		}
	}
	return out
}

// MergeEffectiveFromSnapshot resolves and merges Org ∩ Project ∩ Env ∩ VirtualKey
// policies from ConfigSnapshot cleanly, failing closed if snapshot policy state is corrupted.
func MergeEffectiveFromSnapshot(
	snap *snapshot.ConfigSnapshot,
	cfg *snapshot.EffectiveConfig,
	vk *snapshot.VirtualKeyView,
) (EffectivePolicy, error) {
	if snap == nil {
		return EffectivePolicy{}, ErrPolicyEngineUnavailable
	}
	if err := snap.PoliciesHealthErr(); err != nil {
		return EffectivePolicy{}, fmt.Errorf("%w: %v", ErrPolicyEngineUnavailable, err)
	}
	var orgID, projID, envID, vkID string
	if vk != nil {
		orgID, projID, envID, vkID = vk.OrgID, vk.ProjectID, vk.EnvID, vk.ID
	} else if cfg != nil {
		orgID, projID, envID = cfg.OrgID, cfg.ProjectID, cfg.EnvID
	}

	var layers []ScopedLayer

	// Org baseline from organizations table.
	if cfg != nil && strings.TrimSpace(cfg.MinPluginTrust) != "" {
		layers = append(layers, ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			ScopeID:   cfg.OrgID,
			Rules: snapshot.PolicyRulesView{
				MinPluginTrust: cfg.MinPluginTrust,
			},
		})
	}
	if orgPolicies, err := snap.LookupPoliciesForScope(orgID, snapshot.PolicyScopeOrg, orgID); err != nil {
		return EffectivePolicy{}, fmt.Errorf("%w: %v", ErrPolicyEngineUnavailable, err)
	} else {
		for _, p := range orgPolicies {
			layers = append(layers, ScopedLayer{ScopeType: p.ScopeType, ScopeID: p.ScopeID, Rules: p.ParsedRules})
		}
	}

	// Project baseline from projects.enforced_region.
	if cfg != nil && cfg.EnforcedRegion != nil && strings.TrimSpace(*cfg.EnforcedRegion) != "" {
		reg := strings.ToUpper(strings.TrimSpace(*cfg.EnforcedRegion))
		layers = append(layers, ScopedLayer{
			ScopeType: snapshot.PolicyScopeProject,
			ScopeID:   cfg.ProjectID,
			Rules: snapshot.PolicyRulesView{
				HasAllowedDataRegions: true,
				AllowedDataRegions:    []string{reg},
			},
		})
	}
	if projID != "" {
		if projPolicies, err := snap.LookupPoliciesForScope(orgID, snapshot.PolicyScopeProject, projID); err != nil {
			return EffectivePolicy{}, fmt.Errorf("%w: %v", ErrPolicyEngineUnavailable, err)
		} else {
			for _, p := range projPolicies {
				layers = append(layers, ScopedLayer{ScopeType: p.ScopeType, ScopeID: p.ScopeID, Rules: p.ParsedRules})
			}
		}
	}

	// Environment baseline from environments.require_tested_caps.
	if cfg != nil && cfg.RequireTestedCaps {
		layers = append(layers, ScopedLayer{
			ScopeType: snapshot.PolicyScopeEnv,
			ScopeID:   cfg.EnvID,
			Rules: snapshot.PolicyRulesView{
				MinCapabilityAssurance: AssuranceTested,
			},
		})
	}
	if envID != "" {
		if envPolicies, err := snap.LookupPoliciesForScope(orgID, snapshot.PolicyScopeEnv, envID); err != nil {
			return EffectivePolicy{}, fmt.Errorf("%w: %v", ErrPolicyEngineUnavailable, err)
		} else {
			for _, p := range envPolicies {
				layers = append(layers, ScopedLayer{ScopeType: p.ScopeType, ScopeID: p.ScopeID, Rules: p.ParsedRules})
			}
		}
	}

	// VirtualKey baseline + VIRTUAL_KEY scoped policies.
	if vk != nil {
		var streamPtr *int64
		if vk.MaxConcurrentStreams > 0 {
			v := vk.MaxConcurrentStreams
			streamPtr = &v
		}
		vkRules := snapshot.PolicyRulesView{
			HasAllowedModels:     true,
			AllowedModels:        append([]string(nil), vk.AllowedModels...),
			RPMLimit:             vk.RPMLimit,
			TPMLimit:             vk.TPMLimit,
			MaxConcurrentStreams: streamPtr,
		}
		layers = append(layers, ScopedLayer{
			ScopeType: snapshot.PolicyScopeVirtualKey,
			ScopeID:   vk.ID,
			Rules:     vkRules,
		})
		if vkPolicies, err := snap.LookupPoliciesForScope(orgID, snapshot.PolicyScopeVirtualKey, vkID); err != nil {
			return EffectivePolicy{}, fmt.Errorf("%w: %v", ErrPolicyEngineUnavailable, err)
		} else {
			for _, p := range vkPolicies {
				layers = append(layers, ScopedLayer{ScopeType: p.ScopeType, ScopeID: p.ScopeID, Rules: p.ParsedRules})
			}
		}
	}

	return MergeLayers(layers...), nil
}

// CandidatePolicy represents a child policy submission evaluated against all strict ancestor
// layers during create/update writes.
type CandidatePolicy struct {
	ScopeType   string
	ScopeID     string
	PolicyType  string
	FailureMode string
	Rules       snapshot.PolicyRulesView
}

// ValidateChildPolicyWithinAncestors enforces GAP-11 / TP-FR-0701 write-time invariant:
// returns ErrPolicyRelaxationForbidden (HTTP 422 policy_relaxation_forbidden) if candidate
// attempts to widen allowlists, re-enable denylisted models/adapters, expand regions,
// raise numeric caps, lower trust/assurance thresholds, strip parent guardrail hooks,
// or set FAIL_OPEN on mandatory fail-closed controls or FAIL_CLOSED ancestor policies.
func ValidateChildPolicyWithinAncestors(ancestors []ScopedLayer, ancestorFailureClosed bool, candidate CandidatePolicy) error {
	scopeRank := ScopeHierarchyRank(candidate.ScopeType)
	if scopeRank == 0 {
		return fmt.Errorf("%w: unknown scope_type %q", ErrInvalidPolicyInput, candidate.ScopeType)
	}
	failMode := strings.ToUpper(strings.TrimSpace(candidate.FailureMode))
	if failMode == "" {
		failMode = snapshot.PolicyFailureClosed
	}
	if failMode != snapshot.PolicyFailureClosed && failMode != snapshot.PolicyFailureOpen {
		return fmt.Errorf("%w: invalid failure_mode %q", ErrInvalidPolicyInput, candidate.FailureMode)
	}

	// Mandatory fail-closed posture for security/residency/rate-limit/MCP governance policies (SPEC-CORE-01 / CT-33).
	switch candidate.PolicyType {
	case snapshot.PolicyTypeDataResidency, snapshot.PolicyTypeModelAllowlist, snapshot.PolicyTypeRateLimit, snapshot.PolicyTypeMCPGovernance:
		if failMode != snapshot.PolicyFailureClosed {
			return fmt.Errorf("%w: policy_type %s cannot be set to FAIL_OPEN", ErrPolicyRelaxationForbidden, candidate.PolicyType)
		}
	case snapshot.PolicyTypeGuardrail:
		if ancestorFailureClosed && failMode == snapshot.PolicyFailureOpen {
			return fmt.Errorf("%w: child GUARDRAIL policy cannot weaken ancestor FAIL_CLOSED posture to FAIL_OPEN", ErrPolicyRelaxationForbidden)
		}
	}

	// Filter ancestors strictly above candidate's scope rank, plus structural table baselines at same or higher rank.
	var strictAncestors []ScopedLayer
	for _, a := range ancestors {
		if r := ScopeHierarchyRank(a.ScopeType); r > 0 && (r < scopeRank || (a.Baseline && r <= scopeRank)) {
			strictAncestors = append(strictAncestors, a)
		}
	}
	if len(strictAncestors) == 0 {
		return nil
	}

	ancEff := MergeLayers(strictAncestors...)
	cr := candidate.Rules

	// 1. Model allowlist vs ancestor denylist & ancestor allowlist.
	if cr.HasAllowedModels {
		if isWildcardList(cr.AllowedModels) {
			if len(ancEff.deniedModelsSet) > 0 {
				return fmt.Errorf("%w: child allowed_models=['*'] cannot re-enable ancestor-denylisted models", ErrPolicyRelaxationForbidden)
			}
			if ancEff.HasAllowedModels && !ancEff.wildcardModels {
				return fmt.Errorf("%w: child allowed_models=['*'] widens restricted ancestor allowlist", ErrPolicyRelaxationForbidden)
			}
		} else {
			for _, raw := range cr.AllowedModels {
				tok := strings.TrimSpace(raw)
				if tok == "" {
					continue
				}
				if _, denied := ancEff.deniedModelsSet[tok]; denied {
					return fmt.Errorf("%w: child allowed_models attempts to re-enable denylisted model %q", ErrPolicyRelaxationForbidden, tok)
				}
				if ancEff.HasAllowedModels && !ancEff.wildcardModels {
					if _, ok := ancEff.allowedModelsSet[tok]; !ok {
						return fmt.Errorf("%w: child allowed_models includes %q outside ancestor allowlist", ErrPolicyRelaxationForbidden, tok)
					}
				}
			}
		}
	}

	// 2. Adapter allowlist vs ancestor denylist & ancestor allowlist.
	if cr.HasAllowedAdapters {
		if isWildcardList(cr.AllowedAdapters) {
			if len(ancEff.deniedAdapterSet) > 0 {
				return fmt.Errorf("%w: child allowed_adapters=['*'] cannot re-enable ancestor-denylisted adapters", ErrPolicyRelaxationForbidden)
			}
			if ancEff.HasAllowedAdapters && !ancEff.wildcardAdapters {
				return fmt.Errorf("%w: child allowed_adapters=['*'] widens restricted ancestor adapter allowlist", ErrPolicyRelaxationForbidden)
			}
		} else {
			for _, raw := range cr.AllowedAdapters {
				tok := strings.TrimSpace(raw)
				if tok == "" {
					continue
				}
				if _, denied := ancEff.deniedAdapterSet[tok]; denied {
					return fmt.Errorf("%w: child allowed_adapters attempts to re-enable denylisted adapter %q", ErrPolicyRelaxationForbidden, tok)
				}
				if ancEff.HasAllowedAdapters && !ancEff.wildcardAdapters {
					if _, ok := ancEff.allowedAdapterSet[tok]; !ok {
						return fmt.Errorf("%w: child allowed_adapters includes %q outside ancestor allowlist", ErrPolicyRelaxationForbidden, tok)
					}
				}
			}
		}
	}

	// 3. Data residency allowlist cannot introduce regions outside ancestor allowed_data_regions.
	if cr.HasAllowedDataRegions && ancEff.HasAllowedDataRegions && !ancEff.wildcardRegions {
		if isWildcardList(cr.AllowedDataRegions) {
			return fmt.Errorf("%w: child allowed_data_regions=['*'] widens restricted ancestor residency policy", ErrPolicyRelaxationForbidden)
		}
		for _, raw := range cr.AllowedDataRegions {
			reg := strings.ToUpper(strings.TrimSpace(raw))
			if reg == "" {
				continue
			}
			if _, ok := ancEff.allowedRegionSet[reg]; !ok {
				return fmt.Errorf("%w: child data region %q violates ancestor residency restriction", ErrPolicyRelaxationForbidden, raw)
			}
		}
	}

	// 4. MinPluginTrust and MinCapabilityAssurance cannot weaken ancestor minimums.
	if cr.MinPluginTrust != "" && TrustStrictnessRank(ancEff.MinPluginTrust) > 0 {
		if TrustStrictnessRank(cr.MinPluginTrust) < TrustStrictnessRank(ancEff.MinPluginTrust) {
			return fmt.Errorf("%w: child min_plugin_trust %q weakens ancestor requirement %q", ErrPolicyRelaxationForbidden, cr.MinPluginTrust, ancEff.MinPluginTrust)
		}
	}
	if cr.MinCapabilityAssurance != "" && AssuranceStrictnessRank(ancEff.MinCapabilityAssurance) > 0 {
		if AssuranceStrictnessRank(cr.MinCapabilityAssurance) < AssuranceStrictnessRank(ancEff.MinCapabilityAssurance) {
			return fmt.Errorf("%w: child min_capability_assurance %q weakens ancestor requirement %q", ErrPolicyRelaxationForbidden, cr.MinCapabilityAssurance, ancEff.MinCapabilityAssurance)
		}
	}

	// 5. Numeric ceilings cannot exceed ancestor limits.
	if err := checkChildCapWithinAncestor("rpm_limit", ancEff.RPMLimit, cr.RPMLimit); err != nil {
		return err
	}
	if err := checkChildCapWithinAncestor("tpm_limit", ancEff.TPMLimit, cr.TPMLimit); err != nil {
		return err
	}
	if err := checkChildCapWithinAncestor("max_concurrent_streams", ancEff.MaxConcurrentStreams, cr.MaxConcurrentStreams); err != nil {
		return err
	}
	if err := checkChildCapWithinAncestor("max_context_tokens", ancEff.MaxContextTokens, cr.MaxContextTokens); err != nil {
		return err
	}
	if err := checkChildCapWithinAncestor("max_request_body_bytes", ancEff.MaxRequestBodyBytes, cr.MaxRequestBodyBytes); err != nil {
		return err
	}

	// 6. Guardrail hook posture cannot weaken a FAIL_CLOSED ancestor hook to FAIL_OPEN.
	if len(cr.GuardrailHooks) > 0 && len(ancEff.GuardrailHooks) > 0 {
		ancPosture := make(map[string]string, len(ancEff.GuardrailHooks))
		for _, h := range ancEff.GuardrailHooks {
			ancPosture[h.ExtensionID] = h.FailurePosture
		}
		for _, ch := range cr.GuardrailHooks {
			if ap, exists := ancPosture[strings.TrimSpace(ch.ExtensionID)]; exists {
				if ap == snapshot.PolicyFailureClosed && strings.EqualFold(ch.FailurePosture, snapshot.PolicyFailureOpen) {
					return fmt.Errorf("%w: child guardrail hook %q cannot weaken ancestor FAIL_CLOSED posture to FAIL_OPEN", ErrPolicyRelaxationForbidden, ch.ExtensionID)
				}
			}
		}
	}

	return nil
}

func checkChildCapWithinAncestor(name string, anc, child *int64) error {
	if anc == nil || child == nil {
		return nil
	}
	if *child > *anc {
		return fmt.Errorf("%w: child %s (%d) exceeds ancestor ceiling (%d)", ErrPolicyRelaxationForbidden, name, *child, *anc)
	}
	return nil
}

func intersectAllowlist(
	hasExisting bool,
	existingWildcard bool,
	existing map[string]struct{},
	incoming []string,
	uppercase bool,
) (bool, bool, map[string]struct{}) {
	inWildcard := isWildcardList(incoming)
	inSet := make(map[string]struct{}, len(incoming))
	if !inWildcard {
		for _, raw := range incoming {
			tok := strings.TrimSpace(raw)
			if tok == "" {
				continue
			}
			if uppercase {
				tok = strings.ToUpper(tok)
			}
			inSet[tok] = struct{}{}
		}
	}

	if !hasExisting {
		return true, inWildcard, inSet
	}
	if existingWildcard && inWildcard {
		return true, true, make(map[string]struct{})
	}
	if existingWildcard {
		return true, false, inSet
	}
	if inWildcard {
		return true, false, existing
	}

	inter := make(map[string]struct{})
	for k := range existing {
		if _, ok := inSet[k]; ok {
			inter[k] = struct{}{}
		}
	}
	return true, false, inter
}

func isWildcardList(vals []string) bool {
	for _, v := range vals {
		if strings.TrimSpace(v) == "*" {
			return true
		}
	}
	return false
}

func minPtr(a, b *int64) *int64 {
	if a == nil {
		return clonePtr(b)
	}
	if b == nil {
		return clonePtr(a)
	}
	if *a <= *b {
		v := *a
		return &v
	}
	v := *b
	return &v
}

func clonePtr(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
