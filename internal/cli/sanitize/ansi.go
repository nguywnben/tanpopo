// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package sanitize contains terminal-safe formatting helpers for CLI output.
package sanitize

import "github.com/nguywnben/tanpopo/internal/extension/testharness"

// Terminal strips escape and control sequences from untrusted terminal text.
func Terminal(text string) string { return testharness.SanitizeTerminalText(text) }
