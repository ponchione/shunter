package types

import "errors"

// IsZero reports whether id is the all-zero Identity.
func (id Identity) IsZero() bool {
	return id == Identity{}
}

// Hex returns the canonical 64-char lowercase hex encoding of id
// (SPEC-005 §4.1 canonical string form).
func (id Identity) Hex() string {
	return hexString(id[:])
}

// ErrInvalidIdentityHex is returned when ParseIdentityHex receives a
// string of wrong length or containing non-hex characters.
var ErrInvalidIdentityHex = errors.New("types: invalid identity hex")

// ParseIdentityHex decodes the canonical hex form produced by Hex.
// Returns ErrInvalidIdentityHex for any length mismatch or decode
// failure.
func ParseIdentityHex(s string) (Identity, error) {
	var id Identity
	b, err := parseFixedHex(s, 64, ErrInvalidIdentityHex)
	if err != nil {
		return id, err
	}
	copy(id[:], b)
	return id, nil
}
