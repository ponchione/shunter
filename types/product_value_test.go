package types

import (
	"hash/fnv"
	"testing"
)

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

func TestProductValueCopyArrayStringIsolation(t *testing.T) {
	const seed = uint64(0xa77157)
	orig := ProductValue{NewUint64(1), NewArrayString([]string{"alpha", "beta"})}
	cp := orig.Copy()
	if !cp.Equal(orig) {
		t.Fatalf("seed=%#x op_index=0 operation=copy observed=%#v expected=%#v", seed, cp, orig)
	}

	cp[1].strArr[0] = "copy-mutated"
	if got := orig[1].AsArrayString()[0]; got != "alpha" {
		t.Fatalf("seed=%#x op_index=1 operation=mutate-copy-array observed_source=%q expected=%q", seed, got, "alpha")
	}

	orig[1].strArr[1] = "source-mutated"
	if got := cp[1].AsArrayString()[1]; got != "beta" {
		t.Fatalf("seed=%#x op_index=2 operation=mutate-source-array observed_copy=%q expected=%q", seed, got, "beta")
	}
}

func TestProductValueCopyUUIDValue(t *testing.T) {
	u := [16]byte{0: 1, 15: 2}
	orig := ProductValue{NewUUID(u)}
	cp := orig.Copy()
	if !cp.Equal(orig) {
		t.Fatalf("copied UUID row = %#v, want %#v", cp, orig)
	}
	cp[0] = NewUUID([16]byte{0: 9})
	if got := orig[0].AsUUID(); got != u {
		t.Fatalf("mutating copied UUID value affected original: got %x want %x", got, u)
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

func TestProductValueHash64MatchesStreamingHash(t *testing.T) {
	pv := ProductValue{
		NewUint64(1),
		NewString("a"),
		NewBytes([]byte{2, 3}),
		NewNull(KindString),
		NewArrayString([]string{"x", "yz"}),
	}
	h := fnv.New64a()
	pv.Hash(h)
	if got, want := pv.Hash64(), h.Sum64(); got != want {
		t.Fatalf("ProductValue.Hash64 = %d, want streaming hash %d", got, want)
	}
}

func TestProductValueCopyNil(t *testing.T) {
	var pv ProductValue
	cp := pv.Copy()
	if cp != nil {
		t.Fatal("Copy of nil ProductValue should be nil")
	}
}

func TestCopyProductValuesDetachmentMetamorphic(t *testing.T) {
	const seed = uint64(0xc0fefeed)
	rows := []ProductValue{
		{NewUint64(1), NewString("alice"), NewBytes([]byte{1, 2, 3})},
		{NewUint64(2), NewString("bob"), NewBytes([]byte{4, 5, 6})},
	}
	copied := CopyProductValues(rows)
	assertProductRowsEqual(t, seed, 0, "copy", copied, rows)

	copied[0][0] = NewUint64(99)
	copied[0][2].buf[0] = 0xaa
	if rows[0][0].AsUint64() != 1 || rows[0][2].AsBytes()[0] != 1 {
		t.Fatalf("seed=%#x op_index=1 runtime_config=rows=%d operation=mutate-copy observed_source=%#v expected_source=%#v",
			seed, len(rows), rows[0], ProductValue{NewUint64(1), NewString("alice"), NewBytes([]byte{1, 2, 3})})
	}

	rows[1][1] = NewString("source-mutated")
	rows[1][2].buf[1] = 0xbb
	if copied[1][1].AsString() != "bob" || copied[1][2].AsBytes()[1] != 5 {
		t.Fatalf("seed=%#x op_index=2 runtime_config=rows=%d operation=mutate-source observed_copy=%#v expected_copy=%#v",
			seed, len(rows), copied[1], ProductValue{NewUint64(2), NewString("bob"), NewBytes([]byte{4, 5, 6})})
	}

	secondCopy := CopyProductValues(copied)
	assertProductRowsEqual(t, seed, 3, "copy-mutated-copy", secondCopy, copied)
	secondCopy[0][2].buf[1] = 0xcc
	if copied[0][2].AsBytes()[1] != 2 {
		t.Fatalf("seed=%#x op_index=4 runtime_config=rows=%d operation=mutate-second-copy observed_original_copy=%#v expected_byte=2",
			seed, len(rows), copied[0])
	}
}

func assertProductRowsEqual(t *testing.T, seed uint64, opIndex int, operation string, got, expected []ProductValue) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("seed=%#x op_index=%d runtime_config=rows=%d operation=%s observed_len=%d expected_len=%d",
			seed, opIndex, len(expected), operation, len(got), len(expected))
	}
	for rowIndex := range expected {
		if !got[rowIndex].Equal(expected[rowIndex]) {
			t.Fatalf("seed=%#x op_index=%d runtime_config=rows=%d operation=%s row=%d observed=%#v expected=%#v",
				seed, opIndex, len(expected), operation, rowIndex, got[rowIndex], expected[rowIndex])
		}
	}
}
