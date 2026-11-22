// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("locate repo root from %s: %v", wd, err)
	}
	return root
}

func TestCheckDriftRepositorySyncAndQuickstart(t *testing.T) {
	root := repoRootForTest(t)
	report, err := RunDriftCheck(DriftConfig{
		RootDir:           root,
		ExecuteQuickstart: true,
		QuickstartTimeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunDriftCheck failed: %v", err)
	}
	if report.CLICommandsChecked < 20 {
		t.Fatalf("expected >= 20 CLI commands audited, got %d", report.CLICommandsChecked)
	}
	if report.IncidentRunbooksOK != 7 {
		t.Fatalf("expected 7 incident runbooks verified, got %d", report.IncidentRunbooksOK)
	}
	if report.QuickstartElapsed <= 0 || report.QuickstartElapsed >= 45*time.Second {
		t.Fatalf("unexpected quickstart probe duration: %s", report.QuickstartElapsed)
	}
}

func TestCheckDriftNEG1FlagRenameFails(t *testing.T) {
	helpRoot := `
Usage:
  tanpopo [command]

Available Commands:
  serve       Run server

Flags:
      --output string   Output format
`
	helpServe := `
Usage:
  tanpopo serve [flags]

Flags:
      --listen string   HTTP listen address
      --role string     Service role
`
	tree, err := CrawlCLITree(func(args ...string) (string, error) {
		if len(args) == 0 {
			return helpRoot, nil
		}
		if len(args) == 1 && args[0] == "serve" {
			return helpServe, nil
		}
		return "", nil
	})
	if err != nil {
		t.Fatalf("CrawlCLITree: %v", err)
	}

	tmp := t.TempDir()
	docRel := "stale-quickstart.md"
	docBody := "```bash\ntanpopo serve --role all --renamed-port 8080\n```\n"
	if err := os.WriteFile(filepath.Join(tmp, docRel), []byte(docBody), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = CheckCLIDocsSync(tmp, []string{docRel}, tree)
	if err == nil {
		t.Fatalf("expected NEG-1 CLI flag rename drift check to fail, got nil error")
	}
	if !strings.Contains(err.Error(), "--renamed-port") {
		t.Fatalf("expected error mentioning --renamed-port, got: %v", err)
	}
}

func TestCheckDriftNEG1ShortAndTrailingFlagDriftFail(t *testing.T) {
	helpRoot := `
Usage:
  tanpopo [command]

Available Commands:
  serve       Run server
`
	helpServe := `
Usage:
  tanpopo serve [flags]

Flags:
      --role string   Service role
`
	tree, err := CrawlCLITree(func(args ...string) (string, error) {
		if len(args) == 0 {
			return helpRoot, nil
		}
		if len(args) == 1 && args[0] == "serve" {
			return helpServe, nil
		}
		return "", nil
	})
	if err != nil {
		t.Fatalf("CrawlCLITree: %v", err)
	}

	tmp := t.TempDir()
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "short_flag", body: "```bash\ntanpopo serve -z\n```\n", want: `"-z"`},
		{name: "trailing_pipe_flag", body: "```bash\ntanpopo serve --bogus-flag | cat\n```\n", want: `"--bogus-flag"`},
	} {
		rel := tc.name + ".md"
		if err := os.WriteFile(filepath.Join(tmp, rel), []byte(tc.body), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, err := CheckCLIDocsSync(tmp, []string{rel}, tree)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %s, got: %v", tc.name, tc.want, err)
		}
	}
}

func TestCheckDriftNEG1BrokenLinkAndAlertRunbookDriftFail(t *testing.T) {
	tmp := t.TempDir()
	docPath := filepath.Join(tmp, "README.md")
	if err := os.WriteFile(docPath, []byte("[Broken](./does-not-exist.md)\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := CheckMarkdownLinksAndAssets(tmp, []string{"README.md"}); err == nil {
		t.Fatalf("expected broken markdown link check to fail")
	}

	// NEG-1: Alert runbook drift fails CheckMonitoringRunbooks cleanly.
	for _, dir := range []string{
		filepath.Join(tmp, "deploy", "monitoring"),
		filepath.Join(tmp, "docs", "operations"),
		filepath.Join(tmp, "docs", "guides"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "deploy", "monitoring", "alerts.yaml"), []byte(`groups:
  - name: tanpopo
    rules:
      - alert: BudgetBurn
        labels:
          runbook: INC-01
        annotations:
          runbook_ref: docs/operations/monitoring-incidents.md#inc-99
`), 0o600); err != nil {
		t.Fatalf("WriteFile alerts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "docs", "operations", "monitoring-incidents.md"), []byte("### `INC-01`: Budget\n"), 0o600); err != nil {
		t.Fatalf("WriteFile runbook: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "docs", "guides", "production-ha-and-airgap.md"), []byte("[INC-01](../operations/monitoring-incidents.md#inc-01)\n"), 0o600); err != nil {
		t.Fatalf("WriteFile journey3: %v", err)
	}
	if _, err := CheckMonitoringRunbooks(tmp); err == nil || !strings.Contains(err.Error(), "runbook_ref") {
		t.Fatalf("expected CheckMonitoringRunbooks drift failure on #inc-99, got: %v", err)
	}
}
