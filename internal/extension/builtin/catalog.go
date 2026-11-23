// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package builtin deterministically packs Tanpopo's embedded official adapters
// into verified .tpkg archives and resolves tanpopo-builtin:// marketplace URIs
// without external network access.
package builtin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/extensions/official"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// SourceScheme identifies official adapter packages bundled directly in the binary.
	SourceScheme = "tanpopo-builtin://"
	// SigningDomain binds derived per-deployment Ed25519 root keys to built-in packages.
	SigningDomain = "tanpopo:builtin-official-root-ed25519:v1"
)

// ErrPackageNotFound indicates a requested tanpopo-builtin:// URI does not match any bundled adapter.
var ErrPackageNotFound = fmt.Errorf("%w: builtin official package not found", port.ErrNotFound)

// Package holds an embedded official extension package alongside its deterministic .tpkg archive.
type Package struct {
	Slug          string
	PackageID     string
	DisplayName   string
	Version       string
	ExecutionTier string
	OriginTier    string
	SourceURI     string
	ArchiveSHA256 string
	ArchiveBytes  []byte
	Summary       string
	Capabilities  []string
}

// Catalog holds all built-in official packages pre-validated and packed once at startup.
type Catalog struct {
	ordered []*Package
	byURI   map[string]*Package
}

// SignerFunc signs a canonical SignatureSigningPayload with the instance root key.
type SignerFunc func(payload []byte) ([]byte, error)

// NewCatalog reads extensions/official assets from the binary, validates every
// manifest, and builds deterministic .tpkg archives in memory.
func NewCatalog() (*Catalog, error) {
	fsys := official.FS()
	slugs := official.CatalogSlugs()
	cat := &Catalog{
		ordered: make([]*Package, 0, len(slugs)),
		byURI:   make(map[string]*Package, len(slugs)),
	}
	for _, slug := range slugs {
		pkg, err := buildOfficialPackage(fsys, slug)
		if err != nil {
			return nil, fmt.Errorf("builtin: build package %q: %w", slug, err)
		}
		cat.ordered = append(cat.ordered, pkg)
		cat.byURI[pkg.SourceURI] = pkg
	}
	return cat, nil
}

// Packages returns a shallow copy of the built-in package descriptors sorted by PackageID.
func (c *Catalog) Packages() []Package {
	if c == nil || len(c.ordered) == 0 {
		return nil
	}
	out := make([]Package, len(c.ordered))
	for i, pkg := range c.ordered {
		out[i] = Package{
			Slug:          pkg.Slug,
			PackageID:     pkg.PackageID,
			DisplayName:   pkg.DisplayName,
			Version:       pkg.Version,
			ExecutionTier: pkg.ExecutionTier,
			OriginTier:    pkg.OriginTier,
			SourceURI:     pkg.SourceURI,
			ArchiveSHA256: pkg.ArchiveSHA256,
			Summary:       pkg.Summary,
			Capabilities:  append([]string(nil), pkg.Capabilities...),
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].PackageID < out[j].PackageID
	})
	return out
}

// ResolveStageRequest resolves a tanpopo-builtin://<slug>.tpkg URI, verifies its
// expected SHA-256 digest when supplied, and attaches an authoritative root signature
// when signer is provided.
func (c *Catalog) ResolveStageRequest(sourceURI, expectedSHA256 string, signer SignerFunc) (*installer.StageInstallRequest, func(), error) {
	if c == nil {
		return nil, nil, ErrPackageNotFound
	}
	trimmedURI := strings.TrimSpace(sourceURI)
	if !strings.HasPrefix(strings.ToLower(trimmedURI), SourceScheme) {
		return nil, nil, fmt.Errorf("%w: unsupported scheme %q", ErrPackageNotFound, trimmedURI)
	}
	lookupURI := SourceScheme + trimmedURI[len(SourceScheme):]
	pkg, ok := c.byURI[lookupURI]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %q", ErrPackageNotFound, trimmedURI)
	}

	exp := strings.ToLower(strings.TrimSpace(expectedSHA256))
	if exp != "" {
		if len(exp) != len(pkg.ArchiveSHA256) || subtle.ConstantTimeCompare([]byte(exp), []byte(pkg.ArchiveSHA256)) != 1 {
			return nil, nil, fmt.Errorf("%w: expected=%s actual=%s", installer.ErrDigestMismatch, exp, pkg.ArchiveSHA256)
		}
	}

	var sig installer.ProvenanceSignature
	if signer != nil {
		payload := installer.SignatureSigningPayload(pkg.PackageID, pkg.Version, pkg.ArchiveSHA256)
		rawSig, err := signer(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("builtin: sign package %q: %w", pkg.PackageID, err)
		}
		sig.PublisherOrRootSig = rawSig
	}

	return &installer.StageInstallRequest{
		ArchiveReader: bytes.NewReader(pkg.ArchiveBytes),
		Source: installer.SourceSpec{
			Kind:           installer.SourceKindLocalArchive,
			URI:            pkg.SourceURI,
			ExpectedSHA256: pkg.ArchiveSHA256,
		},
		Signature: sig,
	}, nil, nil
}

func buildOfficialPackage(fsys fs.FS, slug string) (*Package, error) {
	var files []string
	err := fs.WalkDir(fsys, slug, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no embedded files for %q", slug)
	}
	sort.Strings(files)

	var manifestBytes []byte
	fileHashes := make(map[string]string, len(files))

	var rawArchive bytes.Buffer
	gw, err := gzip.NewWriterLevel(&rawArchive, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	gw.Header.ModTime = time.Unix(0, 0).UTC()
	gw.Header.OS = 255
	tw := tar.NewWriter(gw)

	for _, fullPath := range files {
		relPath := strings.TrimPrefix(fullPath, slug+"/")
		data, rerr := fs.ReadFile(fsys, fullPath)
		if rerr != nil {
			_ = tw.Close()
			_ = gw.Close()
			return nil, fmt.Errorf("read %s: %w", fullPath, rerr)
		}
		fileSum := sha256.Sum256(data)
		fileHashes[relPath] = hex.EncodeToString(fileSum[:])
		if relPath == "manifest.yaml" {
			manifestBytes = data
		}
		hdr := &tar.Header{
			Name:     relPath,
			Mode:     0o644,
			Size:     int64(len(data)),
			ModTime:  time.Unix(0, 0).UTC(),
			Typeflag: tar.TypeReg,
			Format:   tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		_ = gw.Close()
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	vm, err := installer.ParseAndValidateManifest(manifestBytes, installer.DefaultValidationPolicy(), fileHashes)
	if err != nil {
		return nil, fmt.Errorf("validate manifest.yaml: %w", err)
	}

	archiveBytes := rawArchive.Bytes()
	sum := sha256.Sum256(archiveBytes)
	digest := hex.EncodeToString(sum[:])

	caps := make([]string, 0, len(vm.Manifest.Capabilities))
	seen := make(map[string]struct{}, len(vm.Manifest.Capabilities))
	for _, capItem := range vm.Manifest.Capabilities {
		capType := strings.TrimSpace(capItem.Type)
		if capType == "" {
			continue
		}
		if _, ok := seen[capType]; !ok {
			seen[capType] = struct{}{}
			caps = append(caps, capType)
		}
	}

	displayName := strings.TrimSpace(vm.Manifest.Metadata.Name)
	if displayName == "" {
		displayName = vm.Manifest.Metadata.ID
	}

	return &Package{
		Slug:          slug,
		PackageID:     vm.Manifest.Metadata.ID,
		DisplayName:   displayName,
		Version:       vm.Manifest.Metadata.Version,
		ExecutionTier: apiExecutionTier(vm.ExecutionTier),
		OriginTier:    trust.TierOfficial,
		SourceURI:     SourceScheme + slug + ".tpkg",
		ArchiveSHA256: digest,
		ArchiveBytes:  archiveBytes,
		Summary:       strings.TrimSpace(vm.Manifest.Metadata.Description),
		Capabilities:  caps,
	}, nil
}

func apiExecutionTier(sqlTier string) string {
	switch sqlTier {
	case installer.ExecutionTierDeclarativeT1:
		return "declarative"
	case installer.ExecutionTierWasmSandboxT2:
		return "sandboxed_wasm"
	case installer.ExecutionTierIsolatedSidecarT3:
		return "isolated_sidecar"
	default:
		return "declarative"
	}
}
