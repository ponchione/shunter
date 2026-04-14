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
type ColEq struct {
	Table  TableID
	Column ColID
	Value  Value
}

func (ColEq) sealed()              {}
func (p ColEq) Tables() []TableID  { return []TableID{p.Table} }

// ColRange matches rows where a column falls within a range.
//
//	Example: events.timestamp >= 1000 AND events.timestamp < 2000
type ColRange struct {
	Table  TableID
	Column ColID
	Lower  Bound
	Upper  Bound
}

func (ColRange) sealed()              {}
func (p ColRange) Tables() []TableID  { return []TableID{p.Table} }

// And combines two predicates; both must match.
type And struct {
	Left  Predicate
	Right Predicate
}

func (And) sealed() {}

// Tables returns the deduplicated union of left and right tables.
// Order is stable: left tables first, then any right-only tables.
// Nil children contribute no tables so malformed trees caught by
// ValidatePredicate do not panic here.
func (p And) Tables() []TableID {
	var left, right []TableID
	if p.Left != nil {
		left = p.Left.Tables()
	}
	if p.Right != nil {
		right = p.Right.Tables()
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

func (AllRows) sealed()              {}
func (p AllRows) Tables() []TableID  { return []TableID{p.Table} }

// Join matches rows from two tables joined on a column pair,
// with an optional filter on either side.
type Join struct {
	Left     TableID
	Right    TableID
	LeftCol  ColID
	RightCol ColID
	Filter   Predicate // optional additional filter (may be nil)
}

func (Join) sealed()             {}
func (p Join) Tables() []TableID { return []TableID{p.Left, p.Right} }

// Bound describes one end of a ColRange. If Unbounded is true, Value and
// Inclusive are ignored and the range is open on that side.
type Bound struct {
	Value     Value
	Inclusive bool
	Unbounded bool
}
