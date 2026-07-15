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
		out[i] = part.Copy()
	}
	return IndexKey{parts: out}
}

// Len returns the number of parts.
func (k IndexKey) Len() int { return len(k.parts) }

// Part returns the i-th part.
func (k IndexKey) Part(i int) types.Value { return k.parts[i].Copy() }

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

// comparePrefix compares k with prefix and treats every key beginning with
// prefix as equal. A bound shorter than a stored composite key therefore
// addresses the complete prefix group.
func (k IndexKey) comparePrefix(prefix IndexKey) int {
	n := min(len(k.parts), len(prefix.parts))
	for i := 0; i < n; i++ {
		if c := k.parts[i].Compare(prefix.parts[i]); c != 0 {
			return c
		}
	}
	if len(k.parts) < len(prefix.parts) {
		return -1
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

// Inclusive constructs a closed bound at the supplied key tuple. A tuple may
// be a complete composite key or a shorter prefix.
func Inclusive(values ...types.Value) Bound {
	return bounded(values, true)
}

// Exclusive constructs an open bound at the supplied key tuple. A tuple may
// be a complete composite key or a shorter prefix.
func Exclusive(values ...types.Value) Bound {
	return bounded(values, false)
}

func bounded(values []types.Value, inclusive bool) Bound {
	if len(values) == 0 {
		panic("store: bounded index endpoint requires at least one value")
	}
	if len(values) == 1 {
		return Bound{Value: values[0].Copy(), Inclusive: inclusive}
	}
	return Bound{Values: types.ProductValue(values).Copy(), Inclusive: inclusive}
}

func indexKeyForBound(bound Bound) IndexKey {
	if len(bound.Values) != 0 {
		return NewIndexKey(bound.Values...)
	}
	return NewIndexKey(bound.Value)
}

// ExtractKey builds an IndexKey from a row using the given column indices.
func ExtractKey(row types.ProductValue, columns []int) IndexKey {
	parts := make([]types.Value, len(columns))
	for i, col := range columns {
		parts[i] = row[col].Copy()
	}
	return IndexKey{parts: parts}
}
