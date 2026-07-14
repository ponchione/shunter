package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ponchione/shunter/types"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

// mintHS256 builds an HS256-signed token for tests. The claims map is
// passed through to jwt.MapClaims verbatim, giving each test fine
// control over which claims are present or absent.
func mintHS256(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(testKey)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestValidateJWTNilConfigFailsWithoutPanic(t *testing.T) {
	s := mintHS256(t, jwt.MapClaims{"sub": "alice", "iss": "issuer"})

	claims, err := ValidateJWT(s, nil)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("ValidateJWT nil config error = %v, want ErrJWTInvalid", err)
	}
	if claims != nil {
		t.Fatalf("ValidateJWT nil config claims = %+v, want nil", claims)
	}
}

func TestValidateJWTEmptySigningKeyRejected(t *testing.T) {
	s := mintHS256(t, jwt.MapClaims{"sub": "alice", "iss": "issuer"})

	claims, err := ValidateJWT(s, &JWTConfig{})
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("ValidateJWT empty signing key error = %v, want ErrJWTInvalid", err)
	}
	if claims != nil {
		t.Fatalf("ValidateJWT empty signing key claims = %+v, want nil", claims)
	}
}

func TestValidateJWTConfigRejectsWeakHS256Keys(t *testing.T) {
	tests := []struct {
		name string
		cfg  *JWTConfig
	}{
		{
			name: "legacy signing key",
			cfg:  &JWTConfig{SigningKey: make([]byte, minHS256KeyBytes-1)},
		},
		{
			name: "explicit verification key",
			cfg: &JWTConfig{VerificationKeys: []JWTVerificationKey{{
				Algorithm: JWTAlgorithmHS256,
				Key:       make([]byte, minHS256KeyBytes-1),
			}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJWTConfig(tt.cfg)
			if !errors.Is(err, ErrJWTInvalid) {
				t.Fatalf("ValidateJWTConfig error = %v, want ErrJWTInvalid", err)
			}
			if !strings.Contains(err.Error(), "at least 32 bytes") {
				t.Fatalf("ValidateJWTConfig error = %v, want minimum-key context", err)
			}
		})
	}
}

func TestValidateJWTConfigIssuerLengthBoundary(t *testing.T) {
	valid := &JWTConfig{
		SigningKey: testKey,
		Issuers:    []string{strings.Repeat("i", MaxIssuerBytes)},
	}
	if err := ValidateJWTConfig(valid); err != nil {
		t.Fatalf("ValidateJWTConfig exact issuer boundary: %v", err)
	}

	invalid := &JWTConfig{
		SigningKey: testKey,
		Issuers:    []string{strings.Repeat("i", MaxIssuerBytes+1)},
	}
	err := ValidateJWTConfig(invalid)
	if !errors.Is(err, ErrJWTInvalid) || !errors.Is(err, ErrJWTClaimTooLarge) {
		t.Fatalf("ValidateJWTConfig oversized issuer error = %v, want ErrJWTInvalid and ErrJWTClaimTooLarge", err)
	}
}

func TestValidateJWTFullyPopulated(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, AuthMode: AuthModeStrict}
	now := time.Now()
	exp := now.Add(time.Hour)
	s := mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": "https://issuer.example",
		"iat": now.Unix(),
		"exp": exp.Unix(),
	})

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "alice" || claims.Issuer != "https://issuer.example" {
		t.Errorf("claims mismatch: %+v", claims)
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Equal(time.Unix(exp.Unix(), 0)) {
		t.Errorf("ExpiresAt = %v, want %v", claims.ExpiresAt, exp)
	}
}

func TestValidateJWTWithoutExpAccepted(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"iat": time.Now().Unix(),
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt != nil {
		t.Errorf("ExpiresAt should be nil without exp claim; got %v", *claims.ExpiresAt)
	}
}

func TestValidateJWTIssuerAllowlistAccepted(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, Issuers: []string{"https://issuer.example"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": "https://issuer.example",
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != "https://issuer.example" {
		t.Fatalf("Issuer = %q, want configured issuer", claims.Issuer)
	}
}

func TestValidateJWTIssuerAllowlistRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, Issuers: []string{"https://issuer.example"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": "https://other.example",
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTIssuerMismatch) {
		t.Fatalf("error = %v, want ErrJWTIssuerMismatch", err)
	}
}

func TestValidateJWTSubjectByteLimit(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, Issuers: []string{"issuer"}}
	exactSubject := strings.Repeat("a", MaxSubjectBytes)
	s := mintHS256(t, jwt.MapClaims{
		"sub": exactSubject,
		"iss": "issuer",
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatalf("ValidateJWT exact subject limit error = %v, want nil", err)
	}
	if len(claims.Subject) != MaxSubjectBytes {
		t.Fatalf("Subject byte length = %d, want %d", len(claims.Subject), MaxSubjectBytes)
	}

	oversizedSubject := strings.Repeat("a", MaxSubjectBytes+1)
	s = mintHS256(t, jwt.MapClaims{
		"sub": oversizedSubject,
		"iss": "issuer",
	})
	_, err = ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTClaimTooLarge) {
		t.Fatalf("ValidateJWT oversized subject error = %T %[1]v, want ErrJWTClaimTooLarge", err)
	}
}

func TestValidateJWTIssuerByteLimit(t *testing.T) {
	exactIssuer := strings.Repeat("i", MaxIssuerBytes)
	cfg := &JWTConfig{SigningKey: testKey, Issuers: []string{exactIssuer}}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": exactIssuer,
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatalf("ValidateJWT exact issuer limit error = %v, want nil", err)
	}
	if len(claims.Issuer) != MaxIssuerBytes {
		t.Fatalf("Issuer byte length = %d, want %d", len(claims.Issuer), MaxIssuerBytes)
	}

	oversizedIssuer := strings.Repeat("i", MaxIssuerBytes+1)
	s = mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": oversizedIssuer,
	})
	_, err = ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTClaimTooLarge) {
		t.Fatalf("ValidateJWT oversized issuer error = %T %[1]v, want ErrJWTClaimTooLarge", err)
	}
}

func TestValidateJWTExpiredRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Errorf("got %v, want ErrJWTInvalid for expired token", err)
	}
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Fatalf("expired token error should preserve jwt.ErrTokenExpired, got %v", err)
	}
}

func TestValidateJWTFutureIssuedAtRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"iat": time.Now().Add(time.Hour).Unix(),
		"exp": time.Now().Add(2 * time.Hour).Unix(),
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Errorf("got %v, want ErrJWTInvalid for future issued-at token", err)
	}
	if !errors.Is(err, jwt.ErrTokenUsedBeforeIssued) {
		t.Fatalf("future issued-at error should preserve jwt.ErrTokenUsedBeforeIssued, got %v", err)
	}
}

func TestValidateJWTNotBeforeRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"nbf": time.Now().Add(time.Hour).Unix(),
		"exp": time.Now().Add(2 * time.Hour).Unix(),
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Errorf("got %v, want ErrJWTInvalid for not-before token", err)
	}
	if !errors.Is(err, jwt.ErrTokenNotValidYet) {
		t.Fatalf("not-before error should preserve jwt.ErrTokenNotValidYet, got %v", err)
	}
}

func TestValidateJWTBadSignatureRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: []byte("fedcba9876543210fedcba9876543210")}
	s := mintHS256(t, jwt.MapClaims{"sub": "a", "iss": "b"})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Errorf("got %v, want ErrJWTInvalid for bad signature", err)
	}
	if !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		t.Fatalf("bad signature error should preserve jwt.ErrTokenSignatureInvalid, got %v", err)
	}
}

func TestValidateJWTMalformedRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	_, err := ValidateJWT("not-a-jwt", cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("got %v, want ErrJWTInvalid for malformed token", err)
	}
	if !errors.Is(err, jwt.ErrTokenMalformed) {
		t.Fatalf("malformed token error should preserve jwt.ErrTokenMalformed, got %v", err)
	}
}

func TestValidateJWTRejectsNonHS256HMAC(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{"sub": "a", "iss": "b"})
	s, err := tok.SignedString(testKey)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("got %v, want ErrJWTInvalid for HS384 token", err)
	}
}

func TestValidateJWTLegacySigningKeyAcceptsHS256TokenWithKeyID(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "a", "iss": "b"})
	tok.Header["kid"] = "ignored-for-legacy-unkeyed-hmac"
	s, err := tok.SignedString(testKey)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "a" || claims.Issuer != "b" {
		t.Fatalf("claims = %+v, want sub/iss", claims)
	}
}

func TestValidateJWTMultipleHS256VerificationKeysUsesKeyID(t *testing.T) {
	oldKey := []byte("0123456789abcdef-old-rotation-key")
	newKey := []byte("0123456789abcdef-new-rotation-key")
	cfg := &JWTConfig{
		VerificationKeys: []JWTVerificationKey{
			{Algorithm: JWTAlgorithmHS256, KeyID: "old", Key: oldKey},
			{Algorithm: JWTAlgorithmHS256, KeyID: "new", Key: newKey},
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "alice", "iss": "issuer"})
	tok.Header["kid"] = "new"
	s, err := tok.SignedString(newKey)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "alice" || claims.Issuer != "issuer" {
		t.Fatalf("claims = %+v, want alice/issuer", claims)
	}
}

func TestValidateJWTKeyIDWithoutMatchingVerifierRejected(t *testing.T) {
	cfg := &JWTConfig{
		VerificationKeys: []JWTVerificationKey{
			{Algorithm: JWTAlgorithmHS256, KeyID: "old", Key: []byte("0123456789abcdef-old-rotation-key")},
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "alice", "iss": "issuer"})
	tok.Header["kid"] = "new"
	s, err := tok.SignedString([]byte("0123456789abcdef-new-rotation-key"))
	if err != nil {
		t.Fatal(err)
	}

	claims, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTUnsupportedAlg) {
		t.Fatalf("ValidateJWT error = %v, want ErrJWTUnsupportedAlg for unmatched kid", err)
	}
	if claims != nil {
		t.Fatalf("claims = %+v, want nil", claims)
	}
}

func TestValidateJWTRS256VerificationKeyAccepted(t *testing.T) {
	privateKey, publicPEM := generateRS256TestKey(t)
	cfg := &JWTConfig{
		VerificationKeys: []JWTVerificationKey{
			{Algorithm: JWTAlgorithmRS256, KeyID: "rsa-1", Key: publicPEM},
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "alice",
		"iss": "https://issuer.example",
		"aud": "shunter-api",
	})
	tok.Header["kid"] = "rsa-1"
	s, err := tok.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "alice" || claims.Issuer != "https://issuer.example" {
		t.Fatalf("claims = %+v, want alice/issuer", claims)
	}
	if got := claims.Audience; len(got) != 1 || got[0] != "shunter-api" {
		t.Fatalf("Audience = %#v, want shunter-api", got)
	}
}

func TestValidateJWTConfigRejectsWeakRS256PublicKey(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, minRS256ModulusBits/2)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &JWTConfig{VerificationKeys: []JWTVerificationKey{{
		Algorithm: JWTAlgorithmRS256,
		Key:       marshalPublicKeyPEM(t, &privateKey.PublicKey),
	}}}

	err = ValidateJWTConfig(cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("ValidateJWTConfig error = %v, want ErrJWTInvalid", err)
	}
	if !strings.Contains(err.Error(), "between 2048 and 8192 bits") {
		t.Fatalf("ValidateJWTConfig error = %v, want RSA modulus context", err)
	}
}

func TestValidateJWTES256VerificationKeyAccepted(t *testing.T) {
	privateKey, publicPEM := generateES256TestKey(t)
	cfg := &JWTConfig{
		VerificationKeys: []JWTVerificationKey{
			{Algorithm: JWTAlgorithmES256, KeyID: "ecdsa-1", Key: publicPEM},
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"sub": "alice",
		"iss": "https://issuer.example",
	})
	tok.Header["kid"] = "ecdsa-1"
	s, err := tok.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "alice" || claims.Issuer != "https://issuer.example" {
		t.Fatalf("claims = %+v, want alice/issuer", claims)
	}
}

func TestValidateJWTMissingSub(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{"iss": "b"})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTMissingClaim) {
		t.Errorf("got %v, want ErrJWTMissingClaim for missing sub", err)
	}
}

func TestValidateJWTMissingIss(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{"sub": "a"})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTMissingClaim) {
		t.Errorf("got %v, want ErrJWTMissingClaim for missing iss", err)
	}
}

func TestValidateJWTHexIdentityMatches(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	derived := DeriveIdentity("issuer", "alice")
	s := mintHS256(t, jwt.MapClaims{
		"sub":          "alice",
		"iss":          "issuer",
		"hex_identity": derived.Hex(),
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.HexIdentity != derived.Hex() {
		t.Errorf("HexIdentity mismatch")
	}
}

func TestValidateJWTHexIdentityMismatchRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub":          "alice",
		"iss":          "issuer",
		"hex_identity": "0000000000000000000000000000000000000000000000000000000000000000",
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTHexIdentityMismatch) {
		t.Errorf("got %v, want ErrJWTHexIdentityMismatch", err)
	}
}

func TestValidateJWTHexIdentityAbsent(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{"sub": "alice", "iss": "issuer"})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.HexIdentity != "" {
		t.Errorf("HexIdentity should be empty when claim absent; got %q", claims.HexIdentity)
	}
}

func TestValidateJWTAudienceAccepted(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, Audiences: []string{"shunter-prod"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"aud": "shunter-prod",
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "shunter-prod" {
		t.Errorf("Audience mismatch: %+v", claims.Audience)
	}
}

func TestValidateJWTAudienceRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, Audiences: []string{"shunter-prod"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"aud": "shunter-staging",
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTAudienceMismatch) {
		t.Errorf("got %v, want ErrJWTAudienceMismatch", err)
	}
}

func TestValidateJWTAudienceDisabledAcceptsAny(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey} // no Audiences configured
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"aud": "literally-anything",
	})
	_, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatalf("audience validation should be skipped when Audiences is empty; got %v", err)
	}
}

func TestValidateJWTPermissionClaims(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub":         "alice",
		"iss":         "issuer",
		"permissions": []string{"messages:send", "messages:read"},
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := claims.Permissions; len(got) != 2 || got[0] != "messages:send" || got[1] != "messages:read" {
		t.Fatalf("Permissions = %#v, want send/read tags", got)
	}

	s = mintHS256(t, jwt.MapClaims{
		"sub":         "alice",
		"iss":         "issuer",
		"permissions": "messages:admin",
	})
	claims, err = ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := claims.Permissions; len(got) != 1 || got[0] != "messages:admin" {
		t.Fatalf("single-string Permissions = %#v, want admin tag", got)
	}
}

func TestValidateJWTExtraClaimsPreserved(t *testing.T) {
	cfg := &JWTConfig{
		SigningKey:  testKey,
		ExtraClaims: []string{" email ", "role", "https://claims.example/session"},
	}
	s := mintHS256(t, jwt.MapClaims{
		"sub":                            "alice",
		"iss":                            "issuer",
		"email":                          "alice@example.com",
		"role":                           "authenticated",
		"https://claims.example/session": map[string]any{"id": "session-1", "aal": 2},
		"missing":                        "not configured",
	})

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"email":                          `"alice@example.com"`,
		"role":                           `"authenticated"`,
		"https://claims.example/session": `{"aal":2,"id":"session-1"}`,
	}
	for name, want := range tests {
		got, ok := claims.Claims.Get(name)
		if !ok {
			t.Fatalf("extra claim %q missing from %#v", name, claims.Claims.Values)
		}
		if string(got) != want {
			t.Fatalf("extra claim %q = %s, want %s", name, got, want)
		}
	}
	if _, ok := claims.Claims.Get("missing"); ok {
		t.Fatal("unconfigured claim was preserved")
	}
}

func TestValidateJWTExtraClaimNumbersPreservePrecision(t *testing.T) {
	cfg := &JWTConfig{
		SigningKey:  testKey,
		ExtraClaims: []string{"session_id", "max_i64", "max_u64", "nested", "ratio"},
	}
	s := mintHS256(t, jwt.MapClaims{
		"sub":        "alice",
		"iss":        "issuer",
		"session_id": json.Number("9007199254740993"),
		"max_i64":    json.Number("9223372036854775807"),
		"max_u64":    json.Number("18446744073709551615"),
		"nested": map[string]any{
			"values": []any{json.Number("9007199254740993"), json.Number("18446744073709551615")},
		},
		"ratio": json.Number("1.25"),
	})

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]string{
		"session_id": "9007199254740993",
		"max_i64":    "9223372036854775807",
		"max_u64":    "18446744073709551615",
		"nested":     `{"values":[9007199254740993,18446744073709551615]}`,
		"ratio":      "1.25",
	}
	for name, want := range wants {
		got, ok := claims.Claims.Get(name)
		if !ok {
			t.Fatalf("extra claim %q missing", name)
		}
		if string(got) != want {
			t.Fatalf("extra claim %q = %s, want %s", name, got, want)
		}
	}
}

func TestValidateJWTNumericDatesWithJSONNumber(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": "issuer",
		"iat": json.Number(fmt.Sprint(now.Add(-time.Minute).Unix())),
		"nbf": json.Number(fmt.Sprint(now.Add(-time.Minute).Unix())),
		"exp": json.Number(fmt.Sprint(now.Add(time.Hour).Unix())),
	})

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Unix() != now.Add(time.Hour).Unix() {
		t.Fatalf("ExpiresAt = %v, want %v", claims.ExpiresAt, now.Add(time.Hour))
	}
	if claims.IssuedAt.Unix() != now.Add(-time.Minute).Unix() {
		t.Fatalf("IssuedAt = %v, want %v", claims.IssuedAt, now.Add(-time.Minute))
	}
}

func TestValidateJWTRejectsFractionalNormalizedNumericDate(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": "issuer",
		"iat": json.Number("1.5"),
		"exp": json.Number(fmt.Sprint(time.Now().Add(time.Hour).Unix())),
	})

	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("ValidateJWT error = %v, want ErrJWTInvalid", err)
	}
}

func TestValidateJWTExtraClaimsSkipMissing(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, ExtraClaims: []string{"email", "role"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub":   "alice",
		"iss":   "issuer",
		"email": "alice@example.com",
	})

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.Claims.Values) != 1 {
		t.Fatalf("extra claims = %#v, want only configured present claim", claims.Claims.Values)
	}
	if got, ok := claims.Claims.Get("email"); !ok || string(got) != `"alice@example.com"` {
		t.Fatalf("email claim = %s, %v; want preserved email", got, ok)
	}
	if _, ok := claims.Claims.Get("role"); ok {
		t.Fatal("missing configured role claim was preserved")
	}
}

func TestValidateJWTExtraClaimsRejectsPerClaimTooLarge(t *testing.T) {
	cfg := &JWTConfig{
		SigningKey:          testKey,
		ExtraClaims:         []string{"email"},
		MaxExtraClaimBytes:  8,
		MaxExtraClaimsBytes: 128,
	}
	s := mintHS256(t, jwt.MapClaims{
		"sub":   "alice",
		"iss":   "issuer",
		"email": "alice@example.com",
	})

	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTClaimTooLarge) {
		t.Fatalf("ValidateJWT error = %v, want ErrJWTClaimTooLarge", err)
	}
}

func TestValidateJWTExtraClaimsRejectsTotalTooLarge(t *testing.T) {
	cfg := &JWTConfig{
		SigningKey:          testKey,
		ExtraClaims:         []string{"email", "role"},
		MaxExtraClaimBytes:  64,
		MaxExtraClaimsBytes: 20,
	}
	s := mintHS256(t, jwt.MapClaims{
		"sub":   "alice",
		"iss":   "issuer",
		"email": "alice@example.com",
		"role":  "authenticated",
	})

	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTClaimTooLarge) {
		t.Fatalf("ValidateJWT error = %v, want ErrJWTClaimTooLarge", err)
	}
}

func TestValidateJWTExtraClaimsRejectsTooManyConfiguredNames(t *testing.T) {
	names := make([]string, maxExtraClaims+1)
	for i := range names {
		names[i] = fmt.Sprintf("claim_%d", i)
	}
	cfg := JWTConfig{SigningKey: testKey, ExtraClaims: names}

	if err := ValidateJWTExtraClaimsConfig(&cfg); !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("ValidateJWTExtraClaimsConfig error = %v, want ErrJWTInvalid", err)
	}
}

func TestValidateJWTExtraClaimsRejectsExcessiveJSONDepth(t *testing.T) {
	deep := any("leaf")
	for range maxExtraClaimDepth + 1 {
		deep = []any{deep}
	}
	cfg := &JWTConfig{
		SigningKey:          testKey,
		ExtraClaims:         []string{"metadata"},
		MaxExtraClaimBytes:  4096,
		MaxExtraClaimsBytes: 4096,
	}
	s := mintHS256(t, jwt.MapClaims{
		"sub":      "alice",
		"iss":      "issuer",
		"metadata": deep,
	})

	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("ValidateJWT error = %v, want ErrJWTInvalid", err)
	}
}

func TestPreserveExtraClaimsRejectsNonJSONValueType(t *testing.T) {
	_, err := preserveExtraClaims(
		jwt.MapClaims{"metadata": map[any]any{"key": "value"}},
		extraClaimConfig{
			names:         []string{"metadata"},
			maxClaimBytes: DefaultMaxExtraClaimBytes,
			maxTotalBytes: DefaultMaxExtraClaimsBytes,
		},
	)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("preserveExtraClaims error = %v, want ErrJWTInvalid", err)
	}
}

func TestValidateJWTRejectsInvalidExtraClaimConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  JWTConfig
	}{
		{name: "empty", cfg: JWTConfig{ExtraClaims: []string{"email", " "}}},
		{name: "duplicate after trim", cfg: JWTConfig{ExtraClaims: []string{"email", " email "}}},
		{name: "too long", cfg: JWTConfig{ExtraClaims: []string{strings.Repeat("a", MaxExtraClaimNameBytes+1)}}},
		{name: "control", cfg: JWTConfig{ExtraClaims: []string{"em\nail"}}},
		{name: "owned", cfg: JWTConfig{ExtraClaims: []string{"permissions"}}},
		{name: "negative per claim", cfg: JWTConfig{MaxExtraClaimBytes: -1}},
		{name: "negative total", cfg: JWTConfig{MaxExtraClaimsBytes: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.SigningKey = testKey
			if err := ValidateJWTExtraClaimsConfig(&tt.cfg); !errors.Is(err, ErrJWTInvalid) {
				t.Fatalf("ValidateJWTExtraClaimsConfig error = %v, want ErrJWTInvalid", err)
			}
		})
	}
}

func TestValidateJWTExtraClaimsDoNotGrantPermissions(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, ExtraClaims: []string{"role"}, Audiences: []string{"authenticated"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub":         "alice",
		"iss":         "issuer",
		"aud":         "authenticated",
		"role":        "service_role",
		"permissions": []string{"messages:send"},
	})

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := claims.Permissions; len(got) != 1 || got[0] != "messages:send" {
		t.Fatalf("Permissions = %#v, want only permissions claim", got)
	}
	if got := claims.Audience; len(got) != 1 || got[0] != "authenticated" {
		t.Fatalf("Audience = %#v, want configured audience", got)
	}
	if got, ok := claims.Claims.Get("role"); !ok || string(got) != `"service_role"` {
		t.Fatalf("role claim = %s, %v; want preserved role", got, ok)
	}
}

func TestValidateJWTLooseStringListClaims(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	tests := []struct {
		name            string
		audience        any
		permissions     any
		wantAudience    []string
		wantPermissions []string
	}{
		{
			name:            "strings",
			audience:        "",
			permissions:     "",
			wantAudience:    []string{""},
			wantPermissions: nil,
		},
		{
			name:            "lists",
			audience:        []any{"", "shunter-prod", 42},
			permissions:     []any{"", "messages:send", 17},
			wantAudience:    []string{"", "shunter-prod"},
			wantPermissions: []string{"messages:send"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mintHS256(t, jwt.MapClaims{
				"sub":         "alice",
				"iss":         "issuer",
				"aud":         tt.audience,
				"permissions": tt.permissions,
			})
			claims, err := ValidateJWT(s, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(claims.Audience, tt.wantAudience) {
				t.Fatalf("Audience = %#v, want %#v", claims.Audience, tt.wantAudience)
			}
			if !reflect.DeepEqual(claims.Permissions, tt.wantPermissions) {
				t.Fatalf("Permissions = %#v, want %#v", claims.Permissions, tt.wantPermissions)
			}
		})
	}
}

func TestValidateJWTConcurrentValidationShortSoak(t *testing.T) {
	const seed = uint64(0xa17c0de)
	const workers = 8
	const iterations = 64

	issuer := "issuer"
	subject := "alice"
	audience := "shunter-prod"
	identity := DeriveIdentity(issuer, subject)
	now := time.Now()
	cfg := &JWTConfig{SigningKey: testKey, Audiences: []string{audience}, AuthMode: AuthModeStrict}
	s := mintHS256(t, jwt.MapClaims{
		"sub":          subject,
		"iss":          issuer,
		"aud":          audience,
		"iat":          now.Unix(),
		"exp":          now.Add(time.Hour).Unix(),
		"hex_identity": identity.Hex(),
		"permissions":  []string{"messages:send", "messages:read"},
	})

	start := make(chan struct{})
	failures := make(chan string, workers*iterations)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for iteration := range iterations {
				opIndex := worker*iterations + iteration
				if ((uint64(worker)<<8)^uint64(iteration)^seed)&3 == 0 {
					runtime.Gosched()
				}
				claims, err := ValidateJWT(s, cfg)
				if err != nil {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ValidateJWT observed_error=%v expected=nil",
						seed, opIndex, worker, workers, iterations, err)
					return
				}
				if claims.Subject != subject || claims.Issuer != issuer {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ValidateJWT observed_claims=%+v expected_subject=%q expected_issuer=%q",
						seed, opIndex, worker, workers, iterations, claims, subject, issuer)
					return
				}
				if got := claims.DeriveIdentity(); got != identity {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=DeriveIdentity observed=%s expected=%s",
						seed, opIndex, worker, workers, iterations, got.Hex(), identity.Hex())
					return
				}
				if len(claims.Audience) != 1 || claims.Audience[0] != audience {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ValidateJWT audience observed=%v expected=[%s]",
						seed, opIndex, worker, workers, iterations, claims.Audience, audience)
					return
				}
				if len(claims.Permissions) != 2 || claims.Permissions[0] != "messages:send" || claims.Permissions[1] != "messages:read" {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ValidateJWT permissions observed=%v expected=[messages:send messages:read]",
						seed, opIndex, worker, workers, iterations, claims.Permissions)
					return
				}
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}

func TestClaimsDeriveIdentity(t *testing.T) {
	c := &Claims{Issuer: "issuer", Subject: "alice"}
	got := c.DeriveIdentity()
	want := DeriveIdentity("issuer", "alice")
	if got != want {
		t.Errorf("Claims.DeriveIdentity returned %x, want %x", got, want)
	}
}

func TestClaimsPrincipalCopiesExternalClaims(t *testing.T) {
	c := &Claims{
		Issuer:      "issuer",
		Subject:     "alice",
		Audience:    []string{"shunter-api"},
		Permissions: []string{"messages:send"},
		Claims: types.AuthClaims{Values: map[string]json.RawMessage{
			"email": []byte(`"alice@example.com"`),
		}},
	}

	principal := c.Principal()
	if principal.Issuer != "issuer" || principal.Subject != "alice" {
		t.Fatalf("Principal identity = %+v, want issuer/alice", principal)
	}
	if len(principal.Audience) != 1 || principal.Audience[0] != "shunter-api" {
		t.Fatalf("Principal audience = %#v, want shunter-api", principal.Audience)
	}
	if len(principal.Permissions) != 1 || principal.Permissions[0] != "messages:send" {
		t.Fatalf("Principal permissions = %#v, want messages:send", principal.Permissions)
	}
	if got, ok := principal.Claims.Get("email"); !ok || string(got) != `"alice@example.com"` {
		t.Fatalf("Principal claims email = %s, %v; want copied email", got, ok)
	}

	principal.Audience[0] = "mutated"
	principal.Permissions[0] = "mutated"
	principal.Claims.Values["email"][1] = 'A'
	if c.Audience[0] != "shunter-api" || c.Permissions[0] != "messages:send" ||
		string(c.Claims.Values["email"]) != `"alice@example.com"` {
		t.Fatalf("Principal aliases Claims slices: claims=%+v principal=%+v", c, principal)
	}

	var nilClaims *Claims
	if got := nilClaims.Principal(); got.Issuer != "" || got.Subject != "" || got.Audience != nil || got.Permissions != nil || got.Claims.Values != nil {
		t.Fatalf("nil Claims Principal = %+v, want zero", got)
	}
}

func generateRS256TestKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, marshalPublicKeyPEM(t, &privateKey.PublicKey)
}

func generateES256TestKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, marshalPublicKeyPEM(t, &privateKey.PublicKey)
}

func marshalPublicKeyPEM(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}
