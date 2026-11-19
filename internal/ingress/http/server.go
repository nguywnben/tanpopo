// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package http implements Tanpopo's Data Plane HTTP server under internal/ingress
// (ADR-0001 / TP-IMP-0015 / SPEC-EXT-06).
//
// Invariants:
//   - Every request receives a validated or freshly minted x-tanpopo-request-id
//     (req_<26_crockford_base32>) and W3C traceparent header (CT-06).
//   - WebSocket / WebRTC Upgrade handshakes fail immediately with 400
//     unsupported_protocol_upgrade (TP-FR-0106).
//   - Virtual Key authentication routes strictly through snapshot.Manager APIs
//     enforcing CheckServingReady() (DEC-019), rejecting admin session cookies
//     on /v1/* with 401 invalid_credential_plane (AUTH-07).
//   - Outbound provider egress runs exclusively through BlindHostCredentialBroker
//     wired with explicit PolicyResolver (DEC-020) and Host narrowing constraints.
//   - All errors emit CanonicalErrorEnvelope with x-tanpopo-request-id and never
//     leak raw secrets or unnormalized upstream payloads.
package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
	"github.com/nguywnben/tanpopo/internal/ingress/native"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/ratelimit"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/telemetry/otel"
)

const (
	// DefaultMaxRequestBodyBytes enforces the 32 MiB Data Plane ingress cap (SPEC-EXT-06).
	DefaultMaxRequestBodyBytes int64 = 32 << 20
	// DefaultFirstByteTimeout bounds upstream response-header waiting when deployment omits it.
	DefaultFirstByteTimeout = 30 * time.Second
	// MaxTraceHeaderLength bounds client-supplied correlation headers to match audit_events.request_id VARCHAR(40).
	MaxTraceHeaderLength = 40
)

var (
	// ErrInvalidServerConfig indicates missing required Data Plane server dependencies.
	ErrInvalidServerConfig = errors.New("ingress: invalid server configuration")
)

// ServerConfig configures the Data Plane HTTP server fail-closed.
type ServerConfig struct {
	Snapshots                *snapshot.Manager
	Broker                   *broker.BlindHostCredentialBroker
	FaultGuard               *wasm.FaultGuard
	KillSwitch               *killswitch.Registry
	PluginsDir               string
	Audit                    storageport.AuditSink
	NowMs                    func() int64
	MaxRequestBodyBytes      int64
	WasmPoolSize             int
	DefaultFirstByteTimeout  time.Duration
	SSEHeartbeatInterval     time.Duration
	IdempotencyDB            *sql.DB
	IdempotencyTTL           time.Duration
	RemoteMediaPolicy        blob.RemoteMediaPolicy
	DisableResponses         bool
	DisableEmbeddings        bool
	DisableModels            bool
	DisableNativePassthrough bool
	PolicyEvaluator          *policyengine.Evaluator
	RateLimiter              ratelimit.RateLimiterPort
	Breaker                  *breaker.CircuitBreaker
	RouteResolver            *resolver.Resolver
	BudgetLedger             *ledger.Service
	DefaultHoldOutputTokens  int64
	UsageObserver            func(UsageRecord)
	NativeUsageObserver      func(native.UsageRecord)
	Telemetry                *otel.Pipeline
}

// UsageRecord captures normalized usage telemetry from Data Plane inference endpoints.
type UsageRecord struct {
	RequestID string
	Route     string
	Model     string
	Stream    bool
	Usage     *port.NormalizedUsage
}

type runtimeCacheKey struct {
	InstallationID  string
	ConnectionID    string
	ActiveVersionID string
	BundleSHA256    string
	PermissionsSHA  [32]byte
	ScopeKey        string
}

type cachedAdapterEntry struct {
	installationID string
	rt             port.ProviderRuntime
	wasmRT         *wasm.Runtime
	tier           string
	perms          installer.PermissionSet
	compiledAt     int64
	inFlight       atomic.Int64
	retired        atomic.Bool
	closeOnce      sync.Once
}

func (e *cachedAdapterEntry) retain() {
	if e != nil {
		e.inFlight.Add(1)
	}
}

func (e *cachedAdapterEntry) closeRuntime(ctx context.Context) error {
	if e == nil || e.wasmRT == nil {
		return nil
	}
	var err error
	e.closeOnce.Do(func() {
		err = e.wasmRT.Close(ctx)
	})
	return err
}

func (e *cachedAdapterEntry) release() {
	if e == nil {
		return
	}
	if e.inFlight.Add(-1) == 0 && e.retired.Load() {
		_ = e.closeRuntime(context.Background())
	}
}

// Server serves Data Plane HTTP inference routes (/v1/chat/completions, /v1/responses, /v1/embeddings, /v1/models, /native/*).
type Server struct {
	snapshots               *snapshot.Manager
	broker                  *broker.BlindHostCredentialBroker
	faultGuard              *wasm.FaultGuard
	killSwitch              *killswitch.Registry
	pluginsDir              string
	audit                   storageport.AuditSink
	nowMs                   func() int64
	maxReqBodyBytes         int64
	wasmPoolSize            int
	firstByteTimeout        time.Duration
	heartbeatInterval       time.Duration
	idempotency             *idempotency.Guard
	remoteMediaPolicy       blob.RemoteMediaPolicy
	policyEval              *policyengine.Evaluator
	rateLimiter             ratelimit.RateLimiterPort
	breaker                 *breaker.CircuitBreaker
	routeResolver           *resolver.Resolver
	budgetLedger            *ledger.Service
	defaultHoldOutputTokens int64
	usageObserver           func(UsageRecord)
	nativeHandler           *native.Handler
	telemetry               *otel.Pipeline

	wasmCache wazero.CompilationCache

	rtMu       sync.Mutex
	rtLoadWG   sync.WaitGroup
	closed     atomic.Bool
	runtimes   map[runtimeCacheKey]*cachedAdapterEntry
	mux        *http.ServeMux
	rootRouter http.Handler
}

// NewServer constructs a fail-closed Data Plane HTTP Server.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Snapshots == nil {
		return nil, fmt.Errorf("%w: non-nil snapshot.Manager is required", ErrInvalidServerConfig)
	}
	if cfg.Broker == nil {
		return nil, fmt.Errorf("%w: non-nil broker.BlindHostCredentialBroker is required", ErrInvalidServerConfig)
	}
	if cfg.MaxRequestBodyBytes < 0 {
		return nil, fmt.Errorf("%w: max request body bytes cannot be negative", ErrInvalidServerConfig)
	}

	var resolvedPluginsDir string
	if strings.TrimSpace(cfg.PluginsDir) != "" {
		absDir, err := filepath.Abs(strings.TrimSpace(cfg.PluginsDir))
		if err != nil {
			return nil, fmt.Errorf("%w: resolve plugins dir: %v", ErrInvalidServerConfig, err)
		}
		if evalDir, err := filepath.EvalSymlinks(absDir); err == nil {
			resolvedPluginsDir = evalDir
		} else {
			resolvedPluginsDir = absDir
		}
	}

	maxBody := cfg.MaxRequestBodyBytes
	if maxBody == 0 {
		maxBody = DefaultMaxRequestBodyBytes
	}
	poolSize := cfg.WasmPoolSize
	if poolSize <= 0 {
		poolSize = wasm.DefaultPoolSize
	}
	firstByteTimeout := cfg.DefaultFirstByteTimeout
	if firstByteTimeout <= 0 {
		firstByteTimeout = DefaultFirstByteTimeout
	}
	hbInterval := cfg.SSEHeartbeatInterval
	if hbInterval == 0 {
		hbInterval = sse.DefaultHeartbeatInterval
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	faultGuard := cfg.FaultGuard
	if faultGuard == nil {
		var onTrap func(installationID string, kind wasm.FaultKind)
		if cfg.Telemetry != nil && cfg.Telemetry.Metrics != nil {
			tel := cfg.Telemetry
			onTrap = func(installationID string, kind wasm.FaultKind) {
				tel.Metrics.RecordSandboxTrap(installationID, mapFaultKindToTrapReason(kind))
			}
		}
		faultGuard = wasm.NewFaultGuard(wasm.FaultGuardConfig{
			AuditSink:  cfg.Audit,
			EntryPoint: wasm.DefaultAuditEntryPoint,
			OnTrap:     onTrap,
		})
	}

	idemGuard := idempotency.NewGuard(idempotency.Config{
		DB:    cfg.IdempotencyDB,
		TTL:   cfg.IdempotencyTTL,
		NowMs: nowFn,
	})

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
		var onTransition func(deploymentID, prevState, newState string)
		if cfg.Telemetry != nil && cfg.Telemetry.Metrics != nil {
			tel := cfg.Telemetry
			snaps := cfg.Snapshots
			onTransition = func(deploymentID, _, newState string) {
				adapterID, region := "unknown", "UNSPECIFIED"
				if snaps != nil {
					if snap := snaps.Current(); snap != nil {
						if a, reg, ok := snap.LookupDeploymentTelemetryLabels(deploymentID); ok {
							adapterID, region = a, reg
						}
					}
				}
				tel.Metrics.SetDeploymentCircuitState(adapterID, region, newState)
			}
		}
		cb = breaker.New(breaker.Config{
			Now:          func() time.Time { return time.UnixMilli(nowFn()).UTC() },
			OnTransition: onTransition,
		})
	}
	routeRes := cfg.RouteResolver
	if routeRes == nil {
		routeRes = resolver.New()
	}

	s := &Server{
		snapshots:               cfg.Snapshots,
		broker:                  cfg.Broker,
		faultGuard:              faultGuard,
		killSwitch:              cfg.KillSwitch,
		pluginsDir:              resolvedPluginsDir,
		audit:                   cfg.Audit,
		nowMs:                   nowFn,
		maxReqBodyBytes:         maxBody,
		wasmPoolSize:            poolSize,
		firstByteTimeout:        firstByteTimeout,
		heartbeatInterval:       hbInterval,
		idempotency:             idemGuard,
		remoteMediaPolicy:       cfg.RemoteMediaPolicy,
		policyEval:              policyEval,
		rateLimiter:             rateLimiter,
		breaker:                 cb,
		routeResolver:           routeRes,
		budgetLedger:            cfg.BudgetLedger,
		defaultHoldOutputTokens: cfg.DefaultHoldOutputTokens,
		usageObserver:           cfg.UsageObserver,
		telemetry:               cfg.Telemetry,
		wasmCache:               wazero.NewCompilationCache(),
		runtimes:                make(map[runtimeCacheKey]*cachedAdapterEntry),
		mux:                     http.NewServeMux(),
	}
	if cfg.Telemetry != nil && cfg.Telemetry.Metrics != nil && cfg.Snapshots != nil {
		snaps := cfg.Snapshots
		if age, ok := snaps.SnapshotAge(); ok {
			cfg.Telemetry.Metrics.SetConfigSnapshotAge("db_poll", age)
		}
		cfg.Telemetry.Metrics.SetConfigSnapshotAgeProvider(func() (string, time.Duration, bool) {
			age, ok := snaps.SnapshotAge()
			return "db_poll", age, ok
		})
	}

	if !cfg.DisableNativePassthrough {
		nh, nhErr := native.NewHandler(native.Config{
			Snapshots:               s.snapshots,
			Broker:                  s.broker,
			KillSwitch:              s.killSwitch,
			Audit:                   s.audit,
			Idempotency:             s.idempotency,
			PolicyEvaluator:         s.policyEval,
			RateLimiter:             s.rateLimiter,
			Breaker:                 s.breaker,
			RouteResolver:           s.routeResolver,
			BudgetLedger:            s.budgetLedger,
			DefaultHoldOutputTokens: s.defaultHoldOutputTokens,
			MaxRequestBodyBytes:     s.maxReqBodyBytes,
			FirstByteTimeout:        s.firstByteTimeout,
			HeartbeatInterval:       s.heartbeatInterval,
			NowMs:                   s.nowMs,
			OnUsage:                 cfg.NativeUsageObserver,
			Telemetry:               s.telemetry,
			AcquireRuntime: func(ctx context.Context, target *snapshot.ResolvedTarget) (port.ProviderRuntime, func(), error) {
				entry, err := s.getOrLoadAdapterRuntime(ctx, target)
				if err != nil {
					return nil, nil, err
				}
				return entry.rt, entry.release, nil
			},
		})
		if nhErr != nil {
			return nil, nhErr
		}
		s.nativeHandler = nh
	}

	if s.killSwitch != nil && s.snapshots != nil {
		s.snapshots.RegisterKillSwitchSync(s.killSwitch.RefreshFromDB)
	}

	s.mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	s.mux.HandleFunc("/v1/rerank", s.handleRerank)
	if !cfg.DisableResponses {
		s.mux.HandleFunc("/v1/responses", s.handleResponses)
	}
	if !cfg.DisableEmbeddings {
		s.mux.HandleFunc("/v1/embeddings", s.handleEmbeddings)
	}
	if !cfg.DisableModels {
		s.mux.HandleFunc("/v1/models", s.handleModels)
	}
	if s.telemetry != nil && s.telemetry.Metrics != nil && s.telemetry.Metrics.HasScrapeToken() {
		s.mux.Handle("/metrics", s.MetricsHandler())
	}
	s.mux.HandleFunc("/", s.handleNotFound)
	s.rootRouter = s.withIngressEnvelope(s.mux)
	return s, nil
}

// MetricsHandler returns the Prometheus /metrics exposition handler when telemetry is configured.
func (s *Server) MetricsHandler() http.Handler {
	if s == nil || s.telemetry == nil || s.telemetry.Metrics == nil {
		return http.NotFoundHandler()
	}
	return s.telemetry.Metrics
}

// FaultGuard returns the Server's active Wasm FaultGuard instance.
func (s *Server) FaultGuard() *wasm.FaultGuard {
	if s == nil {
		return nil
	}
	return s.faultGuard
}

// Breaker returns the Data Plane SM-04 circuit breaker shared across HTTP and native handlers.
func (s *Server) Breaker() *breaker.CircuitBreaker {
	if s == nil {
		return nil
	}
	return s.breaker
}

// RouteResolver returns the Data Plane 6-step route resolver shared across handlers.
func (s *Server) RouteResolver() *resolver.Resolver {
	if s == nil {
		return nil
	}
	return s.routeResolver
}

// ServeHTTP dispatches Data Plane HTTP requests through protocol/correlation guards.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.rootRouter.ServeHTTP(w, r)
}

// CheckServingReady reports whether the Data Plane's plugin runtime and plugin storage are available.
func (s *Server) CheckServingReady(ctx context.Context) error {
	if s == nil || s.closed.Load() {
		return fmt.Errorf("plugin engine is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.wasmCache == nil || strings.TrimSpace(s.pluginsDir) == "" {
		return fmt.Errorf("plugin engine is not initialized")
	}
	info, err := os.Stat(s.pluginsDir)
	if err != nil {
		return fmt.Errorf("plugin storage is unavailable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("plugin storage is unavailable")
	}
	if _, err := os.ReadDir(s.pluginsDir); err != nil {
		return fmt.Errorf("plugin storage is unavailable: %w", err)
	}
	return nil
}

// Close releases all cached Wasm runtimes and shared wazero compilation caches.
func (s *Server) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.rtMu.Lock()
	s.closed.Store(true)
	s.rtMu.Unlock()
	s.rtLoadWG.Wait()

	s.rtMu.Lock()
	entries := make([]*cachedAdapterEntry, 0, len(s.runtimes))
	for _, entry := range s.runtimes {
		if entry != nil {
			entry.retired.Store(true)
			entries = append(entries, entry)
		}
	}
	s.runtimes = make(map[runtimeCacheKey]*cachedAdapterEntry)
	s.rtMu.Unlock()

	var firstErr error
	for _, entry := range entries {
		if err := entry.closeRuntime(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.wasmCache != nil {
		if err := s.wasmCache.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type requestContextKey struct{}

type ingressContext struct {
	RequestID   string
	Traceparent string
}

func isNativeRequestPath(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	for _, p := range []string{r.URL.EscapedPath(), r.URL.Path} {
		if p == "/native" || strings.HasPrefix(p, "/native/") {
			return true
		}
	}
	if r.RequestURI != "" {
		rawPath := r.RequestURI
		if q := strings.IndexByte(rawPath, '?'); q >= 0 {
			rawPath = rawPath[:q]
		}
		if rawPath == "/native" || strings.HasPrefix(rawPath, "/native/") {
			return true
		}
	}
	return false
}

func (s *Server) withIngressEnvelope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL != nil && r.URL.Path == "/metrics" && s.telemetry != nil && s.telemetry.Metrics != nil && s.telemetry.Metrics.HasScrapeToken() {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			s.MetricsHandler().ServeHTTP(w, r)
			return
		}

		reqID := sanitizeOrMintRequestID(r.Header.Get("X-Tanpopo-Request-Id"))
		tp := sanitizeOrMintTraceparent(r.Header.Get("Traceparent"))

		w.Header().Set("X-Tanpopo-Request-Id", reqID)
		w.Header().Set("Traceparent", tp)
		w.Header().Set("X-Content-Type-Options", "nosniff")

		ctx := context.WithValue(r.Context(), requestContextKey{}, ingressContext{
			RequestID:   reqID,
			Traceparent: tp,
		})
		rCtx := r.WithContext(ctx)

		if isNativeRequestPath(rCtx) {
			if isProtocolUpgradeRequest(rCtx) {
				writeCanonicalError(w, reqID, http.StatusBadRequest, "unsupported_protocol_upgrade", "invalid_request_error", "WebSocket and WebRTC protocol upgrades are not supported on this gateway endpoint.", false)
				return
			}
			if s.nativeHandler == nil {
				s.handleNotFound(w, rCtx)
				return
			}
			s.nativeHandler.ServeHTTP(w, rCtx)
			return
		}

		if s.telemetry == nil {
			if isProtocolUpgradeRequest(rCtx) {
				writeCanonicalError(w, reqID, http.StatusBadRequest, "unsupported_protocol_upgrade", "invalid_request_error", "WebSocket and WebRTC protocol upgrades are not supported on this gateway endpoint.", false)
				return
			}
			next.ServeHTTP(w, rCtx)
			return
		}

		spanCtx, rootSpan, traceID := s.telemetry.StartRequestSpan(rCtx.Context(), tp, reqID, "unified_http")
		reqTel := &requestTelemetry{
			pipeline:      s.telemetry,
			reqID:         reqID,
			traceRecordID: idgen.MustNew(idgen.PrefixTrace),
			traceID:       traceID,
			entryPoint:    "unified_http",
			startedAt:     time.Now(),
			startedAtMs:   s.nowMs(),
			rootSpan:      rootSpan,
		}
		rCtx = rCtx.WithContext(withRequestTelemetry(spanCtx, reqTel))
		scw := &statusCaptureWriter{ResponseWriter: w}
		defer reqTel.finish(rCtx.Context(), scw, rCtx)

		if isProtocolUpgradeRequest(rCtx) {
			writeCanonicalError(scw, reqID, http.StatusBadRequest, "unsupported_protocol_upgrade", "invalid_request_error", "WebSocket and WebRTC protocol upgrades are not supported on this gateway endpoint.", false)
			return
		}
		next.ServeHTTP(scw, rCtx)
	})
}

func ingressFromContext(ctx context.Context) ingressContext {
	if ctx != nil {
		if v, ok := ctx.Value(requestContextKey{}).(ingressContext); ok && v.RequestID != "" {
			return v
		}
	}
	return ingressContext{
		RequestID:   mintRequestID(),
		Traceparent: mintTraceparent(),
	}
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	telemetryFromContext(r.Context()).setRoutePattern("not_found")
	meta := ingressFromContext(r.Context())
	writeCanonicalError(w, meta.RequestID, http.StatusNotFound, "resource_not_found", "not_found_error", "The requested Data Plane endpoint does not exist.", false)
}

func isProtocolUpgradeRequest(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("Upgrade")) != "" {
		return true
	}
	for _, connVal := range r.Header.Values("Connection") {
		for _, part := range strings.Split(connVal, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
				return true
			}
		}
	}
	return false
}

func sanitizeOrMintRequestID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > 0 && len(trimmed) <= MaxTraceHeaderLength && isSafeTraceToken(trimmed) {
		return trimmed
	}
	return mintRequestID()
}

func mintRequestID() string {
	trc := idgen.MustNew(idgen.PrefixTrace)
	return "req_" + strings.TrimPrefix(trc, idgen.PrefixTrace+"_")
}

func sanitizeOrMintTraceparent(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if isValidW3CTraceparent(trimmed) {
		return strings.ToLower(trimmed)
	}
	return mintTraceparent()
}

func mintTraceparent() string {
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
	// W3C Trace Context: 00-<32hex>-<16hex>-<2hex> (55 chars total)
	if len(s) != 55 || s[2] != '-' || s[35] != '-' || s[52] != '-' {
		return false
	}
	if s[0] != '0' || s[1] != '0' {
		return false
	}
	if !isHexNonZero(s[3:35]) || !isHexNonZero(s[36:52]) || !isHexString(s[53:55]) {
		return false
	}
	return true
}

func isHexString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

func isHexNonZero(s string) bool {
	nonZero := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
		if c != '0' {
			nonZero = true
		}
	}
	return nonZero
}

type canonicalErrorBody struct {
	Error canonicalErrorDetail `json:"error"`
}

type canonicalErrorDetail struct {
	Code      string `json:"code"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Retryable bool   `json:"retryable"`
}

func writeCanonicalError(w http.ResponseWriter, reqID string, status int, code, errType, message string, retryable bool) {
	if ec, ok := w.(interface{ SetCanonicalErrorCode(string) }); ok {
		ec.SetCanonicalErrorCode(code)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Tanpopo-Request-Id", reqID)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(canonicalErrorBody{
		Error: canonicalErrorDetail{
			Code:      code,
			Type:      errType,
			Message:   message,
			RequestID: reqID,
			Retryable: retryable,
		},
	})
}

func (s *Server) getOrLoadAdapterRuntime(ctx context.Context, target *snapshot.ResolvedTarget) (*cachedAdapterEntry, error) {
	if target == nil || target.Adapter == nil {
		return nil, fmt.Errorf("%w: missing adapter metadata", ErrInvalidServerConfig)
	}
	adp := target.Adapter
	if strings.TrimSpace(adp.ActiveVersionID) == "" {
		return nil, fmt.Errorf("%w: adapter %q has no active_version_id", port.ErrExecutionFault, adp.AdapterSlug)
	}
	if strings.EqualFold(adp.ExtensionState, installer.StateQuarantined) || s.faultGuard.IsQuarantined(adp.InstallationID) {
		return nil, &wasm.ExecutionFaultError{
			Kind:   wasm.FaultKindQuarantined,
			Reason: fmt.Sprintf("adapter %q quarantined", adp.AdapterSlug),
		}
	}

	var connID string
	if target.Connection != nil {
		connID = target.Connection.ID
	}
	key := runtimeCacheKey{
		InstallationID:  adp.InstallationID,
		ConnectionID:    connID,
		ActiveVersionID: adp.ActiveVersionID,
		BundleSHA256:    adp.BundleSHA256,
		PermissionsSHA:  sha256.Sum256([]byte(adp.ApprovedPermissionsJSON)),
		ScopeKey:        adapterRuntimeScopeKey(adp.ExecutionTier, adp, target.Connection),
	}

	s.rtMu.Lock()
	if s.closed.Load() {
		s.rtMu.Unlock()
		return nil, http.ErrServerClosed
	}
	if cached, ok := s.runtimes[key]; ok {
		cached.retain()
		s.rtMu.Unlock()
		return cached, nil
	}
	s.rtLoadWG.Add(1)
	defer s.rtLoadWG.Done()
	s.rtMu.Unlock()

	entry, err := s.loadAdapterRuntimeUncached(ctx, adp, target.Connection)
	if err != nil {
		return nil, err
	}

	var toClose []*cachedAdapterEntry
	s.rtMu.Lock()
	if s.closed.Load() {
		s.rtMu.Unlock()
		_ = entry.closeRuntime(ctx)
		return nil, http.ErrServerClosed
	}
	if existing, ok := s.runtimes[key]; ok {
		existing.retain()
		s.rtMu.Unlock()
		_ = entry.closeRuntime(ctx)
		return existing, nil
	}
	// Evict stale cached runtimes superseded by a version upgrade, permission re-consent,
	// or connection scope change without interrupting in-flight calls on sibling connections.
	for k, old := range s.runtimes {
		if k.InstallationID != adp.InstallationID {
			continue
		}
		versionOrPermChanged := k.ActiveVersionID != adp.ActiveVersionID || k.BundleSHA256 != adp.BundleSHA256 || k.PermissionsSHA != key.PermissionsSHA
		scopeChanged := k.ConnectionID == key.ConnectionID && k.ScopeKey != key.ScopeKey
		if versionOrPermChanged || scopeChanged {
			delete(s.runtimes, k)
			old.retired.Store(true)
			if old.inFlight.Load() == 0 {
				toClose = append(toClose, old)
			}
		}
	}
	entry.retain()
	s.runtimes[key] = entry
	s.rtMu.Unlock()

	for _, old := range toClose {
		_ = old.closeRuntime(ctx)
	}
	return entry, nil
}

// AcquireProviderRuntime returns the approved runtime for a tenant-scoped connection.
// The caller must invoke the returned release function after completing the operation.
func (s *Server) AcquireProviderRuntime(ctx context.Context, orgID, connectionID string) (port.ProviderRuntime, func(), error) {
	if s == nil || s.snapshots == nil {
		return nil, nil, fmt.Errorf("%w: runtime server unavailable", ErrInvalidServerConfig)
	}
	snap := s.snapshots.Current()
	if snap == nil {
		return nil, nil, fmt.Errorf("%w: configuration snapshot unavailable", port.ErrExecutionFault)
	}
	conn, ok := snap.LookupProviderConnection(orgID, connectionID)
	if !ok {
		return nil, nil, fmt.Errorf("provider connection %q is not present in the active snapshot", connectionID)
	}
	adapter, ok := snap.LookupProviderAdapter(conn.AdapterID)
	if !ok {
		return nil, nil, fmt.Errorf("provider adapter %q is not present in the active snapshot", conn.AdapterID)
	}
	entry, err := s.getOrLoadAdapterRuntime(ctx, &snapshot.ResolvedTarget{Connection: conn, Adapter: adapter})
	if err != nil {
		return nil, nil, err
	}
	return entry.rt, entry.release, nil
}

func adapterRuntimeScopeKey(tier string, adp *snapshot.ProviderAdapterView, conn *snapshot.ProviderConnectionView) string {
	var adpHosts []string
	if adp != nil {
		adpHosts = append([]string(nil), adp.AllowedHosts...)
		sort.Strings(adpHosts)
	}
	if tier != installer.ExecutionTierWasmSandboxT2 || conn == nil {
		return strings.Join(adpHosts, ",")
	}
	cidrs := append([]string(nil), conn.AllowedCIDRs...)
	sort.Strings(cidrs)
	port := endpointEffectivePort(conn.EndpointBaseURL)
	var scheme, host string
	if u, err := url.Parse(strings.TrimSpace(conn.EndpointBaseURL)); err == nil {
		scheme = strings.ToLower(u.Scheme)
		host = strings.ToLower(u.Hostname())
	}
	return scheme + "|" + host + "|" + strconv.Itoa(port) + "|" + strconv.FormatBool(conn.AllowPrivateIP) + "|" + strings.Join(cidrs, ",") + "|" + strings.Join(adpHosts, ",")
}

func (s *Server) loadAdapterRuntimeUncached(ctx context.Context, adp *snapshot.ProviderAdapterView, conn *snapshot.ProviderConnectionView) (*cachedAdapterEntry, error) {
	trimmedPerms := strings.TrimSpace(adp.ApprovedPermissionsJSON)
	if trimmedPerms == "" || trimmedPerms == "{}" {
		return nil, fmt.Errorf("%w: adapter %q has missing approved_permissions_json", port.ErrExecutionFault, adp.AdapterSlug)
	}
	var perms installer.PermissionSet
	if err := json.Unmarshal([]byte(trimmedPerms), &perms); err != nil {
		return nil, fmt.Errorf("%w: decode approved permissions: %v", port.ErrInvalidSpec, err)
	}

	manifestBytes, versionDir, err := s.readAdapterManifestBytes(adp)
	if err != nil {
		return nil, err
	}

	trimmedManifestJSON := strings.TrimSpace(adp.ManifestJSON)
	hasDBManifest := trimmedManifestJSON != "" && trimmedManifestJSON != "{}"
	var dbManifest installer.ExtensionManifest
	if hasDBManifest {
		if merr := json.Unmarshal([]byte(trimmedManifestJSON), &dbManifest); merr != nil {
			return nil, fmt.Errorf("%w: corrupt stored manifest_json: %v", port.ErrExecutionFault, merr)
		}
	}

	tier := strings.TrimSpace(adp.ExecutionTier)
	switch tier {
	case installer.ExecutionTierDeclarativeT1:
		authoritativeBytes := manifestBytes
		if versionDir != "" && hasDBManifest {
			diskManifest, derr := decodeBoundedManifestYAML(manifestBytes)
			if derr != nil {
				return nil, derr
			}
			if perr := verifyManifestParity(diskManifest, dbManifest); perr != nil {
				return nil, perr
			}
			authoritativeBytes = []byte(trimmedManifestJSON)
		} else if hasDBManifest {
			authoritativeBytes = []byte(trimmedManifestJSON)
		}
		eng, err := declarative.ParseDeclarativeManifestYAML(authoritativeBytes)
		if err != nil {
			return nil, err
		}
		return &cachedAdapterEntry{
			installationID: adp.InstallationID,
			rt:             eng,
			tier:           tier,
			perms:          perms,
			compiledAt:     s.nowMs(),
		}, nil

	case installer.ExecutionTierWasmSandboxT2:
		if versionDir == "" {
			return nil, fmt.Errorf("%w: plugins directory required for tier-2 wasm adapter %q", port.ErrExecutionFault, adp.AdapterSlug)
		}
		manifest, err := decodeBoundedManifestYAML(manifestBytes)
		if err != nil {
			return nil, err
		}
		if manifest.Runtime == nil || strings.TrimSpace(manifest.Runtime.Entrypoint) == "" {
			return nil, fmt.Errorf("%w: missing wasm runtime.entrypoint", port.ErrInvalidSpec)
		}
		expectedSHA := strings.TrimSpace(manifest.Runtime.SHA256)
		if expectedSHA == "" {
			return nil, fmt.Errorf("%w: missing required wasm runtime.sha256", port.ErrExecutionFault)
		}
		if hasDBManifest {
			if perr := verifyManifestParity(manifest, dbManifest); perr != nil {
				return nil, perr
			}
		}

		wasmBytes, err := readConfinedRegularFile(versionDir, manifest.Runtime.Entrypoint, wasm.MaxWasmBinaryBytes)
		if err != nil {
			return nil, fmt.Errorf("%w: read wasm entrypoint %q: %v", port.ErrExecutionFault, manifest.Runtime.Entrypoint, err)
		}
		sum := sha256.Sum256(wasmBytes)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), expectedSHA) {
			return nil, fmt.Errorf("%w: wasm entrypoint digest mismatch against manifest", port.ErrExecutionFault)
		}

		memPages := wasm.DefaultMemoryLimitPages
		if perms.MaxMemoryMib > 0 {
			memPages = uint32(perms.MaxMemoryMib) * 16
		}
		var callTimeout, chunkTimeout time.Duration
		if manifest.Runtime.Quotas != nil {
			if manifest.Runtime.Quotas.MaxCpuMsPerCall > installer.MaxAllowedCpuMsPerCall {
				return nil, fmt.Errorf("%w: runtime.quotas.maxCpuMsPerCall %d exceeds %d ms cap", port.ErrInvalidSpec, manifest.Runtime.Quotas.MaxCpuMsPerCall, installer.MaxAllowedCpuMsPerCall)
			}
			if manifest.Runtime.Quotas.MaxStreamChunkCpuMs > installer.MaxAllowedStreamChunkCpuMs {
				return nil, fmt.Errorf("%w: runtime.quotas.maxStreamChunkCpuMs %d exceeds %d ms cap", port.ErrInvalidSpec, manifest.Runtime.Quotas.MaxStreamChunkCpuMs, installer.MaxAllowedStreamChunkCpuMs)
			}
			if manifest.Runtime.Quotas.MaxCpuMsPerCall > 0 {
				callTimeout = time.Duration(manifest.Runtime.Quotas.MaxCpuMsPerCall) * time.Millisecond
			}
			if manifest.Runtime.Quotas.MaxStreamChunkCpuMs > 0 {
				chunkTimeout = time.Duration(manifest.Runtime.Quotas.MaxStreamChunkCpuMs) * time.Millisecond
			}
		}

		var narrowHosts []string
		var narrowPorts []int
		var narrowAllowPriv bool
		var narrowCIDRs []string
		requireTLS := perms.RequireTLS
		if conn != nil {
			narrow := buildHostNarrowPolicy(perms, adp, conn)
			narrowHosts = narrow.AllowedHosts
			narrowPorts = narrow.AllowedPorts
			narrowAllowPriv = narrow.AllowPrivateIP
			narrowCIDRs = narrow.AllowedCIDRs
			requireTLS = narrow.RequireTLS
		} else {
			narrowHosts = intersectAllowedHosts(perms.AllowedHosts, adp.AllowedHosts)
			narrowAllowPriv = perms.AllowPrivateIP
		}

		wasmRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
			WasmBytes:            wasmBytes,
			InstallationID:       adp.InstallationID,
			PoolSize:             s.wasmPoolSize,
			MemoryLimitPages:     memPages,
			AllowExtendedMemory:  perms.MaxMemoryMib > 16,
			CallTimeout:          callTimeout,
			StreamChunkTimeout:   chunkTimeout,
			ABIMode:              wasm.ABIModeCanonicalEnvelope,
			AllowedHosts:         narrowHosts,
			AllowedPorts:         narrowPorts,
			AllowPrivateIP:       narrowAllowPriv,
			AllowedCIDRs:         narrowCIDRs,
			AllowInsecureHTTP:    !requireTLS,
			ApprovedSecretSlots:  append([]string(nil), perms.SecretSlots...),
			AllowHostBlindEgress: len(narrowHosts) > 0 && s.broker != nil,
			BlindBroker:          s.broker,
			CompilationCache:     s.wasmCache,
			FaultGuard:           s.faultGuard,
		})
		if err != nil {
			return nil, err
		}
		return &cachedAdapterEntry{
			installationID: adp.InstallationID,
			rt:             wasmRT,
			wasmRT:         wasmRT,
			tier:           tier,
			perms:          perms,
			compiledAt:     s.nowMs(),
		}, nil

	default:
		return nil, fmt.Errorf("%w: unsupported execution_tier %q in M1 data plane", port.ErrExecutionFault, tier)
	}
}

func verifyManifestParity(diskRaw, db installer.ExtensionManifest) error {
	normDisk, err := canonicalizeManifestViaJSON(diskRaw)
	if err != nil {
		return err
	}
	normDB, err := canonicalizeManifestViaJSON(db)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(normDB.Metadata.ID), strings.TrimSpace(normDisk.Metadata.ID)) {
		return fmt.Errorf("%w: on-disk manifest id mismatch against database record", port.ErrExecutionFault)
	}
	if strings.TrimSpace(normDB.Metadata.Version) != strings.TrimSpace(normDisk.Metadata.Version) {
		return fmt.Errorf("%w: on-disk manifest version mismatch against database record", port.ErrExecutionFault)
	}
	if !reflect.DeepEqual(normDB.Runtime, normDisk.Runtime) {
		return fmt.Errorf("%w: on-disk manifest runtime mismatch against database record", port.ErrExecutionFault)
	}
	if !reflect.DeepEqual(normDB.Permissions, normDisk.Permissions) {
		return fmt.Errorf("%w: on-disk manifest permissions mismatch against database record", port.ErrExecutionFault)
	}
	if !reflect.DeepEqual(normDB.Adapter, normDisk.Adapter) {
		return fmt.Errorf("%w: on-disk manifest adapter rules mismatch against database record", port.ErrExecutionFault)
	}
	if !reflect.DeepEqual(normDB.SecretSlots, normDisk.SecretSlots) {
		return fmt.Errorf("%w: on-disk manifest secret slots mismatch against database record", port.ErrExecutionFault)
	}
	if !reflect.DeepEqual(normDB.Capabilities, normDisk.Capabilities) {
		return fmt.Errorf("%w: on-disk manifest capabilities mismatch against database record", port.ErrExecutionFault)
	}
	return nil
}

func canonicalizeManifestViaJSON(m installer.ExtensionManifest) (installer.ExtensionManifest, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return installer.ExtensionManifest{}, fmt.Errorf("%w: canonicalize manifest: %v", port.ErrExecutionFault, err)
	}
	var out installer.ExtensionManifest
	if err := json.Unmarshal(b, &out); err != nil {
		return installer.ExtensionManifest{}, fmt.Errorf("%w: canonicalize manifest: %v", port.ErrExecutionFault, err)
	}
	return out, nil
}

func decodeBoundedManifestYAML(raw []byte) (installer.ExtensionManifest, error) {
	var root yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&root); err != nil {
		return installer.ExtensionManifest{}, fmt.Errorf("%w: decode extension manifest: %v", port.ErrInvalidSpec, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		return installer.ExtensionManifest{}, fmt.Errorf("%w: multi-document manifest YAML is prohibited", port.ErrInvalidSpec)
	}
	aliases, nodes := 0, 0
	if err := checkIngressYAMLNodeBounds(&root, 0, &aliases, &nodes, make(map[*yaml.Node]bool)); err != nil {
		return installer.ExtensionManifest{}, err
	}
	var manifest installer.ExtensionManifest
	if err := root.Decode(&manifest); err != nil {
		return installer.ExtensionManifest{}, fmt.Errorf("%w: decode extension manifest: %v", port.ErrInvalidSpec, err)
	}
	return manifest, nil
}

func checkIngressYAMLNodeBounds(n *yaml.Node, depth int, aliases, expanded *int, visiting map[*yaml.Node]bool) error {
	if n == nil {
		return nil
	}
	*expanded++
	if *expanded > declarative.MaxExpandedYAMLNodes {
		return fmt.Errorf("%w: manifest YAML expanded nodes exceed limit", port.ErrInvalidSpec)
	}
	if depth > declarative.MaxJSONDepth {
		return fmt.Errorf("%w: manifest YAML nesting depth exceeds limit", port.ErrInvalidSpec)
	}
	if visiting[n] {
		return fmt.Errorf("%w: recursive YAML alias cycle prohibited", port.ErrInvalidSpec)
	}
	if n.Kind == yaml.AliasNode {
		*aliases++
		if *aliases > 64 {
			return fmt.Errorf("%w: too many YAML aliases in manifest", port.ErrInvalidSpec)
		}
		if n.Alias == nil {
			return nil
		}
		visiting[n] = true
		err := checkIngressYAMLNodeBounds(n.Alias, depth, aliases, expanded, visiting)
		delete(visiting, n)
		return err
	}
	visiting[n] = true
	defer delete(visiting, n)
	for _, c := range n.Content {
		if err := checkIngressYAMLNodeBounds(c, depth+1, aliases, expanded, visiting); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) readAdapterManifestBytes(adp *snapshot.ProviderAdapterView) ([]byte, string, error) {
	if s.pluginsDir != "" {
		verID := strings.TrimSpace(adp.ActiveVersionID)
		if verID == "" || verID == "." || strings.ContainsAny(verID, `/\`) || strings.Contains(verID, "..") {
			return nil, "", fmt.Errorf("%w: unsafe active_version_id %q", port.ErrExecutionFault, verID)
		}
		verDir := filepath.Join(s.pluginsDir, verID)
		evalVerDir, err := filepath.EvalSymlinks(verDir)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, "", fmt.Errorf("%w: inspect adapter version directory: %v", port.ErrExecutionFault, err)
			}
		} else {
			rel, rerr := filepath.Rel(s.pluginsDir, evalVerDir)
			if rerr != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
				return nil, "", fmt.Errorf("%w: adapter version directory escaped plugins root", port.ErrExecutionFault)
			}
			raw, rerr := readConfinedRegularFile(evalVerDir, "manifest.yaml", declarative.MaxManifestBytes)
			if rerr == nil {
				return raw, evalVerDir, nil
			}
			if !errors.Is(rerr, os.ErrNotExist) {
				return nil, "", fmt.Errorf("%w: invalid on-disk adapter manifest: %v", port.ErrExecutionFault, rerr)
			}
		}
	}
	if strings.TrimSpace(adp.ManifestJSON) != "" && strings.TrimSpace(adp.ManifestJSON) != "{}" {
		return []byte(adp.ManifestJSON), "", nil
	}
	return nil, "", fmt.Errorf("%w: adapter manifest unavailable for version %q", port.ErrExecutionFault, adp.ActiveVersionID)
}

func readConfinedRegularFile(rootDir, relPath string, maxBytes int64) ([]byte, error) {
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimSpace(relPath)))
	if cleaned == "." || cleaned == "" || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return nil, fmt.Errorf("unsafe relative path %q", relPath)
	}
	candidate := filepath.Join(rootDir, cleaned)
	info, err := os.Lstat(candidate)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path %q is not a regular file", relPath)
	}
	evalPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(rootDir, evalPath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("resolved path %q escaped confinement root", relPath)
	}
	f, err := os.Open(evalPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file %q exceeds %d byte cap", relPath, maxBytes)
	}
	return data, nil
}
