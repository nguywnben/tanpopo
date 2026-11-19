// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package azureopenai_test

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

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestAzureDeploymentRequestContract(t *testing.T) {
	manifestPath := filepath.Join("manifest.yaml")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := declarative.ParseDeclarativeManifestYAML(manifest)
	if err != nil {
		t.Fatalf("parse Azure adapter manifest: %v", err)
	}
	probe, err := runtime.BuildValidationProbe(context.Background(), port.ValidationProbeInput{
		RequestID:       "azure-probe-contract",
		ConfigJSON:      `{"endpoint_base_url":"https://tanpopo-resource.openai.azure.com"}`,
		SecretSlotNames: []string{"api_key"},
	})
	if err != nil {
		t.Fatalf("BuildValidationProbe: %v", err)
	}
	if probe.Method != "GET" || probe.TargetURL != "https://tanpopo-resource.openai.azure.com/openai/models?api-version=2024-10-21" {
		t.Fatalf("unexpected Azure validation probe: %s %q", probe.Method, probe.TargetURL)
	}
	if len(probe.CredentialInjections) != 1 || probe.CredentialInjections[0].FieldName != "api-key" {
		t.Fatalf("unexpected probe credential injection: %+v", probe.CredentialInjections)
	}
	got, err := runtime.TransformRequest(context.Background(), port.TransformRequestInput{
		RequestID:             "azure-contract",
		ConfigJSON:            `{"endpoint_base_url":"https://tanpopo-resource.openai.azure.com"}`,
		NormalizedRequestJSON: []byte(`{"model":"gpt-4o-prod","messages":[{"role":"user","content":"hello"}],"stream":true}`),
		SecretSlotRefs:        map[string]string{"api_key": "slot_ref:api_key"},
	})
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	wantURL := "https://tanpopo-resource.openai.azure.com/openai/deployments/gpt-4o-prod/chat/completions?api-version=2024-10-21"
	if got.Method != "POST" || got.TargetURL != wantURL {
		t.Fatalf("request = %s %q, want POST %q", got.Method, got.TargetURL, wantURL)
	}
	if len(got.CredentialInjections) != 1 {
		t.Fatalf("credential injections = %+v, want one blind injection", got.CredentialInjections)
	}
	injection := got.CredentialInjections[0]
	if injection.SecretSlotName != "api_key" || injection.TargetLocation != port.CredentialTargetLocationHeader || injection.FieldName != "api-key" || injection.ValuePrefix != "" {
		t.Fatalf("unexpected Azure credential injection: %+v", injection)
	}
	if got.SafeHeaders["api-key"] != "" {
		t.Fatalf("credential leaked into safe headers: %+v", got.SafeHeaders)
	}
	_, err = runtime.TransformRequest(context.Background(), port.TransformRequestInput{
		RequestID:             "azure-other-resource-denied",
		ConfigJSON:            `{"endpoint_base_url":"https://customer-resource.openai.azure.com"}`,
		NormalizedRequestJSON: []byte(`{"model":"gpt-4o-prod","messages":[{"role":"user","content":"hello"}]}`),
		SecretSlotRefs:        map[string]string{"api_key": "slot_ref:api_key"},
	})
	if err == nil {
		t.Fatal("caller-configured Azure resource outside the exact manifest host was unexpectedly allowed")
	}

	report, err := testharness.TestExtension(context.Background(), testharness.TestOptions{Dir: "."})
	if err != nil {
		t.Fatalf("execute package cassette contracts: %v", err)
	}
	if report.Status != "PASSED" || report.CasesFailed != 0 || report.CasesPassed != 8 {
		t.Fatalf("unexpected cassette report: %+v", report)
	}
}
