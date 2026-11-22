// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !windows

package bootstrap

import (
	"fmt"
	"os"
	"syscall"
)

func verifyOwnerMatch(info os.FileInfo) error {
	if info == nil {
		return ErrInsecureTokenPermissions
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if st.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("%w: owner uid %d does not match process euid %d", ErrInsecureTokenPermissions, st.Uid, os.Geteuid())
	}
	return nil
}
