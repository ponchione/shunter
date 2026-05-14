package auth

import (
	"errors"
	"fmt"
	"slices"
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

const (
	MaxIssuerBytes  = 1024
	MaxSubjectBytes = 1024
)

// JWTConfig is the engine-level auth configuration. SigningKey
// interpretation is algorithm-dependent — HMAC secret for HS*, PEM
// public key for RS*/ES*. Story 2.2 exposes the HS256 path; other
// algorithms can be added once external IdP integration is in scope.
type JWTConfig struct {
	SigningKey []byte
	Issuers    []string // empty = skip issuer allowlist validation
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

// Principal returns the generic external-auth principal represented by c.
func (c *Claims) Principal() types.AuthPrincipal {
	if c == nil {
		return types.AuthPrincipal{}
	}
	return types.AuthPrincipal{
		Issuer:      c.Issuer,
		Subject:     c.Subject,
		Audience:    append([]string(nil), c.Audience...),
		Permissions: append([]string(nil), c.Permissions...),
	}
}

// Validation error sentinels (SPEC-005 §4.3).
var (
	ErrJWTInvalid             = errors.New("auth: JWT invalid")
	ErrJWTMissingClaim        = errors.New("auth: JWT missing required claim")
	ErrJWTIssuerMismatch      = errors.New("auth: JWT issuer not in configured list")
	ErrJWTHexIdentityMismatch = errors.New("auth: hex_identity does not match derived identity")
	ErrJWTAudienceMismatch    = errors.New("auth: JWT audience not in configured list")
	ErrJWTClaimTooLarge       = errors.New("auth: JWT claim too large")
)

// ValidateJWT parses + verifies tokenString against config and
// normalizes its claims. Signature errors, expiry, future `iat`/`nbf`,
// missing sub/iss, mismatched hex_identity, and audience-policy violations all
// map to dedicated sentinels so the transport layer can produce the right HTTP
// 401 responses (SPEC-005 §4.3).
func ValidateJWT(tokenString string, config *JWTConfig) (*Claims, error) {
	if config == nil {
		return nil, fmt.Errorf("%w: config is required", ErrJWTInvalid)
	}
	if len(config.SigningKey) == 0 {
		return nil, fmt.Errorf("%w: signing key is required", ErrJWTInvalid)
	}
	parsed, err := jwt.Parse(tokenString, func(*jwt.Token) (any, error) {
		return config.SigningKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuedAt())
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
	if len(sub) > MaxSubjectBytes {
		return nil, fmt.Errorf("%w: sub exceeds %d bytes", ErrJWTClaimTooLarge, MaxSubjectBytes)
	}
	if len(iss) > MaxIssuerBytes {
		return nil, fmt.Errorf("%w: iss exceeds %d bytes", ErrJWTClaimTooLarge, MaxIssuerBytes)
	}
	if len(config.Issuers) > 0 && !slices.Contains(config.Issuers, iss) {
		return nil, fmt.Errorf("%w: got %q, allowed %v", ErrJWTIssuerMismatch, iss, config.Issuers)
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
	c.Permissions = extractStringListClaim(mc["permissions"], false)

	// Audience: normalize to []string and optionally enforce policy.
	c.Audience = extractStringListClaim(mc["aud"], true)
	if len(config.Audiences) > 0 {
		if !audienceAllowed(c.Audience, config.Audiences) {
			return nil, fmt.Errorf("%w: got %v, allowed %v", ErrJWTAudienceMismatch, c.Audience, config.Audiences)
		}
	}

	return c, nil
}

// extractStringListClaim normalizes JSON-loose claim shapes: string,
// []any with string elements, or missing. allowEmpty preserves the
// JWT audience behavior while permissions continue to drop empty tags.
func extractStringListClaim(raw any, allowEmpty bool) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		if v == "" && !allowEmpty {
			return nil
		}
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && (allowEmpty || s != "") {
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
		if slices.Contains(tokenAud, want) {
			return true
		}
	}
	return false
}
