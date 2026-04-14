package types

import (
	"errors"
	"testing"
)

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

func TestParseIdentityHexWrongLength(t *testing.T) {
	for _, s := range []string{"", "ab", "0123"} {
		_, err := ParseIdentityHex(s)
		if !errors.Is(err, ErrInvalidIdentityHex) {
			t.Errorf("len %d: got %v, want ErrInvalidIdentityHex", len(s), err)
		}
	}
}

func TestParseIdentityHexNonHex(t *testing.T) {
	// 64 chars but with invalid characters.
	bad := "z" + string(make([]byte, 63))
	for i := 1; i < 64; i++ {
		bad = bad[:i] + "0" + bad[i+1:]
	}
	_, err := ParseIdentityHex(bad)
	if !errors.Is(err, ErrInvalidIdentityHex) {
		t.Errorf("got %v, want ErrInvalidIdentityHex", err)
	}
}
