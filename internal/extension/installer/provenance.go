// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package installer

import (
	"crypto/ed25519"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/security/trust"
)

// Installation source channel identifiers (SPEC-EXT-04 §2.1).
const (
	SourceKindLocalArchive = "local_archive"
	SourceKindGit          = "git"
	SourceKindURL          = "url"
	SourceKindOCI          = "oci"
)

var (
	git40HexRe    = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	sha256AnyRe   = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	ociDigestPart = regexp.MustCompile(`@sha256:([0-9a-f]{64})$`)
)

// Canonical provenance and source-pinning error sentinels.
var (
	ErrUnpinnedGitReference = errors.New("installer: unpinned_git_reference (422)")
	ErrUnpinnedOCIDigest    = errors.New("installer: unpinned_oci_digest (422)")
	ErrUnpinnedReference    = errors.New("installer: expected sha256 digest required for remote source")
	ErrDigestMismatch       = errors.New("installer: artifact_digest_mismatch")
	ErrInvalidSignature     = errors.New("installer: invalid Ed25519 package signature")
	ErrOrgTrustPolicyDenied = trust.ErrOrgTrustPolicyDenied
)

// SourceSpec describes the artifact origin channel and immutable reference pin.
type SourceSpec struct {
	Kind           string // local_archive | git | url | oci
	URI            string // file path, git URL, https URL, or oci:// reference
	Ref            string // required 40-hex git commit SHA when Kind == "git"
	ExpectedSHA256 string // optional for local_archive; required for url
}

// ValidateSourceRef enforces immutable pinning rules across installation sources (CT-25).
func ValidateSourceRef(src SourceSpec) (string, error) {
	uri := strings.TrimSpace(src.URI)
	switch strings.TrimSpace(src.Kind) {
	case SourceKindLocalArchive:
		if uri == "" {
			return "", fmt.Errorf("%w: local archive URI/path is required", ErrInvalidArchive)
		}
		if exp := strings.TrimSpace(src.ExpectedSHA256); exp != "" && !sha256AnyRe.MatchString(exp) {
			return "", fmt.Errorf("%w: invalid expectedSha256 %q", ErrDigestMismatch, exp)
		}
		return uri, nil

	case SourceKindGit:
		if uri == "" {
			return "", fmt.Errorf("%w: git repository URI is required", ErrUnpinnedGitReference)
		}
		checkURI := strings.TrimPrefix(uri, "git+")
		u, uerr := url.Parse(checkURI)
		if uerr != nil || (u.Scheme != "https" && u.Scheme != "ssh" && u.Scheme != "file") {
			return "", fmt.Errorf("%w: unsupported git URI %q", ErrUnpinnedGitReference, uri)
		}
		if (u.Scheme == "https" || u.Scheme == "ssh") && u.Hostname() == "" {
			return "", fmt.Errorf("%w: git URI %q requires valid hostname", ErrUnpinnedGitReference, uri)
		}
		if u.Scheme == "https" && u.User != nil {
			return "", fmt.Errorf("%w: embedded credentials forbidden in git URI %q", ErrUnpinnedGitReference, uri)
		}
		ref := strings.TrimSpace(src.Ref)
		if ref == "" && u.Fragment != "" {
			ref = strings.TrimSpace(u.Fragment)
		}
		if ref == "" {
			if idx := strings.LastIndex(uri, "@"); idx > strings.Index(uri, "://")+2 {
				cand := strings.TrimSpace(uri[idx+1:])
				if !strings.ContainsRune(cand, '/') {
					ref = cand
				}
			}
		}
		if !git40HexRe.MatchString(ref) {
			return "", fmt.Errorf("%w: git ref %q must be an immutable 40-hex commit SHA (mutable branches/tags like main/latest forbidden)", ErrUnpinnedGitReference, ref)
		}
		if exp := strings.TrimSpace(src.ExpectedSHA256); exp != "" && !sha256AnyRe.MatchString(exp) {
			return "", fmt.Errorf("%w: invalid expectedSha256 %q", ErrDigestMismatch, exp)
		}
		baseURI := strings.Split(uri, "#")[0]
		return baseURI + "#" + strings.ToLower(ref), nil

	case SourceKindURL:
		u, uerr := url.Parse(uri)
		if uerr != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			return "", fmt.Errorf("%w: invalid URL source %q", ErrUnpinnedReference, uri)
		}
		exp := strings.ToLower(strings.TrimSpace(src.ExpectedSHA256))
		if !sha256LowerRe.MatchString(exp) {
			return "", fmt.Errorf("%w: remote URL install requires 64-hex ExpectedSHA256 pin", ErrUnpinnedReference)
		}
		return uri, nil

	case SourceKindOCI:
		if !strings.HasPrefix(uri, "oci://") {
			return "", fmt.Errorf("%w: OCI reference %q must use oci:// scheme", ErrUnpinnedOCIDigest, uri)
		}
		ociURL, oerr := url.Parse(uri)
		if oerr != nil || ociURL.User != nil || ociURL.Hostname() == "" || strings.Trim(ociURL.Path, "/") == "" {
			return "", fmt.Errorf("%w: OCI reference %q requires valid registry host and repository", ErrUnpinnedOCIDigest, uri)
		}
		withoutScheme := strings.TrimPrefix(uri, "oci://")
		if idx := strings.Index(withoutScheme, "@sha256:"); idx <= 0 || !strings.Contains(withoutScheme[:idx], "/") {
			return "", fmt.Errorf("%w: OCI reference %q must specify registry/repository before @sha256 digest", ErrUnpinnedOCIDigest, uri)
		}
		if !ociDigestPart.MatchString(uri) {
			return "", fmt.Errorf("%w: OCI reference %q must pin immutable @sha256:<64-hex> digest", ErrUnpinnedOCIDigest, uri)
		}
		return uri, nil

	default:
		return "", fmt.Errorf("%w: unsupported source kind %q", ErrInvalidArchive, src.Kind)
	}
}

// TrustedKeyring holds the offline Ed25519 verification keys across Tanpopo's
// three provenance axes (Root -> official, Publisher -> verified, Enterprise -> enterprise_signed).
type TrustedKeyring = trust.TrustedKeyring

// ProvenanceSignature carries detached Ed25519 signatures over SignatureSigningPayload.
type ProvenanceSignature struct {
	PublisherOrRootSig []byte
	EnterpriseSig      []byte
}

// ProvenanceVerdict reports cryptographically derived origin trust tier and orthogonal enterprise signature.
type ProvenanceVerdict struct {
	OriginTrustTier          string // official | verified | unverified_external
	SignatureVerified        bool
	EnterpriseSigned         bool
	SigningKeyFingerprint    string
	EnterpriseKeyFingerprint string
	BundleSHA256             string
}

// SignatureSigningPayload constructs the domain-separated canonical byte slice signed by Ed25519 keys.
func SignatureSigningPayload(canonicalName, version, bundleSHA256Hex string) []byte {
	return []byte("tanpopo:tpkg:v1:" + strings.TrimSpace(canonicalName) + ":" + strings.TrimSpace(version) + ":" + strings.ToLower(strings.TrimSpace(bundleSHA256Hex)))
}

// PublicKeyFingerprint computes the deterministic 64-hex SHA-256 fingerprint of an Ed25519 public key.
func PublicKeyFingerprint(pub ed25519.PublicKey) string {
	return trust.PublicKeyFingerprint(pub)
}

// VerifyProvenance verifies archive digest parity (CT-03), offline Ed25519 signatures across
// Root/Publisher/Enterprise keys, ignores self-declared originTrustTier spoofing, and enforces
// ^@tanpopo(-[a-z0-9]+)?/ root-key protection (CT-05).
func VerifyProvenance(
	vm *ValidatedManifest,
	bundleSHA256 string,
	expectedSHA256 string,
	sig ProvenanceSignature,
	keyring TrustedKeyring,
) (*ProvenanceVerdict, error) {
	if vm == nil {
		return nil, fmt.Errorf("%w: nil validated manifest", ErrManifestInvalid)
	}
	actualSHA := strings.ToLower(strings.TrimSpace(bundleSHA256))
	if !sha256LowerRe.MatchString(actualSHA) {
		return nil, fmt.Errorf("%w: malformed bundle SHA-256 %q", ErrDigestMismatch, bundleSHA256)
	}
	if exp := strings.ToLower(strings.TrimSpace(expectedSHA256)); exp != "" {
		if !sha256LowerRe.MatchString(exp) || subtle.ConstantTimeCompare([]byte(exp), []byte(actualSHA)) != 1 {
			return nil, fmt.Errorf("%w: expected=%s actual=%s", ErrDigestMismatch, exp, actualSHA)
		}
	}

	payload := SignatureSigningPayload(vm.Manifest.Metadata.ID, vm.Manifest.Metadata.Version, actualSHA)
	verdict := &ProvenanceVerdict{
		OriginTrustTier: lockfile.TrustTierUnverifiedExternal,
		BundleSHA256:    actualSHA,
	}

	rootVerified := false
	if len(sig.PublisherOrRootSig) > 0 {
		if fp, ok := verifyAgainstKeyMap(payload, sig.PublisherOrRootSig, keyring.RootPublicKeys); ok {
			rootVerified = true
			verdict.OriginTrustTier = lockfile.TrustTierOfficial
			verdict.SignatureVerified = true
			verdict.SigningKeyFingerprint = fp
		} else if fp, ok := verifyAgainstKeyMap(payload, sig.PublisherOrRootSig, keyring.PublisherPublicKeys); ok {
			verdict.OriginTrustTier = lockfile.TrustTierVerified
			verdict.SignatureVerified = true
			verdict.SigningKeyFingerprint = fp
		} else if fp, ok := verifyAgainstKeyMap(payload, sig.PublisherOrRootSig, keyring.EnterprisePublicKeys); ok {
			// Signed solely by enterprise key -> remains unverified_external publicly + enterprise_signed=true (CR-03).
			verdict.EnterpriseSigned = true
			verdict.SigningKeyFingerprint = fp
			verdict.EnterpriseKeyFingerprint = fp
		} else {
			return nil, fmt.Errorf("%w: provided signature failed Ed25519 verification against trusted keys", ErrInvalidSignature)
		}
	}

	if len(sig.EnterpriseSig) > 0 {
		fp, ok := verifyAgainstKeyMap(payload, sig.EnterpriseSig, keyring.EnterprisePublicKeys)
		if !ok {
			return nil, fmt.Errorf("%w: enterprise signature failed Ed25519 verification", ErrInvalidSignature)
		}
		verdict.EnterpriseSigned = true
		verdict.EnterpriseKeyFingerprint = fp
		if verdict.SigningKeyFingerprint == "" {
			verdict.SigningKeyFingerprint = fp
		}
	}

	// CT-05: Unconditionally reject reserved namespace ^@tanpopo(-[a-z0-9]+)?/ unless root-signed!
	if vm.RequiresRootSignature && !rootVerified {
		return nil, fmt.Errorf("%w: package %q claims reserved @tanpopo* scope without valid Tanpopo root key signature", ErrReservedNamespaceSpoof, vm.Manifest.Metadata.ID)
	}

	return verdict, nil
}

func verifyAgainstKeyMap(payload, sig []byte, keys map[string]ed25519.PublicKey) (string, bool) {
	if len(sig) != ed25519.SignatureSize || len(keys) == 0 {
		return "", false
	}
	for _, pub := range keys {
		if len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, payload, sig) {
			return PublicKeyFingerprint(pub), true
		}
	}
	return "", false
}

// CheckOrgMinTrustPolicy enforces organizations.min_plugin_trust against derived provenance (CT-20 / CR-03).
func CheckOrgMinTrustPolicy(minPluginTrust, originTrustTier string, enterpriseSigned bool) error {
	return trust.CheckOrgMinTrustPolicy(minPluginTrust, originTrustTier, enterpriseSigned)
}
