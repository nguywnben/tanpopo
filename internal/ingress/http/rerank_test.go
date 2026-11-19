// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"encoding/json"
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/port"
)

func TestParseStrictRerankRequest(t *testing.T) {
	req, err := parseStrictRerankRequest([]byte(`{"model":"ranker","query":"hello","documents":["one",{"id":"d2","text":"two"}],"top_n":1,"return_documents":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "ranker" || len(req.Documents) != 2 || req.Documents[1].ID != "d2" || req.TopN == nil || *req.TopN != 1 || !req.ReturnDocuments {
		t.Fatalf("unexpected parsed request: %+v", req)
	}
	for _, raw := range []string{
		`{"model":"ranker","query":"q","documents":[]}`,
		`{"model":"ranker","query":"q","documents":[""]}`,
		`{"model":"ranker","query":"q","documents":["x"],"top_n":2}`,
		`{"model":"ranker","query":"q","documents":["x"],"extra":true}`,
		`{"model":"ranker","query":"q","documents":[{"text":"x","unknown":1}]}`,
		`{"model":"ranker","query":"q","documents":["x"],"return_documents":"yes"}`,
		`{"model":"ranker","query":"q","documents":["x"],"model":"other"}`,
	} {
		if _, err := parseStrictRerankRequest([]byte(raw)); err == nil {
			t.Errorf("invalid rerank input accepted: %s", raw)
		}
	}
}

func TestFinalizeRerankJSONValidatesAndReturnsRequestedDocuments(t *testing.T) {
	req, err := parseStrictRerankRequest([]byte(`{"model":"ranker","query":"q","documents":["first",{"text":"second"}],"top_n":1,"return_documents":true}`))
	if err != nil {
		t.Fatal(err)
	}
	response, err := finalizeRerankJSON(req.Model, req, port.NormalizedInferenceResponse{NormalizedResponseJSON: []byte(`{"id":"rr-1","results":[{"index":1,"relevance_score":0.91}]}`), UpstreamModelID: "ranker-v1"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model   string `json:"model"`
		Results []struct {
			Index    int            `json:"index"`
			Document map[string]string `json:"document"`
		} `json:"results"`
	}
	if err := json.Unmarshal(response, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "ranker-v1" || len(got.Results) != 1 || got.Results[0].Index != 1 || got.Results[0].Document["text"] != "second" {
		t.Fatalf("unexpected normalized rerank response: %s", response)
	}
	for _, raw := range []string{
		`{"id":"rr","results":[{"index":3,"relevance_score":0.5}]}`,
		`{"id":"rr","results":[{"index":1,"relevance_score":0.5},{"index":1,"relevance_score":0.4}]}`,
		`{"id":"rr","results":[{"index":1,"relevance_score":1e999}]}`,
		`{"id":"rr","results":[{"index":1,"relevance_score":0.5},{"index":0,"relevance_score":0.4}]}`,
	} {
		if _, err := finalizeRerankJSON(req.Model, req, port.NormalizedInferenceResponse{NormalizedResponseJSON: []byte(raw)}); err == nil {
			t.Errorf("invalid normalized response accepted: %s", raw)
		}
	}
}
