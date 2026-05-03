package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ponchione/shunter/types"
)

var readRandom = rand.Read

// MintConfig is the engine-level configuration for anonymous-mode
// token minting (SPEC-005 §4.2). Deployment chooses issuer, audience,
// signing key, and expiry policy; Shunter does not impose defaults.
type MintConfig struct {
	Issuer     string
	Audience   string
	SigningKey []byte
	Expiry     time.Duration // 0 = no exp claim
}

// MintAnonymousToken creates and signs a JWT for a fresh anonymous identity.
func MintAnonymousToken(config *MintConfig) (string, types.Identity, error) {
	if config == nil {
		return "", types.Identity{}, fmt.Errorf("auth: mint config is required")
	}
	if len(config.SigningKey) == 0 {
		return "", types.Identity{}, fmt.Errorf("auth: mint signing key is required")
	}
	if config.Issuer == "" {
		return "", types.Identity{}, fmt.Errorf("auth: mint issuer is required")
	}
	subject, err := randomSubject()
	if err != nil {
		return "", types.Identity{}, fmt.Errorf("auth: mint random subject: %w", err)
	}
	identity := DeriveIdentity(config.Issuer, subject)
	now := time.Now()

	claims := jwt.MapClaims{
		"iss": config.Issuer,
		"sub": subject,
		"aud": config.Audience,
		"iat": now.Unix(),
	}
	if config.Expiry > 0 {
		claims["exp"] = now.Add(config.Expiry).Unix()
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
	if _, err := readRandom(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
