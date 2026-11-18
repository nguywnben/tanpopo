// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !windows

package backup

import "os"

func secureOutputRoot(*os.Root) error { return nil }

func publishMemoryArchive(root *os.Root, tempName, finalName string) error {
	pinnedDir, rootPath, err := pathForOpenRoot(root)
	if err != nil {
		return err
	}
	defer pinnedDir.Close()
	return renameRootEntry(rootPath, tempName, finalName)
}
