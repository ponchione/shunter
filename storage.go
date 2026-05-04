package shunter

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

var (
	// ErrSnapshotNotFound reports that a requested completed runtime snapshot does not exist.
	ErrSnapshotNotFound = errors.New("shunter: snapshot not found")
)

type runtimeStorageHandles struct {
	dataDir       string
	registry      schema.SchemaRegistry
	state         *store.CommittedState
	observability *runtimeObservability
}

// CreateSnapshot writes a full snapshot for the runtime's current committed
// state and returns the represented transaction ID. The call is synchronous;
// callers own write quiescence when they need a graceful maintenance point.
func (r *Runtime) CreateSnapshot() (types.TxID, error) {
	handles, err := r.storageHandles()
	if err != nil {
		return 0, err
	}

	txID := handles.state.CommittedTxID()
	writer := commitlog.NewSnapshotWriterWithObserver(handles.dataDir, handles.registry, handles.observability)
	if err := writer.CreateSnapshot(handles.state, txID); err != nil {
		return 0, err
	}
	return txID, nil
}

// CompactCommitLog deletes sealed commit log segments fully covered by a
// completed snapshot. snapshotTxID must name a completed snapshot in the
// runtime data directory.
func (r *Runtime) CompactCommitLog(snapshotTxID types.TxID) error {
	handles, err := r.storageHandles()
	if err != nil {
		return err
	}
	if err := requireCompletedSnapshot(handles.dataDir, snapshotTxID); err != nil {
		return err
	}
	return commitlog.RunCompaction(handles.dataDir, snapshotTxID)
}

func (r *Runtime) storageHandles() (runtimeStorageHandles, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch r.stateName {
	case RuntimeStateBuilt, RuntimeStateReady:
	case RuntimeStateStarting:
		return runtimeStorageHandles{}, ErrRuntimeStarting
	case RuntimeStateClosing, RuntimeStateClosed:
		return runtimeStorageHandles{}, ErrRuntimeClosed
	default:
		return runtimeStorageHandles{}, ErrRuntimeNotReady
	}
	if r.stateName == RuntimeStateReady && !r.ready.Load() {
		return runtimeStorageHandles{}, ErrRuntimeNotReady
	}
	if r.durabilityFatalErr != nil {
		return runtimeStorageHandles{}, ErrRuntimeNotReady
	}
	if r.durability != nil {
		if err := r.durability.FatalError(); err != nil {
			r.durabilityFatalErr = err
			return runtimeStorageHandles{}, ErrRuntimeNotReady
		}
	}
	if r.dataDir == "" || r.registry == nil || r.state == nil {
		return runtimeStorageHandles{}, ErrRuntimeNotReady
	}
	return runtimeStorageHandles{
		dataDir:       r.dataDir,
		registry:      r.registry,
		state:         r.state,
		observability: r.observability,
	}, nil
}

func requireCompletedSnapshot(dataDir string, snapshotTxID types.TxID) error {
	snapshots, err := commitlog.ListSnapshots(dataDir)
	if err != nil {
		return err
	}
	for _, txID := range snapshots {
		if txID == snapshotTxID {
			return nil
		}
	}
	return fmt.Errorf("%w: tx_id %d", ErrSnapshotNotFound, snapshotTxID)
}
