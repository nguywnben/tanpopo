//go:build linux

package backup

import (
	"errors"
	"fmt"
	"os"
	"strings"
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
	if err := verifyNoActiveSwap(); err != nil {
		return nil, err
	}
	shmRoot, err := os.OpenRoot("/dev/shm")
	if err != nil {
		return nil, err
	}
	defer shmRoot.Close()
	privateName := fmt.Sprintf("tanpopo-backup-%d", os.Geteuid())
	info, err := shmRoot.Lstat(privateName)
	if errors.Is(err, os.ErrNotExist) {
		if err := shmRoot.Mkdir(privateName, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, err = shmRoot.Lstat(privateName)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return nil, errors.New("backup: private tmpfs staging directory must be a mode 0700 real directory")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("backup: private tmpfs staging directory is owned by another user")
	}
	root, err := shmRoot.OpenRoot(privateName)
	if err != nil {
		return nil, err
	}
	openedInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(info, openedInfo) {
		_ = root.Close()
		return nil, errors.New("backup: private tmpfs staging directory changed during secure open")
	}
	return root, nil
}

func verifyNoActiveSwap() error {
	data, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return fmt.Errorf("backup: verify swap is disabled for plaintext staging: %w", err)
	}
	if len(strings.Split(strings.TrimSpace(string(data)), "\n")) > 1 {
		return errors.New("backup: plaintext staging requires swap to be disabled")
	}
	return nil
}
