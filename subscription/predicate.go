// Package subscription evaluates registered predicates after commits and
// produces per-connection row deltas.
package subscription

import (
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// TableID is the schema-level table identifier used throughout subscription.
type TableID = schema.TableID

// IndexID is the schema-level index identifier used throughout subscription.
type IndexID = schema.IndexID

// ColID is a zero-based column index within a table schema.
type ColID = types.ColID

// IndexResolver re-exports the schema-layer single-column index resolver.
type IndexResolver = schema.IndexResolver

// Value is the tagged-union column value used in predicates.
type Value = types.Value

// ValueKind re-exports the column kind enum used by SchemaLookup.
type ValueKind = types.ValueKind

// Predicate is a filter expression over rows from one or more tables.
// The interface is sealed: external packages cannot provide implementations.
// This enforcement is what lets the pruning layer rely on an inspectable
// predicate structure (SPEC-004 §3.1).
type Predicate interface {
	// Tables returns the table IDs this predicate reads from. For And it is
	// the deduplicated union of the child tables.
	Tables() []TableID
	sealed()
}

// ColEq matches rows where a column equals a literal value.
// Alias disambiguates self-join sides.
type ColEq struct {
	Table  TableID
	Column ColID
	Alias  uint8
	Value  Value
}

func (ColEq) sealed()             {}
func (p ColEq) Tables() []TableID { return []TableID{p.Table} }

// ColNe matches rows where a column does not equal a literal value.
//
//	Example: messages.channel_id != 42
//
// See ColEq for the meaning of Alias.
type ColNe struct {
	Table  TableID
	Column ColID
	Alias  uint8
	Value  Value
}

func (ColNe) sealed()             {}
func (p ColNe) Tables() []TableID { return []TableID{p.Table} }

// ColRange matches rows where a column falls within a range.
//
//	Example: events.timestamp >= 1000 AND events.timestamp < 2000
//
// See ColEq for the meaning of Alias.
type ColRange struct {
	Table  TableID
	Column ColID
	Alias  uint8
	Lower  Bound
	Upper  Bound
}

func (ColRange) sealed()             {}
func (p ColRange) Tables() []TableID { return []TableID{p.Table} }

// ColEqCol matches joined row pairs where two columns are equal.
// Aliases disambiguate relation instances in self-joins.
type ColEqCol struct {
	LeftTable   TableID
	LeftColumn  ColID
	LeftAlias   uint8
	RightTable  TableID
	RightColumn ColID
	RightAlias  uint8
}

func (ColEqCol) sealed() {}
func (p ColEqCol) Tables() []TableID {
	if p.LeftTable == p.RightTable {
		return []TableID{p.LeftTable}
	}
	return []TableID{p.LeftTable, p.RightTable}
}

// And combines two predicates; both must match.
type And struct {
	Left  Predicate
	Right Predicate
}

func (And) sealed() {}

// Or combines two predicates; either may match.
type Or struct {
	Left  Predicate
	Right Predicate
}

func (Or) sealed() {}

// Tables returns the deduplicated union of left and right tables.
// Order is stable: left tables first, then any right-only tables.
// Nil children contribute no tables so malformed trees caught by
// ValidatePredicate do not panic here.
func (p And) Tables() []TableID {
	return unionPredicateTables(p.Left, p.Right)
}

// Tables returns the deduplicated union of left and right tables.
func (p Or) Tables() []TableID {
	return unionPredicateTables(p.Left, p.Right)
}

func unionPredicateTables(leftPred, rightPred Predicate) []TableID {
	var left, right []TableID
	if leftPred != nil {
		left = leftPred.Tables()
	}
	if rightPred != nil {
		right = rightPred.Tables()
	}
	out := make([]TableID, 0, len(left)+len(right))
	seen := make(map[TableID]struct{}, len(left)+len(right))
	for _, t := range left {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	for _, t := range right {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// AllRows matches every row in a table (no filter).
type AllRows struct {
	Table TableID
}

func (AllRows) sealed()             {}
func (p AllRows) Tables() []TableID { return []TableID{p.Table} }

// NoRows admits a query shape but guarantees it can never emit rows from the
// projected table.
type NoRows struct {
	Table TableID
}

func (NoRows) sealed()             {}
func (p NoRows) Tables() []TableID { return []TableID{p.Table} }

// Join matches rows from two tables joined on a column pair,
// with an optional filter on either side.
// Aliases distinguish self-join sides; ProjectRight selects the emitted side.
type Join struct {
	Left         TableID
	Right        TableID
	LeftCol      ColID
	RightCol     ColID
	LeftAlias    uint8
	RightAlias   uint8
	ProjectRight bool
	Filter       Predicate // optional additional filter (may be nil)
}

func (Join) sealed() {}
func (p Join) Tables() []TableID {
	if p.Left == p.Right {
		return []TableID{p.Left}
	}
	return []TableID{p.Left, p.Right}
}

// ProjectedTable returns the table ID whose row shape the subscription emits.
func (p Join) ProjectedTable() TableID {
	if p.ProjectRight {
		return p.Right
	}
	return p.Left
}

// CrossJoin matches rows from two relations with cartesian-product semantics.
//
// ProjectRight selects which side of the cartesian pair survives at row
// emission time, mirroring Join.ProjectRight for equi-joins. LeftAlias and
// RightAlias disambiguate aliased self-cross-joins when Left == Right.
type CrossJoin struct {
	Left         TableID
	Right        TableID
	LeftAlias    uint8
	RightAlias   uint8
	ProjectRight bool
	Filter       Predicate // optional additional filter (may be nil)
}

func (CrossJoin) sealed() {}
func (p CrossJoin) Tables() []TableID {
	if p.Left == p.Right {
		return []TableID{p.Left}
	}
	return []TableID{p.Left, p.Right}
}

// ProjectedTable returns the table ID whose row shape the subscription emits.
func (p CrossJoin) ProjectedTable() TableID {
	if p.ProjectRight {
		return p.Right
	}
	return p.Left
}

// MultiJoinRelation describes one relation instance in a three-or-more-way
// live join. Alias disambiguates repeated table instances.
type MultiJoinRelation struct {
	Table TableID
	Alias uint8
}

// MultiJoinColumnRef identifies a column in one relation instance.
type MultiJoinColumnRef struct {
	Relation int
	Table    TableID
	Column   ColID
	Alias    uint8
}

// MultiJoinCondition is an equality edge between two relation columns.
type MultiJoinCondition struct {
	Left  MultiJoinColumnRef
	Right MultiJoinColumnRef
}

// MultiJoin matches tuples across three or more relation instances and emits
// rows from ProjectedRelation. Filter is an optional tuple-level predicate.
type MultiJoin struct {
	Relations         []MultiJoinRelation
	Conditions        []MultiJoinCondition
	ProjectedRelation int
	Filter            Predicate
}

func (MultiJoin) sealed() {}

func (p MultiJoin) Tables() []TableID {
	out := make([]TableID, 0, len(p.Relations))
	seen := make(map[TableID]struct{}, len(p.Relations))
	for _, rel := range p.Relations {
		if _, ok := seen[rel.Table]; ok {
			continue
		}
		seen[rel.Table] = struct{}{}
		out = append(out, rel.Table)
	}
	return out
}

// ProjectedTable returns the table ID whose row shape the subscription emits.
func (p MultiJoin) ProjectedTable() TableID {
	if p.ProjectedRelation < 0 || p.ProjectedRelation >= len(p.Relations) {
		return 0
	}
	return p.Relations[p.ProjectedRelation].Table
}

// Bound describes one end of a ColRange. If Unbounded is true, Value and
// Inclusive are ignored and the range is open on that side.
type Bound struct {
	Value     Value
	Inclusive bool
	Unbounded bool
}
