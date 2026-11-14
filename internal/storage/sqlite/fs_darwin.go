// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build darwin

package sqlite

import (
	"bytes"
	"strings"
	"syscall"
)

func detectRemoteFilesystem(dirPath string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dirPath, &st); err != nil {
		return "", err
	}
	raw := make([]byte, len(st.Fstypename))
	for i, b := range st.Fstypename {
		raw[i] = byte(b)
	}
	fsName := strings.ToLower(strings.TrimRight(string(bytes.TrimRight(raw, "\x00")), "\x00"))
	switch fsName {
	case "nfs", "smbfs", "cifs", "afpfs", "webdav":
		return fsName, nil
	default:
		return "", nil
	}
}
