//go:build linux

package backup

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
)

const linuxTmpfsMagic = 0x01021994

func openStagingRoot() (*os.Root, func() error, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/dev/shm", &stat); err != nil {
		return nil, nil, err
	}
	if uint64(stat.Type) != linuxTmpfsMagic {
		return nil, nil, errors.New("/dev/shm is not a volatile tmpfs")
	}
	if err := verifyNoActiveSwap(); err != nil {
		return nil, nil, err
	}
	shmRoot, err := os.OpenRoot("/dev/shm")
	if err != nil {
		return nil, nil, err
	}
	privateID, err := randomHexID()
	if err != nil {
		_ = shmRoot.Close()
		return nil, nil, err
	}
	privateName := fmt.Sprintf(".tanpopo-backup-%d-%s", os.Geteuid(), privateID)
	if err := shmRoot.Mkdir(privateName, 0o700); err != nil {
		_ = shmRoot.Close()
		return nil, nil, err
	}
	info, err := shmRoot.Lstat(privateName)
	if err != nil {
		_ = shmRoot.Remove(privateName)
		_ = shmRoot.Close()
		return nil, nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		_ = shmRoot.Remove(privateName)
		_ = shmRoot.Close()
		return nil, nil, errors.New("backup: private tmpfs staging directory must be a mode 0700 real directory")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		_ = shmRoot.Remove(privateName)
		_ = shmRoot.Close()
		return nil, nil, errors.New("backup: private tmpfs staging directory is owned by another user")
	}
	root, err := shmRoot.OpenRoot(privateName)
	if err != nil {
		_ = shmRoot.Remove(privateName)
		_ = shmRoot.Close()
		return nil, nil, err
	}
	openedInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(info, openedInfo) {
		_ = root.Close()
		_ = shmRoot.Remove(privateName)
		_ = shmRoot.Close()
		return nil, nil, errors.New("backup: private tmpfs staging directory changed during secure open")
	}
	var cleanupOnce sync.Once
	var cleanupErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			cleanupErr = errors.Join(root.Close(), shmRoot.Remove(privateName), shmRoot.Close())
		})
		return cleanupErr
	}
	return root, cleanup, nil
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
