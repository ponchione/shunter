package store

import (
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// ValidateRow checks that a row matches the table schema.
func ValidateRow(ts *schema.TableSchema, row types.ProductValue) error {
	if len(row) != len(ts.Columns) {
		return &RowShapeMismatchError{Expected: len(ts.Columns), Got: len(row)}
	}
	for i, col := range ts.Columns {
		if row[i].Kind() != col.Type {
			return &TypeMismatchError{
				Column:   col.Name,
				Expected: col.Type.String(),
				Got:      row[i].Kind().String(),
			}
		}
	}
	return nil
}
