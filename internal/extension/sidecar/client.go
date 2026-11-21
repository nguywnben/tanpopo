// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// ServiceName matches proto/tanpopo/extension/v1/runtime.proto service ExtensionRuntimeService.
	ServiceName = "tanpopo.extension.v1.ExtensionRuntimeService"

	// DefaultCallTimeout bounds synchronous Tier-3 Sidecar ConnectRPC calls.
	DefaultCallTimeout = 500 * time.Millisecond
	// DefaultStreamChunkTimeout bounds incremental SSE chunk calls.
	DefaultStreamChunkTimeout = 250 * time.Millisecond
	// DefaultCircuitFailureThreshold trips a crashed/unresponsive sidecar after consecutive transport faults.
	DefaultCircuitFailureThreshold = 2
	// DefaultCircuitCooldown bounds how long an open sidecar circuit rejects calls before half-open probe.
	DefaultCircuitCooldown = 15 * time.Second
	// MaxSessionStateBytes enforces the 64 KiB scratch state cap from runtime.proto.
	MaxSessionStateBytes = 64 * 1024
)

var (
	// ErrSidecarDisabled rejects Tier-3 execution when runtime.sidecar.enabled is false (default).
	ErrSidecarDisabled = fmt.Errorf("%w: runtime.sidecar.enabled is false (Tier-3 sidecar disabled by default)", port.ErrExecutionFault)
	// ErrSidecarCircuitOpen rejects calls while an unhealthy/crashed sidecar circuit breaker is open.
	ErrSidecarCircuitOpen = fmt.Errorf("%w: tier-3 sidecar circuit breaker open", port.ErrExecutionFault)
	// ErrSecretLeakBlocked rejects attempts to pass raw non-opaque secrets across sidecar boundaries.
	ErrSecretLeakBlocked = errors.New("sidecar: plaintext secrets must never cross host-to-sidecar boundary")

	metadataBlockedPrefixes = []netip.Prefix{
		netip.MustParsePrefix("169.254.0.0/16"),
		netip.MustParsePrefix("fe80::/10"),
		netip.MustParsePrefix("fd00:ec2::254/128"),
	}
)

// ConnectRPC procedure paths matching proto/tanpopo/extension/v1/runtime.proto.
const (
	ProcBuildValidationProbe   = "/" + ServiceName + "/BuildValidationProbe"
	ProcVerifyValidationResult = "/" + ServiceName + "/VerifyValidationResult"
	ProcTransformRequest       = "/" + ServiceName + "/TransformRequest"
	ProcTransformResponse      = "/" + ServiceName + "/TransformResponse"
	ProcTransformStreamChunk   = "/" + ServiceName + "/TransformStreamChunk"
	ProcNormalizeError         = "/" + ServiceName + "/NormalizeError"
	ProcExtractUsage           = "/" + ServiceName + "/ExtractUsage"
	ProcInspectNativeRequest   = "/" + ServiceName + "/InspectNativeRequest"
	ProcEvaluateGuardrail      = "/" + ServiceName + "/EvaluateGuardrail"
	ProcCancelExecution        = "/" + ServiceName + "/CancelExecution"
)

// jsonConnectCodec encodes/decodes ExtensionRuntimeService payloads using Go JSON
// with canonical runtime.proto snake_case field bindings over ConnectRPC.
type jsonConnectCodec struct{}

func (jsonConnectCodec) Name() string { return "json" }

func (jsonConnectCodec) Marshal(msg any) ([]byte, error) {
	return json.Marshal(msg)
}

func (jsonConnectCodec) Unmarshal(data []byte, msg any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, msg)
}

// Config configures a Tier-3 ConnectRPC Sidecar client.
// Enabled defaults to false (fail-closed) until explicitly opted in by an administrator.
type Config struct {
	Enabled                 bool
	InstallationID          string
	EndpointURL             string
	UDSPath                 string
	CA                      *EphemeralCA
	ClientCertSource        *RotatingCertSource
	ExpectedServerSPIFFE    string
	CallTimeout             time.Duration
	StreamChunkTimeout      time.Duration
	CircuitFailureThreshold int
	CircuitCooldown         time.Duration
	AuditSink               storageport.AuditSink
	Now                     func() time.Time
}

// Client invokes ExtensionRuntimeService over ConnectRPC (mTLS TCP or 0600 UDS)
// implementing port.ExtensionRuntime with per-sidecar circuit breaking and zero secret leakage.
type Client struct {
	installationID string
	baseURL        string
	udsPath        string
	httpClient     *http.Client
	callTimeout    time.Duration
	chunkTimeout   time.Duration
	audit          storageport.AuditSink
	now            func() time.Time

	threshold int
	cooldown  time.Duration

	mu          sync.Mutex
	failures    int
	circuitOpen bool
	openUntil   time.Time
	halfOpenTry bool

	buildValidationProbe   *connect.Client[port.ValidationProbeInput, port.UpstreamHttpSpec]
	verifyValidationResult *connect.Client[port.ValidationResultInput, port.ValidationVerdict]
	transformRequest       *connect.Client[port.TransformRequestInput, port.UpstreamHttpSpec]
	transformResponse      *connect.Client[port.TransformResponseInput, port.NormalizedInferenceResponse]
	transformStreamChunk   *connect.Client[port.StreamChunkInput, port.StreamChunkOutput]
	normalizeError         *connect.Client[port.NormalizeErrorInput, port.CanonicalProviderError]
	extractUsage           *connect.Client[port.ExtractUsageInput, port.NormalizedUsage]
	inspectNativeRequest   *connect.Client[port.NativeRequestInput, port.NativeRoutingHints]
	evaluateGuardrail      *connect.Client[port.GuardrailInput, port.GuardrailVerdict]
	cancelExecution        *connect.Client[port.CancelExecutionSignal, port.CancelAck]
}

var _ port.ExtensionRuntime = (*Client)(nil)

// NewClient constructs a fail-closed ConnectRPC Tier-3 Sidecar client.
func NewClient(cfg Config) (*Client, error) {
	if !cfg.Enabled {
		return nil, ErrSidecarDisabled
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	callTimeout := cfg.CallTimeout
	if callTimeout <= 0 {
		callTimeout = DefaultCallTimeout
	}
	chunkTimeout := cfg.StreamChunkTimeout
	if chunkTimeout <= 0 {
		chunkTimeout = DefaultStreamChunkTimeout
	}
	threshold := cfg.CircuitFailureThreshold
	if threshold <= 0 {
		threshold = DefaultCircuitFailureThreshold
	}
	cooldown := cfg.CircuitCooldown
	if cooldown <= 0 {
		cooldown = DefaultCircuitCooldown
	}

	var transport *http.Transport
	var baseURL string
	udsPath := strings.TrimSpace(cfg.UDSPath)

	if udsPath != "" {
		cleanSock, err := VerifyUDSSocketPermissions(udsPath)
		if err != nil {
			return nil, err
		}
		udsPath = cleanSock
		baseURL = "http://tanpopo.sidecar.local"
		dialer := &net.Dialer{
			Timeout: callTimeout,
			Control: sidecarUDSControl,
		}
		transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				if _, err := VerifyUDSSocketPermissions(cleanSock); err != nil {
					return nil, err
				}
				return dialer.DialContext(ctx, "unix", cleanSock)
			},
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        16,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: callTimeout,
		}
	} else {
		rawEP := strings.TrimSpace(cfg.EndpointURL)
		u, err := url.Parse(rawEP)
		if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil {
			return nil, fmt.Errorf("%w: mTLS sidecar endpoint_url must be https:// without userinfo", port.ErrInvalidSpec)
		}
		tlsCfg, err := BuildClientTLSConfig(cfg.CA, cfg.ClientCertSource, cfg.ExpectedServerSPIFFE)
		if err != nil {
			return nil, err
		}
		baseURL = strings.TrimRight(u.String(), "/")
		dialer := &net.Dialer{
			Timeout: callTimeout,
			Control: sidecarNetworkControl,
		}
		transport = &http.Transport{
			DialContext:         dialer.DialContext,
			TLSClientConfig:     tlsCfg,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        16,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: callTimeout,
		}
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   callTimeout * 2,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	c := &Client{
		installationID: strings.TrimSpace(cfg.InstallationID),
		baseURL:        baseURL,
		udsPath:        udsPath,
		httpClient:     httpClient,
		callTimeout:    callTimeout,
		chunkTimeout:   chunkTimeout,
		audit:          cfg.AuditSink,
		now:            nowFn,
		threshold:      threshold,
		cooldown:       cooldown,
	}

	opts := []connect.ClientOption{
		connect.WithCodec(jsonConnectCodec{}),
	}
	c.buildValidationProbe = connect.NewClient[port.ValidationProbeInput, port.UpstreamHttpSpec](httpClient, baseURL+ProcBuildValidationProbe, opts...)
	c.verifyValidationResult = connect.NewClient[port.ValidationResultInput, port.ValidationVerdict](httpClient, baseURL+ProcVerifyValidationResult, opts...)
	c.transformRequest = connect.NewClient[port.TransformRequestInput, port.UpstreamHttpSpec](httpClient, baseURL+ProcTransformRequest, opts...)
	c.transformResponse = connect.NewClient[port.TransformResponseInput, port.NormalizedInferenceResponse](httpClient, baseURL+ProcTransformResponse, opts...)
	c.transformStreamChunk = connect.NewClient[port.StreamChunkInput, port.StreamChunkOutput](httpClient, baseURL+ProcTransformStreamChunk, opts...)
	c.normalizeError = connect.NewClient[port.NormalizeErrorInput, port.CanonicalProviderError](httpClient, baseURL+ProcNormalizeError, opts...)
	c.extractUsage = connect.NewClient[port.ExtractUsageInput, port.NormalizedUsage](httpClient, baseURL+ProcExtractUsage, opts...)
	c.inspectNativeRequest = connect.NewClient[port.NativeRequestInput, port.NativeRoutingHints](httpClient, baseURL+ProcInspectNativeRequest, opts...)
	c.evaluateGuardrail = connect.NewClient[port.GuardrailInput, port.GuardrailVerdict](httpClient, baseURL+ProcEvaluateGuardrail, opts...)
	c.cancelExecution = connect.NewClient[port.CancelExecutionSignal, port.CancelAck](httpClient, baseURL+ProcCancelExecution, opts...)
	return c, nil
}

// Close closes idle transport connections cleanly.
func (c *Client) Close() error {
	if c != nil && c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}

// CircuitOpen reports whether this sidecar's dedicated circuit breaker is currently open.
func (c *Client) CircuitOpen() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.circuitOpen {
		return false
	}
	if !c.now().Before(c.openUntil) {
		return false
	}
	return true
}

func (c *Client) preCall() error {
	if c == nil {
		return ErrSidecarDisabled
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.circuitOpen {
		return nil
	}
	if c.now().Before(c.openUntil) {
		return ErrSidecarCircuitOpen
	}
	if c.halfOpenTry {
		return ErrSidecarCircuitOpen
	}
	c.halfOpenTry = true
	return nil
}

func (c *Client) recordSuccess() {
	c.mu.Lock()
	c.failures = 0
	c.circuitOpen = false
	c.halfOpenTry = false
	c.mu.Unlock()
}

func (c *Client) recordFailure(ctx context.Context, err error) {
	if !isSidecarFaultError(ctx, err) {
		c.mu.Lock()
		c.halfOpenTry = false
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	c.failures++
	failures := c.failures
	trippedNow := false
	if c.halfOpenTry || c.failures >= c.threshold {
		if !c.circuitOpen {
			trippedNow = true
		}
		c.circuitOpen = true
		c.openUntil = c.now().Add(c.cooldown)
		c.halfOpenTry = false
	}
	c.mu.Unlock()

	if trippedNow && c.audit != nil {
		audID, idErr := idgen.New(idgen.PrefixAudit)
		if idErr == nil {
			_ = c.audit.Append(context.WithoutCancel(ctx), storageport.AuditEvent{
				ID:           audID,
				CreatedAtMs:  c.now().UTC().UnixMilli(),
				ActorKind:    "SYSTEM",
				ActorID:      "sidecar_circuit_breaker",
				EntryPoint:   storageport.AuditEntryPointDataPlaneGuard,
				EventType:    "extension.sidecar.circuit_open",
				TargetType:   "extension_installation",
				TargetID:     nonEmptyOrDefault(c.installationID, c.baseURL),
				MetadataJSON: fmt.Sprintf(`{"failures":%d}`, failures),
			})
		}
	}
}

func isSidecarFaultError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		if errors.Is(err, context.Canceled) || connect.CodeOf(err) == connect.CodeCanceled {
			return false
		}
	}
	var cErr *connect.Error
	if errors.As(err, &cErr) {
		switch cErr.Code() {
		case connect.CodeInvalidArgument, connect.CodeFailedPrecondition, connect.CodePermissionDenied:
			return false
		default:
			return true
		}
	}
	return true
}

func invokeUnary[Req any, Resp any](c *Client, ctx context.Context, timeout time.Duration, cl *connect.Client[Req, Resp], in *Req) (Resp, error) {
	var zero Resp
	if err := c.preCall(); err != nil {
		return zero, err
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := cl.CallUnary(callCtx, connect.NewRequest(in))
	if err != nil {
		c.recordFailure(ctx, err)
		return zero, mapConnectError(err)
	}
	c.recordSuccess()
	if resp == nil || resp.Msg == nil {
		return zero, fmt.Errorf("%w: empty connectrpc response", port.ErrExecutionFault)
	}
	return *resp.Msg, nil
}

func mapConnectError(err error) error {
	var cErr *connect.Error
	if !errors.As(err, &cErr) {
		return fmt.Errorf("%w: %v", port.ErrExecutionFault, err)
	}
	switch cErr.Code() {
	case connect.CodeInvalidArgument, connect.CodeFailedPrecondition:
		return fmt.Errorf("%w: %s", port.ErrInvalidSpec, cErr.Message())
	case connect.CodePermissionDenied:
		return fmt.Errorf("%w: %s", port.ErrProhibitedCookieInjection, cErr.Message())
	default:
		return fmt.Errorf("%w: %s", port.ErrExecutionFault, cErr.Message())
	}
}

// BuildValidationProbe executes ExtensionRuntimeService.BuildValidationProbe over ConnectRPC.
func (c *Client) BuildValidationProbe(ctx context.Context, in port.ValidationProbeInput) (port.UpstreamHttpSpec, error) {
	spec, err := invokeUnary(c, ctx, c.callTimeout, c.buildValidationProbe, &in)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	if err := spec.Validate(); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	return spec, nil
}

// VerifyValidationResult executes ExtensionRuntimeService.VerifyValidationResult over ConnectRPC.
func (c *Client) VerifyValidationResult(ctx context.Context, in port.ValidationResultInput) (port.ValidationVerdict, error) {
	return invokeUnary(c, ctx, c.callTimeout, c.verifyValidationResult, &in)
}

// TransformRequest executes ExtensionRuntimeService.TransformRequest over ConnectRPC.
// Rejects any SecretSlotRefs containing plaintext key material before leaving the Host.
func (c *Client) TransformRequest(ctx context.Context, in port.TransformRequestInput) (port.UpstreamHttpSpec, error) {
	for slot, ref := range in.SecretSlotRefs {
		if !isOpaqueSecretSlotRef(ref) {
			return port.UpstreamHttpSpec{}, fmt.Errorf("%w: slot %q handle is not opaque", ErrSecretLeakBlocked, slot)
		}
	}
	spec, err := invokeUnary(c, ctx, c.callTimeout, c.transformRequest, &in)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	if err := spec.Validate(); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	return spec, nil
}

// TransformResponse executes ExtensionRuntimeService.TransformResponse over ConnectRPC.
func (c *Client) TransformResponse(ctx context.Context, in port.TransformResponseInput) (port.NormalizedInferenceResponse, error) {
	return invokeUnary(c, ctx, c.callTimeout, c.transformResponse, &in)
}

// TransformStreamChunk executes ExtensionRuntimeService.TransformStreamChunk over ConnectRPC.
func (c *Client) TransformStreamChunk(ctx context.Context, in port.StreamChunkInput) (port.StreamChunkOutput, error) {
	if len(in.SessionState) > MaxSessionStateBytes {
		return port.StreamChunkOutput{}, fmt.Errorf("%w: session_state exceeds %d bytes", port.ErrInvalidSpec, MaxSessionStateBytes)
	}
	out, err := invokeUnary(c, ctx, c.chunkTimeout, c.transformStreamChunk, &in)
	if err != nil {
		return port.StreamChunkOutput{}, err
	}
	if len(out.NextSessionState) > MaxSessionStateBytes {
		return port.StreamChunkOutput{}, fmt.Errorf("%w: next_session_state exceeds %d bytes", port.ErrInvalidSpec, MaxSessionStateBytes)
	}
	return out, nil
}

// NormalizeError executes ExtensionRuntimeService.NormalizeError over ConnectRPC.
func (c *Client) NormalizeError(ctx context.Context, in port.NormalizeErrorInput) (port.CanonicalProviderError, error) {
	return invokeUnary(c, ctx, c.callTimeout, c.normalizeError, &in)
}

// ExtractUsage executes ExtensionRuntimeService.ExtractUsage over ConnectRPC.
func (c *Client) ExtractUsage(ctx context.Context, in port.ExtractUsageInput) (port.NormalizedUsage, error) {
	return invokeUnary(c, ctx, c.callTimeout, c.extractUsage, &in)
}

// InspectNativeRequest executes ExtensionRuntimeService.InspectNativeRequest over ConnectRPC.
func (c *Client) InspectNativeRequest(ctx context.Context, in port.NativeRequestInput) (port.NativeRoutingHints, error) {
	hints, err := invokeUnary(c, ctx, c.callTimeout, c.inspectNativeRequest, &in)
	if err != nil {
		return port.NativeRoutingHints{}, err
	}
	for _, inj := range hints.CredentialInjections {
		if err := inj.Validate(); err != nil {
			return port.NativeRoutingHints{}, err
		}
	}
	return hints, nil
}

// EvaluateGuardrail executes ExtensionRuntimeService.EvaluateGuardrail over ConnectRPC.
func (c *Client) EvaluateGuardrail(ctx context.Context, in port.GuardrailInput) (port.GuardrailVerdict, error) {
	return invokeUnary(c, ctx, c.callTimeout, c.evaluateGuardrail, &in)
}

// CancelExecution executes ExtensionRuntimeService.CancelExecution over ConnectRPC.
func (c *Client) CancelExecution(ctx context.Context, sig port.CancelExecutionSignal) (port.CancelAck, error) {
	return invokeUnary(c, ctx, c.callTimeout, c.cancelExecution, &sig)
}

// NewConnectHandler wraps an ExtensionRuntime implementation as a ConnectRPC http.Handler
// serving all 10 ExtensionRuntimeService unary procedures.
func NewConnectHandler(rt port.ExtensionRuntime) http.Handler {
	mux := http.NewServeMux()
	if rt == nil {
		return mux
	}
	opts := []connect.HandlerOption{
		connect.WithCodec(jsonConnectCodec{}),
	}
	handleErr := func(err error) error {
		if err == nil {
			return nil
		}
		if errors.Is(err, port.ErrProhibitedCookieInjection) {
			return connect.NewError(connect.CodePermissionDenied, err)
		}
		if errors.Is(err, port.ErrInvalidSpec) {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
		return connect.NewError(connect.CodeInternal, err)
	}

	mux.Handle(ProcBuildValidationProbe, connect.NewUnaryHandler(ProcBuildValidationProbe, func(ctx context.Context, req *connect.Request[port.ValidationProbeInput]) (*connect.Response[port.UpstreamHttpSpec], error) {
		out, err := rt.BuildValidationProbe(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	mux.Handle(ProcVerifyValidationResult, connect.NewUnaryHandler(ProcVerifyValidationResult, func(ctx context.Context, req *connect.Request[port.ValidationResultInput]) (*connect.Response[port.ValidationVerdict], error) {
		out, err := rt.VerifyValidationResult(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	mux.Handle(ProcTransformRequest, connect.NewUnaryHandler(ProcTransformRequest, func(ctx context.Context, req *connect.Request[port.TransformRequestInput]) (*connect.Response[port.UpstreamHttpSpec], error) {
		out, err := rt.TransformRequest(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	mux.Handle(ProcTransformResponse, connect.NewUnaryHandler(ProcTransformResponse, func(ctx context.Context, req *connect.Request[port.TransformResponseInput]) (*connect.Response[port.NormalizedInferenceResponse], error) {
		out, err := rt.TransformResponse(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	mux.Handle(ProcTransformStreamChunk, connect.NewUnaryHandler(ProcTransformStreamChunk, func(ctx context.Context, req *connect.Request[port.StreamChunkInput]) (*connect.Response[port.StreamChunkOutput], error) {
		out, err := rt.TransformStreamChunk(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	mux.Handle(ProcNormalizeError, connect.NewUnaryHandler(ProcNormalizeError, func(ctx context.Context, req *connect.Request[port.NormalizeErrorInput]) (*connect.Response[port.CanonicalProviderError], error) {
		out, err := rt.NormalizeError(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	mux.Handle(ProcExtractUsage, connect.NewUnaryHandler(ProcExtractUsage, func(ctx context.Context, req *connect.Request[port.ExtractUsageInput]) (*connect.Response[port.NormalizedUsage], error) {
		out, err := rt.ExtractUsage(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	mux.Handle(ProcInspectNativeRequest, connect.NewUnaryHandler(ProcInspectNativeRequest, func(ctx context.Context, req *connect.Request[port.NativeRequestInput]) (*connect.Response[port.NativeRoutingHints], error) {
		out, err := rt.InspectNativeRequest(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	mux.Handle(ProcEvaluateGuardrail, connect.NewUnaryHandler(ProcEvaluateGuardrail, func(ctx context.Context, req *connect.Request[port.GuardrailInput]) (*connect.Response[port.GuardrailVerdict], error) {
		out, err := rt.EvaluateGuardrail(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	mux.Handle(ProcCancelExecution, connect.NewUnaryHandler(ProcCancelExecution, func(ctx context.Context, req *connect.Request[port.CancelExecutionSignal]) (*connect.Response[port.CancelAck], error) {
		out, err := rt.CancelExecution(ctx, *req.Msg)
		if err != nil {
			return nil, handleErr(err)
		}
		return connect.NewResponse(&out), nil
	}, opts...))
	return mux
}

// ListenUDS creates a permission-restricted 0600 Unix domain socket listener for Tier-3 Sidecars.
func ListenUDS(socketPath string) (net.Listener, error) {
	clean := strings.TrimSpace(socketPath)
	if clean == "" {
		return nil, fmt.Errorf("%w: socket path required", ErrUnsafeUDSSocket)
	}
	if _, err := os.Lstat(clean); err == nil {
		if err := os.Remove(clean); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", clean)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(clean, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

func sidecarUDSControl(network, _ string, _ syscall.RawConn) error {
	if network != "unix" {
		return fmt.Errorf("%w: expected unix socket network, got %q", ErrUnsafeUDSSocket, network)
	}
	return nil
}

func sidecarNetworkControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("sidecar: invalid target address %q", address)
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return fmt.Errorf("sidecar: non-IP dial target rejected")
	}
	addr = addr.Unmap()
	for _, p := range metadataBlockedPrefixes {
		if p.Contains(addr) {
			return fmt.Errorf("sidecar: cloud metadata/link-local address rejected")
		}
	}
	return nil
}

func isOpaqueSecretSlotRef(v string) bool {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" || len(trimmed) > 128 {
		return false
	}
	if strings.Contains(trimmed, "sk-") || strings.Contains(trimmed, "Bearer ") || strings.ContainsAny(trimmed, " \t\r\n") {
		return false
	}
	return strings.HasPrefix(trimmed, "slot_ref_") || strings.HasPrefix(trimmed, "handle_")
}

func nonEmptyOrDefault(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
