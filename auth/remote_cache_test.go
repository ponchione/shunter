package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteJWTCacheSetBoundsRetainedSources(t *testing.T) {
	caches := newRemoteJWTCacheSet(4, time.Hour)
	for i := range 100 {
		caches.jwksCache(fmt.Sprintf("jwks-%d", i))
		caches.discoveryCache(fmt.Sprintf("discovery-%d", i))
	}
	caches.mu.Lock()
	defer caches.mu.Unlock()
	if got := len(caches.jwks); got > 4 {
		t.Fatalf("retained JWKS caches = %d, want at most 4", got)
	}
	if got := len(caches.discoveries); got > 4 {
		t.Fatalf("retained discovery caches = %d, want at most 4", got)
	}
}

func TestValidatorsOwnIndependentRemoteCaches(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "rsa-1")
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeJWKS(t, w, jwk)
	}))
	t.Cleanup(srv.Close)
	cfg := &JWTConfig{
		JWKS:     []JWKSConfig{{Issuer: "issuer", JWKSURL: srv.URL, CacheTTL: time.Hour}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	token := mintRS256Token(t, privateKey, "rsa-1", "issuer")
	for range 2 {
		validator, err := NewValidator(cfg)
		if err != nil {
			t.Fatalf("NewValidator: %v", err)
		}
		if _, err := validator.ValidateJWT(context.Background(), token); err != nil {
			t.Fatalf("Validator.ValidateJWT: %v", err)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("JWKS requests = %d, want one per lifecycle-owned validator", got)
	}
}
