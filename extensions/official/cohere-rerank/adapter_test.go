// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package coherererank_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) { os.Exit(netguard.MustRunHermeticMain(m)) }

func TestCohereRerankRequestMappingAndStrictResponse(t *testing.T) {
	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := declarative.ParseDeclarativeManifestYAML(raw)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	request, err := runtime.TransformRequest(context.Background(), port.TransformRequestInput{
		RequestID: "rerank-contract", ConfigJSON: `{"endpoint_base_url":"https://api.cohere.com","_tanpopo_modality":"rerank"}`,
		NormalizedRequestJSON: []byte(`{"model":"rerank-v3.5","query":"find it","documents":["a","b"],"top_n":1}`),
		SecretSlotRefs:        map[string]string{"api_key": "slot_ref:api_key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.TargetURL != "https://api.cohere.com/v2/rerank" || len(request.CredentialInjections) != 1 || request.CredentialInjections[0].ValuePrefix != "Bearer " {
		t.Fatalf("unexpected rerank upstream request: %+v", request)
	}
	response, err := runtime.TransformResponse(context.Background(), port.TransformResponseInput{
		RequestID: "rerank-contract", HTTPStatus: 200, Modality: "RERANK",
		RawResponseBody: []byte(`{"id":"rr-9","results":[{"index":1,"relevance_score":0.9}],"billed_units":{"search_units":2}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Usage == nil || response.Usage.MediaUsage == nil || response.Usage.MediaUsage.RerankSearchUnits != 2 {
		t.Fatalf("exact rerank usage not extracted: %+v", response.Usage)
	}
}

func TestCohereRerankCassetteAndDeterministicPackage(t *testing.T) {
	report, err := testharness.TestExtension(context.Background(), testharness.TestOptions{Dir: "."})
	if err != nil {
		t.Fatalf("TestExtension: %v (report=%+v)", err, report)
	}
	if report.Status != "PASSED" || report.CasesPassed != 8 || report.CasesFailed != 0 || report.CapabilityBadges["rerank"] != "TESTED" {
		t.Fatalf("unexpected rerank cassette report: %+v", report)
	}
	firstPath, secondPath := filepath.Join(t.TempDir(), "a.tpkg"), filepath.Join(t.TempDir(), "b.tpkg")
	first, err := testharness.PackExtension(context.Background(), ".", firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := testharness.PackExtension(context.Background(), ".", secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 == "" || first.SHA256 != second.SHA256 {
		t.Fatalf("package not deterministic: %q != %q", first.SHA256, second.SHA256)
	}
}
