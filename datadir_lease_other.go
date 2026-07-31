//go:build !unix && !windows

package shunter

import (
	"fmt"
	"os"
)

func lockDataDirFile(*os.File, dataDirLeaseMode) error {
	return fmt.Errorf("data dir advisory locks are unsupported on this platform")
}

func unlockDataDirFile(*os.File) error {
	return nil
}

func dataDirRegistryKey(canonicalPath string) string {
	return canonicalPath
}
