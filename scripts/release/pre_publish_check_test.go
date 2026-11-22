// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootForReleaseTest(t *testing.T) string {
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

func signApprovalForTest(t *testing.T, priv ed25519.PrivateKey, rec HumanApprovalRecord) []byte {
	t.Helper()
	sig := ed25519.Sign(priv, CanonicalApprovalPayload(rec))
	rec.SignatureHex = hex.EncodeToString(sig)
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal approval record: %v", err)
	}
	return raw
}

func TestPrePublishCheckDryRunPassesGates01To06AndNeverMarksREL07Pass(t *testing.T) {
	root := repoRootForReleaseTest(t)
	manifestPath := filepath.Join(t.TempDir(), "nested-dist", "SHA256SUMS")

	report, err := RunPrePublishCheck(PrePublishConfig{
		RootDir:          root,
		Mode:             ModeDryRun,
		TargetVersion:    "v1.0.0",
		ToolchainVersion: "go1.26.11",
		IsCI:             true,
		ManifestOutPath:  manifestPath,
	})
	if err != nil {
		t.Fatalf("dry-run expected zero error, got %v", err)
	}
	if report.GateREL07Status != "BLOCKED_PENDING_HUMAN_SIGN_OFF" {
		t.Fatalf("expected BLOCKED_PENDING_HUMAN_SIGN_OFF, got %q", report.GateREL07Status)
	}
	if report.ApprovalError == nil || report.ApprovalError.Status != 403 || report.ApprovalError.ErrorCode != "human_release_approval_required" {
		t.Fatalf("expected dry-run GATE-REL-07 block error 403 human_release_approval_required, got %+v", report.ApprovalError)
	}
	if len(report.Manifest) < 20 {
		t.Fatalf("expected >= 20 manifest entries, got %d", len(report.Manifest))
	}
	sumRaw, err := os.ReadFile(manifestPath)
	if err != nil || !strings.Contains(string(sumRaw), "docs/releases/v1.0.0-readiness-dossier.md") {
		t.Fatalf("expected SHA256SUMS manifest written inside created nested dir, err=%v", err)
	}

	// Even when given a valid non-CI signature, dry-run must keep GATE-REL-07 GateResult.Status == "BLOCKED".
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	rec := HumanApprovalRecord{
		Version:    "v1.0.0",
		CommitSHA:  "c8a2ea1433d6c8e0feb5dbef2248db80ac5933d7",
		Decision:   "GO_WITH_CONDITIONS",
		ApprovedBy: "nguywnben",
		ApprovedAt: "2026-11-22T10:00:00Z",
		TicketRef:  "GATE-REL-07",
		Conditions: []string{"COND-PO-01"},
		Nonce:      "po-dry-001",
	}
	drySignedRep, err := RunPrePublishCheck(PrePublishConfig{
		RootDir:           root,
		Mode:              ModeDryRun,
		TargetVersion:     "v1.0.0",
		ExpectedCommitSHA: rec.CommitSHA,
		ToolchainVersion:  "go1.26.11",
		ApprovalJSON:      signApprovalForTest(t, priv, rec),
		TrustedPublicKey:  pub,
		IsCI:              false,
	})
	if err != nil {
		t.Fatalf("dry-run signed check err: %v", err)
	}
	lastGate := drySignedRep.Gates[len(drySignedRep.Gates)-1]
	if lastGate.GateID != "GATE-REL-07" || lastGate.Status != "BLOCKED" {
		t.Fatalf("dry-run MUST keep GATE-REL-07 GateResult.Status == BLOCKED, got %+v", lastGate)
	}
}

func TestPrePublishCheckNEG1FailClosedWithoutHumanApproval(t *testing.T) {
	root := repoRootForReleaseTest(t)
	report, err := RunPrePublishCheck(PrePublishConfig{
		RootDir:           root,
		Mode:              ModePublish,
		TargetVersion:     "v1.0.0",
		ExpectedCommitSHA: "c8a2ea1433d6c8e0feb5dbef2248db80ac5933d7",
		ToolchainVersion:  "go1.26.11",
		IsCI:              false,
	})
	var appErr *ApprovalError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *ApprovalError, got err=%v", err)
	}
	if appErr.Status != 403 || appErr.ErrorCode != "human_release_approval_required" {
		t.Fatalf("unexpected approval error: %+v", appErr)
	}
	if report.GateREL07Status != "BLOCKED_HUMAN_APPROVAL_REQUIRED" {
		t.Fatalf("unexpected status: %q", report.GateREL07Status)
	}

	var stdout, stderr bytes.Buffer
	exitCode := runPrePublishCLI([]string{"--root", root, "--output", "json"}, &stdout, &stderr, func(string) string { return "" })
	if exitCode != exitCodeApprovalRequired {
		t.Fatalf("expected CLI exit code %d when unapproved, got %d (stderr=%s)", exitCodeApprovalRequired, exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "human_release_approval_required") {
		t.Fatalf("expected human_release_approval_required in stderr, got %s", stderr.String())
	}
}

func TestPrePublishCheckNEG1RejectsTamperingReplayCIAndEdgeCases(t *testing.T) {
	root := repoRootForReleaseTest(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	baseRec := HumanApprovalRecord{
		Version:    "v1.0.0",
		CommitSHA:  "c8a2ea1433d6c8e0feb5dbef2248db80ac5933d7",
		Decision:   "GO_WITH_CONDITIONS",
		ApprovedBy: "nguywnben",
		ApprovedAt: "2026-11-22T10:00:00Z",
		TicketRef:  "GATE-REL-07",
		Conditions: []string{"COND-PO-01"},
		Nonce:      "po-nonce-001",
	}
	signedValid := signApprovalForTest(t, priv, baseRec)

	var appErr *ApprovalError

	// 1. Automated CI environments MUST fail closed even with a valid signature.
	for _, envCase := range []map[string]string{
		{"CI": "1"},
		{"GITHUB_ACTIONS": "true"},
		{"GITHUB_RUN_ID": "4500999999"},
	} {
		getenv := func(k string) string { return envCase[k] }
		if !detectCIEnvironment(getenv) {
			t.Fatalf("detectCIEnvironment failed to detect %+v", envCase)
		}
	}
	_, ciErr := RunPrePublishCheck(PrePublishConfig{
		RootDir:           root,
		Mode:              ModePublish,
		TargetVersion:     "v1.0.0",
		ExpectedCommitSHA: baseRec.CommitSHA,
		ToolchainVersion:  "go1.26.11",
		ApprovalJSON:      signedValid,
		TrustedPublicKey:  pub,
		IsCI:              true,
	})
	if !errors.As(ciErr, &appErr) || appErr.Status != 403 || appErr.ErrorCode != "human_release_approval_required" {
		t.Fatalf("CI automation MUST be blocked even with signed approval; got %v", ciErr)
	}

	// 2. Omitted ExpectedCommitSHA in ModePublish must fail closed (anti-replay).
	_, noSHAErr := RunPrePublishCheck(PrePublishConfig{
		RootDir:           root,
		Mode:              ModePublish,
		TargetVersion:     "v1.0.0",
		ExpectedCommitSHA: "",
		ToolchainVersion:  "go1.26.11",
		ApprovalJSON:      signedValid,
		TrustedPublicKey:  pub,
		IsCI:              false,
	})
	if !errors.As(noSHAErr, &appErr) || appErr.Status != 403 {
		t.Fatalf("omitted ExpectedCommitSHA in publish mode must fail closed, got %v", noSHAErr)
	}

	// 3. Conditions array tampering must invalidate signature.
	tamperedConds := strings.Replace(string(signedValid), `"COND-PO-01"`, `"WAIVED_ALL"`, 1)
	_, condErr := RunPrePublishCheck(PrePublishConfig{
		RootDir:           root,
		Mode:              ModePublish,
		TargetVersion:     "v1.0.0",
		ExpectedCommitSHA: baseRec.CommitSHA,
		ToolchainVersion:  "go1.26.11",
		ApprovalJSON:      []byte(tamperedConds),
		TrustedPublicKey:  pub,
		IsCI:              false,
	})
	if !errors.As(condErr, &appErr) || appErr.Status != 403 {
		t.Fatalf("tampered Conditions must fail signature verification, got %v", condErr)
	}

	// 4. Trailing JSON tokens & unknown JSON fields must fail closed.
	_, trailingErr := RunPrePublishCheck(PrePublishConfig{
		RootDir:           root,
		Mode:              ModePublish,
		TargetVersion:     "v1.0.0",
		ExpectedCommitSHA: baseRec.CommitSHA,
		ToolchainVersion:  "go1.26.11",
		ApprovalJSON:      append(append([]byte{}, signedValid...), []byte(`{"extra":1}`)...),
		TrustedPublicKey:  pub,
		IsCI:              false,
	})
	if !errors.As(trailingErr, &appErr) || appErr.Status != 403 {
		t.Fatalf("trailing JSON bytes must fail closed, got %v", trailingErr)
	}

	unknownJSON := strings.Replace(string(signedValid), `"nonce"`, `"evil":1,"nonce"`, 1)
	_, unknownErr := RunPrePublishCheck(PrePublishConfig{
		RootDir:           root,
		Mode:              ModePublish,
		TargetVersion:     "v1.0.0",
		ExpectedCommitSHA: baseRec.CommitSHA,
		ToolchainVersion:  "go1.26.11",
		ApprovalJSON:      []byte(unknownJSON),
		TrustedPublicKey:  pub,
		IsCI:              false,
	})
	if !errors.As(unknownErr, &appErr) || appErr.Status != 403 {
		t.Fatalf("unknown JSON fields must fail closed, got %v", unknownErr)
	}

	// 5. NO-GO, version mismatch, bad ticket_ref, bad timestamp, control byte, non-40-hex SHA, mismatched SHA.
	badCases := []struct {
		name   string
		mutate func(*HumanApprovalRecord)
	}{
		{"NO-GO decision", func(r *HumanApprovalRecord) { r.Decision = "NO-GO" }},
		{"wrong version", func(r *HumanApprovalRecord) { r.Version = "v0.9.0" }},
		{"wrong ticket_ref", func(r *HumanApprovalRecord) { r.TicketRef = "GATE-REL-06" }},
		{"bad timestamp", func(r *HumanApprovalRecord) { r.ApprovedAt = "not-a-timestamp" }},
		{"empty conditions on GO_WITH_CONDITIONS", func(r *HumanApprovalRecord) { r.Conditions = nil }},
		{"control byte in nonce", func(r *HumanApprovalRecord) { r.Nonce = "bad\x00nonce" }},
		{"non-40-hex commit_sha", func(r *HumanApprovalRecord) { r.CommitSHA = "deadbeef" }},
	}
	for _, bc := range badCases {
		mut := baseRec
		bc.mutate(&mut)
		_, bErr := RunPrePublishCheck(PrePublishConfig{
			RootDir:           root,
			Mode:              ModePublish,
			TargetVersion:     "v1.0.0",
			ExpectedCommitSHA: baseRec.CommitSHA,
			ToolchainVersion:  "go1.26.11",
			ApprovalJSON:      signApprovalForTest(t, priv, mut),
			TrustedPublicKey:  pub,
			IsCI:              false,
		})
		if !errors.As(bErr, &appErr) || appErr.Status != 403 {
			t.Fatalf("%s must fail closed 403, got %v", bc.name, bErr)
		}
	}

	// 6. Symlink --approval-file rejected in CLI.
	tmpDir := t.TempDir()
	realApproval := filepath.Join(tmpDir, "real.json")
	if err := os.WriteFile(realApproval, signedValid, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	symApproval := filepath.Join(tmpDir, "sym.json")
	if err := os.Symlink(realApproval, symApproval); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	var outBuf, errBuf bytes.Buffer
	rc := runPrePublishCLI([]string{
		"--root", root,
		"--commit-sha", baseRec.CommitSHA,
		"--approval-file", symApproval,
		"--public-key-hex", hex.EncodeToString(pub),
	}, &outBuf, &errBuf, func(string) string { return "" })
	if rc != exitCodeApprovalRequired {
		t.Fatalf("symlink --approval-file must exit %d, got %d (stderr=%s)", exitCodeApprovalRequired, rc, errBuf.String())
	}

	// 7. Insecure Go toolchain (< go1.26.11) blocked in publish mode.
	_, tcErr := RunPrePublishCheck(PrePublishConfig{
		RootDir:           root,
		Mode:              ModePublish,
		TargetVersion:     "v1.0.0",
		ExpectedCommitSHA: baseRec.CommitSHA,
		ToolchainVersion:  "go1.26.0",
		ApprovalJSON:      signedValid,
		TrustedPublicKey:  pub,
		IsCI:              false,
	})
	if tcErr == nil || !strings.Contains(tcErr.Error(), "1.26.11") {
		t.Fatalf("expected < go1.26.11 toolchain rejection in publish mode, got %v", tcErr)
	}
}

func TestPrePublishCheckInteractiveHumanSignOffUnlocksPublish(t *testing.T) {
	root := repoRootForReleaseTest(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	rec := HumanApprovalRecord{
		Version:    "v1.0.0",
		CommitSHA:  "c8a2ea1433d6c8e0feb5dbef2248db80ac5933d7",
		Decision:   "GO_WITH_CONDITIONS",
		ApprovedBy: "nguywnben",
		ApprovedAt: "2026-11-22T10:05:00Z",
		TicketRef:  "GATE-REL-07",
		Conditions: []string{"SQLite single-node ceiling disclosed"},
		Nonce:      "po-nonce-002",
	}
	tmpDir := t.TempDir()
	approvalPath := filepath.Join(tmpDir, "gate-rel-07-approval.json")
	if err := os.WriteFile(approvalPath, signApprovalForTest(t, priv, rec), 0o600); err != nil {
		t.Fatalf("WriteFile approval: %v", err)
	}
	manifestTarget := filepath.Join(tmpDir, "dist", "GOVERNANCE_SHA256SUMS")

	rep, err := RunPrePublishCheck(PrePublishConfig{
		RootDir:           root,
		Mode:              ModePublish,
		TargetVersion:     "v1.0.0",
		ExpectedCommitSHA: rec.CommitSHA,
		ToolchainVersion:  "go1.26.11",
		ApprovalJSON:      signApprovalForTest(t, priv, rec),
		TrustedPublicKey:  pub,
		IsCI:              false,
		ManifestOutPath:   manifestTarget,
	})
	if err != nil {
		t.Fatalf("expected signed non-CI publish with go1.26.11 to succeed, got %v", err)
	}
	if rep.GateREL07Status != "APPROVED_BY_PRODUCT_OWNER" || rep.ApprovedBy != "nguywnben" || len(rep.Conditions) != 1 {
		t.Fatalf("unexpected approved report fields: %+v", rep)
	}
	if len(rep.Gates) != 7 {
		t.Fatalf("expected all 7 GATE-REL-01..07 entries, got %d", len(rep.Gates))
	}
	if _, statErr := os.Stat(manifestTarget); statErr != nil {
		t.Fatalf("expected manifest created in non-existent parent dir: %v", statErr)
	}
}
