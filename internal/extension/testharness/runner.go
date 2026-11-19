// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package testharness

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
)

const cassetteSchemaAssetPath = "plugin-schema/v1/cassette.schema.json"

// Cassette defines a deterministic offline contract test case matching cassette.schema.json.
type Cassette struct {
	CaseID                   string                   `yaml:"case_id" json:"case_id"`
	Description              string                   `yaml:"description,omitempty" json:"description,omitempty"`
	Capability               string                   `yaml:"capability" json:"capability"`
	Config                   map[string]any           `yaml:"config,omitempty" json:"config,omitempty"`
	SecretSlots              map[string]string        `yaml:"secret_slots,omitempty" json:"secret_slots,omitempty"`
	NormalizedInput          map[string]any           `yaml:"normalized_input" json:"normalized_input"`
	UpstreamRecordedExchange UpstreamRecordedExchange `yaml:"upstream_recorded_exchange" json:"upstream_recorded_exchange"`
	ExpectedNormalizedOutput *ExpectedOutput          `yaml:"expected_normalized_output,omitempty" json:"expected_normalized_output,omitempty"`
	ExpectedCanonicalError   *ExpectedCanonicalError  `yaml:"expected_canonical_error,omitempty" json:"expected_canonical_error,omitempty"`
}

// UpstreamRecordedExchange stores the offline HTTP response or split SSE chunks to replay.
type UpstreamRecordedExchange struct {
	Status                  int               `yaml:"status" json:"status"`
	Headers                 map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	ExpectedUpstreamHeaders map[string]string `yaml:"expected_upstream_headers,omitempty" json:"expected_upstream_headers,omitempty"`
	RawBody                 string            `yaml:"raw_body,omitempty" json:"raw_body,omitempty"`
	SSEFrames               []string          `yaml:"sse_frames,omitempty" json:"sse_frames,omitempty"`
	RawSSEChunks            []string          `yaml:"raw_sse_chunks,omitempty" json:"raw_sse_chunks,omitempty"`
	AbortAfterChunks        int               `yaml:"abort_after_chunks,omitempty" json:"abort_after_chunks,omitempty"`
}

// ExpectedOutput holds contract assertions for 2xx sync or streaming responses.
type ExpectedOutput struct {
	Model                            string `yaml:"model,omitempty" json:"model,omitempty"`
	FinishReason                     string `yaml:"finish_reason,omitempty" json:"finish_reason,omitempty"`
	Content                          string `yaml:"content,omitempty" json:"content,omitempty"`
	ContentContains                  string `yaml:"content_contains,omitempty" json:"content_contains,omitempty"`
	ToolCallsCount                   *int   `yaml:"tool_calls_count,omitempty" json:"tool_calls_count,omitempty"`
	ToolCallName                     string `yaml:"tool_call_name,omitempty" json:"tool_call_name,omitempty"`
	ToolCallArguments                string `yaml:"tool_call_arguments,omitempty" json:"tool_call_arguments,omitempty"`
	RequireStreamCompleted           bool   `yaml:"require_stream_completed,omitempty" json:"require_stream_completed,omitempty"`
	RequireUsage                     bool   `yaml:"require_usage,omitempty" json:"require_usage,omitempty"`
	InputTokens                      *int64 `yaml:"input_tokens,omitempty" json:"input_tokens,omitempty"`
	OutputTokens                     *int64 `yaml:"output_tokens,omitempty" json:"output_tokens,omitempty"`
	CacheReadTokens                  *int64 `yaml:"cache_read_tokens,omitempty" json:"cache_read_tokens,omitempty"`
	ReasoningTokens                  *int64 `yaml:"reasoning_tokens,omitempty" json:"reasoning_tokens,omitempty"`
	RerankResultsCount               *int   `yaml:"rerank_results_count,omitempty" json:"rerank_results_count,omitempty"`
	RerankFirstIndex                 *int   `yaml:"rerank_first_index,omitempty" json:"rerank_first_index,omitempty"`
	RerankSearchUnits                *int32 `yaml:"rerank_search_units,omitempty" json:"rerank_search_units,omitempty"`
	RequireStreamOptionsIncludeUsage bool   `yaml:"require_stream_options_include_usage,omitempty" json:"require_stream_options_include_usage,omitempty"`
}

// ExpectedCanonicalError holds contract assertions for 4xx/5xx normalized error responses.
type ExpectedCanonicalError struct {
	CanonicalCode         string   `yaml:"canonical_code" json:"canonical_code"`
	ErrorType             string   `yaml:"error_type,omitempty" json:"error_type,omitempty"`
	RetryablePreStream    *bool    `yaml:"retryable_pre_stream,omitempty" json:"retryable_pre_stream,omitempty"`
	TriggerReauth         *bool    `yaml:"trigger_reauth,omitempty" json:"trigger_reauth,omitempty"`
	MessageMustNotContain []string `yaml:"message_must_not_contain,omitempty" json:"message_must_not_contain,omitempty"`
}

// TestOptions configures offline cassette replay execution.
type TestOptions struct {
	Dir          string
	CassettePath string
}

// CaseResult summarizes one executed cassette contract test.
type CaseResult struct {
	CaseID     string `json:"case_id" yaml:"case_id"`
	Capability string `json:"capability" yaml:"capability"`
	Status     string `json:"status" yaml:"status"`
	DurationMs int64  `json:"duration_ms" yaml:"duration_ms"`
	Error      string `json:"error,omitempty" yaml:"error,omitempty"`
	ExitCode   int    `json:"exit_code" yaml:"exit_code"`
}

// TestReport summarizes offline extension contract test results and promoted capability badges.
type TestReport struct {
	ExtensionID      string            `json:"extension_id" yaml:"extension_id"`
	Version          string            `json:"version" yaml:"version"`
	ExecutionTier    string            `json:"execution_tier" yaml:"execution_tier"`
	Status           string            `json:"status" yaml:"status"`
	ExitCode         int               `json:"exit_code" yaml:"exit_code"`
	CasesPassed      int               `json:"cases_passed" yaml:"cases_passed"`
	CasesFailed      int               `json:"cases_failed" yaml:"cases_failed"`
	DurationMs       int64             `json:"duration_ms" yaml:"duration_ms"`
	CapabilityBadges map[string]string `json:"capability_badges" yaml:"capability_badges"`
	SchemaRef        string            `json:"schema_ref" yaml:"schema_ref"`
	Cases            []CaseResult      `json:"cases" yaml:"cases"`
}

// TestExtension validates an extension directory, instantiates its isolated Tier-1/Tier-2 runtime,
// replays offline contract cassettes strictly inside loopback (127.0.0.1), scans all outputs
// with SecretLeakScanner, and promotes verified capabilities from DECLARED to TESTED.
func TestExtension(ctx context.Context, opts TestOptions) (*TestReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()

	valReport, vm, wasmBytes, err := ValidateExtensionDir(ctx, opts.Dir)
	if err != nil {
		return nil, err
	}
	absDir, err := filepath.EvalSymlinks(opts.Dir)
	if err != nil {
		return nil, validationErr("resolve extension directory", err)
	}

	rt, closeRT, err := buildIsolatedRuntime(ctx, absDir, vm, wasmBytes)
	if err != nil {
		return nil, err
	}
	defer closeRT()

	return TestExtensionWithRuntime(ctx, opts, valReport, vm, rt, start)
}

// TestExtensionWithRuntime executes offline cassette replay against an explicitly provided
// ProviderRuntime (used both by TestExtension and unit tests covering buggy/leaking adapters).
func TestExtensionWithRuntime(
	ctx context.Context,
	opts TestOptions,
	valReport *ValidationReport,
	vm *installer.ValidatedManifest,
	rt port.ProviderRuntime,
	start time.Time,
) (*TestReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if start.IsZero() {
		start = time.Now()
	}

	absDir, err := filepath.EvalSymlinks(opts.Dir)
	if err != nil {
		return nil, validationErr("resolve extension directory", err)
	}
	cassettePaths, err := discoverCassettes(absDir, opts.CassettePath)
	if err != nil {
		return nil, err
	}

	cassettes := make([]Cassette, 0, len(cassettePaths))
	for _, p := range cassettePaths {
		c, cerr := loadAndValidateCassette(p)
		if cerr != nil {
			return nil, cerr
		}
		cassettes = append(cassettes, c)
	}

	badges := make(map[string]string, len(valReport.CapabilityBadges))
	for k, v := range valReport.CapabilityBadges {
		badges[k] = v
	}

	capStats := make(map[string]struct{ passed, failed int })
	results := make([]CaseResult, 0, len(cassettes))
	var firstErr error
	passed := 0
	failed := 0

	client := newHermeticLoopbackClient()
	if tr, ok := client.Transport.(*http.Transport); ok {
		defer tr.CloseIdleConnections()
	}

	for _, cas := range cassettes {
		cStart := time.Now()
		cerr := runCassetteCase(ctx, vm, rt, client, cas)
		dur := time.Since(cStart).Milliseconds()
		st := capStats[cas.Capability]
		if cerr != nil {
			failed++
			st.failed++
			capStats[cas.Capability] = st
			code := CodeOf(cerr)
			if firstErr == nil {
				firstErr = cerr
			}
			results = append(results, CaseResult{
				CaseID:     SanitizeTerminalText(cas.CaseID),
				Capability: cas.Capability,
				Status:     "FAILED",
				DurationMs: dur,
				Error:      SanitizeTerminalText(cerr.Error()),
				ExitCode:   code,
			})
		} else {
			passed++
			st.passed++
			capStats[cas.Capability] = st
			results = append(results, CaseResult{
				CaseID:     SanitizeTerminalText(cas.CaseID),
				Capability: cas.Capability,
				Status:     "PASSED",
				DurationMs: dur,
				ExitCode:   ExitOK,
			})
		}
	}

	for capName, st := range capStats {
		if st.passed > 0 && st.failed == 0 {
			badges[capName] = "TESTED"
		}
	}

	overallStatus := "PASSED"
	exitCode := ExitOK
	if firstErr != nil {
		overallStatus = "FAILED"
		exitCode = CodeOf(firstErr)
	}

	report := &TestReport{
		ExtensionID:      valReport.ExtensionID,
		Version:          valReport.Version,
		ExecutionTier:    valReport.ExecutionTier,
		Status:           overallStatus,
		ExitCode:         exitCode,
		CasesPassed:      passed,
		CasesFailed:      failed,
		DurationMs:       time.Since(start).Milliseconds(),
		CapabilityBadges: badges,
		SchemaRef:        cassetteSchemaAssetPath,
		Cases:            results,
	}
	return report, firstErr
}

func buildIsolatedRuntime(ctx context.Context, absDir string, vm *installer.ValidatedManifest, wasmBytes []byte) (port.ProviderRuntime, func(), error) {
	switch vm.ExecutionTier {
	case installer.ExecutionTierDeclarativeT1:
		raw, err := os.ReadFile(filepath.Join(absDir, "manifest.yaml"))
		if err != nil {
			return nil, nil, validationErr("read manifest.yaml", err)
		}
		eng, err := declarative.ParseDeclarativeManifestYAML(raw)
		if err != nil {
			return nil, nil, validationErr("compile declarative runtime", err)
		}
		return eng, func() {}, nil
	case installer.ExecutionTierWasmSandboxT2:
		memPages := uint32(wasm.DefaultMemoryLimitPages)
		allowExt := false
		var callTimeout, chunkTimeout time.Duration
		if vm.Manifest.Runtime != nil && vm.Manifest.Runtime.Quotas != nil {
			if vm.Manifest.Runtime.Quotas.MaxMemoryMib > 0 {
				reqPages := uint32(vm.Manifest.Runtime.Quotas.MaxMemoryMib) * 16
				if reqPages > wasm.DefaultMemoryLimitPages {
					allowExt = true
				}
				memPages = reqPages
			}
			if vm.Manifest.Runtime.Quotas.MaxCpuMsPerCall > 0 {
				callTimeout = time.Duration(vm.Manifest.Runtime.Quotas.MaxCpuMsPerCall) * time.Millisecond
			}
			if vm.Manifest.Runtime.Quotas.MaxStreamChunkCpuMs > 0 {
				chunkTimeout = time.Duration(vm.Manifest.Runtime.Quotas.MaxStreamChunkCpuMs) * time.Millisecond
			}
		}
		var slots []string
		for _, s := range vm.Manifest.SecretSlots {
			slots = append(slots, s.Slot)
		}
		allowedHosts, allowPriv, allowedCIDRs := deriveManifestWasmEgressConfig(vm.Manifest.Permissions.NetworkEgress, false)
		rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
			WasmBytes:           wasmBytes,
			InstallationID:      "test:" + vm.Manifest.Metadata.ID,
			PoolSize:            4,
			MemoryLimitPages:    memPages,
			AllowExtendedMemory: allowExt,
			CallTimeout:         callTimeout,
			StreamChunkTimeout:  chunkTimeout,
			ABIMode:             wasm.ABIModeCanonicalEnvelope,
			AllowedHosts:        allowedHosts,
			AllowPrivateIP:      allowPriv,
			AllowedCIDRs:        allowedCIDRs,
			AllowInsecureHTTP:   !vm.Manifest.Permissions.NetworkEgress.RequireTLS,
			ApprovedSecretSlots: slots,
		})
		if err != nil {
			return nil, nil, validationErr("initialize wasm runtime", err)
		}
		return rt, func() { _ = rt.Close(context.Background()) }, nil
	default:
		return nil, nil, validationErr("unsupported execution tier", nil)
	}
}

func discoverCassettes(absDir, singlePath string) ([]string, error) {
	if strings.TrimSpace(singlePath) != "" {
		fi, err := os.Lstat(singlePath)
		if err != nil {
			return nil, validationErr(fmt.Sprintf("cassette %q not found", singlePath), err)
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			return nil, securityErr(fmt.Sprintf("cassette %q must be a regular non-symlink file", singlePath), nil)
		}
		return []string{singlePath}, nil
	}
	cDir := filepath.Join(absDir, "cassettes")
	entries, err := os.ReadDir(cDir)
	if err != nil {
		return nil, validationErr("cassettes/ directory missing in extension package", err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".yaml" || ext == ".yml" || ext == ".json" {
			paths = append(paths, filepath.Join(cDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, validationErr("no cassette files (*.yaml|*.yml|*.json) found in cassettes/", nil)
	}
	return paths, nil
}

func loadAndValidateCassette(path string) (Cassette, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Cassette{}, validationErr(fmt.Sprintf("read cassette %q", path), err)
	}
	var cas Cassette
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cas); err != nil {
		return Cassette{}, validationErr(fmt.Sprintf("cassette %q violates cassette.schema.json", filepath.Base(path)), err)
	}
	if strings.TrimSpace(cas.CaseID) == "" {
		return Cassette{}, validationErr(fmt.Sprintf("cassette %q missing case_id", filepath.Base(path)), nil)
	}
	if !isSupportedCassetteCapability(cas.Capability) {
		return Cassette{}, validationErr(fmt.Sprintf("cassette %q has unsupported capability %q", cas.CaseID, cas.Capability), nil)
	}
	if cas.NormalizedInput == nil {
		return Cassette{}, validationErr(fmt.Sprintf("cassette %q missing normalized_input", cas.CaseID), nil)
	}
	ex := cas.UpstreamRecordedExchange
	if ex.Status < 100 || ex.Status > 599 {
		return Cassette{}, validationErr(fmt.Sprintf("cassette %q invalid status %d", cas.CaseID, ex.Status), nil)
	}
	if ex.RawBody == "" && len(ex.SSEFrames) == 0 && len(ex.RawSSEChunks) == 0 {
		return Cassette{}, validationErr(fmt.Sprintf("cassette %q upstream_recorded_exchange requires raw_body, sse_frames, or raw_sse_chunks", cas.CaseID), nil)
	}
	if cas.ExpectedNormalizedOutput == nil && cas.ExpectedCanonicalError == nil {
		return Cassette{}, validationErr(fmt.Sprintf("cassette %q requires expected_normalized_output or expected_canonical_error", cas.CaseID), nil)
	}
	return cas, nil
}

func isSupportedCassetteCapability(c string) bool {
	switch c {
	case "chat_completion", "chat_stream", "tool_calling", "error_normalization",
		"validation_probe", "native_passthrough", "usage_extraction", "guardrail", "rerank":
		return true
	default:
		return false
	}
}

func cassetteModality(capability string) string {
	if capability == "rerank" {
		return "RERANK"
	}
	return "text"
}

func cassetteConfigWithModality(raw []byte, capability string) string {
	if capability != "rerank" {
		return string(raw)
	}
	var cfg map[string]any
	if json.Unmarshal(raw, &cfg) != nil || cfg == nil {
		cfg = map[string]any{}
	}
	cfg["_tanpopo_modality"] = "rerank"
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return string(raw)
	}
	return string(encoded)
}

func resolveCassetteSecrets(rawSlots map[string]string) (resolved map[string]string, slotRefs map[string]string, scanner *SecretLeakScanner) {
	resolved = make(map[string]string, len(rawSlots))
	slotRefs = make(map[string]string, len(rawSlots))
	var secValues []string
	for slot, val := range rawSlots {
		actual := val
		if actual == "" || actual == canaryPlaceholder {
			actual = defaultHarnessCanary()
		}
		resolved[slot] = actual
		slotRefs[slot] = "slot_ref:" + slot
		secValues = append(secValues, actual)
	}
	if rawSlots == nil {
		slotRefs["api_key"] = "slot_ref:api_key"
		resolved["api_key"] = defaultHarnessCanary()
		secValues = append(secValues, resolved["api_key"])
	}
	scanner = NewSecretLeakScanner(secValues...)
	return resolved, slotRefs, scanner
}

func runCassetteCase(
	ctx context.Context,
	vm *installer.ValidatedManifest,
	rt port.ProviderRuntime,
	client *http.Client,
	cas Cassette,
) error {
	resolvedSecrets, slotRefs, scanner := resolveCassetteSecrets(cas.SecretSlots)

	cfgBytes, err := json.Marshal(cas.Config)
	if err != nil {
		return validationErr("marshal cassette config", err)
	}
	if cas.Config == nil {
		cfgBytes = []byte("{}")
	}
	reqBytes, err := json.Marshal(cas.NormalizedInput)
	if err != nil {
		return validationErr("marshal normalized_input", err)
	}

	if cas.Capability == "native_passthrough" {
		return runNativePassthroughCassette(ctx, vm, rt, client, cas, resolvedSecrets, reqBytes, scanner)
	}

	// Step 1: Adapter builds UpstreamHttpSpec WITHOUT receiving any plaintext secret!
	spec, err := rt.TransformRequest(ctx, port.TransformRequestInput{
		RequestID:             "test_" + cas.CaseID,
		ConfigJSON:            cassetteConfigWithModality(cfgBytes, cas.Capability),
		NormalizedRequestJSON: reqBytes,
		SecretSlotRefs:        slotRefs,
	})
	if err != nil {
		if leakErr := scanner.Check("TransformRequest error", err.Error()); leakErr != nil {
			return leakErr
		}
		return contractErr(fmt.Sprintf("%s: TransformRequest failed", cas.CaseID), err)
	}

	if err := checkSpecSecurity(vm, spec, scanner); err != nil {
		return err
	}

	if cas.ExpectedNormalizedOutput != nil && cas.ExpectedNormalizedOutput.RequireStreamOptionsIncludeUsage {
		var sent map[string]any
		if uerr := json.Unmarshal(spec.BodyBytes, &sent); uerr != nil {
			return contractErr(fmt.Sprintf("%s: transformed body invalid JSON", cas.CaseID), uerr)
		}
		so, _ := sent["stream_options"].(map[string]any)
		inc, _ := so["include_usage"].(bool)
		if !inc {
			return contractErr(fmt.Sprintf("%s: CR-09 expected stream_options.include_usage=true in upstream body", cas.CaseID), nil)
		}
	}

	// Step 2: Execute outbound HTTP exchange strictly against local 127.0.0.1 mock,
	// injecting blind secret slots only at the Host egress boundary.
	mockAddr, headerMismatchCh, shutdownMock, err := startCassetteMockServer(cas)
	if err != nil {
		return err
	}
	defer shutdownMock()

	httpResp, err := executeHermeticEgress(ctx, client, mockAddr, spec, resolvedSecrets)
	if err != nil {
		return contractErr(fmt.Sprintf("%s: hermetic mock request failed", cas.CaseID), err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	select {
	case hdrErr := <-headerMismatchCh:
		if hdrErr != nil {
			return contractErr(cas.CaseID, hdrErr)
		}
	default:
	}

	sanitizedHeaders := make(map[string]string, len(httpResp.Header))
	for k, vals := range httpResp.Header {
		if strings.EqualFold(k, "set-cookie") || strings.EqualFold(k, "authorization") {
			continue
		}
		if len(vals) > 0 {
			sanitizedHeaders[k] = vals[0]
		}
	}

	// Step 3A: Error Normalization Replay
	if httpResp.StatusCode >= http.StatusBadRequest || cas.ExpectedCanonicalError != nil {
		rawErrBody, rerr := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
		if rerr != nil {
			return contractErr(fmt.Sprintf("%s: read upstream error body", cas.CaseID), rerr)
		}
		ce, nerr := rt.NormalizeError(ctx, port.NormalizeErrorInput{
			RequestID:        "test_" + cas.CaseID,
			HTTPStatus:       int32(httpResp.StatusCode),
			SanitizedHeaders: sanitizedHeaders,
			SanitizedBody:    rawErrBody,
		})
		if nerr != nil {
			if leakErr := scanner.Check("NormalizeError err", nerr.Error()); leakErr != nil {
				return leakErr
			}
			return contractErr(fmt.Sprintf("%s: NormalizeError failed", cas.CaseID), nerr)
		}
		return assertCanonicalError(cas, ce, scanner)
	}

	// Step 3B: Streaming SSE Replay (splitting mid-UTF-8 and mid-"data:")
	ex := cas.UpstreamRecordedExchange
	if len(ex.RawSSEChunks) > 0 || len(ex.SSEFrames) > 0 || cas.Capability == "chat_stream" {
		return replayAndVerifyStream(ctx, rt, httpResp.Body, cas, scanner)
	}

	// Step 3C: Synchronous JSON Response Replay
	rawBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
	if err != nil {
		return contractErr(fmt.Sprintf("%s: read upstream body", cas.CaseID), err)
	}
	normResp, err := rt.TransformResponse(ctx, port.TransformResponseInput{
		RequestID:        "test_" + cas.CaseID,
		HTTPStatus:       int32(httpResp.StatusCode),
		SanitizedHeaders: sanitizedHeaders,
		RawResponseBody:  rawBody,
		Modality:         cassetteModality(cas.Capability),
	})
	if err != nil {
		if leakErr := scanner.Check("TransformResponse err", err.Error()); leakErr != nil {
			return leakErr
		}
		return contractErr(fmt.Sprintf("%s: TransformResponse failed", cas.CaseID), err)
	}
	return assertSyncOutput(cas, normResp, scanner)
}

func runNativePassthroughCassette(
	ctx context.Context,
	vm *installer.ValidatedManifest,
	rt port.ProviderRuntime,
	client *http.Client,
	cas Cassette,
	resolvedSecrets map[string]string,
	reqBytes []byte,
	scanner *SecretLeakScanner,
) error {
	strCfg := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := cas.Config[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	dialect := strCfg("dialect")
	if dialect == "" && len(vm.Manifest.Capabilities) > 0 && len(vm.Manifest.Capabilities[0].SupportedDialects) > 0 {
		dialect = vm.Manifest.Capabilities[0].SupportedDialects[0]
	}
	upstreamPath := strCfg("upstream_path", "path")
	if upstreamPath == "" {
		return validationErr(fmt.Sprintf("%s: native_passthrough cassette requires config.upstream_path", cas.CaseID), nil)
	}
	baseURL := strCfg("endpoint_base_url", "base_url")
	if baseURL == "" && len(vm.Manifest.Permissions.NetworkEgress.AllowedHosts) > 0 {
		scheme := "http"
		if vm.Manifest.Permissions.NetworkEgress.RequireTLS {
			scheme = "https"
		}
		baseURL = scheme + "://" + vm.Manifest.Permissions.NetworkEgress.AllowedHosts[0]
	}
	targetURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(upstreamPath, "/")

	hints, err := rt.InspectNativeRequest(ctx, port.NativeRequestInput{
		RequestID:      "test_" + cas.CaseID,
		Dialect:        dialect,
		UpstreamPath:   upstreamPath,
		RawRequestBody: reqBytes,
	})
	if err != nil {
		if leakErr := scanner.Check("InspectNativeRequest err", err.Error()); leakErr != nil {
			return leakErr
		}
		return contractErr(fmt.Sprintf("%s: InspectNativeRequest failed", cas.CaseID), err)
	}
	if err := scanner.Check("NativeRoutingHints.TargetModel", hints.TargetModel); err != nil {
		return err
	}
	var nativeInjections []port.CredentialInjectInstruction
	if len(resolvedSecrets) > 0 {
		nativeInjections = hints.CredentialInjections
	}
	spec := port.UpstreamHttpSpec{
		Method:               http.MethodPost,
		TargetURL:            targetURL,
		SafeHeaders:          map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
		BodyBytes:            reqBytes,
		CredentialInjections: nativeInjections,
	}
	if err := checkSpecSecurity(vm, spec, scanner); err != nil {
		return err
	}
	if cas.ExpectedNormalizedOutput != nil && cas.ExpectedNormalizedOutput.Model != "" && hints.TargetModel != cas.ExpectedNormalizedOutput.Model {
		return contractErr(fmt.Sprintf("%s: expected native target_model %q, got %q", cas.CaseID, cas.ExpectedNormalizedOutput.Model, hints.TargetModel), nil)
	}

	mockAddr, headerMismatchCh, shutdownMock, err := startCassetteMockServer(cas)
	if err != nil {
		return err
	}
	defer shutdownMock()

	httpResp, err := executeHermeticEgress(ctx, client, mockAddr, spec, resolvedSecrets)
	if err != nil {
		return contractErr(fmt.Sprintf("%s: native hermetic mock request failed", cas.CaseID), err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	select {
	case hdrErr := <-headerMismatchCh:
		if hdrErr != nil {
			return contractErr(cas.CaseID, hdrErr)
		}
	default:
	}

	rawBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
	if err != nil {
		return contractErr(fmt.Sprintf("%s: read native mock response", cas.CaseID), err)
	}

	if httpResp.StatusCode >= http.StatusBadRequest || cas.ExpectedCanonicalError != nil {
		sanitizedHeaders := make(map[string]string, len(httpResp.Header))
		for k, vals := range httpResp.Header {
			if strings.EqualFold(k, "set-cookie") || strings.EqualFold(k, "authorization") {
				continue
			}
			if len(vals) > 0 {
				sanitizedHeaders[k] = vals[0]
			}
		}
		ce, nerr := rt.NormalizeError(ctx, port.NormalizeErrorInput{
			RequestID:        "test_" + cas.CaseID,
			HTTPStatus:       int32(httpResp.StatusCode),
			SanitizedHeaders: sanitizedHeaders,
			SanitizedBody:    rawBody,
		})
		if nerr != nil {
			return contractErr(fmt.Sprintf("%s: NormalizeError failed", cas.CaseID), nerr)
		}
		return assertCanonicalError(cas, ce, scanner)
	}

	usage, err := rt.ExtractUsage(ctx, port.ExtractUsageInput{
		RequestID:                 "test_" + cas.CaseID,
		Modality:                  "native",
		RawResponseOrTrailerBytes: rawBody,
	})
	if err != nil {
		return contractErr(fmt.Sprintf("%s: ExtractUsage failed", cas.CaseID), err)
	}
	if cas.ExpectedNormalizedOutput != nil {
		if cas.ExpectedNormalizedOutput.ContentContains != "" && !strings.Contains(string(rawBody), cas.ExpectedNormalizedOutput.ContentContains) {
			return contractErr(fmt.Sprintf("%s: expected native body to contain %q", cas.CaseID, cas.ExpectedNormalizedOutput.ContentContains), nil)
		}
		return assertUsage(cas.CaseID, cas.ExpectedNormalizedOutput, &usage)
	}
	return nil
}

func checkSpecSecurity(vm *installer.ValidatedManifest, spec port.UpstreamHttpSpec, scanner *SecretLeakScanner) error {
	if err := scanner.Check("spec.TargetURL", spec.TargetURL); err != nil {
		return err
	}
	if err := scanner.Check("spec.BodyBytes", string(spec.BodyBytes)); err != nil {
		return err
	}
	for k, v := range spec.SafeHeaders {
		if err := scanner.Check("spec.SafeHeaders."+k, v); err != nil {
			return err
		}
	}
	if err := spec.Validate(); err != nil {
		if errors.Is(err, port.ErrProhibitedCookieInjection) {
			return securityErr("adapter attempted prohibited Cookie/Set-Cookie injection", err)
		}
		return securityErr("adapter returned unsafe UpstreamHttpSpec", err)
	}

	u, err := url.Parse(spec.TargetURL)
	if err != nil {
		return securityErr("invalid target_url in UpstreamHttpSpec", err)
	}
	if vm.Manifest.Permissions.NetworkEgress.RequireTLS && u.Scheme != "https" {
		return securityErr(fmt.Sprintf("adapter target_url %q violates requireTls:true", spec.TargetURL), nil)
	}
	host := strings.ToLower(u.Hostname())
	hostPort := strings.ToLower(u.Host)
	allowed := false
	for _, ah := range vm.Manifest.Permissions.NetworkEgress.AllowedHosts {
		if strings.EqualFold(ah, host) || (hostPort != "" && strings.EqualFold(ah, hostPort)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return securityErr(fmt.Sprintf("adapter attempted egress to unapproved host %q (allowed: %v)", host, vm.Manifest.Permissions.NetworkEgress.AllowedHosts), nil)
	}
	return nil
}

func startCassetteMockServer(cas Cassette) (addr string, mismatchCh <-chan error, shutdown func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, nil, validationErr("bind loopback cassette mock server", err)
	}
	errCh := make(chan error, 1)
	var lock sync.Mutex
	ex := cas.UpstreamRecordedExchange

	srv := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lock.Lock()
			defer lock.Unlock()
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()

			for hk, hv := range ex.ExpectedUpstreamHeaders {
				expected := strings.ReplaceAll(hv, canaryPlaceholder, defaultHarnessCanary())
				if got := r.Header.Get(hk); got != expected {
					select {
					case errCh <- fmt.Errorf("expected upstream header %s mismatch (expected pattern %q)", hk, hv):
					default:
					}
				}
			}

			for hk, hv := range ex.Headers {
				if strings.EqualFold(hk, "content-length") {
					continue
				}
				w.Header().Set(hk, hv)
			}
			w.WriteHeader(ex.Status)

			chunks, decErr := decodeCassetteChunks(ex)
			if decErr != nil {
				return
			}
			if len(chunks) > 0 {
				fl, canFlush := w.(http.Flusher)
				limit := len(chunks)
				if ex.AbortAfterChunks > 0 && ex.AbortAfterChunks < limit {
					limit = ex.AbortAfterChunks
				}
				for i := 0; i < limit; i++ {
					_, _ = w.Write(chunks[i])
					if canFlush {
						fl.Flush()
					}
				}
				return
			}
			_, _ = io.WriteString(w, ex.RawBody)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), errCh, func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}, nil
}

func decodeCassetteChunks(ex UpstreamRecordedExchange) ([][]byte, error) {
	rawList := ex.RawSSEChunks
	if len(rawList) == 0 {
		rawList = ex.SSEFrames
	}
	if len(rawList) == 0 {
		return nil, nil
	}
	out := make([][]byte, 0, len(rawList))
	for _, item := range rawList {
		if strings.HasPrefix(item, "base64:") {
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(item, "base64:"))
			if err != nil {
				return nil, fmt.Errorf("decode base64 chunk: %w", err)
			}
			out = append(out, raw)
		} else {
			out = append(out, []byte(item))
		}
	}
	return out, nil
}

func executeHermeticEgress(
	ctx context.Context,
	client *http.Client,
	mockAddr string,
	spec port.UpstreamHttpSpec,
	secrets map[string]string,
) (*http.Response, error) {
	origURL, err := url.Parse(spec.TargetURL)
	if err != nil {
		return nil, err
	}
	localURL := &url.URL{
		Scheme:   "http",
		Host:     mockAddr,
		Path:     origURL.Path,
		RawQuery: origURL.RawQuery,
	}
	for _, inj := range spec.CredentialInjections {
		if inj.TargetLocation != port.CredentialTargetLocationQueryParam {
			continue
		}
		sec, ok := secrets[inj.SecretSlotName]
		if !ok {
			return nil, securityErr(fmt.Sprintf("unknown secret slot %q", inj.SecretSlotName), nil)
		}
		q := localURL.Query()
		q.Set(inj.FieldName, inj.ValuePrefix+sec)
		localURL.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, spec.Method, localURL.String(), bytes.NewReader(spec.BodyBytes))
	if err != nil {
		return nil, err
	}
	req.Host = origURL.Host
	for k, v := range spec.SafeHeaders {
		req.Header.Set(k, v)
	}
	for _, inj := range spec.CredentialInjections {
		if inj.TargetLocation != port.CredentialTargetLocationHeader {
			continue
		}
		sec, ok := secrets[inj.SecretSlotName]
		if !ok {
			return nil, securityErr(fmt.Sprintf("unknown secret slot %q", inj.SecretSlotName), nil)
		}
		req.Header.Set(inj.FieldName, inj.ValuePrefix+sec)
	}
	return client.Do(req)
}

func replayAndVerifyStream(
	ctx context.Context,
	rt port.ProviderRuntime,
	body io.Reader,
	cas Cassette,
	scanner *SecretLeakScanner,
) error {
	ex := cas.UpstreamRecordedExchange
	chunks, err := decodeCassetteChunks(ex)
	if err != nil {
		return validationErr("decode cassette chunks", err)
	}
	// Drain the loopback HTTP body to verify real transport round-trip delivery.
	drained, err := io.ReadAll(io.LimitReader(body, 4<<20))
	if err != nil {
		return contractErr(fmt.Sprintf("%s: read mock stream", cas.CaseID), err)
	}
	if len(chunks) == 0 && len(drained) > 0 {
		chunks = [][]byte{drained}
	} else if len(chunks) == 0 {
		chunks = [][]byte{nil}
	}
	truncated := false
	if ex.AbortAfterChunks > 0 && ex.AbortAfterChunks < len(chunks) {
		chunks = chunks[:ex.AbortAfterChunks]
		truncated = true
	}

	var sessionState []byte
	var contentBuf strings.Builder
	var latestUsage *port.NormalizedUsage
	sawDone := false
	completed := false

	for i, chunk := range chunks {
		isEOF := !truncated && i == len(chunks)-1
		out, cerr := rt.TransformStreamChunk(ctx, port.StreamChunkInput{
			RequestID:        "test_" + cas.CaseID,
			SessionState:     sessionState,
			RawUpstreamChunk: chunk,
			IsEOF:            isEOF,
		})
		if cerr != nil {
			if leakErr := scanner.Check("TransformStreamChunk err", cerr.Error()); leakErr != nil {
				return leakErr
			}
			return contractErr(fmt.Sprintf("%s: TransformStreamChunk failed at chunk %d", cas.CaseID, i), cerr)
		}
		sessionState = out.NextSessionState
		if out.IncrementalUsage != nil {
			latestUsage = out.IncrementalUsage
		}
		if out.StreamCompleted {
			completed = true
		}

		for _, frame := range out.NormalizedSSEFrames {
			if leakErr := scanner.Check("stream frame", string(frame)); leakErr != nil {
				return leakErr
			}
			if !utf8.Valid(frame) {
				return contractErr(fmt.Sprintf("%s: emitted SSE frame contains invalid UTF-8 bytes", cas.CaseID), nil)
			}
			if bytes.ContainsRune(frame, '\uFFFD') {
				return contractErr(fmt.Sprintf("%s: emitted SSE frame corrupted multi-byte UTF-8 into replacement rune U+FFFD", cas.CaseID), nil)
			}

			trimmed := bytes.TrimSpace(frame)
			if !bytes.HasPrefix(trimmed, []byte("data:")) {
				continue
			}
			payload := bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
			if string(payload) == "[DONE]" {
				sawDone = true
				continue
			}
			var frameObj map[string]any
			if jerr := json.Unmarshal(payload, &frameObj); jerr != nil {
				return contractErr(fmt.Sprintf("%s: emitted SSE JSON frame invalid", cas.CaseID), jerr)
			}
			if choices, ok := frameObj["choices"].([]any); ok && len(choices) > 0 {
				if c0, ok := choices[0].(map[string]any); ok {
					if delta, ok := c0["delta"].(map[string]any); ok {
						if txt, ok := delta["content"].(string); ok {
							contentBuf.WriteString(txt)
						}
					}
				}
			}
			if latestUsage == nil {
				if u, ok := extractUsageStruct(frameObj["usage"]); ok {
					latestUsage = u
				}
			}
		}
	}

	exp := cas.ExpectedNormalizedOutput
	if exp == nil {
		return nil
	}
	fullContent := contentBuf.String()
	if exp.Content != "" && fullContent != exp.Content {
		return contractErr(fmt.Sprintf("%s: expected streamed content %q, got %q", cas.CaseID, exp.Content, fullContent), nil)
	}
	if exp.ContentContains != "" && !strings.Contains(fullContent, exp.ContentContains) {
		return contractErr(fmt.Sprintf("%s: expected streamed content to contain %q, got %q", cas.CaseID, exp.ContentContains, fullContent), nil)
	}
	if exp.RequireStreamCompleted && (!completed || !sawDone) {
		return contractErr(fmt.Sprintf("%s: dropped end-of-stream completion ([DONE]=%v StreamCompleted=%v)", cas.CaseID, sawDone, completed), nil)
	}
	return assertUsage(cas.CaseID, exp, latestUsage)
}

func assertSyncOutput(cas Cassette, resp port.NormalizedInferenceResponse, scanner *SecretLeakScanner) error {
	if err := scanner.Check("NormalizedResponseJSON", string(resp.NormalizedResponseJSON)); err != nil {
		return err
	}
	if err := scanner.Check("UpstreamModelID", resp.UpstreamModelID); err != nil {
		return err
	}
	if err := scanner.Check("FinishReason", resp.FinishReason); err != nil {
		return err
	}
	if !utf8.Valid(resp.NormalizedResponseJSON) {
		return contractErr(fmt.Sprintf("%s: normalized response contains invalid UTF-8", cas.CaseID), nil)
	}

	var doc map[string]any
	if err := json.Unmarshal(resp.NormalizedResponseJSON, &doc); err != nil {
		return contractErr(fmt.Sprintf("%s: NormalizedResponseJSON is not valid JSON", cas.CaseID), err)
	}

	exp := cas.ExpectedNormalizedOutput
	if exp == nil {
		return nil
	}
	actualModel := resp.UpstreamModelID
	if actualModel == "" {
		actualModel, _ = doc["model"].(string)
	}
	if exp.Model != "" && actualModel != exp.Model {
		return contractErr(fmt.Sprintf("%s: expected model %q, got %q", cas.CaseID, exp.Model, actualModel), nil)
	}

	var firstChoice map[string]any
	if choices, ok := doc["choices"].([]any); ok && len(choices) > 0 {
		firstChoice, _ = choices[0].(map[string]any)
	}
	actualFinish := resp.FinishReason
	if actualFinish == "" && firstChoice != nil {
		actualFinish, _ = firstChoice["finish_reason"].(string)
	}
	if exp.FinishReason != "" && actualFinish != exp.FinishReason {
		return contractErr(fmt.Sprintf("%s: expected finish_reason %q, got %q", cas.CaseID, exp.FinishReason, actualFinish), nil)
	}

	var msg map[string]any
	if firstChoice != nil {
		msg, _ = firstChoice["message"].(map[string]any)
	}
	actualContent, _ := msg["content"].(string)
	if exp.Content != "" && actualContent != exp.Content {
		return contractErr(fmt.Sprintf("%s: expected content %q, got %q", cas.CaseID, exp.Content, actualContent), nil)
	}
	if exp.ContentContains != "" && !strings.Contains(actualContent, exp.ContentContains) {
		return contractErr(fmt.Sprintf("%s: expected content containing %q, got %q", cas.CaseID, exp.ContentContains, actualContent), nil)
	}
	if exp.RerankResultsCount != nil || exp.RerankFirstIndex != nil {
		results, ok := doc["results"].([]any)
		if !ok {
			return contractErr(fmt.Sprintf("%s: normalized rerank results are missing", cas.CaseID), nil)
		}
		if exp.RerankResultsCount != nil && len(results) != *exp.RerankResultsCount {
			return contractErr(fmt.Sprintf("%s: expected %d rerank results, got %d", cas.CaseID, *exp.RerankResultsCount, len(results)), nil)
		}
		if exp.RerankFirstIndex != nil {
			if len(results) == 0 {
				return contractErr(fmt.Sprintf("%s: expected first rerank result", cas.CaseID), nil)
			}
			first, _ := results[0].(map[string]any)
			idx, ok := first["index"].(float64)
			if !ok || int(idx) != *exp.RerankFirstIndex {
				return contractErr(fmt.Sprintf("%s: expected first rerank index %d", cas.CaseID, *exp.RerankFirstIndex), nil)
			}
		}
	}

	if exp.ToolCallsCount != nil {
		tcSlice, _ := msg["tool_calls"].([]any)
		if len(tcSlice) != *exp.ToolCallsCount {
			return contractErr(fmt.Sprintf("%s: expected %d tool_calls, got %d", cas.CaseID, *exp.ToolCallsCount, len(tcSlice)), nil)
		}
		if *exp.ToolCallsCount > 0 {
			tc0, _ := tcSlice[0].(map[string]any)
			fn, _ := tc0["function"].(map[string]any)
			if exp.ToolCallName != "" {
				gotName, _ := fn["name"].(string)
				if gotName != exp.ToolCallName {
					return contractErr(fmt.Sprintf("%s: expected tool_call name %q, got %q", cas.CaseID, exp.ToolCallName, gotName), nil)
				}
			}
			if exp.ToolCallArguments != "" {
				gotArgs, _ := fn["arguments"].(string)
				if gotArgs != exp.ToolCallArguments {
					return contractErr(fmt.Sprintf("%s: expected tool_call arguments %q, got %q", cas.CaseID, exp.ToolCallArguments, gotArgs), nil)
				}
			}
		}
	}

	usage := resp.Usage
	if usage == nil {
		if u, ok := extractUsageStruct(doc["usage"]); ok {
			usage = u
		}
	}
	return assertUsage(cas.CaseID, exp, usage)
}

func assertUsage(caseID string, exp *ExpectedOutput, usage *port.NormalizedUsage) error {
	if !exp.RequireUsage && exp.InputTokens == nil && exp.OutputTokens == nil && exp.RerankSearchUnits == nil {
		return nil
	}
	if usage == nil {
		return contractErr(fmt.Sprintf("%s: missing expected NormalizedUsage", caseID), nil)
	}
	if exp.InputTokens != nil && usage.InputTokens != *exp.InputTokens {
		return contractErr(fmt.Sprintf("%s: expected input_tokens %d, got %d", caseID, *exp.InputTokens, usage.InputTokens), nil)
	}
	if exp.OutputTokens != nil && usage.OutputTokens != *exp.OutputTokens {
		return contractErr(fmt.Sprintf("%s: expected output_tokens %d, got %d", caseID, *exp.OutputTokens, usage.OutputTokens), nil)
	}
	if exp.CacheReadTokens != nil && usage.CacheReadTokens != *exp.CacheReadTokens {
		return contractErr(fmt.Sprintf("%s: expected cache_read_tokens %d, got %d", caseID, *exp.CacheReadTokens, usage.CacheReadTokens), nil)
	}
	if exp.ReasoningTokens != nil && usage.ReasoningTokens != *exp.ReasoningTokens {
		return contractErr(fmt.Sprintf("%s: expected reasoning_tokens %d, got %d", caseID, *exp.ReasoningTokens, usage.ReasoningTokens), nil)
	}
	if exp.RerankSearchUnits != nil && (usage.MediaUsage == nil || usage.MediaUsage.RerankSearchUnits != *exp.RerankSearchUnits) {
		return contractErr(fmt.Sprintf("%s: expected rerank_search_units %d", caseID, *exp.RerankSearchUnits), nil)
	}
	return nil
}

func extractUsageStruct(raw any) (*port.NormalizedUsage, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	u := &port.NormalizedUsage{}
	if v, ok := m["input_tokens"].(float64); ok {
		u.InputTokens = int64(v)
	}
	if v, ok := m["output_tokens"].(float64); ok {
		u.OutputTokens = int64(v)
	}
	if v, ok := m["prompt_tokens"].(float64); ok && u.InputTokens == 0 {
		u.InputTokens = int64(v)
	}
	if v, ok := m["completion_tokens"].(float64); ok && u.OutputTokens == 0 {
		u.OutputTokens = int64(v)
	}
	if v, ok := m["cache_read_tokens"].(float64); ok {
		u.CacheReadTokens = int64(v)
	}
	if v, ok := m["cache_write_tokens"].(float64); ok {
		u.CacheWriteTokens = int64(v)
	}
	if v, ok := m["reasoning_tokens"].(float64); ok {
		u.ReasoningTokens = int64(v)
	}
	return u, true
}

func assertCanonicalError(cas Cassette, ce port.CanonicalProviderError, scanner *SecretLeakScanner) error {
	if err := scanner.Check("CanonicalProviderError.SanitizedPublicMessage", ce.SanitizedPublicMessage); err != nil {
		return err
	}
	if err := scanner.Check("CanonicalProviderError.CanonicalCode", ce.CanonicalCode); err != nil {
		return err
	}
	if err := scanner.Check("CanonicalProviderError.ErrorType", ce.ErrorType); err != nil {
		return err
	}

	exp := cas.ExpectedCanonicalError
	if exp == nil {
		return nil
	}
	if exp.CanonicalCode != "" && ce.CanonicalCode != exp.CanonicalCode {
		return contractErr(fmt.Sprintf("%s: expected canonical_code %q, got %q", cas.CaseID, exp.CanonicalCode, ce.CanonicalCode), nil)
	}
	if exp.ErrorType != "" && ce.ErrorType != exp.ErrorType {
		return contractErr(fmt.Sprintf("%s: expected error_type %q, got %q", cas.CaseID, exp.ErrorType, ce.ErrorType), nil)
	}
	if exp.RetryablePreStream != nil && ce.RetryablePreStream != *exp.RetryablePreStream {
		return contractErr(fmt.Sprintf("%s: expected retryable_pre_stream=%v, got %v", cas.CaseID, *exp.RetryablePreStream, ce.RetryablePreStream), nil)
	}
	if exp.TriggerReauth != nil && ce.TriggerConnectionReauth != *exp.TriggerReauth {
		return contractErr(fmt.Sprintf("%s: expected trigger_reauth=%v, got %v", cas.CaseID, *exp.TriggerReauth, ce.TriggerConnectionReauth), nil)
	}
	for _, banned := range exp.MessageMustNotContain {
		if banned != "" && strings.Contains(ce.SanitizedPublicMessage, banned) {
			return securityErr(fmt.Sprintf("%s: sanitized error message exposed prohibited substring", cas.CaseID), nil)
		}
	}
	return nil
}
