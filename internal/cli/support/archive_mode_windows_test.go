// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package support

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsSupportArchiveUsesProtectedOwnerOnlyACL(t *testing.T) {
	file, err := createArchiveTemp(t.TempDir())
	if err != nil {
		t.Fatalf("create owner-only archive: %v", err)
	}
	path := file.Name()
	if err := secureArchiveFile(file); err != nil {
		_ = file.Close()
		t.Fatalf("secure archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	file, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	user, err := token.GetTokenUser()
	_ = token.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Equals(user.User.Sid) {
		t.Fatal("archive owner differs from the current account")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("DACL ACE count=%v, want one owner ACE", dacl)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != 0 || ace.Mask != supportWindowsFileAllAccess {
		t.Fatalf("unexpected ACE type=%d flags=%#x mask=%#x", ace.Header.AceType, ace.Header.AceFlags, ace.Mask)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !sid.Equals(owner) {
		t.Fatal("archive grants access to a SID other than its owner")
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("archive DACL is inheritable")
	}
}

func TestWindowsSupportBundleOutputRetainsOwnerOnlyACL(t *testing.T) {
	root := t.TempDir()
	output := t.TempDir() + `\diagnostics.tar.gz`
	if err := CreateBundle(t.Context(), root, output); err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	file, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := secureArchiveFile(file); err != nil {
		t.Fatalf("published archive ACL: %v", err)
	}
}
