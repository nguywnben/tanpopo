// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package awsbedrock_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

const (
	awsHost = "bedrock-runtime.us-east-1.amazonaws.com"
	connID  = "conn_bedrock_contract"
)

type bedrockCassette struct {
	Config                   map[string]any `yaml:"config"`
	NormalizedInput          map[string]any `yaml:"normalized_input"`
	UpstreamRecordedExchange struct {
		RawBody      string   `yaml:"raw_body"`
		RawSSEChunks []string `yaml:"raw_sse_chunks"`
	} `yaml:"upstream_recorded_exchange"`
}

type bedrockResolver struct {
	orgID     string
	connID    string
	secretID  string
	authority string
	policy    egress.Policy
}

func (r bedrockResolver) ResolveSlot(_ context.Context, requestedOrg, requestedConn, slot string) (*broker.ResolvedSlot, error) {
	if requestedOrg != r.orgID || requestedConn != r.connID || slot != "aws_credentials" {
		return nil, port.ErrInvalidSpec
	}
	audience := "https://" + r.authority
	return &broker.ResolvedSlot{OrgID: r.orgID, ConnectionID: r.connID, SlotName: slot, SecretID: r.secretID,
		InjectMode: broker.InjectModeHostCryptoSign, BoundAudience: &audience, AllowedHosts: []string{r.authority},
		AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, RequireTLS: true}, nil
}

func (r bedrockResolver) ResolveConnectionPolicy(_ context.Context, requestedOrg, requestedConn string) (*egress.Policy, error) {
	if requestedOrg != r.orgID || requestedConn != r.connID {
		return nil, port.ErrInvalidSpec
	}
	p := r.policy
	return &p, nil
}

type bedrockAudit struct{}

func (bedrockAudit) Append(context.Context, storageport.AuditEvent) error { return nil }

type bedrockHostResolver struct{}

func TestMain(m *testing.M) { os.Exit(netguard.MustRunHermeticMain(m)) }

func TestBedrockOfflineCassetteContractHarness(t *testing.T) {
	report, err := testharness.TestExtension(context.Background(), testharness.TestOptions{Dir: "."})
	if err != nil {
		t.Fatalf("TestExtension: %v (report=%+v)", err, report)
	}
	if report.Status != "PASSED" || report.CasesPassed != 9 || report.CasesFailed != 0 {
		t.Fatalf("unexpected Bedrock cassette report: %+v", report)
	}
	packDir := t.TempDir()
	first, err := testharness.PackExtension(context.Background(), ".", filepath.Join(packDir, "bedrock-a.tpkg"))
	if err != nil {
		t.Fatalf("PackExtension first build: %v", err)
	}
	second, err := testharness.PackExtension(context.Background(), ".", filepath.Join(packDir, "bedrock-b.tpkg"))
	if err != nil {
		t.Fatalf("PackExtension second build: %v", err)
	}
	if first.SHA256 == "" || first.SHA256 != second.SHA256 || first.EntryCount < 10 {
		t.Fatalf("Bedrock package is not deterministic or unexpectedly small: first=%+v second=%+v", first, second)
	}
}

func (bedrockHostResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if host != awsHost {
		return nil, fmt.Errorf("unexpected host lookup %q", host)
	}
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func TestBedrockCassetteUsesBlindBrokerSigV4CanonicalAuthorization(t *testing.T) {
	ctx := context.Background()
	var cassette bedrockCassette
	rawCassette, err := os.ReadFile("cassettes/chat_sync.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(rawCassette, &cassette); err != nil {
		t.Fatal(err)
	}
	var authorization string
	var capturedDate string
	server := newBedrockTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		capturedDate = r.Header.Get("X-Amz-Date")
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/converse") || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected signed request: %s %s headers=%v", r.Method, r.URL, r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cassette.UpstreamRecordedExchange.RawBody))
	})
	defer server.Close()
	authority := awsHost
	cassette.Config["endpoint_base_url"] = "https://" + authority

	validation, vm, wasmBytes, err := testharness.ValidateExtensionDir(ctx, ".")
	if err != nil {
		t.Fatalf("ValidateExtensionDir: %v", err)
	}
	if validation.ExtensionID != "@tanpopo/aws-bedrock" {
		t.Fatalf("unexpected package id %q", validation.ExtensionID)
	}
	var slots []string
	for _, s := range vm.Manifest.SecretSlots {
		slots = append(slots, s.Slot)
	}
	runtime, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: wasmBytes, InstallationID: "test:aws-bedrock", PoolSize: 1,
		MemoryLimitPages: wasm.DefaultMemoryLimitPages, ABIMode: wasm.ABIModeCanonicalEnvelope,
		AllowedHosts: vm.Manifest.Permissions.NetworkEgress.AllowedHosts, ApprovedSecretSlots: slots})
	if err != nil {
		t.Fatalf("wasm.NewRuntime: %v", err)
	}
	defer runtime.Close(ctx)

	cfgJSON, err := json.Marshal(cassette.Config)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON, err := json.Marshal(cassette.NormalizedInput)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := runtime.TransformRequest(ctx, port.TransformRequestInput{RequestID: "bedrock-cassette", ConfigJSON: string(cfgJSON),
		NormalizedRequestJSON: requestJSON, SecretSlotRefs: map[string]string{"aws_credentials": "slot_ref:aws_credentials"}})
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("invalid transformed request: %v", err)
	}
	if !strings.HasPrefix(spec.TargetURL, "https://"+authority+"/model/") || !strings.HasSuffix(spec.TargetURL, "/converse") {
		t.Fatalf("unexpected Bedrock target %q", spec.TargetURL)
	}
	var body map[string]any
	if err := json.Unmarshal(spec.BodyBytes, &body); err != nil {
		t.Fatalf("invalid Converse body %q: %v", spec.BodyBytes, err)
	}
	if body["system"] == nil || body["messages"] == nil || body["inferenceConfig"] == nil {
		t.Fatalf("system/messages/inferenceConfig translation missing: %s", spec.BodyBytes)
	}
	if strings.Contains(string(spec.BodyBytes), "__CANARY_SECRET__") {
		t.Fatal("cassette credential reached the adapter request body")
	}
	if len(spec.CredentialInjections) != 1 {
		t.Fatalf("expected one host signing instruction, got %+v", spec.CredentialInjections)
	}
	injection := spec.CredentialInjections[0]
	if injection.SecretSlotName != "aws_credentials" || injection.TargetLocation != port.CredentialTargetLocationHostCryptoSign ||
		injection.FieldName != "Authorization" || injection.CryptoSign == nil || injection.CryptoSign.Algorithm != port.SigningAlgorithmAWSSigV4 ||
		injection.CryptoSign.AWSRegion != "us-east-1" || injection.CryptoSign.AWSService != "bedrock" {
		t.Fatalf("adapter did not emit the narrow AWS SigV4 host instruction: %+v", injection)
	}

	const credentials = `{"access_key_id":"AKIDEXAMPLE","secret_access_key":"wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}`
	// Keep the signed authority canonical while routing the HTTPS socket only to loopback.
	store, secretID, testOrgID := newBedrockTestVault(t, credentials, authority)
	defer store.Close()
	policy := egress.Policy{AllowedHosts: []string{authority}, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, RequireTLS: true}
	resolver := bedrockResolver{orgID: testOrgID, connID: connID, secretID: secretID, authority: authority, policy: policy}
	clock := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	blindBroker, err := broker.NewBlindBroker(broker.Config{Resolver: resolver, PolicyResolver: resolver, Vault: store,
		UnsealCap: vault.MintUnsealCapabilityForBroker(), Audit: bedrockAudit{}, NowMs: func() int64 { return clock.UnixMilli() },
		HostResolver: bedrockHostResolver{}, ControlHook: netguard.Control})
	if err != nil {
		t.Fatalf("NewBlindBroker: %v", err)
	}
	brokerInjection := broker.CredentialInjection{SecretSlotName: injection.SecretSlotName, Location: broker.TargetHostCryptoSign,
		FieldName: injection.FieldName, CryptoSign: &broker.CryptoSignSpec{Algorithm: broker.SigningAWSSigV4,
			AWSRegion: injection.CryptoSign.AWSRegion, AWSService: injection.CryptoSign.AWSService,
			SignedHeaders: injection.CryptoSign.SignedHeaders}}
	resp, err := blindBroker.Execute(ctx, broker.EgressRequest{OrgID: testOrgID, ConnectionID: connID, Method: spec.Method,
		TargetURL: spec.TargetURL, SafeHeaders: spec.SafeHeaders, BodyBytes: spec.BodyBytes, CredentialInjections: []broker.CredentialInjection{brokerInjection}})
	if err != nil {
		t.Fatalf("BlindBroker.Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cassette response status = %d", resp.StatusCode)
	}
	if !strings.HasPrefix(authorization, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20261119/us-east-1/bedrock/aws4_request, SignedHeaders=") ||
		!strings.Contains(authorization, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date, Signature=") ||
		!regexp.MustCompile(`Signature=[0-9a-f]{64}$`).MatchString(authorization) {
		t.Fatalf("BlindBroker emitted a noncanonical SigV4 Authorization value: %q", authorization)
	}
	if capturedDate != "20261119T120000Z" || strings.Contains(authorization, "wJalrXUtnFEMI") || strings.Contains(string(spec.BodyBytes), "AKIDEXAMPLE") {
		t.Fatalf("host clock or credential isolation invariant failed: date=%q auth=%q", capturedDate, authorization)
	}

	normalized, err := runtime.TransformResponse(ctx, port.TransformResponseInput{RequestID: "bedrock-cassette", HTTPStatus: 200,
		RawResponseBody: []byte(cassette.UpstreamRecordedExchange.RawBody), Modality: "text"})
	if err != nil {
		t.Fatalf("TransformResponse: %v", err)
	}
	var normalizedJSON map[string]any
	if err := json.Unmarshal(normalized.NormalizedResponseJSON, &normalizedJSON); err != nil {
		t.Fatalf("normalized response: %v", err)
	}
	choices, _ := normalizedJSON["choices"].([]any)
	if len(choices) != 1 || normalized.Usage == nil || normalized.Usage.InputTokens != 18 || normalized.Usage.OutputTokens != 3 {
		t.Fatalf("unexpected normalized Bedrock response: %+v", normalized)
	}
}

func TestBedrockEventStreamCassetteNormalizesSplitFramesAndUsage(t *testing.T) {
	ctx := context.Background()
	rawCassette, err := os.ReadFile("cassettes/chat_stream.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cassette bedrockCassette
	if err := yaml.Unmarshal(rawCassette, &cassette); err != nil {
		t.Fatal(err)
	}
	_, vm, wasmBytes, err := testharness.ValidateExtensionDir(ctx, ".")
	if err != nil {
		t.Fatalf("ValidateExtensionDir: %v", err)
	}
	runtime, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: wasmBytes, InstallationID: "test:aws-bedrock-stream", PoolSize: 1,
		MemoryLimitPages: wasm.DefaultMemoryLimitPages, ABIMode: wasm.ABIModeCanonicalEnvelope,
		AllowedHosts: vm.Manifest.Permissions.NetworkEgress.AllowedHosts, ApprovedSecretSlots: []string{"aws_credentials"}})
	if err != nil {
		t.Fatalf("wasm.NewRuntime: %v", err)
	}
	defer runtime.Close(ctx)
	cfgJSON, err := json.Marshal(cassette.Config)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON, err := json.Marshal(cassette.NormalizedInput)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := runtime.TransformRequest(ctx, port.TransformRequestInput{RequestID: "bedrock-eventstream", ConfigJSON: string(cfgJSON),
		NormalizedRequestJSON: requestJSON, SecretSlotRefs: map[string]string{"aws_credentials": "slot_ref:aws_credentials"}})
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if !strings.HasSuffix(spec.TargetURL, "/converse-stream") || spec.SafeHeaders["Accept"] != "application/vnd.amazon.eventstream" {
		t.Fatalf("stream request contract mismatch: target=%q headers=%v", spec.TargetURL, spec.SafeHeaders)
	}
	var state []byte
	var frames [][]byte
	for _, encoded := range cassette.UpstreamRecordedExchange.RawSSEChunks {
		chunk, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "base64:"))
		if err != nil {
			t.Fatalf("decode EventStream cassette chunk: %v", err)
		}
		out, err := runtime.TransformStreamChunk(ctx, port.StreamChunkInput{RequestID: "bedrock-eventstream", SessionState: state, RawUpstreamChunk: chunk})
		if err != nil {
			t.Fatalf("TransformStreamChunk: %v", err)
		}
		state = out.NextSessionState
		frames = append(frames, out.NormalizedSSEFrames...)
	}
	out, err := runtime.TransformStreamChunk(ctx, port.StreamChunkInput{RequestID: "bedrock-eventstream", SessionState: state, IsEOF: true})
	if err != nil {
		t.Fatalf("TransformStreamChunk EOF: %v", err)
	}
	frames = append(frames, out.NormalizedSSEFrames...)
	if !out.StreamCompleted || out.IncrementalUsage == nil || out.IncrementalUsage.InputTokens != 12 || out.IncrementalUsage.OutputTokens != 4 {
		t.Fatalf("EventStream completion/usage mismatch: %+v", out)
	}
	joined := string(bytes.Join(frames, nil))
	if !strings.Contains(joined, "Hello from Bedrock") || !strings.Contains(joined, `"finish_reason":"stop"`) || !strings.Contains(joined, "[DONE]") {
		t.Fatalf("normalized EventStream is missing text, stop, or terminator: %s", joined)
	}
}

func TestBedrockToolEventStreamCassettePreservesIncrementalArguments(t *testing.T) {
	ctx := context.Background()
	rawCassette, err := os.ReadFile("cassettes/tool_stream.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cassette bedrockCassette
	if err := yaml.Unmarshal(rawCassette, &cassette); err != nil {
		t.Fatal(err)
	}
	_, vm, wasmBytes, err := testharness.ValidateExtensionDir(ctx, ".")
	if err != nil {
		t.Fatalf("ValidateExtensionDir: %v", err)
	}
	runtime, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: wasmBytes, InstallationID: "test:aws-bedrock-tool-stream", PoolSize: 1,
		MemoryLimitPages: wasm.DefaultMemoryLimitPages, ABIMode: wasm.ABIModeCanonicalEnvelope,
		AllowedHosts: vm.Manifest.Permissions.NetworkEgress.AllowedHosts, ApprovedSecretSlots: []string{"aws_credentials"}})
	if err != nil {
		t.Fatalf("wasm.NewRuntime: %v", err)
	}
	defer runtime.Close(ctx)
	var state []byte
	var frames [][]byte
	for _, encoded := range cassette.UpstreamRecordedExchange.RawSSEChunks {
		chunk, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "base64:"))
		if err != nil {
			t.Fatalf("decode EventStream cassette chunk: %v", err)
		}
		out, err := runtime.TransformStreamChunk(ctx, port.StreamChunkInput{RequestID: "bedrock-tool-stream", SessionState: state, RawUpstreamChunk: chunk})
		if err != nil {
			t.Fatalf("TransformStreamChunk: %v", err)
		}
		state = out.NextSessionState
		frames = append(frames, out.NormalizedSSEFrames...)
	}
	out, err := runtime.TransformStreamChunk(ctx, port.StreamChunkInput{RequestID: "bedrock-tool-stream", SessionState: state, IsEOF: true})
	if err != nil {
		t.Fatalf("TransformStreamChunk EOF: %v", err)
	}
	frames = append(frames, out.NormalizedSSEFrames...)
	joined := string(bytes.Join(frames, nil))
	if !out.StreamCompleted || !strings.Contains(joined, "tooluse_stream_1") || !strings.Contains(joined, "get_weather") ||
		!strings.Contains(joined, `"arguments":"{\"city\":"`) || !strings.Contains(joined, `"arguments":"\"Oslo\"}"`) {
		t.Fatalf("tool EventStream deltas were not preserved: complete=%v frames=%s", out.StreamCompleted, joined)
	}
}

func newBedrockTestVault(t *testing.T, credential, authority string) (*vault.Store, string, string) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "bedrock.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	keyBytes := make([]byte, vault.KeySizeBytes)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatal(err)
	}
	key, err := vault.NewStaticKEKProvider("bedrock-test", keyBytes)
	clear(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(key.Close)
	store, err := vault.NewStore(vault.Config{DB: db.RawDB(), KEK: key, PostShredHook: db.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	org, err := db.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "bedrock-contract", DisplayName: "Bedrock Contract"})
	if err != nil {
		t.Fatal(err)
	}
	audience := "https://" + authority
	meta, err := store.SealSecret(ctx, vault.SealSecretParams{OrgID: org.ID, Name: "bedrock-test-key", SecretKind: vault.SecretKindAWSSigV4Key,
		BoundAudience: &audience, Plaintext: []byte(credential)})
	if err != nil {
		t.Fatal(err)
	}
	return store, meta.ID, org.ID
}

func newBedrockTLSServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: awsHost}, DNSNames: []string{awsHost},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	trustPath := filepath.Join(t.TempDir(), "bedrock-root.pem")
	if err := os.WriteFile(trustPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", trustPath)
	server := httptest.NewUnstartedServer(handler)
	server.Listener, err = net.Listen("tcp", "127.0.0.1:443")
	if err != nil {
		t.Fatal(err)
	}
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	return server
}
