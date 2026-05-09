// Package atomicfile writes same-directory replacement files with durable
// rename publication.
package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
)

// SyncDir fsyncs a directory so a previous rename in that directory is durable.
func SyncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Options controls WriteFile replacement behavior.
type Options struct {
	// Mode is the final file mode when PreserveMode is false or the destination
	// does not exist. A zero mode leaves the temp file's default 0600 mode.
	Mode os.FileMode

	// PreserveMode keeps the destination file's permission bits when replacing
	// an existing file.
	PreserveMode bool

	// TempPattern is passed to os.CreateTemp. Empty uses ".<base>.tmp-*".
	TempPattern string

	// SyncDir is called after the replacement rename. Empty uses this package's
	// SyncDir function.
	SyncDir func(string) error
}

// WriteFile atomically replaces path with data, fsyncing both the file and the
// parent directory before returning.
func WriteFile(path string, data []byte, opts Options) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tempPattern := opts.TempPattern
	if tempPattern == "" {
		tempPattern = "." + base + ".tmp-*"
	}

	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	mode := opts.Mode
	if opts.PreserveMode {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = tmp.Close()
			return err
		}
	}
	if mode != 0 {
		if err := tmp.Chmod(mode); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	removeTemp = false

	syncDir := opts.SyncDir
	if syncDir == nil {
		syncDir = SyncDir
	}
	return syncDir(dir)
}
