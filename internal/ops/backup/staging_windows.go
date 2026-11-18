// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package backup

func PlatformSupported() bool {
	return windowsPagingFileEncryptionEnabled()
}

func PlatformSupportUnavailableReason() string {
	return "Encrypted backups on Windows require NTFS paging-file encryption to be enabled before the last system boot."
}
