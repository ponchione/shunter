package autoincrement

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestValueAsUint64(t *testing.T) {
	tests := []struct {
		name  string
		value types.Value
		kind  schema.ValueKind
		want  uint64
		ok    bool
	}{
		{name: "positive int8", value: types.NewInt8(7), kind: schema.KindInt8, want: 7, ok: true},
		{name: "negative int8", value: types.NewInt8(-1), kind: schema.KindInt8},
		{name: "positive int64", value: types.NewInt64(9), kind: schema.KindInt64, want: 9, ok: true},
		{name: "negative int64", value: types.NewInt64(-1), kind: schema.KindInt64},
		{name: "uint64 max", value: types.NewUint64(^uint64(0)), kind: schema.KindUint64, want: ^uint64(0), ok: true},
		{name: "null", value: types.NewNull(schema.KindUint64), kind: schema.KindUint64},
		{name: "unsupported kind", value: types.NewString("7"), kind: schema.KindString},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ValueAsUint64(tt.value, tt.kind)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("ValueAsUint64() = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}
