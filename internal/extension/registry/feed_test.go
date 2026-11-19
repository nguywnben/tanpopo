// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func signedEnvelope(t *testing.T, kind FeedKind, serial int64, expires time.Time, entries string, keyID string, private ed25519.PrivateKey) []byte {
	t.Helper()
	payload, err := json.Marshal(struct {
		VersionSerial int64           `json:"version_serial"`
		ExpiresAt     time.Time       `json:"expires_at"`
		Entries       json.RawMessage `json:"entries"`
	}{serial, expires, json.RawMessage(entries)})
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(private, signingPayload(kind, payload))
	raw, err := json.Marshal(SignedFeed{Payload: payload, KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(sig)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestVerifyFeedChecksSignatureExpiryAndMonotonicSerial(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	keys := Keyring{"root-1": pub}
	valid := signedEnvelope(t, FeedIndex, 2, now.Add(time.Hour), `[]`, "root-1", private)
	feed, err := VerifyFeed(FeedIndex, valid, keys, 1, now)
	if err != nil || feed.VersionSerial != 2 || feed.Kind != FeedIndex {
		t.Fatalf("valid signed feed rejected: feed=%+v err=%v", feed, err)
	}
	if _, err := VerifyFeed(FeedIndex, valid, keys, 2, now); !errors.Is(err, ErrFeedRollback) {
		t.Fatalf("serial replay must fail with rollback error, got %v", err)
	}
	if _, err := VerifyFeed(FeedIndex, signedEnvelope(t, FeedIndex, 3, now, `[]`, "root-1", private), keys, 1, now); !errors.Is(err, ErrFeedExpired) {
		t.Fatalf("expired feed must fail, got %v", err)
	}
	wrongKind := signedEnvelope(t, FeedRevocation, 3, now.Add(time.Hour), `[]`, "root-1", private)
	if _, err := VerifyFeed(FeedIndex, wrongKind, keys, 1, now); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("cross-feed signature substitution must fail, got %v", err)
	}
	forged := append([]byte(nil), valid...)
	for i := range forged {
		if forged[i] == '2' {
			forged[i] = '3'
			break
		}
	}
	if _, err := VerifyFeed(FeedIndex, forged, keys, 1, now); err == nil {
		t.Fatal("tampered feed must fail verification")
	}
}

func TestVerifyFeedRejectsMalformedEnvelopeAndUnknownKey(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	raw := signedEnvelope(t, FeedRevocation, 1, now.Add(time.Hour), `[]`, "known", private)
	if _, err := VerifyFeed(FeedRevocation, raw, nil, 0, now); !errors.Is(err, ErrUnknownSigningKey) {
		t.Fatalf("unknown key must fail, got %v", err)
	}
	for _, invalid := range [][]byte{nil, []byte(`{}`), append(raw, []byte(` {}`)...), make([]byte, MaxFeedBytes+1)} {
		if _, err := VerifyFeed(FeedRevocation, invalid, Keyring{"known": pub}, 0, now); err == nil {
			t.Fatalf("invalid envelope accepted (%d bytes)", len(invalid))
		}
	}
}

func TestVerifyDigest(t *testing.T) {
	data := []byte("signed package bytes")
	digest := sha256.Sum256(data)
	if err := VerifyDigest(hex.EncodeToString(digest[:]), digest); err != nil {
		t.Fatalf("matching immutable digest rejected: %v", err)
	}
	for _, value := range []string{fmt.Sprintf("%x", make([]byte, 32)), "SHA256", ""} {
		if err := VerifyDigest(value, digest); err == nil {
			t.Fatalf("mismatching or malformed digest accepted: %q", value)
		}
	}
}

func TestFeedEntryValidationRequiresImmutableOCIAndKnownSeverity(t *testing.T) {
	index := &VerifiedFeed{Kind: FeedIndex, Entries: json.RawMessage(`[{"package_slug":"acme/adapter","version":"1.2.3","digest":"` + fmt.Sprintf("%064x", 1) + `","oci_uri":"oci://registry.example.com/acme/adapter@sha256:` + fmt.Sprintf("%064x", 1) + `"}]`)}
	entries, err := DecodeIndexEntries(index)
	if err != nil || len(entries) != 1 {
		t.Fatalf("valid pinned index rejected: entries=%+v err=%v", entries, err)
	}
	index.Entries = json.RawMessage(`[ {"package_slug":"acme/adapter","version":"1.2.3","digest":"` + fmt.Sprintf("%064x", 1) + `","oci_uri":"oci://registry.example.com/acme/adapter:latest"} ]`)
	if _, err := DecodeIndexEntries(index); err == nil {
		t.Fatal("mutable OCI reference accepted")
	}
	revocations := &VerifiedFeed{Kind: FeedRevocation, Entries: json.RawMessage(`[{"package_slug":"acme/adapter","version_or_sha256":"1.2.3","severity":"CRITICAL_MALICIOUS","reason":"confirmed malware"}]`)}
	if got, err := DecodeRevocationEntries(revocations); err != nil || len(got) != 1 {
		t.Fatalf("valid revocation rejected: entries=%+v err=%v", got, err)
	}
	revocations.Entries = json.RawMessage(`[ {"package_slug":"acme/adapter","version_or_sha256":"1.2.3","severity":"UNKNOWN","reason":"x"} ]`)
	if _, err := DecodeRevocationEntries(revocations); err == nil {
		t.Fatal("unknown advisory severity accepted")
	}
}

type registryRoundTripper func(*http.Request) (*http.Response, error)

func (fn registryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestOCIClientPinsDigestAndScopesCredentialsToRegistry(t *testing.T) {
	body := []byte("verified artifact")
	digest := sha256.Sum256(body)
	client := &OCIClient{HTTPClient: &http.Client{Transport: registryRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "registry.example.com" || req.URL.Path != "/v2/team/adapter/blobs/sha256:"+hex.EncodeToString(digest[:]) {
			t.Fatalf("unexpected registry request URL: %s", req.URL)
		}
		user, password, ok := req.BasicAuth()
		if !ok || user != "bot" || password != "token" {
			t.Fatalf("registry credentials missing or malformed")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
	})}}
	ref := "oci://registry.example.com/team/adapter@sha256:" + hex.EncodeToString(digest[:])
	got, err := client.Pull(context.Background(), ref, &RegistryCredentials{RegistryHost: "registry.example.com", Username: "bot", Password: "token"})
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("pinned artifact pull failed: got=%q err=%v", got, err)
	}
	if _, err := client.Pull(context.Background(), ref, &RegistryCredentials{RegistryHost: "other.example.com", Username: "bot", Password: "token"}); !errors.Is(err, ErrRegistryAuthScope) {
		t.Fatalf("credentials for another registry must be rejected, got %v", err)
	}
	if _, err := client.Pull(context.Background(), "oci://registry.example.com/team/adapter:latest", nil); !errors.Is(err, ErrInvalidOCIReference) {
		t.Fatalf("mutable tag must be rejected, got %v", err)
	}
}

func TestOCIClientRejectsDigestMismatchAndOversize(t *testing.T) {
	body := []byte("unexpected bytes")
	wrongDigest := sha256.Sum256([]byte("expected bytes"))
	client := &OCIClient{MaxBytes: int64(len(body)), HTTPClient: &http.Client{Transport: registryRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
	})}}
	ref := "oci://registry.example.com/team/adapter@sha256:" + hex.EncodeToString(wrongDigest[:])
	if _, err := client.Pull(context.Background(), ref, nil); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	client.MaxBytes = int64(len(body) - 1)
	actualDigest := sha256.Sum256(body)
	if _, err := client.Pull(context.Background(), "oci://registry.example.com/team/adapter@sha256:"+hex.EncodeToString(actualDigest[:]), nil); !errors.Is(err, ErrArtifactTooLarge) {
		t.Fatalf("oversized blob must fail, got %v", err)
	}
}

type fakeRevocationStore struct {
	serial  int64
	entries []RevocationEntry
	pending []RevocationTarget
}

func (s *fakeRevocationStore) LastRevocationSerial(context.Context) (int64, error) { return s.serial, nil }
func (s *fakeRevocationStore) ApplyRevocationFeed(_ context.Context, serial int64, _ time.Time, entries []RevocationEntry) error {
	s.serial = serial
	s.entries = append([]RevocationEntry(nil), entries...)
	for _, entry := range entries {
		if entry.Severity == "CRITICAL" || entry.Severity == "CRITICAL_MALICIOUS" {
			s.pending = append(s.pending, RevocationTarget{ActionID: "action-1", InstallationID: "ins_1", PackageSlug: entry.PackageSlug, VersionOrSHA256: entry.VersionOrSHA256, Severity: entry.Severity, Reason: entry.Reason})
		}
	}
	return nil
}
func (s *fakeRevocationStore) PendingRevocationActions(context.Context) ([]RevocationTarget, error) {
	return append([]RevocationTarget(nil), s.pending...), nil
}
func (s *fakeRevocationStore) CompleteRevocationAction(_ context.Context, id string) error {
	filtered := s.pending[:0]
	for _, target := range s.pending {
		if target.ActionID != id {
			filtered = append(filtered, target)
		}
	}
	s.pending = filtered
	return nil
}

type fakeRevocationActions struct {
	events          []string
	failStreamOnce  bool
}

func (a *fakeRevocationActions) QuarantineInstallation(_ context.Context, target RevocationTarget) error {
	a.events = append(a.events, "quarantine:"+target.InstallationID)
	return nil
}
func (a *fakeRevocationActions) SeverInstallationStreams(_ context.Context, target RevocationTarget) error {
	a.events = append(a.events, "sever:"+target.InstallationID)
	if a.failStreamOnce {
		a.failStreamOnce = false
		return errors.New("stream fence unavailable")
	}
	return nil
}
func (a *fakeRevocationActions) EmitINC03(_ context.Context, target RevocationTarget) error {
	a.events = append(a.events, "inc03:"+target.InstallationID)
	return nil
}

func TestRevocationWatcherPersistsAndRetriesCriticalEnforcement(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	raw := signedEnvelope(t, FeedRevocation, 4, now.Add(time.Hour), `[{"package_slug":"community/bad-adapter","version_or_sha256":"1.0.0","severity":"CRITICAL_MALICIOUS","reason":"confirmed malicious package"}]`, "root", private)
	store := &fakeRevocationStore{serial: 3}
	actions := &fakeRevocationActions{failStreamOnce: true}
	watcher := &RevocationWatcher{Keys: Keyring{"root": private.Public().(ed25519.PublicKey)}, Store: store, Actions: actions, Now: func() time.Time { return now }}
	if err := watcher.ImportVerifiedBytes(context.Background(), raw); err == nil {
		t.Fatal("stream fence failure must keep the durable action pending")
	}
	if store.serial != 4 || len(store.pending) != 1 {
		t.Fatalf("verified feed/action not durably staged: serial=%d pending=%+v", store.serial, store.pending)
	}
	if err := watcher.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.pending) != 0 {
		t.Fatalf("action was not completed after retry: %+v", store.pending)
	}
	want := []string{"quarantine:ins_1", "sever:ins_1", "quarantine:ins_1", "sever:ins_1", "inc03:ins_1"}
	if fmt.Sprint(actions.events) != fmt.Sprint(want) {
		t.Fatalf("unexpected enforcement order: got %v want %v", actions.events, want)
	}
}
