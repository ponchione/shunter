package store

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

var (
	ErrTableNotFound = errors.New("table not found")
	// ErrColumnNotFound is re-exported from SPEC-006 §13 so store-layer
	// integrity paths can construct and match the sentinel via errors.Is
	// across the schema/store boundary.
	ErrColumnNotFound            = schema.ErrColumnNotFound
	ErrTypeMismatch              = errors.New("type mismatch")
	ErrPrimaryKeyViolation       = errors.New("primary key violation")
	ErrUniqueConstraintViolation = errors.New("unique constraint violation")
	ErrDuplicateRow              = errors.New("duplicate row")
	ErrDuplicateRowID            = errors.New("duplicate row id")
	ErrRowNotFound               = errors.New("row not found")
	ErrNullNotAllowed            = errors.New("null not allowed")
	ErrInvalidFloat              = types.ErrInvalidFloat
	ErrRowShapeMismatch          = errors.New("row shape mismatch")
	ErrRowIDOverflow             = errors.New("row id overflow")
	ErrTransactionRolledBack     = errors.New("transaction rolled back")
	ErrTransactionClosed         = errors.New("transaction closed")
)

// TypeMismatchError is returned when a column value doesn't match the schema type.
type TypeMismatchError struct {
	Column   string
	Expected string
	Got      string
}

func (e *TypeMismatchError) Error() string {
	return fmt.Sprintf("type mismatch on column %q: expected %s, got %s", e.Column, e.Expected, e.Got)
}

func (e *TypeMismatchError) Unwrap() error { return ErrTypeMismatch }

// RowShapeMismatchError is returned when a row width doesn't match the schema.
type RowShapeMismatchError struct {
	Expected int
	Got      int
}

func (e *RowShapeMismatchError) Error() string {
	return fmt.Sprintf("row shape mismatch: expected %d columns, got %d", e.Expected, e.Got)
}

func (e *RowShapeMismatchError) Unwrap() error { return ErrRowShapeMismatch }

// PrimaryKeyViolationError is returned when a PK uniqueness check fails.
type PrimaryKeyViolationError struct {
	TableName string
	IndexName string
	Key       IndexKey
}

func (e *PrimaryKeyViolationError) Error() string {
	return fmt.Sprintf("primary key violation on table %q index %q: key %v already exists", e.TableName, e.IndexName, e.Key.parts)
}

func (e *PrimaryKeyViolationError) Unwrap() error { return ErrPrimaryKeyViolation }

// UniqueConstraintViolationError is returned when a unique index check fails.
type UniqueConstraintViolationError struct {
	TableName string
	IndexName string
	Key       IndexKey
}

func (e *UniqueConstraintViolationError) Error() string {
	return fmt.Sprintf("unique constraint violation on table %q index %q: key %v already exists", e.TableName, e.IndexName, e.Key.parts)
}

func (e *UniqueConstraintViolationError) Unwrap() error { return ErrUniqueConstraintViolation }

func uniqueViolationError(table *Table, idx *Index, key IndexKey) error {
	if idx.schema.Primary {
		return &PrimaryKeyViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
	}
	return &UniqueConstraintViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
}
