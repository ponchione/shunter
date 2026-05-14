package bsatn

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/types"
)

var (
	ErrRowLengthMismatch = errors.New("bsatn: row length mismatch")
	ErrInvalidUTF8       = types.ErrInvalidUTF8
	ErrInvalidBool       = errors.New("bsatn: invalid bool")
	ErrInvalidPresence   = errors.New("bsatn: invalid nullable presence marker")
	ErrNullWithoutSchema = errors.New("bsatn: null value requires nullable column schema")
	ErrValueTooLarge     = errors.New("bsatn: value too large")
)

type UnknownValueTagError struct{ Tag byte }

func (e *UnknownValueTagError) Error() string {
	return fmt.Sprintf("bsatn: unknown value tag %d", e.Tag)
}

type TypeTagMismatchError struct {
	Column   string
	Expected types.ValueKind
	Got      byte
}

func (e *TypeTagMismatchError) Error() string {
	return fmt.Sprintf("bsatn: type tag mismatch on column %q: expected %s (tag %d), got tag %d",
		e.Column, e.Expected, byte(e.Expected), e.Got)
}

type RowShapeMismatchError struct {
	TableName string
	Expected  int
	Got       int
}

func (e *RowShapeMismatchError) Error() string {
	return fmt.Sprintf("bsatn: row shape mismatch on table %q: expected %d columns, got %d",
		e.TableName, e.Expected, e.Got)
}
