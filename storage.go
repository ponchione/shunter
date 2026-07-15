package shunter

import (
	"errors"
	"fmt"
	"os"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
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
	executor      *executor.Executor
}

// CreateSnapshot writes a full snapshot for the runtime's current committed
// state and returns the represented transaction ID. On a running runtime the
// call is serialized with executor work and publishes only after the selected
// transaction horizon is durable.
func (r *Runtime) CreateSnapshot() (types.TxID, error) {
	if r == nil {
		return 0, ErrRuntimeNotReady
	}

	r.mu.Lock()
	handles, err := r.storageHandlesLocked()
	if err != nil {
		r.mu.Unlock()
		return 0, err
	}

	writer := commitlog.NewFileSnapshotWriterWithObserver(handles.dataDir, handles.registry, handles.observability)
	if r.stateName == RuntimeStateBuilt {
		txID, err := writer.CreateSnapshotAtCurrentHorizon(handles.state)
		r.mu.Unlock()
		return txID, err
	}
	if handles.executor == nil || handles.executor.Fatal() {
		r.mu.Unlock()
		return 0, ErrRuntimeNotReady
	}

	responseCh := make(chan executor.CreateSnapshotResult, 1)
	err = handles.executor.Submit(executor.CreateSnapshotCmd{
		Capture: func(committed *store.CommittedState, txID types.TxID) error {
			return writer.CreateSnapshot(committed, txID)
		},
		ResponseCh: responseCh,
	})
	r.mu.Unlock()
	if err != nil {
		return 0, err
	}
	result := <-responseCh
	return result.TxID, result.Err
}

// CompactCommitLog deletes sealed commit log segments fully covered by a
// completed snapshot. snapshotTxID must name a completed snapshot in the
// runtime data directory.
func (r *Runtime) CompactCommitLog(snapshotTxID types.TxID) error {
	handles, err := r.storageHandles()
	if err != nil {
		return err
	}
	if err := commitlog.RunCompaction(handles.dataDir, snapshotTxID, handles.registry); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: tx_id %d: %w", ErrSnapshotNotFound, snapshotTxID, err)
		}
		return err
	}
	return nil
}

func (r *Runtime) storageHandles() (runtimeStorageHandles, error) {
	if r == nil {
		return runtimeStorageHandles{}, ErrRuntimeNotReady
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.storageHandlesLocked()
}

func (r *Runtime) storageHandlesLocked() (runtimeStorageHandles, error) {
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
		executor:      r.executor,
	}, nil
}
