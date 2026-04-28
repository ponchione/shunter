package subscription

// ValueIndex is the Tier 1 pruning index (SPEC-004 §5.1).
//
// Maps (table, column, value) → set of query hashes for subscriptions with
// a ColEq predicate on that column.
//
// The `cols` map tracks which columns have active entries for each table,
// used during candidate collection so we don't scan the whole value map.
type ValueIndex struct {
	cols map[TableID]map[ColID]int
	// args: table → column → encoded(value) → set of query hashes.
	args map[TableID]map[ColID]map[string]map[QueryHash]struct{}
}

// NewValueIndex constructs an empty ValueIndex.
func NewValueIndex() *ValueIndex {
	return &ValueIndex{
		cols: make(map[TableID]map[ColID]int),
		args: make(map[TableID]map[ColID]map[string]map[QueryHash]struct{}),
	}
}

func (v *ValueIndex) argsMap(t TableID, c ColID) map[string]map[QueryHash]struct{} {
	byCol, ok := v.args[t]
	if !ok {
		byCol = make(map[ColID]map[string]map[QueryHash]struct{})
		v.args[t] = byCol
	}
	byVal, ok := byCol[c]
	if !ok {
		byVal = make(map[string]map[QueryHash]struct{})
		byCol[c] = byVal
	}
	return byVal
}

// Add registers a (table, column, value) → hash mapping.
func (v *ValueIndex) Add(table TableID, col ColID, value Value, hash QueryHash) {
	byVal := v.argsMap(table, col)
	key := encodeValueKey(value)
	set, ok := byVal[key]
	if !ok {
		set = make(map[QueryHash]struct{})
		byVal[key] = set
	}
	if _, exists := set[hash]; exists {
		return
	}
	set[hash] = struct{}{}

	// Bump the cols refcount.
	byCol, ok := v.cols[table]
	if !ok {
		byCol = make(map[ColID]int)
		v.cols[table] = byCol
	}
	byCol[col]++
}

// Remove removes a (table, column, value) → hash mapping. Empty keys are
// cleaned up so `cols` and `args` only reflect live entries.
func (v *ValueIndex) Remove(table TableID, col ColID, value Value, hash QueryHash) {
	byCol, ok := v.args[table]
	if !ok {
		return
	}
	byVal, ok := byCol[col]
	if !ok {
		return
	}
	key := encodeValueKey(value)
	set, ok := byVal[key]
	if !ok {
		return
	}
	if _, ok := set[hash]; !ok {
		return
	}
	delete(set, hash)
	if len(set) == 0 {
		delete(byVal, key)
	}
	if len(byVal) == 0 {
		delete(byCol, col)
	}
	if len(byCol) == 0 {
		delete(v.args, table)
	}

	// Drop the cols refcount.
	if colsRC, ok := v.cols[table]; ok {
		colsRC[col]--
		if colsRC[col] <= 0 {
			delete(colsRC, col)
		}
		if len(colsRC) == 0 {
			delete(v.cols, table)
		}
	}
}

// Lookup returns all query hashes registered for the given (table, col, value).
// Returns an empty slice (not nil) when there are no matches so callers can
// always range over the result.
func (v *ValueIndex) Lookup(table TableID, col ColID, value Value) []QueryHash {
	byCol, ok := v.args[table]
	if !ok {
		return []QueryHash{}
	}
	byVal, ok := byCol[col]
	if !ok {
		return []QueryHash{}
	}
	set, ok := byVal[encodeValueKey(value)]
	if !ok {
		return []QueryHash{}
	}
	return mapKeys(set)
}

// TrackedColumns returns the columns that have at least one subscription
// registered for the given table. Used during candidate collection.
func (v *ValueIndex) TrackedColumns(table TableID) []ColID {
	byCol, ok := v.cols[table]
	if !ok {
		return []ColID{}
	}
	return mapKeys(byCol)
}

// encodeValueKey produces a stable string key for a Value so it can be used
// as a map key (Value itself is not comparable because of the []byte field).
// Reuses the canonical predicate encoder so ordering stays consistent.
func encodeValueKey(v Value) string {
	enc := encoderPool.Get().(*canonicalEncoder)
	defer func() {
		enc.reset()
		encoderPool.Put(enc)
	}()
	encodeValue(enc, v)
	return string(enc.buf)
}
