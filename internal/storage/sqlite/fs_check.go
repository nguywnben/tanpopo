// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package sqlite implements the pure-Go SQLite WAL persistence driver
// (modernc.org/sqlite, CGO_ENABLED=0) backed by Expand-Only migrations,
// composite multi-tenant keys, network-filesystem rejection, and VACUUM INTO hot backups.
//
// DAG Boundary (Tier 2): May import stdlib, internal/pkg, internal/storage/port,
// internal/storage/migrate, and db/migrations/sqlite.
package sqlite

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// FSChecker inspects a local filesystem directory path and returns a descriptive
// network filesystem type (e.g. "nfs", "smb") if the mount is remote, or "" if local.
type FSChecker func(dirPath string) (remoteFSType string, err error)

func isUNCPath(p string) bool {
	trimmed := strings.TrimSpace(p)
	return strings.HasPrefix(trimmed, `\\`) || strings.HasPrefix(trimmed, `//`)
}

func checkLeafNotSymlink(abs string) error {
	info, err := os.Lstat(abs)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(abs)
			return fmt.Errorf("%w: symlinked sqlite target %q (-> %q) rejected", port.ErrNetworkFilesystem, abs, target)
		}
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("sqlite: inspect target %q: %w", abs, err)
}

func checkLocalPath(targetPath string, customChecker FSChecker) error {
	if strings.TrimSpace(targetPath) == "" || strings.ContainsRune(targetPath, 0) {
		return fmt.Errorf("%w: invalid empty or NUL sqlite path", port.ErrInvalidInput)
	}
	if isUNCPath(targetPath) {
		return fmt.Errorf("%w: UNC/SMB network path %q rejected", port.ErrNetworkFilesystem, targetPath)
	}
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("sqlite: resolve path %q: %w", targetPath, err)
	}
	if isUNCPath(abs) {
		return fmt.Errorf("%w: resolved UNC network path %q rejected", port.ErrNetworkFilesystem, abs)
	}
	if err := checkLeafNotSymlink(abs); err != nil {
		return err
	}

	probeDir, err := existingAncestorDir(abs)
	if err != nil {
		return err
	}
	resolved, symErr := filepath.EvalSymlinks(probeDir)
	if symErr != nil {
		return fmt.Errorf("sqlite: resolve symlinks for %q: %w", probeDir, symErr)
	}
	if isUNCPath(resolved) {
		return fmt.Errorf("%w: resolved symlink network path %q rejected", port.ErrNetworkFilesystem, resolved)
	}
	if customChecker == nil {
		customChecker = detectRemoteFilesystem
	}
	fsType, err := customChecker(resolved)
	if err != nil {
		return fmt.Errorf("sqlite: inspect filesystem for %q: %w", resolved, err)
	}
	if fsType != "" {
		return fmt.Errorf("%w: path %q resides on remote filesystem %q", port.ErrNetworkFilesystem, abs, fsType)
	}
	return nil
}

func existingAncestorDir(p string) (string, error) {
	cur := p
	for {
		if linfo, lerr := os.Lstat(cur); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
			resolved, symErr := filepath.EvalSymlinks(cur)
			if symErr != nil {
				return "", fmt.Errorf("%w: symlink %q along sqlite path cannot be resolved cleanly: %v", port.ErrNetworkFilesystem, cur, symErr)
			}
			if isUNCPath(resolved) {
				return "", fmt.Errorf("%w: symlink target %q rejected", port.ErrNetworkFilesystem, resolved)
			}
			cur = resolved
		}
		info, err := os.Stat(cur)
		if err == nil {
			if info.IsDir() {
				return cur, nil
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("sqlite: stat ancestor %q: %w", cur, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return cur, nil
		}
		cur = parent
	}
}
