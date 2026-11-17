//go:build linux || darwin

package backup

import (
	"errors"
	"os"
	"syscall"
)

func lockBackup(root *os.Root) (func(), error) {
	unlock, locked, err := tryLockBackupMode(root, false)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("backup: failed to acquire output lock")
	}
	return unlock, nil
}

func tryLockBackup(root *os.Root) (func(), bool, error) {
	return tryLockBackupMode(root, true)
}

func tryLockBackupMode(root *os.Root, nonblocking bool) (func(), bool, error) {
	lock, err := root.OpenFile(".tanpopo-backup.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, false, err
	}
	info, err := lock.Stat()
	if err != nil {
		_ = lock.Close()
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		_ = lock.Close()
		return nil, false, errors.New("backup: output lock must be a regular file")
	}
	if err := lock.Chmod(0o600); err != nil {
		_ = lock.Close()
		return nil, false, err
	}
	flags := syscall.LOCK_EX
	if nonblocking {
		flags |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(lock.Fd()), flags); err != nil {
		_ = lock.Close()
		if nonblocking && (errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, true, nil
}
