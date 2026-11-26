// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package plugin

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nguywnben/tanpopo/internal/extension/builtin"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/security/trust"
)

type releaseSignatureManifest struct {
	Signatures []builtin.ReleaseSignatureRecord `json:"signatures"`
}

type releaseSignResult struct {
	OutputPath            string `json:"output_path" yaml:"output_path"`
	SignedCount           int    `json:"signed_count" yaml:"signed_count"`
	TotalSignatures       int    `json:"total_signatures" yaml:"total_signatures"`
	RootKeyFingerprint    string `json:"root_key_fingerprint" yaml:"root_key_fingerprint"`
}

type releaseVerifyEntry struct {
	PackageID     string `json:"package_id" yaml:"package_id"`
	Version       string `json:"version" yaml:"version"`
	ArchiveSHA256 string `json:"archive_sha256" yaml:"archive_sha256"`
	Verified      bool   `json:"verified" yaml:"verified"`
}

type releaseVerifyResult struct {
	Status             string               `json:"status" yaml:"status"`
	RootKeyFingerprint string               `json:"root_key_fingerprint" yaml:"root_key_fingerprint"`
	VerifiedCount      int                  `json:"verified_count" yaml:"verified_count"`
	Entries            []releaseVerifyEntry `json:"entries" yaml:"entries"`
}

func newReleaseSignCmd(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var (
		privateKeyFile string
		signBuiltin    bool
		packageID      string
		version        string
		archivePath    string
		outPath        string
	)
	cmd := &cobra.Command{
		Use:   "release-sign",
		Short: "Sign official extension archives offline with a release Ed25519 private key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateOutputFormat(loc, outputFormat(opts)); err != nil {
				return err
			}
			priv, err := loadEd25519PrivateKeyFile(privateKeyFile)
			if err != nil {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: err.Error()}
			}
			pub := priv.Public().(ed25519.PublicKey)
			fp := trust.PublicKeyFingerprint(pub)

			targets, err := collectReleaseSignTargets(signBuiltin, packageID, version, archivePath)
			if err != nil {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: err.Error()}
			}

			outFile := strings.TrimSpace(outPath)
			if outFile == "" {
				outFile = "release-signatures.json"
			}
			existing, err := readReleaseSignatureManifest(outFile)
			if err != nil {
				return err
			}

			byKey := make(map[string]builtin.ReleaseSignatureRecord, len(existing.Signatures)+len(targets))
			for _, rec := range existing.Signatures {
				k := releaseSigKey(rec.PackageID, rec.Version, rec.ArchiveSHA256)
				byKey[k] = rec
			}
			for _, target := range targets {
				payload := installer.SignatureSigningPayload(target.packageID, target.version, target.sha256)
				sig := ed25519.Sign(priv, payload)
				rec := builtin.ReleaseSignatureRecord{
					PackageID:     target.packageID,
					Version:       target.version,
					ArchiveSHA256: target.sha256,
					RootSignature: base64.StdEncoding.EncodeToString(sig),
				}
				byKey[releaseSigKey(rec.PackageID, rec.Version, rec.ArchiveSHA256)] = rec
			}

			merged := make([]builtin.ReleaseSignatureRecord, 0, len(byKey))
			for _, rec := range byKey {
				merged = append(merged, rec)
			}
			sort.Slice(merged, func(i, j int) bool {
				if merged[i].PackageID != merged[j].PackageID {
					return merged[i].PackageID < merged[j].PackageID
				}
				if merged[i].Version != merged[j].Version {
					return merged[i].Version < merged[j].Version
				}
				return merged[i].ArchiveSHA256 < merged[j].ArchiveSHA256
			})

			if dir := filepath.Dir(outFile); dir != "" && dir != "." {
				if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
					return mkErr
				}
			}
			encoded, err := json.MarshalIndent(releaseSignatureManifest{Signatures: merged}, "", "  ")
			if err != nil {
				return err
			}
			encoded = append(encoded, '\n')
			if err := os.WriteFile(outFile, encoded, 0o644); err != nil {
				return err
			}

			res := &releaseSignResult{
				OutputPath:         outFile,
				SignedCount:        len(targets),
				TotalSignatures:    len(merged),
				RootKeyFingerprint: fp,
			}
			w := outWriter(cmd, opts)
			if fmtName := outputFormat(opts); fmtName == "json" || fmtName == "yaml" {
				return renderStructured(w, opts, res)
			}
			_, _ = fmt.Fprintf(w, "OUTPUT\t%s\nSIGNED\t%d\nTOTAL\t%d\nROOT_FINGERPRINT\t%s\n",
				testharness.SanitizeTerminalText(res.OutputPath),
				res.SignedCount,
				res.TotalSignatures,
				res.RootKeyFingerprint,
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&privateKeyFile, "private-key-file", "", "Path to offline Ed25519 private key file (PKCS8 PEM, hex, or base64)")
	cmd.Flags().BoolVar(&signBuiltin, "builtin", false, "Sign all embedded official built-in extension packages")
	cmd.Flags().StringVar(&packageID, "package-id", "", "Target extension package ID (with --archive)")
	cmd.Flags().StringVar(&version, "version", "", "Target SemVer version (with --archive)")
	cmd.Flags().StringVar(&archivePath, "archive", "", "Path to .tpkg archive to sign")
	cmd.Flags().StringVarP(&outPath, "out", "o", "release-signatures.json", "Path to output release-signatures.json file")
	return cmd
}

func newReleaseVerifyCmd(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var (
		signaturesPath string
		rootPubKeyFile string
		verifyBuiltin  bool
		packageID      string
		version        string
		archivePath    string
	)
	cmd := &cobra.Command{
		Use:   "release-verify",
		Short: "Verify offline release Ed25519 signatures against a root public key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateOutputFormat(loc, outputFormat(opts)); err != nil {
				return err
			}
			if strings.TrimSpace(signaturesPath) == "" {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: "--signatures is required"}
			}
			pub, err := loadEd25519PublicKeyFile(rootPubKeyFile)
			if err != nil {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: err.Error()}
			}
			fp := trust.PublicKeyFingerprint(pub)
			manifest, err := readReleaseSignatureManifestRequired(signaturesPath)
			if err != nil {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: err.Error()}
			}

			sigIndex := make(map[string][]byte, len(manifest.Signatures))
			for _, rec := range manifest.Signatures {
				rawSig, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.RootSignature))
				if derr != nil || len(rawSig) != ed25519.SignatureSize {
					return &testharness.ExitError{
						Code:    testharness.ExitValidation,
						Message: fmt.Sprintf("invalid base64 Ed25519 signature for %s@%s", rec.PackageID, rec.Version),
					}
				}
				sigIndex[releaseSigKey(rec.PackageID, rec.Version, rec.ArchiveSHA256)] = rawSig
			}

			var targets []releaseSignTarget
			if verifyBuiltin || strings.TrimSpace(archivePath) != "" {
				targets, err = collectReleaseSignTargets(verifyBuiltin, packageID, version, archivePath)
				if err != nil {
					return &testharness.ExitError{Code: testharness.ExitValidation, Message: err.Error()}
				}
			} else {
				if len(manifest.Signatures) == 0 {
					return &testharness.ExitError{Code: testharness.ExitValidation, Message: "no signature records to verify"}
				}
				for _, rec := range manifest.Signatures {
					targets = append(targets, releaseSignTarget{
						packageID: strings.TrimSpace(rec.PackageID),
						version:   strings.TrimSpace(rec.Version),
						sha256:    strings.ToLower(strings.TrimSpace(rec.ArchiveSHA256)),
					})
				}
			}

			entries := make([]releaseVerifyEntry, 0, len(targets))
			for _, target := range targets {
				sigRaw, ok := sigIndex[releaseSigKey(target.packageID, target.version, target.sha256)]
				if !ok {
					return &testharness.ExitError{
						Code:    testharness.ExitValidation,
						Message: fmt.Sprintf("missing signature for %s@%s#%s", target.packageID, target.version, target.sha256),
					}
				}
				payload := installer.SignatureSigningPayload(target.packageID, target.version, target.sha256)
				if !ed25519.Verify(pub, payload, sigRaw) {
					return &testharness.ExitError{
						Code:    testharness.ExitValidation,
						Message: fmt.Sprintf("signature verification failed for %s@%s", target.packageID, target.version),
					}
				}
				entries = append(entries, releaseVerifyEntry{
					PackageID:     target.packageID,
					Version:       target.version,
					ArchiveSHA256: target.sha256,
					Verified:      true,
				})
			}

			res := &releaseVerifyResult{
				Status:             "VERIFIED",
				RootKeyFingerprint: fp,
				VerifiedCount:      len(entries),
				Entries:            entries,
			}
			w := outWriter(cmd, opts)
			if fmtName := outputFormat(opts); fmtName == "json" || fmtName == "yaml" {
				return renderStructured(w, opts, res)
			}
			_, _ = fmt.Fprintf(w, "STATUS\t%s\nROOT_FINGERPRINT\t%s\nVERIFIED\t%d\n",
				res.Status, res.RootKeyFingerprint, res.VerifiedCount)
			for _, entry := range entries {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n",
					testharness.SanitizeTerminalText(entry.PackageID),
					testharness.SanitizeTerminalText(entry.Version),
					entry.ArchiveSHA256,
				)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&signaturesPath, "signatures", "", "Path to release-signatures.json file")
	cmd.Flags().StringVar(&rootPubKeyFile, "root-pubkey-file", "", "Path to Ed25519 root public key file (PKIX PEM, 64-hex, or base64)")
	cmd.Flags().BoolVar(&verifyBuiltin, "builtin", false, "Verify signatures cover every embedded official built-in package")
	cmd.Flags().StringVar(&packageID, "package-id", "", "Target extension package ID (with --archive)")
	cmd.Flags().StringVar(&version, "version", "", "Target SemVer version (with --archive)")
	cmd.Flags().StringVar(&archivePath, "archive", "", "Path to .tpkg archive to verify")
	return cmd
}

type releaseSignTarget struct {
	packageID string
	version   string
	sha256    string
}

func collectReleaseSignTargets(useBuiltin bool, packageID, version, archivePath string) ([]releaseSignTarget, error) {
	hasArchive := strings.TrimSpace(archivePath) != ""
	if useBuiltin == hasArchive {
		return nil, fmt.Errorf("specify either --builtin or --archive (with --package-id and --version)")
	}
	if useBuiltin {
		cat, err := builtin.NewCatalog()
		if err != nil {
			return nil, fmt.Errorf("load built-in official catalog: %w", err)
		}
		pkgs := cat.Packages()
		targets := make([]releaseSignTarget, 0, len(pkgs))
		for _, pkg := range pkgs {
			targets = append(targets, releaseSignTarget{
				packageID: pkg.PackageID,
				version:   pkg.Version,
				sha256:    strings.ToLower(strings.TrimSpace(pkg.ArchiveSHA256)),
			})
		}
		return targets, nil
	}
	pkg := strings.TrimSpace(packageID)
	ver := strings.TrimSpace(version)
	if pkg == "" || ver == "" {
		return nil, fmt.Errorf("--package-id and --version are required when using --archive")
	}
	sumHex, err := fileSHA256Hex(archivePath)
	if err != nil {
		return nil, err
	}
	return []releaseSignTarget{{
		packageID: pkg,
		version:   ver,
		sha256:    sumHex,
	}}, nil
}

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func releaseSigKey(pkgID, version, sha256Hex string) string {
	return strings.ToLower(strings.TrimSpace(pkgID)) + "@" + strings.TrimSpace(version) + "#" + strings.ToLower(strings.TrimSpace(sha256Hex))
}

func readReleaseSignatureManifest(path string) (releaseSignatureManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return releaseSignatureManifest{}, nil
		}
		return releaseSignatureManifest{}, err
	}
	var doc releaseSignatureManifest
	if len(strings.TrimSpace(string(raw))) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return releaseSignatureManifest{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return doc, nil
}

func readReleaseSignatureManifestRequired(path string) (releaseSignatureManifest, error) {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return releaseSignatureManifest{}, err
	}
	var doc releaseSignatureManifest
	if err := json.Unmarshal(raw, &doc); err != nil {
		return releaseSignatureManifest{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return doc, nil
}

func loadEd25519PrivateKeyFile(path string) (ed25519.PrivateKey, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil, fmt.Errorf("--private-key-file is required (keys are never generated or embedded automatically)")
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	if block, _ := pem.Decode(raw); block != nil {
		parsed, perr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("parse PKCS8 private key %q: %w", p, perr)
		}
		edPriv, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key %q is not Ed25519", p)
		}
		return edPriv, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if dec, derr := hex.DecodeString(trimmed); derr == nil {
		switch len(dec) {
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(dec), nil
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(dec), nil
		}
	}
	if dec, derr := base64.StdEncoding.DecodeString(trimmed); derr == nil {
		switch len(dec) {
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(dec), nil
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(dec), nil
		}
	}
	return nil, fmt.Errorf("unsupported Ed25519 private key format in %q", p)
}

func loadEd25519PublicKeyFile(path string) (ed25519.PublicKey, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil, fmt.Errorf("--root-pubkey-file is required")
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	if block, _ := pem.Decode(raw); block != nil {
		parsed, perr := x509.ParsePKIXPublicKey(block.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("parse PKIX public key %q: %w", p, perr)
		}
		edPub, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("public key %q is not Ed25519", p)
		}
		return edPub, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if dec, derr := hex.DecodeString(trimmed); derr == nil && len(dec) == ed25519.PublicKeySize {
		return ed25519.PublicKey(dec), nil
	}
	if dec, derr := base64.StdEncoding.DecodeString(trimmed); derr == nil && len(dec) == ed25519.PublicKeySize {
		return ed25519.PublicKey(dec), nil
	}
	return nil, fmt.Errorf("unsupported Ed25519 public key format in %q", p)
}
