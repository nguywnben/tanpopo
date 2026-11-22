// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Command harness orchestrates hermetic performance benchmarks and multi-node
// rolling-restart proxies against a real compiled `tanpopo` binary (TP-IMP-0043).
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func main() {
	mode := flag.String("mode", "fakellm", "operation mode: seed | fakellm | lb")
	listen := flag.String("listen", "127.0.0.1:19001", "HTTP listen address")
	dataDir := flag.String("data-dir", "/tmp/tanpopo-bench-data", "Tanpopo data directory for seed mode")
	fakeURL := flag.String("fake-url", "http://127.0.0.1:19001", "FakeLLM base URL")
	masterKeyHex := flag.String("master-key", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "32-byte hex Master KEK")
	backends := flag.String("backends", "", "comma-separated backend URLs for lb mode")
	chunkDelayMs := flag.Int("chunk-delay-ms", 0, "default delay in ms between streamed SSE chunks")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var err error
	switch *mode {
	case "seed":
		err = runSeed(ctx, *dataDir, *fakeURL, *masterKeyHex)
	case "fakellm":
		err = runFakeLLM(ctx, *listen, *chunkDelayMs)
	case "lb":
		err = runReadyzLB(ctx, *listen, *backends)
	default:
		err = fmt.Errorf("unsupported mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness: %v\n", err)
		os.Exit(1)
	}
}

func runSeed(ctx context.Context, dataDir, fakeURL, kekHex string) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	pluginsDir := filepath.Join(dataDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o700); err != nil {
		return err
	}
	kekBytes, err := hex.DecodeString(strings.TrimSpace(kekHex))
	if err != nil {
		return fmt.Errorf("decode master key: %w", err)
	}
	kek, err := vault.NewStaticKEKProvider("kek_v1", kekBytes)
	clear(kekBytes)
	if err != nil {
		return err
	}
	defer kek.Close()

	dbPath := filepath.Join(dataDir, "tanpopo.db")
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	nowMs := time.Now().UTC().UnixMilli()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB()})
	if err != nil {
		return err
	}
	vs, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: kek, PostShredHook: st.CheckpointWAL})
	if err != nil {
		return err
	}
	defer vs.Close()

	pubPub, pubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	keyring := trust.TrustedKeyring{PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub}}

	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:         st.RawDB(),
		PluginsDir: pluginsDir,
		Keyring:    keyring,
		Audit:      auditLog,
	})
	if err != nil {
		return err
	}
	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), Keyring: keyring})
	if err != nil {
		return err
	}
	catRepo, err := catalog.NewRepository(catalog.Config{
		DB:        st.RawDB(),
		Snapshots: mgr,
		Keyring:   keyring,
		Audit:     auditLog,
	})
	if err != nil {
		return err
	}
	vkSvc, err := vkey.NewService(vkey.ServiceConfig{
		DB:                        st.RawDB(),
		AllowDetachedControlPlane: true,
		AuditSink:                 auditLog,
	})
	if err != nil {
		return err
	}

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
		Slug:           "bench-org",
		DisplayName:    "Benchmark Org",
		MinPluginTrust: storageport.MinTrustAnyApproved,
	})
	if err != nil {
		return err
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{
		Slug:        "bench-proj",
		DisplayName: "Benchmark Project",
	})
	if err != nil {
		return err
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{
		Slug: "prod",
	})
	if err != nil {
		return err
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ('prc_bench_op', NULL, 'HUMAN_LOCAL', 'bench@tanpopo.test', 'ACTIVE', 1, ?, ?)
	`, nowMs, nowMs); err != nil {
		return err
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_bench_op', NULL, 'prc_bench_op', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)
	`, nowMs); err != nil {
		return err
	}

	secMeta, err := vs.SealSecret(ctx, vault.SealSecretParams{
		OrgID:      org.ID,
		Name:       "bench-api-key",
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte("sk-bench-placeholder-not-used-outbound"),
	})
	if err != nil {
		return err
	}

	tmpPack, err := os.MkdirTemp("", "tanpopo-bench-pack-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpPack) }()

	parsedFake, err := url.Parse(fakeURL)
	if err != nil {
		return err
	}
	allowedHost := parsedFake.Hostname()
	if allowedHost == "" {
		allowedHost = "127.0.0.1"
	}

	installAdapter := func(pkgID, slug, yamlBody string, extraFiles map[string][]byte) (string, error) {
		src := filepath.Join(tmpPack, slug)
		if err := os.MkdirAll(src, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(src, "manifest.yaml"), []byte(yamlBody), 0o600); err != nil {
			return "", err
		}
		for rel, content := range extraFiles {
			if err := os.WriteFile(filepath.Join(src, rel), content, 0o600); err != nil {
				return "", err
			}
		}
		tpkg := filepath.Join(tmpPack, slug+".tpkg")
		packRes, err := testharness.PackExtension(ctx, src, tpkg)
		if err != nil {
			return "", err
		}
		sig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload(pkgID, "1.0.0", packRes.SHA256))
		staged, err := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
			ArchivePath: tpkg,
			Source: installer.SourceSpec{
				Kind:           installer.SourceKindLocalArchive,
				URI:            "./" + slug + ".tpkg",
				ExpectedSHA256: packRes.SHA256,
			},
			Signature:   installer.ProvenanceSignature{PublisherOrRootSig: sig},
			ActorID:     "prc_bench_op",
			RequestID:   "req_seed_" + slug,
			TargetOrgID: org.ID,
		})
		if err != nil {
			return "", err
		}
		rec, err := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
			InstallationID:      staged.Installation.InstallationID,
			ExpectedVersion:     staged.Installation.Version,
			ApproverPrincipalID: "prc_bench_op",
			ActivateImmediately: true,
			OrgID:               org.ID,
			RequestID:           "req_approve_" + slug,
		})
		if err != nil {
			return "", err
		}
		if _, err := st.RawDB().ExecContext(ctx, `
			INSERT OR REPLACE INTO org_extension_policies (
				org_id, package_id, enabled, pinned_installation_id, version, created_at_ms, updated_at_ms, org_owner_opt_in
			) VALUES (?, (SELECT package_id FROM extension_installations WHERE id = ?), 1, ?, 1, ?, ?, 1)
		`, org.ID, rec.InstallationID, rec.InstallationID, nowMs, nowMs); err != nil {
			return "", err
		}
		adp, err := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
			InstallationID:    rec.InstallationID,
			AdapterSlug:       slug,
			SupportedDialects: []string{"openai"},
			AllowedHosts:      []string{allowedHost},
		})
		if err != nil {
			return "", err
		}
		return adp.ID, nil
	}

	declManifest := fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/bench-decl"
  name: "Bench Declarative"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "bench-decl"
    supportedDialects: ["openai_compatible"]
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts: [%q]
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:transform_request"
    - "provider:transform_response"
    - "provider:transform_stream_chunk"
    - "provider:normalize_error"
adapter:
  slug: "bench-decl"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`, allowedHost)
	declAdpID, err := installAdapter("@acme/bench-decl", "bench-decl", declManifest, nil)
	if err != nil {
		return err
	}

	wasmBytes := buildBenchWasmAdapter(fakeURL)
	wasmSum := sha256.Sum256(wasmBytes)
	wasmManifest := fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/bench-wasm"
  name: "Bench Wasm"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "adapter.wasm"
  sha256: %q
  quotas:
    maxMemoryMib: 16
    maxCpuMsPerCall: 200
capabilities:
  - type: "provider_adapter"
    adapterSlug: "bench-wasm"
    supportedDialects: ["openai_compatible"]
secretSlots:
  - slot: "api_key"
permissions:
  networkEgress:
    allowedHosts: [%q]
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:transform_request"
    - "provider:transform_response"
`, hex.EncodeToString(wasmSum[:]), allowedHost)
	wasmAdpID, err := installAdapter("@acme/bench-wasm", "bench-wasm", wasmManifest, map[string][]byte{"adapter.wasm": wasmBytes})
	if err != nil {
		return err
	}

	wireModel := func(adpID, connName, modelSlug string) error {
		conn, err := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
			OrgID: org.ID, AdapterID: adpID, Name: connName, EndpointBaseURL: fakeURL,
			AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, DataRegion: "us",
			BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeAPIKey,
			SecretBindings: []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind}},
		})
		if err != nil {
			return err
		}
		conn, err = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "bench val")
		if err != nil {
			return err
		}
		if _, err := catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "bench act"); err != nil {
			return err
		}
		lmod, err := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{
			OrgID: &org.ID, CanonicalSlug: modelSlug, ModalityKind: catalog.ModalityChat,
			ContextWindow: 128000, MaxOutputTokens: 4096,
			BaseCapabilities: map[string]string{"streaming": "VERIFIED"},
		})
		if err != nil {
			return err
		}
		_, err = catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
			OrgID: org.ID, ConnectionID: conn.ID, LogicalModelID: lmod.ID,
			UpstreamModelName: "bench-gpt", AssuranceLevel: catalog.AssuranceTested, State: catalog.DeployStateActiveHealthy,
			ConnectTimeoutMs: 5000, FirstByteTimeoutMs: 10000, IdleChunkTimeoutMs: 10000,
		})
		return err
	}

	if err := wireModel(declAdpID, "bench-decl-conn", "bench-decl"); err != nil {
		return err
	}
	if err := wireModel(wasmAdpID, "bench-wasm-conn", "bench-wasm"); err != nil {
		return err
	}

	vk, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "bench-vk",
		AllowedPlanes: "PROVIDER", MaxConcurrentStreams: 5000,
	})
	if err != nil {
		return err
	}

	out, _ := json.Marshal(map[string]string{
		"org_id":     org.ID,
		"project_id": proj.ID,
		"env_id":     env.ID,
		"vkey":       vk.SecretKey,
		"vkey_id":    vk.Summary.ID,
		"db_path":    dbPath,
	})
	fmt.Println(string(out))
	return nil
}

func runFakeLLM(ctx context.Context, listen string, defaultChunkDelayMs int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"bench-gpt","object":"model"}]}`)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(body, &req)
		if !req.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl_b1","object":"chat.completion","model":"bench-gpt","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`)
			return
		}
		chunks := 6
		delayMs := defaultChunkDelayMs
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		_ = rc.Flush()
		for i := 0; i < chunks; i++ {
			if r.Context().Err() != nil {
				return
			}
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl_b1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"c%d\"}}]}\n\n", i)
			_ = rc.Flush()
			if delayMs > 0 {
				t := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
				select {
				case <-r.Context().Done():
					t.Stop()
					return
				case <-t.C:
				}
			}
		}
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl_b1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", chunks, 8+chunks)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		_ = rc.Flush()
	})

	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runReadyzLB(ctx context.Context, listen, backendsCSV string) error {
	rawList := strings.Split(backendsCSV, ",")
	type backendNode struct {
		url   *url.URL
		ready atomic.Bool
		proxy *httputil.ReverseProxy
	}
	dialer := &net.Dialer{
		Timeout:   2 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return fmt.Errorf("harness lb forbids non-loopback dial to %s", address)
			}
			return nil
		},
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConnsPerHost: 256,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}

	var nodes []*backendNode
	for _, raw := range rawList {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		u, err := url.Parse(trimmed)
		if err != nil {
			return err
		}
		rp := httputil.NewSingleHostReverseProxy(u)
		rp.Transport = transport
		rp.FlushInterval = -1
		bn := &backendNode{url: u, proxy: rp}
		bn.ready.Store(true)
		nodes = append(nodes, bn)
	}
	if len(nodes) == 0 {
		return errors.New("at least one backend URL required")
	}

	probeOnce := func() {
		for _, n := range nodes {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, n.url.String()+"/readyz", nil)
			resp, err := client.Do(req)
			if err != nil {
				n.ready.Store(false)
				continue
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			n.ready.Store(resp.StatusCode == http.StatusOK)
		}
	}
	probeOnce()
	go func() {
		tk := time.NewTicker(40 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				probeOnce()
			}
		}
	}()

	var rr atomic.Uint64
	srv := &http.Server{
		Addr: listen,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var readyNodes []*backendNode
			for _, n := range nodes {
				if n.ready.Load() {
					readyNodes = append(readyNodes, n)
				}
			}
			if len(readyNodes) == 0 {
				http.Error(w, "no ready backends", http.StatusServiceUnavailable)
				return
			}
			idx := rr.Add(1) % uint64(len(readyNodes))
			readyNodes[idx].proxy.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func uleb(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v == 0 {
			out = append(out, b)
			return out
		}
		out = append(out, b|0x80)
	}
}

func sleb64(v int64) []byte {
	var out []byte
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && (b&0x40) == 0) || (v == -1 && (b&0x40) != 0) {
			more = false
		} else {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

func wSection(id byte, payload []byte) []byte {
	var out []byte
	out = append(out, id)
	out = append(out, uleb(uint32(len(payload)))...)
	out = append(out, payload...)
	return out
}

func wString(s string) []byte {
	var out []byte
	out = append(out, uleb(uint32(len(s)))...)
	out = append(out, s...)
	return out
}

// buildBenchWasmAdapter synthesizes a 16 MiB-capped WebAssembly 1.0 module implementing
// transform_request and transform_response against FakeLLM for Tier-2 overhead benchmarking.
func buildBenchWasmAdapter(fakeBaseURL string) []byte {
	type dataSeg struct {
		offset uint32
		data   []byte
	}
	reqNeedle := []byte(`"normalized_request_json":"`)
	prefixChat := []byte(`{"method":"POST","target_url":"` + fakeBaseURL + `/v1/chat/completions","safe_headers":{"Content-Type":"application/json"},"credential_injections":[{"secret_slot_name":"api_key","target_location":"CREDENTIAL_TARGET_LOCATION_HEADER","field_name":"Authorization","value_prefix":"Bearer "}],"body_bytes":"`)
	suffixJSON := []byte(`"}`)
	chatFakeNorm := base64.StdEncoding.EncodeToString([]byte(`{"id":"chatcmpl_wasm_b","object":"chat.completion","model":"bench-gpt","choices":[{"index":0,"message":{"role":"assistant","content":"ok-wasm"},"finish_reason":"stop"}]}`))
	chatRespEnvelope := []byte(`{"normalized_response_json":"` + chatFakeNorm + `","upstream_model_id":"bench-gpt","finish_reason":"stop","usage":{"input_tokens":8,"output_tokens":2,"accuracy":"USAGE_ACCURACY_EXACT"}}`)

	const (
		reqNeedleOff  uint32 = 65536
		prefixChatOff uint32 = 65600
		suffixOff     uint32 = 66100
		chatEnvOff    uint32 = 66200
		scratchOutOff uint32 = 131072
	)
	if len(reqNeedle) > int(prefixChatOff-reqNeedleOff) || len(prefixChat) > int(suffixOff-prefixChatOff) || len(suffixJSON) > int(chatEnvOff-suffixOff) {
		panic("buildBenchWasmAdapter: data segment exceeds reserved offset bounds")
	}
	segs := []dataSeg{
		{offset: reqNeedleOff, data: reqNeedle},
		{offset: prefixChatOff, data: prefixChat},
		{offset: suffixOff, data: suffixJSON},
		{offset: chatEnvOff, data: chatRespEnvelope},
	}

	emitMemcpy := func(b []byte, dstLocal, srcLocal, lenLocal, idxLocal uint32) []byte {
		b = append(b, 0x41, 0x00, 0x21)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x02, 0x40, 0x03, 0x40)
		b = append(b, 0x20)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x20)
		b = append(b, uleb(lenLocal)...)
		b = append(b, 0x4f, 0x0d, 0x01)
		b = append(b, 0x20)
		b = append(b, uleb(dstLocal)...)
		b = append(b, 0x20)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x6a)
		b = append(b, 0x20)
		b = append(b, uleb(srcLocal)...)
		b = append(b, 0x20)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x6a, 0x2d, 0x00, 0x00, 0x3a, 0x00, 0x00)
		b = append(b, 0x20)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x41, 0x01, 0x6a, 0x21)
		b = append(b, uleb(idxLocal)...)
		b = append(b, 0x0c, 0x00, 0x0b, 0x0b)
		return b
	}

	emitFindSubstr := func(b []byte, inPtrL, inLenL, needleOff uint32, needleLen uint32, matchL, iL, kL uint32) []byte {
		b = append(b, 0x41)
		b = append(b, sleb64(-1)...)
		b = append(b, 0x21)
		b = append(b, uleb(matchL)...)
		b = append(b, 0x20)
		b = append(b, uleb(inLenL)...)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleLen))...)
		b = append(b, 0x4f, 0x04, 0x40, 0x41, 0x00, 0x21)
		b = append(b, uleb(iL)...)
		b = append(b, 0x02, 0x40, 0x03, 0x40, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x20)
		b = append(b, uleb(inLenL)...)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleLen))...)
		b = append(b, 0x6b, 0x4b, 0x0d, 0x01, 0x41, 0x00, 0x21)
		b = append(b, uleb(kL)...)
		b = append(b, 0x02, 0x40, 0x03, 0x40, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleLen))...)
		b = append(b, 0x4f, 0x04, 0x40, 0x20)
		b = append(b, uleb(inPtrL)...)
		b = append(b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x6a, 0x21)
		b = append(b, uleb(matchL)...)
		b = append(b, 0x0c, 0x04, 0x0b, 0x20)
		b = append(b, uleb(inPtrL)...)
		b = append(b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x6a, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x6a, 0x2d, 0x00, 0x00, 0x41)
		b = append(b, sleb64(int64(needleOff))...)
		b = append(b, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x6a, 0x2d, 0x00, 0x00, 0x47, 0x0d, 0x01, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x41, 0x01, 0x6a, 0x21)
		b = append(b, uleb(kL)...)
		b = append(b, 0x0c, 0x00, 0x0b, 0x0b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x41, 0x01, 0x6a, 0x21)
		b = append(b, uleb(iL)...)
		b = append(b, 0x0c, 0x00, 0x0b, 0x0b, 0x0b)
		return b
	}

	var reqBody bytes.Buffer
	reqBody.Write([]byte{0x01, 0x09, 0x7f})
	b := reqBody.Bytes()
	b = emitFindSubstr(b, 0, 1, reqNeedleOff, uint32(len(reqNeedle)), 4, 2, 3)
	b = append(b, 0x20, 0x04, 0x41)
	b = append(b, sleb64(int64(len(reqNeedle)))...)
	b = append(b, 0x6a, 0x21, 0x05)
	b = append(b, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x21, 0x0a)
	b = append(b, 0x20, 0x05, 0x21, 0x02, 0x02, 0x40, 0x03, 0x40)
	b = append(b, 0x20, 0x02, 0x20, 0x0a, 0x4f, 0x0d, 0x01)
	b = append(b, 0x20, 0x02, 0x2d, 0x00, 0x00, 0x41, 0x22, 0x46, 0x0d, 0x01)
	b = append(b, 0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, 0x0c, 0x00, 0x0b, 0x0b)
	b = append(b, 0x20, 0x02, 0x20, 0x05, 0x6b, 0x21, 0x06)
	b = append(b, 0x41)
	b = append(b, sleb64(int64(prefixChatOff))...)
	b = append(b, 0x21, 0x07, 0x41)
	b = append(b, sleb64(int64(len(prefixChat)))...)
	b = append(b, 0x21, 0x08)
	b = append(b, 0x41)
	b = append(b, sleb64(int64(scratchOutOff))...)
	b = append(b, 0x21, 0x09)
	b = emitMemcpy(b, 9, 7, 8, 2)
	b = append(b, 0x20, 0x09, 0x20, 0x08, 0x6a, 0x21, 0x09)
	b = emitMemcpy(b, 9, 5, 6, 2)
	b = append(b, 0x20, 0x09, 0x20, 0x06, 0x6a, 0x21, 0x09)
	b = append(b, 0x41)
	b = append(b, sleb64(int64(suffixOff))...)
	b = append(b, 0x21, 0x07, 0x41)
	b = append(b, sleb64(int64(len(suffixJSON)))...)
	b = append(b, 0x21, 0x08)
	b = emitMemcpy(b, 9, 7, 8, 2)
	b = append(b, 0x20, 0x09, 0x20, 0x08, 0x6a, 0x21, 0x09)
	b = append(b, 0x42)
	b = append(b, sleb64(int64(uint64(scratchOutOff)<<32))...)
	b = append(b, 0x20, 0x09, 0x41)
	b = append(b, sleb64(int64(scratchOutOff))...)
	b = append(b, 0x6b, 0xad, 0x84, 0x0b)

	var respBody []byte
	respBody = append(respBody, 0x00, 0x42)
	respBody = append(respBody, sleb64(int64((uint64(chatEnvOff)<<32)|uint64(len(chatRespEnvelope))))...)
	respBody = append(respBody, 0x0b)

	var out []byte
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	out = append(out, wSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	out = append(out, wSection(3, []byte{0x02, 0x00, 0x00})...)
	out = append(out, wSection(5, append(append([]byte{0x01, 0x01}, uleb(4)...), uleb(256)...))...)
	var exp []byte
	exp = append(exp, 0x03)
	exp = append(exp, wString("memory")...)
	exp = append(exp, 0x02, 0x00)
	exp = append(exp, wString("transform_request")...)
	exp = append(exp, 0x00, 0x00)
	exp = append(exp, wString("transform_response")...)
	exp = append(exp, 0x00, 0x01)
	out = append(out, wSection(7, exp)...)
	var code []byte
	code = append(code, 0x02)
	code = append(code, uleb(uint32(len(b)))...)
	code = append(code, b...)
	code = append(code, uleb(uint32(len(respBody)))...)
	code = append(code, respBody...)
	out = append(out, wSection(10, code)...)
	var dataSec []byte
	dataSec = append(dataSec, uleb(uint32(len(segs)))...)
	for _, s := range segs {
		dataSec = append(dataSec, 0x00, 0x41)
		dataSec = append(dataSec, sleb64(int64(s.offset))...)
		dataSec = append(dataSec, 0x0b)
		dataSec = append(dataSec, uleb(uint32(len(s.data)))...)
		dataSec = append(dataSec, s.data...)
	}
	out = append(out, wSection(11, dataSec)...)
	return out
}
