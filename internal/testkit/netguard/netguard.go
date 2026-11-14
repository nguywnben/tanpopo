// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package netguard enforces in-process hermetic network isolation during tests.
// When activated, all outbound TCP/UDP dials and DNS lookups targeting non-loopback
// destinations are intercepted prior to packet egress and fail with a full call stack trace.
//
// In accordance with CONSTRAINTS.md and scripts/ci/check.go Floor Guard, code inside
// internal/ never references http.DefaultClient or http.DefaultTransport; callers
// construct guarded HTTP clients via netguard.NewHTTPClient / WrapTransport and wire
// netguard.CheckLoopbackAddr into production SSRF-guarded transports during tests.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// EnvHermetic enables strict hermetic network guarding when set to "1".
const EnvHermetic = "TANPOPO_TEST_HERMETIC"

// BlockedEgressError reports an attempted non-loopback network or DNS operation
// along with the full goroutine call stack where the call originated.
type BlockedEgressError struct {
	Network string
	Address string
	Reason  string
	Stack   string
}

func (e *BlockedEgressError) Error() string {
	return fmt.Sprintf("hermetic_netguard: blocked %s %q (%s)\n%s", e.Network, e.Address, e.Reason, e.Stack)
}

var (
	guardMu               sync.Mutex
	guardRefCount         int
	mainGuardActive       bool
	savedDefaultRes       *net.Resolver
	savedDefaultTransport http.RoundTripper
	savedDefaultClientTr  http.RoundTripper
)

// IsHermeticEnabled reports whether TANPOPO_TEST_HERMETIC is set to "1".
func IsHermeticEnabled() bool {
	return os.Getenv(EnvHermetic) == "1"
}

func newBlockedErr(network, address, reason string) error {
	return &BlockedEgressError{
		Network: network,
		Address: address,
		Reason:  reason,
		Stack:   string(debug.Stack()),
	}
}

func extractHost(addr string) (string, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "", fmt.Errorf("empty address")
	}
	if host, port, err := net.SplitHostPort(trimmed); err == nil {
		if port != "" {
			p, perr := strconv.Atoi(port)
			if perr != nil || p < 0 || p > 65535 {
				return "", fmt.Errorf("invalid port %q", port)
			}
		}
		return host, nil
	}
	// Strip brackets from bare IPv6 literals such as "[::1]".
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		return trimmed[1 : len(trimmed)-1], nil
	}
	return trimmed, nil
}

// CheckLoopbackAddr validates that addr targets a loopback IP (127.0.0.0/8 or ::1)
// or the literal "localhost" hostname. Returns BlockedEgressError (with stack trace)
// for any external domain, metadata IP, wildcard bind address, or non-loopback IP.
func CheckLoopbackAddr(addr string) error {
	host, err := extractHost(addr)
	if err != nil {
		return newBlockedErr("addr", addr, err.Error())
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	return checkNumericLoopbackHost("addr", addr, host)
}

// CheckLoopbackIPAddr validates that addr resolves to a numeric loopback IP address
// (127.0.0.0/8 or ::1) and rejects unparsed hostnames, zone-scoped addresses, and non-loopback IPs.
func CheckLoopbackIPAddr(addr string) error {
	host, err := extractHost(addr)
	if err != nil {
		return newBlockedErr("connect", addr, err.Error())
	}
	return checkNumericLoopbackHost("connect", addr, host)
}

func checkNumericLoopbackHost(network, rawAddr, host string) error {
	if strings.ContainsRune(host, '%') {
		return newBlockedErr(network, rawAddr, "zone-scoped non-loopback IPv6 address forbidden")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return newBlockedErr(network, rawAddr, "non-loopback hostname/address forbidden under hermetic test guard")
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4.IsLoopback() {
			return nil
		}
		return newBlockedErr(network, rawAddr, "IPv4 address is outside 127.0.0.0/8")
	}
	if ip.IsLoopback() {
		return nil
	}
	return newBlockedErr(network, rawAddr, "IPv6 address is not ::1")
}

// Control enforces strict loopback-only socket connections at net.Dialer connect time.
func Control(network, address string, _ syscall.RawConn) error {
	if err := CheckLoopbackIPAddr(address); err != nil {
		if be, ok := err.(*BlockedEgressError); ok {
			be.Network = network
		}
		return err
	}
	return nil
}

// NewDialer returns a net.Dialer configured with hermetic Control guarding.
func NewDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   2 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   Control,
	}
}

func verifyConnLoopback(conn net.Conn, err error) (net.Conn, error) {
	if err != nil {
		return nil, err
	}
	if conn != nil && conn.RemoteAddr() != nil {
		if cerr := CheckLoopbackIPAddr(conn.RemoteAddr().String()); cerr != nil {
			_ = conn.Close()
			if be, ok := cerr.(*BlockedEgressError); ok {
				be.Network = conn.RemoteAddr().Network()
			}
			return nil, cerr
		}
	}
	return conn, nil
}

// WrapTransport clones base (or allocates a fresh *http.Transport) and enforces
// loopback-only pre-dial, post-dial RemoteAddr(), and proxy checks across all
// context and deprecated Transport dial hooks (DialContext, Dial, DialTLSContext, DialTLS).
func WrapTransport(base *http.Transport) *http.Transport {
	var tr *http.Transport
	if base != nil {
		tr = base.Clone()
	} else {
		tr = &http.Transport{
			DialContext:         NewDialer().DialContext,
			MaxIdleConns:        128,
			MaxIdleConnsPerHost: 64,
			IdleConnTimeout:     30 * time.Second,
		}
	}
	origDial := tr.DialContext
	if origDial == nil {
		origDial = NewDialer().DialContext
	}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if err := CheckLoopbackAddr(addr); err != nil {
			if be, ok := err.(*BlockedEgressError); ok {
				be.Network = network
			}
			return nil, err
		}
		return verifyConnLoopback(origDial(ctx, network, addr))
	}
	if tr.Dial != nil {
		origLegacyDial := tr.Dial
		tr.Dial = func(network, addr string) (net.Conn, error) {
			if err := CheckLoopbackAddr(addr); err != nil {
				if be, ok := err.(*BlockedEgressError); ok {
					be.Network = network
				}
				return nil, err
			}
			return verifyConnLoopback(origLegacyDial(network, addr))
		}
	}
	if tr.Proxy != nil {
		origProxy := tr.Proxy
		tr.Proxy = func(req *http.Request) (*url.URL, error) {
			if req != nil && req.URL != nil {
				if err := CheckLoopbackAddr(req.URL.Host); err != nil {
					if be, ok := err.(*BlockedEgressError); ok {
						be.Network = "http-proxy-target"
					}
					return nil, err
				}
			}
			proxyURL, err := origProxy(req)
			if err != nil || proxyURL == nil {
				return proxyURL, err
			}
			if perr := CheckLoopbackAddr(proxyURL.Host); perr != nil {
				if be, ok := perr.(*BlockedEgressError); ok {
					be.Network = "http-proxy"
				}
				return nil, perr
			}
			return proxyURL, nil
		}
	}
	if tr.DialTLSContext != nil {
		origDialTLS := tr.DialTLSContext
		tr.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if err := CheckLoopbackAddr(addr); err != nil {
				if be, ok := err.(*BlockedEgressError); ok {
					be.Network = network
				}
				return nil, err
			}
			return verifyConnLoopback(origDialTLS(ctx, network, addr))
		}
	}
	if tr.DialTLS != nil {
		origLegacyDialTLS := tr.DialTLS
		tr.DialTLS = func(network, addr string) (net.Conn, error) {
			if err := CheckLoopbackAddr(addr); err != nil {
				if be, ok := err.(*BlockedEgressError); ok {
					be.Network = network
				}
				return nil, err
			}
			return verifyConnLoopback(origLegacyDialTLS(network, addr))
		}
	}
	return tr
}

// WrapRoundTripper wraps an arbitrary http.RoundTripper (e.g. a custom transport or
// *http.Transport) so any outbound HTTP request targeting a non-loopback host is
// rejected before dialing.
func WrapRoundTripper(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		return NewHTTPTransport()
	}
	if tr, ok := rt.(*http.Transport); ok {
		return WrapTransport(tr)
	}
	if grt, ok := rt.(*guardedRoundTripper); ok {
		return grt
	}
	return &guardedRoundTripper{base: rt}
}

type guardedRoundTripper struct {
	base http.RoundTripper
}

func (g *guardedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req != nil && req.URL != nil {
		if err := CheckLoopbackAddr(req.URL.Host); err != nil {
			if be, ok := err.(*BlockedEgressError); ok {
				be.Network = "http"
			}
			return nil, err
		}
	}
	return g.base.RoundTrip(req)
}

// NewHTTPTransport returns a fresh *http.Transport guarded against non-loopback egress.
func NewHTTPTransport() *http.Transport {
	return WrapTransport(nil)
}

// NewHTTPClient returns a fresh *http.Client using a hermetic loopback-only Transport.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Transport: NewHTTPTransport(),
	}
}

func hermeticResolverDial(_ context.Context, network, address string) (net.Conn, error) {
	return nil, newBlockedErr(network, address, "external DNS resolver queries are forbidden under hermetic test guard")
}

func activateGuardsLocked() {
	if savedDefaultRes == nil {
		savedDefaultRes = net.DefaultResolver
	}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial:     hermeticResolverDial,
	}

	if savedDefaultTransport == nil {
		savedDefaultTransport = http.DefaultTransport
	}
	if defTr, ok := savedDefaultTransport.(*http.Transport); ok {
		// Replace stdlib DefaultTransport's unguarded Dialer with a Control-guarded
		// Dialer so even /etc/hosts overrides for localhost fail at connect() time.
		defClone := defTr.Clone()
		guardedDialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			Control:   Control,
		}
		defClone.DialContext = guardedDialer.DialContext
		http.DefaultTransport = WrapTransport(defClone)
	} else {
		http.DefaultTransport = WrapRoundTripper(savedDefaultTransport)
	}

	if http.DefaultClient.Transport != nil {
		if savedDefaultClientTr == nil {
			savedDefaultClientTr = http.DefaultClient.Transport
		}
		http.DefaultClient.Transport = WrapRoundTripper(savedDefaultClientTr)
	}
}

func restoreGuardsLocked() {
	if savedDefaultRes != nil {
		net.DefaultResolver = savedDefaultRes
		savedDefaultRes = nil
	}
	if savedDefaultTransport != nil {
		http.DefaultTransport = savedDefaultTransport
		savedDefaultTransport = nil
	}
	if savedDefaultClientTr != nil {
		http.DefaultClient.Transport = savedDefaultClientTr
		savedDefaultClientTr = nil
	}
}

// InstallHermeticGuard activates hermetic DNS, http.DefaultTransport, and
// http.DefaultClient isolation for the lifetime of t, restoring previous globals
// when the last active guard cleans up.
//
// Concurrency contract: packages running tests with t.Parallel() alongside stdlib
// net/http or net.DefaultResolver calls MUST install MustRunHermeticMain in TestMain
// so globals are initialized once before any parallel test goroutines spawn;
// InstallHermeticGuard leaves globals untouched whenever mainGuardActive or guardRefCount > 0.
func InstallHermeticGuard(t testing.TB) {
	t.Helper()

	guardMu.Lock()
	if !mainGuardActive && guardRefCount == 0 {
		activateGuardsLocked()
	}
	guardRefCount++
	guardMu.Unlock()

	t.Cleanup(func() {
		guardMu.Lock()
		defer guardMu.Unlock()
		if guardRefCount > 0 {
			guardRefCount--
		}
		if guardRefCount == 0 && !mainGuardActive {
			restoreGuardsLocked()
		}
	})
}

// InstallIfHermetic activates the hermetic guards when TANPOPO_TEST_HERMETIC=1.
func InstallIfHermetic(t testing.TB) {
	t.Helper()
	if IsHermeticEnabled() {
		InstallHermeticGuard(t)
	}
}

// MustRunHermeticMain sets TANPOPO_TEST_HERMETIC=1 if unset, activates process-wide
// DNS and default HTTP transport isolation before spawning tests, and returns m.Run().
func MustRunHermeticMain(m *testing.M) int {
	if os.Getenv(EnvHermetic) == "" {
		_ = os.Setenv(EnvHermetic, "1")
	}
	guardMu.Lock()
	if !mainGuardActive {
		mainGuardActive = true
		activateGuardsLocked()
	}
	guardMu.Unlock()

	return m.Run()
}

// runIsolatedGuardStateTest temporarily suspends global guard state to verify
// non-main install/restore semantics in unit tests and restores exact prior globals.
func runIsolatedGuardStateTest(customClientTr http.RoundTripper, fn func()) (restoredDefTr, restoredClientTr, origDefTr http.RoundTripper, restoredResSame bool) {
	guardMu.Lock()
	origMain := mainGuardActive
	origRes := net.DefaultResolver
	origDefTr = http.DefaultTransport
	origClientTr := http.DefaultClient.Transport
	origSavedRes := savedDefaultRes
	origSavedTr := savedDefaultTransport
	origSavedClientTr := savedDefaultClientTr
	origCount := guardRefCount

	mainGuardActive = false
	guardRefCount = 0
	savedDefaultRes = nil
	savedDefaultTransport = nil
	savedDefaultClientTr = nil
	if customClientTr != nil {
		http.DefaultClient.Transport = customClientTr
	}
	guardMu.Unlock()

	defer func() {
		guardMu.Lock()
		mainGuardActive = origMain
		guardRefCount = origCount
		net.DefaultResolver = origRes
		http.DefaultTransport = origDefTr
		http.DefaultClient.Transport = origClientTr
		savedDefaultRes = origSavedRes
		savedDefaultTransport = origSavedTr
		savedDefaultClientTr = origSavedClientTr
		guardMu.Unlock()
	}()

	fn()

	guardMu.Lock()
	restoredDefTr = http.DefaultTransport
	restoredClientTr = http.DefaultClient.Transport
	restoredResSame = net.DefaultResolver == origRes
	guardMu.Unlock()
	return restoredDefTr, restoredClientTr, origDefTr, restoredResSame
}
