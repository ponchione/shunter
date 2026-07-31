//go:build !unix

package shunter

import (
	"fmt"
	"os"
)

func openDataDirMetadataFile(path string) (*os.File, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("metadata path is a symlink")
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("metadata path is not a regular file (mode %s)", before.Mode())
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("metadata path changed while opening")
	}
	return file, after, nil
}
