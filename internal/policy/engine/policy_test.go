// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package engine_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

func i64(v int64) *int64 { return &v }

type spyAuditSink struct {
	events []port.AuditEvent
}

func (s *spyAuditSink) Append(_ context.Context, ev port.AuditEvent) error {
	s.events = append(s.events, ev)
	return nil
}

func TestMergeLayersMonotonicNarrowing(t *testing.T) {
	t.Parallel()

	// Intentionally pass layers out of hierarchy order to test deterministic rank sorting.
	eff := engine.MergeLayers(
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeVirtualKey,
			ScopeID:   "vk_1",
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels:       true,
				AllowedModels:          []string{"gpt-4o", "claude-sonnet-4", "o3"},
				HasAllowedDataRegions:  true,
				AllowedDataRegions:     []string{"eu-central-1", "us-east-1"},
				MinPluginTrust:         engine.MinTrustAnyApproved,
				MinCapabilityAssurance: engine.AssuranceDeclared,
				RPMLimit:               i64(10),
				TPMLimit:               i64(25000),
				MaxConcurrentStreams:   i64(2),
				MaxContextTokens:       i64(8192),
				MaxRequestBodyBytes:    i64(1024 * 1024),
				GuardrailHooks: []snapshot.GuardrailHookSpec{
					{ExtensionID: "ext_pii", FailurePosture: snapshot.PolicyFailureOpen}, // cannot weaken Org FAIL_CLOSED
					{ExtensionID: "ext_audit", FailurePosture: snapshot.PolicyFailureClosed},
				},
			},
		},
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeEnv,
			ScopeID:   "env_1",
			Rules: snapshot.PolicyRulesView{
				MinCapabilityAssurance: engine.AssuranceVerified,
				RPMLimit:               i64(20),
				GuardrailHooks: []snapshot.GuardrailHookSpec{
					{ExtensionID: "ext_env", FailurePosture: snapshot.PolicyFailureOpen},
				},
			},
		},
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeProject,
			ScopeID:   "prj_1",
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels:      true,
				AllowedModels:         []string{"gpt-4o", "claude-sonnet-4"},
				DeniedModels:          []string{"claude-sonnet-4"},
				HasAllowedAdapters:    true,
				AllowedAdapters:       []string{"openai-chat", "anthropic-messages"},
				HasAllowedDataRegions: true,
				AllowedDataRegions:    []string{"EU-CENTRAL-1", "EU-WEST-1"},
				RPMLimit:              i64(30),
			},
		},
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			ScopeID:   "org_1",
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels:      true,
				AllowedModels:         []string{"*"},
				HasAllowedAdapters:    true,
				AllowedAdapters:       []string{"openai-chat"},
				DeniedAdapters:        []string{"anthropic-messages"},
				HasAllowedDataRegions: true,
				AllowedDataRegions:    []string{"EU-CENTRAL-1", "EU-WEST-1"},
				MinPluginTrust:        engine.MinTrustOfficialOnly,
				RPMLimit:              i64(100),
				TPMLimit:              i64(100000),
				MaxConcurrentStreams:  i64(10),
				MaxContextTokens:      i64(32768),
				MaxRequestBodyBytes:   i64(4 * 1024 * 1024),
				GuardrailHooks: []snapshot.GuardrailHookSpec{
					{ExtensionID: "ext_pii", FailurePosture: snapshot.PolicyFailureClosed},
				},
			},
		},
	)

	if !reflect.DeepEqual(eff.AllowedModels, []string{"gpt-4o"}) {
		t.Fatalf("expected AllowedModels [gpt-4o], got %v", eff.AllowedModels)
	}
	if !reflect.DeepEqual(eff.AllowedAdapters, []string{"openai-chat"}) {
		t.Fatalf("expected AllowedAdapters [openai-chat], got %v", eff.AllowedAdapters)
	}
	if !reflect.DeepEqual(eff.AllowedDataRegions, []string{"EU-CENTRAL-1"}) {
		t.Fatalf("expected AllowedDataRegions [EU-CENTRAL-1], got %v", eff.AllowedDataRegions)
	}
	if eff.MinPluginTrust != engine.MinTrustOfficialOnly {
		t.Fatalf("expected MinPluginTrust OFFICIAL_ONLY, got %q", eff.MinPluginTrust)
	}
	if eff.MinCapabilityAssurance != engine.AssuranceVerified {
		t.Fatalf("expected MinCapabilityAssurance VERIFIED, got %q", eff.MinCapabilityAssurance)
	}
	if *eff.RPMLimit != 10 || *eff.TPMLimit != 25000 || *eff.MaxConcurrentStreams != 2 || *eff.MaxContextTokens != 8192 {
		t.Fatalf("unexpected min numeric limits: rpm=%d tpm=%d streams=%d ctx=%d", *eff.RPMLimit, *eff.TPMLimit, *eff.MaxConcurrentStreams, *eff.MaxContextTokens)
	}
	if len(eff.GuardrailHooks) != 3 || eff.GuardrailHooks[0].ExtensionID != "ext_pii" || eff.GuardrailHooks[0].FailurePosture != snapshot.PolicyFailureClosed {
		t.Fatalf("expected ancestor guardrail posture preserved as FAIL_CLOSED, got %+v", eff.GuardrailHooks)
	}

	// Verify tightening child FAIL_OPEN -> FAIL_CLOSED works cleanly.
	eff2 := engine.MergeLayers(
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels: true,
				AllowedModels:    []string{"*"},
				GuardrailHooks: []snapshot.GuardrailHookSpec{
					{ExtensionID: "ext_soft", FailurePosture: snapshot.PolicyFailureOpen},
				},
			},
		},
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeProject,
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels: true,
				AllowedModels:    []string{"*"},
				GuardrailHooks: []snapshot.GuardrailHookSpec{
					{ExtensionID: "ext_soft", FailurePosture: snapshot.PolicyFailureClosed},
				},
			},
		},
	)
	if !reflect.DeepEqual(eff2.AllowedModels, []string{"*"}) || eff2.GuardrailHooks[0].FailurePosture != snapshot.PolicyFailureClosed {
		t.Fatalf("expected wildcard + tightened guardrail hook, got %+v", eff2)
	}
}

func TestValidateChildPolicyWithinAncestorsNegativeCases(t *testing.T) {
	t.Parallel()

	ancestors := []engine.ScopedLayer{
		{
			ScopeType: snapshot.PolicyScopeOrg,
			ScopeID:   "org_1",
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels:       true,
				AllowedModels:          []string{"gpt-4o", "claude-sonnet-4"},
				DeniedModels:           []string{"gpt-4o-mini"},
				HasAllowedAdapters:     true,
				AllowedAdapters:        []string{"openai-chat"},
				DeniedAdapters:         []string{"banned-adapter"},
				HasAllowedDataRegions:  true,
				AllowedDataRegions:     []string{"EU-CENTRAL-1", "EU-WEST-1"},
				MinPluginTrust:         engine.MinTrustVerifiedOnly,
				MinCapabilityAssurance: engine.AssuranceTested,
				RPMLimit:               i64(100),
				TPMLimit:               i64(50000),
				MaxConcurrentStreams:   i64(5),
				MaxContextTokens:       i64(16384),
				MaxRequestBodyBytes:    i64(2048),
				GuardrailHooks: []snapshot.GuardrailHookSpec{
					{ExtensionID: "ext_pii", FailurePosture: snapshot.PolicyFailureClosed},
				},
			},
		},
	}

	forbiddenCases := []struct {
		name      string
		candidate engine.CandidatePolicy
	}{
		{
			name: "mandatory_fail_open_rate_limit",
			candidate: engine.CandidatePolicy{
				ScopeType:   snapshot.PolicyScopeProject,
				PolicyType:  snapshot.PolicyTypeRateLimit,
				FailureMode: snapshot.PolicyFailureOpen,
			},
		},
		{
			name: "guardrail_weakens_ancestor_fail_closed",
			candidate: engine.CandidatePolicy{
				ScopeType:   snapshot.PolicyScopeProject,
				PolicyType:  snapshot.PolicyTypeGuardrail,
				FailureMode: snapshot.PolicyFailureOpen,
			},
		},
		{
			name: "reenable_denylisted_model",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeModelAllowlist,
				Rules: snapshot.PolicyRulesView{
					HasAllowedModels: true,
					AllowedModels:    []string{"gpt-4o-mini"},
				},
			},
		},
		{
			name: "wildcard_models_under_restricted_org",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeModelAllowlist,
				Rules: snapshot.PolicyRulesView{
					HasAllowedModels: true,
					AllowedModels:    []string{"*"},
				},
			},
		},
		{
			name: "widen_models_outside_org_allowlist",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeModelAllowlist,
				Rules: snapshot.PolicyRulesView{
					HasAllowedModels: true,
					AllowedModels:    []string{"gpt-4o", "o3"},
				},
			},
		},
		{
			name: "reenable_denylisted_adapter",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeModelAllowlist,
				Rules: snapshot.PolicyRulesView{
					HasAllowedAdapters: true,
					AllowedAdapters:    []string{"banned-adapter"},
				},
			},
		},
		{
			name: "wildcard_adapters_widens",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeModelAllowlist,
				Rules: snapshot.PolicyRulesView{
					HasAllowedAdapters: true,
					AllowedAdapters:    []string{"*"},
				},
			},
		},
		{
			name: "widen_region_to_us_east_1",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeDataResidency,
				Rules: snapshot.PolicyRulesView{
					HasAllowedDataRegions: true,
					AllowedDataRegions:    []string{"eu-central-1", "us-east-1"},
				},
			},
		},
		{
			name: "wildcard_region_under_eu_only",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeDataResidency,
				Rules: snapshot.PolicyRulesView{
					HasAllowedDataRegions: true,
					AllowedDataRegions:    []string{"*"},
				},
			},
		},
		{
			name: "weaken_plugin_trust",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeModelAllowlist,
				Rules: snapshot.PolicyRulesView{
					MinPluginTrust: engine.MinTrustAnyApproved,
				},
			},
		},
		{
			name: "weaken_capability_assurance",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeEnv,
				PolicyType: snapshot.PolicyTypeModelAllowlist,
				Rules: snapshot.PolicyRulesView{
					MinCapabilityAssurance: engine.AssuranceDeclared,
				},
			},
		},
		{
			name: "raise_rpm_above_org",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeRateLimit,
				Rules:      snapshot.PolicyRulesView{RPMLimit: i64(150)},
			},
		},
		{
			name: "raise_tpm_above_org",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeRateLimit,
				Rules:      snapshot.PolicyRulesView{TPMLimit: i64(90000)},
			},
		},
		{
			name: "raise_streams_above_org",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeVirtualKey,
				PolicyType: snapshot.PolicyTypeRateLimit,
				Rules:      snapshot.PolicyRulesView{MaxConcurrentStreams: i64(8)},
			},
		},
		{
			name: "raise_max_context_above_org",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeRateLimit,
				Rules:      snapshot.PolicyRulesView{MaxContextTokens: i64(32768)},
			},
		},
		{
			name: "raise_max_body_above_org",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeRateLimit,
				Rules:      snapshot.PolicyRulesView{MaxRequestBodyBytes: i64(4096)},
			},
		},
		{
			name: "weaken_guardrail_hook_posture",
			candidate: engine.CandidatePolicy{
				ScopeType:  snapshot.PolicyScopeProject,
				PolicyType: snapshot.PolicyTypeGuardrail,
				Rules: snapshot.PolicyRulesView{
					GuardrailHooks: []snapshot.GuardrailHookSpec{
						{ExtensionID: "ext_pii", FailurePosture: snapshot.PolicyFailureOpen},
					},
				},
			},
		},
	}

	for _, tc := range forbiddenCases {
		t.Run(tc.name, func(t *testing.T) {
			err := engine.ValidateChildPolicyWithinAncestors(ancestors, true, tc.candidate)
			if !errors.Is(err, engine.ErrPolicyRelaxationForbidden) {
				t.Fatalf("expected ErrPolicyRelaxationForbidden, got: %v", err)
			}
		})
	}

	// Compliant narrower child must pass.
	okCand := engine.CandidatePolicy{
		ScopeType:   snapshot.PolicyScopeProject,
		PolicyType:  snapshot.PolicyTypeRateLimit,
		FailureMode: snapshot.PolicyFailureClosed,
		Rules: snapshot.PolicyRulesView{
			HasAllowedModels:       true,
			AllowedModels:          []string{"gpt-4o"},
			HasAllowedDataRegions:  true,
			AllowedDataRegions:     []string{"EU-CENTRAL-1"},
			MinPluginTrust:         engine.MinTrustOfficialOnly,
			MinCapabilityAssurance: engine.AssuranceVerified,
			RPMLimit:               i64(30),
			TPMLimit:               i64(10000),
			MaxConcurrentStreams:   i64(2),
			MaxContextTokens:       i64(8192),
			MaxRequestBodyBytes:    i64(1024),
			GuardrailHooks: []snapshot.GuardrailHookSpec{
				{ExtensionID: "ext_pii", FailurePosture: snapshot.PolicyFailureClosed},
			},
		},
	}
	if err := engine.ValidateChildPolicyWithinAncestors(ancestors, true, okCand); err != nil {
		t.Fatalf("expected compliant narrowing child to pass, got: %v", err)
	}
}

func TestEvaluatorDenialCodesAndSnapshotFailClosed(t *testing.T) {
	t.Parallel()
	eval := engine.NewEvaluator()

	// Nil snapshot -> 503 policy_engine_unavailable
	dec := eval.Evaluate(nil, nil, nil, engine.EvalRequest{ModelSlug: "gpt-4o"})
	if dec.Allowed || dec.HTTPStatus != http.StatusServiceUnavailable || dec.DenialCode != engine.DenialCodeEngineUnavailable {
		t.Fatalf("expected 503 policy_engine_unavailable on nil snapshot, got %+v", dec)
	}

	eff := engine.MergeLayers(engine.ScopedLayer{
		ScopeType: snapshot.PolicyScopeOrg,
		Rules: snapshot.PolicyRulesView{
			HasAllowedModels:       true,
			AllowedModels:          []string{"gpt-4o"},
			DeniedModels:           []string{"gpt-4o-mini"},
			HasAllowedAdapters:     true,
			AllowedAdapters:        []string{"openai-chat"},
			HasAllowedDataRegions:  true,
			AllowedDataRegions:     []string{"EU-CENTRAL-1"},
			MinPluginTrust:         engine.MinTrustVerifiedOrEnterpriseSigned,
			MinCapabilityAssurance: engine.AssuranceTested,
			MaxContextTokens:       i64(4096),
			MaxRequestBodyBytes:    i64(1024),
		},
	})

	cases := []struct {
		name     string
		req      engine.EvalRequest
		wantCode string
		wantHTTP int
	}{
		{
			name:     "denied_model",
			req:      engine.EvalRequest{ModelSlug: "gpt-4o-mini"},
			wantCode: engine.DenialCodeModelDenied,
			wantHTTP: http.StatusForbidden,
		},
		{
			name:     "denied_adapter",
			req:      engine.EvalRequest{ModelSlug: "gpt-4o", AdapterSlug: "anthropic-messages"},
			wantCode: engine.DenialCodeModelDenied,
			wantHTTP: http.StatusForbidden,
		},
		{
			name:     "denied_requested_region",
			req:      engine.EvalRequest{ModelSlug: "gpt-4o", RequestedDataRegion: "US-EAST-1"},
			wantCode: engine.DenialCodeRegionDenied,
			wantHTTP: http.StatusForbidden,
		},
		{
			name:     "denied_deployment_region",
			req:      engine.EvalRequest{ModelSlug: "gpt-4o", DataRegion: "AP-SOUTH-1"},
			wantCode: engine.DenialCodeRegionDenied,
			wantHTTP: http.StatusForbidden,
		},
		{
			name:     "payload_too_large",
			req:      engine.EvalRequest{ModelSlug: "gpt-4o", RequestBodyBytes: 2048},
			wantCode: engine.DenialCodePayloadTooLarge,
			wantHTTP: http.StatusRequestEntityTooLarge,
		},
		{
			name:     "max_context_exceeded",
			req:      engine.EvalRequest{ModelSlug: "gpt-4o", EstimatedTotalTokens: 5000},
			wantCode: engine.DenialCodeMaxContextDenied,
			wantHTTP: http.StatusBadRequest,
		},
		{
			name:     "trust_denied",
			req:      engine.EvalRequest{ModelSlug: "gpt-4o", ExtensionTrustTier: "unverified_external"},
			wantCode: engine.DenialCodeTrustDenied,
			wantHTTP: http.StatusForbidden,
		},
		{
			name:     "assurance_denied",
			req:      engine.EvalRequest{ModelSlug: "gpt-4o", CapabilityAssurance: "DECLARED"},
			wantCode: engine.DenialCodeTrustDenied,
			wantHTTP: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := engine.EvaluateEffective(eff, tc.req)
			if got.Allowed || got.DenialCode != tc.wantCode || got.HTTPStatus != tc.wantHTTP {
				t.Fatalf("got allowed=%v code=%q status=%d; want code=%q status=%d", got.Allowed, got.DenialCode, got.HTTPStatus, tc.wantCode, tc.wantHTTP)
			}
		})
	}

	okDec := engine.EvaluateEffective(eff, engine.EvalRequest{
		ModelSlug:            "gpt-4o",
		AdapterSlug:          "openai-chat",
		DataRegion:           "eu-central-1",
		RequestedDataRegion:  "eu-central-1",
		ExtensionTrustTier:   "enterprise_signed",
		CapabilityAssurance:  "VERIFIED",
		RequestBodyBytes:     512,
		EstimatedTotalTokens: 1024,
	})
	if !okDec.Allowed {
		t.Fatalf("expected compliant request allowed, got %+v", okDec)
	}
}

func TestStoreCreateUpdateGetAndAuditOnRelaxationNEG1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000300000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "policy_store.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	audit := &spyAuditSink{}
	polStore, err := engine.NewStore(engine.StoreConfig{
		DB:    st.RawDB(),
		Audit: audit,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "pol-eng", DisplayName: "Policy Eng"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "p-eu", DisplayName: "EU Project"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}

	// Org creates baseline residency + model denylist + RPM cap.
	orgPol, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:      org.ID,
		ScopeType:  snapshot.PolicyScopeOrg,
		ScopeID:    org.ID,
		PolicyType: snapshot.PolicyTypeDataResidency,
		RulesJSON:  `{"allowed_data_regions":["EU-CENTRAL-1","EU-WEST-1"],"denied_models":["legacy-bad"],"rpm_limit":100}`,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create org policy: %v", err)
	}
	if orgPol.FailureMode != snapshot.PolicyFailureClosed || orgPol.Version != 1 {
		t.Fatalf("unexpected created policy: %+v", orgPol)
	}

	// NEG-1: TEAM_ADMIN submits Project policy trying to allow us-east-1 -> 422 policy_relaxation_forbidden + audit emitted.
	reqID := "req_01TESTRELAXATIONBLOCKED"
	_, err = polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:      org.ID,
		ScopeType:  snapshot.PolicyScopeProject,
		ScopeID:    proj.ID,
		PolicyType: snapshot.PolicyTypeDataResidency,
		RulesJSON:  `{"allowed_data_regions":["us-east-1"]}`,
		Enabled:    true,
		ActorKind:  "principal",
		ActorID:    "usr_team_admin",
		EntryPoint: "admin_api",
		RequestID:  &reqID,
	})
	if !errors.Is(err, engine.ErrPolicyRelaxationForbidden) {
		t.Fatalf("expected ErrPolicyRelaxationForbidden, got: %v", err)
	}
	if len(audit.events) != 1 || audit.events[0].EventType != "policy.relaxation_blocked" {
		t.Fatalf("expected policy.relaxation_blocked audit event, got: %+v", audit.events)
	}
	var meta map[string]string
	_ = json.Unmarshal([]byte(audit.events[0].MetadataJSON), &meta)
	if meta["code"] != "policy_relaxation_forbidden" {
		t.Fatalf("expected code=policy_relaxation_forbidden in audit metadata, got %+v", meta)
	}

	// Valid narrowed project policy succeeds.
	projPol, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:      org.ID,
		ScopeType:  snapshot.PolicyScopeProject,
		ScopeID:    proj.ID,
		PolicyType: snapshot.PolicyTypeRateLimit,
		RulesJSON:  `{"allowed_data_regions":["EU-CENTRAL-1"],"rpm_limit":30}`,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create narrowed project policy: %v", err)
	}

	// Child Env policy narrowing further to RPM=10 succeeds.
	_, err = polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:      org.ID,
		ScopeType:  snapshot.PolicyScopeEnv,
		ScopeID:    env.ID,
		PolicyType: snapshot.PolicyTypeRateLimit,
		RulesJSON:  `{"rpm_limit":10}`,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create env policy: %v", err)
	}

	// Update with stale version -> ErrVersionConflict.
	newRules := `{"rpm_limit":25,"allowed_data_regions":["EU-CENTRAL-1"]}`
	if _, err := polStore.UpdatePolicy(ctx, org.ID, projPol.ID, 99, engine.UpdatePolicyParams{RulesJSON: &newRules}); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict, got: %v", err)
	}
	// Update raising RPM above Org (200 > 100) -> ErrPolicyRelaxationForbidden.
	badRules := `{"rpm_limit":200}`
	if _, err := polStore.UpdatePolicy(ctx, org.ID, projPol.ID, 1, engine.UpdatePolicyParams{RulesJSON: &badRules}); !errors.Is(err, engine.ErrPolicyRelaxationForbidden) {
		t.Fatalf("expected ErrPolicyRelaxationForbidden on update widening, got: %v", err)
	}
	// Valid update increments version to 2.
	updated, err := polStore.UpdatePolicy(ctx, org.ID, projPol.ID, 1, engine.UpdatePolicyParams{RulesJSON: &newRules})
	if err != nil || updated.Version != 2 {
		t.Fatalf("expected version 2 after update, got pol=%+v err=%v", updated, err)
	}

	// Verify runtime ConfigSnapshot integrates Org ∩ Project ∩ Env policies seamlessly!
	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	eff, err := engine.MergeEffectiveFromSnapshot(mgr.Current(), &snapshot.EffectiveConfig{
		OrgID:          org.ID,
		ProjectID:      proj.ID,
		EnvID:          env.ID,
		MinPluginTrust: engine.MinTrustVerifiedOnly,
	}, nil)
	if err != nil {
		t.Fatalf("MergeEffectiveFromSnapshot failed: %v", err)
	}
	if *eff.RPMLimit != 10 || !reflect.DeepEqual(eff.AllowedDataRegions, []string{"EU-CENTRAL-1"}) {
		t.Fatalf("expected merged snapshot policy RPM=10 Regions=[EU-CENTRAL-1], got rpm=%+v regions=%v", eff.RPMLimit, eff.AllowedDataRegions)
	}

	// GetPolicy & ErrNotFound check.
	gotPol, err := polStore.GetPolicy(ctx, org.ID, projPol.ID)
	if err != nil || gotPol.Version != 2 {
		t.Fatalf("GetPolicy unexpected result: got=%+v err=%v", gotPol, err)
	}
	if _, err := polStore.GetPolicy(ctx, org.ID, "pol_nonexistent"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}

	// Evaluator.ResolveEffective with full VirtualKey + Project.enforced_region + Env.require_tested_caps + VirtualKey policy row.
	reg := "EU-CENTRAL-1"
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE projects SET enforced_region = ? WHERE id = ?`, reg, proj.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE environments SET require_tested_caps = 1 WHERE id = ?`, env.ID); err != nil {
		t.Fatal(err)
	}
	vkStreamMax := int64(3)
	vkRow := &snapshot.VirtualKeyView{
		ID:                   "vk_test_01",
		OrgID:                org.ID,
		ProjectID:            proj.ID,
		EnvID:                env.ID,
		AllowedModels:        []string{"*"},
		RPMLimit:             i64(5),
		TPMLimit:             i64(12000),
		MaxConcurrentStreams: vkStreamMax,
	}
	if _, err := st.RawDB().ExecContext(ctx, `INSERT INTO virtual_keys (id, org_id, project_id, env_id, name, key_prefix, last_four, active_key_hash, state, allowed_planes, allowed_models_json, allowed_routes_json, max_concurrent_streams, version, created_at_ms, updated_at_ms) VALUES (?, ?, ?, ?, 'k1', 'vk_tp_1', '0001', X'000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f', 'CREATED_ACTIVE', 'PROVIDER', '["*"]', '["*"]', ?, 1, ?, ?)`, vkRow.ID, org.ID, proj.ID, env.ID, vkStreamMax, nowFn(), nowFn()); err != nil {
		t.Fatal(err)
	}
	// VK-scoped policy that attempts to exceed VK/Env/Project is rejected by loadAncestorLayersTx inference!
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:      org.ID,
		ScopeType:  snapshot.PolicyScopeVirtualKey,
		ScopeID:    vkRow.ID,
		PolicyType: snapshot.PolicyTypeRateLimit,
		RulesJSON:  `{"rpm_limit": 200}`,
		Enabled:    true,
	}); !errors.Is(err, engine.ErrPolicyRelaxationForbidden) {
		t.Fatalf("expected VK relaxation above Env/Project/Org blocked, got: %v", err)
	}
	// Compliant VK-scoped policy succeeds.
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:      org.ID,
		ScopeType:  snapshot.PolicyScopeVirtualKey,
		ScopeID:    vkRow.ID,
		PolicyType: snapshot.PolicyTypeRateLimit,
		RulesJSON:  `{"rpm_limit": 4}`,
		Enabled:    true,
	}); err != nil {
		t.Fatalf("create VK policy failed: %v", err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	eval := engine.NewEvaluator()
	effVK, err := eval.ResolveEffective(mgr.Current(), &snapshot.EffectiveConfig{
		OrgID:             org.ID,
		ProjectID:         proj.ID,
		EnvID:             env.ID,
		MinPluginTrust:    engine.MinTrustOfficialOnly,
		EnforcedRegion:    &reg,
		RequireTestedCaps: true,
	}, vkRow)
	if err != nil {
		t.Fatalf("ResolveEffective failed: %v", err)
	}
	if *effVK.RPMLimit != 4 || effVK.MinPluginTrust != engine.MinTrustOfficialOnly || effVK.MinCapabilityAssurance != engine.AssuranceTested || *effVK.MaxConcurrentStreams != 3 {
		t.Fatalf("unexpected merged VK policy: %+v", effVK)
	}

	// Update FailureMode/Enabled & test validation errors + GetPolicy corrupt row.
	disabled := false
	fmOpen := snapshot.PolicyFailureOpen
	gPol, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:       org.ID,
		ScopeType:   snapshot.PolicyScopeOrg,
		ScopeID:     org.ID,
		PolicyType:  snapshot.PolicyTypeGuardrail,
		FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON:   `{"guardrail_hooks":[{"extension_id":"ext_1","failure_posture":"FAIL_CLOSED"}]}`,
		Enabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := polStore.UpdatePolicy(ctx, org.ID, gPol.ID, 1, engine.UpdatePolicyParams{FailureMode: &fmOpen, Enabled: &disabled}); err != nil {
		t.Fatalf("update org guardrail failed: %v", err)
	}
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{ID: gPol.ID, OrgID: org.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: org.ID, PolicyType: snapshot.PolicyTypeGuardrail, RulesJSON: `{}`}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists on duplicate policy ID, got %v", err)
	}
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{OrgID: "", ScopeType: "INVALID"}); !errors.Is(err, engine.ErrInvalidPolicyInput) {
		t.Fatalf("expected ErrInvalidPolicyInput, got %v", err)
	}
	if _, err := polStore.UpdatePolicy(ctx, "", "", 0, engine.UpdatePolicyParams{}); !errors.Is(err, engine.ErrInvalidPolicyInput) {
		t.Fatalf("expected ErrInvalidPolicyInput on empty update params, got %v", err)
	}
	if _, err := engine.NewStore(engine.StoreConfig{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil DB in NewStore, got %v", err)
	}

	// Corrupt stored policy row triggers ErrPolicyEngineUnavailable on GetPolicy and Eval.
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE policies SET rules_json = '{"bad":1}' WHERE id = ?`, gPol.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := polStore.GetPolicy(ctx, org.ID, gPol.ID); !errors.Is(err, engine.ErrPolicyEngineUnavailable) {
		t.Fatalf("expected ErrPolicyEngineUnavailable on corrupt GetPolicy, got %v", err)
	}
}

func TestTrustAllTiersAndEmptyAllowlistEdges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		min  string
		tier string
		want bool
	}{
		{engine.MinTrustOfficialOnly, "official", true},
		{engine.MinTrustOfficialOnly, "verified", false},
		{engine.MinTrustVerifiedOnly, "verified", true},
		{engine.MinTrustVerifiedOnly, "enterprise_signed", false},
		{engine.MinTrustVerifiedOrEnterpriseSigned, "enterprise_signed", true},
		{engine.MinTrustVerifiedOrEnterpriseSigned, "unverified_external", false},
		{engine.MinTrustAnyApproved, "unverified_external", true},
		{engine.MinTrustAnyApproved, "bogus", false},
	}
	for _, tc := range cases {
		eff := engine.MergeLayers(engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			Rules:     snapshot.PolicyRulesView{MinPluginTrust: tc.min},
		})
		dec := engine.EvaluateEffective(eff, engine.EvalRequest{ModelSlug: "m", ExtensionTrustTier: tc.tier})
		if dec.Allowed != tc.want {
			t.Fatalf("trust min=%q tier=%q got=%v want=%v", tc.min, tc.tier, dec.Allowed, tc.want)
		}
	}
	// Unknown EffectivePolicy.MinPluginTrust evaluates fail-closed.
	if dec := engine.EvaluateEffective(engine.EffectivePolicy{MinPluginTrust: "UNKNOWN_POLICY"}, engine.EvalRequest{ModelSlug: "m", ExtensionTrustTier: "official"}); dec.Allowed || dec.DenialCode != engine.DenialCodeTrustDenied {
		t.Fatalf("expected unknown MinPluginTrust to fail closed, got %+v", dec)
	}

	// Empty allowed_data_regions intersection fails closed with policy_denied_region.
	emptyRegEff := engine.MergeLayers(
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			Rules:     snapshot.PolicyRulesView{HasAllowedDataRegions: true, AllowedDataRegions: []string{"EU-CENTRAL-1"}},
		},
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeProject,
			Rules:     snapshot.PolicyRulesView{HasAllowedDataRegions: true, AllowedDataRegions: []string{"US-EAST-1"}},
		},
	)
	dec := engine.EvaluateEffective(emptyRegEff, engine.EvalRequest{ModelSlug: "m"})
	if dec.Allowed || dec.DenialCode != engine.DenialCodeRegionDenied {
		t.Fatalf("expected policy_denied_region on empty region intersection, got %+v", dec)
	}
	if engine.ModelPermitted(emptyRegEff, "") || engine.AdapterPermitted(emptyRegEff, "") || engine.RegionPermitted(emptyRegEff, "") {
		t.Fatal("empty slugs or empty region against empty intersection must be denied")
	}

	// Adapter allowlist + denylist checks + RegionPermitted wildcard checks.
	adpEff := engine.MergeLayers(
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			Rules: snapshot.PolicyRulesView{
				HasAllowedAdapters: true,
				AllowedAdapters:    []string{"openai-chat", "anthropic-messages"},
				DeniedAdapters:     []string{"anthropic-messages"},
			},
		},
	)
	if !engine.AdapterPermitted(adpEff, "openai-chat") || engine.AdapterPermitted(adpEff, "anthropic-messages") || engine.AdapterPermitted(adpEff, "other") {
		t.Fatalf("unexpected AdapterPermitted behavior: %+v", adpEff)
	}
	if !engine.RegionPermitted(adpEff, "") || !engine.RegionPermitted(adpEff, "US-EAST-1") {
		t.Fatal("unrestricted region policy should allow empty/any region")
	}

	// ScopeHierarchyRank unknown + ValidateChildPolicyWithinAncestors invalid scope/failureMode.
	if engine.ScopeHierarchyRank("UNKNOWN") != 0 {
		t.Fatal("expected rank 0 for UNKNOWN scope")
	}
	if err := engine.ValidateChildPolicyWithinAncestors(nil, false, engine.CandidatePolicy{ScopeType: "UNKNOWN"}); !errors.Is(err, engine.ErrInvalidPolicyInput) {
		t.Fatalf("expected ErrInvalidPolicyInput on unknown scope, got %v", err)
	}
	if err := engine.ValidateChildPolicyWithinAncestors(nil, false, engine.CandidatePolicy{
		ScopeType:   snapshot.PolicyScopeOrg,
		PolicyType:  snapshot.PolicyTypeGuardrail,
		FailureMode: "INVALID_MODE",
	}); !errors.Is(err, engine.ErrInvalidPolicyInput) {
		t.Fatalf("expected ErrInvalidPolicyInput on invalid failure_mode, got %v", err)
	}
}

func TestEvaluatorEvaluateAndStoreEdgeCases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000400000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "edge.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	org, _ := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "edge-org", DisplayName: "Edge"})
	proj, _ := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "edge-proj", DisplayName: "EdgeProj"})
	env, _ := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, port.CreateEnvironmentParams{Slug: "edge-env"})
	audit := &spyAuditSink{}
	polStore, err := engine.NewStore(engine.StoreConfig{DB: st.RawDB(), Audit: audit, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}

	// Org guardrail policy FAIL_CLOSED.
	orgPol, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:       org.ID,
		ScopeType:   snapshot.PolicyScopeOrg,
		ScopeID:     org.ID,
		PolicyType:  snapshot.PolicyTypeGuardrail,
		FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON:   `{"allowed_adapters":["openai-chat"]}`,
		Enabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// ENV scoped policy infers parent project_id from environments table + rejects FAIL_OPEN guardrail relaxation and logs audit event.
	envBad, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:       org.ID,
		ScopeType:   snapshot.PolicyScopeEnv,
		ScopeID:     env.ID,
		PolicyType:  snapshot.PolicyTypeGuardrail,
		FailureMode: snapshot.PolicyFailureOpen,
		RulesJSON:   `{"allowed_adapters":["openai-chat"]}`,
		Enabled:     true,
	})
	if !errors.Is(err, engine.ErrPolicyRelaxationForbidden) || envBad != nil {
		t.Fatalf("expected ENV guardrail FAIL_OPEN relaxation error, got %v", err)
	}
	envPol, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:       org.ID,
		ScopeType:   snapshot.PolicyScopeEnv,
		ScopeID:     env.ID,
		PolicyType:  snapshot.PolicyTypeGuardrail,
		FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON:   `{"allowed_adapters":["openai-chat"]}`,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("expected valid ENV guardrail policy, got %v", err)
	}

	// UpdatePolicy edge cases: ErrNotFound, invalid JSON, ancestor relaxation logged via AuditSink.
	badJSON := `{"not_a_known_field": 1}`
	if _, err := polStore.UpdatePolicy(ctx, org.ID, "pol_missing", 1, engine.UpdatePolicyParams{RulesJSON: &badJSON}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := polStore.UpdatePolicy(ctx, org.ID, envPol.ID, envPol.Version, engine.UpdatePolicyParams{RulesJSON: &badJSON}); !errors.Is(err, engine.ErrInvalidPolicyInput) {
		t.Fatalf("expected ErrInvalidPolicyInput on bad update JSON, got %v", err)
	}
	relaxAdapters := `{"allowed_adapters":["*"]}`
	if _, err := polStore.UpdatePolicy(ctx, org.ID, envPol.ID, envPol.Version, engine.UpdatePolicyParams{RulesJSON: &relaxAdapters}); !errors.Is(err, engine.ErrPolicyRelaxationForbidden) {
		t.Fatalf("expected ErrPolicyRelaxationForbidden on ENV widening adapters, got %v", err)
	}

	// Verify violation audit events were recorded!
	if len(audit.events) < 2 {
		t.Fatalf("expected >= 2 audit events for blocked relaxations, got %d", len(audit.events))
	}

	// Evaluator.Evaluate happy path and nil/corrupt snapshot fail-closed.
	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	eval := engine.NewEvaluator()
	if dec := eval.Evaluate(nil, nil, nil, engine.EvalRequest{ModelSlug: "m"}); dec.Allowed || dec.DenialCode != engine.DenialCodeEngineUnavailable {
		t.Fatalf("expected nil snapshot fail-closed 503, got %+v", dec)
	}
	okDec := eval.Evaluate(mgr.Current(), &snapshot.EffectiveConfig{OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID}, nil, engine.EvalRequest{
		OrgID:       org.ID,
		ProjectID:   proj.ID,
		EnvID:       env.ID,
		ModelSlug:   "gpt-4o",
		AdapterSlug: "openai-chat",
	})
	if !okDec.Allowed {
		t.Fatalf("expected Evaluate happy path allowed, got %+v", okDec)
	}

	// Corrupt ancestor policy triggers ErrPolicyEngineUnavailable during child policy creation.
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE policies SET rules_json = '{"bad_ancestor":1}' WHERE id = ?`, orgPol.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID:      org.ID,
		ScopeType:  snapshot.PolicyScopeProject,
		ScopeID:    proj.ID,
		PolicyType: snapshot.PolicyTypeModelAllowlist,
		RulesJSON:  `{"allowed_models":["gpt-4o"]}`,
		Enabled:    true,
	}); !errors.Is(err, engine.ErrPolicyEngineUnavailable) {
		t.Fatalf("expected ErrPolicyEngineUnavailable on corrupt ancestor row, got %v", err)
	}
}

func BenchmarkEvaluateEffective(b *testing.B) {
	eff := engine.MergeLayers(
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels:      true,
				AllowedModels:         []string{"gpt-4o", "claude-sonnet-4"},
				HasAllowedDataRegions: true,
				AllowedDataRegions:    []string{"EU-CENTRAL-1", "EU-WEST-1"},
				MinPluginTrust:        engine.MinTrustOfficialOnly,
				MaxContextTokens:      i64(32768),
				MaxRequestBodyBytes:   i64(1 << 20),
			},
		},
		engine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeProject,
			Rules: snapshot.PolicyRulesView{
				HasAllowedDataRegions: true,
				AllowedDataRegions:    []string{"EU-CENTRAL-1"},
			},
		},
	)
	req := engine.EvalRequest{
		OrgID:                "org_1",
		ProjectID:            "prj_1",
		EnvID:                "env_1",
		ModelSlug:            "gpt-4o",
		AdapterSlug:          "openai-chat",
		DataRegion:           "EU-CENTRAL-1",
		ExtensionTrustTier:   "official",
		CapabilityAssurance:  "VERIFIED",
		RequestBodyBytes:     1024,
		EstimatedTotalTokens: 512,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := engine.EvaluateEffective(eff, req)
		if !dec.Allowed {
			b.Fatal("unexpected denial in benchmark")
		}
	}
}

func TestTP0019_ReviewAndSecurityHardening(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000500000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	// 1. TargetEvaluation with empty DataRegion fails closed under restricted policy.
	effEU := engine.MergeLayers(engine.ScopedLayer{
		ScopeType: snapshot.PolicyScopeOrg,
		Rules: snapshot.PolicyRulesView{
			HasAllowedDataRegions: true,
			AllowedDataRegions:    []string{"EU-CENTRAL-1"},
		},
	})
	if dec := engine.EvaluateEffective(effEU, engine.EvalRequest{
		ModelSlug:        "gpt-4o",
		TargetEvaluation: true,
	}); dec.Allowed || dec.DenialCode != engine.DenialCodeRegionDenied {
		t.Fatalf("expected empty DataRegion on TargetEvaluation to fail closed, got %+v", dec)
	}

	// 2. RequestedDataRegion constrains DataRegion equality even with no org residency policy or wildcard ["*"].
	effWild := engine.MergeLayers(engine.ScopedLayer{
		ScopeType: snapshot.PolicyScopeOrg,
		Rules: snapshot.PolicyRulesView{
			HasAllowedDataRegions: true,
			AllowedDataRegions:    []string{"*"},
		},
	})
	if dec := engine.EvaluateEffective(effWild, engine.EvalRequest{
		ModelSlug:           "gpt-4o",
		RequestedDataRegion: "EU-CENTRAL-1",
		DataRegion:          "US-EAST-1",
		TargetEvaluation:    true,
	}); dec.Allowed || dec.DenialCode != engine.DenialCodeRegionDenied {
		t.Fatalf("expected RequestedDataRegion mismatch to fail closed even under wildcard policy, got %+v", dec)
	}
	if dec := engine.EvaluateEffective(engine.EffectivePolicy{}, engine.EvalRequest{
		ModelSlug:           "gpt-4o",
		RequestedDataRegion: "EU-CENTRAL-1",
		DataRegion:          "",
		TargetEvaluation:    true,
	}); dec.Allowed || dec.DenialCode != engine.DenialCodeRegionDenied {
		t.Fatalf("expected RequestedDataRegion with empty DataRegion to fail closed, got %+v", dec)
	}

	// 3. DerivedEnterpriseSigned satisfies VERIFIED_OR_ENTERPRISE_SIGNED and ANY_APPROVED, not VERIFIED_ONLY.
	effEnt := engine.MergeLayers(engine.ScopedLayer{
		ScopeType: snapshot.PolicyScopeOrg,
		Rules:     snapshot.PolicyRulesView{MinPluginTrust: engine.MinTrustVerifiedOrEnterpriseSigned},
	})
	if dec := engine.EvaluateEffective(effEnt, engine.EvalRequest{
		ExtensionTrustTier:        "unverified_external",
		ExtensionEnterpriseSigned: true,
		TargetEvaluation:          true,
	}); !dec.Allowed {
		t.Fatalf("expected ExtensionEnterpriseSigned to satisfy VERIFIED_OR_ENTERPRISE_SIGNED, got %+v", dec)
	}
	effVer := engine.MergeLayers(engine.ScopedLayer{
		ScopeType: snapshot.PolicyScopeOrg,
		Rules:     snapshot.PolicyRulesView{MinPluginTrust: engine.MinTrustVerifiedOnly},
	})
	if dec := engine.EvaluateEffective(effVer, engine.EvalRequest{
		ExtensionTrustTier:        "unverified_external",
		ExtensionEnterpriseSigned: true,
		TargetEvaluation:          true,
	}); dec.Allowed || dec.DenialCode != engine.DenialCodeTrustDenied {
		t.Fatalf("expected ExtensionEnterpriseSigned without public verified to fail VERIFIED_ONLY, got %+v", dec)
	}

	// 4. Store scope_id validation, ancestor hint tamper rejection, and cross-policy_type FAIL_CLOSED isolation.
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "pol_hardening.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	polStore, err := engine.NewStore(engine.StoreConfig{DB: st.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}

	org1, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-a", DisplayName: "Org A"})
	if err != nil {
		t.Fatal(err)
	}
	org2, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-b", DisplayName: "Org B"})
	if err != nil {
		t.Fatal(err)
	}
	proj1, err := st.Tenancy().CreateProject(ctx, org1.ID, port.CreateProjectParams{Slug: "p-a", DisplayName: "P A"})
	if err != nil {
		t.Fatal(err)
	}
	proj2, err := st.Tenancy().CreateProject(ctx, org2.ID, port.CreateProjectParams{Slug: "p-b", DisplayName: "P B"})
	if err != nil {
		t.Fatal(err)
	}
	env1, err := st.Tenancy().CreateEnvironment(ctx, org1.ID, proj1.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}

	// Nonexistent or cross-org scope_id rejected cleanly.
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: org2.ID,
		PolicyType: snapshot.PolicyTypeRateLimit, RulesJSON: `{"rpm_limit":10}`, Enabled: true,
	}); !errors.Is(err, engine.ErrInvalidPolicyInput) {
		t.Fatalf("expected ErrInvalidPolicyInput on mismatched ORG scope_id, got %v", err)
	}
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeProject, ScopeID: proj2.ID,
		PolicyType: snapshot.PolicyTypeRateLimit, RulesJSON: `{"rpm_limit":10}`, Enabled: true,
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on cross-org PROJECT scope_id, got %v", err)
	}
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeEnv, ScopeID: env1.ID, AncestorProjID: proj2.ID,
		PolicyType: snapshot.PolicyTypeRateLimit, RulesJSON: `{"rpm_limit":10}`, Enabled: true,
	}); !errors.Is(err, engine.ErrInvalidPolicyInput) {
		t.Fatalf("expected ErrInvalidPolicyInput on conflicting AncestorProjID hint, got %v", err)
	}

	// Cross-policy_type FAIL_CLOSED isolation: Org RATE_LIMIT FAIL_CLOSED must NOT block Project GUARDRAIL FAIL_OPEN.
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: org1.ID,
		PolicyType: snapshot.PolicyTypeRateLimit, FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON: `{"rpm_limit":100}`, Enabled: true,
	}); err != nil {
		t.Fatalf("create org rate limit policy: %v", err)
	}
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeProject, ScopeID: proj1.ID,
		PolicyType: snapshot.PolicyTypeGuardrail, FailureMode: snapshot.PolicyFailureOpen,
		RulesJSON: `{"guardrail_hooks":[{"extension_id":"ext_1","failure_posture":"FAIL_OPEN"}]}`, Enabled: true,
	}); err != nil {
		t.Fatalf("expected child GUARDRAIL FAIL_OPEN to succeed when only ancestor RATE_LIMIT is FAIL_CLOSED, got %v", err)
	}

	// Once an ancestor GUARDRAIL is FAIL_CLOSED, child GUARDRAIL FAIL_OPEN must be rejected!
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: org1.ID,
		PolicyType: snapshot.PolicyTypeGuardrail, FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON: `{"guardrail_hooks":[{"extension_id":"ext_org","failure_posture":"FAIL_CLOSED"}]}`, Enabled: true,
	}); err != nil {
		t.Fatalf("create org guardrail policy: %v", err)
	}
	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeEnv, ScopeID: env1.ID,
		PolicyType: snapshot.PolicyTypeGuardrail, FailureMode: snapshot.PolicyFailureOpen,
		RulesJSON: `{"guardrail_hooks":[{"extension_id":"ext_2","failure_posture":"FAIL_OPEN"}]}`, Enabled: true,
	}); !errors.Is(err, engine.ErrPolicyRelaxationForbidden) {
		t.Fatalf("expected child GUARDRAIL FAIL_OPEN rejected under FAIL_CLOSED ancestor GUARDRAIL, got %v", err)
	}

	// UpdatePolicy rejects empty/invalid FailureMode cleanly with ErrInvalidPolicyInput.
	createdProjPol, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeProject, ScopeID: proj1.ID,
		PolicyType: snapshot.PolicyTypeRateLimit, FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON: `{"rpm_limit":50}`, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	emptyFM := ""
	if _, err := polStore.UpdatePolicy(ctx, org1.ID, createdProjPol.ID, createdProjPol.Version, engine.UpdatePolicyParams{
		FailureMode: &emptyFM,
	}); !errors.Is(err, engine.ErrInvalidPolicyInput) {
		t.Fatalf("expected ErrInvalidPolicyInput for empty UpdatePolicy FailureMode, got %v", err)
	}

	// Real SQLite AuditSink persists policy.relaxation_blocked with non-empty ID & created_at_ms.
	if _, err := st.RawDB().ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS audit_events (
			id            VARCHAR(40) NOT NULL CHECK (length(id) > 0),
			created_at_ms BIGINT NOT NULL CHECK (created_at_ms > 0),
			org_id        VARCHAR(40),
			chain_key     VARCHAR(40) NOT NULL,
			seq_no        BIGINT NOT NULL CHECK (seq_no >= 1),
			actor_kind    VARCHAR(24) NOT NULL,
			actor_id      VARCHAR(64) NOT NULL,
			entry_point   VARCHAR(24) NOT NULL,
			event_type    VARCHAR(64) NOT NULL,
			target_type   VARCHAR(32) NOT NULL,
			target_id     VARCHAR(64) NOT NULL,
			request_id    VARCHAR(40),
			metadata_json TEXT NOT NULL,
			prev_hash     CHAR(64) NOT NULL,
			entry_hash    CHAR(64) NOT NULL,
			PRIMARY KEY (chain_key, seq_no, created_at_ms),
			UNIQUE (id, created_at_ms)
		)`); err != nil {
		t.Fatalf("create audit_events table: %v", err)
	}
	realSink := &sqlitePolicyAuditSink{db: st.RawDB()}
	realAuditStore, err := engine.NewStore(engine.StoreConfig{
		DB:    st.RawDB(),
		Audit: realSink,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := realAuditStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeEnv, ScopeID: env1.ID,
		PolicyType: snapshot.PolicyTypeGuardrail, FailureMode: snapshot.PolicyFailureOpen,
		RulesJSON: `{"guardrail_hooks":[{"extension_id":"ext_3","failure_posture":"FAIL_OPEN"}]}`, Enabled: true,
	}); !errors.Is(err, engine.ErrPolicyRelaxationForbidden) {
		t.Fatalf("expected ErrPolicyRelaxationForbidden with real AuditSink, got %v", err)
	}
	var auditCount int
	if err := st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE org_id = ? AND event_type = 'policy.relaxation_blocked' AND id LIKE 'aud_%' AND created_at_ms > 0`, org1.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 persisted policy.relaxation_blocked row in audit_events, got %d", auditCount)
	}

	// Same-tier baseline enforcement: PROJECT policy cannot broaden projects.enforced_region;
	// ENV policy cannot relax environments.require_tested_caps.
	euReg := "EU-CENTRAL-1"
	projEU, err := st.Tenancy().CreateProject(ctx, org1.ID, port.CreateProjectParams{
		Slug:           "eu-strict",
		DisplayName:    "EU Strict",
		EnforcedRegion: &euReg,
	})
	if err != nil {
		t.Fatal(err)
	}
	envTested, err := st.Tenancy().CreateEnvironment(ctx, org1.ID, projEU.ID, port.CreateEnvironmentParams{
		Slug:              "prod-tested",
		RequireTestedCaps: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := realAuditStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeProject, ScopeID: projEU.ID,
		PolicyType: snapshot.PolicyTypeDataResidency, FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON: `{"allowed_data_regions":["US-EAST-1"]}`, Enabled: true,
	}); !errors.Is(err, engine.ErrPolicyRelaxationForbidden) {
		t.Fatalf("expected PROJECT policy violating projects.enforced_region to fail with ErrPolicyRelaxationForbidden, got %v", err)
	}
	if _, err := realAuditStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org1.ID, ScopeType: snapshot.PolicyScopeEnv, ScopeID: envTested.ID,
		PolicyType: snapshot.PolicyTypeModelAllowlist, FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON: `{"min_capability_assurance":"DECLARED"}`, Enabled: true,
	}); !errors.Is(err, engine.ErrPolicyRelaxationForbidden) {
		t.Fatalf("expected ENV policy relaxing environments.require_tested_caps to fail with ErrPolicyRelaxationForbidden, got %v", err)
	}

	// Disabling an active child policy succeeds cleanly even if its rules became stale.
	disable := false
	if disabled, err := realAuditStore.UpdatePolicy(ctx, org1.ID, createdProjPol.ID, createdProjPol.Version, engine.UpdatePolicyParams{
		Enabled: &disable,
	}); err != nil || disabled.Enabled {
		t.Fatalf("expected clean disable of project policy, got rec=%+v err=%v", disabled, err)
	}
}

type sqlitePolicyAuditSink struct {
	mu  sync.Mutex
	db  *sql.DB
	seq int64
}

func (s *sqlitePolicyAuditSink) Append(ctx context.Context, ev port.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	chainKey := "SYSTEM"
	var orgArg any
	if ev.OrgID != nil && *ev.OrgID != "" {
		chainKey = *ev.OrgID
		orgArg = *ev.OrgID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, created_at_ms, org_id, chain_key, seq_no,
			actor_kind, actor_id, entry_point, event_type,
			target_type, target_id, request_id, metadata_json,
			prev_hash, entry_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.CreatedAtMs, orgArg, chainKey, s.seq,
		ev.ActorKind, ev.ActorID, ev.EntryPoint, ev.EventType,
		ev.TargetType, ev.TargetID, ev.RequestID, ev.MetadataJSON,
		strings.Repeat("0", 64), strings.Repeat("a", 64),
	)
	return err
}

type togglingTxAuditSink struct {
	mu   sync.Mutex
	fail bool
}

func (t *togglingTxAuditSink) setFail(v bool) {
	t.mu.Lock()
	t.fail = v
	t.mu.Unlock()
}

func (t *togglingTxAuditSink) Append(_ context.Context, _ port.AuditEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fail {
		return errors.New("injected policy audit failure")
	}
	return nil
}

func (t *togglingTxAuditSink) AppendTx(_ context.Context, _ *sql.Tx, _ port.AuditEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fail {
		return errors.New("injected policy tx audit failure")
	}
	return nil
}

func TestNEG2TransactionalAuditRollbackPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000400000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "policy_neg2.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	sink := &togglingTxAuditSink{fail: true}
	polStore, err := engine.NewStore(engine.StoreConfig{DB: st.RawDB(), Audit: sink, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "pol-neg2", DisplayName: "Policy NEG2"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: org.ID,
		PolicyType: snapshot.PolicyTypeRateLimit, FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON: `{"rpm_limit":10}`, Enabled: true,
	}); err == nil {
		t.Fatalf("expected CreatePolicy to roll back when AppendTx fails")
	}
	var cnt int
	if err := st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM policies WHERE org_id = ?`, org.ID).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("expected 0 policies after rollback, got %d", cnt)
	}

	sink.setFail(false)
	created, err := polStore.CreatePolicy(ctx, engine.CreatePolicyParams{
		OrgID: org.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: org.ID,
		PolicyType: snapshot.PolicyTypeRateLimit, FailureMode: snapshot.PolicyFailureClosed,
		RulesJSON: `{"rpm_limit":10}`, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	sink.setFail(true)
	newRules := `{"rpm_limit":5}`
	if _, err := polStore.UpdatePolicy(ctx, org.ID, created.ID, created.Version, engine.UpdatePolicyParams{RulesJSON: &newRules}); err == nil {
		t.Fatalf("expected UpdatePolicy to roll back when AppendTx fails")
	}
	got, err := polStore.GetPolicy(ctx, org.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.RulesJSON != `{"rpm_limit":10}` {
		t.Fatalf("expected UpdatePolicy rollback preserving v1 rules, got %+v", got)
	}
}

