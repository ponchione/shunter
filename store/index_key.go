package store

import "github.com/ponchione/shunter/types"

// IndexKey is an ordered tuple of Values used as a B-tree key.
type IndexKey struct {
	parts []types.Value
}

// NewIndexKey constructs an IndexKey from parts.
func NewIndexKey(parts ...types.Value) IndexKey {
	out := make([]types.Value, len(parts))
	for i, part := range parts {
		out[i] = copyIndexValue(part)
	}
	return IndexKey{parts: out}
}

// Len returns the number of parts.
func (k IndexKey) Len() int { return len(k.parts) }

// Part returns the i-th part.
func (k IndexKey) Part(i int) types.Value { return copyIndexValue(k.parts[i]) }

// Compare returns -1, 0, or +1. Lexicographic by position.
// Shorter key is less when it is a prefix of a longer key.
func (k IndexKey) Compare(other IndexKey) int {
	n := min(len(k.parts), len(other.parts))
	for i := 0; i < n; i++ {
		if c := k.parts[i].Compare(other.parts[i]); c != 0 {
			return c
		}
	}
	if len(k.parts) < len(other.parts) {
		return -1
	}
	if len(k.parts) > len(other.parts) {
		return 1
	}
	return 0
}

// Equal returns true if keys are equal.
func (k IndexKey) Equal(other IndexKey) bool {
	return k.Compare(other) == 0
}

func (k IndexKey) hash64() uint64 {
	return types.ProductValue(k.parts).Hash64()
}

// Bound represents one endpoint of an index range.
type Bound = types.IndexBound

// UnboundedLow constructs an unbounded lower endpoint.
func UnboundedLow() Bound {
	return Bound{Unbounded: true}
}

// UnboundedHigh constructs an unbounded upper endpoint.
func UnboundedHigh() Bound {
	return Bound{Unbounded: true}
}

// Inclusive constructs a closed bound at v.
func Inclusive(v types.Value) Bound {
	return Bound{Value: v, Inclusive: true}
}

// Exclusive constructs an open bound at v.
func Exclusive(v types.Value) Bound {
	return Bound{Value: v}
}

// ExtractKey builds an IndexKey from a row using the given column indices.
func ExtractKey(row types.ProductValue, columns []int) IndexKey {
	parts := make([]types.Value, len(columns))
	for i, col := range columns {
		parts[i] = copyIndexValue(row[col])
	}
	return IndexKey{parts: parts}
}

func copyIndexValue(v types.Value) types.Value {
	if v.IsNull() {
		return v
	}
	switch v.Kind() {
	case types.KindBytes:
		return types.NewBytes(v.BytesView())
	case types.KindJSON:
		return copyJSONValue(v)
	case types.KindArrayString:
		return types.NewArrayString(v.ArrayStringView())
	default:
		return v
	}
}

func copyJSONValue(v types.Value) types.Value {
	return mustJSONValue(v.JSONView())
}

func mustJSONValue(raw []byte) types.Value {
	out, err := types.NewJSON(raw)
	if err != nil {
		panic(err)
	}
	return out
}
