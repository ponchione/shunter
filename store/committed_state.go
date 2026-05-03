package store

import (
	"sync"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// CommittedState holds the committed version of all tables.
// Protected by RWMutex: writes are serialized by the executor,
// reads may be concurrent via RLock for snapshots.
type CommittedState struct {
	mu            sync.RWMutex
	tables        map[schema.TableID]*Table
	committedTxID types.TxID
	observer      Observer
}

// NewCommittedState creates an empty committed state.
func NewCommittedState() *CommittedState {
	return &CommittedState{
		tables: make(map[schema.TableID]*Table),
	}
}

// RegisterTable adds a table to the committed state.
func (cs *CommittedState) RegisterTable(id schema.TableID, t *Table) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.tables[id] = t
}

// Table returns the table for the given ID.
//
// Read-view lifetime contract pin — raw-pointer exposure envelope. The RLock is acquired
// only for the map lookup, not for the lifetime of the returned *Table. The
// returned pointer therefore outlives the RLock: callers must hold their own
// envelope that serializes with CommittedState writers for the entire window
// they use the pointer. Three legal envelopes exist in this codebase:
//
//  1. CommittedSnapshot — snapshot methods resolve tables through tableLocked
//     while the snapshot holds cs.RLock() (acquired in Snapshot(), released in
//     Close()); the snapshot's open→Close lifetime bounds every method call on
//     the returned *Table. Iterator surfaces additionally runtime.KeepAlive the
//     snapshot so the RLock is not released mid-iteration.
//  2. Transaction / StateView — the reducer runs on the single executor
//     goroutine under single-writer discipline (executor.executor.go). No
//     concurrent writer can run during a reducer's synchronous window, and
//     the executor does not release the goroutine until the transaction
//     commits or rolls back. Inside that window writes on the returned
//     *Table (AllocRowID, sequence.Next via applyAutoIncrement, etc.) are
//     safe without cs.Lock().
//  3. Commitlog recovery bootstrap — commitlog/recovery.go runs on a single
//     goroutine before any reader attaches; writes on the returned *Table
//     precede the first reducer dispatch.
//
// Hazards the envelope rule prevents:
//   - A caller that retains *Table past the envelope (stashes it into a
//     goroutine that runs after snapshot Close, or past reducer return)
//     races future writers on t.rows / t.indexes / t.sequence.
//   - A caller that retains *Table across a subsequent
//     RegisterTable(id, replacement) holds a stale pointer: the map entry
//     points at `replacement`, but the retained pointer still points at
//     the original and does not observe writes committed via `replacement`.
//     The stale-after-re-register property is pinned by
//     store/committed_state_table_contract_test.go.
//   - A caller on a non-executor goroutine that reads via the pointer
//     without holding cs.RLock() races any in-progress reducer write.
//
// All current callers (store/snapshot.go, store/transaction.go,
// store/state_view.go, commitlog/recovery.go, commitlog/snapshot_io.go) stay
// inside one of the three envelopes. Pinned by
// store/committed_state_table_contract_test.go.
func (cs *CommittedState) Table(id schema.TableID) (*Table, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.tableLocked(id)
}

// TableLocked returns the table for id while the caller already holds RLock or
// Lock. It exists for callers that must perform several committed-state reads
// under one lock envelope; using Table in that window can self-deadlock behind
// a pending writer because sync.RWMutex blocks new readers once a writer waits.
func (cs *CommittedState) TableLocked(id schema.TableID) (*Table, bool) {
	return cs.tableLocked(id)
}

func (cs *CommittedState) tableLocked(id schema.TableID) (*Table, bool) {
	t, ok := cs.tables[id]
	return t, ok
}

// TableIDs returns all registered table IDs.
//
// Read-view lifetime contract pin — same envelope rule as Table(id): the RLock bounds
// only the slice materialization, not any subsequent lookup via the ids.
// Callers must iterate and resolve each id to a *Table under one of the
// three legal envelopes documented on Table().
func (cs *CommittedState) TableIDs() []schema.TableID {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.tableIDsLocked()
}

// TableIDsLocked returns all registered table IDs while the caller already
// holds RLock or Lock.
func (cs *CommittedState) TableIDsLocked() []schema.TableID {
	return cs.tableIDsLocked()
}

func (cs *CommittedState) tableIDsLocked() []schema.TableID {
	ids := make([]schema.TableID, 0, len(cs.tables))
	for id := range cs.tables {
		ids = append(ids, id)
	}
	return ids
}

// CommittedTxID returns the commit horizon represented by this state.
func (cs *CommittedState) CommittedTxID() types.TxID {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.committedTxID
}

// SetCommittedTxID records the commit horizon represented by this state.
func (cs *CommittedState) SetCommittedTxID(txID types.TxID) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.committedTxID = txID
}

// SetObserver wires runtime-scoped observations for snapshots derived from
// this state. Nil restores the no-op behavior used before a runtime exists.
func (cs *CommittedState) SetObserver(observer Observer) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.observer = observer
}

// CommittedTxIDLocked returns the committed horizon while the caller already
// holds RLock or Lock.
func (cs *CommittedState) CommittedTxIDLocked() types.TxID {
	return cs.committedTxID
}

// RLock acquires a read lock.
func (cs *CommittedState) RLock() { cs.mu.RLock() }

// RUnlock releases a read lock.
func (cs *CommittedState) RUnlock() { cs.mu.RUnlock() }

// Lock acquires an exclusive write lock.
func (cs *CommittedState) Lock() { cs.mu.Lock() }

// Unlock releases the write lock.
func (cs *CommittedState) Unlock() { cs.mu.Unlock() }
