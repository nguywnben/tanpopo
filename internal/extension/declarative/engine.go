// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package declarative

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nguywnben/tanpopo/internal/extension/port"
)

var _ port.ProviderRuntime = (*Engine)(nil)

type compiledUsagePaths struct {
	inputTokens      parsedPath
	outputTokens     parsedPath
	cacheReadTokens  parsedPath
	cacheWriteTokens parsedPath
	reasoningTokens  parsedPath
}

// Engine is the immutable compiled Tier-1 Declarative Adapter runtime.
type Engine struct {
	id                string
	slug              string
	nativePrefix      string
	supportedDialects []string
	allowedHosts      map[string]struct{}
	defaultBaseURL    string

	authBindings  map[string]port.CredentialInjectInstruction
	authSlotOrder []string

	probeMethod            string
	probePathParts         []templatePart
	probeDefaultOpenAIPath bool
	probeBodyBytes         []byte
	probeSuccessCodes      map[int32]struct{}
	probeModelsPath        parsedPath
	probeModelIDField      parsedPath

	reqMethod            string
	reqPathParts         []templatePart
	reqDefaultOpenAIPath bool
	staticHeaders        map[string]string
	renames              []compiledRename
	defaults             []compiledDefault
	drops                []parsedPath
	injectStreamUsage    bool
	forceStreamUsage     bool

	respModelPath        parsedPath
	respFinishReasonPath parsedPath
	respContentPath      parsedPath
	respRolePath         parsedPath
	respToolCallsPath    parsedPath
	respUsagePaths       compiledUsagePaths

	streamDoneSentinel       string
	streamDeltaContentPath   parsedPath
	streamDeltaToolCallsPath parsedPath
	streamFinishReasonPath   parsedPath
	streamModelPath          parsedPath
	streamUsagePaths         compiledUsagePaths
	streamErrorMsgPath       parsedPath

	reauthStatuses    map[int32]struct{}
	retryableStatuses map[int32]struct{}
	errMsgPath        parsedPath
	errCodePath       parsedPath

	nativeModelPath     parsedPath
	nativeStreamPath    parsedPath
	nativeMaxTokensPath parsedPath
}

// CompileSpec validates and compiles a finite declarative Spec into an immutable Engine.
func CompileSpec(spec Spec) (*Engine, error) {
	if strings.TrimSpace(spec.Slug) == "" && strings.TrimSpace(spec.ID) == "" {
		return nil, fmt.Errorf("%w: slug or id is required", ErrInvalidDeclarativeSpec)
	}
	allowedHosts := make(map[string]struct{}, len(spec.AllowedHosts))
	for _, rawHost := range spec.AllowedHosts {
		h, err := validateAllowedHostPattern(rawHost)
		if err != nil {
			return nil, err
		}
		allowedHosts[h] = struct{}{}
	}
	if err := validateBaseURL(spec.DefaultBaseURL, allowedHosts); err != nil {
		return nil, err
	}

	eng := &Engine{
		id:                     strings.TrimSpace(spec.ID),
		slug:                   strings.TrimSpace(spec.Slug),
		nativePrefix:           strings.TrimSpace(spec.NativePrefix),
		supportedDialects:      append([]string(nil), spec.SupportedDialects...),
		allowedHosts:           allowedHosts,
		defaultBaseURL:         strings.TrimSpace(spec.DefaultBaseURL),
		authBindings:           make(map[string]port.CredentialInjectInstruction, len(spec.AuthBindings)),
		staticHeaders:          make(map[string]string, len(spec.RequestMapping.StaticHeaders)),
		probeSuccessCodes:      make(map[int32]struct{}),
		reauthStatuses:         make(map[int32]struct{}),
		retryableStatuses:      make(map[int32]struct{}),
		injectStreamUsage:      spec.RequestMapping.InjectStreamUsage,
		forceStreamUsage:       spec.canonicalOpenAI,
		probeDefaultOpenAIPath: spec.canonicalOpenAI && strings.TrimSpace(spec.ValidationProbe.PathTemplate) == "",
		reqDefaultOpenAIPath:   spec.canonicalOpenAI && strings.TrimSpace(spec.RequestMapping.PathTemplate) == "",
	}

	if err := eng.compileAuthBindings(spec.AuthBindings); err != nil {
		return nil, err
	}
	if err := eng.compileValidationProbe(spec.ValidationProbe); err != nil {
		return nil, err
	}
	if err := eng.compileRequestMapping(spec.RequestMapping); err != nil {
		return nil, err
	}
	if err := eng.compileResponseMapping(spec.ResponseMapping); err != nil {
		return nil, err
	}
	if err := eng.compileStreamMapping(spec.StreamMapping); err != nil {
		return nil, err
	}
	if err := eng.compileErrorMapping(spec.ErrorMapping); err != nil {
		return nil, err
	}
	if err := eng.compileNativeMapping(spec.NativeMapping); err != nil {
		return nil, err
	}
	return eng, nil
}

func (e *Engine) compileAuthBindings(bindings []AuthBindingSpec) error {
	if len(bindings) > 32 {
		return fmt.Errorf("%w: too many auth bindings (%d > 32)", ErrInvalidDeclarativeSpec, len(bindings))
	}
	for _, b := range bindings {
		slot := strings.TrimSpace(b.SlotName)
		if slot == "" {
			return fmt.Errorf("%w: auth binding slot name cannot be empty", ErrInvalidDeclarativeSpec)
		}
		if _, dup := e.authBindings[slot]; dup {
			return fmt.Errorf("%w: duplicate auth binding slot %q", ErrInvalidDeclarativeSpec, slot)
		}
		loc := b.TargetLocation
		if loc == "" {
			loc = port.CredentialTargetLocationHeader
		}
		instr := port.CredentialInjectInstruction{
			SecretSlotName: slot,
			TargetLocation: loc,
			FieldName:      strings.TrimSpace(b.FieldName),
			ValuePrefix:    b.ValuePrefix,
		}
		if err := instr.Validate(); err != nil {
			return err
		}
		e.authBindings[slot] = instr
		e.authSlotOrder = append(e.authSlotOrder, slot)
	}
	return nil
}

func (e *Engine) compileValidationProbe(vp ValidationProbeSpec) error {
	m, err := validateMethodToken(vp.Method, http.MethodGet)
	if err != nil {
		return err
	}
	e.probeMethod = m

	pathTmpl := strings.TrimSpace(vp.PathTemplate)
	if pathTmpl == "" {
		pathTmpl = "/v1/models"
	}
	parts, err := validateTemplateString(pathTmpl, true)
	if err != nil {
		return err
	}
	e.probePathParts = parts

	if strings.TrimSpace(vp.BodyJSON) != "" {
		if err := ValidateJSONDepth([]byte(vp.BodyJSON), MaxJSONDepth); err != nil {
			return err
		}
		e.probeBodyBytes = []byte(vp.BodyJSON)
	}

	if len(vp.SuccessStatuses) == 0 {
		e.probeSuccessCodes[http.StatusOK] = struct{}{}
	} else {
		for _, code := range vp.SuccessStatuses {
			if code < 100 || code > 599 {
				return fmt.Errorf("%w: invalid probe HTTP status %d", ErrInvalidDeclarativeSpec, code)
			}
			e.probeSuccessCodes[code] = struct{}{}
		}
	}

	modelsPath := strings.TrimSpace(vp.ModelsArrayPath)
	if modelsPath == "" {
		modelsPath = "data"
	}
	e.probeModelsPath, err = parseDotPath(modelsPath)
	if err != nil {
		return err
	}

	idField := strings.TrimSpace(vp.ModelIDField)
	if idField == "" {
		idField = "id"
	}
	e.probeModelIDField, err = parseDotPath(idField)
	return err
}

func (e *Engine) compileRequestMapping(rm RequestMappingSpec) error {
	m, err := validateMethodToken(rm.Method, http.MethodPost)
	if err != nil {
		return err
	}
	e.reqMethod = m

	pathTmpl := strings.TrimSpace(rm.PathTemplate)
	if pathTmpl == "" {
		pathTmpl = "/v1/chat/completions"
	}
	parts, err := validateTemplateString(pathTmpl, true)
	if err != nil {
		return err
	}
	e.reqPathParts = parts

	ruleCount := len(rm.StaticHeaders) + len(rm.FieldRenames) + len(rm.FieldDefaults) + len(rm.DropFields)
	if ruleCount > MaxOperatorSteps {
		return fmt.Errorf("%w: total static rules (%d) exceed %d", ErrOperatorBudgetExceeded, ruleCount, MaxOperatorSteps)
	}

	for k, v := range rm.StaticHeaders {
		tk := strings.TrimSpace(k)
		if strings.EqualFold(tk, "cookie") || strings.EqualFold(tk, "set-cookie") {
			return fmt.Errorf("%w: header %q forbidden in staticHeaders", port.ErrProhibitedCookieInjection, k)
		}
		if isReservedOrProhibitedHeader(tk) || strings.ContainsAny(tk, " \t\r\n\x00:") || strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("%w: invalid or prohibited static header %q", ErrInvalidDeclarativeSpec, k)
		}
		e.staticHeaders[tk] = v
	}

	renameKeys := make([]string, 0, len(rm.FieldRenames))
	for from := range rm.FieldRenames {
		renameKeys = append(renameKeys, from)
	}
	sort.Strings(renameKeys)
	for _, fromRaw := range renameKeys {
		fromPath, err := parseDotPath(fromRaw)
		if err != nil {
			return err
		}
		toPath, err := parseDotPath(rm.FieldRenames[fromRaw])
		if err != nil {
			return err
		}
		if len(fromPath.tokens) == 0 || len(toPath.tokens) == 0 {
			return fmt.Errorf("%w: empty rename source or target", ErrInvalidDeclarativeSpec)
		}
		e.renames = append(e.renames, compiledRename{from: fromPath, to: toPath})
	}

	defaultKeys := make([]string, 0, len(rm.FieldDefaults))
	for k := range rm.FieldDefaults {
		defaultKeys = append(defaultKeys, k)
	}
	sort.Strings(defaultKeys)
	for _, k := range defaultKeys {
		p, err := parseDotPath(k)
		if err != nil {
			return err
		}
		if len(p.tokens) == 0 {
			return fmt.Errorf("%w: empty fieldDefaults path", ErrInvalidDeclarativeSpec)
		}
		val := rm.FieldDefaults[k]
		if err := validateValueDepthAndSafety(val, 1); err != nil {
			return err
		}
		e.defaults = append(e.defaults, compiledDefault{to: p, value: deepCloneJSONSafe(val)})
	}

	drops := append([]string(nil), rm.DropFields...)
	sort.Strings(drops)
	for _, d := range drops {
		p, err := parseDotPath(d)
		if err != nil {
			return err
		}
		if len(p.tokens) == 0 {
			return fmt.Errorf("%w: empty dropFields path", ErrInvalidDeclarativeSpec)
		}
		e.drops = append(e.drops, p)
	}
	return nil
}

func compileUsagePaths(spec UsagePathSpec, fallback UsagePathSpec) (compiledUsagePaths, error) {
	pick := func(v, fb string) string {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return fb
	}
	var out compiledUsagePaths
	var err error
	if out.inputTokens, err = parseDotPath(pick(spec.InputTokens, fallback.InputTokens)); err != nil {
		return out, err
	}
	if out.outputTokens, err = parseDotPath(pick(spec.OutputTokens, fallback.OutputTokens)); err != nil {
		return out, err
	}
	if out.cacheReadTokens, err = parseDotPath(pick(spec.CacheReadTokens, fallback.CacheReadTokens)); err != nil {
		return out, err
	}
	if out.cacheWriteTokens, err = parseDotPath(pick(spec.CacheWriteTokens, fallback.CacheWriteTokens)); err != nil {
		return out, err
	}
	if out.reasoningTokens, err = parseDotPath(pick(spec.ReasoningTokens, fallback.ReasoningTokens)); err != nil {
		return out, err
	}
	return out, nil
}

func (e *Engine) compileResponseMapping(rm ResponseMappingSpec) error {
	orDefault := func(v, d string) string {
		if strings.TrimSpace(v) != "" {
			return v
		}
		return d
	}
	var err error
	if e.respModelPath, err = parseDotPath(orDefault(rm.ModelPath, "model")); err != nil {
		return err
	}
	if e.respFinishReasonPath, err = parseDotPath(orDefault(rm.FinishReasonPath, "choices.0.finish_reason")); err != nil {
		return err
	}
	if e.respContentPath, err = parseDotPath(orDefault(rm.ContentPath, "choices.0.message.content")); err != nil {
		return err
	}
	if e.respRolePath, err = parseDotPath(orDefault(rm.RolePath, "choices.0.message.role")); err != nil {
		return err
	}
	if e.respToolCallsPath, err = parseDotPath(orDefault(rm.ToolCallsPath, "choices.0.message.tool_calls")); err != nil {
		return err
	}
	e.respUsagePaths, err = compileUsagePaths(rm.UsagePaths, UsagePathSpec{
		InputTokens:     "usage.prompt_tokens",
		OutputTokens:    "usage.completion_tokens",
		CacheReadTokens: "usage.prompt_tokens_details.cached_tokens",
		ReasoningTokens: "usage.completion_tokens_details.reasoning_tokens",
	})
	return err
}

func (e *Engine) compileStreamMapping(sm StreamMappingSpec) error {
	orDefault := func(v, d string) string {
		if strings.TrimSpace(v) != "" {
			return v
		}
		return d
	}
	e.streamDoneSentinel = orDefault(sm.DoneSentinel, "[DONE]")
	var err error
	if e.streamDeltaContentPath, err = parseDotPath(orDefault(sm.DeltaContentPath, "choices.0.delta.content")); err != nil {
		return err
	}
	if e.streamDeltaToolCallsPath, err = parseDotPath(orDefault(sm.DeltaToolCallsPath, "choices.0.delta.tool_calls")); err != nil {
		return err
	}
	if e.streamFinishReasonPath, err = parseDotPath(orDefault(sm.FinishReasonPath, "choices.0.finish_reason")); err != nil {
		return err
	}
	if e.streamModelPath, err = parseDotPath(orDefault(sm.ModelPath, "model")); err != nil {
		return err
	}
	if e.streamErrorMsgPath, err = parseDotPath(orDefault(sm.ErrorMessagePath, "error.message")); err != nil {
		return err
	}
	e.streamUsagePaths, err = compileUsagePaths(sm.UsagePaths, UsagePathSpec{
		InputTokens:     "usage.prompt_tokens",
		OutputTokens:    "usage.completion_tokens",
		CacheReadTokens: "usage.prompt_tokens_details.cached_tokens",
		ReasoningTokens: "usage.completion_tokens_details.reasoning_tokens",
	})
	return err
}

func (e *Engine) compileErrorMapping(em ErrorMappingSpec) error {
	if len(em.ReauthStatuses) == 0 {
		e.reauthStatuses[http.StatusUnauthorized] = struct{}{}
		e.reauthStatuses[http.StatusForbidden] = struct{}{}
	} else {
		for _, c := range em.ReauthStatuses {
			e.reauthStatuses[c] = struct{}{}
		}
	}
	if len(em.RetryableStatuses) == 0 {
		for _, c := range []int32{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
			e.retryableStatuses[c] = struct{}{}
		}
	} else {
		for _, c := range em.RetryableStatuses {
			e.retryableStatuses[c] = struct{}{}
		}
	}
	var err error
	msgPath := strings.TrimSpace(em.MessagePath)
	if msgPath == "" {
		msgPath = "error.message"
	}
	if e.errMsgPath, err = parseDotPath(msgPath); err != nil {
		return err
	}
	codePath := strings.TrimSpace(em.CodePath)
	if codePath == "" {
		codePath = "error.code"
	}
	e.errCodePath, err = parseDotPath(codePath)
	return err
}

func (e *Engine) compileNativeMapping(nm NativeMappingSpec) error {
	orDefault := func(v, d string) string {
		if strings.TrimSpace(v) != "" {
			return v
		}
		return d
	}
	var err error
	if e.nativeModelPath, err = parseDotPath(orDefault(nm.ModelPath, "model")); err != nil {
		return err
	}
	if e.nativeStreamPath, err = parseDotPath(orDefault(nm.StreamPath, "stream")); err != nil {
		return err
	}
	e.nativeMaxTokensPath, err = parseDotPath(orDefault(nm.MaxTokensPath, "max_tokens"))
	return err
}

func (e *Engine) resolveBaseURLAndVars(configJSON string) (string, map[string]string, error) {
	vars := make(map[string]string)
	baseURL := e.defaultBaseURL
	if strings.TrimSpace(configJSON) != "" {
		cfg, err := decodeBoundedJSONMap([]byte(configJSON))
		if err != nil {
			return "", nil, err
		}
		if rawBase, ok := cfg["endpoint_base_url"].(string); ok && strings.TrimSpace(rawBase) != "" {
			baseURL = strings.TrimSpace(rawBase)
		} else if rawBase, ok := cfg["base_url"].(string); ok && strings.TrimSpace(rawBase) != "" {
			baseURL = strings.TrimSpace(rawBase)
		}
		for k, v := range cfg {
			if s, ok := v.(string); ok {
				vars[k] = s
			}
		}
	}
	if baseURL == "" {
		return "", nil, fmt.Errorf("%w: endpoint_base_url is required", ErrInvalidDeclarativeSpec)
	}
	return baseURL, vars, nil
}

func (e *Engine) buildCredentialInjections(slotNames []string) ([]port.CredentialInjectInstruction, error) {
	if len(slotNames) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(slotNames))
	out := make([]port.CredentialInjectInstruction, 0, len(slotNames))
	for _, s := range slotNames {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		instr, ok := e.authBindings[trimmed]
		if !ok {
			return nil, fmt.Errorf("%w: unbound secret slot %q requested", ErrInvalidDeclarativeSpec, trimmed)
		}
		out = append(out, instr)
	}
	return out, nil
}

// BuildValidationProbe implements port.ProviderRuntime Phase-1 connection probe construction.
func (e *Engine) BuildValidationProbe(ctx context.Context, in port.ValidationProbeInput) (port.UpstreamHttpSpec, error) {
	if err := ctx.Err(); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	budget := newStepBudget()
	baseURL, vars, err := e.resolveBaseURLAndVars(in.ConfigJSON)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	targetURL, err := renderEndpointURL(baseURL, e.probePathParts, e.probeDefaultOpenAIPath, vars, e.allowedHosts, budget)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	inj, err := e.buildCredentialInjections(in.SecretSlotNames)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	headers := map[string]string{"Accept": "application/json"}
	if len(e.probeBodyBytes) > 0 {
		headers["Content-Type"] = "application/json"
	}
	spec := port.UpstreamHttpSpec{
		Method:               e.probeMethod,
		TargetURL:            targetURL,
		SafeHeaders:          headers,
		BodyBytes:            append([]byte(nil), e.probeBodyBytes...),
		CredentialInjections: inj,
	}
	if err := spec.Validate(); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	return spec, nil
}

// VerifyValidationResult implements port.ProviderRuntime Phase-2 probe response inspection.
func (e *Engine) VerifyValidationResult(ctx context.Context, in port.ValidationResultInput) (port.ValidationVerdict, error) {
	if err := ctx.Err(); err != nil {
		return port.ValidationVerdict{}, err
	}
	if _, isReauth := e.reauthStatuses[in.HTTPStatus]; isReauth {
		normErr, _ := e.NormalizeError(ctx, port.NormalizeErrorInput{
			RequestID:        in.RequestID,
			HTTPStatus:       in.HTTPStatus,
			SanitizedHeaders: in.SanitizedHeaders,
			SanitizedBody:    in.ResponseBody,
		})
		return port.ValidationVerdict{
			Valid:            false,
			FailureCode:      "auth_failed",
			SanitizedMessage: normErr.SanitizedPublicMessage,
		}, nil
	}
	if _, ok := e.probeSuccessCodes[in.HTTPStatus]; !ok {
		normErr, _ := e.NormalizeError(ctx, port.NormalizeErrorInput{
			RequestID:        in.RequestID,
			HTTPStatus:       in.HTTPStatus,
			SanitizedHeaders: in.SanitizedHeaders,
			SanitizedBody:    in.ResponseBody,
		})
		return port.ValidationVerdict{
			Valid:            false,
			FailureCode:      normErr.CanonicalCode,
			SanitizedMessage: normErr.SanitizedPublicMessage,
		}, nil
	}

	var modelIDs []string
	if len(bytes.TrimSpace(in.ResponseBody)) > 0 {
		bodyMap, err := decodeBoundedJSONMap(in.ResponseBody)
		if err != nil {
			return port.ValidationVerdict{
				Valid:            false,
				FailureCode:      "invalid_probe_response",
				SanitizedMessage: sanitizePublicMessage(err.Error()),
			}, nil
		}
		budget := newStepBudget()
		arrAny, found, gerr := getByPath(bodyMap, e.probeModelsPath, budget)
		if gerr != nil {
			return port.ValidationVerdict{
				Valid:            false,
				FailureCode:      "probe_budget_exceeded",
				SanitizedMessage: "probe response exceeded declarative complexity budget",
			}, nil
		}
		if found {
			if items, ok := arrAny.([]any); ok {
				for _, item := range items {
					idVal, okID, ierr := getByPath(item, e.probeModelIDField, budget)
					if ierr != nil {
						break
					}
					if okID {
						if s, okStr := idVal.(string); okStr && strings.TrimSpace(s) != "" {
							modelIDs = append(modelIDs, s)
						}
					}
				}
			}
		}
	}

	return port.ValidationVerdict{
		Valid:            true,
		DetectedModelIDs: modelIDs,
	}, nil
}

// TransformRequest implements port.ProviderRuntime request mapping with CR-09 stream_options injection.
func (e *Engine) TransformRequest(ctx context.Context, in port.TransformRequestInput) (port.UpstreamHttpSpec, error) {
	if err := ctx.Err(); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	budget := newStepBudget()

	baseURL, vars, err := e.resolveBaseURLAndVars(in.ConfigJSON)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}

	payload, err := decodeBoundedJSONMap(in.NormalizedRequestJSON)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}

	if err := e.applyRequestRules(payload, budget); err != nil {
		return port.UpstreamHttpSpec{}, err
	}

	if modelStr, ok := payload["model"].(string); ok && strings.TrimSpace(modelStr) != "" {
		vars["model"] = strings.TrimSpace(modelStr)
	}

	targetURL, err := renderEndpointURL(baseURL, e.reqPathParts, e.reqDefaultOpenAIPath, vars, e.allowedHosts, budget)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}

	slots := make([]string, 0, len(in.SecretSlotRefs))
	for k := range in.SecretSlotRefs {
		slots = append(slots, k)
	}
	sort.Strings(slots)
	inj, err := e.buildCredentialInjections(slots)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return port.UpstreamHttpSpec{}, fmt.Errorf("%w: encode transformed body: %v", port.ErrExecutionFault, err)
	}

	headers := make(map[string]string, len(e.staticHeaders)+2)
	headers["Content-Type"] = "application/json"
	headers["Accept"] = "application/json"
	for k, v := range e.staticHeaders {
		headers[k] = v
	}

	spec := port.UpstreamHttpSpec{
		Method:               e.reqMethod,
		TargetURL:            targetURL,
		SafeHeaders:          headers,
		BodyBytes:            bodyBytes,
		OutboundBlobSplices:  append([]port.BlobRef(nil), in.BlobRefs...),
		CredentialInjections: inj,
	}
	if err := spec.Validate(); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	return spec, nil
}

func (e *Engine) applyRequestRules(payload map[string]any, budget *stepBudget) error {
	for _, d := range e.defaults {
		if err := setByPath(payload, d.to, d.value, false, budget); err != nil {
			return err
		}
	}
	for _, r := range e.renames {
		val, found, err := getByPath(payload, r.from, budget)
		if err != nil {
			return err
		}
		if found {
			if err := setByPath(payload, r.to, val, true, budget); err != nil {
				return err
			}
			if r.from.raw != r.to.raw {
				if err := deleteByPath(payload, r.from, budget); err != nil {
					return err
				}
			}
		}
	}
	for _, drop := range e.drops {
		if err := deleteByPath(payload, drop, budget); err != nil {
			return err
		}
	}
	if e.injectStreamUsage {
		if err := budget.consume(1); err != nil {
			return err
		}
		if streamOn, ok := extractBool(payload["stream"]); ok && streamOn {
			opts, _ := payload["stream_options"].(map[string]any)
			if opts == nil {
				opts = make(map[string]any, 1)
				payload["stream_options"] = opts
			}
			if _, hasInclude := opts["include_usage"]; e.forceStreamUsage || !hasInclude {
				opts["include_usage"] = true
			}
		}
	}
	return nil
}

// TransformResponse implements port.ProviderRuntime synchronous response normalization.
func (e *Engine) TransformResponse(ctx context.Context, in port.TransformResponseInput) (port.NormalizedInferenceResponse, error) {
	if err := ctx.Err(); err != nil {
		return port.NormalizedInferenceResponse{}, err
	}
	budget := newStepBudget()
	rawMap, err := decodeBoundedJSONMap(in.RawResponseBody)
	if err != nil {
		return port.NormalizedInferenceResponse{}, fmt.Errorf("%w: %w", ErrUpstreamMalformedResponse, err)
	}

	modelID := e.extractStringPath(rawMap, e.respModelPath, budget)
	finishReason := e.extractStringPath(rawMap, e.respFinishReasonPath, budget)
	content := e.extractStringPath(rawMap, e.respContentPath, budget)
	role := e.extractStringPath(rawMap, e.respRolePath, budget)
	if role == "" {
		role = "assistant"
	}
	toolCalls, err := extractToolCalls(rawMap, e.respToolCallsPath, budget, true)
	if err != nil {
		return port.NormalizedInferenceResponse{}, err
	}

	usage, err := extractUsageFromMap(rawMap, e.respUsagePaths, budget)
	if err != nil {
		return port.NormalizedInferenceResponse{}, err
	}

	msgMap := map[string]any{
		"role":    role,
		"content": content,
	}
	if len(toolCalls) > 0 {
		msgMap["tool_calls"] = toolCalls
	}

	normObj := map[string]any{
		"object": "chat.completion",
		"model":  modelID,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       msgMap,
				"finish_reason": finishReason,
			},
		},
		"usage": usage,
	}
	if idStr, ok := rawMap["id"].(string); ok && idStr != "" {
		normObj["id"] = idStr
	}

	normBytes, err := json.Marshal(normObj)
	if err != nil {
		return port.NormalizedInferenceResponse{}, fmt.Errorf("%w: marshal normalized response: %v", port.ErrExecutionFault, err)
	}

	return port.NormalizedInferenceResponse{
		NormalizedResponseJSON: normBytes,
		Usage:                  usage,
		UpstreamModelID:        modelID,
		FinishReason:           finishReason,
	}, nil
}

func extractToolCalls(root any, p parsedPath, budget *stepBudget, complete bool) ([]map[string]any, error) {
	if len(p.tokens) == 0 {
		return nil, nil
	}
	val, found, err := getByPath(root, p, budget)
	if err != nil {
		return nil, err
	}
	if !found || val == nil {
		return nil, nil
	}
	rawArr, ok := val.([]any)
	if !ok && complete {
		return nil, ErrUpstreamMalformedResponse
	}
	if len(rawArr) == 0 {
		return nil, nil
	}

	type indexedCall struct {
		hasIdx bool
		idx    int64
		entry  map[string]any
	}
	if err := budget.consume(len(rawArr)); err != nil {
		return nil, err
	}
	items := make([]indexedCall, 0, len(rawArr))
	for _, rawElem := range rawArr {
		src, ok := rawElem.(map[string]any)
		if complete {
			fn, _ := src["function"].(map[string]any)
			args, _ := fn["arguments"].(string)
			if !json.Valid([]byte(args)) {
				return nil, ErrUpstreamMalformedResponse
			}
		}
		if !ok {
			continue
		}
		entry := make(map[string]any, 4)
		var (
			idxVal int64
			hasIdx bool
		)
		if rawIdx, exists := src["index"]; exists {
			number, _ := rawIdx.(json.Number)
			n, err := number.Int64()
			if err != nil || n < 0 {
				return nil, ErrUpstreamMalformedResponse
			}
			entry["index"] = n
			idxVal = n
			hasIdx = true
		}
		if idStr, ok := src["id"].(string); ok && idStr != "" {
			entry["id"] = idStr
		}
		if typStr, ok := src["type"].(string); ok && typStr != "" {
			entry["type"] = typStr
		}
		if fnRaw, ok := src["function"].(map[string]any); ok {
			fnOut := make(map[string]any, 2)
			if name, ok := fnRaw["name"].(string); ok && name != "" {
				fnOut["name"] = name
			}
			// Preserve raw JSON argument delta fragment verbatim without trimming/mutating.
			if args, ok := fnRaw["arguments"].(string); ok {
				fnOut["arguments"] = args
			}
			if len(fnOut) > 0 {
				entry["function"] = fnOut
			}
		}
		if len(entry) > 0 {
			items = append(items, indexedCall{
				hasIdx: hasIdx,
				idx:    idxVal,
				entry:  entry,
			})
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].hasIdx != items[j].hasIdx {
			return items[i].hasIdx
		}
		return items[i].hasIdx && items[i].idx < items[j].idx
	})
	out := make([]map[string]any, len(items))
	for i := range items {
		out[i] = items[i].entry
	}
	return out, nil
}

func (e *Engine) extractStringPath(root any, p parsedPath, budget *stepBudget) string {
	val, found, err := getByPath(root, p, budget)
	if err != nil || !found {
		return ""
	}
	s, _ := extractString(val)
	return s
}

func extractUsageFromMap(root any, paths compiledUsagePaths, budget *stepBudget) (*port.NormalizedUsage, error) {
	readOptInt := func(p parsedPath) (int64, bool, error) {
		if len(p.tokens) == 0 {
			return 0, false, nil
		}
		v, found, err := getByPath(root, p, budget)
		if err != nil {
			return 0, false, err
		}
		if !found {
			return 0, false, nil
		}
		n, ok := extractInt64(v)
		if !ok || n < 0 {
			return 0, false, nil
		}
		return n, true, nil
	}

	inTok, hasIn, err := readOptInt(paths.inputTokens)
	if err != nil {
		return nil, err
	}
	outTok, hasOut, err := readOptInt(paths.outputTokens)
	if err != nil {
		return nil, err
	}
	cacheRead, hasCR, err := readOptInt(paths.cacheReadTokens)
	if err != nil {
		return nil, err
	}
	cacheWrite, hasCW, err := readOptInt(paths.cacheWriteTokens)
	if err != nil {
		return nil, err
	}
	reasoning, hasReas, err := readOptInt(paths.reasoningTokens)
	if err != nil {
		return nil, err
	}

	accuracy := port.UsageAccuracyUnmeteredFlagged
	if hasIn || hasOut || hasCR || hasCW || hasReas {
		accuracy = port.UsageAccuracyExact
	}

	return &port.NormalizedUsage{
		InputTokens:      inTok,
		OutputTokens:     outTok,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		ReasoningTokens:  reasoning,
		Accuracy:         accuracy,
	}, nil
}

// TransformStreamChunk implements bounded SSE chunk parsing & incremental usage emission.
func (e *Engine) TransformStreamChunk(ctx context.Context, in port.StreamChunkInput) (port.StreamChunkOutput, error) {
	if err := ctx.Err(); err != nil {
		return port.StreamChunkOutput{}, err
	}
	if len(in.RawUpstreamChunk) > MaxPayloadBytes {
		return port.StreamChunkOutput{}, fmt.Errorf("%w: stream chunk exceeds max %d bytes", ErrInvalidDeclarativeSpec, MaxPayloadBytes)
	}
	if len(in.SessionState) > MaxSessionStateBytes {
		return port.StreamChunkOutput{}, fmt.Errorf("%w: session_state exceeds %d bytes", port.ErrExecutionFault, MaxSessionStateBytes)
	}

	combined := make([]byte, 0, len(in.SessionState)+len(in.RawUpstreamChunk))
	combined = append(combined, in.SessionState...)
	combined = append(combined, in.RawUpstreamChunk...)

	budget := newStepBudget()
	var frames [][]byte
	var latestUsage *port.NormalizedUsage
	completed := false

	pos := 0
	linesProcessed := 0
	for pos < len(combined) {
		nl := bytes.IndexByte(combined[pos:], '\n')
		var line []byte
		if nl < 0 {
			if !in.IsEOF {
				break
			}
			line = combined[pos:]
			pos = len(combined)
		} else {
			line = combined[pos : pos+nl]
			pos += nl + 1
		}

		linesProcessed++
		if linesProcessed > MaxSSELinesPerChunk {
			return port.StreamChunkOutput{}, ErrOperatorBudgetExceeded
		}
		if err := budget.consume(1); err != nil {
			return port.StreamChunkOutput{}, err
		}

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == ':' {
			continue
		}
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		dataPayload := bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
		if len(dataPayload) == 0 {
			continue
		}
		if string(dataPayload) == e.streamDoneSentinel {
			completed = true
			frames = append(frames, []byte("data: [DONE]\n\n"))
			continue
		}

		chunkObj, err := decodeBoundedJSONMap(dataPayload)
		if err != nil {
			return port.StreamChunkOutput{}, err
		}

		// Reset operator budget per decoded SSE message so single chunks holding
		// up to MaxSSELinesPerChunk frames can complete without false exhaustion.
		frameBudget := newStepBudget()
		if errMsg := e.extractStringPath(chunkObj, e.streamErrorMsgPath, frameBudget); errMsg != "" {
			return port.StreamChunkOutput{}, fmt.Errorf("%w: upstream stream error: %s", port.ErrExecutionFault, sanitizePublicMessage(errMsg))
		}

		delta := e.extractStringPath(chunkObj, e.streamDeltaContentPath, frameBudget)
		toolCalls, err := extractToolCalls(chunkObj, e.streamDeltaToolCallsPath, frameBudget, false)
		if err != nil {
			return port.StreamChunkOutput{}, err
		}
		finishReason := e.extractStringPath(chunkObj, e.streamFinishReasonPath, frameBudget)
		modelID := e.extractStringPath(chunkObj, e.streamModelPath, frameBudget)

		u, uerr := extractUsageFromMap(chunkObj, e.streamUsagePaths, frameBudget)
		if uerr != nil {
			return port.StreamChunkOutput{}, uerr
		}
		if u != nil && u.Accuracy == port.UsageAccuracyExact {
			latestUsage = u
		}

		deltaObj := map[string]any{}
		if delta != "" {
			deltaObj["content"] = delta
		}
		if len(toolCalls) > 0 {
			deltaObj["tool_calls"] = toolCalls
		}
		frameMap := map[string]any{
			"object": "chat.completion.chunk",
			"model":  modelID,
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         deltaObj,
					"finish_reason": finishReasonOrNil(finishReason),
				},
			},
		}
		if idStr, ok := chunkObj["id"].(string); ok && idStr != "" {
			frameMap["id"] = idStr
		}
		if latestUsage != nil && u != nil && u.Accuracy == port.UsageAccuracyExact {
			frameMap["usage"] = u
		}
		encoded, err := json.Marshal(frameMap)
		if err != nil {
			return port.StreamChunkOutput{}, fmt.Errorf("%w: marshal SSE frame: %v", port.ErrExecutionFault, err)
		}
		var sseBuf bytes.Buffer
		sseBuf.Grow(len(encoded) + 8)
		sseBuf.WriteString("data: ")
		sseBuf.Write(encoded)
		sseBuf.WriteString("\n\n")
		frames = append(frames, sseBuf.Bytes())
	}

	var nextState []byte
	if pos < len(combined) {
		rem := combined[pos:]
		if len(rem) > MaxSessionStateBytes {
			return port.StreamChunkOutput{}, fmt.Errorf("%w: incomplete SSE line exceeds %d bytes", port.ErrExecutionFault, MaxSessionStateBytes)
		}
		nextState = append([]byte(nil), rem...)
	}
	if in.IsEOF {
		completed = true
		nextState = nil
	}

	return port.StreamChunkOutput{
		NextSessionState:    nextState,
		NormalizedSSEFrames: frames,
		IncrementalUsage:    latestUsage,
		StreamCompleted:     completed,
	}, nil
}

func finishReasonOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ErrUpstreamMalformedResponse identifies an invalid synchronous tool-call payload.
var ErrUpstreamMalformedResponse = fmt.Errorf("%w: upstream_malformed_response", port.ErrExecutionFault)

// NormalizeError implements port.ProviderRuntime upstream HTTP failure normalization.
func (e *Engine) NormalizeError(ctx context.Context, in port.NormalizeErrorInput) (port.CanonicalProviderError, error) {
	if err := ctx.Err(); err != nil {
		return port.CanonicalProviderError{}, err
	}
	status := in.HTTPStatus
	if (status >= http.StatusOK && status < http.StatusMultipleChoices) || status == http.StatusBadGateway {
		trimmedBody := bytes.TrimSpace(in.SanitizedBody)
		if len(trimmedBody) > 0 {
			body, err := decodeBoundedJSONMap(trimmedBody)
			if err != nil {
				looksLikeJSON := trimmedBody[0] == '{' || trimmedBody[0] == '['
				if (status >= http.StatusOK && status < http.StatusMultipleChoices) || looksLikeJSON {
					return port.CanonicalProviderError{
						CanonicalCode:          "upstream_malformed_response",
						ErrorType:              "server_error",
						RetryablePreStream:     false,
						SanitizedPublicMessage: "upstream returned malformed or truncated JSON response",
					}, nil
				}
			} else {
				_, toolErr := extractToolCalls(body, e.respToolCallsPath, newStepBudget(), true)
				if errors.Is(toolErr, ErrUpstreamMalformedResponse) {
					return port.CanonicalProviderError{
						CanonicalCode:          "upstream_malformed_response",
						ErrorType:              "server_error",
						RetryablePreStream:     false,
						SanitizedPublicMessage: "upstream returned malformed tool calls",
					}, nil
				}
			}
		}
	}
	_, triggerReauth := e.reauthStatuses[status]
	_, retryable := e.retryableStatuses[status]

	canonicalCode := "upstream_error"
	errType := "provider_error"
	var cooldownMs int32

	switch {
	case triggerReauth:
		canonicalCode = "upstream_auth_failed"
		errType = "authentication_error"
		retryable = false
	case status == http.StatusTooManyRequests:
		canonicalCode = "upstream_rate_limited"
		errType = "rate_limit_error"
		cooldownMs = parseRetryAfterMs(in.SanitizedHeaders, 5000)
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		canonicalCode = "upstream_timeout"
		errType = "timeout_error"
		retryable = true
		cooldownMs = 1000
	case status >= 500:
		canonicalCode = "upstream_unavailable"
		errType = "server_error"
		cooldownMs = parseRetryAfterMs(in.SanitizedHeaders, 2000)
	case status >= 400:
		canonicalCode = "upstream_bad_request"
		errType = "invalid_request_error"
	}

	pubMsg := fmt.Sprintf("upstream returned HTTP %d", status)
	if len(bytes.TrimSpace(in.SanitizedBody)) > 0 {
		if bodyMap, err := decodeBoundedJSONMap(in.SanitizedBody); err == nil {
			budget := newStepBudget()
			if extracted := e.extractStringPath(bodyMap, e.errMsgPath, budget); extracted != "" {
				pubMsg = sanitizePublicMessage(extracted)
			}
			if !triggerReauth && status != http.StatusTooManyRequests {
				if extractedCode := sanitizeErrorCode(e.extractStringPath(bodyMap, e.errCodePath, budget)); extractedCode != "" {
					canonicalCode = extractedCode
				}
			}
		}
	}

	return port.CanonicalProviderError{
		CanonicalCode:           canonicalCode,
		ErrorType:               errType,
		RetryablePreStream:      retryable,
		CooldownMs:              cooldownMs,
		TriggerConnectionReauth: triggerReauth,
		SanitizedPublicMessage:  pubMsg,
	}, nil
}

func parseRetryAfterMs(headers map[string]string, fallback int32) int32 {
	for k, v := range headers {
		if strings.EqualFold(strings.TrimSpace(k), "retry-after") {
			secs, err := strconv.Atoi(strings.TrimSpace(v))
			if err == nil && secs >= 0 {
				if secs > 3600 {
					secs = 3600
				}
				return int32(secs) * 1000
			}
		}
	}
	return fallback
}

func sanitizeErrorCode(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len(trimmed) > 64 {
		return ""
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		isValid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.'
		if !isValid {
			return ""
		}
	}
	return strings.ToLower(trimmed)
}

func sanitizePublicMessage(s string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(s))
	if len(clean) <= 256 {
		return clean
	}
	cut := 0
	for idx := range clean {
		if idx > 256 {
			break
		}
		cut = idx
	}
	if cut == 0 {
		// Single rune > 256 is impossible in UTF-8, safeguard anyway.
		for idx, r := range clean {
			return clean[idx : idx+utf8.RuneLen(r)]
		}
	}
	return clean[:cut]
}

// ExtractUsage implements port.ProviderRuntime usage extraction with byte-estimate fallback (CR-09).
func (e *Engine) ExtractUsage(ctx context.Context, in port.ExtractUsageInput) (port.NormalizedUsage, error) {
	if err := ctx.Err(); err != nil {
		return port.NormalizedUsage{}, err
	}
	rawTrimmed := bytes.TrimSpace(in.RawResponseOrTrailerBytes)
	isSSE := bytes.HasPrefix(rawTrimmed, []byte("data:"))
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(rawTrimmed, []byte("data:")))

	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte(e.streamDoneSentinel)) {
		m, err := decodeBoundedJSONMap(trimmed)
		if err != nil && errors.Is(err, ErrJSONDepthExceeded) {
			return port.NormalizedUsage{}, err
		}
		if err == nil {
			budget := newStepBudget()
			paths := e.respUsagePaths
			if isSSE {
				paths = e.streamUsagePaths
			}
			usage, uerr := extractUsageFromMap(m, paths, budget)
			if uerr != nil {
				return port.NormalizedUsage{}, uerr
			}
			if usage != nil && usage.Accuracy == port.UsageAccuracyExact {
				return *usage, nil
			}
			if isSSE {
				fallbackUsage, ferr := extractUsageFromMap(m, e.respUsagePaths, budget)
				if ferr != nil {
					return port.NormalizedUsage{}, ferr
				}
				if fallbackUsage != nil && fallbackUsage.Accuracy == port.UsageAccuracyExact {
					return *fallbackUsage, nil
				}
			}
		}
	}

	if in.FallbackUTF8InputBytes > 0 || in.FallbackUTF8OutputBytes > 0 {
		estIn := (in.FallbackUTF8InputBytes + 3) / 4
		estOut := (in.FallbackUTF8OutputBytes + 3) / 4
		return port.NormalizedUsage{
			InputTokens:  estIn,
			OutputTokens: estOut,
			Accuracy:     port.UsageAccuracyEstimated,
		}, nil
	}

	return port.NormalizedUsage{
		Accuracy: port.UsageAccuracyUnmeteredFlagged,
	}, nil
}

// InspectNativeRequest implements port.ProviderRuntime /native/* hint extraction.
func (e *Engine) InspectNativeRequest(ctx context.Context, in port.NativeRequestInput) (port.NativeRoutingHints, error) {
	if err := ctx.Err(); err != nil {
		return port.NativeRoutingHints{}, err
	}
	budget := newStepBudget()
	var hints port.NativeRoutingHints

	if len(bytes.TrimSpace(in.RawRequestBody)) > 0 {
		m, err := decodeBoundedJSONMap(in.RawRequestBody)
		if err != nil {
			return port.NativeRoutingHints{}, err
		}
		hints.TargetModel = e.extractStringPath(m, e.nativeModelPath, budget)
		if val, found, err := getByPath(m, e.nativeStreamPath, budget); err != nil {
			return port.NativeRoutingHints{}, err
		} else if found {
			if b, ok := extractBool(val); ok {
				hints.Stream = b
			}
		}
		if val, found, err := getByPath(m, e.nativeMaxTokensPath, budget); err != nil {
			return port.NativeRoutingHints{}, err
		} else if found {
			if n, ok := extractInt64(val); ok && n >= 0 {
				hints.MaxOutputTokensHint = n
			}
		}
	}

	// Fallback: check /models/{model} segment in UpstreamPath when JSON omitted model.
	if hints.TargetModel == "" && in.UpstreamPath != "" {
		if idx := strings.Index(in.UpstreamPath, "/models/"); idx >= 0 {
			rest := in.UpstreamPath[idx+len("/models/"):]
			if colon := strings.IndexAny(rest, ":/?"); colon >= 0 {
				rest = rest[:colon]
			}
			hints.TargetModel = strings.TrimSpace(rest)
		}
	}

	if len(e.authSlotOrder) > 0 {
		hints.CredentialInjections = make([]port.CredentialInjectInstruction, 0, len(e.authSlotOrder))
		for _, slot := range e.authSlotOrder {
			hints.CredentialInjections = append(hints.CredentialInjections, e.authBindings[slot])
		}
	}
	return hints, nil
}
