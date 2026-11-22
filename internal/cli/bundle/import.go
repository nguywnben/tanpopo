// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package bundle

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/registry"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

// ImportOptions configures atomic verification and import of an air-gapped .tpbundle.
type ImportOptions struct {
	BundlePath    string
	PublicKeyPath string
	DataDir       string
	DestDir       string
	DBPath        string
	MirrorTo      string
	Now           func() time.Time
	LookupEnv     func(string) (string, bool)
}

// ImportResult summarizes atomically imported extensions, feeds, and optional local mirrors.
type ImportResult struct {
	Status          string   `json:"status" yaml:"status"`
	BundleSHA256    string   `json:"bundle_sha256" yaml:"bundle_sha256"`
	KeyID           string   `json:"key_id" yaml:"key_id"`
	DestDir         string   `json:"dest_dir" yaml:"dest_dir"`
	MirrorDir       string   `json:"mirror_dir,omitempty" yaml:"mirror_dir,omitempty"`
	ImportedCount   int      `json:"imported_count" yaml:"imported_count"`
	ExtensionSlugs  []string `json:"extension_slugs,omitempty" yaml:"extension_slugs,omitempty"`
	AppliedIndex    bool     `json:"applied_index" yaml:"applied_index"`
	AppliedRevoc    bool     `json:"applied_revocations" yaml:"applied_revocations"`
}

// ImportBundle verifies the entire .tpbundle before touching destination files or DB,
// then stages and publishes the verified artifacts atomically with full rollback on error.
func ImportBundle(ctx context.Context, opts ImportOptions) (*ImportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lookupEnv := opts.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	destDir := strings.TrimSpace(opts.DestDir)
	if destDir == "" {
		dataDir := strings.TrimSpace(opts.DataDir)
		if dataDir == "" {
			if v, ok := lookupEnv("TANPOPO_DATA_DIR"); ok && strings.TrimSpace(v) != "" {
				dataDir = strings.TrimSpace(v)
			} else {
				dataDir = "data"
			}
		}
		destDir = filepath.Join(dataDir, "airgap")
	}

	var (
		lastIdxSerial int64
		lastRevSerial int64
		regStore      *registry.SQLStore
		dbClose       func() error
	)
	if dbPath := strings.TrimSpace(opts.DBPath); dbPath != "" {
		if _, err := os.Lstat(dbPath); err == nil {
			st, serr := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
			if serr != nil {
				return nil, fmt.Errorf("open registry database %q: %w", dbPath, serr)
			}
			dbClose = st.Close
			defer func() {
				if dbClose != nil {
					_ = dbClose()
				}
			}()
			rs, rerr := registry.NewSQLStore(st.RawDB(), opts.Now)
			if rerr != nil {
				return nil, rerr
			}
			regStore = rs
			if lastIdxSerial, err = regStore.LastIndexSerial(ctx); err != nil {
				return nil, fmt.Errorf("read stored index serial: %w", err)
			}
			if lastRevSerial, err = regStore.LastRevocationSerial(ctx); err != nil {
				return nil, fmt.Errorf("read stored revocation serial: %w", err)
			}
		}
	}

	// Verify 100% of bundle signatures, digests, .tpkg entries, and revocation expiry BEFORE
	// modifying destination state or mirroring artifacts (CT-31 atomic fail-closed).
	verified, err := verifyAndReadBundle(ctx, VerifyOptions{
		BundlePath:           opts.BundlePath,
		PublicKeyPath:        opts.PublicKeyPath,
		DataDir:              opts.DataDir,
		LastIndexSerial:      lastIdxSerial,
		LastRevocationSerial: lastRevSerial,
		Now:                  opts.Now,
		LookupEnv:            lookupEnv,
	})
	if err != nil {
		return nil, err
	}

	var mirrorDir string
	if rawMirror := strings.TrimSpace(opts.MirrorTo); rawMirror != "" {
		resolvedMirror, merr := resolveOfflineMirrorDir(rawMirror, lookupEnv)
		if merr != nil {
			return nil, merr
		}
		mirrorDir = resolvedMirror
	}

	// Stage into a sibling temporary directory so any I/O failure preserves destDir untouched.
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absDest), 0o750); err != nil {
		return nil, err
	}
	stageDir, err := os.MkdirTemp(filepath.Dir(absDest), ".tanpopo-airgap-stage-*")
	if err != nil {
		return nil, err
	}
	cleanupStage := true
	defer func() {
		if cleanupStage {
			_ = os.RemoveAll(stageDir)
		}
	}()

	for relPath, content := range verified.Files {
		cleanRel, cerr := cleanArchivePath(relPath)
		if cerr != nil {
			return nil, cerr
		}
		target := filepath.Join(stageDir, filepath.FromSlash(cleanRel))
		if err := writeFileAtomic(target, content, 0o640); err != nil {
			return nil, fmt.Errorf("stage %s: %w", cleanRel, err)
		}
	}

	// Stage optional local mirror before swapping primary destination.
	var stagedMirrorDir string
	if mirrorDir != "" {
		absMirror, merr := filepath.Abs(mirrorDir)
		if merr != nil {
			return nil, merr
		}
		if err := os.MkdirAll(filepath.Dir(absMirror), 0o750); err != nil {
			return nil, err
		}
		stagedMirrorDir, err = os.MkdirTemp(filepath.Dir(absMirror), ".tanpopo-mirror-stage-*")
		if err != nil {
			return nil, err
		}
		defer func() {
			if stagedMirrorDir != "" {
				_ = os.RemoveAll(stagedMirrorDir)
			}
		}()
		for _, art := range verified.Manifest.Artifacts {
			target := filepath.Join(stagedMirrorDir, filepath.FromSlash(art.Path))
			if err := writeFileAtomic(target, verified.Files[art.Path], 0o644); err != nil {
				return nil, fmt.Errorf("stage mirror %s: %w", art.Path, err)
			}
		}
		for _, feedPath := range []string{indexEntryName, revocEntryName, lockfileEntryName} {
			if content, exists := verified.Files[feedPath]; exists {
				target := filepath.Join(stagedMirrorDir, filepath.FromSlash(feedPath))
				if err := writeFileAtomic(target, content, 0o644); err != nil {
					return nil, fmt.Errorf("stage mirror %s: %w", feedPath, err)
				}
			}
		}
	}

	// Publish directories with deferred commit/rollback so later failures revert prior swaps.
	var (
		commits   []func()
		rollbacks []func()
		committed bool
	)
	defer func() {
		if !committed {
			for i := len(rollbacks) - 1; i >= 0; i-- {
				rollbacks[i]()
			}
		}
	}()

	commitDest, rollbackDest, err := prepareDirSwap(stageDir, absDest)
	if err != nil {
		return nil, fmt.Errorf("publish air-gap bundle directory: %w", err)
	}
	cleanupStage = false
	rollbacks = append(rollbacks, rollbackDest)
	commits = append(commits, commitDest)

	if mirrorDir != "" {
		absMirror, _ := filepath.Abs(mirrorDir)
		commitMirror, rollbackMirror, err := prepareDirSwap(stagedMirrorDir, absMirror)
		if err != nil {
			return nil, fmt.Errorf("publish offline mirror directory: %w", err)
		}
		stagedMirrorDir = ""
		rollbacks = append(rollbacks, rollbackMirror)
		commits = append(commits, commitMirror)
		mirrorDir = absMirror
	}

	appliedIndex := false
	appliedRevoc := false
	if regStore != nil {
		if verified.IndexFeed != nil {
			if err := regStore.ApplyIndexFeed(ctx, verified.IndexFeed.VersionSerial, verified.IndexFeed.ExpiresAt, verified.IndexEntries); err != nil {
				return nil, contractFail("failed persisting verified marketplace index", err)
			}
			appliedIndex = true
		}
		if verified.RevFeed != nil {
			if err := regStore.ApplyRevocationFeed(ctx, verified.RevFeed.VersionSerial, verified.RevFeed.ExpiresAt, verified.RevEntries); err != nil {
				return nil, contractFail("failed persisting verified revocation feed", err)
			}
			appliedRevoc = true
		}
	}

	committed = true
	for _, commitFn := range commits {
		commitFn()
	}

	return &ImportResult{
		Status:         "IMPORTED",
		BundleSHA256:   verified.Result.BundleSHA256,
		KeyID:          verified.Result.KeyID,
		DestDir:        absDest,
		MirrorDir:      mirrorDir,
		ImportedCount:  len(verified.Manifest.Artifacts),
		ExtensionSlugs: verified.Result.ExtensionSlugs,
		AppliedIndex:   appliedIndex,
		AppliedRevoc:   appliedRevoc,
	}, nil
}

func resolveOfflineMirrorDir(raw string, lookupEnv func(string) (string, bool)) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "file://") {
		u, err := url.Parse(trimmed)
		if err != nil || strings.TrimSpace(u.Path) == "" {
			return "", validationFail(fmt.Sprintf("invalid file:// mirror URI %q", raw), err)
		}
		return u.Path, nil
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "oci://") {
		u, err := url.Parse(trimmed)
		if err != nil || strings.TrimSpace(u.Host) == "" {
			return "", validationFail(fmt.Sprintf("invalid oci:// mirror URI %q", raw), err)
		}
		root := ""
		if lookupEnv != nil {
			root, _ = lookupEnv("TANPOPO_BUNDLE_OCI_MIRROR_ROOT")
		}
		root = strings.TrimSpace(root)
		if root == "" {
			return "", validationFail("offline air-gap mode forbids remote oci:// network pushes; set TANPOPO_BUNDLE_OCI_MIRROR_ROOT or use a local directory / file:// mirror", nil)
		}
		relRepo := strings.TrimPrefix(filepath.Clean("/"+u.Path), "/")
		return filepath.Join(root, u.Host, filepath.FromSlash(relRepo)), nil
	}
	if strings.Contains(trimmed, "://") {
		return "", validationFail(fmt.Sprintf("unsupported mirror URI scheme in offline mode: %q", raw), nil)
	}
	return trimmed, nil
}

func prepareDirSwap(stageDir, targetDir string) (commit func(), rollback func(), err error) {
	info, lerr := os.Lstat(targetDir)
	if lerr != nil {
		if os.IsNotExist(lerr) {
			if err := os.Rename(stageDir, targetDir); err != nil {
				return nil, nil, err
			}
			return func() {}, func() { _ = os.RemoveAll(targetDir) }, nil
		}
		return nil, nil, lerr
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, validationFail(fmt.Sprintf("refusing symlink destination %q", targetDir), nil)
	}
	backupDir := fmt.Sprintf("%s.bak-%d", targetDir, time.Now().UTC().UnixNano())
	if err := os.Rename(targetDir, backupDir); err != nil {
		return nil, nil, err
	}
	if err := os.Rename(stageDir, targetDir); err != nil {
		_ = os.Rename(backupDir, targetDir)
		return nil, nil, err
	}
	commit = func() {
		_ = os.RemoveAll(backupDir)
	}
	rollback = func() {
		rollbackTmp := fmt.Sprintf("%s.rollback-%d", targetDir, time.Now().UTC().UnixNano())
		_ = os.Rename(targetDir, rollbackTmp)
		if err := os.Rename(backupDir, targetDir); err == nil {
			_ = os.RemoveAll(rollbackTmp)
		} else {
			_ = os.RemoveAll(stageDir)
		}
	}
	return commit, rollback, nil
}
