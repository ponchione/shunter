package shunter

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BackupDataDir copies a stopped runtime's complete DataDir into outputPath.
// The source directory must exist and must not be a symlink. The output path
// must not already exist, and it must not be nested inside the source DataDir.
//
// BackupDataDir is an offline helper: callers must stop the runtime that owns
// dataDir before calling it. The helper does not quiesce, lock, or snapshot a
// running runtime.
func BackupDataDir(dataDir, outputPath string) error {
	src, err := cleanRequiredPath("data dir", dataDir)
	if err != nil {
		return err
	}
	dst, err := cleanRequiredPath("backup output", outputPath)
	if err != nil {
		return err
	}
	return copyOfflineDataDir(src, dst, "source data dir", "copy", false)
}

// RestoreDataDir copies a complete offline DataDir backup into dataDir.
// The backup directory must exist and must not be a symlink. The destination
// may be missing or empty, but RestoreDataDir refuses to merge backup contents
// into a non-empty directory.
//
// RestoreDataDir is an offline helper: callers must stop the runtime that owns
// dataDir before calling it.
func RestoreDataDir(backupPath, dataDir string) error {
	src, err := cleanRequiredPath("backup", backupPath)
	if err != nil {
		return err
	}
	dst, err := cleanRequiredPath("data dir", dataDir)
	if err != nil {
		return err
	}
	return copyOfflineDataDir(src, dst, "backup", "restore", true)
}

func cleanRequiredPath(label, path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("%s path is required", label)
	}
	return filepath.Clean(trimmed), nil
}

func copyOfflineDataDir(src, dst, sourceLabel, action string, allowEmptyDestination bool) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("read %s %s: %w", sourceLabel, src, err)
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s %s is a symlink; refusing to %s", sourceLabel, src, action)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("%s %s is not a directory", sourceLabel, src)
	}
	if err := rejectNestedCopy(src, dst); err != nil {
		return err
	}
	if allowEmptyDestination {
		if err := ensureMissingOrEmptyDir(dst); err != nil {
			return err
		}
	} else if err := requireMissingPath("backup output", dst); err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("create destination data dir %s: %w", dst, err)
	}
	if err := os.Chmod(dst, srcInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod destination data dir %s: %w", dst, err)
	}
	return copyDirectoryContents(src, dst)
}

func rejectNestedCopy(src, dst string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolve source path %s: %w", src, err)
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve destination path %s: %w", dst, err)
	}
	if sameOrNestedPath(srcAbs, dstAbs) {
		return fmt.Errorf("destination %s must not be inside source data dir %s", dst, src)
	}
	return nil
}

func sameOrNestedPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func requireMissingPath(label, path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s %s already exists", label, path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect %s %s: %w", label, path, err)
	}
	return nil
}

func ensureMissingOrEmptyDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect restore destination %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("restore destination %s is a symlink; refusing to restore", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("restore destination %s is not a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read restore destination %s: %w", path, err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("restore destination %s is not empty", path)
	}
	return nil
}

func copyDirectoryContents(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("resolve relative path for %s: %w", path, err)
		}
		target := filepath.Join(dst, rel)
		entryType := entry.Type()
		if entryType&os.ModeSymlink != 0 {
			return fmt.Errorf("source entry %s is a symlink; refusing to copy", path)
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat source entry %s: %w", path, err)
		}
		mode := info.Mode()
		switch {
		case entry.IsDir():
			if err := os.MkdirAll(target, mode.Perm()); err != nil {
				return fmt.Errorf("create destination directory %s: %w", target, err)
			}
			if err := os.Chmod(target, mode.Perm()); err != nil {
				return fmt.Errorf("chmod destination directory %s: %w", target, err)
			}
			return nil
		case mode.IsRegular():
			return copyRegularFile(path, target, mode.Perm())
		default:
			return fmt.Errorf("source entry %s has unsupported mode %s", path, mode)
		}
	})
}

func copyRegularFile(src, dst string, mode fs.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create destination file %s: %w", dst, err)
	}
	defer func() {
		if closeErr := out.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close destination file %s: %w", dst, closeErr)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync destination file %s: %w", dst, err)
	}
	return nil
}
