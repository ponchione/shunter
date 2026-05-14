package subscription

import (
	"github.com/ponchione/shunter/types"
)

func copyRowOffset(in *uint64) *uint64 {
	return copyRowLimit(in)
}

// ValidateOffset checks the narrow executable live OFFSET surface.
func ValidateOffset(pred Predicate, offset *uint64, aggregate *Aggregate, s SchemaLookup) error {
	return validateRowOffset(pred, offset, aggregate, s)
}

func validateRowOffset(pred Predicate, offset *uint64, aggregate *Aggregate, s SchemaLookup) error {
	if offset == nil {
		return nil
	}
	_, err := validateWindowSingleTable("OFFSET", "offset", pred, aggregate, s)
	return err
}

func offsetInitialRows(rows []types.ProductValue, offset *uint64) []types.ProductValue {
	if len(rows) == 0 || offset == nil || *offset == 0 {
		return rows
	}
	if uint64(len(rows)) <= *offset {
		return nil
	}
	return rows[int(*offset):]
}
