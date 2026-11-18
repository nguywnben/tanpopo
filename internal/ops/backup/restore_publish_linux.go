// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build linux

package backup

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func renameRootDirectoryNoReplace(root *os.Root, source, destination string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	conn, err := dir.SyscallConn()
	if err != nil {
		return err
	}
	var fd uintptr
	if err := conn.Control(func(raw uintptr) { fd = raw }); err != nil {
		return err
	}
	if fd == 0 {
		return errors.New("backup: destination parent handle is invalid")
	}
	return unix.Renameat2(int(fd), source, int(fd), destination, unix.RENAME_NOREPLACE)
}
