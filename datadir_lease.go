package shunter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const dataDirLeaseFilenameSuffix = ".shunter-lock"

// ErrDataDirInUse reports that a runtime or offline operation already owns a
// canonical DataDir path.
var ErrDataDirInUse = errors.New("shunter: data dir is in use")

var errDataDirLockWouldBlock = errors.New("shunter: data dir lock would block")

var dataDirLeaseRegistry = struct {
	sync.Mutex
	paths map[string]dataDirLeaseState
}{paths: make(map[string]dataDirLeaseState)}

type dataDirLeaseState struct {
	readers int
	writer  bool
}

type dataDirLeaseMode uint8

const (
	dataDirLeaseShared dataDirLeaseMode = iota
	dataDirLeaseExclusive
)

type dataDirLease struct {
	canonicalPath string
	registryKey   string
	mode          dataDirLeaseMode
	file          *os.File
	releaseOnce   sync.Once
	releaseErr    error
}

func acquireDataDirLease(dataDir string, mode dataDirLeaseMode, prepareParent func(string) error) (*dataDirLease, error) {
	canonicalPath, err := resolvePathForContainment(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir %s: %w", dataDir, err)
	}
	registryKey := dataDirRegistryKey(canonicalPath)
	if !reserveDataDirLease(registryKey, mode) {
		return nil, dataDirInUseError(canonicalPath)
	}

	reserved := true
	defer func() {
		if reserved {
			releaseDataDirLeaseReservation(registryKey, mode)
		}
	}()

	parent := filepath.Dir(canonicalPath)
	if prepareParent != nil {
		if err := prepareParent(parent); err != nil {
			return nil, fmt.Errorf("prepare data dir lock parent %s: %w", parent, err)
		}
	}

	lockPath := canonicalPath + dataDirLeaseFilenameSuffix
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open data dir lock %s: %w", lockPath, err)
	}
	if err := lockDataDirFile(file, mode); err != nil {
		closeErr := file.Close()
		if errors.Is(err, errDataDirLockWouldBlock) {
			return nil, errors.Join(dataDirInUseError(canonicalPath), closeErr)
		}
		return nil, errors.Join(fmt.Errorf("lock data dir %s: %w", canonicalPath, err), closeErr)
	}

	reserved = false
	return &dataDirLease{
		canonicalPath: canonicalPath,
		registryKey:   registryKey,
		mode:          mode,
		file:          file,
	}, nil
}

func (l *dataDirLease) release() error {
	if l == nil {
		return nil
	}
	l.releaseOnce.Do(func() {
		var unlockErr, closeErr error
		if l.file != nil {
			unlockErr = unlockDataDirFile(l.file)
			closeErr = l.file.Close()
			l.file = nil
		}
		releaseDataDirLeaseReservation(l.registryKey, l.mode)
		l.releaseErr = errors.Join(unlockErr, closeErr)
	})
	return l.releaseErr
}

func reserveDataDirLease(key string, mode dataDirLeaseMode) bool {
	dataDirLeaseRegistry.Lock()
	defer dataDirLeaseRegistry.Unlock()
	state := dataDirLeaseRegistry.paths[key]
	if mode == dataDirLeaseExclusive {
		if state.writer || state.readers > 0 {
			return false
		}
		state.writer = true
	} else {
		if state.writer {
			return false
		}
		state.readers++
	}
	dataDirLeaseRegistry.paths[key] = state
	return true
}

func releaseDataDirLeaseReservation(key string, mode dataDirLeaseMode) {
	dataDirLeaseRegistry.Lock()
	state, exists := dataDirLeaseRegistry.paths[key]
	if exists {
		if mode == dataDirLeaseExclusive {
			state.writer = false
		} else if state.readers > 0 {
			state.readers--
		}
		if !state.writer && state.readers == 0 {
			delete(dataDirLeaseRegistry.paths, key)
		} else {
			dataDirLeaseRegistry.paths[key] = state
		}
	}
	dataDirLeaseRegistry.Unlock()
}

func dataDirInUseError(canonicalPath string) error {
	return fmt.Errorf("%w: %s", ErrDataDirInUse, canonicalPath)
}

func releaseLeaseInto(retErr *error, lease *dataDirLease) {
	if lease == nil {
		return
	}
	*retErr = errors.Join(*retErr, lease.release())
}
