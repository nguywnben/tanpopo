// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package testharness

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
)

// PackResult describes a reproducible .tpkg archive produced and self-verified by PackExtension.
type PackResult struct {
	ExtensionID string `json:"extension_id" yaml:"extension_id"`
	Version     string `json:"version" yaml:"version"`
	OutputPath  string `json:"output_path" yaml:"output_path"`
	SHA256      string `json:"sha256" yaml:"sha256"`
	SizeBytes   int64  `json:"size_bytes" yaml:"size_bytes"`
	EntryCount  int    `json:"entry_count" yaml:"entry_count"`
	Status      string `json:"status" yaml:"status"`
}

type packEntry struct {
	relPath string
	data    []byte
}

// PackExtension validates dir and writes a bit-for-bit reproducible .tpkg archive
// using epoch-0 timestamps, zero UID/GID metadata, deterministic gzip compression,
// and lexicographically ordered regular files, then self-verifies via installer.ExtractSafeArchiveFromFile.
func PackExtension(ctx context.Context, dir, outPath string) (*PackResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	valReport, vm, _, err := ValidateExtensionDir(ctx, dir)
	if err != nil {
		return nil, err
	}

	absDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, validationErr("resolve extension directory", err)
	}

	entries, err := collectDeterministicEntries(absDir)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, validationErr("extension directory contains no packable files", nil)
	}
	if len(entries) > installer.MaxArchiveEntries {
		return nil, validationErr(fmt.Sprintf("extension contains %d files exceeding %d entry limit", len(entries), installer.MaxArchiveEntries), nil)
	}

	if strings.TrimSpace(outPath) == "" {
		safeName := strings.TrimPrefix(vm.Manifest.Metadata.ID, "@")
		safeName = strings.ReplaceAll(safeName, "/", "-")
		outPath = filepath.Join(absDir, fmt.Sprintf("%s-%s.tpkg", safeName, vm.Manifest.Metadata.Version))
	}

	archiveBytes, err := buildDeterministicTPKG(entries)
	if err != nil {
		return nil, validationErr("build deterministic .tpkg archive", err)
	}

	if mkErr := os.MkdirAll(filepath.Dir(outPath), 0o755); mkErr != nil {
		return nil, validationErr("create .tpkg output directory", mkErr)
	}
	if werr := os.WriteFile(outPath, archiveBytes, 0o644); werr != nil {
		return nil, validationErr("write .tpkg archive", werr)
	}

	// Self-verify round-trip extraction, archive guards, and entrypoint digest parity inside .tpkg.
	verifyDir, verr := os.MkdirTemp("", "tanpopo-pack-verify-*")
	if verr != nil {
		return nil, validationErr("create verification temp dir", verr)
	}
	defer func() { _ = os.RemoveAll(verifyDir) }()

	extracted, exErr := installer.ExtractSafeArchiveFromFile(outPath, verifyDir)
	if exErr != nil {
		return nil, contractErr("packed .tpkg failed archive guard self-verification", exErr)
	}
	if _, mErr := installer.ParseAndValidateManifest(extracted.ManifestBytes, installer.DefaultValidationPolicy(), extracted.FileSHA256); mErr != nil {
		return nil, classifyInstallerError(mErr)
	}

	sum := sha256.Sum256(archiveBytes)
	digest := hex.EncodeToString(sum[:])
	if extracted.BundleSHA256 != digest {
		return nil, contractErr("packed .tpkg digest verification mismatch", nil)
	}

	return &PackResult{
		ExtensionID: valReport.ExtensionID,
		Version:     valReport.Version,
		OutputPath:  outPath,
		SHA256:      digest,
		SizeBytes:   int64(len(archiveBytes)),
		EntryCount:  len(entries),
		Status:      "PACKED",
	}, nil
}

func collectDeterministicEntries(absDir string) ([]packEntry, error) {
	var out []packEntry
	err := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, wErr error) error {
		if wErr != nil {
			return wErr
		}
		if path == absDir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return securityErr(fmt.Sprintf("symlink %q prohibited inside .tpkg", path), nil)
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return securityErr(fmt.Sprintf("non-regular file %q prohibited inside .tpkg", path), nil)
		}
		rel, rerr := filepath.Rel(absDir, path)
		if rerr != nil {
			return rerr
		}
		relSlash := filepath.ToSlash(rel)
		if strings.HasSuffix(strings.ToLower(relSlash), ".tpkg") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out = append(out, packEntry{relPath: relSlash, data: data})
		return nil
	})
	if err != nil {
		var ex *ExitError
		if errors.As(err, &ex) {
			return nil, err
		}
		return nil, validationErr("collect archive files", err)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].relPath < out[j].relPath
	})
	return out, nil
}

func buildDeterministicTPKG(entries []packEntry) ([]byte, error) {
	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	gw.ModTime = time.Unix(0, 0).UTC()
	gw.OS = 255 // Unknown/canonical OS byte for cross-OS determinism

	tw := tar.NewWriter(gw)
	epoch := time.Unix(0, 0).UTC()
	for _, entry := range entries {
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     entry.relPath,
			Size:     int64(len(entry.data)),
			Mode:     0o644,
			Uid:      0,
			Gid:      0,
			Uname:    "",
			Gname:    "",
			ModTime:  epoch,
			Format:   tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(entry.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
