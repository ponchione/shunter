package types

import (
	"encoding/hex"
	"errors"
	"fmt"
)

// IsZero reports whether id is the all-zero Identity.
func (id Identity) IsZero() bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

// Hex returns the canonical 64-char lowercase hex encoding of id
// (SPEC-005 §4.1 canonical string form).
func (id Identity) Hex() string {
	return hex.EncodeToString(id[:])
}

// ErrInvalidIdentityHex is returned when ParseIdentityHex receives a
// string of wrong length or containing non-hex characters.
var ErrInvalidIdentityHex = errors.New("types: invalid identity hex")

// ParseIdentityHex decodes the canonical hex form produced by Hex.
// Returns ErrInvalidIdentityHex for any length mismatch or decode
// failure.
func ParseIdentityHex(s string) (Identity, error) {
	var id Identity
	if len(s) != 64 {
		return id, fmt.Errorf("%w: length %d, want 64", ErrInvalidIdentityHex, len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("%w: %v", ErrInvalidIdentityHex, err)
	}
	copy(id[:], b)
	return id, nil
}
