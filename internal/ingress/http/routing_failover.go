// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/failover"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
)

func extractChatCapabilities(req *chatCompletionIngressRequest) []string {
	var caps []string
	if req == nil {
		return caps
	}
	if req.Stream {
		caps = append(caps, "streaming")
	}
	if len(req.Tools) > 0 {
		caps = append(caps, "tool_calling")
	}
	caps = appendResponseFormatCaps(caps, req.ResponseFormat)
	if strings.TrimSpace(req.ReasoningEffort) != "" {
		caps = append(caps, "reasoning")
	}
	hasVision, hasAudio, hasDoc := detectMultimodalParts(req.Messages)
	caps = appendMultimodalCaps(caps, hasVision, hasAudio, hasDoc)
	return caps
}

func extractResponsesCapabilities(req *responsesIngressRequest, inputItems []any) []string {
	var caps []string
	if req == nil {
		return caps
	}
	if req.Stream {
		caps = append(caps, "streaming")
	}
	if len(req.Tools) > 0 {
		caps = append(caps, "tool_calling")
	}
	if req.Text != nil && req.Text.Format != nil {
		caps = appendResponseFormatCaps(caps, req.Text.Format)
	}
	if req.Reasoning != nil && strings.TrimSpace(req.Reasoning.Effort) != "" {
		caps = append(caps, "reasoning")
	}
	if len(inputItems) > 0 {
		hasVision, hasAudio, hasDoc := detectMultimodalParts(inputItems)
		caps = appendMultimodalCaps(caps, hasVision, hasAudio, hasDoc)
	}
	return caps
}

func extractEmbeddingCapabilities(req *embeddingIngressRequest) []string {
	caps := []string{"embeddings"}
	if req != nil && req.Dimensions != nil {
		caps = append(caps, "custom_dimensions")
	}
	return caps
}

func appendResponseFormatCaps(caps []string, rf *responseFormat) []string {
	if rf == nil {
		return caps
	}
	switch strings.ToLower(strings.TrimSpace(rf.Type)) {
	case "json_schema":
		isStrict := (rf.Strict != nil && *rf.Strict) || (rf.JSONSchema != nil && rf.JSONSchema.Strict)
		if isStrict {
			caps = append(caps, "strict_json_schema")
		} else {
			caps = append(caps, "json_schema_loose")
		}
	case "json_object":
		caps = append(caps, "json_object")
	}
	return caps
}

func appendMultimodalCaps(caps []string, hasVision, hasAudio, hasDoc bool) []string {
	if hasVision {
		caps = append(caps, "vision")
	}
	if hasAudio {
		caps = append(caps, "audio_input")
	}
	if hasDoc {
		caps = append(caps, "document_input")
	}
	return caps
}

func (s *Server) executeRoutedPreStreamFailover(
	w http.ResponseWriter,
	clientCtx context.Context,
	streamCtx context.Context,
	reqID string,
	modelSlug string,
	candidates []*resolver.RankedTarget,
	attemptFn failover.AttemptFunc,
) failover.ExecutionResult {
	execRes := failover.Execute(streamCtx, failover.Config{
		Breaker: s.breaker,
		Now:     func() time.Time { return time.UnixMilli(s.nowMs()).UTC() },
	}, candidates, attemptFn)
	if execRes.ClientCanceled && !execRes.StreamStarted && clientCtx != nil && clientCtx.Err() == nil && streamCtx != nil && streamCtx.Err() != nil {
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
		return execRes
	}
	if execRes.AttemptCount == 0 {
		if execRes.DeadlineExceeded {
			writeCanonicalError(w, reqID, http.StatusGatewayTimeout, "upstream_timeout", "upstream_error", "Upstream failover deadline exceeded.", true)
			return execRes
		}
		if execRes.ExhaustedBreaker {
			writeCanonicalError(
				w,
				reqID,
				http.StatusServiceUnavailable,
				"no_compliant_deployment_available",
				"residency_unavailable_error",
				fmt.Sprintf("No healthy compliant deployment is available for model %q.", modelSlug),
				true,
			)
		}
	}
	return execRes
}

func (s *Server) makeNormalizedUpstreamAttemptError(
	ctx context.Context,
	w http.ResponseWriter,
	reqID string,
	rt port.ProviderRuntime,
	upstreamStatus int,
	rawHdrs http.Header,
	body []byte,
) failover.AttemptResult {
	flatHdrs := flattenHTTPHeaders(rawHdrs)
	httpStatus := mapUpstreamHTTPStatus(upstreamStatus)

	code := "upstream_error"
	errType := "upstream_error"
	msg := fmt.Sprintf("Upstream provider returned HTTP %d.", upstreamStatus)
	retryableFlag := upstreamStatus == http.StatusTooManyRequests || upstreamStatus == 529 || upstreamStatus >= 500

	if rt != nil {
		if normErr, nerr := rt.NormalizeError(ctx, port.NormalizeErrorInput{
			RequestID:        reqID,
			HTTPStatus:       int32(upstreamStatus),
			SanitizedHeaders: flatHdrs,
			SanitizedBody:    body,
		}); nerr == nil {
			code = sanitizeErrorToken(normErr.CanonicalCode, "upstream_error")
			errType = sanitizeErrorToken(normErr.ErrorType, "upstream_error")
			if sMsg := sanitizePublicErrorMessage(normErr.SanitizedPublicMessage, 512); sMsg != "" {
				msg = sMsg
			}
			retryableFlag = normErr.RetryablePreStream
		}
	}

	var outcome breaker.OutcomeKind
	var retryFailover bool
	var retryAfterRaw string
	switch {
	case upstreamStatus == http.StatusTooManyRequests || upstreamStatus == 529:
		outcome = breaker.OutcomeThrottle429
		retryFailover = true
		if rawHdrs != nil {
			retryAfterRaw = strings.TrimSpace(rawHdrs.Get("Retry-After"))
		}
	case upstreamStatus >= 500 || upstreamStatus == http.StatusRequestTimeout:
		outcome = breaker.OutcomeSystemFault
		retryFailover = true
	default:
		outcome = breaker.OutcomeCallerError
		retryFailover = false
	}

	return failover.AttemptResult{
		Succeeded:     false,
		Retryable:     retryFailover,
		Outcome:       outcome,
		HTTPStatus:    upstreamStatus,
		RetryAfterRaw: retryAfterRaw,
		FlushFinal: func() {
			writeCanonicalError(w, reqID, httpStatus, code, errType, msg, retryableFlag)
		},
	}
}

func (s *Server) classifyBrokerAttemptError(
	w http.ResponseWriter,
	clientCtx context.Context,
	streamCtx context.Context,
	reqID string,
	err error,
	beforeStream bool,
) failover.AttemptResult {
	if errors.Is(err, context.Canceled) {
		if clientCtx != nil && clientCtx.Err() == nil && streamCtx != nil && streamCtx.Err() != nil {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusUnauthorized,
				FlushFinal: func() {
					msg := "Virtual Key was revoked or suspended before response dispatch."
					if !beforeStream {
						msg = "Virtual Key was revoked or suspended before stream dispatch."
					}
					writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", msg, false)
				},
			}
		}
		return failover.AttemptResult{
			Succeeded: false,
			Retryable: false,
			Outcome:   breaker.OutcomeClientCanceled,
		}
	}

	var bErr *broker.Error
	if errors.As(err, &bErr) {
		if bErr.HTTPStatus >= 400 && bErr.HTTPStatus < 500 {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: bErr.HTTPStatus,
				FlushFinal: func() {
					s.writeBrokerOrEgressError(w, reqID, err)
				},
			}
		}
		st := bErr.HTTPStatus
		if st <= 0 {
			st = http.StatusBadGateway
		}
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  true,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: st,
			FlushFinal: func() {
				s.writeBrokerOrEgressError(w, reqID, err)
			},
		}
	}

	var eErr *egress.Error
	if errors.As(err, &eErr) {
		st := eErr.HTTPStatus
		if st <= 0 {
			st = http.StatusForbidden
		}
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  false,
			Outcome:    breaker.OutcomeCallerError,
			HTTPStatus: st,
			FlushFinal: func() {
				s.writeBrokerOrEgressError(w, reqID, err)
			},
		}
	}

	st := http.StatusBadGateway
	if errors.Is(err, context.DeadlineExceeded) {
		st = http.StatusGatewayTimeout
	}
	return failover.AttemptResult{
		Succeeded:  false,
		Retryable:  true,
		Outcome:    breaker.OutcomeSystemFault,
		HTTPStatus: st,
		FlushFinal: func() {
			s.writeBrokerOrEgressError(w, reqID, err)
		},
	}
}

func (s *Server) classifyPreparedAttemptError(
	w http.ResponseWriter,
	clientCtx context.Context,
	streamCtx context.Context,
	reqID string,
	err error,
) failover.AttemptResult {
	if errors.Is(err, context.Canceled) {
		if clientCtx != nil && clientCtx.Err() == nil && streamCtx != nil && streamCtx.Err() != nil {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusUnauthorized,
				FlushFinal: func() {
					writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
				},
			}
		}
		return failover.AttemptResult{
			Succeeded: false,
			Retryable: false,
			Outcome:   breaker.OutcomeClientCanceled,
		}
	}
	var pErr *preparedRequestErr
	if errors.As(err, &pErr) {
		if pErr.cause != nil {
			var bErr *broker.Error
			var eErr *egress.Error
			if errors.As(pErr.cause, &bErr) || errors.As(pErr.cause, &eErr) {
				status := http.StatusBadGateway
				if bErr != nil {
					if bErr.HTTPStatus > 0 {
						status = bErr.HTTPStatus
					}
				} else if eErr != nil {
					if eErr.HTTPStatus > 0 {
						status = eErr.HTTPStatus
					} else {
						status = http.StatusForbidden
					}
				}
				return failover.AttemptResult{
					Succeeded:  false,
					Retryable:  false,
					Outcome:    breaker.OutcomeCallerError,
					HTTPStatus: status,
					FlushFinal: func() {
						s.writePreparedRequestError(w, reqID, err)
					},
				}
			}
		} else if pErr.httpStatus >= 400 && pErr.httpStatus < 500 {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: pErr.httpStatus,
				FlushFinal: func() {
					s.writePreparedRequestError(w, reqID, err)
				},
			}
		}
	}
	return failover.AttemptResult{
		Succeeded:  false,
		Retryable:  true,
		Outcome:    breaker.OutcomeSystemFault,
		HTTPStatus: http.StatusBadGateway,
		FlushFinal: func() {
			s.writePreparedRequestError(w, reqID, err)
		},
	}
}

func classifyMidStreamPumpOutcome(clientCtx, streamCtx context.Context, pumpRes sse.PumpResult, pumpErr error) breaker.OutcomeKind {
	if pumpRes.ClientDisconnected || errors.Is(pumpErr, context.Canceled) {
		if clientCtx != nil && clientCtx.Err() == nil && streamCtx != nil && streamCtx.Err() != nil {
			return breaker.OutcomeCallerError
		}
		return breaker.OutcomeClientCanceled
	}
	return breaker.OutcomeSystemFault
}

func (s *Server) classifyTransformResponseError(
	w http.ResponseWriter,
	clientCtx context.Context,
	ctx context.Context,
	reqID string,
	err error,
	malformedMsg string,
) failover.AttemptResult {
	if errors.Is(err, context.Canceled) {
		if clientCtx != nil && clientCtx.Err() == nil && ctx != nil && ctx.Err() != nil {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusUnauthorized,
				FlushFinal: func() {
					writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before response dispatch.", false)
				},
			}
		}
		return failover.AttemptResult{
			Succeeded: false,
			Retryable: false,
			Outcome:   breaker.OutcomeClientCanceled,
		}
	}
	if errors.Is(err, declarative.ErrUpstreamMalformedResponse) {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  true,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: http.StatusBadGateway,
			FlushFinal: func() {
				writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", malformedMsg, false)
			},
		}
	}
	return failover.AttemptResult{
		Succeeded:  false,
		Retryable:  true,
		Outcome:    breaker.OutcomeSystemFault,
		HTTPStatus: http.StatusBadGateway,
		FlushFinal: func() {
			s.writeRuntimeFaultError(w, reqID, err)
		},
	}
}

func (s *Server) classifyPreStreamPumpAttemptError(
	w http.ResponseWriter,
	clientCtx context.Context,
	streamCtx context.Context,
	reqID string,
	pumpErr error,
) failover.AttemptResult {
	if errors.Is(pumpErr, sse.ErrLineTooLarge) {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  false,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: http.StatusBadGateway,
			FlushFinal: func() {
				writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream SSE stream exceeded maximum line byte limit.", false)
			},
		}
	}
	if errors.Is(pumpErr, context.DeadlineExceeded) || errors.Is(pumpErr, sse.ErrIdleChunkTimeout) {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  true,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: http.StatusGatewayTimeout,
			FlushFinal: func() {
				writeCanonicalError(w, reqID, http.StatusGatewayTimeout, "upstream_timeout", "upstream_error", "Upstream SSE stream timed out before emitting first frame.", true)
			},
		}
	}
	if errors.Is(pumpErr, context.Canceled) {
		if clientCtx != nil && clientCtx.Err() == nil && streamCtx != nil && streamCtx.Err() != nil {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusUnauthorized,
				FlushFinal: func() {
					writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before stream dispatch.", false)
				},
			}
		}
		return failover.AttemptResult{
			Succeeded: false,
			Retryable: false,
			Outcome:   breaker.OutcomeClientCanceled,
		}
	}
	if errors.Is(pumpErr, port.ErrExecutionFault) {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  true,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: http.StatusBadGateway,
			FlushFinal: func() {
				s.writeRuntimeFaultError(w, reqID, pumpErr)
			},
		}
	}
	return failover.AttemptResult{
		Succeeded:  false,
		Retryable:  true,
		Outcome:    breaker.OutcomeSystemFault,
		HTTPStatus: http.StatusBadGateway,
		FlushFinal: func() {
			writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream stream terminated before emitting a valid SSE frame.", false)
		},
	}
}

func classifyRuntimeLoadAttemptError(w http.ResponseWriter, s *Server, reqID string, rtErr error) failover.AttemptResult {
	var fault *wasm.ExecutionFaultError
	if errors.As(rtErr, &fault) && fault.Kind == wasm.FaultKindQuarantined {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  true,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: http.StatusServiceUnavailable,
			FlushFinal: func() {
				s.writeRuntimeFaultError(w, reqID, rtErr)
			},
		}
	}
	return failover.AttemptResult{
		Succeeded:  false,
		Retryable:  true,
		Outcome:    breaker.OutcomeSystemFault,
		HTTPStatus: http.StatusBadGateway,
		FlushFinal: func() {
			s.writeRuntimeFaultError(w, reqID, rtErr)
		},
	}
}
