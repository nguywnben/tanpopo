// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func TestParseStrictRerankRequest(t *testing.T) {
	req, err := parseStrictRerankRequest([]byte(`{"model":"ranker","query":"hello","documents":["one",{"id":"d2","text":"two"}],"top_n":1,"return_documents":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "ranker" || len(req.Documents) != 2 || req.Documents[1].ID != "d2" || req.TopN == nil || *req.TopN != 1 || !req.ReturnDocuments {
		t.Fatalf("unexpected parsed request: %+v", req)
	}
	for _, raw := range []string{
		`{"model":"ranker","query":"q","documents":[]}`,
		`{"model":"ranker","query":"q","documents":[""]}`,
		`{"model":"ranker","query":"q","documents":["x"],"top_n":2}`,
		`{"model":"ranker","query":"q","documents":["x"],"extra":true}`,
		`{"model":"ranker","query":"q","documents":[{"text":"x","unknown":1}]}`,
		`{"model":"ranker","query":"q","documents":["x"],"return_documents":"yes"}`,
		`{"model":"ranker","query":"q","documents":["x"],"model":"other"}`,
	} {
		if _, err := parseStrictRerankRequest([]byte(raw)); err == nil {
			t.Errorf("invalid rerank input accepted: %s", raw)
		}
	}
}

func TestFinalizeRerankJSONValidatesAndReturnsRequestedDocuments(t *testing.T) {
	req, err := parseStrictRerankRequest([]byte(`{"model":"ranker","query":"q","documents":["first",{"text":"second"}],"top_n":1,"return_documents":true}`))
	if err != nil {
		t.Fatal(err)
	}
	response, err := finalizeRerankJSON(req.Model, req, port.NormalizedInferenceResponse{NormalizedResponseJSON: []byte(`{"id":"rr-1","results":[{"index":1,"relevance_score":0.91}]}`), UpstreamModelID: "ranker-v1", Usage: &port.NormalizedUsage{InputTokens: 3, OutputTokens: 2, MediaUsage: &port.MediaUsage{RerankSearchUnits: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model   string           `json:"model"`
		Usage   map[string]int64 `json:"usage"`
		Results []struct {
			Index    int               `json:"index"`
			Document map[string]string `json:"document"`
		} `json:"results"`
	}
	if err := json.Unmarshal(response, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "ranker-v1" || len(got.Results) != 1 || got.Results[0].Index != 1 || got.Results[0].Document["text"] != "second" || got.Usage["search_units"] != 4 || got.Usage["total_tokens"] != 5 {
		t.Fatalf("unexpected normalized rerank response: %s", response)
	}
	for _, raw := range []string{
		`{"id":"rr","results":[{"index":3,"relevance_score":0.5}]}`,
		`{"id":"rr","results":[{"index":1,"relevance_score":0.5},{"index":1,"relevance_score":0.4}]}`,
		`{"id":"rr","results":[{"index":1,"relevance_score":1e999}]}`,
		`{"id":"rr","results":[{"index":1,"relevance_score":0.5},{"index":0,"relevance_score":0.4}]}`,
	} {
		if _, err := finalizeRerankJSON(req.Model, req, port.NormalizedInferenceResponse{NormalizedResponseJSON: []byte(raw)}); err == nil {
			t.Errorf("invalid normalized response accepted: %s", raw)
		}
	}
}

func TestHostEgressExpandsAzureManifestSuffixOnlyToExactOperatorApprovedHost(t *testing.T) {
	perms := installer.PermissionSet{
		AllowedHosts:         []string{"tanpopo-resource.openai.azure.com"},
		EndpointHostSuffixes: []string{".openai.azure.com"},
		RequireTLS:           true,
	}
	adapter := &snapshot.ProviderAdapterView{AllowedHosts: []string{"customer-resource.openai.azure.com"}}
	connection := &snapshot.ProviderConnectionView{EndpointBaseURL: "https://customer-resource.openai.azure.com"}
	policy := buildHostNarrowPolicy(perms, adapter, connection)
	if len(policy.AllowedHosts) != 1 || policy.AllowedHosts[0] != "customer-resource.openai.azure.com" || !policy.RequireTLS {
		t.Fatalf("suffix did not become only the exact adapter/connection host: %+v", policy)
	}
	connection.EndpointBaseURL = "https://customer-resource.openai.azure.com.attacker.test"
	policy = buildHostNarrowPolicy(perms, adapter, connection)
	if len(policy.AllowedHosts) != 0 {
		t.Fatalf("suffix lookalike widened egress: %+v", policy.AllowedHosts)
	}
}

func TestRerankRouteAuthenticatesTransformsNormalizesAndReportsSearchUnits(t *testing.T) {
	ctx := context.Background()
	clockNow := time.UnixMilli(1760000200000).UTC()
	nowMs := func() int64 { return clockNow.UnixMilli() }
	workDir := t.TempDir()
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(workDir, "tanpopo.db"), NowMs: nowMs})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := store.RawDB()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db, NowMs: nowMs})
	if err != nil {
		t.Fatal(err)
	}

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v2/rerank" {
			t.Errorf("upstream request = %s %s, want POST /v2/rerank", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer rerank-provider-secret" {
			t.Errorf("provider Authorization = %q, want host-blind injected credential", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read transformed provider request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode transformed provider request %q: %v", body, err)
		} else if request["query"] != "tanpopo purpose" || request["model"] != "rerank-v3.5" {
			t.Errorf("provider received unexpected transformed request: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"provider-rerank-1","model":"rerank-v3.5","results":[{"index":1,"relevance_score":0.93,"document":{"text":"provider copy"}}],"billed_units":{"search_units":3}}`)
	}))
	defer upstream.Close()

	publisherPublic, publisherPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := trust.TrustedKeyring{PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": publisherPublic}}
	manager, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db, NowMs: nowMs, Keyring: keyring})
	if err != nil {
		t.Fatal(err)
	}
	masterKey := []byte("0123456789abcdef0123456789abcdef")
	kek, err := vault.NewStaticKEKProvider("rerank-test-kek", masterKey)
	if err != nil {
		t.Fatal(err)
	}
	defer kek.Close()
	vaultStore, err := vault.NewStore(vault.Config{DB: db, KEK: kek, NowMs: nowMs, PostShredHook: store.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer vaultStore.Close()
	catalogRepo, err := catalog.NewRepository(catalog.Config{DB: db, NowMs: nowMs, Snapshots: manager, Keyring: installer.TrustedKeyring{PublisherPublicKeys: keyring.PublisherPublicKeys}, Audit: auditLog})
	if err != nil {
		t.Fatal(err)
	}
	blindBroker, err := broker.NewBlindBroker(broker.Config{
		Resolver: catalogRepo, PolicyResolver: catalogRepo, Vault: vaultStore,
		UnsealCap: vault.MintUnsealCapabilityForBroker(), Audit: auditLog, NowMs: nowMs,
	})
	if err != nil {
		t.Fatal(err)
	}

	org, err := store.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "rerank-test-org", DisplayName: "Rerank Test Org", MinPluginTrust: storageport.MinTrustVerifiedOnly})
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "rerank-test-project", DisplayName: "Rerank Test Project"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := store.Tenancy().CreateEnvironment(ctx, org.ID, project.ID, storageport.CreateEnvironmentParams{Slug: "test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms) VALUES ('prc_rerank_operator', NULL, 'HUMAN_LOCAL', 'rerank-operator@example.test', 'ACTIVE', 1, ?, ?)`, []any{nowMs(), nowMs()}},
		{`INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms) VALUES ('rb_rerank_operator', NULL, 'prc_rerank_operator', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)`, []any{nowMs()}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	pluginsDir := filepath.Join(workDir, "plugins")
	lifecycle, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB: db, Audit: auditLog, PluginsDir: pluginsDir, LockfilePath: filepath.Join(workDir, "tanpopo.lock"),
		Keyring: installer.TrustedKeyring{PublisherPublicKeys: keyring.PublisherPublicKeys}, NowMs: nowMs,
	})
	if err != nil {
		t.Fatal(err)
	}
	extensionDir := filepath.Join(workDir, "rerank-adapter")
	if err := os.MkdirAll(extensionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const adapterManifest = `apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@acme/rerank-route-test"
  name: "Rerank Route Test Adapter"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "rerank-route-test"
    supportedDialects: ["cohere_rerank_v2"]
    authMethods:
      - id: "provider_api_key"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["provider_api_key"]
permissions:
  networkEgress:
    allowedHosts: ["127.0.0.1"]
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:transform_request"
    - "provider:transform_response"
adapter:
  slug: "rerank-route-test"
  wireProtocol: "openai_chat_v1"
  supportedDialects: ["cohere_rerank_v2"]
  auth:
    - id: "provider_api_key"
      secretSlot: "api_key"
      mode: "host_blind_inject"
      headerName: "Authorization"
      valuePrefix: "Bearer "
  requestMapping:
    method: "POST"
    rerankPathTemplate: "/v2/rerank"
  responseMapping:
    modelPath: "model"
    rerankIdPath: "id"
    rerankResultsPath: "results"
    rerankIndexPath: "index"
    rerankScorePath: "relevance_score"
    rerankDocumentPath: "document"
    usagePaths:
      rerankSearchUnits: "billed_units.search_units"
`
	if err := os.WriteFile(filepath.Join(extensionDir, "manifest.yaml"), []byte(adapterManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(workDir, "rerank-adapter.tpkg")
	packed, err := testharness.PackExtension(ctx, extensionDir, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	installation, err := lifecycle.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchivePath: archivePath,
		Source:      installer.SourceSpec{Kind: installer.SourceKindLocalArchive, URI: "./rerank-adapter.tpkg", ExpectedSHA256: packed.SHA256},
		Signature:   installer.ProvenanceSignature{PublisherOrRootSig: ed25519.Sign(publisherPrivate, installer.SignatureSigningPayload("@acme/rerank-route-test", "1.0.0", packed.SHA256))},
		ActorID:     "prc_rerank_operator", TargetOrgID: org.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := lifecycle.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID: installation.Installation.InstallationID, ExpectedVersion: installation.Installation.Version,
		ApproverPrincipalID: "prc_rerank_operator", ActivateImmediately: true, OrgID: org.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := catalogRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
		InstallationID: active.InstallationID, ExpectedActiveVersionID: *active.ActiveVersionID,
		AdapterSlug: "rerank-route-test", SupportedDialects: []string{"cohere_rerank_v2"}, AllowedHosts: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := vaultStore.SealSecret(ctx, vault.SealSecretParams{OrgID: org.ID, Name: "rerank-provider-key", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte("rerank-provider-secret")})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := catalogRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID: org.ID, AdapterID: adapter.ID, Name: "rerank-local-provider", EndpointBaseURL: upstream.URL,
		AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, DataRegion: "test",
		BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secret.ID, InjectMode: catalog.InjectModeHostBlind}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err = catalogRepo.TransitionConnectionState(ctx, org.ID, connection.ID, catalog.ConnStateValidating, connection.Version, "test validation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalogRepo.TransitionConnectionState(ctx, org.ID, connection.ID, catalog.ConnStateActive, connection.Version, "test activation"); err != nil {
		t.Fatal(err)
	}
	model, err := catalogRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{
		OrgID: &org.ID, CanonicalSlug: "acme/rerank-v1", ModalityKind: catalog.ModalityRerank,
		ContextWindow: 8192, MaxOutputTokens: 512, BaseCapabilities: map[string]string{"rerank": catalog.AssuranceTested},
	})
	if err != nil {
		t.Fatal(err)
	}
	nativeDialect := "cohere_rerank_v2"
	_, err = catalogRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
		OrgID: org.ID, ConnectionID: connection.ID, LogicalModelID: model.ID,
		UpstreamModelName: "rerank-v3.5", NativeDialect: &nativeDialect,
		CapabilityOverrides: map[string]string{"rerank": catalog.AssuranceTested}, AssuranceLevel: catalog.AssuranceTested,
		Pricing: snapshot.PricingNanoUSD{RerankUnitNanoUSD: 250}, State: catalog.DeployStateActiveHealthy,
		ConnectTimeoutMs: 5000, FirstByteTimeoutMs: 5000, IdleChunkTimeoutMs: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	keyService, err := vkey.NewService(vkey.ServiceConfig{DB: db, NowMs: nowMs, Snapshots: manager, AuditSink: auditLog})
	if err != nil {
		t.Fatal(err)
	}
	virtualKey, err := keyService.Create(ctx, vkey.CreateParams{
		OrgID: org.ID, ProjectID: project.ID, EnvID: env.ID, Name: "rerank-route-key", AllowedPlanes: "PROVIDER", MaxConcurrentStreams: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	var observedUsage *port.NormalizedUsage
	server, err := NewServer(ServerConfig{
		Snapshots: manager, Broker: blindBroker, PluginsDir: pluginsDir, Audit: auditLog, NowMs: nowMs,
		UsageObserver: func(record UsageRecord) { observedUsage = record.Usage },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())

	unauthenticated := httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(`{"model":"acme/rerank-v1","query":"tanpopo purpose","documents":["unrelated","Tanpopo is an integration gateway."],"top_n":1,"return_documents":true}`))
	unauthenticated.Header.Set("Content-Type", "application/json")
	unauthenticatedResponse := httptest.NewRecorder()
	server.ServeHTTP(unauthenticatedResponse, unauthenticated)
	if unauthenticatedResponse.Code != http.StatusUnauthorized || upstreamCalls.Load() != 0 {
		t.Fatalf("unauthenticated request status=%d upstream calls=%d body=%s", unauthenticatedResponse.Code, upstreamCalls.Load(), unauthenticatedResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(`{"model":"acme/rerank-v1","query":"tanpopo purpose","documents":["unrelated","Tanpopo is an integration gateway."],"top_n":1,"return_documents":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+virtualKey.SecretKey)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("rerank route status=%d body=%s", response.Code, response.Body.String())
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("provider calls=%d, want exactly one authenticated request", got)
	}
	var normalized struct {
		Model   string           `json:"model"`
		Usage   map[string]int64 `json:"usage"`
		Results []struct {
			Index    int               `json:"index"`
			Score    float64           `json:"relevance_score"`
			Document map[string]string `json:"document"`
		} `json:"results"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &normalized); err != nil {
		t.Fatalf("decode normalized route response: %v body=%s", err, response.Body.String())
	}
	if normalized.Model != "rerank-v3.5" || len(normalized.Results) != 1 || normalized.Results[0].Index != 1 || normalized.Results[0].Score != 0.93 || normalized.Results[0].Document["text"] != "Tanpopo is an integration gateway." {
		t.Fatalf("unexpected normalized rerank results: %+v", normalized)
	}
	if normalized.Usage["search_units"] != 3 {
		t.Fatalf("route response search_units=%d, want 3: %s", normalized.Usage["search_units"], response.Body.String())
	}
	if observedUsage == nil || observedUsage.MediaUsage == nil || observedUsage.MediaUsage.RerankSearchUnits != 3 {
		t.Fatalf("route usage observer did not receive exact rerank billing units: %+v", observedUsage)
	}
}
