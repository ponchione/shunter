package subscription

import (
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// ProjectionColumn describes one output column for a live subscription whose
// predicate still evaluates rows from a table-shaped source.
type ProjectionColumn struct {
	Schema schema.ColumnSchema
	Table  TableID
	Column ColID
	Alias  uint8
}

// ValidateProjection checks that a live row projection can be applied to the
// table-shaped rows emitted by pred.
func ValidateProjection(pred Predicate, columns []ProjectionColumn, s SchemaLookup) error {
	return validateProjectionColumns(pred, columns, s)
}

func validateProjectionColumns(pred Predicate, columns []ProjectionColumn, s SchemaLookup) error {
	if len(columns) == 0 {
		return nil
	}
	if multi, ok := pred.(MultiJoin); ok && !multiJoinProjectionFilterIsEquality(multi.Filter) {
		return fmt.Errorf("%w: multi-join projections only support equality filters", ErrInvalidPredicate)
	}
	for i, col := range columns {
		label := fmt.Sprintf("projection column %d", i)
		if err := validateDeclaredColumnIndex(label, col.Column, col.Schema); err != nil {
			return err
		}
		if !projectionColumnMatchesEmittedRelation(pred, col) {
			if _, ok := pred.(MultiJoin); ok {
				return fmt.Errorf("%w: projection column %d must come from a joined relation", ErrInvalidPredicate, i)
			}
			return fmt.Errorf("%w: projection column %d must come from the emitted relation", ErrInvalidPredicate, i)
		}
		if err := validateDeclaredColumnSource(label, col.Table, col.Column, col.Schema, s); err != nil {
			return err
		}
	}
	return nil
}

func multiJoinProjectionFilterIsEquality(pred Predicate) bool {
	switch p := pred.(type) {
	case nil:
		return true
	case ColEq:
		return true
	case ColEqCol:
		return true
	case And:
		return multiJoinProjectionFilterIsEquality(p.Left) && multiJoinProjectionFilterIsEquality(p.Right)
	default:
		return false
	}
}

func projectionColumnMatchesEmittedRelation(pred Predicate, col ProjectionColumn) bool {
	switch p := pred.(type) {
	case Join:
		if col.Table != p.ProjectedTable() {
			return false
		}
		if p.Left != p.Right {
			return true
		}
		if p.ProjectRight {
			return col.Alias == p.RightAlias
		}
		return col.Alias == p.LeftAlias
	case CrossJoin:
		if col.Table != p.ProjectedTable() {
			return false
		}
		if p.Left != p.Right {
			return true
		}
		if p.ProjectRight {
			return col.Alias == p.RightAlias
		}
		return col.Alias == p.LeftAlias
	case MultiJoin:
		_, ok := multiJoinProjectionRelation(p.Relations, col)
		return ok
	default:
		tables := pred.Tables()
		return len(tables) == 1 && col.Table == tables[0] && col.Alias == 0
	}
}

func multiJoinProjectionRelation(relations []MultiJoinRelation, col ProjectionColumn) (int, bool) {
	for i, rel := range relations {
		if col.Table == rel.Table && col.Alias == rel.Alias {
			return i, true
		}
	}
	return 0, false
}

func projectRows(rows []types.ProductValue, columns []ProjectionColumn) []types.ProductValue {
	if len(rows) == 0 || len(columns) == 0 {
		return rows
	}
	out := make([]types.ProductValue, 0, len(rows))
	for _, row := range rows {
		projected := make(types.ProductValue, 0, len(columns))
		for _, col := range columns {
			idx := int(col.Column)
			if idx < 0 || idx >= len(row) {
				continue
			}
			projected = append(projected, row[idx])
		}
		out = append(out, projected)
	}
	return out
}

func projectJoinInitialRows(pred Predicate, rows []types.ProductValue, columns []ProjectionColumn) []types.ProductValue {
	if _, ok := pred.(MultiJoin); ok {
		return rows
	}
	return projectRows(rows, columns)
}

func projectionUpdateColumns(fallback []schema.ColumnSchema, projection []ProjectionColumn) []schema.ColumnSchema {
	if len(projection) == 0 {
		return fallback
	}
	out := make([]schema.ColumnSchema, len(projection))
	for i, col := range projection {
		out[i] = col.Schema
	}
	return out
}
