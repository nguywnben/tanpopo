// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/sidecar"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/vault"
)

type stubSidecarRuntime struct{}

func (stubSidecarRuntime) BuildValidationProbe(_ context.Context, in port.ValidationProbeInput) (port.UpstreamHttpSpec, error) {
	return port.UpstreamHttpSpec{
		Method:    http.MethodGet,
		TargetURL: "https://api.example.com/v1/models?tag=" + in.RequestID,
	}, nil
}

func (stubSidecarRuntime) VerifyValidationResult(_ context.Context, in port.ValidationResultInput) (port.ValidationVerdict, error) {
	return port.ValidationVerdict{Valid: in.HTTPStatus == http.StatusOK}, nil
}

func (stubSidecarRuntime) TransformRequest(_ context.Context, in port.TransformRequestInput) (port.UpstreamHttpSpec, error) {
	return port.UpstreamHttpSpec{
		Method:    http.MethodPost,
		TargetURL: "https://api.example.com/v1/chat/completions",
		BodyBytes: in.NormalizedRequestJSON,
	}, nil
}

func (stubSidecarRuntime) TransformResponse(_ context.Context, in port.TransformResponseInput) (port.NormalizedInferenceResponse, error) {
	return port.NormalizedInferenceResponse{
		NormalizedResponseJSON: in.RawResponseBody,
		UpstreamModelID:        "sidecar-model",
		FinishReason:           "stop",
		Usage:                  &port.NormalizedUsage{InputTokens: 1, OutputTokens: 1, Accuracy: port.UsageAccuracyExact},
	}, nil
}

func (stubSidecarRuntime) TransformStreamChunk(_ context.Context, in port.StreamChunkInput) (port.StreamChunkOutput, error) {
	return port.StreamChunkOutput{
		NextSessionState:    in.SessionState,
		NormalizedSSEFrames: [][]byte{in.RawUpstreamChunk},
		StreamCompleted:     in.IsEOF,
	}, nil
}

func (stubSidecarRuntime) NormalizeError(_ context.Context, in port.NormalizeErrorInput) (port.CanonicalProviderError, error) {
	return port.CanonicalProviderError{CanonicalCode: "upstream_error", ErrorType: "upstream_error", SanitizedPublicMessage: fmt.Sprintf("status %d", in.HTTPStatus)}, nil
}

func (stubSidecarRuntime) ExtractUsage(_ context.Context, _ port.ExtractUsageInput) (port.NormalizedUsage, error) {
	return port.NormalizedUsage{InputTokens: 1, OutputTokens: 1, Accuracy: port.UsageAccuracyExact}, nil
}

func (stubSidecarRuntime) InspectNativeRequest(_ context.Context, in port.NativeRequestInput) (port.NativeRoutingHints, error) {
	return port.NativeRoutingHints{TargetModel: in.Dialect + "-model"}, nil
}

func (stubSidecarRuntime) EvaluateGuardrail(_ context.Context, in port.GuardrailInput) (port.GuardrailVerdict, error) {
	return port.GuardrailVerdict{Allowed: true, RedactedPayloadJSON: in.PayloadJSON}, nil
}

func (stubSidecarRuntime) CancelExecution(_ context.Context, _ port.CancelExecutionSignal) (port.CancelAck, error) {
	return port.CancelAck{Released: true}, nil
}

func TestTier3SidecarFailClosedByDefaultAndEnabledMTLS(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	h := setupIngressHarness(t, 0)
	ctx := h.ctx
	now := h.clk.Now().UnixMilli()

	// Seed a Tier-3 ISOLATED_SIDECAR_T3 adapter + connection into storage and reload snapshots.
	pkgID := "pkg_t3_sidecar"
	verID := "ver_t3_sidecar"
	instID := "inst_t3_sidecar"
	adpID := "adp_t3_sidecar"
	manifestJSON := `{"apiVersion":"tanpopo.dev/extension/v1alpha1","kind":"ExtensionManifest","metadata":{"id":"@acme/sidecar-t3","version":"1.0.0","publisher":{"id":"acme","displayName":"Acme"}},"runtime":{"tier":"ISOLATED_SIDECAR_T3","entrypoint":"https://127.0.0.1:8443","sha256":"` + strings.Repeat("a", 64) + `"},"permissions":{"executionTier":"ISOLATED_SIDECAR_T3","secretSlots":["api_key"]}}`
	permsRaw, err := json.Marshal(installer.PermissionSet{
		ExecutionTier:  installer.ExecutionTierIsolatedSidecarT3,
		AllowedHosts:   []string{"127.0.0.1"},
		AllowPrivateIP: true,
		SecretSlots:    []string{"api_key"},
	})
	if err != nil {
		t.Fatalf("marshal permissions: %v", err)
	}

	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{
			`INSERT INTO extension_packages(id,canonical_name,extension_kind,publisher_id,origin_trust_tier,created_at_ms) VALUES(?,?,?,?,?,?)`,
			[]any{pkgID, "@acme/sidecar-t3", "PROVIDER_ADAPTER", "acme", "official", now},
		},
		{
			`INSERT INTO extension_versions(id,package_id,semver,contract_version,core_version_range,execution_tier,bundle_sha256,signature_verified,signing_key_fingerprint,source_uri,manifest_json,created_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			[]any{verID, pkgID, "1.0.0", "1.0", ">=0.1.0", installer.ExecutionTierIsolatedSidecarT3, strings.Repeat("a", 64), 1, installer.PublicKeyFingerprint(h.pubPub), "builtin://sidecar-t3", manifestJSON, now},
		},
		{
			`INSERT INTO extension_installations(id,package_id,active_version_id,state,approved_permissions_json,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?)`,
			[]any{instID, pkgID, verID, "ENABLED_ACTIVE", string(permsRaw), 1, now, now},
		},
		{
			`INSERT INTO provider_adapters(id,installation_id,adapter_slug,supported_dialects_json,allowed_hosts_json,version,created_at_ms) VALUES(?,?,?,?,?,?,?)`,
			[]any{adpID, instID, "acme-sidecar-t3", `["openai"]`, `["127.0.0.1"]`, 1, now},
		},
	} {
		if _, err := h.store.RawDB().ExecContext(ctx, stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed Tier-3 DB row: %v", err)
		}
	}
	secMeta, err := h.vault.SealSecret(ctx, vault.SealSecretParams{
		OrgID:      h.org.ID,
		Name:       "t3-sidecar-key",
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte("sk-sidecar-secret"),
	})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}
	conn, err := h.catalog.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID:           h.org.ID,
		AdapterID:       adpID,
		Name:            "sidecar-conn",
		EndpointBaseURL: h.fake.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "US",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind},
		},
	})
	if err != nil {
		t.Fatalf("CreateProviderConnection: %v", err)
	}
	connID := conn.ID
	if err := h.snapshots.Refresh(ctx); err != nil {
		t.Fatalf("snapshots.Refresh: %v", err)
	}

	// 1. Default ServerConfig (SidecarEnabled: false) MUST fail closed with ErrSidecarDisabled.
	disabledSrv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots: h.snapshots,
		Broker:    h.broker,
		Audit:     h.audit,
	})
	if err != nil {
		t.Fatalf("NewServer(disabled): %v", err)
	}
	defer func() { _ = disabledSrv.Close(ctx) }()
	if _, _, err := disabledSrv.AcquireProviderRuntime(ctx, h.org.ID, connID); !errors.Is(err, sidecar.ErrSidecarDisabled) {
		t.Fatalf("expected ErrSidecarDisabled when SidecarEnabled=false, got %v", err)
	}

	// 2. When opted in (SidecarEnabled: true), AcquireProviderRuntime routes ConnectRPC mTLS over TLS 1.3.
	ca, err := sidecar.NewEphemeralCA(time.Hour, time.Now)
	if err != nil {
		t.Fatalf("NewEphemeralCA: %v", err)
	}
	hostSPIFFE := "spiffe://tanpopo.internal/core/gateway"
	sidecarSPIFFE := "spiffe://tanpopo.internal/ext/" + instID
	serverSrc, err := sidecar.NewRotatingCertSource(ca, sidecarSPIFFE, true, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewRotatingCertSource(server): %v", err)
	}
	clientSrc, err := sidecar.NewRotatingCertSource(ca, hostSPIFFE, false, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewRotatingCertSource(client): %v", err)
	}
	srvTLS, err := sidecar.BuildServerTLSConfig(ca, serverSrc, hostSPIFFE)
	if err != nil {
		t.Fatalf("BuildServerTLSConfig: %v", err)
	}
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	tlsLn := tls.NewListener(rawLn, srvTLS)
	sidecarHTTP := &http.Server{
		Handler:           sidecar.NewConnectHandler(stubSidecarRuntime{}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = sidecarHTTP.Serve(tlsLn) }()
	defer func() { _ = sidecarHTTP.Close() }()
	sidecarURL := "https://" + rawLn.Addr().String()

	enabledSrv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:      h.snapshots,
		Broker:         h.broker,
		Audit:          h.audit,
		SidecarEnabled: true,
		SidecarRuntimeFactory: func(_ context.Context, adp *snapshot.ProviderAdapterView, _ installer.PermissionSet) (port.ProviderRuntime, io.Closer, error) {
			sc, cerr := sidecar.NewClient(sidecar.Config{
				Enabled:              true,
				InstallationID:       adp.InstallationID,
				EndpointURL:          sidecarURL,
				CA:                   ca,
				ClientCertSource:     clientSrc,
				ExpectedServerSPIFFE: sidecarSPIFFE,
			})
			if cerr != nil {
				return nil, nil, cerr
			}
			return sc, sc, nil
		},
	})
	if err != nil {
		t.Fatalf("NewServer(enabled): %v", err)
	}

	rt, release, err := enabledSrv.AcquireProviderRuntime(ctx, h.org.ID, connID)
	if err != nil {
		t.Fatalf("AcquireProviderRuntime(enabled): %v", err)
	}
	spec, err := rt.TransformRequest(ctx, port.TransformRequestInput{
		RequestID:             "req_sidecar_test",
		NormalizedRequestJSON: []byte(`{"model":"gpt-t3-sidecar"}`),
	})
	release()
	if err != nil {
		t.Fatalf("TransformRequest via ConnectRPC mTLS sidecar: %v", err)
	}
	if spec.TargetURL != "https://api.example.com/v1/chat/completions" || string(spec.BodyBytes) != `{"model":"gpt-t3-sidecar"}` {
		t.Fatalf("unexpected TransformRequest spec: %+v", spec)
	}
	if err := enabledSrv.Close(ctx); err != nil {
		t.Fatalf("enabledSrv.Close: %v", err)
	}
}

func TestTier3SidecarRejectsEscapedUDSSocketPath(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	h := setupIngressHarness(t, 0)
	ctx := h.ctx
	now := h.clk.Now().UnixMilli()

	socketDir := t.TempDir()
	outsideDir := t.TempDir()
	symlinkEscape := filepath.Join(socketDir, "escape")
	if err := os.Symlink(outsideDir, symlinkEscape); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	cases := []struct {
		name       string
		socketDir  string
		entrypoint string
	}{
		{name: "unconfigured_socket_dir", socketDir: "", entrypoint: "unix://" + filepath.Join(socketDir, "a.sock")},
		{name: "absolute_outside_dir", socketDir: socketDir, entrypoint: "unix://" + filepath.Join(outsideDir, "a.sock")},
		{name: "dotdot_traversal", socketDir: socketDir, entrypoint: "unix://" + filepath.Join(socketDir, "..", filepath.Base(outsideDir), "a.sock")},
		{name: "symlink_escape", socketDir: socketDir, entrypoint: "unix://" + filepath.Join(symlinkEscape, "a.sock")},
	}

	for i, tc := range cases {
		pkgID := fmt.Sprintf("pkg_t3_uds_%d", i)
		verID := fmt.Sprintf("ver_t3_uds_%d", i)
		instID := fmt.Sprintf("inst_t3_uds_%d", i)
		adpID := fmt.Sprintf("adp_t3_uds_%d", i)

		epJSON, _ := json.Marshal(tc.entrypoint)
		manifestJSON := `{"apiVersion":"tanpopo.dev/extension/v1alpha1","kind":"ExtensionManifest","metadata":{"id":"@acme/sidecar-uds","version":"1.0.0","publisher":{"id":"acme","displayName":"Acme"}},"runtime":{"tier":"ISOLATED_SIDECAR_T3","entrypoint":` + string(epJSON) + `,"sha256":"` + strings.Repeat("b", 64) + `"},"permissions":{"executionTier":"ISOLATED_SIDECAR_T3"}}`
		permsRaw, _ := json.Marshal(installer.PermissionSet{
			ExecutionTier:  installer.ExecutionTierIsolatedSidecarT3,
			AllowedHosts:   []string{"127.0.0.1"},
			AllowPrivateIP: true,
		})
		for _, stmt := range []struct {
			query string
			args  []any
		}{
			{`INSERT INTO extension_packages(id,canonical_name,extension_kind,publisher_id,origin_trust_tier,created_at_ms) VALUES(?,?,?,?,?,?)`, []any{pkgID, "@acme/sidecar-uds-" + fmt.Sprint(i), "PROVIDER_ADAPTER", "acme", "official", now}},
			{`INSERT INTO extension_versions(id,package_id,semver,contract_version,core_version_range,execution_tier,bundle_sha256,signature_verified,signing_key_fingerprint,source_uri,manifest_json,created_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, []any{verID, pkgID, "1.0.0", "1.0", ">=0.1.0", installer.ExecutionTierIsolatedSidecarT3, strings.Repeat("b", 64), 1, installer.PublicKeyFingerprint(h.pubPub), "builtin://sidecar-uds", manifestJSON, now}},
			{`INSERT INTO extension_installations(id,package_id,active_version_id,state,approved_permissions_json,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?)`, []any{instID, pkgID, verID, "ENABLED_ACTIVE", string(permsRaw), 1, now, now}},
			{`INSERT INTO provider_adapters(id,installation_id,adapter_slug,supported_dialects_json,allowed_hosts_json,version,created_at_ms) VALUES(?,?,?,?,?,?,?)`, []any{adpID, instID, "acme-uds-" + fmt.Sprint(i), `["openai"]`, `["127.0.0.1"]`, 1, now}},
		} {
			if _, err := h.store.RawDB().ExecContext(ctx, stmt.query, stmt.args...); err != nil {
				t.Fatalf("%s seed DB: %v", tc.name, err)
			}
		}
		conn, err := h.catalog.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
			OrgID:           h.org.ID,
			AdapterID:       adpID,
			Name:            "uds-conn-" + fmt.Sprint(i),
			EndpointBaseURL: h.fake.URL(),
			AllowPrivateIP:  true,
			AllowedCIDRs:    []string{"127.0.0.1/32"},
			DataRegion:      "US",
			BillingMode:     catalog.BillingModeCommercialAPI,
			AuthMode:        catalog.AuthModeNone,
		})
		if err != nil {
			t.Fatalf("%s CreateProviderConnection: %v", tc.name, err)
		}
		if err := h.snapshots.Refresh(ctx); err != nil {
			t.Fatalf("%s Refresh: %v", tc.name, err)
		}

		srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots:        h.snapshots,
			Broker:           h.broker,
			Audit:            h.audit,
			SidecarEnabled:   true,
			SidecarSocketDir: tc.socketDir,
		})
		if err != nil {
			t.Fatalf("%s NewServer: %v", tc.name, err)
		}
		_, _, err = srv.AcquireProviderRuntime(ctx, h.org.ID, conn.ID)
		_ = srv.Close(ctx)
		if !errors.Is(err, port.ErrInvalidSpec) {
			t.Fatalf("%s: expected port.ErrInvalidSpec for UDS escape %q, got %v", tc.name, tc.entrypoint, err)
		}
	}
}

