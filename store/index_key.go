package store

import "github.com/ponchione/shunter/types"

// IndexKey is an ordered tuple of Values used as a B-tree key.
type IndexKey struct {
	parts []types.Value
}

// NewIndexKey constructs an IndexKey from parts.
func NewIndexKey(parts ...types.Value) IndexKey {
	return IndexKey{parts: parts}
}

// Len returns the number of parts.
func (k IndexKey) Len() int { return len(k.parts) }

// Part returns the i-th part.
func (k IndexKey) Part(i int) types.Value { return k.parts[i] }

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

// ExtractKey builds an IndexKey from a row using the given column indices.
func ExtractKey(row types.ProductValue, columns []int) IndexKey {
	parts := make([]types.Value, len(columns))
	for i, col := range columns {
		parts[i] = row[col]
	}
	return IndexKey{parts: parts}
}
