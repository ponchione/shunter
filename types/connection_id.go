package types

import (
	"encoding/hex"
	"errors"
	"fmt"
)

// IsZero reports whether c is the all-zero ConnectionID. The zero
// value is reserved for internal callers (no active connection).
func (c ConnectionID) IsZero() bool {
	for _, b := range c {
		if b != 0 {
			return false
		}
	}
	return true
}

// Hex returns the canonical 32-char lowercase hex encoding of c.
func (c ConnectionID) Hex() string {
	return hex.EncodeToString(c[:])
}

// ErrInvalidConnectionIDHex is returned when ParseConnectionIDHex
// receives a string of wrong length or with non-hex characters.
var ErrInvalidConnectionIDHex = errors.New("types: invalid connection_id hex")

// ParseConnectionIDHex decodes the canonical hex form produced by
// ConnectionID.Hex.
func ParseConnectionIDHex(s string) (ConnectionID, error) {
	var c ConnectionID
	if len(s) != 32 {
		return c, fmt.Errorf("%w: length %d, want 32", ErrInvalidConnectionIDHex, len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return c, fmt.Errorf("%w: %v", ErrInvalidConnectionIDHex, err)
	}
	copy(c[:], b)
	return c, nil
}
