// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package installer

import (
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/security/trust"
)

func verifyStageProvenance(
	vm *ValidatedManifest,
	actualBundleSHA256, expectedBundleSHA256 string,
	sig ProvenanceSignature,
	keyring TrustedKeyring,
	trustedBuiltin bool,
) (*ProvenanceVerdict, error) {
	// Whenever a release/publisher signature is supplied, or for any external source,
	// run strict VerifyProvenance (CT-03 / CT-05) without bypass.
	if !trustedBuiltin || len(sig.PublisherOrRootSig) > 0 || vm == nil || !trust.IsReservedTanpopoNamespace(vm.Manifest.Metadata.ID) {
		return VerifyProvenance(vm, actualBundleSHA256, expectedBundleSHA256, sig, keyring)
	}

	// Binary-embedded official package with no offline release signature configured:
	// verify SHA-256 digest and any optional enterprise signature, and report
	// truthful unsigned provenance (signature_verified=false, origin_trust_tier=unverified_external).
	actualHex := strings.ToLower(strings.TrimSpace(actualBundleSHA256))
	expHex := strings.ToLower(strings.TrimSpace(expectedBundleSHA256))
	if expHex != "" {
		expBytes, err := hex.DecodeString(expHex)
		actualBytes, aerr := hex.DecodeString(actualHex)
		if err != nil || aerr != nil || len(expBytes) != 32 || len(actualBytes) != 32 || subtle.ConstantTimeCompare(expBytes, actualBytes) != 1 {
			return nil, fmt.Errorf("%w: bundle sha256 mismatch (expected %q, actual %q)", ErrDigestMismatch, expHex, actualHex)
		}
	}

	verdict := &ProvenanceVerdict{
		OriginTrustTier:   lockfile.TrustTierUnverifiedExternal,
		SignatureVerified: false,
		BundleSHA256:      actualHex,
	}
	if len(sig.EnterpriseSig) > 0 {
		if len(sig.EnterpriseSig) != ed25519.SignatureSize {
			return nil, fmt.Errorf("%w: invalid enterprise Ed25519 signature length", ErrInvalidSignature)
		}
		payload := SignatureSigningPayload(vm.Manifest.Metadata.ID, vm.Manifest.Metadata.Version, actualHex)
		entFP := ""
		for fp, pub := range keyring.EnterprisePublicKeys {
			if len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, payload, sig.EnterpriseSig) {
				entFP = fp
				break
			}
		}
		if entFP == "" {
			return nil, fmt.Errorf("%w: enterprise signature failed Ed25519 verification", ErrInvalidSignature)
		}
		verdict.EnterpriseSigned = true
		verdict.EnterpriseKeyFingerprint = entFP
		verdict.SigningKeyFingerprint = entFP
	}
	return verdict, nil
}

// ComputeKeyringEffectiveProvenance evaluates live keyring provenance for an installed/candidate version.
func ComputeKeyringEffectiveProvenance(canonicalName string, sigVerified bool, sigFP string, entSigned bool, entFP string, keyring trust.TrustedKeyring) (originTrust string, liveSigVerified bool, liveEntSigned bool, liveFP string) {
	derived := keyring.DerivePublicTrustTier(canonicalName, sigVerified, sigFP)
	liveSigVerified = derived == trust.TierOfficial || derived == trust.TierVerified
	liveEntSigned = keyring.DeriveEnterpriseSigned(sigVerified, entSigned, sigFP, entFP)
	if !liveSigVerified && liveEntSigned {
		liveEntFP := strings.ToLower(strings.TrimSpace(entFP))
		if liveEntFP == "" {
			liveEntFP = strings.ToLower(strings.TrimSpace(sigFP))
		}
		liveFP = liveEntFP
	} else if liveSigVerified {
		liveFP = strings.ToLower(strings.TrimSpace(sigFP))
	}
	return derived, liveSigVerified, liveEntSigned, liveFP
}
