// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package upgrade contains the reusable extension compatibility check shared by
// the Unit 30 Admin API and the complete Unit 32 CLI lifecycle workflow.
package upgrade

import (
	"fmt"
	"strings"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
)

type InstalledExtension struct {
	InstallationID string
	PackageID      string
	Version        string
	CoreRange      string
}

type BlockingExtension struct {
	InstallationID string `json:"installation_id"`
	PackageID      string `json:"package_id"`
	Installed      string `json:"installed_version"`
	CoreRange      string `json:"core_version_range"`
	Reason         string `json:"reason"`
}

type PreflightResult struct {
	Compatible         bool                `json:"compatible"`
	State              string              `json:"state"`
	BlockingExtensions []BlockingExtension `json:"blocking_extensions"`
}

// Evaluate checks the target core version against every active extension's
// persisted compatibility range. Invalid stored ranges fail closed as blockers.
func Evaluate(target string, installed []InstalledExtension) (PreflightResult, error) {
	target = strings.TrimSpace(target)
	if _, err := installer.SemverMatchesRange(target, ">=0.0.0"); err != nil {
		return PreflightResult{}, fmt.Errorf("invalid target core version: %w", err)
	}
	result := PreflightResult{Compatible: true, State: "READY_TO_UPGRADE", BlockingExtensions: []BlockingExtension{}}
	for _, ext := range installed {
		matches, err := installer.SemverMatchesRange(target, ext.CoreRange)
		if err == nil && matches {
			continue
		}
		reason := "target core version is outside the extension compatibility range"
		if err != nil {
			reason = "stored extension compatibility range is invalid: " + err.Error()
		}
		result.BlockingExtensions = append(result.BlockingExtensions, BlockingExtension{
			InstallationID: ext.InstallationID, PackageID: ext.PackageID, Installed: ext.Version,
			CoreRange: ext.CoreRange, Reason: reason,
		})
	}
	if len(result.BlockingExtensions) != 0 {
		result.Compatible = false
		result.State = "BLOCKED_INCOMPATIBLE"
	}
	return result, nil
}
