//go:build !linux

package backup

import (
	"errors"
	"os"
)

func openStagingRoot() (*os.Root, error) {
	return nil, errors.New("backup: volatile staging filesystem is unsupported")
}
