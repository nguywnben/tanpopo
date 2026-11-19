// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
