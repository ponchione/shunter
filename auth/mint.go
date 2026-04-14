package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ponchione/shunter/types"
)

// MintConfig is the engine-level configuration for anonymous-mode
// token minting (SPEC-005 §4.2). Deployment chooses issuer, audience,
// signing key, and expiry policy; Shunter does not impose defaults.
type MintConfig struct {
	Issuer     string
	Audience   string
	SigningKey []byte
	Expiry     time.Duration // 0 = no exp claim
}

// MintAnonymousToken generates a fresh anonymous Identity, builds the
// matching JWT, signs it with config.SigningKey, and returns both the
// signed token and the derived Identity (SPEC-005 §4.2).
//
// Subject entropy: 16 bytes from crypto/rand, hex-encoded. 128 bits
// of randomness is ample collision resistance for anonymous-scope
// identifiers.
func MintAnonymousToken(config *MintConfig) (string, types.Identity, error) {
	subject, err := randomSubject()
	if err != nil {
		return "", types.Identity{}, fmt.Errorf("auth: mint random subject: %w", err)
	}
	identity := DeriveIdentity(config.Issuer, subject)

	claims := jwt.MapClaims{
		"iss": config.Issuer,
		"sub": subject,
		"aud": config.Audience,
		"iat": time.Now().Unix(),
	}
	if config.Expiry > 0 {
		claims["exp"] = time.Now().Add(config.Expiry).Unix()
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(config.SigningKey)
	if err != nil {
		return "", types.Identity{}, fmt.Errorf("auth: sign minted token: %w", err)
	}
	return signed, identity, nil
}

// randomSubject returns 16 random bytes hex-encoded (32 chars).
func randomSubject() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
