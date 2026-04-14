package types

import "testing"

// =============================================================
// Story 1.5: ProductValue
// =============================================================

func TestProductValueEqualSame(t *testing.T) {
	a := ProductValue{NewInt64(1), NewString("x")}
	b := ProductValue{NewInt64(1), NewString("x")}
	if !a.Equal(b) {
		t.Fatal("same values, same order should be equal")
	}
}

func TestProductValueEqualDifferentOrder(t *testing.T) {
	a := ProductValue{NewInt64(1), NewString("x")}
	b := ProductValue{NewString("x"), NewInt64(1)}
	if a.Equal(b) {
		t.Fatal("same values, different order should not be equal")
	}
}

func TestProductValueEqualDifferentLength(t *testing.T) {
	a := ProductValue{NewInt64(1)}
	b := ProductValue{NewInt64(1), NewInt64(2)}
	if a.Equal(b) {
		t.Fatal("different length should not be equal")
	}
}

func TestProductValueHashEqual(t *testing.T) {
	a := ProductValue{NewInt64(1), NewString("x")}
	b := ProductValue{NewInt64(1), NewString("x")}
	if a.Hash64() != b.Hash64() {
		t.Fatal("equal rows should produce equal hashes")
	}
}

func TestProductValueHashConcatenationAmbiguity(t *testing.T) {
	// ("a", "bc") vs ("ab", "c") must not collide
	a := ProductValue{NewString("a"), NewString("bc")}
	b := ProductValue{NewString("ab"), NewString("c")}
	if a.Hash64() == b.Hash64() {
		t.Fatal(`("a","bc") and ("ab","c") should produce different hashes`)
	}
}

func TestProductValueCopyIsolation(t *testing.T) {
	orig := ProductValue{NewInt64(1), NewBytes([]byte{10, 20})}
	cp := orig.Copy()

	// Mutating copy should not affect original.
	cp[0] = NewInt64(999)
	if orig[0].AsInt64() != 1 {
		t.Fatal("mutating copy affected original (slice element)")
	}
}

func TestProductValueCopyBytesIsolation(t *testing.T) {
	orig := ProductValue{NewBytes([]byte{1, 2, 3})}
	cp := orig.Copy()

	// Mutate the bytes buffer inside the copy.
	cp[0].buf[0] = 0xFF
	if orig[0].AsBytes()[0] == 0xFF {
		t.Fatal("mutating copied Bytes value affected original")
	}
}

func TestProductValueEmptyEqual(t *testing.T) {
	a := ProductValue{}
	b := ProductValue{}
	if !a.Equal(b) {
		t.Fatal("empty ProductValues should be equal")
	}
}

func TestProductValueEmptyHash(t *testing.T) {
	// Should not panic.
	pv := ProductValue{}
	_ = pv.Hash64()
}

func TestProductValueCopyNil(t *testing.T) {
	var pv ProductValue
	cp := pv.Copy()
	if cp != nil {
		t.Fatal("Copy of nil ProductValue should be nil")
	}
}
