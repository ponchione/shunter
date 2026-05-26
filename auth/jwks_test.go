package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestValidateJWTJWKSRS256FetchesAndCaches(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "rsa-1")
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJWKS(t, w, jwk)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:   "https://issuer.example",
			JWKSURL:  srv.URL,
			CacheTTL: time.Hour,
		}},
		Issuers:  []string{"https://issuer.example"},
		AuthMode: AuthModeStrict,
	}
	token := mintRS256Token(t, privateKey, "rsa-1", "https://issuer.example")
	for range 2 {
		claims, err := ValidateJWT(token, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if claims.Subject != "alice" || claims.Issuer != "https://issuer.example" {
			t.Fatalf("claims = %+v, want alice/issuer", claims)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("jwks requests = %d, want 1 cached fetch", got)
	}
}

func TestValidateJWTJWKSRefreshesForUnknownKeyID(t *testing.T) {
	oldPrivateKey, oldJWK := generateRS256JWK(t, "old")
	newPrivateKey, newJWK := generateRS256JWK(t, "new")
	keys := atomic.Value{}
	keys.Store([]jwkDocumentKey{oldJWK})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJWKS(t, w, keys.Load().([]jwkDocumentKey)...)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:   "issuer",
			JWKSURL:  srv.URL,
			CacheTTL: time.Hour,
		}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	if _, err := ValidateJWT(mintRS256Token(t, oldPrivateKey, "old", "issuer"), cfg); err != nil {
		t.Fatalf("old token did not validate: %v", err)
	}

	keys.Store([]jwkDocumentKey{newJWK})
	claims, err := ValidateJWT(mintRS256Token(t, newPrivateKey, "new", "issuer"), cfg)
	if err != nil {
		t.Fatalf("new token after jwks rotation did not validate: %v", err)
	}
	if claims.Subject != "alice" {
		t.Fatalf("claims = %+v, want alice", claims)
	}
}

func TestValidateJWTJWKSRefreshesForUnknownKeyIDWithLocalCandidate(t *testing.T) {
	_, localPEM := generateRS256TestKey(t)
	oldPrivateKey, oldJWK := generateRS256JWK(t, "old")
	newPrivateKey, newJWK := generateRS256JWK(t, "new")
	keys := atomic.Value{}
	keys.Store([]jwkDocumentKey{oldJWK})
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJWKS(t, w, keys.Load().([]jwkDocumentKey)...)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		VerificationKeys: []JWTVerificationKey{{
			Algorithm: JWTAlgorithmRS256,
			Key:       localPEM,
		}},
		JWKS: []JWKSConfig{{
			Issuer:   "issuer",
			JWKSURL:  srv.URL,
			CacheTTL: time.Hour,
		}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	if _, err := ValidateJWT(mintRS256Token(t, oldPrivateKey, "old", "issuer"), cfg); err != nil {
		t.Fatalf("old token did not validate: %v", err)
	}

	keys.Store([]jwkDocumentKey{newJWK})
	claims, err := ValidateJWT(mintRS256Token(t, newPrivateKey, "new", "issuer"), cfg)
	if err != nil {
		t.Fatalf("new token after jwks rotation did not validate with local candidate present: %v", err)
	}
	if claims.Subject != "alice" {
		t.Fatalf("claims = %+v, want alice", claims)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("jwks requests = %d, want refresh after kid miss", got)
	}
}

func TestValidateJWTJWKSDoesNotFallbackToUnkeyedKeyWhenTokenHasKeyID(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJWKS(t, w, jwk)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:   "issuer",
			JWKSURL:  srv.URL,
			CacheTTL: time.Hour,
		}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}

	_, err := ValidateJWT(mintRS256Token(t, privateKey, "unexpected", "issuer"), cfg)
	if !errors.Is(err, ErrJWTUnsupportedAlg) {
		t.Fatalf("ValidateJWT error = %v, want ErrJWTUnsupportedAlg for keyed token against unkeyed JWKS key", err)
	}
}

func TestValidateJWTJWKSIssuerMustMatchSource(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "rsa-1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJWKS(t, w, jwk)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:  "trusted-issuer",
			JWKSURL: srv.URL,
		}},
		AuthMode: AuthModeStrict,
	}
	_, err := ValidateJWT(mintRS256Token(t, privateKey, "rsa-1", "attacker-issuer"), cfg)
	if !errors.Is(err, ErrJWTUnsupportedAlg) {
		t.Fatalf("ValidateJWT error = %v, want ErrJWTUnsupportedAlg for issuer-bound jwks miss", err)
	}
}

func TestValidateJWTJWKSTrimsSourceIssuerForMatch(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "rsa-1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJWKS(t, w, jwk)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:  " issuer ",
			JWKSURL: srv.URL,
		}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	claims, err := ValidateJWT(mintRS256Token(t, privateKey, "rsa-1", "issuer"), cfg)
	if err != nil {
		t.Fatalf("ValidateJWT with trimmed JWKS issuer failed: %v", err)
	}
	if claims.Issuer != "issuer" {
		t.Fatalf("claims issuer = %q, want issuer", claims.Issuer)
	}
}

func TestValidateJWTJWKSCacheIsScopedBySourceAlgorithmPolicy(t *testing.T) {
	rsaPrivateKey, rsaJWK := generateRS256JWK(t, "rsa-1")
	esPrivateKey, esJWK := generateES256JWK(t, "ec-1")
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJWKS(t, w, rsaJWK, esJWK)
	}))
	t.Cleanup(srv.Close)

	rsCfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:     "issuer",
			JWKSURL:    srv.URL,
			Algorithms: []JWTAlgorithm{JWTAlgorithmRS256},
			CacheTTL:   time.Hour,
		}},
		Issuers: []string{"issuer"},
	}
	if _, err := ValidateJWT(mintRS256Token(t, rsaPrivateKey, "rsa-1", "issuer"), rsCfg); err != nil {
		t.Fatalf("RS256 token did not validate: %v", err)
	}

	esTok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"sub": "alice",
		"iss": "issuer",
		"iat": time.Now().Unix(),
	})
	esToken, err := esTok.SignedString(esPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	esCfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:     "issuer",
			JWKSURL:    srv.URL,
			Algorithms: []JWTAlgorithm{JWTAlgorithmES256},
			CacheTTL:   time.Hour,
		}},
		Issuers: []string{"issuer"},
	}
	if _, err := ValidateJWT(esToken, esCfg); err != nil {
		t.Fatalf("ES256 token did not validate with same JWKS URL and different algorithm policy: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("jwks requests = %d, want separate fetches for distinct source policies", got)
	}
}

func TestValidateJWTJWKSES256Accepted(t *testing.T) {
	privateKey, jwk := generateES256JWK(t, "ec-1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJWKS(t, w, jwk)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:     "issuer",
			JWKSURL:    srv.URL,
			Algorithms: []JWTAlgorithm{JWTAlgorithmES256},
		}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"sub": "alice",
		"iss": "issuer",
		"iat": time.Now().Unix(),
	})
	tok.Header["kid"] = "ec-1"
	token, err := tok.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := ValidateJWT(token, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "alice" || claims.Issuer != "issuer" {
		t.Fatalf("claims = %+v, want alice/issuer", claims)
	}
}

func TestFetchJWKSRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"keys":[]}`))
		_, _ = w.Write([]byte(strings.Repeat(" ", maxJWKSResponseBytes)))
	}))
	t.Cleanup(srv.Close)

	_, err := fetchJWKS(JWKSConfig{Issuer: "issuer", JWKSURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("fetchJWKS error = %v, want response size error", err)
	}
}

func TestFetchJWKSRejectsTrailingJSON(t *testing.T) {
	_, jwk := generateRS256JWK(t, "rsa-1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJWKS(t, w, jwk)
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	t.Cleanup(srv.Close)

	_, err := fetchJWKS(JWKSConfig{Issuer: "issuer", JWKSURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "trailing JSON value") {
		t.Fatalf("fetchJWKS error = %v, want trailing JSON error", err)
	}
}

func TestValidateJWTJWKSConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  *JWTConfig
	}{
		{
			name: "missing issuer",
			cfg:  &JWTConfig{JWKS: []JWKSConfig{{JWKSURL: "https://issuer.example/jwks.json"}}},
		},
		{
			name: "missing host",
			cfg:  &JWTConfig{JWKS: []JWKSConfig{{Issuer: "issuer", JWKSURL: "https:///jwks.json"}}},
		},
		{
			name: "external http url",
			cfg:  &JWTConfig{JWKS: []JWKSConfig{{Issuer: "issuer", JWKSURL: "http://issuer.example/jwks.json"}}},
		},
		{
			name: "unsupported algorithm",
			cfg:  &JWTConfig{JWKS: []JWKSConfig{{Issuer: "issuer", JWKSURL: "https://issuer.example/jwks.json", Algorithms: []JWTAlgorithm{JWTAlgorithmHS256}}}},
		},
		{
			name: "negative cache ttl",
			cfg:  &JWTConfig{JWKS: []JWKSConfig{{Issuer: "issuer", JWKSURL: "https://issuer.example/jwks.json", CacheTTL: -time.Second}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateJWTConfig(tt.cfg); !errors.Is(err, ErrJWTInvalid) {
				t.Fatalf("ValidateJWTConfig error = %v, want ErrJWTInvalid", err)
			}
		})
	}
}

func TestResolveJWKRejectsInvalidRSAKeyBounds(t *testing.T) {
	validPrivateKey, validJWK := generateRS256JWK(t, "rsa-1")
	validJWK.N = base64.RawURLEncoding.EncodeToString(validPrivateKey.PublicKey.N.Bytes())

	tests := []struct {
		name string
		jwk  jwkDocumentKey
	}{
		{
			name: "small modulus",
			jwk: func() jwkDocumentKey {
				jwk := validJWK
				jwk.N = base64.RawURLEncoding.EncodeToString(big.NewInt(17).Bytes())
				return jwk
			}(),
		},
		{
			name: "oversized modulus",
			jwk: func() jwkDocumentKey {
				jwk := validJWK
				oversized := new(big.Int).Lsh(big.NewInt(1), 8192)
				jwk.N = base64.RawURLEncoding.EncodeToString(oversized.Bytes())
				return jwk
			}(),
		},
		{
			name: "even exponent",
			jwk: func() jwkDocumentKey {
				jwk := validJWK
				jwk.E = base64.RawURLEncoding.EncodeToString(big.NewInt(2).Bytes())
				return jwk
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolveJWK(tt.jwk); err == nil {
				t.Fatal("resolveJWK succeeded; want invalid RSA jwk error")
			}
		})
	}
}

func mintRS256Token(t *testing.T, privateKey *rsa.PrivateKey, keyID, issuer string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "alice",
		"iss": issuer,
		"iat": time.Now().Unix(),
	})
	tok.Header["kid"] = keyID
	token, err := tok.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func generateRS256JWK(t *testing.T, keyID string) (*rsa.PrivateKey, jwkDocumentKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, jwkDocumentKey{
		KeyType:   "RSA",
		KeyID:     keyID,
		Algorithm: "RS256",
		Use:       "sig",
		N:         base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
		E:         base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
	}
}

func generateES256JWK(t *testing.T, keyID string) (*ecdsa.PrivateKey, jwkDocumentKey) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, jwkDocumentKey{
		KeyType:   "EC",
		KeyID:     keyID,
		Algorithm: "ES256",
		Use:       "sig",
		Crv:       "P-256",
		X:         base64.RawURLEncoding.EncodeToString(padP256Coordinate(privateKey.PublicKey.X)),
		Y:         base64.RawURLEncoding.EncodeToString(padP256Coordinate(privateKey.PublicKey.Y)),
	}
}

func padP256Coordinate(v *big.Int) []byte {
	out := make([]byte, 32)
	b := v.Bytes()
	copy(out[len(out)-len(b):], b)
	return out
}

func writeJWKS(t *testing.T, w http.ResponseWriter, keys ...jwkDocumentKey) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(jwksDocument{Keys: keys}); err != nil {
		t.Fatal(err)
	}
}
