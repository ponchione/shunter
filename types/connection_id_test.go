package types

import (
	"errors"
	"testing"
)

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

func TestParseConnectionIDHexWrongLength(t *testing.T) {
	for _, s := range []string{"", "ab", "0123", "0123456789abcdef"} {
		_, err := ParseConnectionIDHex(s)
		if !errors.Is(err, ErrInvalidConnectionIDHex) {
			t.Errorf("len %d: got %v, want ErrInvalidConnectionIDHex", len(s), err)
		}
	}
}

func TestParseConnectionIDHexNonHex(t *testing.T) {
	bad := "z0123456789abcdef0123456789abcde" // 32 chars with leading z
	_, err := ParseConnectionIDHex(bad)
	if !errors.Is(err, ErrInvalidConnectionIDHex) {
		t.Errorf("got %v, want ErrInvalidConnectionIDHex", err)
	}
}
