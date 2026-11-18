// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/fakellm"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type auditRecorder struct {
	buf canary.SafeBuffer
}

func (a *auditRecorder) Append(_ context.Context, ev storageport.AuditEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, _ = a.buf.Write(append(b, '\n'))
	return nil
}

func (a *auditRecorder) AppendTx(ctx context.Context, _ *sql.Tx, ev storageport.AuditEvent) error {
	return a.Append(ctx, ev)
}

// TestEXTAccept01_VerticalSliceM1 proves GATE-M1 (EXT-SLICE-M1) end-to-end without editing
// Core across both Tier-1 Declarative and Tier-2 Sandboxed Wasm synthetic AcmeAI adapters:
//   - AC-1: .tpkg pack -> stage -> approve -> Vault canary -> catalog binding -> sync + SSE + tool_calls
//   - AC-2: CT-11 downstream disconnect upstream socket close < 500 ms + AUTH-06 revocation + 0 goroutine leaks
//   - NEG-1: CT-10 / FMEA-01 TCP RST mid-stream -> mid_stream_abort, 0 failover calls, 0 synthetic [DONE]
//   - NEG-2: FZ-03 oversized (> 2 MiB) unterminated SSE line aborts boundedly
func TestEXTAccept01_VerticalSliceM1(t *testing.T) {
	for _, tier := range []string{"declarative", "wasm"} {
		tier := tier
		t.Run("tier_"+tier, func(t *testing.T) {
			runEXTSliceM1ForTier(t, tier)
		})
	}
}

// TestEXTAccept01_FullLifecycleProcessContract pins the process-level boundary
// required by the ten-step acceptance flow. It exercises the currently available
// offline SDK commands and checks that the live server and all required Admin API
// surfaces remain reachable without replacing the Core process or binary.
//
// Endpoint probes are deliberately not treated as proof of lifecycle behavior:
// the authenticated install/approval, EU routing/ledger, upgrade/rollback,
// quarantine timing, and forensic result assertions remain runtime dependencies
// until the corresponding live workflow can be driven end to end here.
func TestEXTAccept01_FullLifecycleProcessContract(t *testing.T) {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("locate Tanpopo module from integration test directory: %v", err)
	}
	work := t.TempDir()
	binary := filepath.Join(work, "tanpopo")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build single Tanpopo executable: %v\n%s", err, output)
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	initialDigest := sha256.Sum256(binaryBytes)
	initialSHA := hex.EncodeToString(initialDigest[:])

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	dataDir := filepath.Join(work, "data")
	cmd := exec.Command(binary, "serve", "--role=all", "--listen", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"TANPOPO_DATA_DIR="+dataDir,
		"TANPOPO_MASTER_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"TANPOPO_MASTER_KEY_FILE=",
		"TANPOPO_TEST_HERMETIC=1",
	)
	var serverOutput bytes.Buffer
	cmd.Stdout, cmd.Stderr = &serverOutput, &serverOutput
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Tanpopo Core process: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	client := netguard.NewHTTPClient()
	client.Timeout = 2 * time.Second
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, reqErr := client.Get(baseURL + "/readyz")
		if reqErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Tanpopo did not become ready; pid=%d output=%s", pid, serverOutput.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	assertCoreInvariant := func(step string) {
		t.Helper()
		if cmd.ProcessState != nil {
			t.Fatalf("step %s: Core process exited (pid=%d): %s", step, pid, serverOutput.String())
		}
		if got := cmd.Process.Pid; got != pid {
			t.Fatalf("step %s: Core PID changed from %d to %d", step, pid, got)
		}
		current, readErr := os.ReadFile(binary)
		if readErr != nil {
			t.Fatalf("step %s: read Core executable: %v", step, readErr)
		}
		gotSHA := sha256.Sum256(current)
		if hex.EncodeToString(gotSHA[:]) != initialSHA {
			t.Fatalf("step %s: Core executable SHA-256 changed", step)
		}
	}
	assertCoreInvariant("startup")

	// Mandatory step 1: scaffold, run the offline contract suite, and pack from
	// outside the Core source tree. The current scaffold CLI may not yet allow
	// choosing the required @acme-cloud namespace; keep that assertion explicit.
	extensionDir := filepath.Join(work, "external-adapter")
	runCLI := func(args ...string) ([]byte, error) {
		t.Helper()
		c := exec.Command(binary, args...)
		c.Dir = work
		return c.CombinedOutput()
	}
	if out, runErr := runCLI("plugin", "init", "regional-llm-adapter", "--dir", extensionDir); runErr != nil {
		t.Errorf("step 1: tanpopo plugin init: %v\n%s", runErr, out)
	} else {
		manifest, readErr := os.ReadFile(filepath.Join(extensionDir, "manifest.yaml"))
		if readErr != nil {
			t.Errorf("step 1: read generated manifest: %v", readErr)
		} else if !strings.Contains(string(manifest), "@acme-cloud/regional-llm-adapter") {
			t.Errorf("step 1 contract dependency: plugin init must scaffold exact package @acme-cloud/regional-llm-adapter; current manifest does not")
		}
		if out, runErr := runCLI("plugin", "test", extensionDir); runErr != nil {
			t.Errorf("step 1: tanpopo plugin test (CT-01..CT-17): %v\n%s", runErr, out)
		}
		if out, runErr := runCLI("plugin", "pack", extensionDir, "--out", filepath.Join(work, "acme-0.2.0.tpkg")); runErr != nil {
			t.Errorf("step 1: tanpopo plugin pack: %v\n%s", runErr, out)
		}
	}
	assertCoreInvariant("step 1")

	// Steps 2-9 require authenticated state-changing workflows. Probe the
	// canonical API surfaces from the running process and reject missing routes;
	// status codes such as 401/403/405 are expected here because this probe does
	// not impersonate an authenticated administrator.
	apiSurfaces := []struct{ step, method, path string }{
		{"2 pending permission approval", http.MethodGet, "/admin/v1/extensions/installations"},
		{"3 permission approval and lock pin", http.MethodGet, "/admin/v1/extensions/lockfile"},
		{"4 connection/route/budget setup", http.MethodGet, "/admin/v1/routes"},
		{"5 inference and exact settlement", http.MethodPost, "/v1/chat/completions"},
		{"6 EU fail-closed and estimated settlement", http.MethodPost, "/v1/chat/completions"},
		{"7 pending reconsent", http.MethodPost, "/admin/v1/extensions/install"},
		{"8 rollback", http.MethodPost, "/admin/v1/extensions/installation-contract/rollback"},
		{"9 quarantine and blast-radius", http.MethodGet, "/admin/v1/blast-radius?target_type=extension&target_id=contract"},
	}
	for _, surface := range apiSurfaces {
		req, reqErr := http.NewRequest(surface.method, baseURL+surface.path, nil)
		if reqErr != nil {
			t.Errorf("step %s: construct API probe: %v", surface.step, reqErr)
			continue
		}
		resp, reqErr := client.Do(req)
		if reqErr != nil {
			t.Errorf("step %s: API contract unavailable: %v", surface.step, reqErr)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			t.Errorf("step %s: required API route %s %s is not mounted", surface.step, surface.method, surface.path)
		}
		assertCoreInvariant(surface.step)
	}

	// Mandatory step 10: unchanged executable and still-serving process after
	// every lifecycle operation. Full behavioral assertions above remain gated
	// on authenticated bootstrap/session, ledger setup, and lifecycle APIs.
	assertCoreInvariant("step 10")
}

func runEXTSliceM1ForTier(t *testing.T, tier string) {
	t.Helper()
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000500000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "tanpopo.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	audit := &auditRecorder{}

	pubPub, pubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := trust.TrustedKeyring{
		PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub},
	}

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:      st.RawDB(),
		NowMs:   nowFn,
		Keyring: keyring,
	})
	if err != nil {
		t.Fatal(err)
	}

	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}
	kek, err := vault.NewStaticKEKProvider("kek-m1", masterKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kek.Close)

	vStore, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		NowMs:         nowFn,
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vStore.Close)

	catRepo, err := catalog.NewRepository(catalog.Config{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: mgr,
		Keyring:   keyring,
		Audit:     audit,
	})
	if err != nil {
		t.Fatal(err)
	}

	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver:       catRepo,
		PolicyResolver: catRepo, // DEC-020: explicit ConnectionPolicyResolver
		Vault:          vStore,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          audit,
		NowMs:          nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:         st.RawDB(),
		PluginsDir: pluginsDir,
		Keyring:    keyring,
		Audit:      audit,
		NowMs:      nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	vkSvc, err := vkey.NewService(vkey.ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: mgr,
		AuditSink: audit,
	})
	if err != nil {
		t.Fatal(err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
		Slug:           "acme-org",
		DisplayName:    "Acme Org",
		MinPluginTrust: storageport.MinTrustVerifiedOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{
		Slug:        "acme-proj",
		DisplayName: "Acme Project",
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{
		Slug: "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ('prc_op_m1', NULL, 'HUMAN_LOCAL', 'op-m1@acme.test', 'ACTIVE', 1, ?, ?)
	`, nowFn(), nowFn()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_op_m1', NULL, 'prc_op_m1', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)
	`, nowFn()); err != nil {
		t.Fatal(err)
	}

	// Primary FakeLLMServer + standby failover target (must NEVER receive requests mid-stream).
	fake := fakellm.NewServer(t)
	backupFake := fakellm.NewServer(t)

	// Trickle / tool_calls / oversized-line test server bound on loopback for deterministic AC-2 & NEG-2.
	trickleDisconnectCh := make(chan time.Time, 8)
	var trickleMu sync.Mutex
	var trickleReqs int
	var trickleAuths []string
	var trickleMode string // "trickle" | "tool_sync" | "oversized_sse"
	trickleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		_ = r.Body.Close()

		trickleMu.Lock()
		trickleReqs++
		trickleAuths = append(trickleAuths, r.Header.Get("Authorization"))
		mode := trickleMode
		trickleMu.Unlock()

		switch mode {
		case "tool_sync":
			var reqObj map[string]any
			_ = json.Unmarshal(body, &reqObj)
			tools, _ := reqObj["tools"].([]any)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl_acme_tool_1",
				"object":  "chat.completion",
				"created": nowFn() / 1000,
				"model":   reqObj["model"],
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]any{
								{
									"id":   "call_weather_1",
									"type": "function",
									"function": map[string]any{
										"name":      "get_weather",
										"arguments": fmt.Sprintf(`{"city":"Tokyo","tool_count":%d}`, len(tools)),
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
				"usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 8, "total_tokens": 20},
			})
			return

		case "oversized_sse":
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			// Emit > 2 MiB unterminated SSE line to exercise FZ-03 / NEG-2 cap.
			_, _ = w.Write([]byte("data: "))
			chunk := bytes.Repeat([]byte("a"), 64*1024)
			for i := 0; i < 40; i++ {
				if _, werr := w.Write(chunk); werr != nil {
					return
				}
			}
			_, _ = w.Write([]byte("\n\n"))
			return

		default: // "trickle": 1 frame every 100ms up to 40 frames until client disconnects.
			rc := http.NewResponseController(w)
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			_ = rc.Flush()

			for i := 0; i < 40; i++ {
				select {
				case <-r.Context().Done():
					select {
					case trickleDisconnectCh <- time.Now():
					default:
					}
					return
				case <-time.After(100 * time.Millisecond):
				}
				if _, werr := fmt.Fprintf(w, "data: {\"id\":\"chatcmpl_trickle\",\"object\":\"chat.completion.chunk\",\"model\":\"acme-gpt-upstream\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"tok-%d\"}}]}\n\n", i); werr != nil {
					select {
					case trickleDisconnectCh <- time.Now():
					default:
					}
					return
				}
				_ = rc.Flush()
			}
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			_ = rc.Flush()
		}
	}))
	t.Cleanup(trickleSrv.Close)

	// Step 1: Build and pack net-new synthetic AcmeAI .tpkg extension (0 Core edits).
	extDir := filepath.Join(tmp, "acme-"+tier)
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pkgID := "@acme/acme-" + tier
	adpSlug := "acme-" + tier
	tpkgOut := filepath.Join(tmp, fmt.Sprintf("acme-%s-1.0.0.tpkg", tier))

	targetEndpointURL := fake.URL() + "/v1/chat/completions"
	var manifestYAML string
	if tier == "declarative" {
		manifestYAML = fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: %q
  name: "AcmeAI Tier-1 Adapter"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: %q
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
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:build_validation_probe"
    - "provider:transform_request"
    - "provider:transform_response"
    - "provider:transform_stream_chunk"
    - "provider:normalize_error"
adapter:
  slug: %q
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`, pkgID, adpSlug, adpSlug)
	} else {
		wasmDir := filepath.Join(extDir, "dist")
		if err := os.MkdirAll(wasmDir, 0o755); err != nil {
			t.Fatal(err)
		}
		wasmBytes := buildAcmeChatWASM(targetEndpointURL)
		if err := os.WriteFile(filepath.Join(wasmDir, "adapter.wasm"), wasmBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(wasmBytes)
		wasmSHA := hex.EncodeToString(sum[:])

		manifestYAML = fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: %q
  name: "AcmeAI Tier-2 Wasm Adapter"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "dist/adapter.wasm"
  sha256: %q
  quotas:
    maxMemoryMib: 16
    maxCpuMsPerCall: 50
    maxStreamChunkCpuMs: 25
capabilities:
  - type: "provider_adapter"
    adapterSlug: %q
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
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:transform_request"
    - "provider:transform_response"
    - "provider:transform_stream_chunk"
    - "provider:normalize_error"
`, pkgID, wasmSHA, adpSlug)
	}
	if err := os.WriteFile(filepath.Join(extDir, "manifest.yaml"), []byte(manifestYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	packRes, err := testharness.PackExtension(ctx, extDir, tpkgOut)
	if err != nil {
		t.Fatalf("PackExtension(%s): %v", tier, err)
	}

	// Step 2: StageFreshInstall -> verify PENDING_PERMISSION_APPROVAL -> ApproveAndActivate -> ENABLED_ACTIVE.
	sig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload(pkgID, "1.0.0", packRes.SHA256))
	staged, err := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchivePath: tpkgOut,
		Source: installer.SourceSpec{
			Kind:           installer.SourceKindLocalArchive,
			URI:            fmt.Sprintf("./acme-%s-1.0.0.tpkg", tier),
			ExpectedSHA256: packRes.SHA256,
		},
		Signature: installer.ProvenanceSignature{
			PublisherOrRootSig: sig,
		},
		ActorID:     "prc_op_m1",
		RequestID:   "req_m1_stage",
		TargetOrgID: org.ID,
	})
	if err != nil {
		t.Fatalf("StageFreshInstall: %v", err)
	}
	if staged.Installation.State != installer.StatePendingPermissionApproval {
		t.Fatalf("expected PENDING_PERMISSION_APPROVAL, got %q", staged.Installation.State)
	}
	rec, err := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: "prc_op_m1",
		ActivateImmediately: true,
		OrgID:               org.ID,
		RequestID:           "req_m1_approve",
	})
	if err != nil {
		t.Fatalf("ApproveAndActivate: %v", err)
	}
	if rec.State != installer.StateEnabledActive {
		t.Fatalf("expected ENABLED_ACTIVE, got %q", rec.State)
	}

	// Step 3: Seal canary token into Vault & wire Provider Catalog + Deployments.
	canarySec := canary.DefaultCanarySecret()
	secMeta, err := vStore.SealSecret(ctx, vault.SealSecretParams{
		OrgID:      org.ID,
		Name:       "acme-api-key",
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte(canarySec),
	})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}

	adp, err := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    rec.InstallationID,
		AdapterSlug:       adpSlug,
		SupportedDialects: []string{"openai"},
		AllowedHosts:      []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("CreateProviderAdapter: %v", err)
	}

	primaryConn, err := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID:           org.ID,
		AdapterID:       adp.ID,
		Name:            "acme-primary",
		EndpointBaseURL: fake.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind},
		},
	})
	if err != nil {
		t.Fatalf("CreateProviderConnection: %v", err)
	}
	primaryConn, err = catRepo.TransitionConnectionState(ctx, org.ID, primaryConn.ID, catalog.ConnStateValidating, primaryConn.Version, "validate")
	if err != nil {
		t.Fatal(err)
	}
	primaryConn, err = catRepo.TransitionConnectionState(ctx, org.ID, primaryConn.ID, catalog.ConnStateActive, primaryConn.Version, "activate")
	if err != nil {
		t.Fatal(err)
	}

	// Standby secondary connection to verify 0 mid-stream failover calls (FMEA-01).
	backupConn, err := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID:           org.ID,
		AdapterID:       adp.ID,
		Name:            "acme-backup",
		EndpointBaseURL: backupFake.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	backupConn, err = catRepo.TransitionConnectionState(ctx, org.ID, backupConn.ID, catalog.ConnStateValidating, backupConn.Version, "validate")
	if err != nil {
		t.Fatal(err)
	}
	backupConn, err = catRepo.TransitionConnectionState(ctx, org.ID, backupConn.ID, catalog.ConnStateActive, backupConn.Version, "activate")
	if err != nil {
		t.Fatal(err)
	}

	lmod, err := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{
		OrgID:           &org.ID,
		CanonicalSlug:   "acme/chat-1",
		ModalityKind:    catalog.ModalityChat,
		ContextWindow:   128000,
		MaxOutputTokens: 4096,
		BaseCapabilities: map[string]string{
			"streaming":    "VERIFIED",
			"tool_calling": "VERIFIED",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	primaryDepl, err := catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
		ID:                 "dpl_01_primary",
		OrgID:              org.ID,
		ConnectionID:       primaryConn.ID,
		LogicalModelID:     lmod.ID,
		UpstreamModelName:  "acme-gpt-upstream",
		AssuranceLevel:     catalog.AssuranceVerified,
		State:              catalog.DeployStateActiveHealthy,
		ConnectTimeoutMs:   5000,
		FirstByteTimeoutMs: 5000,
		IdleChunkTimeoutMs: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Secondary deployment at higher ID so M1 deterministic ordering selects primaryDepl first.
	_, err = catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
		ID:                 "dpl_02_backup",
		OrgID:              org.ID,
		ConnectionID:       backupConn.ID,
		LogicalModelID:     lmod.ID,
		UpstreamModelName:  "acme-gpt-backup",
		AssuranceLevel:     catalog.AssuranceVerified,
		State:              catalog.DeployStateActiveHealthy,
		ConnectTimeoutMs:   5000,
		FirstByteTimeoutMs: 5000,
		IdleChunkTimeoutMs: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Step 4: Issue Virtual Key & start Data Plane HTTP server.
	createdVK, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID:                org.ID,
		ProjectID:            proj.ID,
		EnvID:                env.ID,
		Name:                 "m1-e2e-key",
		AllowedPlanes:        "PROVIDER",
		MaxConcurrentStreams: 16,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	gwSrv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:            mgr,
		Broker:               brk,
		PluginsDir:           pluginsDir,
		Audit:                audit,
		NowMs:                nowFn,
		SSEHeartbeatInterval: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gwSrv.Close(context.Background()) })

	httpSrv := httptest.NewServer(gwSrv)
	t.Cleanup(httpSrv.Close)

	client := httpSrv.Client()
	t.Cleanup(client.CloseIdleConnections)

	postChat := func(payload string, cancelCtx context.Context) (*http.Response, error) {
		c := ctx
		if cancelCtx != nil {
			c = cancelCtx
		}
		req, rerr := http.NewRequestWithContext(c, http.MethodPost, httpSrv.URL+"/v1/chat/completions", strings.NewReader(payload))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+createdVK.SecretKey)
		return client.Do(req)
	}

	// --- AC-1a: Synchronous /v1/chat/completions against FakeLLMServer ---
	resp, err := postChat(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hello acme"}]}`, nil)
	if err != nil {
		t.Fatalf("sync chat request failed: %v", err)
	}
	syncBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync status=%d want 200; body=%s", resp.StatusCode, string(syncBody))
	}
	if !strings.HasPrefix(resp.Header.Get("X-Tanpopo-Request-Id"), "req_") {
		t.Fatalf("missing X-Tanpopo-Request-Id header: %q", resp.Header.Get("X-Tanpopo-Request-Id"))
	}

	var chatResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(syncBody, &chatResp); err != nil {
		t.Fatalf("decode sync chat completion: %v; body=%s", err, string(syncBody))
	}
	if chatResp.Object != "chat.completion" || chatResp.ID == "" || chatResp.Created == 0 ||
		len(chatResp.Choices) != 1 || chatResp.Choices[0].Message.Content != "ok-from-fakellm" ||
		chatResp.Usage.TotalTokens != 15 {
		t.Fatalf("unexpected sync schema response (CT-07): %+v", chatResp)
	}

	// Verify FakeLLMServer received unsealed secret & upstream model rewrite, ZERO vk_tp_*.
	reqs := fake.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 FakeLLM request, got %d", len(reqs))
	}
	if reqs[0].Headers.Get("Authorization") != "Bearer "+canarySec {
		t.Fatalf("FakeLLM did not receive unsealed Authorization canary header: %q", reqs[0].Headers.Get("Authorization"))
	}
	if strings.Contains(string(reqs[0].Body), createdVK.SecretKey) {
		t.Fatal("Virtual Key leaked into upstream request body")
	}
	if !strings.Contains(string(reqs[0].Body), `"acme-gpt-upstream"`) {
		t.Fatalf("expected upstream model rewrite in request body, got %s", string(reqs[0].Body))
	}

	// --- AC-1b: Streaming SSE /v1/chat/completions + tool_calls delta via FakeLLMServer ---
	fake.EnqueueFault("malformed_tool_json=true")
	sseResp, err := postChat(`{"model":"acme/chat-1","stream":true,"tools":[{"type":"function","function":{"name":"get_weather"}}],"messages":[{"role":"user","content":"weather in tokyo"}]}`, nil)
	if err != nil {
		t.Fatalf("SSE chat request failed: %v", err)
	}
	sseBytes, _ := io.ReadAll(sseResp.Body)
	_ = sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status=%d want 200; body=%s", sseResp.StatusCode, string(sseBytes))
	}
	if !strings.HasPrefix(sseResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("unexpected SSE Content-Type: %q", sseResp.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(sseResp.Header.Get("X-Tanpopo-Request-Id"), "req_") {
		t.Fatalf("missing X-Tanpopo-Request-Id on SSE response")
	}
	sseStr := string(sseBytes)
	if !strings.Contains(sseStr, `"tool_calls"`) || !strings.Contains(sseStr, `"get_weather"`) || !strings.Contains(sseStr, "data: [DONE]") {
		t.Fatalf("SSE output missing expected tool_calls delta or [DONE] marker:\n%s", sseStr)
	}

	// Verify sync + tool_calls OpenAI wire schema round-trip cleanly on tier_declarative & tier_wasm.
	// Note: for tier_declarative, point connection to trickleSrv (whose URL is on 127.0.0.1);
	// for tier_wasm, Wasm runtime targetURL is bound to fake.URL(); we verify tool_calls sync request
	// hit FakeLLMServer with tools intact in the forwarded JSON!
	if tier == "declarative" {
		trickleMu.Lock()
		trickleMode = "tool_sync"
		trickleMu.Unlock()
		if _, err := st.RawDB().ExecContext(ctx, `
			UPDATE provider_connections SET endpoint_base_url = ?, version = version + 1 WHERE id = ?
		`, trickleSrv.URL, primaryConn.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := st.RawDB().ExecContext(ctx, `
			INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
			VALUES (?, 'provider_connection', ?, 'UPSERT', 0, ?)
		`, org.ID, primaryConn.ID, nowFn()+1); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Refresh(ctx); err != nil {
			t.Fatal(err)
		}

		toolResp, err := postChat(`{"model":"acme/chat-1","tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],"messages":[{"role":"user","content":"call tool"}]}`, nil)
		if err != nil {
			t.Fatalf("sync tool_calls request failed: %v", err)
		}
		toolBody, _ := io.ReadAll(toolResp.Body)
		_ = toolResp.Body.Close()
		if toolResp.StatusCode != http.StatusOK {
			t.Fatalf("sync tool_calls status=%d; body=%s", toolResp.StatusCode, string(toolBody))
		}
		if !strings.Contains(string(toolBody), `"tool_calls"`) || !strings.Contains(string(toolBody), `"call_weather_1"`) {
			t.Fatalf("expected OpenAI tool_calls in normalized sync response, got: %s", string(toolBody))
		}

		// --- NEG-2 (FZ-03): > 2 MiB unterminated SSE line aborts boundedly ---
		trickleMu.Lock()
		trickleMode = "oversized_sse"
		trickleMu.Unlock()
		bigResp, err := postChat(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"oversized sse"}]}`, nil)
		if err != nil {
			t.Fatalf("oversized SSE request failed: %v", err)
		}
		bigBody, _ := io.ReadAll(bigResp.Body)
		_ = bigResp.Body.Close()
		if bigResp.StatusCode != http.StatusBadGateway || !strings.Contains(string(bigBody), "upstream_malformed_response") {
			t.Fatalf("expected 502 upstream_malformed_response for >2MiB SSE line, got status=%d body=%s", bigResp.StatusCode, string(bigBody))
		}

		// --- AC-2 (CT-11): Downstream client abort after 3rd trickled frame closes upstream in < 500 ms ---
		trickleMu.Lock()
		trickleMode = "trickle"
		trickleMu.Unlock()
		// Drain stale events if any.
		for len(trickleDisconnectCh) > 0 {
			<-trickleDisconnectCh
		}

		streamCtx, cancelStream := context.WithCancel(ctx)
		trickleResp, err := postChat(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"trickle"}]}`, streamCtx)
		if err != nil {
			t.Fatalf("trickle request failed: %v", err)
		}
		sc := bufio.NewScanner(trickleResp.Body)
		framesSeen := 0
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data:") {
				framesSeen++
				if framesSeen >= 3 {
					break
				}
			}
		}
		cancelAt := time.Now()
		cancelStream()
		_ = trickleResp.Body.Close()

		select {
		case discAt := <-trickleDisconnectCh:
			if elapsed := discAt.Sub(cancelAt); elapsed > 500*time.Millisecond {
				t.Fatalf("AC-2 violation: upstream disconnect took %v > 500 ms", elapsed)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("AC-2 violation: upstream trickle socket did not close within 1s after client disconnect")
		}

		// --- AUTH-06: Emergency key revocation aborts active SSE stream < 500 ms ---
		for len(trickleDisconnectCh) > 0 {
			<-trickleDisconnectCh
		}
		revStreamResp, err := postChat(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"revoke mid stream"}]}`, nil)
		if err != nil {
			t.Fatalf("revoke stream request failed: %v", err)
		}
		revScanner := bufio.NewScanner(revStreamResp.Body)
		for revScanner.Scan() {
			if strings.HasPrefix(revScanner.Text(), "data:") {
				break
			}
		}
		revokeStart := time.Now()
		if _, rerr := vkSvc.Revoke(ctx, vkey.RevokeParams{
			OrgID:           org.ID,
			KeyID:           createdVK.Summary.ID,
			ExpectedVersion: createdVK.Summary.Version,
			Mode:            vkey.RevokeModeEmergency,
			ActorID:         "prc_op_m1",
		}); rerr != nil {
			t.Fatalf("vkey emergency revoke failed: %v", rerr)
		}
		select {
		case discAt := <-trickleDisconnectCh:
			if elapsed := discAt.Sub(revokeStart); elapsed > 500*time.Millisecond {
				t.Fatalf("AUTH-06 violation: revoked stream disconnect took %v > 500 ms", elapsed)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("AUTH-06 violation: active stream did not abort within 1s of emergency key revocation")
		}
		_, _ = io.ReadAll(revStreamResp.Body)
		_ = revStreamResp.Body.Close()

		// Re-issue key and restore connection to FakeLLMServer for NEG-1 mid-stream cut.
		newVK, err := vkSvc.Create(ctx, vkey.CreateParams{
			OrgID:                org.ID,
			ProjectID:            proj.ID,
			EnvID:                env.ID,
			Name:                 "m1-e2e-key-2",
			AllowedPlanes:        "PROVIDER",
			MaxConcurrentStreams: 16,
		})
		if err != nil {
			t.Fatal(err)
		}
		createdVK = newVK
		if _, err := st.RawDB().ExecContext(ctx, `
			UPDATE provider_connections SET endpoint_base_url = ?, version = version + 1 WHERE id = ?
		`, fake.URL(), primaryConn.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := st.RawDB().ExecContext(ctx, `
			INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
			VALUES (?, 'provider_connection', ?, 'UPSERT', 0, ?)
		`, org.ID, primaryConn.ID, nowFn()+2); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		_ = primaryDepl
	} else {
		// Verify Wasm sync forwarding also preserved tools array in UpstreamHttpSpec BodyBytes!
		fake.ResetRequests()
		toolSyncResp, err := postChat(`{"model":"acme/chat-1","tools":[{"type":"function","function":{"name":"get_weather"}}],"messages":[{"role":"user","content":"tools via wasm"}]}`, nil)
		if err != nil {
			t.Fatalf("wasm tool sync request failed: %v", err)
		}
		_ = toolSyncResp.Body.Close()
		wReqs := fake.Requests()
		if len(wReqs) != 1 || !strings.Contains(string(wReqs[0].Body), `"get_weather"`) {
			t.Fatalf("expected Wasm request transform to forward tools array, got reqs=%d", len(wReqs))
		}

		// Also verify AC-2 delayed first-byte disconnect propagation via FakeLLMServer (< 500ms) for tier_wasm.
		fake.EnqueueFault("delay_first_byte_ms=1500")
		cancelCtx, cancelFn := context.WithCancel(ctx)
		go func() {
			time.Sleep(150 * time.Millisecond)
			cancelFn()
		}()
		t0 := time.Now()
		dResp, derr := postChat(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"cancel wasm"}]}`, cancelCtx)
		if derr == nil && dResp != nil {
			_ = dResp.Body.Close()
		}
		select {
		case <-fake.DisconnectEvents():
			if elapsed := time.Since(t0); elapsed > 700*time.Millisecond {
				t.Fatalf("delayed abort took %v", elapsed)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("FakeLLMServer did not observe disconnect within 1s for tier_wasm")
		}

		// --- Tier-2 Wasm NEG-2 (FZ-03) & AUTH-06 against trickleSrv ---
		// Install a secondary Tier-2 Wasm extension targeting trickleSrv so both AUTH-06 emergency
		// revocation and > 2 MiB unterminated SSE line checks run through Tier-2 wazero too!
		trickleExtDir := filepath.Join(tmp, "acme-wasm-trickle")
		if err := os.MkdirAll(filepath.Join(trickleExtDir, "dist"), 0o755); err != nil {
			t.Fatal(err)
		}
		trickleWasm := buildAcmeChatWASM(trickleSrv.URL + "/v1/chat/completions")
		if err := os.WriteFile(filepath.Join(trickleExtDir, "dist", "adapter.wasm"), trickleWasm, 0o600); err != nil {
			t.Fatal(err)
		}
		trSum := sha256.Sum256(trickleWasm)
		trManifest := fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/acme-wasm-trickle"
  name: "AcmeAI Tier-2 Wasm Trickle Adapter"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "dist/adapter.wasm"
  sha256: %q
  quotas:
    maxMemoryMib: 16
    maxCpuMsPerCall: 50
    maxStreamChunkCpuMs: 25
capabilities:
  - type: "provider_adapter"
    adapterSlug: "acme-wasm-trickle"
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
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:transform_request"
    - "provider:transform_response"
    - "provider:transform_stream_chunk"
    - "provider:normalize_error"
`, hex.EncodeToString(trSum[:]))
		if err := os.WriteFile(filepath.Join(trickleExtDir, "manifest.yaml"), []byte(trManifest), 0o600); err != nil {
			t.Fatal(err)
		}
		trTPKG := filepath.Join(tmp, "acme-wasm-trickle-1.0.0.tpkg")
		trPack, err := testharness.PackExtension(ctx, trickleExtDir, trTPKG)
		if err != nil {
			t.Fatal(err)
		}
		trSig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload("@acme/acme-wasm-trickle", "1.0.0", trPack.SHA256))
		trStaged, err := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
			ArchivePath: trTPKG,
			Source:      installer.SourceSpec{Kind: installer.SourceKindLocalArchive, URI: "./acme-wasm-trickle-1.0.0.tpkg", ExpectedSHA256: trPack.SHA256},
			Signature:   installer.ProvenanceSignature{PublisherOrRootSig: trSig},
			ActorID:     "prc_op_m1",
			RequestID:   "req_m1_tr_stage",
			TargetOrgID: org.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		trRec, err := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
			InstallationID:      trStaged.Installation.InstallationID,
			ExpectedVersion:     trStaged.Installation.Version,
			ApproverPrincipalID: "prc_op_m1",
			ActivateImmediately: true,
			OrgID:               org.ID,
			RequestID:           "req_m1_tr_approve",
		})
		if err != nil {
			t.Fatal(err)
		}
		trAdp, err := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
			InstallationID:    trRec.InstallationID,
			AdapterSlug:       "acme-wasm-trickle",
			SupportedDialects: []string{"openai"},
			AllowedHosts:      []string{"127.0.0.1"},
		})
		if err != nil {
			t.Fatal(err)
		}
		trConn, err := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
			OrgID:           org.ID,
			AdapterID:       trAdp.ID,
			Name:            "acme-wasm-trickle-conn",
			EndpointBaseURL: trickleSrv.URL,
			AllowPrivateIP:  true,
			AllowedCIDRs:    []string{"127.0.0.1/32"},
			DataRegion:      "us",
			BillingMode:     catalog.BillingModeCommercialAPI,
			AuthMode:        catalog.AuthModeAPIKey,
			SecretBindings:  []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind}},
		})
		if err != nil {
			t.Fatal(err)
		}
		trConn, err = catRepo.TransitionConnectionState(ctx, org.ID, trConn.ID, catalog.ConnStateValidating, trConn.Version, "validate")
		if err != nil {
			t.Fatal(err)
		}
		trConn, err = catRepo.TransitionConnectionState(ctx, org.ID, trConn.ID, catalog.ConnStateActive, trConn.Version, "activate")
		if err != nil {
			t.Fatal(err)
		}
		trLMod, err := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{
			OrgID:            &org.ID,
			CanonicalSlug:    "acme/chat-wasm-trickle",
			ModalityKind:     catalog.ModalityChat,
			ContextWindow:    128000,
			MaxOutputTokens:  4096,
			BaseCapabilities: map[string]string{"streaming": "VERIFIED"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
			ID:                 "dpl_wasm_trickle",
			OrgID:              org.ID,
			ConnectionID:       trConn.ID,
			LogicalModelID:     trLMod.ID,
			UpstreamModelName:  "acme-gpt-trickle",
			AssuranceLevel:     catalog.AssuranceVerified,
			State:              catalog.DeployStateActiveHealthy,
			ConnectTimeoutMs:   5000,
			FirstByteTimeoutMs: 5000,
			IdleChunkTimeoutMs: 5000,
		}); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Refresh(ctx); err != nil {
			t.Fatal(err)
		}

		// Tier-2 Wasm NEG-2 (FZ-03) > 2 MiB unterminated SSE line
		trickleMu.Lock()
		trickleMode = "oversized_sse"
		trickleMu.Unlock()
		bigWasmResp, err := postChat(`{"model":"acme/chat-wasm-trickle","stream":true,"messages":[{"role":"user","content":"oversized wasm"}]}`, nil)
		if err != nil {
			t.Fatalf("wasm oversized SSE request failed: %v", err)
		}
		bigWasmBody, _ := io.ReadAll(bigWasmResp.Body)
		_ = bigWasmResp.Body.Close()
		if bigWasmResp.StatusCode != http.StatusBadGateway || !strings.Contains(string(bigWasmBody), "upstream_malformed_response") {
			t.Fatalf("expected 502 upstream_malformed_response for >2MiB SSE line in tier_wasm, got status=%d body=%s", bigWasmResp.StatusCode, string(bigWasmBody))
		}

		// Tier-2 Wasm AUTH-06 Emergency key revocation aborts active SSE stream < 500 ms
		trickleMu.Lock()
		trickleMode = "trickle"
		trickleMu.Unlock()
		for len(trickleDisconnectCh) > 0 {
			<-trickleDisconnectCh
		}
		revWasmResp, err := postChat(`{"model":"acme/chat-wasm-trickle","stream":true,"messages":[{"role":"user","content":"revoke wasm stream"}]}`, nil)
		if err != nil {
			t.Fatalf("wasm revoke stream request failed: %v", err)
		}
		revWasmScanner := bufio.NewScanner(revWasmResp.Body)
		for revWasmScanner.Scan() {
			if strings.HasPrefix(revWasmScanner.Text(), "data:") {
				break
			}
		}
		revWasmStart := time.Now()
		if _, rerr := vkSvc.Revoke(ctx, vkey.RevokeParams{
			OrgID:           org.ID,
			KeyID:           createdVK.Summary.ID,
			ExpectedVersion: createdVK.Summary.Version,
			Mode:            vkey.RevokeModeEmergency,
			ActorID:         "prc_op_m1",
		}); rerr != nil {
			t.Fatalf("vkey emergency revoke (wasm) failed: %v", rerr)
		}
		select {
		case discAt := <-trickleDisconnectCh:
			if elapsed := discAt.Sub(revWasmStart); elapsed > 500*time.Millisecond {
				t.Fatalf("AUTH-06 tier_wasm violation: revoked stream disconnect took %v > 500 ms", elapsed)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("AUTH-06 tier_wasm violation: active stream did not abort within 1s of emergency key revocation")
		}
		_, _ = io.ReadAll(revWasmResp.Body)
		_ = revWasmResp.Body.Close()

		// Re-issue active virtual key for subsequent NEG-1 mid-stream cut verification.
		newWasmVK, err := vkSvc.Create(ctx, vkey.CreateParams{
			OrgID:                org.ID,
			ProjectID:            proj.ID,
			EnvID:                env.ID,
			Name:                 "m1-e2e-wasm-key-2",
			AllowedPlanes:        "PROVIDER",
			MaxConcurrentStreams: 16,
		})
		if err != nil {
			t.Fatal(err)
		}
		createdVK = newWasmVK
		if err := mgr.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// --- NEG-1 (CT-10 / FMEA-01): Mid-stream TCP RST after 2 chunks -> mid_stream_abort, 0 failover, 0 [DONE] ---
	fake.ResetRequests()
	backupFake.ResetRequests()
	fake.EnqueueFault("tcp_rst_after_chunks=2")

	cutResp, err := postChat(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"cut mid stream"}]}`, nil)
	if err != nil {
		t.Fatalf("mid-stream cut request failed: %v", err)
	}
	cutBodyBytes, _ := io.ReadAll(cutResp.Body)
	_ = cutResp.Body.Close()
	if cutResp.StatusCode != http.StatusOK {
		t.Fatalf("expected SSE 200 header before chunk-2 cut, got %d; body=%s", cutResp.StatusCode, string(cutBodyBytes))
	}
	cutStr := string(cutBodyBytes)
	if !strings.Contains(cutStr, "chunk-0") || !strings.Contains(cutStr, "chunk-1") {
		t.Fatalf("expected chunks 0 & 1 flushed prior to RST:\n%s", cutStr)
	}
	if !strings.Contains(cutStr, `"mid_stream_abort"`) || !strings.Contains(cutStr, "event: error") {
		t.Fatalf("FMEA-01 violation: expected canonical event: error mid_stream_abort:\n%s", cutStr)
	}
	if strings.Contains(cutStr, "[DONE]") {
		t.Fatalf("FMEA-01 violation: synthetic [DONE] emitted after mid-stream cut:\n%s", cutStr)
	}
	if len(fake.Requests()) != 1 {
		t.Fatalf("FMEA-01 violation: expected exactly 1 primary upstream attempt (0 retries), got %d", len(fake.Requests()))
	}
	if len(backupFake.Requests()) != 0 {
		t.Fatalf("FMEA-01 violation: expected 0 failover calls to backup FakeLLMServer, got %d", len(backupFake.Requests()))
	}

	// Final CanaryScanner sweep: 0 leaked canary or virtual keys across responses and audit logs!
	scanner := canary.NewScanner(canarySec, createdVK.SecretKey)
	scanner.AssertClean(t,
		canary.NamedData{Source: "sync_body", Data: syncBody},
		canary.NamedData{Source: "sse_body", Data: sseBytes},
		canary.NamedData{Source: "cut_body", Data: cutBodyBytes},
	)
	scanner.AssertCleanBuffer(t, "audit_events", &audit.buf)
}

// buildAcmeChatWASM compiles a compact pure-Go WebAssembly 1.0 module implementing Tanpopo's
// canonical slice ABI (transform_request, transform_response, transform_stream_chunk, normalize_error).
// It parses base64 fields dynamically out of linear memory so real FakeLLMServer responses/chunks
// round-trip verbatim through wazero without hardcoding mock completions.
func buildAcmeChatWASM(targetURL string) []byte {
	const (
		outBase   = uint32(131072) // scratch output buffer at 128 KiB
		dataStart = uint32(65536)  // static segments start at 64 KiB
	)

	type seg struct {
		off  uint32
		data []byte
	}
	var segs []seg
	cursor := dataStart
	addSeg := func(s string) (uint32, uint32) {
		off := cursor
		b := []byte(s)
		segs = append(segs, seg{off: off, data: b})
		cursor += uint32(len(b)) + 4
		return off, uint32(len(b))
	}

	reqNeedlePtr, reqNeedleLen := addSeg(`"normalized_request_json":"`)
	reqPrefixPtr, reqPrefixLen := addSeg(fmt.Sprintf(`{"method":"POST","target_url":%q,"credential_injections":[{"secret_slot_name":"api_key","target_location":"CREDENTIAL_TARGET_LOCATION_HEADER","field_name":"Authorization","value_prefix":"Bearer "}],"body_bytes":"`, targetURL))
	reqSuffixPtr, reqSuffixLen := addSeg(`"}`)

	respNeedlePtr, respNeedleLen := addSeg(`"raw_response_body":"`)
	respPrefixPtr, respPrefixLen := addSeg(`{"normalized_response_json":"`)
	respSuffixPtr, respSuffixLen := addSeg(`","usage":{"input_tokens":10,"output_tokens":5,"accuracy":"USAGE_ACCURACY_EXACT"}}`)

	chunkNeedlePtr, chunkNeedleLen := addSeg(`"raw_upstream_chunk":"`)
	chunkPrefixPtr, chunkPrefixLen := addSeg(`{"normalized_sse_frames":["`)
	chunkSuffixPtr, chunkSuffixLen := addSeg(`"],"stream_completed":false}`)

	errJSONPtr, errJSONLen := addSeg(`{"canonical_code":"upstream_error","error_type":"upstream_error","sanitized_public_message":"Upstream failure (acme-wasm)","retryable_pre_stream":true}`)

	uleb := func(v uint32) []byte {
		var out []byte
		for {
			b := byte(v & 0x7f)
			v >>= 7
			if v != 0 {
				b |= 0x80
			}
			out = append(out, b)
			if v == 0 {
				return out
			}
		}
	}
	sleb64 := func(v int64) []byte {
		var out []byte
		for {
			b := byte(v & 0x7f)
			v >>= 7
			more := !((v == 0 && (b&0x40) == 0) || (v == -1 && (b&0x40) != 0))
			if more {
				b |= 0x80
			}
			out = append(out, b)
			if !more {
				return out
			}
		}
	}
	wstr := func(s string) []byte {
		return append(uleb(uint32(len(s))), s...)
	}
	wsec := func(id byte, payload []byte) []byte {
		out := []byte{id}
		out = append(out, uleb(uint32(len(payload)))...)
		return append(out, payload...)
	}
	i32Const := func(v uint32) []byte {
		return append([]byte{0x41}, sleb64(int64(int32(v)))...)
	}

	// Section 1: Types
	// 0: (i32, i32) -> i64
	// 1: (8x i32) -> i64
	// 2: (3x i32) -> ()
	typeSec := []byte{0x03}
	typeSec = append(typeSec, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e)
	typeSec = append(typeSec, 0x60, 0x08, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7e)
	typeSec = append(typeSec, 0x60, 0x03, 0x7f, 0x7f, 0x7f, 0x00)

	// Section 3: Functions (0=memcpy, 1=extract_b64_wrap, 2..5=exports)
	fnSec := []byte{0x06, 0x02, 0x01, 0x00, 0x00, 0x00, 0x00}

	// Section 5: Memory (min 4 pages = 256 KiB, max 16 pages)
	memSec := []byte{0x01, 0x01, 0x04, 0x10}

	// Section 7: Exports
	var expSec []byte
	expSec = append(expSec, 0x05)
	expSec = append(expSec, wstr("memory")...)
	expSec = append(expSec, 0x02, 0x00)
	for i, name := range []string{"transform_request", "transform_response", "transform_stream_chunk", "normalize_error"} {
		expSec = append(expSec, wstr(name)...)
		expSec = append(expSec, 0x00, byte(2+i))
	}

	// Func 0: memcpy(dst=0, src=1, len=2)
	var fn0 []byte
	fn0 = append(fn0, 0x01, 0x01, 0x7f) // 1 local: i32 i (idx 3)
	fn0 = append(fn0, 0x02, 0x40)       // block
	fn0 = append(fn0, 0x03, 0x40)       // loop
	fn0 = append(fn0, 0x20, 0x03, 0x20, 0x02, 0x4f, 0x0d, 0x01)
	fn0 = append(fn0, 0x20, 0x00, 0x20, 0x03, 0x6a) // dst+i
	fn0 = append(fn0, 0x20, 0x01, 0x20, 0x03, 0x6a) // src+i
	fn0 = append(fn0, 0x2d, 0x00, 0x00)             // i32.load8_u
	fn0 = append(fn0, 0x3a, 0x00, 0x00)             // i32.store8
	fn0 = append(fn0, 0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03)
	fn0 = append(fn0, 0x0c, 0x00, 0x0b, 0x0b, 0x0b)

	// Func 1: extract_b64_wrap(inPtr=0, inLen=1, needlePtr=2, needleLen=3, prefixPtr=4, prefixLen=5, suffixPtr=6, suffixLen=7) -> i64
	// Locals: 8=curOut, 9=i, 10=j, 11=match, 12=b
	var fn1 []byte
	fn1 = append(fn1, 0x01, 0x05, 0x7f)
	// memcpy(outBase, prefixPtr, prefixLen)
	fn1 = append(fn1, i32Const(outBase)...)
	fn1 = append(fn1, 0x20, 0x04, 0x20, 0x05, 0x10, 0x00)
	// curOut = outBase + prefixLen
	fn1 = append(fn1, i32Const(outBase)...)
	fn1 = append(fn1, 0x20, 0x05, 0x6a, 0x21, 0x08)

	// block $searchDone
	fn1 = append(fn1, 0x02, 0x40)
	// if inLen < needleLen: br $searchDone
	fn1 = append(fn1, 0x20, 0x01, 0x20, 0x03, 0x49, 0x0d, 0x00)
	// loop $scan (i=9 starts 0)
	fn1 = append(fn1, 0x03, 0x40)
	// if i + needleLen > inLen: br $searchDone (depth 1)
	fn1 = append(fn1, 0x20, 0x09, 0x20, 0x03, 0x6a, 0x20, 0x01, 0x4b, 0x0d, 0x01)
	// match = 1; j = 0
	fn1 = append(fn1, 0x41, 0x01, 0x21, 0x0b, 0x41, 0x00, 0x21, 0x0a)
	// block $cmpDone; loop $cmp
	fn1 = append(fn1, 0x02, 0x40, 0x03, 0x40)
	fn1 = append(fn1, 0x20, 0x0a, 0x20, 0x03, 0x4f, 0x0d, 0x01) // if j >= needleLen: br $cmpDone
	// mem8[inPtr + i + j] != mem8[needlePtr + j]
	fn1 = append(fn1, 0x20, 0x00, 0x20, 0x09, 0x6a, 0x20, 0x0a, 0x6a, 0x2d, 0x00, 0x00)
	fn1 = append(fn1, 0x20, 0x02, 0x20, 0x0a, 0x6a, 0x2d, 0x00, 0x00, 0x47) // i32.ne
	fn1 = append(fn1, 0x04, 0x40, 0x41, 0x00, 0x21, 0x0b, 0x0c, 0x02, 0x0b) // if ne { match=0; br $cmpDone }
	fn1 = append(fn1, 0x20, 0x0a, 0x41, 0x01, 0x6a, 0x21, 0x0a, 0x0c, 0x00, 0x0b, 0x0b)
	// if match != 0
	fn1 = append(fn1, 0x20, 0x0b, 0x04, 0x40)
	fn1 = append(fn1, 0x41, 0x00, 0x21, 0x0a) // j = 0
	// block $copyDone; loop $copy
	fn1 = append(fn1, 0x02, 0x40, 0x03, 0x40)
	// addr = inPtr + i + needleLen + j; if addr >= inPtr + inLen: br $copyDone
	fn1 = append(fn1, 0x20, 0x00, 0x20, 0x09, 0x6a, 0x20, 0x03, 0x6a, 0x20, 0x0a, 0x6a)
	fn1 = append(fn1, 0x22, 0x0b) // local.tee match (reuse as addr)
	fn1 = append(fn1, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x4f, 0x0d, 0x01)
	// b = mem8[addr]; if b == '"' (34): br $copyDone
	fn1 = append(fn1, 0x20, 0x0b, 0x2d, 0x00, 0x00, 0x22, 0x0c, 0x41, 0x22, 0x46, 0x0d, 0x01)
	// mem8[curOut] = b; curOut++; j++; br $copy
	fn1 = append(fn1, 0x20, 0x08, 0x20, 0x0c, 0x3a, 0x00, 0x00)
	fn1 = append(fn1, 0x20, 0x08, 0x41, 0x01, 0x6a, 0x21, 0x08)
	fn1 = append(fn1, 0x20, 0x0a, 0x41, 0x01, 0x6a, 0x21, 0x0a)
	fn1 = append(fn1, 0x0c, 0x00, 0x0b, 0x0b)
	// br $searchDone (outermost block is at depth 1 from inside `if match != 0`)
	fn1 = append(fn1, 0x0c, 0x02, 0x0b)
	// i++; br $scan
	fn1 = append(fn1, 0x20, 0x09, 0x41, 0x01, 0x6a, 0x21, 0x09, 0x0c, 0x00, 0x0b, 0x0b)

	// memcpy(curOut, suffixPtr, suffixLen)
	fn1 = append(fn1, 0x20, 0x08, 0x20, 0x06, 0x20, 0x07, 0x10, 0x00)
	// curOut += suffixLen
	fn1 = append(fn1, 0x20, 0x08, 0x20, 0x07, 0x6a, 0x21, 0x08)
	// return (i64(outBase) << 32) | i64(curOut - outBase)
	fn1 = append(fn1, 0x42)
	fn1 = append(fn1, sleb64(int64(uint64(outBase)<<32))...)
	fn1 = append(fn1, 0x20, 0x08)
	fn1 = append(fn1, i32Const(outBase)...)
	fn1 = append(fn1, 0x6b, 0xad, 0x84, 0x0b) // i32.sub -> i64.extend_i32_u -> i64.or -> end

	makeWrapper := func(nPtr, nLen, pPtr, pLen, sPtr, sLen uint32) []byte {
		var b []byte
		b = append(b, 0x00) // 0 locals
		b = append(b, 0x20, 0x00, 0x20, 0x01)
		b = append(b, i32Const(nPtr)...)
		b = append(b, i32Const(nLen)...)
		b = append(b, i32Const(pPtr)...)
		b = append(b, i32Const(pLen)...)
		b = append(b, i32Const(sPtr)...)
		b = append(b, i32Const(sLen)...)
		b = append(b, 0x10, 0x01, 0x0b)
		return b
	}

	fn2 := makeWrapper(reqNeedlePtr, reqNeedleLen, reqPrefixPtr, reqPrefixLen, reqSuffixPtr, reqSuffixLen)
	fn3 := makeWrapper(respNeedlePtr, respNeedleLen, respPrefixPtr, respPrefixLen, respSuffixPtr, respSuffixLen)
	fn4 := makeWrapper(chunkNeedlePtr, chunkNeedleLen, chunkPrefixPtr, chunkPrefixLen, chunkSuffixPtr, chunkSuffixLen)

	var fn5 []byte
	fn5 = append(fn5, 0x00, 0x42)
	fn5 = append(fn5, sleb64(int64((uint64(errJSONPtr)<<32)|uint64(errJSONLen)))...)
	fn5 = append(fn5, 0x0b)

	funcs := [][]byte{fn0, fn1, fn2, fn3, fn4, fn5}
	var codeSec []byte
	codeSec = append(codeSec, uleb(uint32(len(funcs)))...)
	for _, f := range funcs {
		codeSec = append(codeSec, uleb(uint32(len(f)))...)
		codeSec = append(codeSec, f...)
	}

	var dataSec []byte
	dataSec = append(dataSec, uleb(uint32(len(segs)))...)
	for _, s := range segs {
		dataSec = append(dataSec, 0x00)
		dataSec = append(dataSec, i32Const(s.off)...)
		dataSec = append(dataSec, 0x0b)
		dataSec = append(dataSec, uleb(uint32(len(s.data)))...)
		dataSec = append(dataSec, s.data...)
	}

	var out []byte
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	out = append(out, wsec(1, typeSec)...)
	out = append(out, wsec(3, fnSec)...)
	out = append(out, wsec(5, memSec)...)
	out = append(out, wsec(7, expSec)...)
	out = append(out, wsec(10, codeSec)...)
	out = append(out, wsec(11, dataSec)...)
	return out
}
