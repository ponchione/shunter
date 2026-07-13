package shunter

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ponchione/shunter/internal/atomicfile"
)

// BackupDataDir transactionally copies a stopped runtime's complete DataDir
// into outputPath. The output becomes visible only after the complete staged
// copy is durable.
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

// RestoreDataDir transactionally copies a complete offline DataDir backup into
// dataDir. The restored contents become visible only after the complete staged
// copy is durable.
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

func copyOfflineDataDir(src, dst, sourceLabel, action string, allowEmptyDestination bool) (retErr error) {
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
	destinationWasEmpty := false
	var destinationMode fs.FileMode
	if allowEmptyDestination {
		info, exists, err := inspectMissingOrEmptyDir(dst)
		if err != nil {
			return err
		}
		if exists {
			destinationWasEmpty = true
			destinationMode = info.Mode().Perm()
		}
	} else if err := requireMissingPath("backup output", dst); err != nil {
		return err
	}

	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create destination parent directory %s: %w", parent, err)
	}
	staging, err := makeOfflineCopyStagingDir(parent, "."+filepath.Base(dst)+".staging-*")
	if err != nil {
		return fmt.Errorf("create staging data dir beside %s: %w", dst, err)
	}
	removeStaging := true
	defer func() {
		if removeStaging {
			if err := removeOfflineCopyTreeForCleanup(staging); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("remove staging data dir %s: %w", staging, err))
			} else if err := syncOfflineCopyDir(parent); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("sync removal of staging data dir %s: %w", staging, err))
			}
		}
	}()
	if err := copyDirectoryContents(src, staging, srcInfo); err != nil {
		return err
	}
	if err := syncOfflineCopyTree(staging); err != nil {
		return fmt.Errorf("sync staged data dir %s: %w", staging, err)
	}

	if destinationWasEmpty {
		if err := removeOfflineCopyEmpty(dst); err != nil {
			return fmt.Errorf("remove empty restore destination %s before publication: %w", dst, err)
		}
		if err := syncOfflineCopyDir(parent); err != nil {
			rollbackErr := restoreEmptyOfflineCopyDestination(dst, destinationMode, parent)
			return errors.Join(fmt.Errorf("sync removal of empty restore destination %s: %w", dst, err), rollbackErr)
		}
	} else {
		label := "destination"
		if !allowEmptyDestination {
			label = "backup output"
		}
		if err := requireMissingPath(label, dst); err != nil {
			return err
		}
	}

	if err := renameOfflineCopy(staging, dst); err != nil {
		var rollbackErr error
		if destinationWasEmpty {
			rollbackErr = restoreEmptyOfflineCopyDestination(dst, destinationMode, parent)
		}
		return errors.Join(fmt.Errorf("publish staged data dir %s: %w", dst, err), rollbackErr)
	}
	removeStaging = false
	if err := syncOfflineCopyDir(parent); err != nil {
		removeStaging = true
		rollbackErr := renameOfflineCopy(dst, staging)
		if rollbackErr == nil {
			rollbackErr = syncOfflineCopyDir(parent)
		} else {
			removeErr := removeOfflineCopyTreeForCleanup(dst)
			if removeErr == nil {
				removeErr = syncOfflineCopyDir(parent)
			}
			rollbackErr = errors.Join(rollbackErr, removeErr)
			removeStaging = false
		}
		if destinationWasEmpty {
			rollbackErr = errors.Join(rollbackErr, restoreEmptyOfflineCopyDestination(dst, destinationMode, parent))
		}
		return errors.Join(fmt.Errorf("sync published data dir %s: %w", dst, err), rollbackErr)
	}
	return nil
}

var (
	makeOfflineCopyStagingDir = os.MkdirTemp
	chmodOfflineCopyPath      = os.Chmod
	removeOfflineCopyEmpty    = os.Remove
	removeOfflineCopyTree     = os.RemoveAll
	renameOfflineCopy         = os.Rename
	syncOfflineCopyDir        = atomicfile.SyncDir
)

func restoreEmptyOfflineCopyDestination(dst string, mode fs.FileMode, parent string) error {
	if err := os.Mkdir(dst, mode); err != nil {
		return fmt.Errorf("restore original empty destination %s: %w", dst, err)
	}
	if err := syncOfflineCopyDir(parent); err != nil {
		return fmt.Errorf("sync restored empty destination %s: %w", dst, err)
	}
	return nil
}

func rejectNestedCopy(src, dst string) error {
	srcAbs, err := resolvePathForContainment(src)
	if err != nil {
		return fmt.Errorf("resolve source path %s: %w", src, err)
	}
	dstAbs, err := resolvePathForContainment(dst)
	if err != nil {
		return fmt.Errorf("resolve destination path %s: %w", dst, err)
	}
	if sameOrNestedPath(srcAbs, dstAbs) {
		return fmt.Errorf("destination %s must not be inside source data dir %s", dst, src)
	}
	return nil
}

func resolvePathForContainment(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	existing := filepath.Clean(abs)
	var missing []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			resolved, err := filepath.EvalSymlinks(existing)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fs.ErrNotExist
		}
		missing = append(missing, filepath.Base(existing))
		existing = parent
	}
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

func inspectMissingOrEmptyDir(path string) (fs.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect restore destination %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("restore destination %s is a symlink; refusing to restore", path)
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("restore destination %s is not a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, fmt.Errorf("read restore destination %s: %w", path, err)
	}
	if len(entries) != 0 {
		return nil, false, fmt.Errorf("restore destination %s is not empty", path)
	}
	return info, true, nil
}

type offlineCopyDirectoryMode struct {
	path string
	mode fs.FileMode
}

func copyDirectoryContents(src, dst string, rootInfo fs.FileInfo) error {
	expected := map[string]fs.FileInfo{".": rootInfo}
	directoryModes := []offlineCopyDirectoryMode{{path: dst, mode: rootInfo.Mode().Perm()}}
	if err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("stat source entry %s: %w", path, err)
			}
			if !sameFileSnapshot(rootInfo, info) {
				return fmt.Errorf("source entry %s changed while copying; refusing to copy", path)
			}
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
		expected[rel] = info
		switch {
		case entry.IsDir():
			// Keep every staged directory owner-private and writable until all
			// children have been copied and the source tree has been verified.
			if err := os.Mkdir(target, 0o700); err != nil {
				return fmt.Errorf("create destination directory %s: %w", target, err)
			}
			directoryModes = append(directoryModes, offlineCopyDirectoryMode{path: target, mode: mode.Perm()})
			return nil
		case mode.IsRegular():
			return copyRegularFile(path, target, mode.Perm(), info)
		default:
			return fmt.Errorf("source entry %s has unsupported mode %s", path, mode)
		}
	}); err != nil {
		return err
	}
	if err := verifyOfflineCopySource(src, expected); err != nil {
		return err
	}

	// Apply final modes deepest-first so read-only parents never prevent
	// finalizing their children. The staging root is therefore finalized last
	// and remains private while any copied content is incomplete.
	sort.Slice(directoryModes, func(i, j int) bool {
		return len(directoryModes[i].path) > len(directoryModes[j].path)
	})
	for _, directory := range directoryModes {
		if err := chmodOfflineCopyPath(directory.path, directory.mode); err != nil {
			return fmt.Errorf("chmod destination directory %s: %w", directory.path, err)
		}
	}
	return nil
}

func verifyOfflineCopySource(src string, expected map[string]fs.FileInfo) error {
	seen := make(map[string]bool, len(expected))
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		want, ok := expected[rel]
		if !ok {
			return fmt.Errorf("source entry %s appeared while copying; refusing to copy", path)
		}
		got, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat source entry %s after copy: %w", path, err)
		}
		if !sameFileSnapshot(want, got) {
			return fmt.Errorf("source entry %s changed while copying; refusing to copy", path)
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		return err
	}
	for rel := range expected {
		if !seen[rel] {
			return fmt.Errorf("source entry %s disappeared while copying; refusing to copy", filepath.Join(src, rel))
		}
	}
	return nil
}

func syncOfflineCopyTree(root string) error {
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, dir := range dirs {
		if err := syncOfflineCopyDir(dir); err != nil {
			return fmt.Errorf("sync directory %s: %w", dir, err)
		}
	}
	return nil
}

func removeOfflineCopyTreeForCleanup(root string) error {
	// Final source modes may make staged directories read-only. Restore
	// owner access before RemoveAll so a failure after mode finalization still
	// honors the no-partial-artifact guarantee.
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if err := os.Chmod(path, 0o700); err != nil {
				return fmt.Errorf("prepare directory %s for removal: %w", path, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return removeOfflineCopyTree(root)
}

// copyRegularFileAfterCopyHook is a test-only instrumentation point for
// copyRegularFile source mutation races.
var copyRegularFileAfterCopyHook func(string)

func copyRegularFile(src, dst string, mode fs.FileMode, expected fs.FileInfo) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file %s: %w", src, err)
	}
	defer in.Close()

	srcInfo, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat source file %s: %w", src, err)
	}
	if !srcInfo.Mode().IsRegular() {
		return fmt.Errorf("source entry %s has unsupported mode %s", src, srcInfo.Mode())
	}
	if expected != nil && !sameFileSnapshot(expected, srcInfo) {
		return fmt.Errorf("source entry %s changed while copying; refusing to copy", src)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create destination file %s: %w", dst, err)
	}
	defer func() {
		closeErr := out.Close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("close destination file %s: %w", dst, closeErr)
		}
		if err != nil {
			_ = os.Remove(dst)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if copyRegularFileAfterCopyHook != nil {
		copyRegularFileAfterCopyHook(src)
	}
	srcInfo, err = os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat source entry %s after copy: %w", src, err)
	}
	if expected != nil && !sameFileSnapshot(expected, srcInfo) {
		return fmt.Errorf("source entry %s changed while copying; refusing to copy", src)
	}
	if err := out.Chmod(mode); err != nil {
		return fmt.Errorf("chmod destination file %s: %w", dst, err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync destination file %s: %w", dst, err)
	}
	return nil
}

func sameFileSnapshot(a, b fs.FileInfo) bool {
	return os.SameFile(a, b) &&
		a.Mode() == b.Mode() &&
		a.Size() == b.Size() &&
		a.ModTime() == b.ModTime()
}
