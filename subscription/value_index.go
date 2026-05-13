package subscription

import "github.com/ponchione/shunter/types"

// ValueIndex maps (table, column, value) to candidate query hashes.
// cols records active columns so candidate collection can avoid full scans.
type ValueIndex struct {
	cols columnRefCounts
	// args: table → column → encoded(value) → set of query hashes.
	args map[TableID]map[ColID]map[valueKey]map[QueryHash]struct{}
}

// NewValueIndex constructs an empty ValueIndex.
func NewValueIndex() *ValueIndex {
	return &ValueIndex{
		cols: newColumnRefCounts(),
		args: make(map[TableID]map[ColID]map[valueKey]map[QueryHash]struct{}),
	}
}

func (v *ValueIndex) argsMap(t TableID, c ColID) map[valueKey]map[QueryHash]struct{} {
	byCol, ok := v.args[t]
	if !ok {
		byCol = make(map[ColID]map[valueKey]map[QueryHash]struct{})
		v.args[t] = byCol
	}
	byVal, ok := byCol[c]
	if !ok {
		byVal = make(map[valueKey]map[QueryHash]struct{})
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
	v.cols.add(table, col)
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

	v.cols.remove(table, col)
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

// ForEachHash calls fn for every query hash registered for (table, col, value).
func (v *ValueIndex) ForEachHash(table TableID, col ColID, value Value, fn func(QueryHash)) {
	byCol, ok := v.args[table]
	if !ok {
		return
	}
	byVal, ok := byCol[col]
	if !ok {
		return
	}
	set, ok := byVal[encodeValueKey(value)]
	if !ok {
		return
	}
	for h := range set {
		fn(h)
	}
}

// TrackedColumns returns the columns that have at least one subscription
// registered for the given table. Used during candidate collection.
func (v *ValueIndex) TrackedColumns(table TableID) []ColID {
	return v.cols.trackedColumns(table)
}

// ForEachTrackedColumn calls fn for every tracked column on table.
func (v *ValueIndex) ForEachTrackedColumn(table TableID, fn func(ColID)) {
	v.cols.forEachTrackedColumn(table, fn)
}

type valueKey struct {
	kind  types.ValueKind
	null  bool
	i64   int64
	u64   uint64
	str   string
	bytes string
	hi    uint64
	lo    uint64
	w256  [4]uint64
	uuid  [16]byte
}

// encodeValueKey produces a stable comparable key for a Value.
func encodeValueKey(v Value) valueKey {
	k := valueKey{kind: v.Kind(), null: v.IsNull()}
	if v.IsNull() {
		return k
	}
	switch v.Kind() {
	case types.KindBool:
		if v.AsBool() {
			k.u64 = 1
		}
	case types.KindInt8:
		k.i64 = int64(v.AsInt8())
	case types.KindInt16:
		k.i64 = int64(v.AsInt16())
	case types.KindInt32:
		k.i64 = int64(v.AsInt32())
	case types.KindInt64:
		k.i64 = v.AsInt64()
	case types.KindUint8:
		k.u64 = uint64(v.AsUint8())
	case types.KindUint16:
		k.u64 = uint64(v.AsUint16())
	case types.KindUint32:
		k.u64 = uint64(v.AsUint32())
	case types.KindUint64:
		k.u64 = v.AsUint64()
	case types.KindFloat32:
		k.u64 = uint64(canonicalFloat32Bits(v.AsFloat32()))
	case types.KindFloat64:
		k.u64 = canonicalFloat64Bits(v.AsFloat64())
	case types.KindString:
		k.str = v.AsString()
	case types.KindBytes:
		k.bytes = string(v.BytesView())
	case types.KindInt128:
		hi, lo := v.AsInt128()
		k.hi = uint64(hi)
		k.lo = lo
	case types.KindUint128:
		k.hi, k.lo = v.AsUint128()
	case types.KindInt256:
		w0, w1, w2, w3 := v.AsInt256()
		k.w256 = [4]uint64{uint64(w0), w1, w2, w3}
	case types.KindUint256:
		w0, w1, w2, w3 := v.AsUint256()
		k.w256 = [4]uint64{w0, w1, w2, w3}
	case types.KindTimestamp:
		k.i64 = v.AsTimestamp()
	case types.KindDuration:
		k.i64 = v.AsDurationMicros()
	case types.KindArrayString:
		enc := acquireCanonicalEncoder()
		xs := v.ArrayStringView()
		enc.writeU32(uint32(len(xs)))
		for _, s := range xs {
			enc.writeU32(uint32(len(s)))
			enc.buf = append(enc.buf, s...)
		}
		k.str = string(enc.buf)
		releaseCanonicalEncoder(enc)
	case types.KindUUID:
		k.uuid = v.AsUUID()
	case types.KindJSON:
		k.bytes = string(v.JSONView())
	}
	return k
}
