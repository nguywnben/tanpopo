// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !windows

package backup

import (
	"os"

	"golang.org/x/sys/unix"
)

func secureOutputRoot(*os.Root) error         { return nil }
func secureOutputFile(*os.Root, string) error { return nil }

func publishMemoryArchive(root *os.Root, tempName, finalName string) error {
	pinnedDir, _, err := pathForOpenRoot(root)
	if err != nil {
		return err
	}
	defer pinnedDir.Close()
	directoryFD := int(pinnedDir.Fd())
	if err := unix.Linkat(directoryFD, tempName, directoryFD, finalName, 0); err != nil {
		return err
	}
	if err := root.Remove(tempName); err != nil {
		_ = root.Remove(finalName)
		return err
	}
	return nil
}
