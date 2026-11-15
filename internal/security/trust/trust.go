// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package trust provides the shared Tier-3 cryptographic extension provenance
// classifier and TP-BR-0202 organization plugin trust policy evaluator consumed
// identically by both ConfigSnapshot (Tier 4) and Control/Data Plane domain
// services (Tier 5) so trust decisions never drift across layers.
package trust

import (
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Canonical package trust tiers matching CR-03 and tanpopo.lock.
const (
	TierOfficial           = "official"
	TierVerified           = "verified"
	TierUnverifiedExternal = "unverified_external"
)

// Canonical organization trust denial sentinel shared with installer and catalog.
var (
	ErrOrgTrustPolicyDenied = errors.New("installer: org_trust_policy_denied (403)")
	ReservedNamespaceRe     = regexp.MustCompile(`^@tanpopo(-[a-z0-9]+)?/`)
)

// IsReservedTanpopoNamespace reports whether pkgID matches ^@tanpopo(-[a-z0-9]+)?/ (CT-05).
func IsReservedTanpopoNamespace(pkgID string) bool {
	return ReservedNamespaceRe.MatchString(strings.TrimSpace(pkgID))
}

// NormalizeTrustTier normalizes and validates package provenance trust tiers.
func NormalizeTrustTier(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case TierOfficial:
		return TierOfficial, nil
	case TierVerified:
		return TierVerified, nil
	case TierUnverifiedExternal, "unverified-external":
		return TierUnverifiedExternal, nil
	default:
		return "", fmt.Errorf("trust: unknown trust tier %q", raw)
	}
}

// PublicKeyFingerprint computes the deterministic 64-hex SHA-256 fingerprint of an Ed25519 public key.
func PublicKeyFingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

// TrustedKeyring holds the offline Ed25519 verification keys across Tanpopo's
// three provenance axes (Root -> official, Publisher -> verified, Enterprise -> enterprise_signed).
type TrustedKeyring struct {
	RootPublicKeys       map[string]ed25519.PublicKey
	PublisherPublicKeys  map[string]ed25519.PublicKey
	EnterprisePublicKeys map[string]ed25519.PublicKey
}

// Clone returns a deep copy of keyring maps and Ed25519 public key slices.
func (keyring TrustedKeyring) Clone() TrustedKeyring {
	return TrustedKeyring{
		RootPublicKeys:       cloneKeyMap(keyring.RootPublicKeys),
		PublisherPublicKeys:  cloneKeyMap(keyring.PublisherPublicKeys),
		EnterprisePublicKeys: cloneKeyMap(keyring.EnterprisePublicKeys),
	}
}

func cloneKeyMap(src map[string]ed25519.PublicKey) map[string]ed25519.PublicKey {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]ed25519.PublicKey, len(src))
	for k, pub := range src {
		if len(pub) > 0 {
			out[k] = append(ed25519.PublicKey(nil), pub...)
		}
	}
	return out
}

// DerivePublicTrustTier classifies a verified artifact against the current public trust keys.
// It fails closed to unverified_external whenever signatureVerified is false or the key
// fingerprint is no longer present in RootPublicKeys/PublisherPublicKeys.
func (keyring TrustedKeyring) DerivePublicTrustTier(canonicalName string, signatureVerified bool, fingerprint string) string {
	fp := strings.ToLower(strings.TrimSpace(fingerprint))
	if !signatureVerified || fp == "" {
		return TierUnverifiedExternal
	}
	for _, pub := range keyring.RootPublicKeys {
		if len(pub) == ed25519.PublicKeySize && PublicKeyFingerprint(pub) == fp {
			return TierOfficial
		}
	}
	if !IsReservedTanpopoNamespace(canonicalName) {
		for _, pub := range keyring.PublisherPublicKeys {
			if len(pub) == ed25519.PublicKeySize && PublicKeyFingerprint(pub) == fp {
				return TierVerified
			}
		}
	}
	return TierUnverifiedExternal
}

// DeriveEnterpriseSigned verifies persisted enterprise_signed provenance against the active
// EnterprisePublicKeys in keyring so withdrawing or rotating an enterprise key revokes trust.
func (keyring TrustedKeyring) DeriveEnterpriseSigned(signatureVerified, enterpriseSigned bool, signingFingerprint, enterpriseFingerprint string) bool {
	if !enterpriseSigned || len(keyring.EnterprisePublicKeys) == 0 {
		return false
	}
	entFP := strings.ToLower(strings.TrimSpace(enterpriseFingerprint))
	if entFP == "" && !signatureVerified {
		// Single-signed enterprise artifacts store the enterprise key fingerprint in signing_key_fingerprint.
		entFP = strings.ToLower(strings.TrimSpace(signingFingerprint))
	}
	if entFP == "" {
		// Pre-0008 dual-signed rows lack enterprise_key_fingerprint; require non-empty EnterprisePublicKeys.
		return signatureVerified
	}
	for _, pub := range keyring.EnterprisePublicKeys {
		if len(pub) == ed25519.PublicKeySize && PublicKeyFingerprint(pub) == entFP {
			return true
		}
	}
	return false
}

// IsUsableExtensionState reports whether an extension_installations.state allows serving traffic (SM-01).
func IsUsableExtensionState(state string) bool {
	switch strings.TrimSpace(state) {
	case "ENABLED_ACTIVE", "ROLLED_BACK", "UPDATING_PENDING_RECONSENT":
		return true
	default:
		return false
	}
}

// CheckOrgMinTrustPolicy enforces organizations.min_plugin_trust against derived provenance (CT-20 / CR-03).
func CheckOrgMinTrustPolicy(minPluginTrust, originTrustTier string, enterpriseSigned bool) error {
	normTier, err := NormalizeTrustTier(originTrustTier)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOrgTrustPolicyDenied, err)
	}
	switch minPluginTrust {
	case port.MinTrustOfficialOnly:
		if normTier == TierOfficial {
			return nil
		}
	case port.MinTrustVerifiedOnly:
		if normTier == TierOfficial || normTier == TierVerified {
			return nil
		}
	case port.MinTrustVerifiedOrEnterpriseSigned:
		if normTier == TierOfficial || normTier == TierVerified || enterpriseSigned {
			return nil
		}
	case port.MinTrustAnyApproved:
		return nil
	default:
		return fmt.Errorf("%w: unknown organization min_plugin_trust policy %q", ErrOrgTrustPolicyDenied, minPluginTrust)
	}
	return fmt.Errorf("%w: org requires %s (got origin_trust_tier=%s, enterprise_signed=%v)", ErrOrgTrustPolicyDenied, minPluginTrust, normTier, enterpriseSigned)
}

// OrgExtensionTrustInput captures active extension provenance and org policy state for TP-BR-0202 evaluation.
type OrgExtensionTrustInput struct {
	MinPluginTrust           string
	CanonicalName            string
	ActiveVersionID          string
	SignatureVerified        bool
	SigningKeyFingerprint    string
	EnterpriseSigned         bool
	EnterpriseKeyFingerprint string
	OrgPolicyEnabled         sql.NullInt64
	OrgOwnerOptIn            bool
}

// OrgExtensionTrustVerdict is the canonical TP-BR-0202 evaluation shared by snapshot and catalog.
type OrgExtensionTrustVerdict struct {
	HasActiveVersion           bool
	DisabledByOrgPolicy        bool
	DerivedPublicTrust         string
	DerivedEnterpriseSigned    bool
	MinTrustErr                error
	MinTrustDenied             bool
	HasValidOrgOwnerOptIn      bool
	RequiresOrgOwnerChildOptIn bool
}

// EvaluateOrgExtensionTrust computes the authoritative trust posture for an active extension version
// against the current TrustedKeyring and org_extension_policies (ignoring stale extension_packages.origin_trust_tier).
func EvaluateOrgExtensionTrust(keyring TrustedKeyring, in OrgExtensionTrustInput) OrgExtensionTrustVerdict {
	hasActive := strings.TrimSpace(in.ActiveVersionID) != ""
	disabled := in.OrgPolicyEnabled.Valid && in.OrgPolicyEnabled.Int64 == 0
	derived := keyring.DerivePublicTrustTier(in.CanonicalName, in.SignatureVerified, in.SigningKeyFingerprint)
	derivedEnt := keyring.DeriveEnterpriseSigned(in.SignatureVerified, in.EnterpriseSigned, in.SigningKeyFingerprint, in.EnterpriseKeyFingerprint)
	minErr := CheckOrgMinTrustPolicy(in.MinPluginTrust, derived, derivedEnt)
	hasOwnerOptIn := in.OrgPolicyEnabled.Valid && in.OrgPolicyEnabled.Int64 == 1 && in.OrgOwnerOptIn
	requiresOwnerChild := !hasActive || (derived == TierUnverifiedExternal && !derivedEnt && !hasOwnerOptIn)
	return OrgExtensionTrustVerdict{
		HasActiveVersion:           hasActive,
		DisabledByOrgPolicy:        disabled,
		DerivedPublicTrust:         derived,
		DerivedEnterpriseSigned:    derivedEnt,
		MinTrustErr:                minErr,
		MinTrustDenied:             !hasActive || minErr != nil,
		HasValidOrgOwnerOptIn:      hasOwnerOptIn,
		RequiresOrgOwnerChildOptIn: requiresOwnerChild,
	}
}
