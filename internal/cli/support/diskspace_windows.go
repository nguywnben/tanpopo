// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package support

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func diskFreeBytes(path string) (uint64, error) {
	wide, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(wide, &available, nil, nil); err != nil {
		return 0, err
	}
	return available, nil
}
