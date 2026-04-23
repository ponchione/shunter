package subscription

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

// fakeSchema is a minimal SchemaLookup for validation tests.
type fakeSchema struct {
	tables  map[TableID]map[ColID]types.ValueKind
	indexes map[TableID]map[ColID]bool
}

func newFakeSchema() *fakeSchema {
	return &fakeSchema{
		tables:  map[TableID]map[ColID]types.ValueKind{},
		indexes: map[TableID]map[ColID]bool{},
	}
}

func (s *fakeSchema) addTable(t TableID, cols map[ColID]types.ValueKind, indexed ...ColID) {
	s.tables[t] = cols
	idx := map[ColID]bool{}
	for _, c := range indexed {
		idx[c] = true
	}
	s.indexes[t] = idx
}

func (s *fakeSchema) TableExists(t TableID) bool {
	_, ok := s.tables[t]
	return ok
}

func (s *fakeSchema) ColumnExists(t TableID, c ColID) bool {
	cols, ok := s.tables[t]
	if !ok {
		return false
	}
	_, ok = cols[c]
	return ok
}

func (s *fakeSchema) ColumnType(t TableID, c ColID) types.ValueKind {
	return s.tables[t][c]
}

func (s *fakeSchema) HasIndex(t TableID, c ColID) bool {
	return s.indexes[t] != nil && s.indexes[t][c]
}

func (s *fakeSchema) TableName(t TableID) string {
	if _, ok := s.tables[t]; ok {
		return "t"
	}
	return ""
}

func (s *fakeSchema) ColumnCount(t TableID) int {
	cols, ok := s.tables[t]
	if !ok {
		return 0
	}
	return len(cols)
}

// IndexIDForColumn synthesizes a stable IndexID per (table, col) when the
// column is declared indexed. Matches the encoding used by tests that share
// this schema with mockCommitted.
func (s *fakeSchema) IndexIDForColumn(t TableID, c ColID) (IndexID, bool) {
	if s.indexes[t] != nil && s.indexes[t][c] {
		return syntheticIndexID(t, c), true
	}
	return 0, false
}

// syntheticIndexID derives a stable IndexID from (table, col) for tests.
func syntheticIndexID(t TableID, c ColID) IndexID {
	return IndexID(uint32(t)*1000 + uint32(c))
}

func baseSchema() *fakeSchema {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindInt32}, 0)
	s.addTable(3, map[ColID]types.ValueKind{0: types.KindUint64})
	return s
}

func TestValidateColEqValid(t *testing.T) {
	s := baseSchema()
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	if err := ValidatePredicate(p, s); err != nil {
		t.Fatalf("ValidatePredicate = %v, want nil", err)
	}
}

func TestValidateAndThreeTables(t *testing.T) {
	s := baseSchema()
	inner := And{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
		Right: ColEq{Table: 2, Column: 0, Value: types.NewUint64(2)},
	}
	outer := And{
		Left:  inner,
		Right: ColEq{Table: 3, Column: 0, Value: types.NewUint64(3)},
	}
	err := ValidatePredicate(outer, s)
	if !errors.Is(err, ErrTooManyTables) {
		t.Fatalf("want ErrTooManyTables, got %v", err)
	}
}

func TestValidateJoinIndexOnLeft(t *testing.T) {
	s := baseSchema()
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	if err := ValidatePredicate(p, s); err != nil {
		t.Fatalf("ValidatePredicate = %v, want nil", err)
	}
}

func TestValidateJoinIndexOnRight(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	if err := ValidatePredicate(p, s); err != nil {
		t.Fatalf("ValidatePredicate = %v, want nil", err)
	}
}

func TestValidateJoinUnindexed(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	err := ValidatePredicate(p, s)
	if !errors.Is(err, ErrUnindexedJoin) {
		t.Fatalf("want ErrUnindexedJoin, got %v", err)
	}
}

func TestValidateNoRowsKnownTable(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	if err := ValidatePredicate(NoRows{Table: 1}, s); err != nil {
		t.Fatalf("ValidatePredicate(NoRows) = %v, want nil", err)
	}
}

func TestValidateNoRowsUnknownTable(t *testing.T) {
	s := newFakeSchema()
	if err := ValidatePredicate(NoRows{Table: 1}, s); !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("want ErrTableNotFound, got %v", err)
	}
}

func TestValidateColEqMissingTable(t *testing.T) {
	s := baseSchema()
	p := ColEq{Table: 999, Column: 0, Value: types.NewUint64(1)}
	err := ValidatePredicate(p, s)
	if !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("want ErrTableNotFound, got %v", err)
	}
}

func TestValidateColEqMissingColumn(t *testing.T) {
	s := baseSchema()
	p := ColEq{Table: 1, Column: 99, Value: types.NewUint64(1)}
	err := ValidatePredicate(p, s)
	if !errors.Is(err, ErrColumnNotFound) {
		t.Fatalf("want ErrColumnNotFound, got %v", err)
	}
}

func TestValidateColEqKindMismatch(t *testing.T) {
	s := baseSchema()
	p := ColEq{Table: 1, Column: 0, Value: types.NewString("not uint64")}
	err := ValidatePredicate(p, s)
	if !errors.Is(err, ErrInvalidPredicate) {
		t.Fatalf("want ErrInvalidPredicate, got %v", err)
	}
}

func TestValidateColRangeBoundMismatch(t *testing.T) {
	s := baseSchema()
	p := ColRange{
		Table:  1,
		Column: 0,
		Lower:  Bound{Value: types.NewString("x")},
		Upper:  Bound{Unbounded: true},
	}
	err := ValidatePredicate(p, s)
	if !errors.Is(err, ErrInvalidPredicate) {
		t.Fatalf("want ErrInvalidPredicate, got %v", err)
	}
}

func TestValidateNestedAndValid(t *testing.T) {
	s := baseSchema()
	p := And{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
		Right: ColRange{Table: 2, Column: 1, Lower: Bound{Value: types.NewInt32(10), Inclusive: true}, Upper: Bound{Unbounded: true}},
	}
	if err := ValidatePredicate(p, s); err != nil {
		t.Fatalf("ValidatePredicate = %v, want nil", err)
	}
}

func TestValidateNilPredicate(t *testing.T) {
	s := baseSchema()
	err := ValidatePredicate(nil, s)
	if !errors.Is(err, ErrInvalidPredicate) {
		t.Fatalf("want ErrInvalidPredicate, got %v", err)
	}
}

func TestValidateAndNilChild(t *testing.T) {
	s := baseSchema()
	p := And{Left: ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}, Right: nil}
	err := ValidatePredicate(p, s)
	if !errors.Is(err, ErrInvalidPredicate) {
		t.Fatalf("want ErrInvalidPredicate, got %v", err)
	}
}

func TestValidateJoinFilterOutsideTables(t *testing.T) {
	s := baseSchema()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: ColEq{Table: 3, Column: 0, Value: types.NewUint64(5)},
	}
	err := ValidatePredicate(p, s)
	if !errors.Is(err, ErrInvalidPredicate) {
		t.Fatalf("want ErrInvalidPredicate, got %v", err)
	}
}

func TestValidateJoinKindMismatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindString}, 0)
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	err := ValidatePredicate(p, s)
	if !errors.Is(err, ErrInvalidPredicate) {
		t.Fatalf("want ErrInvalidPredicate, got %v", err)
	}
}
