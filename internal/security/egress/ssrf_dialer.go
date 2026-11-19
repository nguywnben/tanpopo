// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package egress implements Tanpopo's connect-time anti-SSRF / anti-DNS-rebinding
// dialer (SSRFGuardDialer), redirect sanitizing/validation guard, and canonical
// NewGuardedHTTPClient factory (TP-IMP-0010, ADR-0002, CT-14, CT-15, CT-26).
//
// DAG Boundary (Tier 3): Imports Go standard library only. Never references
// http.DefaultClient, http.DefaultTransport, or net.DefaultResolver.
package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Canonical egress rejection errors returned prior to socket creation or upon redirect violation.
var (
	// ErrInvalidPolicy indicates a malformed SSRF egress Policy or target specification.
	ErrInvalidPolicy = errors.New("egress: invalid policy or target URL")
	// ErrHostDenied indicates target host or port is absent from Manifest/Connection allowed_hosts (403 egress_host_denied).
	ErrHostDenied = errors.New("egress: host or port denied by allowlist")
	// ErrPrivateIPBlocked indicates resolved IP falls in a denied private/loopback/CGNAT/reserved range.
	ErrPrivateIPBlocked = errors.New("egress: private or non-global IP blocked")
	// ErrMetadataBlocked indicates an attempt to reach link-local or cloud metadata endpoints (169.254.0.0/16, fd00:ec2::254).
	ErrMetadataBlocked = errors.New("egress: cloud metadata endpoint blocked")
	// ErrPortDenied indicates an unapproved TCP port was requested on a host.
	ErrPortDenied = errors.New("egress: port denied by policy")
	// ErrTLSRequired indicates plain HTTP was attempted when Policy.RequireTLS is enabled.
	ErrTLSRequired = errors.New("egress: https TLS required")
	// ErrRedirectBlocked indicates a 301/302/303/307/308 redirect failed origin/host/SSRF policy checks (CT-15).
	ErrRedirectBlocked = errors.New("egress: redirect blocked by security policy")
)

// Error represents a structured fail-closed egress denial carrying canonical HTTP status and error code.
type Error struct {
	HTTPStatus int
	Code       string
	Message    string
	Err        error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("%s (%d): %s", e.Code, e.HTTPStatus, e.Message)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s (%d): %v", e.Code, e.HTTPStatus, e.Err)
	}
	return fmt.Sprintf("%s (%d)", e.Code, e.HTTPStatus)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// HostResolver abstracts DNS resolution so DNS rebinding (CT-26) can be tested hermetically.
type HostResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type stdHostResolver struct {
	resolver *net.Resolver
}

func (r stdHostResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return r.resolver.LookupNetIP(ctx, network, host)
}

// NewStdHostResolver constructs an explicit non-default net.Resolver HostResolver.
func NewStdHostResolver() HostResolver {
	return stdHostResolver{resolver: &net.Resolver{PreferGo: true}}
}

var (
	// Unconditionally blocked prefixes (NEVER permitted even when AllowPrivateIP == true).
	unconditionalMetadataPrefixes = []netip.Prefix{
		netip.MustParsePrefix("169.254.0.0/16"),     // IPv4 link-local & AWS/GCP/Azure IMDS
		netip.MustParsePrefix("100.100.100.200/32"), // Alibaba Cloud metadata endpoint
		netip.MustParsePrefix("0.0.0.0/8"),          // RFC 1122 unspecified / local host routing alias
		netip.MustParsePrefix("::/128"),             // IPv6 unspecified
		netip.MustParsePrefix("fe80::/10"),          // IPv6 link-local
		netip.MustParsePrefix("fd00:ec2::254/128"),  // AWS EC2 IPv6 metadata endpoint
		netip.MustParsePrefix("224.0.0.0/4"),        // IPv4 multicast
		netip.MustParsePrefix("ff00::/8"),           // IPv6 multicast
	}

	// Non-public IP ranges blocked unless AllowPrivateIP == true AND matched by narrowed AllowedCIDRs.
	privateAndSpecialPrefixes = []netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"),     // IPv4 loopback
		netip.MustParsePrefix("10.0.0.0/8"),      // RFC 1918
		netip.MustParsePrefix("172.16.0.0/12"),   // RFC 1918
		netip.MustParsePrefix("192.168.0.0/16"),  // RFC 1918
		netip.MustParsePrefix("100.64.0.0/10"),   // RFC 6598 CGNAT
		netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
		netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
		netip.MustParsePrefix("198.18.0.0/15"),   // Network benchmark testing
		netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
		netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
		netip.MustParsePrefix("240.0.0.0/4"),     // Reserved / 255.255.255.255/32
		netip.MustParsePrefix("::/96"),           // Deprecated IPv4-compatible IPv6 (e.g. ::127.0.0.1, ::169.254.169.254)
		netip.MustParsePrefix("::1/128"),         // IPv6 loopback
		netip.MustParsePrefix("::ffff:0:0:0/96"), // SIIT IPv4-translated addresses
		netip.MustParsePrefix("fc00::/7"),        // IPv6 ULA
		netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 well-known prefix
		netip.MustParsePrefix("64:ff9b:1::/48"),  // NAT64 local-use prefix
		netip.MustParsePrefix("100::/64"),        // Discard-only prefix
		netip.MustParsePrefix("2001::/32"),       // Teredo
		netip.MustParsePrefix("2001:2::/48"),     // IPv6 Benchmarking (RFC 5180 / 6890)
		netip.MustParsePrefix("2001:10::/28"),    // Deprecated ORCHID (RFC 4843)
		netip.MustParsePrefix("2001:20::/28"),    // ORCHIDv2 (RFC 7343)
		netip.MustParsePrefix("2001:db8::/32"),   // Documentation prefix
		netip.MustParsePrefix("2002::/16"),       // 6to4
		netip.MustParsePrefix("3fff::/20"),       // IPv6 Documentation (RFC 9637)
		netip.MustParsePrefix("fec0::/10"),       // Deprecated site-local
	}

	// Transition prefixes embedding an IPv4 address whose embedded target must be checked
	// against unconditionalMetadataPrefixes even when AllowPrivateIP == true.
	ipv4CompatTransitionPrefix = netip.MustParsePrefix("::/96")
	siitTransitionPrefix       = netip.MustParsePrefix("::ffff:0:0:0/96")
	nat64WellKnownPrefix       = netip.MustParsePrefix("64:ff9b::/96")
	nat64LocalPrefix           = netip.MustParsePrefix("64:ff9b:1::/48")
	sixToFourPrefix            = netip.MustParsePrefix("2002::/16")
)

// Policy defines default-deny outbound allowlisting and connect-time IP constraints.
type Policy struct {
	AllowedHosts     []string
	AllowPrivateIP   bool
	AllowedCIDRs     []string
	AllowedPorts     []int
	RequireTLS       bool
	SensitiveHeaders []string
}

type parsedHostEntry struct {
	host    string
	hasPort bool
	port    int
}

type compiledPolicy struct {
	raw              Policy
	hosts            []parsedHostEntry
	allowedCIDRs     []netip.Prefix
	allowedPorts     map[int]bool
	sensitiveHeaders map[string]bool
}

func compilePolicy(p Policy) (*compiledPolicy, error) {
	if len(p.AllowedHosts) == 0 {
		return nil, &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    "at least one allowed_hosts entry is required (default-deny)",
			Err:        ErrHostDenied,
		}
	}

	cp := &compiledPolicy{
		raw:              p,
		hosts:            make([]parsedHostEntry, 0, len(p.AllowedHosts)),
		allowedPorts:     make(map[int]bool, len(p.AllowedPorts)),
		sensitiveHeaders: make(map[string]bool),
	}

	for _, rawHost := range p.AllowedHosts {
		entry, err := parseAllowedHostEntry(rawHost)
		if err != nil {
			return nil, err
		}
		cp.hosts = append(cp.hosts, entry)
	}

	for _, port := range p.AllowedPorts {
		if port < 1 || port > 65535 {
			return nil, &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "egress_invalid_policy",
				Message:    fmt.Sprintf("invalid allowed port %d", port),
				Err:        ErrInvalidPolicy,
			}
		}
		cp.allowedPorts[port] = true
	}

	if p.AllowPrivateIP {
		if len(p.AllowedCIDRs) == 0 {
			return nil, &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "egress_invalid_policy",
				Message:    "allow_private_ip=true requires explicit non-empty allowed_cidrs narrowing",
				Err:        ErrInvalidPolicy,
			}
		}
		for _, cidr := range p.AllowedCIDRs {
			pfx, err := netip.ParsePrefix(strings.TrimSpace(cidr))
			if err != nil {
				return nil, &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "egress_invalid_policy",
					Message:    fmt.Sprintf("invalid allowed_cidrs entry %q: %v", cidr, err),
					Err:        ErrInvalidPolicy,
				}
			}
			pfx = pfx.Masked()
			// Forbid wildcard 0.0.0.0/0 or ::/0 overrides in AllowedCIDRs.
			if pfx.Bits() == 0 {
				return nil, &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "egress_invalid_policy",
					Message:    "unrestricted 0.0.0.0/0 or ::/0 CIDR is forbidden in allowed_cidrs",
					Err:        ErrInvalidPolicy,
				}
			}
			cp.allowedCIDRs = append(cp.allowedCIDRs, pfx)
		}
	}

	for _, h := range defaultSensitiveHeaderNames {
		cp.sensitiveHeaders[h] = true
	}
	for _, h := range p.SensitiveHeaders {
		trimmed := strings.ToLower(strings.TrimSpace(h))
		if trimmed != "" {
			cp.sensitiveHeaders[trimmed] = true
		}
	}

	return cp, nil
}

func parseAllowedHostEntry(raw string) (parsedHostEntry, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.ContainsAny(trimmed, "*?/@\\ \t\r\n\x00") {
		return parsedHostEntry{}, &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       "egress_invalid_policy",
			Message:    fmt.Sprintf("invalid allowed_hosts entry %q", raw),
			Err:        ErrInvalidPolicy,
		}
	}

	// Handle IPv6 brackets without port, e.g. "[::1]".
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		ip, err := netip.ParseAddr(inner)
		if err != nil || ip.Zone() != "" {
			return parsedHostEntry{}, &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "egress_invalid_policy",
				Message:    fmt.Sprintf("invalid IPv6 allowed_hosts entry %q", raw),
				Err:        ErrInvalidPolicy,
			}
		}
		return parsedHostEntry{host: ip.String()}, nil
	}

	if host, portStr, err := net.SplitHostPort(trimmed); err == nil {
		port, perr := strconv.Atoi(portStr)
		if perr != nil || port < 1 || port > 65535 {
			return parsedHostEntry{}, &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "egress_invalid_policy",
				Message:    fmt.Sprintf("invalid port in allowed_hosts entry %q", raw),
				Err:        ErrInvalidPolicy,
			}
		}
		normHost, herr := normalizeHostForMatch(host)
		if herr != nil {
			return parsedHostEntry{}, herr
		}
		return parsedHostEntry{host: normHost, hasPort: true, port: port}, nil
	}

	normHost, herr := normalizeHostForMatch(trimmed)
	if herr != nil {
		return parsedHostEntry{}, herr
	}
	return parsedHostEntry{host: normHost}, nil
}

func normalizeHostForMatch(rawHost string) (string, error) {
	h := strings.TrimSpace(rawHost)
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	if h == "" || strings.HasSuffix(h, ".") || strings.ContainsAny(h, "%* \t\r\n\x00/\\@?") {
		return "", &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("invalid target host %q", rawHost),
			Err:        ErrHostDenied,
		}
	}
	if ip, err := netip.ParseAddr(h); err == nil {
		if ip.Zone() != "" {
			return "", &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_host_denied",
				Message:    "zone-scoped IPv6 hosts are forbidden",
				Err:        ErrHostDenied,
			}
		}
		return ip.String(), nil
	}
	return strings.ToLower(h), nil
}

// HostMatches checks whether targetURL's scheme, hostname, and effective port are permitted
// by allowedHosts (and optional allowedPorts when entries omit explicit ports).
func HostMatches(targetURL *url.URL, allowedHosts []string, allowedPorts ...int) bool {
	cp, err := compilePolicy(Policy{
		AllowedHosts: allowedHosts,
		AllowedPorts: allowedPorts,
	})
	if err != nil {
		return false
	}
	return cp.validateRequestURL(targetURL) == nil
}

func (cp *compiledPolicy) validateRequestURL(u *url.URL) error {
	if u == nil {
		return &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       "egress_invalid_policy",
			Message:    "nil target URL",
			Err:        ErrInvalidPolicy,
		}
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "https" && scheme != "http" {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("unsupported URL scheme %q", u.Scheme),
			Err:        ErrHostDenied,
		}
	}
	if cp.raw.RequireTLS && scheme != "https" {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_tls_required",
			Message:    "https scheme is required by connection egress policy",
			Err:        ErrTLSRequired,
		}
	}
	if u.User != nil || strings.ContainsAny(u.Host, "\\@ \t\r\n\x00") {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    "ambiguous URL authority/userinfo is forbidden",
			Err:        ErrHostDenied,
		}
	}
	host := u.Hostname()
	normHost, err := normalizeHostForMatch(host)
	if err != nil {
		return err
	}
	port, perr := effectivePort(u, scheme)
	if perr != nil {
		return perr
	}
	return cp.matchHostAndPort(normHost, port, scheme)
}

func effectivePort(u *url.URL, scheme string) (int, error) {
	if pStr := u.Port(); pStr != "" {
		p, err := strconv.Atoi(pStr)
		if err != nil || p < 1 || p > 65535 {
			return 0, &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_host_denied",
				Message:    fmt.Sprintf("invalid URL port %q", pStr),
				Err:        ErrHostDenied,
			}
		}
		return p, nil
	}
	if scheme == "https" {
		return 443, nil
	}
	return 80, nil
}

func (cp *compiledPolicy) matchHostAndPort(normHost string, port int, scheme string) error {
	hostMatched := false
	for _, entry := range cp.hosts {
		if entry.host != normHost {
			continue
		}
		hostMatched = true
		if entry.hasPort {
			if entry.port == port {
				return nil
			}
			continue
		}
		if len(cp.allowedPorts) > 0 {
			if cp.allowedPorts[port] {
				return nil
			}
			continue
		}
		// Entries without explicit port default strictly to standard protocol port (443 / 80)
		// to block port scanning (e.g. 10.20.0.5:6379) against allowed hosts.
		switch {
		case scheme == "https" && port == 443:
			return nil
		case scheme == "http" && port == 80:
			return nil
		case scheme == "" && (port == 443 || port == 80):
			return nil
		}
	}

	if hostMatched {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("port %d on host %q is not permitted by allowlist", port, normHost),
			Err:        errors.Join(ErrHostDenied, ErrPortDenied),
		}
	}
	return &Error{
		HTTPStatus: http.StatusForbidden,
		Code:       "egress_host_denied",
		Message:    fmt.Sprintf("host %q is not permitted by allowed_hosts", normHost),
		Err:        ErrHostDenied,
	}
}

// ValidateTargetURL validates rawURL syntactically and against Policy allowlists prior to DNS lookup.
func (p Policy) ValidateTargetURL(rawURL string) (*url.URL, error) {
	cp, err := compilePolicy(p)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       "egress_invalid_policy",
			Message:    fmt.Sprintf("malformed URL: %v", err),
			Err:        ErrInvalidPolicy,
		}
	}
	if err := cp.validateRequestURL(u); err != nil {
		return nil, err
	}
	return u, nil
}

// DialerConfig configures SSRFGuardDialer.
type DialerConfig struct {
	Policy      Policy
	DialTimeout time.Duration
	KeepAlive   time.Duration
	Resolver    HostResolver
	// ControlHook adds a secondary connect-time restriction (e.g. netguard.Control in hermetic tests).
	ControlHook func(network, address string, c syscall.RawConn) error
}

// SSRFGuardDialer validates host allowlists pre-DNS and inspects resolved IPs at
// net.Dialer.Control socket-connect time to defeat SSRF, DNS rebinding, and IPv4-mapped IPv6 bypasses.
type SSRFGuardDialer struct {
	policy      *compiledPolicy
	dialTimeout time.Duration
	keepAlive   time.Duration
	resolver    HostResolver
	controlHook func(network, address string, c syscall.RawConn) error
}

// NewDialer constructs an SSRFGuardDialer enforcing cfg.Policy.
func NewDialer(cfg DialerConfig) (*SSRFGuardDialer, error) {
	cp, err := compilePolicy(cfg.Policy)
	if err != nil {
		return nil, err
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	keepAlive := cfg.KeepAlive
	if keepAlive <= 0 {
		keepAlive = 30 * time.Second
	}
	res := cfg.Resolver
	if res == nil {
		res = stdHostResolver{resolver: &net.Resolver{}}
	}
	return &SSRFGuardDialer{
		policy:      cp,
		dialTimeout: dialTimeout,
		keepAlive:   keepAlive,
		resolver:    res,
		controlHook: cfg.ControlHook,
	}, nil
}

// Control is the net.Dialer.Control hook executed immediately prior to kernel connect(2).
func (d *SSRFGuardDialer) Control(network, address string, c syscall.RawConn) error {
	if !isAllowedTCPNetwork(network) {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("non-TCP dial network %q forbidden", network),
			Err:        ErrHostDenied,
		}
	}
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("invalid socket address %q", address),
			Err:        ErrHostDenied,
		}
	}
	port, perr := strconv.Atoi(portStr)
	if perr != nil || port < 1 || port > 65535 {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_port_denied",
			Message:    fmt.Sprintf("invalid target port %q", portStr),
			Err:        errors.Join(ErrHostDenied, ErrPortDenied),
		}
	}
	if strings.ContainsRune(host, '%') {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_metadata_blocked",
			Message:    "zone-scoped IPv6 socket target forbidden",
			Err:        ErrMetadataBlocked,
		}
	}
	ip, parseErr := netip.ParseAddr(host)
	if parseErr != nil {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_private_ip_blocked",
			Message:    fmt.Sprintf("non-numeric address %q in socket Control", host),
			Err:        ErrPrivateIPBlocked,
		}
	}
	if err := d.checkConnectIP(ip, port); err != nil {
		return err
	}
	if d.controlHook != nil {
		if herr := d.controlHook(network, address, c); herr != nil {
			return herr
		}
	}
	// Strictly narrow in hermetic test environment so zero external TCP SYNs can escape.
	if os.Getenv("TANPOPO_TEST_HERMETIC") == "1" {
		unmapped := ip.Unmap()
		if !unmapped.IsLoopback() {
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_hermetic_blocked",
				Message:    fmt.Sprintf("non-loopback IP %s blocked under TANPOPO_TEST_HERMETIC=1", unmapped),
				Err:        ErrPrivateIPBlocked,
			}
		}
	}
	return nil
}

func extractTransitionEmbeddedIPv4(ip netip.Addr) (netip.Addr, bool) {
	if !ip.Is6() {
		return netip.Addr{}, false
	}
	b := ip.As16()
	switch {
	case ipv4CompatTransitionPrefix.Contains(ip):
		// Exclude ::/128 (unspecified) and ::1/128 (IPv6 loopback), which share ::/96 numerically
		// but represent native IPv6 addresses rather than IPv4-compatible tunnels.
		if ip.IsLoopback() || ip.IsUnspecified() {
			return netip.Addr{}, false
		}
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case siitTransitionPrefix.Contains(ip),
		nat64WellKnownPrefix.Contains(ip),
		nat64LocalPrefix.Contains(ip):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case sixToFourPrefix.Contains(ip):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	default:
		return netip.Addr{}, false
	}
}

// ClassifyStaticEndpointIP enforces Tanpopo's full IPv4, IPv6, IPv4-mapped IPv6,
// zone-ID, transition-prefix (::/96, 64:ff9b::/96, 2002::/16, fec0::/10), and
// metadata/link-local SSRF rules without requiring a full dialer Policy.
func ClassifyStaticEndpointIP(ip netip.Addr, allowPrivateIP bool) error {
	if !ip.IsValid() || ip.Zone() != "" {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_metadata_blocked",
			Message:    "invalid or zone-scoped IP address",
			Err:        ErrMetadataBlocked,
		}
	}
	unmapped := ip.Unmap()
	embeddedV4, hasEmbeddedV4 := extractTransitionEmbeddedIPv4(ip)
	for _, pfx := range unconditionalMetadataPrefixes {
		if pfx.Contains(ip) || pfx.Contains(unmapped) || (hasEmbeddedV4 && pfx.Contains(embeddedV4)) {
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_metadata_blocked",
				Message:    fmt.Sprintf("address %s matches unconditionally blocked metadata/link-local range %s", ip, pfx),
				Err:        ErrMetadataBlocked,
			}
		}
	}
	if ip.Is4In6() {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_private_ip_blocked",
			Message:    fmt.Sprintf("IPv4-mapped IPv6 address %s is forbidden", ip),
			Err:        ErrPrivateIPBlocked,
		}
	}

	isPrivateOrSpecial := !unmapped.IsGlobalUnicast() || unmapped.IsPrivate() || unmapped.IsLoopback() ||
		unmapped.IsLinkLocalUnicast() || unmapped.IsLinkLocalMulticast() || unmapped.IsMulticast() ||
		unmapped.IsUnspecified() || unmapped.IsInterfaceLocalMulticast()
	if !isPrivateOrSpecial {
		for _, pfx := range privateAndSpecialPrefixes {
			if pfx.Contains(unmapped) || pfx.Contains(ip) {
				isPrivateOrSpecial = true
				break
			}
		}
	}
	if isPrivateOrSpecial && !allowPrivateIP {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_private_ip_blocked",
			Message:    fmt.Sprintf("non-public IP %s blocked because allow_private_ip=false", unmapped),
			Err:        ErrPrivateIPBlocked,
		}
	}
	return nil
}

// ValidateStaticEndpointURL checks Control Plane endpoint URLs for scheme/authority hygiene,
// blocked local ports, reserved metadata/localhost names (with trailing dot normalization),
// IP literal classification, and fail-closed HostResolver DNS verification when resolver != nil.
func ValidateStaticEndpointURL(ctx context.Context, rawURL string, allowPrivateIP bool, resolver HostResolver, blockedPorts map[int]bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return &Error{
			HTTPStatus: http.StatusUnprocessableEntity,
			Code:       "invalid_endpoint_url",
			Message:    "valid http or https endpoint_url required",
			Err:        ErrInvalidPolicy,
		}
	}
	if strings.ContainsAny(parsed.Host, "\\@ \t\r\n\x00") {
		return &Error{
			HTTPStatus: http.StatusUnprocessableEntity,
			Code:       "invalid_endpoint_url",
			Message:    "ambiguous endpoint_url authority is forbidden",
			Err:        ErrInvalidPolicy,
		}
	}

	portNum := 443
	if parsed.Scheme == "http" {
		portNum = 80
	}
	if p := parsed.Port(); p != "" {
		pv, perr := strconv.Atoi(p)
		if perr != nil || pv < 1 || pv > 65535 {
			return &Error{
				HTTPStatus: http.StatusUnprocessableEntity,
				Code:       "invalid_endpoint_url",
				Message:    "invalid endpoint port",
				Err:        ErrInvalidPolicy,
			}
		}
		portNum = pv
	}
	if blockedPorts[portNum] {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_port_denied",
			Message:    fmt.Sprintf("port %d denied by blocked control-plane ports policy", portNum),
			Err:        ErrPortDenied,
		}
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if ip, perr := netip.ParseAddr(host); perr == nil {
		return ClassifyStaticEndpointIP(ip, allowPrivateIP)
	}
	canonicalHost := strings.TrimRight(host, ".")
	if canonicalHost == "" {
		return &Error{
			HTTPStatus: http.StatusUnprocessableEntity,
			Code:       "invalid_endpoint_url",
			Message:    "endpoint_url hostname cannot be empty",
			Err:        ErrInvalidPolicy,
		}
	}
	if canonicalHost == "metadata.google.internal" || canonicalHost == "metadata.goog" || canonicalHost == "instance-data" {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_metadata_blocked",
			Message:    fmt.Sprintf("metadata hostname %q is unconditionally blocked", canonicalHost),
			Err:        ErrMetadataBlocked,
		}
	}
	if canonicalHost == "localhost" || strings.HasSuffix(canonicalHost, ".localhost") {
		if !allowPrivateIP {
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_private_ip_blocked",
				Message:    fmt.Sprintf("localhost endpoint %q blocked because allow_private_ip=false", canonicalHost),
				Err:        ErrPrivateIPBlocked,
			}
		}
		return nil
	}

	if resolver != nil {
		addrs, lerr := resolver.LookupNetIP(ctx, "ip", canonicalHost)
		if lerr != nil || len(addrs) == 0 {
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "ssrf_blocked_private_ip",
				Message:    fmt.Sprintf("DNS resolution failed for endpoint host %q", canonicalHost),
				Err:        errors.Join(ErrPrivateIPBlocked, lerr),
			}
		}
		for _, ip := range addrs {
			if cerr := ClassifyStaticEndpointIP(ip, allowPrivateIP); cerr != nil {
				return cerr
			}
		}
	}
	return nil
}

func (d *SSRFGuardDialer) checkConnectIP(ip netip.Addr, port int) error {
	if !ip.IsValid() || ip.Zone() != "" {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_metadata_blocked",
			Message:    "invalid or zone-scoped IP address",
			Err:        ErrMetadataBlocked,
		}
	}
	// Reject explicit IPv4-mapped IPv6 sockets (e.g. ::ffff:127.0.0.1 / ::ffff:169.254.169.254)
	// and check embedded IPv4 addresses across IPv6 transition prefixes against metadata ranges.
	unmapped := ip.Unmap()
	embeddedV4, hasEmbeddedV4 := extractTransitionEmbeddedIPv4(ip)
	for _, pfx := range unconditionalMetadataPrefixes {
		if pfx.Contains(ip) || pfx.Contains(unmapped) || (hasEmbeddedV4 && pfx.Contains(embeddedV4)) {
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_metadata_blocked",
				Message:    fmt.Sprintf("address %s matches unconditionally blocked metadata/link-local range %s", ip, pfx),
				Err:        ErrMetadataBlocked,
			}
		}
	}
	if ip.Is4In6() {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_private_ip_blocked",
			Message:    fmt.Sprintf("IPv4-mapped IPv6 address %s is forbidden", ip),
			Err:        ErrPrivateIPBlocked,
		}
	}

	isPrivateOrSpecial := !unmapped.IsGlobalUnicast() || unmapped.IsPrivate() || unmapped.IsLoopback() ||
		unmapped.IsLinkLocalUnicast() || unmapped.IsLinkLocalMulticast() || unmapped.IsMulticast() ||
		unmapped.IsUnspecified() || unmapped.IsInterfaceLocalMulticast()
	if !isPrivateOrSpecial {
		for _, pfx := range privateAndSpecialPrefixes {
			if pfx.Contains(unmapped) {
				isPrivateOrSpecial = true
				break
			}
		}
	}

	if isPrivateOrSpecial {
		if !d.policy.raw.AllowPrivateIP {
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_private_ip_blocked",
				Message:    fmt.Sprintf("non-public IP %s blocked because allow_private_ip=false", unmapped),
				Err:        ErrPrivateIPBlocked,
			}
		}
		matchedCIDR := false
		for _, allowed := range d.policy.allowedCIDRs {
			if allowed.Contains(unmapped) {
				matchedCIDR = true
				break
			}
		}
		if !matchedCIDR {
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_private_ip_blocked",
				Message:    fmt.Sprintf("private IP %s is outside configured allowed_cidrs", unmapped),
				Err:        ErrPrivateIPBlocked,
			}
		}
	}

	// Ensure if AllowedPorts was configured on the policy, connect port is allowed too.
	if len(d.policy.allowedPorts) > 0 && !d.policy.allowedPorts[port] {
		// Explicit host:port entries can still authorize a port directly; matchHostAndPort verifies both.
		explicitPortOK := false
		for _, entry := range d.policy.hosts {
			if entry.hasPort && entry.port == port {
				explicitPortOK = true
				break
			}
		}
		if !explicitPortOK {
			return &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_port_denied",
				Message:    fmt.Sprintf("port %d denied by allowed_ports", port),
				Err:        errors.Join(ErrHostDenied, ErrPortDenied),
			}
		}
	}
	return nil
}

// CheckAddr validates a host:port string carrying a numeric IP against connect-time SSRF rules.
func (d *SSRFGuardDialer) CheckAddr(addr string) error {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("invalid address %q", addr),
			Err:        ErrHostDenied,
		}
	}
	port, perr := strconv.Atoi(portStr)
	if perr != nil || port < 1 || port > 65535 {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_port_denied",
			Message:    fmt.Sprintf("invalid port %q", portStr),
			Err:        errors.Join(ErrHostDenied, ErrPortDenied),
		}
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_private_ip_blocked",
			Message:    fmt.Sprintf("expected numeric IP in CheckAddr, got %q", host),
			Err:        ErrPrivateIPBlocked,
		}
	}
	return d.checkConnectIP(ip, port)
}

// DialContext resolves host (if non-IP), validates EVERY returned IP before dialing,
// pins the validated IP to avoid DNS-rebinding TOCTOU, and attaches Control at socket connect.
func (d *SSRFGuardDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if !isAllowedTCPNetwork(network) {
		return nil, &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("network %q forbidden", network),
			Err:        ErrHostDenied,
		}
	}
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return nil, &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("invalid dial address %q", addr),
			Err:        ErrHostDenied,
		}
	}
	normHost, err := normalizeHostForMatch(host)
	if err != nil {
		return nil, err
	}
	port, perr := strconv.Atoi(portStr)
	if perr != nil || port < 1 || port > 65535 {
		return nil, &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_port_denied",
			Message:    fmt.Sprintf("invalid dial port %q", portStr),
			Err:        errors.Join(ErrHostDenied, ErrPortDenied),
		}
	}
	// Pre-DNS allowlist check: unapproved domains are blocked before invoking HostResolver (CT-14 / NEG-1).
	schemeHint := ""
	if d.policy.raw.RequireTLS {
		schemeHint = "https"
	}
	if err := d.policy.matchHostAndPort(normHost, port, schemeHint); err != nil {
		return nil, err
	}

	sysDialer := &net.Dialer{
		Timeout:   d.dialTimeout,
		KeepAlive: d.keepAlive,
		Control:   d.Control,
	}

	if literalIP, err := netip.ParseAddr(normHost); err == nil {
		if err := d.checkConnectIP(literalIP, port); err != nil {
			return nil, err
		}
		return sysDialer.DialContext(ctx, network, net.JoinHostPort(literalIP.String(), portStr))
	}

	lookupNet := "ip"
	switch network {
	case "tcp4":
		lookupNet = "ip4"
	case "tcp6":
		lookupNet = "ip6"
	}
	addrs, err := d.resolver.LookupNetIP(ctx, lookupNet, normHost)
	if err != nil {
		return nil, fmt.Errorf("egress: resolve %q: %w", normHost, err)
	}
	if len(addrs) == 0 {
		return nil, &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("empty DNS resolution for %q", normHost),
			Err:        ErrHostDenied,
		}
	}

	candidates := make([]netip.Addr, 0, len(addrs))
	for _, resolved := range addrs {
		if cerr := d.checkConnectIP(resolved, port); cerr != nil {
			// Any metadata endpoint or mixed public+private DNS answer aborts fail-closed.
			if errors.Is(cerr, ErrMetadataBlocked) || !d.policy.raw.AllowPrivateIP {
				return nil, cerr
			}
			continue
		}
		candidates = append(candidates, resolved)
	}
	if len(candidates) == 0 {
		return nil, d.checkConnectIP(addrs[0], port)
	}

	var lastErr error
	for _, targetIP := range candidates {
		targetAddr := net.JoinHostPort(targetIP.String(), portStr)
		conn, derr := sysDialer.DialContext(ctx, network, targetAddr)
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	return nil, lastErr
}

func isAllowedTCPNetwork(network string) bool {
	return network == "tcp" || network == "tcp4" || network == "tcp6"
}

// ClientConfig configures egress.NewGuardedHTTPClient.
type ClientConfig struct {
	Policy                   Policy
	Timeout                  time.Duration
	DisableTotalTimeout      bool
	DisableKeepAlives        bool
	ResponseHeaderTimeout    time.Duration
	DialTimeout              time.Duration
	TLSHandshakeTimeout      time.Duration
	MaxRedirects             int
	DenyCrossOriginRedirects bool
	Resolver                 HostResolver
	// RootCAs optionally supplies an explicit TLS trust pool. When nil, system roots are used.
	RootCAs     *x509.CertPool
	ControlHook func(network, address string, c syscall.RawConn) error
}

type policyCheckingRoundTripper struct {
	policy *compiledPolicy
	base   http.RoundTripper
}

func (rt *policyCheckingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       "egress_invalid_policy",
			Message:    "HTTP request URL is required",
			Err:        ErrInvalidPolicy,
		}
	}
	// Re-enforce scheme, RequireTLS, authority hygiene, and AllowedHosts BEFORE transport DNS/dial.
	if err := rt.policy.validateRequestURL(req.URL); err != nil {
		return nil, err
	}
	return rt.base.RoundTrip(req)
}

// NewGuardedHTTPClient is the canonical Tanpopo factory for constructing outbound HTTP clients
// wired with SSRFGuardDialer, pre-DNS policy enforcement, TLS >= 1.2, disabled env proxies,
// and CheckRedirect protection (TP-IMP-0010 / scripts/ci/check.go).
func NewGuardedHTTPClient(cfg ClientConfig) (*http.Client, error) {
	dialer, err := NewDialer(DialerConfig{
		Policy:      cfg.Policy,
		DialTimeout: cfg.DialTimeout,
		Resolver:    cfg.Resolver,
		ControlHook: cfg.ControlHook,
	})
	if err != nil {
		return nil, err
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	clientTimeout := timeout
	respHeaderTimeout := cfg.ResponseHeaderTimeout
	if cfg.DisableTotalTimeout {
		clientTimeout = 0
		if respHeaderTimeout <= 0 {
			respHeaderTimeout = timeout
		}
	}
	tlsTimeout := cfg.TLSHandshakeTimeout
	if tlsTimeout <= 0 {
		tlsTimeout = 10 * time.Second
	}

	// Never configure http.ProxyFromEnvironment so ambient HTTP_PROXY variables cannot bypass SSRFGuard.
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		DisableKeepAlives:     cfg.DisableKeepAlives,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: respHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    cfg.RootCAs,
		},
	}

	redirectGuard := newCompiledRedirectGuard(dialer, RedirectConfig{
		Policy:                   cfg.Policy,
		MaxRedirects:             cfg.MaxRedirects,
		DenyCrossOriginRedirects: cfg.DenyCrossOriginRedirects,
	})

	return &http.Client{
		Timeout: clientTimeout,
		Transport: &policyCheckingRoundTripper{
			policy: dialer.policy,
			base:   tr,
		},
		CheckRedirect: redirectGuard,
	}, nil
}
