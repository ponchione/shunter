// Package atomicfile provides durable filesystem publication helpers for
// same-directory file replacement and recursive directory creation.
package atomicfile

import (
	"errors"
	"fmt"
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

// MkdirAllDurable creates each missing directory in path and synchronizes its
// containing directory before creating the next component. syncDir may be
// supplied by callers that need fault injection; nil uses SyncDir.
//
// When path is partially present, the containing directory of the deepest
// existing component is synchronized first. When the complete path already
// exists, its containing directory is synchronized. Those repairs cover the
// only unsynced directory entry a prior failed ordered invocation can leave
// behind before the path is accepted or extended.
func MkdirAllDurable(path string, perm os.FileMode, syncDir func(string) error) error {
	cleanPath := filepath.Clean(path)
	missing := make([]string, 0)
	existing := cleanPath
	for {
		info, err := os.Stat(existing)
		if err == nil {
			if !info.IsDir() {
				return &os.PathError{Op: "mkdir", Path: existing, Err: fmt.Errorf("not a directory")}
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, existing)
		parent := filepath.Dir(existing)
		if parent == existing {
			return err
		}
		existing = parent
	}
	if syncDir == nil {
		syncDir = SyncDir
	}
	if len(missing) == 0 {
		parent := filepath.Dir(cleanPath)
		if parent == cleanPath {
			return nil
		}
		if err := syncDir(parent); err != nil {
			return fmt.Errorf("sync containing directory %s for existing directory %s: %w", parent, cleanPath, err)
		}
		return nil
	}

	// The deepest existing component can be residue from a prior invocation
	// whose containing-directory sync failed. Repair its publication before
	// placing another directory beneath it.
	existingParent := filepath.Dir(existing)
	if existingParent != existing {
		if err := syncDir(existingParent); err != nil {
			return fmt.Errorf("sync containing directory %s for existing directory %s: %w", existingParent, existing, err)
		}
	}

	for i := len(missing) - 1; i >= 0; i-- {
		component := missing[i]
		if err := os.Mkdir(component, perm); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return err
			}
			info, statErr := os.Stat(component)
			if statErr != nil {
				return statErr
			}
			if !info.IsDir() {
				return &os.PathError{Op: "mkdir", Path: component, Err: fmt.Errorf("not a directory")}
			}
		}
		parent := filepath.Dir(component)
		if err := syncDir(parent); err != nil {
			return fmt.Errorf("sync containing directory %s for new directory %s: %w", parent, component, err)
		}
	}
	return nil
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
