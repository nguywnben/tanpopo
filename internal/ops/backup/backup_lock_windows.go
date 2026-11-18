// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package backup

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
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

func windowsPagingFileEncryptionEnabled() bool {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\FileSystem`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, valueType, err := key.GetIntegerValue("NtfsEncryptPagingFile")
	return err == nil && valueType == registry.DWORD && value == 1
}

func secureWindowsOutputRoot(root *os.Root) error {
	rootPath := root.Name()
	rootInfo, err := root.Stat(".")
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(windows.StringToUTF16Ptr(rootPath), windows.WRITE_DAC|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(handle), rootPath)
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(rootInfo, opened) {
		return errors.New("backup: output directory changed while securing its Windows ACL")
	}
	acl, err := privateWindowsACL(true)
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

func secureOutputRoot(root *os.Root) error { return secureWindowsOutputRoot(root) }

func privateWindowsACL(directory bool) (*windows.ACL, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	inherit := uint32(0)
	if directory {
		inherit = windows.CONTAINER_INHERIT_ACE | windows.OBJECT_INHERIT_ACE
	}
	entries := []windows.EXPLICIT_ACCESS{
		{AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Inheritance: inherit, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid)}},
		{AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Inheritance: inherit, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeValue: windows.TrusteeValueFromSID(systemSID)}},
	}
	return windows.ACLFromEntries(entries, nil)
}

func publishMemoryArchive(root *os.Root, tempName, finalName string) error {
	name, err := windows.NewNTUnicodeString(tempName)
	if err != nil {
		return err
	}
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
	oa := &windows.OBJECT_ATTRIBUTES{Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: dirHandle, ObjectName: name}
	var iosb windows.IO_STATUS_BLOCK
	var fileHandle windows.Handle
	err = windows.NtCreateFile(&fileHandle, windows.DELETE|windows.SYNCHRONIZE, oa, &iosb, nil, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(fileHandle)
	finalUTF16, err := windows.UTF16FromString(finalName)
	if err != nil {
		return err
	}
	nameBytes := (len(finalUTF16) - 1) * 2
	var layout struct {
		ReplaceIfExists uint32
		RootDirectory   windows.Handle
		FileNameLength  uint32
		FileName        [1]uint16
	}
	nameOffset := int(unsafe.Offsetof(layout.FileName))
	buffer := make([]byte, nameOffset+nameBytes)
	info := (*struct {
		ReplaceIfExists uint32
		RootDirectory   windows.Handle
		FileNameLength  uint32
		FileName        [1]uint16
	})(unsafe.Pointer(&buffer[0]))
	info.RootDirectory = dirHandle
	info.FileNameLength = uint32(nameBytes)
	copy((*[windows.MAX_LONG_PATH]uint16)(unsafe.Pointer(&info.FileName[0]))[:len(finalUTF16)-1], finalUTF16[:len(finalUTF16)-1])
	return windows.NtSetInformationFile(fileHandle, &iosb, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation)
}
