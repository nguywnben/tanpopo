// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package upgrade

import "testing"

func TestEvaluatePreflightCompatibilityAndFailClosedRanges(t *testing.T) {
	tests := []struct {
		name          string
		target        string
		installed     []InstalledExtension
		wantCompatible bool
		wantBlockers  int
	}{
		{name: "all ranges accept target", target: "v0.2.0", installed: []InstalledExtension{{InstallationID: "i1", PackageID: "adapter", Version: "1.0.0", CoreRange: ">=0.1.0 <0.3.0"}}, wantCompatible: true},
		{name: "out of range blocks", target: "v0.3.0", installed: []InstalledExtension{{InstallationID: "i1", PackageID: "adapter", Version: "1.0.0", CoreRange: ">=0.1.0 <0.3.0"}}, wantBlockers: 1},
		{name: "invalid persisted range blocks", target: "v0.2.0", installed: []InstalledExtension{{InstallationID: "i1", PackageID: "adapter", Version: "1.0.0", CoreRange: "not a range"}}, wantBlockers: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Evaluate(test.target, test.installed)
			if err != nil {
				t.Fatalf("Evaluate returned unexpected error: %v", err)
			}
			if got.Compatible != test.wantCompatible || len(got.BlockingExtensions) != test.wantBlockers {
				t.Fatalf("Evaluate() = %+v, want compatible=%t blockers=%d", got, test.wantCompatible, test.wantBlockers)
			}
		})
	}
}

func TestEvaluateRejectsInvalidTargetVersion(t *testing.T) {
	if _, err := Evaluate("not-semver", nil); err == nil {
		t.Fatal("Evaluate accepted an invalid target core version")
	}
}
