// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
)

type modelListResponse struct {
	Object string          `json:"object"`
	Data   []modelCardJSON `json:"data"`
}

type modelCardJSON struct {
	ID            string                          `json:"id"`
	Object        string                          `json:"object"`
	Created       int64                           `json:"created"`
	OwnedBy       string                          `json:"owned_by"`
	ContextWindow int64                           `json:"context_window,omitempty"`
	MaxOutputTok  int64                           `json:"max_output_tokens,omitempty"`
	AssuranceTier string                          `json:"assurance_tier,omitempty"`
	Capabilities  snapshot.ModelCapabilityProfile `json:"capabilities"`
	XTanpopoCaps  xTanpopoCapabilitiesMetadata    `json:"x_tanpopo_capabilities"`
}

type xTanpopoCapabilitiesMetadata struct {
	ModalityKind    string                          `json:"modality_kind,omitempty"`
	AssuranceTier   string                          `json:"assurance_tier"`
	ContextWindow   int64                           `json:"context_window,omitempty"`
	MaxOutputTokens int64                           `json:"max_output_tokens,omitempty"`
	Supported       snapshot.ModelCapabilityProfile `json:"supported"`
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	meta := ingressFromContext(r.Context())
	if r.Method != http.MethodGet {
		writeCanonicalError(w, meta.RequestID, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "GET is required on /v1/models.", false)
		return
	}

	_, cancelStream, match, handleID, ok := s.authenticateProviderRequest(w, r, meta.RequestID, "/v1/models")
	if !ok {
		return
	}
	defer cancelStream()
	defer s.snapshots.Streams().Unregister(match.Key.ID, handleID)

	snap := s.snapshots.Current()
	if snap == nil {
		writeCanonicalError(w, meta.RequestID, http.StatusServiceUnavailable, "snapshot_not_ready", "api_error", "Configuration snapshot unavailable.", true)
		return
	}

	eff, pErr := s.policyEval.ResolveEffective(snap, match.Config, match.Key)
	if pErr != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusServiceUnavailable, policyengine.DenialCodeEngineUnavailable, "api_error", "Policy engine snapshot is corrupted or unavailable; failing closed.", true)
		return
	}
	reqRegion, regionOK := resolveRequestedDataRegion(r)
	if !regionOK {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Conflicting X-Tanpopo-Data-Region header values.", false)
		return
	}
	if !s.acquirePreIdempotencyRPM(w, r, meta.RequestID, match, &preAdmissionDecision{effPolicy: eff, reqRegion: reqRegion}) {
		return
	}

	requireTested := match.Config != nil && match.Config.RequireTestedCaps
	authorized := snap.ListAuthorizedLogicalModels(
		match.Key.OrgID,
		match.Key.ProjectID,
		match.Key.EnvID,
		match.Key.AllowedModels,
		requireTested,
	)

	cards := make([]modelCardJSON, 0, len(authorized))
	for _, m := range authorized {
		compliantCand := firstPolicyCompliantCandidate(snap, match, eff, reqRegion, m.ID)
		if compliantCand == nil {
			continue
		}
		ownedBy := m.OwnedBy
		if compliantCand.Adapter != nil && strings.TrimSpace(compliantCand.Adapter.AdapterSlug) != "" {
			ownedBy = strings.TrimSpace(compliantCand.Adapter.AdapterSlug)
		}
		assuranceTier := m.AssuranceTier
		if compliantCand.Deployment != nil && strings.TrimSpace(compliantCand.Deployment.AssuranceLevel) != "" {
			assuranceTier = strings.ToUpper(strings.TrimSpace(compliantCand.Deployment.AssuranceLevel))
		}
		profile := snapshot.BuildModelCapabilityProfile(compliantCand, requireTested)
		cards = append(cards, modelCardJSON{
			ID:            m.ID,
			Object:        "model",
			Created:       m.CreatedSec,
			OwnedBy:       ownedBy,
			ContextWindow: m.ContextWindow,
			MaxOutputTok:  m.MaxOutputTokens,
			AssuranceTier: assuranceTier,
			Capabilities:  profile,
			XTanpopoCaps: xTanpopoCapabilitiesMetadata{
				ModalityKind:    m.ModalityKind,
				AssuranceTier:   assuranceTier,
				ContextWindow:   m.ContextWindow,
				MaxOutputTokens: m.MaxOutputTokens,
				Supported:       profile,
			},
		})
	}

	body, err := json.Marshal(modelListResponse{
		Object: "list",
		Data:   cards,
	})
	if err != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusInternalServerError, "internal_error", "api_error", "Failed to encode authorized models response.", true)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Tanpopo-Request-Id", meta.RequestID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func firstPolicyCompliantCandidate(
	snap *snapshot.ConfigSnapshot,
	match snapshot.KeyMatchResult,
	eff policyengine.EffectivePolicy,
	reqRegion string,
	modelSlug string,
) *snapshot.ResolvedTarget {
	if !policyengine.ModelPermitted(eff, modelSlug) {
		return nil
	}
	candidates := snap.ListCandidateTargets(match.Key.OrgID, modelSlug)
	for _, cand := range candidates {
		if cand == nil || cand.Deployment == nil || cand.Deployment.State != "ACTIVE_HEALTHY" {
			continue
		}
		if cand.Connection == nil || cand.Connection.State != snapshot.ConnectionStateActive || !cand.Connection.VisibleToProject(match.Key.OrgID, match.Key.ProjectID) {
			continue
		}
		if cand.Adapter == nil || strings.EqualFold(cand.Adapter.ExtensionState, installer.StateQuarantined) {
			continue
		}
		dec := policyengine.EvaluateEffective(eff, policyengine.EvalRequest{
			OrgID:                     match.Key.OrgID,
			ProjectID:                 match.Key.ProjectID,
			EnvID:                     match.Key.EnvID,
			ModelSlug:                 modelSlug,
			AdapterSlug:               cand.Adapter.AdapterSlug,
			DataRegion:                cand.Deployment.DataRegion,
			RequestedDataRegion:       reqRegion,
			ExtensionTrustTier:        cand.Connection.DerivedPublicTrust,
			ExtensionEnterpriseSigned: cand.Connection.DerivedEnterpriseSigned,
			CapabilityAssurance:       cand.Deployment.AssuranceLevel,
			TargetEvaluation:          true,
		})
		if dec.Allowed {
			return cand
		}
	}
	return nil
}

