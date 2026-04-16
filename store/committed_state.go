package store

import (
	"sync"

	"github.com/ponchione/shunter/schema"
)

// CommittedState holds the committed version of all tables.
// Protected by RWMutex: writes are serialized by the executor,
// reads may be concurrent via RLock for snapshots.
type CommittedState struct {
	mu     sync.RWMutex
	tables map[schema.TableID]*Table
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
func (cs *CommittedState) Table(id schema.TableID) (*Table, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.tableLocked(id)
}

func (cs *CommittedState) tableLocked(id schema.TableID) (*Table, bool) {
	t, ok := cs.tables[id]
	return t, ok
}

// TableIDs returns all registered table IDs.
func (cs *CommittedState) TableIDs() []schema.TableID {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.tableIDsLocked()
}

func (cs *CommittedState) tableIDsLocked() []schema.TableID {
	ids := make([]schema.TableID, 0, len(cs.tables))
	for id := range cs.tables {
		ids = append(ids, id)
	}
	return ids
}

// RLock acquires a read lock.
func (cs *CommittedState) RLock() { cs.mu.RLock() }

// RUnlock releases a read lock.
func (cs *CommittedState) RUnlock() { cs.mu.RUnlock() }

// Lock acquires an exclusive write lock.
func (cs *CommittedState) Lock() { cs.mu.Lock() }

// Unlock releases the write lock.
func (cs *CommittedState) Unlock() { cs.mu.Unlock() }
