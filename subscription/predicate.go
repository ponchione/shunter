// Package subscription implements the subscription evaluator (SPEC-004).
//
// The evaluator answers one question after every committed transaction:
// which clients care about this change, and what exactly changed in their
// view of the data?
//
// This file provides the structured predicate tree consumed by every
// downstream subsystem (pruning, delta computation, validation).
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

// IndexResolver is the canonical schema-layer surface for resolving a
// single-column index ID; re-exported here for subscription callers so
// they do not need to import schema directly. SPEC-006 §7 declares the
// canonical interface in schema/registry.go.
type IndexResolver = schema.IndexResolver

// Value is the tagged-union column value used in predicates.
type Value = types.Value

// ValueKind re-exports the column kind enum used by SchemaLookup.
type ValueKind = types.ValueKind

// Predicate is a filter expression over rows from one or two tables.
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
//
//	Example: messages.channel_id = 42
//
// Alias is the relation-instance tag that disambiguates which side of a
// self-join the leaf applies to. For distinct-table joins and single-table
// predicates it is left at its zero default — the Table check in MatchRow is
// sufficient to route the leaf to the correct side. For self-join filters
// (Join.Left == Join.Right) compile stamps 0 when the leaf names the left
// alias and 1 when it names the right alias, mirroring Join.LeftAlias /
// Join.RightAlias.
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

// Join matches rows from two tables joined on a column pair,
// with an optional filter on either side.
//
// LeftAlias and RightAlias are opaque relation-instance tags that distinguish
// the two sides when Left == Right (aliased self-join). For distinct-table
// joins they are left at their zero default — Left != Right is sufficient to
// disambiguate the sides. Validation rejects Left == Right with equal
// aliases, since that would describe a degenerate single-relation join.
//
// ProjectRight selects which side of the joined pair survives at row
// emission time. False (the zero value) projects the Left side (the classical
// `SELECT lhs.*` shape); true projects the Right side (`SELECT rhs.*`).
// Reference: SubscriptionPlan::subscribed_table_id at
// reference/SpacetimeDB/crates/subscription/src/lib.rs:367 — each plan
// returns rows shaped like one concrete table.
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

// Bound describes one end of a ColRange. If Unbounded is true, Value and
// Inclusive are ignored and the range is open on that side.
type Bound struct {
	Value     Value
	Inclusive bool
	Unbounded bool
}
