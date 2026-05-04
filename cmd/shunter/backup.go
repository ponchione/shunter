package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func runBackup(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter backup")
	dataDir := fs.String("data-dir", "", "offline runtime DataDir to copy")
	outputPath := fs.String("out", "", "backup output directory; must not already exist")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if code := requirePath(stderr, "data-dir", *dataDir); code != 0 {
		return code
	}
	if code := requirePath(stderr, "out", *outputPath); code != 0 {
		return code
	}

	if err := copyOfflineDataDir(*dataDir, *outputPath, false); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	writeCLIStatusf(stdout, "backed up %s to %s\n", strings.TrimSpace(*dataDir), strings.TrimSpace(*outputPath))
	return 0
}

func runRestore(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter restore")
	backupPath := fs.String("backup", "", "backup directory to restore")
	dataDir := fs.String("data-dir", "", "restore destination DataDir; must not contain existing state")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if code := requirePath(stderr, "backup", *backupPath); code != 0 {
		return code
	}
	if code := requirePath(stderr, "data-dir", *dataDir); code != 0 {
		return code
	}

	if err := copyOfflineDataDir(*backupPath, *dataDir, true); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	writeCLIStatusf(stdout, "restored %s to %s\n", strings.TrimSpace(*backupPath), strings.TrimSpace(*dataDir))
	return 0
}

func copyOfflineDataDir(srcPath, dstPath string, allowEmptyDestination bool) error {
	src := filepath.Clean(strings.TrimSpace(srcPath))
	dst := filepath.Clean(strings.TrimSpace(dstPath))

	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("read source data dir %s: %w", src, err)
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source data dir %s is a symlink; refusing to copy", src)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source data dir %s is not a directory", src)
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
