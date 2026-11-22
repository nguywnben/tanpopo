// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !windows

package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

func TestEnsureTokenRejectsSharedOrWritableDir(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openBootstrapDB(t, fk)

	sharedDir := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(sharedDir, 0o777); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	mgr, err := NewManager(ManagerConfig{
		DB:        st.RawDB(),
		TokenPath: filepath.Join(sharedDir, "bootstrap.token"),
		AuditSink: testTxAuditSink{},
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := mgr.EnsureToken(context.Background()); !errors.Is(err, ErrInsecureTokenPermissions) {
		t.Fatalf("expected ErrInsecureTokenPermissions for group/world-writable dir, got %v", err)
	}

	info, statErr := os.Stat(sharedDir)
	if statErr != nil || info.Mode().Perm() != 0o777 {
		t.Fatalf("EnsureToken unexpectedly mutated shared dir permissions: perm=%v err=%v", info.Mode().Perm(), statErr)
	}
}
