// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package support

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const supportWindowsFileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

// createArchiveTemp creates the file with an explicit protected owner-only DACL,
// so no other account can open a temporary archive before diagnostic data is written.
func createArchiveTemp(directory string) (*os.File, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	user, err := token.GetTokenUser()
	_ = token.Close()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	if sid == "" {
		return nil, errors.New("support: resolve current Windows account SID")
	}
	sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf("O:%sD:P(A;;FA;;;%s)", sid, sid))
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	for attempt := 0; attempt < 10; attempt++ {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(directory, ".tanpopo-support-"+hex.EncodeToString(nonce[:])+".tmp")
		path, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, err
		}
		handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER|windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_TEMPORARY, 0)
		if err == windows.ERROR_FILE_EXISTS || err == windows.ERROR_ALREADY_EXISTS {
			continue
		}
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(handle), name)
		if file == nil {
			_ = windows.CloseHandle(handle)
			return nil, errors.New("support: open created archive handle")
		}
		return file, nil
	}
	return nil, errors.New("support: could not allocate a unique temporary archive name")
}

func secureArchiveFile(file *os.File) error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return err
	}
	user, err := token.GetTokenUser()
	_ = token.Close()
	if err != nil {
		return err
	}
	ownerSID := user.User.Sid
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if !owner.Equals(ownerSID) {
		return errors.New("support: temporary archive owner does not match current account")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if dacl == nil || dacl.AceCount != 1 {
		return errors.New("support: temporary archive must have a single owner-only DACL entry")
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		return err
	}
	aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != 0 || ace.Mask != supportWindowsFileAllAccess || !aceSID.Equals(ownerSID) {
		return errors.New("support: temporary archive DACL is not restricted to the owner")
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("support: temporary archive DACL inherits external permissions")
	}
	return nil
}
