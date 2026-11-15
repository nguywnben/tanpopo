// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package native

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/ratelimit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
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
	Snapshots           *snapshot.Manager
	Broker              *broker.BlindHostCredentialBroker
	Audit               storageport.AuditSink
	Idempotency         *idempotency.Guard
	PolicyEvaluator     *policyengine.Evaluator
	RateLimiter         ratelimit.RateLimiterPort
	AcquireRuntime      RuntimeAcquirer
	NowMs               func() int64
	MaxRequestBodyBytes int64
	FirstByteTimeout    time.Duration
	HeartbeatInterval   time.Duration
	OnUsage             func(UsageRecord)
}

// Handler serves /native/{dialect}/{upstreamPath} passthrough requests.
type Handler struct {
	snapshots         *snapshot.Manager
	broker            *broker.BlindHostCredentialBroker
	audit             storageport.AuditSink
	idempotency       *idempotency.Guard
	policyEval        *policyengine.Evaluator
	rateLimiter       ratelimit.RateLimiterPort
	acquireRuntime    RuntimeAcquirer
	nowMs             func() int64
	maxReqBodyBytes   int64
	firstByteTimeout  time.Duration
	heartbeatInterval time.Duration
	onUsage           func(UsageRecord)
	regexMu           sync.RWMutex
	regexCache        map[string]*regexp.Regexp
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
	return &Handler{
		snapshots:         cfg.Snapshots,
		broker:            cfg.Broker,
		audit:             cfg.Audit,
		idempotency:       cfg.Idempotency,
		policyEval:        policyEval,
		rateLimiter:       rateLimiter,
		acquireRuntime:    cfg.AcquireRuntime,
		nowMs:             nowFn,
		maxReqBodyBytes:   maxBody,
		firstByteTimeout:  firstByte,
		heartbeatInterval: hb,
		onUsage:           cfg.OnUsage,
		regexCache:        make(map[string]*regexp.Regexp),
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

	// Step 3: Read bounded body without altering any bytes destined for upstream.
	rawBody, ok := h.readBoundedBody(w, r, reqID)
	if !ok {
		return
	}
	defer clear(rawBody)

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
	estTokens := estimateNativeTokensFromJSON(rawBody)
	reqRegion, regionOK := resolveNativeRequestedDataRegion(r)
	if !regionOK {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Conflicting X-Tanpopo-Data-Region header values.", false)
		return
	}
	if !h.evaluateNativePreAdmissionPolicy(w, reqID, match, eff, rawHints.Model, reqRegion, rawBody, estTokens) {
		return
	}

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

	// Step 4: Resolve policy-compliant target deployment, verify Same-Dialect Invariant (CT-13) and Adapter Allowlist (NEG-2).
	target, ok := h.resolveNativeTargetWithPolicy(w, reqID, snap, match, eff, reqRegion, dialect, upstreamPath, rawHints.Model)
	if !ok {
		return
	}

	// Step 5: Acquire runtime, execute bounded InspectNativeRequest without mutating rawBody.
	rt, releaseCurr, rtErr := h.acquireRuntime(streamCtx, target)
	if rtErr != nil {
		if errors.Is(rtErr, context.Canceled) && r.Context().Err() == nil && streamCtx.Err() != nil {
			writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
			return
		}
		h.writeRuntimeFaultError(w, reqID, rtErr)
		return
	}
	defer func() {
		if releaseCurr != nil {
			releaseCurr()
		}
	}()

	safeHeaders := buildSanitizedNativeHeaders(r.Header, target.Adapter.AllowedNativeHeaders, rawKey, reqID, tp)

	narrowPolicy := buildHostNarrowPolicyFromTarget(target)
	wasmCtx := wasm.WithEgressScope(streamCtx, wasm.EgressScope{
		OrgID:        match.Key.OrgID,
		ConnectionID: target.Connection.ID,
		ActorKind:    "virtual_key",
		ActorID:      match.Key.ID,
		EntryPoint:   wasm.DefaultAuditEntryPoint,
		RequestID:    reqID,
		NarrowPolicy: &narrowPolicy,
	})

	insp, err := InspectNativeRequest(wasmCtx, rt, reqID, dialect, upstreamPath, safeHeaders, rawBody, rawHints)
	if err != nil {
		if errors.Is(err, context.Canceled) && r.Context().Err() == nil && streamCtx.Err() != nil {
			writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
			return
		}
		h.writeRuntimeFaultError(w, reqID, err)
		return
	}
	if insp.TargetModel != "" && !modelAllowlistPermits(match.Key.AllowedModels, insp.TargetModel) {
		writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key model allowlist denies %q.", insp.TargetModel), false)
		return
	}
	if insp.TargetModel != "" && insp.TargetModel != rawHints.Model {
		if !h.evaluateNativePreAdmissionPolicy(w, reqID, match, eff, insp.TargetModel, reqRegion, rawBody, estTokens) {
			return
		}
		newTarget, ok := h.resolveNativeTargetWithPolicy(w, reqID, snap, match, eff, reqRegion, dialect, upstreamPath, insp.TargetModel)
		if !ok {
			return
		}
		if newTarget.Deployment.ID != target.Deployment.ID || newTarget.Adapter.ID != target.Adapter.ID || newTarget.Connection.ID != target.Connection.ID {
			adapterChanged := newTarget.Adapter.ID != target.Adapter.ID
			if releaseCurr != nil {
				releaseCurr()
				releaseCurr = nil
			}
			target = newTarget
			safeHeaders = buildSanitizedNativeHeaders(r.Header, target.Adapter.AllowedNativeHeaders, rawKey, reqID, tp)
			narrowPolicy = buildHostNarrowPolicyFromTarget(target)
			var newErr error
			rt, releaseCurr, newErr = h.acquireRuntime(streamCtx, target)
			if newErr != nil {
				if errors.Is(newErr, context.Canceled) && r.Context().Err() == nil && streamCtx.Err() != nil {
					writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
					return
				}
				h.writeRuntimeFaultError(w, reqID, newErr)
				return
			}
			if adapterChanged {
				newWasmCtx := wasm.WithEgressScope(streamCtx, wasm.EgressScope{
					OrgID:        match.Key.OrgID,
					ConnectionID: target.Connection.ID,
					ActorKind:    "virtual_key",
					ActorID:      match.Key.ID,
					EntryPoint:   wasm.DefaultAuditEntryPoint,
					RequestID:    reqID,
					NarrowPolicy: &narrowPolicy,
				})
				reRawHints := rawHints
				reRawHints.Model = insp.TargetModel
				reInsp, reErr := InspectNativeRequest(newWasmCtx, rt, reqID, dialect, upstreamPath, safeHeaders, rawBody, reRawHints)
				if reErr != nil {
					if errors.Is(reErr, context.Canceled) && r.Context().Err() == nil && streamCtx.Err() != nil {
						writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before dispatch.", false)
						return
					}
					h.writeRuntimeFaultError(w, reqID, reErr)
					return
				}
				if reInsp.TargetModel != "" && reInsp.TargetModel != insp.TargetModel {
					writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Target adapter redirected model after deployment re-resolution.", false)
					return
				}
				if reInsp.TargetModel == "" {
					reInsp.TargetModel = insp.TargetModel
				}
				if !modelAllowlistPermits(match.Key.AllowedModels, reInsp.TargetModel) {
					writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key model allowlist denies %q.", reInsp.TargetModel), false)
					return
				}
				insp = reInsp
			}
		}
	}

	requireTested := match.Config != nil && match.Config.RequireTestedCaps
	for _, reqCap := range insp.Hints.RequiredCapabilities {
		capName := strings.TrimSpace(reqCap)
		if capName == "" {
			continue
		}
		if !snapshot.DeploymentSupportsCapability(target, requireTested, capName) {
			effModel := firstNonEmpty(insp.TargetModel, rawHints.Model)
			writeCanonicalError(w, reqID, http.StatusBadRequest, "capability_not_supported", "invalid_request_error", fmt.Sprintf("Target deployment for model %q does not support required capability %q.", effModel, capName), false)
			return
		}
	}

	if insp.Stream && !rawHints.Stream {
		if !h.acquireDeferredStreamSlot(w, r, reqID, admLease) {
			return
		}
	}

	// Step 6: Build sanitized outbound request preserving exact rawBody bytes.
	parsedBase, perr := url.Parse(strings.TrimSpace(target.Connection.EndpointBaseURL))
	if perr != nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Target connection endpoint base URL is malformed.", false)
		return
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
		h.writeBrokerOrEgressError(w, reqID, cerr)
		return
	}

	var credInjections []broker.CredentialInjection
	if !strings.EqualFold(strings.TrimSpace(target.Connection.AuthMode), "NONE") {
		if len(target.Connection.SecretBindings) == 0 {
			writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Authenticated provider connection has no bound secret slots.", false)
			return
		}
		if len(insp.Hints.CredentialInjections) == 0 {
			writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Provider adapter omitted required credential injection instructions for authenticated connection.", false)
			return
		}
		var cerr error
		credInjections, cerr = mapCredentialInjections(insp.Hints.CredentialInjections, target.Connection.SecretBindings)
		if cerr != nil {
			writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Provider adapter requested an invalid or unauthorized credential injection.", false)
			return
		}
	}
	stripInjectedCredentialHeaders(safeHeaders, credInjections)
	timeoutOverride := h.firstByteTimeout
	if target.Deployment.FirstByteTimeoutMs > 0 {
		timeoutOverride = time.Duration(target.Deployment.FirstByteTimeoutMs) * time.Millisecond
	}

	outboundBody := append([]byte(nil), rawBody...)
	defer clear(outboundBody)

	egressReq := broker.EgressRequest{
		OrgID:                match.Key.OrgID,
		ConnectionID:         target.Connection.ID,
		Method:               r.Method,
		TargetURL:            targetURL,
		SafeHeaders:          safeHeaders,
		BodyBytes:            outboundBody,
		CredentialInjections: credInjections,
		TimeoutOverride:      timeoutOverride,
		ActorKind:            "virtual_key",
		ActorID:              match.Key.ID,
		EntryPoint:           wasm.DefaultAuditEntryPoint,
		RequestID:            reqID,
	}
	constraint := broker.HostEgressConstraint{NarrowPolicy: &narrowPolicy}

	if insp.Stream {
		h.executeStreamingNative(w, r.Context(), streamCtx, reqID, dialect, insp.TargetModel, match.Key, rt, egressReq, constraint, target, rawBody, admLease)
		return
	}
	h.executeSyncNative(w, r.Context(), streamCtx, reqID, dialect, insp.TargetModel, match.Key, rt, egressReq, constraint, rawBody, idemClaim, admLease)
}

func (h *Handler) executeSyncNative(
	w http.ResponseWriter,
	clientCtx context.Context,
	ctx context.Context,
	reqID string,
	dialect string,
	model string,
	key *snapshot.VirtualKeyView,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	rawReqBody []byte,
	idemClaim *idempotency.Claim,
	admLease *nativeAdmissionLease,
) {
	upResp, err := h.broker.ExecuteWithHostConstraint(ctx, egressReq, constraint)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && ctx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before response dispatch.", false)
			}
			return
		}
		h.writeBrokerOrEgressError(w, reqID, err)
		return
	}

	sanitizedHdrs := flattenHTTPHeaders(upResp.SanitizedHeaders)
	if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
		h.writeNormalizedUpstreamError(w, ctx, reqID, rt, upResp.StatusCode, sanitizedHdrs, upResp.Body)
		return
	}

	usage := ExtractSyncUsage(ctx, rt, reqID, rawReqBody, upResp.Body)
	h.reconcileAdmissionUsage(admLease, &usage)
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
}

func (h *Handler) executeStreamingNative(
	w http.ResponseWriter,
	clientCtx context.Context,
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
) {
	upCtx, cancelUp := context.WithCancel(streamCtx)
	defer cancelUp()

	upStream, err := h.broker.ExecuteStreamWithHostConstraint(upCtx, egressReq, constraint)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && streamCtx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before stream dispatch.", false)
			}
			return
		}
		h.writeBrokerOrEgressError(w, reqID, err)
		return
	}
	defer func() { _ = upStream.Body.Close() }()

	if upStream.StatusCode < 200 || upStream.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(upStream.Body, 1<<20))
		h.writeNormalizedUpstreamError(w, streamCtx, reqID, rt, upStream.StatusCode, flattenHTTPHeaders(upStream.SanitizedHeaders), errBody)
		return
	}

	var idleChunkTimeout time.Duration
	if target != nil && target.Deployment != nil && target.Deployment.IdleChunkTimeoutMs > 0 {
		idleChunkTimeout = time.Duration(target.Deployment.IdleChunkTimeoutMs) * time.Millisecond
	}

	acc := NewSSEUsageAccumulator(rt, reqID, rawReqBody)
	pumpRes, pumpErr := pumpNativeSSE(streamCtx, w, reqID, upStream.Body, cancelUp, h.heartbeatInterval, idleChunkTimeout, acc)
	if pumpErr != nil && !pumpRes.streamStarted {
		if errors.Is(pumpErr, sse.ErrLineTooLarge) {
			writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream SSE stream exceeded maximum line byte limit.", false)
			return
		}
		if errors.Is(pumpErr, context.DeadlineExceeded) || errors.Is(pumpErr, sse.ErrIdleChunkTimeout) {
			writeCanonicalError(w, reqID, http.StatusGatewayTimeout, "upstream_timeout", "upstream_error", "Upstream SSE stream timed out before emitting first frame.", true)
			return
		}
		if errors.Is(pumpErr, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && streamCtx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before stream dispatch.", false)
			}
			return
		}
		writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream native stream terminated before emitting any frame.", false)
		return
	}

	usageCtx := context.WithoutCancel(streamCtx)
	finalUsage := acc.Finalize(usageCtx)
	h.reconcileAdmissionUsage(admLease, &finalUsage)
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
}

type nativeReadMsg struct {
	data  []byte
	isEOF bool
	err   error
}

type nativePumpRes struct {
	streamStarted      bool
	framesEmitted      int
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
				if acc != nil {
					acc.Feed(ctx, msg.data)
				}
				if !res.streamStarted {
					writeNativeSSEHeaders(w)
					res.streamStarted = true
					startHeartbeat()
				}
				if _, werr := w.Write(msg.data); werr != nil {
					res.clientDisconnected = true
					abortUpstream()
					return res, werr
				}
				res.framesEmitted++
				if ferr := rc.Flush(); ferr != nil {
					res.clientDisconnected = true
					abortUpstream()
					return res, ferr
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

func adapterMatchesDialect(target *snapshot.ResolvedTarget, dialect string) bool {
	if target == nil || target.Deployment == nil || target.Adapter == nil {
		return false
	}
	depDialect := strings.ToLower(strings.TrimSpace(target.Deployment.NativeDialect))
	if depDialect != "" {
		return depDialect == dialect
	}
	adpPrefix := strings.Trim(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(target.Adapter.NativePrefix)), "/native/"), "/")
	if adpPrefix != "" {
		return adpPrefix == dialect
	}
	if len(target.Adapter.SupportedDialects) == 1 {
		return strings.EqualFold(strings.TrimSpace(target.Adapter.SupportedDialects[0]), dialect)
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

func (h *Handler) writeNormalizedUpstreamError(w http.ResponseWriter, ctx context.Context, reqID string, rt port.ProviderRuntime, upstreamStatus int, headers map[string]string, body []byte) {
	normErr, nerr := rt.NormalizeError(ctx, port.NormalizeErrorInput{
		RequestID:        reqID,
		HTTPStatus:       int32(upstreamStatus),
		SanitizedHeaders: headers,
		SanitizedBody:    body,
	})
	if nerr != nil {
		writeCanonicalError(w, reqID, mapUpstreamHTTPStatus(upstreamStatus), "upstream_error", "upstream_error", "Upstream provider request failed.", upstreamStatus == http.StatusTooManyRequests || upstreamStatus >= 500)
		return
	}
	code := sanitizeErrorToken(normErr.CanonicalCode, "upstream_error")
	errType := sanitizeErrorToken(normErr.ErrorType, "upstream_error")
	msg := sanitizePublicErrorMessage(normErr.SanitizedPublicMessage, 512)
	if msg == "" {
		msg = fmt.Sprintf("Upstream provider returned HTTP %d.", upstreamStatus)
	}
	writeCanonicalError(w, reqID, mapUpstreamHTTPStatus(upstreamStatus), code, errType, msg, normErr.RetryablePreStream)
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

func (h *Handler) resolveNativeTargetWithPolicy(
	w http.ResponseWriter,
	reqID string,
	snap *snapshot.ConfigSnapshot,
	match snapshot.KeyMatchResult,
	eff policyengine.EffectivePolicy,
	reqRegion string,
	dialect string,
	upstreamPath string,
	modelSlug string,
) (*snapshot.ResolvedTarget, bool) {
	candidates := snap.ListCandidateTargets(match.Key.OrgID, modelSlug)
	var (
		sawQuarantined     bool
		sawDialectMismatch bool
		sawPathDenied      bool
		sawModelDenied     bool
		modelDenial        policyengine.EvalDecision
		sawTrustDenied     bool
		trustDenial        policyengine.EvalDecision
		sawRegionDenied    bool
		regionDenial       policyengine.EvalDecision
	)
	for _, cand := range candidates {
		if cand == nil || cand.Deployment == nil || cand.Deployment.State != "ACTIVE_HEALTHY" {
			continue
		}
		if cand.Connection == nil || cand.Connection.State != snapshot.ConnectionStateActive || !cand.Connection.VisibleToProject(match.Key.OrgID, match.Key.ProjectID) {
			continue
		}
		if cand.Adapter == nil {
			continue
		}
		dec := policyengine.EvaluateEffective(eff, policyengine.EvalRequest{
			OrgID:                     match.Key.OrgID,
			ProjectID:                 match.Key.ProjectID,
			EnvID:                     match.Key.EnvID,
			ModelSlug:                 modelSlug,
			AdapterSlug:               cand.Adapter.AdapterSlug,
			DataRegion:                cand.Deployment.DataRegion,
			RequestedDataRegion:       reqRegion,
			ExtensionTrustTier:        cand.Connection.DerivedPublicTrust,
			ExtensionEnterpriseSigned: cand.Connection.DerivedEnterpriseSigned,
			CapabilityAssurance:       cand.Deployment.AssuranceLevel,
			TargetEvaluation:          true,
		})
		if !dec.Allowed {
			switch dec.DenialCode {
			case policyengine.DenialCodeModelDenied:
				if !sawModelDenied {
					sawModelDenied = true
					modelDenial = dec
				}
			case policyengine.DenialCodeTrustDenied:
				if !sawTrustDenied {
					sawTrustDenied = true
					trustDenial = dec
				}
			case policyengine.DenialCodeRegionDenied:
				if !sawRegionDenied {
					sawRegionDenied = true
					regionDenial = dec
				}
			}
			continue
		}
		if strings.EqualFold(cand.Adapter.ExtensionState, installer.StateQuarantined) {
			sawQuarantined = true
			continue
		}
		if !adapterMatchesDialect(cand, dialect) {
			sawDialectMismatch = true
			continue
		}
		if !h.adapterAllowsNativePath(cand.Adapter, upstreamPath) {
			sawPathDenied = true
			continue
		}
		return cand, true
	}

	if sawQuarantined {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "extension_quarantined", "extension_error", "The backing provider adapter extension is quarantined.", false)
		return nil, false
	}
	if sawDialectMismatch {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "dialect_mismatch", "invalid_request_error", fmt.Sprintf("Target deployment for model %q uses a different native dialect than %q.", modelSlug, dialect), false)
		return nil, false
	}
	if sawPathDenied {
		writeCanonicalError(w, reqID, http.StatusForbidden, "native_path_not_allowed", "permission_error", fmt.Sprintf("Native upstream path %q is denied by Default-Deny adapter policy.", upstreamPath), false)
		return nil, false
	}
	if sawModelDenied {
		writeCanonicalError(w, reqID, http.StatusForbidden, policyengine.DenialCodeModelDenied, "permission_error", modelDenial.DenialReason, false)
		return nil, false
	}
	if sawTrustDenied {
		writeCanonicalError(w, reqID, trustDenial.HTTPStatus, policyengine.DenialCodeTrustDenied, trustDenial.ErrorType, trustDenial.DenialReason, false)
		return nil, false
	}
	if sawRegionDenied {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "no_compliant_deployment_available", "residency_unavailable_error", regionDenial.DenialReason+" ("+policyengine.DenialCodeRegionDenied+")", true)
		return nil, false
	}
	writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "no_compliant_deployment_available", "residency_unavailable_error", fmt.Sprintf("No compliant active deployment is available for model %q.", modelSlug), true)
	return nil, false
}

func estimateNativeTokensFromJSON(raw []byte) int64 {
	base := max(int64(1), int64(len(raw)+3)/4)
	if len(raw) == 0 {
		return base
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return base
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return base
	}

	var maxOut int64
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
				return finalizeNativeEstTokens(base, maxOut)
			}
			if num, ok := valTok.(json.Number); ok {
				if parsed, ok := parseNativeJSONNumberCeiling(num); ok && parsed > maxOut {
					maxOut = parsed
				}
			} else if d, ok := valTok.(json.Delim); ok {
				if skipNativeJSONComposite(dec, d) != nil {
					return finalizeNativeEstTokens(base, maxOut)
				}
			}
		case "generationConfig", "generation_config":
			valTok, err := dec.Token()
			if err != nil {
				return finalizeNativeEstTokens(base, maxOut)
			}
			d, ok := valTok.(json.Delim)
			if !ok {
				continue
			}
			if d != '{' {
				if skipNativeJSONComposite(dec, d) != nil {
					return finalizeNativeEstTokens(base, maxOut)
				}
				continue
			}
			for dec.More() {
				subKeyTok, err := dec.Token()
				if err != nil {
					return finalizeNativeEstTokens(base, maxOut)
				}
				subKey, ok := subKeyTok.(string)
				if !ok {
					return finalizeNativeEstTokens(base, maxOut)
				}
				if subKey == "maxOutputTokens" || subKey == "max_output_tokens" {
					subValTok, err := dec.Token()
					if err != nil {
						return finalizeNativeEstTokens(base, maxOut)
					}
					if num, ok := subValTok.(json.Number); ok {
						if parsed, ok := parseNativeJSONNumberCeiling(num); ok && parsed > maxOut {
							maxOut = parsed
						}
					} else if sd, ok := subValTok.(json.Delim); ok {
						if skipNativeJSONComposite(dec, sd) != nil {
							return finalizeNativeEstTokens(base, maxOut)
						}
					}
					continue
				}
				if skipNativeNextJSONValue(dec) != nil {
					return finalizeNativeEstTokens(base, maxOut)
				}
			}
			if _, err := dec.Token(); err != nil {
				return finalizeNativeEstTokens(base, maxOut)
			}
		default:
			if skipNativeNextJSONValue(dec) != nil {
				return finalizeNativeEstTokens(base, maxOut)
			}
		}
	}
	return finalizeNativeEstTokens(base, maxOut)
}

func finalizeNativeEstTokens(base, maxOut int64) int64 {
	if maxOut > 0 {
		if maxOut >= (1<<62) || base > (1<<62)-maxOut {
			return 1 << 62
		}
		return base + maxOut
	}
	return base
}

func skipNativeNextJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok {
		return skipNativeJSONComposite(dec, d)
	}
	return nil
}

func skipNativeJSONComposite(dec *json.Decoder, open json.Delim) error {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
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

