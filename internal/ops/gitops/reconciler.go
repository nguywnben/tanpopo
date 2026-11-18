// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package gitops validates a declarative configuration and its immutable
// extension lock, then atomically replaces only the applied configuration.
package gitops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"gopkg.in/yaml.v3"
)

const (
	maxConfigBytes = 4 << 20
	maxLockBytes   = 8 << 20
)

var (
	ErrInvalidInput     = errors.New("gitops: invalid input")
	ErrUnsafeConfig     = errors.New("gitops: configuration rejected")
	ErrLockIntegrity    = errors.New("gitops: lockfile integrity check failed")
	ErrArtifactMismatch = errors.New("gitops: package digest check failed")
	ErrApply            = errors.New("gitops: configuration reconciliation failed")
)

var envReference = regexp.MustCompile(`^\$\{ENV:([A-Za-z_][A-Za-z0-9_]*)\}$`)

// Options names the desired config, its applied destination, the immutable
// lockfile, and the already-resolved artifact paths keyed by lock extension ID.
// Artifact path resolution is left to the caller; package bytes are always
// hashed here before any configuration is applied.
type Options struct {
	DesiredConfigPath string
	AppliedConfigPath string
	LockfilePath      string
	ArtifactPaths     map[string]string
}

// Reconcile validates desired YAML, validates tanpopo.lock and every pinned
// package digest, then atomically replaces AppliedConfigPath. It never writes
// LockfilePath. Secret-valued fields may contain only a complete ${ENV:NAME}
// reference; the referenced environment value is never read by this package.
func Reconcile(ctx context.Context, opts Options) error {
	if ctx == nil || strings.TrimSpace(opts.DesiredConfigPath) == "" || strings.TrimSpace(opts.AppliedConfigPath) == "" || strings.TrimSpace(opts.LockfilePath) == "" {
		return ErrInvalidInput
	}
	desired, err := readRegularFile(opts.DesiredConfigPath, maxConfigBytes)
	if err != nil || validateConfig(desired) != nil {
		return ErrUnsafeConfig
	}
	lockBytes, err := readRegularFile(opts.LockfilePath, maxLockBytes)
	if err != nil {
		return ErrLockIntegrity
	}
	lf, err := lockfile.ParseBytes(lockBytes)
	if err != nil {
		return ErrLockIntegrity
	}
	if err := verifyArtifacts(lf, opts.ArtifactPaths); err != nil {
		return ErrArtifactMismatch
	}
	if ctx.Err() != nil {
		return ErrApply
	}
	// Refuse a lock change during validation. The lock remains a read-only input
	// and its content is never rewritten as part of this operation.
	currentLock, err := readRegularFile(opts.LockfilePath, maxLockBytes)
	if err != nil || !bytes.Equal(lockBytes, currentLock) {
		return ErrLockIntegrity
	}
	if err := replaceAtomically(ctx, opts.AppliedConfigPath, desired); err != nil {
		return ErrApply
	}
	return nil
}

func validateConfig(raw []byte) error {
	if len(raw) == 0 || len(raw) > maxConfigBytes {
		return ErrUnsafeConfig
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		return ErrUnsafeConfig
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrUnsafeConfig
	}
	return validateNode(&doc, false, make(map[*yaml.Node]bool))
}

func validateNode(node *yaml.Node, secretField bool, seen map[*yaml.Node]bool) error {
	if node == nil || seen[node] {
		return ErrUnsafeConfig
	}
	seen[node] = true
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			if err := validateNode(child, secretField, seen); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		keys := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" {
				return ErrUnsafeConfig
			}
			if _, exists := keys[key.Value]; exists {
				return ErrUnsafeConfig
			}
			keys[key.Value] = struct{}{}
			if err := validateNode(value, isSecretField(key.Value), seen); err != nil {
				return err
			}
		}
		if len(node.Content)%2 != 0 {
			return ErrUnsafeConfig
		}
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			if secretField {
				return ErrUnsafeConfig
			}
			return nil
		}
		value := node.Value
		match := envReference.FindStringSubmatch(value)
		if strings.Contains(value, "${ENV:") && match == nil {
			return ErrUnsafeConfig
		}
		if secretField && (value == "" || match == nil) {
			return ErrUnsafeConfig
		}
	case yaml.AliasNode:
		return ErrUnsafeConfig
	default:
		return ErrUnsafeConfig
	}
	return nil
}

func isSecretField(key string) bool {
	canonical := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.TrimSpace(key)))
	for _, marker := range []string{"apikey", "authorization", "credential", "token", "password", "secret", "privatekey", "masterkey", "masterkek"} {
		if strings.Contains(canonical, marker) {
			return true
		}
	}
	return false
}

func verifyArtifacts(lf *lockfile.Lockfile, paths map[string]string) error {
	if lf == nil || len(paths) != len(lf.Extensions) {
		return ErrArtifactMismatch
	}
	for packageID, entry := range lf.Extensions {
		path, ok := paths[packageID]
		if !ok || strings.TrimSpace(path) == "" {
			return ErrArtifactMismatch
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return ErrArtifactMismatch
		}
		if err := lf.VerifyArtifactDigest(packageID, digest); err != nil {
			return ErrArtifactMismatch
		}
		if strings.TrimSpace(entry.ArtifactSHA256) == "" {
			return ErrArtifactMismatch
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrArtifactMismatch
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ErrArtifactMismatch
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return "", ErrArtifactMismatch
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", ErrArtifactMismatch
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func readRegularFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > limit {
		return nil, ErrInvalidInput
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrInvalidInput
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, ErrInvalidInput
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrInvalidInput
	}
	return raw, nil
}

func replaceAtomically(ctx context.Context, target string, content []byte) error {
	dir := filepath.Dir(target)
	dirInfo, err := os.Lstat(dir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return ErrApply
	}
	mode := os.FileMode(0o600)
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return ErrApply
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return ErrApply
	}
	tmp, err := os.CreateTemp(dir, ".tanpopo-config-*.tmp")
	if err != nil {
		return ErrApply
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return ErrApply
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return ErrApply
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return ErrApply
	}
	if err := tmp.Close(); err != nil {
		return ErrApply
	}
	if ctx.Err() != nil {
		return ErrApply
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return ErrApply
	}
	if err := syncDirectory(dir); err != nil {
		return ErrApply
	}
	return nil
}
