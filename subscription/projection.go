package subscription

import (
	"fmt"
	"slices"

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

func copyProjectionColumns(in []ProjectionColumn) []ProjectionColumn {
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
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
	for i, col := range columns {
		if col.Schema.Index != int(col.Column) {
			return fmt.Errorf("%w: projection column %d schema index %d does not match source column %d", ErrInvalidPredicate, i, col.Schema.Index, col.Column)
		}
		if !projectionColumnMatchesEmittedRelation(pred, col) {
			return fmt.Errorf("%w: projection column %d must come from the emitted relation", ErrInvalidPredicate, i)
		}
		if !s.TableExists(col.Table) {
			return fmt.Errorf("%w: projection column %d table %d", ErrTableNotFound, i, col.Table)
		}
		if !s.ColumnExists(col.Table, col.Column) {
			return fmt.Errorf("%w: projection column %d table %d column %d", ErrColumnNotFound, i, col.Table, col.Column)
		}
		if want := s.ColumnType(col.Table, col.Column); col.Schema.Type != want {
			return fmt.Errorf("%w: projection column %d kind %s does not match column kind %s", ErrInvalidPredicate, i, col.Schema.Type, want)
		}
	}
	return nil
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
		if p.ProjectedRelation < 0 || p.ProjectedRelation >= len(p.Relations) {
			return false
		}
		rel := p.Relations[p.ProjectedRelation]
		return col.Table == rel.Table && col.Alias == rel.Alias
	default:
		tables := pred.Tables()
		return len(tables) == 1 && col.Table == tables[0] && col.Alias == 0
	}
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
