// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func buildSignedTestFixtures(t *testing.T, expiryOffset time.Duration) (
	pub ed25519.PublicKey,
	priv ed25519.PrivateKey,
	pubPath string,
	privPath string,
	lockPath string,
	pkgDir string,
	idxPath string,
	revPath string,
	sbomPath string,
	now time.Time,
) {
	t.Helper()
	now = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

	var err error
	pub, priv, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	dir := t.TempDir()
	pubDER, _ := x509.MarshalPKIXPublicKey(pub)
	privDER, _ := x509.MarshalPKCS8PrivateKey(priv)
	pubPath = filepath.Join(dir, "trust-root.pub")
	privPath = filepath.Join(dir, "trust-root.key")
	if err := os.WriteFile(pubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o644); err != nil {
		t.Fatalf("write pub: %v", err)
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0o600); err != nil {
		t.Fatalf("write priv: %v", err)
	}

	pkgDir = filepath.Join(dir, "packages")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir pkgDir: %v", err)
	}
	manifestYAML := `contract_version: "0.2"
id: "@acme/offline-adapter"
name: "Acme Offline Adapter"
version: "1.0.0"
kind: "provider_adapter"
execution_tier: "tier_declarative"
description: "Hermetic test extension"
license: "Apache-2.0"
capabilities:
  - "provider.chat_completions"
permissions:
  egress:
    allowed_hosts: ["api.internal.example"]
    require_tls: true
    allow_private_ip: false
adapter:
  protocol: "openai"
  base_url: "https://api.internal.example/v1"
  chat_completions_path: "/chat/completions"
  auth:
    mode: "bearer"
    secret_slot: "api_key"
`
	tpkgBytes := makeTestTpkgArchive(t, map[string][]byte{
		"manifest.yaml": []byte(manifestYAML),
	})
	sum := sha256.Sum256(tpkgBytes)
	tpkgSHA := hex.EncodeToString(sum[:])
	tpkgPath := filepath.Join(pkgDir, "acme-offline-adapter.tpkg")
	if err := os.WriteFile(tpkgPath, tpkgBytes, 0o644); err != nil {
		t.Fatalf("write .tpkg: %v", err)
	}

	lf := lockfile.NewEmpty(now)
	lf.Extensions["@acme/offline-adapter"] = lockfile.LockedEntry{
		Version:         "1.0.0",
		Origin:          "tanpopo-local://acme-offline-adapter.tpkg",
		ArtifactSHA256:  tpkgSHA,
		ContractVersion: "0.2",
		TrustTier:       trust.TierVerified,
		ExecutionTier:   "DECLARATIVE_T1",
		ApprovedPermissions: lockfile.ApprovedPermissions{
			ApprovedBy:   "sec@acme.test",
			ApprovedAt:   now.Format(time.RFC3339),
			AllowedHosts: []string{"api.internal.example"},
			RequireTLS:   true,
			SecretSlots:  []string{"api_key"},
		},
	}
	lockPath = filepath.Join(dir, "tanpopo.lock")
	lockBytes, merr := lf.MarshalDeterministic()
	if merr != nil {
		t.Fatalf("marshal tanpopo.lock: %v", merr)
	}
	if err := os.WriteFile(lockPath, lockBytes, 0o600); err != nil {
		t.Fatalf("write tanpopo.lock: %v", err)
	}

	keyID := trust.PublicKeyFingerprint(pub)
	idxEntries, _ := json.Marshal([]registry.IndexEntry{{
		PackageSlug: "@acme/offline-adapter",
		Version:     "1.0.0",
		Digest:      tpkgSHA,
		OCIURI:      "oci://harbor.corp.internal/tanpopo-extensions/acme-offline@sha256:" + tpkgSHA,
	}})
	idxPath = filepath.Join(dir, "index.json")
	if err := os.WriteFile(idxPath, signFeedEnvelope(t, registry.FeedIndex, 10, now.Add(24*time.Hour), idxEntries, keyID, priv), 0o644); err != nil {
		t.Fatalf("write index.json: %v", err)
	}

	revEntries, _ := json.Marshal([]registry.RevocationEntry{{
		PackageSlug:     "@bad/revoked-ext",
		VersionOrSHA256: "0.9.0",
		Severity:        "CRITICAL",
		Reason:          "malicious exfiltration",
	}})
	revPath = filepath.Join(dir, "revocations.json")
	if err := os.WriteFile(revPath, signFeedEnvelope(t, registry.FeedRevocation, 4, now.Add(expiryOffset), revEntries, keyID, priv), 0o644); err != nil {
		t.Fatalf("write revocations.json: %v", err)
	}

	sbomPath = filepath.Join(dir, "bom.cdx.json")
	if err := os.WriteFile(sbomPath, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.5"}`), 0o644); err != nil {
		t.Fatalf("write sbom: %v", err)
	}

	return pub, priv, pubPath, privPath, lockPath, pkgDir, idxPath, revPath, sbomPath, now
}

func signFeedEnvelope(t *testing.T, kind registry.FeedKind, serial int64, expiresAt time.Time, entries []byte, keyID string, priv ed25519.PrivateKey) []byte {
	t.Helper()
	payloadRaw, _ := json.Marshal(registry.FeedPayload{
		VersionSerial: serial,
		ExpiresAt:     expiresAt.UTC(),
		Entries:       entries,
	})
	signingInput := append([]byte("tanpopo:marketplace:"+string(kind)+":v1\x00"), payloadRaw...)
	sig := ed25519.Sign(priv, signingInput)
	envRaw, _ := json.Marshal(registry.SignedFeed{
		Payload:   payloadRaw,
		KeyID:     keyID,
		Signature: base64.StdEncoding.EncodeToString(sig),
	})
	return envRaw
}

func makeTestTpkgArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			ModTime:  time.Unix(0, 0).UTC(),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tar write header %s: %v", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("tar write body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func TestAirGapBundleExportVerifyImportRoundtrip(t *testing.T) {
	ctx := context.Background()
	_, _, pubPath, privPath, lockPath, pkgDir, idxPath, revPath, sbomPath, now := buildSignedTestFixtures(t, 48*time.Hour)

	outBundle := filepath.Join(t.TempDir(), "tanpopo-airgap.tpbundle")
	exp, err := ExportBundle(ctx, ExportOptions{
		LockfilePath:       lockPath,
		PackagesDir:        pkgDir,
		SigningKeyPath:     privPath,
		PublicKeyPath:      pubPath,
		IndexFeedPath:      idxPath,
		RevocationFeedPath: revPath,
		SBOMPaths:          []string{sbomPath},
		OutputPath:         outBundle,
		TanpopoVersion:     "1.0.0",
		Now:                func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("ExportBundle: %v", err)
	}
	if exp.ExtensionsCount != 1 || !exp.IncludesIndex || !exp.IncludesRevoc || exp.SBOMCount != 1 {
		t.Fatalf("unexpected export metadata: %+v", exp)
	}

	ver, err := VerifyBundle(ctx, VerifyOptions{
		BundlePath:    outBundle,
		PublicKeyPath: pubPath,
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("VerifyBundle: %v", err)
	}
	if ver.Status != "VERIFIED" || ver.IndexSerial != 10 || ver.RevocationSerial != 4 {
		t.Fatalf("unexpected verify result: %+v", ver)
	}

	destDir := filepath.Join(t.TempDir(), "airgap")
	mirrorRoot := filepath.Join(t.TempDir(), "oci-mirror")
	dbPath := filepath.Join(t.TempDir(), "tanpopo.db")
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("sqlite open/migrate: %v", err)
	}
	_ = st.Close()

	imp, err := ImportBundle(ctx, ImportOptions{
		BundlePath:    outBundle,
		PublicKeyPath: pubPath,
		DestDir:       destDir,
		DBPath:        dbPath,
		MirrorTo:      "oci://harbor.corp.internal/tanpopo-extensions",
		Now:           func() time.Time { return now },
		LookupEnv: func(k string) (string, bool) {
			if k == "TANPOPO_BUNDLE_OCI_MIRROR_ROOT" {
				return mirrorRoot, true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if imp.Status != "IMPORTED" || !imp.AppliedIndex || !imp.AppliedRevoc {
		t.Fatalf("unexpected import result: %+v", imp)
	}
	if _, err := os.Stat(filepath.Join(destDir, "tanpopo.lock")); err != nil {
		t.Fatalf("imported tanpopo.lock missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mirrorRoot, "harbor.corp.internal", "tanpopo-extensions", "feeds", "revocations.json")); err != nil {
		t.Fatalf("mirrored revocations.json missing: %v", err)
	}
}

func TestCT31_TamperedInnerTpkgAbortsWithExit22AndPreservesState(t *testing.T) {
	ctx := context.Background()
	_, _, pubPath, privPath, lockPath, pkgDir, idxPath, revPath, _, now := buildSignedTestFixtures(t, 48*time.Hour)

	outBundle := filepath.Join(t.TempDir(), "valid.tpbundle")
	if _, err := ExportBundle(ctx, ExportOptions{
		LockfilePath:       lockPath,
		PackagesDir:        pkgDir,
		SigningKeyPath:     privPath,
		PublicKeyPath:      pubPath,
		IndexFeedPath:      idxPath,
		RevocationFeedPath: revPath,
		OutputPath:         outBundle,
		Now:                func() time.Time { return now },
	}); err != nil {
		t.Fatalf("ExportBundle: %v", err)
	}

	// Pre-populate destination directory with existing sentinel file to confirm rollback preserves it.
	destDir := filepath.Join(t.TempDir(), "existing-airgap")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir destDir: %v", err)
	}
	sentinelPath := filepath.Join(destDir, "existing-sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("untouched-state"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// Unpack valid bundle, flip 1 byte inside embedded .tpkg WITHOUT updating manifest, and repack.
	tamperedBundle := filepath.Join(t.TempDir(), "tampered-1byte.tpbundle")
	mutateArchiveEntry(t, outBundle, tamperedBundle, func(name string, data []byte) (string, []byte) {
		if strings.HasSuffix(name, ".tpkg") {
			cp := append([]byte(nil), data...)
			cp[len(cp)-1] ^= 0x01
			return name, cp
		}
		return name, data
	}, false)

	_, verr := VerifyBundle(ctx, VerifyOptions{
		BundlePath:    tamperedBundle,
		PublicKeyPath: pubPath,
		Now:           func() time.Time { return now },
	})
	if verr == nil || testharness.CodeOf(verr) != testharness.ExitContract {
		t.Fatalf("expected ExitContract (22) on 1-byte tampered .tpkg verify, got: %v (code=%d)", verr, testharness.CodeOf(verr))
	}
	if !strings.Contains(verr.Error(), ".tpkg") {
		t.Fatalf("expected offending .tpkg artifact named in error, got: %v", verr)
	}

	_, ierr := ImportBundle(ctx, ImportOptions{
		BundlePath:    tamperedBundle,
		PublicKeyPath: pubPath,
		DestDir:       destDir,
		Now:           func() time.Time { return now },
	})
	if ierr == nil || testharness.CodeOf(ierr) != testharness.ExitContract {
		t.Fatalf("expected ExitContract (22) on tampered import, got: %v", ierr)
	}
	gotSentinel, err := os.ReadFile(sentinelPath)
	if err != nil || string(gotSentinel) != "untouched-state" {
		t.Fatalf("existing state mutated despite aborted import: %q err=%v", string(gotSentinel), err)
	}
}

func TestCT31_InvalidSignatureAndExpiredRevocationsAbortAtomic(t *testing.T) {
	ctx := context.Background()
	_, _, pubPath, privPath, lockPath, pkgDir, idxPath, revPath, _, now := buildSignedTestFixtures(t, 2*time.Hour)
	outBundle := filepath.Join(t.TempDir(), "bundle.tpbundle")
	if _, err := ExportBundle(ctx, ExportOptions{
		LockfilePath:       lockPath,
		PackagesDir:        pkgDir,
		SigningKeyPath:     privPath,
		PublicKeyPath:      pubPath,
		IndexFeedPath:      idxPath,
		RevocationFeedPath: revPath,
		OutputPath:         outBundle,
		Now:                func() time.Time { return now },
	}); err != nil {
		t.Fatalf("ExportBundle: %v", err)
	}

	// Wrong public key (invalid root signature) -> ExitContract 22.
	wrongPub, _, _ := ed25519.GenerateKey(rand.Reader)
	wrongPubDER, _ := x509.MarshalPKIXPublicKey(wrongPub)
	wrongPubPath := filepath.Join(t.TempDir(), "wrong.pub")
	if err := os.WriteFile(wrongPubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: wrongPubDER}), 0o644); err != nil {
		t.Fatalf("write wrong pub: %v", err)
	}
	_, errWrongSig := ImportBundle(ctx, ImportOptions{
		BundlePath:    outBundle,
		PublicKeyPath: wrongPubPath,
		DestDir:       filepath.Join(t.TempDir(), "unused-dest"),
		Now:           func() time.Time { return now },
	})
	if errWrongSig == nil || testharness.CodeOf(errWrongSig) != testharness.ExitContract {
		t.Fatalf("expected ExitContract (22) for wrong trust root signature, got %v", errWrongSig)
	}

	// Expired revocations.json -> advance clock past +2h revocations expiry (index still valid until +24h) -> ExitContract 22.
	_, errExpired := VerifyBundle(ctx, VerifyOptions{
		BundlePath:    outBundle,
		PublicKeyPath: pubPath,
		Now:           func() time.Time { return now.Add(3 * time.Hour) },
	})
	if errExpired == nil || testharness.CodeOf(errExpired) != testharness.ExitContract || !strings.Contains(errExpired.Error(), "revocations.json") {
		t.Fatalf("expected ExitContract (22) naming revocations.json on expired feed, got %v", errExpired)
	}

	// Smuggled traversal or unmanifested entry -> ExitContract 22.
	smuggled := filepath.Join(t.TempDir(), "smuggled.tpbundle")
	mutateArchiveEntry(t, outBundle, smuggled, nil, true)
	_, errSmuggled := VerifyBundle(ctx, VerifyOptions{
		BundlePath:    smuggled,
		PublicKeyPath: pubPath,
		Now:           func() time.Time { return now },
	})
	if errSmuggled == nil || testharness.CodeOf(errSmuggled) != testharness.ExitContract {
		t.Fatalf("expected ExitContract (22) for unmanifested entry, got %v", errSmuggled)
	}
}

func mutateArchiveEntry(t *testing.T, srcPath, dstPath string, mutator func(name string, data []byte) (string, []byte), addExtra bool) {
	t.Helper()
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read src bundle: %v", err)
	}
	zr, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)

	var out bytes.Buffer
	zw, _ := zstd.NewWriter(&out)
	tw := tar.NewWriter(zw)
	for {
		hdr, herr := tr.Next()
		if herr == io.EOF {
			break
		}
		if herr != nil {
			t.Fatalf("tar next: %v", herr)
		}
		body, _ := io.ReadAll(tr)
		name := hdr.Name
		if mutator != nil {
			name, body = mutator(name, body)
		}
		_ = tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     hdr.Mode,
			Size:     int64(len(body)),
			ModTime:  hdr.ModTime,
			Typeflag: tar.TypeReg,
		})
		_, _ = tw.Write(body)
	}
	if addExtra {
		extra := []byte("unverified-smuggled-payload")
		_ = tw.WriteHeader(&tar.Header{
			Name:     "extra/evil.txt",
			Mode:     0o644,
			Size:     int64(len(extra)),
			ModTime:  time.Unix(0, 0).UTC(),
			Typeflag: tar.TypeReg,
		})
		_, _ = tw.Write(extra)
	}
	_ = tw.Close()
	_ = zw.Close()
	if err := os.WriteFile(dstPath, out.Bytes(), 0o644); err != nil {
		t.Fatalf("write mutated bundle: %v", err)
	}
}
