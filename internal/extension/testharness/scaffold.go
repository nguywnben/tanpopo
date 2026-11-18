// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package testharness

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nguywnben/tanpopo/sdk"
)

var validPluginSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// ScaffoldOptions configures `tanpopo plugin init`.
type ScaffoldOptions struct {
	Name      string
	Namespace string
	Tier      string
	Dir       string
	Force     bool
}

// ScaffoldResult reports files created by ScaffoldExtension.
type ScaffoldResult struct {
	ExtensionID string   `json:"extension_id" yaml:"extension_id"`
	Tier        string   `json:"tier" yaml:"tier"`
	OutputDir   string   `json:"output_dir" yaml:"output_dir"`
	Files       []string `json:"files" yaml:"files"`
	Status      string   `json:"status" yaml:"status"`
}

// ScaffoldExtension materializes a standalone Tier-1 Declarative or Tier-2 Wasm
// adapter workspace from binary-embedded SDK templates with zero network calls.
func ScaffoldExtension(opts ScaffoldOptions) (*ScaffoldResult, error) {
	name := strings.TrimSpace(opts.Name)
	if !validPluginSlugRe.MatchString(name) || strings.Contains(name, "..") {
		return nil, validationErr(fmt.Sprintf("invalid plugin name %q: must match ^[a-z0-9][a-z0-9._-]{0,62}$", opts.Name), nil)
	}

	tmplRoot, canonicalTier, oldID, oldSlug, err := resolveTemplateTier(opts.Tier)
	if err != nil {
		return nil, err
	}

	outDir := strings.TrimSpace(opts.Dir)
	if outDir == "" {
		outDir = name
	}
	if info, statErr := os.Lstat(outDir); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, securityErr(fmt.Sprintf("target directory %q cannot be a symlink", outDir), nil)
		}
		if !info.IsDir() {
			return nil, validationErr(fmt.Sprintf("target path %q exists and is not a directory", outDir), nil)
		}
		entries, rerr := os.ReadDir(outDir)
		if rerr != nil {
			return nil, validationErr("read target directory", rerr)
		}
		if len(entries) > 0 && !opts.Force {
			return nil, validationErr(fmt.Sprintf("target directory %q is non-empty (pass --force to overwrite)", outDir), nil)
		}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, validationErr("create target plugin directory", err)
	}

	namespace := strings.TrimSpace(opts.Namespace)
	if namespace == "" {
		namespace = "example"
	}
	if !validPluginSlugRe.MatchString(namespace) || strings.Contains(namespace, "..") {
		return nil, validationErr(fmt.Sprintf("invalid plugin namespace %q", opts.Namespace), nil)
	}
	extID := "@" + namespace + "/" + name
	var createdFiles []string

	err = fs.WalkDir(sdk.EmbeddedAssets, tmplRoot, func(path string, d fs.DirEntry, wErr error) error {
		if wErr != nil {
			return wErr
		}
		rel := strings.TrimPrefix(path, tmplRoot)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		dstPath := filepath.Join(outDir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		data, rerr := sdk.EmbeddedAssets.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if rel == "manifest.yaml" {
			txt := string(data)
			txt = strings.ReplaceAll(txt, oldID, extID)
			txt = strings.ReplaceAll(txt, oldSlug, name)
			data = []byte(txt)
		}
		if werr := os.WriteFile(dstPath, data, 0o644); werr != nil {
			return werr
		}
		createdFiles = append(createdFiles, rel)
		return nil
	})
	if err != nil {
		return nil, validationErr("copy embedded extension template", err)
	}

	return &ScaffoldResult{
		ExtensionID: extID,
		Tier:        canonicalTier,
		OutputDir:   outDir,
		Files:       createdFiles,
		Status:      "CREATED",
	}, nil
}

func resolveTemplateTier(rawTier string) (tmplRoot, canonicalTier, oldID, oldSlug string, err error) {
	norm := strings.ToLower(strings.TrimSpace(rawTier))
	switch norm {
	case "", "1", "tier1", "declarative", "declarative_t1":
		return "templates/tier1-declarative", "declarative", "@example/starter-declarative-adapter", "starter-declarative", nil
	case "2", "tier2", "wasm", "sandboxed_wasm", "wasm_sandbox_t2":
		return "templates/tier2-wasm", "sandboxed_wasm", "@example/starter-wasm-adapter", "starter-wasm", nil
	default:
		return "", "", "", "", validationErr(fmt.Sprintf("unsupported tier %q: expected 1|declarative or 2|wasm", rawTier), nil)
	}
}
