// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package installer implements safe .tpkg archive extraction (CT-04 / FZ-02),
// ExtensionManifest structural/security validation (CT-01, CT-02, CT-24),
// Ed25519 provenance/namespace/org-trust verification (CT-03, CT-05, CT-20, CT-25),
// and the SM-01 installation, upgrade escalation-guard (CT-18), < 5s rollback,
// and quarantine lifecycle (TP-IMP-0011).
package installer

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Hard archive security ceilings per SPEC-SEC-02 and TP-IMP-0011 (CT-04).
const (
	MaxArchiveEntries         = 256
	MaxTotalUncompressedBytes = 64 << 20 // 64 MiB
	MaxArchiveCompressedBytes = 64 << 20 // 64 MiB
	MaxManifestBytes          = 1 << 20  // 1 MiB
	MaxCompressionRatio       = 50
	minRatioCheckBytes        = 4096   // Avoid false-positives on tiny valid < 4 KiB gzip headers
	midStreamRatioMinBytes    = 131072 // Avoid false-positives on valid initial zero pages while aborting bombs early
)

var (
	// ErrInvalidArchive indicates corrupted gzip/tar framing or missing manifest.yaml.
	ErrInvalidArchive = errors.New("installer: invalid .tpkg archive")
	// ErrArchivePathTraversal indicates Zip-Slip, absolute path, backslash, or drive-prefix traversal (CT-04).
	ErrArchivePathTraversal = errors.New("installer: archive path traversal rejected")
	// ErrProhibitedArchiveEntry indicates symlink, hardlink, device node, or special tar entry (CT-04).
	ErrProhibitedArchiveEntry = errors.New("installer: prohibited non-regular archive entry (symlink/hardlink/device)")
	// ErrTooManyArchiveEntries indicates archive entry count exceeded MaxArchiveEntries (256).
	ErrTooManyArchiveEntries = errors.New("installer: archive exceeds maximum 256 entries")
	// ErrArchiveBomb indicates decompression exceeded 64 MiB or 50:1 compression ratio ceiling (CT-04).
	ErrArchiveBomb = errors.New("installer: archive bomb detected (size or ratio limit exceeded)")
	// ErrPathNotConfined indicates an extraction or deletion path escaped plugins_dir (CT-04).
	ErrPathNotConfined = errors.New("installer: path is not confined inside plugins directory")
)

// ExtractedArchive describes a verified .tpkg archive unpacked into a confined staging folder.
type ExtractedArchive struct {
	BundleSHA256  string
	ManifestBytes []byte
	ManifestPath  string
	StagingDir    string
	FileSHA256    map[string]string // clean forward-slash relative path -> sha256 hex
	EntryCount    int
	TotalBytes    int64
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ExtractSafeArchiveFromFile opens archivePath (refusing symlinks) and unpacks via ExtractSafeArchive.
func ExtractSafeArchiveFromFile(archivePath, pluginsDir string) (*ExtractedArchive, error) {
	if strings.TrimSpace(archivePath) == "" {
		return nil, fmt.Errorf("%w: archive path is required", ErrInvalidArchive)
	}
	fi, err := os.Lstat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("installer: stat archive %q: %w", archivePath, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: archive source %q cannot be a symlink", ErrPathNotConfined, archivePath)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: archive source %q must be a regular file", ErrInvalidArchive, archivePath)
	}
	if fi.Size() > MaxArchiveCompressedBytes {
		return nil, fmt.Errorf("%w: compressed archive %d bytes exceeds %d byte cap", ErrArchiveBomb, fi.Size(), MaxArchiveCompressedBytes)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("installer: open archive %q: %w", archivePath, err)
	}
	defer func() { _ = f.Close() }()

	return ExtractSafeArchive(f, pluginsDir)
}

// ExtractSafeArchive reads a gzipped tar (.tpkg) stream bounded in memory/disk, rejects
// path traversal, symlinks/hardlinks, > 256 entries, > 64 MiB uncompressed size, and
// > 50:1 compression ratio bombs, computes exact SHA-256 digests, and writes strictly
// inside a symlink-confined directory under pluginsDir (CT-04).
// Never executes any embedded file or install hook.
func ExtractSafeArchive(r io.Reader, pluginsDir string) (*ExtractedArchive, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: reader is nil", ErrInvalidArchive)
	}
	canonPluginsDir, err := ensureConfinedRootDir(pluginsDir)
	if err != nil {
		return nil, err
	}

	// Bound compressed stream to MaxArchiveCompressedBytes + 1 to fail immediately on oversized inputs.
	// Wrap in bufio.Reader so gzip.NewReader uses our reader directly instead of allocating a hidden internal buffer,
	// letting us detect any byte immediately following gzip member 1's EOF trailer.
	archiveHash := sha256.New()
	limitedCompressed := io.LimitReader(r, MaxArchiveCompressedBytes+1)
	compCounter := &countingReader{r: io.TeeReader(limitedCompressed, archiveHash)}
	compBufReader := bufio.NewReader(compCounter)

	gz, err := gzip.NewReader(compBufReader)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid gzip header: %v", ErrInvalidArchive, err)
	}
	// Reject concatenated multi-member gzip streams (hidden payload smuggling defense).
	gz.Multistream(false)
	defer func() { _ = gz.Close() }()

	stagingDir, err := os.MkdirTemp(canonPluginsDir, "stage-*")
	if err != nil {
		return nil, fmt.Errorf("installer: create staging dir: %w", err)
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	canonStagingDir, err := filepath.EvalSymlinks(stagingDir)
	if err != nil {
		return nil, fmt.Errorf("installer: resolve staging dir: %w", err)
	}
	if !isStrictlyInside(canonPluginsDir, canonStagingDir) {
		return nil, fmt.Errorf("%w: staging dir escaped plugins_dir", ErrPathNotConfined)
	}

	tr := tar.NewReader(gz)
	seenPaths := make(map[string]bool)
	seenLowerPaths := make(map[string]bool)
	fileHashes := make(map[string]string)
	var manifestBytes []byte
	var manifestRelPath string
	var entryCount int
	var totalUncompressed int64

	buf := make([]byte, 32*1024)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: corrupt tar stream: %v", ErrInvalidArchive, err)
		}
		if compCounter.n > MaxArchiveCompressedBytes {
			return nil, fmt.Errorf("%w: compressed archive exceeds %d byte cap", ErrArchiveBomb, MaxArchiveCompressedBytes)
		}

		entryCount++
		if entryCount > MaxArchiveEntries {
			return nil, fmt.Errorf("%w: %d > %d entries", ErrTooManyArchiveEntries, entryCount, MaxArchiveEntries)
		}

		cleanRel, isRootDot, pathErr := sanitizeArchiveEntryPath(hdr.Name)
		if pathErr != nil {
			return nil, pathErr
		}
		lowerRel := strings.ToLower(cleanRel)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if isRootDot {
				continue
			}
			if seenPaths[cleanRel] || seenLowerPaths[lowerRel] {
				return nil, fmt.Errorf("%w: duplicate or case-colliding entry %q", ErrInvalidArchive, cleanRel)
			}
			seenPaths[cleanRel] = true
			seenLowerPaths[lowerRel] = true
			if _, derr := ensureConfinedDirPath(canonStagingDir, cleanRel); derr != nil {
				return nil, derr
			}

		case tar.TypeReg:
			if isRootDot || cleanRel == "" {
				return nil, fmt.Errorf("%w: invalid root regular file name %q", ErrArchivePathTraversal, hdr.Name)
			}
			if seenPaths[cleanRel] || seenLowerPaths[lowerRel] {
				return nil, fmt.Errorf("%w: duplicate or case-colliding entry %q", ErrInvalidArchive, cleanRel)
			}
			seenPaths[cleanRel] = true
			seenLowerPaths[lowerRel] = true

			if hdr.Size < 0 || hdr.Size > MaxTotalUncompressedBytes || totalUncompressed+hdr.Size > MaxTotalUncompressedBytes {
				return nil, fmt.Errorf("%w: declared entry size %d exceeds %d MiB cap", ErrArchiveBomb, hdr.Size, MaxTotalUncompressedBytes>>20)
			}
			if hdr.Mode&(int64(os.ModeSetuid)|int64(os.ModeSetgid)) != 0 {
				return nil, fmt.Errorf("%w: setuid/setgid mode forbidden on %q", ErrProhibitedArchiveEntry, cleanRel)
			}

			if parentRel := path.Dir(cleanRel); parentRel != "." && parentRel != "" {
				if _, derr := ensureConfinedDirPath(canonStagingDir, parentRel); derr != nil {
					return nil, derr
				}
			}
			destPath := filepath.Join(canonStagingDir, filepath.FromSlash(cleanRel))
			if err := verifyConfinementForCreate(canonStagingDir, destPath); err != nil {
				return nil, err
			}

			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
			if err != nil {
				return nil, fmt.Errorf("installer: create %q: %w", cleanRel, err)
			}

			isManifest := lowerRel == "manifest.yaml" || lowerRel == "manifest.yml"
			var manifestBuf bytes.Buffer
			entryHasher := sha256.New()
			remainingAllowed := MaxTotalUncompressedBytes - totalUncompressed

			writeErr := func() error {
				defer func() { _ = outFile.Close() }()
				limitedEntry := io.LimitReader(tr, remainingAllowed+1)
				var entryWritten int64
				for {
					n, rerr := limitedEntry.Read(buf)
					if n > 0 {
						entryWritten += int64(n)
						totalUncompressed += int64(n)
						if totalUncompressed > MaxTotalUncompressedBytes {
							return fmt.Errorf("%w: unpacked size exceeds %d MiB limit", ErrArchiveBomb, MaxTotalUncompressedBytes>>20)
						}
						if entryWritten > hdr.Size {
							return fmt.Errorf("%w: tar entry %q payload exceeds declared header size", ErrInvalidArchive, cleanRel)
						}
						if totalUncompressed >= midStreamRatioMinBytes {
							if err := checkBombRatio(totalUncompressed, compCounter.n); err != nil {
								return err
							}
						}
						if isManifest {
							if manifestBuf.Len()+n > MaxManifestBytes {
								return fmt.Errorf("%w: manifest.yaml exceeds %d byte cap", ErrArchiveBomb, MaxManifestBytes)
							}
							manifestBuf.Write(buf[:n])
						}
						_, _ = entryHasher.Write(buf[:n])
						if _, werr := outFile.Write(buf[:n]); werr != nil {
							return fmt.Errorf("installer: write %q: %w", cleanRel, werr)
						}
					}
					if rerr == io.EOF {
						break
					}
					if rerr != nil {
						return fmt.Errorf("%w: read entry %q: %v", ErrInvalidArchive, cleanRel, rerr)
					}
				}
				if entryWritten != hdr.Size {
					return fmt.Errorf("%w: truncated tar entry %q (wrote %d of %d declared bytes)", ErrInvalidArchive, cleanRel, entryWritten, hdr.Size)
				}
				return nil
			}()
			if writeErr != nil {
				return nil, writeErr
			}
			fileHashes[cleanRel] = hex.EncodeToString(entryHasher.Sum(nil))
			if isManifest {
				if manifestBytes != nil {
					return nil, fmt.Errorf("%w: duplicate manifest YAML in archive", ErrInvalidArchive)
				}
				manifestBytes = manifestBuf.Bytes()
				manifestRelPath = cleanRel
			}

		default:
			// Unconditionally reject tar.TypeSymlink, tar.TypeLink, TypeChar, TypeBlock, TypeFifo, TypeGNUSparse, etc.
			return nil, fmt.Errorf("%w: entry %q type=%q", ErrProhibitedArchiveEntry, hdr.Name, string(hdr.Typeflag))
		}
	}

	// Drain remaining decompressed bytes inside gzip member 1 (e.g. tar trailer zero padding)
	// strictly through the 64 MiB uncompressed & 50:1 bomb ratio guards so post-tar bombs cannot hide!
	for {
		n, rerr := gz.Read(buf)
		if n > 0 {
			totalUncompressed += int64(n)
			if totalUncompressed > MaxTotalUncompressedBytes {
				return nil, fmt.Errorf("%w: unpacked trailer exceeds %d MiB cap", ErrArchiveBomb, MaxTotalUncompressedBytes>>20)
			}
			if err := checkBombRatio(totalUncompressed, compCounter.n); err != nil {
				return nil, err
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, fmt.Errorf("%w: corrupt gzip trailer: %v", ErrInvalidArchive, rerr)
		}
	}

	// Verify zero trailing or multi-stream bytes follow gzip member 1 EOF.
	if _, berr := compBufReader.ReadByte(); berr != io.EOF {
		return nil, fmt.Errorf("%w: trailing or concatenated data after gzip EOF is forbidden", ErrInvalidArchive)
	}
	if compCounter.n > MaxArchiveCompressedBytes {
		return nil, fmt.Errorf("%w: compressed archive exceeds %d byte cap", ErrArchiveBomb, MaxArchiveCompressedBytes)
	}
	if err := checkBombRatio(totalUncompressed, compCounter.n); err != nil {
		return nil, err
	}
	if manifestBytes == nil {
		return nil, fmt.Errorf("%w: missing manifest.yaml at archive root", ErrInvalidArchive)
	}

	cleanupStaging = false
	return &ExtractedArchive{
		BundleSHA256:  hex.EncodeToString(archiveHash.Sum(nil)),
		ManifestBytes: manifestBytes,
		ManifestPath:  manifestRelPath,
		StagingDir:    canonStagingDir,
		FileSHA256:    fileHashes,
		EntryCount:    entryCount,
		TotalBytes:    totalUncompressed,
	}, nil
}

func checkBombRatio(uncompressed, compressed int64) error {
	if uncompressed <= minRatioCheckBytes {
		return nil
	}
	if compressed <= 0 {
		return fmt.Errorf("%w: uncompressed data with zero compressed bytes", ErrArchiveBomb)
	}
	if uncompressed > compressed*MaxCompressionRatio {
		return fmt.Errorf("%w: compression ratio %d:1 exceeds %d:1 ceiling", ErrArchiveBomb, uncompressed/compressed, MaxCompressionRatio)
	}
	return nil
}

func sanitizeArchiveEntryPath(raw string) (cleanRel string, isRootDot bool, err error) {
	if strings.TrimSpace(raw) == "" {
		return "", false, fmt.Errorf("%w: empty archive entry path", ErrArchivePathTraversal)
	}
	if strings.ContainsRune(raw, '\x00') || strings.ContainsAny(raw, "\r\n") {
		return "", false, fmt.Errorf("%w: control byte in entry path %q", ErrArchivePathTraversal, raw)
	}
	if strings.ContainsRune(raw, '\\') {
		return "", false, fmt.Errorf("%w: backslash path separator forbidden in entry %q", ErrArchivePathTraversal, raw)
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "", false, fmt.Errorf("%w: absolute path forbidden in entry %q", ErrArchivePathTraversal, raw)
	}
	if strings.ContainsRune(raw, ':') {
		return "", false, fmt.Errorf("%w: drive prefix or NTFS stream ':' forbidden in entry %q", ErrArchivePathTraversal, raw)
	}

	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return "", false, fmt.Errorf("%w: parent traversal '..' forbidden in entry %q", ErrArchivePathTraversal, raw)
		}
		if isReservedWindowsDeviceName(seg) {
			return "", false, fmt.Errorf("%w: reserved OS device segment %q forbidden in entry %q", ErrArchivePathTraversal, seg, raw)
		}
	}

	trimmed := strings.TrimSuffix(raw, "/")
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "" {
		return "", true, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", false, fmt.Errorf("%w: cleaned path %q escapes root", ErrArchivePathTraversal, raw)
	}
	return cleaned, false, nil
}

func isReservedWindowsDeviceName(seg string) bool {
	if seg == "" {
		return false
	}
	base := seg
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

func ensureConfinedDirPath(canonRoot, relDir string) (string, error) {
	cur := canonRoot
	for _, seg := range strings.Split(relDir, "/") {
		if seg == "" || seg == "." {
			continue
		}
		next := filepath.Join(cur, seg)
		if err := verifyConfinementForCreate(canonRoot, next); err != nil {
			return "", err
		}
		fi, lerr := os.Lstat(next)
		switch {
		case lerr == nil:
			if fi.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("%w: existing symlink dir %q", ErrPathNotConfined, next)
			}
			if !fi.IsDir() {
				return "", fmt.Errorf("%w: path component %q collides with non-directory", ErrInvalidArchive, relDir)
			}
		case os.IsNotExist(lerr):
			if merr := os.Mkdir(next, 0o700); merr != nil {
				return "", fmt.Errorf("installer: mkdir %q: %w", relDir, merr)
			}
		default:
			return "", fmt.Errorf("installer: lstat dir %q: %w", relDir, lerr)
		}
		resolved, rerr := filepath.EvalSymlinks(next)
		if rerr != nil || !isStrictlyInside(canonRoot, resolved) {
			return "", fmt.Errorf("%w: created dir %q escaped confined root", ErrPathNotConfined, relDir)
		}
		cur = resolved
	}
	return cur, nil
}

func ensureConfinedRootDir(pluginsDir string) (string, error) {
	if strings.TrimSpace(pluginsDir) == "" {
		return "", fmt.Errorf("%w: plugins_dir is required", ErrPathNotConfined)
	}
	absRoot, err := filepath.Abs(pluginsDir)
	if err != nil {
		return "", fmt.Errorf("installer: resolve plugins_dir %q: %w", pluginsDir, err)
	}
	if fi, lerr := os.Lstat(absRoot); lerr == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: plugins_dir %q cannot be a symlink", ErrPathNotConfined, absRoot)
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("%w: plugins_dir %q is not a directory", ErrPathNotConfined, absRoot)
		}
	} else if !os.IsNotExist(lerr) {
		return "", fmt.Errorf("installer: stat plugins_dir %q: %w", absRoot, lerr)
	}
	if err := os.MkdirAll(absRoot, 0o700); err != nil {
		return "", fmt.Errorf("installer: create plugins_dir %q: %w", absRoot, err)
	}
	canon, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("installer: eval symlinks on plugins_dir %q: %w", absRoot, err)
	}
	return canon, nil
}

func verifyConfinementForCreate(canonRoot, targetPath string) error {
	parent := filepath.Dir(targetPath)
	canonParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("%w: resolve parent %q: %v", ErrPathNotConfined, parent, err)
	}
	if canonParent != canonRoot && !isStrictlyInside(canonRoot, canonParent) {
		return fmt.Errorf("%w: parent %q escapes confined root", ErrPathNotConfined, parent)
	}
	if fi, lerr := os.Lstat(targetPath); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: existing symlink at target %q", ErrPathNotConfined, targetPath)
	}
	if !isStrictlyInside(canonRoot, filepath.Join(canonParent, filepath.Base(targetPath))) {
		return fmt.Errorf("%w: target %q escapes confined root", ErrPathNotConfined, targetPath)
	}
	return nil
}

func isStrictlyInside(canonRoot, candidate string) bool {
	rel, err := filepath.Rel(canonRoot, candidate)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	prefix := ".." + string(filepath.Separator)
	return !strings.HasPrefix(rel, prefix)
}

// SafeRemovePackageDir resolves canonical symlinks via filepath.EvalSymlinks and
// verifies confinement at depth >= 1 strictly inside pluginsDir before removal (CT-04).
func SafeRemovePackageDir(pluginsDir, pkgDir string) error {
	if strings.TrimSpace(pluginsDir) == "" || strings.TrimSpace(pkgDir) == "" {
		return fmt.Errorf("%w: pluginsDir and pkgDir are required", ErrPathNotConfined)
	}
	canonRoot, err := filepath.EvalSymlinks(pluginsDir)
	if err != nil {
		return fmt.Errorf("%w: resolve pluginsDir %q: %v", ErrPathNotConfined, pluginsDir, err)
	}
	fi, lerr := os.Lstat(pkgDir)
	if lerr != nil {
		if os.IsNotExist(lerr) {
			return nil
		}
		return fmt.Errorf("installer: lstat target %q: %w", pkgDir, lerr)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: refusing to remove symlink target %q", ErrPathNotConfined, pkgDir)
	}
	canonTarget, err := filepath.EvalSymlinks(pkgDir)
	if err != nil {
		return fmt.Errorf("%w: resolve pkgDir %q: %v", ErrPathNotConfined, pkgDir, err)
	}
	if !isStrictlyInside(canonRoot, canonTarget) {
		return fmt.Errorf("%w: target %q is not strictly inside %q", ErrPathNotConfined, canonTarget, canonRoot)
	}
	return os.RemoveAll(canonTarget)
}
