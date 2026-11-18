// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package doctor

import (
	"context"
	"testing"
)

func TestRunProducesEightChecksInContractOrder(t *testing.T) {
	pass := func(context.Context) Status { return StatusPass }
	report := Run(context.Background(), Probes{
		ClockSkew: pass, PortsAvailable: pass, MasterKEKReady: pass,
		StorageFSLock: pass, BundleLockfileIntegrity: pass,
		TLSPosture: pass, EgressDNSSSRF: pass,
	})
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	if report.HasFailures() {
		t.Fatal("all passing probes reported a failure")
	}
	if got := report.Checks[4].Status; got != StatusSkipped {
		t.Fatalf("cluster cache status = %q, want SKIPPED when HA probe is absent", got)
	}
}

func TestRunFailsClosedAndDoesNotExposeProbeErrors(t *testing.T) {
	secret := "probe-output-must-not-escape"
	probes := Probes{
		ClockSkew:               func(context.Context) Status { return StatusPass },
		PortsAvailable:          func(context.Context) Status { return Status(secret) },
		MasterKEKReady:          func(context.Context) Status { return StatusPass },
		StorageFSLock:           func(context.Context) Status { return StatusPass },
		ClusterCache:            func(context.Context) Status { return StatusPass },
		BundleLockfileIntegrity: func(context.Context) Status { return StatusPass },
		TLSPosture:              func(context.Context) Status { return StatusPass },
		EgressDNSSSRF:           func(context.Context) Status { return StatusPass },
	}
	report := Run(context.Background(), probes)
	if !report.HasFailures() {
		t.Fatal("invalid probe status did not fail closed")
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, check := range report.Checks {
		if string(check.Status) == secret {
			t.Fatal("report exposed probe-supplied value")
		}
	}
}

func TestRunCancellationSkipsRemainingProbes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	probe := func(context.Context) Status { called = true; return StatusPass }
	report := Run(ctx, Probes{ClockSkew: probe})
	if called {
		t.Fatal("probe ran after context cancellation")
	}
	if len(report.Checks) != 8 || report.Checks[0].Status != StatusSkipped {
		t.Fatalf("unexpected canceled report: %+v", report)
	}
	if !report.HasFailures() {
		t.Fatal("canceled mandatory checks were treated as healthy")
	}
}

func TestRunDoesNotAllowMandatoryProbeToSkip(t *testing.T) {
	probe := func(context.Context) Status { return StatusSkipped }
	report := Run(context.Background(), Probes{ClockSkew: probe})
	if report.Checks[0].Status != StatusFail {
		t.Fatalf("mandatory skipped check status = %q, want FAIL", report.Checks[0].Status)
	}
}

func TestRunDoesNotAllowConfiguredClusterProbeToSkip(t *testing.T) {
	probe := func(context.Context) Status { return StatusSkipped }
	report := Run(context.Background(), Probes{ClusterCache: probe})
	if report.Checks[4].Status != StatusFail {
		t.Fatalf("configured cluster check status = %q, want FAIL", report.Checks[4].Status)
	}
}
