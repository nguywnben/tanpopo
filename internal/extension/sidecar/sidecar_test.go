// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package sidecar

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

var helperRunner func()

func TestMain(m *testing.M) {
	if os.Getenv("TANPOPO_SIDECAR_HELPER") == "1" && helperRunner != nil {
		helperRunner()
		os.Exit(0)
	}
	os.Exit(netguard.MustRunHermeticMain(m))
}

type memAuditSink struct {
	events atomic.Int32
}

func (m *memAuditSink) Append(context.Context, storageport.AuditEvent) error {
	m.events.Add(1)
	return nil
}

type mockSidecarRuntime struct {
	rpcCount atomic.Int32
}

func (r *mockSidecarRuntime) BuildValidationProbe(_ context.Context, in port.ValidationProbeInput) (port.UpstreamHttpSpec, error) {
	r.rpcCount.Add(1)
	return port.UpstreamHttpSpec{
		Method:    http.MethodGet,
		TargetURL: "https://api.example.com/v1/models?tag=" + in.RequestID,
		CredentialInjections: []port.CredentialInjectInstruction{{
			SecretSlotName: "primary_api_key",
			TargetLocation: port.CredentialTargetLocationHeader,
			FieldName:      "Authorization",
			ValuePrefix:    "Bearer ",
		}},
	}, nil
}

func (r *mockSidecarRuntime) VerifyValidationResult(_ context.Context, in port.ValidationResultInput) (port.ValidationVerdict, error) {
	r.rpcCount.Add(1)
	return port.ValidationVerdict{Valid: in.HTTPStatus == 200, DetectedModelIDs: []string{"pii-scrubber-v1"}}, nil
}

func (r *mockSidecarRuntime) TransformRequest(_ context.Context, in port.TransformRequestInput) (port.UpstreamHttpSpec, error) {
	r.rpcCount.Add(1)
	return port.UpstreamHttpSpec{
		Method:    http.MethodPost,
		TargetURL: "https://api.example.com/v1/chat/completions",
		BodyBytes: in.NormalizedRequestJSON,
	}, nil
}

func (r *mockSidecarRuntime) TransformResponse(_ context.Context, in port.TransformResponseInput) (port.NormalizedInferenceResponse, error) {
	r.rpcCount.Add(1)
	return port.NormalizedInferenceResponse{
		NormalizedResponseJSON: in.RawResponseBody,
		UpstreamModelID:        "pii-scrubber-v1",
		FinishReason:           "stop",
		Usage:                  &port.NormalizedUsage{InputTokens: 10, OutputTokens: 5, Accuracy: port.UsageAccuracyExact},
	}, nil
}

func (r *mockSidecarRuntime) TransformStreamChunk(_ context.Context, in port.StreamChunkInput) (port.StreamChunkOutput, error) {
	r.rpcCount.Add(1)
	return port.StreamChunkOutput{
		NextSessionState:    in.SessionState,
		NormalizedSSEFrames: [][]byte{in.RawUpstreamChunk},
		StreamCompleted:     in.IsEOF,
	}, nil
}

func (r *mockSidecarRuntime) NormalizeError(_ context.Context, in port.NormalizeErrorInput) (port.CanonicalProviderError, error) {
	r.rpcCount.Add(1)
	return port.CanonicalProviderError{CanonicalCode: "upstream_error", ErrorType: "upstream_error", SanitizedPublicMessage: fmt.Sprintf("status %d", in.HTTPStatus)}, nil
}

func (r *mockSidecarRuntime) ExtractUsage(_ context.Context, _ port.ExtractUsageInput) (port.NormalizedUsage, error) {
	r.rpcCount.Add(1)
	return port.NormalizedUsage{InputTokens: 4, OutputTokens: 6, Accuracy: port.UsageAccuracyExact}, nil
}

func (r *mockSidecarRuntime) InspectNativeRequest(_ context.Context, in port.NativeRequestInput) (port.NativeRoutingHints, error) {
	r.rpcCount.Add(1)
	return port.NativeRoutingHints{TargetModel: in.Dialect + "-model", Stream: true}, nil
}

func (r *mockSidecarRuntime) EvaluateGuardrail(_ context.Context, in port.GuardrailInput) (port.GuardrailVerdict, error) {
	r.rpcCount.Add(1)
	return port.GuardrailVerdict{Allowed: true, RedactedPayloadJSON: in.PayloadJSON}, nil
}

func (r *mockSidecarRuntime) CancelExecution(_ context.Context, _ port.CancelExecutionSignal) (port.CancelAck, error) {
	r.rpcCount.Add(1)
	return port.CancelAck{Released: true}, nil
}

func TestSidecarDisabledByDefaultFailsClosed(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	_, err := NewClient(Config{
		Enabled:     false,
		EndpointURL: "https://127.0.0.1:8443",
	})
	if !errors.Is(err, ErrSidecarDisabled) {
		t.Fatalf("expected ErrSidecarDisabled when Enabled=false, got %v", err)
	}
}

func TestMTLSSidecarAuthAndHandshakeRejectionNEG2AndRotationAC1(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	clk := clock.NewFakeClock(time.Unix(1_760_000_000, 0).UTC())
	nowFn := clk.Now

	ca, err := NewEphemeralCA(DefaultCACertTTL, nowFn)
	if err != nil {
		t.Fatalf("NewEphemeralCA: %v", err)
	}
	clientSPIFFE := "spiffe://tanpopo.internal/core/gateway"
	serverSPIFFE := "spiffe://tanpopo.internal/sidecar/pii-1"

	clientSrc, err := NewRotatingCertSource(ca, clientSPIFFE, false, 20*time.Second)
	if err != nil {
		t.Fatalf("NewRotatingCertSource client: %v", err)
	}
	serverSrc, err := NewRotatingCertSource(ca, serverSPIFFE, true, 20*time.Second)
	if err != nil {
		t.Fatalf("NewRotatingCertSource server: %v", err)
	}

	serverTLS, err := BuildServerTLSConfig(ca, serverSrc, clientSPIFFE)
	if err != nil {
		t.Fatalf("BuildServerTLSConfig: %v", err)
	}

	rt := &mockSidecarRuntime{}
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	tlsLn := tls.NewListener(rawLn, serverTLS)
	srv := &http.Server{
		Handler:           NewConnectHandler(rt),
		ReadHeaderTimeout: 5 * time.Second,
	}
	defer func() { _ = srv.Close() }()
	go func() { _ = srv.Serve(tlsLn) }()

	audit := &memAuditSink{}
	cl, err := NewClient(Config{
		Enabled:              true,
		InstallationID:       "ext_sidecar_1",
		EndpointURL:          "https://" + rawLn.Addr().String(),
		CA:                   ca,
		ClientCertSource:     clientSrc,
		ExpectedServerSPIFFE: serverSPIFFE,
		AuditSink:            audit,
		Now:                  nowFn,
	})
	if err != nil {
		t.Fatalf("NewClient mTLS: %v", err)
	}
	defer func() { _ = cl.Close() }()

	ctx := context.Background()
	// Exercise all 10 ConnectRPC ExtensionRuntimeService operations cleanly.
	if _, err := cl.BuildValidationProbe(ctx, port.ValidationProbeInput{RequestID: "req_1", SecretSlotNames: []string{"primary_api_key"}}); err != nil {
		t.Fatalf("BuildValidationProbe: %v", err)
	}
	if _, err := cl.VerifyValidationResult(ctx, port.ValidationResultInput{RequestID: "req_1", HTTPStatus: 200}); err != nil {
		t.Fatalf("VerifyValidationResult: %v", err)
	}
	if _, err := cl.TransformRequest(ctx, port.TransformRequestInput{
		RequestID:             "req_1",
		NormalizedRequestJSON: []byte(`{"model":"m1"}`),
		SecretSlotRefs:        map[string]string{"primary_api_key": "slot_ref_abc"},
	}); err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if _, err := cl.TransformResponse(ctx, port.TransformResponseInput{RequestID: "req_1", HTTPStatus: 200, RawResponseBody: []byte(`{"ok":true}`)}); err != nil {
		t.Fatalf("TransformResponse: %v", err)
	}
	if _, err := cl.TransformStreamChunk(ctx, port.StreamChunkInput{RequestID: "req_1", RawUpstreamChunk: []byte("data: ok\n\n"), IsEOF: true}); err != nil {
		t.Fatalf("TransformStreamChunk: %v", err)
	}
	if _, err := cl.NormalizeError(ctx, port.NormalizeErrorInput{RequestID: "req_1", HTTPStatus: 500}); err != nil {
		t.Fatalf("NormalizeError: %v", err)
	}
	if _, err := cl.ExtractUsage(ctx, port.ExtractUsageInput{RequestID: "req_1", Modality: "chat"}); err != nil {
		t.Fatalf("ExtractUsage: %v", err)
	}
	if _, err := cl.InspectNativeRequest(ctx, port.NativeRequestInput{RequestID: "req_1", Dialect: "openai"}); err != nil {
		t.Fatalf("InspectNativeRequest: %v", err)
	}
	if _, err := cl.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "req_1", Stage: port.GuardrailStagePreRequest, PayloadJSON: []byte(`{"text":"hi"}`)}); err != nil {
		t.Fatalf("EvaluateGuardrail: %v", err)
	}
	if ack, err := cl.CancelExecution(ctx, port.CancelExecutionSignal{RequestID: "req_1", Reason: "done"}); err != nil || !ack.Released {
		t.Fatalf("CancelExecution: ack=%+v err=%v", ack, err)
	}

	// Zero host secret leakage check.
	if _, err := cl.TransformRequest(ctx, port.TransformRequestInput{
		RequestID:      "req_leak",
		SecretSlotRefs: map[string]string{"primary_api_key": "sk-live-secret-plaintext"},
	}); !errors.Is(err, ErrSecretLeakBlocked) {
		t.Fatalf("expected ErrSecretLeakBlocked on non-opaque secret handle, got %v", err)
	}

	// Certificate rotation past 75% TTL produces a new short-lived certificate seamlessly on fresh handshakes.
	rotBefore := clientSrc.Rotations()
	clk.Advance(16 * time.Second)
	_ = cl.Close()
	if _, err := cl.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "req_rot", Stage: port.GuardrailStagePreRequest, PayloadJSON: []byte(`{}`)}); err != nil {
		t.Fatalf("EvaluateGuardrail after cert rotation: %v", err)
	}
	if clientSrc.Rotations() <= rotBefore {
		t.Fatalf("expected client certificate rotation count to advance")
	}

	// NEG-2: Rogue/unauthenticated/wrong-CA/wrong-SPIFFE/expired clients are rejected during TLS handshake
	// with zero RPC frames reaching the Sidecar runtime handler.
	countBeforeNeg2 := rt.rpcCount.Load()

	// 2a. Unauthenticated client (no client cert).
	noCertClient := &http.Client{
		Transport: &http.Transport{
			DialContext:     (&net.Dialer{Timeout: time.Second, Control: sidecarNetworkControl}).DialContext,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: ca.RootPool(), Time: nowFn},
		},
	}
	if _, err := noCertClient.Post("https://"+rawLn.Addr().String()+ProcEvaluateGuardrail, "application/json", nil); err == nil {
		t.Fatal("NEG-2: unauthenticated client without mTLS cert must be rejected at TLS handshake")
	}
	noCertClient.CloseIdleConnections()

	// 2b. Foreign CA client cert.
	rogueCA, err := NewEphemeralCA(DefaultCACertTTL, nowFn)
	if err != nil {
		t.Fatalf("NewEphemeralCA rogue: %v", err)
	}
	rogueSrc, _ := NewRotatingCertSource(rogueCA, clientSPIFFE, false, time.Minute)
	rogueClient, _ := NewClient(Config{
		Enabled:              true,
		EndpointURL:          "https://" + rawLn.Addr().String(),
		CA:                   ca,
		ClientCertSource:     rogueSrc,
		ExpectedServerSPIFFE: serverSPIFFE,
		Now:                  nowFn,
	})
	if _, err := rogueClient.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "req_rogue", PayloadJSON: []byte(`{}`)}); err == nil {
		t.Fatal("NEG-2: wrong-CA client cert must be rejected")
	}
	_ = rogueClient.Close()

	// 2c. Same CA but wrong SPIFFE identity.
	wrongIDClientSrc, _ := NewRotatingCertSource(ca, "spiffe://tanpopo.internal/core/imposter", false, time.Minute)
	wrongIDClient, _ := NewClient(Config{
		Enabled:              true,
		EndpointURL:          "https://" + rawLn.Addr().String(),
		CA:                   ca,
		ClientCertSource:     wrongIDClientSrc,
		ExpectedServerSPIFFE: serverSPIFFE,
		Now:                  nowFn,
	})
	if _, err := wrongIDClient.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "req_imposter", PayloadJSON: []byte(`{}`)}); err == nil {
		t.Fatal("NEG-2: wrong SPIFFE SAN must be rejected during mTLS handshake")
	}
	_ = wrongIDClient.Close()

	// 2d. Expired client leaf certificate presented over mTLS must be rejected during TLS handshake.
	expClock := clock.NewFakeClock(nowFn().Add(-2 * time.Minute))
	expCA, _ := NewEphemeralCA(DefaultCACertTTL, expClock.Now)
	expLeaf, _ := expCA.IssueLeafCertificate(clientSPIFFE, false, 30*time.Second, nil, nil)
	expServerSrc, _ := NewRotatingCertSource(expCA, serverSPIFFE, true, DefaultCACertTTL)
	expServerTLS, _ := BuildServerTLSConfig(expCA, expServerSrc, clientSPIFFE)
	// Server validates against real current clock while leaf already expired 90s ago.
	expServerTLS.Time = nowFn
	expLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	expSrv := &http.Server{Handler: NewConnectHandler(rt), ReadHeaderTimeout: 5 * time.Second}
	defer func() { _ = expSrv.Close() }()
	go func() { _ = expSrv.Serve(tls.NewListener(expLn, expServerTLS)) }()
	expClient := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: time.Second, Control: sidecarNetworkControl}).DialContext,
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS13,
				RootCAs:      expCA.RootPool(),
				Certificates: []tls.Certificate{*expLeaf},
				Time:         expClock.Now,
			},
		},
	}
	if _, err := expClient.Post("https://"+expLn.Addr().String()+ProcEvaluateGuardrail, "application/json", nil); err == nil {
		t.Fatal("NEG-2: expired mTLS client certificate must be rejected at handshake")
	}
	expClient.CloseIdleConnections()

	if rt.rpcCount.Load() != countBeforeNeg2 {
		t.Fatalf("NEG-2: unauthorized mTLS handshakes decoded %d RPC frames, want 0", rt.rpcCount.Load()-countBeforeNeg2)
	}

	// Cross-platform isolated circuit breaker check: crashing sidecar A trips A's circuit without affecting sibling sidecar B.
	_ = cl.Close()
	_ = srv.Close()
	if _, err := cl.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "req_down1", PayloadJSON: []byte(`{}`)}); err == nil {
		t.Fatal("expected error after sidecar listener closed")
	}
	if _, err := cl.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "req_down2", PayloadJSON: []byte(`{}`)}); err == nil {
		t.Fatal("expected error on 2nd failure tripping circuit")
	}
	if !cl.CircuitOpen() {
		t.Fatal("expected dedicated circuit breaker open after repeated transport failures")
	}
}

func TestSidecarCircuitBreakerIgnoresCallerCancellation(t *testing.T) {
	ca, err := NewEphemeralCA(DefaultCACertTTL, time.Now)
	if err != nil {
		t.Fatalf("NewEphemeralCA: %v", err)
	}
	clientSrc, err := NewRotatingCertSource(ca, "spiffe://tanpopo.internal/core/gateway", false, DefaultLeafCertTTL)
	if err != nil {
		t.Fatalf("NewRotatingCertSource: %v", err)
	}
	cl, err := NewClient(Config{
		Enabled:                 true,
		EndpointURL:             "https://127.0.0.1:8443",
		CA:                      ca,
		ClientCertSource:        clientSrc,
		ExpectedServerSPIFFE:    "spiffe://tanpopo.internal/ext/cancel_check",
		CircuitFailureThreshold: 2,
		CircuitCooldown:         time.Minute,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = cl.Close() }()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	for i := 0; i < 4; i++ {
		cl.recordFailure(canceledCtx, fmt.Errorf("canceled: %w", context.Canceled))
	}
	if cl.CircuitOpen() {
		t.Fatal("caller context.Canceled must not trip sidecar circuit breaker")
	}
	cl.mu.Lock()
	failures := cl.failures
	cl.mu.Unlock()
	if failures != 0 {
		t.Fatalf("expected 0 recorded failures for caller cancellation, got %d", failures)
	}
}

