// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package testharness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateCheckExtension_CT24AndNMinusOne(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	scaffoldRes, err := ScaffoldExtension(ScaffoldOptions{
		Name:      "nminus1-probe",
		Namespace: "acme",
		Tier:      "declarative",
		Dir:       tmp,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("ScaffoldExtension: %v", err)
	}

	t.Run("compatible_active_contract", func(t *testing.T) {
		rep, err := MigrateCheckExtension(ctx, MigrateCheckOptions{Dir: scaffoldRes.OutputDir})
		if err != nil {
			t.Fatalf("expected compatible report, got err=%v", err)
		}
		if !rep.Compatible || rep.DeprecatedContract || rep.Status != "COMPATIBLE" || rep.ExitCode != ExitOK {
			t.Fatalf("unexpected report: %+v", rep)
		}
	})

	t.Run("n_minus_1_deprecation_warning_passes_when_in_window", func(t *testing.T) {
		// Force manifest coreVersionRange to cover >=0.1.0 <0.3.0 so 0.2.0 satisfies range while contractVersion 0.1 triggers N-1 warning against supported [0.1, 0.2].
		mPath := filepath.Join(scaffoldRes.OutputDir, "manifest.yaml")
		raw, err := os.ReadFile(mPath)
		if err != nil {
			t.Fatal(err)
		}
		updated := strings.Replace(string(raw), `">=0.1.0 <0.2.0"`, `">=0.1.0 <0.3.0"`, 1)
		updated = strings.Replace(updated, `>=0.1.0 <0.2.0`, `>=0.1.0 <0.3.0`, 1)
		if err := os.WriteFile(mPath, []byte(updated), 0o600); err != nil {
			t.Fatal(err)
		}
		rep, err := MigrateCheckExtension(ctx, MigrateCheckOptions{
			Dir:                       scaffoldRes.OutputDir,
			TargetCoreVersion:         "0.2.0",
			SupportedContractVersions: []string{"0.1", "0.2"},
		})
		if err != nil {
			t.Fatalf("expected N-1 window to remain compatible, got err=%v", err)
		}
		if !rep.Compatible || !rep.DeprecatedContract || rep.Status != "COMPATIBLE_WITH_DEPRECATION_WARNING" || len(rep.Findings) == 0 {
			t.Fatalf("expected deprecation warning report, got %+v", rep)
		}
	})

	t.Run("incompatible_contract_version_exits_22", func(t *testing.T) {
		rep, err := MigrateCheckExtension(ctx, MigrateCheckOptions{
			Dir:                       scaffoldRes.OutputDir,
			TargetCoreVersion:         "0.2.0",
			SupportedContractVersions: []string{"0.2"},
		})
		if err == nil || CodeOf(err) != ExitContract {
			t.Fatalf("expected ExitContract (22), got err=%v code=%d", err, CodeOf(err))
		}
		if rep == nil || rep.Compatible || rep.Status != "INCOMPATIBLE" || rep.ExitCode != ExitContract {
			t.Fatalf("expected INCOMPATIBLE report, got %+v", rep)
		}
	})

	t.Run("incompatible_core_version_range_exits_22", func(t *testing.T) {
		rep, err := MigrateCheckExtension(ctx, MigrateCheckOptions{
			Dir:                       scaffoldRes.OutputDir,
			TargetCoreVersion:         "2.0.0",
			SupportedContractVersions: []string{"0.1"},
		})
		if err == nil || CodeOf(err) != ExitContract {
			t.Fatalf("expected ExitContract (22) for coreVersionRange mismatch, got err=%v code=%d", err, CodeOf(err))
		}
		if rep == nil || rep.Compatible || rep.Status != "INCOMPATIBLE" {
			t.Fatalf("unexpected report: %+v", rep)
		}
	})

	t.Run("legacy_scoped_secret_unseal_rejected_exit_14", func(t *testing.T) {
		badDir := filepath.Join(t.TempDir(), "bad-unseal")
		if err := os.MkdirAll(badDir, 0o755); err != nil {
			t.Fatal(err)
		}
		badManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/bad-unseal"
  version: "0.1.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.2.0"
capabilities:
  - scoped_secret_unseal
`
		if err := os.WriteFile(filepath.Join(badDir, "manifest.yaml"), []byte(badManifest), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := MigrateCheckExtension(ctx, MigrateCheckOptions{Dir: badDir})
		if err == nil || CodeOf(err) != ExitSecurity {
			t.Fatalf("expected ExitSecurity (14) for removed scoped_secret_unseal, got err=%v code=%d", err, CodeOf(err))
		}
	})

	t.Run("unordered_supported_contracts_still_identifies_highest_version", func(t *testing.T) {
		rep, err := MigrateCheckExtension(ctx, MigrateCheckOptions{
			Dir:                       scaffoldRes.OutputDir,
			TargetCoreVersion:         "0.2.0",
			SupportedContractVersions: []string{"0.2", "0.1"},
		})
		if err != nil {
			t.Fatalf("expected N-1 warning when descending order passed, got err=%v", err)
		}
		if !rep.Compatible || !rep.DeprecatedContract || rep.Status != "COMPATIBLE_WITH_DEPRECATION_WARNING" {
			t.Fatalf("expected N-1 warning regardless of flag order, got %+v", rep)
		}
	})

	t.Run("scoped_secret_unseal_inside_capability_type_exits_14", func(t *testing.T) {
		badDir := filepath.Join(t.TempDir(), "bad-unseal-type")
		if err := os.MkdirAll(badDir, 0o755); err != nil {
			t.Fatal(err)
		}
		badManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/bad-unseal-type"
  version: "0.1.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.2.0"
capabilities:
  - type: "scoped_secret_unseal"
`
		if err := os.WriteFile(filepath.Join(badDir, "manifest.yaml"), []byte(badManifest), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := MigrateCheckExtension(ctx, MigrateCheckOptions{Dir: badDir})
		if err == nil || CodeOf(err) != ExitSecurity {
			t.Fatalf("expected ExitSecurity (14) for capabilities[].type scoped_secret_unseal, got err=%v code=%d", err, CodeOf(err))
		}
	})

	t.Run("wildcard_egress_security_violation_exits_14", func(t *testing.T) {
		badDir := filepath.Join(t.TempDir(), "bad-wildcard")
		if err := os.MkdirAll(badDir, 0o755); err != nil {
			t.Fatal(err)
		}
		wild := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@acme/bad-wildcard"
  name: Bad Wildcard
  version: "0.1.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
permissions:
  networkEgress:
    allowedHosts: ["*.example.com"]
    allowPrivateIp: false
    requireTls: true
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "bad-wild"
    supportedDialects: ["openai"]
    nativePassthroughPrefix: "/native/bad-wild"
    allowedNativePaths: ["^v1/chat/completions$"]
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
`
		if err := os.WriteFile(filepath.Join(badDir, "manifest.yaml"), []byte(wild), 0o600); err != nil {
			t.Fatal(err)
		}
		rep, err := MigrateCheckExtension(ctx, MigrateCheckOptions{
			Dir:                       badDir,
			TargetCoreVersion:         "0.2.0",
			SupportedContractVersions: []string{"0.1"},
		})
		if err == nil || CodeOf(err) != ExitSecurity {
			t.Fatalf("expected ExitSecurity (14) for wildcard egress, got err=%v code=%d rep=%+v", err, CodeOf(err), rep)
		}
	})

	t.Run("g122_symlink_inside_extension_dir_rejected_exit_14", func(t *testing.T) {
		symDir := filepath.Join(t.TempDir(), "sym-ext")
		if err := os.MkdirAll(symDir, 0o755); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(scaffoldRes.OutputDir, "manifest.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(symDir, "manifest.yaml"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc/passwd", filepath.Join(symDir, "leak.txt")); err != nil {
			t.Fatalf("symlink setup failed: %v", err)
		}
		if _, err := MigrateCheckExtension(ctx, MigrateCheckOptions{Dir: symDir}); err == nil || CodeOf(err) != ExitSecurity {
			t.Fatalf("G122 regression: expected ExitSecurity (14) for symlink inside extension dir, got err=%v code=%d", err, CodeOf(err))
		}
	})
}
