package subscription

// TableIndex is the Tier 3 fallback index (SPEC-004 §5.3).
//
// Any change to a table triggers evaluation of every query hash registered
// under that table. Used for predicates that have no extractable equality
// filter (range-only, AllRows, etc.).
type TableIndex struct {
	tables map[TableID]map[QueryHash]struct{}
}

// NewTableIndex constructs an empty TableIndex.
func NewTableIndex() *TableIndex {
	return &TableIndex{tables: make(map[TableID]map[QueryHash]struct{})}
}

// Add registers table → hash.
func (ti *TableIndex) Add(table TableID, hash QueryHash) {
	set, ok := ti.tables[table]
	if !ok {
		set = make(map[QueryHash]struct{})
		ti.tables[table] = set
	}
	set[hash] = struct{}{}
}

// Remove removes table → hash. Cleans up empty tables.
func (ti *TableIndex) Remove(table TableID, hash QueryHash) {
	set, ok := ti.tables[table]
	if !ok {
		return
	}
	delete(set, hash)
	if len(set) == 0 {
		delete(ti.tables, table)
	}
}

// Lookup returns all query hashes registered for the table.
func (ti *TableIndex) Lookup(table TableID) []QueryHash {
	set, ok := ti.tables[table]
	if !ok {
		return []QueryHash{}
	}
	out := make([]QueryHash, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	return out
}
