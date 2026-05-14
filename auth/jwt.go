package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
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

// JWTAlgorithm identifies a JWT signing algorithm Shunter can verify locally.
type JWTAlgorithm string

const (
	JWTAlgorithmHS256 JWTAlgorithm = "HS256"
	JWTAlgorithmRS256 JWTAlgorithm = "RS256"
	JWTAlgorithmES256 JWTAlgorithm = "ES256"
)

// JWTVerificationKey is one locally configured JWT verification key.
//
// Key is an HMAC secret for HS256 and a PEM-encoded public key or certificate
// for RS256/ES256. KeyID optionally matches the token header's `kid` value for
// overlapping key rotation.
type JWTVerificationKey struct {
	Algorithm JWTAlgorithm
	KeyID     string
	Key       []byte
}

// JWTConfig is the engine-level auth configuration. SigningKey
// is the legacy HS256 verification key and remains supported for anonymous
// token minting and existing strict-mode configuration. VerificationKeys adds
// explicit local verification keys for HS256, RS256, and ES256.
type JWTConfig struct {
	SigningKey       []byte
	VerificationKeys []JWTVerificationKey
	Issuers          []string // empty = skip issuer allowlist validation
	Audiences        []string // empty = skip audience validation (SPEC-005 §4.1)
	AuthMode         AuthMode
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
	ErrJWTUnsupportedAlg      = errors.New("auth: JWT algorithm unsupported")
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
	keys, err := resolveJWTVerificationKeys(config)
	if err != nil {
		return nil, err
	}
	alg, keyID, err := jwtHeaderAlgorithmAndKeyID(tokenString)
	if err != nil {
		return nil, errors.Join(ErrJWTInvalid, err)
	}
	candidates := selectJWTVerificationKeys(keys, alg, keyID)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: %w: alg=%q kid=%q", ErrJWTInvalid, ErrJWTUnsupportedAlg, alg, keyID)
	}

	var lastErr error
	var semanticErr error
	for _, candidate := range candidates {
		parsed, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
			if token == nil || token.Method == nil || token.Method.Alg() != string(candidate.algorithm) {
				return nil, fmt.Errorf("%w: %w: alg=%q", ErrJWTInvalid, ErrJWTUnsupportedAlg, alg)
			}
			return candidate.key, nil
		}, jwt.WithValidMethods([]string{string(candidate.algorithm)}), jwt.WithIssuedAt())
		if err == nil {
			if !parsed.Valid {
				lastErr = fmt.Errorf("%w: token reported invalid", ErrJWTInvalid)
				continue
			}
			return claimsFromValidatedToken(parsed, config)
		}
		lastErr = err
		if !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
			semanticErr = err
		}
	}
	if semanticErr != nil {
		return nil, errors.Join(ErrJWTInvalid, semanticErr)
	}
	if lastErr != nil {
		return nil, errors.Join(ErrJWTInvalid, lastErr)
	}
	return nil, fmt.Errorf("%w: token reported invalid", ErrJWTInvalid)
}

// ValidateJWTConfig validates configured local verification keys without
// requiring a token. Hosts can call it at startup to catch malformed key
// material before serving protocol traffic.
func ValidateJWTConfig(config *JWTConfig) error {
	_, err := resolveJWTVerificationKeys(config)
	return err
}

type resolvedJWTVerificationKey struct {
	algorithm JWTAlgorithm
	keyID     string
	key       any
}

func resolveJWTVerificationKeys(config *JWTConfig) ([]resolvedJWTVerificationKey, error) {
	if config == nil {
		return nil, fmt.Errorf("%w: config is required", ErrJWTInvalid)
	}
	specs := make([]JWTVerificationKey, 0, len(config.VerificationKeys)+1)
	if len(config.SigningKey) != 0 {
		specs = append(specs, JWTVerificationKey{
			Algorithm: JWTAlgorithmHS256,
			Key:       append([]byte(nil), config.SigningKey...),
		})
	}
	specs = append(specs, config.VerificationKeys...)
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: signing key or verification key is required", ErrJWTInvalid)
	}
	keys := make([]resolvedJWTVerificationKey, 0, len(specs))
	for i, spec := range specs {
		key, err := resolveJWTVerificationKey(spec)
		if err != nil {
			return nil, fmt.Errorf("%w: verification key %d: %w", ErrJWTInvalid, i+1, err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func resolveJWTVerificationKey(spec JWTVerificationKey) (resolvedJWTVerificationKey, error) {
	if len(spec.Key) == 0 {
		return resolvedJWTVerificationKey{}, fmt.Errorf("key material is required")
	}
	switch spec.Algorithm {
	case JWTAlgorithmHS256:
		return resolvedJWTVerificationKey{
			algorithm: spec.Algorithm,
			keyID:     spec.KeyID,
			key:       append([]byte(nil), spec.Key...),
		}, nil
	case JWTAlgorithmRS256:
		key, err := parseRSAPublicKeyPEM(spec.Key)
		if err != nil {
			return resolvedJWTVerificationKey{}, err
		}
		return resolvedJWTVerificationKey{algorithm: spec.Algorithm, keyID: spec.KeyID, key: key}, nil
	case JWTAlgorithmES256:
		key, err := parseECDSAPublicKeyPEM(spec.Key)
		if err != nil {
			return resolvedJWTVerificationKey{}, err
		}
		return resolvedJWTVerificationKey{algorithm: spec.Algorithm, keyID: spec.KeyID, key: key}, nil
	default:
		if spec.Algorithm == "" {
			return resolvedJWTVerificationKey{}, fmt.Errorf("%w: algorithm is required", ErrJWTUnsupportedAlg)
		}
		return resolvedJWTVerificationKey{}, fmt.Errorf("%w: %s", ErrJWTUnsupportedAlg, spec.Algorithm)
	}
}

func jwtHeaderAlgorithmAndKeyID(tokenString string) (JWTAlgorithm, string, error) {
	parsed, _, err := jwt.NewParser().ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return "", "", err
	}
	if parsed == nil {
		return "", "", fmt.Errorf("token header missing")
	}
	alg, ok := parsed.Header["alg"].(string)
	if !ok || alg == "" {
		return "", "", fmt.Errorf("%w: missing alg", ErrJWTUnsupportedAlg)
	}
	var keyID string
	if raw, ok := parsed.Header["kid"]; ok {
		keyID, ok = raw.(string)
		if !ok {
			return "", "", fmt.Errorf("kid header must be a string")
		}
	}
	return JWTAlgorithm(alg), keyID, nil
}

func selectJWTVerificationKeys(keys []resolvedJWTVerificationKey, alg JWTAlgorithm, keyID string) []resolvedJWTVerificationKey {
	var algMatches []resolvedJWTVerificationKey
	for _, key := range keys {
		if key.algorithm == alg {
			algMatches = append(algMatches, key)
		}
	}
	if keyID == "" {
		return algMatches
	}
	var keyed []resolvedJWTVerificationKey
	var unkeyed []resolvedJWTVerificationKey
	for _, key := range algMatches {
		switch key.keyID {
		case keyID:
			keyed = append(keyed, key)
		case "":
			unkeyed = append(unkeyed, key)
		}
	}
	if len(keyed) != 0 {
		return keyed
	}
	return unkeyed
}

func parseRSAPublicKeyPEM(data []byte) (*rsa.PublicKey, error) {
	key, err := parsePublicKeyPEM(data)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("PEM public key is %T, want *rsa.PublicKey", key)
	}
	return rsaKey, nil
}

func parseECDSAPublicKeyPEM(data []byte) (*ecdsa.PublicKey, error) {
	key, err := parsePublicKeyPEM(data)
	if err != nil {
		return nil, err
	}
	ecdsaKey, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("PEM public key is %T, want *ecdsa.PublicKey", key)
	}
	if ecdsaKey.Curve == nil || ecdsaKey.Curve.Params() == nil || ecdsaKey.Curve.Params().BitSize != 256 {
		return nil, fmt.Errorf("ES256 requires a P-256 public key")
	}
	return ecdsaKey, nil
}

func parsePublicKeyPEM(data []byte) (any, error) {
	rest := bytes.TrimSpace(data)
	block, rest := pem.Decode(rest)
	if block == nil {
		return nil, fmt.Errorf("PEM public key is required")
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("PEM must contain exactly one public key or certificate")
	}
	switch block.Type {
	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key, nil
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	case "CERTIFICATE":
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		return cert.PublicKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

func claimsFromValidatedToken(parsed *jwt.Token, config *JWTConfig) (*Claims, error) {
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
