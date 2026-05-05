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
	// ColumnCount returns the number of columns in the table. Used by the
	// join evaluator to project concatenated LHS++RHS rows onto one side.
	// Zero is returned for unknown tables.
	ColumnCount(TableID) int
}

// ValidatePredicate checks the structural and schema-level constraints for
// subscription registration.
func ValidatePredicate(pred Predicate, schema SchemaLookup) error {
	if err := validateRootPredicate(pred); err != nil {
		return err
	}
	return validateWithOptions(pred, schema, validateOptions{requireJoinIndex: true})
}

// ValidateQueryPredicate checks the structural/schema constraints needed by
// ad-hoc query execution. Unlike subscription registration, one-off query
// execution may scan tables directly, so it does not require join-column
// indexes.
func ValidateQueryPredicate(pred Predicate, schema SchemaLookup) error {
	if err := validateRootPredicate(pred); err != nil {
		return err
	}
	return validateWithOptions(pred, schema, validateOptions{requireJoinIndex: false})
}

func validateRootPredicate(pred Predicate) error {
	if pred == nil {
		return fmt.Errorf("%w: nil predicate", ErrInvalidPredicate)
	}
	if tables := pred.Tables(); len(tables) > 2 {
		return fmt.Errorf("%w: predicate references %d tables", ErrTooManyTables, len(tables))
	}
	return nil
}

type validateOptions struct {
	requireJoinIndex bool
}

// validateWithOptions performs recursive structural validation.
func validateWithOptions(pred Predicate, s SchemaLookup, opts validateOptions) error {
	switch p := pred.(type) {
	case ColEq:
		return validateColEq(p, s)
	case ColNe:
		return validateColNe(p, s)
	case ColRange:
		return validateColRange(p, s)
	case ColEqCol:
		return validateColEqCol(p, s)
	case And:
		return validateBinaryPredicate("And", p.Left, p.Right, s, opts)
	case Or:
		return validateBinaryPredicate("Or", p.Left, p.Right, s, opts)
	case AllRows:
		return validateTableExists(p.Table, s)
	case NoRows:
		return validateTableExists(p.Table, s)
	case Join:
		return validateJoin(p, s, opts)
	case CrossJoin:
		return validateCrossJoin(p, s, opts)
	default:
		return fmt.Errorf("%w: unsupported predicate %T", ErrInvalidPredicate, pred)
	}
}

func validateBinaryPredicate(name string, left, right Predicate, s SchemaLookup, opts validateOptions) error {
	if left == nil || right == nil {
		return fmt.Errorf("%w: %s with nil child", ErrInvalidPredicate, name)
	}
	if err := validateWithOptions(left, s, opts); err != nil {
		return err
	}
	return validateWithOptions(right, s, opts)
}

func validateColEq(p ColEq, s SchemaLookup) error {
	return validateColumnValue("ColEq", p.Table, p.Column, p.Value.Kind(), s)
}

func validateColNe(p ColNe, s SchemaLookup) error {
	return validateColumnValue("ColNe", p.Table, p.Column, p.Value.Kind(), s)
}

func validateColumnValue(name string, table TableID, col ColID, got ValueKind, s SchemaLookup) error {
	want, err := validateColumn(table, col, s)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%w: %s value kind %s does not match column kind %s", ErrInvalidPredicate, name, got, want)
	}
	return nil
}

func validateColRange(p ColRange, s SchemaLookup) error {
	want, err := validateColumn(p.Table, p.Column, s)
	if err != nil {
		return err
	}
	if !p.Lower.Unbounded && p.Lower.Value.Kind() != want {
		return fmt.Errorf("%w: ColRange lower bound kind %s does not match column kind %s", ErrInvalidPredicate, p.Lower.Value.Kind(), want)
	}
	if !p.Upper.Unbounded && p.Upper.Value.Kind() != want {
		return fmt.Errorf("%w: ColRange upper bound kind %s does not match column kind %s", ErrInvalidPredicate, p.Upper.Value.Kind(), want)
	}
	return nil
}

func validateColEqCol(p ColEqCol, s SchemaLookup) error {
	leftKind, err := validateColumn(p.LeftTable, p.LeftColumn, s)
	if err != nil {
		return err
	}
	rightKind, err := validateColumn(p.RightTable, p.RightColumn, s)
	if err != nil {
		return err
	}
	if leftKind != rightKind {
		return fmt.Errorf("%w: ColEqCol column kinds differ (%s vs %s)", ErrInvalidPredicate, leftKind, rightKind)
	}
	return nil
}

func validateTableExists(table TableID, s SchemaLookup) error {
	if !s.TableExists(table) {
		return fmt.Errorf("%w: table %d", ErrTableNotFound, table)
	}
	return nil
}

func validateColumn(table TableID, col ColID, s SchemaLookup) (ValueKind, error) {
	if err := validateTableExists(table, s); err != nil {
		return 0, err
	}
	if !s.ColumnExists(table, col) {
		return 0, fmt.Errorf("%w: table %d column %d", ErrColumnNotFound, table, col)
	}
	return s.ColumnType(table, col), nil
}

func validateJoin(p Join, s SchemaLookup, opts validateOptions) error {
	if !s.TableExists(p.Left) {
		return fmt.Errorf("%w: join left table %d", ErrTableNotFound, p.Left)
	}
	if !s.TableExists(p.Right) {
		return fmt.Errorf("%w: join right table %d", ErrTableNotFound, p.Right)
	}
	if p.Left == p.Right && p.LeftAlias == p.RightAlias {
		return fmt.Errorf("%w: self-join requires distinct relation aliases (table %d)", ErrInvalidPredicate, p.Left)
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
	if opts.requireJoinIndex && !s.HasIndex(p.Left, p.LeftCol) && !s.HasIndex(p.Right, p.RightCol) {
		return fmt.Errorf("%w: join %d.%d = %d.%d", ErrUnindexedJoin, p.Left, p.LeftCol, p.Right, p.RightCol)
	}
	if p.Filter != nil {
		for _, ft := range p.Filter.Tables() {
			if ft != p.Left && ft != p.Right {
				return fmt.Errorf("%w: join filter references table %d outside join", ErrInvalidPredicate, ft)
			}
		}
		if p.Left == p.Right {
			if err := validateSelfJoinFilterAliases(p.Filter, p.LeftAlias, p.RightAlias); err != nil {
				return err
			}
		}
		if err := validateWithOptions(p.Filter, s, opts); err != nil {
			return err
		}
	}
	return nil
}

// validateSelfJoinFilterAliases walks a self-join's filter and rejects leaves
// whose Alias does not match one of the enclosing Join's relation-instance
// tags. Distinct-table joins skip this check because the Table check is
// sufficient to route leaves to their side.
func validateSelfJoinFilterAliases(p Predicate, leftAlias, rightAlias uint8) error {
	switch x := p.(type) {
	case ColEq:
		if !isJoinSideAlias(x.Alias, leftAlias, rightAlias) {
			return fmt.Errorf("%w: self-join filter alias %d does not match Join.LeftAlias=%d or RightAlias=%d", ErrInvalidPredicate, x.Alias, leftAlias, rightAlias)
		}
	case ColNe:
		if !isJoinSideAlias(x.Alias, leftAlias, rightAlias) {
			return fmt.Errorf("%w: self-join filter alias %d does not match Join.LeftAlias=%d or RightAlias=%d", ErrInvalidPredicate, x.Alias, leftAlias, rightAlias)
		}
	case ColRange:
		if !isJoinSideAlias(x.Alias, leftAlias, rightAlias) {
			return fmt.Errorf("%w: self-join filter alias %d does not match Join.LeftAlias=%d or RightAlias=%d", ErrInvalidPredicate, x.Alias, leftAlias, rightAlias)
		}
	case ColEqCol:
		if !isJoinSideAlias(x.LeftAlias, leftAlias, rightAlias) {
			return fmt.Errorf("%w: self-join filter alias %d does not match Join.LeftAlias=%d or RightAlias=%d", ErrInvalidPredicate, x.LeftAlias, leftAlias, rightAlias)
		}
		if !isJoinSideAlias(x.RightAlias, leftAlias, rightAlias) {
			return fmt.Errorf("%w: self-join filter alias %d does not match Join.LeftAlias=%d or RightAlias=%d", ErrInvalidPredicate, x.RightAlias, leftAlias, rightAlias)
		}
	case And:
		return validateSelfJoinFilterAliasChildren(x.Left, x.Right, leftAlias, rightAlias)
	case Or:
		return validateSelfJoinFilterAliasChildren(x.Left, x.Right, leftAlias, rightAlias)
	}
	return nil
}

func isJoinSideAlias(alias, leftAlias, rightAlias uint8) bool {
	return alias == leftAlias || alias == rightAlias
}

func validateSelfJoinFilterAliasChildren(left, right Predicate, leftAlias, rightAlias uint8) error {
	if left != nil {
		if err := validateSelfJoinFilterAliases(left, leftAlias, rightAlias); err != nil {
			return err
		}
	}
	if right != nil {
		if err := validateSelfJoinFilterAliases(right, leftAlias, rightAlias); err != nil {
			return err
		}
	}
	return nil
}

func validateCrossJoin(p CrossJoin, s SchemaLookup, opts validateOptions) error {
	if !s.TableExists(p.Left) {
		return fmt.Errorf("%w: cross join left table %d", ErrTableNotFound, p.Left)
	}
	if !s.TableExists(p.Right) {
		return fmt.Errorf("%w: cross join right table %d", ErrTableNotFound, p.Right)
	}
	if p.Left == p.Right && p.LeftAlias == p.RightAlias {
		return fmt.Errorf("%w: self-cross-join requires distinct relation aliases (table %d)", ErrInvalidPredicate, p.Left)
	}
	if p.Filter != nil {
		for _, ft := range p.Filter.Tables() {
			if ft != p.Left && ft != p.Right {
				return fmt.Errorf("%w: cross join filter references table %d outside join", ErrInvalidPredicate, ft)
			}
		}
		if p.Left == p.Right {
			if err := validateSelfJoinFilterAliases(p.Filter, p.LeftAlias, p.RightAlias); err != nil {
				return err
			}
		}
		if err := validateWithOptions(p.Filter, s, opts); err != nil {
			return err
		}
	}
	return nil
}
