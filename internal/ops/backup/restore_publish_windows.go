// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package backup

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func renameRootDirectoryNoReplace(root *os.Root, source, destination string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	dirConn, err := dir.SyscallConn()
	if err != nil {
		return err
	}
	var dirHandle windows.Handle
	if err := dirConn.Control(func(fd uintptr) { dirHandle = windows.Handle(fd) }); err != nil {
		return err
	}
	sourceName, err := windows.NewNTUnicodeString(source)
	if err != nil {
		return err
	}
	oa := &windows.OBJECT_ATTRIBUTES{Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: dirHandle, ObjectName: sourceName}
	var iosb windows.IO_STATUS_BLOCK
	var sourceHandle windows.Handle
	err = windows.NtCreateFile(&sourceHandle, windows.DELETE|windows.SYNCHRONIZE, oa, &iosb, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(sourceHandle)

	name, err := windows.UTF16FromString(destination)
	if err != nil {
		return err
	}
	nameBytes := (len(name) - 1) * 2
	if nameBytes <= 0 || nameBytes > windows.MAX_LONG_PATH*2 {
		return windows.ERROR_FILENAME_EXCED_RANGE
	}
	var layout struct {
		Flags          uint32
		RootDirectory  windows.Handle
		FileNameLength uint32
		FileName       [1]uint16
	}
	nameOffset := int(unsafe.Offsetof(layout.FileName))
	buffer := make([]byte, nameOffset+nameBytes)
	info := (*struct {
		Flags          uint32
		RootDirectory  windows.Handle
		FileNameLength uint32
		FileName       [1]uint16
	})(unsafe.Pointer(&buffer[0]))
	info.RootDirectory = dirHandle
	info.FileNameLength = uint32(nameBytes)
	copy(unsafe.Slice(&info.FileName[0], len(name)-1), name[:len(name)-1])
	return windows.SetFileInformationByHandle(sourceHandle, windows.FileRenameInfoEx, &buffer[0], uint32(len(buffer)))
}
