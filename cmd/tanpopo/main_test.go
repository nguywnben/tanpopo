// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersionAndHelp(t *testing.T) {
	for _, arg := range []string{"--version", "-v", "version"} {
		var out, errBuf bytes.Buffer
		code := run([]string{arg}, &out, &errBuf)
		if code != 0 {
			t.Fatalf("%s returned exit code %d, want 0 (stderr=%q)", arg, code, errBuf.String())
		}
		if !strings.Contains(out.String(), "tanpopo version ") {
			t.Fatalf("%s output %q missing version banner", arg, out.String())
		}
	}

	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"help"}} {
		var out, errBuf bytes.Buffer
		code := run(args, &out, &errBuf)
		if code != 0 {
			t.Fatalf("help(%v) returned exit code %d, want 0", args, code)
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Fatalf("help(%v) output %q missing Usage section", args, out.String())
		}
	}
}

func TestRunUnknownCommandReturnsValidationExitCode2(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"unknown-subcommand"}, &out, &errBuf)
	if code != 2 {
		t.Fatalf("unknown command returned %d, want SPEC-OPS-04 validation exit code 2", code)
	}
	if !strings.Contains(errBuf.String(), "unknown command or flag") {
		t.Fatalf("unexpected stderr: %q", errBuf.String())
	}
}
