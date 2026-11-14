// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build linux

package sqlite

import "syscall"

// Linux statfs magic constants for network/cluster filesystems where SQLite WAL locking is unsafe.
const (
	magicNFS  = 0x6969
	magicSMB  = 0x517b
	magicSMB2 = 0xfe534d42
	magicCIFS = 0xff534d42
	magicAFS  = 0x5346414f
	magicCeph = 0x00c36400
	magic9P   = 0x01021997
)

func detectRemoteFilesystem(dirPath string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dirPath, &st); err != nil {
		return "", err
	}
	switch uint64(uint32(st.Type)) {
	case magicNFS:
		return "nfs", nil
	case magicSMB:
		return "smb", nil
	case magicSMB2:
		return "smb2", nil
	case magicCIFS:
		return "cifs", nil
	case magicAFS:
		return "afs", nil
	case magicCeph:
		return "ceph", nil
	case magic9P:
		return "9p", nil
	default:
		return "", nil
	}
}
