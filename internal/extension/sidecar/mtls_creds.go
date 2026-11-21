// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package sidecar implements Tanpopo's Tier-3 Isolated Sidecar execution boundary
// using ConnectRPC over permission-restricted 0600 Unix domain sockets or
// short-lived ephemeral-CA mutual-TLS (AUTH-08 / ADR-0002 / TP-IMP-0042).
//
// Invariants:
//   - Tier-3 Sidecar ships disabled by default (Enabled == false fails closed).
//   - A Sidecar never receives the Host credential vault or plaintext secrets.
//   - Every mTLS handshake requires TLS 1.3, mutual client+server certificates signed
//     by Tanpopo's ephemeral internal CA, non-expired validity bounds, and exact
//     SPIFFE URI SAN identity matching before a single RPC frame is decoded.
//   - UDS transports reject symlinks, non-socket files, or permissions looser than 0600.
package sidecar

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultLeafCertTTL bounds short-lived rotating Tier-3 mTLS peer certificates.
	DefaultLeafCertTTL = 15 * time.Minute
	// DefaultCACertTTL bounds an ephemeral Core instance internal CA lifetime.
	DefaultCACertTTL = 24 * time.Hour
	// SPIFFETenantDomain is the trust domain used for internal Core<->Sidecar identities.
	SPIFFETenantDomain = "tanpopo.internal"
)

var (
	// ErrMTLSRejected indicates peer certificate or SPIFFE SAN verification failure (AUTH-08).
	ErrMTLSRejected = errors.New("sidecar: mutual-TLS identity verification failed")
	// ErrUnsafeUDSSocket indicates missing, symlinked, or overly permissive UDS permissions.
	ErrUnsafeUDSSocket = errors.New("sidecar: unix domain socket failed 0600/non-symlink security checks")
)

// EphemeralCA manages an in-memory, per-process ECDSA P-256 internal root CA
// issuing short-lived mutual-TLS leaf certificates with SPIFFE URI SANs (AUTH-08).
type EphemeralCA struct {
	mu       sync.RWMutex
	rootCert *x509.Certificate
	rootKey  *ecdsa.PrivateKey
	pool     *x509.CertPool
	now      func() time.Time
}

// NewEphemeralCA mints a fresh internal P-256 root CA for sidecar mutual TLS.
func NewEphemeralCA(ttl time.Duration, now func() time.Time) (*EphemeralCA, error) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if ttl <= 0 {
		ttl = DefaultCACertTTL
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("sidecar: generate ephemeral CA key: %w", err)
	}
	serial, err := randomSerialNumber()
	if err != nil {
		return nil, err
	}
	t0 := now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "tanpopo-ephemeral-sidecar-ca", Organization: []string{"Tanpopo Internal"}},
		NotBefore:             t0.Add(-30 * time.Second),
		NotAfter:              t0.Add(ttl),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("sidecar: sign ephemeral CA cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("sidecar: parse ephemeral CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &EphemeralCA{
		rootCert: cert,
		rootKey:  priv,
		pool:     pool,
		now:      now,
	}, nil
}

// RootPool returns a CertPool trusting only this ephemeral internal CA.
func (ca *EphemeralCA) RootPool() *x509.CertPool {
	if ca == nil {
		return nil
	}
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	return ca.pool
}

// IssueLeafCertificate issues a short-lived leaf TLS certificate bound to a SPIFFE URI SAN.
// Loopback hosts/IPs are added for local sidecar loopback listeners while identity checks
// bind strictly to the SPIFFE URI SAN.
func (ca *EphemeralCA) IssueLeafCertificate(spiffeID string, isServer bool, ttl time.Duration, extraIPs []net.IP, extraDNS []string) (*tls.Certificate, error) {
	if ca == nil || ca.rootCert == nil || ca.rootKey == nil {
		return nil, fmt.Errorf("%w: ephemeral CA unavailable", ErrMTLSRejected)
	}
	uri, err := validateSPIFFEURI(spiffeID)
	if err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = DefaultLeafCertTTL
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("sidecar: generate leaf key: %w", err)
	}
	serial, err := randomSerialNumber()
	if err != nil {
		return nil, err
	}
	ca.mu.RLock()
	rootCert := ca.rootCert
	rootKey := ca.rootKey
	nowFn := ca.now
	ca.mu.RUnlock()

	t0 := nowFn().UTC()
	eku := x509.ExtKeyUsageClientAuth
	if isServer {
		eku = x509.ExtKeyUsageServerAuth
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: spiffeID},
		NotBefore:             t0.Add(-15 * time.Second),
		NotAfter:              t0.Add(ttl),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{eku},
		BasicConstraintsValid: true,
		URIs:                  []*url.URL{uri},
		DNSNames:              append([]string{"localhost"}, extraDNS...),
		IPAddresses:           append([]net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback}, extraIPs...),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, rootCert, &priv.PublicKey, rootKey)
	if err != nil {
		return nil, fmt.Errorf("sidecar: sign leaf certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("sidecar: parse signed leaf: %w", err)
	}
	return &tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		Leaf:        leaf,
	}, nil
}

// RotatingCertSource caches and automatically refreshes short-lived SPIFFE leaf certificates
// before expiration without interrupting mTLS connections.
type RotatingCertSource struct {
	ca       *EphemeralCA
	spiffeID string
	isServer bool
	ttl      time.Duration
	now      func() time.Time

	mu   sync.Mutex
	cur  *tls.Certificate
	exp  time.Time
	gen  int
}

// NewRotatingCertSource creates an auto-rotating peer certificate provider.
func NewRotatingCertSource(ca *EphemeralCA, spiffeID string, isServer bool, ttl time.Duration) (*RotatingCertSource, error) {
	if ca == nil {
		return nil, fmt.Errorf("%w: CA required for rotating certificate source", ErrMTLSRejected)
	}
	if _, err := validateSPIFFEURI(spiffeID); err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = DefaultLeafCertTTL
	}
	return &RotatingCertSource{
		ca:       ca,
		spiffeID: spiffeID,
		isServer: isServer,
		ttl:      ttl,
		now:      ca.now,
	}, nil
}

// Certificate returns an active non-expired certificate, rotating cleanly prior to expiry.
func (s *RotatingCertSource) Certificate() (*tls.Certificate, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: certificate source unavailable", ErrMTLSRejected)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	// Rotate at 75% lifetime (remaining <= ttl/4) or if unset.
	refreshWindow := s.ttl / 4
	if refreshWindow < time.Second {
		refreshWindow = time.Second
	}
	if s.cur != nil && now.Add(refreshWindow).Before(s.exp) {
		return s.cur, nil
	}
	cert, err := s.ca.IssueLeafCertificate(s.spiffeID, s.isServer, s.ttl, nil, nil)
	if err != nil {
		return nil, err
	}
	s.cur = cert
	s.exp = cert.Leaf.NotAfter
	s.gen++
	return s.cur, nil
}

// Rotations returns how many times a fresh leaf cert has been minted.
func (s *RotatingCertSource) Rotations() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// BuildClientTLSConfig enforces TLS 1.3, mutual authentication with clientCert,
// root CA pinning to ca, and strict expectedServerSPIFFE SAN verification during handshake.
func BuildClientTLSConfig(ca *EphemeralCA, clientSrc *RotatingCertSource, expectedServerSPIFFE string) (*tls.Config, error) {
	if ca == nil || clientSrc == nil {
		return nil, fmt.Errorf("%w: client mTLS requires ephemeral CA and client certificate source", ErrMTLSRejected)
	}
	if _, err := validateSPIFFEURI(expectedServerSPIFFE); err != nil {
		return nil, err
	}
	pool := ca.RootPool()
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    pool,
		Time:       ca.now,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return clientSrc.Certificate()
		},
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyPeerLeafSPIFFE(rawCerts, pool, expectedServerSPIFFE, x509.ExtKeyUsageServerAuth, ca.now())
		},
	}, nil
}

// BuildServerTLSConfig enforces TLS 1.3, RequireAndVerifyClientCert against ca,
// and strict allowedClientSPIFFE SAN verification prior to decoding any ConnectRPC frame (NEG-2).
func BuildServerTLSConfig(ca *EphemeralCA, serverSrc *RotatingCertSource, allowedClientSPIFFE string) (*tls.Config, error) {
	if ca == nil || serverSrc == nil {
		return nil, fmt.Errorf("%w: server mTLS requires ephemeral CA and server certificate source", ErrMTLSRejected)
	}
	if _, err := validateSPIFFEURI(allowedClientSPIFFE); err != nil {
		return nil, err
	}
	pool := ca.RootPool()
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  pool,
		Time:       ca.now,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return serverSrc.Certificate()
		},
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyPeerLeafSPIFFE(rawCerts, pool, allowedClientSPIFFE, x509.ExtKeyUsageClientAuth, ca.now())
		},
	}, nil
}

func verifyPeerLeafSPIFFE(rawCerts [][]byte, roots *x509.CertPool, wantSPIFFE string, eku x509.ExtKeyUsage, at time.Time) error {
	if len(rawCerts) == 0 || roots == nil {
		return ErrMTLSRejected
	}
	certs := make([]*x509.Certificate, 0, len(rawCerts))
	for _, raw := range rawCerts {
		c, err := x509.ParseCertificate(raw)
		if err != nil {
			return fmt.Errorf("%w: parse peer certificate: %v", ErrMTLSRejected, err)
		}
		certs = append(certs, c)
	}
	leaf := certs[0]
	inter := x509.NewCertPool()
	for _, c := range certs[1:] {
		inter.AddCert(c)
	}
	opts := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: inter,
		CurrentTime:   at,
		KeyUsages:     []x509.ExtKeyUsage{eku},
	}
	if _, err := leaf.Verify(opts); err != nil {
		return fmt.Errorf("%w: peer cert verification: %v", ErrMTLSRejected, err)
	}
	for _, u := range leaf.URIs {
		if u != nil && u.String() == wantSPIFFE {
			return nil
		}
	}
	return fmt.Errorf("%w: missing expected SPIFFE SAN %q", ErrMTLSRejected, wantSPIFFE)
}

// VerifyUDSSocketPermissions verifies socketPath is a Unix domain socket (never a symlink)
// with permissions strictly restricted to owner 0600 on Unix platforms (CR-02 / AUTH-08).
func VerifyUDSSocketPermissions(socketPath string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(socketPath))
	if clean == "" || clean == "." {
		return "", fmt.Errorf("%w: socket path required", ErrUnsafeUDSSocket)
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return "", fmt.Errorf("%w: stat socket %q: %v", ErrUnsafeUDSSocket, clean, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: symlink forbidden at %q", ErrUnsafeUDSSocket, clean)
	}
	if info.Mode()&fs.ModeSocket == 0 {
		return "", fmt.Errorf("%w: %q is not a Unix domain socket", ErrUnsafeUDSSocket, clean)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			return "", fmt.Errorf("%w: socket permissions %04o on %q must be 0600", ErrUnsafeUDSSocket, perm, clean)
		}
	}
	return clean, nil
}

// ConfineSocketPath enforces that candidate resolves canonically (following any
// existing ancestor symlinks) strictly inside socketRoot.
func ConfineSocketPath(socketRoot, candidate string) (string, error) {
	socketRoot = strings.TrimSpace(socketRoot)
	candidate = strings.TrimSpace(candidate)
	if socketRoot == "" {
		return "", fmt.Errorf("%w: sidecar socket directory not configured", ErrUnsafeUDSSocket)
	}
	if candidate == "" {
		return "", fmt.Errorf("%w: empty sidecar socket path", ErrUnsafeUDSSocket)
	}
	evalRoot, err := evalPathWithExistingAncestor(socketRoot)
	if err != nil {
		return "", fmt.Errorf("%w: resolve sidecar socket dir: %v", ErrUnsafeUDSSocket, err)
	}
	evalTarget, err := evalPathWithExistingAncestor(candidate)
	if err != nil {
		return "", fmt.Errorf("%w: resolve sidecar socket path: %v", ErrUnsafeUDSSocket, err)
	}
	rel, err := filepath.Rel(evalRoot, evalTarget)
	if err != nil || rel == "" || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: sidecar socket %q escapes confined socket dir", ErrUnsafeUDSSocket, candidate)
	}
	return evalTarget, nil
}

func evalPathWithExistingAncestor(p string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return "", err
	}
	resolvedParent, perr := evalPathWithExistingAncestor(parent)
	if perr != nil {
		return "", perr
	}
	return filepath.Join(resolvedParent, filepath.Base(abs)), nil
}

func validateSPIFFEURI(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "spiffe://"+SPIFFETenantDomain+"/") {
		return nil, fmt.Errorf("%w: SPIFFE ID %q must belong to spiffe://%s/", ErrMTLSRejected, raw, SPIFFETenantDomain)
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme != "spiffe" || u.Host != SPIFFETenantDomain || strings.Trim(u.Path, "/") == "" {
		return nil, fmt.Errorf("%w: malformed SPIFFE URI %q", ErrMTLSRejected, raw)
	}
	return u, nil
}

func randomSerialNumber() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("sidecar: generate certificate serial: %w", err)
	}
	if n.Sign() <= 0 {
		return big.NewInt(1), nil
	}
	return n, nil
}
