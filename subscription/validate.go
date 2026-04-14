package subscription

import "fmt"

// SchemaLookup is the narrow read-only schema surface used by the
// subscription package. During registration the executor provides this from
// committed state; callers that only care about predicate validation can
// satisfy a narrower subset of the interface.
type SchemaLookup interface {
	TableExists(TableID) bool
	ColumnExists(TableID, ColID) bool
	ColumnType(TableID, ColID) ValueKind
	HasIndex(TableID, ColID) bool
	// TableName returns the declared table name for wire/debug use. Empty
	// string is acceptable when the caller does not carry names.
	TableName(TableID) string
}

// ValidatePredicate checks the structural and schema-level constraints
// defined in SPEC-004 §3.3:
//
//  1. At most two tables
//  2. Every referenced table exists
//  3. Every referenced column exists
//  4. Literal column values match the column type
//  5. Join predicates require an index on at least one side of the join
//  6. Predicates use literal values (no cross-column references outside joins)
func ValidatePredicate(pred Predicate, schema SchemaLookup) error {
	if pred == nil {
		return fmt.Errorf("%w: nil predicate", ErrInvalidPredicate)
	}
	if tables := pred.Tables(); len(tables) > 2 {
		return fmt.Errorf("%w: predicate references %d tables", ErrTooManyTables, len(tables))
	}
	return validate(pred, schema)
}

// validate performs recursive structural validation.
func validate(pred Predicate, s SchemaLookup) error {
	switch p := pred.(type) {
	case ColEq:
		return validateColEq(p, s)
	case ColRange:
		return validateColRange(p, s)
	case And:
		if p.Left == nil || p.Right == nil {
			return fmt.Errorf("%w: And with nil child", ErrInvalidPredicate)
		}
		if err := validate(p.Left, s); err != nil {
			return err
		}
		return validate(p.Right, s)
	case AllRows:
		if !s.TableExists(p.Table) {
			return fmt.Errorf("%w: table %d", ErrTableNotFound, p.Table)
		}
		return nil
	case Join:
		return validateJoin(p, s)
	default:
		return fmt.Errorf("%w: unsupported predicate %T", ErrInvalidPredicate, pred)
	}
}

func validateColEq(p ColEq, s SchemaLookup) error {
	if !s.TableExists(p.Table) {
		return fmt.Errorf("%w: table %d", ErrTableNotFound, p.Table)
	}
	if !s.ColumnExists(p.Table, p.Column) {
		return fmt.Errorf("%w: table %d column %d", ErrColumnNotFound, p.Table, p.Column)
	}
	want := s.ColumnType(p.Table, p.Column)
	if p.Value.Kind() != want {
		return fmt.Errorf("%w: ColEq value kind %s does not match column kind %s", ErrInvalidPredicate, p.Value.Kind(), want)
	}
	return nil
}

func validateColRange(p ColRange, s SchemaLookup) error {
	if !s.TableExists(p.Table) {
		return fmt.Errorf("%w: table %d", ErrTableNotFound, p.Table)
	}
	if !s.ColumnExists(p.Table, p.Column) {
		return fmt.Errorf("%w: table %d column %d", ErrColumnNotFound, p.Table, p.Column)
	}
	want := s.ColumnType(p.Table, p.Column)
	if !p.Lower.Unbounded && p.Lower.Value.Kind() != want {
		return fmt.Errorf("%w: ColRange lower bound kind %s does not match column kind %s", ErrInvalidPredicate, p.Lower.Value.Kind(), want)
	}
	if !p.Upper.Unbounded && p.Upper.Value.Kind() != want {
		return fmt.Errorf("%w: ColRange upper bound kind %s does not match column kind %s", ErrInvalidPredicate, p.Upper.Value.Kind(), want)
	}
	return nil
}

func validateJoin(p Join, s SchemaLookup) error {
	if !s.TableExists(p.Left) {
		return fmt.Errorf("%w: join left table %d", ErrTableNotFound, p.Left)
	}
	if !s.TableExists(p.Right) {
		return fmt.Errorf("%w: join right table %d", ErrTableNotFound, p.Right)
	}
	if !s.ColumnExists(p.Left, p.LeftCol) {
		return fmt.Errorf("%w: join left column %d.%d", ErrColumnNotFound, p.Left, p.LeftCol)
	}
	if !s.ColumnExists(p.Right, p.RightCol) {
		return fmt.Errorf("%w: join right column %d.%d", ErrColumnNotFound, p.Right, p.RightCol)
	}
	leftKind := s.ColumnType(p.Left, p.LeftCol)
	rightKind := s.ColumnType(p.Right, p.RightCol)
	if leftKind != rightKind {
		return fmt.Errorf("%w: join column kinds differ (%s vs %s)", ErrInvalidPredicate, leftKind, rightKind)
	}
	if !s.HasIndex(p.Left, p.LeftCol) && !s.HasIndex(p.Right, p.RightCol) {
		return fmt.Errorf("%w: join %d.%d = %d.%d", ErrUnindexedJoin, p.Left, p.LeftCol, p.Right, p.RightCol)
	}
	if p.Filter != nil {
		// Filter must only reference the two join tables.
		for _, ft := range p.Filter.Tables() {
			if ft != p.Left && ft != p.Right {
				return fmt.Errorf("%w: join filter references table %d outside join", ErrInvalidPredicate, ft)
			}
		}
		if err := validate(p.Filter, s); err != nil {
			return err
		}
	}
	return nil
}
