// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package backup

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func renameRootDirectoryNoReplace(root *os.Root, source, destination string) error {
	rootInfo, err := root.Stat(".")
	if err != nil {
		return err
	}
	path := root.Name()
	pathInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(rootInfo, pathInfo) {
		return errors.New("backup: restore parent changed before atomic publication")
	}
	return windows.MoveFile(windows.StringToUTF16Ptr(filepath.Join(path, source)), windows.StringToUTF16Ptr(filepath.Join(path, destination)))
}
