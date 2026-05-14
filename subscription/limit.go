package subscription

import (
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
	_, err := validateWindowSingleTable("LIMIT", "limit", pred, aggregate, s)
	return err
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
