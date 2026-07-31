//go:build windows

package shunter

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func lockDataDirFile(file *os.File, mode dataDirLeaseMode) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if mode == dataDirLeaseExclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errDataDirLockWouldBlock
	}
	return err
}

func unlockDataDirFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
}

func dataDirRegistryKey(canonicalPath string) string {
	return strings.ToLower(canonicalPath)
}
