// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package netguard

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(MustRunHermeticMain(m))
}

func assertBlockedWithStackTrace(t *testing.T, err error, desc string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected hermetic netguard block error, got nil", desc)
	}
	var blocked *BlockedEgressError
	if errors.As(err, &blocked) {
		if !strings.Contains(blocked.Stack, "goroutine ") {
			t.Fatalf("%s: expected call stack trace in BlockedEgressError, got %q", desc, blocked.Stack)
		}
		return
	}
	// Go's net.Resolver stringifies Dial errors into *net.DNSError.Err instead of wrapping.
	if !strings.Contains(err.Error(), "hermetic_netguard:") || !strings.Contains(err.Error(), "goroutine ") {
		t.Fatalf("%s: expected hermetic netguard error with stack trace, got %T: %v", desc, err, err)
	}
}

func TestNetguardBlocksExternalEgressWithStackTraceNEG1(t *testing.T) {
	t.Parallel()
	InstallHermeticGuard(t)
	InstallIfHermetic(t)

	// 1. Blocked HTTPS egress to external domain via NewHTTPClient.
	client := NewHTTPClient()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	assertBlockedWithStackTrace(t, err, "https://api.openai.com/v1/models")

	// 2. Blocked external DNS lookup via net.DefaultResolver.
	_, dnsErr := net.LookupHost("api.openai.com")
	assertBlockedWithStackTrace(t, dnsErr, "net.LookupHost(api.openai.com)")
	_, lookupIPErr := net.LookupIP("api.openai.com")
	assertBlockedWithStackTrace(t, lookupIPErr, "net.LookupIP(api.openai.com)")
	_, lookupAddrErr := net.LookupAddr("8.8.8.8")
	assertBlockedWithStackTrace(t, lookupAddrErr, "net.LookupAddr(8.8.8.8)")

	// 3. Blocked UDP socket dial to 8.8.8.8:53 via Dialer Control.
	d := NewDialer()
	conn, udpErr := d.Dial("udp", "8.8.8.8:53")
	if conn != nil {
		_ = conn.Close()
	}
	assertBlockedWithStackTrace(t, udpErr, "UDP 8.8.8.8:53")

	// 4. Blocked metadata / RFC1918 / wildcard / spoofed / IPv4-mapped IPv6 addresses.
	for _, forbidden := range []string{
		"",
		"0.0.0.0:80",
		"169.254.169.254:80",
		"10.0.0.1:443",
		"[::]:80",
		"[fe80::1%eth0]:443",
		"[2001:4860:4860::8888]:443",
		"::ffff:169.254.169.254",
		"[::ffff:8.8.8.8]:443",
		"localhost.",
		"127.0.0.1.nip.io:80",
		"127.0.0.1:badport",
		"exfil.attacker.example:443",
	} {
		assertBlockedWithStackTrace(t, CheckLoopbackAddr(forbidden), "CheckLoopbackAddr("+forbidden+")")
		assertBlockedWithStackTrace(t, CheckLoopbackIPAddr(forbidden), "CheckLoopbackIPAddr("+forbidden+")")
	}

	// Direct CheckLoopbackIPAddr with empty address and TCP dial block via NewDialer.
	assertBlockedWithStackTrace(t, CheckLoopbackIPAddr(""), "CheckLoopbackIPAddr(empty)")
	tcpConn, tcpErr := d.Dial("tcp", "1.1.1.1:443")
	if tcpConn != nil {
		_ = tcpConn.Close()
	}
	assertBlockedWithStackTrace(t, tcpErr, "TCP 1.1.1.1:443")

	// Control rejects unparsed hostname even when it says localhost.
	assertBlockedWithStackTrace(t, Control("tcp", "localhost:8080", nil), "Control(localhost:8080)")
}

func TestNetguardAllowsLoopbackServersAndCustomTransport(t *testing.T) {
	t.Parallel()
	InstallHermeticGuard(t)

	for _, allowed := range []string{
		"127.0.0.1:8080",
		"127.0.1.99:9000",
		"[::1]:8443",
		"::1",
		"[::1]",
		"::ffff:127.0.0.1",
		"localhost:8080",
	} {
		if err := CheckLoopbackAddr(allowed); err != nil {
			t.Fatalf("expected CheckLoopbackAddr(%q) to allow loopback, got %v", allowed, err)
		}
	}

	if err := Control("tcp", "127.0.0.1:8080", nil); err != nil {
		t.Fatalf("expected Control loopback IPv4 allow, got %v", err)
	}
	if err := Control("tcp", "[::1]:8080", nil); err != nil {
		t.Fatalf("expected Control loopback IPv6 allow, got %v", err)
	}

	// HTTP loopback test server works cleanly through guarded client.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("loopback-ok"))
	}))
	defer srv.Close()

	client := NewHTTPClient()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("loopback HTTP request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	// Custom Transport wrapped with DialTLSContext also blocks external hosts.
	customBase := &http.Transport{
		DialContext: NewDialer().DialContext,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&tls.Dialer{NetDialer: NewDialer()}).DialContext(ctx, network, addr)
		},
	}
	wrapped := WrapTransport(customBase)
	tlsClient := &http.Client{Transport: wrapped}
	tlsReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://1.1.1.1:443/", nil)
	if err != nil {
		t.Fatalf("new tls request: %v", err)
	}
	tlsResp, tlsErr := tlsClient.Do(tlsReq)
	if tlsResp != nil {
		_ = tlsResp.Body.Close()
	}
	assertBlockedWithStackTrace(t, tlsErr, "WrapTransport DialTLSContext 1.1.1.1")

	// Arbitrary custom RoundTripper wrapped via WrapRoundTripper also enforces loopback.
	customRT := WrapRoundTripper(dummyRoundTripper{})
	if rtAgain := WrapRoundTripper(customRT); rtAgain != customRT {
		t.Fatalf("expected WrapRoundTripper to be idempotent")
	}
	if defaultRT := WrapRoundTripper(nil); defaultRT == nil {
		t.Fatalf("expected WrapRoundTripper(nil) non-nil")
	}
	customClient := &http.Client{Transport: customRT}
	extReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.openai.com/v1/models", nil)
	extResp, extErr := customClient.Do(extReq)
	if extResp != nil {
		_ = extResp.Body.Close()
	}
	assertBlockedWithStackTrace(t, extErr, "WrapRoundTripper external domain")

	// Deprecated http.Transport.DialTLS and Dial hooks must also be wrapped by WrapTransport.
	legacyTLSTr := WrapTransport(&http.Transport{
		DialContext: NewDialer().DialContext,
		Dial: func(network, addr string) (net.Conn, error) {
			return NewDialer().Dial(network, addr)
		},
		DialTLS: func(network, addr string) (net.Conn, error) {
			return (&tls.Dialer{NetDialer: NewDialer()}).DialContext(context.Background(), network, addr)
		},
	})
	legacyClient := &http.Client{Transport: legacyTLSTr}
	legTLSReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://1.1.1.1:443/", nil)
	legTLSResp, legTLSErr := legacyClient.Do(legTLSReq)
	if legTLSResp != nil {
		_ = legTLSResp.Body.Close()
	}
	assertBlockedWithStackTrace(t, legTLSErr, "WrapTransport deprecated DialTLS 1.1.1.1")

	legHTTPReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://169.254.169.254/", nil)
	legHTTPResp, legHTTPErr := legacyClient.Do(legHTTPReq)
	if legHTTPResp != nil {
		_ = legHTTPResp.Body.Close()
	}
	assertBlockedWithStackTrace(t, legHTTPErr, "WrapTransport deprecated Dial 169.254.169.254")

	// Post-dial RemoteAddr() check blocks even custom DialContext returning an external RemoteAddr.
	spoofConnTr := WrapTransport(&http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return fakeExternalConn{}, nil
		},
	})
	spoofClient := &http.Client{Transport: spoofConnTr}
	spoofReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost:8080/", nil)
	spoofResp, spoofErr := spoofClient.Do(spoofReq)
	if spoofResp != nil {
		_ = spoofResp.Body.Close()
	}
	assertBlockedWithStackTrace(t, spoofErr, "post-dial verifyConnLoopback external RemoteAddr")
}

type fakeExternalConn struct{ net.Conn }

func (fakeExternalConn) Close() error { return nil }
func (fakeExternalConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 443}
}

type dummyRoundTripper struct{}

func (dummyRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("should not reach underlying RoundTrip on external URL")
}

func TestNetguardBlocksRedirectAndProxyBypassAttempts(t *testing.T) {
	InstallHermeticGuard(t)

	// 1. Loopback server redirecting 302 to external API or cloud IMDS must be intercepted.
	redirSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/imds" {
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
			return
		}
		http.Redirect(w, r, "https://api.openai.com/v1/models", http.StatusFound)
	}))
	defer redirSrv.Close()

	client := NewHTTPClient()
	for _, path := range []string{"/openai", "/imds"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, redirSrv.URL+path, nil)
		if err != nil {
			t.Fatalf("new redirect request: %v", err)
		}
		resp, doErr := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		assertBlockedWithStackTrace(t, doErr, "redirect bypass "+path)
	}

	// 2. Proxy bypass attempts (both external proxy host AND local proxy forwarding external URL).
	t.Setenv("HTTP_PROXY", "http://exfil.attacker.example:8888")
	t.Setenv("HTTPS_PROXY", "http://exfil.attacker.example:8888")
	envProxyBase := NewHTTPTransport()
	envProxyBase.Proxy = http.ProxyFromEnvironment
	envProxyClient := &http.Client{
		Transport: WrapTransport(envProxyBase),
	}
	reqExtProxy, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, redirSrv.URL+"/ok", nil)
	respExtProxy, errExtProxy := envProxyClient.Do(reqExtProxy)
	if respExtProxy != nil {
		_ = respExtProxy.Body.Close()
	}
	assertBlockedWithStackTrace(t, errExtProxy, "HTTP_PROXY external proxy host")

	localProxyURL, _ := url.Parse("http://127.0.0.1:8888")
	localProxyBase := NewHTTPTransport()
	localProxyBase.Proxy = http.ProxyURL(localProxyURL)
	localProxyClient := &http.Client{
		Transport: WrapTransport(localProxyBase),
	}
	reqExtTarget, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.openai.com/v1/models", nil)
	respExtTarget, errExtTarget := localProxyClient.Do(reqExtTarget)
	if respExtTarget != nil {
		_ = respExtTarget.Body.Close()
	}
	assertBlockedWithStackTrace(t, errExtTarget, "local HTTP proxy forwarding external URL")
}

func TestNetguardCleanupRestoresResolverWhenNotMain(t *testing.T) {
	// Intentionally non-parallel: validates non-main InstallHermeticGuard restore path.
	restoredDefTr, _, origDefTr, restoredResSame := runIsolatedGuardStateTest(nil, func() {
		t.Run("subtest", func(t *testing.T) {
			InstallHermeticGuard(t)
			if !IsHermeticEnabled() {
				t.Fatalf("expected IsHermeticEnabled true")
			}
		})
	})
	if !restoredResSame || restoredDefTr != origDefTr {
		t.Fatalf("expected resolver and DefaultTransport restored after cleanup (resSame=%v trSame=%v)", restoredResSame, restoredDefTr == origDefTr)
	}
}
