// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"encoding/json"
	"net/http"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
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
		cards = append(cards, modelCardJSON{
			ID:            m.ID,
			Object:        "model",
			Created:       m.CreatedSec,
			OwnedBy:       m.OwnedBy,
			ContextWindow: m.ContextWindow,
			MaxOutputTok:  m.MaxOutputTokens,
			AssuranceTier: m.AssuranceTier,
			Capabilities:  m.Capabilities,
			XTanpopoCaps: xTanpopoCapabilitiesMetadata{
				ModalityKind:    m.ModalityKind,
				AssuranceTier:   m.AssuranceTier,
				ContextWindow:   m.ContextWindow,
				MaxOutputTokens: m.MaxOutputTokens,
				Supported:       m.Capabilities,
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
