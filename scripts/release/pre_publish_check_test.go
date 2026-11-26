// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/builtin"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
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

func buildTestReleaseSignatures(t *testing.T, priv ed25519.PrivateKey) ([]builtin.ReleaseSignatureRecord, []byte) {
	t.Helper()
	cat, err := builtin.NewCatalog()
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	pkgs := cat.Packages()
	records := make([]builtin.ReleaseSignatureRecord, 0, len(pkgs))
	for _, pkg := range pkgs {
		payload := installer.SignatureSigningPayload(pkg.PackageID, pkg.Version, pkg.ArchiveSHA256)
		sig := ed25519.Sign(priv, payload)
		records = append(records, builtin.ReleaseSignatureRecord{
			PackageID:     pkg.PackageID,
			Version:       pkg.Version,
			ArchiveSHA256: pkg.ArchiveSHA256,
			RootSignature: base64.StdEncoding.EncodeToString(sig),
		})
	}
	raw, err := json.MarshalIndent(releaseSignaturesManifestDoc{Signatures: records}, "", "  ")
	if err != nil {
		t.Fatalf("marshal release signatures: %v", err)
	}
	return records, raw
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
	relPub, relPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey release root: %v", err)
	}
	records, sigManifestRaw := buildTestReleaseSignatures(t, relPriv)

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
		RootDir:                    root,
		Mode:                       ModePublish,
		TargetVersion:              "v1.0.0",
		ExpectedCommitSHA:          rec.CommitSHA,
		ToolchainVersion:           "go1.26.11",
		ApprovalJSON:               signApprovalForTest(t, priv, rec),
		TrustedPublicKey:           pub,
		ReleaseSignaturesJSON:      sigManifestRaw,
		ReleaseRootPublicKeys:      []ed25519.PublicKey{relPub},
		EmbeddedReleaseRootPubKeys: []ed25519.PublicKey{relPub},
		IsCI:                       false,
		ManifestOutPath:            manifestTarget,
	})
	if err != nil {
		t.Fatalf("expected signed non-CI publish with go1.26.11 and valid release signing to succeed, got %v", err)
	}
	if rep.GateREL07Status != "APPROVED_BY_PRODUCT_OWNER" || rep.ApprovedBy != "nguywnben" || len(rep.Conditions) != 1 {
		t.Fatalf("unexpected approved report fields: %+v", rep)
	}
	if rep.ReleaseSigningStatus != releaseSigningVerified || rep.SignedOfficialPackages != len(records) || rep.TotalOfficialPackages != len(records) {
		t.Fatalf("unexpected release signing fields: %+v", rep)
	}
	if len(rep.Gates) != 8 {
		t.Fatalf("expected 8 gate entries (GATE-REL-01..06 + GATE-REL-04-SIGNING + GATE-REL-07), got %d", len(rep.Gates))
	}
	if _, statErr := os.Stat(manifestTarget); statErr != nil {
		t.Fatalf("expected manifest created in non-existent parent dir: %v", statErr)
	}
}

func TestPrePublishCheckPublishFailsClosedOnMissingOrInvalidReleaseSigning(t *testing.T) {
	root := repoRootForReleaseTest(t)
	poPub, poPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey PO: %v", err)
	}
	relPub, relPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey release root: %v", err)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey other: %v", err)
	}

	rec := HumanApprovalRecord{
		Version:    "v1.0.0",
		CommitSHA:  "c8a2ea1433d6c8e0feb5dbef2248db80ac5933d7",
		Decision:   "GO",
		ApprovedBy: "nguywnben",
		ApprovedAt: "2026-11-26T10:05:00Z",
		TicketRef:  "GATE-REL-07",
		Nonce:      "po-nonce-rel-003",
	}
	signedApproval := signApprovalForTest(t, poPriv, rec)
	records, fullManifest := buildTestReleaseSignatures(t, relPriv)

	marshalRecords := func(recs []builtin.ReleaseSignatureRecord) []byte {
		raw, mErr := json.Marshal(releaseSignaturesManifestDoc{Signatures: recs})
		if mErr != nil {
			t.Fatalf("marshal records: %v", mErr)
		}
		return raw
	}

	partial := marshalRecords(records[:len(records)-1])

	wrongDigest := append([]builtin.ReleaseSignatureRecord(nil), records...)
	wrongDigest[0].ArchiveSHA256 = strings.Repeat("0", 64)

	badSig := append([]builtin.ReleaseSignatureRecord(nil), records...)
	badSig[0].RootSignature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))

	orphan := append([]builtin.ReleaseSignatureRecord(nil), records...)
	orphan = append(orphan, builtin.ReleaseSignatureRecord{
		PackageID:     "@tanpopo/nonexistent",
		Version:       "0.1.0",
		ArchiveSHA256: strings.Repeat("a", 64),
		RootSignature: records[0].RootSignature,
	})

	duplicate := append([]builtin.ReleaseSignatureRecord(nil), records...)
	duplicate = append(duplicate, records[0])

	cases := []struct {
		name      string
		sigs      []byte
		roots     []ed25519.PublicKey
		embedded  []ed25519.PublicKey
		errSubstr string
	}{
		{
			name:      "unsigned repo default fails publish",
			sigs:      nil,
			roots:     nil,
			errSubstr: "missing trusted Ed25519 release root",
		},
		{
			name:      "empty signatures with root key fails publish",
			sigs:      marshalRecords(nil),
			roots:     []ed25519.PublicKey{relPub},
			errSubstr: "missing official package signatures",
		},
		{
			name:      "valid signatures without root key fails publish",
			sigs:      fullManifest,
			roots:     nil,
			errSubstr: "missing trusted Ed25519 release root",
		},
		{
			name:      "incomplete signatures fails publish",
			sigs:      partial,
			roots:     []ed25519.PublicKey{relPub},
			errSubstr: "incomplete official package signature manifest",
		},
		{
			name:      "inconsistent archive digest fails publish",
			sigs:      marshalRecords(wrongDigest),
			roots:     []ed25519.PublicKey{relPub},
			errSubstr: "inconsistent archive digest",
		},
		{
			name:      "invalid signature bytes fails publish",
			sigs:      marshalRecords(badSig),
			roots:     []ed25519.PublicKey{relPub},
			errSubstr: "signature verification failed",
		},
		{
			name:      "wrong trusted release root key fails publish",
			sigs:      fullManifest,
			roots:     []ed25519.PublicKey{otherPub},
			errSubstr: "signature verification failed",
		},
		{
			name:      "orphan signature entry fails publish",
			sigs:      marshalRecords(orphan),
			roots:     []ed25519.PublicKey{relPub},
			errSubstr: "orphan signature record",
		},
		{
			name:      "duplicate signature entry fails publish",
			sigs:      marshalRecords(duplicate),
			roots:     []ed25519.PublicKey{relPub},
			errSubstr: "duplicate signature record",
		},
		{
			name:      "inconsistent embedded vs configured root keys fails publish",
			sigs:      fullManifest,
			roots:     []ed25519.PublicKey{relPub},
			embedded:  []ed25519.PublicKey{otherPub},
			errSubstr: "missing from configured release root keys",
		},
		{
			name:      "malformed JSON fails publish",
			sigs:      []byte(`{"signatures": [{"unknown":1}]}`),
			roots:     []ed25519.PublicKey{relPub},
			errSubstr: "decode release-signatures.json",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep, rErr := RunPrePublishCheck(PrePublishConfig{
				RootDir:                    root,
				Mode:                       ModePublish,
				TargetVersion:              "v1.0.0",
				ExpectedCommitSHA:          rec.CommitSHA,
				ToolchainVersion:           "go1.26.11",
				ApprovalJSON:               signedApproval,
				TrustedPublicKey:           poPub,
				ReleaseSignaturesJSON:      tc.sigs,
				ReleaseRootPublicKeys:      tc.roots,
				EmbeddedReleaseRootPubKeys: tc.embedded,
				IsCI:                       false,
			})
			if rErr == nil || !strings.Contains(rErr.Error(), tc.errSubstr) {
				t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.errSubstr, rErr)
			}
			if rep.GateREL07Status != "BLOCKED_RELEASE_SIGNING_INVALID" {
				t.Fatalf("%s: expected BLOCKED_RELEASE_SIGNING_INVALID, got %q", tc.name, rep.GateREL07Status)
			}
		})
	}

	// Dry-run still rejects partial/corrupt/mismatched release signing even though clean unsigned RCs pass.
	if _, dryErr := RunPrePublishCheck(PrePublishConfig{
		RootDir:               root,
		Mode:                  ModeDryRun,
		TargetVersion:         "v1.0.0",
		ToolchainVersion:      "go1.26.11",
		ReleaseSignaturesJSON: partial,
		IsCI:                  true,
	}); dryErr == nil {
		t.Fatal("dry-run must fail closed on partial release signatures")
	}
}

func TestPrePublishCLI_SignApprovalAndBOMPublishRoundtrip(t *testing.T) {
	root := repoRootForReleaseTest(t)
	poPub, poPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey PO: %v", err)
	}
	relPub, relPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey release root: %v", err)
	}

	tmpDir := t.TempDir()
	poPKCS8, err := x509.MarshalPKCS8PrivateKey(poPriv)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	poKeyFile := filepath.Join(tmpDir, "po.key.pem")
	if err := os.WriteFile(poKeyFile, append([]byte{0xEF, 0xBB, 0xBF}, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: poPKCS8})...), 0o600); err != nil {
		t.Fatalf("WriteFile PO key: %v", err)
	}

	relPKIX, err := x509.MarshalPKIXPublicKey(relPub)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	relPubFile := filepath.Join(tmpDir, "release-root.pub.pem")
	if err := os.WriteFile(relPubFile, append([]byte{0xEF, 0xBB, 0xBF}, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: relPKIX})...), 0o600); err != nil {
		t.Fatalf("WriteFile release pub: %v", err)
	}

	_, sigsRaw := buildTestReleaseSignatures(t, relPriv)
	sigsFile := filepath.Join(tmpDir, "release-signatures.json")
	if err := os.WriteFile(sigsFile, append([]byte{0xEF, 0xBB, 0xBF}, sigsRaw...), 0o600); err != nil {
		t.Fatalf("WriteFile release signatures: %v", err)
	}

	approvalFile := filepath.Join(tmpDir, "gate-rel-07-approval.json")
	commitSHA := "bb92a7280ff20ea7259954cab0d3cac4fd04226f"

	// CI environment cannot execute sign-approval.
	var ciOut, ciErr bytes.Buffer
	ciRC := runPrePublishCLI([]string{
		"--mode", ModeSignApproval,
		"--target-version", "v1.0.0",
		"--commit-sha", commitSHA,
		"--approved-by", "nguywnben",
		"--decision", "GO_WITH_CONDITIONS",
		"--condition", "COND-PO-01",
		"--private-key-file", poKeyFile,
		"--out", approvalFile,
	}, &ciOut, &ciErr, func(k string) string {
		if k == "CI" {
			return "true"
		}
		return ""
	})
	if ciRC != exitCodeApprovalRequired {
		t.Fatalf("expected CI sign-approval to exit %d, got %d (%s)", exitCodeApprovalRequired, ciRC, ciErr.String())
	}

	// Non-CI offline signing succeeds cleanly.
	var signOut, signErr bytes.Buffer
	signRC := runPrePublishCLI([]string{
		"--mode", ModeSignApproval,
		"--target-version", "v1.0.0",
		"--commit-sha", commitSHA,
		"--approved-by", "nguywnben",
		"--decision", "GO_WITH_CONDITIONS",
		"--condition", "COND-PO-01",
		"--private-key-file", poKeyFile,
		"--out", approvalFile,
	}, &signOut, &signErr, func(string) string { return "" })
	if signRC != 0 {
		t.Fatalf("sign-approval failed rc=%d stderr=%s", signRC, signErr.String())
	}

	// CLI dry-run validates BOM-prefixed release signatures, release root PEM, and signed PO approval file.
	var dryOut, dryErr bytes.Buffer
	dryRC := runPrePublishCLI([]string{
		"--root", root,
		"--mode", ModeDryRun,
		"--target-version", "v1.0.0",
		"--commit-sha", commitSHA,
		"--approval-file", approvalFile,
		"--public-key-hex", hex.EncodeToString(poPub),
		"--release-signatures", sigsFile,
		"--release-root-pubkey-file", relPubFile,
		"--output", "json",
	}, &dryOut, &dryErr, func(string) string { return "" })
	if dryRC != 0 {
		t.Fatalf("dry-run signed CLI failed rc=%d stderr=%s stdout=%s", dryRC, dryErr.String(), dryOut.String())
	}
	var dryRep PrePublishReport
	if err := json.Unmarshal(dryOut.Bytes(), &dryRep); err != nil {
		t.Fatalf("unmarshal dry-run report: %v", err)
	}
	if dryRep.ReleaseSigningStatus != releaseSigningVerified || dryRep.GateREL07Status != "DRY_RUN_SIGNATURE_CHECKED_NO_PUBLISH" {
		t.Fatalf("unexpected dry-run signed report status: %+v", dryRep)
	}

	// Publish verification with go1.26.11 unlocks cleanly using the CLI-generated files.
	approvalBytes, err := readBoundedRegularFile(approvalFile, maxApprovalFileBytes)
	if err != nil {
		t.Fatalf("read signed approval: %v", err)
	}
	sigsBytes, err := readBoundedRegularFile(sigsFile, maxSignaturesFileBytes)
	if err != nil {
		t.Fatalf("read sigs file: %v", err)
	}
	loadedRelPub, err := loadEd25519PublicKeyFile(relPubFile)
	if err != nil {
		t.Fatalf("load release root pub: %v", err)
	}
	pubRep, pubErr := RunPrePublishCheck(PrePublishConfig{
		RootDir:               root,
		Mode:                  ModePublish,
		TargetVersion:         "v1.0.0",
		ExpectedCommitSHA:     commitSHA,
		ToolchainVersion:      "go1.26.11",
		ApprovalJSON:          approvalBytes,
		TrustedPublicKey:      poPub,
		ReleaseSignaturesJSON: sigsBytes,
		ReleaseRootPublicKeys: []ed25519.PublicKey{loadedRelPub},
		IsCI:                  false,
	})
	if pubErr != nil || pubRep.GateREL07Status != "APPROVED_BY_PRODUCT_OWNER" || pubRep.ReleaseSigningStatus != releaseSigningVerified {
		t.Fatalf("publish verification failed: rep=%+v err=%v", pubRep, pubErr)
	}
}
