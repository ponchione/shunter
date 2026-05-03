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
// The returned pointer is protected only during lookup; callers must use it
// inside a snapshot, executor transaction, or recovery bootstrap window.
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
// Resolve and use them inside the same safety windows as Table.
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
