// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package engine

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
)

// Canonical policy denial codes emitted by the runtime evaluator (TP-IMP-0019 / SPEC-CORE-01).
const (
	DenialCodeEngineUnavailable = "policy_engine_unavailable"
	DenialCodeModelDenied       = "policy_denied_model"
	DenialCodeRegionDenied      = "policy_denied_region"
	DenialCodeMaxContextDenied  = "policy_denied_max_context"
	DenialCodeTrustDenied       = "policy_denied_trust"
	DenialCodePayloadTooLarge   = "payload_too_large"
)

// EvalRequest captures all request attributes needed for < 0.2ms deterministic policy evaluation.
type EvalRequest struct {
	OrgID                     string
	ProjectID                 string
	EnvID                     string
	ModelSlug                 string
	AdapterSlug               string
	DataRegion                string
	RequestedDataRegion       string
	ExtensionTrustTier        string
	ExtensionEnterpriseSigned bool
	CapabilityAssurance       string
	RequestBodyBytes          int64
	EstimatedTotalTokens      int64
	TargetEvaluation          bool
}

// EvalDecision reports the deterministic Fail-Closed verdict for a request.
type EvalDecision struct {
	Allowed         bool
	HTTPStatus      int
	DenialCode      string
	ErrorType       string
	DenialReason    string
	Err             error
	EffectivePolicy EffectivePolicy
}

// Evaluator evaluates Fail-Closed hierarchical policies directly atop an immutable ConfigSnapshot.
type Evaluator struct{}

// NewEvaluator constructs a stateless Evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

// ResolveEffective computes EffectivePolicy for (cfg, vk) directly atop ConfigSnapshot.
func (e *Evaluator) ResolveEffective(
	snap *snapshot.ConfigSnapshot,
	cfg *snapshot.EffectiveConfig,
	vk *snapshot.VirtualKeyView,
) (EffectivePolicy, error) {
	return MergeEffectiveFromSnapshot(snap, cfg, vk)
}

// Evaluate merges hierarchy rules and enforces Fail-Closed checks across model,
// adapter, data residency, body size, max context tokens, plugin trust, and assurance.
func (e *Evaluator) Evaluate(
	snap *snapshot.ConfigSnapshot,
	cfg *snapshot.EffectiveConfig,
	vk *snapshot.VirtualKeyView,
	req EvalRequest,
) EvalDecision {
	eff, err := MergeEffectiveFromSnapshot(snap, cfg, vk)
	if err != nil {
		return EvalDecision{
			Allowed:      false,
			HTTPStatus:   http.StatusServiceUnavailable,
			DenialCode:   DenialCodeEngineUnavailable,
			ErrorType:    "api_error",
			DenialReason: "Policy engine snapshot is corrupted or unavailable; failing closed.",
			Err:          err,
		}
	}
	return EvaluateEffective(eff, req)
}

func regionDeniedDecision(eff EffectivePolicy, reason, errDetail string) EvalDecision {
	return EvalDecision{
		Allowed:         false,
		HTTPStatus:      http.StatusForbidden,
		DenialCode:      DenialCodeRegionDenied,
		ErrorType:       "residency_unavailable_error",
		DenialReason:    reason,
		Err:             fmt.Errorf("%w: %s", ErrPolicyDeniedRegion, errDetail),
		EffectivePolicy: eff,
	}
}

// EvaluateEffective runs pure in-memory policy checks against an already-merged EffectivePolicy.
func EvaluateEffective(eff EffectivePolicy, req EvalRequest) EvalDecision {
	// 1. Model allowlist & denylist check.
	model := strings.TrimSpace(req.ModelSlug)
	if model != "" {
		if !ModelPermitted(eff, model) {
			return EvalDecision{
				Allowed:         false,
				HTTPStatus:      http.StatusForbidden,
				DenialCode:      DenialCodeModelDenied,
				ErrorType:       "permission_error",
				DenialReason:    fmt.Sprintf("Model %q is denied by hierarchical organization/project/environment/key policy.", model),
				Err:             fmt.Errorf("%w: model %q denied", ErrPolicyDeniedModel, model),
				EffectivePolicy: eff,
			}
		}
	}

	// 2. Adapter allowlist & denylist check.
	adp := strings.TrimSpace(req.AdapterSlug)
	if adp != "" && !AdapterPermitted(eff, adp) {
		return EvalDecision{
			Allowed:         false,
			HTTPStatus:      http.StatusForbidden,
			DenialCode:      DenialCodeModelDenied,
			ErrorType:       "permission_error",
			DenialReason:    fmt.Sprintf("Provider adapter %q is denied by hierarchical policy.", adp),
			Err:             fmt.Errorf("%w: adapter %q denied", ErrPolicyDeniedModel, adp),
			EffectivePolicy: eff,
		}
	}

	// 3. Data residency intersection (Org ∩ Project ∩ Env ∩ RequestHeader) + fail-closed target residency check.
	isTargetCheck := req.TargetEvaluation || adp != ""
	reqReg := strings.ToUpper(strings.TrimSpace(req.RequestedDataRegion))
	depReg := strings.ToUpper(strings.TrimSpace(req.DataRegion))
	if eff.HasAllowedDataRegions {
		if !eff.wildcardRegions && len(eff.AllowedDataRegions) == 0 {
			return regionDeniedDecision(eff, "Effective hierarchical allowed_data_regions intersection is empty.", "empty allowed_data_regions intersection")
		}
		if reqReg != "" && !RegionPermitted(eff, reqReg) {
			return regionDeniedDecision(eff, fmt.Sprintf("Requested data region %q is outside effective allowed_data_regions %v.", reqReg, eff.AllowedDataRegions), fmt.Sprintf("requested region %q denied", reqReg))
		}
		if (isTargetCheck || depReg != "") && !RegionPermitted(eff, depReg) {
			return regionDeniedDecision(eff, fmt.Sprintf("Deployment data region %q is outside effective allowed_data_regions %v.", depReg, eff.AllowedDataRegions), fmt.Sprintf("deployment region %q denied", depReg))
		}
	}
	if reqReg != "" && (isTargetCheck || depReg != "") {
		if depReg == "" || reqReg != depReg {
			return regionDeniedDecision(eff, fmt.Sprintf("Deployment data region %q does not match requested data region %q.", depReg, reqReg), fmt.Sprintf("deployment region %q != requested %q", depReg, reqReg))
		}
	}

	// 4. Request body payload cap.
	if eff.MaxRequestBodyBytes != nil && req.RequestBodyBytes > *eff.MaxRequestBodyBytes {
		return EvalDecision{
			Allowed:         false,
			HTTPStatus:      http.StatusRequestEntityTooLarge,
			DenialCode:      DenialCodePayloadTooLarge,
			ErrorType:       "invalid_request_error",
			DenialReason:    fmt.Sprintf("Request payload (%d bytes) exceeds policy limit (%d bytes).", req.RequestBodyBytes, *eff.MaxRequestBodyBytes),
			Err:             fmt.Errorf("%w: %d > %d", ErrPolicyPayloadTooLarge, req.RequestBodyBytes, *eff.MaxRequestBodyBytes),
			EffectivePolicy: eff,
		}
	}

	// 5. Max context tokens cap.
	if eff.MaxContextTokens != nil && req.EstimatedTotalTokens > *eff.MaxContextTokens {
		return EvalDecision{
			Allowed:         false,
			HTTPStatus:      http.StatusBadRequest,
			DenialCode:      DenialCodeMaxContextDenied,
			ErrorType:       "invalid_request_error",
			DenialReason:    fmt.Sprintf("Estimated request tokens (%d) exceed policy max_context_tokens (%d).", req.EstimatedTotalTokens, *eff.MaxContextTokens),
			Err:             fmt.Errorf("%w: %d > %d", ErrPolicyDeniedMaxContext, req.EstimatedTotalTokens, *eff.MaxContextTokens),
			EffectivePolicy: eff,
		}
	}

	// 6. Minimum plugin trust & capability assurance checks.
	if eff.MinPluginTrust != "" && (strings.TrimSpace(req.ExtensionTrustTier) != "" || req.ExtensionEnterpriseSigned || req.TargetEvaluation) {
		if !trustTierSatisfies(req.ExtensionTrustTier, req.ExtensionEnterpriseSigned, eff.MinPluginTrust) {
			return EvalDecision{
				Allowed:         false,
				HTTPStatus:      http.StatusForbidden,
				DenialCode:      DenialCodeTrustDenied,
				ErrorType:       "permission_error",
				DenialReason:    fmt.Sprintf("Extension trust tier %q (enterprise_signed=%v) violates policy minimum %q.", req.ExtensionTrustTier, req.ExtensionEnterpriseSigned, eff.MinPluginTrust),
				Err:             fmt.Errorf("%w: trust %q (enterprise_signed=%v) < %q", ErrPolicyDeniedTrust, req.ExtensionTrustTier, req.ExtensionEnterpriseSigned, eff.MinPluginTrust),
				EffectivePolicy: eff,
			}
		}
	}
	if eff.MinCapabilityAssurance != "" && (strings.TrimSpace(req.CapabilityAssurance) != "" || req.TargetEvaluation) {
		if AssuranceStrictnessRank(req.CapabilityAssurance) < AssuranceStrictnessRank(eff.MinCapabilityAssurance) {
			return EvalDecision{
				Allowed:         false,
				HTTPStatus:      http.StatusBadRequest,
				DenialCode:      DenialCodeTrustDenied,
				ErrorType:       "invalid_request_error",
				DenialReason:    fmt.Sprintf("Deployment assurance %q violates policy minimum %q.", req.CapabilityAssurance, eff.MinCapabilityAssurance),
				Err:             fmt.Errorf("%w: assurance %q < %q", ErrPolicyDeniedTrust, req.CapabilityAssurance, eff.MinCapabilityAssurance),
				EffectivePolicy: eff,
			}
		}
	}

	return EvalDecision{
		Allowed:         true,
		HTTPStatus:      http.StatusOK,
		EffectivePolicy: eff,
	}
}

// ModelPermitted reports whether modelSlug is allowed by eff.
func ModelPermitted(eff EffectivePolicy, modelSlug string) bool {
	tok := strings.TrimSpace(modelSlug)
	if tok == "" {
		return false
	}
	if _, denied := eff.deniedModelsSet[tok]; denied {
		return false
	}
	if !eff.HasAllowedModels || eff.wildcardModels {
		return true
	}
	_, ok := eff.allowedModelsSet[tok]
	return ok
}

// ModelExplicitlyDenied reports whether modelSlug appears on the hierarchical denied_models list.
func ModelExplicitlyDenied(eff EffectivePolicy, modelSlug string) bool {
	tok := strings.TrimSpace(modelSlug)
	if tok == "" {
		return false
	}
	_, denied := eff.deniedModelsSet[tok]
	return denied
}

// AncestorModelPermitted reports whether modelSlug satisfies ancestor-scope
// (ORG ∩ PROJECT ∩ ENV, excluding VIRTUAL_KEY) model allowlists and overall denylists.
func AncestorModelPermitted(eff EffectivePolicy, modelSlug string) bool {
	tok := strings.TrimSpace(modelSlug)
	if tok == "" {
		return false
	}
	if _, denied := eff.deniedModelsSet[tok]; denied {
		return false
	}
	if !eff.hasAncestorAllowedModels || eff.ancestorWildcardModels {
		return true
	}
	_, ok := eff.ancestorAllowedModelsSet[tok]
	return ok
}

// AdapterPermitted reports whether adapterSlug is allowed by eff.
func AdapterPermitted(eff EffectivePolicy, adapterSlug string) bool {
	tok := strings.TrimSpace(adapterSlug)
	if tok == "" {
		return false
	}
	if _, denied := eff.deniedAdapterSet[tok]; denied {
		return false
	}
	if !eff.HasAllowedAdapters || eff.wildcardAdapters {
		return true
	}
	_, ok := eff.allowedAdapterSet[tok]
	return ok
}

// RegionPermitted reports whether region is permitted by eff.
func RegionPermitted(eff EffectivePolicy, region string) bool {
	reg := strings.ToUpper(strings.TrimSpace(region))
	if reg == "" {
		return !eff.HasAllowedDataRegions || eff.wildcardRegions
	}
	if !eff.HasAllowedDataRegions || eff.wildcardRegions {
		return true
	}
	_, ok := eff.allowedRegionSet[reg]
	return ok
}

func trustTierSatisfies(provenanceTier string, enterpriseSigned bool, minPolicy string) bool {
	p := strings.ToLower(strings.TrimSpace(provenanceTier))
	switch strings.ToUpper(strings.TrimSpace(minPolicy)) {
	case MinTrustOfficialOnly:
		return p == "official"
	case MinTrustVerifiedOnly:
		return p == "official" || p == "verified"
	case MinTrustVerifiedOrEnterpriseSigned:
		return p == "official" || p == "verified" || p == "enterprise_signed" || (enterpriseSigned && p != "")
	case MinTrustAnyApproved:
		return p == "official" || p == "verified" || p == "enterprise_signed" || p == "unverified_external" || (enterpriseSigned && p != "")
	default:
		return false
	}
}
