// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !windows

package sidecar

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

func init() {
	helperRunner = func() {
		udsPath := os.Getenv("TANPOPO_HELPER_UDS")
		if udsPath == "" {
			os.Exit(2)
		}
		ln, err := ListenUDS(udsPath)
		if err != nil {
			os.Exit(3)
		}
		srv := &http.Server{
			Handler:           NewConnectHandler(&mockSidecarRuntime{}),
			ReadHeaderTimeout: 5 * time.Second,
		}
		_ = srv.Serve(ln)
	}
}

func TestSidecarSIGKILLTripsDedicatedCircuitBreakerAndUDSChecksAC1(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	clk := clock.NewFakeClock(time.Unix(1_760_000_000, 0).UTC())
	sockDir, err := os.MkdirTemp("", "tp-sc-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(sockDir) }()
	udsPath := filepath.Join(sockDir, "s.sock")

	// Start an isolated out-of-process Sidecar child speaking ConnectRPC over a 0600 UDS socket.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "TANPOPO_SIDECAR_HELPER=1", "TANPOPO_HELPER_UDS="+udsPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sidecar helper process: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := VerifyUDSSocketPermissions(udsPath); err == nil {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	audit := &memAuditSink{}
	cl, err := NewClient(Config{
		Enabled:                 true,
		InstallationID:          "ext_uds_killed",
		UDSPath:                 udsPath,
		CircuitFailureThreshold: 1,
		CircuitCooldown:         30 * time.Second,
		AuditSink:               audit,
		Now:                     clk.Now,
	})
	if err != nil {
		t.Fatalf("NewClient UDS: %v", err)
	}
	defer func() { _ = cl.Close() }()

	ctx := context.Background()
	if _, err := cl.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "req_live", Stage: port.GuardrailStagePreRequest, PayloadJSON: []byte(`{"ok":1}`)}); err != nil {
		t.Fatalf("EvaluateGuardrail over UDS failed: %v", err)
	}

	// Abruptly SIGKILL the sidecar process -> dedicated circuit breaker trips open cleanly.
	if err := cmd.Process.Signal(os.Kill); err != nil {
		t.Fatalf("SIGKILL sidecar process: %v", err)
	}
	_, _ = cmd.Process.Wait()

	if _, err := cl.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "req_after_kill", PayloadJSON: []byte(`{}`)}); err == nil {
		t.Fatal("expected transport error immediately after SIGKILL")
	}
	if !cl.CircuitOpen() {
		t.Fatal("AC-1: expected dedicated sidecar circuit breaker to trip open on SIGKILL crash")
	}
	if _, err := cl.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "req_fast_fail", PayloadJSON: []byte(`{}`)}); !errors.Is(err, ErrSidecarCircuitOpen) {
		t.Fatalf("expected ErrSidecarCircuitOpen while breaker is open, got %v", err)
	}
	if audit.events.Load() == 0 {
		t.Fatal("expected audit event on sidecar circuit breaker open")
	}

	// Verify loose UDS permissions (0666) fail VerifyUDSSocketPermissions.
	looseSock := filepath.Join(sockDir, "loose.sock")
	looseLn, err := ListenUDS(looseSock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = looseLn.Close() }()
	if err := os.Chmod(looseSock, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyUDSSocketPermissions(looseSock); !errors.Is(err, ErrUnsafeUDSSocket) {
		t.Fatalf("expected ErrUnsafeUDSSocket for 0666 UDS permissions, got %v", err)
	}
}
