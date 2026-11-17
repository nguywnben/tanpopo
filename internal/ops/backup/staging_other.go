//go:build !linux

package backup

import (
	"errors"
	"os"
)

func openStagingRoot() (*os.Root, func() error, error) {
	return nil, nil, errors.New("backup: volatile staging filesystem is unsupported")
}
