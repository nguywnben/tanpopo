// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package broker

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
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
	mu     sync.Mutex
	events []port.AuditEvent
}

func (a *auditRecorder) Append(_ context.Context, ev port.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, ev)
	return nil
}

func (a *auditRecorder) snapshot() []port.AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]port.AuditEvent, len(a.events))
	copy(out, a.events)
	return out
}

func TestAudienceMatchesEdgeCases(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }
	u, err := url.Parse("https://api.openai.com:443/v1/chat/completions")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	if ok, _ := AudienceMatches(u, ptr("https://api.openai.com")); !ok {
		t.Fatalf("expected implicit port 443 match")
	}
	if ok, _ := AudienceMatches(u, ptr("https://api.openai.com/v1")); !ok {
		t.Fatalf("expected path prefix match")
	}
	if ok, _ := AudienceMatches(u, ptr("api.openai.com")); !ok {
		t.Fatalf("expected hostname match")
	}
	if ok, _ := AudienceMatches(u, ptr("http://api.openai.com")); ok {
		t.Fatalf("expected scheme mismatch to fail")
	}
	if ok, _ := AudienceMatches(u, ptr("https://api.openai.com:8443")); ok {
		t.Fatalf("expected port mismatch to fail")
	}
	if ok, _ := AudienceMatches(u, ptr("https://api.openai.com/v2")); ok {
		t.Fatalf("expected path prefix mismatch to fail")
	}
	if ok, _ := AudienceMatches(u, ptr("   ")); ok {
		t.Fatalf("expected blank non-nil BoundAudience to fail")
	}
}

type slotKey struct {
	orgID string
	conn  string
	slot  string
}

type mapResolver struct {
	mu    sync.RWMutex
	slots map[slotKey]*ResolvedSlot
	conns map[string]*egress.Policy
}

func newMapResolver() *mapResolver {
	return &mapResolver{
		slots: make(map[slotKey]*ResolvedSlot),
		conns: make(map[string]*egress.Policy),
	}
}

func (r *mapResolver) put(slot *ResolvedSlot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slots[slotKey{orgID: slot.OrgID, conn: slot.ConnectionID, slot: slot.SlotName}] = slot
}

func (r *mapResolver) ResolveSlot(_ context.Context, orgID, connectionID, slotName string) (*ResolvedSlot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.slots[slotKey{orgID: orgID, conn: connectionID, slot: slotName}]
	if !ok {
		return nil, port.ErrNotFound
	}
	return s, nil
}

func (r *mapResolver) ResolveConnectionPolicy(_ context.Context, orgID, connectionID string) (*egress.Policy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.conns[orgID+"|"+connectionID]
	if !ok {
		return nil, port.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

type countingVault struct {
	inner       VaultUnsealer
	unsealCalls atomic.Int64
}

func (c *countingVault) UnsealWithCapability(ctx context.Context, cap vault.UnsealCapability, orgID, secretID string) (*vault.UnsealedSecret, error) {
	c.unsealCalls.Add(1)
	return c.inner.UnsealWithCapability(ctx, cap, orgID, secretID)
}

type mockHostResolver struct {
	calls atomic.Int64
	fn    func(call int64, host string) ([]netip.Addr, error)
}

func (m *mockHostResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	n := m.calls.Add(1)
	return m.fn(n, host)
}

type brokerHarness struct {
	st       *sqlite.Store
	vs       *vault.Store
	counted  *countingVault
	resolver *mapResolver
	audit    *auditRecorder
	orgID    string
	connID   string
	scanner  *canary.Scanner
	broker   *BlindHostCredentialBroker
}

func newBrokerHarness(t *testing.T, dnsRes egress.HostResolver) *brokerHarness {
	t.Helper()
	ctx := context.Background()
	fc := clock.NewFakeClock(clock.DefaultFakeStart())
	dbPath := filepath.Join(t.TempDir(), "broker.db")

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	kekBytes := make([]byte, vault.KeySizeBytes)
	if _, err := rand.Read(kekBytes); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	kek, err := vault.NewStaticKEKProvider("kek_broker_test", kekBytes)
	clear(kekBytes)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	t.Cleanup(kek.Close)

	vs, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		NowMs:         func() int64 { return fc.Now().UnixMilli() },
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	t.Cleanup(vs.Close)

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		Slug:        "org-broker",
		DisplayName: "Org Broker",
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	resolver := newMapResolver()
	audit := &auditRecorder{}
	counted := &countingVault{inner: vs}
	capToken := vault.MintUnsealCapabilityForBroker()

	br, err := NewBlindBroker(Config{
		Resolver:     resolver,
		Vault:        counted,
		UnsealCap:    capToken,
		Audit:        audit,
		NowMs:        func() int64 { return fc.Now().UnixMilli() },
		HostResolver: dnsRes,
		ControlHook:  netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker: %v", err)
	}

	return &brokerHarness{
		st:       st,
		vs:       vs,
		counted:  counted,
		resolver: resolver,
		audit:    audit,
		orgID:    org.ID,
		connID:   idgen.MustNew(idgen.PrefixConnection),
		scanner:  canary.NewScanner(),
		broker:   br,
	}
}

func (h *brokerHarness) sealCanarySecret(t *testing.T, name string, boundAud *string, pt []byte) *vault.SecretMetadata {
	t.Helper()
	meta, err := h.vs.SealSecret(context.Background(), vault.SealSecretParams{
		OrgID:         h.orgID,
		Name:          name,
		SecretKind:    vault.SecretKindAPIKey,
		BoundAudience: boundAud,
		Plaintext:     pt,
	})
	if err != nil {
		t.Fatalf("SealSecret(%q): %v", name, err)
	}
	return meta
}

func assertNoCanaryInEvents(t *testing.T, scanner *canary.Scanner, events []port.AuditEvent) {
	t.Helper()
	for i, ev := range events {
		findings, err := scanner.ScanJSON(fmt.Sprintf("audit[%d]:%s", i, ev.EventType), ev)
		if err != nil {
			t.Fatalf("ScanJSON audit[%d]: %v", i, err)
		}
		if len(findings) > 0 {
			t.Fatalf("canary leak detected in audit event %d: %v", i, findings)
		}
	}
}

func TestBlindBrokerAC1FakeLLMInjectsBearerAndZeroLeakToPlugin(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := newBrokerHarness(t, nil)
	srv := fakellm.NewServer(t)

	canaryVal := canary.DefaultCanarySecret()
	aud := srv.URL()
	meta := h.sealCanarySecret(t, "openai-canary", &aud, []byte(canaryVal))

	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "api_key",
		SecretID:       meta.ID,
		BoundAudience:  &aud,
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})

	resp, err := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    srv.URL() + "/v1/chat/completions",
		SafeHeaders: map[string]string{
			"Content-Type": "application/json",
		},
		BodyBytes: []byte(`{"model":"gpt-4o","stream":false}`),
		CredentialInjections: []CredentialInjection{
			{
				SecretSlotName: "api_key",
				Location:       TargetBearer,
				FieldName:      "Authorization",
				ValuePrefix:    "Bearer ",
			},
		},
		RequestID: "req_ac1",
	})
	if err != nil {
		t.Fatalf("Execute AC-1: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (%s)", resp.StatusCode, string(resp.Body))
	}

	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(reqs))
	}
	if got := reqs[0].Headers.Get("Authorization"); got != "Bearer "+canaryVal {
		t.Fatalf("FakeLLMServer did not receive injected Bearer credential")
	}

	h.scanner.AssertClean(t, canary.NamedData{Source: "broker.body", Data: resp.Body})
	assertNoCanaryInEvents(t, h.scanner, h.audit.snapshot())
}

func TestBlindBrokerNEG1_CT14_BlocksUnapprovedHostPreDNS(t *testing.T) {
	t.Parallel()

	res := &mockHostResolver{
		fn: func(_ int64, _ string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
	}
	h := newBrokerHarness(t, res)
	aud := "https://api.example.com"
	meta := h.sealCanarySecret(t, "exfil-test", &aud, []byte(canary.DefaultCanarySecret()))
	h.resolver.put(&ResolvedSlot{
		OrgID:         h.orgID,
		ConnectionID:  h.connID,
		SlotName:      "api_key",
		SecretID:      meta.ID,
		BoundAudience: &aud,
		AllowedHosts:  []string{"api.example.com"},
		RequireTLS:    true,
	})

	_, err := h.broker.Execute(context.Background(), EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    "https://evil.attacker.invalid/steal",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "api_key", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if !errors.Is(err, egress.ErrHostDenied) {
		t.Fatalf("expected egress.ErrHostDenied, got %v", err)
	}
	if h.counted.unsealCalls.Load() != 0 {
		t.Fatalf("expected 0 vault unseals on host mismatch, got %d", h.counted.unsealCalls.Load())
	}
	if res.calls.Load() != 0 {
		t.Fatalf("expected 0 DNS lookups on host mismatch, got %d", res.calls.Load())
	}
	if len(h.audit.snapshot()) == 0 {
		t.Fatalf("expected security audit event on denied egress host")
	}
	assertNoCanaryInEvents(t, h.scanner, h.audit.snapshot())
}

func TestBlindBrokerNEG2_CT15_BlocksRedirectExfilAndMetadata(t *testing.T) {
	t.Parallel()

	var evilHits atomic.Int64
	evilServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		evilHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer evilServer.Close()

	var redirectTarget atomic.Value
	approved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dest, _ := redirectTarget.Load().(string)
		http.Redirect(w, r, dest, http.StatusFound)
	}))
	defer approved.Close()

	h := newBrokerHarness(t, nil)
	aud := approved.URL
	meta := h.sealCanarySecret(t, "redirect-key", &aud, []byte(canary.DefaultCanarySecret()))
	approvedAddr := strings.TrimPrefix(approved.URL, "http://")
	evilAddr := strings.TrimPrefix(evilServer.URL, "http://")
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "api_key",
		SecretID:       meta.ID,
		BoundAudience:  &aud,
		AllowedHosts:   []string{approvedAddr, evilAddr},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})

	for _, dest := range []string{
		"https://other-host.test/steal",
		"http://169.254.169.254/latest/meta-data/",
		evilServer.URL + "/cross-origin-even-if-listed",
	} {
		redirectTarget.Store(dest)
		_, err := h.broker.Execute(context.Background(), EgressRequest{
			OrgID:        h.orgID,
			ConnectionID: h.connID,
			Method:       http.MethodGet,
			TargetURL:    approved.URL + "/v1/models",
			CredentialInjections: []CredentialInjection{
				{SecretSlotName: "api_key", Location: TargetBearer, FieldName: "Authorization"},
			},
		})
		if !errors.Is(err, egress.ErrRedirectBlocked) {
			t.Fatalf("expected ErrRedirectBlocked for destination %s, got %v", dest, err)
		}
	}
	if evilHits.Load() != 0 {
		t.Fatalf("expected 0 requests delivered to cross-origin redirect target, got %d", evilHits.Load())
	}
	assertNoCanaryInEvents(t, h.scanner, h.audit.snapshot())
}

func TestBlindBrokerNEG3_CT16_Scrubs401EchoedSecretStripsSetCookieAndBansCookieInjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := newBrokerHarness(t, nil)
	srv := fakellm.NewServer(t)

	secretStr := canary.DefaultCanarySecret()
	aud := srv.URL()
	meta := h.sealCanarySecret(t, "echo-secret", &aud, []byte(secretStr))
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "api_key",
		SecretID:       meta.ID,
		BoundAudience:  &aud,
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})

	// 1. Trigger FakeLLMServer 401 echoing Authorization secret verbatim in error payload.
	resp, err := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    srv.URL() + "/v1/chat/completions",
		SafeHeaders: map[string]string{
			fakellm.FaultHeader: "echo_auth_header_in_error",
		},
		BodyBytes: []byte(`{"model":"gpt-4o"}`),
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "api_key", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if err != nil {
		t.Fatalf("expected scrubbed 401 response rather than transport error, got %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 status preserved after scrub, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(resp.Body), RedactedCredentialPlaceholder) {
		t.Fatalf("expected placeholder %q in scrubbed 401 body, got %q", RedactedCredentialPlaceholder, string(resp.Body))
	}
	h.scanner.AssertClean(t, canary.NamedData{Source: "401.body", Data: resp.Body})
	assertNoCanaryInEvents(t, h.scanner, h.audit.snapshot())

	// 2. Custom server setting Set-Cookie and echoing secret inside non-auth response header + base64 body.
	rawCustom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "session="+secretStr)
		w.Header().Set("Authorization", "Bearer "+secretStr)
		w.Header().Set("X-Debug-Echo", "leaked="+secretStr)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("raw=" + secretStr + " b64=" + base64.StdEncoding.EncodeToString([]byte(secretStr))))
	}))
	defer rawCustom.Close()
	rawAddr := strings.TrimPrefix(rawCustom.URL, "http://")
	metaAny := h.sealCanarySecret(t, "echo-secret-2", nil, []byte(secretStr))
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "custom_key",
		SecretID:       metaAny.ID,
		AllowedHosts:   []string{rawAddr},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})

	resp2, err := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    rawCustom.URL + "/err",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "custom_key", Location: TargetHeader, FieldName: "X-Api-Key"},
		},
	})
	if err != nil {
		t.Fatalf("Execute custom 500: %v", err)
	}
	if resp2.SanitizedHeaders.Get("Set-Cookie") != "" || resp2.SanitizedHeaders.Get("Authorization") != "" {
		t.Fatalf("expected Set-Cookie and Authorization stripped from SanitizedHeaders")
	}
	if got := resp2.SanitizedHeaders.Get("X-Debug-Echo"); got != "leaked="+RedactedCredentialPlaceholder {
		t.Fatalf("expected X-Debug-Echo header scrubbed, got %q", got)
	}
	h.scanner.AssertClean(t, canary.NamedData{Source: "500.body", Data: resp2.Body})

	// 3. Hard ban Cookie / Set-Cookie injection in BOTH CredentialInjections and SafeHeaders (422).
	unsealsBefore := h.counted.unsealCalls.Load()
	for _, fieldName := range []string{"Cookie", "cOoKiE", "Set-Cookie", "set-cookie"} {
		_, cerr := h.broker.Execute(ctx, EgressRequest{
			OrgID:        h.orgID,
			ConnectionID: h.connID,
			Method:       http.MethodPost,
			TargetURL:    srv.URL() + "/v1/chat/completions",
			CredentialInjections: []CredentialInjection{
				{SecretSlotName: "api_key", Location: TargetHeader, FieldName: fieldName},
			},
		})
		if !errors.Is(cerr, ErrProhibitedCookieInjection) {
			t.Fatalf("expected ErrProhibitedCookieInjection (422) for FieldName %q, got %v", fieldName, cerr)
		}
		var bErr *Error
		if !errors.As(cerr, &bErr) || bErr.HTTPStatus != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 HTTPStatus for cookie ban, got %+v", cerr)
		}
	}
	_, safeCookieErr := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    srv.URL() + "/v1/models",
		SafeHeaders:  map[string]string{"Cookie": "session=hijack"},
		ConnectionPolicy: &egress.Policy{
			AllowedHosts:   []string{srv.Addr()},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
	})
	if !errors.Is(safeCookieErr, ErrProhibitedCookieInjection) {
		t.Fatalf("expected ErrProhibitedCookieInjection on SafeHeaders Cookie, got %v", safeCookieErr)
	}
	if h.counted.unsealCalls.Load() != unsealsBefore {
		t.Fatalf("expected 0 unseals during prohibited Cookie rejection")
	}
}

func TestBlindBrokerNEG4_CT26_AUTH16_DNSRebindingPortScanAndAudienceMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	secretVal := canary.DefaultCanarySecret()

	// 1. DNS Rebinding flipping lookup -> 169.254.169.254 or [::ffff:127.0.0.1]
	res := &mockHostResolver{
		fn: func(call int64, _ string) ([]netip.Addr, error) {
			if call == 1 {
				return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("::ffff:127.0.0.1")}, nil
		},
	}
	h := newBrokerHarness(t, res)
	audRebind := "https://rebind.attacker.example"
	meta := h.sealCanarySecret(t, "rebind-key", &audRebind, []byte(secretVal))
	h.resolver.put(&ResolvedSlot{
		OrgID:         h.orgID,
		ConnectionID:  h.connID,
		SlotName:      "api_key",
		SecretID:      meta.ID,
		BoundAudience: &audRebind,
		AllowedHosts:  []string{"rebind.attacker.example"},
		RequireTLS:    true,
	})

	for i := 0; i < 2; i++ {
		_, err := h.broker.Execute(ctx, EgressRequest{
			OrgID:        h.orgID,
			ConnectionID: h.connID,
			Method:       http.MethodGet,
			TargetURL:    "https://rebind.attacker.example/v1/models",
			CredentialInjections: []CredentialInjection{
				{SecretSlotName: "api_key", Location: TargetBearer, FieldName: "Authorization"},
			},
		})
		if !errors.Is(err, egress.ErrMetadataBlocked) && !errors.Is(err, egress.ErrPrivateIPBlocked) {
			t.Fatalf("iteration %d: expected SSRF metadata/private IP block, got %v", i, err)
		}
	}

	// 2. Port Scan block (10.20.0.5:6379 denied when 10.20.0.5:8000 is allowed)
	audLAN := "http://10.20.0.5:8000"
	metaLAN := h.sealCanarySecret(t, "lan-key", &audLAN, []byte(secretVal))
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "lan_key",
		SecretID:       metaLAN.ID,
		BoundAudience:  &audLAN,
		AllowedHosts:   []string{"10.20.0.5:8000"},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"10.20.0.0/24"},
	})
	_, portErr := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    "http://10.20.0.5:6379/",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "lan_key", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if !errors.Is(portErr, egress.ErrHostDenied) || !errors.Is(portErr, egress.ErrPortDenied) {
		t.Fatalf("expected port scan 10.20.0.5:6379 blocked, got %v", portErr)
	}

	// 3. Audience mismatch: openai secret bound to https://api.openai.com sent to https://custom-proxy.test
	unsealsBefore := h.counted.unsealCalls.Load()
	audOpenAI := "https://api.openai.com"
	metaOpenAI := h.sealCanarySecret(t, "openai-bound", &audOpenAI, []byte(secretVal))
	h.resolver.put(&ResolvedSlot{
		OrgID:         h.orgID,
		ConnectionID:  h.connID,
		SlotName:      "openai_key",
		SecretID:      metaOpenAI.ID,
		BoundAudience: &audOpenAI,
		AllowedHosts:  []string{"api.openai.com", "custom-proxy.test"},
		RequireTLS:    true,
	})
	_, audErr := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    "https://custom-proxy.test/v1/chat/completions",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "openai_key", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if !errors.Is(audErr, ErrAudienceMismatch) {
		t.Fatalf("expected ErrAudienceMismatch (403 credential_audience_mismatch), got %v", audErr)
	}
	if h.counted.unsealCalls.Load() != unsealsBefore {
		t.Fatalf("expected 0 unseals on slot-level audience mismatch")
	}

	// 4. Defense-in-depth: even if ResolvedSlot omitted BoundAudience, Vault UnsealedSecret enforces its BoundAudience!
	h.resolver.put(&ResolvedSlot{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		SlotName:     "openai_key_nil_slot_aud",
		SecretID:     metaOpenAI.ID,
		AllowedHosts: []string{"custom-proxy.test"},
		RequireTLS:   true,
	})
	_, vaultAudErr := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    "https://custom-proxy.test/v1/chat/completions",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "openai_key_nil_slot_aud", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if !errors.Is(vaultAudErr, ErrAudienceMismatch) {
		t.Fatalf("expected authoritative Vault BoundAudience check to block cross-host replay, got %v", vaultAudErr)
	}
	assertNoCanaryInEvents(t, h.scanner, h.audit.snapshot())
}

func TestBlindBrokerHeaderBasicHMACAndErrorScrubbing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := newBrokerHarness(t, nil)

	var lastHeader http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastHeader = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	srvAddr := strings.TrimPrefix(srv.URL, "http://")

	secretStr := canary.DefaultCanarySecret()
	aud := srv.URL
	meta := h.sealCanarySecret(t, "multi-mode-secret", &aud, []byte(secretStr))
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "multi_slot",
		SecretID:       meta.ID,
		BoundAudience:  &aud,
		AllowedHosts:   []string{srvAddr},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})

	// A. Header injection (x-api-key)
	_, err := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    srv.URL + "/v1/messages",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "multi_slot", Location: TargetHeader, FieldName: "x-api-key"},
		},
	})
	if err != nil {
		t.Fatalf("TargetHeader Execute: %v", err)
	}
	if got := lastHeader.Get("x-api-key"); got != secretStr {
		t.Fatalf("expected x-api-key credential injected cleanly")
	}

	// B. Basic auth injection
	_, err = h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    srv.URL + "/v1/models",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "multi_slot", Location: TargetBasic, FieldName: "Authorization", ValuePrefix: "svc:"},
		},
	})
	if err != nil {
		t.Fatalf("TargetBasic Execute: %v", err)
	}
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("svc:"+secretStr))
	if got := lastHeader.Get("Authorization"); got != wantBasic {
		t.Fatalf("expected Basic header matching Vault secret")
	}

	// C. Host-side HMAC_SHA256 signing across HEX_LOWER / BASE64 / BASE64URL
	payload := []byte("POST\n/v1/chat\n1731561600")
	mac := hmac.New(sha256.New, []byte(secretStr))
	_, _ = mac.Write(payload)
	sum := mac.Sum(nil)

	for _, tc := range []struct {
		enc  SignatureEncoding
		want string
	}{
		{enc: EncodingHexLower, want: "v1=" + hex.EncodeToString(sum)},
		{enc: EncodingBase64, want: "v1=" + base64.StdEncoding.EncodeToString(sum)},
		{enc: EncodingBase64URL, want: "v1=" + base64.RawURLEncoding.EncodeToString(sum)},
	} {
		_, err := h.broker.Execute(ctx, EgressRequest{
			OrgID:        h.orgID,
			ConnectionID: h.connID,
			Method:       http.MethodPost,
			TargetURL:    srv.URL + "/v1/chat",
			CredentialInjections: []CredentialInjection{
				{
					SecretSlotName: "multi_slot",
					Location:       TargetHostCryptoSign,
					FieldName:      "X-Tanpopo-Signature",
					ValuePrefix:    "v1=",
					CryptoSign: &CryptoSignSpec{
						Algorithm:        SigningHMACSHA256,
						CanonicalPayload: payload,
						OutputEncoding:   tc.enc,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("TargetHostCryptoSign (%s) Execute: %v", tc.enc, err)
		}
		if got := lastHeader.Get("X-Tanpopo-Signature"); got != tc.want {
			t.Fatalf("encoding %s: got signature %q, want %q", tc.enc, got, tc.want)
		}
	}

	// D. QueryParam failure error scrubbing (closed server causes *url.Error carrying raw query string -> scrubbed!)
	closedSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closedSrv.URL
	closedAddr := strings.TrimPrefix(closedURL, "http://")
	closedSrv.Close()

	metaNoAud := h.sealCanarySecret(t, "qp-secret", nil, []byte(secretStr))
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "closed_qp",
		SecretID:       metaNoAud.ID,
		AllowedHosts:   []string{closedAddr},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})
	_, qpErr := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    closedURL + "/v1/models",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "closed_qp", Location: TargetQueryParam, FieldName: "key"},
		},
	})
	if qpErr == nil {
		t.Fatalf("expected transport dial error against closed server")
	}
	h.scanner.AssertClean(t, canary.NamedData{Source: "query_param_err", Data: []byte(qpErr.Error())})

	// E. Unsupported AWS_SIGV4 returns 422 in v0.1 cleanly (out-of-scope -> TP-IMP-0035)
	_, sigV4Err := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    srv.URL + "/invoke",
		CredentialInjections: []CredentialInjection{
			{
				SecretSlotName: "multi_slot",
				Location:       TargetHostCryptoSign,
				FieldName:      "Authorization",
				CryptoSign: &CryptoSignSpec{
					Algorithm: SigningAWSSigV4,
				},
			},
		},
	})
	if !errors.Is(sigV4Err, ErrUnsupportedSigningAlgorithm) {
		t.Fatalf("expected ErrUnsupportedSigningAlgorithm for AWS_SIGV4 in v0.1, got %v", sigV4Err)
	}

	// F. Zero-value UnsealCapability fails closed inside Vault.
	zeroCapBroker, err := NewBlindBroker(Config{
		Resolver:  h.resolver,
		Vault:     h.vs,
		UnsealCap: vault.UnsealCapability{},
		Audit:     h.audit,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker(zeroCap): %v", err)
	}
	_, zErr := zeroCapBroker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    srv.URL + "/v1/models",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "multi_slot", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if !errors.Is(zErr, vault.ErrMissingUnsealCapability) {
		t.Fatalf("expected vault.ErrMissingUnsealCapability, got %v", zErr)
	}
}

func TestBlindBrokerConcurrentExecution_RaceSafe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := newBrokerHarness(t, nil)
	srv := fakellm.NewServer(t)

	aud := srv.URL()
	meta := h.sealCanarySecret(t, "concurrent-canary", &aud, []byte(canary.DefaultCanarySecret()))
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "api_key",
		SecretID:       meta.ID,
		BoundAudience:  &aud,
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := h.broker.Execute(ctx, EgressRequest{
				OrgID:        h.orgID,
				ConnectionID: h.connID,
				Method:       http.MethodPost,
				TargetURL:    srv.URL() + "/v1/chat/completions",
				BodyBytes:    []byte(`{"model":"gpt-4o","stream":false}`),
				CredentialInjections: []CredentialInjection{
					{SecretSlotName: "api_key", Location: TargetBearer, FieldName: "Authorization"},
				},
				RequestID: fmt.Sprintf("req_%d", idx),
			})
			if err != nil {
				errs <- err
				return
			}
			if len(h.scanner.ScanBytes("resp", resp.Body)) > 0 {
				errs <- fmt.Errorf("canary leak in concurrent response")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent broker error: %v", err)
	}
	assertNoCanaryInEvents(t, h.scanner, h.audit.snapshot())
}

type failingAuditSink struct{}

func (failingAuditSink) Append(context.Context, port.AuditEvent) error {
	return errors.New("audit write failed")
}

func TestBlindBrokerValidationFailClosedAndMultiSlotIntersection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := newBrokerHarness(t, nil)
	srv := fakellm.NewServer(t)

	// 1. NewBlindBroker nil dependency fail-closed checks.
	if _, err := NewBlindBroker(Config{Vault: h.vs, Audit: h.audit}); !errors.Is(err, ErrInvalidBrokerConfig) {
		t.Fatalf("expected ErrInvalidBrokerConfig for nil Resolver, got %v", err)
	}
	if _, err := NewBlindBroker(Config{Resolver: h.resolver, Audit: h.audit}); !errors.Is(err, ErrInvalidBrokerConfig) {
		t.Fatalf("expected ErrInvalidBrokerConfig for nil Vault, got %v", err)
	}
	if _, err := NewBlindBroker(Config{Resolver: h.resolver, Vault: h.vs}); !errors.Is(err, ErrInvalidBrokerConfig) {
		t.Fatalf("expected ErrInvalidBrokerConfig for nil AuditSink, got %v", err)
	}

	// 2. Unauthenticated LAN call via ConnectionPolicyResolver vs missing policy fail-closed.
	h.resolver.conns[h.orgID+"|"+h.connID] = &egress.Policy{
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	}
	resp, err := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    srv.URL() + "/v1/models",
	})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected ConnectionPolicyResolver LAN GET to succeed, got resp=%v err=%v", resp, err)
	}
	_, noPolErr := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: "con_unregistered",
		Method:       http.MethodGet,
		TargetURL:    srv.URL() + "/v1/models",
	})
	if !errors.Is(noPolErr, port.ErrNotFound) {
		t.Fatalf("expected port.ErrNotFound for missing connection policy, got %v", noPolErr)
	}

	// 3. Cross-tenant & cross-connection resolved slot mismatches rejected before unseal.
	meta := h.sealCanarySecret(t, "tenant-scoped", nil, []byte(canary.DefaultCanarySecret()))
	h.resolver.put(&ResolvedSlot{
		OrgID:          "org_other",
		ConnectionID:   h.connID,
		SlotName:       "wrong_org",
		SecretID:       meta.ID,
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})
	// Direct resolver returning mismatched slot.OrgID.
	h.resolver.mu.Lock()
	h.resolver.slots[slotKey{orgID: h.orgID, conn: h.connID, slot: "wrong_org"}] = &ResolvedSlot{
		OrgID:          "org_other",
		ConnectionID:   h.connID,
		SlotName:       "wrong_org",
		SecretID:       meta.ID,
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	}
	h.resolver.slots[slotKey{orgID: h.orgID, conn: h.connID, slot: "wrong_conn"}] = &ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   "con_other",
		SlotName:       "wrong_conn",
		SecretID:       meta.ID,
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	}
	h.resolver.mu.Unlock()

	for _, slotName := range []string{"wrong_org", "wrong_conn"} {
		_, serr := h.broker.Execute(ctx, EgressRequest{
			OrgID:        h.orgID,
			ConnectionID: h.connID,
			Method:       http.MethodGet,
			TargetURL:    srv.URL() + "/v1/models",
			CredentialInjections: []CredentialInjection{
				{SecretSlotName: slotName, Location: TargetBearer, FieldName: "Authorization"},
			},
		})
		if !errors.Is(serr, egress.ErrHostDenied) {
			t.Fatalf("expected egress.ErrHostDenied for %s, got %v", slotName, serr)
		}
	}

	// 4. Fail-closed audit sink during credential unseal aborts before network egress.
	reqsBefore := len(srv.Requests())
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "good_slot",
		SecretID:       meta.ID,
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})
	failAuditBroker, err := NewBlindBroker(Config{
		Resolver:  h.resolver,
		Vault:     h.vs,
		UnsealCap: vault.MintUnsealCapabilityForBroker(),
		Audit:     failingAuditSink{},
	})
	if err != nil {
		t.Fatalf("NewBlindBroker(failingAuditSink): %v", err)
	}
	_, audFailErr := failAuditBroker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    srv.URL() + "/v1/models",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "good_slot", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if audFailErr == nil {
		t.Fatalf("expected Execute to fail closed when AuditSink fails during credential unseal")
	}
	if len(srv.Requests()) != reqsBefore {
		t.Fatalf("expected 0 outbound HTTP requests when unseal auditing fails")
	}

	// 5. MaxResponseBodyBytes overflow protection & banned safe headers.
	smallBroker, err := NewBlindBroker(Config{
		Resolver:             h.resolver,
		Vault:                h.vs,
		UnsealCap:            vault.MintUnsealCapabilityForBroker(),
		Audit:                h.audit,
		MaxResponseBodyBytes: 8,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker(small): %v", err)
	}
	_, sizeErr := smallBroker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    srv.URL() + "/v1/models",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "good_slot", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if !errors.Is(sizeErr, ErrResponseTooLarge) {
		t.Fatalf("expected ErrResponseTooLarge when body > MaxResponseBodyBytes, got %v", sizeErr)
	}

	_, hostHeaderErr := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    srv.URL() + "/v1/models",
		SafeHeaders:  map[string]string{"Host": "evil.attacker.invalid"},
	})
	if !errors.Is(hostHeaderErr, ErrInvalidEgressRequest) {
		t.Fatalf("expected host header override rejected in SafeHeaders, got %v", hostHeaderErr)
	}
}

func TestBlindBrokerSecurityHardeningRegressions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := newBrokerHarness(t, nil)
	srv := fakellm.NewServer(t)

	// 1. AudienceMatches blocks dot-segment traversal (.., ., %2e%2e, %2f, backslash).
	ptr := func(s string) *string { return &s }
	audPrefix := ptr("https://api.example.com/v1/tenant-a")
	for _, tricky := range []string{
		"https://api.example.com/v1/tenant-a/../tenant-b/chat",
		"https://api.example.com/v1/tenant-a/%2e%2e/tenant-b/chat",
		"https://api.example.com/v1/tenant-a/./../tenant-b/chat",
		"https://api.example.com/v1/tenant-a/%2e/sub",
		"https://api.example.com/v1/tenant-a/sub%2f..%2ftenant-b",
	} {
		u, err := url.Parse(tricky)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", tricky, err)
		}
		if ok, _ := AudienceMatches(u, audPrefix); ok {
			t.Fatalf("AudienceMatches MUST reject traversal URL %q against prefix %q", tricky, *audPrefix)
		}
	}

	// 2. Multi-slot CredentialInjections & intersectPolicies real Execute + narrowing cases.
	secret1 := canary.DefaultCanarySecret()
	secret2 := canary.DefaultForbiddenCIKey()
	h.scanner.AddSecret(secret2)
	meta1 := h.sealCanarySecret(t, "multi-1", nil, []byte(secret1))
	meta2 := h.sealCanarySecret(t, "multi-2", nil, []byte(secret2))

	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "slot_bearer",
		SecretID:       meta1.ID,
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.0/8"},
	})
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   h.connID,
		SlotName:       "slot_sig",
		SecretID:       meta2.ID,
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})
	resp, err := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    srv.URL() + "/v1/chat/completions",
		BodyBytes:    []byte(`{"model":"gpt-4o"}`),
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "slot_bearer", Location: TargetBearer, FieldName: "Authorization"},
			{
				SecretSlotName: "slot_sig",
				Location:       TargetHostCryptoSign,
				FieldName:      "X-Second-Sig",
				CryptoSign: &CryptoSignSpec{
					Algorithm:        SigningHMACSHA256,
					CanonicalPayload: []byte("body"),
					OutputEncoding:   EncodingHexLower,
				},
			},
		},
	})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("multi-slot narrowing Execute failed: resp=%v err=%v", resp, err)
	}
	h.scanner.AssertClean(t, canary.NamedData{Source: "multi.resp", Data: resp.Body})

	// Disjoint AllowedPorts intersection must fail closed (never widen to nil -> 80/443).
	if _, perr := intersectPolicies([]egress.Policy{
		{AllowedHosts: []string{"api.example.com"}, AllowedPorts: []int{443}},
		{AllowedHosts: []string{"api.example.com"}, AllowedPorts: []int{8443}},
	}); !errors.Is(perr, egress.ErrPortDenied) {
		t.Fatalf("expected ErrPortDenied for disjoint AllowedPorts intersection, got %v", perr)
	}
	// One slot omitting AllowedPorts ({80,443}) + another specifying [443, 8443] must narrow to [443].
	narrowed, perr := intersectPolicies([]egress.Policy{
		{AllowedHosts: []string{"api.example.com"}, RequireTLS: true},
		{AllowedHosts: []string{"api.example.com"}, AllowedPorts: []int{443, 8443}},
	})
	if perr != nil || len(narrowed.AllowedPorts) != 1 || narrowed.AllowedPorts[0] != 443 {
		t.Fatalf("expected narrowed AllowedPorts=[443], got %+v err=%v", narrowed, perr)
	}
	// Disjoint AllowedCIDRs must fail closed.
	if _, cerr := intersectPolicies([]egress.Policy{
		{AllowedHosts: []string{"127.0.0.1:8080"}, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}},
		{AllowedHosts: []string{"127.0.0.1:8080"}, AllowPrivateIP: true, AllowedCIDRs: []string{"10.20.0.0/24"}},
	}); !errors.Is(cerr, egress.ErrPrivateIPBlocked) {
		t.Fatalf("expected ErrPrivateIPBlocked for disjoint AllowedCIDRs, got %v", cerr)
	}

	// 3. scrubbedError.Unwrap() must NEVER leak raw secret anywhere down the error chain.
	sc := &secretScrubber{}
	sc.addSecret([]byte("s+k/test?secret=123"))
	defer sc.zeroize()
	rawChain := fmt.Errorf("outer: %w", &url.Error{
		Op:  "Get",
		URL: "https://api.example.com/v1?k=s%2bk%2ftest%3fsecret%3d123",
		Err: fmt.Errorf("wrapped with s+k/test?secret=123: %w", egress.ErrRedirectBlocked),
	})
	sanitized := sc.sanitizeError(rawChain)
	if !errors.Is(sanitized, egress.ErrRedirectBlocked) {
		t.Fatalf("expected sanitized error to preserve sentinel match via Unwrap")
	}
	for walk := sanitized; walk != nil; {
		if strings.Contains(walk.Error(), "s+k/test?secret=123") || strings.Contains(walk.Error(), "s%2bk") {
			t.Fatalf("unredacted secret found in sanitized error chain node: %q", walk.Error())
		}
		if u, ok := walk.(interface{ Unwrap() error }); ok {
			walk = u.Unwrap()
		} else {
			break
		}
	}

	// 4. TargetHostCryptoSign collision with SafeHeaders, duplicate QueryParam, and invalid/hop-by-hop FieldName.
	for _, tc := range []struct {
		name string
		req  EgressRequest
		want error
	}{
		{
			name: "crypto_sign_safe_header_collision",
			req: EgressRequest{
				OrgID:        h.orgID,
				ConnectionID: h.connID,
				Method:       http.MethodPost,
				TargetURL:    srv.URL() + "/v1/chat",
				SafeHeaders:  map[string]string{"X-Sig": "forged"},
				CredentialInjections: []CredentialInjection{
					{
						SecretSlotName: "slot_sig",
						Location:       TargetHostCryptoSign,
						FieldName:      "x-sig",
						CryptoSign:     &CryptoSignSpec{Algorithm: SigningHMACSHA256},
					},
				},
			},
			want: ErrInvalidEgressRequest,
		},
		{
			name: "duplicate_query_param",
			req: EgressRequest{
				OrgID:        h.orgID,
				ConnectionID: h.connID,
				Method:       http.MethodGet,
				TargetURL:    srv.URL() + "/v1/models",
				CredentialInjections: []CredentialInjection{
					{SecretSlotName: "slot_bearer", Location: TargetQueryParam, FieldName: "api_key"},
					{SecretSlotName: "slot_sig", Location: TargetQueryParam, FieldName: "api_key"},
				},
			},
			want: ErrInvalidEgressRequest,
		},
		{
			name: "hop_by_hop_injection_field",
			req: EgressRequest{
				OrgID:        h.orgID,
				ConnectionID: h.connID,
				Method:       http.MethodGet,
				TargetURL:    srv.URL() + "/v1/models",
				CredentialInjections: []CredentialInjection{
					{SecretSlotName: "slot_bearer", Location: TargetHeader, FieldName: "Transfer-Encoding"},
				},
			},
			want: ErrInvalidEgressRequest,
		},
		{
			name: "invalid_header_tchar_space",
			req: EgressRequest{
				OrgID:        h.orgID,
				ConnectionID: h.connID,
				Method:       http.MethodGet,
				TargetURL:    srv.URL() + "/v1/models",
				CredentialInjections: []CredentialInjection{
					{SecretSlotName: "slot_bearer", Location: TargetHeader, FieldName: "X Bad Header"},
				},
			},
			want: ErrInvalidEgressRequest,
		},
	} {
		if _, rerr := h.broker.Execute(ctx, tc.req); !errors.Is(rerr, tc.want) {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, rerr)
		}
	}
}

