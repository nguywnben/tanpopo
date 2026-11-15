// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package lockfile provides deterministic reading, writing, validation, and
// SHA-256 digest verification for tanpopo.lock (TP-IMP-0011 / SPEC-EXT-04 / CT-03).
package lockfile

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// CurrentVersion is the supported tanpopo.lock schema version.
	CurrentVersion = 1
	maxLockBytes   = 8 << 20 // 8 MiB ceiling on lockfile reads
)

// Canonical trust tiers and execution tiers stored in tanpopo.lock.
const (
	TrustTierOfficial           = "official"
	TrustTierVerified           = "verified"
	TrustTierUnverifiedExternal = "unverified_external"
)

var (
	// ErrInvalidLockfile indicates malformed YAML/schema or invalid fields in tanpopo.lock.
	ErrInvalidLockfile = errors.New("lockfile: invalid tanpopo.lock")
	// ErrNotFound indicates a requested package is absent from tanpopo.lock.
	ErrNotFound = errors.New("lockfile: package entry not found")
	// ErrDigestMismatch indicates an archive's SHA-256 digest does not match tanpopo.lock (CT-03 / exit 22).
	ErrDigestMismatch = errors.New("lockfile: artifact_digest_mismatch")
	// ErrSymlinkTarget indicates the lockfile path is a symlink and is refused fail-closed.
	ErrSymlinkTarget = errors.New("lockfile: symlink path forbidden")
)

var sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Lockfile represents the root tanpopo.lock supply-chain manifest.
type Lockfile struct {
	LockfileVersion int                    `yaml:"lockfileVersion" json:"lockfileVersion"`
	GeneratedAt     string                 `yaml:"generatedAt" json:"generatedAt"`
	Extensions      map[string]LockedEntry `yaml:"extensions" json:"extensions"`
}

// LockedEntry pins an installed extension version, origin, SHA-256 digest,
// provenance classification, approved permissions, and rollback pointer.
type LockedEntry struct {
	Version                      string              `yaml:"version" json:"version"`
	Origin                       string              `yaml:"origin" json:"origin"`
	ArtifactSHA256               string              `yaml:"artifactSha256" json:"artifactSha256"`
	ContractVersion              string              `yaml:"contractVersion" json:"contractVersion"`
	TrustTier                    string              `yaml:"trustTier" json:"trustTier"`
	EnterpriseSigned             bool                `yaml:"enterpriseSigned,omitempty" json:"enterpriseSigned,omitempty"`
	OrgInternalSignatureVerified bool                `yaml:"orgInternalSignatureVerified,omitempty" json:"orgInternalSignatureVerified,omitempty"`
	SigningKeyFingerprint        string              `yaml:"signingKeyFingerprint,omitempty" json:"signingKeyFingerprint,omitempty"`
	ExecutionTier                string              `yaml:"executionTier" json:"executionTier"`
	ApprovedPermissions          ApprovedPermissions `yaml:"approvedPermissions" json:"approvedPermissions"`
	PreviousPinnedRollback       *PinnedRollback     `yaml:"previousPinnedRollback,omitempty" json:"previousPinnedRollback,omitempty"`
}

// ApprovedPermissions captures the human-consented capability set P in tanpopo.lock.
type ApprovedPermissions struct {
	ApprovedBy           string            `yaml:"approvedBy" json:"approvedBy"`
	ApprovedAt           string            `yaml:"approvedAt" json:"approvedAt"`
	AllowedHosts         []string          `yaml:"allowedHosts,omitempty" json:"allowedHosts,omitempty"`
	AllowPrivateIP       bool              `yaml:"allowPrivateIp,omitempty" json:"allowPrivateIp,omitempty"`
	RequireTLS           bool              `yaml:"requireTls,omitempty" json:"requireTls,omitempty"`
	ExecutionTier        string            `yaml:"executionTier,omitempty" json:"executionTier,omitempty"`
	SecretSlots          []string          `yaml:"secretSlots,omitempty" json:"secretSlots,omitempty"`
	SecretBindingModes   map[string]string `yaml:"secretBindingModes,omitempty" json:"secretBindingModes,omitempty"`
	Hooks                []string          `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	MaxMemoryMib         int               `yaml:"maxMemoryMib,omitempty" json:"maxMemoryMib,omitempty"`
	InspectRequestBody   bool              `yaml:"inspectRequestBody,omitempty" json:"inspectRequestBody,omitempty"`
	InspectResponseBody  bool              `yaml:"inspectResponseBody,omitempty" json:"inspectResponseBody,omitempty"`
}

// PinnedRollback points to the retained known-good prior version for < 5s rollback.
type PinnedRollback struct {
	Version        string `yaml:"version" json:"version"`
	ArtifactSHA256 string `yaml:"artifactSha256" json:"artifactSha256"`
}

// NewEmpty constructs an initialized version-1 Lockfile.
func NewEmpty(now time.Time) *Lockfile {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &Lockfile{
		LockfileVersion: CurrentVersion,
		GeneratedAt:     now.UTC().Format(time.RFC3339),
		Extensions:      make(map[string]LockedEntry),
	}
}

// NormalizeTrustTier validates and normalizes package trust tiers (accepting the
// hyphenated "unverified-external" alias used in YAML prose).
func NormalizeTrustTier(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case TrustTierOfficial:
		return TrustTierOfficial, nil
	case TrustTierVerified:
		return TrustTierVerified, nil
	case TrustTierUnverifiedExternal, "unverified-external":
		return TrustTierUnverifiedExternal, nil
	default:
		return "", fmt.Errorf("%w: unsupported trustTier %q", ErrInvalidLockfile, raw)
	}
}

// Validate verifies structural completeness and invariant formatting of lf.
func (lf *Lockfile) Validate() error {
	if lf == nil {
		return fmt.Errorf("%w: nil lockfile", ErrInvalidLockfile)
	}
	if lf.LockfileVersion != CurrentVersion {
		return fmt.Errorf("%w: unsupported lockfileVersion %d (expected %d)", ErrInvalidLockfile, lf.LockfileVersion, CurrentVersion)
	}
	for name, entry := range lf.Extensions {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%w: extension key cannot be blank", ErrInvalidLockfile)
		}
		if err := entry.validate(name); err != nil {
			return err
		}
	}
	return nil
}

func (e LockedEntry) validate(pkgName string) error {
	if strings.TrimSpace(e.Version) == "" || strings.TrimSpace(e.Origin) == "" || strings.TrimSpace(e.ContractVersion) == "" {
		return fmt.Errorf("%w: package %q requires version, origin, and contractVersion", ErrInvalidLockfile, pkgName)
	}
	sha := strings.ToLower(strings.TrimSpace(e.ArtifactSHA256))
	if !sha256HexRe.MatchString(sha) {
		return fmt.Errorf("%w: package %q has invalid artifactSha256 %q", ErrInvalidLockfile, pkgName, e.ArtifactSHA256)
	}
	if _, err := NormalizeTrustTier(e.TrustTier); err != nil {
		return fmt.Errorf("%w: package %q: %v", ErrInvalidLockfile, pkgName, err)
	}
	switch strings.TrimSpace(e.ExecutionTier) {
	case "DECLARATIVE_T1", "WASM_SANDBOX_T2", "ISOLATED_SIDECAR_T3":
	default:
		return fmt.Errorf("%w: package %q has unsupported executionTier %q", ErrInvalidLockfile, pkgName, e.ExecutionTier)
	}
	if appTier := strings.TrimSpace(e.ApprovedPermissions.ExecutionTier); appTier != "" && appTier != strings.TrimSpace(e.ExecutionTier) {
		return fmt.Errorf("%w: package %q approvedPermissions.executionTier %q mismatches %q", ErrInvalidLockfile, pkgName, appTier, e.ExecutionTier)
	}
	if strings.TrimSpace(e.ApprovedPermissions.ApprovedBy) == "" {
		return fmt.Errorf("%w: package %q requires approvedPermissions.approvedBy", ErrInvalidLockfile, pkgName)
	}
	if e.PreviousPinnedRollback != nil {
		if strings.TrimSpace(e.PreviousPinnedRollback.Version) == "" {
			return fmt.Errorf("%w: package %q previousPinnedRollback missing version", ErrInvalidLockfile, pkgName)
		}
		prevSHA := strings.ToLower(strings.TrimSpace(e.PreviousPinnedRollback.ArtifactSHA256))
		if !sha256HexRe.MatchString(prevSHA) {
			return fmt.Errorf("%w: package %q previousPinnedRollback has invalid artifactSha256", ErrInvalidLockfile, pkgName)
		}
	}
	return nil
}

// Read parses and validates tanpopo.lock from path, rejecting symlinks and unknown YAML keys.
func Read(path string) (*Lockfile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: path is required", ErrInvalidLockfile)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s", ErrSymlinkTarget, path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrInvalidLockfile, path)
	}
	if fi.Size() > maxLockBytes {
		return nil, fmt.Errorf("%w: file exceeds %d byte cap", ErrInvalidLockfile, maxLockBytes)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("lockfile: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, maxLockBytes+1))
	if err != nil {
		return nil, fmt.Errorf("lockfile: read %q: %w", path, err)
	}
	if len(raw) > maxLockBytes {
		return nil, fmt.Errorf("%w: file exceeds %d byte cap", ErrInvalidLockfile, maxLockBytes)
	}

	return ParseBytes(raw)
}

// ParseBytes decodes YAML bytes strictly into a validated Lockfile.
func ParseBytes(raw []byte) (*Lockfile, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: empty lockfile", ErrInvalidLockfile)
	}
	var lf Lockfile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&lf); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidLockfile, err)
	}
	// Reject trailing multi-document YAML streams.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing YAML document is forbidden", ErrInvalidLockfile)
	}
	if lf.Extensions == nil {
		lf.Extensions = make(map[string]LockedEntry)
	}
	for k, v := range lf.Extensions {
		tier, err := NormalizeTrustTier(v.TrustTier)
		if err != nil {
			return nil, fmt.Errorf("%w: package %q: %v", ErrInvalidLockfile, k, err)
		}
		v.TrustTier = tier
		v.ArtifactSHA256 = strings.ToLower(strings.TrimSpace(v.ArtifactSHA256))
		if v.PreviousPinnedRollback != nil {
			v.PreviousPinnedRollback.ArtifactSHA256 = strings.ToLower(strings.TrimSpace(v.PreviousPinnedRollback.ArtifactSHA256))
		}
		lf.Extensions[k] = v
	}
	if err := lf.Validate(); err != nil {
		return nil, err
	}
	return &lf, nil
}

// MarshalDeterministic serializes lf cleanly with canonical slice ordering for reproducible diffs.
func (lf *Lockfile) MarshalDeterministic() ([]byte, error) {
	if err := lf.Validate(); err != nil {
		return nil, err
	}
	cp := Lockfile{
		LockfileVersion: lf.LockfileVersion,
		GeneratedAt:     lf.GeneratedAt,
		Extensions:      make(map[string]LockedEntry, len(lf.Extensions)),
	}
	for k, e := range lf.Extensions {
		norm := e
		tier, err := NormalizeTrustTier(norm.TrustTier)
		if err != nil {
			return nil, err
		}
		norm.TrustTier = tier
		norm.ArtifactSHA256 = strings.ToLower(strings.TrimSpace(norm.ArtifactSHA256))
		norm.ApprovedPermissions = cloneSortedPermissions(norm.ApprovedPermissions)
		if norm.PreviousPinnedRollback != nil {
			rb := *norm.PreviousPinnedRollback
			rb.ArtifactSHA256 = strings.ToLower(strings.TrimSpace(rb.ArtifactSHA256))
			norm.PreviousPinnedRollback = &rb
		}
		cp.Extensions[k] = norm
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&cp); err != nil {
		return nil, fmt.Errorf("lockfile: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("lockfile: close yaml encoder: %w", err)
	}
	return buf.Bytes(), nil
}

func cloneSortedPermissions(p ApprovedPermissions) ApprovedPermissions {
	out := p
	if p.AllowedHosts != nil {
		out.AllowedHosts = append([]string(nil), p.AllowedHosts...)
		sort.Strings(out.AllowedHosts)
	}
	if p.SecretSlots != nil {
		out.SecretSlots = append([]string(nil), p.SecretSlots...)
		sort.Strings(out.SecretSlots)
	}
	if p.Hooks != nil {
		out.Hooks = append([]string(nil), p.Hooks...)
		sort.Strings(out.Hooks)
	}
	if p.SecretBindingModes != nil {
		out.SecretBindingModes = make(map[string]string, len(p.SecretBindingModes))
		for k, v := range p.SecretBindingModes {
			out.SecretBindingModes[k] = v
		}
	}
	return out
}

// WriteAtomic validates, serializes deterministically, and atomically replaces path via temp-file rename.
func WriteAtomic(path string, lf *Lockfile) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%w: target path is required", ErrInvalidLockfile)
	}
	if lf == nil {
		return fmt.Errorf("%w: nil lockfile", ErrInvalidLockfile)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("lockfile: resolve path %q: %w", path, err)
	}
	if fi, lerr := os.Lstat(absPath); lerr == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkTarget, absPath)
		}
	} else if !os.IsNotExist(lerr) {
		return fmt.Errorf("lockfile: stat %q: %w", absPath, lerr)
	}

	raw, err := lf.MarshalDeterministic()
	if err != nil {
		return err
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("lockfile: create parent dir %q: %w", dir, err)
	}
	if dfi, derr := os.Lstat(dir); derr != nil || dfi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: parent dir %q cannot be a symlink", ErrSymlinkTarget, dir)
	}

	tmpFile, err := os.CreateTemp(dir, ".tanpopo.lock.tmp-*")
	if err != nil {
		return fmt.Errorf("lockfile: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("lockfile: write temp file: %w", err)
	}
	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("lockfile: chmod temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("lockfile: sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("lockfile: close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		return fmt.Errorf("lockfile: atomic rename %q -> %q: %w", tmpPath, absPath, err)
	}
	cleanupTmp = false
	return nil
}

// VerifyArtifactDigest verifies actualSHA256 against the pinned digest recorded in tanpopo.lock (CT-03).
func (lf *Lockfile) VerifyArtifactDigest(packageName, actualSHA256 string) error {
	if lf == nil {
		return fmt.Errorf("%w: lockfile is nil", ErrInvalidLockfile)
	}
	entry, ok := lf.Extensions[packageName]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, packageName)
	}
	expected := strings.ToLower(strings.TrimSpace(entry.ArtifactSHA256))
	actual := strings.ToLower(strings.TrimSpace(actualSHA256))
	if !sha256HexRe.MatchString(expected) || !sha256HexRe.MatchString(actual) {
		return fmt.Errorf("%w: package %q malformed sha256 digest", ErrDigestMismatch, packageName)
	}
	if subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) != 1 {
		return fmt.Errorf("%w: package %q expected=%s actual=%s", ErrDigestMismatch, packageName, expected, actual)
	}
	return nil
}
