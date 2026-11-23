// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"crypto/ed25519"
	"errors"
	"testing"
)

func TestStaticKEKProvider_DomainEd25519DerivationAndSigning(t *testing.T) {
	t.Parallel()

	rawKEK := make([]byte, KeySizeBytes)
	for i := range rawKEK {
		rawKEK[i] = byte(i + 1)
	}
	p1, err := NewStaticKEKProvider("kek_v1", rawKEK)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer p1.Close()

	p2, err := NewStaticKEKProvider("kek_v1", rawKEK)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider second instance: %v", err)
	}
	defer p2.Close()

	const domain = "tanpopo:builtin-official-root-ed25519:v1"
	activeID1, pubs1, err := p1.DeriveDomainEd25519PublicKeys(domain)
	if err != nil {
		t.Fatalf("DeriveDomainEd25519PublicKeys: %v", err)
	}
	activeID2, pubs2, err := p2.DeriveDomainEd25519PublicKeys(domain)
	if err != nil {
		t.Fatalf("DeriveDomainEd25519PublicKeys second instance: %v", err)
	}
	if activeID1 != "tanpopo-builtin:kek_v1" || activeID1 != activeID2 {
		t.Fatalf("unexpected active key id %q vs %q", activeID1, activeID2)
	}
	if !pubs1[activeID1].Equal(pubs2[activeID2]) {
		t.Fatalf("derived public keys differ across identical KEK instances")
	}

	payload := []byte("tanpopo:pkg:@tanpopo/openai:0.1.0:abc")
	sigKeyID, sig, err := p1.SignDomainEd25519(domain, payload)
	if err != nil {
		t.Fatalf("SignDomainEd25519: %v", err)
	}
	if sigKeyID != activeID1 {
		t.Fatalf("expected signing key id %q, got %q", activeID1, sigKeyID)
	}
	if !ed25519.Verify(pubs2[sigKeyID], payload, sig) {
		t.Fatalf("ed25519 verification failed across matching KEK instance")
	}

	// Domain separation must produce distinct keys.
	_, otherPubs, err := p1.DeriveDomainEd25519PublicKeys("tanpopo:other-domain:v1")
	if err != nil {
		t.Fatalf("DeriveDomainEd25519PublicKeys other domain: %v", err)
	}
	if ed25519.Verify(otherPubs[sigKeyID], payload, sig) {
		t.Fatalf("signature across distinct domains must not verify")
	}

	// Rotated KEK preserves verification of historical signatures even when loaded under a new key ID.
	newKEK := make([]byte, KeySizeBytes)
	for i := range newKEK {
		newKEK[i] = byte(250 - i)
	}
	rot, err := NewStaticKEKProvider("kek_v2", newKEK)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider rotated: %v", err)
	}
	defer rot.Close()
	if err := rot.RegisterKey("kek_prev_0", rawKEK, false); err != nil {
		t.Fatalf("RegisterKey kek_prev_0: %v", err)
	}
	_, rotPubs, err := rot.DeriveDomainEd25519PublicKeys(domain)
	if err != nil {
		t.Fatalf("DeriveDomainEd25519PublicKeys rotated: %v", err)
	}
	if !rotPubs["tanpopo-builtin:kek_prev_0"].Equal(pubs1[activeID1]) {
		t.Fatalf("expected renamed previous KEK key_prev_0 to derive identical public key")
	}

	// Empty domain fails closed.
	if _, _, err := p1.DeriveDomainEd25519PublicKeys(" "); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey for empty domain, got %v", err)
	}
	if _, _, err := p1.SignDomainEd25519(" ", payload); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey on SignDomainEd25519 empty domain, got %v", err)
	}

	// Closed provider refuses derivation and signing.
	p1.Close()
	if _, _, err := p1.DeriveDomainEd25519PublicKeys(domain); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey after Close, got %v", err)
	}
	if _, _, err := p1.SignDomainEd25519(domain, payload); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey on Sign after Close, got %v", err)
	}
}
