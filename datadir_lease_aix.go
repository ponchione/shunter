//go:build aix

package shunter

import (
	"errors"
	"os"
	"syscall"
)

func lockDataDirFile(file *os.File, mode dataDirLeaseMode) error {
	lockType := int16(syscall.F_RDLCK)
	if mode == dataDirLeaseExclusive {
		lockType = syscall.F_WRLCK
	}
	lock := syscall.Flock_t{Type: lockType, Whence: 0, Start: 0, Len: 0}
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &lock); err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EAGAIN) {
			return errDataDirLockWouldBlock
		}
		return err
	}
	return nil
}

func unlockDataDirFile(file *os.File) error {
	lock := syscall.Flock_t{Type: syscall.F_UNLCK, Whence: 0, Start: 0, Len: 0}
	return syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &lock)
}

func dataDirRegistryKey(canonicalPath string) string {
	return canonicalPath
}
