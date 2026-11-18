// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTanpopoRootCLIPluginFlow(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "tanpopo version") {
		t.Fatalf("--version unexpected: code=%d out=%q err=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"unknown-cmd"}, &stdout, &stderr); code != 2 {
		t.Fatalf("expected exit code 2 on unknown command, got %d", code)
	}

	workDir := filepath.Join(t.TempDir(), "e2e-plugin")
	for _, args := range [][]string{
		{"plugin", "init", "e2e-plugin", "--tier", "declarative", "--dir", workDir},
		{"plugin", "validate", workDir},
		{"plugin", "test", workDir},
		{"plugin", "pack", workDir},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("command %v failed with code=%d: out=%q err=%q", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestTanpopoRootSupportBundleCommand(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(t.TempDir(), "diagnostic.tar.gz")
	var stdout, stderr bytes.Buffer
	code := run([]string{"support-bundle", "--root", root, "-o", output}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("support-bundle failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("bundle missing: %v", err)
	}
}
