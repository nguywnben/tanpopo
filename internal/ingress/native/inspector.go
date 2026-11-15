// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package native implements Tanpopo's Lossless Native Dialect Passthrough
// (/native/{dialect}/{upstreamPath}) ingress plane (TP-FR-0105 / SPEC-EXT-06 / CT-13).
package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
)

const (
	// MaxInspectMetadataBytes caps the JSON metadata slice sent into InspectNativeRequest
	// so large multimodal native requests never breach Wasm linear memory caps while the
	// full rawBody continues untouched to BlindHostCredentialBroker (CR-05).
	MaxInspectMetadataBytes = 256 * 1024
)

// RawBodyHints contains read-only routing hints extracted non-destructively from an ingress request.
type RawBodyHints struct {
	Model     string
	Stream    bool
	MaxTokens int64
}

// InspectResult holds combined routing/credential hints for a native passthrough call.
type InspectResult struct {
	Hints       port.NativeRoutingHints
	TargetModel string
	Stream      bool
}

// UsageRecord captures usage extracted from a completed native passthrough request or stream.
type UsageRecord struct {
	RequestID    string
	Dialect      string
	Model        string
	OrgID        string
	ProjectID    string
	EnvID        string
	VirtualKeyID string
	Usage        port.NormalizedUsage
}

// ExtractRawHints non-destructively inspects top-level model, stream, and max_tokens hints
// from rawBody without mutating or re-serializing the caller's bytes.
func ExtractRawHints(rawBody []byte, upstreamPath, altParam string) (RawBodyHints, error) {
	var hints RawBodyHints
	if strings.Contains(upstreamPath, ":streamGenerateContent") || strings.EqualFold(strings.TrimSpace(altParam), "sse") {
		hints.Stream = true
	}

	trimmed := bytes.TrimSpace(rawBody)
	if len(trimmed) > 0 {
		obj, err := blob.DecodeStrictJSONObject(trimmed)
		if err != nil {
			return RawBodyHints{}, fmt.Errorf("malformed JSON request body")
		}
		if rawModel, ok := obj["model"]; ok && rawModel != nil {
			m, ok := rawModel.(string)
			if !ok {
				return RawBodyHints{}, fmt.Errorf("field 'model' must be a string")
			}
			hints.Model = strings.TrimSpace(m)
		}
		if rawStream, ok := obj["stream"]; ok && rawStream != nil {
			b, ok := rawStream.(bool)
			if !ok {
				return RawBodyHints{}, fmt.Errorf("field 'stream' must be a boolean")
			}
			if b {
				hints.Stream = true
			}
		}
		for _, tokKey := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
			if v, ok := obj[tokKey]; ok && v != nil {
				if n, ok := jsonNumberInt64(v); ok && n >= 0 {
					hints.MaxTokens = n
					break
				}
			}
		}
	}

	if hints.Model == "" {
		hints.Model = ExtractModelFromPath(upstreamPath)
	}
	return hints, nil
}

// ExtractModelFromPath parses vendor paths containing models/{model} (e.g., Gemini native URLs).
func ExtractModelFromPath(upstreamPath string) string {
	norm := "/" + strings.TrimPrefix(strings.TrimSpace(upstreamPath), "/")
	idx := strings.Index(norm, "/models/")
	if idx < 0 {
		return ""
	}
	rest := norm[idx+len("/models/"):]
	if cut := strings.IndexAny(rest, ":/?"); cut >= 0 {
		rest = rest[:cut]
	}
	return strings.TrimSpace(rest)
}

// BuildBoundedInspectPayload prepares a bounded JSON payload for rt.InspectNativeRequest
// without touching rawBody. Large payloads (> 256 KiB) are summarized down to routing hints.
func BuildBoundedInspectPayload(rawBody []byte, rawHints RawBodyHints) []byte {
	trimmed := bytes.TrimSpace(rawBody)
	if len(trimmed) == 0 {
		return nil
	}
	if len(rawBody) <= MaxInspectMetadataBytes {
		return rawBody
	}
	meta := make(map[string]any, 3)
	if rawHints.Model != "" {
		meta["model"] = rawHints.Model
	}
	if rawHints.Stream {
		meta["stream"] = true
	}
	if rawHints.MaxTokens > 0 {
		meta["max_tokens"] = rawHints.MaxTokens
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return nil
	}
	return b
}

// InspectNativeRequest invokes the adapter runtime's InspectNativeRequest hook non-destructively
// and merges path/query streaming and model hints.
func InspectNativeRequest(
	ctx context.Context,
	rt port.ProviderRuntime,
	reqID string,
	dialect string,
	upstreamPath string,
	sanitizedHeaders map[string]string,
	rawBody []byte,
	rawHints RawBodyHints,
) (InspectResult, error) {
	boundedBody := BuildBoundedInspectPayload(rawBody, rawHints)
	hints, err := rt.InspectNativeRequest(ctx, port.NativeRequestInput{
		RequestID:        reqID,
		Dialect:          dialect,
		UpstreamPath:     "/" + strings.TrimPrefix(upstreamPath, "/"),
		SanitizedHeaders: sanitizedHeaders,
		RawRequestBody:   boundedBody,
	})
	if err != nil {
		return InspectResult{}, err
	}

	targetModel := strings.TrimSpace(hints.TargetModel)
	if targetModel == "" {
		targetModel = rawHints.Model
	}
	isStream := hints.Stream || rawHints.Stream

	return InspectResult{
		Hints:       hints,
		TargetModel: targetModel,
		Stream:      isStream,
	}, nil
}

// ExtractSyncUsage runs non-destructive usage extraction against an untouched synchronous response body.
func ExtractSyncUsage(
	ctx context.Context,
	rt port.ProviderRuntime,
	reqID string,
	rawReqBody []byte,
	rawRespBody []byte,
) port.NormalizedUsage {
	if rt == nil {
		return port.NormalizedUsage{Accuracy: port.UsageAccuracyUnmeteredFlagged}
	}
	usage, err := rt.ExtractUsage(ctx, port.ExtractUsageInput{
		RequestID:                 reqID,
		Modality:                  "NATIVE",
		RawResponseOrTrailerBytes: rawRespBody,
		FallbackUTF8InputBytes:    int64(len(rawReqBody)),
		FallbackUTF8OutputBytes:   int64(len(rawRespBody)),
	})
	if err != nil || !usage.Accuracy.IsValid() {
		if len(rawReqBody) > 0 || len(rawRespBody) > 0 {
			return port.NormalizedUsage{
				InputTokens:  (int64(len(rawReqBody)) + 3) / 4,
				OutputTokens: (int64(len(rawRespBody)) + 3) / 4,
				Accuracy:     port.UsageAccuracyEstimated,
			}
		}
		return port.NormalizedUsage{Accuracy: port.UsageAccuracyUnmeteredFlagged}
	}
	return usage
}

// SSEUsageAccumulator incrementally collects exact usage across multi-event native SSE streams
// (such as Anthropic message_start + message_delta or Gemini/OpenAI trailing usage chunks)
// without modifying the raw SSE byte stream returned to the caller.
type SSEUsageAccumulator struct {
	rt          port.ProviderRuntime
	reqID       string
	inputBytes  int64
	outputBytes int64
	lastData    []byte
	exactSeen   bool
	best        port.NormalizedUsage
}

// NewSSEUsageAccumulator creates an accumulator bound to the given runtime and input byte length.
func NewSSEUsageAccumulator(rt port.ProviderRuntime, reqID string, rawReqBody []byte) *SSEUsageAccumulator {
	return &SSEUsageAccumulator{
		rt:         rt,
		reqID:      reqID,
		inputBytes: int64(len(rawReqBody)),
		best:       port.NormalizedUsage{Accuracy: port.UsageAccuracyUnmeteredFlagged},
	}
}

// Feed inspects complete newline-delimited SSE bytes non-destructively.
func (a *SSEUsageAccumulator) Feed(ctx context.Context, complete []byte) {
	if a == nil || len(complete) == 0 {
		return
	}
	pos := 0
	for pos < len(complete) {
		nl := bytes.IndexByte(complete[pos:], '\n')
		var line []byte
		if nl < 0 {
			line = complete[pos:]
			pos = len(complete)
		} else {
			line = complete[pos : pos+nl]
			pos += nl + 1
		}
		trimmed := bytes.TrimSpace(line)
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		a.outputBytes += int64(len(payload))
		if a.rt == nil {
			continue
		}
		if payload[0] == '{' {
			a.lastData = append(a.lastData[:0], trimmed...)
			if hasUsageCandidateKey(payload) {
				u, err := a.rt.ExtractUsage(ctx, port.ExtractUsageInput{
					RequestID:                 a.reqID,
					Modality:                  "NATIVE",
					RawResponseOrTrailerBytes: trimmed,
					FallbackUTF8InputBytes:    a.inputBytes,
					FallbackUTF8OutputBytes:   a.outputBytes,
				})
				if err == nil && u.Accuracy == port.UsageAccuracyExact {
					a.mergeExact(u)
				}
			}
		}
	}
}

func (a *SSEUsageAccumulator) mergeExact(u port.NormalizedUsage) {
	a.exactSeen = true
	a.best.Accuracy = port.UsageAccuracyExact
	if u.InputTokens > 0 {
		a.best.InputTokens = u.InputTokens
	}
	if u.OutputTokens > 0 {
		a.best.OutputTokens = u.OutputTokens
	}
	if u.CacheReadTokens > 0 {
		a.best.CacheReadTokens = u.CacheReadTokens
	}
	if u.CacheWriteTokens > 0 {
		a.best.CacheWriteTokens = u.CacheWriteTokens
	}
	if u.ReasoningTokens > 0 {
		a.best.ReasoningTokens = u.ReasoningTokens
	}
	if u.MediaUsage != nil {
		m := *u.MediaUsage
		a.best.MediaUsage = &m
	}
}

// Finalize returns accumulated exact usage or falls back to byte estimation / unmetered.
func (a *SSEUsageAccumulator) Finalize(ctx context.Context) port.NormalizedUsage {
	if a == nil {
		return port.NormalizedUsage{Accuracy: port.UsageAccuracyUnmeteredFlagged}
	}
	if a.exactSeen {
		return a.best
	}
	if a.rt != nil {
		u, err := a.rt.ExtractUsage(ctx, port.ExtractUsageInput{
			RequestID:                 a.reqID,
			Modality:                  "NATIVE",
			RawResponseOrTrailerBytes: a.lastData,
			FallbackUTF8InputBytes:    a.inputBytes,
			FallbackUTF8OutputBytes:   a.outputBytes,
		})
		if err == nil && u.Accuracy.IsValid() {
			return u
		}
	}
	if a.inputBytes > 0 || a.outputBytes > 0 {
		return port.NormalizedUsage{
			InputTokens:  (a.inputBytes + 3) / 4,
			OutputTokens: (a.outputBytes + 3) / 4,
			Accuracy:     port.UsageAccuracyEstimated,
		}
	}
	return port.NormalizedUsage{Accuracy: port.UsageAccuracyUnmeteredFlagged}
}

func hasUsageCandidateKey(payload []byte) bool {
	return bytes.Contains(payload, []byte(`"usage"`)) ||
		bytes.Contains(payload, []byte(`"usageMetadata"`)) ||
		bytes.Contains(payload, []byte(`"usage_metadata"`)) ||
		bytes.Contains(payload, []byte(`"prompt_eval_count"`)) ||
		bytes.Contains(payload, []byte(`"eval_count"`))
}

func jsonNumberInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, true
		}
		fv, err := strconv.ParseFloat(x.String(), 64)
		if err != nil || math.IsNaN(fv) || math.IsInf(fv, 0) {
			return 0, false
		}
		if fv >= float64(1<<62) {
			return 1 << 62, true
		}
		if fv <= -float64(1<<62) {
			return -(1 << 62), true
		}
		return int64(fv), true
	case float64:
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	default:
		return 0, false
	}
}
