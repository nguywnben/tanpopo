// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package testharness

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
)

// MigrateCheckOptions configures forward/backward extension contract checks (`CT-24`, `BA-OQ-05`).
type MigrateCheckOptions struct {
	Dir                       string
	TargetCoreVersion         string
	SupportedContractVersions []string
}

// MigrateCheckFinding captures a single deprecation warning or breaking compatibility issue.
type MigrateCheckFinding struct {
	Code     string `json:"code" yaml:"code"`
	Severity string `json:"severity" yaml:"severity"` // "WARN" | "ERROR"
	Message  string `json:"message" yaml:"message"`
}

// MigrateCheckReport summarizes N / N-1 contract and schema compatibility for an extension.
type MigrateCheckReport struct {
	ExtensionID               string                `json:"extension_id" yaml:"extension_id"`
	ExtensionVersion          string                `json:"extension_version" yaml:"extension_version"`
	ContractVersion           string                `json:"contract_version" yaml:"contract_version"`
	CoreVersionRange          string                `json:"core_version_range" yaml:"core_version_range"`
	TargetCoreVersion         string                `json:"target_core_version" yaml:"target_core_version"`
	SupportedContractVersions []string              `json:"supported_contract_versions" yaml:"supported_contract_versions"`
	Compatible                bool                  `json:"compatible" yaml:"compatible"`
	DeprecatedContract        bool                  `json:"deprecated_contract" yaml:"deprecated_contract"`
	Status                    string                `json:"status" yaml:"status"` // "COMPATIBLE" | "COMPATIBLE_WITH_DEPRECATION_WARNING" | "INCOMPATIBLE"
	ExitCode                  int                   `json:"exit_code" yaml:"exit_code"`
	Findings                  []MigrateCheckFinding `json:"findings,omitempty" yaml:"findings,omitempty"`
}

// MigrateCheckExtension validates an extension directory against N and N-1 Core/Contract windows
// per CT-24 and BA-OQ-05, returning ExitContract (22) when incompatible.
func MigrateCheckExtension(ctx context.Context, opts MigrateCheckOptions) (*MigrateCheckReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		dir = "."
	}
	targetCore := strings.TrimSpace(opts.TargetCoreVersion)
	if targetCore == "" {
		targetCore = installer.ActiveCoreSemver
	}
	if !schemaSemverRe.MatchString(targetCore) {
		return nil, validationErr(fmt.Sprintf("invalid target core semver %q", targetCore), nil)
	}
	supported := make([]string, 0, len(opts.SupportedContractVersions))
	for _, s := range opts.SupportedContractVersions {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			supported = append(supported, trimmed)
		}
	}
	if len(supported) == 0 {
		supported = []string{installer.ActiveContractVersion}
	}

	_, manifestBytes, archiveFiles, err := loadExtensionFilesForValidation(dir)
	if err != nil {
		return nil, err
	}
	rawObj, err := decodeYAMLAsJSONObject(manifestBytes)
	if err != nil {
		return nil, validationErr("malformed manifest.yaml", err)
	}
	if hasRemovedLegacyUnsealCapability(rawObj) {
		return nil, securityErr("manifest declares removed scoped_secret_unseal capability (CR-05)", nil)
	}

	pol := installer.DefaultValidationPolicy()
	pol.CoreVersion = targetCore
	pol.SupportedContractVersions = append([]string(nil), supported...)

	vm, parseErr := installer.ParseAndValidateManifest(manifestBytes, pol, archiveFiles)

	metaMap, _ := rawObj["metadata"].(map[string]any)
	compatMap, _ := rawObj["compatibility"].(map[string]any)
	extID, _ := metaMap["id"].(string)
	extVer, _ := metaMap["version"].(string)
	contractVer, _ := compatMap["contractVersion"].(string)
	coreRange, _ := compatMap["coreVersionRange"].(string)
	if vm != nil {
		extID = vm.Manifest.Metadata.ID
		extVer = vm.Manifest.Metadata.Version
		contractVer = vm.Manifest.Compatibility.ContractVersion
		coreRange = vm.Manifest.Compatibility.CoreVersionRange
	}

	report := &MigrateCheckReport{
		ExtensionID:               strings.TrimSpace(extID),
		ExtensionVersion:          strings.TrimSpace(extVer),
		ContractVersion:           strings.TrimSpace(contractVer),
		CoreVersionRange:          strings.TrimSpace(coreRange),
		TargetCoreVersion:         targetCore,
		SupportedContractVersions: supported,
		Compatible:                true,
		Status:                    "COMPATIBLE",
		ExitCode:                  ExitOK,
	}

	if parseErr != nil {
		isSec := errors.Is(parseErr, installer.ErrProhibitedCookiePermission) ||
			errors.Is(parseErr, installer.ErrProhibitedCapabilityTier) ||
			errors.Is(parseErr, port.ErrProhibitedCookieInjection) ||
			errors.Is(parseErr, installer.ErrWildcardEgressForbidden)
		report.Compatible = false
		if isSec {
			report.Status = "SECURITY_VIOLATION"
			report.ExitCode = ExitSecurity
			report.Findings = append(report.Findings, MigrateCheckFinding{
				Code:     "SECURITY_POLICY_VIOLATION",
				Severity: "ERROR",
				Message:  parseErr.Error(),
			})
			return report, classifyInstallerError(parseErr)
		}
		report.Status = "INCOMPATIBLE"
		report.ExitCode = ExitContract
		report.Findings = append(report.Findings, MigrateCheckFinding{
			Code:     "CONTRACT_INCOMPATIBLE",
			Severity: "ERROR",
			Message:  parseErr.Error(),
		})
		return report, contractErr("extension N-1 contract compatibility check failed", parseErr)
	}

	// Flag deprecation warning if using N-1 contractVersion while a newer supported contract exists.
	latestContract := pickHighestContractVersion(supported)
	if report.ContractVersion != "" && report.ContractVersion != latestContract {
		report.DeprecatedContract = true
		report.Status = "COMPATIBLE_WITH_DEPRECATION_WARNING"
		report.Findings = append(report.Findings, MigrateCheckFinding{
			Code:     "CONTRACT_VERSION_N_MINUS_1",
			Severity: "WARN",
			Message:  fmt.Sprintf("contractVersion %q runs in N-1 compatibility mode alongside active %q; plan upgrade within the deprecation window", report.ContractVersion, latestContract),
		})
	}
	return report, nil
}

func pickHighestContractVersion(versions []string) string {
	if len(versions) == 0 {
		return ""
	}
	best := versions[0]
	for _, candidate := range versions[1:] {
		if compareDottedNumeric(candidate, best) > 0 {
			best = candidate
		}
	}
	return best
}

func compareDottedNumeric(a, b string) int {
	pa := strings.Split(strings.TrimSpace(a), ".")
	pb := strings.Split(strings.TrimSpace(b), ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var va, vb int
		if i < len(pa) {
			va, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			vb, _ = strconv.Atoi(pb[i])
		}
		if va != vb {
			if va > vb {
				return 1
			}
			return -1
		}
	}
	return strings.Compare(a, b)
}

func hasRemovedLegacyUnsealCapability(doc map[string]any) bool {
	if containsLegacyUnseal(doc["capabilities"]) {
		return true
	}
	if perms, ok := doc["permissions"].(map[string]any); ok {
		if containsLegacyUnseal(perms["hooks"]) {
			return true
		}
	}
	if adp, ok := doc["adapter"].(map[string]any); ok {
		if containsLegacyUnseal(adp["auth"]) {
			return true
		}
	}
	return false
}

func containsLegacyUnseal(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "scoped_secret_unseal")
	case []any:
		for _, item := range x {
			if containsLegacyUnseal(item) {
				return true
			}
		}
	case map[string]any:
		for _, key := range []string{"name", "type", "mode"} {
			if s, ok := x[key].(string); ok && strings.EqualFold(strings.TrimSpace(s), "scoped_secret_unseal") {
				return true
			}
		}
		if containsLegacyUnseal(x["authMethods"]) {
			return true
		}
	}
	return false
}
