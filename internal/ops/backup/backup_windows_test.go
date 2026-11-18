// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestWindowsOutputACLProtectsExistingArchiveAndDirectory(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "tanpopo-backup-existing.tpop.enc")
	if err := os.WriteFile(archivePath, []byte("ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openOutputRoot(dir)
	if err != nil {
		t.Fatalf("secure output root and existing archive: %v", err)
	}
	defer root.Close()
	for _, path := range []string{dir, archivePath} {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION)
		_ = file.Close()
		if err != nil {
			t.Fatalf("read DACL for %q: %v", path, err)
		}
		dacl, _, err := sd.DACL()
		if err != nil {
			t.Fatalf("read DACL entries for %q: %v", path, err)
		}
		if dacl == nil || dacl.AceCount != 2 {
			t.Fatalf("%q DACL has %v ACEs; want exactly service-account and SYSTEM ACEs", path, dacl)
		}
	}
}

func TestWindowsOutputRootRejectsHardLinksToFilesOutsideDirectory(t *testing.T) {
	parent := t.TempDir()
	backupDir := filepath.Join(parent, "backups")
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	externalFile := filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(externalFile, []byte("outside data"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(backupDir, "linked-archive.tpop.enc")
	if err := windows.CreateHardLink(windows.StringToUTF16Ptr(linkPath), windows.StringToUTF16Ptr(externalFile), 0); err != nil {
		t.Fatalf("create hard link on the Windows CI filesystem: %v", err)
	}
	if root, err := openOutputRoot(backupDir); err == nil {
		_ = root.Close()
		t.Fatal("output root accepted an archive hard-linked outside the backup directory")
	}
	file, err := os.Open(externalFile)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		t.Fatal(err)
	}
	if info.NumberOfLinks != 2 {
		t.Fatalf("external file link count=%d; want 2", info.NumberOfLinks)
	}
}

func TestWindowsPagingFileEncryptionMustPrecedeCurrentBoot(t *testing.T) {
	boot := time.Date(2026, 11, 18, 4, 0, 0, 0, time.UTC)
	if !windowsPagingFileEncryptionApplied(1, registry.DWORD, boot.Add(-10*time.Minute), boot) {
		t.Fatal("pagefile encryption configured before boot should be considered active")
	}
	for _, tc := range []struct {
		name      string
		value     uint64
		valueType uint32
		modified  time.Time
	}{
		{name: "changed after boot", value: 1, valueType: registry.DWORD, modified: boot.Add(time.Minute)},
		{name: "disabled", value: 0, valueType: registry.DWORD, modified: boot.Add(-time.Hour)},
		{name: "wrong type", value: 1, valueType: registry.SZ, modified: boot.Add(-time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if windowsPagingFileEncryptionApplied(tc.value, tc.valueType, tc.modified, boot) {
				t.Fatal("unapplied or invalid pagefile-encryption setting was accepted")
			}
		})
	}
}
