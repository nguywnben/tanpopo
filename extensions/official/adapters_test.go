// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package official_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	"github.com/nguywnben/tanpopo/internal/ingress/native"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestOfficialAdaptersContractAndPackaging(t *testing.T) {
	ctx := context.Background()
	tpkgTmp := t.TempDir()

	adapters := []struct {
		dir      string
		id       string
		wantTier string
	}{
		{dir: "openai", id: "@tanpopo/openai", wantTier: installer.ExecutionTierDeclarativeT1},
		{dir: "anthropic", id: "@tanpopo/anthropic", wantTier: installer.ExecutionTierWasmSandboxT2},
		{dir: "gemini", id: "@tanpopo/gemini", wantTier: installer.ExecutionTierWasmSandboxT2},
		{dir: "openai-compatible", id: "@tanpopo/openai-compatible", wantTier: installer.ExecutionTierDeclarativeT1},
		{dir: "ollama", id: "@tanpopo/ollama", wantTier: installer.ExecutionTierDeclarativeT1},
		{dir: "azure-openai", id: "@tanpopo/azure-openai", wantTier: installer.ExecutionTierDeclarativeT1},
	}

	for _, tc := range adapters {
		tc := tc
		t.Run(tc.dir, func(t *testing.T) {
			valRep, vm, _, err := testharness.ValidateExtensionDir(ctx, tc.dir)
			if err != nil {
				t.Fatalf("ValidateExtensionDir(%s): %v", tc.dir, err)
			}
			if valRep.ExtensionID != tc.id {
				t.Fatalf("ExtensionID = %q, want %q", valRep.ExtensionID, tc.id)
			}
			if vm.ExecutionTier != tc.wantTier {
				t.Fatalf("ExecutionTier = %q, want %q", vm.ExecutionTier, tc.wantTier)
			}
			if vm.Manifest.Metadata.TrustTier != "official" {
				t.Fatalf("TrustTier = %q, want official", vm.Manifest.Metadata.TrustTier)
			}

			testRep, err := testharness.TestExtension(ctx, testharness.TestOptions{Dir: tc.dir})
			if err != nil {
				t.Fatalf("TestExtension(%s): %v", tc.dir, err)
			}
			if testRep.Status != "PASSED" || testRep.CasesFailed != 0 || testRep.CasesPassed < 8 {
				t.Fatalf("unexpected TestExtension report for %s: %+v", tc.dir, testRep)
			}
			for _, capName := range []string{"chat_completion", "chat_stream", "tool_calling", "error_normalization", "native_passthrough"} {
				if testRep.CapabilityBadges[capName] != "TESTED" {
					t.Fatalf("%s missing TESTED badge for %s (got %q)", tc.dir, capName, testRep.CapabilityBadges[capName])
				}
			}

			out1 := filepath.Join(tpkgTmp, tc.dir+"-1.tpkg")
			out2 := filepath.Join(tpkgTmp, tc.dir+"-2.tpkg")
			pack1, err := testharness.PackExtension(ctx, tc.dir, out1)
			if err != nil {
				t.Fatalf("PackExtension 1 (%s): %v", tc.dir, err)
			}
			pack2, err := testharness.PackExtension(ctx, tc.dir, out2)
			if err != nil {
				t.Fatalf("PackExtension 2 (%s): %v", tc.dir, err)
			}
			if pack1.SHA256 == "" || pack1.SHA256 != pack2.SHA256 {
				t.Fatalf("%s .tpkg pack not deterministic: %q vs %q", tc.dir, pack1.SHA256, pack2.SHA256)
			}
			if pack1.SizeBytes == 0 || pack1.EntryCount < 10 {
				t.Fatalf("%s .tpkg unexpectedly small: %+v", tc.dir, pack1)
			}
		})
	}
}

func loadWasmAdapterRuntime(t *testing.T, dir string) (*wasm.Runtime, func()) {
	t.Helper()
	ctx := context.Background()
	_, vm, wasmBytes, err := testharness.ValidateExtensionDir(ctx, dir)
	if err != nil {
		t.Fatalf("ValidateExtensionDir(%s): %v", dir, err)
	}
	var slots []string
	for _, s := range vm.Manifest.SecretSlots {
		slots = append(slots, s.Slot)
	}
	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:           wasmBytes,
		InstallationID:      "test:" + vm.Manifest.Metadata.ID,
		PoolSize:            2,
		MemoryLimitPages:    wasm.DefaultMemoryLimitPages,
		ABIMode:             wasm.ABIModeCanonicalEnvelope,
		AllowedHosts:        vm.Manifest.Permissions.NetworkEgress.AllowedHosts,
		AllowInsecureHTTP:   !vm.Manifest.Permissions.NetworkEgress.RequireTLS,
		ApprovedSecretSlots: slots,
	})
	if err != nil {
		t.Fatalf("wasm.NewRuntime(%s): %v", dir, err)
	}
	return rt, func() { _ = rt.Close(context.Background()) }
}

func TestOfficialAnthropicAndGeminiAllHooks(t *testing.T) {
	ctx := context.Background()

	t.Run("anthropic_hooks_and_blob_splice", func(t *testing.T) {
		rt, cleanup := loadWasmAdapterRuntime(t, "anthropic")
		defer cleanup()

		probe, err := rt.BuildValidationProbe(ctx, port.ValidationProbeInput{
			RequestID:       "req_anth_probe",
			ConfigJSON:      `{}`,
			SecretSlotNames: []string{"api_key"},
		})
		if err != nil {
			t.Fatalf("BuildValidationProbe: %v", err)
		}
		if probe.Method != "GET" || probe.TargetURL != "https://api.anthropic.com/v1/models" {
			t.Fatalf("unexpected probe: %+v", probe)
		}

		verdict, err := rt.VerifyValidationResult(ctx, port.ValidationResultInput{
			RequestID:    "req_anth_probe",
			HTTPStatus:   200,
			ResponseBody: []byte(`{"data":[{"id":"claude-sonnet-4-5"}]}`),
		})
		if err != nil || !verdict.Valid || len(verdict.DetectedModelIDs) != 1 || verdict.DetectedModelIDs[0] != "claude-sonnet-4-5" {
			t.Fatalf("VerifyValidationResult unexpected: verdict=%+v err=%v", verdict, err)
		}

		blobURI := "blob_ref://sha256/0123456789abcdef"
		rawB64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
		rawData := "data:image/png;base64," + rawB64
		reqPayload := map[string]any{
			"model": "claude-sonnet-4-5",
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "image_url", "image_url": map[string]any{"url": blobURI}},
					},
				},
			},
		}
		reqBytes, _ := json.Marshal(reqPayload)
		spec, err := rt.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "req_anth_blob",
			ConfigJSON:            `{"endpoint_base_url":"https://api.anthropic.com/v1"}`,
			NormalizedRequestJSON: reqBytes,
			BlobRefs: []port.BlobRef{
				{BlobID: "b1", URI: blobURI, MimeType: "image/png", ByteLength: int64(len(rawData))},
			},
		})
		if err != nil {
			t.Fatalf("TransformRequest with blob_ref: %v", err)
		}
		if spec.TargetURL != "https://api.anthropic.com/v1/messages" {
			t.Fatalf("expected trimmed TargetURL without /v1/v1 duplication, got %q", spec.TargetURL)
		}
		if len(spec.OutboundBlobSplices) != 1 || spec.OutboundBlobSplices[0].URI != blobURI {
			t.Fatalf("expected OutboundBlobSplices to carry %s, got %+v", blobURI, spec.OutboundBlobSplices)
		}
		spliced, err := blob.SpliceHostBlobs(spec.BodyBytes, map[string][]byte{blobURI: []byte(rawData)}, 1<<20)
		if err != nil {
			t.Fatalf("SpliceHostBlobs: %v", err)
		}
		var anthReq struct {
			Messages []struct {
				Content []struct {
					Type   string `json:"type"`
					Source struct {
						Type      string `json:"type"`
						MediaType string `json:"media_type"`
						Data      string `json:"data"`
					} `json:"source"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(spliced, &anthReq); err != nil {
			t.Fatalf("unmarshal spliced Anthropic payload: %v (%s)", err, string(spliced))
		}
		if len(anthReq.Messages) != 1 || len(anthReq.Messages[0].Content) != 1 ||
			anthReq.Messages[0].Content[0].Type != "image" ||
			anthReq.Messages[0].Content[0].Source.Type != "base64" ||
			anthReq.Messages[0].Content[0].Source.MediaType != "image/png" ||
			anthReq.Messages[0].Content[0].Source.Data != rawB64 ||
			strings.Contains(string(spliced), "blob_ref://") {
			t.Fatalf("unexpected Anthropic spliced base64 payload: %s", string(spliced))
		}

		// End-to-end > 64 KiB OffloadEndpointMultimodalRequest -> TransformRequest -> SpliceHostBlobs
		hugeB64 := strings.Repeat("QUFB", 25000)
		hugeReq, _ := json.Marshal(map[string]any{
			"model": "claude-sonnet-4-5",
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + hugeB64}},
					},
				},
			},
		})
		offloadRes, err := blob.OffloadEndpointMultimodalRequest(ctx, "req_anth_huge", hugeReq, "claude-sonnet-4-5", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{}, "messages")
		if err != nil {
			t.Fatalf("OffloadEndpointMultimodalRequest: %v", err)
		}
		defer offloadRes.Scrub()
		hugeSpec, err := rt.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "req_anth_huge",
			ConfigJSON:            `{}`,
			NormalizedRequestJSON: offloadRes.NormalizedJSON,
			BlobRefs:              offloadRes.BlobRefs,
		})
		if err != nil {
			t.Fatalf("TransformRequest huge blob: %v", err)
		}
		hugeSpliced, err := blob.SpliceHostBlobs(hugeSpec.BodyBytes, offloadRes.HostBlobs, blob.DefaultMaxBlobBytes)
		if err != nil {
			t.Fatalf("SpliceHostBlobs huge blob: %v", err)
		}
		if !strings.Contains(string(hugeSpliced), `"data":"`+hugeB64+`"`) || strings.Contains(string(hugeSpliced), "blob_ref://") {
			t.Fatalf("expected valid huge raw base64 payload without blob_ref://")
		}

		// FMEA-01 / TP-BR-0503: premature SSE EOF without message_stop MUST NOT emit [DONE] or mark completed
		chunk1, err := rt.TransformStreamChunk(ctx, port.StreamChunkInput{
			RequestID:        "req_anth_trunc",
			RawUpstreamChunk: []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_trunc\",\"model\":\"claude-sonnet-4-5\",\"usage\":{\"input_tokens\":5}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n"),
			IsEOF:            false,
		})
		if err != nil {
			t.Fatalf("TransformStreamChunk(chunk1): %v", err)
		}
		eofChunk, err := rt.TransformStreamChunk(ctx, port.StreamChunkInput{
			RequestID:    "req_anth_trunc",
			SessionState: chunk1.NextSessionState,
			IsEOF:        true,
		})
		if err != nil {
			t.Fatalf("TransformStreamChunk(eof): %v", err)
		}
		if eofChunk.StreamCompleted {
			t.Fatalf("FMEA-01 violation: Anthropic premature EOF without message_stop must not report StreamCompleted=true")
		}
		for _, f := range eofChunk.NormalizedSSEFrames {
			if strings.Contains(string(f), "[DONE]") {
				t.Fatalf("FMEA-01 violation: Anthropic premature EOF must not synthesize data: [DONE]: %q", string(f))
			}
		}

		gv, err := rt.EvaluateGuardrail(ctx, port.GuardrailInput{
			RequestID:   "req_anth_guard",
			Stage:       port.GuardrailStagePreRequest,
			PayloadJSON: []byte(`{"ok":true}`),
		})
		if err != nil || !gv.Allowed {
			t.Fatalf("EvaluateGuardrail unexpected: %+v err=%v", gv, err)
		}
	})

	t.Run("gemini_hooks_and_blob_splice", func(t *testing.T) {
		rt, cleanup := loadWasmAdapterRuntime(t, "gemini")
		defer cleanup()

		probe, err := rt.BuildValidationProbe(ctx, port.ValidationProbeInput{
			RequestID:       "req_gem_probe",
			ConfigJSON:      `{"endpoint_base_url":"https://generativelanguage.googleapis.com/v1beta"}`,
			SecretSlotNames: []string{"api_key"},
		})
		if err != nil {
			t.Fatalf("BuildValidationProbe: %v", err)
		}
		if probe.TargetURL != "https://generativelanguage.googleapis.com/v1beta/models" {
			t.Fatalf("unexpected Gemini probe URL: %s", probe.TargetURL)
		}

		verdict, err := rt.VerifyValidationResult(ctx, port.ValidationResultInput{
			RequestID:    "req_gem_probe",
			HTTPStatus:   200,
			ResponseBody: []byte(`{"models":[{"name":"models/gemini-2.5-flash"}]}`),
		})
		if err != nil || !verdict.Valid || len(verdict.DetectedModelIDs) != 1 || verdict.DetectedModelIDs[0] != "gemini-2.5-flash" {
			t.Fatalf("VerifyValidationResult unexpected: verdict=%+v err=%v", verdict, err)
		}

		blobURI := "blob_ref://sha256/fedcba9876543210"
		rawB64 := "JVBERi0xLjQK"
		rawData := "data:application/pdf;base64," + rawB64
		reqBytes, _ := json.Marshal(map[string]any{
			"model": "gemini-2.5-flash",
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "input_file", "file_data": blobURI, "mime_type": "application/pdf"},
					},
				},
			},
		})
		spec, err := rt.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "req_gem_blob",
			ConfigJSON:            `{"endpoint_base_url":"https://generativelanguage.googleapis.com/v1beta"}`,
			NormalizedRequestJSON: reqBytes,
			BlobRefs: []port.BlobRef{
				{BlobID: "b2", URI: blobURI, MimeType: "application/pdf", ByteLength: int64(len(rawData))},
			},
		})
		if err != nil {
			t.Fatalf("TransformRequest with blob_ref: %v", err)
		}
		if spec.TargetURL != "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent" {
			t.Fatalf("expected trimmed Gemini TargetURL without /v1beta/v1beta, got %q", spec.TargetURL)
		}
		if len(spec.OutboundBlobSplices) != 1 {
			t.Fatalf("expected OutboundBlobSplices on Gemini PDF blob, got %+v", spec.OutboundBlobSplices)
		}
		spliced, err := blob.SpliceHostBlobs(spec.BodyBytes, map[string][]byte{blobURI: []byte(rawData)}, 1<<20)
		if err != nil {
			t.Fatalf("SpliceHostBlobs: %v", err)
		}
		var gemReq struct {
			Contents []struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"contents"`
		}
		if err := json.Unmarshal(spliced, &gemReq); err != nil {
			t.Fatalf("unmarshal spliced Gemini payload: %v (%s)", err, string(spliced))
		}
		if len(gemReq.Contents) != 1 || len(gemReq.Contents[0].Parts) != 1 ||
			gemReq.Contents[0].Parts[0].InlineData == nil ||
			gemReq.Contents[0].Parts[0].InlineData.MimeType != "application/pdf" ||
			gemReq.Contents[0].Parts[0].InlineData.Data != rawB64 ||
			strings.Contains(string(spliced), "blob_ref://") {
			t.Fatalf("unexpected Gemini spliced inlineData payload: %s", string(spliced))
		}

		// End-to-end > 64 KiB image offload + TransformRequest + SpliceHostBlobs
		hugeB64 := strings.Repeat("QUFB", 25000)
		hugeReq, _ := json.Marshal(map[string]any{
			"model": "gemini-2.5-flash",
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + hugeB64}},
					},
				},
			},
		})
		offloadRes, err := blob.OffloadEndpointMultimodalRequest(ctx, "req_gem_huge", hugeReq, "gemini-2.5-flash", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{}, "messages")
		if err != nil {
			t.Fatalf("OffloadEndpointMultimodalRequest: %v", err)
		}
		defer offloadRes.Scrub()
		hugeSpec, err := rt.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "req_gem_huge",
			ConfigJSON:            `{}`,
			NormalizedRequestJSON: offloadRes.NormalizedJSON,
			BlobRefs:              offloadRes.BlobRefs,
		})
		if err != nil {
			t.Fatalf("TransformRequest huge Gemini blob: %v", err)
		}
		hugeSpliced, err := blob.SpliceHostBlobs(hugeSpec.BodyBytes, offloadRes.HostBlobs, blob.DefaultMaxBlobBytes)
		if err != nil {
			t.Fatalf("SpliceHostBlobs huge Gemini blob: %v", err)
		}
		if !strings.Contains(string(hugeSpliced), `"inlineData":{"mimeType":"image/png","data":"`+hugeB64+`"}`) || strings.Contains(string(hugeSpliced), "blob_ref://") {
			t.Fatalf("expected valid huge Gemini inlineData base64 without blob_ref://")
		}

		// FMEA-01 / TP-BR-0503: premature SSE EOF without finishReason MUST NOT emit [DONE] or mark completed
		chunk1, err := rt.TransformStreamChunk(ctx, port.StreamChunkInput{
			RequestID:        "req_gem_trunc",
			RawUpstreamChunk: []byte("data: {\"responseId\":\"resp_t1\",\"modelVersion\":\"gemini-2.5-flash\",\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"partial\"}]}}]}\n\n"),
			IsEOF:            false,
		})
		if err != nil {
			t.Fatalf("TransformStreamChunk(chunk1): %v", err)
		}
		eofChunk, err := rt.TransformStreamChunk(ctx, port.StreamChunkInput{
			RequestID:    "req_gem_trunc",
			SessionState: chunk1.NextSessionState,
			IsEOF:        true,
		})
		if err != nil {
			t.Fatalf("TransformStreamChunk(eof): %v", err)
		}
		if eofChunk.StreamCompleted {
			t.Fatalf("FMEA-01 violation: Gemini premature EOF without finishReason must not report StreamCompleted=true")
		}
		for _, f := range eofChunk.NormalizedSSEFrames {
			if strings.Contains(string(f), "[DONE]") || strings.Contains(string(f), "finish_reason") {
				t.Fatalf("FMEA-01 violation: Gemini premature EOF must not synthesize finish_reason or [DONE]: %q", string(f))
			}
		}
	})
}

func TestNEG1CoreVendorNeutrality(t *testing.T) {
	internalDir := filepath.Join("..", "..", "internal")
	bannedHosts := []string{
		"api.openai.com",
		"api.anthropic.com",
		"generativelanguage.googleapis.com",
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileAST, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(fileAST, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				val = lit.Value
			}
			for _, host := range bannedHosts {
				if strings.Contains(val, host) {
					t.Errorf("NEG-1 violation: hardcoded vendor hostname %q found in Core file %s", host, path)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
}

func TestNEG2GeminiNeverUsesQueryStringSecret(t *testing.T) {
	ctx := context.Background()
	rt, cleanup := loadWasmAdapterRuntime(t, "gemini")
	defer cleanup()

	probe, err := rt.BuildValidationProbe(ctx, port.ValidationProbeInput{
		RequestID:       "neg2_probe",
		ConfigJSON:      `{}`,
		SecretSlotNames: []string{"api_key"},
	})
	if err != nil {
		t.Fatalf("BuildValidationProbe: %v", err)
	}
	assertGeminiHeaderAuthOnly(t, probe.TargetURL, probe.CredentialInjections)

	for _, stream := range []bool{false, true} {
		spec, err := rt.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "neg2_req",
			ConfigJSON:            `{}`,
			NormalizedRequestJSON: []byte(`{"model":"gemini-2.5-flash","stream":` + strconv.FormatBool(stream) + `,"messages":[{"role":"user","content":"hi"}]}`),
		})
		if err != nil {
			t.Fatalf("TransformRequest(stream=%v): %v", stream, err)
		}
		assertGeminiHeaderAuthOnly(t, spec.TargetURL, spec.CredentialInjections)
	}

	hints, err := rt.InspectNativeRequest(ctx, port.NativeRequestInput{
		RequestID:      "neg2_native",
		Dialect:        "gemini_generate_content",
		UpstreamPath:   "v1beta/models/gemini-2.5-flash:generateContent",
		RawRequestBody: []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
	})
	if err != nil {
		t.Fatalf("InspectNativeRequest: %v", err)
	}
	assertGeminiHeaderAuthOnly(t, "", hints.CredentialInjections)
}

func assertGeminiHeaderAuthOnly(t *testing.T, targetURL string, injections []port.CredentialInjectInstruction) {
	t.Helper()
	if strings.Contains(targetURL, "key=") {
		t.Fatalf("NEG-2 violation: outbound Gemini URL %q must never contain ?key=", targetURL)
	}
	if len(injections) == 0 {
		t.Fatalf("expected non-empty CredentialInjections for Gemini")
	}
	for _, inj := range injections {
		if inj.TargetLocation != port.CredentialTargetLocationHeader {
			t.Fatalf("NEG-2 violation: Gemini CredentialInjection TargetLocation = %q, want HEADER", inj.TargetLocation)
		}
		if !strings.EqualFold(inj.FieldName, "x-goog-api-key") {
			t.Fatalf("unexpected Gemini credential header %q, want x-goog-api-key", inj.FieldName)
		}
	}
}

func TestOfficialAdaptersNegativeAndNativePaths(t *testing.T) {
	ctx := context.Background()

	loadRuntime := func(dir string) (port.ProviderRuntime, *installer.ValidatedManifest, func()) {
		t.Helper()
		_, vm, _, err := testharness.ValidateExtensionDir(ctx, dir)
		if err != nil {
			t.Fatalf("ValidateExtensionDir(%s): %v", dir, err)
		}
		if vm.ExecutionTier == installer.ExecutionTierWasmSandboxT2 {
			rt, cleanup := loadWasmAdapterRuntime(t, dir)
			return rt, vm, cleanup
		}
		raw, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
		if err != nil {
			t.Fatalf("read manifest(%s): %v", dir, err)
		}
		eng, err := declarative.ParseDeclarativeManifestYAML(raw)
		if err != nil {
			t.Fatalf("ParseDeclarativeManifestYAML(%s): %v", dir, err)
		}
		return eng, vm, func() {}
	}

	cases := []struct {
		dir            string
		isTier1        bool
		allowedPaths   []string
		forbiddenPaths []string
	}{
		{
			dir:            "openai",
			isTier1:        true,
			allowedPaths:   []string{"v1/chat/completions", "v1/responses", "v1/embeddings", "v1/models"},
			forbiddenPaths: []string{"/v1/chat/completions", "admin/keys"},
		},
		{
			dir:            "anthropic",
			isTier1:        false,
			allowedPaths:   []string{"v1/messages", "v1/messages/count_tokens", "v1/models"},
			forbiddenPaths: []string{"/v1/messages", "v1/chat/completions"},
		},
		{
			dir:            "gemini",
			isTier1:        false,
			allowedPaths:   []string{"v1beta/models/gemini-2.5-flash:generateContent", "v1beta/models/gemini-2.5-flash:streamGenerateContent", "v1beta/models"},
			forbiddenPaths: []string{"/v1beta/models/gemini-2.5-flash:generateContent", "v1beta/models/a/b:generateContent"},
		},
		{
			dir:            "openai-compatible",
			isTier1:        true,
			allowedPaths:   []string{"v1/chat/completions", "v1/completions", "v1/embeddings", "v1/models"},
			forbiddenPaths: []string{"/v1/chat/completions", "admin/health"},
		},
		{
			dir:            "ollama",
			isTier1:        true,
			allowedPaths:   []string{"v1/chat/completions", "v1/embeddings", "v1/models", "api/chat", "api/generate", "api/tags"},
			forbiddenPaths: []string{"/api/chat", "api/pull"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.dir, func(t *testing.T) {
			rt, vm, cleanup := loadRuntime(tc.dir)
			defer cleanup()

			// 1. 403 error normalization across all 5 adapters
			norm403, err := rt.NormalizeError(ctx, port.NormalizeErrorInput{
				RequestID:     "req_403_" + tc.dir,
				HTTPStatus:    403,
				SanitizedBody: []byte(`{"error":{"message":"permission denied"}}`),
			})
			if err != nil {
				t.Fatalf("NormalizeError(403): %v", err)
			}
			if norm403.ErrorType != "authentication_error" ||
				(norm403.CanonicalCode != "authentication_error" && norm403.CanonicalCode != "upstream_auth_failed") ||
				!norm403.TriggerConnectionReauth || norm403.RetryablePreStream {
				t.Fatalf("unexpected 403 normalized error for %s: %+v", tc.dir, norm403)
			}

			// 2. Malformed upstream JSON handling (graceful error without panic/trap)
			if tc.isTier1 {
				if _, err := rt.TransformResponse(ctx, port.TransformResponseInput{
					RequestID:       "req_bad_json_" + tc.dir,
					HTTPStatus:      200,
					RawResponseBody: []byte("{broken"),
				}); err == nil {
					t.Fatalf("expected Tier-1 %s TransformResponse to reject malformed JSON", tc.dir)
				}
			} else {
				// Tier-2 Wasm tolerates malformed JSON safely without fabricating a synthetic 200 choice.
				res, err := rt.TransformResponse(ctx, port.TransformResponseInput{
					RequestID:       "req_bad_json_" + tc.dir,
					HTTPStatus:      200,
					RawResponseBody: []byte("{broken"),
				})
				if err != nil || len(res.NormalizedResponseJSON) != 0 {
					t.Fatalf("expected empty NormalizedResponseJSON and nil error for %s on malformed JSON, got res=%+v err=%v", tc.dir, res, err)
				}
			}

			// 3. Tier-1 premature SSE EOF without [DONE]
			if tc.isTier1 {
				c1, err := rt.TransformStreamChunk(ctx, port.StreamChunkInput{
					RequestID:        "req_t1_eof_" + tc.dir,
					RawUpstreamChunk: []byte("data: {\"id\":\"cmpl_1\",\"model\":\"m1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"),
					IsEOF:            false,
				})
				if err != nil {
					t.Fatalf("TransformStreamChunk(chunk1): %v", err)
				}
				eof, err := rt.TransformStreamChunk(ctx, port.StreamChunkInput{
					RequestID:    "req_t1_eof_" + tc.dir,
					SessionState: c1.NextSessionState,
					IsEOF:        true,
				})
				if err != nil {
					t.Fatalf("TransformStreamChunk(eof): %v", err)
				}
				if eof.StreamCompleted {
					t.Fatalf("%s premature EOF without [DONE] must not mark StreamCompleted=true", tc.dir)
				}
				for _, f := range eof.NormalizedSSEFrames {
					if strings.Contains(string(f), "[DONE]") {
						t.Fatalf("%s premature EOF must not synthesize [DONE]: %q", tc.dir, string(f))
					}
				}
			}

			// 4. Native passthrough allowedNativePaths regex verification (matching native/handler.go semantics)
			if len(vm.Manifest.Capabilities) == 0 {
				t.Fatalf("%s manifest missing capabilities", tc.dir)
			}
			patterns := vm.Manifest.Capabilities[0].AllowedNativePaths
			if len(patterns) == 0 {
				t.Fatalf("%s manifest missing allowedNativePaths", tc.dir)
			}
			compiled := make([]*regexp.Regexp, 0, len(patterns))
			for _, pat := range patterns {
				re, err := regexp.Compile("^(?:" + pat + ")$")
				if err != nil {
					t.Fatalf("%s invalid allowedNativePath regex %q: %v", tc.dir, pat, err)
				}
				compiled = append(compiled, re)
			}
			matchesAny := func(path string) bool {
				for _, re := range compiled {
					if re.MatchString(path) {
						return true
					}
				}
				return false
			}
			for _, p := range tc.allowedPaths {
				if !matchesAny(p) {
					t.Fatalf("%s allowedNativePaths failed to match expected normalized path %q", tc.dir, p)
				}
			}
			for _, p := range tc.forbiddenPaths {
				if matchesAny(p) {
					t.Fatalf("%s allowedNativePaths unexpectedly matched forbidden path %q", tc.dir, p)
				}
			}
		})
	}

	// Verify Anthropic Wasm safely ignores message_delta omitting "delta" without trapping.
	anthRT, cleanupAnth := loadWasmAdapterRuntime(t, "anthropic")
	defer cleanupAnth()
	chunk, err := anthRT.TransformStreamChunk(ctx, port.StreamChunkInput{
		RequestID:        "req_anth_missing_delta",
		RawUpstreamChunk: []byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"),
		IsEOF:            true,
	})
	if err != nil {
		t.Fatalf("Anthropic message_delta missing delta should not fail: %v", err)
	}
	if !chunk.StreamCompleted {
		t.Fatalf("expected StreamCompleted=true after message_stop")
	}
}

func TestWasmAdaptersSSEPumpAndNativeUsage(t *testing.T) {
	ctx := context.Background()

	t.Run("anthropic_sse_pump_clean_and_truncated", func(t *testing.T) {
		rt, cleanup := loadWasmAdapterRuntime(t, "anthropic")
		defer cleanup()

		cleanSSE := strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_pump_1","model":"claude-sonnet-4-5","usage":{"input_tokens":12,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
			``,
		}, "\n")

		rec := httptest.NewRecorder()
		res, err := sse.Pump(ctx, sse.PumpConfig{
			Writer:              rec,
			Upstream:            io.NopCloser(strings.NewReader(cleanSSE)),
			Runtime:             rt,
			RequestID:           "req_pump_anth_clean",
			RequireDoneSentinel: true,
		})
		if err != nil {
			t.Fatalf("clean Anthropic sse.Pump returned unexpected error: %v (body=%s)", err, rec.Body.String())
		}
		if !res.SawDone || res.Usage == nil || res.Usage.InputTokens != 12 || res.Usage.OutputTokens != 7 || res.Usage.CacheReadTokens != 3 || res.Usage.CacheWriteTokens != 4 {
			t.Fatalf("unexpected Anthropic clean pump result: %+v usage=%+v", res, res.Usage)
		}
		if strings.Contains(rec.Body.String(), "mid_stream_abort") || !strings.Contains(rec.Body.String(), "data: [DONE]") {
			t.Fatalf("unexpected Anthropic clean SSE body: %s", rec.Body.String())
		}

		truncSSE := strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_pump_t","model":"claude-sonnet-4-5","usage":{"input_tokens":5}}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"cut"}}`,
			``,
			``,
		}, "\n")
		truncRec := httptest.NewRecorder()
		_, err = sse.Pump(ctx, sse.PumpConfig{
			Writer:              truncRec,
			Upstream:            io.NopCloser(strings.NewReader(truncSSE)),
			Runtime:             rt,
			RequestID:           "req_pump_anth_trunc",
			RequireDoneSentinel: true,
		})
		if !errors.Is(err, sse.ErrMidStreamAbort) {
			t.Fatalf("expected ErrMidStreamAbort on truncated Anthropic stream, got %v", err)
		}
		if !strings.Contains(truncRec.Body.String(), `"code":"mid_stream_abort"`) || strings.Contains(truncRec.Body.String(), "data: [DONE]") {
			t.Fatalf("unexpected truncated Anthropic SSE body: %s", truncRec.Body.String())
		}
	})

	t.Run("gemini_sse_pump_clean_and_truncated", func(t *testing.T) {
		rt, cleanup := loadWasmAdapterRuntime(t, "gemini")
		defer cleanup()

		cleanSSE := strings.Join([]string{
			`data: {"responseId":"gem_pump_1","modelVersion":"gemini-2.5-flash","candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]}}],"usageMetadata":{"promptTokenCount":10}}`,
			``,
			`data: {"responseId":"gem_pump_1","modelVersion":"gemini-2.5-flash","candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"cachedContentTokenCount":2,"thoughtsTokenCount":1}}`,
			``,
			``,
		}, "\n")

		rec := httptest.NewRecorder()
		res, err := sse.Pump(ctx, sse.PumpConfig{
			Writer:              rec,
			Upstream:            io.NopCloser(strings.NewReader(cleanSSE)),
			Runtime:             rt,
			RequestID:           "req_pump_gem_clean",
			RequireDoneSentinel: true,
		})
		if err != nil {
			t.Fatalf("clean Gemini sse.Pump returned unexpected error: %v (body=%s)", err, rec.Body.String())
		}
		if !res.SawDone || res.Usage == nil || res.Usage.InputTokens != 10 || res.Usage.OutputTokens != 5 || res.Usage.CacheReadTokens != 2 || res.Usage.ReasoningTokens != 1 {
			t.Fatalf("unexpected Gemini clean pump result: %+v usage=%+v", res, res.Usage)
		}
		if strings.Contains(rec.Body.String(), "mid_stream_abort") || !strings.Contains(rec.Body.String(), "data: [DONE]") {
			t.Fatalf("unexpected Gemini clean SSE body: %s", rec.Body.String())
		}

		truncSSE := "data: {\"responseId\":\"gem_t\",\"modelVersion\":\"gemini-2.5-flash\",\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"cut\"}]}}]}\n\n"
		truncRec := httptest.NewRecorder()
		_, err = sse.Pump(ctx, sse.PumpConfig{
			Writer:              truncRec,
			Upstream:            io.NopCloser(strings.NewReader(truncSSE)),
			Runtime:             rt,
			RequestID:           "req_pump_gem_trunc",
			RequireDoneSentinel: true,
		})
		if !errors.Is(err, sse.ErrMidStreamAbort) {
			t.Fatalf("expected ErrMidStreamAbort on truncated Gemini stream, got %v", err)
		}
		if !strings.Contains(truncRec.Body.String(), `"code":"mid_stream_abort"`) || strings.Contains(truncRec.Body.String(), "data: [DONE]") {
			t.Fatalf("unexpected truncated Gemini SSE body: %s", truncRec.Body.String())
		}
	})

	t.Run("native_sse_usage_extraction", func(t *testing.T) {
		anthRT, cleanupAnth := loadWasmAdapterRuntime(t, "anthropic")
		defer cleanupAnth()

		anthAcc := native.NewSSEUsageAccumulator(anthRT, "req_nat_anth", []byte(`{"model":"claude-sonnet-4-5"}`))
		anthAcc.Feed(ctx, []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":42,\"cache_read_input_tokens\":6,\"cache_creation_input_tokens\":8}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":19}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
		anthUsage := anthAcc.Finalize(ctx)
		if anthUsage.Accuracy != port.UsageAccuracyExact || anthUsage.InputTokens != 42 || anthUsage.OutputTokens != 19 || anthUsage.CacheReadTokens != 6 || anthUsage.CacheWriteTokens != 8 {
			t.Fatalf("unexpected Anthropic native SSE usage: %+v", anthUsage)
		}

		gemRT, cleanupGem := loadWasmAdapterRuntime(t, "gemini")
		defer cleanupGem()

		gemAcc := native.NewSSEUsageAccumulator(gemRT, "req_nat_gem", []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
		gemAcc.Feed(ctx, []byte("data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":30,\"candidatesTokenCount\":11,\"cachedContentTokenCount\":5,\"thoughtsTokenCount\":3}}\n\ndata: [DONE]\n\n"))
		gemUsage := gemAcc.Finalize(ctx)
		if gemUsage.Accuracy != port.UsageAccuracyExact || gemUsage.InputTokens != 30 || gemUsage.OutputTokens != 11 || gemUsage.CacheReadTokens != 5 || gemUsage.ReasoningTokens != 3 {
			t.Fatalf("unexpected Gemini native SSE usage: %+v", gemUsage)
		}

		directGem, err := gemRT.ExtractUsage(ctx, port.ExtractUsageInput{
			RequestID:                 "req_nat_gem_line",
			Modality:                  "NATIVE",
			RawResponseOrTrailerBytes: []byte(`data: {"usageMetadata":{"promptTokenCount":14,"candidatesTokenCount":9}}`),
		})
		if err != nil || directGem.Accuracy != port.UsageAccuracyExact || directGem.InputTokens != 14 || directGem.OutputTokens != 9 {
			t.Fatalf("direct Gemini SSE ExtractUsage unexpected: %+v err=%v", directGem, err)
		}
	})
}

func TestRound2AdapterAndSchemaRegressions(t *testing.T) {
	ctx := context.Background()

	t.Run("gemini_model_path_traversal_escaping_and_empty_candidates", func(t *testing.T) {
		rt, cleanup := loadWasmAdapterRuntime(t, "gemini")
		defer cleanup()

		reqBytes, _ := json.Marshal(map[string]any{
			"model": "../bad?x=1",
			"messages": []any{
				map[string]any{"role": "user", "content": "hi"},
			},
		})
		spec, err := rt.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "req_gem_escape",
			ConfigJSON:            `{}`,
			NormalizedRequestJSON: reqBytes,
		})
		if err != nil {
			t.Fatalf("TransformRequest with special model chars: %v", err)
		}
		if strings.Contains(spec.TargetURL, "../bad") || strings.Contains(spec.TargetURL, "?x=1") {
			t.Fatalf("Gemini TargetURL allowed unescaped model path traversal/query: %q", spec.TargetURL)
		}
		if !strings.Contains(spec.TargetURL, "/models/%2E%2E%2Fbad%3Fx%3D1:generateContent") {
			t.Fatalf("expected percent-encoded Gemini model path, got %q", spec.TargetURL)
		}

		emptyResp, err := rt.TransformResponse(ctx, port.TransformResponseInput{
			RequestID:       "req_gem_empty_cands",
			HTTPStatus:      200,
			RawResponseBody: []byte(`{"candidates":[]}`),
		})
		if err != nil || len(emptyResp.NormalizedResponseJSON) != 0 {
			t.Fatalf("expected empty NormalizedResponseJSON on empty Gemini candidates, got res=%+v err=%v", emptyResp, err)
		}
	})

	t.Run("small_inline_raw_base64_file_data", func(t *testing.T) {
		rawB64 := "JVBERi0xLjQK" // "PDF" <= 64 KiB, raw base64 without data: prefix

		anthRT, cleanupAnth := loadWasmAdapterRuntime(t, "anthropic")
		defer cleanupAnth()
		reqBytes, _ := json.Marshal(map[string]any{
			"model": "claude-sonnet-4-5",
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "input_file", "file_data": rawB64, "mime_type": "application/pdf"},
					},
				},
			},
		})
		anthSpec, err := anthRT.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "req_anth_inline_b64",
			ConfigJSON:            `{}`,
			NormalizedRequestJSON: reqBytes,
		})
		if err != nil {
			t.Fatalf("Anthropic TransformRequest small inline raw base64: %v", err)
		}
		if !strings.Contains(string(anthSpec.BodyBytes), `"type":"base64"`) ||
			!strings.Contains(string(anthSpec.BodyBytes), `"data":"`+rawB64+`"`) ||
			strings.Contains(string(anthSpec.BodyBytes), `"type":"url"`) {
			t.Fatalf("expected Anthropic inline base64 document block, got %s", string(anthSpec.BodyBytes))
		}

		gemRT, cleanupGem := loadWasmAdapterRuntime(t, "gemini")
		defer cleanupGem()
		gemReqBytes, _ := json.Marshal(map[string]any{
			"model": "gemini-2.5-flash",
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "input_file", "file_data": rawB64, "mime_type": "application/pdf"},
					},
				},
			},
		})
		gemSpec, err := gemRT.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "req_gem_inline_b64",
			ConfigJSON:            `{}`,
			NormalizedRequestJSON: gemReqBytes,
		})
		if err != nil {
			t.Fatalf("Gemini TransformRequest small inline raw base64: %v", err)
		}
		if !strings.Contains(string(gemSpec.BodyBytes), `"inlineData":{"mimeType":"application/pdf","data":"`+rawB64+`"}`) ||
			strings.Contains(string(gemSpec.BodyBytes), `"fileData"`) {
			t.Fatalf("expected Gemini inlineData for small raw base64 file_data, got %s", string(gemSpec.BodyBytes))
		}
	})

	t.Run("gemini_finish_reason_precedence_max_tokens_over_tools", func(t *testing.T) {
		gemRT, cleanup := loadWasmAdapterRuntime(t, "gemini")
		defer cleanup()

		respBody := []byte(`{"responseId":"gem_max_tok","modelVersion":"gemini-2.5-flash","candidates":[{"finishReason":"MAX_TOKENS","content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"Hanoi"}}}]}}]}`)
		res, err := gemRT.TransformResponse(ctx, port.TransformResponseInput{
			RequestID:       "req_gem_finish_prec",
			HTTPStatus:      200,
			RawResponseBody: respBody,
		})
		if err != nil {
			t.Fatalf("TransformResponse: %v", err)
		}
		var parsed struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(res.NormalizedResponseJSON, &parsed); err != nil {
			t.Fatalf("unmarshal normalized response: %v (%s)", err, string(res.NormalizedResponseJSON))
		}
		if len(parsed.Choices) != 1 || parsed.Choices[0].FinishReason != "length" {
			t.Fatalf("expected MAX_TOKENS finishReason to map to length even with functionCall, got %+v", parsed)
		}
	})

	t.Run("tier1_openai_sse_usage_schema", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join("openai", "manifest.yaml"))
		if err != nil {
			t.Fatalf("read openai manifest: %v", err)
		}
		eng, err := declarative.ParseDeclarativeManifestYAML(raw)
		if err != nil {
			t.Fatalf("ParseDeclarativeManifestYAML: %v", err)
		}
		chunk := []byte("data: {\"id\":\"chatcmpl-u1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":21,\"completion_tokens\":9,\"total_tokens\":30,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":2}}}\n\ndata: [DONE]\n\n")
		out, err := eng.TransformStreamChunk(ctx, port.StreamChunkInput{
			RequestID:        "req_t1_usage_schema",
			RawUpstreamChunk: chunk,
			IsEOF:            true,
		})
		if err != nil {
			t.Fatalf("TransformStreamChunk: %v", err)
		}
		var sawOpenAIUsage bool
		for _, f := range out.NormalizedSSEFrames {
			s := string(f)
			if strings.Contains(s, `"usage"`) {
				if strings.Contains(s, `"prompt_tokens":21`) && strings.Contains(s, `"completion_tokens":9`) && strings.Contains(s, `"total_tokens":30`) && !strings.Contains(s, `"input_tokens"`) {
					sawOpenAIUsage = true
				} else {
					t.Fatalf("expected OpenAI schema usage fields in Tier-1 SSE frame, got %s", s)
				}
			}
		}
		if !sawOpenAIUsage {
			t.Fatalf("expected usage frame in Tier-1 SSE output")
		}
	})
}


