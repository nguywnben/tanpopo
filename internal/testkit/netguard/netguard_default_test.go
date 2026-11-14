// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Dedicated CR-11 / CONSTRAINTS.md E4 negative-test suite verifying that netguard
// intercepts direct stdlib net/http defaults (http.Get, http.Post, http.Head,
// http.DefaultClient, and unconfigured &http.Client{} / third-party clients) and
// verifying OS-level kernel network-namespace isolation drops raw-IP net.Dial calls.
package netguard

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestNetguardBlocksDirectStdlibHTTPAndThirdPartyClientsNEG1(t *testing.T) {
	t.Parallel()
	InstallHermeticGuard(t)

	// 1. Direct http.Get against external hostname.
	r1, err1 := http.Get("https://api.openai.com/v1/models")
	if r1 != nil {
		_ = r1.Body.Close()
	}
	assertBlockedWithStackTrace(t, err1, "direct http.Get(api.openai.com)")

	// 2. Direct http.Get against raw external IP (bypasses DNS; proves http.DefaultTransport is guarded!).
	r2, err2 := http.Get("https://1.1.1.1/")
	if r2 != nil {
		_ = r2.Body.Close()
	}
	assertBlockedWithStackTrace(t, err2, "direct http.Get(1.1.1.1)")

	// 3. Direct http.Post and http.Head to external endpoints.
	r3, err3 := http.Post("https://api.anthropic.com/v1/messages", "application/json", strings.NewReader(`{}`))
	if r3 != nil {
		_ = r3.Body.Close()
	}
	assertBlockedWithStackTrace(t, err3, "direct http.Post(api.anthropic.com)")

	r4, err4 := http.Head("http://169.254.169.254/latest/meta-data/")
	if r4 != nil {
		_ = r4.Body.Close()
	}
	assertBlockedWithStackTrace(t, err4, "direct http.Head(169.254.169.254)")

	// 4. Bare &http.Client{} (typical third-party SDK default using http.DefaultTransport) and http.DefaultClient.
	thirdPartyClient := &http.Client{}
	for _, target := range []string{
		"https://api.openai.com/v1/models",
		"https://1.1.1.1/",
		"http://169.254.169.254/latest/meta-data/",
	} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if err != nil {
			t.Fatalf("new request %s: %v", target, err)
		}
		respA, errA := thirdPartyClient.Do(req)
		if respA != nil {
			_ = respA.Body.Close()
		}
		assertBlockedWithStackTrace(t, errA, "thirdParty &http.Client{} "+target)

		respB, errB := http.DefaultClient.Do(req)
		if respB != nil {
			_ = respB.Body.Close()
		}
		assertBlockedWithStackTrace(t, errB, "http.DefaultClient "+target)
	}

	// 5. Loopback calls using default &http.Client{} and http.Get still succeed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok-default-loopback")
	}))
	defer srv.Close()

	respOK, errOK := http.Get(srv.URL)
	if errOK != nil {
		t.Fatalf("loopback http.Get(%s) failed: %v", srv.URL, errOK)
	}
	_ = respOK.Body.Close()
	if respOK.StatusCode != http.StatusOK {
		t.Fatalf("loopback http.Get status=%d", respOK.StatusCode)
	}

	// 6. Unguarded hostname net.Dial intercepted by net.DefaultResolver prior to socket connect.
	conn, dialHostErr := net.Dial("tcp", "api.openai.com:443")
	if conn != nil {
		_ = conn.Close()
	}
	assertBlockedWithStackTrace(t, dialHostErr, "unguarded net.Dial(api.openai.com:443)")
}

func TestNetguardRestoreIncludesDefaultTransportAndClient(t *testing.T) {
	// Non-parallel: verifies restoreGuardsLocked when mainGuardActive is false and
	// http.DefaultClient.Transport had a pre-existing custom transport.
	restoredDefTr, restoredClientTr, origDefTr, restoredResSame := runIsolatedGuardStateTest(dummyRoundTripper{}, func() {
		t.Run("subtest", func(t *testing.T) {
			InstallHermeticGuard(t)
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.openai.com/v1/models", nil)
			resp, err := http.DefaultClient.Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			assertBlockedWithStackTrace(t, err, "wrapped custom DefaultClient.Transport")
		})
	})

	if !restoredResSame || restoredDefTr != origDefTr {
		t.Fatalf("expected DefaultResolver and http.DefaultTransport restored after cleanup")
	}
	if _, ok := restoredClientTr.(dummyRoundTripper); !ok {
		t.Fatalf("expected custom http.DefaultClient.Transport restored after cleanup, got %T", restoredClientTr)
	}
}

func TestLinuxOSNetworkNamespaceDropsRawUnguardedNetDial(t *testing.T) {
	t.Parallel()

	// Fail closed on Linux GitHub Actions CI if Stage 1 did not execute inside unshare -n!
	if os.Getenv("GITHUB_ACTIONS") == "true" && runtime.GOOS == "linux" && os.Getenv("TANPOPO_OS_NETNS_ISOLATED") != "1" {
		t.Fatalf("CI isolation contract violation: Linux GitHub Actions run must execute inside verified unshare --net with TANPOPO_OS_NETNS_ISOLATED=1")
	}

	// Never transmit real raw-IP packets to external Internet IPs on unisolated workstations
	// (e.g. local Windows dev or runners without kernel NETNS). Only probe raw unguarded net.Dial
	// when running inside an empirically verified OS network namespace (TANPOPO_OS_NETNS_ISOLATED=1).
	if os.Getenv("TANPOPO_OS_NETNS_ISOLATED") != "1" {
		return
	}

	// Verify loopback TCP bind/dial succeeds inside isolated netns while raw external UDP & TCP fail at kernel layer.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("loopback listen failed inside isolated netns: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, aerr := ln.Accept()
		if aerr == nil {
			_ = c.Close()
		}
	}()
	lc, lerr := net.Dial("tcp", ln.Addr().String())
	if lerr != nil {
		_ = ln.Close()
		t.Fatalf("loopback net.Dial failed inside isolated netns: %v", lerr)
	}
	_ = lc.Close()
	_ = ln.Close()
	<-done

	if c, udpErr := net.Dial("udp", "8.8.8.8:53"); udpErr == nil {
		_ = c.Close()
		t.Fatalf("CRITICAL ISOLATION FAILURE: raw net.Dial(udp, 8.8.8.8:53) succeeded inside supposedly isolated netns!")
	}
	if c, tcpErr := net.Dial("tcp", "1.1.1.1:443"); tcpErr == nil {
		_ = c.Close()
		t.Fatalf("CRITICAL ISOLATION FAILURE: raw net.Dial(tcp, 1.1.1.1:443) succeeded inside supposedly isolated netns!")
	}
}
