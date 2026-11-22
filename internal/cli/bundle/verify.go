// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package bundle

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
)

// VerifyOptions configures offline cryptographic validation of a .tpbundle archive.
type VerifyOptions struct {
	BundlePath            string
	PublicKeyPath         string
	DataDir               string
	LastIndexSerial       int64
	LastRevocationSerial  int64
	Now                   func() time.Time
	LookupEnv             func(string) (string, bool)
}

// VerifyResult reports verified bundle metadata, digests, and extension packages.
type VerifyResult struct {
	Status              string   `json:"status" yaml:"status"`
	BundlePath          string   `json:"bundle_path" yaml:"bundle_path"`
	BundleSHA256        string   `json:"bundle_sha256" yaml:"bundle_sha256"`
	KeyID               string   `json:"key_id" yaml:"key_id"`
	VerifiedEntries     int      `json:"verified_entries" yaml:"verified_entries"`
	ExtensionSlugs      []string `json:"extension_slugs" yaml:"extension_slugs"`
	IndexSerial         int64    `json:"index_serial,omitempty" yaml:"index_serial,omitempty"`
	RevocationSerial    int64    `json:"revocation_serial,omitempty" yaml:"revocation_serial,omitempty"`
	RevocationExpiresAt string   `json:"revocation_expires_at,omitempty" yaml:"revocation_expires_at,omitempty"`
}

type verifiedBundleContents struct {
	Result       *VerifyResult
	Manifest     BundleManifest
	Files        map[string][]byte
	IndexEntries []registry.IndexEntry
	IndexFeed    *registry.VerifiedFeed
	RevEntries   []registry.RevocationEntry
	RevFeed      *registry.VerifiedFeed
}

// VerifyBundle executes strict zero-network signature, digest, .tpkg safety, and feed
// freshness verification on a .tpbundle archive, returning ExitContract (22) on failure.
func VerifyBundle(ctx context.Context, opts VerifyOptions) (*VerifyResult, error) {
	verified, err := verifyAndReadBundle(ctx, opts)
	if err != nil {
		return nil, err
	}
	return verified.Result, nil
}

func verifyAndReadBundle(ctx context.Context, opts VerifyOptions) (*verifiedBundleContents, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bundlePath := strings.TrimSpace(opts.BundlePath)
	if bundlePath == "" {
		return nil, validationFail("bundle path is required", nil)
	}
	lookupEnv := opts.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	pubKey, keyFP, err := ResolveTrustRootKey(opts.PublicKeyPath, opts.DataDir, lookupEnv)
	if err != nil {
		return nil, err
	}

	rawBundle, err := readRegularFileBounded(bundlePath, MaxBundleCompressedBytes)
	if err != nil {
		return nil, contractFail(fmt.Sprintf("failed reading bundle %q", bundlePath), err)
	}
	bundleSHA256 := sha256HexBytes(rawBundle)

	zr, err := zstd.NewReader(bytes.NewReader(rawBundle), zstd.WithDecoderMaxMemory(128<<20))
	if err != nil {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: invalid zstd stream", bundlePath), err)
	}
	defer zr.Close()

	limitedTar := io.LimitReader(zr, MaxBundleUncompressedBytes+1)
	tr := tar.NewReader(limitedTar)
	files := make(map[string][]byte)
	var totalUncompressed int64

	for {
		hdr, herr := tr.Next()
		if errors.Is(herr, io.EOF) {
			break
		}
		if herr != nil {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: corrupt tar framing", bundlePath), herr)
		}
		if len(files) >= MaxBundleEntries {
			return nil, contractFail(fmt.Sprintf("bundle exceeds maximum %d entries", MaxBundleEntries), nil)
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: prohibited non-regular tar entry (type %d)", hdr.Name, hdr.Typeflag), nil)
		}
		cleanName, cerr := cleanArchivePath(hdr.Name)
		if cerr != nil {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: invalid archive path", hdr.Name), cerr)
		}
		if _, dup := files[cleanName]; dup {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: duplicate archive entry", cleanName), nil)
		}
		if hdr.Size < 0 || hdr.Size > MaxBundleEntryBytes {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: entry size %d outside bounds", cleanName, hdr.Size), nil)
		}
		data, rerr := io.ReadAll(io.LimitReader(tr, MaxBundleEntryBytes+1))
		if rerr != nil {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: failed reading entry", cleanName), rerr)
		}
		if int64(len(data)) != hdr.Size || int64(len(data)) > MaxBundleEntryBytes {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: truncated or oversized entry", cleanName), nil)
		}
		totalUncompressed += int64(len(data))
		if totalUncompressed > MaxBundleUncompressedBytes {
			return nil, contractFail("bundle exceeds maximum uncompressed size limit", nil)
		}
		if totalUncompressed > minRatioCheckBytes && int64(len(rawBundle)) > 0 && (totalUncompressed/int64(len(rawBundle))) > MaxBundleRatio {
			return nil, contractFail("bundle decompression ratio exceeds bomb guard threshold", nil)
		}
		files[cleanName] = data
	}

	manifestBytes, ok := files[manifestFileName]
	if !ok {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: missing manifest", manifestFileName), nil)
	}
	sigBytes, ok := files[signatureFileName]
	if !ok {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: missing detached signature", signatureFileName), nil)
	}

	var sigDoc BundleSignature
	if err := decodeStrictJSON(sigBytes, &sigDoc); err != nil {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: malformed signature envelope", signatureFileName), err)
	}
	if strings.ToLower(strings.TrimSpace(sigDoc.Algorithm)) != "ed25519" {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: unsupported signature algorithm %q", signatureFileName, sigDoc.Algorithm), nil)
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(sigDoc.KeyID))), []byte(strings.ToLower(keyFP))) != 1 {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: signing key fingerprint %q does not match pinned trust root %q", signatureFileName, sigDoc.KeyID, keyFP), nil)
	}
	sigRaw, err := base64.StdEncoding.DecodeString(sigDoc.Signature)
	if err != nil || len(sigRaw) != ed25519.SignatureSize {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: malformed Ed25519 signature bytes", signatureFileName), err)
	}
	signedPayload := append([]byte(bundleSigningDomainPrefix), manifestBytes...)
	if !ed25519.Verify(pubKey, signedPayload, sigRaw) {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: Ed25519 bundle signature verification failed", signatureFileName), nil)
	}

	var manifest BundleManifest
	if err := decodeStrictJSON(manifestBytes, &manifest); err != nil {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: malformed bundle manifest", manifestFileName), err)
	}
	if manifest.SchemaVersion != FormatVersion {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: unsupported schema_version %q", manifestFileName, manifest.SchemaVersion), nil)
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(manifest.KeyID))), []byte(strings.ToLower(keyFP))) != 1 {
		return nil, contractFail(fmt.Sprintf("offending artifact %q: manifest key_id mismatch", manifestFileName), nil)
	}

	expectedPaths := map[string]struct{}{
		manifestFileName:  {},
		signatureFileName: {},
	}
	verifyFileRef := func(ref *ManifestFileRef, expectedName string) error {
		if ref == nil {
			return nil
		}
		if ref.Path != expectedName {
			return contractFail(fmt.Sprintf("offending artifact %q: unexpected manifest reference path", ref.Path), nil)
		}
		raw, exists := files[ref.Path]
		if !exists {
			return contractFail(fmt.Sprintf("offending artifact %q: missing from archive", ref.Path), nil)
		}
		if int64(len(raw)) != ref.SizeBytes || !equalDigestHex(sha256HexBytes(raw), ref.SHA256) {
			return contractFail(fmt.Sprintf("offending artifact %q: sha256 digest mismatch", ref.Path), nil)
		}
		expectedPaths[ref.Path] = struct{}{}
		return nil
	}

	if err := verifyFileRef(manifest.Lockfile, lockfileEntryName); err != nil {
		return nil, err
	}
	if err := verifyFileRef(manifest.IndexFeed, indexEntryName); err != nil {
		return nil, err
	}
	if err := verifyFileRef(manifest.RevocationFeed, revocEntryName); err != nil {
		return nil, err
	}
	if err := verifyFileRef(manifest.TrustRootPub, trustRootName); err != nil {
		return nil, err
	}

	var extByPkg = make(map[string]ArtifactEntry)
	for _, art := range manifest.Artifacts {
		cleanArtPath, cerr := cleanArchivePath(art.Path)
		if cerr != nil || cleanArtPath != art.Path {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: invalid artifact path", art.Path), cerr)
		}
		raw, exists := files[art.Path]
		if !exists {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: missing from archive", art.Path), nil)
		}
		if int64(len(raw)) != art.SizeBytes || !equalDigestHex(sha256HexBytes(raw), art.SHA256) {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: sha256 digest mismatch", art.Path), nil)
		}
		expectedPaths[art.Path] = struct{}{}
		if art.Kind == "extension" {
			tmpStage, serr := os.MkdirTemp("", "tanpopo-verify-tpkg-*")
			if serr != nil {
				return nil, serr
			}
			extracted, xerr := installer.ExtractSafeArchive(bytes.NewReader(raw), tmpStage)
			_ = os.RemoveAll(tmpStage)
			if xerr != nil {
				return nil, contractFail(fmt.Sprintf("offending artifact %q: inner .tpkg failed archive integrity check", art.Path), xerr)
			}
			if !equalDigestHex(extracted.BundleSHA256, art.SHA256) {
				return nil, contractFail(fmt.Sprintf("offending artifact %q: inner .tpkg digest mismatch", art.Path), nil)
			}
			extByPkg[art.PackageID] = art
		}
	}

	// Refuse any unmanifested file inside .tpbundle (fail-closed anti-smuggling).
	for name := range files {
		if _, known := expectedPaths[name]; !known {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: unmanifested entry forbidden inside bundle", name), nil)
		}
	}

	var extSlugs []string
	if manifest.Lockfile != nil {
		lf, lerr := lockfile.ParseBytes(files[lockfileEntryName])
		if lerr != nil {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: invalid lockfile", lockfileEntryName), lerr)
		}
		for pkgID, le := range lf.Extensions {
			art, ok := extByPkg[pkgID]
			if !ok {
				return nil, contractFail(fmt.Sprintf("offending artifact %q: extension missing from bundle", pkgID), nil)
			}
			if !equalDigestHex(art.SHA256, le.ArtifactSHA256) || art.Version != le.Version {
				return nil, contractFail(fmt.Sprintf("offending artifact %q: extension does not match tanpopo.lock", art.Path), nil)
			}
			extSlugs = append(extSlugs, pkgID)
		}
		sort.Strings(extSlugs)
	}

	nowFn := time.Now
	if opts.Now != nil {
		nowFn = opts.Now
	}
	now := nowFn().UTC()
	keyring := registry.Keyring{keyFP: pubKey}

	res := &VerifyResult{
		Status:          "VERIFIED",
		BundlePath:      bundlePath,
		BundleSHA256:    bundleSHA256,
		KeyID:           keyFP,
		VerifiedEntries: len(expectedPaths),
		ExtensionSlugs:  extSlugs,
	}

	var idxFeed *registry.VerifiedFeed
	var idxEntries []registry.IndexEntry
	if manifest.IndexFeed != nil {
		var verr error
		idxFeed, verr = registry.VerifyFeed(registry.FeedIndex, files[indexEntryName], keyring, opts.LastIndexSerial, now)
		if verr != nil {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: index feed verification failed (%v)", indexEntryName, verr), verr)
		}
		idxEntries, verr = registry.DecodeIndexEntries(idxFeed)
		if verr != nil {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: invalid index feed entries", indexEntryName), verr)
		}
		res.IndexSerial = idxFeed.VersionSerial
	}

	var revFeed *registry.VerifiedFeed
	var revEntries []registry.RevocationEntry
	if manifest.RevocationFeed != nil {
		var verr error
		revFeed, verr = registry.VerifyFeed(registry.FeedRevocation, files[revocEntryName], keyring, opts.LastRevocationSerial, now)
		if verr != nil {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: revocation feed verification failed (%v)", revocEntryName, verr), verr)
		}
		revEntries, verr = registry.DecodeRevocationEntries(revFeed)
		if verr != nil {
			return nil, contractFail(fmt.Sprintf("offending artifact %q: invalid revocation feed entries", revocEntryName), verr)
		}
		res.RevocationSerial = revFeed.VersionSerial
		res.RevocationExpiresAt = revFeed.ExpiresAt.UTC().Format(time.RFC3339)
	}

	return &verifiedBundleContents{
		Result:       res,
		Manifest:     manifest,
		Files:        files,
		IndexEntries: idxEntries,
		IndexFeed:    idxFeed,
		RevEntries:   revEntries,
		RevFeed:      revFeed,
	}, nil
}

func decodeStrictJSON(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data forbidden")
	}
	return nil
}
