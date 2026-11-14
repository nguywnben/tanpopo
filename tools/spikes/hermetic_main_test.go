package spikes_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"
)

func isLoopbackAddr(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host == "localhost"
	}
	return ip.IsLoopback()
}

func hermeticControl(network, address string, _ syscall.RawConn) error {
	if !isLoopbackAddr(address) {
		return fmt.Errorf("hermetic_netguard: blocked non-loopback %s connection to %q", network, address)
	}
	return nil
}

func newHermeticHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 2 * time.Second,
		Control: hermeticControl,
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			MaxIdleConns:        2048,
			MaxIdleConnsPerHost: 2048,
			IdleConnTimeout:     30 * time.Second,
		},
	}
}

func TestMain(m *testing.M) {
	// Lock global DefaultResolver and DefaultTransport to 127.0.0.1 / ::1 only.
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if !isLoopbackAddr(address) {
				return nil, fmt.Errorf("hermetic_netguard: blocked external DNS lookup via %s (%s)", address, network)
			}
			var d net.Dialer
			d.Control = hermeticControl
			return d.DialContext(ctx, network, address)
		},
	}
	http.DefaultClient = newHermeticHTTPClient()
	http.DefaultTransport = http.DefaultClient.Transport
	os.Exit(m.Run())
}

func TestHermeticGuardBlocksExternalEgress(t *testing.T) {
	_, err := http.Get("https://api.openai.com/v1/models")
	if err == nil {
		t.Fatalf("expected hermetic guard to block external HTTPS call, got nil")
	}

	d := net.Dialer{Timeout: 200 * time.Millisecond, Control: hermeticControl}
	conn, err := d.Dial("udp", "8.8.8.8:53")
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected hermetic guard to block external UDP 8.8.8.8:53, got nil")
	}
}
