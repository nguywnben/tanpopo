//go:build !linux && !darwin

package backup

import (
	"errors"
	"os"
)

func lockBackup(_ *os.Root) (func(), error) {
	return nil, errors.New("backup: cross-process output locking is unsupported")
}
