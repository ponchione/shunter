package store

import (
	"fmt"
	"iter"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Table holds the rows and indexes for a single table.
type Table struct {
	schema       *schema.TableSchema
	rows         map[types.RowID]types.ProductValue
	nextID       types.RowID
	indexes      []*Index
	rowHashIndex map[uint64][]types.RowID // set semantics for no-PK tables
	sequence     *Sequence
	sequenceCol  int
}

// NewTable creates a table from its schema, initializing indexes.
func NewTable(ts *schema.TableSchema) *Table {
	t := &Table{
		schema:      ts,
		rows:        make(map[types.RowID]types.ProductValue),
		nextID:      1,
		sequenceCol: -1,
	}

	for i := range ts.Columns {
		if ts.Columns[i].AutoIncrement {
			t.sequence = NewSequence()
			t.sequenceCol = i
			break
		}
	}

	// Create indexes from schema.
	for i := range ts.Indexes {
		t.indexes = append(t.indexes, NewIndex(&ts.Indexes[i]))
	}

	// Set semantics hash index for tables without PK.
	if _, hasPK := ts.PrimaryIndex(); !hasPK {
		t.rowHashIndex = make(map[uint64][]types.RowID)
	}

	return t
}

// Schema returns the table's schema.
func (t *Table) Schema() *schema.TableSchema { return t.schema }

// AllocRowID returns a new, strictly increasing RowID.
func (t *Table) AllocRowID() types.RowID {
	id := t.nextID
	t.nextID++
	return id
}

// InsertRow stores a row. Does not validate — caller must validate first.
// Returns error on index constraint violations.
func (t *Table) InsertRow(id types.RowID, row types.ProductValue) error {
	if _, exists := t.rows[id]; exists {
		return fmt.Errorf("%w: %d", ErrDuplicateRowID, id)
	}

	// Unique/PK constraint check + index insertion.
	if err := t.insertIntoIndexes(id, row); err != nil {
		return err
	}

	// Set semantics check for no-PK tables.
	if t.rowHashIndex != nil {
		h := row.Hash64()
		bucket := t.rowHashIndex[h]
		for _, existingID := range bucket {
			if t.rows[existingID].Equal(row) {
				// Rollback index insertions.
				t.removeFromIndexes(id, row)
				return ErrDuplicateRow
			}
		}
		t.rowHashIndex[h] = append(bucket, id)
	}

	t.rows[id] = row.Copy()
	return nil
}

// DeleteRow removes a row. Returns the old row and true, or zero and false.
func (t *Table) DeleteRow(id types.RowID) (types.ProductValue, bool) {
	row, ok := t.rows[id]
	if !ok {
		return nil, false
	}
	t.removeFromIndexes(id, row)

	if t.rowHashIndex != nil {
		h := row.Hash64()
		bucket := t.rowHashIndex[h]
		for i, rid := range bucket {
			if rid == id {
				t.rowHashIndex[h] = append(bucket[:i], bucket[i+1:]...)
				if len(t.rowHashIndex[h]) == 0 {
					delete(t.rowHashIndex, h)
				}
				break
			}
		}
	}

	delete(t.rows, id)
	return row.Copy(), true
}

// GetRow retrieves a row by RowID.
func (t *Table) GetRow(id types.RowID) (types.ProductValue, bool) {
	row, ok := t.rows[id]
	if !ok {
		return nil, false
	}
	return row.Copy(), true
}

// rowView returns the stored row without copying. It is package-internal and
// callers must treat the returned ProductValue as read-only.
func (t *Table) rowView(id types.RowID) (types.ProductValue, bool) {
	row, ok := t.rows[id]
	return row, ok
}

// Scan yields all rows in unordered iteration.
func (t *Table) Scan() iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for id, row := range t.rows {
			if !yield(id, row.Copy()) {
				return
			}
		}
	}
}

// RowCount returns the number of rows.
func (t *Table) RowCount() int { return len(t.rows) }

// NextID returns the current next RowID (for export).
func (t *Table) NextID() types.RowID { return t.nextID }

// SetNextID restores the RowID counter (for recovery).
func (t *Table) SetNextID(id types.RowID) { t.nextID = id }

// SequenceValue returns the current sequence counter and whether the table has an autoincrement column.
func (t *Table) SequenceValue() (uint64, bool) {
	if t.sequence == nil {
		return 0, false
	}
	return t.sequence.Peek(), true
}

// SetSequenceValue restores the sequence counter (for recovery).
func (t *Table) SetSequenceValue(v uint64) {
	if t.sequence == nil {
		return
	}
	t.sequence.Reset(v)
}

// IndexByID returns the index with the given ID, or nil.
func (t *Table) IndexByID(id schema.IndexID) *Index {
	for _, idx := range t.indexes {
		if idx.schema.ID == id {
			return idx
		}
	}
	return nil
}

// PrimaryIndex returns the primary index, or nil.
func (t *Table) PrimaryIndex() *Index {
	for _, idx := range t.indexes {
		if idx.schema.Primary {
			return idx
		}
	}
	return nil
}

// insertIntoIndexes inserts key→rowID into all indexes.
// On constraint failure at index N, rolls back indexes 0..N-1.
func (t *Table) insertIntoIndexes(id types.RowID, row types.ProductValue) error {
	for i, idx := range t.indexes {
		key := idx.ExtractKey(row)

		// Unique constraint check.
		if idx.schema.Unique {
			if existing := idx.btree.Seek(key); existing != nil {
				// Rollback previously inserted indexes.
				for j := 0; j < i; j++ {
					rk := t.indexes[j].ExtractKey(row)
					t.indexes[j].btree.Remove(rk, id)
				}
				return uniqueViolationError(t, idx, key)
			}
		}

		idx.btree.Insert(key, id)
	}
	return nil
}

// removeFromIndexes removes key→rowID from all indexes.
func (t *Table) removeFromIndexes(id types.RowID, row types.ProductValue) {
	for _, idx := range t.indexes {
		key := idx.ExtractKey(row)
		idx.btree.Remove(key, id)
	}
}
