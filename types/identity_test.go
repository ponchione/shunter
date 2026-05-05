package types

import "testing"

func TestIdentityIsZero(t *testing.T) {
	var zero Identity
	if !zero.IsZero() {
		t.Error("zero Identity should be IsZero")
	}

	var nonzero Identity
	nonzero[17] = 1
	if nonzero.IsZero() {
		t.Error("non-zero Identity must not be IsZero")
	}
}

func TestIdentityHexRoundTrip(t *testing.T) {
	var id Identity
	for i := range id {
		id[i] = byte(i)
	}
	s := id.Hex()
	if len(s) != 64 {
		t.Fatalf("hex length = %d, want 64", len(s))
	}
	back, err := ParseIdentityHex(s)
	if err != nil {
		t.Fatal(err)
	}
	if back != id {
		t.Errorf("round-trip mismatch: got % x, want % x", back, id)
	}
}
