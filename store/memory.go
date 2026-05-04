package store

import (
	"unsafe"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const (
	StoreMemoryKindTableRows = "table_rows"
	StoreMemoryKindIndex     = "index"
)

// MemoryUsage describes approximate store memory held by one table or index.
// Bytes are intended for operational trend visibility, not exact Go heap
// accounting.
type MemoryUsage struct {
	Kind      string
	TableID   schema.TableID
	TableName string
	IndexID   schema.IndexID
	IndexName string
	Bytes     uint64
}

// MemoryUsage returns approximate memory usage for every table and schema
// index in the committed state.
func (cs *CommittedState) MemoryUsage() []MemoryUsage {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.memoryUsageLocked()
}

// RecordMemoryUsage emits approximate memory usage through the configured
// observer when that observer says memory gauges are enabled.
func (cs *CommittedState) RecordMemoryUsage() {
	cs.mu.RLock()
	observer, ok := cs.observer.(MemoryObserver)
	if !ok || !observer.StoreMemoryUsageEnabled() {
		cs.mu.RUnlock()
		return
	}
	usage := cs.memoryUsageLocked()
	cs.mu.RUnlock()

	observer.RecordStoreMemoryUsage(usage)
}

func (cs *CommittedState) memoryUsageLocked() []MemoryUsage {
	ids := cs.tableIDsLocked()
	out := make([]MemoryUsage, 0, len(ids))
	for _, tableID := range ids {
		table, ok := cs.tableLocked(tableID)
		if !ok {
			continue
		}
		out = append(out, table.memoryUsage(tableID)...)
	}
	return out
}

func (t *Table) memoryUsage(tableID schema.TableID) []MemoryUsage {
	out := []MemoryUsage{{
		Kind:      StoreMemoryKindTableRows,
		TableID:   tableID,
		TableName: t.schema.Name,
		Bytes:     t.tableRowsMemoryBytes(),
	}}
	for _, idx := range t.indexes {
		out = append(out, MemoryUsage{
			Kind:      StoreMemoryKindIndex,
			TableID:   tableID,
			TableName: t.schema.Name,
			IndexID:   idx.schema.ID,
			IndexName: idx.schema.Name,
			Bytes:     idx.ApproxMemoryBytes(),
		})
	}
	return out
}

func (t *Table) tableRowsMemoryBytes() uint64 {
	var n uint64
	for _, row := range t.rows {
		n += uint64(unsafe.Sizeof(types.RowID(0)))
		n += row.ApproxMemoryBytes()
	}
	if t.rowHashIndex != nil {
		for _, rowIDs := range t.rowHashIndex {
			n += uint64(unsafe.Sizeof(uint64(0)))
			n += uint64(cap(rowIDs)) * uint64(unsafe.Sizeof(types.RowID(0)))
		}
	}
	return n
}

// ApproxMemoryBytes returns a deterministic approximation of index memory.
func (idx *Index) ApproxMemoryBytes() uint64 {
	if idx == nil || idx.btree == nil {
		return 0
	}
	return idx.btree.ApproxMemoryBytes()
}

// ApproxMemoryBytes returns a deterministic approximation of B-tree memory.
func (b *BTreeIndex) ApproxMemoryBytes() uint64 {
	if b == nil {
		return 0
	}
	n := uint64(unsafe.Sizeof(*b))
	n += uint64(cap(b.pages)) * uint64(unsafe.Sizeof((*btreePage)(nil)))
	for _, page := range b.pages {
		if page == nil {
			continue
		}
		n += uint64(unsafe.Sizeof(*page))
		n += uint64(cap(page.entries)) * uint64(unsafe.Sizeof(btreeEntry{}))
		for _, entry := range page.entries {
			n += types.ProductValue(entry.key.parts).ApproxMemoryBytes()
			n += uint64(cap(entry.rowIDs)) * uint64(unsafe.Sizeof(types.RowID(0)))
		}
	}
	return n
}
