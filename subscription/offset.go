package subscription

import (
	"fmt"

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
	if s == nil {
		return fmt.Errorf("%w: offset schema lookup is nil", ErrInvalidPredicate)
	}
	if aggregate != nil {
		return fmt.Errorf("%w: live OFFSET views do not support aggregate views", ErrInvalidPredicate)
	}
	if _, ok := pred.(Join); ok {
		return fmt.Errorf("%w: live OFFSET views require a single table", ErrInvalidPredicate)
	}
	if _, ok := pred.(CrossJoin); ok {
		return fmt.Errorf("%w: live OFFSET views require a single table", ErrInvalidPredicate)
	}
	if _, ok := pred.(MultiJoin); ok {
		return fmt.Errorf("%w: live OFFSET views require a single table", ErrInvalidPredicate)
	}
	tables := pred.Tables()
	if len(tables) != 1 {
		return fmt.Errorf("%w: live OFFSET views require one referenced table", ErrInvalidPredicate)
	}
	if !s.TableExists(tables[0]) {
		return fmt.Errorf("%w: OFFSET table %d", ErrTableNotFound, tables[0])
	}
	return nil
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
