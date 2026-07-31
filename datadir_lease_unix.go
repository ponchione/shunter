//go:build unix && !aix

package shunter

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockDataDirFile(file *os.File, mode dataDirLeaseMode) error {
	how := unix.LOCK_SH | unix.LOCK_NB
	if mode == dataDirLeaseExclusive {
		how = unix.LOCK_EX | unix.LOCK_NB
	}
	if err := unix.Flock(int(file.Fd()), how); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return errDataDirLockWouldBlock
		}
		return err
	}
	return nil
}

func unlockDataDirFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func dataDirRegistryKey(canonicalPath string) string {
	return canonicalPath
}
