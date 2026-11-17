//go:build linux

package backup

import (
	"errors"
	"os"
	"syscall"
)

const linuxTmpfsMagic = 0x01021994

func openStagingRoot() (*os.Root, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/dev/shm", &stat); err != nil {
		return nil, err
	}
	if uint64(stat.Type) != linuxTmpfsMagic {
		return nil, errors.New("/dev/shm is not a volatile tmpfs")
	}
	root, err := os.OpenRoot("/dev/shm")
	if err != nil {
		return nil, err
	}
	return root, nil
}
