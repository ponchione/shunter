// Package auth implements SPEC-005 §4 authentication and identity:
// Identity derivation from JWT claims, token validation, and
// anonymous-token minting.
package auth

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/ponchione/shunter/types"
)

// DeriveIdentity produces a stable 32-byte Identity from a JWT
// (issuer, subject) pair (SPEC-005 §4.1). The derivation is
// deterministic, collision-resistant, and safe against the
// "ab,c" vs "a,bc" boundary ambiguity by length-prefixing the
// issuer before feeding it to SHA-256.
func DeriveIdentity(issuer, subject string) types.Identity {
	h := sha256.New()
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(issuer)))
	h.Write(lenBuf[:])
	h.Write([]byte(issuer))
	h.Write([]byte(subject))
	var id types.Identity
	sum := h.Sum(nil)
	copy(id[:], sum)
	return id
}
