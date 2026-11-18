// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package doctor defines the ordered, side-effect-free result contract for the
// eight environmental preflight checks in SPEC-OPS-01. Host integrations
// provide probes so this package does not own network or storage clients.
package doctor

import (
	"context"
	"errors"
)

type Status string

const (
	StatusPass    Status = "PASS"
	StatusWarn    Status = "WARN"
	StatusFail    Status = "FAIL"
	StatusSkipped Status = "SKIPPED"
)

const (
	CheckClockSkew               = "clock-skew"
	CheckPortsAvailable          = "ports-available"
	CheckMasterKEKReady          = "master-kek-ready"
	CheckStorageFSLock           = "storage-fs-lock"
	CheckClusterCache            = "cluster-cache"
	CheckBundleLockfileIntegrity = "bundle-lockfile-integrity"
	CheckTLSPosture              = "tls-posture"
	CheckEgressDNSSSRF           = "egress-dns-ssrf"
)

// Probe reports only a status. It intentionally has no message/error return
// channel, so secret-bearing provider errors cannot accidentally enter doctor
// output or logs.
type Probe func(context.Context) Status

// Probes contains the environmental checks in their documented order. A nil
// ClusterCache probe means HA mode is disabled and is reported as SKIPPED.
type Probes struct {
	ClockSkew               Probe
	PortsAvailable          Probe
	MasterKEKReady          Probe
	StorageFSLock           Probe
	ClusterCache            Probe
	BundleLockfileIntegrity Probe
	TLSPosture              Probe
	EgressDNSSSRF           Probe
}

type Check struct {
	ID     string `json:"id"`
	Status Status `json:"status"`
}

type Report struct {
	Checks []Check `json:"checks"`
}

// Run executes checks in SPEC-OPS-01 order. Missing mandatory probes fail
// closed. A canceled context marks remaining checks SKIPPED without invoking
// their probes.
func Run(ctx context.Context, probes Probes) Report {
	ordered := []struct {
		id       string
		probe    Probe
		optional bool
	}{
		{CheckClockSkew, probes.ClockSkew, false},
		{CheckPortsAvailable, probes.PortsAvailable, false},
		{CheckMasterKEKReady, probes.MasterKEKReady, false},
		{CheckStorageFSLock, probes.StorageFSLock, false},
		{CheckClusterCache, probes.ClusterCache, true},
		{CheckBundleLockfileIntegrity, probes.BundleLockfileIntegrity, false},
		{CheckTLSPosture, probes.TLSPosture, false},
		{CheckEgressDNSSSRF, probes.EgressDNSSSRF, false},
	}
	report := Report{Checks: make([]Check, 0, len(ordered))}
	for _, item := range ordered {
		status := StatusFail
		switch {
		case ctx.Err() != nil:
			status = StatusSkipped
		case item.probe == nil && item.optional:
			status = StatusSkipped
		case item.probe != nil:
			status = item.probe(ctx)
			if !validStatus(status) {
				status = StatusFail
			}
			if status == StatusSkipped {
				status = StatusFail
			}
		}
		report.Checks = append(report.Checks, Check{ID: item.id, Status: status})
	}
	return report
}

func validStatus(status Status) bool {
	switch status {
	case StatusPass, StatusWarn, StatusFail, StatusSkipped:
		return true
	default:
		return false
	}
}

// HasFailures returns true if any check failed. Warnings and intentionally
// skipped HA checks do not turn an otherwise usable report into a failure.
func (r Report) HasFailures() bool {
	for _, check := range r.Checks {
		if check.Status == StatusFail || (check.Status == StatusSkipped && check.ID != CheckClusterCache) {
			return true
		}
	}
	return false
}

// Validate checks that a report has the complete stable ID ordering.
func (r Report) Validate() error {
	ids := [...]string{CheckClockSkew, CheckPortsAvailable, CheckMasterKEKReady, CheckStorageFSLock, CheckClusterCache, CheckBundleLockfileIntegrity, CheckTLSPosture, CheckEgressDNSSSRF}
	if len(r.Checks) != len(ids) {
		return errors.New("doctor: report must contain all eight checks")
	}
	for i, check := range r.Checks {
		if check.ID != ids[i] || !validStatus(check.Status) {
			return errors.New("doctor: report has invalid check order or status")
		}
	}
	return nil
}
