//go:build !linux

package backup

import (
	"errors"
	"os"
	"runtime"
)

func PlatformSupported() bool { return runtime.GOOS == "darwin" || runtime.GOOS == "windows" }

func openStagingRoot(*os.Root) (*os.Root, func() error, error) {
	return nil, nil, errors.New("backup: volatile staging filesystem is unsupported")
}
