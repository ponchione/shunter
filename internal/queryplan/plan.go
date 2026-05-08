// Package queryplan derives execution-facing SQL metadata from parsed query
// text before protocol compilation resolves schema objects.
package queryplan

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/query/sql"
)

// Options controls which parsed SQL features are admitted by the caller's
// read surface.
type Options struct {
	AllowLimit   bool
	AllowOrderBy bool
	AllowOffset  bool
}

// Plan is the parser/planner boundary consumed by protocol SQL compilation.
// It preserves the parsed statement while carrying derived metadata that
// downstream schema resolution should not recompute.
type Plan struct {
	Statement           sql.Statement
	OrderBy             []sql.OrderByColumn
	NormalizedPredicate sql.Predicate
	UsesCallerIdentity  bool
}

// Build parses query text and applies parser-level feature gates shared by
// one-off queries, subscriptions, declared queries, and declared views.
func Build(query string, opts Options) (Plan, error) {
	stmt, err := sql.Parse(query)
	if err != nil {
		return Plan{}, normalizeParseError(err, opts.AllowLimit)
	}
	orderBy := OrderByColumns(stmt)
	if stmt.UnsupportedLimit {
		return Plan{}, sql.UnsupportedFeatureError{SQL: query}
	}
	if stmt.UnsupportedOffset {
		return Plan{}, sql.UnsupportedFeatureError{SQL: query}
	}
	if !opts.AllowOrderBy && len(orderBy) != 0 {
		return Plan{}, sql.UnsupportedFeatureError{SQL: query}
	}
	if !opts.AllowLimit && stmt.HasLimit {
		return Plan{}, sql.UnsupportedFeatureError{SQL: query}
	}
	if !opts.AllowOffset && stmt.HasOffset {
		return Plan{}, sql.UnsupportedFeatureError{SQL: query}
	}
	normalized := NormalizePredicate(stmt.Predicate)
	return Plan{
		Statement:           stmt,
		OrderBy:             orderBy,
		NormalizedPredicate: normalized,
		UsesCallerIdentity:  PredicateUsesCallerIdentity(normalized),
	}, nil
}

func normalizeParseError(err error, allowLimit bool) error {
	// Typed compile errors already carry the final user-facing text.
	var dupErr sql.DuplicateNameError
	if errors.As(err, &dupErr) {
		return err
	}
	var unresolvedErr sql.UnresolvedVarError
	if errors.As(err, &unresolvedErr) {
		return err
	}
	var unsupSelectErr sql.UnsupportedSelectError
	if errors.As(err, &unsupSelectErr) {
		if !allowLimit && unsupSelectErr.HasLimit {
			return sql.UnsupportedFeatureError{SQL: unsupSelectErr.SQL}
		}
		return err
	}
	var unqualErr sql.UnqualifiedNamesError
	if errors.As(err, &unqualErr) {
		return err
	}
	var joinTypeErr sql.UnsupportedJoinTypeError
	if errors.As(err, &joinTypeErr) {
		return err
	}
	var unsupExprErr sql.UnsupportedExprError
	if errors.As(err, &unsupExprErr) {
		return err
	}
	return fmt.Errorf("parse: %w", err)
}

// OrderByColumns returns the complete ORDER BY term list while preserving
// compatibility with statements parsed before the multi-column field existed.
func OrderByColumns(stmt sql.Statement) []sql.OrderByColumn {
	if len(stmt.OrderByColumns) != 0 {
		return stmt.OrderByColumns
	}
	if stmt.OrderBy != nil {
		return []sql.OrderByColumn{*stmt.OrderBy}
	}
	return nil
}

// NormalizePredicate folds SQL boolean constants after callers have retained
// the original predicate tree for schema/type validation.
func NormalizePredicate(pred sql.Predicate) sql.Predicate {
	switch p := pred.(type) {
	case sql.AndPredicate:
		left := NormalizePredicate(p.Left)
		right := NormalizePredicate(p.Right)
		if isSQLFalsePredicate(left) {
			return left
		}
		if isSQLFalsePredicate(right) {
			return right
		}
		if isSQLTruePredicate(left) {
			return right
		}
		if isSQLTruePredicate(right) {
			return left
		}
		return sql.AndPredicate{Left: left, Right: right}
	case sql.OrPredicate:
		left := NormalizePredicate(p.Left)
		right := NormalizePredicate(p.Right)
		if isSQLTruePredicate(left) || isSQLTruePredicate(right) {
			return sql.TruePredicate{}
		}
		if isSQLFalsePredicate(left) {
			return right
		}
		if isSQLFalsePredicate(right) {
			return left
		}
		return sql.OrPredicate{Left: left, Right: right}
	default:
		return pred
	}
}

func isSQLTruePredicate(pred sql.Predicate) bool {
	_, ok := pred.(sql.TruePredicate)
	return ok
}

func isSQLFalsePredicate(pred sql.Predicate) bool {
	_, ok := pred.(sql.FalsePredicate)
	return ok
}

// PredicateUsesCallerIdentity reports whether a normalized SQL predicate still
// depends on the :sender caller identity placeholder.
func PredicateUsesCallerIdentity(pred sql.Predicate) bool {
	switch p := pred.(type) {
	case sql.ComparisonPredicate:
		return p.Filter.Literal.Kind == sql.LitSender
	case sql.AndPredicate:
		return PredicateUsesCallerIdentity(p.Left) || PredicateUsesCallerIdentity(p.Right)
	case sql.OrPredicate:
		return PredicateUsesCallerIdentity(p.Left) || PredicateUsesCallerIdentity(p.Right)
	default:
		return false
	}
}
