//go:build !linux

package backup

import (
	"errors"
	"os"
	"runtime"
)

func PlatformSupported() bool {
	switch runtime.GOOS {
	case "darwin":
		return true
	case "windows":
		return windowsPagingFileEncryptionEnabled()
	default:
		return false
	}
}

func PlatformSupportUnavailableReason() string {
	if runtime.GOOS == "windows" {
		return "Encrypted backups on Windows require NTFS paging-file encryption to be enabled."
	}
	return "Encrypted backups are unsupported on this operating system."
}

func openStagingRoot(*os.Root) (*os.Root, func() error, error) {
	return nil, nil, errors.New("backup: volatile staging filesystem is unsupported")
}
