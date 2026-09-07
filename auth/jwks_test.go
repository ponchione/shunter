package auth

import (
	"context"
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
	"sync"
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

func TestValidateJWTJWKSExtraClaimNumberPreservesPrecision(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "rsa-precision")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJWKS(t, w, jwk)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:   "https://issuer.example",
			JWKSURL:  srv.URL,
			CacheTTL: time.Hour,
		}},
		Issuers:     []string{"https://issuer.example"},
		ExtraClaims: []string{"session_id"},
		AuthMode:    AuthModeStrict,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":        "alice",
		"iss":        "https://issuer.example",
		"session_id": json.Number("9007199254740993"),
	})
	token.Header["kid"] = "rsa-precision"
	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := ValidateJWT(signed, cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := claims.Claims.Get("session_id")
	if !ok || string(got) != "9007199254740993" {
		t.Fatalf("session_id = %s, %v; want exact integer", got, ok)
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

func TestValidateJWTJWKSBoundsSequentialUnknownKeyIDRefreshes(t *testing.T) {
	trustedPrivateKey, trustedJWK := generateRS256JWK(t, "trusted")
	missingPrivateKey, _ := generateRS256JWK(t, "missing")
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJWKS(t, w, trustedJWK)
	}))
	t.Cleanup(srv.Close)

	source := JWKSConfig{Issuer: "issuer", JWKSURL: srv.URL, CacheTTL: time.Hour}
	cfg := &JWTConfig{JWKS: []JWKSConfig{source}, Issuers: []string{"issuer"}, AuthMode: AuthModeStrict}
	if _, err := ValidateJWT(mintRS256Token(t, trustedPrivateKey, "trusted", "issuer"), cfg); err != nil {
		t.Fatalf("warm trusted token: %v", err)
	}
	missingToken := mintRS256Token(t, missingPrivateKey, "missing", "issuer")
	for range 2 {
		if _, err := ValidateJWT(missingToken, cfg); err == nil {
			t.Fatal("missing key token unexpectedly validated")
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("JWKS requests = %d, want one warm fetch and one bounded miss refresh", got)
	}
}

func TestValidateJWTJWKSConcurrentUnknownKeyIDWaitersShareRefresh(t *testing.T) {
	trustedPrivateKey, trustedJWK := generateRS256JWK(t, "trusted")
	missingPrivateKey, _ := generateRS256JWK(t, "missing")
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJWKS(t, w, trustedJWK)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS:     []JWKSConfig{{Issuer: "issuer", JWKSURL: srv.URL, CacheTTL: time.Hour}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	if _, err := ValidateJWT(mintRS256Token(t, trustedPrivateKey, "trusted", "issuer"), cfg); err != nil {
		t.Fatalf("warm trusted token: %v", err)
	}
	missingToken := mintRS256Token(t, missingPrivateKey, "missing", "issuer")

	const waiters = 16
	start := make(chan struct{})
	errs := make(chan error, waiters)
	var wg sync.WaitGroup
	for range waiters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := ValidateJWT(missingToken, cfg)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil {
			t.Fatal("missing key token unexpectedly validated")
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("JWKS requests = %d, want concurrent waiters to share one miss refresh", got)
	}
}

func TestValidateJWTJWKSAcceptsRotationAfterRefreshCooldown(t *testing.T) {
	oldPrivateKey, oldJWK := generateRS256JWK(t, "old")
	missingPrivateKey, _ := generateRS256JWK(t, "missing")
	newPrivateKey, newJWK := generateRS256JWK(t, "new")
	keys := atomic.Value{}
	keys.Store([]jwkDocumentKey{oldJWK})
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJWKS(t, w, keys.Load().([]jwkDocumentKey)...)
	}))
	t.Cleanup(srv.Close)

	source := JWKSConfig{Issuer: "issuer", JWKSURL: srv.URL, CacheTTL: time.Hour}
	cfg := &JWTConfig{JWKS: []JWKSConfig{source}, Issuers: []string{"issuer"}, AuthMode: AuthModeStrict}
	if _, err := ValidateJWT(mintRS256Token(t, oldPrivateKey, "old", "issuer"), cfg); err != nil {
		t.Fatalf("warm old token: %v", err)
	}
	if _, err := ValidateJWT(mintRS256Token(t, missingPrivateKey, "missing", "issuer"), cfg); err == nil {
		t.Fatal("missing key token unexpectedly validated")
	}
	keys.Store([]jwkDocumentKey{newJWK})

	defaultRemoteJWTCaches.mu.Lock()
	cache := defaultRemoteJWTCaches.jwks[jwksCacheKey(source)]
	defaultRemoteJWTCaches.mu.Unlock()
	if cache == nil {
		t.Fatal("JWKS cache missing after validation")
	}
	cache.mu.Lock()
	cache.lastForcedRefreshAt = time.Now().Add(-defaultJWKSRefreshCooldown - time.Second)
	cache.mu.Unlock()

	if _, err := ValidateJWT(mintRS256Token(t, newPrivateKey, "new", "issuer"), cfg); err != nil {
		t.Fatalf("new key after refresh cooldown did not validate: %v", err)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("JWKS requests = %d, want warm fetch, bounded miss, and post-cooldown rotation refresh", got)
	}
}

func TestValidateJWTLocalKeySuccessSkipsJWKSForSameIssuer(t *testing.T) {
	localPrivateKey, localPEM := generateRS256TestKey(t)
	_, remoteJWK := generateRS256JWK(t, "remote")
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJWKS(t, w, remoteJWK)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		VerificationKeys: []JWTVerificationKey{{Algorithm: JWTAlgorithmRS256, KeyID: "local", Key: localPEM}},
		JWKS:             []JWKSConfig{{Issuer: "issuer", JWKSURL: srv.URL, CacheTTL: time.Hour}},
		Issuers:          []string{"issuer"},
		AuthMode:         AuthModeStrict,
	}
	if _, err := ValidateJWT(mintRS256Token(t, localPrivateKey, "local", "issuer"), cfg); err != nil {
		t.Fatalf("local token did not validate: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("JWKS requests = %d, want none after local verification success", got)
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

func TestResolveJWKRejectsInvalidECPoints(t *testing.T) {
	_, valid := generateES256JWK(t, "ec-1")
	oversized := base64.RawURLEncoding.EncodeToString(make([]byte, 33))
	for _, tc := range []struct{ name, curve, x, y string }{
		{"wrong curve", "P-384", valid.X, valid.Y},
		{"invalid x encoding", "P-256", "!", valid.Y},
		{"invalid y encoding", "P-256", valid.X, "!"},
		{"oversized x", "P-256", oversized, valid.Y},
		{"oversized y", "P-256", valid.X, oversized},
		{"off curve", "P-256", "AA", "AA"},
		{"empty coordinates", "P-256", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := valid
			key.Crv, key.X, key.Y = tc.curve, tc.x, tc.y
			if _, err := resolveJWK(key); err == nil {
				t.Fatal("resolveJWK accepted an invalid EC point")
			}
		})
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

func TestValidateJWTJWKSFailureCooldownBoundsSequentialFetches(t *testing.T) {
	privateKey, _ := generateRS256JWK(t, "missing")
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS:     []JWKSConfig{{Issuer: "issuer", JWKSURL: srv.URL}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	token := mintRS256Token(t, privateKey, "missing", "issuer")
	for range 2 {
		if _, err := ValidateJWT(token, cfg); err == nil {
			t.Fatal("ValidateJWT succeeded against unavailable JWKS")
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("JWKS requests = %d, want one request during failure cooldown", got)
	}
}

func TestValidateJWTJWKSFailureWaitersShareOneFetch(t *testing.T) {
	privateKey, _ := generateRS256JWK(t, "missing")
	var requests atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		enteredOnce.Do(func() { close(entered) })
		<-release
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS:     []JWKSConfig{{Issuer: "issuer", JWKSURL: srv.URL}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	token := mintRS256Token(t, privateKey, "missing", "issuer")
	const waiters = 16
	start := make(chan struct{})
	errs := make(chan error, waiters)
	for range waiters {
		go func() {
			<-start
			_, err := ValidateJWT(token, cfg)
			errs <- err
		}()
	}
	close(start)
	<-entered
	close(release)
	for range waiters {
		if err := <-errs; err == nil {
			t.Fatal("ValidateJWT succeeded against unavailable JWKS")
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("JWKS requests = %d, want one shared failed fetch", got)
	}
}

func TestValidateJWTJWKSRecoversAfterFailureCooldown(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "rsa-1")
	var requests atomic.Int32
	var healthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		if !healthy.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJWKS(t, w, jwk)
	}))
	t.Cleanup(srv.Close)

	source := JWKSConfig{Issuer: "issuer", JWKSURL: srv.URL}
	cfg := &JWTConfig{JWKS: []JWKSConfig{source}, Issuers: []string{"issuer"}, AuthMode: AuthModeStrict}
	token := mintRS256Token(t, privateKey, "rsa-1", "issuer")
	if _, err := ValidateJWT(token, cfg); err == nil {
		t.Fatal("ValidateJWT succeeded during JWKS failure")
	}
	healthy.Store(true)
	defaultRemoteJWTCaches.mu.Lock()
	cache := defaultRemoteJWTCaches.jwks[jwksCacheKey(source)]
	defaultRemoteJWTCaches.mu.Unlock()
	if cache == nil {
		t.Fatal("JWKS cache missing after failed validation")
	}
	cache.mu.Lock()
	cache.retryAfter = time.Now().Add(-time.Second)
	cache.mu.Unlock()
	if _, err := ValidateJWT(token, cfg); err != nil {
		t.Fatalf("ValidateJWT after JWKS recovery: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("JWKS requests = %d, want failure and post-cooldown recovery fetch", got)
	}
}

func TestValidateJWTContextCancelsJWKSFetch(t *testing.T) {
	privateKey, _ := generateRS256JWK(t, "missing")
	entered := make(chan struct{})
	remoteCanceled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		close(entered)
		<-req.Context().Done()
		close(remoteCanceled)
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:         "issuer",
			JWKSURL:        srv.URL,
			RefreshTimeout: 10 * time.Second,
		}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	token := mintRS256Token(t, privateKey, "missing", "issuer")
	go func() {
		_, err := ValidateJWTContext(ctx, token, cfg)
		errCh <- err
	}()
	<-entered
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateJWTContext error = %v, want context cancellation", err)
	}
	select {
	case <-remoteCanceled:
	case <-time.After(time.Second):
		t.Fatal("JWKS HTTP request context was not canceled")
	}
}

func TestValidateJWTContextCancelDoesNotAbortSharedJWKSFetch(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "rsa-1")
	entered := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		close(entered)
		select {
		case <-release:
			writeJWKS(t, w, jwk)
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)

	source := JWKSConfig{Issuer: "issuer", JWKSURL: srv.URL, RefreshTimeout: 10 * time.Second}
	validator, err := NewValidator(&JWTConfig{
		JWKS:     []JWKSConfig{source},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	token := mintRS256Token(t, privateKey, "rsa-1", "issuer")
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := validator.ValidateJWT(leaderCtx, token)
		leaderErr <- err
	}()
	<-entered
	followerErr := make(chan error, 1)
	go func() {
		_, err := validator.ValidateJWT(context.Background(), token)
		followerErr <- err
	}()

	deadline := time.Now().Add(time.Second)
	for {
		cache := validator.caches.jwksCache(jwksCacheKey(source))
		cache.mu.Lock()
		waiters := 0
		if cache.inFlight != nil {
			waiters = cache.inFlight.waiters
		}
		cache.mu.Unlock()
		if waiters == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("shared JWKS waiters = %d, want 2", waiters)
		}
		time.Sleep(time.Millisecond)
	}

	cancelLeader()
	if err := <-leaderErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want context cancellation", err)
	}
	close(release)
	if err := <-followerErr; err != nil {
		t.Fatalf("follower validation failed after leader cancellation: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("JWKS requests = %d, want one shared fetch", got)
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
	point, err := privateKey.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, jwkDocumentKey{
		KeyType:   "EC",
		KeyID:     keyID,
		Algorithm: "ES256",
		Use:       "sig",
		Crv:       "P-256",
		X:         base64.RawURLEncoding.EncodeToString(point[1:33]),
		Y:         base64.RawURLEncoding.EncodeToString(point[33:]),
	}
}

func writeJWKS(t *testing.T, w http.ResponseWriter, keys ...jwkDocumentKey) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(jwksDocument{Keys: keys}); err != nil {
		t.Fatal(err)
	}
}
