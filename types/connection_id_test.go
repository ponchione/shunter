package types

import "testing"

func TestConnectionIDIsZero(t *testing.T) {
	var zero ConnectionID
	if !zero.IsZero() {
		t.Error("zero ConnectionID should be IsZero")
	}
	var nz ConnectionID
	nz[3] = 0x01
	if nz.IsZero() {
		t.Error("non-zero ConnectionID must not be IsZero")
	}
}

func TestConnectionIDHexRoundTrip(t *testing.T) {
	var c ConnectionID
	for i := range c {
		c[i] = byte(i * 16)
	}
	s := c.Hex()
	if len(s) != 32 {
		t.Fatalf("hex length = %d, want 32", len(s))
	}
	back, err := ParseConnectionIDHex(s)
	if err != nil {
		t.Fatal(err)
	}
	if back != c {
		t.Errorf("round-trip mismatch: got % x, want % x", back, c)
	}
}
