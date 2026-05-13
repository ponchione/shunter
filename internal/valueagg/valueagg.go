// Package valueagg centralizes value-level aggregate helpers shared by runtime
// read paths.
package valueagg

import (
	"fmt"

	"github.com/ponchione/shunter/types"
)

// DistinctSet counts distinct Values by canonical value equality.
type DistinctSet struct {
	buckets map[uint64][]types.Value
	n       uint64
}

// NewDistinctSet constructs an empty distinct-value set.
func NewDistinctSet() *DistinctSet {
	return &DistinctSet{buckets: make(map[uint64][]types.Value)}
}

// Add inserts value if it is not already present.
func (s *DistinctSet) Add(value types.Value) {
	hash := value.Hash64()
	for _, existing := range s.buckets[hash] {
		if value.Equal(existing) {
			return
		}
	}
	s.buckets[hash] = append(s.buckets[hash], value)
	s.n++
}

// Count returns the number of distinct values inserted.
func (s *DistinctSet) Count() uint64 {
	return s.n
}

// Sum accumulates SUM aggregate values with Shunter's aggregate result kinds.
type Sum struct {
	kind     types.ValueKind
	nullable bool
	seen     bool
	i64      int64
	u64      uint64
	f64      float64
	err      error
}

// NewSum constructs an empty SUM accumulator for the result column shape.
func NewSum(kind types.ValueKind, nullable bool) *Sum {
	return &Sum{kind: kind, nullable: nullable}
}

// Add contributes one value to the accumulator. Null values are ignored.
func (a *Sum) Add(value types.Value) error {
	if a.err != nil {
		return a.err
	}
	if value.IsNull() {
		return nil
	}
	a.seen = true
	switch a.kind {
	case types.KindInt64:
		n, ok := valueAsInt64(value)
		if !ok {
			a.err = fmt.Errorf("SUM aggregate received non-signed value kind %s", value.Kind())
			return a.err
		}
		sum := a.i64 + n
		if (n > 0 && sum < a.i64) || (n < 0 && sum > a.i64) {
			a.err = fmt.Errorf("SUM aggregate overflowed Int64")
			return a.err
		}
		a.i64 = sum
	case types.KindUint64:
		n, ok := valueAsUint64(value)
		if !ok {
			a.err = fmt.Errorf("SUM aggregate received non-unsigned value kind %s", value.Kind())
			return a.err
		}
		if ^uint64(0)-a.u64 < n {
			a.err = fmt.Errorf("SUM aggregate overflowed Uint64")
			return a.err
		}
		a.u64 += n
	case types.KindFloat64:
		n, ok := valueAsFloat64(value)
		if !ok {
			a.err = fmt.Errorf("SUM aggregate received non-float value kind %s", value.Kind())
			return a.err
		}
		a.f64 += n
	default:
		a.err = fmt.Errorf("SUM aggregate result kind %s not supported", a.kind)
	}
	return a.err
}

// Value returns the final aggregate value.
func (a *Sum) Value() (types.Value, error) {
	if a.err != nil {
		return types.Value{}, a.err
	}
	if a.nullable && !a.seen {
		return types.NewNull(a.kind), nil
	}
	switch a.kind {
	case types.KindInt64:
		return types.NewInt64(a.i64), nil
	case types.KindUint64:
		return types.NewUint64(a.u64), nil
	case types.KindFloat64:
		return types.NewFloat64(a.f64)
	default:
		return types.Value{}, fmt.Errorf("SUM aggregate result kind %s not supported", a.kind)
	}
}

func valueAsInt64(value types.Value) (int64, bool) {
	switch value.Kind() {
	case types.KindInt8:
		return int64(value.AsInt8()), true
	case types.KindInt16:
		return int64(value.AsInt16()), true
	case types.KindInt32:
		return int64(value.AsInt32()), true
	case types.KindInt64:
		return value.AsInt64(), true
	default:
		return 0, false
	}
}

func valueAsUint64(value types.Value) (uint64, bool) {
	switch value.Kind() {
	case types.KindUint8:
		return uint64(value.AsUint8()), true
	case types.KindUint16:
		return uint64(value.AsUint16()), true
	case types.KindUint32:
		return uint64(value.AsUint32()), true
	case types.KindUint64:
		return value.AsUint64(), true
	default:
		return 0, false
	}
}

func valueAsFloat64(value types.Value) (float64, bool) {
	switch value.Kind() {
	case types.KindFloat32:
		return float64(value.AsFloat32()), true
	case types.KindFloat64:
		return value.AsFloat64(), true
	default:
		return 0, false
	}
}
