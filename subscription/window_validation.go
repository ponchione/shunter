package subscription

import "fmt"

func validateWindowSingleTable(label, schemaLabel string, pred Predicate, aggregate *Aggregate, s SchemaLookup) (TableID, error) {
	if s == nil {
		return 0, fmt.Errorf("%w: %s schema lookup is nil", ErrInvalidPredicate, schemaLabel)
	}
	if aggregate != nil {
		return 0, fmt.Errorf("%w: live %s views do not support aggregate views", ErrInvalidPredicate, label)
	}
	switch pred.(type) {
	case Join, CrossJoin, MultiJoin:
		return 0, fmt.Errorf("%w: live %s views require a single table", ErrInvalidPredicate, label)
	}
	tables := pred.Tables()
	if len(tables) != 1 {
		return 0, fmt.Errorf("%w: live %s views require one referenced table", ErrInvalidPredicate, label)
	}
	table := tables[0]
	if !s.TableExists(table) {
		return 0, fmt.Errorf("%w: %s table %d", ErrTableNotFound, label, table)
	}
	return table, nil
}
