// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/testkit/fakellm"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
)

func uleb(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func sleb64(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		more := !((v == 0 && (b&0x40) == 0) || (v == -1 && (b&0x40) != 0))
		if more {
			b |= 0x80
		}
		out = append(out, b)
		if !more {
			return out
		}
	}
}

func wSection(id byte, payload []byte) []byte {
	var out []byte
	out = append(out, id)
	out = append(out, uleb(uint32(len(payload)))...)
	out = append(out, payload...)
	return out
}

func wString(s string) []byte {
	var out []byte
	out = append(out, uleb(uint32(len(s)))...)
	out = append(out, s...)
	return out
}

// buildTP0016MultimodalWasmAdapter builds a valid Tier-2 Wasm binary with a strict 16 MiB
// (256 pages) linear memory ceiling. Its `transform_request(ptr, len)` traps if len > 65536
// (proving 20 MiB attachments never enter Wasm), extracts the base64 `normalized_request_json`
// containing `blob_ref://`, selects `/v1/responses` or `/v1/chat/completions`, and emits a
// canonical `UpstreamHttpSpec`. Its `transform_response(ptr, len)` maps FakeLLM's JSON to
// `NormalizedInferenceResponse` for both Chat and Responses modalities.
func buildTP0016MultimodalWasmAdapter(fakeBaseURL string) []byte {
	type dataSeg struct {
		offset uint32
		data   []byte
	}
	reqNeedle := []byte(`"normalized_request_json":"`)
	respTagNeedle := []byte(`_tanpopo_modality\":\"responses`)
	outputTextNeedle := []byte(`"RESPONSES"`)

	prefixChat := []byte(`{"method":"POST","target_url":"` + fakeBaseURL + `/v1/chat/completions","safe_headers":{"Content-Type":"application/json"},"credential_injections":[{"secret_slot_name":"api_key","target_location":"CREDENTIAL_TARGET_LOCATION_HEADER","field_name":"Authorization","value_prefix":"Bearer "}],"body_bytes":"`)
	prefixResp := []byte(`{"method":"POST","target_url":"` + fakeBaseURL + `/v1/responses","safe_headers":{"Content-Type":"application/json"},"credential_injections":[{"secret_slot_name":"api_key","target_location":"CREDENTIAL_TARGET_LOCATION_HEADER","field_name":"Authorization","value_prefix":"Bearer "}],"body_bytes":"`)
	suffixJSON := []byte(`"}`)

	chatFakeNorm := base64.StdEncoding.EncodeToString([]byte(`{"id":"chatcmpl_wasm_1","object":"chat.completion","model":"acme-gpt-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"ok-wasm-chat"},"finish_reason":"stop"}]}`))
	respFakeNorm := base64.StdEncoding.EncodeToString([]byte(`{"id":"resp_wasm_1","object":"response","status":"completed","model":"acme-gpt-upstream","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok-wasm-resp"}]}]}`))

	chatRespEnvelope := []byte(`{"normalized_response_json":"` + chatFakeNorm + `","upstream_model_id":"acme-gpt-upstream","finish_reason":"stop","usage":{"input_tokens":10,"output_tokens":5,"accuracy":"USAGE_ACCURACY_EXACT"}}`)
	respRespEnvelope := []byte(`{"normalized_response_json":"` + respFakeNorm + `","upstream_model_id":"acme-gpt-upstream","finish_reason":"stop","usage":{"input_tokens":10,"output_tokens":4,"accuracy":"USAGE_ACCURACY_EXACT"}}`)

	const (
		reqNeedleOff   uint32 = 65536
		respNeedleOff  uint32 = 65600
		outNeedleOff   uint32 = 65664
		prefixChatOff  uint32 = 65728
		prefixRespOff  uint32 = 66176
		suffixOff      uint32 = 66624
		chatEnvOff     uint32 = 66688
		respEnvOff     uint32 = 67600
		scratchOutOff  uint32 = 131072
	)
	segs := []dataSeg{
		{offset: reqNeedleOff, data: reqNeedle},
		{offset: respNeedleOff, data: respTagNeedle},
		{offset: outNeedleOff, data: outputTextNeedle},
		{offset: prefixChatOff, data: prefixChat},
		{offset: prefixRespOff, data: prefixResp},
		{offset: suffixOff, data: suffixJSON},
		{offset: chatEnvOff, data: chatRespEnvelope},
		{offset: respEnvOff, data: respRespEnvelope},
	}

	// Helpers to emit Wasm byte sequences for substring search and memcpy.
	emitMemcpy := func(b []byte, dstLocal, srcLocal, lenLocal, idxLocal uint32) []byte {
		b = append(b, 0x41, 0x00, 0x21)
		b = append(b, uleb(idxLocal)...) // idx = 0
		b = append(b, 0x02, 0x40)       // block
		b = append(b, 0x03, 0x40)       // loop
		// br_if 1 if idx >= len
		b = append(b, 0x20)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x20)
		b = append(b, uleb(lenLocal)...)
		b = append(b, 0x4f, 0x0d, 0x01) // i32.ge_u; br_if 1
		// i32.store8 (dst + idx) = i32.load8_u (src + idx)
		b = append(b, 0x20)
		b = append(b, uleb(dstLocal)...)
		b = append(b, 0x20)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x6a) // add
		b = append(b, 0x20)
		b = append(b, uleb(srcLocal)...)
		b = append(b, 0x20)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x6a)             // add
		b = append(b, 0x2d, 0x00, 0x00) // i32.load8_u
		b = append(b, 0x3a, 0x00, 0x00) // i32.store8
		// idx = idx + 1; br 0
		b = append(b, 0x20)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x41, 0x01, 0x6a, 0x21)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x0c, 0x00, 0x0b, 0x0b)
		return b
	}

	// Emits code to find mem[needleOff : needleOff+needleLen] in mem[inPtr : inPtr+inLen],
	// storing match start address (inPtr+i) into matchLocal, or -1 if not found.
	emitFindSubstr := func(b []byte, inPtrL, inLenL, needleOff uint32, needleLen uint32, matchL, iL, kL uint32) []byte {
		// matchL = -1
		b = append(b, 0x41)
		b = append(b, sleb64(-1)...)
		b = append(b, 0x21)
		b = append(b, uleb(matchL)...)
		// if inLen < needleLen skip
		b = append(b, 0x20)
		b = append(b, uleb(inLenL)...)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleLen))...)
		b = append(b, 0x4f) // inLen >= needleLen
		b = append(b, 0x04, 0x40)
		// i = 0
		b = append(b, 0x41, 0x00, 0x21)
		b = append(b, uleb(iL)...)
		b = append(b, 0x02, 0x40) // block outer
		b = append(b, 0x03, 0x40) // loop i
		// if i > inLen - needleLen -> br 1
		b = append(b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x20)
		b = append(b, uleb(inLenL)...)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleLen))...)
		b = append(b, 0x6b) // sub
		b = append(b, 0x4b, 0x0d, 0x01) // i32.gt_u; br_if 1
		// inner k = 0
		b = append(b, 0x41, 0x00, 0x21)
		b = append(b, uleb(kL)...)
		b = append(b, 0x02, 0x40) // block inner
		b = append(b, 0x03, 0x40) // loop k
		// if k >= needleLen -> found!
		b = append(b, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleLen))...)
		b = append(b, 0x4f) // ge_u
		b = append(b, 0x04, 0x40)
		b = append(b, 0x20)
		b = append(b, uleb(inPtrL)...)
		b = append(b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x6a, 0x21)
		b = append(b, uleb(matchL)...) // match = inPtr + i
		b = append(b, 0x0c, 0x04)       // br 4 (exit outer block)
		b = append(b, 0x0b)             // end if k>=needleLen
		// compare mem[inPtr+i+k] != mem[needleOff+k] -> br 1 (break inner)
		b = append(b, 0x20)
		b = append(b, uleb(inPtrL)...)
		b = append(b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x6a)
		b = append(b, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x6a, 0x2d, 0x00, 0x00) // load8_u(inPtr+i+k)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleOff))...)
		b = append(b, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x6a, 0x2d, 0x00, 0x00) // load8_u(needleOff+k)
		b = append(b, 0x47, 0x0d, 0x01)       // i32.ne; br_if 1
		// k = k + 1; br 0
		b = append(b, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x41, 0x01, 0x6a, 0x21)
		b = append(b, uleb(kL)...)
		b = append(b, 0x0c, 0x00, 0x0b, 0x0b)
		// i = i + 1; br 0
		b = append(b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x41, 0x01, 0x6a, 0x21)
		b = append(b, uleb(iL)...)
		b = append(b, 0x0c, 0x00, 0x0b, 0x0b)
		b = append(b, 0x0b) // end outer if
		return b
	}

	// Func 0: transform_request(ptr=0, len=1) -> i64
	// Locals: 2:i, 3:k, 4:match, 5:b64Start, 6:b64Len, 7:prefPtr, 8:prefLen, 9:cursor, 10:endPtr
	var reqBody []byte
	reqBody = append(reqBody, 0x01, 0x09, 0x7f) // 9 i32 locals (indices 2..10)
	// Guard: if len > 65536 -> trap (unreachable), proving large attachments were offloaded!
	reqBody = append(reqBody, 0x20, 0x01, 0x41)
	reqBody = append(reqBody, sleb64(65536)...)
	reqBody = append(reqBody, 0x4b, 0x04, 0x40, 0x00, 0x0b)
	// Find "normalized_request_json":"
	reqBody = emitFindSubstr(reqBody, 0, 1, reqNeedleOff, uint32(len(reqNeedle)), 4, 2, 3)
	// If match == -1 -> unreachable
	reqBody = append(reqBody, 0x20, 0x04, 0x41)
	reqBody = append(reqBody, sleb64(-1)...)
	reqBody = append(reqBody, 0x46, 0x04, 0x40, 0x00, 0x0b)
	// b64Start = match + len(reqNeedle)
	reqBody = append(reqBody, 0x20, 0x04, 0x41)
	reqBody = append(reqBody, sleb64(int64(len(reqNeedle)))...)
	reqBody = append(reqBody, 0x6a, 0x21, 0x05)
	// Find closing '"' starting at b64Start up to inPtr + inLen (local 10)
	reqBody = append(reqBody, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x21, 0x0a)
	reqBody = append(reqBody, 0x20, 0x05, 0x21, 0x02) // i = b64Start
	reqBody = append(reqBody, 0x02, 0x40, 0x03, 0x40)
	reqBody = append(reqBody, 0x20, 0x02, 0x20, 0x0a, 0x4f, 0x0d, 0x01) // i >= endPtr -> br 1
	reqBody = append(reqBody, 0x20, 0x02, 0x2d, 0x00, 0x00, 0x41, 0x22, 0x46, 0x0d, 0x01) // mem[i] == '"' -> br 1
	reqBody = append(reqBody, 0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, 0x0c, 0x00, 0x0b, 0x0b)
	// b64Len = i - b64Start
	reqBody = append(reqBody, 0x20, 0x02, 0x20, 0x05, 0x6b, 0x21, 0x06)
	// Check if "_tanpopo_modality\":\"responses" is inside input
	reqBody = emitFindSubstr(reqBody, 0, 1, respNeedleOff, uint32(len(respTagNeedle)), 4, 2, 3)
	// if match != -1 -> prefResp else prefChat
	reqBody = append(reqBody, 0x20, 0x04, 0x41)
	reqBody = append(reqBody, sleb64(-1)...)
	reqBody = append(reqBody, 0x47, 0x04, 0x40)
	reqBody = append(reqBody, 0x41)
	reqBody = append(reqBody, sleb64(int64(prefixRespOff))...)
	reqBody = append(reqBody, 0x21, 0x07, 0x41)
	reqBody = append(reqBody, sleb64(int64(len(prefixResp)))...)
	reqBody = append(reqBody, 0x21, 0x08, 0x05)
	reqBody = append(reqBody, 0x41)
	reqBody = append(reqBody, sleb64(int64(prefixChatOff))...)
	reqBody = append(reqBody, 0x21, 0x07, 0x41)
	reqBody = append(reqBody, sleb64(int64(len(prefixChat)))...)
	reqBody = append(reqBody, 0x21, 0x08, 0x0b)
	// Copy prefix -> b64 -> suffix into scratchOutOff
	reqBody = append(reqBody, 0x41)
	reqBody = append(reqBody, sleb64(int64(scratchOutOff))...)
	reqBody = append(reqBody, 0x21, 0x09) // cursor = scratchOutOff
	reqBody = emitMemcpy(reqBody, 9, 7, 8, 2)
	reqBody = append(reqBody, 0x20, 0x09, 0x20, 0x08, 0x6a, 0x21, 0x09) // cursor += prefLen
	reqBody = emitMemcpy(reqBody, 9, 5, 6, 2)
	reqBody = append(reqBody, 0x20, 0x09, 0x20, 0x06, 0x6a, 0x21, 0x09) // cursor += b64Len
	reqBody = append(reqBody, 0x41)
	reqBody = append(reqBody, sleb64(int64(suffixOff))...)
	reqBody = append(reqBody, 0x21, 0x07, 0x41)
	reqBody = append(reqBody, sleb64(int64(len(suffixJSON)))...)
	reqBody = append(reqBody, 0x21, 0x08)
	reqBody = emitMemcpy(reqBody, 9, 7, 8, 2)
	reqBody = append(reqBody, 0x20, 0x09, 0x20, 0x08, 0x6a, 0x21, 0x09) // cursor += len(suffix)
	// Return ((scratchOutOff << 32) | (cursor - scratchOutOff))
	reqBody = append(reqBody, 0x42)
	reqBody = append(reqBody, sleb64(int64(uint64(scratchOutOff)<<32))...)
	reqBody = append(reqBody, 0x20, 0x09, 0x41)
	reqBody = append(reqBody, sleb64(int64(scratchOutOff))...)
	reqBody = append(reqBody, 0x6b, 0xad, 0x84, 0x0b) // sub; i64.extend_i32_u; i64.or; end

	// Func 1: transform_response(ptr=0, len=1) -> i64
	// Locals: 2:i, 3:k, 4:match
	var respBody []byte
	respBody = append(respBody, 0x01, 0x03, 0x7f)
	respBody = emitFindSubstr(respBody, 0, 1, outNeedleOff, uint32(len(outputTextNeedle)), 4, 2, 3)
	respBody = append(respBody, 0x20, 0x04, 0x41)
	respBody = append(respBody, sleb64(-1)...)
	respBody = append(respBody, 0x47, 0x04, 0x7e) // if match != -1 (result i64)
	respBody = append(respBody, 0x42)
	respBody = append(respBody, sleb64(int64((uint64(respEnvOff)<<32)|uint64(len(respRespEnvelope))))...)
	respBody = append(respBody, 0x05)
	respBody = append(respBody, 0x42)
	respBody = append(respBody, sleb64(int64((uint64(chatEnvOff)<<32)|uint64(len(chatRespEnvelope))))...)
	respBody = append(respBody, 0x0b, 0x0b)

	var out []byte
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	// Section 1 Type: (i32, i32) -> i64
	out = append(out, wSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	// Section 3 Function: 2 funcs of type 0
	out = append(out, wSection(3, []byte{0x02, 0x00, 0x00})...)
	// Section 5 Memory: min=4 pages (256 KiB), max=256 pages (16 MiB)
	out = append(out, wSection(5, append(append([]byte{0x01, 0x01}, uleb(4)...), uleb(256)...))...)
	// Section 7 Export: memory, transform_request, transform_response
	var exp []byte
	exp = append(exp, 0x03)
	exp = append(exp, wString("memory")...)
	exp = append(exp, 0x02, 0x00)
	exp = append(exp, wString("transform_request")...)
	exp = append(exp, 0x00, 0x00)
	exp = append(exp, wString("transform_response")...)
	exp = append(exp, 0x00, 0x01)
	out = append(out, wSection(7, exp)...)
	// Section 10 Code
	var code []byte
	code = append(code, 0x02)
	code = append(code, uleb(uint32(len(reqBody)))...)
	code = append(code, reqBody...)
	code = append(code, uleb(uint32(len(respBody)))...)
	code = append(code, respBody...)
	out = append(out, wSection(10, code)...)
	// Section 11 Data
	var dataSec []byte
	dataSec = append(dataSec, uleb(uint32(len(segs)))...)
	for _, s := range segs {
		dataSec = append(dataSec, 0x00, 0x41)
		dataSec = append(dataSec, sleb64(int64(s.offset))...)
		dataSec = append(dataSec, 0x0b)
		dataSec = append(dataSec, uleb(uint32(len(s.data)))...)
		dataSec = append(dataSec, s.data...)
	}
	out = append(out, wSection(11, dataSec)...)
	return out
}

func doIngressReq(t *testing.T, h *ingressHarness, method, path, vkeyTok string, body []byte, extraHdrs map[string]string) *http.Response {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, h.httpSrv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if vkeyTok != "" {
		req.Header.Set("Authorization", "Bearer "+vkeyTok)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extraHdrs {
		req.Header.Set(k, v)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readJSONResp(t *testing.T, resp *http.Response) (int, http.Header, map[string]any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	raw, err := ioReadAllBounded(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, resp.Header, m
}

func ioReadAllBounded(resp *http.Response) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(resp.Body)
	return buf.Bytes(), err
}

func TestTP0016_AC1_20MiBMultimodalThrough16MiBWasmSandbox(t *testing.T) {
	h := setupIngressHarness(t, 0) // default 32 MiB bounded ingress ceiling

	// Install & activate a real Tier-2 Wasm Adapter capped at 16 MiB linear memory.
	wasmDir := t.TempDir()
	wasmBytes := buildTP0016MultimodalWasmAdapter(h.fake.URL())
	if err := os.WriteFile(filepath.Join(wasmDir, "adapter.wasm"), wasmBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	wasmSum := sha256.Sum256(wasmBytes)
	wasmSHA := hex.EncodeToString(wasmSum[:])

	wasmManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/multimodal-wasm"
  name: "Acme Multimodal Wasm Adapter"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "adapter.wasm"
  sha256: "` + wasmSHA + `"
  quotas:
    maxMemoryMib: 16
    maxCpuMsPerCall: 200
capabilities:
  - type: "provider_adapter"
    adapterSlug: "acme-wasm-mm"
    supportedDialects: ["openai_compatible"]
secretSlots:
  - slot: "api_key"
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:transform_request"
    - "provider:transform_response"
`
	if err := os.WriteFile(filepath.Join(wasmDir, "manifest.yaml"), []byte(wasmManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	tpkgPath := filepath.Join(t.TempDir(), "acme-wasm-1.0.0.tpkg")
	packRes, err := testharness.PackExtension(h.ctx, wasmDir, tpkgPath)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(h.pubPriv, installer.SignatureSigningPayload("@acme/multimodal-wasm", "1.0.0", packRes.SHA256))
	staged, err := h.installer.StageFreshInstall(h.ctx, installer.StageInstallRequest{
		ArchivePath: tpkgPath,
		Source: installer.SourceSpec{
			Kind:           installer.SourceKindLocalArchive,
			URI:            "./acme-wasm-1.0.0.tpkg",
			ExpectedSHA256: packRes.SHA256,
		},
		Signature:   installer.ProvenanceSignature{PublisherOrRootSig: sig},
		ActorID:     "prc_op_1",
		RequestID:   "req_wasm_stage",
		TargetOrgID: h.org.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := h.installer.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: "prc_op_1",
		ActivateImmediately: true,
		OrgID:               h.org.ID,
		RequestID:           "req_wasm_approve",
	})
	if err != nil {
		t.Fatal(err)
	}

	adp, err := h.catalog.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    rec.InstallationID,
		AdapterSlug:       "acme-wasm-mm",
		SupportedDialects: []string{"openai"},
		AllowedHosts:      []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	secMeta, err := h.vault.SealSecret(h.ctx, catalogVaultSeal(h.org.ID, "wasm-mm-key", h.secret))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := h.catalog.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           h.org.ID,
		AdapterID:       adp.ID,
		Name:            "wasm-mm-conn",
		EndpointBaseURL: h.fake.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err = h.catalog.TransitionConnectionState(h.ctx, h.org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "validate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.TransitionConnectionState(h.ctx, h.org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "activate"); err != nil {
		t.Fatal(err)
	}

	lm, err := h.catalog.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		OrgID:         &h.org.ID,
		CanonicalSlug: "acme/wasm-vision-20mb",
		ModalityKind:  catalog.ModalityChat,
		ContextWindow: 128000,
		BaseCapabilities: map[string]string{
			"vision":      "VERIFIED",
			"audio_input": "VERIFIED",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:              h.org.ID,
		ConnectionID:       conn.ID,
		LogicalModelID:     lm.ID,
		UpstreamModelName:  "acme-gpt-upstream",
		AssuranceLevel:     catalog.AssuranceVerified,
		State:              catalog.DeployStateActiveHealthy,
		ConnectTimeoutMs:   5000,
		FirstByteTimeoutMs: 5000,
		IdleChunkTimeoutMs: 5000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	// Sanity-verify the Wasm adapter directly loads and validates against runtime.
	if checkWasmRT, werr := directWasmCheck(h.ctx, h.fake.URL(), wasmBytes); werr != nil {
		t.Fatalf("direct wasm check failed: %v", werr)
	} else {
		_ = checkWasmRT.Close(h.ctx)
	}

	// Build a real 20 MiB base64 image payload (exceeds both 16 MiB Wasm linear memory and 4 MiB MaxGuestPayloadBytes).
	hugeBase64 := "data:image/png;base64," + strings.Repeat("QUFB", (20<<20)/4)
	if len(hugeBase64) < (20 << 20) {
		t.Fatalf("expected >= 20 MiB attachment, got %d bytes", len(hugeBase64))
	}

	// 1. Call POST /v1/chat/completions with 20 MiB attachment through 16 MiB Tier-2 Wasm sandbox.
	chatReqBody, _ := json.Marshal(map[string]any{
		"model": "acme/wasm-vision-20mb",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "describe image"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": hugeBase64}},
				},
			},
		},
	})
	h.fake.ResetRequests()
	chatResp := doIngressReq(t, h, http.MethodPost, "/v1/chat/completions", h.vkeyRaw, chatReqBody, nil)
	code, _, chatJSON := readJSONResp(t, chatResp)
	if code != http.StatusOK {
		t.Fatalf("AC-1 chat/completions expected 200 OK, got %d fakeReqs=%d body=%v", code, len(h.fake.Requests()), chatJSON)
	}
	recorded := h.fake.Requests()
	if len(recorded) != 1 || recorded[0].Path != "/v1/chat/completions" {
		t.Fatalf("expected 1 upstream /v1/chat/completions request, got %+v", recorded)
	}
	if recorded[0].Headers.Get("Authorization") != "Bearer "+h.secret {
		t.Fatalf("expected BlindBroker injected secret, got auth=%q", recorded[0].Headers.Get("Authorization"))
	}
	if !bytes.Contains(recorded[0].Body, []byte(hugeBase64[:4096])) || len(recorded[0].Body) < (20<<20) {
		t.Fatalf("expected full >=20 MiB spliced payload at FakeLLM upstream, got %d bytes", len(recorded[0].Body))
	}

	// 2. Call POST /v1/responses with the same 20 MiB attachment through 16 MiB Tier-2 Wasm sandbox.
	respReqBody, _ := json.Marshal(map[string]any{
		"model": "acme/wasm-vision-20mb",
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "inspect image"},
					map[string]any{"type": "input_image", "url": hugeBase64},
				},
			},
		},
	})
	h.fake.ResetRequests()
	respResp := doIngressReq(t, h, http.MethodPost, "/v1/responses", h.vkeyRaw, respReqBody, nil)
	code, _, respJSON := readJSONResp(t, respResp)
	if code != http.StatusOK || respJSON["object"] != "response" {
		t.Fatalf("AC-1 /v1/responses expected 200 object=response, got %d body=%v", code, respJSON)
	}
	if usageMap, ok := respJSON["usage"].(map[string]any); !ok || usageMap["input_tokens"] == nil || usageMap["output_tokens"] == nil || usageMap["prompt_tokens"] == nil {
		t.Fatalf("expected input_tokens/output_tokens/prompt_tokens in /v1/responses usage, got %v", respJSON["usage"])
	}
	recorded = h.fake.Requests()
	if len(recorded) != 1 || recorded[0].Path != "/v1/responses" {
		t.Fatalf("expected 1 upstream /v1/responses request, got %+v", recorded)
	}
	if len(recorded[0].Body) < (20 << 20) {
		t.Fatalf("expected full >=20 MiB spliced /v1/responses payload at FakeLLM upstream, got %d bytes", len(recorded[0].Body))
	}
}

func TestTP0016_AC2_ModelsFilteredToAuthorizedAllowlist(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// Add 3 LogicalModels in same org (gpt-4o-mini, text-embedding-3-small, secret-internal-model)
	// plus another org's model to prove multi-tenant + Virtual Key filtering.
	addModel := func(orgID, connID, slug, modality string, caps map[string]string) {
		lm, err := h.catalog.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
			OrgID:            &orgID,
			CanonicalSlug:    slug,
			ModalityKind:     modality,
			ContextWindow:    64000,
			MaxOutputTokens:  4096,
			BaseCapabilities: caps,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
			OrgID:             orgID,
			ConnectionID:      connID,
			LogicalModelID:    lm.ID,
			UpstreamModelName: slug,
			AssuranceLevel:    catalog.AssuranceVerified,
			State:             catalog.DeployStateActiveHealthy,
		}); err != nil {
			t.Fatal(err)
		}
	}
	addModel(h.org.ID, h.connID, "gpt-4o-mini", catalog.ModalityChat, map[string]string{"streaming": "VERIFIED", "tool_calling": "TESTED"})
	addModel(h.org.ID, h.connID, "text-embedding-3-small", catalog.ModalityEmbedding, map[string]string{"embeddings": "VERIFIED"})
	addModel(h.org.ID, h.connID, "secret-internal-model", catalog.ModalityChat, map[string]string{"streaming": "VERIFIED"})

	orgOther, err := h.store.Tenancy().CreateOrganization(h.ctx, storageport.CreateOrgParams{Slug: "org-other-models", DisplayName: "Other Org"})
	if err != nil {
		t.Fatal(err)
	}
	secOther, err := h.vault.SealSecret(h.ctx, catalogVaultSeal(orgOther.ID, "other-sec", h.secret))
	if err != nil {
		t.Fatal(err)
	}
	connA, okConnA := h.snapshots.Current().LookupProviderConnection(h.org.ID, h.connID)
	if !okConnA || connA == nil {
		t.Fatalf("missing org A connection")
	}
	connOther, err := h.catalog.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgOther.ID,
		AdapterID:       connA.AdapterID,
		Name:            "other-conn",
		EndpointBaseURL: h.fake.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings:  []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secOther.ID, InjectMode: catalog.InjectModeHostBlind}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connOther, err = h.catalog.TransitionConnectionState(h.ctx, orgOther.ID, connOther.ID, catalog.ConnStateValidating, connOther.Version, "val")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.TransitionConnectionState(h.ctx, orgOther.ID, connOther.ID, catalog.ConnStateActive, connOther.Version, "act"); err != nil {
		t.Fatal(err)
	}
	addModel(orgOther.ID, connOther.ID, "other-org-secret-model", catalog.ModalityChat, map[string]string{"streaming": "VERIFIED"})

	// Mint a Virtual Key restricted exclusively to gpt-4o-mini and text-embedding-3-small.
	vkRes, err := h.vkeys.Create(h.ctx, vkey.CreateParams{
		OrgID:         h.org.ID,
		ProjectID:     h.project.ID,
		EnvID:         h.env.ID,
		Name:          "scoped-models-key",
		AllowedPlanes: "PROVIDER",
		AllowedRoutes: []string{"/v1/*"},
		AllowedModels: []string{"gpt-4o-mini", "text-embedding-3-small"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	resp := doIngressReq(t, h, http.MethodGet, "/v1/models", vkRes.SecretKey, nil, nil)
	code, _, body := readJSONResp(t, resp)
	if code != http.StatusOK || body["object"] != "list" {
		t.Fatalf("GET /v1/models failed: status=%d body=%v", code, body)
	}
	data, _ := body["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("AC-2 expected exactly 2 authorized models, got %d: %v", len(data), data)
	}
	ids := map[string]map[string]any{}
	for _, rawItem := range data {
		item := rawItem.(map[string]any)
		ids[item["id"].(string)] = item
	}
	if _, ok := ids["gpt-4o-mini"]; !ok {
		t.Fatalf("missing gpt-4o-mini in /v1/models: %v", ids)
	}
	if _, ok := ids["text-embedding-3-small"]; !ok {
		t.Fatalf("missing text-embedding-3-small in /v1/models: %v", ids)
	}
	if _, ok := ids["secret-internal-model"]; ok {
		t.Fatalf("secret-internal-model leaked in /v1/models: %v", ids)
	}
	if _, ok := ids["acme/chat-1"]; ok {
		t.Fatalf("acme/chat-1 leaked outside VK allowlist: %v", ids)
	}
	if _, ok := ids["other-org-secret-model"]; ok {
		t.Fatalf("other-org-secret-model leaked across tenant boundary in /v1/models: %v", ids)
	}
	embCard := ids["text-embedding-3-small"]
	caps := embCard["capabilities"].(map[string]any)
	if caps["embeddings"] != true {
		t.Fatalf("expected embeddings=true in model capability metadata: %v", embCard)
	}
	if embCard["x_tanpopo_capabilities"] == nil {
		t.Fatalf("expected x_tanpopo_capabilities enrichment in model card: %v", embCard)
	}
}

func TestTP0016_NEG1_StrictCapabilityEnforcementCT08(t *testing.T) {
	h := setupIngressHarness(t, 0)
	h.fake.ResetRequests()

	assertCapRejected := func(name, path string, payload []byte) {
		t.Helper()
		start := time.Now()
		resp := doIngressReq(t, h, http.MethodPost, path, h.vkeyRaw, payload, nil)
		elapsed := time.Since(start)
		code, _, body := readJSONResp(t, resp)
		if code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400 capability_not_supported, got %d body=%v", name, code, body)
		}
		errMap, _ := body["error"].(map[string]any)
		if errMap["code"] != "capability_not_supported" {
			t.Fatalf("%s: expected capability_not_supported, got %v", name, body)
		}
		if elapsed > 50*time.Millisecond {
			t.Fatalf("%s: capability preflight rejection took %v (expected <2ms local preflight)", name, elapsed)
		}
	}

	// acme/chat-1 declares only streaming & tool_calling (not strict_json_schema, vision, embeddings).
	assertCapRejected("chat_strict_json_schema", "/v1/chat/completions", []byte(`{
		"model":"acme/chat-1",
		"messages":[{"role":"user","content":"hi"}],
		"response_format":{"type":"json_schema","json_schema":{"name":"out","strict":true}}
	}`))

	assertCapRejected("responses_strict_json_schema", "/v1/responses", []byte(`{
		"model":"acme/chat-1",
		"input":"hi",
		"text":{"format":{"type":"json_schema","json_schema":{"name":"out","strict":true}}}
	}`))

	// Add a model supporting json_object (without strict_json_schema) to prove flat Responses
	// text.format.strict=true does not bypass CT-08 by falling back to json_object.
	jsonLM, err := h.catalog.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		OrgID:            &h.org.ID,
		CanonicalSlug:    "acme/json-obj-only",
		ModalityKind:     catalog.ModalityChat,
		ContextWindow:    64000,
		BaseCapabilities: map[string]string{"streaming": "VERIFIED", "json_object": "VERIFIED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:             h.org.ID,
		ConnectionID:      h.connID,
		LogicalModelID:    jsonLM.ID,
		UpstreamModelName: "acme-json-only",
		AssuranceLevel:    catalog.AssuranceVerified,
		State:             catalog.DeployStateActiveHealthy,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	assertCapRejected("responses_flat_strict_json_schema", "/v1/responses", []byte(`{
		"model":"acme/json-obj-only",
		"input":"hi",
		"text":{"format":{"type":"json_schema","name":"out","strict":true,"schema":{"type":"object"}}}
	}`))

	assertCapRejected("responses_vision", "/v1/responses", []byte(`{
		"model":"acme/chat-1",
		"input":[{"role":"user","content":[{"type":"input_image","url":"data:image/png;base64,aGk="}]}]
	}`))

	assertCapRejected("chat_audio_part", "/v1/chat/completions", []byte(`{
		"model":"acme/chat-1",
		"messages":[{"role":"user","content":[{"type":"audio","audio":{"url":"https://example.com/a.wav"}}]}]
	}`))

	assertCapRejected("chat_document_part", "/v1/chat/completions", []byte(`{
		"model":"acme/chat-1",
		"messages":[{"role":"user","content":[{"type":"document","document":{"url":"https://example.com/doc.pdf"}}]}]
	}`))

	assertCapRejected("embeddings_unsupported", "/v1/embeddings", []byte(`{
		"model":"acme/chat-1",
		"input":"hello"
	}`))

	// Duplicate/case-colliding JSON keys must be rejected 400 with 0 upstream calls.
	for _, tc := range []struct {
		name string
		path string
		body []byte
	}{
		{"chat_dup_tools", "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function"}],"Tools":null}`)},
		{"responses_dup_content", "/v1/responses", []byte(`{"model":"acme/chat-1","input":[{"role":"user","content":[{"type":"input_image","url":"data:image/png;base64,aGk="}],"Content":"hi"}]}`)},
	} {
		resp := doIngressReq(t, h, http.MethodPost, tc.path, h.vkeyRaw, tc.body, nil)
		code, _, body := readJSONResp(t, resp)
		if code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400 invalid_request_error on duplicate/case-colliding key, got %d body=%v", tc.name, code, body)
		}
	}

	if got := len(h.fake.Requests()); got != 0 {
		t.Fatalf("CT-08 expected 0 upstream egress calls on capability/strict-JSON rejection, got %d", got)
	}
}

func TestTP0016_NEG2_MultimodalSSRFDefense(t *testing.T) {
	h := setupIngressHarness(t, 0)
	// Add a vision-capable model so CT-08 allows reaching blob offloading.
	lm, err := h.catalog.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		OrgID:            &h.org.ID,
		CanonicalSlug:    "acme/vision-ssrf",
		ModalityKind:     catalog.ModalityChat,
		ContextWindow:    128000,
		BaseCapabilities: map[string]string{"vision": "VERIFIED", "streaming": "VERIFIED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:             h.org.ID,
		ConnectionID:      h.connID,
		LogicalModelID:    lm.ID,
		UpstreamModelName: "acme-vision",
		AssuranceLevel:    catalog.AssuranceVerified,
		State:             catalog.DeployStateActiveHealthy,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	ssrfPayload := []byte(`{
		"model":"acme/vision-ssrf",
		"input":[{"role":"user","content":[{"type":"input_image","url":"http://169.254.169.254/latest/meta-data/"}]}]
	}`)

	// 1. Default policy disables remote media URLs -> 400 remote_media_url_disabled.
	h.fake.ResetRequests()
	resp1 := doIngressReq(t, h, http.MethodPost, "/v1/responses", h.vkeyRaw, ssrfPayload, nil)
	code1, _, body1 := readJSONResp(t, resp1)
	if code1 != http.StatusBadRequest || body1["error"].(map[string]any)["code"] != "remote_media_url_disabled" {
		t.Fatalf("NEG-2 expected 400 remote_media_url_disabled, got %d body=%v", code1, body1)
	}
	if len(h.fake.Requests()) != 0 {
		t.Fatalf("expected 0 upstream calls on remote_media_url_disabled")
	}

	// 2. Policy enabled without AllowedHosts MUST fail closed (403 ssrf_blocked_ip) even for non-metadata targets,
	// and explicitly configuring AllowedHosts without AllowPrivateIP also blocks metadata IPs (403 ssrf_blocked_ip).
	for _, pol := range []blob.RemoteMediaPolicy{
		{Enabled: true},
		{Enabled: true, AllowedHosts: []string{"169.254.169.254"}, AllowedPorts: []int{80}},
	} {
		srvEnabled, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots:         h.snapshots,
			Broker:            h.broker,
			PluginsDir:        h.pluginsD,
			Audit:             h.audit,
			RemoteMediaPolicy: pol,
		})
		if err != nil {
			t.Fatal(err)
		}
		httpEnabled := httptest.NewServer(srvEnabled)

		req, _ := http.NewRequest(http.MethodPost, httpEnabled.URL+"/v1/responses", bytes.NewReader(ssrfPayload))
		req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		req.Header.Set("Content-Type", "application/json")
		resp2, err := h.client.Do(req)
		if err != nil {
			httpEnabled.Close()
			_ = srvEnabled.Close(context.Background())
			t.Fatal(err)
		}
		code2, _, body2 := readJSONResp(t, resp2)
		httpEnabled.Close()
		_ = srvEnabled.Close(context.Background())
		if code2 != http.StatusForbidden || body2["error"].(map[string]any)["code"] != "ssrf_blocked_ip" {
			t.Fatalf("NEG-2 expected 403 ssrf_blocked_ip for policy=%+v, got %d body=%v", pol, code2, body2)
		}
	}
}

func TestTP0016_NEG3_IdempotencyIsolationConcurrencyAndSQLite(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// Re-mount Server with SQLite IdempotencyDB enabled to test persistence too.
	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:     h.snapshots,
		Broker:        h.broker,
		PluginsDir:    h.pluginsD,
		Audit:         h.audit,
		IdempotencyDB: h.store.RawDB(),
		NowMs:         func() int64 { return h.clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	// Provision Org B with identical Virtual Key permissions and model slug.
	orgB, err := h.store.Tenancy().CreateOrganization(h.ctx, storageport.CreateOrgParams{Slug: "org-b-idem", DisplayName: "Org B"})
	if err != nil {
		t.Fatal(err)
	}
	projB, err := h.store.Tenancy().CreateProject(h.ctx, orgB.ID, storageport.CreateProjectParams{Slug: "proj-b", DisplayName: "Proj B"})
	if err != nil {
		t.Fatal(err)
	}
	envB, err := h.store.Tenancy().CreateEnvironment(h.ctx, orgB.ID, projB.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	secB, err := h.vault.SealSecret(h.ctx, catalogVaultSeal(orgB.ID, "org-b-key", h.secret))
	if err != nil {
		t.Fatal(err)
	}
	// Reuse existing adapter by reading conn's AdapterID from snapshot.
	connA, okConnA := h.snapshots.Current().LookupProviderConnection(h.org.ID, h.connID)
	if !okConnA || connA == nil {
		t.Fatalf("expected existing org A connection %q in snapshot", h.connID)
	}
	connB, err := h.catalog.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgB.ID,
		AdapterID:       connA.AdapterID,
		Name:            "org-b-conn",
		EndpointBaseURL: h.fake.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings:  []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secB.ID, InjectMode: catalog.InjectModeHostBlind}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connB, err = h.catalog.TransitionConnectionState(h.ctx, orgB.ID, connB.ID, catalog.ConnStateValidating, connB.Version, "val")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.TransitionConnectionState(h.ctx, orgB.ID, connB.ID, catalog.ConnStateActive, connB.Version, "act"); err != nil {
		t.Fatal(err)
	}
	lmB, err := h.catalog.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		OrgID:            &orgB.ID,
		CanonicalSlug:    "acme/chat-1",
		ModalityKind:     catalog.ModalityChat,
		ContextWindow:    128000,
		BaseCapabilities: map[string]string{"streaming": "VERIFIED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:             orgB.ID,
		ConnectionID:      connB.ID,
		LogicalModelID:    lmB.ID,
		UpstreamModelName: "org-b-upstream",
		AssuranceLevel:    catalog.AssuranceVerified,
		State:             catalog.DeployStateActiveHealthy,
	}); err != nil {
		t.Fatal(err)
	}
	vkB, err := h.vkeys.Create(h.ctx, vkey.CreateParams{
		OrgID:         orgB.ID,
		ProjectID:     projB.ID,
		EnvID:         envB.ID,
		Name:          "org-b-vk",
		AllowedPlanes: "PROVIDER",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	tryCallURL := func(url, tok, idemKey string, body []byte) (int, http.Header, map[string]any, error) {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return 0, nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		if idemKey != "" {
			req.Header.Set("Idempotency-Key", idemKey)
		}
		resp, err := h.client.Do(req)
		if err != nil {
			return 0, nil, nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		raw, rerr := ioReadAllBounded(resp)
		if rerr != nil {
			return 0, nil, nil, rerr
		}
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return resp.StatusCode, resp.Header, m, nil
	}
	callURL := func(url, tok, idemKey string, body []byte) (int, http.Header, map[string]any) {
		t.Helper()
		c, hdr, m, err := tryCallURL(url, tok, idemKey, body)
		if err != nil {
			t.Fatal(err)
		}
		return c, hdr, m
	}

	payloadA := []byte(`{"model":"acme/chat-1","input":"hello-org-a"}`)
	payloadDiff := []byte(`{"model":"acme/chat-1","input":"modified-payload"}`)

	// 1. First request from Org A executes upstream once.
	h.fake.ResetRequests()
	code, hdr, resA1 := callURL(ts.URL+"/v1/responses", h.vkeyRaw, "req-123", payloadA)
	if code != http.StatusOK || hdr.Get("Idempotent-Replayed") != "" {
		t.Fatalf("expected fresh 200 without Idempotent-Replayed, got %d hdr=%v body=%v", code, hdr, resA1)
	}
	if len(h.fake.Requests()) != 1 {
		t.Fatalf("expected 1 upstream call")
	}

	// 2. Duplicate retry from Org A replays deterministically with 0 new upstream calls.
	code, hdr, resA2 := callURL(ts.URL+"/v1/responses", h.vkeyRaw, "req-123", payloadA)
	if code != http.StatusOK || hdr.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("expected 200 Idempotent-Replayed: true, got %d hdr=%v body=%v", code, hdr, resA2)
	}
	if len(h.fake.Requests()) != 1 {
		t.Fatalf("expected still only 1 upstream call after replay, got %d", len(h.fake.Requests()))
	}

	// 3. Reusing same Idempotency-Key with modified payload -> 422 idempotency_key_reused_mismatch.
	code, _, resMismatch := callURL(ts.URL+"/v1/responses", h.vkeyRaw, "req-123", payloadDiff)
	if code != http.StatusUnprocessableEntity || resMismatch["error"].(map[string]any)["code"] != "idempotency_key_reused_mismatch" {
		t.Fatalf("expected 422 idempotency_key_reused_mismatch, got %d body=%v", code, resMismatch)
	}

	// 4. NEG-3 Cross-tenant isolation: Org B sending identical Idempotency-Key "req-123" MUST hit Org B upstream, NOT replay Org A!
	code, hdr, resB := callURL(ts.URL+"/v1/responses", vkB.SecretKey, "req-123", payloadA)
	if code != http.StatusOK || hdr.Get("Idempotent-Replayed") != "" {
		t.Fatalf("NEG-3 expected fresh Org B execution (no cross-tenant replay), got %d hdr=%v body=%v", code, hdr, resB)
	}
	if resB["model"] != "org-b-upstream" || len(h.fake.Requests()) != 2 {
		t.Fatalf("NEG-3 expected Org B upstream call to org-b-upstream, got body=%v reqs=%d", resB, len(h.fake.Requests()))
	}

	// 5. Concurrent in-flight request returns 409 request_in_progress using FakeLLM first-byte delay.
	h.fake.EnqueueFault("delay_first_byte_ms=250")
	type callResult struct {
		code int
		err  error
	}
	errCh := make(chan callResult, 1)
	go func() {
		c, _, _, cerr := tryCallURL(ts.URL+"/v1/responses", h.vkeyRaw, "req-inflight-1", payloadA)
		errCh <- callResult{code: c, err: cerr}
	}()
	// Wait slightly so first request enters FakeLLM delay_first_byte_ms after claiming IN_FLIGHT.
	time.Sleep(40 * time.Millisecond)
	confCode, _, confBody := callURL(ts.URL+"/v1/responses", h.vkeyRaw, "req-inflight-1", payloadA)
	if confCode != http.StatusConflict || confBody["error"].(map[string]any)["code"] != "request_in_progress" {
		t.Fatalf("expected 409 request_in_progress for concurrent duplicate, got %d body=%v", confCode, confBody)
	}
	firstRes := <-errCh
	if firstRes.err != nil {
		t.Fatalf("initial concurrent request errored: %v", firstRes.err)
	}
	if firstRes.code != http.StatusOK {
		t.Fatalf("expected initial concurrent request to settle 200, got %d", firstRes.code)
	}
}

func TestTP0016_ResponsesSSEAndEmbeddingsAndRollbackToggles(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// Add an embedding model with custom_dimensions verified.
	embLM, err := h.catalog.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		OrgID:         &h.org.ID,
		CanonicalSlug: "acme/embed-1",
		ModalityKind:  catalog.ModalityEmbedding,
		ContextWindow: 8192,
		BaseCapabilities: map[string]string{
			"embeddings":        "VERIFIED",
			"custom_dimensions": "VERIFIED",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:             h.org.ID,
		ConnectionID:      h.connID,
		LogicalModelID:    embLM.ID,
		UpstreamModelName: "acme-embed-upstream",
		AssuranceLevel:    catalog.AssuranceVerified,
		State:             catalog.DeployStateActiveHealthy,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	// 1. Streaming /v1/responses normalizes OpenAI Responses SSE events including response.completed usage.
	sseResp := doIngressReq(t, h, http.MethodPost, "/v1/responses", h.vkeyRaw, []byte(`{
		"model":"acme/chat-1",
		"input":"stream something",
		"stream":true
	}`), map[string]string{fakellm.StreamChunksHeader: "2"})
	rawSSE, err := ioReadAllBounded(sseResp)
	_ = sseResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if sseResp.StatusCode != http.StatusOK || !strings.Contains(sseResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected 200 text/event-stream on /v1/responses stream, got %d hdr=%v body=%s", sseResp.StatusCode, sseResp.Header, string(rawSSE))
	}
	if !strings.Contains(string(rawSSE), "event: response.output_text.delta") || !strings.Contains(string(rawSSE), "event: response.completed") {
		t.Fatalf("expected normalized response.output_text.delta and response.completed events, got:\n%s", string(rawSSE))
	}

	// 2. Embeddings happy path with dimensions & token array input.
	embResp := doIngressReq(t, h, http.MethodPost, "/v1/embeddings", h.vkeyRaw, []byte(`{
		"model":"acme/embed-1",
		"input":[101,102,103],
		"encoding_format":"float",
		"dimensions":256
	}`), nil)
	code, _, embJSON := readJSONResp(t, embResp)
	if code != http.StatusOK || embJSON["object"] != "list" {
		t.Fatalf("expected 200 object=list on /v1/embeddings, got %d body=%v", code, embJSON)
	}

	// 3. Embeddings invalid dimensions / encoding_format / empty input -> 400.
	badEmb := doIngressReq(t, h, http.MethodPost, "/v1/embeddings", h.vkeyRaw, []byte(`{
		"model":"acme/embed-1",
		"input":"hello",
		"dimensions":0
	}`), nil)
	badCode, _, _ := readJSONResp(t, badEmb)
	if badCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-positive dimensions, got %d", badCode)
	}

	// 4. Disallowed HTTP method on /v1/embeddings returns 405 MethodNotAllowed (CT-09 malformed embeddings covered in engine_test.go).
	badMethod := doIngressReq(t, h, http.MethodGet, "/v1/embeddings", h.vkeyRaw, nil, nil)
	mCode, _, _ := readJSONResp(t, badMethod)
	if mCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 MethodNotAllowed on GET /v1/embeddings, got %d", mCode)
	}

	// 5. Rollback endpoint toggles independently disable new routes without affecting /v1/chat/completions.
	toggledSrv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:         h.snapshots,
		Broker:            h.broker,
		PluginsDir:        h.pluginsD,
		Audit:             h.audit,
		DisableResponses:  true,
		DisableEmbeddings: true,
		DisableModels:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = toggledSrv.Close(context.Background()) })
	tsToggle := httptest.NewServer(toggledSrv)
	t.Cleanup(tsToggle.Close)

	for _, ep := range []struct{ method, path string }{
		{http.MethodPost, "/v1/responses"},
		{http.MethodPost, "/v1/embeddings"},
		{http.MethodGet, "/v1/models"},
	} {
		req, _ := http.NewRequest(ep.method, tsToggle.URL+ep.path, nil)
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		rc, _, _ := readJSONResp(t, resp)
		if rc != http.StatusNotFound {
			t.Fatalf("expected 404 when %s disabled by rollback toggle, got %d", ep.path, rc)
		}
	}
}

func directWasmCheck(ctx context.Context, fakeBaseURL string, wasmBytes []byte) (*wasm.Runtime, error) {
	var portNum int
	if idx := strings.LastIndex(fakeBaseURL, ":"); idx >= 0 {
		for i := idx + 1; i < len(fakeBaseURL); i++ {
			c := fakeBaseURL[i]
			if c >= '0' && c <= '9' {
				portNum = portNum*10 + int(c-'0')
			}
		}
	}
	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:           wasmBytes,
		MemoryLimitPages:    wasm.DefaultMemoryLimitPages,
		AllowedHosts:        []string{"127.0.0.1"},
		AllowedPorts:        []int{portNum},
		AllowPrivateIP:      true,
		AllowedCIDRs:        []string{"127.0.0.1/32"},
		AllowInsecureHTTP:   true,
		ApprovedSecretSlots: []string{"api_key"},
	})
	if err != nil {
		return nil, err
	}
	spec, err := rt.TransformRequest(ctx, port.TransformRequestInput{
		RequestID:             "req_dbg",
		ConfigJSON:            `{"endpoint_base_url":"` + fakeBaseURL + `"}`,
		NormalizedRequestJSON: []byte(`{"model":"m"}`),
	})
	if err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}
	if string(spec.BodyBytes) != `{"model":"m"}` {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("unexpected spec.BodyBytes: %q", string(spec.BodyBytes))
	}
	nresp, err := rt.TransformResponse(ctx, port.TransformResponseInput{
		RequestID:       "req_dbg",
		HTTPStatus:      200,
		RawResponseBody: []byte(`{"id":"c1","choices":[{"message":{"content":"hi"}}]}`),
		Modality:        "CHAT",
	})
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("TransformResponse failed: %w", err)
	}
	if len(nresp.NormalizedResponseJSON) == 0 {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("empty NormalizedResponseJSON")
	}
	return rt, nil
}

func catalogVaultSeal(orgID, name, secret string) vault.SealSecretParams {
	return vault.SealSecretParams{
		OrgID:      orgID,
		Name:       name,
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte(secret),
	}
}
