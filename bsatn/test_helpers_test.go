package bsatn

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func mustFloat32Value(tb testing.TB, x float32) types.Value {
	tb.Helper()
	v, err := types.NewFloat32(x)
	if err != nil {
		tb.Fatal(err)
	}
	return v
}

func mustFloat64Value(tb testing.TB, x float64) types.Value {
	tb.Helper()
	v, err := types.NewFloat64(x)
	if err != nil {
		tb.Fatal(err)
	}
	return v
}

func mustJSONValue(tb testing.TB, raw string) types.Value {
	if tb != nil {
		tb.Helper()
	}
	v, err := types.NewJSON([]byte(raw))
	if err != nil {
		if tb != nil {
			tb.Fatal(err)
		}
		panic(err)
	}
	return v
}
