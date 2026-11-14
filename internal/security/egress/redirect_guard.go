// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package egress

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

const defaultMaxRedirects = 5

var defaultSensitiveHeaderNames = []string{
	"authorization",
	"proxy-authorization",
	"cookie",
	"set-cookie",
	"x-api-key",
	"x-goog-api-key",
	"x-auth-token",
	"api-key",
}

// RedirectConfig configures CheckRedirect security validation on outbound HTTP hops.
type RedirectConfig struct {
	Policy                   Policy
	MaxRedirects             int // 0 => default (5); < 0 => forbid all redirects
	DenyCrossOriginRedirects bool
	ExtraSensitiveHeaders    []string
}

// NewRedirectGuard returns an http.Client.CheckRedirect callback that validates
// every 301/302/303/307/308 hop against Policy.AllowedHosts + SSRFGuardDialer rules
// and strips 100% of authentication headers on cross-origin redirects (CT-15).
func NewRedirectGuard(cfg RedirectConfig) (func(req *http.Request, via []*http.Request) error, error) {
	dialer, err := NewDialer(DialerConfig{Policy: cfg.Policy})
	if err != nil {
		return nil, err
	}
	return newCompiledRedirectGuard(dialer, cfg), nil
}

func newCompiledRedirectGuard(dialer *SSRFGuardDialer, cfg RedirectConfig) func(req *http.Request, via []*http.Request) error {
	maxHops := cfg.MaxRedirects
	if maxHops == 0 {
		maxHops = defaultMaxRedirects
	}

	stripSet := make(map[string]bool)
	for k := range dialer.policy.sensitiveHeaders {
		stripSet[k] = true
	}
	for _, h := range cfg.ExtraSensitiveHeaders {
		trimmed := strings.ToLower(strings.TrimSpace(h))
		if trimmed != "" {
			stripSet[trimmed] = true
		}
	}

	return func(req *http.Request, via []*http.Request) error {
		if req == nil || req.URL == nil {
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_redirect_blocked",
				Message:    "redirect missing target URL",
				Err:        ErrRedirectBlocked,
			}
		}

		// Strip sensitive credential headers immediately on any cross-origin redirect
		// relative to both the initial origin and the immediate previous hop.
		isCrossOrigin := false
		var prevScheme string
		if len(via) > 0 {
			initial := via[0]
			prev := via[len(via)-1]
			if prev != nil && prev.URL != nil {
				prevScheme = strings.ToLower(prev.URL.Scheme)
			}
			initialCross := initial != nil && initial.URL != nil && !sameOriginURL(initial.URL, req.URL)
			prevCross := prev != nil && prev.URL != nil && !sameOriginURL(prev.URL, req.URL)
			if initialCross || prevCross {
				isCrossOrigin = true
				stripSensitiveHeaders(req.Header, stripSet)
			}
		}

		if maxHops < 0 || len(via) >= maxHops {
			stripSensitiveHeaders(req.Header, stripSet)
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_redirect_blocked",
				Message:    fmt.Sprintf("redirect hop limit exceeded (%d)", len(via)),
				Err:        ErrRedirectBlocked,
			}
		}

		targetScheme := strings.ToLower(strings.TrimSpace(req.URL.Scheme))
		if prevScheme == "https" && targetScheme != "https" {
			stripSensitiveHeaders(req.Header, stripSet)
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_redirect_blocked",
				Message:    "HTTPS-to-HTTP protocol downgrade redirect forbidden",
				Err:        ErrRedirectBlocked,
			}
		}

		if cfg.DenyCrossOriginRedirects && isCrossOrigin {
			stripSensitiveHeaders(req.Header, stripSet)
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_redirect_blocked",
				Message:    fmt.Sprintf("cross-origin redirect to %q blocked by policy", req.URL.Host),
				Err:        ErrRedirectBlocked,
			}
		}

		// Re-evaluate allowed_hosts, scheme, and port constraints on redirect destination.
		if err := dialer.policy.validateRequestURL(req.URL); err != nil {
			stripSensitiveHeaders(req.Header, stripSet)
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_redirect_blocked",
				Message:    fmt.Sprintf("redirect destination %q denied: %v", req.URL.Host, err),
				Err:        fmt.Errorf("%w: %w", ErrRedirectBlocked, err),
			}
		}

		// Eagerly fail IP-literal redirect targets (e.g. http://169.254.169.254/ or [::ffff:127.0.0.1])
		// before dialer invocation.
		if host := req.URL.Hostname(); host != "" {
			if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
				port, _ := effectivePort(req.URL, targetScheme)
				if cerr := dialer.CheckAddr(net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))); cerr != nil {
					stripSensitiveHeaders(req.Header, stripSet)
					return &Error{
						HTTPStatus: http.StatusForbidden,
						Code:       "egress_redirect_blocked",
						Message:    fmt.Sprintf("redirect target IP %s blocked: %v", ip, cerr),
						Err:        fmt.Errorf("%w: %w", ErrRedirectBlocked, cerr),
					}
				}
			}
		}

		return nil
	}
}

func sameOriginURL(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	sa := strings.ToLower(strings.TrimSpace(a.Scheme))
	sb := strings.ToLower(strings.TrimSpace(b.Scheme))
	if sa == "" || sa != sb {
		return false
	}
	ha := strings.ToLower(strings.TrimSpace(a.Hostname()))
	hb := strings.ToLower(strings.TrimSpace(b.Hostname()))
	if ha == "" || ha != hb {
		return false
	}
	pa, erra := effectivePort(a, sa)
	pb, errb := effectivePort(b, sb)
	if erra != nil || errb != nil {
		return false
	}
	return pa == pb
}

func stripSensitiveHeaders(h http.Header, stripSet map[string]bool) {
	if h == nil {
		return
	}
	for key := range h {
		if stripSet[strings.ToLower(strings.TrimSpace(key))] {
			delete(h, key)
		}
	}
}
