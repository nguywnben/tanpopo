// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
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
	if err != nil {
		return false
	}
	info, err := key.Stat()
	if err != nil {
		return false
	}
	bootTime := time.Now().Add(-windows.DurationSinceBoot())
	return windowsPagingFileEncryptionApplied(value, valueType, info.ModTime(), bootTime)
}

func windowsPagingFileEncryptionApplied(value uint64, valueType uint32, registryModified, bootTime time.Time) bool {
	return valueType == registry.DWORD && value == 1 && registryModified.Before(bootTime.Add(-2*time.Minute))
}

func secureWindowsOutputRoot(root *os.Root) error {
	rootPath := root.Name()
	rootInfo, err := root.Stat(".")
	if err != nil {
		return err
	}
	// Deny new directory handles that can add, remove, or link files while
	// existing child ACLs are inspected and hardened. If a conflicting writable
	// handle already exists, fail closed before changing any child ACL.
	handle, err := windows.CreateFile(windows.StringToUTF16Ptr(rootPath), windows.WRITE_DAC|windows.WRITE_OWNER|windows.READ_CONTROL, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
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
	owner, err := currentWindowsServiceSID()
	if err != nil {
		return err
	}
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, acl, nil); err != nil {
		return err
	}
	return secureExistingWindowsChildren(root)
}

func secureExistingWindowsChildren(root *os.Root) error {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := root.Lstat(entry.Name())
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() || !info.Mode().IsRegular() {
			return fmt.Errorf("backup: unexpected non-regular entry %q in Windows backup directory", entry.Name())
		}
		if err := secureWindowsChild(root, entry.Name(), info); err != nil {
			return fmt.Errorf("backup: secure existing backup entry %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func secureWindowsChild(root *os.Root, name string, expected os.FileInfo) error {
	path := filepath.Join(root.Name(), name)
	handle, err := windows.CreateFile(windows.StringToUTF16Ptr(path), windows.WRITE_DAC|windows.WRITE_OWNER|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(handle), path)
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(expected, opened) {
		return errors.New("backup: existing output entry changed while securing its Windows ACL")
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 {
		return errors.New("backup: output files with hard links outside the backup directory are not allowed")
	}
	acl, err := privateWindowsACL(false)
	if err != nil {
		return err
	}
	owner, err := currentWindowsServiceSID()
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, acl, nil)
}

func secureOutputFile(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup: output archive must be a regular non-symlink file")
	}
	return secureWindowsChild(root, name, info)
}

func secureOutputRoot(root *os.Root) error { return secureWindowsOutputRoot(root) }

func currentWindowsServiceSID() (*windows.SID, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}

func privateWindowsACL(directory bool) (*windows.ACL, error) {
	serviceSID, err := currentWindowsServiceSID()
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
		{AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Inheritance: inherit, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeValue: windows.TrusteeValueFromSID(serviceSID)}},
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
