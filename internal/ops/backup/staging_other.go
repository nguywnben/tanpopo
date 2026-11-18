//go:build !linux && !windows

package backup

import (
	"runtime"
)

func PlatformSupported() bool {
	return runtime.GOOS == "darwin"
}

func PlatformSupportUnavailableReason() string {
	return "Encrypted backups are unsupported on this operating system."
}
