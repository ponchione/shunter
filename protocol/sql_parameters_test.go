package protocol

import (
	"bytes"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func TestCompileSQLQueryStringWithParametersCopiesBoundValue(t *testing.T) {
	sl := newMockSchema("files", 1,
		schema.ColumnSchema{Index: 0, Name: "body", Type: schema.KindBytes},
	)
	raw := []byte{1, 2, 3}
	compiled, err := CompileSQLQueryStringWithParameters(
		"SELECT * FROM files WHERE body = :body",
		sl,
		nil,
		SQLQueryValidationOptions{},
		[]SQLQueryParameterValue{{Name: "body", Value: types.NewBytesOwned(raw)}},
	)
	if err != nil {
		t.Fatalf("CompileSQLQueryStringWithParameters: %v", err)
	}
	raw[0] = 9

	pred, ok := compiled.Predicate().(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicate type = %T, want subscription.ColEq", compiled.Predicate())
	}
	got := pred.Value.AsBytes()
	if want := []byte{1, 2, 3}; !bytes.Equal(got, want) {
		t.Fatalf("bound value = %v, want %v", got, want)
	}
}
