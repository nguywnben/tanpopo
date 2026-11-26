// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package resolver implements Tanpopo's deterministic 6-step capability-aware
// route resolver, hard Data Residency fence, strategy ranker, and zero-side-effect
// dry-run Explain simulator (SPEC-CORE-01 / TP-IMP-0020).
package resolver

import (
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
)

// RejectionError captures deterministic Fail-Closed HTTP rejection metadata
// when no compliant, capable, healthy deployment survives resolution.
type RejectionError struct {
	HTTPStatus int
	Code       string
	ErrorType  string
	Message    string
	Retryable  bool
}

func (e *RejectionError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s (%d): %s", e.Code, e.HTTPStatus, e.Message)
}

// ResolveRequest contains all pre-resolved inputs needed for deterministic
// < 0.2ms in-memory 6-step routing without upstream or SQLite I/O.
type ResolveRequest struct {
	RequestID            string
	Snapshot             *snapshot.ConfigSnapshot
	OrgID                string
	ProjectID            string
	EnvID                string
	TargetModel          string
	EffectivePolicy      policyengine.EffectivePolicy
	RequestedDataRegion  string
	Dialect              string
	RequiredCapabilities []string
	EstimatedInputTokens int64
	MaxOutputTokens      int64
	RequireTestedCaps    bool
	Breaker              *breaker.CircuitBreaker
	WeightedSeed         *int64
	DryRun               bool

	// Optional native-passthrough path allowlist check applied at Step 2.
	NativePathAllowed func(adp *snapshot.ProviderAdapterView) bool
}

// RankedTarget is a compliant, capable, healthy candidate selected at Step 5/6.
type RankedTarget struct {
	Rank                int
	Target              *snapshot.ResolvedTarget
	PriorityOrder       int
	Weight              int
	EstimatedCostNano   int64
	EstimatedCostMicros int64
	EWMALatency         time.Duration
	HasEWMA             bool
	Reason              string
}

// EffectivePolicySummary captures sanitized policy metadata for RouteExplainResponse.
type EffectivePolicySummary struct {
	AllowedDataRegions     []string `json:"allowed_data_regions"`
	MinPluginTrust         string   `json:"min_plugin_trust,omitempty"`
	MinCapabilityAssurance string   `json:"min_capability_assurance,omitempty"`
	MaxContextTokens       *int64   `json:"max_context_tokens,omitempty"`
}

// ExplainSelectedCandidate matches SPEC-CORE-01 Section 4 / OpenAPI RouteExplainResponse items.
type ExplainSelectedCandidate struct {
	Rank                int     `json:"rank"`
	DeploymentID        string  `json:"deployment_id"`
	DataRegion          string  `json:"data_region"`
	EstimatedCostMicros int64   `json:"estimated_cost_micros"`
	EstimatedCostNano   int64   `json:"estimated_cost_nano_usd,omitempty"`
	PriorityOrder       int     `json:"priority_order"`
	Weight              int     `json:"weight"`
	EWMALatencyMs       float64 `json:"ewma_latency_ms,omitempty"`
	Reason              string  `json:"reason"`
}

// ExplainRejectedDeployment matches SPEC-CORE-01 Section 4 rejected deployment diagnostics.
type ExplainRejectedDeployment struct {
	DeploymentID string `json:"deployment_id"`
	Stage        string `json:"stage"`
	RuleCode     string `json:"rule_code"`
	Detail       string `json:"detail"`
}

// ExplainResult carries the full RouteExplainResponse-compatible decision tree alongside
// the ordered runtime RankedTarget candidates produced by the exact same 6-step path.
type ExplainResult struct {
	RequestID                 string                      `json:"request_id"`
	DryRun                    bool                        `json:"dry_run"`
	RouteSlug                 string                      `json:"route_slug"`
	Strategy                  string                      `json:"strategy,omitempty"`
	MaxFailoverHops           int                         `json:"max_failover_hops"`
	EffectivePolicy           EffectivePolicySummary      `json:"effective_policy"`
	RequiredCapabilities      []string                    `json:"required_capabilities"`
	SelectedCandidatesOrdered []ExplainSelectedCandidate  `json:"selected_candidates_ordered"`
	RejectedDeployments       []ExplainRejectedDeployment `json:"rejected_deployments"`

	SelectedTargets []*RankedTarget `json:"-"`
	Rejection       *RejectionError `json:"-"`
}

// Resolver executes Tanpopo's 6-step routing pipeline across immutable ConfigSnapshots.
type Resolver struct {
	swrrMu     sync.Mutex
	swrrStates map[string]map[string]int64
	randCounter atomic.Uint64
}

// New constructs a Resolver.
func New() *Resolver {
	return &Resolver{
		swrrStates: make(map[string]map[string]int64),
	}
}

type rawCandidate struct {
	target        *snapshot.ResolvedTarget
	deploymentID  string
	priorityOrder int
	weight        int
	estCostNano   int64
	ewmaLatency   time.Duration
	hasEWMA       bool
	degradedConn  bool
}

// Resolve executes the 6-step routing pipeline for live inference dispatch.
func (r *Resolver) Resolve(req ResolveRequest) ([]*RankedTarget, *RejectionError) {
	req.DryRun = false
	res := r.evaluate(req)
	if res.Rejection != nil {
		return nil, res.Rejection
	}
	return res.SelectedTargets, nil
}

// Explain executes the exact same 6-step pipeline strictly read-only without mutating
// circuit breakers, SWRR counters, rate limits, budgets, or network sockets.
func (r *Resolver) Explain(req ResolveRequest) ExplainResult {
	req.DryRun = true
	return r.evaluate(req)
}

func (r *Resolver) evaluate(req ResolveRequest) ExplainResult {
	reqID := strings.TrimSpace(req.RequestID)
	out := ExplainResult{
		RequestID:                 reqID,
		DryRun:                    true,
		RouteSlug:                 strings.TrimSpace(req.TargetModel),
		RequiredCapabilities:      normalizeCapabilities(req.RequiredCapabilities),
		SelectedCandidatesOrdered: []ExplainSelectedCandidate{},
		RejectedDeployments:       []ExplainRejectedDeployment{},
		EffectivePolicy:           summarizeEffectivePolicy(req.EffectivePolicy, req.RequestedDataRegion, ""),
	}

	if req.Snapshot == nil {
		out.Rejection = &RejectionError{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       "snapshot_not_ready",
			ErrorType:  "api_error",
			Message:    "Configuration snapshot unavailable.",
			Retryable:  true,
		}
		return out
	}
	if err := req.Snapshot.RoutesHealthy(); err != nil {
		out.Rejection = &RejectionError{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       policyengine.DenialCodeEngineUnavailable,
			ErrorType:  "api_error",
			Message:    "Route catalog snapshot is corrupted or unavailable; failing closed.",
			Retryable:  true,
		}
		return out
	}

	dialect := strings.ToLower(strings.TrimSpace(req.Dialect))
	reqRegion := strings.ToUpper(strings.TrimSpace(req.RequestedDataRegion))
	estIn := max(int64(0), req.EstimatedInputTokens)
	estOut := max(int64(0), req.MaxOutputTokens)
	estTotal := safeAddInt64(estIn, estOut)

	// Step 1: Discover RoutePolicy (if present) or fall back to 1:1 LogicalModel targets.
	strategy := snapshot.RouteStrategyPriorityFailover
	maxHops := snapshot.DefaultMaxFailoverHops
	routeReqRegion := ""
	routeKey := req.OrgID + "|" + req.EnvID + "|" + out.RouteSlug

	var rawPool []rawCandidate
	rp, hasRoute, err := req.Snapshot.LookupRoutePolicy(req.OrgID, req.EnvID, out.RouteSlug)
	if err != nil {
		out.Rejection = &RejectionError{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       policyengine.DenialCodeEngineUnavailable,
			ErrorType:  "api_error",
			Message:    "Route lookup failed; failing closed.",
			Retryable:  true,
		}
		return out
	}
	useRoute := false
	if hasRoute && rp != nil {
		if rp.ProjectID != "" && rp.ProjectID != req.ProjectID {
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				Stage:    "HARD_COMPLIANCE_FILTER",
				RuleCode: "ROUTE_PROJECT_MISMATCH",
				Detail:   fmt.Sprintf("Route %q belongs to project %q, not caller project %q.", out.RouteSlug, rp.ProjectID, req.ProjectID),
			})
			rawPool = nil
		} else {
			useRoute = true
			strategy = rp.Strategy
			maxHops = rp.MaxFailoverHops
			if rp.RequiredRegion != nil {
				routeReqRegion = strings.ToUpper(strings.TrimSpace(*rp.RequiredRegion))
			}
			out.EffectivePolicy = summarizeEffectivePolicy(req.EffectivePolicy, reqRegion, routeReqRegion)
			rawPool = make([]rawCandidate, 0, len(rp.Targets))
			for _, tgt := range rp.Targets {
				cand, ok := req.Snapshot.LookupResolvedTarget(req.OrgID, tgt.DeploymentID)
				if !ok || cand == nil || cand.Deployment == nil {
					out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
						DeploymentID: tgt.DeploymentID,
						Stage:        "HARD_COMPLIANCE_FILTER",
						RuleCode:     "DEPLOYMENT_NOT_FOUND_OR_CROSS_ORG",
						Detail:       fmt.Sprintf("Deployment %q does not exist inside caller organization.", tgt.DeploymentID),
					})
					continue
				}
				w := tgt.Weight
				rawPool = append(rawPool, rawCandidate{
					target:        cand,
					deploymentID:  cand.Deployment.ID,
					priorityOrder: tgt.PriorityOrder,
					weight:        w,
				})
			}
		}
	} else {
		cands := req.Snapshot.ListCandidateTargets(req.OrgID, out.RouteSlug)
		rawPool = make([]rawCandidate, 0, len(cands))
		for i, cand := range cands {
			if cand == nil || cand.Deployment == nil {
				continue
			}
			rawPool = append(rawPool, rawCandidate{
				target:        cand,
				deploymentID:  cand.Deployment.ID,
				priorityOrder: i + 1,
				weight:        100,
			})
		}
	}

	out.Strategy = strategy
	out.MaxFailoverHops = maxHops

	var (
		sawQuarantined     bool
		sawDialectMismatch bool
		sawPathDenied      bool
		sawModelDenied     bool
		modelDenial        policyengine.EvalDecision
		sawTrustDenied     bool
		trustDenial        policyengine.EvalDecision
		sawRegionDenied    bool
		regionDenialDetail string
		step3Entered       int
		step3MissingCaps   []string
		step3MissingDetail string
		step3ContextDenied bool
		step4Entered       int
	)

	var routeShadowsLM bool
	if useRoute {
		_, routeShadowsLM = req.Snapshot.LookupLogicalModelBySlug(req.OrgID, out.RouteSlug)
	}

	// Steps 2, 3, and 4: Hard Compliance -> Hard Capabilities -> SM-04 Health & Circuit Breaker.
	survivors := make([]rawCandidate, 0, len(rawPool))
	for _, item := range rawPool {
		cand := item.target
		dep := cand.Deployment
		conn := cand.Connection
		adp := cand.Adapter

		// Tenant, logical model & project ownership invariant.
		if dep.OrgID != req.OrgID || cand.LogicalModel == nil || conn == nil || !conn.VisibleToProjectScopeOnly(req.OrgID, req.ProjectID) {
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_COMPLIANCE_FILTER",
				RuleCode:     "SCOPE_OR_CONNECTION_UNAVAILABLE",
				Detail:       "Deployment logical model or connection is missing or not visible to caller project.",
			})
			continue
		}

		// Step 2a: SM-02 Connection health (ACTIVE or DEGRADED permitted per SPEC-CORE-01).
		if conn.State != snapshot.ConnectionStateActive && conn.State != snapshot.ConnectionStateDegraded {
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_COMPLIANCE_FILTER",
				RuleCode:     "CONNECTION_INACTIVE",
				Detail:       fmt.Sprintf("Connection %q state is %s.", conn.ID, conn.State),
			})
			continue
		}
		item.degradedConn = conn.State == snapshot.ConnectionStateDegraded

		// Step 2b: Adapter integrity & SM-01 quarantine guard.
		if adp == nil {
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_COMPLIANCE_FILTER",
				RuleCode:     "ADAPTER_MISSING",
				Detail:       "Backing provider adapter metadata is missing.",
			})
			continue
		}
		if conn.AdapterQuarantined || strings.EqualFold(adp.ExtensionState, installer.StateQuarantined) {
			sawQuarantined = true
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_COMPLIANCE_FILTER",
				RuleCode:     "EXTENSION_QUARANTINED",
				Detail:       fmt.Sprintf("Adapter %q is quarantined.", adp.AdapterSlug),
			})
			continue
		}
		if !conn.VisibleToProject(req.OrgID, req.ProjectID) {
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_COMPLIANCE_FILTER",
				RuleCode:     "SCOPE_OR_CONNECTION_UNAVAILABLE",
				Detail:       "Backing extension is disabled or blocked by organization trust policy.",
			})
			continue
		}

		// Step 2c: Route-level required_region hard intersection (fail-closed CT-08).
		depRegion := strings.ToUpper(strings.TrimSpace(dep.DataRegion))
		if routeReqRegion != "" && depRegion != routeReqRegion {
			sawRegionDenied = true
			if regionDenialDetail == "" {
				regionDenialDetail = fmt.Sprintf("Deployment region %q violates route required_region %q.", dep.DataRegion, routeReqRegion)
			}
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_COMPLIANCE_FILTER",
				RuleCode:     "DATA_RESIDENCY_MISMATCH",
				Detail:       fmt.Sprintf("Deployment region %q does not match route required_region %q.", dep.DataRegion, routeReqRegion),
			})
			continue
		}
		// Ensure request header and route required_region don't yield empty intersection.
		if routeReqRegion != "" && reqRegion != "" && routeReqRegion != reqRegion {
			sawRegionDenied = true
			if regionDenialDetail == "" {
				regionDenialDetail = fmt.Sprintf("Requested region %q conflicts with route required_region %q.", reqRegion, routeReqRegion)
			}
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_COMPLIANCE_FILTER",
				RuleCode:     "DATA_RESIDENCY_MISMATCH",
				Detail:       fmt.Sprintf("Requested region %q conflicts with route required_region %q.", reqRegion, routeReqRegion),
			})
			continue
		}

		// Step 2d: Hierarchical Policy Evaluation (Org ∩ Project ∩ Env ∩ VirtualKey ∩ Request).
		evalModel := dep.CanonicalModelSlug
		if useRoute {
			switch {
			case !policyengine.ModelPermitted(req.EffectivePolicy, out.RouteSlug):
				evalModel = out.RouteSlug
			case dep.CanonicalModelSlug != "" && !policyengine.AncestorModelPermitted(req.EffectivePolicy, dep.CanonicalModelSlug):
				// Route aliases cannot bypass ORG/PROJECT/ENV MODEL_ALLOWLIST restrictions.
				evalModel = dep.CanonicalModelSlug
			case dep.CanonicalModelSlug != "" && policyengine.ModelExplicitlyDenied(req.EffectivePolicy, dep.CanonicalModelSlug):
				evalModel = dep.CanonicalModelSlug
			case routeShadowsLM && dep.CanonicalModelSlug != "" && dep.CanonicalModelSlug != out.RouteSlug:
				// Prevent a route_slug that collides with a LogicalModel slug from silently redirecting to a denied model.
				evalModel = dep.CanonicalModelSlug
			case dep.CanonicalModelSlug == "" || !policyengine.ModelPermitted(req.EffectivePolicy, dep.CanonicalModelSlug):
				// Legitimate non-shadowing route alias permitted by ancestor + VK policies.
				evalModel = out.RouteSlug
			}
		} else if evalModel == "" {
			evalModel = out.RouteSlug
		}
		dec := policyengine.EvaluateEffective(req.EffectivePolicy, policyengine.EvalRequest{
			OrgID:                     req.OrgID,
			ProjectID:                 req.ProjectID,
			EnvID:                     req.EnvID,
			ModelSlug:                 evalModel,
			AdapterSlug:               adp.AdapterSlug,
			DataRegion:                dep.DataRegion,
			RequestedDataRegion:       reqRegion,
			ExtensionTrustTier:        conn.DerivedPublicTrust,
			ExtensionEnterpriseSigned: conn.DerivedEnterpriseSigned,
			CapabilityAssurance:       dep.AssuranceLevel,
			EstimatedTotalTokens:      estTotal,
			TargetEvaluation:          true,
		})
		if !dec.Allowed {
			ruleCode := strings.ToUpper(dec.DenialCode)
			switch dec.DenialCode {
			case policyengine.DenialCodeModelDenied:
				ruleCode = "MODEL_DENIED_BY_POLICY"
				if !sawModelDenied {
					sawModelDenied = true
					modelDenial = dec
				}
			case policyengine.DenialCodeTrustDenied:
				ruleCode = "TRUST_OR_ASSURANCE_DENIED"
				if !sawTrustDenied {
					sawTrustDenied = true
					trustDenial = dec
				}
			case policyengine.DenialCodeRegionDenied:
				ruleCode = "DATA_RESIDENCY_MISMATCH"
				sawRegionDenied = true
				if regionDenialDetail == "" {
					regionDenialDetail = dec.DenialReason + " (" + policyengine.DenialCodeRegionDenied + ")"
				}
			}
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_COMPLIANCE_FILTER",
				RuleCode:     ruleCode,
				Detail:       dec.DenialReason,
			})
			continue
		}

		// Step 2e: Dialect invariant & Default-Deny native path allowlist for /native/{dialect}/... calls.
		if dialect != "" {
			if !targetMatchesNativeDialect(cand, dialect) {
				sawDialectMismatch = true
				out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
					DeploymentID: dep.ID,
					Stage:        "HARD_COMPLIANCE_FILTER",
					RuleCode:     "DIALECT_MISMATCH",
					Detail:       fmt.Sprintf("Deployment uses native dialect %q instead of %q.", dep.NativeDialect, dialect),
				})
				continue
			}
		}
		if req.NativePathAllowed != nil && !req.NativePathAllowed(adp) {
			sawPathDenied = true
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_COMPLIANCE_FILTER",
				RuleCode:     "NATIVE_PATH_NOT_ALLOWED",
				Detail:       "Native upstream path is denied by Default-Deny adapter policy.",
			})
			continue
		}

		// Passed Step 2! Enter Step 3 (Hard Capability & Context-Window Filter).
		step3Entered++
		if cand.LogicalModel != nil && cand.LogicalModel.ContextWindow > 0 && estTotal > cand.LogicalModel.ContextWindow {
			step3ContextDenied = true
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_CAPABILITY_FILTER",
				RuleCode:     "CONTEXT_WINDOW_EXCEEDED",
				Detail:       fmt.Sprintf("Estimated tokens (%d) exceed model context window (%d).", estTotal, cand.LogicalModel.ContextWindow),
			})
			continue
		}

		if missingCap := firstMissingCapability(cand, req.RequireTestedCaps, out.RequiredCapabilities); missingCap != "" {
			step3MissingCaps = append(step3MissingCaps, missingCap)
			if step3MissingDetail == "" {
				step3MissingDetail = fmt.Sprintf("Deployment %q does not support %s.", dep.ID, missingCap)
			}
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HARD_CAPABILITY_FILTER",
				RuleCode:     "CAPABILITY_NOT_SUPPORTED",
				Detail:       fmt.Sprintf("Deployment declares or verifies %s=false.", missingCap),
			})
			continue
		}

		// Passed Step 3! Enter Step 4 (SM-04 Health & Circuit Filter).
		step4Entered++
		status := req.Breaker.PeekState(dep.ID, dep.State, dep.CooldownUntilMs)
		if !status.Eligible {
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dep.ID,
				Stage:        "HEALTH_AND_CIRCUIT_FILTER",
				RuleCode:     status.State,
				Detail:       fmt.Sprintf("Deployment SM-04 state is %s.", status.State),
			})
			continue
		}

		item.estCostNano = estimateCostNano(dep.Pricing, estIn, estOut)
		item.ewmaLatency = status.EWMALatency
		item.hasEWMA = status.HasEWMALatency
		survivors = append(survivors, item)
	}

	if len(survivors) == 0 {
		out.Rejection = buildNoSurvivorError(
			out.RouteSlug, dialect,
			sawQuarantined, sawDialectMismatch, sawPathDenied,
			sawModelDenied, modelDenial,
			sawTrustDenied, trustDenial,
			sawRegionDenied, regionDenialDetail,
			step3Entered, step4Entered,
			step3MissingCaps, step3MissingDetail, step3ContextDenied,
		)
		return out
	}

	// Step 5: Strategy Ranking.
	ordered := r.rankCandidates(routeKey, strategy, survivors, req.WeightedSeed, req.DryRun)

	// Step 6: Bound execution candidates to 1 + max_failover_hops.
	limit := 1 + maxHops
	if limit < 1 {
		limit = 1
	}
	if len(ordered) > limit {
		for _, dropped := range ordered[limit:] {
			out.RejectedDeployments = append(out.RejectedDeployments, ExplainRejectedDeployment{
				DeploymentID: dropped.deploymentID,
				Stage:        "FAILOVER_HOP_BOUND",
				RuleCode:     "MAX_FAILOVER_HOPS_EXCEEDED",
				Detail:       fmt.Sprintf("Candidate exceeded 1 + max_failover_hops (%d) cap.", limit),
			})
		}
		ordered = ordered[:limit]
	}

	out.SelectedTargets = make([]*RankedTarget, len(ordered))
	out.SelectedCandidatesOrdered = make([]ExplainSelectedCandidate, len(ordered))
	for i, item := range ordered {
		reason := reasonForRank(i, strategy, item.degradedConn)
		costMicros := item.estCostNano / 1000
		var ewmaMs float64
		if item.hasEWMA {
			ewmaMs = float64(item.ewmaLatency.Microseconds()) / 1000.0
		}
		out.SelectedTargets[i] = &RankedTarget{
			Rank:                i + 1,
			Target:              item.target,
			PriorityOrder:       item.priorityOrder,
			Weight:              item.weight,
			EstimatedCostNano:   item.estCostNano,
			EstimatedCostMicros: costMicros,
			EWMALatency:         item.ewmaLatency,
			HasEWMA:             item.hasEWMA,
			Reason:              reason,
		}
		out.SelectedCandidatesOrdered[i] = ExplainSelectedCandidate{
			Rank:                i + 1,
			DeploymentID:        item.deploymentID,
			DataRegion:          item.target.Deployment.DataRegion,
			EstimatedCostMicros: costMicros,
			EstimatedCostNano:   item.estCostNano,
			PriorityOrder:       item.priorityOrder,
			Weight:              item.weight,
			EWMALatencyMs:       ewmaMs,
			Reason:              reason,
		}
	}
	return out
}

func (r *Resolver) rankCandidates(routeKey, strategy string, items []rawCandidate, seed *int64, dryRun bool) []rawCandidate {
	switch strategy {
	case snapshot.RouteStrategyWeightedRR:
		return r.rankWeightedRR(routeKey+"|wrr", items, dryRun)

	case snapshot.RouteStrategyWeighted:
		return r.rankWeightedSample(items, seed)

	case snapshot.RouteStrategyLowestCost:
		cp := append([]rawCandidate(nil), items...)
		sort.SliceStable(cp, func(i, j int) bool {
			if cp[i].estCostNano != cp[j].estCostNano {
				return cp[i].estCostNano < cp[j].estCostNano
			}
			if cmp := compareEWMA(cp[i], cp[j]); cmp != 0 {
				return cmp < 0
			}
			if cp[i].priorityOrder != cp[j].priorityOrder {
				return cp[i].priorityOrder < cp[j].priorityOrder
			}
			return cp[i].deploymentID < cp[j].deploymentID
		})
		return cp

	case snapshot.RouteStrategyLowestLatency:
		cp := append([]rawCandidate(nil), items...)
		sort.SliceStable(cp, func(i, j int) bool {
			if cmp := compareEWMA(cp[i], cp[j]); cmp != 0 {
				return cmp < 0
			}
			if cp[i].priorityOrder != cp[j].priorityOrder {
				return cp[i].priorityOrder < cp[j].priorityOrder
			}
			return cp[i].deploymentID < cp[j].deploymentID
		})
		return cp

	default: // PRIORITY_FAILOVER
		return r.rankPriorityFailover(routeKey, items, dryRun)
	}
}

func (r *Resolver) rankPriorityFailover(routeKey string, items []rawCandidate, dryRun bool) []rawCandidate {
	cp := append([]rawCandidate(nil), items...)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].priorityOrder != cp[j].priorityOrder {
			return cp[i].priorityOrder < cp[j].priorityOrder
		}
		if cp[i].degradedConn != cp[j].degradedConn {
			return !cp[i].degradedConn
		}
		return cp[i].deploymentID < cp[j].deploymentID
	})

	out := make([]rawCandidate, 0, len(cp))
	for start := 0; start < len(cp); {
		end := start + 1
		for end < len(cp) && cp[end].priorityOrder == cp[start].priorityOrder && cp[end].degradedConn == cp[start].degradedConn {
			end++
		}
		if end-start == 1 {
			out = append(out, cp[start])
		} else {
			tierKey := fmt.Sprintf("%s|p%d|deg%t", routeKey, cp[start].priorityOrder, cp[start].degradedConn)
			tierOrdered := r.rankWeightedRR(tierKey, cp[start:end], dryRun)
			out = append(out, tierOrdered...)
		}
		start = end
	}
	return out
}

// rankWeightedRR applies Smooth Weighted Round-Robin across weights to pick the primary,
// then appends remaining backups ordered cleanly by weight/priority.
func (r *Resolver) rankWeightedRR(groupKey string, items []rawCandidate, dryRun bool) []rawCandidate {
	if len(items) <= 1 {
		return append([]rawCandidate(nil), items...)
	}

	effWeights := make([]int64, len(items))
	var totalWeight int64
	for i, it := range items {
		if it.weight > 0 {
			effWeights[i] = int64(it.weight)
			totalWeight += int64(it.weight)
		}
	}
	if totalWeight == 0 {
		for i := range effWeights {
			effWeights[i] = 1
		}
		totalWeight = int64(len(items))
	}

	r.swrrMu.Lock()
	var stateMap map[string]int64
	if dryRun {
		orig := r.swrrStates[groupKey]
		stateMap = make(map[string]int64, len(items))
		for k, v := range orig {
			stateMap[k] = v
		}
	} else {
		if r.swrrStates == nil {
			r.swrrStates = make(map[string]map[string]int64)
		}
		stateMap = r.swrrStates[groupKey]
		if stateMap == nil {
			stateMap = make(map[string]int64, len(items))
			r.swrrStates[groupKey] = stateMap
		}
	}

	bestIdx := -1
	var bestScore int64
	for i, it := range items {
		cur := stateMap[it.deploymentID] + effWeights[i]
		stateMap[it.deploymentID] = cur
		if effWeights[i] > 0 && (bestIdx == -1 || cur > bestScore || (cur == bestScore && it.deploymentID < items[bestIdx].deploymentID)) {
			bestIdx = i
			bestScore = cur
		}
	}
	if bestIdx >= 0 {
		stateMap[items[bestIdx].deploymentID] -= totalWeight
	}
	r.swrrMu.Unlock()

	out := make([]rawCandidate, 0, len(items))
	if bestIdx >= 0 {
		out = append(out, items[bestIdx])
	}
	rest := make([]rawCandidate, 0, len(items)-1)
	for i, it := range items {
		if i == bestIdx {
			continue
		}
		rest = append(rest, it)
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].weight != rest[j].weight {
			return rest[i].weight > rest[j].weight
		}
		if rest[i].priorityOrder != rest[j].priorityOrder {
			return rest[i].priorityOrder < rest[j].priorityOrder
		}
		return rest[i].deploymentID < rest[j].deploymentID
	})
	return append(out, rest...)
}

func (r *Resolver) rankWeightedSample(items []rawCandidate, seed *int64) []rawCandidate {
	if len(items) <= 1 {
		return append([]rawCandidate(nil), items...)
	}
	var rngSeed int64
	if seed != nil {
		rngSeed = *seed
	} else {
		rngSeed = int64(r.randCounter.Add(1)) ^ time.Now().UnixNano()
	}
	// Deterministic seeded local PRNG per request (weighted sampling without replacement).
	prng := rand.New(rand.NewSource(rngSeed)) // #nosec G404 -- weighted load distribution, non-cryptographic
	rem := append([]rawCandidate(nil), items...)
	sort.Slice(rem, func(i, j int) bool { return rem[i].deploymentID < rem[j].deploymentID })

	out := make([]rawCandidate, 0, len(rem))
	for len(rem) > 0 {
		var sum int64
		for _, it := range rem {
			if it.weight > 0 {
				sum += int64(it.weight)
			}
		}
		if sum <= 0 {
			out = append(out, rem...)
			break
		}
		pick := prng.Int63n(sum)
		chosen := 0
		var acc int64
		for i, it := range rem {
			if it.weight <= 0 {
				continue
			}
			acc += int64(it.weight)
			if pick < acc {
				chosen = i
				break
			}
		}
		out = append(out, rem[chosen])
		rem = append(rem[:chosen], rem[chosen+1:]...)
	}
	return out
}

func compareEWMA(a, b rawCandidate) int {
	if a.hasEWMA && b.hasEWMA {
		if a.ewmaLatency < b.ewmaLatency {
			return -1
		}
		if a.ewmaLatency > b.ewmaLatency {
			return 1
		}
		return 0
	}
	if a.hasEWMA != b.hasEWMA {
		// Measured healthy latency precedes unmeasured candidates.
		if a.hasEWMA {
			return -1
		}
		return 1
	}
	return 0
}

func reasonForRank(idx int, strategy string, degraded bool) string {
	prefix := "MATCHED_PRIMARY"
	if idx > 0 {
		prefix = "MATCHED_FALLBACK"
	}
	if degraded {
		return prefix + "_DEGRADED_" + strategy
	}
	return prefix + "_HEALTHY_" + strategy
}

// FirstMissingCapability returns the first capability in reqCaps not satisfied by target,
// or "" if all required capabilities are supported.
func FirstMissingCapability(target *snapshot.ResolvedTarget, requireTested bool, reqCaps []string) string {
	return firstMissingCapability(target, requireTested, normalizeCapabilities(reqCaps))
}

func firstMissingCapability(target *snapshot.ResolvedTarget, requireTested bool, reqCaps []string) string {
	for _, capName := range reqCaps {
		// Support custom_dimensions/dimensions dual alias as checked in embeddings handler.
		if capName == "custom_dimensions" || capName == "dimensions" {
			if !snapshot.DeploymentSupportsCapability(target, requireTested, "custom_dimensions") &&
				!snapshot.DeploymentSupportsCapability(target, requireTested, "dimensions") {
				return "custom_dimensions"
			}
			continue
		}
		if capName == "json_schema_loose" {
			if !snapshot.DeploymentSupportsCapability(target, requireTested, "strict_json_schema") &&
				!snapshot.DeploymentSupportsCapability(target, requireTested, "json_object") {
				return "strict_json_schema"
			}
			continue
		}
		if !snapshot.DeploymentSupportsCapability(target, requireTested, capName) {
			return capName
		}
	}
	return ""
}

func targetMatchesNativeDialect(target *snapshot.ResolvedTarget, dialect string) bool {
	if target == nil || target.Deployment == nil || target.Adapter == nil {
		return false
	}
	depDialect := strings.ToLower(strings.TrimSpace(target.Deployment.NativeDialect))
	if depDialect != "" {
		return depDialect == dialect
	}
	adpPrefix := strings.Trim(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(target.Adapter.NativePrefix)), "/native/"), "/")
	if adpPrefix != "" {
		return adpPrefix == dialect
	}
	if len(target.Adapter.SupportedDialects) == 1 {
		return strings.EqualFold(strings.TrimSpace(target.Adapter.SupportedDialects[0]), dialect)
	}
	return false
}

func estimateCostNano(p snapshot.PricingNanoUSD, estInTokens, estOutTokens int64) int64 {
	if p.IsCanonicalPerToken() {
		inCost := mulCapInt64(max(int64(0), estInTokens), max(int64(0), p.InputTokenNanoUSD))
		outCost := mulCapInt64(max(int64(0), estOutTokens), max(int64(0), p.OutputTokenNanoUSD))
		return safeAddInt64(inCost, outCost)
	}
	// Legacy v1 compatibility: price_per_1m * tokens / 1_000_000 in strict integer arithmetic (0 floats).
	inCost := mulDivSat(estInTokens, p.InputPer1MNanoUSD, 1_000_000)
	outCost := mulDivSat(estOutTokens, p.OutputPer1MNanoUSD, 1_000_000)
	return safeAddInt64(inCost, outCost)
}

func mulCapInt64(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > math.MaxInt64/b {
		return math.MaxInt64
	}
	return a * b
}

func mulDivSat(a, b, div int64) int64 {
	if a <= 0 || b <= 0 || div <= 0 {
		return 0
	}
	if a > math.MaxInt64/b {
		return math.MaxInt64
	}
	return (a * b) / div
}

func safeAddInt64(a, b int64) int64 {
	if a <= 0 {
		return max(int64(0), b)
	}
	if b <= 0 {
		return a
	}
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

func normalizeCapabilities(caps []string) []string {
	if len(caps) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(caps))
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		norm := strings.ToLower(strings.TrimSpace(c))
		if norm == "" {
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	return out
}

func summarizeEffectivePolicy(eff policyengine.EffectivePolicy, reqRegion, routeRegion string) EffectivePolicySummary {
	var regions []string
	if eff.HasAllowedDataRegions {
		regions = append([]string(nil), eff.AllowedDataRegions...)
	}
	if routeRegion != "" {
		regions = intersectSingleRegion(regions, eff.HasAllowedDataRegions, routeRegion)
	}
	if reqRegion != "" {
		hasReg := eff.HasAllowedDataRegions || routeRegion != ""
		regions = intersectSingleRegion(regions, hasReg, reqRegion)
	}
	if regions == nil {
		regions = []string{}
	}
	var maxCtx *int64
	if eff.MaxContextTokens != nil {
		v := *eff.MaxContextTokens
		maxCtx = &v
	}
	return EffectivePolicySummary{
		AllowedDataRegions:     regions,
		MinPluginTrust:         eff.MinPluginTrust,
		MinCapabilityAssurance: eff.MinCapabilityAssurance,
		MaxContextTokens:       maxCtx,
	}
}

func intersectSingleRegion(existing []string, hasExisting bool, reg string) []string {
	if !hasExisting {
		return []string{reg}
	}
	for _, r := range existing {
		if strings.EqualFold(r, "*") || strings.EqualFold(r, reg) {
			return []string{reg}
		}
	}
	return []string{}
}

func buildNoSurvivorError(
	modelSlug, dialect string,
	sawQuarantined, sawDialectMismatch, sawPathDenied bool,
	sawModelDenied bool, modelDenial policyengine.EvalDecision,
	sawTrustDenied bool, trustDenial policyengine.EvalDecision,
	sawRegionDenied bool, regionDetail string,
	step3Entered, step4Entered int,
	step3MissingCaps []string, step3MissingDetail string, step3ContextDenied bool,
) *RejectionError {
	if step3Entered > 0 && step4Entered == 0 {
		if len(step3MissingCaps) > 0 {
			msg := step3MissingDetail
			if msg == "" {
				msg = fmt.Sprintf("No compliant deployment for %q supports required capability %q.", modelSlug, step3MissingCaps[0])
			}
			return &RejectionError{
				HTTPStatus: http.StatusBadRequest,
				Code:       "capability_not_supported",
				ErrorType:  "invalid_request_error",
				Message:    msg,
				Retryable:  false,
			}
		}
		if step3ContextDenied {
			return &RejectionError{
				HTTPStatus: http.StatusBadRequest,
				Code:       "capability_not_supported",
				ErrorType:  "invalid_request_error",
				Message:    fmt.Sprintf("Estimated request tokens exceed context_window across all compliant deployments for %q.", modelSlug),
				Retryable:  false,
			}
		}
	}

	if step4Entered > 0 {
		return &RejectionError{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       "no_compliant_deployment_available",
			ErrorType:  "residency_unavailable_error",
			Message:    fmt.Sprintf("No compliant active deployment is available for model %q.", modelSlug),
			Retryable:  true,
		}
	}

	if sawQuarantined {
		return &RejectionError{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       "extension_quarantined",
			ErrorType:  "extension_error",
			Message:    "The backing provider adapter extension is quarantined.",
			Retryable:  false,
		}
	}
	if dialect != "" && sawDialectMismatch {
		return &RejectionError{
			HTTPStatus: http.StatusBadRequest,
			Code:       "dialect_mismatch",
			ErrorType:  "invalid_request_error",
			Message:    fmt.Sprintf("Target deployment for model %q uses a different native dialect than %q.", modelSlug, dialect),
			Retryable:  false,
		}
	}
	if sawPathDenied {
		return &RejectionError{
			HTTPStatus: http.StatusForbidden,
			Code:       "native_path_not_allowed",
			ErrorType:  "permission_error",
			Message:    "Native upstream path is denied by Default-Deny adapter policy.",
			Retryable:  false,
		}
	}
	if sawModelDenied {
		return &RejectionError{
			HTTPStatus: http.StatusForbidden,
			Code:       policyengine.DenialCodeModelDenied,
			ErrorType:  "permission_error",
			Message:    modelDenial.DenialReason,
			Retryable:  false,
		}
	}
	if sawTrustDenied {
		return &RejectionError{
			HTTPStatus: trustDenial.HTTPStatus,
			Code:       policyengine.DenialCodeTrustDenied,
			ErrorType:  trustDenial.ErrorType,
			Message:    trustDenial.DenialReason,
			Retryable:  false,
		}
	}
	if sawRegionDenied {
		return &RejectionError{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       "no_compliant_deployment_available",
			ErrorType:  "residency_unavailable_error",
			Message:    regionDetail,
			Retryable:  true,
		}
	}
	return &RejectionError{
		HTTPStatus: http.StatusServiceUnavailable,
		Code:       "no_compliant_deployment_available",
		ErrorType:  "residency_unavailable_error",
		Message:    fmt.Sprintf("No compliant active deployment is available for model %q.", modelSlug),
		Retryable:  true,
	}
}
