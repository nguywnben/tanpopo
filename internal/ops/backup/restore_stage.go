// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"fmt"
	"os"
	"runtime"
)

func openRestoreScratch() (*os.Root, func(), error) {
	base, err := restoreScratchBase()
	if err != nil {
		return nil, nil, fmt.Errorf("backup: secure restore scratch unavailable: %w", err)
	}
	name, err := os.MkdirTemp(base, ".tanpopo-restore-")
	if err != nil {
		return nil, nil, fmt.Errorf("backup: create private restore scratch: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(name, 0o700); err != nil {
			_ = os.RemoveAll(name)
			return nil, nil, fmt.Errorf("backup: protect restore scratch: %w", err)
		}
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		_ = os.RemoveAll(name)
		return nil, nil, err
	}
	if err := secureOpenedOutputRoot(root); err != nil {
		_ = root.Close()
		_ = os.RemoveAll(name)
		return nil, nil, err
	}
	cleanup := func() {
		_ = root.Close()
		_ = os.RemoveAll(name)
	}
	return root, cleanup, nil
}
