// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package builtin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/nguywnben/tanpopo/extensions/official"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestBuiltinCatalog_DeterministicPackingAndProvenance(t *testing.T) {
	t.Parallel()

	cat1, err := NewCatalog()
	if err != nil {
		t.Fatalf("NewCatalog first: %v", err)
	}
	cat2, err := NewCatalog()
	if err != nil {
		t.Fatalf("NewCatalog second: %v", err)
	}

	pkgs1 := cat1.Packages()
	pkgs2 := cat2.Packages()
	if len(pkgs1) != len(official.CatalogSlugs()) {
		t.Fatalf("expected %d packages, got %d", len(official.CatalogSlugs()), len(pkgs1))
	}
	if len(pkgs1) != len(pkgs2) {
		t.Fatalf("package count mismatch across calls: %d vs %d", len(pkgs1), len(pkgs2))
	}
	for i := range pkgs1 {
		if pkgs1[i].PackageID != pkgs2[i].PackageID || pkgs1[i].ArchiveSHA256 != pkgs2[i].ArchiveSHA256 {
			t.Fatalf("non-deterministic archive digest for %s: %s vs %s", pkgs1[i].PackageID, pkgs1[i].ArchiveSHA256, pkgs2[i].ArchiveSHA256)
		}
		if pkgs1[i].OriginTier != "official" {
			t.Fatalf("expected official origin tier for %s, got %s", pkgs1[i].PackageID, pkgs1[i].OriginTier)
		}
	}

	rawKEK := make([]byte, vault.KeySizeBytes)
	for i := range rawKEK {
		rawKEK[i] = byte(i + 7)
	}
	kek, err := vault.NewStaticKEKProvider("kek_v1", rawKEK)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer kek.Close()

	_, pubs, err := kek.DeriveDomainEd25519PublicKeys(SigningDomain)
	if err != nil {
		t.Fatalf("DeriveDomainEd25519PublicKeys: %v", err)
	}
	keyring := installer.TrustedKeyring{RootPublicKeys: pubs}
	signFn := func(payload []byte) ([]byte, error) {
		_, sig, serr := kek.SignDomainEd25519(SigningDomain, payload)
		return sig, serr
	}

	stagingRoot := t.TempDir()
	for _, pkg := range pkgs1 {
		req, cleanup, rerr := cat1.ResolveStageRequest(pkg.SourceURI, pkg.ArchiveSHA256, signFn)
		if rerr != nil {
			t.Fatalf("ResolveStageRequest(%s): %v", pkg.SourceURI, rerr)
		}
		extracted, xerr := installer.ExtractSafeArchive(req.ArchiveReader, stagingRoot)
		if cleanup != nil {
			cleanup()
		}
		if xerr != nil {
			t.Fatalf("ExtractSafeArchive(%s): %v", pkg.PackageID, xerr)
		}
		rawSum := sha256.Sum256(cat1.byURI[pkg.SourceURI].ArchiveBytes)
		actualSHA := hex.EncodeToString(rawSum[:])
		if actualSHA != pkg.ArchiveSHA256 {
			t.Fatalf("digest mismatch for %s: %s vs %s", pkg.PackageID, actualSHA, pkg.ArchiveSHA256)
		}
		vm, verr := installer.ParseAndValidateManifest(extracted.ManifestBytes, installer.DefaultValidationPolicy(), extracted.FileSHA256)
		if verr != nil {
			t.Fatalf("ParseAndValidateManifest(%s): %v", pkg.PackageID, verr)
		}
		verdict, perr := installer.VerifyProvenance(vm, actualSHA, pkg.ArchiveSHA256, req.Signature, keyring)
		if perr != nil {
			t.Fatalf("VerifyProvenance(%s): %v", pkg.PackageID, perr)
		}
		if verdict.OriginTrustTier != "official" || !verdict.SignatureVerified {
			t.Fatalf("expected official verified verdict for %s, got %+v", pkg.PackageID, verdict)
		}

		// CT-05 enforcement remains strict without matching root key in keyring.
		if _, err := installer.VerifyProvenance(vm, actualSHA, pkg.ArchiveSHA256, req.Signature, installer.TrustedKeyring{}); !errors.Is(err, installer.ErrInvalidSignature) && !errors.Is(err, installer.ErrReservedNamespaceSpoof) {
			t.Fatalf("expected CT-05/signature rejection with empty keyring for %s, got %v", pkg.PackageID, err)
		}
	}
}

func TestBuiltinCatalog_RejectsInvalidURIAndDigest(t *testing.T) {
	t.Parallel()

	cat, err := NewCatalog()
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	if _, _, err := cat.ResolveStageRequest("tanpopo-local://openai.tpkg", "", nil); !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound for non-builtin scheme, got %v", err)
	}
	if _, _, err := cat.ResolveStageRequest("tanpopo-builtin://does-not-exist.tpkg", "", nil); !errors.Is(err, ErrPackageNotFound) || !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrPackageNotFound wrapping port.ErrNotFound for missing slug, got %v", err)
	}
	if req, _, err := cat.ResolveStageRequest("TANPOPO-BUILTIN://openai.tpkg", "", nil); err != nil || req == nil {
		t.Fatalf("expected mixed-case TANPOPO-BUILTIN scheme to resolve, got req=%v err=%v", req, err)
	}
	if _, _, err := cat.ResolveStageRequest("tanpopo-builtin://openai.tpkg", "0000000000000000000000000000000000000000000000000000000000000000", nil); !errors.Is(err, installer.ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch for wrong sha256, got %v", err)
	}
}

func TestDiagBuiltinDigests(t *testing.T) {
	cat, err := NewCatalog()
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	for _, pkg := range cat.Packages() {
		t.Logf("DIAG_PKG %s %s %s %d", pkg.PackageID, pkg.Version, pkg.ArchiveSHA256, len(cat.byURI[pkg.SourceURI].ArchiveBytes))
	}
}

