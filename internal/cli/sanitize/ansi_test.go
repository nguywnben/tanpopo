// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package sanitize

import "testing"

func TestTerminalRemovesCSIAndOSC52(t *testing.T) {
	got := Terminal("safe\x1b[31m red\x1b[0m\x1b]52;c;clipboard-data\x07 text")
	if got != "safe red text" {
		t.Fatalf("Terminal()=%q", got)
	}
}
