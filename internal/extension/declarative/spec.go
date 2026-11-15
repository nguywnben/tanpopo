// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package declarative implements the Tier-1 non-Turing-complete Declarative Adapter
// runtime engine satisfying extension/port.ProviderRuntime (TP-IMP-0012 / SPEC-EXT-02).
//
// Security boundaries:
//   - Zero filesystem, environment variable, network, or Vault access operators.
//   - Hard cap of JSON nesting depth <= 32 checked in O(1) memory before decoding.
//   - Hard cap of <= 256 operator execution steps per message invocation.
//   - Zero backtracking regular expressions; single-pass finite template substitution.
package declarative

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	"gopkg.in/yaml.v3"
)

const (
	// MaxJSONDepth enforces the hard cap of JSON nesting depth <= 32 (FZ-01 / TP-IMP-0012).
	MaxJSONDepth = 32
	// MaxOperatorSteps enforces the hard cap of <= 256 operator steps per message (POC-02).
	MaxOperatorSteps = 256
	// MaxPayloadBytes bounds single-message input payloads against memory amplification.
	MaxPayloadBytes = 8 * 1024 * 1024
	// MaxManifestBytes bounds raw declarative YAML/JSON spec inputs.
	MaxManifestBytes = 512 * 1024
	// MaxSessionStateBytes bounds per-stream SSE scratch state passed across chunks.
	MaxSessionStateBytes = 128 * 1024
	// MaxSSELinesPerChunk bounds per-chunk SSE line scanning to keep step budgets deterministic.
	MaxSSELinesPerChunk = 256
	// MaxExpandedYAMLNodes caps expanded AST nodes (including YAML aliases) before Decode().
	MaxExpandedYAMLNodes = 10000
	// MaxPathSegments bounds dot-separated JSON traversal path segments within MaxJSONDepth.
	MaxPathSegments = MaxJSONDepth
)

var (
	// ErrInvalidDeclarativeSpec indicates malformed or unsafe declarative YAML/JSON mapping rules.
	ErrInvalidDeclarativeSpec = fmt.Errorf("%w: invalid declarative adapter spec", port.ErrInvalidSpec)
	// ErrRecursiveTemplate indicates recursive, nested, or self-referential template placeholders.
	ErrRecursiveTemplate = fmt.Errorf("%w: recursive or nested template placeholders prohibited", ErrInvalidDeclarativeSpec)
	// ErrJSONDepthExceeded indicates JSON nesting depth exceeds MaxJSONDepth (32).
	ErrJSONDepthExceeded = fmt.Errorf("%w: json nesting depth exceeds %d", port.ErrInvalidSpec, MaxJSONDepth)
	// ErrOperatorBudgetExceeded indicates evaluation exceeded MaxOperatorSteps (256).
	ErrOperatorBudgetExceeded = fmt.Errorf("%w: operator step budget (%d) exceeded", port.ErrExecutionFault, MaxOperatorSteps)
)

// Spec defines the finite declarative configuration compiled into an immutable Engine.
type Spec struct {
	ID                string              `yaml:"id" json:"id"`
	Slug              string              `yaml:"slug" json:"slug"`
	NativePrefix      string              `yaml:"nativePrefix" json:"native_prefix"`
	SupportedDialects []string            `yaml:"supportedDialects" json:"supported_dialects"`
	AllowedHosts      []string            `yaml:"allowedHosts" json:"allowed_hosts"`
	DefaultBaseURL    string              `yaml:"defaultBaseUrl" json:"default_base_url"`
	AuthBindings      []AuthBindingSpec   `yaml:"authBindings" json:"auth_bindings"`
	ValidationProbe   ValidationProbeSpec `yaml:"validationProbe" json:"validation_probe"`
	RequestMapping    RequestMappingSpec  `yaml:"requestMapping" json:"request_mapping"`
	ResponseMapping   ResponseMappingSpec `yaml:"responseMapping" json:"response_mapping"`
	StreamMapping     StreamMappingSpec   `yaml:"streamMapping" json:"stream_mapping"`
	ErrorMapping      ErrorMappingSpec    `yaml:"errorMapping" json:"error_mapping"`
	NativeMapping     NativeMappingSpec   `yaml:"nativeMapping" json:"native_mapping"`
	canonicalOpenAI   bool
}

// AuthBindingSpec declares how a Host Vault secret slot binds to outbound HTTP credentials.
type AuthBindingSpec struct {
	SlotName       string                        `yaml:"slot" json:"slot"`
	TargetLocation port.CredentialTargetLocation `yaml:"targetLocation" json:"target_location"`
	FieldName      string                        `yaml:"fieldName" json:"field_name"`
	ValuePrefix    string                        `yaml:"valuePrefix" json:"value_prefix"`
}

// ValidationProbeSpec defines Phase-1/Phase-2 connection probe generation & evaluation.
type ValidationProbeSpec struct {
	Method          string  `yaml:"method" json:"method"`
	PathTemplate    string  `yaml:"pathTemplate" json:"path_template"`
	BodyJSON        string  `yaml:"bodyJson" json:"body_json"`
	SuccessStatuses []int32 `yaml:"successStatuses" json:"success_statuses"`
	ModelsArrayPath string  `yaml:"modelsArrayPath" json:"models_array_path"`
	ModelIDField    string  `yaml:"modelIdField" json:"model_id_field"`
}

// RequestMappingSpec defines bounded non-Turing-complete JSON request rewrites.
type RequestMappingSpec struct {
	Method            string            `yaml:"method" json:"method"`
	PathTemplate      string            `yaml:"pathTemplate" json:"path_template"`
	StaticHeaders     map[string]string `yaml:"staticHeaders" json:"static_headers"`
	FieldRenames      map[string]string `yaml:"fieldRenames" json:"field_renames"`
	FieldDefaults     map[string]any    `yaml:"fieldDefaults" json:"field_defaults"`
	DropFields        []string          `yaml:"dropFields" json:"drop_fields"`
	InjectStreamUsage bool              `yaml:"injectStreamUsage" json:"inject_stream_usage"`
}

// UsagePathSpec specifies dot-paths for extracting the 5-component NormalizedUsage counters.
type UsagePathSpec struct {
	InputTokens      string `yaml:"inputTokens" json:"input_tokens"`
	OutputTokens     string `yaml:"outputTokens" json:"output_tokens"`
	CacheReadTokens  string `yaml:"cacheReadTokens" json:"cache_read_tokens"`
	CacheWriteTokens string `yaml:"cacheWriteTokens" json:"cache_write_tokens"`
	ReasoningTokens  string `yaml:"reasoningTokens" json:"reasoning_tokens"`
}

// ResponseMappingSpec defines finite dot-path extraction for synchronous responses.
type ResponseMappingSpec struct {
	ModelPath        string        `yaml:"modelPath" json:"model_path"`
	FinishReasonPath string        `yaml:"finishReasonPath" json:"finish_reason_path"`
	ContentPath      string        `yaml:"contentPath" json:"content_path"`
	RolePath         string        `yaml:"rolePath" json:"role_path"`
	ToolCallsPath    string        `yaml:"toolCallsPath" json:"tool_calls_path"`
	UsagePaths       UsagePathSpec `yaml:"usagePaths" json:"usage_paths"`
}

// StreamMappingSpec defines bounded SSE frame parsing and delta normalization.
type StreamMappingSpec struct {
	DoneSentinel       string        `yaml:"doneSentinel" json:"done_sentinel"`
	DeltaContentPath   string        `yaml:"deltaContentPath" json:"delta_content_path"`
	DeltaToolCallsPath string        `yaml:"deltaToolCallsPath" json:"delta_tool_calls_path"`
	FinishReasonPath   string        `yaml:"finishReasonPath" json:"finish_reason_path"`
	ModelPath          string        `yaml:"modelPath" json:"model_path"`
	UsagePaths         UsagePathSpec `yaml:"usagePaths" json:"usage_paths"`
	ErrorMessagePath   string        `yaml:"errorMessagePath" json:"error_message_path"`
}

// ErrorMappingSpec allows mapping upstream HTTP error codes onto canonical behaviors.
type ErrorMappingSpec struct {
	ReauthStatuses    []int32 `yaml:"reauthStatuses" json:"reauth_statuses"`
	RetryableStatuses []int32 `yaml:"retryableStatuses" json:"retryable_statuses"`
	MessagePath       string  `yaml:"messagePath" json:"message_path"`
	CodePath          string  `yaml:"codePath" json:"code_path"`
}

// NativeMappingSpec extracts model, stream, and max_tokens hints from /native/* passthroughs.
type NativeMappingSpec struct {
	ModelPath     string `yaml:"modelPath" json:"model_path"`
	StreamPath    string `yaml:"streamPath" json:"stream_path"`
	MaxTokensPath string `yaml:"maxTokensPath" json:"max_tokens_path"`
	PathModelFlag string `yaml:"pathModelSegment" json:"path_model_segment"`
}

// pathToken is a pre-parsed dot-path segment (key name or non-negative slice index).
type pathToken struct {
	key     string
	index   int
	isIndex bool
}

type parsedPath struct {
	raw    string
	tokens []pathToken
}

type compiledRename struct {
	from parsedPath
	to   parsedPath
}

type compiledDefault struct {
	to    parsedPath
	value any
}

// manifestEnvelope accepts canonical DeclarativeProviderAdapter YAML manifests (SPEC-EXT-02).
type manifestEnvelope struct {
	APIVersion  string         `yaml:"apiVersion"`
	Kind        string         `yaml:"kind"`
	Metadata    manifestMeta   `yaml:"metadata"`
	Permissions manifestPerms  `yaml:"permissions"`
	SecretSlots []manifestSlot `yaml:"secretSlots"`
	Adapter     *adapterBlock  `yaml:"adapter"`
}

type manifestMeta struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type manifestPerms struct {
	NetworkEgress struct {
		AllowedHosts []string `yaml:"allowedHosts"`
	} `yaml:"networkEgress"`
}

type manifestSlot struct {
	Slot string `yaml:"slot"`
}

type adapterBlock struct {
	Slug              string               `yaml:"slug"`
	NativePrefix      string               `yaml:"nativePrefix"`
	BaseURL           string               `yaml:"baseUrl"`
	BaseURLTemplate   string               `yaml:"baseUrlTemplate"`
	WireProtocol      string               `yaml:"wireProtocol"`
	UsageExtraction   manifestUsagePaths   `yaml:"usageExtraction"`
	SupportedDialects []string             `yaml:"supportedDialects"`
	Endpoints         map[string]string    `yaml:"endpoints"`
	Auth              []manifestAuthRule   `yaml:"auth"`
	ValidationProbe   *ValidationProbeSpec `yaml:"validationProbe"`
	RequestMapping    *RequestMappingSpec  `yaml:"requestMapping"`
	ResponseMapping   *ResponseMappingSpec `yaml:"responseMapping"`
	StreamMapping     *StreamMappingSpec   `yaml:"streamMapping"`
	ErrorMapping      *ErrorMappingSpec    `yaml:"errorMapping"`
	NativeMapping     *NativeMappingSpec   `yaml:"nativeMapping"`
}

type manifestUsagePaths struct {
	InputTokens      string `yaml:"inputTokensPath"`
	OutputTokens     string `yaml:"outputTokensPath"`
	CacheReadTokens  string `yaml:"cacheReadTokensPath"`
	CacheWriteTokens string `yaml:"cacheWriteTokensPath"`
	ReasoningTokens  string `yaml:"reasoningTokensPath"`
}

func (p manifestUsagePaths) fillMissing(out *UsagePathSpec) {
	for _, field := range []struct {
		target *string
		path   string
	}{
		{&out.InputTokens, p.InputTokens},
		{&out.OutputTokens, p.OutputTokens},
		{&out.CacheReadTokens, p.CacheReadTokens},
		{&out.CacheWriteTokens, p.CacheWriteTokens},
		{&out.ReasoningTokens, p.ReasoningTokens},
	} {
		if strings.TrimSpace(*field.target) == "" {
			*field.target = strings.TrimPrefix(strings.TrimSpace(field.path), "$.")
		}
	}
}

type manifestAuthRule struct {
	ID          string `yaml:"id"`
	Slot        string `yaml:"slot"`
	SecretSlot  string `yaml:"secretSlot"`
	Mode        string `yaml:"mode"`
	Location    string `yaml:"location"`
	HeaderName  string `yaml:"headerName"`
	ParamName   string `yaml:"paramName"`
	Prefix      string `yaml:"prefix"`
	ValuePrefix string `yaml:"valuePrefix"`
}

// ParseDeclarativeManifestYAML parses a canonical DeclarativeProviderAdapter YAML document
// into a compiled, security-validated Engine.
func ParseDeclarativeManifestYAML(raw []byte) (*Engine, error) {
	if len(raw) == 0 || len(raw) > MaxManifestBytes {
		return nil, fmt.Errorf("%w: manifest byte length %d out of bounds (1..%d)", ErrInvalidDeclarativeSpec, len(raw), MaxManifestBytes)
	}
	var root yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("%w: yaml decode: %v", ErrInvalidDeclarativeSpec, err)
	}
	// Reject multi-document streams cleanly.
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%w: multiple YAML documents forbidden", ErrInvalidDeclarativeSpec)
	}
	aliases := 0
	expandedNodes := 0
	visiting := make(map[*yaml.Node]bool)
	if err := validateYAMLNodeBounds(&root, 0, &aliases, &expandedNodes, visiting); err != nil {
		return nil, err
	}

	var env manifestEnvelope
	if err := root.Decode(&env); err != nil {
		return nil, fmt.Errorf("%w: manifest structure decode: %v", ErrInvalidDeclarativeSpec, err)
	}
	if env.APIVersion != "" && env.APIVersion != "tanpopo.dev/extension/v1alpha1" {
		return nil, fmt.Errorf("%w: unsupported apiVersion %q", ErrInvalidDeclarativeSpec, env.APIVersion)
	}
	if env.Kind != "" && env.Kind != "DeclarativeProviderAdapter" && env.Kind != "ExtensionManifest" {
		return nil, fmt.Errorf("%w: unsupported kind %q", ErrInvalidDeclarativeSpec, env.Kind)
	}
	if env.Adapter == nil {
		return nil, fmt.Errorf("%w: adapter section is required", ErrInvalidDeclarativeSpec)
	}

	spec, err := specFromAdapterBlock(env)
	if err != nil {
		return nil, err
	}
	return CompileSpec(spec)
}

func specFromAdapterBlock(env manifestEnvelope) (Spec, error) {
	ab := env.Adapter
	slug := strings.TrimSpace(ab.Slug)
	if slug == "" {
		slug = strings.TrimSpace(env.Metadata.ID)
	}
	baseURL := strings.TrimSpace(ab.BaseURLTemplate)
	if baseURL == "" {
		baseURL = strings.TrimSpace(ab.BaseURL)
	}
	spec := Spec{
		ID:                strings.TrimSpace(env.Metadata.ID),
		Slug:              slug,
		NativePrefix:      strings.TrimSpace(ab.NativePrefix),
		SupportedDialects: append([]string(nil), ab.SupportedDialects...),
		AllowedHosts:      append([]string(nil), env.Permissions.NetworkEgress.AllowedHosts...),
		DefaultBaseURL:    baseURL,
	}
	if ab.ValidationProbe != nil {
		spec.ValidationProbe = *ab.ValidationProbe
	}
	if spec.ValidationProbe.PathTemplate == "" && ab.Endpoints["models"] != "" {
		spec.ValidationProbe.PathTemplate = ab.Endpoints["models"]
	}
	if ab.RequestMapping != nil {
		spec.RequestMapping = *ab.RequestMapping
	}
	wireProto := strings.ToLower(strings.TrimSpace(ab.WireProtocol))
	if wireProto == "openai_chat_v1" {
		// SPEC-EXT-02 CR-09: openai_chat_v1 automatically enables stream_options.include_usage=true.
		spec.RequestMapping.InjectStreamUsage = true
		spec.canonicalOpenAI = true
		if len(spec.SupportedDialects) == 0 {
			spec.SupportedDialects = []string{"openai_chat"}
		}
	} else if wireProto != "" {
		return Spec{}, fmt.Errorf("%w: unsupported wireProtocol %q", ErrInvalidDeclarativeSpec, ab.WireProtocol)
	}
	if spec.RequestMapping.PathTemplate == "" && ab.Endpoints["chat"] != "" {
		spec.RequestMapping.PathTemplate = ab.Endpoints["chat"]
	}
	if ab.ResponseMapping != nil {
		spec.ResponseMapping = *ab.ResponseMapping
	}
	if ab.StreamMapping != nil {
		spec.StreamMapping = *ab.StreamMapping
	}
	ab.UsageExtraction.fillMissing(&spec.ResponseMapping.UsagePaths)
	ab.UsageExtraction.fillMissing(&spec.StreamMapping.UsagePaths)
	if ab.ErrorMapping != nil {
		spec.ErrorMapping = *ab.ErrorMapping
	}
	if ab.NativeMapping != nil {
		spec.NativeMapping = *ab.NativeMapping
	}
	for _, a := range ab.Auth {
		slot := strings.TrimSpace(a.SecretSlot)
		if slot == "" {
			slot = strings.TrimSpace(a.Slot)
		}
		if slot == "" {
			slot = "api_key"
		}
		targetLoc := port.CredentialTargetLocationHeader
		fieldName := strings.TrimSpace(a.HeaderName)
		loc := strings.ToLower(strings.TrimSpace(a.Location))
		mode := strings.ToLower(strings.TrimSpace(a.Mode))
		if mode == "host_crypto_sign" || mode == "scoped_secret_unseal" || loc == "host_crypto_sign" {
			return Spec{}, fmt.Errorf("%w: auth mode %q is not supported in Tier-1 declarative YAML", ErrInvalidDeclarativeSpec, a.Mode)
		}
		if mode != "" && mode != "host_blind_inject" && mode != "header" && mode != "query" && mode != "query_param" && mode != "bearer" {
			return Spec{}, fmt.Errorf("%w: unknown auth mode %q in Tier-1 declarative YAML", ErrInvalidDeclarativeSpec, a.Mode)
		}
		if loc == "query" || loc == "query_param" || strings.TrimSpace(a.ParamName) != "" {
			targetLoc = port.CredentialTargetLocationQueryParam
			if strings.TrimSpace(a.ParamName) != "" {
				fieldName = strings.TrimSpace(a.ParamName)
			}
		}
		if fieldName == "" && targetLoc == port.CredentialTargetLocationHeader {
			fieldName = "Authorization"
		}
		prefix := a.ValuePrefix
		if prefix == "" {
			prefix = a.Prefix
		}
		spec.AuthBindings = append(spec.AuthBindings, AuthBindingSpec{
			SlotName:       slot,
			TargetLocation: targetLoc,
			FieldName:      fieldName,
			ValuePrefix:    prefix,
		})
	}
	return spec, nil
}

func validateYAMLNodeBounds(n *yaml.Node, depth int, aliases, expandedNodes *int, visiting map[*yaml.Node]bool) error {
	if n == nil {
		return nil
	}
	*expandedNodes++
	if *expandedNodes > MaxExpandedYAMLNodes {
		return fmt.Errorf("%w: YAML expanded node count exceeds %d", ErrInvalidDeclarativeSpec, MaxExpandedYAMLNodes)
	}
	if depth > MaxJSONDepth {
		return fmt.Errorf("%w: YAML depth > %d", ErrJSONDepthExceeded, MaxJSONDepth)
	}
	if visiting[n] {
		return fmt.Errorf("%w: recursive YAML alias cycle prohibited", ErrInvalidDeclarativeSpec)
	}
	if n.Kind == yaml.AliasNode {
		*aliases++
		if *aliases > 64 {
			return fmt.Errorf("%w: too many YAML aliases (> 64)", ErrInvalidDeclarativeSpec)
		}
		if n.Alias == nil {
			return nil
		}
		visiting[n] = true
		err := validateYAMLNodeBounds(n.Alias, depth, aliases, expandedNodes, visiting)
		delete(visiting, n)
		return err
	}
	visiting[n] = true
	defer delete(visiting, n)
	for _, c := range n.Content {
		if err := validateYAMLNodeBounds(c, depth+1, aliases, expandedNodes, visiting); err != nil {
			return err
		}
	}
	return nil
}

// DefaultOpenAICompatibleSpec returns a pre-filled OpenAI-compatible Spec with the given slug & hosts.
func DefaultOpenAICompatibleSpec(slug string, allowedHosts []string) Spec {
	return Spec{
		Slug:              slug,
		SupportedDialects: []string{"openai_chat"},
		AllowedHosts:      append([]string(nil), allowedHosts...),
		AuthBindings: []AuthBindingSpec{
			{
				SlotName:       "api_key",
				TargetLocation: port.CredentialTargetLocationHeader,
				FieldName:      "Authorization",
				ValuePrefix:    "Bearer ",
			},
		},
		ValidationProbe: ValidationProbeSpec{
			Method:          "GET",
			PathTemplate:    "/v1/models",
			SuccessStatuses: []int32{200},
			ModelsArrayPath: "data",
			ModelIDField:    "id",
		},
		RequestMapping: RequestMappingSpec{
			Method:            "POST",
			PathTemplate:      "/v1/chat/completions",
			InjectStreamUsage: true,
		},
		ResponseMapping: ResponseMappingSpec{
			ModelPath:        "model",
			FinishReasonPath: "choices.0.finish_reason",
			ContentPath:      "choices.0.message.content",
			RolePath:         "choices.0.message.role",
			UsagePaths: UsagePathSpec{
				InputTokens:     "usage.prompt_tokens",
				OutputTokens:    "usage.completion_tokens",
				CacheReadTokens: "usage.prompt_tokens_details.cached_tokens",
				ReasoningTokens: "usage.completion_tokens_details.reasoning_tokens",
			},
		},
		StreamMapping: StreamMappingSpec{
			DoneSentinel:     "[DONE]",
			DeltaContentPath: "choices.0.delta.content",
			FinishReasonPath: "choices.0.finish_reason",
			ModelPath:        "model",
			UsagePaths: UsagePathSpec{
				InputTokens:     "usage.prompt_tokens",
				OutputTokens:    "usage.completion_tokens",
				CacheReadTokens: "usage.prompt_tokens_details.cached_tokens",
				ReasoningTokens: "usage.completion_tokens_details.reasoning_tokens",
			},
			ErrorMessagePath: "error.message",
		},
		NativeMapping: NativeMappingSpec{
			ModelPath:     "model",
			StreamPath:    "stream",
			MaxTokensPath: "max_tokens",
		},
	}
}

func validateAllowedHostPattern(raw string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(raw))
	if h == "" || strings.Contains(h, "*") || strings.ContainsAny(h, " \t\r\n\x00/@?#") {
		return "", fmt.Errorf("%w: invalid or wildcard allowed host %q", ErrInvalidDeclarativeSpec, raw)
	}
	return h, nil
}

func validateTemplateString(tmpl string, allowQuery bool) ([]templatePart, error) {
	if strings.ContainsAny(tmpl, "\r\n\x00") {
		return nil, fmt.Errorf("%w: control characters prohibited in template %q", ErrInvalidDeclarativeSpec, tmpl)
	}
	if strings.Contains(tmpl, "{{") || strings.Contains(tmpl, "}}") || strings.Contains(tmpl, "${") {
		return nil, fmt.Errorf("%w: %q", ErrRecursiveTemplate, tmpl)
	}
	if strings.Contains(tmpl, "..") {
		return nil, fmt.Errorf("%w: path traversal segments prohibited in template %q", ErrInvalidDeclarativeSpec, tmpl)
	}
	if !allowQuery && strings.Contains(tmpl, "?") {
		return nil, fmt.Errorf("%w: unexpected query string in path segment", ErrInvalidDeclarativeSpec)
	}

	var parts []templatePart
	i := 0
	for i < len(tmpl) {
		openIdx := strings.IndexByte(tmpl[i:], '{')
		if openIdx < 0 {
			// Check for stray '}'
			if strings.IndexByte(tmpl[i:], '}') >= 0 {
				return nil, fmt.Errorf("%w: unmatched closing brace in %q", ErrRecursiveTemplate, tmpl)
			}
			if i < len(tmpl) {
				parts = append(parts, templatePart{literal: tmpl[i:]})
			}
			break
		}
		absOpen := i + openIdx
		if strings.IndexByte(tmpl[i:absOpen], '}') >= 0 {
			return nil, fmt.Errorf("%w: unmatched closing brace in %q", ErrRecursiveTemplate, tmpl)
		}
		if absOpen > i {
			parts = append(parts, templatePart{literal: tmpl[i:absOpen]})
		}
		rest := tmpl[absOpen+1:]
		closeIdx := strings.IndexByte(rest, '}')
		if closeIdx < 0 {
			return nil, fmt.Errorf("%w: unclosed placeholder in %q", ErrRecursiveTemplate, tmpl)
		}
		varName := rest[:closeIdx]
		if strings.ContainsAny(varName, "{}$ \t\r\n/\\?&=#") || varName == "" {
			return nil, fmt.Errorf("%w: invalid placeholder {%s} in %q", ErrRecursiveTemplate, varName, tmpl)
		}
		parts = append(parts, templatePart{varName: varName, isVar: true})
		i = absOpen + 1 + closeIdx + 1
	}
	return parts, nil
}

type templatePart struct {
	literal string
	varName string
	isVar   bool
}

func parseDotPath(raw string) (parsedPath, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return parsedPath{}, nil
	}
	if strings.ContainsAny(p, "{}$*[] \t\r\n\x00") {
		return parsedPath{}, fmt.Errorf("%w: unsupported path syntax %q", ErrInvalidDeclarativeSpec, raw)
	}
	segs := strings.Split(p, ".")
	if len(segs) > MaxPathSegments {
		return parsedPath{}, fmt.Errorf("%w: path %q exceeds max depth %d", ErrJSONDepthExceeded, raw, MaxPathSegments)
	}
	tokens := make([]pathToken, 0, len(segs))
	for _, s := range segs {
		if s == "" {
			return parsedPath{}, fmt.Errorf("%w: empty path segment in %q", ErrInvalidDeclarativeSpec, raw)
		}
		if idx, err := strconv.Atoi(s); err == nil {
			if idx < 0 || idx > 1024 {
				return parsedPath{}, fmt.Errorf("%w: array index %d out of bounds in %q", ErrInvalidDeclarativeSpec, idx, raw)
			}
			tokens = append(tokens, pathToken{key: s, index: idx, isIndex: true})
		} else {
			tokens = append(tokens, pathToken{key: s})
		}
	}
	return parsedPath{raw: p, tokens: tokens}, nil
}

func validateValueDepthAndSafety(v any, depth int) error {
	if depth > MaxJSONDepth {
		return fmt.Errorf("%w: default value nesting depth > %d", ErrJSONDepthExceeded, MaxJSONDepth)
	}
	switch typed := v.(type) {
	case string:
		if strings.Contains(typed, "{{") || strings.Contains(typed, "${") {
			return fmt.Errorf("%w: template syntax prohibited in static fieldDefaults value", ErrRecursiveTemplate)
		}
	case map[string]any:
		for _, child := range typed {
			if err := validateValueDepthAndSafety(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateValueDepthAndSafety(child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateMethodToken(m, fallback string) (string, error) {
	val := strings.ToUpper(strings.TrimSpace(m))
	if val == "" {
		val = fallback
	}
	switch val {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
		return val, nil
	default:
		return "", fmt.Errorf("%w: unsupported HTTP method %q", ErrInvalidDeclarativeSpec, val)
	}
}

func isReservedOrProhibitedHeader(name string) bool {
	if strings.EqualFold(name, "cookie") || strings.EqualFold(name, "set-cookie") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "authorization", "host", "connection", "transfer-encoding", "te",
		"trailer", "upgrade", "content-length", "keep-alive", "proxy-connection",
		"proxy-authorization":
		return true
	default:
		return false
	}
}

func validateBaseURL(raw string, allowedHosts map[string]struct{}) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("%w: invalid defaultBaseUrl %q", ErrInvalidDeclarativeSpec, raw)
	}
	if len(allowedHosts) > 0 {
		hostname := strings.ToLower(u.Hostname())
		hostPort := strings.ToLower(u.Host)
		_, okHost := allowedHosts[hostname]
		_, okHostPort := allowedHosts[hostPort]
		if !okHost && !okHostPort {
			return fmt.Errorf("%w: defaultBaseUrl host %q not permitted by allowedHosts", ErrInvalidDeclarativeSpec, hostPort)
		}
	}
	return nil
}
