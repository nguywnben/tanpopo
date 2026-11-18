//go:build linux

package backup

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

const linuxTmpfsMagic = 0x01021994

// PlatformSupported reports whether the operating system has the verified
// volatile staging primitives required for encrypted backup creation.
func PlatformSupported() bool {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/dev/shm", &stat); err != nil {
		return false
	}
	return verifyNoActiveSwap() == nil && verifyVolatileStagingPreconditions(uint64(stat.Type), nil, true) == nil
}

func PlatformSupportUnavailableReason() string {
	return "Encrypted backups require verified /dev/shm tmpfs staging with swap disabled."
}

func openStagingRoot(outputRoot *os.Root) (*os.Root, func() error, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/dev/shm", &stat); err != nil {
		return nil, nil, err
	}
	swapData, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return nil, nil, fmt.Errorf("backup: verify swap is disabled for plaintext staging: %w", err)
	}
	if err := verifyVolatileStagingPreconditions(uint64(stat.Type), swapData, false); err != nil {
		return nil, nil, err
	}
	outputInfo, err := outputRoot.Stat(".")
	if err != nil {
		return nil, nil, err
	}
	identity, ok := outputInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, nil, errors.New("backup: output directory identity is unavailable")
	}
	namespace := fmt.Sprintf("%x-%x", uint64(identity.Dev), identity.Ino)
	shmRoot, err := os.OpenRoot("/dev/shm")
	if err != nil {
		return nil, nil, err
	}
	if err := cleanupAbandonedStagingRoots(shmRoot, namespace); err != nil {
		_ = shmRoot.Close()
		return nil, nil, err
	}
	privateID, err := randomHexID()
	if err != nil {
		_ = shmRoot.Close()
		return nil, nil, err
	}
	privateName := fmt.Sprintf(".tanpopo-backup-%d-%s-%s", os.Geteuid(), namespace, privateID)
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
	cleanup := func() error {
		return errors.Join(root.Close(), shmRoot.Remove(privateName), shmRoot.Close())
	}
	return root, cleanup, nil
}

func cleanupAbandonedStagingRoots(root *os.Root, namespace string) error {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf(".tanpopo-backup-%d-%s-", os.Geteuid(), namespace)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		token := strings.TrimPrefix(name, prefix)
		if len(token) != 32 {
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
		stageRoot, err := root.OpenRoot(name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		children, err := fs.ReadDir(stageRoot.FS(), ".")
		if err == nil {
			for _, child := range children {
				if err := removeTree(stageRoot, child.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
					_ = stageRoot.Close()
					return fmt.Errorf("backup: clean abandoned staging entry %q: %w", child.Name(), err)
				}
			}
		}
		if err != nil {
			_ = stageRoot.Close()
			return err
		}
		if err := stageRoot.Close(); err != nil {
			return err
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
	return verifyProcSwapsDisabled(data)
}

func verifyProcSwapsDisabled(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return errors.New("backup: /proc/swaps is empty; cannot verify swap is disabled for plaintext staging")
	}
	if len(strings.Split(trimmed, "\n")) > 1 {
		return errors.New("backup: plaintext staging requires swap to be disabled")
	}
	return nil
}

func verifyVolatileStagingPreconditions(fsType uint64, swapData []byte, skipSwapCheck bool) error {
	if fsType != linuxTmpfsMagic {
		return errors.New("/dev/shm is not a volatile tmpfs")
	}
	if skipSwapCheck {
		return nil
	}
	return verifyProcSwapsDisabled(swapData)
}
