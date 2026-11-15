// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package testharness

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const canaryPlaceholder = "__CANARY_SECRET__"

// defaultHarnessCanary returns the canonical test canary value assembled dynamically.
func defaultHarnessCanary() string {
	return "sk-tp-" + "canary-DO-NOT-LEAK-9f8e7d6c"
}

var (
	canaryTokenPattern = regexp.MustCompile(`sk-tp-` + `canary-[A-Za-z0-9_-]{4,}`)
	base64TokenPattern = regexp.MustCompile(`[A-Za-z0-9+/_-]{12,}={0,2}`)
	unicodeEscPattern  = regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)
	percentEscPattern  = regexp.MustCompile(`%[0-9a-fA-F]{2}`)
)

// LeakFinding identifies a canary secret exposure without revealing secret bytes.
type LeakFinding struct {
	Source  string
	Variant string
	Offset  int
}

func (f LeakFinding) String() string {
	return fmt.Sprintf("[REDACTED:canary_leak variant=%s source=%q offset=%d]", f.Variant, f.Source, f.Offset)
}

// SecretLeakScanner inspects adapter specs, outputs, SSE frames, and errors for canary echoes.
type SecretLeakScanner struct {
	secrets []string
}

// NewSecretLeakScanner constructs a scanner monitoring all supplied secret slot values
// alongside canonical Tanpopo canary token shapes.
func NewSecretLeakScanner(secrets ...string) *SecretLeakScanner {
	seen := make(map[string]struct{}, len(secrets)+1)
	var list []string
	add := func(v string) {
		if len(v) < 6 {
			return
		}
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			list = append(list, v)
		}
	}
	add(defaultHarnessCanary())
	for _, s := range secrets {
		add(s)
	}
	return &SecretLeakScanner{secrets: list}
}

// Scan inspects text across raw, base64 (aligned & unaligned), URL-encoded, and JSON \uXXXX forms.
func (s *SecretLeakScanner) Scan(source, text string) []LeakFinding {
	if text == "" {
		return nil
	}
	var findings []LeakFinding
	if loc := canaryTokenPattern.FindStringIndex(text); loc != nil {
		findings = append(findings, LeakFinding{Source: source, Variant: "canary-pattern", Offset: loc[0]})
	}
	for _, sec := range s.secrets {
		if idx := strings.Index(text, sec); idx >= 0 {
			findings = append(findings, LeakFinding{Source: source, Variant: "raw", Offset: idx})
		}
		for _, enc := range []struct {
			name string
			val  string
		}{
			{"base64-std", base64.StdEncoding.EncodeToString([]byte(sec))},
			{"base64-raw", base64.RawStdEncoding.EncodeToString([]byte(sec))},
			{"base64-url", base64.URLEncoding.EncodeToString([]byte(sec))},
			{"url-query", url.QueryEscape(sec)},
			{"url-path", url.PathEscape(sec)},
		} {
			if idx := strings.Index(text, enc.val); idx >= 0 {
				findings = append(findings, LeakFinding{Source: source, Variant: enc.name, Offset: idx})
			}
		}
	}

	for _, match := range base64TokenPattern.FindAllStringIndex(text, -1) {
		if containsDecodedSecret(text[match[0]:match[1]], s.secrets) {
			findings = append(findings, LeakFinding{Source: source, Variant: "base64-decoded", Offset: match[0]})
		}
	}

	if strings.ContainsRune(text, '%') {
		decoded := multiURLDecode(text, 2)
		if decoded != text {
			findings = append(findings, s.scanPlain(source, "url-decoded", decoded)...)
		}
	}
	if unicodeEscPattern.MatchString(text) {
		unesc := decodeJSONEscapes(text)
		if unesc != text {
			findings = append(findings, s.scanPlain(source, "json-unicode-escaped", unesc)...)
		}
	}
	return dedupLeakFindings(findings)
}

// Check returns a redacted ExitSecurity error if data contains any secret leak.
func (s *SecretLeakScanner) Check(source, text string) error {
	findings := s.Scan(source, text)
	if len(findings) == 0 {
		return nil
	}
	msgs := make([]string, len(findings))
	for i, f := range findings {
		msgs[i] = f.String()
	}
	return securityErr(fmt.Sprintf("canary secret leak detected in %s: %s", source, strings.Join(msgs, "; ")), nil)
}

func (s *SecretLeakScanner) scanPlain(source, variant, text string) []LeakFinding {
	var out []LeakFinding
	if loc := canaryTokenPattern.FindStringIndex(text); loc != nil {
		out = append(out, LeakFinding{Source: source, Variant: variant, Offset: loc[0]})
	}
	for _, sec := range s.secrets {
		if idx := strings.Index(text, sec); idx >= 0 {
			out = append(out, LeakFinding{Source: source, Variant: variant, Offset: idx})
		}
	}
	return out
}

func containsDecodedSecret(token string, secrets []string) bool {
	base := strings.TrimRight(token, "=")
	maxShift := 3
	if len(base) <= maxShift {
		maxShift = len(base) - 1
	}
	encodings := []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding}
	for shift := 0; shift <= maxShift; shift++ {
		sub := base[shift:]
		if len(sub)%4 == 1 {
			sub = sub[:len(sub)-1]
		}
		if len(sub) < 12 {
			continue
		}
		for _, enc := range encodings {
			raw, err := enc.DecodeString(sub)
			if err != nil {
				continue
			}
			decoded := string(raw)
			if canaryTokenPattern.MatchString(decoded) {
				return true
			}
			for _, sec := range secrets {
				if strings.Contains(decoded, sec) {
					return true
				}
			}
		}
	}
	return false
}

func multiURLDecode(text string, passes int) string {
	cur := text
	for i := 0; i < passes; i++ {
		next := percentEscPattern.ReplaceAllStringFunc(cur, func(esc string) string {
			u, err := url.PathUnescape(esc)
			if err != nil {
				return esc
			}
			return u
		})
		if next == cur {
			break
		}
		cur = next
	}
	return cur
}

func decodeJSONEscapes(text string) string {
	return unicodeEscPattern.ReplaceAllStringFunc(text, func(tok string) string {
		var s string
		if err := json.Unmarshal([]byte(`"`+tok+`"`), &s); err == nil {
			return s
		}
		return tok
	})
}

func dedupLeakFindings(in []LeakFinding) []LeakFinding {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]LeakFinding, 0, len(in))
	for _, f := range in {
		k := fmt.Sprintf("%s|%s|%d", f.Source, f.Variant, f.Offset)
		if !seen[k] {
			seen[k] = true
			out = append(out, f)
		}
	}
	return out
}

var errLoopbackOnlyBlocked = errors.New("testharness: outbound socket dial blocked outside loopback 127.0.0.1/::1")

// loopbackOnlyDialControl guarantees zero packets ever leave host loopback even if
// a malicious adapter or redirect attempts to connect externally during `plugin test`.
func loopbackOnlyDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: invalid address %q", errLoopbackOnlyBlocked, address)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: non-IP literal target %q", errLoopbackOnlyBlocked, host)
	}
	if !addr.IsLoopback() {
		return fmt.Errorf("%w: refused non-loopback IP %s", errLoopbackOnlyBlocked, addr.String())
	}
	return nil
}

// newHermeticLoopbackClient builds an HTTP client whose Dialer Control hook permits
// strictly loopback (127.0.0.1 / ::1) sockets and rejects HTTP redirects.
func newHermeticLoopbackClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: loopbackOnlyDialControl,
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		DisableKeepAlives: true,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("testharness: redirects forbidden in hermetic replay")
		},
	}
}
