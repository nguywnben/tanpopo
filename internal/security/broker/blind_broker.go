// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package broker implements Tanpopo's BlindHostCredentialBroker (TP-IMP-0010,
// ADR-0002, CR-05, CT-14..16, CT-26, AUTH-16): resolves opaque secret slots via
// CredentialBindingResolver, verifies allowed_hosts and bound_audience prior to
// opening any TCP socket, unseals credentials strictly via vault.UnsealCapability
// with immediate RAM zeroization, performs host-side Bearer/Header/Basic/HMAC_SHA256
// injection while hard-banning Cookie/Set-Cookie injection (422), and scrubs
// echoed credentials from upstream errors and response payloads.
//
// DAG Boundary (Tier 3): May import internal/pkg, internal/storage/port, and
// peer internal/security/* packages only. Never mints UnsealCapability in non-test
// code and never imports internal/testkit or higher tiers.
package broker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// RedactedCredentialPlaceholder replaces any echoed secret substring in upstream
// error responses, sanitized headers, and returned errors (CT-16 / NEG-3).
const RedactedCredentialPlaceholder = "[REDACTED_CREDENTIAL]"

const (
	defaultMaxResponseBodyBytes int64 = 16 << 20 // 16 MiB bound for buffered broker responses
	defaultMaxTimeout                 = 5 * time.Minute
)

// Canonical broker security errors.
var (
	ErrInvalidBrokerConfig         = errors.New("broker: invalid broker configuration")
	ErrInvalidEgressRequest        = errors.New("broker: invalid egress request")
	ErrProhibitedCookieInjection   = errors.New("broker: Cookie and Set-Cookie injection are unconditionally prohibited")
	ErrAudienceMismatch            = errors.New("broker: target URL does not match credential bound_audience")
	ErrUnsupportedSigningAlgorithm = errors.New("broker: unsupported host signing algorithm in v0.1")
	ErrResponseTooLarge            = errors.New("broker: upstream response body exceeds configured maximum")
)

// Error describes a fail-closed BlindHostCredentialBroker rejection with canonical HTTP status and error code.
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

// TargetLocation identifies how BlindHostCredentialBroker attaches a resolved secret.
type TargetLocation string

const (
	TargetBearer         TargetLocation = "BEARER"
	TargetHeader         TargetLocation = "HEADER"
	TargetBasic          TargetLocation = "BASIC"
	TargetQueryParam     TargetLocation = "QUERY_PARAM"
	TargetHostCryptoSign TargetLocation = "HOST_CRYPTO_SIGN"
)

const (
	InjectModeHostBlind      = "HOST_BLIND_INJECT"
	InjectModeHostCryptoSign = "HOST_CRYPTO_SIGN"
)

func allowsInjectionMode(mode string, target TargetLocation) bool {
	switch mode {
	case "":
		return true // Existing resolvers may omit a mode.
	case InjectModeHostBlind:
		return target == TargetBearer || target == TargetHeader || target == TargetBasic || target == TargetQueryParam
	case InjectModeHostCryptoSign:
		return target == TargetHostCryptoSign
	default:
		return false
	}
}

// SigningAlgorithm identifies Host-executed request-signing algorithms (runtime.proto).
type SigningAlgorithm string

const (
	SigningHMACSHA256 SigningAlgorithm = "HMAC_SHA256"
	SigningAWSSigV4   SigningAlgorithm = "AWS_SIGV4"
	SigningRSASHA256  SigningAlgorithm = "RSA_SHA256"
)

// SignatureEncoding controls output formatting for HMAC_SHA256 signatures.
type SignatureEncoding string

const (
	EncodingHexLower  SignatureEncoding = "HEX_LOWER"
	EncodingBase64    SignatureEncoding = "BASE64"
	EncodingBase64URL SignatureEncoding = "BASE64URL"
)

// CryptoSignSpec instructs Host HMAC/signature calculation without disclosing the signing key.
type CryptoSignSpec struct {
	Algorithm                SigningAlgorithm
	CanonicalPayload         []byte
	OutputEncoding           SignatureEncoding
	AWSRegion                string
	AWSService               string
	SigningTimestampEpochSec int64
	SignedHeaders            []string
}

// CredentialInjection binds an opaque manifest slot name to a concrete outbound location.
type CredentialInjection struct {
	SecretSlotName string
	Location       TargetLocation
	FieldName      string
	ValuePrefix    string
	CryptoSign     *CryptoSignSpec
}

// ResolvedSlot carries non-secret metadata resolved for (orgID, connectionID, slotName).
// Plaintext secrets are never returned by CredentialBindingResolver; only Vault holds secrets.
type ResolvedSlot struct {
	OrgID        string
	ConnectionID string
	SlotName     string
	SecretID     string
	// InjectMode restricts use of the credential when specified by the binding resolver.
	InjectMode     string
	BoundAudience  *string
	AllowedHosts   []string
	AllowPrivateIP bool
	AllowedCIDRs   []string
	AllowedPorts   []int
	RequireTLS     bool
	Timeout        time.Duration
}

// CredentialBindingResolver abstracts control-plane slot lookup (decoupled from TP-IMP-0011/0012 tables).
type CredentialBindingResolver interface {
	ResolveSlot(ctx context.Context, orgID, connectionID, slotName string) (*ResolvedSlot, error)
}

// ConnectionPolicyResolver optionally resolves connection egress policy when a call
// performs unauthenticated egress (e.g. self-hosted LAN auth_mode=NONE with zero slots).
type ConnectionPolicyResolver interface {
	ResolveConnectionPolicy(ctx context.Context, orgID, connectionID string) (*egress.Policy, error)
}

// VaultUnsealer abstracts Vault credential unsealing guarded by UnsealCapability.
type VaultUnsealer interface {
	UnsealWithCapability(ctx context.Context, cap vault.UnsealCapability, orgID, secretID string) (*vault.UnsealedSecret, error)
}

// Config configures BlindHostCredentialBroker fail-closed.
type Config struct {
	Resolver CredentialBindingResolver
	// PolicyResolver optionally provides an explicit Host-authoritative connection
	// egress policy lookup for zero-slot connections (auth_mode=NONE). When nil,
	// BlindHostCredentialBroker falls back to Resolver.(ConnectionPolicyResolver)
	// and fails closed if neither resolves an authoritative policy.
	PolicyResolver            ConnectionPolicyResolver
	Vault                     VaultUnsealer
	UnsealCap                 vault.UnsealCapability
	Audit                     port.AuditSink
	NowMs                     func() int64
	MaxResponseBodyBytes      int64
	DefaultTimeout            time.Duration
	MaxTimeout                time.Duration
	AllowCrossOriginRedirects bool
	HostResolver              egress.HostResolver
	ControlHook               func(network, address string, c syscall.RawConn) error
}

// EgressRequest represents an adapter-produced intermediate HTTP request carrying zero
// raw secrets and zero self-declared egress permissions (strictly non-authoritative).
// Egress allowlists are always resolved from Host-authoritative Control Plane state.
type EgressRequest struct {
	OrgID                string
	ConnectionID         string
	Method               string
	TargetURL            string
	SafeHeaders          map[string]string
	BodyBytes            []byte
	CredentialInjections []CredentialInjection
	TimeoutOverride      time.Duration
	ActorKind            string
	ActorID              string
	EntryPoint           string
	RequestID            string
}

// HostEgressConstraint allows trusted Host Control/Data Plane callers to apply an
// additional monotonic narrowing constraint on top of the approved Connection policy.
// It can only tighten egress (never grant or widen permissions) and is intentionally
// excluded from EgressRequest so adapter/guest specs cannot map into it.
type HostEgressConstraint struct {
	NarrowPolicy *egress.Policy
}

// EgressResponse returns upstream HTTP status, Cookie/credential-stripped headers, and scrubbed body.
type EgressResponse struct {
	StatusCode       int
	SanitizedHeaders http.Header
	Body             []byte
}

// EgressStreamResponse returns upstream HTTP status, Cookie/credential-stripped headers,
// and a live scrubbing io.ReadCloser that strips any echoed credential variants (including
// secrets split across chunk boundaries) and zeroizes ephemeral scrubber memory on Close.
type EgressStreamResponse struct {
	StatusCode       int
	SanitizedHeaders http.Header
	Body             io.ReadCloser
}

// BlindHostCredentialBroker executes outbound HTTP requests with connect-time SSRF protection,
// audience-bound credential unsealing, edge injection, and bidirectional secret redaction.
type BlindHostCredentialBroker struct {
	resolver                  CredentialBindingResolver
	policyResolver            ConnectionPolicyResolver
	hasExplicitPolicyResolver bool
	vault                     VaultUnsealer
	unsealCap                 vault.UnsealCapability
	audit                     port.AuditSink
	nowMs                     func() int64
	maxResponseBodyBytes      int64
	defaultTimeout            time.Duration
	maxTimeout                time.Duration
	allowCrossOriginRedirects bool
	hostResolver              egress.HostResolver
	controlHook               func(network, address string, c syscall.RawConn) error
}

// NewBlindBroker constructs a BlindHostCredentialBroker, failing closed on missing dependencies.
func NewBlindBroker(cfg Config) (*BlindHostCredentialBroker, error) {
	if cfg.Resolver == nil {
		return nil, fmt.Errorf("%w: non-nil CredentialBindingResolver is required", ErrInvalidBrokerConfig)
	}
	if cfg.Vault == nil {
		return nil, fmt.Errorf("%w: non-nil VaultUnsealer is required", ErrInvalidBrokerConfig)
	}
	if cfg.Audit == nil {
		return nil, fmt.Errorf("%w: non-nil port.AuditSink is required", ErrInvalidBrokerConfig)
	}
	policyResolver := cfg.PolicyResolver
	explicitPolicyResolver := policyResolver != nil
	if policyResolver == nil {
		if cpr, ok := cfg.Resolver.(ConnectionPolicyResolver); ok {
			policyResolver = cpr
		}
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	maxBody := cfg.MaxResponseBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxResponseBodyBytes
	}
	maxTimeout := cfg.MaxTimeout
	if maxTimeout <= 0 {
		maxTimeout = defaultMaxTimeout
	}
	defTimeout := cfg.DefaultTimeout
	if defTimeout <= 0 {
		defTimeout = 30 * time.Second
	}
	if defTimeout > maxTimeout {
		defTimeout = maxTimeout
	}
	return &BlindHostCredentialBroker{
		resolver:                  cfg.Resolver,
		policyResolver:            policyResolver,
		hasExplicitPolicyResolver: explicitPolicyResolver,
		vault:                     cfg.Vault,
		unsealCap:                 cfg.UnsealCap,
		audit:                     cfg.Audit,
		nowMs:                     nowFn,
		maxResponseBodyBytes:      maxBody,
		defaultTimeout:            defTimeout,
		maxTimeout:                maxTimeout,
		allowCrossOriginRedirects: cfg.AllowCrossOriginRedirects,
		hostResolver:              cfg.HostResolver,
		controlHook:               cfg.ControlHook,
	}, nil
}

// AudienceMatches checks whether targetURL matches boundAudience (AUTH-16 / CT-19).
// If boundAudience is nil, returns true (connection AllowedHosts still governs target hosts).
// If boundAudience is non-nil and blank or targets a different scheme/host/port/path-prefix,
// returns false with a non-secret mismatch explanation.
func AudienceMatches(targetURL *url.URL, boundAudience *string) (bool, string) {
	if targetURL == nil {
		return false, "nil target URL"
	}
	if boundAudience == nil {
		return true, ""
	}
	aud := strings.TrimSpace(*boundAudience)
	if aud == "" {
		return false, "blank bound_audience cannot be satisfied"
	}

	targetScheme := strings.ToLower(strings.TrimSpace(targetURL.Scheme))
	targetHost := canonicalHost(targetURL.Hostname())
	targetPort := urlPortOrDefault(targetURL, targetScheme)
	if targetHost == "" || targetPort <= 0 {
		return false, "invalid target URL authority"
	}

	if strings.Contains(aud, "://") {
		audURL, err := url.Parse(aud)
		if err != nil || audURL.Hostname() == "" || audURL.User != nil {
			return false, "malformed bound_audience URI"
		}
		audScheme := strings.ToLower(strings.TrimSpace(audURL.Scheme))
		if audScheme != targetScheme {
			return false, fmt.Sprintf("scheme mismatch: target=%q audience=%q", targetScheme, audScheme)
		}
		audHost := canonicalHost(audURL.Hostname())
		if !strings.EqualFold(targetHost, audHost) {
			return false, fmt.Sprintf("host mismatch: target=%q audience=%q", targetHost, audHost)
		}
		audPort := urlPortOrDefault(audURL, audScheme)
		if targetPort != audPort {
			return false, fmt.Sprintf("port mismatch: target=%d audience=%d", targetPort, audPort)
		}
		if audPath := strings.TrimRight(audURL.EscapedPath(), "/"); audPath != "" {
			cleanAudPath, aerr := cleanAudiencePath(audPath)
			if aerr != nil {
				return false, fmt.Sprintf("invalid bound_audience path: %s", aerr.Error())
			}
			cleanTgtPath, terr := cleanAudiencePath(targetURL.EscapedPath())
			if terr != nil {
				return false, fmt.Sprintf("unsafe target URL path: %s", terr.Error())
			}
			if cleanTgtPath != cleanAudPath && !strings.HasPrefix(cleanTgtPath, cleanAudPath+"/") {
				return false, fmt.Sprintf("path mismatch: target does not match bound_audience prefix %q", cleanAudPath)
			}
		}
		return true, ""
	}

	// Host-only or host:port audience specification.
	if h, pStr, err := net.SplitHostPort(aud); err == nil {
		audPort, perr := strconv.Atoi(pStr)
		if perr != nil || audPort < 1 || audPort > 65535 {
			return false, "invalid port in bound_audience"
		}
		if !strings.EqualFold(targetHost, canonicalHost(h)) || targetPort != audPort {
			return false, fmt.Sprintf("authority mismatch: target=%s:%d audience=%s:%d", targetHost, targetPort, canonicalHost(h), audPort)
		}
		return true, ""
	}

	audHost := canonicalHost(aud)
	if audHost == "" || !strings.EqualFold(targetHost, audHost) {
		return false, fmt.Sprintf("host mismatch: target=%q audience=%q", targetHost, audHost)
	}
	return true, ""
}

func cleanAudiencePath(rawEscapedPath string) (string, error) {
	if strings.ContainsAny(rawEscapedPath, "\\\x00\r\n") {
		return "", errors.New("backslash or control characters are forbidden in URL path")
	}
	trimmed := strings.TrimPrefix(rawEscapedPath, "/")
	if trimmed != "" {
		segments := strings.Split(trimmed, "/")
		for _, seg := range segments {
			if seg == "." || seg == ".." {
				return "", errors.New("dot-segment path traversal is forbidden in URL path")
			}
			decoded, err := url.PathUnescape(seg)
			if err != nil {
				return "", errors.New("malformed percent-encoding in URL path")
			}
			if decoded == "." || decoded == ".." || strings.ContainsAny(decoded, "/\\\x00\r\n") {
				return "", errors.New("encoded dot-segment or path separator is forbidden in URL path")
			}
		}
	}
	cleaned := path.Clean("/" + trimmed)
	if cleaned == "/" {
		return "", nil
	}
	return cleaned, nil
}

func canonicalHost(raw string) string {
	h := strings.TrimSpace(raw)
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	if ip, err := netip.ParseAddr(h); err == nil {
		return ip.String()
	}
	return strings.ToLower(h)
}

func urlPortOrDefault(u *url.URL, scheme string) int {
	if u == nil {
		return 0
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return 0
		}
		return n
	}
	switch scheme {
	case "https":
		return 443
	case "http":
		return 80
	default:
		return 0
	}
}

// Execute validates egress allowlists and audience bindings, resolves & unseals credentials
// strictly via UnsealCapability, injects credentials at the network edge, executes via SSRFGuard,
// strips Set-Cookie/Cookie/credential headers, and redacts any echoed secret substrings before returning.
func (b *BlindHostCredentialBroker) Execute(ctx context.Context, req EgressRequest) (*EgressResponse, error) {
	return b.execute(ctx, req, HostEgressConstraint{})
}

// ExecuteWithHostConstraint behaves identically to Execute while allowing a trusted Host caller
// to apply an optional monotonic narrowing policy constraint (never granting or widening egress).
func (b *BlindHostCredentialBroker) ExecuteWithHostConstraint(ctx context.Context, req EgressRequest, constraint HostEgressConstraint) (*EgressResponse, error) {
	return b.execute(ctx, req, constraint)
}

// ExecuteStream opens a streaming egress connection enforcing the exact same allowlists,
// audience bindings, TOCTOU pre-connect hooks, header sanitization, and credential redaction as Execute,
// returning a live scrubbing io.ReadCloser whose Close zeroizes all ephemeral secret buffers.
func (b *BlindHostCredentialBroker) ExecuteStream(ctx context.Context, req EgressRequest) (*EgressStreamResponse, error) {
	return b.executeStream(ctx, req, HostEgressConstraint{})
}

// ExecuteStreamWithHostConstraint behaves identically to ExecuteStream while applying
// an optional HostEgressConstraint narrowing policy.
func (b *BlindHostCredentialBroker) ExecuteStreamWithHostConstraint(ctx context.Context, req EgressRequest, constraint HostEgressConstraint) (*EgressStreamResponse, error) {
	return b.executeStream(ctx, req, constraint)
}

func (b *BlindHostCredentialBroker) execute(ctx context.Context, req EgressRequest, constraint HostEgressConstraint) (*EgressResponse, error) {
	out, err := b.sendOutbound(ctx, req, constraint, false)
	if err != nil {
		return nil, err
	}
	if out.closeIdle != nil {
		defer out.closeIdle()
	}
	defer out.scrubber.zeroize()
	defer func() { _ = out.resp.Body.Close() }()

	limited := io.LimitReader(out.resp.Body, b.maxResponseBodyBytes+1)
	rawBody, readErr := io.ReadAll(limited)
	defer clear(rawBody)
	if readErr != nil {
		return nil, out.scrubber.sanitizeError(fmt.Errorf("broker: read upstream response: %w", readErr))
	}
	if int64(len(rawBody)) > b.maxResponseBodyBytes {
		return nil, &Error{
			HTTPStatus: http.StatusBadGateway,
			Code:       "upstream_response_too_large",
			Message:    fmt.Sprintf("upstream response exceeds %d byte cap", b.maxResponseBodyBytes),
			Err:        ErrResponseTooLarge,
		}
	}

	scrubbedBody := out.scrubber.scrubBytes(rawBody)
	return &EgressResponse{
		StatusCode:       out.resp.StatusCode,
		SanitizedHeaders: out.sanitizedHeaders,
		Body:             scrubbedBody,
	}, nil
}

func (b *BlindHostCredentialBroker) executeStream(ctx context.Context, req EgressRequest, constraint HostEgressConstraint) (*EgressStreamResponse, error) {
	out, err := b.sendOutbound(ctx, req, constraint, true)
	if err != nil {
		return nil, err
	}
	return &EgressStreamResponse{
		StatusCode:       out.resp.StatusCode,
		SanitizedHeaders: out.sanitizedHeaders,
		Body:             newScrubbingStreamReader(out.resp.Body, out.scrubber, out.closeIdle),
	}, nil
}

type outboundSendResult struct {
	resp             *http.Response
	sanitizedHeaders http.Header
	scrubber         *secretScrubber
	closeIdle        func()
}

func (b *BlindHostCredentialBroker) sendOutbound(ctx context.Context, req EgressRequest, constraint HostEgressConstraint, streaming bool) (*outboundSendResult, error) {
	var failZeroize *secretScrubber
	handoff := false
	defer func() {
		if !handoff && failZeroize != nil {
			failZeroize.zeroize()
		}
	}()
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is required", ErrInvalidEgressRequest)
	}
	if strings.TrimSpace(req.OrgID) == "" || strings.TrimSpace(req.ConnectionID) == "" {
		return nil, fmt.Errorf("%w: org_id and connection_id are required", ErrInvalidEgressRequest)
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" || strings.ContainsAny(method, " \t\r\n\x00") {
		return nil, fmt.Errorf("%w: invalid HTTP method", ErrInvalidEgressRequest)
	}
	rawURL := strings.TrimSpace(req.TargetURL)
	parsedURL, err := url.Parse(rawURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" || parsedURL.User != nil {
		return nil, fmt.Errorf("%w: invalid target_url", ErrInvalidEgressRequest)
	}

	// 1. Enforce DEC-004 Cookie / Set-Cookie ban & header hygiene prior to any resolver/vault/socket action.
	if err := validateOutboundHeadersAndInjections(req.SafeHeaders, req.CredentialInjections); err != nil {
		b.recordAudit(ctx, req, "broker.prohibited_cookie_injection", map[string]any{
			"target_host": parsedURL.Host,
			"reason":      err.Error(),
		})
		return nil, err
	}

	// 2. Enforce optional HostEgressConstraint fail-closed prior to slot/vault resolution, then
	// resolve slots/policies and validate HostMatches + BoundAudience prior to unsealing or dialing.
	if constraint.NarrowPolicy != nil {
		if _, cerr := constraint.NarrowPolicy.ValidateTargetURL(rawURL); cerr != nil {
			_ = b.recordAudit(ctx, req, "broker.egress_host_denied", map[string]any{
				"target_host": parsedURL.Host,
				"reason":      cerr.Error(),
			})
			return nil, cerr
		}
	}

	var plans []brokerSlotPlan
	var policies []egress.Policy

	if len(req.CredentialInjections) > 0 {
		plans = make([]brokerSlotPlan, 0, len(req.CredentialInjections))
		for _, inj := range req.CredentialInjections {
			slot, rerr := b.resolver.ResolveSlot(ctx, req.OrgID, req.ConnectionID, inj.SecretSlotName)
			if rerr != nil {
				b.recordAudit(ctx, req, "broker.slot_resolution_failed", map[string]any{
					"slot_name": inj.SecretSlotName,
					"target":    parsedURL.Host,
				})
				return nil, rerr
			}
			if slot == nil {
				return nil, fmt.Errorf("%w: resolved slot %q is nil", ErrInvalidEgressRequest, inj.SecretSlotName)
			}
			if slot.OrgID != req.OrgID {
				_ = b.recordAudit(ctx, req, "broker.cross_tenant_slot_denied", map[string]any{
					"slot_name": inj.SecretSlotName,
				})
				return nil, &egress.Error{
					HTTPStatus: http.StatusForbidden,
					Code:       "egress_host_denied",
					Message:    "resolved credential slot organization mismatch",
					Err:        egress.ErrHostDenied,
				}
			}
			if slot.ConnectionID != req.ConnectionID {
				_ = b.recordAudit(ctx, req, "broker.connection_slot_mismatch", map[string]any{
					"slot_name": inj.SecretSlotName,
				})
				return nil, &egress.Error{
					HTTPStatus: http.StatusForbidden,
					Code:       "egress_host_denied",
					Message:    "resolved credential slot connection mismatch",
					Err:        egress.ErrHostDenied,
				}
			}

			if !allowsInjectionMode(slot.InjectMode, inj.Location) {
				b.recordAudit(ctx, req, "broker.credential_binding_mode_denied", map[string]any{
					"slot_name": inj.SecretSlotName,
				})
				return nil, &Error{
					HTTPStatus: http.StatusForbidden,
					Code:       "credential_binding_mode_denied",
					Message:    "credential injection does not match the resolved slot binding mode",
					Err:        ErrInvalidEgressRequest,
				}
			}

			pol := egress.Policy{
				AllowedHosts:   slot.AllowedHosts,
				AllowPrivateIP: slot.AllowPrivateIP,
				AllowedCIDRs:   slot.AllowedCIDRs,
				AllowedPorts:   slot.AllowedPorts,
				RequireTLS:     slot.RequireTLS,
			}
			if _, verr := pol.ValidateTargetURL(rawURL); verr != nil {
				b.recordAudit(ctx, req, "broker.egress_host_denied", map[string]any{
					"slot_name":   inj.SecretSlotName,
					"target_host": parsedURL.Host,
					"reason":      verr.Error(),
				})
				return nil, verr
			}
			if ok, mismatchReason := AudienceMatches(parsedURL, slot.BoundAudience); !ok {
				b.recordAudit(ctx, req, "broker.credential_audience_mismatch", map[string]any{
					"slot_name":   inj.SecretSlotName,
					"target_host": parsedURL.Host,
					"reason":      mismatchReason,
				})
				return nil, &Error{
					HTTPStatus: http.StatusForbidden,
					Code:       "credential_audience_mismatch",
					Message:    mismatchReason,
					Err:        ErrAudienceMismatch,
				}
			}
			plans = append(plans, brokerSlotPlan{inj: inj, slot: slot})
			policies = append(policies, pol)
		}
		if b.hasExplicitPolicyResolver {
			connPolicy, cerr := b.resolveAuthoritativeConnectionPolicy(ctx, req, parsedURL, rawURL)
			if cerr != nil {
				return nil, cerr
			}
			policies = append(policies, connPolicy)
		}
	} else {
		connPolicy, cerr := b.resolveAuthoritativeConnectionPolicy(ctx, req, parsedURL, rawURL)
		if cerr != nil {
			return nil, cerr
		}
		policies = append(policies, connPolicy)
	}

	if constraint.NarrowPolicy != nil {
		if _, cerr := constraint.NarrowPolicy.ValidateTargetURL(rawURL); cerr != nil {
			_ = b.recordAudit(ctx, req, "broker.egress_host_denied", map[string]any{
				"target_host": parsedURL.Host,
				"reason":      cerr.Error(),
			})
			return nil, cerr
		}
		policies = append(policies, *constraint.NarrowPolicy)
	}

	mergedPolicy, err := intersectPolicies(policies)
	if err != nil {
		b.recordAudit(ctx, req, "broker.egress_host_denied", map[string]any{
			"target_host": parsedURL.Host,
			"reason":      err.Error(),
		})
		return nil, err
	}

	// Copy URL before mutating query params cleanly.
	outURL := *parsedURL
	var bodyReader io.Reader
	if len(req.BodyBytes) > 0 {
		bodyCopy := make([]byte, len(req.BodyBytes))
		copy(bodyCopy, req.BodyBytes)
		bodyReader = bytes.NewReader(bodyCopy)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, outURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("%w: construct outbound request: %v", ErrInvalidEgressRequest, err)
	}
	for k, v := range req.SafeHeaders {
		httpReq.Header.Set(strings.TrimSpace(k), v)
	}

	// 3. Unseal capability-protected secrets, re-verify Vault bound_audience, inject into wire buffers,
	// capture transient scrubber variants, and immediately zeroize unsealed secret memory.
	scrubber := &secretScrubber{}
	failZeroize = scrubber

	injectedSensitiveHeaders := make([]string, 0, len(plans))
	for _, item := range plans {
		if strings.TrimSpace(item.slot.SecretID) == "" {
			return nil, fmt.Errorf("%w: resolved slot %q missing secret_id", ErrInvalidEgressRequest, item.inj.SecretSlotName)
		}
		unsealed, uerr := b.vault.UnsealWithCapability(ctx, b.unsealCap, req.OrgID, item.slot.SecretID)
		if uerr != nil {
			b.recordAudit(ctx, req, "broker.secret_unseal_failed", map[string]any{
				"slot_name": item.inj.SecretSlotName,
				"reason":    uerr.Error(),
			})
			return nil, uerr
		}

		injectErr := func() error {
			defer unsealed.Zeroize()
			if unsealed.OrgID != req.OrgID {
				_ = b.recordAudit(ctx, req, "broker.cross_tenant_unseal_denied", map[string]any{
					"slot_name": item.inj.SecretSlotName,
				})
				return &egress.Error{
					HTTPStatus: http.StatusForbidden,
					Code:       "egress_host_denied",
					Message:    "unsealed secret organization mismatch",
					Err:        egress.ErrHostDenied,
				}
			}
			// Authoritative bound_audience verification from Vault record prior to opening TCP socket.
			if ok, mismatchReason := AudienceMatches(httpReq.URL, unsealed.BoundAudience); !ok {
				b.recordAudit(ctx, req, "broker.credential_audience_mismatch", map[string]any{
					"slot_name":   item.inj.SecretSlotName,
					"target_host": httpReq.URL.Host,
					"reason":      mismatchReason,
				})
				return &Error{
					HTTPStatus: http.StatusForbidden,
					Code:       "credential_audience_mismatch",
					Message:    mismatchReason,
					Err:        ErrAudienceMismatch,
				}
			}

			scrubber.addSecret(unsealed.Plaintext)
			hdrName, aerr := applyCredentialInjection(httpReq, item.inj, unsealed.Plaintext, scrubber)
			if aerr != nil {
				return aerr
			}
			if hdrName != "" {
				injectedSensitiveHeaders = append(injectedSensitiveHeaders, hdrName)
			}
			if err := b.recordAudit(ctx, req, "broker.credential_egress_authorized", map[string]any{
				"slot_name":      item.inj.SecretSlotName,
				"target_host":    httpReq.URL.Host,
				"location":       string(item.inj.Location),
				"secret_version": unsealed.Version,
			}); err != nil {
				return fmt.Errorf("broker: audit credential unseal: %w", err)
			}
			return nil
		}()
		if injectErr != nil {
			return nil, injectErr
		}
	}

	mergedPolicy.SensitiveHeaders = append(mergedPolicy.SensitiveHeaders, injectedSensitiveHeaders...)

	timeout := b.defaultTimeout
	if req.TimeoutOverride > 0 {
		timeout = req.TimeoutOverride
	}
	if timeout > b.maxTimeout {
		timeout = b.maxTimeout
	}
	for _, item := range plans {
		if item.slot.Timeout > 0 && item.slot.Timeout < timeout {
			timeout = item.slot.Timeout
		}
	}

	// Re-verify live connection/extension authorization immediately prior to socket dial (TOCTOU guard).
	if verr := b.reverifyAuthorizationBeforeDispatch(ctx, req, parsedURL, rawURL, plans, constraint, ""); verr != nil {
		_ = b.recordAudit(ctx, req, "broker.egress_revoked_pre_dispatch", map[string]any{
			"target_host": parsedURL.Host,
			"reason":      scrubber.scrubString(verr.Error()),
		})
		return nil, scrubber.sanitizeError(verr)
	}

	effectiveControlHook := func(network, address string, c syscall.RawConn) error {
		if rerr := b.reverifyAuthorizationBeforeDispatch(ctx, req, parsedURL, rawURL, plans, constraint, address); rerr != nil {
			_ = b.recordAudit(ctx, req, "broker.egress_revoked_pre_connect", map[string]any{
				"target_host": parsedURL.Host,
				"dial_addr":   address,
				"reason":      scrubber.scrubString(rerr.Error()),
			})
			return rerr
		}
		if b.controlHook != nil {
			return b.controlHook(network, address, c)
		}
		return nil
	}

	clientCfg := egress.ClientConfig{
		Policy:                   mergedPolicy,
		Timeout:                  timeout,
		DisableTotalTimeout:      streaming,
		DisableKeepAlives:        true,
		DenyCrossOriginRedirects: !b.allowCrossOriginRedirects,
		Resolver:                 b.hostResolver,
		ControlHook:              effectiveControlHook,
	}
	if streaming {
		clientCfg.ResponseHeaderTimeout = timeout
	}
	client, err := egress.NewGuardedHTTPClient(clientCfg)
	if err != nil {
		return nil, scrubber.sanitizeError(err)
	}

	resp, doErr := client.Do(httpReq)
	if doErr != nil {
		client.CloseIdleConnections()
		_ = b.recordAudit(ctx, req, "broker.egress_transport_blocked", map[string]any{
			"target_host": parsedURL.Host,
			"reason":      scrubber.scrubString(doErr.Error()),
		})
		return nil, scrubber.sanitizeError(doErr)
	}

	sanitizedHeaders := sanitizeResponseHeaders(resp.Header, injectedSensitiveHeaders, scrubber)
	handoff = true
	return &outboundSendResult{
		resp:             resp,
		sanitizedHeaders: sanitizedHeaders,
		scrubber:         scrubber,
		closeIdle:        client.CloseIdleConnections,
	}, nil
}

type brokerSlotPlan struct {
	inj  CredentialInjection
	slot *ResolvedSlot
}

func (b *BlindHostCredentialBroker) reverifyAuthorizationBeforeDispatch(
	ctx context.Context,
	req EgressRequest,
	parsedURL *url.URL,
	rawURL string,
	plans []brokerSlotPlan,
	constraint HostEgressConstraint,
	dialAddr string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var policies []egress.Policy
	if len(plans) > 0 {
		for _, item := range plans {
			slot, rerr := b.resolver.ResolveSlot(ctx, req.OrgID, req.ConnectionID, item.inj.SecretSlotName)
			if rerr != nil {
				return rerr
			}
			if slot == nil {
				return fmt.Errorf("%w: resolved slot %q is nil", ErrInvalidEgressRequest, item.inj.SecretSlotName)
			}
			if slot.OrgID != req.OrgID || slot.ConnectionID != req.ConnectionID {
				return &egress.Error{
					HTTPStatus: http.StatusForbidden,
					Code:       "egress_host_denied",
					Message:    "resolved credential slot tenant or connection mismatch",
					Err:        egress.ErrHostDenied,
				}
			}
			if slot.SecretID != item.slot.SecretID || slot.InjectMode != item.slot.InjectMode || !allowsInjectionMode(slot.InjectMode, item.inj.Location) {
				return &Error{
					HTTPStatus: http.StatusForbidden,
					Code:       "credential_binding_mode_denied",
					Message:    "credential binding changed or no longer permitted prior to dispatch",
					Err:        ErrInvalidEgressRequest,
				}
			}
			pol := egress.Policy{
				AllowedHosts:   slot.AllowedHosts,
				AllowPrivateIP: slot.AllowPrivateIP,
				AllowedCIDRs:   slot.AllowedCIDRs,
				AllowedPorts:   slot.AllowedPorts,
				RequireTLS:     slot.RequireTLS,
			}
			if _, verr := pol.ValidateTargetURL(rawURL); verr != nil {
				return verr
			}
			if ok, mismatchReason := AudienceMatches(parsedURL, slot.BoundAudience); !ok {
				return &Error{
					HTTPStatus: http.StatusForbidden,
					Code:       "credential_audience_mismatch",
					Message:    mismatchReason,
					Err:        ErrAudienceMismatch,
				}
			}
			policies = append(policies, pol)
		}
		if b.hasExplicitPolicyResolver {
			connPolicy, cerr := b.resolveAuthoritativeConnectionPolicy(ctx, req, parsedURL, rawURL)
			if cerr != nil {
				return cerr
			}
			policies = append(policies, connPolicy)
		}
	} else {
		connPolicy, cerr := b.resolveAuthoritativeConnectionPolicy(ctx, req, parsedURL, rawURL)
		if cerr != nil {
			return cerr
		}
		policies = append(policies, connPolicy)
	}
	if constraint.NarrowPolicy != nil {
		if _, cerr := constraint.NarrowPolicy.ValidateTargetURL(rawURL); cerr != nil {
			return cerr
		}
		policies = append(policies, *constraint.NarrowPolicy)
	}
	mergedPolicy, err := intersectPolicies(policies)
	if err != nil {
		return err
	}
	if _, verr := mergedPolicy.ValidateTargetURL(rawURL); verr != nil {
		return verr
	}
	if strings.TrimSpace(dialAddr) != "" {
		if cerr := verifyConnectSocketAddr(mergedPolicy, parsedURL, dialAddr); cerr != nil {
			return cerr
		}
	}
	return nil
}

func verifyConnectSocketAddr(policy egress.Policy, parsedURL *url.URL, dialAddr string) error {
	liveDialer, err := egress.NewDialer(egress.DialerConfig{Policy: policy})
	if err != nil {
		return err
	}
	if err := liveDialer.CheckAddr(dialAddr); err != nil {
		return err
	}
	_, portStr, err := net.SplitHostPort(strings.TrimSpace(dialAddr))
	if err != nil {
		return &egress.Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("invalid connect address %q", dialAddr),
			Err:        egress.ErrHostDenied,
		}
	}
	if parsedURL != nil && parsedURL.Hostname() != "" {
		socketURL := *parsedURL
		socketURL.Host = net.JoinHostPort(parsedURL.Hostname(), portStr)
		if _, verr := policy.ValidateTargetURL(socketURL.String()); verr != nil {
			return verr
		}
	}
	return nil
}

func (b *BlindHostCredentialBroker) resolveAuthoritativeConnectionPolicy(
	ctx context.Context,
	req EgressRequest,
	parsedURL *url.URL,
	rawURL string,
) (egress.Policy, error) {
	if b.policyResolver == nil {
		_ = b.recordAudit(ctx, req, "broker.egress_policy_missing", map[string]any{
			"target_host": parsedURL.Host,
			"reason":      "missing authoritative ConnectionPolicyResolver",
		})
		return egress.Policy{}, &egress.Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    "unauthenticated egress requires an authoritative ConnectionPolicyResolver",
			Err:        egress.ErrHostDenied,
		}
	}
	resolved, rerr := b.policyResolver.ResolveConnectionPolicy(ctx, req.OrgID, req.ConnectionID)
	if rerr != nil {
		_ = b.recordAudit(ctx, req, "broker.policy_resolution_failed", map[string]any{
			"target_host": parsedURL.Host,
		})
		return egress.Policy{}, rerr
	}
	if resolved == nil {
		_ = b.recordAudit(ctx, req, "broker.egress_policy_missing", map[string]any{
			"target_host": parsedURL.Host,
			"reason":      "resolved ConnectionPolicy is nil",
		})
		return egress.Policy{}, &egress.Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    "authoritative ConnectionPolicy is nil",
			Err:        egress.ErrHostDenied,
		}
	}
	if _, verr := resolved.ValidateTargetURL(rawURL); verr != nil {
		_ = b.recordAudit(ctx, req, "broker.egress_host_denied", map[string]any{
			"target_host": parsedURL.Host,
			"reason":      verr.Error(),
		})
		return egress.Policy{}, verr
	}
	return *resolved, nil
}

func isValidHTTPToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func isProhibitedFramingOrHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "connection", "transfer-encoding", "te", "trailer", "upgrade",
		"content-length", "keep-alive", "proxy-connection", "proxy-authorization":
		return true
	default:
		return false
	}
}

func checkHeaderSafeCollision(field string, safeHeaders map[string]string) error {
	for hk := range safeHeaders {
		if strings.EqualFold(strings.TrimSpace(hk), field) {
			return &Error{
				HTTPStatus: http.StatusUnprocessableEntity,
				Code:       "credential_header_collision",
				Message:    fmt.Sprintf("safe_headers cannot override credential header %q", field),
				Err:        ErrInvalidEgressRequest,
			}
		}
	}
	return nil
}

func validateOutboundHeadersAndInjections(safeHeaders map[string]string, injections []CredentialInjection) error {
	for k, v := range safeHeaders {
		tk := strings.TrimSpace(k)
		if !isValidHTTPToken(tk) || strings.ContainsAny(v, "\r\n\x00") {
			return &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "invalid_safe_header",
				Message:    fmt.Sprintf("invalid safe header %q", k),
				Err:        ErrInvalidEgressRequest,
			}
		}
		if isCookieHeader(tk) {
			return &Error{
				HTTPStatus: http.StatusUnprocessableEntity,
				Code:       "prohibited_cookie_injection",
				Message:    fmt.Sprintf("header %q is prohibited by DEC-004", k),
				Err:        ErrProhibitedCookieInjection,
			}
		}
		if isProhibitedFramingOrHopHeader(tk) || strings.EqualFold(tk, "authorization") {
			return &Error{
				HTTPStatus: http.StatusUnprocessableEntity,
				Code:       "prohibited_safe_header",
				Message:    fmt.Sprintf("header %q cannot be set in safe_headers", k),
				Err:        ErrInvalidEgressRequest,
			}
		}
	}

	seenHeaderFields := make(map[string]bool)
	seenQueryParams := make(map[string]bool)
	for _, inj := range injections {
		if strings.TrimSpace(inj.SecretSlotName) == "" {
			return &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "invalid_credential_injection",
				Message:    "secret_slot_name is required",
				Err:        ErrInvalidEgressRequest,
			}
		}
		field := strings.TrimSpace(inj.FieldName)
		if (inj.Location == TargetBearer || inj.Location == TargetBasic) && field == "" {
			field = "Authorization"
		}
		if field == "" {
			return &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "invalid_credential_injection",
				Message:    "field_name is required",
				Err:        ErrInvalidEgressRequest,
			}
		}
		if isCookieHeader(field) {
			return &Error{
				HTTPStatus: http.StatusUnprocessableEntity,
				Code:       "prohibited_cookie_injection",
				Message:    fmt.Sprintf("credential injection targeting %q is unconditionally prohibited by DEC-004", inj.FieldName),
				Err:        ErrProhibitedCookieInjection,
			}
		}
		if strings.ContainsAny(field, "\r\n\x00:") || strings.ContainsAny(inj.ValuePrefix, "\r\n\x00") {
			return &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "invalid_credential_injection",
				Message:    "control characters forbidden in credential injection",
				Err:        ErrInvalidEgressRequest,
			}
		}

		switch inj.Location {
		case TargetBearer, TargetHeader, TargetBasic:
			if !isValidHTTPToken(field) || isProhibitedFramingOrHopHeader(field) {
				return &Error{
					HTTPStatus: http.StatusUnprocessableEntity,
					Code:       "prohibited_credential_header",
					Message:    fmt.Sprintf("credential injection header %q is invalid or prohibited", inj.FieldName),
					Err:        ErrInvalidEgressRequest,
				}
			}
			lk := strings.ToLower(field)
			if seenHeaderFields[lk] {
				return &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "duplicate_credential_header",
					Message:    fmt.Sprintf("duplicate credential header %q", field),
					Err:        ErrInvalidEgressRequest,
				}
			}
			if cerr := checkHeaderSafeCollision(field, safeHeaders); cerr != nil {
				return cerr
			}
			seenHeaderFields[lk] = true
		case TargetQueryParam:
			if strings.ContainsAny(field, "=&?# \t") || field != inj.FieldName {
				return &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "invalid_credential_injection",
					Message:    fmt.Sprintf("invalid query param field_name %q", field),
					Err:        ErrInvalidEgressRequest,
				}
			}
			if seenQueryParams[field] {
				return &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "duplicate_credential_query_param",
					Message:    fmt.Sprintf("duplicate credential query param %q", field),
					Err:        ErrInvalidEgressRequest,
				}
			}
			seenQueryParams[field] = true
		case TargetHostCryptoSign:
			if !isValidHTTPToken(field) || isProhibitedFramingOrHopHeader(field) {
				return &Error{
					HTTPStatus: http.StatusUnprocessableEntity,
					Code:       "prohibited_credential_header",
					Message:    fmt.Sprintf("signature header %q is invalid or prohibited", inj.FieldName),
					Err:        ErrInvalidEgressRequest,
				}
			}
			if inj.CryptoSign == nil {
				return &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "invalid_crypto_sign",
					Message:    "CryptoSign spec is required for TargetHostCryptoSign",
					Err:        ErrInvalidEgressRequest,
				}
			}
			switch inj.CryptoSign.Algorithm {
			case SigningHMACSHA256:
				switch inj.CryptoSign.OutputEncoding {
				case "", EncodingHexLower, EncodingBase64, EncodingBase64URL:
				default:
					return &Error{
						HTTPStatus: http.StatusBadRequest,
						Code:       "invalid_crypto_sign",
						Message:    fmt.Sprintf("unsupported HMAC encoding %q", inj.CryptoSign.OutputEncoding),
						Err:        ErrInvalidEgressRequest,
					}
				}
			case SigningAWSSigV4, SigningRSASHA256:
				return &Error{
					HTTPStatus: http.StatusUnprocessableEntity,
					Code:       "unsupported_signing_algorithm",
					Message:    fmt.Sprintf("signing algorithm %q is not supported in v0.1 blind broker", inj.CryptoSign.Algorithm),
					Err:        ErrUnsupportedSigningAlgorithm,
				}
			default:
				return &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "invalid_crypto_sign",
					Message:    fmt.Sprintf("unknown signing algorithm %q", inj.CryptoSign.Algorithm),
					Err:        ErrInvalidEgressRequest,
				}
			}
			lk := strings.ToLower(field)
			if seenHeaderFields[lk] {
				return &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "duplicate_credential_header",
					Message:    fmt.Sprintf("duplicate credential header %q", field),
					Err:        ErrInvalidEgressRequest,
				}
			}
			if cerr := checkHeaderSafeCollision(field, safeHeaders); cerr != nil {
				return cerr
			}
			seenHeaderFields[lk] = true
		default:
			return &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "invalid_credential_injection",
				Message:    fmt.Sprintf("unsupported TargetLocation %q", inj.Location),
				Err:        ErrInvalidEgressRequest,
			}
		}
	}
	return nil
}

func isCookieHeader(name string) bool {
	return strings.EqualFold(name, "cookie") || strings.EqualFold(name, "set-cookie")
}

func applyCredentialInjection(req *http.Request, inj CredentialInjection, secret []byte, scrubber *secretScrubber) (string, error) {
	field := strings.TrimSpace(inj.FieldName)
	switch inj.Location {
	case TargetBearer:
		if field == "" {
			field = "Authorization"
		}
		prefix := inj.ValuePrefix
		if prefix == "" {
			prefix = "Bearer "
		}
		valBuf := make([]byte, len(prefix)+len(secret))
		copy(valBuf, prefix)
		copy(valBuf[len(prefix):], secret)
		req.Header.Set(field, string(valBuf))
		clear(valBuf)
		return field, nil

	case TargetHeader:
		valBuf := make([]byte, len(inj.ValuePrefix)+len(secret))
		copy(valBuf, inj.ValuePrefix)
		copy(valBuf[len(inj.ValuePrefix):], secret)
		req.Header.Set(field, string(valBuf))
		clear(valBuf)
		return field, nil

	case TargetBasic:
		if field == "" {
			field = "Authorization"
		}
		var credBuf []byte
		if inj.ValuePrefix != "" {
			credBuf = make([]byte, len(inj.ValuePrefix)+len(secret))
			copy(credBuf, inj.ValuePrefix)
			copy(credBuf[len(inj.ValuePrefix):], secret)
		} else if bytes.Contains(secret, []byte(":")) {
			credBuf = make([]byte, len(secret))
			copy(credBuf, secret)
		} else {
			credBuf = make([]byte, len(secret)+1)
			copy(credBuf, secret)
			credBuf[len(secret)] = ':'
		}
		encoded := make([]byte, base64.StdEncoding.EncodedLen(len(credBuf)))
		base64.StdEncoding.Encode(encoded, credBuf)
		rawEncoded := make([]byte, base64.RawStdEncoding.EncodedLen(len(credBuf)))
		base64.RawStdEncoding.Encode(rawEncoded, credBuf)
		clear(credBuf)
		scrubber.addSecret(encoded)
		scrubber.addSecret(rawEncoded)
		req.Header.Set(field, "Basic "+string(encoded))
		clear(encoded)
		clear(rawEncoded)
		return field, nil

	case TargetQueryParam:
		q := req.URL.Query()
		valBuf := make([]byte, len(inj.ValuePrefix)+len(secret))
		copy(valBuf, inj.ValuePrefix)
		copy(valBuf[len(inj.ValuePrefix):], secret)
		q.Set(field, string(valBuf))
		clear(valBuf)
		req.URL.RawQuery = q.Encode()
		return "", nil

	case TargetHostCryptoSign:
		sig, err := signHMACSHA256(secret, inj.CryptoSign)
		if err != nil {
			return "", err
		}
		req.Header.Set(field, inj.ValuePrefix+sig)
		return field, nil

	default:
		return "", fmt.Errorf("%w: unsupported target location %q", ErrInvalidEgressRequest, inj.Location)
	}
}

func signHMACSHA256(key []byte, spec *CryptoSignSpec) (string, error) {
	if spec == nil || spec.Algorithm != SigningHMACSHA256 {
		return "", ErrUnsupportedSigningAlgorithm
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(spec.CanonicalPayload)
	sum := mac.Sum(nil)
	defer clear(sum)

	switch spec.OutputEncoding {
	case "", EncodingHexLower:
		return hex.EncodeToString(sum), nil
	case EncodingBase64:
		return base64.StdEncoding.EncodeToString(sum), nil
	case EncodingBase64URL:
		return base64.RawURLEncoding.EncodeToString(sum), nil
	default:
		return "", fmt.Errorf("%w: unsupported encoding %q", ErrInvalidEgressRequest, spec.OutputEncoding)
	}
}

func clonePolicy(p egress.Policy) egress.Policy {
	out := p
	out.AllowedHosts = append([]string(nil), p.AllowedHosts...)
	out.AllowedCIDRs = append([]string(nil), p.AllowedCIDRs...)
	out.AllowedPorts = append([]int(nil), p.AllowedPorts...)
	out.SensitiveHeaders = append([]string(nil), p.SensitiveHeaders...)
	return out
}

func policyHostPortMap(p egress.Policy) (map[string]map[int]bool, error) {
	explicitPorts := make(map[int]bool, len(p.AllowedPorts))
	for _, port := range p.AllowedPorts {
		explicitPorts[port] = true
	}

	out := make(map[string]map[int]bool, len(p.AllowedHosts))
	for _, rawHost := range p.AllowedHosts {
		trimmed := strings.TrimSpace(rawHost)
		if trimmed == "" {
			continue
		}
		var hostPart string
		var explicitPort int
		hasPort := false
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			hostPart = trimmed[1 : len(trimmed)-1]
		} else if h, pStr, err := net.SplitHostPort(trimmed); err == nil {
			n, perr := strconv.Atoi(pStr)
			if perr != nil || n < 1 || n > 65535 {
				return nil, &egress.Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "egress_invalid_policy",
					Message:    fmt.Sprintf("invalid port in allowed_hosts %q", rawHost),
					Err:        egress.ErrInvalidPolicy,
				}
			}
			hostPart = h
			explicitPort = n
			hasPort = true
		} else {
			hostPart = trimmed
		}

		canon := canonicalHost(hostPart)
		if canon == "" {
			continue
		}
		ports, ok := out[canon]
		if !ok {
			ports = make(map[int]bool)
			out[canon] = ports
		}
		if hasPort {
			if len(explicitPorts) == 0 || explicitPorts[explicitPort] {
				ports[explicitPort] = true
			}
			continue
		}
		if len(explicitPorts) > 0 {
			for port := range explicitPorts {
				ports[port] = true
			}
		} else if p.RequireTLS {
			ports[443] = true
		} else {
			ports[80] = true
			ports[443] = true
		}
	}
	return out, nil
}

func parseCIDRPrefixes(raw []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(raw))
	for _, s := range raw {
		pfx, err := netip.ParsePrefix(strings.TrimSpace(s))
		if err != nil {
			return nil, &egress.Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "egress_invalid_policy",
				Message:    fmt.Sprintf("invalid allowed_cidrs entry %q: %v", s, err),
				Err:        egress.ErrInvalidPolicy,
			}
		}
		out = append(out, pfx.Masked())
	}
	return out, nil
}

func intersectPrefixLists(a, b []netip.Prefix) []netip.Prefix {
	seen := make(map[netip.Prefix]bool)
	var out []netip.Prefix
	for _, pa := range a {
		for _, pb := range b {
			if !pa.Overlaps(pb) {
				continue
			}
			narrower := pa
			if pb.Bits() > pa.Bits() {
				narrower = pb
			}
			if !seen[narrower] {
				seen[narrower] = true
				out = append(out, narrower)
			}
		}
	}
	return out
}

func intersectPolicies(policies []egress.Policy) (egress.Policy, error) {
	if len(policies) == 0 {
		return egress.Policy{}, egress.ErrHostDenied
	}
	if len(policies) == 1 {
		return clonePolicy(policies[0]), nil
	}

	merged := egress.Policy{
		AllowPrivateIP: true,
		RequireTLS:     false,
	}
	sensSeen := make(map[string]bool)
	var sensHeaders []string

	for _, p := range policies {
		if !p.AllowPrivateIP {
			merged.AllowPrivateIP = false
		}
		if p.RequireTLS {
			merged.RequireTLS = true
		}
		for _, h := range p.SensitiveHeaders {
			trimmed := strings.TrimSpace(h)
			if trimmed == "" {
				continue
			}
			lk := strings.ToLower(trimmed)
			if !sensSeen[lk] {
				sensSeen[lk] = true
				sensHeaders = append(sensHeaders, trimmed)
			}
		}
	}
	merged.SensitiveHeaders = sensHeaders

	// Intersect canonical (host, port) capabilities so one slot omitting AllowedPorts
	// or using "host" vs "host:443" never widens another slot's restrictions.
	commonHostPorts, err := policyHostPortMap(policies[0])
	if err != nil {
		return egress.Policy{}, err
	}
	anyHostMatch := false
	for _, p := range policies[1:] {
		nextMap, perr := policyHostPortMap(p)
		if perr != nil {
			return egress.Policy{}, perr
		}
		for host, curPorts := range commonHostPorts {
			otherPorts, exists := nextMap[host]
			if !exists {
				delete(commonHostPorts, host)
				continue
			}
			anyHostMatch = true
			for port := range curPorts {
				if !otherPorts[port] {
					delete(curPorts, port)
				}
			}
			if len(curPorts) == 0 {
				delete(commonHostPorts, host)
			}
		}
	}

	hosts := make([]string, 0, len(commonHostPorts))
	for h := range commonHostPorts {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	var intersectedHosts []string
	portSet := make(map[int]bool)
	for _, h := range hosts {
		ports := commonHostPorts[h]
		pList := make([]int, 0, len(ports))
		for port := range ports {
			pList = append(pList, port)
			portSet[port] = true
		}
		sort.Ints(pList)
		for _, port := range pList {
			intersectedHosts = append(intersectedHosts, net.JoinHostPort(h, strconv.Itoa(port)))
		}
	}
	if len(intersectedHosts) == 0 {
		if anyHostMatch {
			return egress.Policy{}, &egress.Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_host_denied",
				Message:    "multi-slot allowed_ports intersection is empty",
				Err:        errors.Join(egress.ErrHostDenied, egress.ErrPortDenied),
			}
		}
		return egress.Policy{}, &egress.Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    "multi-slot allowed_hosts intersection is empty",
			Err:        egress.ErrHostDenied,
		}
	}
	merged.AllowedHosts = intersectedHosts
	merged.AllowedPorts = make([]int, 0, len(portSet))
	for port := range portSet {
		merged.AllowedPorts = append(merged.AllowedPorts, port)
	}
	sort.Ints(merged.AllowedPorts)

	if !merged.AllowPrivateIP {
		merged.AllowedCIDRs = nil
	} else {
		commonCIDRs, cerr := parseCIDRPrefixes(policies[0].AllowedCIDRs)
		if cerr != nil {
			return egress.Policy{}, cerr
		}
		for _, p := range policies[1:] {
			nextCIDRs, nerr := parseCIDRPrefixes(p.AllowedCIDRs)
			if nerr != nil {
				return egress.Policy{}, nerr
			}
			commonCIDRs = intersectPrefixLists(commonCIDRs, nextCIDRs)
		}
		if len(commonCIDRs) == 0 {
			return egress.Policy{}, &egress.Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "egress_private_ip_blocked",
				Message:    "multi-slot allowed_cidrs intersection is empty",
				Err:        egress.ErrPrivateIPBlocked,
			}
		}
		merged.AllowedCIDRs = make([]string, len(commonCIDRs))
		for i, pfx := range commonCIDRs {
			merged.AllowedCIDRs[i] = pfx.String()
		}
	}
	return merged, nil
}

func sanitizeResponseHeaders(hdr http.Header, injectedHeaders []string, scrubber *secretScrubber) http.Header {
	out := make(http.Header, len(hdr))
	banned := map[string]bool{
		"cookie":              true,
		"set-cookie":          true,
		"authorization":       true,
		"proxy-authorization": true,
	}
	for _, h := range injectedHeaders {
		if trimmed := strings.ToLower(strings.TrimSpace(h)); trimmed != "" {
			banned[trimmed] = true
		}
	}
	for k, vals := range hdr {
		lk := strings.ToLower(strings.TrimSpace(k))
		if banned[lk] {
			continue
		}
		scrubbedVals := make([]string, len(vals))
		for i, v := range vals {
			scrubbedVals[i] = scrubber.scrubString(v)
		}
		out[k] = scrubbedVals
	}
	return out
}

func (b *BlindHostCredentialBroker) recordAudit(ctx context.Context, req EgressRequest, eventType string, meta map[string]any) error {
	id, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return err
	}
	orgID := req.OrgID
	actorKind := strings.TrimSpace(req.ActorKind)
	if actorKind == "" {
		actorKind = "extension_runtime"
	}
	actorID := strings.TrimSpace(req.ActorID)
	if actorID == "" {
		actorID = req.ConnectionID
	}
	entryPoint := strings.TrimSpace(req.EntryPoint)
	if entryPoint == "" {
		entryPoint = port.AuditEntryPointDataPlaneGuard
	}
	var reqIDPtr *string
	if rid := strings.TrimSpace(req.RequestID); rid != "" {
		reqIDPtr = &rid
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["connection_id"] = req.ConnectionID
	rawMeta, merr := json.Marshal(meta)
	if merr != nil {
		rawMeta = []byte(`{}`)
	}

	return b.audit.Append(ctx, port.AuditEvent{
		ID:           id,
		CreatedAtMs:  b.nowMs(),
		OrgID:        &orgID,
		ActorKind:    actorKind,
		ActorID:      actorID,
		EntryPoint:   entryPoint,
		EventType:    eventType,
		TargetType:   "provider_connection",
		TargetID:     req.ConnectionID,
		RequestID:    reqIDPtr,
		MetadataJSON: string(rawMeta),
	})
}

// secretScrubber holds ephemeral byte slices of injected secret encodings for a single
// broker Execute call, replaces any occurrence with [REDACTED_CREDENTIAL], and wipes all
// transient byte patterns upon return.
type secretScrubber struct {
	mu     sync.RWMutex
	tokens [][]byte
}

func (s *secretScrubber) hasTokens() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	has := len(s.tokens) > 0
	s.mu.RUnlock()
	return has
}

func (s *secretScrubber) trailingPrefixHold(scrubbed []byte) int {
	if s == nil || len(scrubbed) == 0 {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	holdLen := 0
	for _, tok := range s.tokens {
		maxK := len(tok) - 1
		if maxK > len(scrubbed) {
			maxK = len(scrubbed)
		}
		for k := maxK; k > holdLen; k-- {
			if bytes.HasSuffix(scrubbed, tok[:k]) {
				holdLen = k
				break
			}
		}
	}
	return holdLen
}

func (s *secretScrubber) addSecret(secret []byte) {
	if s == nil || len(secret) == 0 {
		return
	}
	rawStr := string(secret)
	qEsc := url.QueryEscape(rawStr)
	pEsc := url.PathEscape(rawStr)
	candidates := [][]byte{
		append([]byte(nil), secret...),
		[]byte(base64.StdEncoding.EncodeToString(secret)),
		[]byte(base64.RawStdEncoding.EncodeToString(secret)),
		[]byte(base64.URLEncoding.EncodeToString(secret)),
		[]byte(base64.RawURLEncoding.EncodeToString(secret)),
		[]byte(qEsc),
		[]byte(strings.ToLower(qEsc)),
		[]byte(pEsc),
		[]byte(strings.ToLower(pEsc)),
	}
	if jsonQuoted, err := json.Marshal(rawStr); err == nil && len(jsonQuoted) >= 2 {
		candidates = append(candidates, jsonQuoted[1:len(jsonQuoted)-1])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]bool, len(s.tokens)+len(candidates))
	for _, existing := range s.tokens {
		seen[string(existing)] = true
	}
	for _, cand := range candidates {
		if len(cand) == 0 || seen[string(cand)] {
			clear(cand)
			continue
		}
		seen[string(cand)] = true
		s.tokens = append(s.tokens, cand)
	}
	sort.Slice(s.tokens, func(i, j int) bool {
		return len(s.tokens[i]) > len(s.tokens[j])
	})
}

func (s *secretScrubber) scrubBytes(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	if s == nil || len(in) == 0 {
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.tokens) == 0 {
		return out
	}
	redacted := []byte(RedactedCredentialPlaceholder)
	for _, tok := range s.tokens {
		if len(tok) > 0 && bytes.Contains(out, tok) {
			out = bytes.ReplaceAll(out, tok, redacted)
		}
	}
	return out
}

func (s *secretScrubber) scrubString(in string) string {
	if s == nil || in == "" {
		return in
	}
	s.mu.RLock()
	hasTokens := len(s.tokens) > 0
	s.mu.RUnlock()
	if !hasTokens {
		return in
	}
	b := s.scrubBytes([]byte(in))
	res := string(b)
	clear(b)
	return res
}

var safeMatchedSentinels = []error{
	egress.ErrHostDenied,
	egress.ErrPrivateIPBlocked,
	egress.ErrMetadataBlocked,
	egress.ErrPortDenied,
	egress.ErrTLSRequired,
	egress.ErrRedirectBlocked,
	egress.ErrInvalidPolicy,
	ErrInvalidBrokerConfig,
	ErrInvalidEgressRequest,
	ErrProhibitedCookieInjection,
	ErrAudienceMismatch,
	ErrUnsupportedSigningAlgorithm,
	ErrResponseTooLarge,
	vault.ErrMissingUnsealCapability,
	port.ErrNotFound,
	port.ErrInvalidInput,
	context.Canceled,
	context.DeadlineExceeded,
}

type scrubbedError struct {
	msg       string
	sentinels []error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() []error {
	return e.sentinels
}

func collectSafeSentinels(err error) []error {
	var matched []error
	for _, s := range safeMatchedSentinels {
		if errors.Is(err, s) {
			matched = append(matched, s)
		}
	}
	return matched
}

func (s *secretScrubber) sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	rawMsg := err.Error()
	cleanMsg := s.scrubString(rawMsg)

	var uErr *url.Error
	if errors.As(err, &uErr) && uErr != nil && cleanMsg != rawMsg {
		innerSentinels := collectSafeSentinels(err)
		cleanURL := s.scrubString(uErr.URL)
		innerMsg := ""
		if uErr.Err != nil {
			innerMsg = s.scrubString(uErr.Err.Error())
		}
		var innerErr error = &scrubbedError{
			msg:       innerMsg,
			sentinels: innerSentinels,
		}
		out := &url.Error{
			Op:  uErr.Op,
			URL: cleanURL,
			Err: innerErr,
		}
		if out.Error() != cleanMsg && cleanMsg != "" {
			return &scrubbedError{
				msg:       cleanMsg,
				sentinels: append(innerSentinels, out),
			}
		}
		return out
	}

	if cleanMsg == rawMsg {
		return err
	}
	return &scrubbedError{
		msg:       cleanMsg,
		sentinels: collectSafeSentinels(err),
	}
}

func (s *secretScrubber) zeroize() {
	if s == nil {
		return
	}
	s.mu.Lock()
	for i := range s.tokens {
		clear(s.tokens[i])
		s.tokens[i] = nil
	}
	s.tokens = nil
	s.mu.Unlock()
}

type scrubbingStreamReader struct {
	rc        io.ReadCloser
	mu        sync.Mutex
	scrubber  *secretScrubber
	closeIdle func()
	pending   []byte
	outBuf    []byte
	readErr   error
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

var streamReadBufPool = sync.Pool{
	New: func() any {
		return new([32 * 1024]byte)
	},
}

func newScrubbingStreamReader(rc io.ReadCloser, scrubber *secretScrubber, closeIdle func()) io.ReadCloser {
	return &scrubbingStreamReader{
		rc:        rc,
		scrubber:  scrubber,
		closeIdle: closeIdle,
	}
}

func (r *scrubbingStreamReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	zeroSpins := 0
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return 0, io.ErrClosedPipe
		}
		if len(r.outBuf) > 0 {
			n := copy(p, r.outBuf)
			clear(r.outBuf[:n])
			r.outBuf = r.outBuf[n:]
			if len(r.outBuf) == 0 {
				r.outBuf = nil
			}
			var retErr error
			if len(r.outBuf) == 0 && len(r.pending) == 0 && r.readErr != nil {
				retErr = r.readErr
			}
			r.mu.Unlock()
			return n, retErr
		}
		if r.readErr != nil {
			err := r.readErr
			r.mu.Unlock()
			return 0, err
		}
		// Fast pass-through when zero secrets are registered (zero buffering).
		if !r.scrubber.hasTokens() {
			rc := r.rc
			r.mu.Unlock()
			n, err := rc.Read(p)
			if err != nil {
				r.mu.Lock()
				if r.closed {
					r.mu.Unlock()
					return 0, io.ErrClosedPipe
				}
				r.readErr = err
				r.mu.Unlock()
			}
			return n, err
		}
		rc := r.rc
		r.mu.Unlock()

		tmpPtr := streamReadBufPool.Get().(*[32 * 1024]byte)
		n, err := rc.Read(tmpPtr[:])

		r.mu.Lock()
		if r.closed {
			clear(tmpPtr[:n])
			clear(tmpPtr[:])
			streamReadBufPool.Put(tmpPtr)
			r.mu.Unlock()
			return 0, io.ErrClosedPipe
		}
		if n > 0 {
			r.pending = append(r.pending, tmpPtr[:n]...)
			clear(tmpPtr[:n])
		}
		clear(tmpPtr[:])
		streamReadBufPool.Put(tmpPtr)
		if err != nil {
			r.readErr = r.scrubber.sanitizeError(err)
		}
		r.processPendingLocked(err != nil)
		if len(r.outBuf) > 0 {
			copied := copy(p, r.outBuf)
			clear(r.outBuf[:copied])
			r.outBuf = r.outBuf[copied:]
			if len(r.outBuf) == 0 {
				r.outBuf = nil
			}
			var retErr error
			if len(r.outBuf) == 0 && len(r.pending) == 0 && r.readErr != nil {
				retErr = r.readErr
			}
			r.mu.Unlock()
			return copied, retErr
		}
		if r.readErr != nil {
			retErr := r.readErr
			r.mu.Unlock()
			return 0, retErr
		}
		r.mu.Unlock()

		if n == 0 {
			zeroSpins++
			if zeroSpins >= 16 {
				return 0, nil
			}
		}
	}
}

func (r *scrubbingStreamReader) processPendingLocked(atEOF bool) {
	if len(r.pending) == 0 {
		return
	}
	scrubbed := r.scrubber.scrubBytes(r.pending)
	clear(r.pending)
	r.pending = nil

	holdLen := 0
	if !atEOF {
		holdLen = r.scrubber.trailingPrefixHold(scrubbed)
	}

	emitLen := len(scrubbed) - holdLen
	if emitLen > 0 {
		r.outBuf = append(r.outBuf, scrubbed[:emitLen]...)
	}
	if holdLen > 0 {
		r.pending = append([]byte(nil), scrubbed[emitLen:]...)
	}
	clear(scrubbed)
}

func (r *scrubbingStreamReader) Close() error {
	r.closeOnce.Do(func() {
		if r.rc != nil {
			r.closeErr = r.rc.Close()
		}
		if r.closeIdle != nil {
			r.closeIdle()
		}
		r.mu.Lock()
		r.closed = true
		clear(r.pending)
		r.pending = nil
		clear(r.outBuf)
		r.outBuf = nil
		if r.scrubber != nil {
			r.scrubber.zeroize()
			r.scrubber = nil
		}
		r.mu.Unlock()
	})
	return r.closeErr
}

