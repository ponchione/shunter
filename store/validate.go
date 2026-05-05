package store

import (
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// ValidateRow checks that a row matches the table schema.
func ValidateRow(ts *schema.TableSchema, row types.ProductValue) error {
	if len(row) != len(ts.Columns) {
		return &RowShapeMismatchError{Expected: len(ts.Columns), Got: len(row)}
	}
	for i, col := range ts.Columns {
		value := row[i]
		if value.Kind() != col.Type {
			return &TypeMismatchError{
				Column:   col.Name,
				Expected: col.Type.String(),
				Got:      value.Kind().String(),
			}
		}
		if value.IsNull() && !col.Nullable {
			return fmt.Errorf("%w: column %q", ErrNullNotAllowed, col.Name)
		}
	}
	return nil
}
