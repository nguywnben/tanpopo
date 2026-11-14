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
	"os"
	"runtime/debug"
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
	guardMu          sync.Mutex
	guardRefCount    int
	mainGuardActive  bool
	savedDefaultRes  *net.Resolver
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
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
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

// WrapTransport clones base (or allocates a fresh *http.Transport) and enforces
// loopback-only pre-dial and connect-time Control checks.
func WrapTransport(base *http.Transport) *http.Transport {
	var tr *http.Transport
	if base != nil {
		tr = base.Clone()
	} else {
		tr = &http.Transport{
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
		return origDial(ctx, network, addr)
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
			return origDialTLS(ctx, network, addr)
		}
	}
	return tr
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

func activateResolverLocked() {
	if savedDefaultRes == nil {
		savedDefaultRes = net.DefaultResolver
	}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial:     hermeticResolverDial,
	}
}

// InstallHermeticGuard activates the hermetic DNS resolver guard for the lifetime of t
// and restores net.DefaultResolver when the last active guard cleans up.
// When MustRunHermeticMain or an earlier active guard already installed the resolver,
// net.DefaultResolver is left untouched to remain race-free across t.Parallel() tests.
func InstallHermeticGuard(t testing.TB) {
	t.Helper()

	guardMu.Lock()
	if !mainGuardActive && guardRefCount == 0 {
		activateResolverLocked()
	}
	guardRefCount++
	guardMu.Unlock()

	t.Cleanup(func() {
		guardMu.Lock()
		defer guardMu.Unlock()
		if guardRefCount > 0 {
			guardRefCount--
		}
		if guardRefCount == 0 && !mainGuardActive && savedDefaultRes != nil {
			net.DefaultResolver = savedDefaultRes
			savedDefaultRes = nil
		}
	})
}

// InstallIfHermetic activates the hermetic DNS guard when TANPOPO_TEST_HERMETIC=1.
func InstallIfHermetic(t testing.TB) {
	t.Helper()
	if IsHermeticEnabled() {
		InstallHermeticGuard(t)
	}
}

// MustRunHermeticMain sets TANPOPO_TEST_HERMETIC=1 if unset, activates the global
// DNS resolver guard, and runs m. It returns m.Run()'s exit code for os.Exit.
func MustRunHermeticMain(m *testing.M) int {
	if os.Getenv(EnvHermetic) == "" {
		_ = os.Setenv(EnvHermetic, "1")
	}
	guardMu.Lock()
	mainGuardActive = true
	activateResolverLocked()
	guardMu.Unlock()

	return m.Run()
}
