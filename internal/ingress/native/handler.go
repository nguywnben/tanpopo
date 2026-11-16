// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package native

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/ratelimit"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/failover"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/telemetry/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultMaxRequestBodyBytes int64 = 32 << 20
	defaultFirstByteTimeout          = 30 * time.Second
	maxTraceHeaderLength             = 40
	defaultChunkBytes                = sse.DefaultChunkBytes
)

var (
	// ErrInvalidConfig indicates missing required dependencies for native passthrough Handler.
	ErrInvalidConfig = errors.New("native: invalid handler configuration")

	dialectTokenRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

	nativeReadBufPool = sync.Pool{
		New: func() any {
			b := make([]byte, defaultChunkBytes)
			return &b
		},
	}
)

// RuntimeAcquirer loads an active provider adapter runtime and returns a release callback.
type RuntimeAcquirer func(ctx context.Context, target *snapshot.ResolvedTarget) (port.ProviderRuntime, func(), error)

// Config configures the lossless /native/{dialect}/{upstreamPath} ingress handler.
type Config struct {
	Snapshots               *snapshot.Manager
	Broker                  *broker.BlindHostCredentialBroker
	Audit                   storageport.AuditSink
	Idempotency             *idempotency.Guard
	PolicyEvaluator         *policyengine.Evaluator
	RateLimiter             ratelimit.RateLimiterPort
	Breaker                 *breaker.CircuitBreaker
	RouteResolver           *resolver.Resolver
	AcquireRuntime          RuntimeAcquirer
	BudgetLedger            *ledger.Service
	DefaultHoldOutputTokens int64
	NowMs                   func() int64
	MaxRequestBodyBytes     int64
	FirstByteTimeout        time.Duration
	HeartbeatInterval       time.Duration
	OnUsage                 func(UsageRecord)
	Telemetry               *otel.Pipeline
}

// Handler serves /native/{dialect}/{upstreamPath} passthrough requests.
type Handler struct {
	snapshots               *snapshot.Manager
	broker                  *broker.BlindHostCredentialBroker
	audit                   storageport.AuditSink
	idempotency             *idempotency.Guard
	policyEval              *policyengine.Evaluator
	rateLimiter             ratelimit.RateLimiterPort
	breaker                 *breaker.CircuitBreaker
	routeResolver           *resolver.Resolver
	acquireRuntime          RuntimeAcquirer
	budgetLedger            *ledger.Service
	defaultHoldOutputTokens int64
	nowMs                   func() int64
	maxReqBodyBytes         int64
	firstByteTimeout        time.Duration
	heartbeatInterval       time.Duration
	onUsage                 func(UsageRecord)
	telemetry               *otel.Pipeline
	regexMu                 sync.RWMutex
	regexCache              map[string]*regexp.Regexp
}

// NewHandler constructs a fail-closed native passthrough Handler.
func NewHandler(cfg Config) (*Handler, error) {
	if cfg.Snapshots == nil || cfg.Broker == nil || cfg.AcquireRuntime == nil {
		return nil, fmt.Errorf("%w: snapshots, broker, and acquireRuntime are required", ErrInvalidConfig)
	}
	maxBody := cfg.MaxRequestBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxRequestBodyBytes
	}
	firstByte := cfg.FirstByteTimeout
	if firstByte <= 0 {
		firstByte = defaultFirstByteTimeout
	}
	hb := cfg.HeartbeatInterval
	if hb == 0 {
		hb = sse.DefaultHeartbeatInterval
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	policyEval := cfg.PolicyEvaluator
	if policyEval == nil {
		policyEval = policyengine.NewEvaluator()
	}
	rateLimiter := cfg.RateLimiter
	if rateLimiter == nil {
		rateLimiter = ratelimit.NewMemoryLimiter(ratelimit.Config{
			Now: func() time.Time { return time.UnixMilli(nowFn()).UTC() },
		})
	}
	cb := cfg.Breaker
	if cb == nil {
		cb = breaker.New(breaker.Config{
			Now: func() time.Time { return time.UnixMilli(nowFn()).UTC() },
		})
	}
	routeRes := cfg.RouteResolver
	if routeRes == nil {
		routeRes = resolver.New()
	}
	return &Handler{
		snapshots:               cfg.Snapshots,
		broker:                  cfg.Broker,
		audit:                   cfg.Audit,
		idempotency:             cfg.Idempotency,
		policyEval:              policyEval,
		rateLimiter:             rateLimiter,
		breaker:                 cb,
		routeResolver:           routeRes,
		acquireRuntime:          cfg.AcquireRuntime,
		budgetLedger:            cfg.BudgetLedger,
		defaultHoldOutputTokens: cfg.DefaultHoldOutputTokens,
		nowMs:                   nowFn,
		maxReqBodyBytes:         maxBody,
		firstByteTimeout:        firstByte,
		heartbeatInterval:       hb,
		onUsage:                 cfg.OnUsage,
		telemetry:               cfg.Telemetry,
		regexCache:              make(map[string]*regexp.Regexp),
	}, nil
}

// ServeHTTP handles /native/{dialect}/{upstreamPath} with strict canonicalization,
// Virtual Key authentication + stripping, Same-Dialect Guard (400 dialect_mismatch),
// Default-Deny path allowlisting (403 native_path_not_allowed), and byte-preserving
// request/response/SSE relay through BlindHostCredentialBroker.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqID := sanitizeOrMintRequestID(firstNonEmpty(w.Header().Get("X-Tanpopo-Request-Id"), r.Header.Get("X-Tanpopo-Request-Id")))
	tp := sanitizeOrMintTraceparent(firstNonEmpty(w.Header().Get("Traceparent"), r.Header.Get("Traceparent")))
	w.Header().Set("X-Tanpopo-Request-Id", reqID)
	w.Header().Set("Traceparent", tp)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	var reqTel *nativeRequestTelemetry
	if h.telemetry != nil {
		spanCtx, rootSpan, traceID := h.telemetry.StartRequestSpan(r.Context(), tp, reqID, "native_http")
		reqTel = &nativeRequestTelemetry{
			pipeline:    h.telemetry,
			reqID:       reqID,
			traceID:     traceID,
			entryPoint:  "native_http",
			startedAt:   time.Now(),
			startedAtMs: h.nowMs(),
			rootSpan:    rootSpan,
		}
		scw := &nativeStatusCaptureWriter{ResponseWriter: w}
		w = scw
		r = r.WithContext(withNativeTelemetry(spanCtx, reqTel))
		defer reqTel.finish(r.Context(), scw, r)
	}

	if r.Header.Get("Upgrade") != "" || headerContainsToken(r.Header.Values("Connection"), "upgrade") {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "unsupported_protocol_upgrade", "invalid_request_error", "HTTP protocol upgrades are not permitted on /native/{dialect}/{upstreamPath}.", false)
		return
	}

	// Step 1: Canonicalize and validate raw request path BEFORE path.Clean or any socket work (NEG-2).
	dialect, upstreamPath, cleanQuery, altParam, ok := validateAndParseNativePath(w, r, reqID)
	if !ok {
		return
	}

	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeCanonicalError(w, reqID, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "Only POST or GET is allowed on /native/{dialect}/{upstreamPath}.", false)
		return
	}

	// Step 2: Virtual Key extraction across all native credential carriers & DEC-019 auth.
	rawKey, streamCtx, cancelStream, match, handleID, ok := h.authenticateNativeRequest(w, r, reqID, dialect, upstreamPath)
	if !ok {
		return
	}
	defer cancelStream()
	defer h.snapshots.Streams().Unregister(match.Key.ID, handleID)
	reqTel.recordAuth(match)

	// Step 3: Read bounded body without altering any bytes destined for upstream.
	rawBody, ok := h.readBoundedBody(w, r, reqID)
	if !ok {
		return
	}
	defer clear(rawBody)
	reqTel.captureDebugPayloadIfAuthorized(rawBody)

	rawHints, err := ExtractRawHints(rawBody, upstreamPath, altParam)
	if err != nil {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Malformed JSON request body.", false)
		return
	}

	// Fast fail-closed snapshot & policy integrity check before path/model dispatch.
	snap := h.snapshots.Current()
	if snap == nil {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "snapshot_not_ready", "api_error", "Configuration snapshot unavailable.", true)
		return
	}
	eff, pErr := h.policyEval.ResolveEffective(snap, match.Config, match.Key)
	if pErr != nil {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, policyengine.DenialCodeEngineUnavailable, "api_error", "Policy engine snapshot is corrupted or unavailable; failing closed.", true)
		return
	}

	if rawHints.Model == "" {
		writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Native passthrough path rejected or missing target model.", false)
		return
	}
	reqTel.recordModel(rawHints.Model)
	estTokens, estBaseIn, estMaxOut, completionCount := estimateNativeTokensAndCountFromJSON(rawBody)
	reqRegion, regionOK := resolveNativeRequestedDataRegion(r)
	if !regionOK {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Conflicting X-Tanpopo-Data-Region header values.", false)
		return
	}
	_, endPolicyStage := reqTel.startStage(r.Context(), "Policy", attribute.String("logical_model", rawHints.Model))
	policyAllowed := h.evaluateNativePreAdmissionPolicy(w, reqID, match, eff, rawHints.Model, reqRegion, rawBody, estTokens)
	if !policyAllowed {
		endPolicyStage("denied", "", nil)
		return
	}
	endPolicyStage("ok", "", nil)

	// Step 3b: When Idempotency-Key is supplied on synchronous POST, enforce RPM BEFORE idempotency SQLite lookup
	// and reserve TPM ONLY after a fresh IN_FLIGHT claim (so cached replays still count against RPM without burning TPM).
	var idemClaim *idempotency.Claim
	var admLease *nativeAdmissionLease
	if !rawHints.Stream && r.Method == http.MethodPost && len(r.Header.Values("Idempotency-Key")) > 0 {
		if !h.acquireNativePreIdempotencyRPM(w, r, reqID, match, eff) {
			return
		}
		idemRoutePath := "/native/" + dialect + "/" + upstreamPath
		if cleanQuery != "" {
			idemRoutePath += "?" + cleanQuery
		}
		claim, replayedOrRejected := h.beginIdempotency(w, r, reqID, match.Key, idemRoutePath, rawBody)
		if replayedOrRejected {
			return
		}
		idemClaim = claim
		if idemClaim != nil {
			defer idemClaim.Abort(context.Background())
		}
		lease, ok := h.acquireNativePostIdempotencyTPM(w, r, reqID, match, eff, estTokens)
		if !ok {
			return
		}
		admLease = lease
	} else {
		lease, ok := h.acquireNativeRateLimits(w, r, reqID, match, eff, estTokens, rawHints.Stream)
		if !ok {
			return
		}
		admLease = lease
	}
	defer admLease.Release()

	// Step 4: Resolve policy-compliant target deployments via 6-step route resolver (CT-08, CT-13, SM-04).
	candidates, ok := h.resolveNativeTargetsWithPolicy(w, reqID, snap, match, eff, reqRegion, dialect, upstreamPath, rawHints.Model, nil, estBaseIn, estMaxOut)
	if !ok {
		return
	}

	// Step 5: Acquire runtime, execute bounded InspectNativeRequest without mutating rawBody.
	// If multiple failover candidates exist and the initial candidate's runtime is quarantined or
	// fails inspection (non-canceled), fall through into failover.Execute so SM-04 records the
	// fault and tries compliant backups cleanly.
	initialTarget := candidates[0].Target
	var (
		initialRT      port.ProviderRuntime
		releaseInitial func()
		insp           InspectResult
		hasInitialInsp bool
	)
	rtLoaded, releaseLoaded, rtErr := h.acquireRuntime(streamCtx, initialTarget)
	if rtErr != nil {
		if errors.Is(rtErr, context.Canceled) {
			if r.Context().Err() == nil && streamCtx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
			}
			return
		}
		if len(candidates) <= 1 {
			h.writeRuntimeFaultError(w, reqID, rtErr)
			return
		}
	} else {
		initialRT = rtLoaded
		releaseInitial = releaseLoaded
		defer func() {
			if releaseInitial != nil {
				releaseInitial()
			}
		}()

		safeHeaders := buildSanitizedNativeHeaders(r.Header, initialTarget.Adapter.AllowedNativeHeaders, rawKey, reqID, tp)
		narrowPolicy := buildHostNarrowPolicyFromTarget(initialTarget)
		wasmCtx := wasm.WithEgressScope(streamCtx, wasm.EgressScope{
			OrgID:        match.Key.OrgID,
			ConnectionID: initialTarget.Connection.ID,
			ActorKind:    "virtual_key",
			ActorID:      match.Key.ID,
			EntryPoint:   wasm.DefaultAuditEntryPoint,
			RequestID:    reqID,
			NarrowPolicy: &narrowPolicy,
		})

		adpID, _, extTier := nativeTargetTelemetryLabels(initialTarget)
		inspCtx, endInspHook := reqTel.startStage(wasmCtx, "ExtensionHook",
			attribute.String("hook", "inspect_native_request"),
			attribute.String("adapter_id", adpID),
			attribute.String("extension_tier", extTier),
		)
		inspRes, err := InspectNativeRequest(inspCtx, initialRT, reqID, dialect, upstreamPath, safeHeaders, rawBody, rawHints)
		inspStatus := "ok"
		if err != nil {
			inspStatus = "error"
		}
		endInspHook(inspStatus, "inspect_native_request", err)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				if r.Context().Err() == nil && streamCtx.Err() != nil {
					writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
				}
				return
			}
			if len(candidates) <= 1 {
				h.writeRuntimeFaultError(w, reqID, err)
				return
			}
			if releaseInitial != nil {
				releaseInitial()
				releaseInitial = nil
			}
			initialRT = nil
		} else {
			insp = inspRes
			hasInitialInsp = true
			if insp.TargetModel != "" && !modelAllowlistPermits(match.Key.AllowedModels, insp.TargetModel) {
				writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key model allowlist denies %q.", insp.TargetModel), false)
				return
			}
		}
	}

	effectiveModel := rawHints.Model
	if hasInitialInsp {
		effectiveModel = firstNonEmpty(insp.TargetModel, rawHints.Model)
		reqCaps := compactNonEmptyStrings(insp.Hints.RequiredCapabilities)
		if insp.TargetModel != "" && insp.TargetModel != rawHints.Model {
			if !h.evaluateNativePreAdmissionPolicy(w, reqID, match, eff, insp.TargetModel, reqRegion, rawBody, estTokens) {
				return
			}
		}
		if (insp.TargetModel != "" && insp.TargetModel != rawHints.Model) || len(reqCaps) > 0 {
			newCandidates, ok := h.resolveNativeTargetsWithPolicy(w, reqID, snap, match, eff, reqRegion, dialect, upstreamPath, effectiveModel, reqCaps, estBaseIn, estMaxOut)
			if !ok {
				return
			}
			candidates = newCandidates
			newTarget := candidates[0].Target
			if !sameResolvedTarget(newTarget, initialTarget) {
				adapterChanged := newTarget.Adapter.ID != initialTarget.Adapter.ID
				if releaseInitial != nil {
					releaseInitial()
					releaseInitial = nil
				}
				initialRT = nil
				initialTarget = newTarget

				var newErr error
				initialRT, releaseInitial, newErr = h.acquireRuntime(streamCtx, initialTarget)
				if newErr != nil {
					if errors.Is(newErr, context.Canceled) {
						if r.Context().Err() == nil && streamCtx.Err() != nil {
							writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
						}
						return
					}
					if len(candidates) <= 1 {
						h.writeRuntimeFaultError(w, reqID, newErr)
						return
					}
					hasInitialInsp = false
				} else if adapterChanged {
					newSafeHeaders := buildSanitizedNativeHeaders(r.Header, initialTarget.Adapter.AllowedNativeHeaders, rawKey, reqID, tp)
					newNarrowPolicy := buildHostNarrowPolicyFromTarget(initialTarget)
					newWasmCtx := wasm.WithEgressScope(streamCtx, wasm.EgressScope{
						OrgID:        match.Key.OrgID,
						ConnectionID: initialTarget.Connection.ID,
						ActorKind:    "virtual_key",
						ActorID:      match.Key.ID,
						EntryPoint:   wasm.DefaultAuditEntryPoint,
						RequestID:    reqID,
						NarrowPolicy: &newNarrowPolicy,
					})
					reRawHints := rawHints
					reRawHints.Model = effectiveModel
					reInsp, reErr := InspectNativeRequest(newWasmCtx, initialRT, reqID, dialect, upstreamPath, newSafeHeaders, rawBody, reRawHints)
					if reErr != nil {
						if errors.Is(reErr, context.Canceled) {
							if r.Context().Err() == nil && streamCtx.Err() != nil {
								writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
							}
							return
						}
						if len(candidates) <= 1 {
							h.writeRuntimeFaultError(w, reqID, reErr)
							return
						}
						if releaseInitial != nil {
							releaseInitial()
							releaseInitial = nil
						}
						initialRT = nil
						hasInitialInsp = false
					} else {
						if reInsp.TargetModel != "" && reInsp.TargetModel != effectiveModel {
							writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Target adapter redirected model after deployment re-resolution.", false)
							return
						}
						if reInsp.TargetModel == "" {
							reInsp.TargetModel = effectiveModel
						}
						if !modelAllowlistPermits(match.Key.AllowedModels, reInsp.TargetModel) {
							writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key model allowlist denies %q.", reInsp.TargetModel), false)
							return
						}
						reCaps := compactNonEmptyStrings(reInsp.Hints.RequiredCapabilities)
						if missingCap := resolver.FirstMissingCapability(initialTarget, match.Config != nil && match.Config.RequireTestedCaps, reCaps); missingCap != "" {
							writeCanonicalError(w, reqID, http.StatusBadRequest, "capability_not_supported", "invalid_request_error", fmt.Sprintf("Deployment %q does not support required capability %q.", initialTarget.Deployment.ID, missingCap), false)
							return
						}
						insp = reInsp
						effectiveModel = firstNonEmpty(insp.TargetModel, effectiveModel)
					}
				}
			}
		}

		if insp.Stream && !rawHints.Stream {
			if !h.acquireDeferredStreamSlot(w, r, reqID, admLease) {
				return
			}
		}
	}

	outboundBody := append([]byte(nil), rawBody...)
	defer clear(outboundBody)

	budgetSess := h.newNativeBudgetSession(match.Key, reqID, rawBody, estBaseIn, estMaxOut, completionCount)
	if !budgetSess.reserveInitial(streamCtx, w, candidates) {
		return
	}
	defer budgetSess.releaseIfUnsettled()

	requireTested := match.Config != nil && match.Config.RequireTestedCaps
	inspAdapterID := ""
	if hasInitialInsp && initialTarget != nil && initialTarget.Adapter != nil {
		inspAdapterID = initialTarget.Adapter.ID
	}
	execRes := failover.Execute(streamCtx, failover.Config{
		Breaker: h.breaker,
		Now:     func() time.Time { return time.UnixMilli(h.nowMs()).UTC() },
	}, candidates, func(hopCtx context.Context, attemptIdx int, cand *resolver.RankedTarget) failover.AttemptResult {
		var targetID, adpID, region string
		if cand != nil && cand.Target != nil {
			reqTel.recordRouteAttempt(cand.Target, effectiveModel)
			adpID, region, _ = nativeTargetTelemetryLabels(cand.Target)
			if cand.Target.Deployment != nil {
				targetID = cand.Target.Deployment.ID
			}
			if h.telemetry != nil && h.telemetry.Metrics != nil && cand.Target.Adapter != nil {
				stateStr := "ACTIVE_HEALTHY"
				if cand.Target.Deployment != nil && cand.Target.Deployment.State != "" {
					stateStr = cand.Target.Deployment.State
				}
				h.telemetry.Metrics.SetDeploymentCircuitState(adpID, region, stateStr)
			}
		}
		hopSpanCtx, endHopStage := reqTel.startStage(hopCtx, "RouteAttempt",
			attribute.Int("attempt", attemptIdx+1),
			attribute.String("adapter_id", adpID),
			attribute.String("deployment_id", targetID),
			attribute.String("data_region", region),
		)
		var reuseRT port.ProviderRuntime
		var releaseReuse func()
		var preInsp *InspectResult
		if hasInitialInsp && sameResolvedTarget(cand.Target, initialTarget) && initialRT != nil {
			reuseRT = initialRT
			releaseReuse = releaseInitial
			inspCopy := insp
			preInsp = &inspCopy
			releaseInitial = nil
			initialRT = nil
		}
		res := h.executeNativeHop(
			w,
			r,
			streamCtx,
			hopSpanCtx,
			reqID,
			tp,
			dialect,
			upstreamPath,
			cleanQuery,
			rawKey,
			rawBody,
			rawHints,
			outboundBody,
			match.Key,
			requireTested,
			cand.Target,
			effectiveModel,
			inspAdapterID,
			reuseRT,
			releaseReuse,
			preInsp,
			idemClaim,
			admLease,
			budgetSess,
		)
		st := "ok"
		var hopErr error
		if !res.Succeeded {
			st = fmt.Sprintf("http_%d", res.HTTPStatus)
			if res.HTTPStatus >= 500 || res.HTTPStatus == 0 {
				hopErr = fmt.Errorf("route attempt ended with status %d", res.HTTPStatus)
			}
		}
		endHopStage(st, targetID, hopErr)
		return res
	})
	if execRes.ClientCanceled && !execRes.StreamStarted && r.Context().Err() == nil && streamCtx.Err() != nil {
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
		return
	}
	if execRes.AttemptCount == 0 {
		if execRes.DeadlineExceeded {
			writeCanonicalError(w, reqID, http.StatusGatewayTimeout, "upstream_timeout", "upstream_error", "Upstream failover deadline exceeded.", true)
			return
		}
		if execRes.ExhaustedBreaker {
			writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "no_compliant_deployment_available", "residency_unavailable_error", fmt.Sprintf("No healthy compliant deployment is available for model %q.", effectiveModel), true)
		}
	}
}

func sameResolvedTarget(a, b *snapshot.ResolvedTarget) bool {
	if a == nil || b == nil || a.Deployment == nil || b.Deployment == nil || a.Adapter == nil || b.Adapter == nil || a.Connection == nil || b.Connection == nil {
		return false
	}
	return a.Deployment.ID == b.Deployment.ID && a.Adapter.ID == b.Adapter.ID && a.Connection.ID == b.Connection.ID
}

func compactNonEmptyStrings(in []string) []string {
	var out []string
	for _, s := range in {
		if tr := strings.TrimSpace(s); tr != "" {
			out = append(out, tr)
		}
	}
	return out
}

func (h *Handler) executeNativeHop(
	w http.ResponseWriter,
	r *http.Request,
	streamCtx context.Context,
	hopCtx context.Context,
	reqID string,
	traceparent string,
	dialect string,
	upstreamPath string,
	cleanQuery string,
	rawKey string,
	rawBody []byte,
	rawHints RawBodyHints,
	outboundBody []byte,
	key *snapshot.VirtualKeyView,
	requireTested bool,
	target *snapshot.ResolvedTarget,
	effectiveModel string,
	initialAdapterID string,
	rt port.ProviderRuntime,
	releaseCurr func(),
	preInsp *InspectResult,
	idemClaim *idempotency.Claim,
	admLease *nativeAdmissionLease,
	budgetSess *nativeBudgetSession,
) failover.AttemptResult {
	clientCtx := r.Context()
	if abortRes, ok := budgetSess.ensureHoldForTarget(hopCtx, w, target); !ok {
		return abortRes
	}
	if rt == nil {
		var rtErr error
		rt, releaseCurr, rtErr = h.acquireRuntime(hopCtx, target)
		if rtErr != nil {
			if errors.Is(rtErr, context.Canceled) {
				return h.classifyNativeBrokerAttemptError(w, clientCtx, streamCtx, reqID, rtErr, true)
			}
			return h.classifyNativeRuntimeLoadError(w, reqID, rtErr)
		}
	}
	if releaseCurr != nil {
		defer releaseCurr()
	}

	safeHeaders := buildSanitizedNativeHeaders(r.Header, target.Adapter.AllowedNativeHeaders, rawKey, reqID, traceparent)
	narrowPolicy := buildHostNarrowPolicyFromTarget(target)

	var insp InspectResult
	if preInsp != nil && target.Adapter.ID == initialAdapterID {
		insp = *preInsp
	} else {
		wasmCtx := wasm.WithEgressScope(hopCtx, wasm.EgressScope{
			OrgID:        key.OrgID,
			ConnectionID: target.Connection.ID,
			ActorKind:    "virtual_key",
			ActorID:      key.ID,
			EntryPoint:   wasm.DefaultAuditEntryPoint,
			RequestID:    reqID,
			NarrowPolicy: &narrowPolicy,
		})
		hopRawHints := rawHints
		hopRawHints.Model = effectiveModel
		reInsp, reErr := InspectNativeRequest(wasmCtx, rt, reqID, dialect, upstreamPath, safeHeaders, rawBody, hopRawHints)
		if reErr != nil {
			if errors.Is(reErr, context.Canceled) {
				return h.classifyNativeBrokerAttemptError(w, clientCtx, streamCtx, reqID, reErr, true)
			}
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  true,
				Outcome:    breaker.OutcomeSystemFault,
				HTTPStatus: http.StatusBadGateway,
				FlushFinal: func() {
					h.writeRuntimeFaultError(w, reqID, reErr)
				},
			}
		}
		if reInsp.TargetModel != "" && reInsp.TargetModel != effectiveModel {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusBadGateway,
				FlushFinal: func() {
					writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Target adapter redirected model after deployment re-resolution.", false)
				},
			}
		}
		if reInsp.TargetModel == "" {
			reInsp.TargetModel = effectiveModel
		}
		if !modelAllowlistPermits(key.AllowedModels, reInsp.TargetModel) {
			deniedModel := reInsp.TargetModel
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusForbidden,
				FlushFinal: func() {
					writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key model allowlist denies %q.", deniedModel), false)
				},
			}
		}
		reCaps := compactNonEmptyStrings(reInsp.Hints.RequiredCapabilities)
		if missingCap := resolver.FirstMissingCapability(target, requireTested, reCaps); missingCap != "" {
			depID := target.Deployment.ID
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusBadRequest,
				FlushFinal: func() {
					writeCanonicalError(w, reqID, http.StatusBadRequest, "capability_not_supported", "invalid_request_error", fmt.Sprintf("Deployment %q does not support required capability %q.", depID, missingCap), false)
				},
			}
		}
		insp = reInsp
	}

	// Step 6: Build sanitized outbound request preserving exact rawBody bytes.
	parsedBase, perr := url.Parse(strings.TrimSpace(target.Connection.EndpointBaseURL))
	if perr != nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  false,
			Outcome:    breaker.OutcomeCallerError,
			HTTPStatus: http.StatusBadGateway,
			FlushFinal: func() {
				writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Target connection endpoint base URL is malformed.", false)
			},
		}
	}
	parsedBase.Path = strings.TrimRight(parsedBase.Path, "/") + "/" + upstreamPath
	if cleanQuery != "" {
		if parsedBase.RawQuery != "" {
			parsedBase.RawQuery += "&" + cleanQuery
		} else {
			parsedBase.RawQuery = cleanQuery
		}
	}
	targetURL := parsedBase.String()
	if _, cerr := narrowPolicy.ValidateTargetURL(targetURL); cerr != nil {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  false,
			Outcome:    breaker.OutcomeCallerError,
			HTTPStatus: http.StatusForbidden,
			FlushFinal: func() {
				h.writeBrokerOrEgressError(w, reqID, cerr)
			},
		}
	}

	var credInjections []broker.CredentialInjection
	if !strings.EqualFold(strings.TrimSpace(target.Connection.AuthMode), "NONE") {
		if len(target.Connection.SecretBindings) == 0 {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusBadGateway,
				FlushFinal: func() {
					writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Authenticated provider connection has no bound secret slots.", false)
				},
			}
		}
		if len(insp.Hints.CredentialInjections) == 0 {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusBadGateway,
				FlushFinal: func() {
					writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Provider adapter omitted required credential injection instructions for authenticated connection.", false)
				},
			}
		}
		var cerr error
		credInjections, cerr = mapCredentialInjections(insp.Hints.CredentialInjections, target.Connection.SecretBindings)
		if cerr != nil {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusBadGateway,
				FlushFinal: func() {
					writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Provider adapter requested an invalid or unauthorized credential injection.", false)
				},
			}
		}
	}
	stripInjectedCredentialHeaders(safeHeaders, credInjections)
	timeoutOverride := h.firstByteTimeout
	if target.Deployment.FirstByteTimeoutMs > 0 {
		timeoutOverride = time.Duration(target.Deployment.FirstByteTimeoutMs) * time.Millisecond
	}

	egressReq := broker.EgressRequest{
		OrgID:                key.OrgID,
		ConnectionID:         target.Connection.ID,
		Method:               r.Method,
		TargetURL:            targetURL,
		SafeHeaders:          safeHeaders,
		BodyBytes:            outboundBody,
		CredentialInjections: credInjections,
		TimeoutOverride:      timeoutOverride,
		ActorKind:            "virtual_key",
		ActorID:              key.ID,
		EntryPoint:           wasm.DefaultAuditEntryPoint,
		RequestID:            reqID,
	}
	constraint := broker.HostEgressConstraint{NarrowPolicy: &narrowPolicy}

	hopModel := firstNonEmpty(insp.TargetModel, effectiveModel)
	if insp.Stream {
		if admLease != nil && admLease.streamLease == nil && admLease.limits.MaxConcurrentStreams != nil {
			lease, sRes, sErr := h.rateLimiter.AcquireStreamSlot(hopCtx, admLease.scope, *admLease.limits.MaxConcurrentStreams)
			if sErr != nil {
				if errors.Is(sErr, ratelimit.ErrConcurrencyLimitExceeded) {
					return failover.AttemptResult{
						Succeeded:  false,
						Retryable:  false,
						Outcome:    breaker.OutcomeCallerError,
						HTTPStatus: http.StatusTooManyRequests,
						FlushFinal: func() {
							w.Header().Set("X-RateLimit-Remaining-Streams", "0")
							w.Header().Set("Retry-After", strconv.Itoa(max(1, sRes.RetryAfterSec)))
							writeCanonicalError(w, reqID, http.StatusTooManyRequests, "concurrency_limit_exceeded", "rate_limit_error", "Maximum concurrent active streams limit reached for this Virtual Key.", true)
						},
					}
				}
				return failover.AttemptResult{
					Succeeded:  false,
					Retryable:  false,
					Outcome:    breaker.OutcomeCallerError,
					HTTPStatus: http.StatusServiceUnavailable,
					FlushFinal: func() {
						writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
					},
				}
			}
			admLease.streamLease = lease
			w.Header().Set("X-RateLimit-Remaining-Streams", strconv.FormatInt(sRes.RemainingStreams, 10))
		}
		return h.executeStreamingNativeAttempt(w, clientCtx, hopCtx, streamCtx, reqID, dialect, hopModel, key, rt, egressReq, constraint, target, rawBody, admLease, budgetSess)
	}
	return h.executeSyncNativeAttempt(w, clientCtx, hopCtx, streamCtx, reqID, dialect, hopModel, key, rt, egressReq, constraint, target, rawBody, idemClaim, admLease, budgetSess)
}

func (h *Handler) executeSyncNativeAttempt(
	w http.ResponseWriter,
	clientCtx context.Context,
	ctx context.Context,
	streamCtx context.Context,
	reqID string,
	dialect string,
	model string,
	key *snapshot.VirtualKeyView,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	target *snapshot.ResolvedTarget,
	rawReqBody []byte,
	idemClaim *idempotency.Claim,
	admLease *nativeAdmissionLease,
	budgetSess *nativeBudgetSession,
) failover.AttemptResult {
	reqTel := nativeTelemetryFromContext(ctx)
	adpID, region, _ := nativeTargetTelemetryLabels(target)
	eCtx, endEgressStage := reqTel.startStage(ctx, "Egress",
		attribute.String("adapter_id", adpID),
		attribute.String("logical_model", model),
		attribute.String("data_region", region),
	)
	egressStart := time.Now()
	upResp, err := h.broker.ExecuteWithHostConstraint(eCtx, egressReq, constraint)
	egressDur := time.Since(egressStart)
	reqTel.addUpstreamDuration(egressDur)
	statusCode := upResp.StatusCode
	if err != nil && statusCode == 0 {
		statusCode = http.StatusBadGateway
	}
	if h.telemetry != nil && h.telemetry.Metrics != nil {
		h.telemetry.Metrics.RecordUpstreamCall(adpID, model, region, statusCode, "total", egressDur)
	}
	stageStatus := "ok"
	if err != nil {
		stageStatus = "error"
	} else if statusCode >= 400 {
		stageStatus = "upstream_error"
	}
	endEgressStage(stageStatus, "", err)
	if err != nil {
		return h.classifyNativeBrokerAttemptError(w, clientCtx, streamCtx, reqID, err, true)
	}

	if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
		return h.makeNormalizedNativeUpstreamAttemptError(ctx, w, reqID, rt, upResp.StatusCode, upResp.SanitizedHeaders, upResp.Body)
	}

	usage := ExtractSyncUsage(ctx, rt, reqID, rawReqBody, upResp.Body)
	settledUsage, serr := budgetSess.settle(ctx, target, model, usage, int64(len(upResp.Body)), false)
	if serr != nil {
		budgetSess.releaseIfUnsettled()
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  false,
			Outcome:    breaker.OutcomeCallerError,
			HTTPStatus: http.StatusServiceUnavailable,
			FlushFinal: func() {
				writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "budget_ledger_unavailable", "api_error", "Budget ledger settlement failed; failing closed to prevent unbilled response delivery.", false)
			},
		}
	}
	usage = settledUsage
	h.reconcileAdmissionUsage(admLease, &usage)
	h.recordNativeUsageTelemetry(ctx, model, usage)
	h.recordUsage(ctx, UsageRecord{
		RequestID:    reqID,
		Dialect:      dialect,
		Model:        model,
		OrgID:        key.OrgID,
		ProjectID:    key.ProjectID,
		EnvID:        key.EnvID,
		VirtualKeyID: key.ID,
		Usage:        usage,
	})

	contentType := "application/json; charset=utf-8"
	if ct := strings.TrimSpace(upResp.SanitizedHeaders.Get("Content-Type")); ct != "" && !strings.ContainsAny(ct, "\r\n\x00") {
		contentType = ct
	}
	accHeader := formatUsageAccuracyHeader(usage.Accuracy)

	replayHdrs := map[string]string{
		"Content-Type":             contentType,
		"Cache-Control":            "no-store",
		"X-Tanpopo-Usage-Accuracy": accHeader,
	}
	if idemClaim != nil {
		baseCtx := ctx
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		completeCtx, cancelComplete := context.WithTimeout(context.WithoutCancel(baseCtx), 2*time.Second)
		cerr := idemClaim.Complete(completeCtx, upResp.StatusCode, replayHdrs, upResp.Body)
		cancelComplete()
		if cerr != nil {
			idemClaim.Abort(context.Background())
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Tanpopo-Request-Id", reqID)
	w.Header().Set("X-Tanpopo-Usage-Accuracy", accHeader)
	w.WriteHeader(upResp.StatusCode)
	_, _ = w.Write(upResp.Body)
	return failover.AttemptResult{
		Succeeded:  true,
		Outcome:    breaker.OutcomeSuccess,
		HTTPStatus: upResp.StatusCode,
	}
}

func (h *Handler) executeStreamingNativeAttempt(
	w http.ResponseWriter,
	clientCtx context.Context,
	hopCtx context.Context,
	streamCtx context.Context,
	reqID string,
	dialect string,
	model string,
	key *snapshot.VirtualKeyView,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	target *snapshot.ResolvedTarget,
	rawReqBody []byte,
	admLease *nativeAdmissionLease,
	budgetSess *nativeBudgetSession,
) failover.AttemptResult {
	reqTel := nativeTelemetryFromContext(hopCtx)
	adpID, region, extTier := nativeTargetTelemetryLabels(target)
	eCtx, endEgressStage := reqTel.startStage(hopCtx, "Egress",
		attribute.String("adapter_id", adpID),
		attribute.String("logical_model", model),
		attribute.String("data_region", region),
		attribute.Bool("stream", true),
	)
	egressStart := time.Now()

	upCtx, cancelUp := context.WithCancel(eCtx)
	defer cancelUp()

	upStream, err := h.broker.ExecuteStreamWithHostConstraint(upCtx, egressReq, constraint)
	if err != nil {
		dur := time.Since(egressStart)
		reqTel.addUpstreamDuration(dur)
		if h.telemetry != nil && h.telemetry.Metrics != nil {
			h.telemetry.Metrics.RecordUpstreamCall(adpID, model, region, http.StatusBadGateway, "total", dur)
		}
		endEgressStage("error", "", err)
		return h.classifyNativeBrokerAttemptError(w, clientCtx, streamCtx, reqID, err, false)
	}
	defer func() { _ = upStream.Body.Close() }()

	if upStream.StatusCode < 200 || upStream.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(upStream.Body, 1<<20))
		dur := time.Since(egressStart)
		reqTel.addUpstreamDuration(dur)
		if h.telemetry != nil && h.telemetry.Metrics != nil {
			h.telemetry.Metrics.RecordUpstreamCall(adpID, model, region, upStream.StatusCode, "total", dur)
		}
		endEgressStage("upstream_error", "", nil)
		return h.makeNormalizedNativeUpstreamAttemptError(hopCtx, w, reqID, rt, upStream.StatusCode, upStream.SanitizedHeaders, errBody)
	}

	var idleChunkTimeout time.Duration
	if target != nil && target.Deployment != nil && target.Deployment.IdleChunkTimeoutMs > 0 {
		idleChunkTimeout = time.Duration(target.Deployment.IdleChunkTimeoutMs) * time.Millisecond
	}

	var onChunkRelayed func(time.Duration)
	if h.telemetry != nil && h.telemetry.Metrics != nil {
		h.telemetry.Metrics.AddActiveStream("native_http", 1)
		defer h.telemetry.Metrics.AddActiveStream("native_http", -1)
		tel := h.telemetry
		onChunkRelayed = func(relayDuration time.Duration) {
			tel.Metrics.RecordSSEChunkRelay(extTier, relayDuration)
		}
	}

	acc := NewSSEUsageAccumulator(rt, reqID, rawReqBody)
	onStreamStarted := func() {
		ttft := time.Since(egressStart)
		reqTel.setTTFTMs(ttft.Milliseconds())
		if h.telemetry != nil && h.telemetry.Metrics != nil {
			h.telemetry.Metrics.RecordUpstreamCall(adpID, model, region, upStream.StatusCode, "ttft", ttft)
		}
		budgetSess.markStreaming(hopCtx)
	}
	pumpRes, pumpErr := pumpNativeSSE(eCtx, w, reqID, upStream.Body, cancelUp, h.heartbeatInterval, idleChunkTimeout, acc, onStreamStarted, onChunkRelayed)
	totalDur := time.Since(egressStart)
	reqTel.addUpstreamDuration(totalDur)
	if h.telemetry != nil && h.telemetry.Metrics != nil {
		h.telemetry.Metrics.RecordUpstreamCall(adpID, model, region, upStream.StatusCode, "total", totalDur)
	}
	stageStatus := "ok"
	if pumpErr != nil {
		stageStatus = "error"
	}
	endEgressStage(stageStatus, "", pumpErr)

	if pumpErr != nil && !pumpRes.streamStarted {
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
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  true,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: http.StatusBadGateway,
			FlushFinal: func() {
				writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream native stream terminated before emitting any frame.", false)
			},
		}
	}

	usageCtx := context.WithoutCancel(hopCtx)
	finalUsage := acc.Finalize(usageCtx)
	settledUsage, serr := budgetSess.settle(usageCtx, target, model, finalUsage, pumpRes.emittedBytes, pumpErr != nil)
	if serr == nil {
		finalUsage = settledUsage
	}
	h.reconcileAdmissionUsage(admLease, &finalUsage)
	h.recordNativeUsageTelemetry(usageCtx, model, finalUsage)
	h.recordUsage(usageCtx, UsageRecord{
		RequestID:    reqID,
		Dialect:      dialect,
		Model:        model,
		OrgID:        key.OrgID,
		ProjectID:    key.ProjectID,
		EnvID:        key.EnvID,
		VirtualKeyID: key.ID,
		Usage:        finalUsage,
	})

	outcome := breaker.OutcomeSuccess
	if pumpErr != nil {
		if pumpRes.clientDisconnected || errors.Is(pumpErr, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && streamCtx != nil && streamCtx.Err() != nil {
				outcome = breaker.OutcomeCallerError
			} else {
				outcome = breaker.OutcomeClientCanceled
			}
		} else {
			outcome = breaker.OutcomeSystemFault
		}
	}
	return failover.AttemptResult{
		Succeeded:     pumpErr == nil,
		StreamStarted: true,
		Retryable:     false,
		Outcome:       outcome,
		HTTPStatus:    http.StatusOK,
	}
}

func (h *Handler) makeNormalizedNativeUpstreamAttemptError(
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

func (h *Handler) classifyNativeBrokerAttemptError(
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
					h.writeBrokerOrEgressError(w, reqID, err)
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
				h.writeBrokerOrEgressError(w, reqID, err)
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
				h.writeBrokerOrEgressError(w, reqID, err)
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
			h.writeBrokerOrEgressError(w, reqID, err)
		},
	}
}

func (h *Handler) classifyNativeRuntimeLoadError(w http.ResponseWriter, reqID string, rtErr error) failover.AttemptResult {
	var fault *wasm.ExecutionFaultError
	if errors.As(rtErr, &fault) && fault.Kind == wasm.FaultKindQuarantined {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  true,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: http.StatusServiceUnavailable,
			FlushFinal: func() {
				h.writeRuntimeFaultError(w, reqID, rtErr)
			},
		}
	}
	return failover.AttemptResult{
		Succeeded:  false,
		Retryable:  true,
		Outcome:    breaker.OutcomeSystemFault,
		HTTPStatus: http.StatusBadGateway,
		FlushFinal: func() {
			h.writeRuntimeFaultError(w, reqID, rtErr)
		},
	}
}

type nativeReadMsg struct {
	data  []byte
	isEOF bool
	err   error
}

type nativePumpRes struct {
	streamStarted      bool
	framesEmitted      int
	emittedBytes       int64
	clientDisconnected bool
}

func pumpNativeSSE(
	ctx context.Context,
	w http.ResponseWriter,
	reqID string,
	upstream io.ReadCloser,
	cancelUp context.CancelFunc,
	hbInterval time.Duration,
	idleTimeout time.Duration,
	acc *SSEUsageAccumulator,
	onStreamStarted func(),
	onChunkRelayed func(time.Duration),
) (nativePumpRes, error) {
	var res nativePumpRes
	rc := http.NewResponseController(w)

	msgCh := make(chan nativeReadMsg, 4)
	readerDone := make(chan struct{})
	stopReader := make(chan struct{})

	go func() {
		defer close(readerDone)
		defer close(msgCh)
		bufPtr := nativeReadBufPool.Get().(*[]byte)
		buf := *bufPtr

		var partial []byte
		defer func() {
			clear(partial)
			clear(buf)
			nativeReadBufPool.Put(bufPtr)
		}()

		for {
			n, rerr := upstream.Read(buf)
			if n > 0 {
				isEOF := errors.Is(rerr, io.EOF)
				complete, nextPartial, scanErr := sse.ScanCompleteSSELines(partial, buf[:n], sse.DefaultMaxLineBytes, isEOF)
				partial = nextPartial
				if scanErr != nil {
					select {
					case msgCh <- nativeReadMsg{err: scanErr}:
					case <-stopReader:
					}
					return
				}
				if len(complete) > 0 || isEOF {
					select {
					case msgCh <- nativeReadMsg{data: complete, isEOF: isEOF}:
					case <-stopReader:
						return
					}
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					if len(partial) > 0 {
						finalSlice := make([]byte, len(partial))
						copy(finalSlice, partial)
						clear(partial)
						partial = nil
						select {
						case msgCh <- nativeReadMsg{data: finalSlice, isEOF: true}:
						case <-stopReader:
							return
						}
					} else if n == 0 {
						select {
						case msgCh <- nativeReadMsg{isEOF: true}:
						case <-stopReader:
							return
						}
					}
					return
				}
				select {
				case msgCh <- nativeReadMsg{err: rerr}:
				case <-stopReader:
				}
				return
			}
		}
	}()

	abortUpstream := func() {
		if cancelUp != nil {
			cancelUp()
		}
		_ = upstream.Close()
		close(stopReader)
		<-readerDone
	}

	var hbTicker *time.Ticker
	var hbCh <-chan time.Time
	startHeartbeat := func() {
		if hbInterval > 0 && hbTicker == nil {
			hbTicker = time.NewTicker(hbInterval)
			hbCh = hbTicker.C
		}
	}
	defer func() {
		if hbTicker != nil {
			hbTicker.Stop()
		}
	}()

	var idleTimer *time.Timer
	var idleCh <-chan time.Time
	if idleTimeout > 0 {
		idleTimer = time.NewTimer(idleTimeout)
		idleCh = idleTimer.C
		defer idleTimer.Stop()
	}
	resetIdleTimer := func() {
		if idleTimer == nil {
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(idleTimeout)
	}

	var upstreamErr error
	sawEOF := false

pumpLoop:
	for {
		select {
		case <-ctx.Done():
			res.clientDisconnected = true
			abortUpstream()
			return res, ctx.Err()

		case <-idleCh:
			abortUpstream()
			if !res.streamStarted {
				return res, sse.ErrIdleChunkTimeout
			}
			writeMidStreamAbort(w, rc, reqID, "upstream native SSE stream idle timeout")
			return res, fmt.Errorf("%w: %w", sse.ErrMidStreamAbort, sse.ErrIdleChunkTimeout)

		case <-hbCh:
			if res.streamStarted {
				if _, werr := w.Write([]byte(": ping\n\n")); werr != nil {
					res.clientDisconnected = true
					abortUpstream()
					return res, werr
				}
				if ferr := rc.Flush(); ferr != nil {
					res.clientDisconnected = true
					abortUpstream()
					return res, ferr
				}
			}

		case msg, ok := <-msgCh:
			if !ok {
				break pumpLoop
			}
			resetIdleTimer()
			if msg.err != nil {
				upstreamErr = msg.err
				break pumpLoop
			}
			if len(msg.data) > 0 {
				var chunkStart time.Time
				if onChunkRelayed != nil {
					chunkStart = time.Now()
				}
				if acc != nil {
					acc.Feed(ctx, msg.data)
				}
				if !res.streamStarted {
					writeNativeSSEHeaders(w)
					res.streamStarted = true
					if onStreamStarted != nil {
						onStreamStarted()
					}
					startHeartbeat()
				}
				if _, werr := w.Write(msg.data); werr != nil {
					res.clientDisconnected = true
					abortUpstream()
					return res, werr
				}
				res.framesEmitted++
				res.emittedBytes += int64(len(msg.data))
				if ferr := rc.Flush(); ferr != nil {
					res.clientDisconnected = true
					abortUpstream()
					return res, ferr
				}
				if onChunkRelayed != nil {
					onChunkRelayed(time.Since(chunkStart))
				}
			}
			if msg.isEOF {
				sawEOF = true
				break pumpLoop
			}
		}
	}

	downstreamCanceled := ctx.Err() != nil
	if cancelUp != nil {
		cancelUp()
	}
	_ = upstream.Close()
	close(stopReader)
	<-readerDone

	if downstreamCanceled {
		res.clientDisconnected = true
		return res, ctx.Err()
	}
	if upstreamErr != nil {
		if !res.streamStarted {
			return res, upstreamErr
		}
		writeMidStreamAbort(w, rc, reqID, "upstream native stream interrupted mid-stream")
		return res, fmt.Errorf("%w: %w", sse.ErrMidStreamAbort, upstreamErr)
	}
	if !sawEOF || res.framesEmitted == 0 {
		if !res.streamStarted {
			return res, sse.ErrMidStreamAbort
		}
		writeMidStreamAbort(w, rc, reqID, "upstream native stream closed unexpectedly")
		return res, sse.ErrMidStreamAbort
	}
	return res, nil
}

func (h *Handler) recordUsage(ctx context.Context, rec UsageRecord) {
	if h.onUsage != nil {
		h.onUsage(rec)
	}
	if h.audit != nil {
		metaBytes, err := json.Marshal(map[string]any{
			"dialect":            rec.Dialect,
			"model":              rec.Model,
			"input_tokens":       rec.Usage.InputTokens,
			"output_tokens":      rec.Usage.OutputTokens,
			"cache_read_tokens":  rec.Usage.CacheReadTokens,
			"cache_write_tokens": rec.Usage.CacheWriteTokens,
			"reasoning_tokens":   rec.Usage.ReasoningTokens,
			"accuracy":           formatUsageAccuracyHeader(rec.Usage.Accuracy),
		})
		if err == nil {
			reqIDCopy := rec.RequestID
			auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer auditCancel()
			_ = h.audit.Append(auditCtx, storageport.AuditEvent{
				ID:           idgen.MustNew(idgen.PrefixAudit),
				CreatedAtMs:  h.nowMs(),
				OrgID:        &rec.OrgID,
				ActorKind:    "virtual_key",
				ActorID:      rec.VirtualKeyID,
				EntryPoint:   wasm.DefaultAuditEntryPoint,
				EventType:    "native.inference.usage",
				TargetType:   "virtual_key",
				TargetID:     rec.VirtualKeyID,
				RequestID:    &reqIDCopy,
				MetadataJSON: string(metaBytes),
			})
		}
	}
}

func validateAndParseNativePath(w http.ResponseWriter, r *http.Request, reqID string) (dialect, upstreamPath, cleanQuery, altParam string, ok bool) {
	candidates := []string{r.URL.EscapedPath(), r.URL.Path}
	if r.RequestURI != "" {
		rawReqPath := r.RequestURI
		if qIdx := strings.IndexByte(rawReqPath, '?'); qIdx >= 0 {
			rawReqPath = rawReqPath[:qIdx]
		}
		if rawReqPath != "" {
			candidates = append(candidates, rawReqPath)
		}
	}

	for _, cand := range candidates {
		if cand == "" {
			continue
		}
		if cand == "/native" || cand == "/native/" {
			writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Native dialect and upstream path are required.", false)
			return "", "", "", "", false
		}
		if !strings.HasPrefix(cand, "/native/") {
			writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Malformed native passthrough path.", false)
			return "", "", "", "", false
		}
		low := strings.ToLower(cand)
		if strings.ContainsRune(cand, '\\') ||
			strings.Contains(low, "%5c") ||
			strings.Contains(low, "%00") ||
			strings.Contains(cand, "//") ||
			hasControlChars(cand) {
			writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Path traversal or illegal characters are prohibited on /native/*.", false)
			return "", "", "", "", false
		}
		trimmed := strings.TrimPrefix(cand, "/native/")
		segs := strings.Split(trimmed, "/")
		for _, seg := range segs {
			if seg == "" || seg == "." || seg == ".." {
				writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Path dot-segments or empty segments are prohibited on /native/*.", false)
				return "", "", "", "", false
			}
			decoded, derr := url.PathUnescape(seg)
			if derr != nil || decoded == "" || decoded == "." || decoded == ".." ||
				strings.ContainsAny(decoded, " \t\r\n/\\?#%") || hasControlChars(decoded) {
				writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Encoded traversal, whitespace, double-encoding, or control characters are prohibited on /native/*.", false)
				return "", "", "", "", false
			}
		}
	}

	rawAfterPrefix := strings.TrimPrefix(r.URL.Path, "/native/")
	parts := strings.SplitN(rawAfterPrefix, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Native dialect and upstream path are required.", false)
		return "", "", "", "", false
	}

	dialect = strings.ToLower(strings.TrimSpace(parts[0]))
	if !dialectTokenRe.MatchString(dialect) {
		writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Invalid native dialect identifier.", false)
		return "", "", "", "", false
	}

	cleaned := strings.TrimPrefix(path.Clean("/"+parts[1]), "/")
	if cleaned == "" || cleaned == "." || strings.HasPrefix(cleaned, "..") || strings.ContainsAny(cleaned, " \t\r\n%") {
		writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Upstream path canonicalization failed.", false)
		return "", "", "", "", false
	}

	if r.URL.RawQuery != "" {
		if strings.ContainsRune(r.URL.RawQuery, ';') || hasControlChars(r.URL.RawQuery) {
			writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Smuggled query parameters are prohibited on /native/*.", false)
			return "", "", "", "", false
		}
		qVals, qErr := url.ParseQuery(r.URL.RawQuery)
		if qErr != nil {
			writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Malformed query string on /native/*.", false)
			return "", "", "", "", false
		}
		sanitizedQuery := url.Values{}
		for k, vals := range qVals {
			lk := strings.ToLower(strings.TrimSpace(k))
			switch lk {
			case "alt":
				if len(vals) != 1 || (vals[0] != "sse" && vals[0] != "json") {
					writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Unsupported 'alt' query value on /native/*.", false)
					return "", "", "", "", false
				}
				altParam = vals[0]
				sanitizedQuery.Set("alt", vals[0])
			case "beta":
				if len(vals) != 1 || (vals[0] != "true" && vals[0] != "false") {
					writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", "Unsupported 'beta' query value on /native/*.", false)
					return "", "", "", "", false
				}
				sanitizedQuery.Set("beta", vals[0])
			default:
				writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", fmt.Sprintf("Query parameter %q is prohibited on /native/*.", k), false)
				return "", "", "", "", false
			}
		}
		cleanQuery = sanitizedQuery.Encode()
	}

	return dialect, cleaned, cleanQuery, altParam, true
}

func (h *Handler) authenticateNativeRequest(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	dialect string,
	upstreamPath string,
) (string, context.Context, context.CancelFunc, snapshot.KeyMatchResult, uint64, bool) {
	if hasAdminSessionCookie(r) {
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "Admin session cookies are prohibited on /native/* inference endpoints; authenticate with a Virtual Key.", false)
		return "", nil, nil, snapshot.KeyMatchResult{}, 0, false
	}

	var candidates []string
	for _, rawAuth := range r.Header.Values("Authorization") {
		authHdr := strings.TrimSpace(rawAuth)
		if authHdr == "" {
			continue
		}
		tok, ok := extractBearerToken(authHdr)
		if !ok {
			writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Malformed Authorization header.", false)
			return "", nil, nil, snapshot.KeyMatchResult{}, 0, false
		}
		candidates = append(candidates, tok)
	}
	for _, hdrName := range []string{"x-api-key", "x-goog-api-key", "X-Tanpopo-Virtual-Key"} {
		for _, v := range r.Header.Values(hdrName) {
			tv := strings.TrimSpace(v)
			if tv == "" {
				continue
			}
			if strings.ContainsAny(tv, " \t\r\n") {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Malformed Virtual Key header.", false)
				return "", nil, nil, snapshot.KeyMatchResult{}, 0, false
			}
			candidates = append(candidates, tv)
		}
	}

	if len(candidates) == 0 {
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Missing Virtual Key credential.", false)
		return "", nil, nil, snapshot.KeyMatchResult{}, 0, false
	}

	rawKey := candidates[0]
	for _, c := range candidates[1:] {
		if c != rawKey {
			writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Conflicting Virtual Key credentials supplied across headers.", false)
			return "", nil, nil, snapshot.KeyMatchResult{}, 0, false
		}
	}
	if !strings.HasPrefix(rawKey, "vk_tp_") {
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "Presented credential is not a Data Plane Virtual Key.", false)
		return "", nil, nil, snapshot.KeyMatchResult{}, 0, false
	}

	streamCtx, cancelStream := context.WithCancel(r.Context())
	targetEnvID := strings.TrimSpace(r.Header.Get("X-Tanpopo-Env-Id"))
	match, handleID, authErr := h.snapshots.RegisterAuthenticatedStream(rawKey, targetEnvID, cancelStream)
	if authErr != nil {
		cancelStream()
		h.writeSnapshotAuthError(w, reqID, authErr)
		return "", nil, nil, snapshot.KeyMatchResult{}, 0, false
	}

	if !planePermitsProvider(match.Key.AllowedPlanes) {
		h.snapshots.Streams().Unregister(match.Key.ID, handleID)
		cancelStream()
		writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", "Virtual Key is not authorized for the PROVIDER plane.", false)
		return "", nil, nil, snapshot.KeyMatchResult{}, 0, false
	}
	if !routeAllowlistPermitsNative(match.Key.AllowedRoutes, dialect, upstreamPath) {
		h.snapshots.Streams().Unregister(match.Key.ID, handleID)
		cancelStream()
		writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key route allowlist denies /native/%s/%s.", dialect, upstreamPath), false)
		return "", nil, nil, snapshot.KeyMatchResult{}, 0, false
	}
	return rawKey, streamCtx, cancelStream, match, handleID, true
}

func routeAllowlistPermitsNative(allowedRoutes []string, dialect, upstreamPath string) bool {
	fullPath := "/native/" + dialect + "/" + upstreamPath
	dialectWildcard := "/native/" + dialect + "/*"
	for _, r := range allowedRoutes {
		tr := strings.TrimSpace(r)
		if tr == "*" || tr == "/native/*" || tr == dialectWildcard || tr == fullPath || tr == "native" || tr == "native."+dialect {
			return true
		}
	}
	return false
}

const maxCompiledNativePathRegex = 512

func (h *Handler) compiledPathRegex(pat string) (*regexp.Regexp, error) {
	full := "^(?:" + pat + ")$"
	h.regexMu.RLock()
	re, ok := h.regexCache[full]
	h.regexMu.RUnlock()
	if ok {
		return re, nil
	}
	if _, err := regexp.Compile(pat); err != nil {
		return nil, err
	}
	compiled, err := regexp.Compile(full)
	if err != nil {
		return nil, err
	}
	h.regexMu.Lock()
	if h.regexCache == nil {
		h.regexCache = make(map[string]*regexp.Regexp)
	}
	if len(h.regexCache) >= maxCompiledNativePathRegex {
		clear(h.regexCache)
	}
	h.regexCache[full] = compiled
	h.regexMu.Unlock()
	return compiled, nil
}

func (h *Handler) adapterAllowsNativePath(adp *snapshot.ProviderAdapterView, upstreamPath string) bool {
	if adp == nil || len(adp.AllowedNativePaths) == 0 {
		return false // Strict Default-Deny (TP-IMP-0017)
	}
	for _, rawPattern := range adp.AllowedNativePaths {
		pat := strings.TrimSpace(rawPattern)
		if pat == "" {
			continue
		}
		re, err := h.compiledPathRegex(pat)
		if err != nil {
			continue
		}
		if re.MatchString(upstreamPath) {
			return true
		}
	}
	return false
}

func stripInjectedCredentialHeaders(safeHeaders map[string]string, creds []broker.CredentialInjection) {
	if len(safeHeaders) == 0 || len(creds) == 0 {
		return
	}
	strip := make(map[string]struct{}, len(creds))
	for _, c := range creds {
		if c.Location != broker.TargetHeader && c.Location != broker.TargetHostCryptoSign {
			continue
		}
		if f := strings.ToLower(strings.TrimSpace(c.FieldName)); f != "" {
			strip[f] = struct{}{}
		}
	}
	for k := range safeHeaders {
		if _, found := strip[strings.ToLower(strings.TrimSpace(k))]; found {
			delete(safeHeaders, k)
		}
	}
}

func buildSanitizedNativeHeaders(
	inbound http.Header,
	allowlist []string,
	rawVirtualKey string,
	reqID string,
	traceparent string,
) map[string]string {
	allowedSet := make(map[string]struct{}, len(allowlist))
	for _, h := range allowlist {
		lh := strings.ToLower(strings.TrimSpace(h))
		if lh != "" && !isProhibitedOutboundHeader(lh) {
			allowedSet[lh] = struct{}{}
		}
	}

	out := map[string]string{
		"X-Tanpopo-Request-Id": reqID,
		"Traceparent":          traceparent,
	}

	for name, vals := range inbound {
		if len(vals) == 0 {
			continue
		}
		low := strings.ToLower(strings.TrimSpace(name))
		if isProhibitedOutboundHeader(low) {
			continue
		}
		_, explicitlyAllowed := allowedSet[low]
		isStandardFraming := low == "content-type" || low == "accept"
		if !explicitlyAllowed && !isStandardFraming {
			continue
		}
		joined := strings.TrimSpace(strings.Join(vals, ", "))
		if joined == "" || strings.ContainsAny(joined, "\r\n\x00") {
			continue
		}
		if rawVirtualKey != "" && strings.Contains(joined, rawVirtualKey) {
			continue
		}
		out[http.CanonicalHeaderKey(name)] = joined
	}
	return out
}

func isProhibitedOutboundHeader(lower string) bool {
	switch lower {
	case "", "authorization", "proxy-authorization", "x-api-key", "x-goog-api-key",
		"x-tanpopo-virtual-key", "cookie", "set-cookie", "host", "connection",
		"content-length", "transfer-encoding", "te", "trailer", "upgrade",
		"keep-alive", "proxy-connection", "accept-encoding", "x-tanpopo-request-id",
		"traceparent", "x-tanpopo-env-id", "idempotency-key":
		return true
	default:
		return strings.HasPrefix(lower, "x-tanpopo-")
	}
}

func (h *Handler) readBoundedBody(w http.ResponseWriter, r *http.Request, reqID string) ([]byte, bool) {
	if r.Body == nil {
		return nil, true
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxReqBodyBytes)
	rawBody, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		clear(rawBody)
		var maxErr *http.MaxBytesError
		if errors.As(readErr, &maxErr) {
			writeCanonicalError(w, reqID, http.StatusRequestEntityTooLarge, "payload_too_large", "invalid_request_error", fmt.Sprintf("Request body exceeds %d byte limit.", h.maxReqBodyBytes), false)
			return nil, false
		}
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Failed to read request body.", false)
		return nil, false
	}
	return rawBody, true
}

func (h *Handler) beginIdempotency(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	key *snapshot.VirtualKeyView,
	routePath string,
	rawBody []byte,
) (*idempotency.Claim, bool) {
	rawIdem := r.Header.Values("Idempotency-Key")
	if len(rawIdem) == 0 || h.idempotency == nil {
		return nil, false
	}
	if len(rawIdem) > 1 {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Multiple Idempotency-Key headers are prohibited.", false)
		return nil, true
	}
	idemKey, err := idempotency.ValidateKey(rawIdem[0])
	if err != nil {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Idempotency-Key must be 1..128 printable non-whitespace ASCII characters.", false)
		return nil, true
	}

	digest := idempotency.ComputePayloadSHA256(r.Method, routePath, rawBody)
	scope := idempotency.ScopeKey{
		OrgID:          key.OrgID,
		ProjectID:      key.ProjectID,
		VirtualKeyID:   key.ID,
		IdempotencyKey: idemKey,
	}
	res, berr := h.idempotency.Begin(r.Context(), scope, digest)
	if berr != nil {
		writeCanonicalError(w, reqID, http.StatusInternalServerError, "internal_error", "api_error", "Failed to claim idempotency key.", true)
		return nil, true
	}
	switch res.Outcome {
	case idempotency.OutcomeAcquired:
		return res.Claim, false
	case idempotency.OutcomeReplay:
		for k, v := range res.Replay.Headers {
			if k != "" {
				w.Header().Set(k, v)
			}
		}
		w.Header().Set("X-Tanpopo-Request-Id", reqID)
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(res.Replay.StatusCode)
		_, _ = w.Write(res.Replay.Body)
		return nil, true
	case idempotency.OutcomeInFlight:
		writeCanonicalError(w, reqID, http.StatusConflict, "request_in_progress", "conflict_error", "A request with this Idempotency-Key is currently in progress.", true)
		return nil, true
	case idempotency.OutcomeMismatch:
		writeCanonicalError(w, reqID, http.StatusUnprocessableEntity, "idempotency_key_reused_mismatch", "invalid_request_error", "Idempotency-Key was reused with a modified request payload.", false)
		return nil, true
	default:
		writeCanonicalError(w, reqID, http.StatusInternalServerError, "internal_error", "api_error", "Unexpected idempotency state.", true)
		return nil, true
	}
}

func (h *Handler) writeSnapshotAuthError(w http.ResponseWriter, reqID string, err error) {
	switch {
	case errors.Is(err, snapshot.ErrSnapshotNotLoaded),
		errors.Is(err, snapshot.ErrSnapshotStale),
		errors.Is(err, snapshot.ErrSnapshotWatcherStopped):
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "snapshot_not_ready", "api_error", "Gateway configuration snapshot is stale or unavailable.", true)
	case errors.Is(err, snapshot.ErrKeyQuotaPaused):
		writeCanonicalError(w, reqID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limit_error", "Virtual Key is temporarily quota-paused.", true)
	case errors.Is(err, snapshot.ErrKeySuspended),
		errors.Is(err, snapshot.ErrEnvironmentScopeDenied),
		errors.Is(err, snapshot.ErrTenantSuspended):
		writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", "Virtual Key or tenant scope is inactive or denied.", false)
	default:
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key is invalid, expired, or revoked.", false)
	}
}

func (h *Handler) writeRuntimeFaultError(w http.ResponseWriter, reqID string, err error) {
	var fault *wasm.ExecutionFaultError
	if errors.As(err, &fault) {
		if fault.Kind == wasm.FaultKindQuarantined {
			writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "extension_quarantined", "extension_error", "The backing provider adapter extension is quarantined.", false)
			return
		}
		writeCanonicalError(w, reqID, fault.HTTPStatus(), fault.CanonicalCode(), fault.ErrorType(), "Provider adapter runtime encountered an isolated execution fault.", true)
		return
	}
	writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Provider adapter runtime failed during native passthrough inspection.", true)
}

func (h *Handler) writeBrokerOrEgressError(w http.ResponseWriter, reqID string, err error) {
	var bErr *broker.Error
	if errors.As(err, &bErr) {
		status := bErr.HTTPStatus
		if status <= 0 {
			status = http.StatusBadGateway
		}
		errType := "upstream_error"
		if status == http.StatusForbidden || status == http.StatusUnprocessableEntity {
			errType = "permission_error"
		}
		writeCanonicalError(w, reqID, status, bErr.Code, errType, "Outbound provider request rejected or failed at host broker.", false)
		return
	}
	var eErr *egress.Error
	if errors.As(err, &eErr) {
		status := eErr.HTTPStatus
		if status <= 0 {
			status = http.StatusForbidden
		}
		writeCanonicalError(w, reqID, status, eErr.Code, "invalid_request_error", "Outbound target failed host SSRF/egress policy enforcement.", false)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeCanonicalError(w, reqID, http.StatusGatewayTimeout, "upstream_timeout", "upstream_error", "Upstream provider timed out.", true)
		return
	}
	writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_connection_failed", "upstream_error", "Failed to reach upstream provider.", true)
}

func buildHostNarrowPolicyFromTarget(target *snapshot.ResolvedTarget) egress.Policy {
	var perms installer.PermissionSet
	if target != nil && target.Adapter != nil && strings.TrimSpace(target.Adapter.ApprovedPermissionsJSON) != "" {
		if err := json.Unmarshal([]byte(target.Adapter.ApprovedPermissionsJSON), &perms); err != nil {
			perms = installer.PermissionSet{}
		}
	}
	var adpHosts []string
	if target != nil && target.Adapter != nil {
		adpHosts = target.Adapter.AllowedHosts
	}
	hosts := intersectAllowedHosts(perms.AllowedHosts, adpHosts)

	var allowedPorts []int
	var allowPriv bool
	var allowedCIDRs []string
	if target != nil && target.Connection != nil {
		if p := endpointEffectivePort(target.Connection.EndpointBaseURL); p > 0 {
			allowedPorts = []int{p}
		}
		allowPriv = target.Connection.AllowPrivateIP
		allowedCIDRs = append([]string(nil), target.Connection.AllowedCIDRs...)
	}
	if !perms.AllowPrivateIP {
		allowPriv = false
	}
	return egress.Policy{
		AllowedHosts:   hosts,
		AllowedPorts:   allowedPorts,
		AllowPrivateIP: allowPriv,
		AllowedCIDRs:   allowedCIDRs,
		RequireTLS:     perms.RequireTLS,
	}
}

func intersectAllowedHosts(permHosts, adpHosts []string) []string {
	type entry struct {
		host    string
		port    string
		hasPort bool
	}
	parseEntry := func(raw string) (entry, bool) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return entry{}, false
		}
		if addr, err := netip.ParseAddr(strings.Trim(trimmed, "[]")); err == nil {
			return entry{host: strings.ToLower(addr.String())}, true
		}
		if strings.Contains(trimmed, ":") {
			if h, p, err := net.SplitHostPort(trimmed); err == nil {
				nh := strings.ToLower(strings.Trim(strings.TrimSpace(h), "[]"))
				np := strings.TrimSpace(p)
				if nh == "" || np == "" {
					return entry{}, false
				}
				return entry{host: nh, port: np, hasPort: true}, true
			}
		}
		return entry{host: strings.ToLower(trimmed)}, true
	}
	formatEntry := func(e entry) string {
		if e.hasPort {
			return net.JoinHostPort(e.host, e.port)
		}
		if strings.Contains(e.host, ":") {
			return "[" + e.host + "]"
		}
		return e.host
	}
	normalize := func(in []string) []entry {
		seen := make(map[string]struct{}, len(in))
		out := make([]entry, 0, len(in))
		for _, raw := range in {
			e, ok := parseEntry(raw)
			if !ok {
				continue
			}
			k := formatEntry(e)
			if _, dup := seen[k]; !dup {
				seen[k] = struct{}{}
				out = append(out, e)
			}
		}
		return out
	}

	np := normalize(permHosts)
	if len(np) == 0 {
		return nil
	}
	na := normalize(adpHosts)
	if len(na) == 0 {
		out := make([]string, len(np))
		for i, e := range np {
			out[i] = formatEntry(e)
		}
		sort.Strings(out)
		return out
	}

	seen := make(map[string]struct{})
	var out []string
	for _, pe := range np {
		for _, ae := range na {
			if pe.host != ae.host {
				continue
			}
			var narrowed entry
			switch {
			case pe.hasPort && ae.hasPort:
				if pe.port != ae.port {
					continue
				}
				narrowed = pe
			case pe.hasPort:
				narrowed = pe
			case ae.hasPort:
				narrowed = ae
			default:
				narrowed = pe
			}
			k := formatEntry(narrowed)
			if _, dup := seen[k]; !dup {
				seen[k] = struct{}{}
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func mapCredentialInjections(specs []port.CredentialInjectInstruction, bindings map[string]*snapshot.ConnectionSecretBindingView) ([]broker.CredentialInjection, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if len(bindings) == 0 {
		return nil, fmt.Errorf("authenticated connection lacks required secret bindings")
	}
	out := make([]broker.CredentialInjection, 0, len(specs))
	for _, s := range specs {
		b, bound := bindings[s.SecretSlotName]
		if !bound || b == nil {
			continue
		}
		if b.InjectMode == broker.InjectModeHostCryptoSign {
			if s.TargetLocation != port.CredentialTargetLocationHostCryptoSign {
				return nil, fmt.Errorf("slot %q requires HOST_CRYPTO_SIGN binding mode", s.SecretSlotName)
			}
		}
		var loc broker.TargetLocation
		switch s.TargetLocation {
		case port.CredentialTargetLocationHeader:
			loc = broker.TargetHeader
		case port.CredentialTargetLocationQueryParam:
			loc = broker.TargetQueryParam
		case port.CredentialTargetLocationHostCryptoSign:
			loc = broker.TargetHostCryptoSign
		default:
			return nil, fmt.Errorf("unsupported credential target_location %q", s.TargetLocation)
		}
		inj := broker.CredentialInjection{
			SecretSlotName: s.SecretSlotName,
			Location:       loc,
			FieldName:      s.FieldName,
			ValuePrefix:    s.ValuePrefix,
		}
		if s.CryptoSign != nil {
			inj.CryptoSign = &broker.CryptoSignSpec{
				Algorithm:                broker.SigningAlgorithm(strings.TrimPrefix(string(s.CryptoSign.Algorithm), "SIGNING_ALGORITHM_")),
				CanonicalPayload:         append([]byte(nil), s.CryptoSign.CanonicalPayload...),
				OutputEncoding:           broker.SignatureEncoding(strings.TrimPrefix(string(s.CryptoSign.OutputEncoding), "SIGNATURE_ENCODING_")),
				AWSRegion:                s.CryptoSign.AWSRegion,
				AWSService:               s.CryptoSign.AWSService,
				SigningTimestampEpochSec: s.CryptoSign.SigningTimestampEpochSec,
				SignedHeaders:            append([]string(nil), s.CryptoSign.SignedHeaders...),
			}
		}
		out = append(out, inj)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no adapter credential slots match connection secret bindings")
	}
	return out, nil
}

func hasControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func hasAdminSessionCookie(r *http.Request) bool {
	for _, c := range r.Cookies() {
		name := strings.ToLower(strings.TrimSpace(c.Name))
		switch name {
		case "tanpopo_session", "__host-tanpopo_session", "tanpopo_admin_session", "__host-tanpopo_admin_session":
			if strings.TrimSpace(c.Value) != "" {
				return true
			}
		}
	}
	return false
}

func extractBearerToken(authHdr string) (string, bool) {
	if authHdr == "" {
		return "", false
	}
	const prefix = "Bearer "
	if len(authHdr) <= len(prefix) || !strings.EqualFold(authHdr[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(authHdr[len(prefix):])
	if tok == "" || strings.ContainsAny(tok, " \t\r\n") {
		return "", false
	}
	return tok, true
}

func planePermitsProvider(allowedPlanes string) bool {
	trimmed := strings.TrimSpace(allowedPlanes)
	if trimmed == "" {
		return false
	}
	if trimmed == "*" {
		return true
	}
	for _, part := range strings.Split(trimmed, ",") {
		p := strings.ToUpper(strings.TrimSpace(part))
		if p == "*" || p == "PROVIDER" || p == "DATA" || p == "ALL" {
			return true
		}
	}
	return false
}

func modelAllowlistPermits(allowedModels []string, model string) bool {
	for _, m := range allowedModels {
		tr := strings.TrimSpace(m)
		if tr == "*" || tr == model {
			return true
		}
	}
	return false
}

func endpointEffectivePort(rawURL string) int {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return 0
	}
	if pStr := u.Port(); pStr != "" {
		if pNum, perr := strconv.Atoi(pStr); perr == nil && pNum >= 1 && pNum <= 65535 {
			return pNum
		}
		return 0
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return 443
	case "http":
		return 80
	default:
		return 0
	}
}

func flattenHTTPHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}

func formatUsageAccuracyHeader(acc port.UsageAccuracy) string {
	switch acc {
	case port.UsageAccuracyExact:
		return "EXACT"
	case port.UsageAccuracyUnmeteredFlagged:
		return "UNMETERED_FLAGGED"
	default:
		return "ESTIMATED"
	}
}

func mapUpstreamHTTPStatus(status int) int {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity,
		http.StatusTooManyRequests:
		return status
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func writeNativeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

func writeMidStreamAbort(w http.ResponseWriter, rc *http.ResponseController, requestID, message string) {
	envelope := map[string]any{
		"error": map[string]any{
			"code":       "mid_stream_abort",
			"type":       "upstream_error",
			"message":    message,
			"request_id": requestID,
			"retryable":  false,
		},
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		payload = []byte(`{"error":{"code":"mid_stream_abort","type":"upstream_error","message":"upstream stream interrupted","retryable":false}}`)
	}
	_, _ = w.Write([]byte("event: error\ndata: "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte("\n\n"))
	_ = rc.Flush()
}

func writeCanonicalError(w http.ResponseWriter, reqID string, status int, code, errType, message string, retryable bool) {
	if ec, ok := w.(interface{ SetCanonicalErrorCode(string) }); ok {
		ec.SetCanonicalErrorCode(code)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Tanpopo-Request-Id", reqID)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":       code,
			"type":       errType,
			"message":    message,
			"request_id": reqID,
			"retryable":  retryable,
		},
	})
}

func sanitizeOrMintRequestID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > 0 && len(trimmed) <= maxTraceHeaderLength && isSafeTraceToken(trimmed) {
		return trimmed
	}
	trc := idgen.MustNew(idgen.PrefixTrace)
	return "req_" + strings.TrimPrefix(trc, idgen.PrefixTrace+"_")
}

func sanitizeOrMintTraceparent(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if isValidW3CTraceparent(trimmed) {
		return strings.ToLower(trimmed)
	}
	var traceID [16]byte
	var spanID [8]byte
	_, _ = rand.Read(traceID[:])
	_, _ = rand.Read(spanID[:])
	traceID[0] |= 0x01
	spanID[0] |= 0x01
	return "00-" + hex.EncodeToString(traceID[:]) + "-" + hex.EncodeToString(spanID[:]) + "-01"
}

func isSafeTraceToken(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func isValidW3CTraceparent(s string) bool {
	if len(s) != 55 || s[2] != '-' || s[35] != '-' || s[52] != '-' || s[0] != '0' || s[1] != '0' {
		return false
	}
	nonZero := false
	for i := 3; i < 55; i++ {
		if i == 35 || i == 52 {
			continue
		}
		c := s[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
		if i < 52 && c != '0' {
			nonZero = true
		}
	}
	return nonZero
}

func sanitizeErrorToken(raw, fallback string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 64 {
		return fallback
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
			continue
		}
		return fallback
	}
	return trimmed
}

func sanitizePublicErrorMessage(raw string, maxBytes int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(min(len(trimmed), maxBytes))
	for _, r := range trimmed {
		if r == '\r' || r == '\n' || r == '\t' {
			r = ' '
		} else if r < 0x20 || r == 0x7f {
			continue
		}
		rLen := utf8.RuneLen(r)
		if rLen < 0 {
			continue
		}
		if b.Len()+rLen > maxBytes {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func headerContainsToken(vals []string, target string) bool {
	for _, v := range vals {
		for _, tok := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), target) {
				return true
			}
		}
	}
	return false
}

type nativeAdmissionLease struct {
	streamLease *ratelimit.StreamLease
	scope       ratelimit.LimitScope
	limits      ratelimit.Limits
	estTokens   int64
}

func (a *nativeAdmissionLease) Release() {
	if a != nil && a.streamLease != nil {
		a.streamLease.Release()
	}
}

func (h *Handler) evaluateNativePreAdmissionPolicy(
	w http.ResponseWriter,
	reqID string,
	match snapshot.KeyMatchResult,
	eff policyengine.EffectivePolicy,
	modelSlug string,
	reqRegion string,
	rawBody []byte,
	estTokens int64,
) bool {
	if !modelAllowlistPermits(match.Key.AllowedModels, modelSlug) {
		writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key model allowlist denies %q.", modelSlug), false)
		return false
	}
	dec := policyengine.EvaluateEffective(eff, policyengine.EvalRequest{
		OrgID:                match.Key.OrgID,
		ProjectID:            match.Key.ProjectID,
		EnvID:                match.Key.EnvID,
		ModelSlug:            modelSlug,
		RequestedDataRegion:  reqRegion,
		RequestBodyBytes:     int64(len(rawBody)),
		EstimatedTotalTokens: estTokens,
	})
	if !dec.Allowed {
		if dec.DenialCode == policyengine.DenialCodeRegionDenied {
			writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "no_compliant_deployment_available", "residency_unavailable_error", dec.DenialReason+" ("+policyengine.DenialCodeRegionDenied+")", true)
			return false
		}
		writeCanonicalError(w, reqID, dec.HTTPStatus, dec.DenialCode, dec.ErrorType, dec.DenialReason, false)
		return false
	}
	return true
}

func resolveNativeRequestedDataRegion(r *http.Request) (string, bool) {
	if r == nil {
		return "", true
	}
	var resolved string
	for _, hdr := range r.Header.Values("X-Tanpopo-Data-Region") {
		for _, part := range strings.Split(hdr, ",") {
			tok := strings.ToUpper(strings.TrimSpace(part))
			if tok == "" {
				continue
			}
			if resolved != "" && resolved != tok {
				return "", false
			}
			resolved = tok
		}
	}
	return resolved, true
}

func (h *Handler) acquireNativePreIdempotencyRPM(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	match snapshot.KeyMatchResult,
	eff policyengine.EffectivePolicy,
) bool {
	scope := ratelimit.LimitScope{
		OrgID:        match.Key.OrgID,
		ProjectID:    match.Key.ProjectID,
		EnvID:        match.Key.EnvID,
		VirtualKeyID: match.Key.ID,
	}
	rpmOnly := ratelimit.Limits{
		RPMLimit:             eff.RPMLimit,
		MaxConcurrentStreams: eff.MaxConcurrentStreams,
	}
	rlRes, rlErr := h.rateLimiter.AllowRequest(r.Context(), scope, 0, rpmOnly)
	writeNativeRateLimitHeaders(w, rlRes, false)
	if rlErr != nil {
		if errors.Is(rlErr, ratelimit.ErrRateLimitExceeded) {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, rlRes.RetryAfterSec)))
			writeCanonicalError(w, reqID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limit_error", "Rate limit exceeded for Virtual Key or parent policy scope.", true)
			return false
		}
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
		return false
	}
	return true
}

func (h *Handler) acquireNativePostIdempotencyTPM(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	match snapshot.KeyMatchResult,
	eff policyengine.EffectivePolicy,
	estTokens int64,
) (*nativeAdmissionLease, bool) {
	scope := ratelimit.LimitScope{
		OrgID:        match.Key.OrgID,
		ProjectID:    match.Key.ProjectID,
		EnvID:        match.Key.EnvID,
		VirtualKeyID: match.Key.ID,
	}
	limits := ratelimit.Limits{
		RPMLimit:             eff.RPMLimit,
		TPMLimit:             eff.TPMLimit,
		MaxConcurrentStreams: eff.MaxConcurrentStreams,
	}
	tpmOnly := ratelimit.Limits{
		TPMLimit:             eff.TPMLimit,
		MaxConcurrentStreams: eff.MaxConcurrentStreams,
	}
	rlRes, rlErr := h.rateLimiter.AllowRequest(r.Context(), scope, estTokens, tpmOnly)
	writeNativeRateLimitHeaders(w, rlRes, false)
	if rlErr != nil {
		if errors.Is(rlErr, ratelimit.ErrRateLimitExceeded) {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, rlRes.RetryAfterSec)))
			writeCanonicalError(w, reqID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limit_error", "Rate limit exceeded for Virtual Key or parent policy scope.", true)
			return nil, false
		}
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
		return nil, false
	}
	return &nativeAdmissionLease{
		scope:     scope,
		limits:    limits,
		estTokens: estTokens,
	}, true
}

func (h *Handler) acquireNativeRateLimits(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	match snapshot.KeyMatchResult,
	eff policyengine.EffectivePolicy,
	estTokens int64,
	stream bool,
) (*nativeAdmissionLease, bool) {
	scope := ratelimit.LimitScope{
		OrgID:        match.Key.OrgID,
		ProjectID:    match.Key.ProjectID,
		EnvID:        match.Key.EnvID,
		VirtualKeyID: match.Key.ID,
	}
	limits := ratelimit.Limits{
		RPMLimit:             eff.RPMLimit,
		TPMLimit:             eff.TPMLimit,
		MaxConcurrentStreams: eff.MaxConcurrentStreams,
	}

	var streamLease *ratelimit.StreamLease
	if stream && limits.MaxConcurrentStreams != nil {
		lease, sRes, sErr := h.rateLimiter.AcquireStreamSlot(r.Context(), scope, *limits.MaxConcurrentStreams)
		if sErr != nil {
			if errors.Is(sErr, ratelimit.ErrConcurrencyLimitExceeded) {
				w.Header().Set("X-RateLimit-Remaining-Streams", "0")
				w.Header().Set("Retry-After", strconv.Itoa(max(1, sRes.RetryAfterSec)))
				writeCanonicalError(w, reqID, http.StatusTooManyRequests, "concurrency_limit_exceeded", "rate_limit_error", "Maximum concurrent active streams limit reached for this Virtual Key.", true)
				return nil, false
			}
			writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
			return nil, false
		}
		streamLease = lease
		w.Header().Set("X-RateLimit-Remaining-Streams", strconv.FormatInt(sRes.RemainingStreams, 10))
	}

	rlRes, rlErr := h.rateLimiter.AllowRequest(r.Context(), scope, estTokens, limits)
	writeNativeRateLimitHeaders(w, rlRes, streamLease != nil)
	if rlErr != nil {
		if streamLease != nil {
			streamLease.Release()
		}
		if errors.Is(rlErr, ratelimit.ErrRateLimitExceeded) {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, rlRes.RetryAfterSec)))
			writeCanonicalError(w, reqID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limit_error", "Rate limit exceeded for Virtual Key or parent policy scope.", true)
			return nil, false
		}
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
		return nil, false
	}

	return &nativeAdmissionLease{
		streamLease: streamLease,
		scope:       scope,
		limits:      limits,
		estTokens:   estTokens,
	}, true
}

func (h *Handler) acquireDeferredStreamSlot(w http.ResponseWriter, r *http.Request, reqID string, adm *nativeAdmissionLease) bool {
	if adm == nil || adm.streamLease != nil || adm.limits.MaxConcurrentStreams == nil {
		return true
	}
	lease, sRes, sErr := h.rateLimiter.AcquireStreamSlot(r.Context(), adm.scope, *adm.limits.MaxConcurrentStreams)
	if sErr != nil {
		if errors.Is(sErr, ratelimit.ErrConcurrencyLimitExceeded) {
			w.Header().Set("X-RateLimit-Remaining-Streams", "0")
			w.Header().Set("Retry-After", strconv.Itoa(max(1, sRes.RetryAfterSec)))
			writeCanonicalError(w, reqID, http.StatusTooManyRequests, "concurrency_limit_exceeded", "rate_limit_error", "Maximum concurrent active streams limit reached for this Virtual Key.", true)
			return false
		}
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
		return false
	}
	adm.streamLease = lease
	w.Header().Set("X-RateLimit-Remaining-Streams", strconv.FormatInt(sRes.RemainingStreams, 10))
	return true
}

func writeNativeRateLimitHeaders(w http.ResponseWriter, res ratelimit.CheckResult, streamHeaderWritten bool) {
	if res.HasRPM {
		w.Header().Set("X-RateLimit-Remaining-Requests", strconv.FormatInt(res.RemainingRequests, 10))
	}
	if res.HasTPM {
		w.Header().Set("X-RateLimit-Remaining-Tokens", strconv.FormatInt(res.RemainingTokens, 10))
	}
	if res.HasStreams && !streamHeaderWritten {
		w.Header().Set("X-RateLimit-Remaining-Streams", strconv.FormatInt(res.RemainingStreams, 10))
	}
}

func (h *Handler) reconcileAdmissionUsage(adm *nativeAdmissionLease, usage *port.NormalizedUsage) {
	if h == nil || h.rateLimiter == nil || adm == nil || usage == nil {
		return
	}
	actual := max(int64(0), usage.InputTokens) +
		max(int64(0), usage.OutputTokens) +
		max(int64(0), usage.ReasoningTokens) +
		max(int64(0), usage.CacheWriteTokens)
	if usage.Accuracy == port.UsageAccuracyUnmeteredFlagged && actual == 0 {
		return
	}
	_ = h.rateLimiter.ReconcileTokens(context.Background(), adm.scope, adm.estTokens, actual, adm.limits)
}

func (h *Handler) resolveNativeTargetsWithPolicy(
	w http.ResponseWriter,
	reqID string,
	snap *snapshot.ConfigSnapshot,
	match snapshot.KeyMatchResult,
	eff policyengine.EffectivePolicy,
	reqRegion string,
	dialect string,
	upstreamPath string,
	modelSlug string,
	requiredCaps []string,
	estBaseIn int64,
	estMaxOut int64,
) ([]*resolver.RankedTarget, bool) {
	requireTested := match.Config != nil && match.Config.RequireTestedCaps
	candidates, rej := h.routeResolver.Resolve(resolver.ResolveRequest{
		RequestID:            reqID,
		Snapshot:             snap,
		OrgID:                match.Key.OrgID,
		ProjectID:            match.Key.ProjectID,
		EnvID:                match.Key.EnvID,
		TargetModel:          modelSlug,
		EffectivePolicy:      eff,
		RequestedDataRegion:  reqRegion,
		Dialect:              dialect,
		RequiredCapabilities: requiredCaps,
		EstimatedInputTokens: estBaseIn,
		MaxOutputTokens:      estMaxOut,
		RequireTestedCaps:    requireTested,
		Breaker:              h.breaker,
		NativePathAllowed: func(adp *snapshot.ProviderAdapterView) bool {
			return h.adapterAllowsNativePath(adp, upstreamPath)
		},
	})
	if rej != nil {
		writeCanonicalError(w, reqID, rej.HTTPStatus, rej.Code, rej.ErrorType, rej.Message, rej.Retryable)
		return nil, false
	}
	return candidates, true
}

func estimateNativeTokensFromJSON(raw []byte) (estTotal, baseIn, maxOut int64) {
	estTotal, baseIn, maxOut, _ = estimateNativeTokensAndCountFromJSON(raw)
	return estTotal, baseIn, maxOut
}

func estimateNativeTokensAndCountFromJSON(raw []byte) (estTotal, baseIn, maxOut, completionCount int64) {
	completionCount = 1
	rawLen := int64(len(raw))
	if rawLen == 0 {
		return 1, 1, 0, 1
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		b := max(int64(1), (rawLen+3)/4)
		return b, b, 0, 1
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		b := max(int64(1), (rawLen+3)/4)
		return b, b, 0, 1
	}

	var excessMediaBytes int64
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			break
		}
		key, ok := keyTok.(string)
		if !ok {
			break
		}
		switch key {
		case "max_completion_tokens", "max_tokens", "max_output_tokens":
			valTok, err := dec.Token()
			if err != nil {
				return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
			}
			if num, ok := valTok.(json.Number); ok {
				if parsed, ok := parseNativeJSONNumberCeiling(num); ok && parsed > maxOut {
					maxOut = parsed
				}
			} else if d, ok := valTok.(json.Delim); ok {
				ex, skipErr := skipNativeJSONCompositeCountMedia(dec, d, key, false)
				excessMediaBytes += ex
				if skipErr != nil {
					return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
				}
			}
		case "n", "candidateCount", "candidate_count":
			valTok, err := dec.Token()
			if err != nil {
				return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
			}
			if num, ok := valTok.(json.Number); ok {
				if parsed, ok := parseNativeJSONNumberCeiling(num); ok && parsed > completionCount {
					completionCount = parsed
				}
			} else if d, ok := valTok.(json.Delim); ok {
				ex, skipErr := skipNativeJSONCompositeCountMedia(dec, d, key, false)
				excessMediaBytes += ex
				if skipErr != nil {
					return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
				}
			}
		case "generationConfig", "generation_config":
			valTok, err := dec.Token()
			if err != nil {
				return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
			}
			d, ok := valTok.(json.Delim)
			if !ok {
				continue
			}
			if d != '{' {
				ex, skipErr := skipNativeJSONCompositeCountMedia(dec, d, key, false)
				excessMediaBytes += ex
				if skipErr != nil {
					return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
				}
				continue
			}
			for dec.More() {
				subKeyTok, err := dec.Token()
				if err != nil {
					return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
				}
				subKey, ok := subKeyTok.(string)
				if !ok {
					return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
				}
				if subKey == "maxOutputTokens" || subKey == "max_output_tokens" {
					subValTok, err := dec.Token()
					if err != nil {
						return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
					}
					if num, ok := subValTok.(json.Number); ok {
						if parsed, ok := parseNativeJSONNumberCeiling(num); ok && parsed > maxOut {
							maxOut = parsed
						}
					} else if sd, ok := subValTok.(json.Delim); ok {
						ex, skipErr := skipNativeJSONCompositeCountMedia(dec, sd, subKey, false)
						excessMediaBytes += ex
						if skipErr != nil {
							return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
						}
					}
					continue
				}
				if subKey == "candidateCount" || subKey == "candidate_count" {
					subValTok, err := dec.Token()
					if err != nil {
						return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
					}
					if num, ok := subValTok.(json.Number); ok {
						if parsed, ok := parseNativeJSONNumberCeiling(num); ok && parsed > completionCount {
							completionCount = parsed
						}
					} else if sd, ok := subValTok.(json.Delim); ok {
						ex, skipErr := skipNativeJSONCompositeCountMedia(dec, sd, subKey, false)
						excessMediaBytes += ex
						if skipErr != nil {
							return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
						}
					}
					continue
				}
				ex, skipErr := skipNativeNextJSONValueCountMedia(dec, key, subKey, false)
				excessMediaBytes += ex
				if skipErr != nil {
					return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
				}
			}
			if _, err := dec.Token(); err != nil {
				return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
			}
		default:
			ex, skipErr := skipNativeNextJSONValueCountMedia(dec, "", key, isNativeMultimodalRootKey(key))
			excessMediaBytes += ex
			if skipErr != nil {
				return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
			}
		}
	}
	return finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount)
}

func isNativeMultimodalRootKey(key string) bool {
	return key == "messages" || key == "input" || key == "contents"
}

func finalizeNativeEstTokens(rawLen, excessMediaBytes, maxOut, completionCount int64) (estTotal, baseIn, outCap, count int64) {
	if completionCount < 1 {
		completionCount = 1
	}
	effBytes := rawLen - excessMediaBytes
	if effBytes < 1 {
		effBytes = 1
	}
	baseIn = max(int64(1), (effBytes+3)/4)
	scaledOut := maxOut
	if scaledOut > 0 && completionCount > 1 {
		scaledOut = mulCapNativeInt64(scaledOut, completionCount, 1<<62)
	}
	if scaledOut > 0 {
		if scaledOut >= (1<<62) || baseIn > (1<<62)-scaledOut {
			return 1 << 62, baseIn, scaledOut, completionCount
		}
		return baseIn + scaledOut, baseIn, scaledOut, completionCount
	}
	if completionCount > 1 {
		scaledDefaultOut := mulCapNativeInt64(pricing.DefaultHoldOutputTokens, completionCount, 1<<62)
		if scaledDefaultOut >= (1<<62) || baseIn > (1<<62)-scaledDefaultOut {
			return 1 << 62, baseIn, 0, completionCount
		}
		return baseIn + scaledDefaultOut, baseIn, 0, completionCount
	}
	return baseIn, baseIn, 0, completionCount
}

func skipNativeNextJSONValueCountMedia(dec *json.Decoder, parentObjKey, key string, inMediaRoot bool) (int64, error) {
	tok, err := dec.Token()
	if err != nil {
		return 0, err
	}
	if d, ok := tok.(json.Delim); ok {
		return skipNativeJSONCompositeCountMedia(dec, d, key, inMediaRoot)
	}
	if s, ok := tok.(string); ok {
		// Scalar values directly under root keys (even non-array top-level fields) are never discounted.
		imm, _ := nativeMediaBlobExcessBytes(false, parentObjKey, key, s)
		return imm, nil
	}
	return 0, nil
}

func skipNativeJSONCompositeCountMedia(dec *json.Decoder, open json.Delim, parentKey string, inMediaRoot bool) (int64, error) {
	type frame struct {
		isObject            bool
		expectKey           bool
		curKey              string
		objName             string
		inMediaRoot         bool
		disallowedMedia     bool
		pendingBase64Excess int64
	}
	stack := []frame{{
		isObject:    open == '{',
		expectKey:   open == '{',
		curKey:      parentKey,
		objName:     parentKey,
		inMediaRoot: inMediaRoot,
	}}
	var excess int64
	for len(stack) > 0 {
		tok, err := dec.Token()
		if err != nil {
			for i := len(stack) - 1; i >= 0; i-- {
				f := stack[i]
				if f.pendingBase64Excess > 0 && !f.disallowedMedia {
					excess += f.pendingBase64Excess
				}
			}
			return excess, err
		}
		top := &stack[len(stack)-1]
		if d, ok := tok.(json.Delim); ok {
			nextObjName := top.objName
			if top.isObject {
				nextObjName = top.curKey
			}
			switch d {
			case '{':
				stack = append(stack, frame{isObject: true, expectKey: true, objName: nextObjName, inMediaRoot: top.inMediaRoot})
			case '[':
				stack = append(stack, frame{isObject: false, expectKey: false, curKey: top.curKey, objName: nextObjName, inMediaRoot: top.inMediaRoot})
			case '}', ']':
				if top.pendingBase64Excess > 0 && !top.disallowedMedia {
					excess += top.pendingBase64Excess
				}
				stack = stack[:len(stack)-1]
				if len(stack) > 0 && stack[len(stack)-1].isObject {
					stack[len(stack)-1].expectKey = true
				}
			}
			continue
		}
		if top.isObject && top.expectKey {
			if k, ok := tok.(string); ok {
				top.curKey = k
			}
			top.expectKey = false
			continue
		}
		if s, ok := tok.(string); ok {
			if top.isObject {
				switch strings.ToLower(strings.TrimSpace(top.curKey)) {
				case "mimetype", "mime_type", "media_type":
					if dm := strings.ToLower(strings.TrimSpace(s)); dm != "" && !isDiscountedNativeMediaMIME(dm) {
						top.disallowedMedia = true
					}
				case "type":
					if dt := strings.ToLower(strings.TrimSpace(s)); strings.Contains(dt, "video") {
						top.disallowedMedia = true
					}
				}
			}
			imm, pend := nativeMediaBlobExcessBytes(top.inMediaRoot, top.objName, top.curKey, s)
			excess += imm
			if pend > 0 {
				top.pendingBase64Excess += pend
			}
		}
		if top.isObject {
			top.expectKey = true
		}
	}
	return excess, nil
}

func isDiscountedNativeMediaMIME(mimeType string) bool {
	slash := strings.IndexByte(mimeType, '/')
	if slash <= 0 || slash == len(mimeType)-1 {
		return false
	}
	major := mimeType[:slash]
	switch {
	case major == "image" || major == "audio":
		return true
	case mimeType == "application/pdf" || mimeType == "application/octet-stream":
		return true
	default:
		return false
	}
}

func nativeMediaBlobExcessBytes(inMediaRoot bool, parentObjKey, key, s string) (immediate, pendingRawBase64 int64) {
	const cappedBlobBytes = 1024
	if !inMediaRoot || len(s) <= cappedBlobBytes {
		return 0, 0
	}
	k := strings.ToLower(strings.TrimSpace(key))
	pk := strings.ToLower(strings.TrimSpace(parentObjKey))
	switch k {
	case "url", "image_url", "file_url":
		if isNativeMediaURLParentKey(pk) && isStrictNativeMultimodalBase64DataURI(s) {
			return int64(len(s) - cappedBlobBytes), 0
		}
	case "file_data", "image_base64", "audio_base64", "b64_json":
		if isNativeMediaPayloadParentKey(pk) {
			if isStrictNativeMultimodalBase64DataURI(s) {
				return int64(len(s) - cappedBlobBytes), 0
			}
			if isStrictNativeBase64Payload(s) {
				return 0, int64(len(s) - cappedBlobBytes)
			}
		}
	case "data":
		if isNativeMediaContainerKey(pk) {
			if isStrictNativeMultimodalBase64DataURI(s) {
				return int64(len(s) - cappedBlobBytes), 0
			}
			if isStrictNativeBase64Payload(s) {
				return 0, int64(len(s) - cappedBlobBytes)
			}
		}
	}
	return 0, 0
}

func isNativeMediaURLParentKey(pk string) bool {
	switch pk {
	case "image_url", "image", "input_image", "file", "input_file", "document", "source", "input_audio", "audio", "content", "parts", "messages", "input", "contents":
		return true
	default:
		return false
	}
}

func isNativeMediaPayloadParentKey(pk string) bool {
	switch pk {
	case "file", "input_file", "document", "image", "image_url", "input_image", "audio", "input_audio", "source", "inlinedata", "inline_data", "content", "parts", "messages", "input", "contents":
		return true
	default:
		return false
	}
}

func isNativeMediaContainerKey(pk string) bool {
	switch pk {
	case "source", "input_audio", "inlinedata", "inline_data", "image", "audio", "file", "input_file", "document", "image_url":
		return true
	default:
		return false
	}
}

func isStrictNativeMultimodalBase64DataURI(s string) bool {
	if len(s) < 12 || !strings.EqualFold(s[:5], "data:") {
		return false
	}
	headLimit := min(len(s), 128)
	headLower := strings.ToLower(s[5:headLimit])
	semiIdx := strings.Index(headLower, ";base64,")
	if semiIdx <= 0 {
		return false
	}
	mimeType := strings.SplitN(headLower[:semiIdx], ";", 2)[0]
	slash := strings.IndexByte(mimeType, '/')
	if slash <= 0 || slash == len(mimeType)-1 {
		return false
	}
	major := mimeType[:slash]
	subtype := mimeType[slash+1:]
	switch {
	case major == "image" || major == "audio":
	case mimeType == "application/pdf" || mimeType == "application/octet-stream":
	default:
		return false
	}
	for i := 0; i < len(subtype); i++ {
		c := subtype[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-') {
			return false
		}
	}
	payloadStart := 5 + semiIdx + len(";base64,")
	return isStrictNativeBase64Payload(s[payloadStart:])
}

func isStrictNativeBase64Payload(s string) bool {
	if len(s) == 0 || len(s)%4 != 0 {
		return false
	}
	strictEnc := base64.StdEncoding.Strict()
	var src [4096]byte
	var dst [3072]byte
	n := len(s)
	for off := 0; off < n; off += len(src) {
		end := min(off+len(src), n)
		chunkLen := end - off
		if end < n {
			// Ensure padding '=' never appears outside the very end of the full string.
			for i := off; i < end; i++ {
				if s[i] == '=' {
					return false
				}
			}
		}
		copy(src[:chunkLen], s[off:end])
		if _, err := strictEnc.Decode(dst[:], src[:chunkLen]); err != nil {
			return false
		}
	}
	return true
}

func parseNativeJSONNumberCeiling(num json.Number) (int64, bool) {
	if iv, err := num.Int64(); err == nil {
		if iv <= 0 {
			return 0, false
		}
		if iv >= (1 << 62) {
			return 1 << 62, true
		}
		return iv, true
	}
	fv, err := strconv.ParseFloat(num.String(), 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) && !math.IsNaN(fv) && fv > 0 {
			return 1 << 62, true
		}
		return 0, false
	}
	if math.IsNaN(fv) || fv <= 0 {
		return 0, false
	}
	if math.IsInf(fv, 1) || fv >= float64(1<<62) {
		return 1 << 62, true
	}
	return int64(math.Ceil(fv)), true
}

