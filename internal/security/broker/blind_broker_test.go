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
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestBlindBrokerEnforcesResolvedSlotBindingMode(t *testing.T) {
	t.Parallel()

	dns := &mockHostResolver{}
	h := newBrokerHarness(t, dns)
	h.resolver.put(&ResolvedSlot{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		SlotName:     "signing_key",
		InjectMode:   InjectModeHostCryptoSign,
		AllowedHosts: []string{"api.example.com"},
	})
	_, err := h.broker.Execute(context.Background(), EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    "https://api.example.com",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "signing_key", Location: TargetBearer},
		},
	})
	var denied *Error
	if !errors.As(err, &denied) || denied.Code != "credential_binding_mode_denied" {
		t.Fatalf("expected binding mode denial, got %v", err)
	}
	if dns.calls.Load() != 0 || h.counted.unsealCalls.Load() != 0 {
		t.Fatal("binding mode denial must precede DNS and vault access")
	}
}

func TestBlindBrokerOAuthSlotMissingResolverAudienceFailsClosed(t *testing.T) {
	t.Parallel()
	h := newBrokerHarness(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	audience := srv.URL
	meta, err := h.vs.SealSecret(context.Background(), vault.SealSecretParams{OrgID: h.orgID, Name: "OAuth token set", SecretKind: vault.SecretKindOAuth2TokenSet, BoundAudience: &audience, Plaintext: []byte("sealed-token-set")})
	if err != nil {
		t.Fatal(err)
	}
	h.resolver.put(&ResolvedSlot{OrgID: h.orgID, ConnectionID: h.connID, SlotName: "oauth_access_token", SecretID: meta.ID, InjectMode: InjectModeHostBlind, BoundAudience: nil, AllowedHosts: []string{srv.Listener.Addr().String()}, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}})
	_, err = h.broker.Execute(context.Background(), EgressRequest{OrgID: h.orgID, ConnectionID: h.connID, Method: http.MethodGet, TargetURL: srv.URL + "/resource", CredentialInjections: []CredentialInjection{{SecretSlotName: "oauth_access_token", Location: TargetBearer, FieldName: "Authorization", ValuePrefix: "Bearer "}}, RequestID: "oauth-missing-audience"})
	var brokerErr *Error
	if !errors.As(err, &brokerErr) || !errors.Is(err, ErrAudienceMismatch) {
		t.Fatalf("Execute error=%v, want fail-closed audience mismatch", err)
	}
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

type slotOnlyResolver struct{}

func (slotOnlyResolver) ResolveSlot(_ context.Context, _, _, _ string) (*ResolvedSlot, error) {
	return nil, port.ErrNotFound
}

type nilPolicyResolver struct{}

func (nilPolicyResolver) ResolveSlot(_ context.Context, _, _, _ string) (*ResolvedSlot, error) {
	return nil, port.ErrNotFound
}

func (nilPolicyResolver) ResolveConnectionPolicy(_ context.Context, _, _ string) (*egress.Policy, error) {
	return nil, nil
}

func TestBlindBrokerZeroSlotTrustBoundaryAndNoSelfDeclaredPolicyBypass(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// 1. Provenance invariant: EgressRequest MUST carry zero *egress.Policy / egress.Policy fields
	// so adapter-produced request envelopes can never self-declare egress permissions.
	reqType := reflect.TypeOf(EgressRequest{})
	policyType := reflect.TypeOf(egress.Policy{})
	policyPtrType := reflect.TypeOf(&egress.Policy{})
	for i := 0; i < reqType.NumField(); i++ {
		f := reqType.Field(i)
		if f.Type == policyType || f.Type == policyPtrType {
			t.Fatalf("security boundary violation: EgressRequest exposes policy field %q (%v)", f.Name, f.Type)
		}
	}

	dnsRes := &mockHostResolver{
		fn: func(_ int64, _ string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
	}
	h := newBrokerHarness(t, dnsRes)
	srv := fakellm.NewServer(t)

	// 2. Zero CredentialInjections WITHOUT any ConnectionPolicyResolver implementation -> fail closed pre-DNS.
	noPolicyBroker, err := NewBlindBroker(Config{
		Resolver:     slotOnlyResolver{},
		Vault:        h.vs,
		UnsealCap:    vault.MintUnsealCapabilityForBroker(),
		Audit:        h.audit,
		HostResolver: dnsRes,
		ControlHook:  netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker(slotOnly): %v", err)
	}
	forgedWider := HostEgressConstraint{
		NarrowPolicy: &egress.Policy{
			AllowedHosts:   []string{"evil.attacker.invalid", srv.Addr()},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
	}
	_, noResErr := noPolicyBroker.ExecuteWithHostConstraint(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodPost,
		TargetURL:    "https://evil.attacker.invalid/exfil",
	}, forgedWider)
	if !errors.Is(noResErr, egress.ErrHostDenied) {
		t.Fatalf("expected ErrHostDenied when no ConnectionPolicyResolver exists, got %v", noResErr)
	}
	if dnsRes.calls.Load() != 0 {
		t.Fatalf("expected 0 DNS lookups when ConnectionPolicyResolver is absent, got %d", dnsRes.calls.Load())
	}

	// 3. Zero CredentialInjections when ConnectionPolicyResolver returns nil policy -> fail closed pre-DNS.
	nilPolBroker, err := NewBlindBroker(Config{
		Resolver:     nilPolicyResolver{},
		Vault:        h.vs,
		UnsealCap:    vault.MintUnsealCapabilityForBroker(),
		Audit:        h.audit,
		HostResolver: dnsRes,
		ControlHook:  netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker(nilPolicyResolver): %v", err)
	}
	_, nilPolErr := nilPolBroker.ExecuteWithHostConstraint(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    srv.URL() + "/v1/models",
	}, forgedWider)
	if !errors.Is(nilPolErr, egress.ErrHostDenied) {
		t.Fatalf("expected ErrHostDenied when resolved ConnectionPolicy is nil, got %v", nilPolErr)
	}
	if len(srv.Requests()) != 0 || dnsRes.calls.Load() != 0 {
		t.Fatalf("expected 0 upstream hits and 0 DNS lookups on nil ConnectionPolicy")
	}

	// 4. Authoritative ConnectionPolicy vs forged/wider HostEgressConstraint:
	// Authoritative Connection only permits TLS https://approved.example.com (port 443).
	// Forged constraint tries to add evil.attacker.invalid, AllowPrivateIP=true, 10.0.0.0/8, port 6379, RequireTLS=false.
	h.resolver.conns[h.orgID+"|"+h.connID] = &egress.Policy{
		AllowedHosts: []string{"approved.example.com"},
		RequireTLS:   true,
	}
	dnsBefore := dnsRes.calls.Load()
	for _, attempt := range []struct {
		name      string
		targetURL string
		wantErr   error
	}{
		{name: "extra_evil_domain", targetURL: "https://evil.attacker.invalid/steal", wantErr: egress.ErrHostDenied},
		{name: "private_lan_escape", targetURL: "http://10.20.0.5:6379/", wantErr: egress.ErrTLSRequired},
		{name: "loopback_fake_server", targetURL: srv.URL() + "/v1/models", wantErr: egress.ErrTLSRequired},
		{name: "http_downgrade_on_approved_host", targetURL: "http://approved.example.com/v1/models", wantErr: egress.ErrTLSRequired},
		{name: "unapproved_port_on_approved_host", targetURL: "https://approved.example.com:6379/v1", wantErr: egress.ErrPortDenied},
	} {
		t.Run(attempt.name, func(t *testing.T) {
			_, perr := h.broker.ExecuteWithHostConstraint(ctx, EgressRequest{
				OrgID:        h.orgID,
				ConnectionID: h.connID,
				Method:       http.MethodGet,
				TargetURL:    attempt.targetURL,
			}, HostEgressConstraint{
				NarrowPolicy: &egress.Policy{
					AllowedHosts:   []string{"evil.attacker.invalid", "approved.example.com", "approved.example.com:6379", srv.Addr(), "10.20.0.5:6379"},
					AllowPrivateIP: true,
					AllowedCIDRs:   []string{"10.0.0.0/8", "127.0.0.1/32"},
					AllowedPorts:   []int{80, 443, 6379},
					RequireTLS:     false,
				},
			})
			if !errors.Is(perr, attempt.wantErr) {
				t.Fatalf("%s: expected %v, got %v", attempt.name, attempt.wantErr, perr)
			}
		})
	}
	if dnsRes.calls.Load() != dnsBefore {
		t.Fatalf("expected 0 DNS lookups across all forged-constraint rejections, got %d", dnsRes.calls.Load()-dnsBefore)
	}

	// 5. Redirect widening defense: authoritative Connection allows only approvedSrv, while forged constraint
	// tries to also allow unapprovedRedirectTarget -> 302 redirect MUST still fail with ErrRedirectBlocked.
	var redirectHits atomic.Int64
	redirectTargetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTargetSrv.Close()

	redirectingApprovedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTargetSrv.URL+"/exfil", http.StatusFound)
	}))
	defer redirectingApprovedSrv.Close()

	approvedAddr := strings.TrimPrefix(redirectingApprovedSrv.URL, "http://")
	targetAddr := strings.TrimPrefix(redirectTargetSrv.URL, "http://")
	h.resolver.conns[h.orgID+"|"+h.connID] = &egress.Policy{
		AllowedHosts:   []string{approvedAddr},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	}
	_, redirErr := h.broker.ExecuteWithHostConstraint(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: h.connID,
		Method:       http.MethodGet,
		TargetURL:    redirectingApprovedSrv.URL + "/probe",
	}, HostEgressConstraint{
		NarrowPolicy: &egress.Policy{
			AllowedHosts:   []string{approvedAddr, targetAddr},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
	})
	if !errors.Is(redirErr, egress.ErrRedirectBlocked) {
		t.Fatalf("expected ErrRedirectBlocked when redirect leaves authoritative host despite forged constraint, got %v", redirErr)
	}
	if redirectHits.Load() != 0 {
		t.Fatalf("expected 0 hits on unapproved redirect target, got %d", redirectHits.Load())
	}

	// 6. Valid self-hosted LAN auth_mode=NONE request (zero CredentialInjections) backed by authoritative ConnectionPolicyResolver.
	lanConnID := idgen.MustNew(idgen.PrefixConnection)
	h.resolver.conns[h.orgID+"|"+lanConnID] = &egress.Policy{
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	}
	lanResp, err := h.broker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: lanConnID,
		Method:       http.MethodPost,
		TargetURL:    srv.URL() + "/v1/chat/completions",
		BodyBytes:    []byte(`{"model":"llama3","stream":false}`),
	})
	if err != nil || lanResp.StatusCode != http.StatusOK {
		t.Fatalf("expected valid auth_mode=NONE LAN request via ConnectionPolicyResolver to succeed, got resp=%v err=%v", lanResp, err)
	}

	// 7. Explicit Config.PolicyResolver also bounds credentialed calls (slot cannot widen past Connection policy).
	explicitBroker, err := NewBlindBroker(Config{
		Resolver:       h.resolver,
		PolicyResolver: h.resolver,
		Vault:          h.vs,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          h.audit,
		HostResolver:   dnsRes,
		ControlHook:    netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker(explicit PolicyResolver): %v", err)
	}
	meta := h.sealCanarySecret(t, "scoped-slot-sec", nil, []byte(canary.DefaultCanarySecret()))
	broadSlotConn := idgen.MustNew(idgen.PrefixConnection)
	// Connection policy ONLY allows srv.Addr(); slot erroneously includes evil.attacker.invalid too.
	h.resolver.conns[h.orgID+"|"+broadSlotConn] = &egress.Policy{
		AllowedHosts:   []string{srv.Addr()},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	}
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   broadSlotConn,
		SlotName:       "api_key",
		SecretID:       meta.ID,
		AllowedHosts:   []string{srv.Addr(), "evil.attacker.invalid"},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})
	_, connFenceErr := explicitBroker.Execute(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: broadSlotConn,
		Method:       http.MethodPost,
		TargetURL:    "https://evil.attacker.invalid/steal",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "api_key", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if !errors.Is(connFenceErr, egress.ErrHostDenied) {
		t.Fatalf("expected explicit Config.PolicyResolver to fence slot away from unapproved domain, got %v", connFenceErr)
	}
	assertNoCanaryInEvents(t, h.scanner, h.audit.snapshot())
}

type callbackVaultUnsealer struct {
	inner    VaultUnsealer
	onUnseal func()
}

func (c *callbackVaultUnsealer) UnsealWithCapability(ctx context.Context, cap vault.UnsealCapability, orgID, secretID string) (*vault.UnsealedSecret, error) {
	sec, err := c.inner.UnsealWithCapability(ctx, cap, orgID, secretID)
	if err == nil && c.onUnseal != nil {
		c.onUnseal()
	}
	return sec, err
}

type callbackHostResolver struct {
	onLookup func()
	addr     netip.Addr
}

func (c *callbackHostResolver) LookupNetIP(_ context.Context, _, _ string) ([]netip.Addr, error) {
	if c.onLookup != nil {
		c.onLookup()
	}
	return []netip.Addr{c.addr}, nil
}

func TestBlindBrokerRevokeQuarantineMidFlightTOCTOU(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Revoke/quarantine after initial ResolveSlot & Vault unseal, BEFORE HTTP dispatch.
	h1 := newBrokerHarness(t, nil)
	var upstreamHits1 atomic.Int32
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits1.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv1.Close()
	u1, _ := url.Parse(srv1.URL)

	meta1 := h1.sealCanarySecret(t, "midflight-key", nil, []byte(canary.DefaultCanarySecret()))
	conn1 := idgen.MustNew(idgen.PrefixConnection)
	h1.resolver.conns[h1.orgID+"|"+conn1] = &egress.Policy{
		AllowedHosts:   []string{u1.Host},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	}
	h1.resolver.put(&ResolvedSlot{
		OrgID:          h1.orgID,
		ConnectionID:   conn1,
		SlotName:       "api_key",
		SecretID:       meta1.ID,
		AllowedHosts:   []string{u1.Host},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})

	brk1, err := NewBlindBroker(Config{
		Resolver:       h1.resolver,
		PolicyResolver: h1.resolver,
		Vault: &callbackVaultUnsealer{
			inner: h1.vs,
			onUnseal: func() {
				h1.resolver.mu.Lock()
				delete(h1.resolver.slots, slotKey{orgID: h1.orgID, conn: conn1, slot: "api_key"})
				delete(h1.resolver.conns, h1.orgID+"|"+conn1)
				h1.resolver.mu.Unlock()
			},
		},
		UnsealCap: vault.MintUnsealCapabilityForBroker(),
		Audit:     h1.audit,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err1 := brk1.Execute(ctx, EgressRequest{
		OrgID:        h1.orgID,
		ConnectionID: conn1,
		Method:       http.MethodPost,
		TargetURL:    srv1.URL + "/v1/chat/completions",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "api_key", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if !errors.Is(err1, port.ErrNotFound) {
		t.Fatalf("expected pre-dispatch TOCTOU block after post-unseal revoke, got %v", err1)
	}
	if upstreamHits1.Load() != 0 {
		t.Fatalf("expected 0 upstream HTTP requests after mid-flight revoke, got %d", upstreamHits1.Load())
	}

	// 2. Zero-slot revocation AFTER pre-dispatch check (during DNS LookupNetIP, before connect(2)).
	h2 := newBrokerHarness(t, nil)
	var upstreamHits2 atomic.Int32
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits2.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()
	u2, _ := url.Parse(srv2.URL)
	targetHost := "api.revoked-dial.internal:" + u2.Port()

	conn2 := idgen.MustNew(idgen.PrefixConnection)
	h2.resolver.conns[h2.orgID+"|"+conn2] = &egress.Policy{
		AllowedHosts:   []string{targetHost},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	}
	brk2, err := NewBlindBroker(Config{
		Resolver:       h2.resolver,
		PolicyResolver: h2.resolver,
		Vault:          h2.vs,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          h2.audit,
		HostResolver: &callbackHostResolver{
			addr: netip.MustParseAddr("127.0.0.1"),
			onLookup: func() {
				h2.resolver.mu.Lock()
				delete(h2.resolver.conns, h2.orgID+"|"+conn2)
				h2.resolver.mu.Unlock()
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err2 := brk2.Execute(ctx, EgressRequest{
		OrgID:        h2.orgID,
		ConnectionID: conn2,
		Method:       http.MethodGet,
		TargetURL:    "http://" + targetHost + "/v1/models",
	})
	if err2 == nil || !errors.Is(err2, port.ErrNotFound) {
		t.Fatalf("expected ControlHook connect-time block after mid-dial revoke, got %v", err2)
	}
	if upstreamHits2.Load() != 0 {
		t.Fatalf("expected 0 upstream HTTP requests after pre-connect revoke, got %d", upstreamHits2.Load())
	}
	assertNoCanaryInEvents(t, h1.scanner, h1.audit.snapshot())
}

func TestBlindBrokerConnectTimeSocketIPAndPortPolicyTightening(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// A. Credentialed slot: CIDR tightened mid-dial (between pre-dispatch & ControlHook connect(2)).
	hA := newBrokerHarness(t, nil)
	var hitsA atomic.Int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvA.Close()
	uA, _ := url.Parse(srvA.URL)
	portA, _ := strconv.Atoi(uA.Port())
	hostA := "api.cidr-slot.internal:" + uA.Port()

	metaA := hA.sealCanarySecret(t, "cidr-slot-key", nil, []byte(canary.DefaultCanarySecret()))
	connA := idgen.MustNew(idgen.PrefixConnection)
	hA.resolver.conns[hA.orgID+"|"+connA] = &egress.Policy{
		AllowedHosts:   []string{hostA},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
		AllowedPorts:   []int{portA},
	}
	hA.resolver.put(&ResolvedSlot{
		OrgID:          hA.orgID,
		ConnectionID:   connA,
		SlotName:       "api_key",
		SecretID:       metaA.ID,
		AllowedHosts:   []string{hostA},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
		AllowedPorts:   []int{portA},
	})

	brkA, err := NewBlindBroker(Config{
		Resolver:       hA.resolver,
		PolicyResolver: hA.resolver,
		Vault:          hA.vs,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          hA.audit,
		HostResolver: &callbackHostResolver{
			addr: netip.MustParseAddr("127.0.0.1"),
			onLookup: func() {
				hA.resolver.mu.Lock()
				hA.resolver.conns[hA.orgID+"|"+connA].AllowedCIDRs = []string{"10.0.0.0/8"}
				hA.resolver.slots[slotKey{orgID: hA.orgID, conn: connA, slot: "api_key"}].AllowedCIDRs = []string{"10.0.0.0/8"}
				hA.resolver.mu.Unlock()
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, errA := brkA.Execute(ctx, EgressRequest{
		OrgID:        hA.orgID,
		ConnectionID: connA,
		Method:       http.MethodPost,
		TargetURL:    "http://" + hostA + "/v1/chat/completions",
		CredentialInjections: []CredentialInjection{
			{SecretSlotName: "api_key", Location: TargetBearer, FieldName: "Authorization"},
		},
	})
	if errA == nil || !errors.Is(errA, egress.ErrPrivateIPBlocked) {
		t.Fatalf("expected connect-time ErrPrivateIPBlocked after mid-dial CIDR tightening, got %v", errA)
	}
	if hitsA.Load() != 0 {
		t.Fatalf("expected 0 upstream hits after connect-time CIDR block, got %d", hitsA.Load())
	}
	assertNoCanaryInEvents(t, hA.scanner, hA.audit.snapshot())

	// B. Zero-slot egress: connect-time CIDR tightened mid-dial -> blocked before connect(2).
	hB := newBrokerHarness(t, nil)
	var hitsB atomic.Int32
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvB.Close()
	uB, _ := url.Parse(srvB.URL)
	portB, _ := strconv.Atoi(uB.Port())
	hostB := "api.cidr-zero.internal:" + uB.Port()

	connB := idgen.MustNew(idgen.PrefixConnection)
	hB.resolver.conns[hB.orgID+"|"+connB] = &egress.Policy{
		AllowedHosts:   []string{hostB},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
		AllowedPorts:   []int{portB},
	}
	brkB, err := NewBlindBroker(Config{
		Resolver:       hB.resolver,
		PolicyResolver: hB.resolver,
		Vault:          hB.vs,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          hB.audit,
		HostResolver: &callbackHostResolver{
			addr: netip.MustParseAddr("127.0.0.1"),
			onLookup: func() {
				hB.resolver.mu.Lock()
				hB.resolver.conns[hB.orgID+"|"+connB].AllowedCIDRs = []string{"192.168.50.0/24"}
				hB.resolver.mu.Unlock()
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, errB := brkB.Execute(ctx, EgressRequest{
		OrgID:        hB.orgID,
		ConnectionID: connB,
		Method:       http.MethodGet,
		TargetURL:    "http://" + hostB + "/v1/models",
	}); errB == nil || !errors.Is(errB, egress.ErrPrivateIPBlocked) {
		t.Fatalf("expected zero-slot connect-time ErrPrivateIPBlocked after CIDR tightening, got %v", errB)
	}
	if hitsB.Load() != 0 {
		t.Fatalf("expected 0 upstream hits for zero-slot CIDR tightening, got %d", hitsB.Load())
	}

	// C. Connect-time socket PORT tightening mid-dial -> blocked before connect(2).
	hC := newBrokerHarness(t, nil)
	var hitsC atomic.Int32
	srvC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsC.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvC.Close()
	uC, _ := url.Parse(srvC.URL)
	portC, _ := strconv.Atoi(uC.Port())
	otherPort := portC + 1
	if otherPort > 65535 {
		otherPort = portC - 1
	}
	hostC := "api.port-tighten.internal"

	connC := idgen.MustNew(idgen.PrefixConnection)
	hC.resolver.conns[hC.orgID+"|"+connC] = &egress.Policy{
		AllowedHosts:   []string{hostC},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
		AllowedPorts:   []int{portC, otherPort},
	}
	brkC, err := NewBlindBroker(Config{
		Resolver:       hC.resolver,
		PolicyResolver: hC.resolver,
		Vault:          hC.vs,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          hC.audit,
		HostResolver: &callbackHostResolver{
			addr: netip.MustParseAddr("127.0.0.1"),
			onLookup: func() {
				hC.resolver.mu.Lock()
				hC.resolver.conns[hC.orgID+"|"+connC].AllowedPorts = []int{otherPort}
				hC.resolver.mu.Unlock()
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, errC := brkC.Execute(ctx, EgressRequest{
		OrgID:        hC.orgID,
		ConnectionID: connC,
		Method:       http.MethodGet,
		TargetURL:    fmt.Sprintf("http://%s:%d/v1/models", hostC, portC),
	}); errC == nil || !errors.Is(errC, egress.ErrPortDenied) {
		t.Fatalf("expected connect-time ErrPortDenied after port tightening, got %v", errC)
	}
	if hitsC.Load() != 0 {
		t.Fatalf("expected 0 upstream hits after connect-time port tightening, got %d", hitsC.Load())
	}
}

func TestBlindBrokerExecuteStreamScrubbingAndCancellation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newBrokerHarness(t, nil)

	canarySecret := canary.DefaultCanarySecret()
	b64Secret := base64.StdEncoding.EncodeToString([]byte(canarySecret))
	splitIdx := len(canarySecret) / 2
	if splitIdx < 4 {
		splitIdx = 4
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Set-Cookie", "bad=session")
		w.Header().Set("X-Echo", canarySecret)
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		// Chunk 1: prefix + first half of secret (must be held partially until remainder arrives)
		_, _ = fmt.Fprintf(w, "data: {\"chunk\":1,\"val\":\"%s", canarySecret[:splitIdx])
		if fl != nil {
			fl.Flush()
		}
		time.Sleep(10 * time.Millisecond)
		// Chunk 2: second half of raw secret + base64 secret + trailing event
		_, _ = fmt.Fprintf(w, "%s\",\"b64\":\"%s\"}\n\ndata: [DONE]\n\n", canarySecret[splitIdx:], b64Secret)
		if fl != nil {
			fl.Flush()
		}
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	connID := idgen.MustNew(idgen.PrefixConnection)
	secMeta := h.sealCanarySecret(t, "acme_stream_key", nil, []byte(canarySecret))
	h.resolver.put(&ResolvedSlot{
		OrgID:          h.orgID,
		ConnectionID:   connID,
		SlotName:       "api_key",
		SecretID:       secMeta.ID,
		AllowedHosts:   []string{u.Host},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})
	h.resolver.conns[h.orgID+"|"+connID] = &egress.Policy{
		AllowedHosts:   []string{u.Host},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	}

	brk, err := NewBlindBroker(Config{
		Resolver:       h.resolver,
		PolicyResolver: h.resolver,
		Vault:          h.vs,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          h.audit,
	})
	if err != nil {
		t.Fatal(err)
	}

	streamResp, err := brk.ExecuteStream(ctx, EgressRequest{
		OrgID:        h.orgID,
		ConnectionID: connID,
		Method:       http.MethodPost,
		TargetURL:    srv.URL + "/v1/chat/completions",
		CredentialInjections: []CredentialInjection{
			{
				SecretSlotName: "api_key",
				Location:       TargetBearer,
				FieldName:      "Authorization",
				ValuePrefix:    "Bearer ",
			},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	defer func() { _ = streamResp.Body.Close() }()

	if streamResp.SanitizedHeaders.Get("Set-Cookie") != "" {
		t.Fatalf("expected Set-Cookie stripped, got %q", streamResp.SanitizedHeaders.Get("Set-Cookie"))
	}
	if strings.Contains(streamResp.SanitizedHeaders.Get("X-Echo"), canarySecret) || !strings.Contains(streamResp.SanitizedHeaders.Get("X-Echo"), RedactedCredentialPlaceholder) {
		t.Fatalf("expected X-Echo header scrubbed, got %q", streamResp.SanitizedHeaders.Get("X-Echo"))
	}

	allBytes, rerr := io.ReadAll(streamResp.Body)
	if rerr != nil {
		t.Fatalf("read ExecuteStream body: %v", rerr)
	}
	bodyStr := string(allBytes)
	if findings := canary.NewScanner(canarySecret).ScanString("stream_body", bodyStr); len(findings) > 0 {
		t.Fatalf("canary leak detected in ExecuteStream body: %v", findings)
	}
	if strings.Count(bodyStr, RedactedCredentialPlaceholder) != 2 {
		t.Fatalf("expected 2 redacted placeholders (split raw + base64), got %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "data: [DONE]\n\n") {
		t.Fatalf("expected trailing data: [DONE] intact, got %q", bodyStr)
	}

	// Closing must zeroize state cleanly and subsequent reads must fail closed.
	if cerr := streamResp.Body.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	var scratch [8]byte
	if _, rerr2 := streamResp.Body.Read(scratch[:]); !errors.Is(rerr2, io.ErrClosedPipe) {
		t.Fatalf("expected io.ErrClosedPipe after Close, got %v", rerr2)
	}
}

func TestSecretScrubberConcurrentZeroizeNoRace(t *testing.T) {
	t.Parallel()
	for iter := 0; iter < 50; iter++ {
		s := &secretScrubber{}
		s.addSecret([]byte("sk-live-canary-race-test-987654"))

		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = s.scrubString("error dialing with sk-live-canary-race-test-987654")
		}()
		go func() {
			defer wg.Done()
			_ = s.trailingPrefixHold([]byte("partial sk-live"))
		}()
		go func() {
			defer wg.Done()
			s.zeroize()
		}()
		wg.Wait()
	}
}
