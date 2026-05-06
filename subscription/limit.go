package subscription

import (
	"fmt"

	"github.com/ponchione/shunter/types"
)

func copyRowLimit(in *uint64) *uint64 {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// ValidateLimit checks the narrow executable live LIMIT surface.
func ValidateLimit(pred Predicate, limit *uint64, aggregate *Aggregate, s SchemaLookup) error {
	return validateRowLimit(pred, limit, aggregate, s)
}

func validateRowLimit(pred Predicate, limit *uint64, aggregate *Aggregate, s SchemaLookup) error {
	if limit == nil {
		return nil
	}
	if s == nil {
		return fmt.Errorf("%w: limit schema lookup is nil", ErrInvalidPredicate)
	}
	if aggregate != nil {
		return fmt.Errorf("%w: live LIMIT views do not support aggregate views", ErrInvalidPredicate)
	}
	if _, ok := pred.(Join); ok {
		return fmt.Errorf("%w: live LIMIT views require a single table", ErrInvalidPredicate)
	}
	if _, ok := pred.(CrossJoin); ok {
		return fmt.Errorf("%w: live LIMIT views require a single table", ErrInvalidPredicate)
	}
	if _, ok := pred.(MultiJoin); ok {
		return fmt.Errorf("%w: live LIMIT views require a single table", ErrInvalidPredicate)
	}
	tables := pred.Tables()
	if len(tables) != 1 {
		return fmt.Errorf("%w: live LIMIT views require one referenced table", ErrInvalidPredicate)
	}
	if !s.TableExists(tables[0]) {
		return fmt.Errorf("%w: LIMIT table %d", ErrTableNotFound, tables[0])
	}
	return nil
}

func limitInitialRows(rows []types.ProductValue, limit *uint64) []types.ProductValue {
	if len(rows) == 0 || limit == nil {
		return rows
	}
	if *limit == 0 {
		return nil
	}
	if uint64(len(rows)) <= *limit {
		return rows
	}
	return rows[:int(*limit)]
}
