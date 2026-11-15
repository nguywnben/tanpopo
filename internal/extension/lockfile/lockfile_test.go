// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestLockfileWriteAtomicReadVerifyAndCT03Mismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "tanpopo.lock")
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

	bundleBytes := []byte("reproducible-tpkg-archive-bytes-v1")
	digest := sha256Of(bundleBytes)

	lf := NewEmpty(now)
	lf.Extensions["@acme-cloud/regional-llm"] = LockedEntry{
		Version:               "0.2.0",
		Origin:                "https://packages.example.com/acme-0.2.0.tpkg",
		ArtifactSHA256:        digest,
		ContractVersion:       "0.1",
		TrustTier:             TrustTierUnverifiedExternal,
		EnterpriseSigned:      true,
		SigningKeyFingerprint: "fp_abc123",
		ExecutionTier:         "WASM_SANDBOX_T2",
		ApprovedPermissions: ApprovedPermissions{
			ApprovedBy:   "prc_01testreviewer",
			ApprovedAt:   now.Format(time.RFC3339),
			AllowedHosts: []string{"z.example.com", "a.example.com"},
			RequireTLS:   true,
			SecretSlots:  []string{"primary_api_key"},
			MaxMemoryMib: 16,
		},
		PreviousPinnedRollback: &PinnedRollback{
			Version:        "0.1.9",
			ArtifactSHA256: sha256Of([]byte("prior-v0.1.9")),
		},
	}

	if err := WriteAtomic(lockPath, lf); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	loaded, err := Read(lockPath)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if err := loaded.VerifyArtifactDigest("@acme-cloud/regional-llm", digest); err != nil {
		t.Fatalf("VerifyArtifactDigest valid: %v", err)
	}
	entry := loaded.Extensions["@acme-cloud/regional-llm"]
	if len(entry.ApprovedPermissions.AllowedHosts) != 2 || entry.ApprovedPermissions.AllowedHosts[0] != "a.example.com" {
		t.Fatalf("expected deterministic sorted allowedHosts, got %v", entry.ApprovedPermissions.AllowedHosts)
	}

	// CT-03: Flipping 1 byte in archive triggers ErrDigestMismatch immediately.
	mutated := append([]byte(nil), bundleBytes...)
	mutated[0] ^= 0xff
	if err := loaded.VerifyArtifactDigest("@acme-cloud/regional-llm", sha256Of(mutated)); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch on 1-byte mutation (CT-03), got %v", err)
	}

	if err := loaded.VerifyArtifactDigest("@missing/pkg", digest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown package, got %v", err)
	}
}

func TestLockfileRejectsUnknownFieldsBadVersionAndSymlinks(t *testing.T) {
	t.Parallel()

	if _, err := ParseBytes([]byte("lockfileVersion: 99\ngeneratedAt: 2026-11-15T10:00:00Z\nextensions: {}\n")); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected ErrInvalidLockfile for bad version, got %v", err)
	}
	if _, err := ParseBytes([]byte("lockfileVersion: 1\nextraBadKey: true\nextensions: {}\n")); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected ErrInvalidLockfile for unknown key, got %v", err)
	}
	if _, err := ParseBytes([]byte("lockfileVersion: 1\nextensions: {}\n---\nlockfileVersion: 1\n")); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected ErrInvalidLockfile for multi-doc YAML, got %v", err)
	}

	// Alias hyphenated "unverified-external" must normalize cleanly to TrustTierUnverifiedExternal.
	yamlAlias := `lockfileVersion: 1
generatedAt: "2026-11-15T10:00:00Z"
extensions:
  "@community/ext":
    version: "1.0.0"
    origin: "./ext.tpkg"
    artifactSha256: "` + strings.Repeat("a", 64) + `"
    contractVersion: "0.1"
    trustTier: "unverified-external"
    executionTier: "DECLARATIVE_T1"
    approvedPermissions:
      approvedBy: "prc_owner"
      approvedAt: "2026-11-15T10:00:00Z"
`
	lf, err := ParseBytes([]byte(yamlAlias))
	if err != nil {
		t.Fatalf("ParseBytes alias: %v", err)
	}
	if lf.Extensions["@community/ext"].TrustTier != TrustTierUnverifiedExternal {
		t.Fatalf("expected normalized trustTier %q, got %q", TrustTierUnverifiedExternal, lf.Extensions["@community/ext"].TrustTier)
	}

	if runtime.GOOS != "windows" {
		dir := t.TempDir()
		realPath := filepath.Join(dir, "real.lock")
		if err := os.WriteFile(realPath, []byte(yamlAlias), 0o600); err != nil {
			t.Fatalf("write real.lock: %v", err)
		}
		symPath := filepath.Join(dir, "sym.lock")
		if err := os.Symlink(realPath, symPath); err == nil {
			if _, rerr := Read(symPath); !errors.Is(rerr, ErrSymlinkTarget) {
				t.Fatalf("expected ErrSymlinkTarget on Read symlink, got %v", rerr)
			}
			if werr := WriteAtomic(symPath, lf); !errors.Is(werr, ErrSymlinkTarget) {
				t.Fatalf("expected ErrSymlinkTarget on WriteAtomic symlink, got %v", werr)
			}
		}
	}
}

func TestLockfileValidationAndErrorBranches(t *testing.T) {
	t.Parallel()

	if _, err := Read(""); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected ErrInvalidLockfile on empty Read path, got %v", err)
	}
	if err := WriteAtomic("", NewEmpty(time.Time{})); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected ErrInvalidLockfile on empty WriteAtomic path, got %v", err)
	}
	if err := WriteAtomic(filepath.Join(t.TempDir(), "x.lock"), nil); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected ErrInvalidLockfile on nil lockfile, got %v", err)
	}
	if _, err := ParseBytes([]byte("   \n")); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected ErrInvalidLockfile on empty bytes, got %v", err)
	}
	if err := (*Lockfile)(nil).Validate(); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected ErrInvalidLockfile validating nil lockfile, got %v", err)
	}
	if err := (*Lockfile)(nil).VerifyArtifactDigest("pkg", strings.Repeat("a", 64)); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected ErrInvalidLockfile verifying nil lockfile, got %v", err)
	}

	// Bad SHA-256, blank entry key, missing approvedBy, bad rollback SHA-256.
	bad := NewEmpty(time.Now())
	bad.Extensions[""] = LockedEntry{}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected blank package key error, got %v", err)
	}
	delete(bad.Extensions, "")
	bad.Extensions["@acme/p"] = LockedEntry{
		Version:         "1.0.0",
		Origin:          "./p.tpkg",
		ArtifactSHA256:  "not-hex",
		ContractVersion: "0.1",
		TrustTier:       TrustTierVerified,
		ApprovedPermissions: ApprovedPermissions{
			ApprovedBy: "prc_1",
		},
	}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected invalid artifactSha256 error, got %v", err)
	}
	validEntry := bad.Extensions["@acme/p"]
	validEntry.ArtifactSHA256 = strings.Repeat("a", 64)
	validEntry.ExecutionTier = "BAD_TIER"
	bad.Extensions["@acme/p"] = validEntry
	if err := bad.Validate(); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected invalid executionTier error, got %v", err)
	}
	validEntry.ExecutionTier = "WASM_SANDBOX_T2"
	validEntry.PreviousPinnedRollback = &PinnedRollback{Version: "0.9.0", ArtifactSHA256: "bad"}
	bad.Extensions["@acme/p"] = validEntry
	if err := bad.Validate(); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected invalid rollback sha256 error, got %v", err)
	}
	validEntry.PreviousPinnedRollback = nil
	validEntry.ApprovedPermissions.ApprovedBy = ""
	bad.Extensions["@acme/p"] = validEntry
	if err := bad.Validate(); !errors.Is(err, ErrInvalidLockfile) {
		t.Fatalf("expected missing approvedBy error, got %v", err)
	}
}

