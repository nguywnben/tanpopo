// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package support

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/canary"
)

func TestCreateBundleRedactsAndRestrictsArchivePermissions(t *testing.T) {
	root := t.TempDir()
	secret := canary.DefaultCanarySecret()
	config := "provider: [invalid\nTANPOPO_MASTER_KEY: yaml-secret-material\n"
	if err := os.WriteFile(filepath.Join(root, "tanpopo.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(root, "logs")
	if err := os.Mkdir(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "bad.log"), []byte("TANPOPO_MASTER_KEY=plain-secret-material\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "recent.jsonl"), []byte(`{"api_key":"`+secret+`","authorization":"Bearer secret-token-value","messages":[{"content":"private prompt"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "support.tar.gz")
	if err := CreateBundle(t.Context(), root, archive); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		got := info.Mode().Perm()
		t.Fatalf("archive mode=%#o, want 0600", got)
	}
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var all strings.Builder
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
		if hdr.Mode != 0o600 {
			t.Errorf("member %s mode=%#o", hdr.Name, hdr.Mode)
		}
	}
	text := all.String()
	if !strings.Contains(text, `"name":"storage-fs-lock"`) || !strings.Contains(text, `"name":"egress-dns-ssrf","status":"pass"`) {
		t.Fatalf("support bundle omitted the doctor health report: %s", text)
	}
	for _, value := range []string{secret, "yaml-secret-material", "plain-secret-material", "password@example.test", "Bearer secret-token-value", "private prompt"} {
		if strings.Contains(text, value) {
			t.Errorf("archive leaked %q", value)
		}
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatal("expected redaction marker in archive")
	}
}

func TestCreateBundleSkipsSymlinkedLogDirectory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.log"), []byte("private data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "logs")); err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "support.tar.gz")
	if err := CreateBundle(t.Context(), root, archive); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil || hdr.Name != "doctor.json" {
		t.Fatalf("expected only the local doctor report, got member=%v err=%v", hdr, err)
	}
	doctor, err := io.ReadAll(tr)
	if err != nil || !strings.Contains(string(doctor), `"name":"logs_directory","status":"unsafe_path"`) {
		t.Fatalf("expected symlink warning in doctor report: %s err=%v", doctor, err)
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Fatalf("unexpected archive member after doctor report: %v", err)
	}
}

func TestCommandCreatesArchiveWithOutputFlag(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(t.TempDir(), "operator-report.tar.gz")
	var out bytes.Buffer
	cmd := NewCommand(Options{RootDir: root, Stdout: &out})
	cmd.SetArgs([]string{"-o", archive})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("archive not created: %v", err)
	}
	if !strings.Contains(out.String(), archive) {
		t.Fatalf("output=%q", out.String())
	}
}

func TestCreateBundleDoesNotOverwriteExistingOutput(t *testing.T) {
	root, destination := t.TempDir(), filepath.Join(t.TempDir(), "existing.tar.gz")
	if err := os.WriteFile(destination, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CreateBundle(t.Context(), root, destination); err == nil {
		t.Fatal("expected existing destination to be rejected")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("existing destination was changed: %q", got)
	}
}

func TestRootedLogOpenSurvivesDirectoryPathReplacement(t *testing.T) {
	parent := t.TempDir()
	logs := filepath.Join(parent, "logs")
	if err := os.Mkdir(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "recent.log"), []byte("inside-root"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalDir, err := os.Lstat(logs)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(logs)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	expected, err := root.Lstat("recent.log")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "recent.log"), []byte("outside-root"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(logs, filepath.Join(parent, "logs-original")); err == nil {
		if err := os.Symlink(outside, logs); err != nil {
			t.Fatal(err)
		}
	} else if runtime.GOOS != "windows" {
		t.Fatalf("replace opened directory on %s: %v", runtime.GOOS, err)
	} else {
		currentDir, statErr := os.Lstat(logs)
		if statErr != nil || !os.SameFile(originalDir, currentDir) {
			t.Fatalf("Windows prevented replacement but directory identity changed: stat=%v", statErr)
		}
	}
	f, err := openRootedInput(root, "recent.log", expected)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "inside-root" {
		t.Fatalf("read %q after path replacement", got)
	}
}

func TestCreateBundleRejectsOutputMatchingCollectedLog(t *testing.T) {
	root := t.TempDir()
	logs := filepath.Join(root, "logs")
	if err := os.Mkdir(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logs, "recent.log")
	if err := os.WriteFile(logPath, []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CreateBundle(t.Context(), root, logPath); err == nil {
		t.Fatal("expected collected input collision to be rejected")
	}
	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "log" {
		t.Fatalf("input log was changed: %q", got)
	}
}

func TestCollectRecentLogsBoundsScanAndSelectsNewestFiveHundred(t *testing.T) {
	logs := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < maxLogFiles+5; i++ {
		name := filepath.Join(logs, fmt.Sprintf("log-%03d.log", i))
		if err := os.WriteFile(name, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := base.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(name, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(logs)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	files, err := collectRecentLogs(root, "logs")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != maxLogFiles {
		t.Fatalf("got %d log files, want %d", len(files), maxLogFiles)
	}
	for _, file := range files {
		if file.name == filepath.Join("logs", "log-000.log") {
			t.Fatal("oldest log was selected")
		}
	}
	if files[0].name != filepath.Join("logs", "log-504.log") {
		t.Fatalf("newest log=%s", files[0].name)
	}
}
