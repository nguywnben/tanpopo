// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package bundle

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
	"github.com/nguywnben/tanpopo/internal/security/trust"
)

// ExportOptions configures creation of a signed offline .tpbundle archive.
type ExportOptions struct {
	LockfilePath          string
	PackagesDir           string
	SigningKeyPath        string
	PublicKeyPath         string
	IndexFeedPath         string
	RevocationFeedPath    string
	IncludeRevocationFeed bool
	SBOMPaths             []string
	ArtifactPaths         []string
	OutputPath            string
	TanpopoVersion        string
	Now                   func() time.Time
}

// ExportResult reports the produced air-gap bundle digest and inventory.
type ExportResult struct {
	OutputPath       string   `json:"output_path" yaml:"output_path"`
	BundleSHA256     string   `json:"bundle_sha256" yaml:"bundle_sha256"`
	KeyID            string   `json:"key_id" yaml:"key_id"`
	SizeBytes        int64    `json:"size_bytes" yaml:"size_bytes"`
	ExtensionsCount  int      `json:"extensions_count" yaml:"extensions_count"`
	SBOMCount        int      `json:"sbom_count" yaml:"sbom_count"`
	ArtifactsCount   int      `json:"artifacts_count" yaml:"artifacts_count"`
	IncludesIndex    bool     `json:"includes_index" yaml:"includes_index"`
	IncludesRevoc    bool     `json:"includes_revocation_feed" yaml:"includes_revocation_feed"`
	ExtensionSlugs   []string `json:"extension_slugs,omitempty" yaml:"extension_slugs,omitempty"`
}

type archiveEntryPayload struct {
	path string
	data []byte
}

// ExportBundle packages locked .tpkg extensions, optional signed marketplace/revocation
// feeds, SBOMs, extra artifacts, and a detached Ed25519 signature into a deterministic .tar.zst .tpbundle.
func ExportBundle(ctx context.Context, opts ExportOptions) (*ExportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lockPath := strings.TrimSpace(opts.LockfilePath)
	if lockPath == "" {
		return nil, validationFail("--lockfile is required", nil)
	}
	outPath := strings.TrimSpace(opts.OutputPath)
	if outPath == "" {
		return nil, validationFail("--output is required", nil)
	}
	signPath := strings.TrimSpace(opts.SigningKeyPath)
	if signPath == "" {
		return nil, validationFail("--signing-key is required to sign the air-gap bundle", nil)
	}
	privKey, err := LoadPrivateKey(signPath)
	if err != nil {
		return nil, validationFail("failed loading signing private key", err)
	}
	derivedPub := privKey.Public().(ed25519.PublicKey)
	keyID := trust.PublicKeyFingerprint(derivedPub)

	if pubPath := strings.TrimSpace(opts.PublicKeyPath); pubPath != "" {
		pubKey, pubFP, perr := LoadPublicKey(pubPath)
		if perr != nil {
			return nil, validationFail("failed loading public key", perr)
		}
		if pubFP != keyID || !bytes.Equal(pubKey, derivedPub) {
			return nil, validationFail("--public-key does not match --signing-key public key", nil)
		}
	}
	pubPEM, err := encodePublicKeyPEM(derivedPub)
	if err != nil {
		return nil, err
	}

	nowFn := time.Now
	if opts.Now != nil {
		nowFn = opts.Now
	}
	now := nowFn().UTC()

	lockRaw, err := readRegularFileBounded(lockPath, maxLockOrFeedBytes)
	if err != nil {
		return nil, validationFail(fmt.Sprintf("failed reading lockfile %q", lockPath), err)
	}
	lf, err := lockfile.ParseBytes(lockRaw)
	if err != nil {
		return nil, contractFail(fmt.Sprintf("invalid tanpopo.lock %q", lockPath), err)
	}

	packagesDir := strings.TrimSpace(opts.PackagesDir)
	if packagesDir == "" {
		packagesDir = filepath.Dir(lockPath)
	}
	tpkgBySHA, err := indexLocalTpkgArchives(packagesDir)
	if err != nil {
		return nil, validationFail(fmt.Sprintf("failed scanning packages directory %q", packagesDir), err)
	}

	manifest := BundleManifest{
		SchemaVersion:  FormatVersion,
		CreatedAt:      now.Format(time.RFC3339),
		TanpopoVersion: strings.TrimSpace(opts.TanpopoVersion),
		KeyID:          keyID,
		Lockfile: &ManifestFileRef{
			Path:      lockfileEntryName,
			SHA256:    sha256HexBytes(lockRaw),
			SizeBytes: int64(len(lockRaw)),
		},
		TrustRootPub: &ManifestFileRef{
			Path:      trustRootName,
			SHA256:    sha256HexBytes(pubPEM),
			SizeBytes: int64(len(pubPEM)),
		},
		Artifacts: make([]ArtifactEntry, 0, len(lf.Extensions)+len(opts.SBOMPaths)+len(opts.ArtifactPaths)),
	}

	var entries []archiveEntryPayload
	entries = append(entries,
		archiveEntryPayload{path: lockfileEntryName, data: lockRaw},
		archiveEntryPayload{path: trustRootName, data: pubPEM},
	)

	pkgIDs := make([]string, 0, len(lf.Extensions))
	for pkgID := range lf.Extensions {
		pkgIDs = append(pkgIDs, pkgID)
	}
	sort.Strings(pkgIDs)

	seenPaths := map[string]struct{}{
		manifestFileName:  {},
		signatureFileName: {},
		lockfileEntryName: {},
		trustRootName:     {},
	}

	for _, pkgID := range pkgIDs {
		le := lf.Extensions[pkgID]
		wantSHA := strings.ToLower(strings.TrimSpace(le.ArtifactSHA256))
		rawArchive, ok := tpkgBySHA[wantSHA]
		if !ok {
			return nil, contractFail(fmt.Sprintf("locked extension %s (%s) with sha256 %s not found in %q", pkgID, le.Version, wantSHA, packagesDir), nil)
		}
		tmpStage, serr := os.MkdirTemp("", "tanpopo-export-tpkg-*")
		if serr != nil {
			return nil, serr
		}
		extracted, xerr := installer.ExtractSafeArchive(bytes.NewReader(rawArchive), tmpStage)
		_ = os.RemoveAll(tmpStage)
		if xerr != nil {
			return nil, contractFail(fmt.Sprintf("locked extension %s failed .tpkg archive verification", pkgID), xerr)
		}
		if !equalDigestHex(extracted.BundleSHA256, wantSHA) {
			return nil, contractFail(fmt.Sprintf("locked extension %s digest mismatch", pkgID), nil)
		}
		archivePath := fmt.Sprintf("packages/%s-%s.tpkg", safePackageSlug(pkgID), wantSHA[:12])
		if _, dup := seenPaths[archivePath]; dup {
			return nil, validationFail(fmt.Sprintf("duplicate archive path %q", archivePath), nil)
		}
		seenPaths[archivePath] = struct{}{}
		entries = append(entries, archiveEntryPayload{path: archivePath, data: rawArchive})
		manifest.Artifacts = append(manifest.Artifacts, ArtifactEntry{
			Path:      archivePath,
			Kind:      "extension",
			PackageID: pkgID,
			Version:   le.Version,
			SHA256:    wantSHA,
			SizeBytes: int64(len(rawArchive)),
		})
	}

	keyring := registry.Keyring{keyID: derivedPub}

	if idxPath := strings.TrimSpace(opts.IndexFeedPath); idxPath != "" {
		rawIdx, rerr := readRegularFileBounded(idxPath, maxLockOrFeedBytes)
		if rerr != nil {
			return nil, validationFail(fmt.Sprintf("failed reading index feed %q", idxPath), rerr)
		}
		verifiedIdx, verr := registry.VerifyFeed(registry.FeedIndex, rawIdx, keyring, 0, now)
		if verr != nil {
			return nil, contractFail(fmt.Sprintf("index feed %q failed verification", idxPath), verr)
		}
		if _, derr := registry.DecodeIndexEntries(verifiedIdx); derr != nil {
			return nil, contractFail(fmt.Sprintf("index feed %q contains invalid entries", idxPath), derr)
		}
		seenPaths[indexEntryName] = struct{}{}
		manifest.IndexFeed = &ManifestFileRef{Path: indexEntryName, SHA256: sha256HexBytes(rawIdx), SizeBytes: int64(len(rawIdx))}
		entries = append(entries, archiveEntryPayload{path: indexEntryName, data: rawIdx})
	}

	revPath := strings.TrimSpace(opts.RevocationFeedPath)
	if revPath == "" && opts.IncludeRevocationFeed {
		candidate := filepath.Join(filepath.Dir(lockPath), "revocations.json")
		if _, lerr := os.Lstat(candidate); lerr == nil {
			revPath = candidate
		} else {
			return nil, validationFail("--include-revocation-feed set but no --revocation-feed or adjacent revocations.json found", lerr)
		}
	}
	if revPath != "" {
		rawRev, rerr := readRegularFileBounded(revPath, maxLockOrFeedBytes)
		if rerr != nil {
			return nil, validationFail(fmt.Sprintf("failed reading revocation feed %q", revPath), rerr)
		}
		verifiedRev, verr := registry.VerifyFeed(registry.FeedRevocation, rawRev, keyring, 0, now)
		if verr != nil {
			return nil, contractFail(fmt.Sprintf("revocation feed %q failed verification", revPath), verr)
		}
		if _, derr := registry.DecodeRevocationEntries(verifiedRev); derr != nil {
			return nil, contractFail(fmt.Sprintf("revocation feed %q contains invalid entries", revPath), derr)
		}
		seenPaths[revocEntryName] = struct{}{}
		manifest.RevocationFeed = &ManifestFileRef{Path: revocEntryName, SHA256: sha256HexBytes(rawRev), SizeBytes: int64(len(rawRev))}
		entries = append(entries, archiveEntryPayload{path: revocEntryName, data: rawRev})
	}

	sbomCount := 0
	for _, p := range opts.SBOMPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		raw, rerr := readRegularFileBounded(p, maxLockOrFeedBytes)
		if rerr != nil {
			return nil, validationFail(fmt.Sprintf("failed reading SBOM %q", p), rerr)
		}
		entryPath, cerr := cleanArchivePath("sbom/" + filepath.Base(p))
		if cerr != nil {
			return nil, validationFail("invalid SBOM file name", cerr)
		}
		if _, dup := seenPaths[entryPath]; dup {
			return nil, validationFail(fmt.Sprintf("duplicate bundle path %q", entryPath), nil)
		}
		seenPaths[entryPath] = struct{}{}
		entries = append(entries, archiveEntryPayload{path: entryPath, data: raw})
		manifest.Artifacts = append(manifest.Artifacts, ArtifactEntry{
			Path:      entryPath,
			Kind:      "sbom",
			SHA256:    sha256HexBytes(raw),
			SizeBytes: int64(len(raw)),
		})
		sbomCount++
	}

	artifactCount := 0
	for _, p := range opts.ArtifactPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		raw, rerr := readRegularFileBounded(p, MaxBundleEntryBytes)
		if rerr != nil {
			return nil, validationFail(fmt.Sprintf("failed reading artifact %q", p), rerr)
		}
		entryPath, cerr := cleanArchivePath("artifacts/" + filepath.Base(p))
		if cerr != nil {
			return nil, validationFail("invalid artifact file name", cerr)
		}
		if _, dup := seenPaths[entryPath]; dup {
			return nil, validationFail(fmt.Sprintf("duplicate bundle path %q", entryPath), nil)
		}
		seenPaths[entryPath] = struct{}{}
		entries = append(entries, archiveEntryPayload{path: entryPath, data: raw})
		manifest.Artifacts = append(manifest.Artifacts, ArtifactEntry{
			Path:      entryPath,
			Kind:      "artifact",
			SHA256:    sha256HexBytes(raw),
			SizeBytes: int64(len(raw)),
		})
		artifactCount++
	}

	sort.Slice(manifest.Artifacts, func(i, j int) bool {
		return manifest.Artifacts[i].Path < manifest.Artifacts[j].Path
	})

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	sigRaw := ed25519.Sign(privKey, append([]byte(bundleSigningDomainPrefix), manifestBytes...))
	sigDoc := BundleSignature{
		Algorithm: "ed25519",
		KeyID:     keyID,
		Signature: base64.StdEncoding.EncodeToString(sigRaw),
	}
	sigBytes, err := json.MarshalIndent(sigDoc, "", "  ")
	if err != nil {
		return nil, err
	}

	entries = append(entries,
		archiveEntryPayload{path: manifestFileName, data: manifestBytes},
		archiveEntryPayload{path: signatureFileName, data: sigBytes},
	)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})
	if len(entries) > MaxBundleEntries {
		return nil, validationFail(fmt.Sprintf("bundle exceeds maximum %d entries", MaxBundleEntries), nil)
	}

	var rawArchiveBuf bytes.Buffer
	zw, err := zstd.NewWriter(&rawArchiveBuf, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(zw)
	epochZero := time.Unix(0, 0).UTC()
	for _, entry := range entries {
		hdr := &tar.Header{
			Name:     entry.path,
			Mode:     0o644,
			Size:     int64(len(entry.data)),
			ModTime:  epochZero,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			_ = tw.Close()
			_ = zw.Close()
			return nil, err
		}
		if _, err := tw.Write(entry.data); err != nil {
			_ = tw.Close()
			_ = zw.Close()
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	archiveBytes := rawArchiveBuf.Bytes()
	if int64(len(archiveBytes)) > MaxBundleCompressedBytes {
		return nil, validationFail("exported archive exceeds compressed size limit", nil)
	}

	if err := writeFileAtomic(outPath, archiveBytes, 0o644); err != nil {
		return nil, err
	}

	return &ExportResult{
		OutputPath:      outPath,
		BundleSHA256:    sha256HexBytes(archiveBytes),
		KeyID:           keyID,
		SizeBytes:       int64(len(archiveBytes)),
		ExtensionsCount: len(pkgIDs),
		SBOMCount:       sbomCount,
		ArtifactsCount:  artifactCount,
		IncludesIndex:   manifest.IndexFeed != nil,
		IncludesRevoc:   manifest.RevocationFeed != nil,
		ExtensionSlugs:  pkgIDs,
	}, nil
}

func indexLocalTpkgArchives(dir string) (map[string][]byte, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(absDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	items, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte)
	for _, item := range items {
		if item.IsDir() || item.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(strings.ToLower(item.Name()), ".tpkg") {
			continue
		}
		fi, serr := root.Lstat(item.Name())
		if serr != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() || fi.Size() > MaxBundleEntryBytes {
			continue
		}
		data, rerr := root.ReadFile(item.Name())
		if rerr != nil {
			return nil, rerr
		}
		out[sha256HexBytes(data)] = data
	}
	return out, nil
}
