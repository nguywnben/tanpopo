//go:build linux

package backup

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
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
	if err := cleanupAbandonedStagingRoots(shmRoot); err != nil {
		_ = shmRoot.Close()
		return nil, nil, err
	}
	privateID, err := randomHexID()
	if err != nil {
		_ = shmRoot.Close()
		return nil, nil, err
	}
	privateName := fmt.Sprintf(".tanpopo-backup-%d-%d-%s", os.Geteuid(), os.Getpid(), privateID)
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

func cleanupAbandonedStagingRoots(root *os.Root) error {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf(".tanpopo-backup-%d-", os.Geteuid())
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		pidText, token, ok := strings.Cut(rest, "-")
		if !ok || len(token) != 32 {
			continue
		}
		pid, err := strconv.Atoi(pidText)
		if err != nil || pid <= 0 {
			continue
		}
		if _, err := hex.DecodeString(token); err != nil {
			continue
		}
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
			continue
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Geteuid()) {
			continue
		}
		if err := syscall.Kill(pid, 0); err == nil || errors.Is(err, syscall.EPERM) {
			continue
		} else if !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("backup: inspect staging owner process %d: %w", pid, err)
		}
		if err := removeTree(root, name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("backup: remove abandoned staging directory %q: %w", name, err)
		}
	}
	return nil
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
