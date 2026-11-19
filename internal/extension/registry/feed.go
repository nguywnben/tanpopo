// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package registry verifies signed, versioned marketplace and revocation feeds.
package registry

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const MaxFeedBytes = 8 << 20

var (
	ErrInvalidFeed       = errors.New("registry: invalid feed")
	ErrInvalidSignature  = errors.New("registry: invalid feed signature")
	ErrFeedExpired       = errors.New("registry: feed expired")
	ErrFeedRollback      = errors.New("registry: feed serial did not increase")
	ErrUnknownSigningKey = errors.New("registry: unknown feed signing key")
)

type FeedKind string

const (
	FeedIndex      FeedKind = "index"
	FeedRevocation FeedKind = "revocations"
)

// SignedFeed is a detached signature over the exact payload bytes. Signatures
// are domain separated by FeedKind to prevent cross-feed substitution.
type SignedFeed struct {
	Payload   json.RawMessage `json:"payload"`
	KeyID     string          `json:"key_id"`
	Signature string          `json:"signature"`
}

type FeedPayload struct {
	VersionSerial int64           `json:"version_serial"`
	ExpiresAt     time.Time       `json:"expires_at"`
	Entries       json.RawMessage `json:"entries"`
}

type IndexEntry struct {
	PackageSlug string `json:"package_slug"`
	Version     string `json:"version"`
	Digest      string `json:"digest"`
	OCIURI      string `json:"oci_uri"`
}

type RevocationEntry struct {
	PackageSlug      string `json:"package_slug"`
	VersionOrSHA256  string `json:"version_or_sha256"`
	Severity         string `json:"severity"`
	Reason           string `json:"reason"`
}

type Keyring map[string]ed25519.PublicKey

// VerifiedFeed contains a validated payload and its signed bytes. Importers
// must persist the serial and feed contents in one transaction before exposing
// the new feed to readers.
type VerifiedFeed struct {
	Kind          FeedKind
	VersionSerial int64
	ExpiresAt     time.Time
	Entries       json.RawMessage
	Payload       json.RawMessage
	KeyID         string
}

// VerifyFeed checks envelope shape, trusted key, signature, expiry, and strict
// anti-rollback before returning any data for import. The caller supplies the
// last serial from durable storage and its trusted clock.
func VerifyFeed(kind FeedKind, raw []byte, keys Keyring, lastSerial int64, now time.Time) (*VerifiedFeed, error) {
	if kind != FeedIndex && kind != FeedRevocation {
		return nil, fmt.Errorf("%w: unsupported feed kind", ErrInvalidFeed)
	}
	if len(raw) == 0 || len(raw) > MaxFeedBytes {
		return nil, fmt.Errorf("%w: feed size outside limits", ErrInvalidFeed)
	}
	var envelope SignedFeed
	if err := decodeOne(raw, &envelope); err != nil || len(envelope.Payload) == 0 || strings.TrimSpace(envelope.KeyID) == "" || envelope.Signature == "" {
		return nil, fmt.Errorf("%w: malformed signed envelope", ErrInvalidFeed)
	}
	key, ok := keys[envelope.KeyID]
	if !ok {
		return nil, ErrUnknownSigningKey
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: malformed trusted key", ErrInvalidFeed)
	}
	sig, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(key, signingPayload(kind, envelope.Payload), sig) {
		return nil, ErrInvalidSignature
	}
	var payload FeedPayload
	if err := decodeOne(envelope.Payload, &payload); err != nil || payload.VersionSerial <= 0 || len(payload.Entries) == 0 || !json.Valid(payload.Entries) {
		return nil, fmt.Errorf("%w: malformed payload", ErrInvalidFeed)
	}
	if payload.VersionSerial <= lastSerial {
		return nil, fmt.Errorf("%w: got %d after %d", ErrFeedRollback, payload.VersionSerial, lastSerial)
	}
	if payload.ExpiresAt.IsZero() || !now.Before(payload.ExpiresAt) {
		return nil, ErrFeedExpired
	}
	return &VerifiedFeed{Kind: kind, VersionSerial: payload.VersionSerial, ExpiresAt: payload.ExpiresAt, Entries: append(json.RawMessage(nil), payload.Entries...), Payload: append(json.RawMessage(nil), envelope.Payload...), KeyID: envelope.KeyID}, nil
}

func signingPayload(kind FeedKind, payload []byte) []byte {
	return append([]byte("tanpopo:marketplace:"+string(kind)+":v1\x00"), payload...)
}

func decodeOne(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing JSON value")
	}
	return nil
}

// VerifyDigest compares a downloaded immutable OCI blob with its expected
// SHA-256 digest using constant-time comparison.
func VerifyDigest(expectedHex string, actual [32]byte) error {
	expected, err := decodeSHA256(expectedHex)
	if err != nil || subtle.ConstantTimeCompare(expected, actual[:]) != 1 {
		return fmt.Errorf("registry: OCI artifact digest mismatch")
	}
	return nil
}

// DecodeIndexEntries validates the signed index payload's package identity and
// immutable artifact pin before it is passed to an installer.
func DecodeIndexEntries(feed *VerifiedFeed) ([]IndexEntry, error) {
	if feed == nil || feed.Kind != FeedIndex {
		return nil, fmt.Errorf("%w: index feed required", ErrInvalidFeed)
	}
	var entries []IndexEntry
	if err := decodeOne(feed.Entries, &entries); err != nil || entries == nil {
		return nil, fmt.Errorf("%w: malformed index entries", ErrInvalidFeed)
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.PackageSlug) == "" || strings.TrimSpace(entry.Version) == "" || decodeDigest(entry.Digest) != nil || !validOCIURI(entry.OCIURI, entry.Digest) {
			return nil, fmt.Errorf("%w: index entry must include package, version, and digest-pinned OCI URI", ErrInvalidFeed)
		}
	}
	return entries, nil
}

// DecodeRevocationEntries validates severities before any state-changing
// importer is called; advisory text remains data and is not trusted as policy.
func DecodeRevocationEntries(feed *VerifiedFeed) ([]RevocationEntry, error) {
	if feed == nil || feed.Kind != FeedRevocation {
		return nil, fmt.Errorf("%w: revocation feed required", ErrInvalidFeed)
	}
	var entries []RevocationEntry
	if err := decodeOne(feed.Entries, &entries); err != nil || entries == nil {
		return nil, fmt.Errorf("%w: malformed revocation entries", ErrInvalidFeed)
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.PackageSlug) == "" || strings.TrimSpace(entry.VersionOrSHA256) == "" || strings.TrimSpace(entry.Reason) == "" {
			return nil, fmt.Errorf("%w: incomplete revocation entry", ErrInvalidFeed)
		}
		switch entry.Severity {
		case "CRITICAL", "CRITICAL_MALICIOUS", "HIGH", "VULNERABLE_UPGRADE_ADVISED":
		default:
			return nil, fmt.Errorf("%w: unsupported revocation severity", ErrInvalidFeed)
		}
	}
	return entries, nil
}

// FetchSignedFeed fetches one HTTPS resource through the caller-provided HTTP
// client (which must use the configured SSRFGuard transport). Redirects are
// rejected to prevent moving a trusted feed request to another authority.
func FetchSignedFeed(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: guarded HTTP client is required", ErrInvalidFeed)
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("%w: feed URL must be an HTTPS URL without credentials or fragment", ErrInvalidFeed)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid feed request", ErrInvalidFeed)
	}
	guarded := *client
	guarded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := guarded.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry: feed server returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxFeedBytes {
		return nil, fmt.Errorf("%w: feed exceeds size limit", ErrInvalidFeed)
	}
	return body, nil
}

func validOCIURI(raw, digest string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "oci" && u.User == nil && u.Hostname() != "" && strings.Trim(u.Path, "/") != "" && strings.HasSuffix(raw, "@sha256:"+digest)
}

func decodeDigest(s string) error {
	_, err := decodeSHA256(s)
	return err
}

func decodeSHA256(s string) ([]byte, error) {
	if len(s) != 64 || strings.ToLower(s) != s {
		return nil, fmt.Errorf("invalid lowercase sha256 digest")
	}
	decoded, err := hex.DecodeString(s)
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("invalid sha256 digest")
	}
	return decoded, nil
}
