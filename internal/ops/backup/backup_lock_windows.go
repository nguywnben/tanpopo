// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package backup

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockBackup(root *os.Root) (func(), error) {
	lock, err := root.OpenFile(".tanpopo-backup.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = lock.Close()
		return nil, errors.New("backup: output lock must be a regular file")
	}
	var overlapped windows.Overlapped
	err = windows.LockFileEx(windows.Handle(lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, &overlapped)
		_ = lock.Close()
	}, nil
}
