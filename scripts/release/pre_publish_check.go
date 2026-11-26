// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Command pre_publish_check verifies GATE-REL-01..06 release readiness
// prerequisites aligned to SPEC-QA-04, generates a deterministic SHA-256
// release input manifest confined to repository root, and enforces the
// fail-closed GATE-REL-07 human Product Owner approval gate (HTTP 403
// human_release_approval_required, NEG-1) that can never be unlocked by
// automated CI runners.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/builtin"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/security/trust"
)

const (
	// ModePublish enforces full GATE-REL-01..07 verification including human PO sign-off and release signing.
	ModePublish = "publish"
	// ModeDryRun verifies GATE-REL-01..06 and generates checksums while keeping GATE-REL-07 blocked.
	ModeDryRun = "dry-run"
	// ModeSignApproval creates an offline Ed25519-signed GATE-REL-07 PO approval record using a caller-supplied private key file.
	ModeSignApproval = "sign-approval"

	exitCodeApprovalRequired = 4
	approvalDomainPrefix     = "tanpopo:gate-rel-07:v1\x00"
	defaultTargetVersion     = "v1.0.0"
	maxApprovalFileBytes     = 64 * 1024
	maxSignaturesFileBytes   = 1024 * 1024
	maxPubKeyFileBytes       = 16 * 1024
	minGoMajor               = 1
	minGoMinor               = 26
	minGoPatch               = 11

	releaseSigningVerified        = "SIGNED_VERIFIED"
	releaseSigningUnsignedRC      = "UNSIGNED_INTERNAL_RC"
	releaseSigningInvalidOrMissing = "INVALID_OR_MISSING"
)

var commitSHA40Pattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// ApprovalError models a fail-closed 403 human_release_approval_required gate rejection (NEG-1).
type ApprovalError struct {
	Status    int    `json:"status"`
	ErrorCode string `json:"error_code"`
	Reason    string `json:"reason"`
}

func (e *ApprovalError) Error() string {
	return fmt.Sprintf("HTTP %d %s: %s", e.Status, e.ErrorCode, e.Reason)
}

// GateResult summarizes verification status for a single GATE-REL-xx gate.
type GateResult struct {
	GateID  string   `json:"gate_id"`
	Name    string   `json:"name"`
	Status  string   `json:"status"` // PASS, BLOCKED, FAIL
	Details []string `json:"details"`
}

// ArtifactChecksum records a deterministic SHA-256 digest of a release input artifact.
type ArtifactChecksum struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// HumanApprovalRecord captures an Ed25519-signed human Product Owner GATE-REL-07 decision.
type HumanApprovalRecord struct {
	Version      string   `json:"version"`
	CommitSHA    string   `json:"commit_sha"`
	Decision     string   `json:"decision"` // GO or GO_WITH_CONDITIONS
	ApprovedBy   string   `json:"approved_by"`
	ApprovedAt   string   `json:"approved_at"`
	TicketRef    string   `json:"ticket_ref"`
	Conditions   []string `json:"conditions,omitempty"`
	Nonce        string   `json:"nonce"`
	SignatureHex string   `json:"signature_hex"`
}

// PrePublishConfig configures RunPrePublishCheck.
type PrePublishConfig struct {
	RootDir                    string
	Mode                       string // "publish" or "dry-run"
	TargetVersion              string
	ExpectedCommitSHA          string
	ToolchainVersion           string // defaults to runtime.Version()
	ApprovalJSON               []byte
	TrustedPublicKey           ed25519.PublicKey
	ReleaseSignaturesJSON      []byte
	ReleaseRootPublicKeys      []ed25519.PublicKey
	EmbeddedReleaseRootPubKeys []ed25519.PublicKey
	IsCI                       bool
	ManifestOutPath            string
}

// PrePublishReport captures the full GATE-REL-01..07 output and manifest.
type PrePublishReport struct {
	TargetVersion              string             `json:"target_version"`
	Mode                       string             `json:"mode"`
	IsCI                       bool               `json:"is_ci"`
	ToolchainVersion           string             `json:"toolchain_version"`
	ReleaseSigningStatus       string             `json:"release_signing_status"`
	SignedOfficialPackages     int                `json:"signed_official_packages"`
	TotalOfficialPackages      int                `json:"total_official_packages"`
	ReleaseRootKeyFingerprints []string           `json:"release_root_key_fingerprints,omitempty"`
	Gates                      []GateResult       `json:"gates"`
	GateREL07Status            string             `json:"gate_rel_07_status"`
	ApprovalError              *ApprovalError     `json:"approval_error,omitempty"`
	ApprovedBy                 string             `json:"approved_by,omitempty"`
	ApprovedAt                 string             `json:"approved_at,omitempty"`
	Decision                   string             `json:"decision,omitempty"`
	Conditions                 []string           `json:"conditions,omitempty"`
	Manifest                   []ArtifactChecksum `json:"manifest"`
}

func main() {
	os.Exit(runPrePublishCLI(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func detectCIEnvironment(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	isTruthy := func(v string) bool {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes":
			return true
		default:
			return false
		}
	}
	if isTruthy(getenv("CI")) ||
		isTruthy(getenv("GITHUB_ACTIONS")) ||
		isTruthy(getenv("GITLAB_CI")) ||
		isTruthy(getenv("CIRCLECI")) ||
		isTruthy(getenv("BUILDKITE")) ||
		isTruthy(getenv("TF_BUILD")) {
		return true
	}
	for _, nonEmptyKey := range []string{"GITHUB_RUN_ID", "GITHUB_WORKFLOW", "RUNNER_OS", "JENKINS_URL"} {
		if strings.TrimSpace(getenv(nonEmptyKey)) != "" {
			return true
		}
	}
	return false
}

func readBoundedRegularFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlinks are prohibited: %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected regular file: %s", path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file %s exceeds maximum allowed size (%d bytes)", path, maxBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("file %s exceeds maximum allowed size (%d bytes)", path, maxBytes)
	}
	return raw, nil
}

func emitCLIResult(stdout, stderr io.Writer, outputFmt string, report *PrePublishReport, err error) int {
	if strings.EqualFold(strings.TrimSpace(outputFmt), "json") {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else if report != nil {
		printTextReport(stdout, report)
	}

	if err != nil {
		var appErr *ApprovalError
		if errors.As(err, &appErr) {
			fmt.Fprintf(stderr, "BLOCKED: %s\n", appErr.Error())
			return exitCodeApprovalRequired
		}
		fmt.Fprintf(stderr, "FAIL: pre_publish_check: %v\n", err)
		return 1
	}

	if report != nil && report.Mode == ModeDryRun {
		fmt.Fprintf(stderr, "DRY-RUN ONLY: GATE-REL-07 remains %s; exit 0 does NOT authorize release or tag creation.\n", report.GateREL07Status)
	}
	return 0
}

type stringListFlag []string

func (s *stringListFlag) String() string { return strings.Join(*s, ",") }
func (s *stringListFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func runPrePublishCLI(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fsFlags := flag.NewFlagSet("pre_publish_check", flag.ContinueOnError)
	fsFlags.SetOutput(stderr)

	rootFlag := fsFlags.String("root", ".", "Repository root directory")
	modeFlag := fsFlags.String("mode", ModePublish, "Execution mode: publish (default, fail-closed GATE-REL-07), dry-run, or sign-approval")
	versionFlag := fsFlags.String("target-version", defaultTargetVersion, "Target release version (e.g., v1.0.0)")
	commitFlag := fsFlags.String("commit-sha", "", "40-hex target commit SHA (required in publish and sign-approval modes)")
	approvalFile := fsFlags.String("approval-file", "", "Path to Ed25519-signed GATE-REL-07 PO approval JSON")
	pubKeyHexFlag := fsFlags.String("public-key-hex", "", "Hex-encoded Ed25519 public key for verifying PO sign-off")
	releaseSigsFile := fsFlags.String("release-signatures", "", "Optional path to release-signatures.json (defaults to extensions/official/release-signatures.json under --root)")
	releaseRootHexFlag := fsFlags.String("release-root-pubkey-hex", "", "Comma-separated 64-hex Ed25519 release root public key(s) for verifying official packages")
	releaseRootFileFlag := fsFlags.String("release-root-pubkey-file", "", "Path to Ed25519 release root public key file (PKIX PEM, 64-hex, or base64)")
	privKeyFile := fsFlags.String("private-key-file", "", "Offline Ed25519 private key file (PKCS8 PEM, hex, or base64) for --mode sign-approval only")
	approvedByFlag := fsFlags.String("approved-by", "", "Product Owner identifier for --mode sign-approval")
	decisionFlag := fsFlags.String("decision", "GO_WITH_CONDITIONS", "Approval decision (GO or GO_WITH_CONDITIONS) for --mode sign-approval")
	approvedAtFlag := fsFlags.String("approved-at", "", "Optional RFC3339 approval timestamp for --mode sign-approval (defaults to current UTC time)")
	nonceFlag := fsFlags.String("nonce", "", "Unique approval nonce for --mode sign-approval (generated cryptographically if omitted)")
	var conditionFlags stringListFlag
	fsFlags.Var(&conditionFlags, "condition", "Release condition entry for --mode sign-approval GO_WITH_CONDITIONS (repeatable)")
	signOutFlag := fsFlags.String("out", "gate-rel-07-approval.json", "Output JSON path for --mode sign-approval")
	manifestOut := fsFlags.String("write-manifest", "", "Optional output path for SHA256SUMS manifest")
	outputFmt := fsFlags.String("output", "text", "Output format: text or json")

	if err := fsFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	absRoot, err := filepath.Abs(*rootFlag)
	if err != nil {
		fmt.Fprintf(stderr, "pre_publish_check: resolve root: %v\n", err)
		return 2
	}

	mode := strings.TrimSpace(*modeFlag)
	targetVer := strings.TrimSpace(*versionFlag)
	isCI := detectCIEnvironment(getenv)

	makeCLIApprovalError := func(reason string) int {
		appErr := &ApprovalError{
			Status:    403,
			ErrorCode: "human_release_approval_required",
			Reason:    reason,
		}
		rep := &PrePublishReport{
			TargetVersion:    targetVer,
			Mode:             mode,
			IsCI:             isCI,
			ToolchainVersion: runtime.Version(),
			GateREL07Status:  "BLOCKED_HUMAN_APPROVAL_REQUIRED",
			ApprovalError:    appErr,
		}
		return emitCLIResult(stdout, stderr, *outputFmt, rep, appErr)
	}

	if mode == ModeSignApproval {
		if isCI {
			return makeCLIApprovalError("automated CI environments are permanently blocked from signing GATE-REL-07 approval records")
		}
		if err := runSignApprovalCLI(
			targetVer,
			strings.TrimSpace(*commitFlag),
			strings.TrimSpace(*decisionFlag),
			strings.TrimSpace(*approvedByFlag),
			strings.TrimSpace(*approvedAtFlag),
			conditionFlags,
			strings.TrimSpace(*nonceFlag),
			strings.TrimSpace(*privKeyFile),
			strings.TrimSpace(*signOutFlag),
			stdout,
		); err != nil {
			fmt.Fprintf(stderr, "FAIL: pre_publish_check sign-approval: %v\n", err)
			return 1
		}
		return 0
	}

	var approvalJSON []byte
	approvalPath := strings.TrimSpace(*approvalFile)
	if approvalPath == "" && getenv != nil {
		approvalPath = strings.TrimSpace(getenv("TANPOPO_PO_RELEASE_APPROVAL_FILE"))
	}
	if approvalPath != "" {
		raw, readErr := readBoundedRegularFile(approvalPath, maxApprovalFileBytes)
		if readErr != nil {
			return makeCLIApprovalError(fmt.Sprintf("read approval file %s: %v", approvalPath, readErr))
		}
		approvalJSON = raw
	} else if getenv != nil {
		if inline := strings.TrimSpace(getenv("TANPOPO_PO_RELEASE_APPROVAL_JSON")); inline != "" {
			if len(inline) > maxApprovalFileBytes {
				return makeCLIApprovalError("inline approval JSON exceeds maximum allowed size")
			}
			approvalJSON = []byte(inline)
		}
	}

	var pubKey ed25519.PublicKey
	pubHex := strings.TrimSpace(*pubKeyHexFlag)
	if pubHex == "" && getenv != nil {
		pubHex = strings.TrimSpace(getenv("TANPOPO_PO_APPROVAL_PUBKEY_HEX"))
	}
	if pubHex != "" {
		rawPub, decErr := hex.DecodeString(pubHex)
		if decErr != nil || len(rawPub) != ed25519.PublicKeySize {
			return makeCLIApprovalError(fmt.Sprintf("invalid public key hex (expected %d bytes)", ed25519.PublicKeySize))
		}
		pubKey = ed25519.PublicKey(rawPub)
	}

	var releaseSigsJSON []byte
	sigsPath := strings.TrimSpace(*releaseSigsFile)
	if sigsPath == "" && getenv != nil {
		sigsPath = strings.TrimSpace(getenv("TANPOPO_RELEASE_SIGNATURES_PATH"))
	}
	if sigsPath != "" {
		raw, readErr := readBoundedRegularFile(sigsPath, maxSignaturesFileBytes)
		if readErr != nil {
			fmt.Fprintf(stderr, "FAIL: pre_publish_check: read release signatures %s: %v\n", sigsPath, readErr)
			return 1
		}
		releaseSigsJSON = raw
	}

	var releaseRootKeys []ed25519.PublicKey
	rootHex := strings.TrimSpace(*releaseRootHexFlag)
	if rootHex == "" && getenv != nil {
		rootHex = strings.TrimSpace(getenv("TANPOPO_RELEASE_ROOT_PUBKEYS"))
	}
	if rootHex != "" {
		parsed, pErr := parseEd25519PublicKeysHex(rootHex)
		if pErr != nil {
			fmt.Fprintf(stderr, "FAIL: pre_publish_check: invalid release root public key hex: %v\n", pErr)
			return 1
		}
		releaseRootKeys = append(releaseRootKeys, parsed...)
	}

	rootFile := strings.TrimSpace(*releaseRootFileFlag)
	if rootFile == "" && getenv != nil {
		rootFile = strings.TrimSpace(getenv("TANPOPO_TRUST_ROOT_PUB"))
	}
	if rootFile != "" {
		parsed, pErr := loadEd25519PublicKeyFile(rootFile)
		if pErr != nil {
			fmt.Fprintf(stderr, "FAIL: pre_publish_check: load release root public key %s: %v\n", rootFile, pErr)
			return 1
		}
		releaseRootKeys = append(releaseRootKeys, parsed)
	}

	var embeddedRootKeys []ed25519.PublicKey
	if strings.TrimSpace(builtin.ReleaseRootPublicKeysHex) != "" {
		parsed, pErr := parseEd25519PublicKeysHex(builtin.ReleaseRootPublicKeysHex)
		if pErr != nil {
			fmt.Fprintf(stderr, "FAIL: pre_publish_check: invalid embedded ReleaseRootPublicKeysHex: %v\n", pErr)
			return 1
		}
		embeddedRootKeys = append(embeddedRootKeys, parsed...)
	}

	report, runErr := RunPrePublishCheck(PrePublishConfig{
		RootDir:                    absRoot,
		Mode:                       mode,
		TargetVersion:              targetVer,
		ExpectedCommitSHA:          strings.TrimSpace(*commitFlag),
		ToolchainVersion:           runtime.Version(),
		ApprovalJSON:               approvalJSON,
		TrustedPublicKey:           pubKey,
		ReleaseSignaturesJSON:      releaseSigsJSON,
		ReleaseRootPublicKeys:      releaseRootKeys,
		EmbeddedReleaseRootPubKeys: embeddedRootKeys,
		IsCI:                       isCI,
		ManifestOutPath:            strings.TrimSpace(*manifestOut),
	})
	return emitCLIResult(stdout, stderr, *outputFmt, report, runErr)
}

func printTextReport(w io.Writer, r *PrePublishReport) {
	fmt.Fprintf(w, "Tanpopo Pre-Publish Release Check (version=%s mode=%s is_ci=%t toolchain=%s)\n", r.TargetVersion, r.Mode, r.IsCI, r.ToolchainVersion)
	for _, g := range r.Gates {
		fmt.Fprintf(w, "  [%s] %s — %s\n", g.Status, g.GateID, g.Name)
		for _, d := range g.Details {
			fmt.Fprintf(w, "      - %s\n", d)
		}
	}
	if r.ReleaseSigningStatus != "" {
		fmt.Fprintf(w, "  Release Signing Status: %s (%d/%d official packages)\n", r.ReleaseSigningStatus, r.SignedOfficialPackages, r.TotalOfficialPackages)
	}
	fmt.Fprintf(w, "  GATE-REL-07 Status: %s\n", r.GateREL07Status)
	if r.ApprovalError != nil {
		fmt.Fprintf(w, "  GATE-REL-07 Enforcement: HTTP %d %s (%s)\n", r.ApprovalError.Status, r.ApprovalError.ErrorCode, r.ApprovalError.Reason)
	} else if r.ApprovedBy != "" {
		fmt.Fprintf(w, "  Approved By: %s (%s) at %s\n", r.ApprovedBy, r.Decision, r.ApprovedAt)
	}
	fmt.Fprintf(w, "  SHA-256 Manifest Entries: %d\n", len(r.Manifest))
}

// CheckMinGoToolchainVersion enforces >= go1.26.11 (eliminating 41 Go <=1.26.0 stdlib CVEs).
func CheckMinGoToolchainVersion(version string) error {
	trimmed := strings.TrimSpace(version)
	trimmed = strings.TrimPrefix(trimmed, "go")
	parts := strings.SplitN(trimmed, ".", 3)
	if len(parts) < 3 {
		return fmt.Errorf("unrecognized Go toolchain version %q (expected >= go%d.%d.%d)", version, minGoMajor, minGoMinor, minGoPatch)
	}
	patchPart := parts[2]
	for idx, ch := range patchPart {
		if ch < '0' || ch > '9' {
			patchPart = patchPart[:idx]
			break
		}
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	pat, err3 := strconv.Atoi(patchPart)
	if err1 != nil || err2 != nil || err3 != nil {
		return fmt.Errorf("parse Go toolchain version %q failed", version)
	}
	if maj > minGoMajor || (maj == minGoMajor && min > minGoMinor) || (maj == minGoMajor && min == minGoMinor && pat >= minGoPatch) {
		return nil
	}
	return fmt.Errorf("Go toolchain %q is below mandatory release minimum go%d.%d.%d", version, minGoMajor, minGoMinor, minGoPatch)
}

func writeManifestFileSafely(path string, content []byte) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to overwrite symlink or non-regular manifest target: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create manifest parent directory %s: %w", parent, err)
	}
	if pInfo, err := os.Lstat(parent); err == nil && pInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("manifest parent directory cannot be a symlink: %s", parent)
	}
	return os.WriteFile(path, content, 0o600)
}

// RunPrePublishCheck validates GATE-REL-01..06, builds a deterministic SHA-256
// release manifest confined to RootDir, and enforces GATE-REL-07.
func RunPrePublishCheck(cfg PrePublishConfig) (*PrePublishReport, error) {
	if cfg.Mode == "" {
		cfg.Mode = ModePublish
	}
	if cfg.TargetVersion == "" {
		cfg.TargetVersion = defaultTargetVersion
	}
	if cfg.ToolchainVersion == "" {
		cfg.ToolchainVersion = runtime.Version()
	}

	report := &PrePublishReport{
		TargetVersion:    cfg.TargetVersion,
		Mode:             cfg.Mode,
		IsCI:             cfg.IsCI,
		ToolchainVersion: cfg.ToolchainVersion,
	}

	if cfg.Mode != ModePublish && cfg.Mode != ModeDryRun {
		report.GateREL07Status = "BLOCKED_INVALID_MODE"
		return report, fmt.Errorf("invalid mode %q: expected %q or %q", cfg.Mode, ModePublish, ModeDryRun)
	}

	rootFS, err := os.OpenRoot(cfg.RootDir)
	if err != nil {
		report.GateREL07Status = "BLOCKED_INVALID_ROOT"
		return report, fmt.Errorf("open repository root %s: %w", cfg.RootDir, err)
	}
	defer func() { _ = rootFS.Close() }()

	gates, gateErr := evaluateGates01To06(rootFS, cfg.ToolchainVersion)
	report.Gates = append(report.Gates, gates...)

	manifest, manifestErr := buildArtifactManifest(rootFS)
	if manifestErr != nil {
		report.GateREL07Status = "BLOCKED_MANIFEST_ERROR"
		return report, manifestErr
	}
	report.Manifest = manifest

	if cfg.ManifestOutPath != "" {
		var buf bytes.Buffer
		for _, item := range manifest {
			fmt.Fprintf(&buf, "%s  %s\n", item.SHA256, item.Path)
		}
		if err := writeManifestFileSafely(cfg.ManifestOutPath, buf.Bytes()); err != nil {
			report.GateREL07Status = "BLOCKED_MANIFEST_WRITE_ERROR"
			return report, fmt.Errorf("write SHA256SUMS manifest %s: %w", cfg.ManifestOutPath, err)
		}
	}

	if gateErr != nil {
		report.GateREL07Status = "BLOCKED_PREREQUISITE_GATE_FAILED"
		return report, gateErr
	}

	relSummary, relErr := evaluateReleaseSigning(cfg, rootFS)
	report.ReleaseSigningStatus = relSummary.status
	report.SignedOfficialPackages = relSummary.signedCount
	report.TotalOfficialPackages = relSummary.totalCount
	report.ReleaseRootKeyFingerprints = relSummary.rootFingerprints

	if cfg.Mode == ModeDryRun {
		if relErr != nil {
			report.GateREL07Status = "BLOCKED_RELEASE_SIGNING_INVALID"
			report.Gates = append(report.Gates, GateResult{
				GateID:  "GATE-REL-04-SIGNING",
				Name:    "Official Release Signing & Trust Root Parity",
				Status:  "FAIL",
				Details: []string{relErr.Error()},
			})
			return report, relErr
		}
		report.Gates = append(report.Gates, GateResult{
			GateID:  "GATE-REL-04-SIGNING",
			Name:    "Official Release Signing & Trust Root Parity",
			Status:  "PASS",
			Details: relSummary.details,
		})
	}

	approvalRec, appErr := VerifyHumanApproval(cfg)
	if cfg.Mode == ModeDryRun {
		// In dry-run mode, GATE-REL-07 must NEVER report Status="PASS".
		if appErr != nil {
			report.GateREL07Status = "BLOCKED_PENDING_HUMAN_SIGN_OFF"
			report.ApprovalError = appErr
			report.Gates = append(report.Gates, GateResult{
				GateID: "GATE-REL-07",
				Name:   "Product Owner Launch Sign-Off",
				Status: "BLOCKED",
				Details: []string{
					fmt.Sprintf("Dry-run confirmed fail-closed gate: %s", appErr.Error()),
					"No git tag or GitHub Release may be created without interactive human PO approval.",
				},
			})
			return report, nil
		}
		report.GateREL07Status = "DRY_RUN_SIGNATURE_CHECKED_NO_PUBLISH"
		report.ApprovedBy = approvalRec.ApprovedBy
		report.ApprovedAt = approvalRec.ApprovedAt
		report.Decision = approvalRec.Decision
		report.Conditions = append([]string(nil), approvalRec.Conditions...)
		report.Gates = append(report.Gates, GateResult{
			GateID: "GATE-REL-07",
			Name:   "Product Owner Launch Sign-Off",
			Status: "BLOCKED",
			Details: []string{
				fmt.Sprintf("Dry-run verified Ed25519 signature by %s (%s); dry-run never unlocks publish", approvalRec.ApprovedBy, approvalRec.Decision),
			},
		})
		return report, nil
	}

	if appErr != nil {
		report.GateREL07Status = "BLOCKED_HUMAN_APPROVAL_REQUIRED"
		report.ApprovalError = appErr
		report.Gates = append(report.Gates, GateResult{
			GateID:  "GATE-REL-07",
			Name:    "Product Owner Launch Sign-Off",
			Status:  "BLOCKED",
			Details: []string{appErr.Error()},
		})
		return report, appErr
	}

	if tcErr := CheckMinGoToolchainVersion(cfg.ToolchainVersion); tcErr != nil {
		report.GateREL07Status = "BLOCKED_INSECURE_TOOLCHAIN"
		return report, tcErr
	}

	if relErr != nil {
		report.GateREL07Status = "BLOCKED_RELEASE_SIGNING_INVALID"
		report.Gates = append(report.Gates, GateResult{
			GateID:  "GATE-REL-04-SIGNING",
			Name:    "Official Release Signing & Trust Root Parity",
			Status:  "FAIL",
			Details: []string{relErr.Error()},
		})
		return report, relErr
	}
	report.Gates = append(report.Gates, GateResult{
		GateID:  "GATE-REL-04-SIGNING",
		Name:    "Official Release Signing & Trust Root Parity",
		Status:  "PASS",
		Details: relSummary.details,
	})

	report.GateREL07Status = "APPROVED_BY_PRODUCT_OWNER"
	report.ApprovedBy = approvalRec.ApprovedBy
	report.ApprovedAt = approvalRec.ApprovedAt
	report.Decision = approvalRec.Decision
	report.Conditions = append([]string(nil), approvalRec.Conditions...)
	report.Gates = append(report.Gates, GateResult{
		GateID: "GATE-REL-07",
		Name:   "Product Owner Launch Sign-Off",
		Status: "PASS",
		Details: []string{
			fmt.Sprintf("Verified Ed25519 PO sign-off by %s (%s) for %s at %s", approvalRec.ApprovedBy, approvalRec.Decision, approvalRec.CommitSHA, approvalRec.ApprovedAt),
		},
	})
	return report, nil
}

func containsControlBytes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// CanonicalApprovalPayload returns the domain-separated byte payload signed by the Product Owner.
// All fields including length-prefixed Conditions are authenticated.
func CanonicalApprovalPayload(rec HumanApprovalRecord) []byte {
	var b strings.Builder
	b.WriteString(approvalDomainPrefix)
	for _, field := range []string{
		rec.Version,
		rec.CommitSHA,
		rec.Decision,
		rec.ApprovedBy,
		rec.ApprovedAt,
		rec.TicketRef,
		rec.Nonce,
		strconv.Itoa(len(rec.Conditions)),
	} {
		b.WriteString(field)
		b.WriteByte(0)
	}
	for _, cond := range rec.Conditions {
		b.WriteString(strconv.Itoa(len(cond)))
		b.WriteByte(':')
		b.WriteString(cond)
		b.WriteByte(0)
	}
	return []byte(b.String())
}

// VerifyHumanApproval enforces the fail-closed GATE-REL-07 guard (NEG-1).
func VerifyHumanApproval(cfg PrePublishConfig) (*HumanApprovalRecord, *ApprovalError) {
	deny := func(reason string) *ApprovalError {
		return &ApprovalError{
			Status:    403,
			ErrorCode: "human_release_approval_required",
			Reason:    reason,
		}
	}

	if cfg.IsCI {
		return nil, deny("automated CI workflows are permanently blocked from satisfying GATE-REL-07 human release approval")
	}
	if cfg.Mode != ModeDryRun && !commitSHA40Pattern.MatchString(strings.TrimSpace(cfg.ExpectedCommitSHA)) {
		return nil, deny("publish mode requires a valid 40-hex --commit-sha matching the approved commit")
	}
	cleanedApproval := bytes.TrimPrefix(cfg.ApprovalJSON, []byte{0xEF, 0xBB, 0xBF})
	if len(bytes.TrimSpace(cleanedApproval)) == 0 {
		return nil, deny("missing Product Owner signed GATE-REL-07 approval record")
	}
	if len(cfg.TrustedPublicKey) != ed25519.PublicKeySize {
		return nil, deny("missing trusted Product Owner Ed25519 public key")
	}

	var rec HumanApprovalRecord
	dec := json.NewDecoder(bytes.NewReader(cleanedApproval))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return nil, deny(fmt.Sprintf("invalid approval JSON: %v", err))
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, deny("unexpected trailing tokens after approval JSON object")
	}

	requiredFields := []string{
		rec.Version,
		rec.CommitSHA,
		rec.Decision,
		rec.ApprovedBy,
		rec.ApprovedAt,
		rec.TicketRef,
		rec.Nonce,
		rec.SignatureHex,
	}
	for _, v := range requiredFields {
		if strings.TrimSpace(v) == "" {
			return nil, deny("incomplete approval record: all identity, commit_sha, timestamp, ticket_ref, nonce, and signature_hex fields are required")
		}
		if containsControlBytes(v) {
			return nil, deny("prohibited control characters in signed approval field")
		}
	}
	for _, c := range rec.Conditions {
		if strings.TrimSpace(c) == "" || containsControlBytes(c) {
			return nil, deny("approval condition entries must be non-empty and free of control bytes")
		}
	}
	if !commitSHA40Pattern.MatchString(rec.CommitSHA) {
		return nil, deny(fmt.Sprintf("approval commit_sha %q must be a 40-character hexadecimal Git SHA", rec.CommitSHA))
	}
	if rec.Version != cfg.TargetVersion {
		return nil, deny(fmt.Sprintf("approval version %q does not match target %q", rec.Version, cfg.TargetVersion))
	}
	if rec.TicketRef != "GATE-REL-07" {
		return nil, deny(fmt.Sprintf("approval ticket_ref must equal GATE-REL-07, got %q", rec.TicketRef))
	}
	if rec.Decision != "GO" && rec.Decision != "GO_WITH_CONDITIONS" {
		return nil, deny(fmt.Sprintf("approval decision %q does not authorize release (requires GO or GO_WITH_CONDITIONS)", rec.Decision))
	}
	if rec.Decision == "GO_WITH_CONDITIONS" && len(rec.Conditions) == 0 {
		return nil, deny("GO_WITH_CONDITIONS decision requires non-empty authenticated conditions list")
	}
	if _, err := time.Parse(time.RFC3339, rec.ApprovedAt); err != nil {
		return nil, deny(fmt.Sprintf("invalid RFC3339 approved_at timestamp: %v", err))
	}
	if strings.TrimSpace(cfg.ExpectedCommitSHA) != "" &&
		subtle.ConstantTimeCompare([]byte(strings.ToLower(rec.CommitSHA)), []byte(strings.ToLower(strings.TrimSpace(cfg.ExpectedCommitSHA)))) != 1 {
		return nil, deny(fmt.Sprintf("approval commit_sha %q does not match target %q", rec.CommitSHA, cfg.ExpectedCommitSHA))
	}

	sigBytes, err := hex.DecodeString(strings.TrimSpace(rec.SignatureHex))
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return nil, deny("malformed Ed25519 signature_hex")
	}
	if !ed25519.Verify(cfg.TrustedPublicKey, CanonicalApprovalPayload(rec), sigBytes) {
		return nil, deny("Ed25519 signature verification failed for GATE-REL-07 approval record")
	}
	return &rec, nil
}

func readRootFile(rootFS *os.Root, rel string) ([]byte, error) {
	f, err := rootFS.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// evaluateGates01To06 aligns 1:1 with docs/quality/release-readiness.md (SPEC-QA-04) section 3
// and docs/planning/units/TP-IMP-0047.md section 4.
func evaluateGates01To06(rootFS *os.Root, toolchainVer string) ([]GateResult, error) {
	var failures []string

	checkFiles := func(paths []string) ([]string, error) {
		var details []string
		for _, rel := range paths {
			info, err := rootFS.Stat(filepath.FromSlash(rel))
			if err != nil {
				return details, fmt.Errorf("missing required file %s: %w", rel, err)
			}
			if !info.Mode().IsRegular() || info.Size() == 0 {
				return details, fmt.Errorf("required file %s must be a non-empty regular file", rel)
			}
			details = append(details, fmt.Sprintf("verified %s (%d bytes)", rel, info.Size()))
		}
		return details, nil
	}

	type gateSpec struct {
		id         string
		name       string
		files      []string
		extraCheck func() ([]string, error)
	}

	specs := []gateSpec{
		{
			id:   "GATE-REL-01",
			name: "Functional, Contract, Floor Guard & Multi-OS Test Evidence",
			files: []string{
				"CONSTRAINTS.md",
				"go.mod",
				".github/workflows/ci.yml",
				"scripts/ci/check.go",
				"openapi/admin-v1.yaml",
				"openapi/dataplane-v1.yaml",
				"proto/tanpopo/extension/v1/runtime.proto",
				"docs/planning/implementation-index.md",
			},
			extraCheck: func() ([]string, error) {
				rawCI, err := readRootFile(rootFS, ".github/workflows/ci.yml")
				if err != nil {
					return nil, err
				}
				for _, osRunner := range []string{"ubuntu-24.04", "macos-15", "windows-2022"} {
					if !strings.Contains(string(rawCI), osRunner) {
						return nil, fmt.Errorf(".github/workflows/ci.yml missing required matrix runner %s", osRunner)
					}
				}
				return []string{"verified 3-OS CI matrix (ubuntu-24.04, macos-15, windows-2022) & contract specs"}, nil
			},
		},
		{
			id:   "GATE-REL-02",
			name: "Compatibility Matrix (OS, Dual-DB SQLite/PG16, Browser, MCP & N-1)",
			files: []string{
				"internal/storage/sqlite/store.go",
				"internal/storage/postgres/driver.go",
				"internal/extension/testharness/migrate_check.go",
				"internal/ops/migrate/sqlite_to_pg.go",
				"deploy/helm/tanpopo/Chart.yaml",
				"deploy/helm/tanpopo/values.yaml",
				"deploy/compose/docker-compose.standalone.yml",
				"deploy/systemd/tanpopo.service",
			},
			extraCheck: func() ([]string, error) {
				sqlFiles, err := fs.Glob(rootFS.FS(), "db/migrations/sqlite/*.sql")
				if err != nil {
					return nil, err
				}
				pgFiles, err := fs.Glob(rootFS.FS(), "db/migrations/postgres/*.sql")
				if err != nil {
					return nil, err
				}
				if len(sqlFiles) == 0 || len(pgFiles) == 0 {
					return nil, fmt.Errorf("missing Dual-DB migrations (sqlite=%d postgres=%d)", len(sqlFiles), len(pgFiles))
				}
				return []string{fmt.Sprintf("verified Dual-DB migrations (sqlite=%d, postgres=%d) & N-1 migrate-check", len(sqlFiles), len(pgFiles))}, nil
			},
		},
		{
			id:   "GATE-REL-03",
			name: "Security Audit, Fuzzing, 5-Surface Pen-Test & SLO Performance",
			files: []string{
				"docs/security/pentest-v1.0-remediation-report.md",
				"docs/quality/benchmark-evidence.md",
				"docs/quality/nonfunctional-slo.md",
				"tests/security/pentest_5surfaces_test.go",
				"tests/perf/harness/main.go",
				"tests/perf/k6/sse_1000_streams.js",
				"internal/ingress/http/overload_shedding_test.go",
			},
			extraCheck: func() ([]string, error) {
				rawBench, err := readRootFile(rootFS, "docs/quality/benchmark-evidence.md")
				if err != nil {
					return nil, err
				}
				if !strings.Contains(string(rawBench), "103.69") || !strings.Contains(string(rawBench), "UNVERIFIED") {
					return nil, fmt.Errorf("benchmark-evidence.md missing COND-PO-01 honest limit/UNVERIFIED disclosures")
				}
				tcNote := fmt.Sprintf("Go runtime toolchain %s (publish requires >= go%d.%d.%d)", toolchainVer, minGoMajor, minGoMinor, minGoPatch)
				return []string{"verified 5-surface pen-test & COND-PO-01 empirical ceilings", tcNote}, nil
			},
		},
		{
			id:   "GATE-REL-04",
			name: "Apache-2.0 Licensing, CycloneDX SBOM, Provenance & DEC-004 Compliance",
			files: []string{
				"LICENSE",
				"NOTICE",
				"SECURITY.md",
				"web/console/package.json",
				".goreleaser.yaml",
				".github/workflows/release-dry-run.yml",
				"scripts/release/check-sbom-licenses.py",
				"scripts/release/spdx-identifiers.json",
				"scripts/release/verify-artifacts.sh",
			},
			extraCheck: func() ([]string, error) {
				licRaw, err := readRootFile(rootFS, "LICENSE")
				if err != nil {
					return nil, err
				}
				if !strings.Contains(string(licRaw), "Apache License") || !strings.Contains(string(licRaw), "Version 2.0") {
					return nil, fmt.Errorf("root LICENSE must be Apache-2.0")
				}
				pkgRaw, err := readRootFile(rootFS, "web/console/package.json")
				if err != nil {
					return nil, err
				}
				var pkgMeta struct {
					License string `json:"license"`
				}
				if err := json.Unmarshal(pkgRaw, &pkgMeta); err != nil || pkgMeta.License != "Apache-2.0" {
					return nil, fmt.Errorf("web/console/package.json license must equal Apache-2.0, got %q", pkgMeta.License)
				}
				rawRel, err := readRootFile(rootFS, ".github/workflows/release-dry-run.yml")
				if err != nil {
					return nil, err
				}
				if !strings.Contains(string(rawRel), "scripts/release/verify-artifacts.sh") {
					return nil, fmt.Errorf(".github/workflows/release-dry-run.yml must invoke scripts/release/verify-artifacts.sh")
				}
				rawGoreleaser, err := readRootFile(rootFS, ".goreleaser.yaml")
				if err != nil {
					return nil, err
				}
				if !strings.Contains(string(rawGoreleaser), "builtin.ReleaseRootPublicKeysHex") ||
					!strings.Contains(string(rawGoreleaser), "TANPOPO_RELEASE_ROOT_PUBKEYS") {
					return nil, fmt.Errorf(".goreleaser.yaml must wire TANPOPO_RELEASE_ROOT_PUBKEYS into builtin.ReleaseRootPublicKeysHex")
				}
				rawVerify, err := readRootFile(rootFS, "scripts/release/verify-artifacts.sh")
				if err != nil {
					return nil, err
				}
				if !strings.Contains(string(rawVerify), "--skip=publish") {
					return nil, fmt.Errorf("scripts/release/verify-artifacts.sh must preserve --skip=publish")
				}
				return []string{"verified Apache-2.0 LICENSE + web/console/package.json, SBOM checker, GoReleaser root-key wiring & --skip=publish dry run"}, nil
			},
		},
		{
			id:   "GATE-REL-05",
			name: "Documentation, OpenAPI/CLI Sync, 15 Locales, Astryx & Accessibility",
			files: []string{
				"README.md",
				"CHANGELOG.md",
				"CONTRIBUTING.md",
				"docs/guides/quickstart.md",
				"docs/guides/plugin-authoring-sdk.md",
				"docs/guides/production-ha-and-airgap.md",
				"docs/guides/security-and-mcp-governance.md",
				"scripts/docs/check_drift.go",
				"web/console/src/astryx-themes.ts",
				"internal/webui/dist/index.html",
				"internal/webui/embed.go",
				"docs/releases/v1.0.0-readiness-dossier.md",
			},
			extraCheck: func() ([]string, error) {
				cliLocales, err := fs.Glob(rootFS.FS(), "internal/pkg/i18n/locales/*.json")
				if err != nil {
					return nil, err
				}
				uiLocales, err := fs.Glob(rootFS.FS(), "web/console/src/i18n/locales/*.json")
				if err != nil {
					return nil, err
				}
				if len(cliLocales) != 15 || len(uiLocales) != 15 {
					return nil, fmt.Errorf("expected 15 CLI and 15 UI locales (got cli=%d ui=%d)", len(cliLocales), len(uiLocales))
				}
				dossierRaw, err := readRootFile(rootFS, "docs/releases/v1.0.0-readiness-dossier.md")
				if err != nil {
					return nil, err
				}
				dossier := string(dossierRaw)
				for _, requiredMarker := range []string{
					"GATE-REL-01", "GATE-REL-02", "GATE-REL-03", "GATE-REL-04",
					"GATE-REL-05", "GATE-REL-06", "GATE-REL-07", "COND-PO-01",
					"DR-04", "UNVERIFIED", "1.26.11", "web/console/e2e/console_e2e.py",
				} {
					if !strings.Contains(dossier, requiredMarker) {
						return nil, fmt.Errorf("docs/releases/v1.0.0-readiness-dossier.md missing required marker %q", requiredMarker)
					}
				}
				return []string{"verified 15 CLI + 15 UI locales, embedded Console bundle & dossier COND-PO-01 markers"}, nil
			},
		},
		{
			id:   "GATE-REL-06",
			name: "SRE Ops Readiness, DR-01..04, FMEA-01..08, Air-Gap (CT-31) & < 60s Canary Rollback",
			files: []string{
				"docs/operations/monitoring-incidents.md",
				"docs/operations/backups-recovery.md",
				"deploy/docker/Dockerfile",
				"deploy/monitoring/alerts.yaml",
				"internal/cli/bundle/verify.go",
				"internal/cli/bundle/import.go",
				"tests/chaos/fmea_suite_test.go",
				"tests/chaos/dr_drills_test.go",
				"scripts/release/canary_rollback_drill_test.go",
			},
			extraCheck: func() ([]string, error) {
				alertsRaw, err := readRootFile(rootFS, "deploy/monitoring/alerts.yaml")
				if err != nil {
					return nil, err
				}
				if !strings.Contains(string(alertsRaw), "ALT-GATEWAY-BURN") {
					return nil, fmt.Errorf("deploy/monitoring/alerts.yaml missing ALT-GATEWAY-BURN trigger")
				}
				return []string{"verified DR-01..04, FMEA-01..08, Air-Gap CT-31 & ALT-GATEWAY-BURN canary drill"}, nil
			},
		},
	}

	results := make([]GateResult, 0, len(specs))
	for _, sp := range specs {
		res := GateResult{
			GateID: sp.id,
			Name:   sp.name,
			Status: "PASS",
		}
		fileDetails, err := checkFiles(sp.files)
		res.Details = append(res.Details, fileDetails...)
		if err != nil {
			res.Status = "FAIL"
			res.Details = append(res.Details, err.Error())
			failures = append(failures, fmt.Sprintf("%s: %v", sp.id, err))
		} else if sp.extraCheck != nil {
			extra, exErr := sp.extraCheck()
			res.Details = append(res.Details, extra...)
			if exErr != nil {
				res.Status = "FAIL"
				res.Details = append(res.Details, exErr.Error())
				failures = append(failures, fmt.Sprintf("%s: %v", sp.id, exErr))
			}
		}
		results = append(results, res)
	}

	if len(failures) > 0 {
		return results, fmt.Errorf("gate failures: %s", strings.Join(failures, "; "))
	}
	return results, nil
}

func buildArtifactManifest(rootFS *os.Root) ([]ArtifactChecksum, error) {
	paths := []string{
		".goreleaser.yaml",
		"CHANGELOG.md",
		"CONSTRAINTS.md",
		"LICENSE",
		"NOTICE",
		"README.md",
		"SECURITY.md",
		"deploy/compose/docker-compose.standalone.yml",
		"deploy/docker/Dockerfile",
		"deploy/helm/tanpopo/Chart.yaml",
		"deploy/helm/tanpopo/values.yaml",
		"deploy/monitoring/alerts.yaml",
		"deploy/systemd/tanpopo.service",
		"docs/quality/benchmark-evidence.md",
		"docs/releases/v1.0.0-readiness-dossier.md",
		"docs/security/pentest-v1.0-remediation-report.md",
		"internal/webui/dist/index.html",
		"openapi/admin-v1.yaml",
		"openapi/dataplane-v1.yaml",
		"proto/tanpopo/extension/v1/runtime.proto",
		"scripts/release/check-sbom-licenses.py",
		"scripts/release/spdx-identifiers.json",
		"scripts/release/verify-artifacts.sh",
	}
	sort.Strings(paths)

	out := make([]ArtifactChecksum, 0, len(paths))
	for _, rel := range paths {
		info, err := rootFS.Stat(filepath.FromSlash(rel))
		if err != nil {
			return nil, fmt.Errorf("stat manifest artifact %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("manifest artifact %s must be regular file", rel)
		}
		raw, err := readRootFile(rootFS, rel)
		if err != nil {
			return nil, fmt.Errorf("read manifest artifact %s: %w", rel, err)
		}
		sum := sha256.Sum256(raw)
		out = append(out, ArtifactChecksum{
			Path:      rel,
			SHA256:    hex.EncodeToString(sum[:]),
			SizeBytes: info.Size(),
		})
	}
	return out, nil
}

type releaseSigningSummary struct {
	status           string
	signedCount      int
	totalCount       int
	rootFingerprints []string
	details          []string
}

type releaseSignaturesManifestDoc struct {
	Signatures []builtin.ReleaseSignatureRecord `json:"signatures"`
}

func evaluateReleaseSigning(cfg PrePublishConfig, rootFS *os.Root) (releaseSigningSummary, error) {
	cat, err := builtin.NewCatalog()
	if err != nil {
		return releaseSigningSummary{status: releaseSigningInvalidOrMissing}, fmt.Errorf("release signing: load built-in official catalog: %w", err)
	}
	pkgs := cat.Packages()
	if len(pkgs) == 0 {
		return releaseSigningSummary{status: releaseSigningInvalidOrMissing}, errors.New("release signing: embedded official catalog is empty")
	}
	summary := releaseSigningSummary{
		status:     releaseSigningInvalidOrMissing,
		totalCount: len(pkgs),
	}

	rawSigs := cfg.ReleaseSignaturesJSON
	if rawSigs == nil {
		rawSigs, err = readRootFile(rootFS, "extensions/official/release-signatures.json")
		if err != nil {
			return summary, fmt.Errorf("release signing: read extensions/official/release-signatures.json: %w", err)
		}
	}
	cleanedSigs := bytes.TrimPrefix(rawSigs, []byte{0xEF, 0xBB, 0xBF})
	var doc releaseSignaturesManifestDoc
	dec := json.NewDecoder(bytes.NewReader(cleanedSigs))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return summary, fmt.Errorf("release signing: decode release-signatures.json: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return summary, errors.New("release signing: unexpected trailing content in release-signatures.json")
	}

	embeddedRoots := make(map[string]ed25519.PublicKey, len(cfg.EmbeddedReleaseRootPubKeys))
	for _, pub := range cfg.EmbeddedReleaseRootPubKeys {
		if len(pub) != ed25519.PublicKeySize {
			return summary, errors.New("release signing: invalid embedded release root Ed25519 public key length")
		}
		embeddedRoots[trust.PublicKeyFingerprint(pub)] = pub
	}
	configuredRoots := make(map[string]ed25519.PublicKey, len(cfg.ReleaseRootPublicKeys))
	for _, pub := range cfg.ReleaseRootPublicKeys {
		if len(pub) != ed25519.PublicKeySize {
			return summary, errors.New("release signing: invalid release root Ed25519 public key length")
		}
		configuredRoots[trust.PublicKeyFingerprint(pub)] = pub
	}

	if len(embeddedRoots) > 0 && len(configuredRoots) > 0 {
		if len(embeddedRoots) != len(configuredRoots) {
			return summary, fmt.Errorf("release signing: inconsistent release root key sets (embedded=%d configured=%d)", len(embeddedRoots), len(configuredRoots))
		}
		for fp := range embeddedRoots {
			if _, ok := configuredRoots[fp]; !ok {
				return summary, fmt.Errorf("release signing: embedded release root key %s missing from configured release root keys", fp)
			}
		}
	}

	trustedRoots := make(map[string]ed25519.PublicKey, len(embeddedRoots)+len(configuredRoots))
	for fp, pub := range embeddedRoots {
		trustedRoots[fp] = pub
	}
	for fp, pub := range configuredRoots {
		trustedRoots[fp] = pub
	}

	expectedByPkgVer := make(map[string]builtin.Package, len(pkgs))
	for _, pkg := range pkgs {
		expectedByPkgVer[strings.ToLower(pkg.PackageID)+"@"+pkg.Version] = pkg
	}

	sigsByKey := make(map[string][]byte, len(doc.Signatures))
	seenPkgVer := make(map[string]struct{}, len(doc.Signatures))
	for _, rec := range doc.Signatures {
		pkgID := strings.ToLower(strings.TrimSpace(rec.PackageID))
		ver := strings.TrimSpace(rec.Version)
		sha := strings.ToLower(strings.TrimSpace(rec.ArchiveSHA256))
		if pkgID == "" || ver == "" || len(sha) != 64 {
			return summary, fmt.Errorf("release signing: incomplete record metadata for %q@%q", rec.PackageID, rec.Version)
		}
		if _, hexErr := hex.DecodeString(sha); hexErr != nil {
			return summary, fmt.Errorf("release signing: invalid archive_sha256 for %s@%s", rec.PackageID, rec.Version)
		}
		pkgVer := pkgID + "@" + ver
		if _, dup := seenPkgVer[pkgVer]; dup {
			return summary, fmt.Errorf("release signing: duplicate signature record for %s@%s", rec.PackageID, rec.Version)
		}
		seenPkgVer[pkgVer] = struct{}{}

		pkg, known := expectedByPkgVer[pkgVer]
		if !known {
			return summary, fmt.Errorf("release signing: inconsistent orphan signature record for unknown package %s@%s", rec.PackageID, rec.Version)
		}
		if sha != strings.ToLower(pkg.ArchiveSHA256) {
			return summary, fmt.Errorf("release signing: inconsistent archive digest for %s@%s (manifest=%s expected=%s)", rec.PackageID, rec.Version, sha, pkg.ArchiveSHA256)
		}

		sigRaw, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.RootSignature))
		if derr != nil || len(sigRaw) != ed25519.SignatureSize {
			return summary, fmt.Errorf("release signing: malformed Ed25519 signature for %s@%s", rec.PackageID, rec.Version)
		}
		sigsByKey[pkgVer+"#"+sha] = sigRaw
	}

	if cfg.Mode == ModeDryRun && len(doc.Signatures) == 0 && len(trustedRoots) == 0 {
		summary.status = releaseSigningUnsignedRC
		summary.details = []string{
			fmt.Sprintf("Unsigned internal RC build (0/%d signed official packages, 0 trusted release root keys); official extensions surface unverified_external with signature_verified=false", len(pkgs)),
			"Publish mode requires offline signatures covering all official packages and matching trusted release root public keys.",
		}
		return summary, nil
	}

	if len(trustedRoots) == 0 {
		return summary, errors.New("release signing: missing trusted Ed25519 release root public key (--release-root-pubkey-hex, --release-root-pubkey-file, TANPOPO_RELEASE_ROOT_PUBKEYS, or embedded ReleaseRootPublicKeysHex)")
	}
	if len(doc.Signatures) == 0 {
		return summary, fmt.Errorf("release signing: missing official package signatures (0/%d signed)", len(pkgs))
	}
	if len(doc.Signatures) != len(pkgs) {
		return summary, fmt.Errorf("release signing: incomplete official package signature manifest (%d/%d signed)", len(doc.Signatures), len(pkgs))
	}

	matchedRoots := make(map[string]struct{}, len(trustedRoots))
	for _, pkg := range pkgs {
		sigKey := strings.ToLower(pkg.PackageID) + "@" + pkg.Version + "#" + strings.ToLower(pkg.ArchiveSHA256)
		sigRaw, ok := sigsByKey[sigKey]
		if !ok {
			return summary, fmt.Errorf("release signing: missing official package signature for %s@%s", pkg.PackageID, pkg.Version)
		}
		payload := installer.SignatureSigningPayload(pkg.PackageID, pkg.Version, pkg.ArchiveSHA256)
		verified := false
		for fp, pub := range trustedRoots {
			if ed25519.Verify(pub, payload, sigRaw) {
				verified = true
				matchedRoots[fp] = struct{}{}
			}
		}
		if !verified {
			return summary, fmt.Errorf("release signing: Ed25519 signature verification failed for %s@%s#%s", pkg.PackageID, pkg.Version, pkg.ArchiveSHA256)
		}
		summary.signedCount++
	}

	fps := make([]string, 0, len(matchedRoots))
	for fp := range matchedRoots {
		fps = append(fps, fp)
	}
	sort.Strings(fps)
	summary.status = releaseSigningVerified
	summary.rootFingerprints = fps
	summary.details = []string{
		fmt.Sprintf("verified %d/%d official extension signatures against trusted Ed25519 release root(s): %s", summary.signedCount, summary.totalCount, strings.Join(fps, ", ")),
	}
	return summary, nil
}

func parseEd25519PublicKeysHex(raw string) ([]ed25519.PublicKey, error) {
	var out []ed25519.PublicKey
	for _, tok := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		trimmed := strings.TrimSpace(tok)
		if trimmed == "" {
			continue
		}
		dec, err := hex.DecodeString(trimmed)
		if err != nil || len(dec) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("expected 64-hex Ed25519 public key")
		}
		out = append(out, ed25519.PublicKey(dec))
	}
	if len(out) == 0 {
		return nil, errors.New("empty public key list")
	}
	return out, nil
}

func loadEd25519PublicKeyFile(path string) (ed25519.PublicKey, error) {
	raw, err := readBoundedRegularFile(path, maxPubKeyFileBytes)
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if block, _ := pem.Decode(raw); block != nil {
		parsed, perr := x509.ParsePKIXPublicKey(block.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("parse PKIX public key %q: %w", path, perr)
		}
		edPub, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("public key %q is not Ed25519", path)
		}
		return edPub, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if dec, derr := hex.DecodeString(trimmed); derr == nil && len(dec) == ed25519.PublicKeySize {
		return ed25519.PublicKey(dec), nil
	}
	if dec, derr := base64.StdEncoding.DecodeString(trimmed); derr == nil && len(dec) == ed25519.PublicKeySize {
		return ed25519.PublicKey(dec), nil
	}
	return nil, fmt.Errorf("unsupported Ed25519 public key format in %q", path)
}

func loadEd25519PrivateKeyFile(path string) (ed25519.PrivateKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("--private-key-file is required (keys are never generated or embedded)")
	}
	raw, err := readBoundedRegularFile(path, maxPubKeyFileBytes)
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if block, _ := pem.Decode(raw); block != nil {
		parsed, perr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("parse PKCS8 private key %q: %w", path, perr)
		}
		edPriv, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key %q is not Ed25519", path)
		}
		return edPriv, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if dec, derr := hex.DecodeString(trimmed); derr == nil {
		switch len(dec) {
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(dec), nil
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(dec), nil
		}
	}
	if dec, derr := base64.StdEncoding.DecodeString(trimmed); derr == nil {
		switch len(dec) {
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(dec), nil
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(dec), nil
		}
	}
	return nil, fmt.Errorf("unsupported Ed25519 private key format in %q", path)
}

func runSignApprovalCLI(targetVer, commitSHA, decision, approvedBy, approvedAt string, conditions []string, nonce, privKeyPath, outPath string, stdout io.Writer) error {
	if strings.TrimSpace(targetVer) == "" || containsControlBytes(targetVer) {
		return fmt.Errorf("--target-version must be non-empty and free of control bytes")
	}
	if !commitSHA40Pattern.MatchString(commitSHA) {
		return fmt.Errorf("--commit-sha must be a 40-character hexadecimal SHA, got %q", commitSHA)
	}
	if strings.TrimSpace(approvedBy) == "" || containsControlBytes(approvedBy) {
		return errors.New("--approved-by must be non-empty and free of control bytes")
	}
	if decision != "GO" && decision != "GO_WITH_CONDITIONS" {
		return fmt.Errorf("--decision must be GO or GO_WITH_CONDITIONS, got %q", decision)
	}
	if decision == "GO_WITH_CONDITIONS" && len(conditions) == 0 {
		return errors.New("GO_WITH_CONDITIONS requires at least one --condition entry")
	}
	for _, cond := range conditions {
		if strings.TrimSpace(cond) == "" || containsControlBytes(cond) {
			return errors.New("--condition entries must be non-empty and free of control bytes")
		}
	}
	if strings.TrimSpace(approvedAt) == "" {
		approvedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if _, err := time.Parse(time.RFC3339, approvedAt); err != nil {
		return fmt.Errorf("invalid --approved-at RFC3339 timestamp: %w", err)
	}
	if strings.TrimSpace(nonce) == "" {
		var rnd [16]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return fmt.Errorf("generate nonce: %w", err)
		}
		nonce = "po-" + hex.EncodeToString(rnd[:])
	} else if containsControlBytes(nonce) {
		return errors.New("--nonce cannot contain control bytes")
	}
	priv, err := loadEd25519PrivateKeyFile(privKeyPath)
	if err != nil {
		return err
	}
	rec := HumanApprovalRecord{
		Version:    targetVer,
		CommitSHA:  strings.ToLower(commitSHA),
		Decision:   decision,
		ApprovedBy: strings.TrimSpace(approvedBy),
		ApprovedAt: approvedAt,
		TicketRef:  "GATE-REL-07",
		Conditions: append([]string(nil), conditions...),
		Nonce:      strings.TrimSpace(nonce),
	}
	sig := ed25519.Sign(priv, CanonicalApprovalPayload(rec))
	rec.SignatureHex = hex.EncodeToString(sig)

	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if outPath == "" {
		outPath = "gate-rel-07-approval.json"
	}
	if err := writeManifestFileSafely(outPath, raw); err != nil {
		return err
	}
	pub := priv.Public().(ed25519.PublicKey)
	fmt.Fprintf(stdout, "Wrote GATE-REL-07 signed approval: %s\nPublic Key Hex: %s\nFingerprint: %s\n", outPath, hex.EncodeToString(pub), trust.PublicKeyFingerprint(pub))
	return nil
}
