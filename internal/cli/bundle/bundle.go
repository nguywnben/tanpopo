// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package bundle implements Tanpopo's zero-egress Air-Gap Offline Bundle
// toolchain (`tanpopo bundle export|verify|import`) with deterministic .tar.zst
// packaging, Ed25519 signature verification, constant-time SHA-256 validation,
// signed revocation freshness checks, and atomic rollback-safe import (TP-IMP-0045 / CT-31).
package bundle

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/security/trust"
)

const (
	// FormatVersion is the canonical .tpbundle schema version.
	FormatVersion = "1"

	manifestFileName  = "bundle-manifest.json"
	signatureFileName = "bundle-manifest.sig"
	lockfileEntryName = "tanpopo.lock"
	indexEntryName    = "feeds/index.json"
	revocEntryName    = "feeds/revocations.json"
	trustRootName     = "trust/trust-root.pub"

	bundleSigningDomainPrefix = "tanpopo:airgap-bundle:v1\x00"

	MaxBundleCompressedBytes   int64 = 512 << 20  // 512 MiB
	MaxBundleUncompressedBytes int64 = 1024 << 20 // 1 GiB
	MaxBundleEntryBytes        int64 = 256 << 20  // 256 MiB per file entry
	MaxBundleEntries           int   = 512
	MaxBundleRatio             int64 = 60
	minRatioCheckBytes         int64 = 4096
	maxKeyFileBytes            int64 = 64 << 10
	maxLockOrFeedBytes         int64 = 8 << 20
)

// ManifestFileRef records the exact archive path, lowercase 64-hex SHA-256, and byte size.
type ManifestFileRef struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// ArtifactEntry describes one extension package, SBOM, binary, or OCI artifact inside .tpbundle.
type ArtifactEntry struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"` // extension | sbom | artifact
	PackageID string `json:"package_id,omitempty"`
	Version   string `json:"version,omitempty"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// BundleManifest is the signed inventory of every file permitted inside a .tpbundle archive.
type BundleManifest struct {
	SchemaVersion  string           `json:"schema_version"`
	CreatedAt      string           `json:"created_at"`
	TanpopoVersion string           `json:"tanpopo_version,omitempty"`
	KeyID          string           `json:"key_id"`
	Lockfile       *ManifestFileRef `json:"lockfile,omitempty"`
	IndexFeed      *ManifestFileRef `json:"index_feed,omitempty"`
	RevocationFeed *ManifestFileRef `json:"revocation_feed,omitempty"`
	TrustRootPub   *ManifestFileRef `json:"trust_root_pub,omitempty"`
	Artifacts      []ArtifactEntry  `json:"artifacts"`
}

// BundleSignature stores the detached Ed25519 signature over domain-separated manifest bytes.
type BundleSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

func contractFail(msg string, cause error) error {
	return &testharness.ExitError{
		Code:    testharness.ExitContract,
		Message: msg,
		Cause:   cause,
	}
}

func validationFail(msg string, cause error) error {
	return &testharness.ExitError{
		Code:    testharness.ExitValidation,
		Message: msg,
		Cause:   cause,
	}
}

func sha256HexBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func equalDigestHex(a, b string) bool {
	ab, aerr := hex.DecodeString(strings.ToLower(strings.TrimSpace(a)))
	bb, berr := hex.DecodeString(strings.ToLower(strings.TrimSpace(b)))
	if aerr != nil || berr != nil || len(ab) != sha256.Size || len(bb) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(ab, bb) == 1
}

// readRegularFileBounded reads filePath via non-symlink os.Lstat + os.OpenRoot confinement.
func readRegularFileBounded(filePath string, maxBytes int64) ([]byte, error) {
	cleaned := strings.TrimSpace(filePath)
	if cleaned == "" {
		return nil, errors.New("file path is required")
	}
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	fi, err := root.Lstat(base)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("path %q must be a regular non-symlink file", filePath)
	}
	if fi.Size() < 0 || fi.Size() > maxBytes {
		return nil, fmt.Errorf("file %q size %d exceeds %d byte limit", filePath, fi.Size(), maxBytes)
	}
	f, err := root.Open(base)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file %q exceeded %d byte read limit", filePath, maxBytes)
	}
	return data, nil
}

// LoadPublicKey parses a trusted Ed25519 public key from PEM, 64-hex, or base64.
func LoadPublicKey(filePath string) (ed25519.PublicKey, string, error) {
	raw, err := readRegularFileBounded(filePath, maxKeyFileBytes)
	if err != nil {
		return nil, "", err
	}
	pub, err := parsePublicKeyBytes(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parse public key %q: %w", filePath, err)
	}
	return pub, trust.PublicKeyFingerprint(pub), nil
}

func parsePublicKeyBytes(raw []byte) (ed25519.PublicKey, error) {
	trimmed := strings.TrimSpace(string(raw))
	if block, _ := pem.Decode([]byte(trimmed)); block != nil {
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		pub, ok := parsed.(ed25519.PublicKey)
		if !ok || len(pub) != ed25519.PublicKeySize {
			return nil, errors.New("PEM block does not contain an Ed25519 public key")
		}
		return append(ed25519.PublicKey(nil), pub...), nil
	}
	if strings.HasPrefix(trimmed, "{") {
		var obj struct {
			PublicKey string `json:"public_key"`
		}
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil && strings.TrimSpace(obj.PublicKey) != "" {
			trimmed = strings.TrimSpace(obj.PublicKey)
		}
	}
	if len(trimmed) == ed25519.PublicKeySize*2 {
		if dec, err := hex.DecodeString(trimmed); err == nil && len(dec) == ed25519.PublicKeySize {
			return ed25519.PublicKey(dec), nil
		}
	}
	if dec, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(dec) == ed25519.PublicKeySize {
		return ed25519.PublicKey(dec), nil
	}
	return nil, errors.New("unsupported Ed25519 public key encoding (expected PEM, 64-hex, or base64)")
}

// LoadPrivateKey parses an Ed25519 signing private key from PKCS#8 PEM, hex seed/key, or base64.
func LoadPrivateKey(filePath string) (ed25519.PrivateKey, error) {
	raw, err := readRegularFileBounded(filePath, maxKeyFileBytes)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if block, _ := pem.Decode([]byte(trimmed)); block != nil {
		parsed, perr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("parse private key %q: %w", filePath, perr)
		}
		priv, ok := parsed.(ed25519.PrivateKey)
		if !ok || len(priv) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("private key %q is not Ed25519", filePath)
		}
		return append(ed25519.PrivateKey(nil), priv...), nil
	}
	if dec, herr := hex.DecodeString(trimmed); herr == nil {
		switch len(dec) {
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(dec), nil
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(dec), nil
		}
	}
	if dec, berr := base64.StdEncoding.DecodeString(trimmed); berr == nil {
		switch len(dec) {
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(dec), nil
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(dec), nil
		}
	}
	return nil, fmt.Errorf("parse private key %q: unsupported Ed25519 private key format", filePath)
}

func encodePublicKeyPEM(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// ResolveTrustRootKey resolves the pinned external Ed25519 public key strictly from
// explicit CLI flags or host configuration, refusing unpinned self-signed bundle keys.
func ResolveTrustRootKey(explicitPath, dataDir string, lookupEnv func(string) (string, bool)) (ed25519.PublicKey, string, error) {
	if p := strings.TrimSpace(explicitPath); p != "" {
		pub, fp, err := LoadPublicKey(p)
		if err != nil {
			return nil, "", contractFail(fmt.Sprintf("failed loading trusted public key %q", p), err)
		}
		return pub, fp, nil
	}
	if lookupEnv != nil {
		if envPath, ok := lookupEnv("TANPOPO_TRUST_ROOT_PUB"); ok && strings.TrimSpace(envPath) != "" {
			pub, fp, err := LoadPublicKey(strings.TrimSpace(envPath))
			if err != nil {
				return nil, "", contractFail(fmt.Sprintf("failed loading TANPOPO_TRUST_ROOT_PUB %q", envPath), err)
			}
			return pub, fp, nil
		}
	}
	candidates := []string{"/etc/tanpopo/trust-root.pub"}
	if d := strings.TrimSpace(dataDir); d != "" {
		candidates = append(candidates, filepath.Join(d, "trust-root.pub"))
	}
	for _, cand := range candidates {
		if _, err := os.Lstat(cand); err == nil {
			pub, fp, lerr := LoadPublicKey(cand)
			if lerr != nil {
				return nil, "", contractFail(fmt.Sprintf("failed loading pinned trust root %q", cand), lerr)
			}
			return pub, fp, nil
		}
	}
	return nil, "", contractFail("trusted Ed25519 public key required (--public-key, TANPOPO_TRUST_ROOT_PUB, or /etc/tanpopo/trust-root.pub)", nil)
}

func cleanArchivePath(raw string) (string, error) {
	if raw == "" || strings.ContainsRune(raw, 0) || strings.ContainsRune(raw, '\\') {
		return "", errors.New("invalid archive entry path")
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", errors.New("control character in archive entry path")
		}
	}
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("absolute archive entry path %q forbidden", raw)
	}
	if len(raw) >= 2 && ((raw[0] >= 'A' && raw[0] <= 'Z') || (raw[0] >= 'a' && raw[0] <= 'z')) && raw[1] == ':' {
		return "", fmt.Errorf("drive-prefixed archive entry path %q forbidden", raw)
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("path traversal entry %q forbidden", raw)
	}
	return cleaned, nil
}

func safePackageSlug(pkgID string) string {
	var b strings.Builder
	for _, r := range pkgID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "extension"
	}
	return out
}

func writeFileAtomic(destPath string, data []byte, mode os.FileMode) error {
	parent := filepath.Dir(destPath)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".tanpopo-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return err
	}
	keep = true
	return nil
}
