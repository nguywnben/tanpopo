// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package sqlite

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func detectRemoteFilesystem(dirPath string) (string, error) {
	vol := filepath.VolumeName(dirPath)
	if vol == "" {
		return "", nil
	}
	root := vol + `\`
	ptr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", err
	}
	if windows.GetDriveType(ptr) == windows.DRIVE_REMOTE {
		return "smb/remote-drive", nil
	}
	return "", nil
}
