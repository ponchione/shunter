package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ponchione/shunter/types"
)

// AuthMode controls whether the engine requires a valid externally
// issued JWT (Strict) or mints a fresh one for callers that connect
// without a token (Anonymous) per SPEC-005 §4.2.
type AuthMode uint8

const (
	AuthModeStrict AuthMode = iota
	AuthModeAnonymous
)

// JWTConfig is the engine-level auth configuration. SigningKey
// interpretation is algorithm-dependent — HMAC secret for HS*, PEM
// public key for RS*/ES*. Story 2.2 exposes the HS256 path; other
// algorithms can be added once external IdP integration is in scope.
type JWTConfig struct {
	SigningKey []byte
	Audiences  []string // empty = skip audience validation (SPEC-005 §4.1)
	AuthMode   AuthMode
}

// Claims is the validated, normalized claim set returned from
// ValidateJWT. Fields map onto SPEC-005 §4.1 semantics; `HexIdentity`
// is the optional redundant identity claim carried alongside (iss,
// sub).
type Claims struct {
	Subject     string
	Issuer      string
	Audience    []string
	ExpiresAt   *time.Time // nil when the token omits `exp`
	IssuedAt    time.Time
	HexIdentity string
	// Permissions carries optional caller permission tags from the
	// `permissions` JWT claim.
	Permissions []string
}

// DeriveIdentity is a convenience wrapper: identity derivation uses
// the same (iss, sub) pair that this Claims object was validated
// against.
func (c *Claims) DeriveIdentity() types.Identity {
	return DeriveIdentity(c.Issuer, c.Subject)
}

// Validation error sentinels (SPEC-005 §4.3).
var (
	ErrJWTInvalid             = errors.New("auth: JWT invalid")
	ErrJWTMissingClaim        = errors.New("auth: JWT missing required claim")
	ErrJWTHexIdentityMismatch = errors.New("auth: hex_identity does not match derived identity")
	ErrJWTAudienceMismatch    = errors.New("auth: JWT audience not in configured list")
)

// ValidateJWT parses + verifies tokenString against config and
// normalizes its claims. Signature errors, expiry, missing sub/iss,
// mismatched hex_identity, and audience-policy violations all map to
// dedicated sentinels so the transport layer can produce the right
// HTTP 401 responses (SPEC-005 §4.3).
func ValidateJWT(tokenString string, config *JWTConfig) (*Claims, error) {
	parsed, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		// v1 supports HS256 only. Reject unexpected methods so an
		// attacker can't downgrade to `alg: none` or request a
		// different algorithm whose key material we haven't
		// configured.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: unsupported signing alg %v", ErrJWTInvalid, t.Header["alg"])
		}
		return config.SigningKey, nil
	})
	if err != nil {
		return nil, errors.Join(ErrJWTInvalid, err)
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("%w: token reported invalid", ErrJWTInvalid)
	}

	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("%w: unexpected claims type %T", ErrJWTInvalid, parsed.Claims)
	}

	c := &Claims{}
	sub, _ := mc["sub"].(string)
	if sub == "" {
		return nil, fmt.Errorf("%w: sub", ErrJWTMissingClaim)
	}
	iss, _ := mc["iss"].(string)
	if iss == "" {
		return nil, fmt.Errorf("%w: iss", ErrJWTMissingClaim)
	}
	c.Subject = sub
	c.Issuer = iss

	// Optional claims — presence-tolerant.
	if expFloat, ok := mc["exp"].(float64); ok {
		t := time.Unix(int64(expFloat), 0)
		c.ExpiresAt = &t
	}
	if iatFloat, ok := mc["iat"].(float64); ok {
		c.IssuedAt = time.Unix(int64(iatFloat), 0)
	}
	if hex, ok := mc["hex_identity"].(string); ok && hex != "" {
		expected := DeriveIdentity(iss, sub).Hex()
		if hex != expected {
			return nil, fmt.Errorf("%w: got %s, want %s", ErrJWTHexIdentityMismatch, hex, expected)
		}
		c.HexIdentity = hex
	}
	c.Permissions = extractStringClaim(mc["permissions"])

	// Audience: normalize to []string and optionally enforce policy.
	c.Audience = extractAudience(mc["aud"])
	if len(config.Audiences) > 0 {
		if !audienceAllowed(c.Audience, config.Audiences) {
			return nil, fmt.Errorf("%w: got %v, allowed %v", ErrJWTAudienceMismatch, c.Audience, config.Audiences)
		}
	}

	return c, nil
}

// extractAudience normalizes the JSON-loose `aud` shape (string, or
// []any with string elements, or missing) into []string.
func extractAudience(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func extractStringClaim(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// audienceAllowed returns true when at least one audience value in
// tokenAud appears in configuredAllowed.
func audienceAllowed(tokenAud, configuredAllowed []string) bool {
	for _, want := range configuredAllowed {
		for _, got := range tokenAud {
			if got == want {
				return true
			}
		}
	}
	return false
}
