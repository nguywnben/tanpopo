// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package builtin

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguywnben/tanpopo/extensions/official"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/security/trust"
)

// ReleaseRootPublicKeysHex may be set at release build time via -ldflags
// ("-X github.com/nguywnben/tanpopo/internal/extension/builtin.ReleaseRootPublicKeysHex=...")
// using comma-separated 64-hex Ed25519 public keys. Private signing keys are
// never embedded in source code or release binaries.
var ReleaseRootPublicKeysHex string

type releaseSignatureFile struct {
	Signatures []ReleaseSignatureRecord `json:"signatures"`
}

// ReleaseSignatureRecord binds an official built-in package archive digest to an
// offline Ed25519 release signature produced outside the runtime instance.
type ReleaseSignatureRecord struct {
	PackageID     string `json:"package_id"`
	Version       string `json:"version"`
	ArchiveSHA256 string `json:"archive_sha256"`
	RootSignature string `json:"root_signature_base64"`
}

// LoadReleaseRootPublicKeys loads authentic offline Ed25519 release root public
// keys from link-time configuration or explicit operator host configuration
// (TANPOPO_RELEASE_ROOT_PUBKEYS, TANPOPO_TRUST_ROOT_PUB, or <dataDir>/trust-root.pub).
// It never derives signing material from instance-local encryption secrets (KEK).
func LoadReleaseRootPublicKeys(lookupEnv func(string) (string, bool), dataDir string) (map[string]ed25519.PublicKey, error) {
	out := make(map[string]ed25519.PublicKey)

	addKey := func(pub ed25519.PublicKey) {
		if len(pub) != ed25519.PublicKeySize {
			return
		}
		fp := trust.PublicKeyFingerprint(pub)
		out[fp] = pub
	}

	parseHexKeys := func(raw string, src string) error {
		for _, tok := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		}) {
			trimmed := strings.TrimSpace(tok)
			if trimmed == "" {
				continue
			}
			dec, err := hex.DecodeString(trimmed)
			if err != nil || len(dec) != ed25519.PublicKeySize {
				return fmt.Errorf("builtin: invalid Ed25519 hex public key from %s", src)
			}
			addKey(ed25519.PublicKey(dec))
		}
		return nil
	}

	parseKeyFile := func(path string) error {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if block, _ := pem.Decode(raw); block != nil {
			parsed, perr := x509.ParsePKIXPublicKey(block.Bytes)
			if perr != nil {
				return fmt.Errorf("builtin: parse PKIX public key %q: %w", path, perr)
			}
			edPub, ok := parsed.(ed25519.PublicKey)
			if !ok {
				return fmt.Errorf("builtin: key %q is not Ed25519", path)
			}
			addKey(edPub)
			return nil
		}
		trimmed := strings.TrimSpace(string(raw))
		if len(trimmed) == ed25519.PublicKeySize*2 {
			return parseHexKeys(trimmed, path)
		}
		if dec, derr := base64.StdEncoding.DecodeString(trimmed); derr == nil && len(dec) == ed25519.PublicKeySize {
			addKey(ed25519.PublicKey(dec))
			return nil
		}
		return fmt.Errorf("builtin: unsupported public key format in %q", path)
	}

	if strings.TrimSpace(ReleaseRootPublicKeysHex) != "" {
		if err := parseHexKeys(ReleaseRootPublicKeysHex, "ldflags"); err != nil {
			return nil, err
		}
	}
	if lookupEnv != nil {
		if rawHex, ok := lookupEnv("TANPOPO_RELEASE_ROOT_PUBKEYS"); ok && strings.TrimSpace(rawHex) != "" {
			if err := parseHexKeys(rawHex, "TANPOPO_RELEASE_ROOT_PUBKEYS"); err != nil {
				return nil, err
			}
		}
		if pathVal, ok := lookupEnv("TANPOPO_TRUST_ROOT_PUB"); ok && strings.TrimSpace(pathVal) != "" {
			if err := parseKeyFile(strings.TrimSpace(pathVal)); err != nil {
				return nil, fmt.Errorf("builtin: load TANPOPO_TRUST_ROOT_PUB: %w", err)
			}
		}
	}
	if d := strings.TrimSpace(dataDir); d != "" {
		cand := filepath.Join(d, "trust-root.pub")
		if _, err := os.Lstat(cand); err == nil {
			if err := parseKeyFile(cand); err != nil {
				return nil, fmt.Errorf("builtin: load %s: %w", cand, err)
			}
		}
	}
	return out, nil
}

func parseReleaseSignaturesJSON(raw []byte) (map[string][]byte, error) {
	var doc releaseSignatureFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("builtin: decode release-signatures.json: %w", err)
	}
	out := make(map[string][]byte, len(doc.Signatures))
	for _, rec := range doc.Signatures {
		sigRaw, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.RootSignature))
		if derr != nil || len(sigRaw) != ed25519.SignatureSize {
			return nil, fmt.Errorf("builtin: invalid base64 signature for %s@%s", rec.PackageID, rec.Version)
		}
		key := strings.ToLower(strings.TrimSpace(rec.PackageID)) + "@" + strings.TrimSpace(rec.Version) + "#" + strings.ToLower(strings.TrimSpace(rec.ArchiveSHA256))
		out[key] = sigRaw
	}
	return out, nil
}

func loadEmbeddedReleaseSignatures() (map[string][]byte, error) {
	raw, err := fs.ReadFile(official.FS(), "release-signatures.json")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("builtin: read release-signatures.json: %w", err)
	}
	return parseReleaseSignaturesJSON(raw)
}

// LoadOperatorReleaseSignatures loads optional offline release signature records
// from TANPOPO_RELEASE_SIGNATURES_PATH or <dataDir>/release-signatures.json. All loaded
// signatures still require cryptographic verification against LoadReleaseRootPublicKeys.
func (c *Catalog) LoadOperatorReleaseSignatures(lookupEnv func(string) (string, bool), dataDir string) error {
	if c == nil {
		return nil
	}
	mergeFile := func(path string) error {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed, perr := parseReleaseSignaturesJSON(raw)
		if perr != nil {
			return perr
		}
		if c.releaseSigs == nil {
			c.releaseSigs = make(map[string][]byte, len(parsed))
		}
		for k, v := range parsed {
			c.releaseSigs[k] = v
		}
		return nil
	}
	if lookupEnv != nil {
		if pathVal, ok := lookupEnv("TANPOPO_RELEASE_SIGNATURES_PATH"); ok && strings.TrimSpace(pathVal) != "" {
			if err := mergeFile(strings.TrimSpace(pathVal)); err != nil {
				return fmt.Errorf("builtin: load TANPOPO_RELEASE_SIGNATURES_PATH: %w", err)
			}
		}
	}
	if d := strings.TrimSpace(dataDir); d != "" {
		cand := filepath.Join(d, "release-signatures.json")
		if _, err := os.Lstat(cand); err == nil {
			if err := mergeFile(cand); err != nil {
				return fmt.Errorf("builtin: load %s: %w", cand, err)
			}
		}
	}
	return nil
}

// PackageProvenance summarizes verified release trust metadata for a built-in package
// against the provided root public keys (without fabricating signatures).
type PackageProvenance struct {
	OriginTrustTier       string
	SignatureVerified     bool
	SigningKeyFingerprint string
	RootSignature         []byte
}

// EvaluateProvenance verifies whether pkg carries an authentic offline release signature
// under rootKeys. Unsigned builds report origin_trust_tier="unverified_external" and
// signature_verified=false truthfully.
func (c *Catalog) EvaluateProvenance(pkg Package, rootKeys map[string]ed25519.PublicKey) PackageProvenance {
	out := PackageProvenance{
		OriginTrustTier:   lockfile.TrustTierUnverifiedExternal,
		SignatureVerified: false,
	}
	if len(rootKeys) == 0 {
		return out
	}
	sigKey := strings.ToLower(pkg.PackageID) + "@" + pkg.Version + "#" + strings.ToLower(pkg.ArchiveSHA256)
	sig := c.releaseSigs[sigKey]
	if len(sig) != ed25519.SignatureSize {
		return out
	}
	payload := installer.SignatureSigningPayload(pkg.PackageID, pkg.Version, pkg.ArchiveSHA256)
	for fp, pub := range rootKeys {
		if len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, payload, sig) {
			out.OriginTrustTier = trust.TierOfficial
			out.SignatureVerified = true
			out.SigningKeyFingerprint = fp
			out.RootSignature = append([]byte(nil), sig...)
			return out
		}
	}
	return out
}
