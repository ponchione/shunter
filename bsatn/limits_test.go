package bsatn

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestValueCodecPayloadLimitBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		value func(*testing.T, int) types.Value
	}{
		{
			name: "string",
			value: func(_ *testing.T, n int) types.Value {
				return types.NewString(strings.Repeat("a", n))
			},
		},
		{
			name: "bytes",
			value: func(_ *testing.T, n int) types.Value {
				return types.NewBytesOwned(bytes.Repeat([]byte{'a'}, n))
			},
		},
		{
			name: "json",
			value: func(t *testing.T, n int) types.Value {
				t.Helper()
				value, err := types.NewJSON([]byte(`"` + strings.Repeat("a", n-2) + `"`))
				if err != nil {
					t.Fatalf("NewJSON length %d: %v", n, err)
				}
				return value
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name+"_at_limit", func(t *testing.T) {
			value := test.value(t, maxValuePayloadBytes)
			encoded, err := AppendValue(nil, value)
			if err != nil {
				t.Fatalf("AppendValue at limit: %v", err)
			}
			decoded, err := DecodeValue(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("DecodeValue at limit: %v", err)
			}
			if !decoded.Equal(value) {
				t.Fatal("value at limit did not round-trip")
			}
		})
		runtime.GC()

		t.Run(test.name+"_over_limit_rolls_back", func(t *testing.T) {
			prefix := []byte{0xaa, 0xbb}
			got, err := AppendValue(append([]byte(nil), prefix...), test.value(t, maxValuePayloadBytes+1))
			if !errors.Is(err, ErrValueTooLarge) {
				t.Fatalf("AppendValue over limit error = %v, want ErrValueTooLarge", err)
			}
			if !bytes.Equal(got, prefix) {
				t.Fatalf("AppendValue over limit result = %x, want prefix %x", got, prefix)
			}
		})
		runtime.GC()
	}
}

func TestArrayStringCodecItemLimitAndProductRoundTrip(t *testing.T) {
	xs := make([]string, maxArrayStringItems)
	value := types.NewArrayStringOwned(xs)
	ts := &schema.TableSchema{
		Name: "labels",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "values", Type: types.KindArrayString},
		},
	}
	encoded, err := AppendProductValueForSchema(nil, types.ProductValue{value}, ts)
	if err != nil {
		t.Fatalf("AppendProductValueForSchema at item limit: %v", err)
	}
	decoded, err := DecodeProductValueFromBytes(encoded, ts)
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes at item limit: %v", err)
	}
	if len(decoded) != 1 || len(decoded[0].ArrayStringView()) != maxArrayStringItems {
		t.Fatalf("decoded array size = %d, want %d", len(decoded[0].ArrayStringView()), maxArrayStringItems)
	}

	prefix := []byte{0xaa, 0xbb}
	tooMany := types.NewArrayStringOwned(make([]string, maxArrayStringItems+1))
	got, err := AppendValue(append([]byte(nil), prefix...), tooMany)
	if !errors.Is(err, ErrValueTooLarge) {
		t.Fatalf("AppendValue over item limit error = %v, want ErrValueTooLarge", err)
	}
	if !bytes.Equal(got, prefix) {
		t.Fatalf("AppendValue over item limit result = %x, want prefix %x", got, prefix)
	}
}

func TestArrayStringCodecAggregatePayloadLimit(t *testing.T) {
	atLimit := types.NewArrayStringOwned([]string{strings.Repeat("a", maxValuePayloadBytes-8)})
	encoded, err := AppendValue(nil, atLimit)
	if err != nil {
		t.Fatalf("AppendValue at aggregate limit: %v", err)
	}
	decoded, err := DecodeValue(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodeValue at aggregate limit: %v", err)
	}
	if !decoded.Equal(atLimit) {
		t.Fatal("array string at aggregate limit did not round-trip")
	}

	prefix := []byte{0xaa, 0xbb}
	overLimit := types.NewArrayStringOwned([]string{strings.Repeat("a", maxValuePayloadBytes-7)})
	got, err := AppendValue(append([]byte(nil), prefix...), overLimit)
	if !errors.Is(err, ErrValueTooLarge) {
		t.Fatalf("AppendValue over aggregate limit error = %v, want ErrValueTooLarge", err)
	}
	if !bytes.Equal(got, prefix) {
		t.Fatalf("AppendValue over aggregate limit result = %x, want prefix %x", got, prefix)
	}

	row := types.ProductValue{overLimit}
	got, err = AppendProductValue(append([]byte(nil), prefix...), row)
	if !errors.Is(err, ErrValueTooLarge) {
		t.Fatalf("AppendProductValue over aggregate limit error = %v, want ErrValueTooLarge", err)
	}
	if !bytes.Equal(got, prefix) {
		t.Fatalf("AppendProductValue over aggregate limit result = %x, want prefix %x", got, prefix)
	}
}
