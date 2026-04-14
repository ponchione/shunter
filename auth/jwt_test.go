package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testKey = []byte("test-secret-key")

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
}

func TestValidateJWTBadSignatureRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: []byte("WRONG-KEY")}
	s := mintHS256(t, jwt.MapClaims{"sub": "a", "iss": "b"})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Errorf("got %v, want ErrJWTInvalid for bad signature", err)
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

func TestClaimsDeriveIdentity(t *testing.T) {
	c := &Claims{Issuer: "issuer", Subject: "alice"}
	got := c.DeriveIdentity()
	want := DeriveIdentity("issuer", "alice")
	if got != want {
		t.Errorf("Claims.DeriveIdentity returned %x, want %x", got, want)
	}
}
