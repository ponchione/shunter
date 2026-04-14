package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestQueryHashDeterministic(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	h1 := ComputeQueryHash(p, nil)
	h2 := ComputeQueryHash(p, nil)
	if h1 != h2 {
		t.Fatalf("deterministic: %v != %v", h1, h2)
	}
}

func TestQueryHashValueDifferent(t *testing.T) {
	p1 := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	p2 := ColEq{Table: 1, Column: 0, Value: types.NewUint64(43)}
	if ComputeQueryHash(p1, nil) == ComputeQueryHash(p2, nil) {
		t.Fatal("different values should produce different hashes")
	}
}

func TestQueryHashSameClient(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	id := types.Identity{1, 2, 3}
	h1 := ComputeQueryHash(p, &id)
	h2 := ComputeQueryHash(p, &id)
	if h1 != h2 {
		t.Fatalf("same client: %v != %v", h1, h2)
	}
}

func TestQueryHashDifferentClients(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	a := types.Identity{1}
	b := types.Identity{2}
	if ComputeQueryHash(p, &a) == ComputeQueryHash(p, &b) {
		t.Fatal("different clients should produce different parameterized hashes")
	}
}

func TestQueryHashNoClientVsClient(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	id := types.Identity{1}
	if ComputeQueryHash(p, nil) == ComputeQueryHash(p, &id) {
		t.Fatal("non-parameterized vs parameterized should differ")
	}
}

func TestQueryHashAndOrderMatters(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 2, Column: 0, Value: types.NewUint64(2)}
	p1 := And{Left: a, Right: b}
	p2 := And{Left: b, Right: a}
	if ComputeQueryHash(p1, nil) == ComputeQueryHash(p2, nil) {
		t.Fatal("And order matters")
	}
}

func TestQueryHashJoinFilterDiffers(t *testing.T) {
	withoutF := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, Filter: nil}
	withF := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: ColEq{Table: 2, Column: 1, Value: types.NewInt32(7)}}
	if ComputeQueryHash(withoutF, nil) == ComputeQueryHash(withF, nil) {
		t.Fatal("Join with vs without filter should differ")
	}
}

func TestQueryHashStringIs64Hex(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	h := ComputeQueryHash(p, nil)
	s := h.String()
	if len(s) != 64 {
		t.Fatalf("hex len = %d, want 64", len(s))
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			t.Fatalf("non-hex char %q in %s", c, s)
		}
	}
}

func TestQueryHashAllKindsRoundTrip(t *testing.T) {
	// Ensure all kinds can be hashed without panicking.
	f32, _ := types.NewFloat32(1.5)
	f64, _ := types.NewFloat64(2.25)
	cases := []Value{
		types.NewBool(true),
		types.NewInt8(-1),
		types.NewUint8(1),
		types.NewInt16(-1),
		types.NewUint16(1),
		types.NewInt32(-1),
		types.NewUint32(1),
		types.NewInt64(-1),
		types.NewUint64(1),
		f32,
		f64,
		types.NewString("hi"),
		types.NewBytes([]byte{1, 2, 3}),
	}
	for _, v := range cases {
		p := ColEq{Table: 1, Column: 0, Value: v}
		h := ComputeQueryHash(p, nil)
		if h == (QueryHash{}) {
			t.Fatalf("zero hash for kind %s", v.Kind())
		}
	}
}

func TestQueryHashRangeBoundDiffers(t *testing.T) {
	inclusive := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(0), Inclusive: true},
		Upper: Bound{Unbounded: true}}
	exclusive := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(0), Inclusive: false},
		Upper: Bound{Unbounded: true}}
	if ComputeQueryHash(inclusive, nil) == ComputeQueryHash(exclusive, nil) {
		t.Fatal("inclusive vs exclusive lower bound should differ")
	}
}
