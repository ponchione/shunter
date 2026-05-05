package types

import "errors"

// IsZero reports whether c is the all-zero ConnectionID. The zero
// value is reserved for internal callers (no active connection).
func (c ConnectionID) IsZero() bool {
	return c == ConnectionID{}
}

// Hex returns the canonical 32-char lowercase hex encoding of c.
func (c ConnectionID) Hex() string {
	return hexString(c[:])
}

// ErrInvalidConnectionIDHex is returned when ParseConnectionIDHex
// receives a string of wrong length or with non-hex characters.
var ErrInvalidConnectionIDHex = errors.New("types: invalid connection_id hex")

// ParseConnectionIDHex decodes the canonical hex form produced by
// ConnectionID.Hex.
func ParseConnectionIDHex(s string) (ConnectionID, error) {
	var c ConnectionID
	b, err := parseFixedHex(s, 32, ErrInvalidConnectionIDHex)
	if err != nil {
		return c, err
	}
	copy(c[:], b)
	return c, nil
}
