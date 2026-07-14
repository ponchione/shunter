package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOIDCDiscoveryLoopbackHTTPAllowed(t *testing.T) {
	_, jwk := generateRS256JWK(t, "rsa-1")
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeOIDCDiscovery(t, w, oidcDiscoveryDoc(srv.URL, srv.URL+"/jwks", "RS256"))
		case "/jwks":
			writeJWKS(t, w, jwk)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	source, err := jwksForOIDCDiscovery(OIDCDiscoveryConfig{Issuer: srv.URL, CacheTTL: time.Hour})
	if err != nil {
		t.Fatalf("jwksForOIDCDiscovery returned error: %v", err)
	}
	if source.Issuer != srv.URL || source.JWKSURL != srv.URL+"/jwks" {
		t.Fatalf("resolved source = %+v, want issuer %q jwks %q", source, srv.URL, srv.URL+"/jwks")
	}
}

func TestOIDCDiscoveryRejectsNonLoopbackHTTP(t *testing.T) {
	err := ValidateJWTConfig(&JWTConfig{
		OIDCDiscovery: []OIDCDiscoveryConfig{{Issuer: "http://issuer.example"}},
	})
	if !errors.Is(err, ErrJWTInvalid) || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("ValidateJWTConfig error = %v, want HTTPS validation error", err)
	}
}

func TestOIDCDiscoveryRequiresDiscoveryURLForNonURLIssuer(t *testing.T) {
	err := ValidateJWTConfig(&JWTConfig{
		OIDCDiscovery: []OIDCDiscoveryConfig{{Issuer: "issuer"}},
	})
	if !errors.Is(err, ErrJWTInvalid) || !strings.Contains(err.Error(), "discovery url is required") {
		t.Fatalf("ValidateJWTConfig error = %v, want required discovery url error", err)
	}
}

func TestOIDCDiscoveryRejectsIssuerMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOIDCDiscovery(t, w, oidcDiscoveryDoc("https://other.example", "https://issuer.example/jwks"))
	}))
	t.Cleanup(srv.Close)

	_, err := jwksForOIDCDiscovery(OIDCDiscoveryConfig{Issuer: srv.URL, DiscoveryURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "does not match configured issuer") {
		t.Fatalf("jwksForOIDCDiscovery error = %v, want issuer mismatch", err)
	}
}

func TestOIDCDiscoveryRejectsMissingJWKSURI(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOIDCDiscovery(t, w, map[string]any{"issuer": srv.URL})
	}))
	t.Cleanup(srv.Close)

	_, err := jwksForOIDCDiscovery(OIDCDiscoveryConfig{Issuer: srv.URL, DiscoveryURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "jwks_uri is required") {
		t.Fatalf("jwksForOIDCDiscovery error = %v, want missing jwks_uri", err)
	}
}

func TestOIDCDiscoveryRejectsTrailingJSON(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOIDCDiscovery(t, w, oidcDiscoveryDoc(srv.URL, srv.URL+"/jwks", "RS256"))
		_, _ = w.Write([]byte(`{"issuer":"extra"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := jwksForOIDCDiscovery(OIDCDiscoveryConfig{Issuer: srv.URL, DiscoveryURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "trailing JSON value") {
		t.Fatalf("jwksForOIDCDiscovery error = %v, want trailing JSON error", err)
	}
}

func TestOIDCDiscoveryRejectsUnsupportedAlgorithm(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOIDCDiscovery(t, w, oidcDiscoveryDoc(srv.URL, srv.URL+"/jwks", "HS256"))
	}))
	t.Cleanup(srv.Close)

	_, err := jwksForOIDCDiscovery(OIDCDiscoveryConfig{Issuer: srv.URL, DiscoveryURL: srv.URL})
	if !errors.Is(err, ErrJWTUnsupportedAlg) {
		t.Fatalf("jwksForOIDCDiscovery error = %v, want ErrJWTUnsupportedAlg", err)
	}
}

func TestOIDCDiscoveryAbsentAlgorithmListDefaultsToRemoteAlgorithms(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOIDCDiscovery(t, w, oidcDiscoveryDoc(srv.URL, srv.URL+"/jwks"))
	}))
	t.Cleanup(srv.Close)

	source, err := jwksForOIDCDiscovery(OIDCDiscoveryConfig{Issuer: srv.URL, DiscoveryURL: srv.URL})
	if err != nil {
		t.Fatalf("jwksForOIDCDiscovery returned error: %v", err)
	}
	if len(source.Algorithms) != 0 {
		t.Fatalf("resolved algorithms = %#v, want default JWKS remote algorithm behavior", source.Algorithms)
	}
	if !jwksSourceAllowsAlgorithm(source, JWTAlgorithmRS256) || !jwksSourceAllowsAlgorithm(source, JWTAlgorithmES256) {
		t.Fatalf("resolved source does not allow default RS256/ES256 algorithms: %+v", source)
	}
}

func TestOIDCDiscoveryIntersectsConfiguredAndAdvertisedAlgorithms(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOIDCDiscovery(t, w, oidcDiscoveryDoc(srv.URL, srv.URL+"/jwks", "RS256", "ES256"))
	}))
	t.Cleanup(srv.Close)

	source, err := jwksForOIDCDiscovery(OIDCDiscoveryConfig{
		Issuer:       srv.URL,
		DiscoveryURL: srv.URL,
		Algorithms:   []JWTAlgorithm{JWTAlgorithmES256},
	})
	if err != nil {
		t.Fatalf("jwksForOIDCDiscovery returned error: %v", err)
	}
	if len(source.Algorithms) != 1 || source.Algorithms[0] != JWTAlgorithmES256 {
		t.Fatalf("resolved algorithms = %#v, want ES256 intersection", source.Algorithms)
	}
}

func TestOIDCDiscoveryResultReusedFromCache(t *testing.T) {
	var requests atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeOIDCDiscovery(t, w, oidcDiscoveryDoc(srv.URL, srv.URL+"/jwks", "RS256"))
	}))
	t.Cleanup(srv.Close)

	cfg := OIDCDiscoveryConfig{Issuer: srv.URL, DiscoveryURL: srv.URL, CacheTTL: time.Hour}
	for range 2 {
		if _, err := jwksForOIDCDiscovery(cfg); err != nil {
			t.Fatalf("jwksForOIDCDiscovery returned error: %v", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("discovery requests = %d, want 1 cached fetch", got)
	}
}

func TestOIDCDiscoveryCacheDoesNotShareRefreshTimeoutBetweenConfigurations(t *testing.T) {
	var requests atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeOIDCDiscovery(t, w, oidcDiscoveryDoc(srv.URL, srv.URL+"/jwks", "RS256"))
	}))
	t.Cleanup(srv.Close)

	base := OIDCDiscoveryConfig{
		Issuer:       srv.URL,
		DiscoveryURL: srv.URL,
		CacheTTL:     time.Hour,
	}
	first := base
	first.RefreshTimeout = time.Second
	firstSource, err := jwksForOIDCDiscovery(first)
	if err != nil {
		t.Fatalf("first jwksForOIDCDiscovery returned error: %v", err)
	}
	if got := firstSource.RefreshTimeout; got != time.Second {
		t.Fatalf("first refresh timeout = %v, want %v", got, time.Second)
	}

	second := base
	second.RefreshTimeout = 9 * time.Second
	secondSource, err := jwksForOIDCDiscovery(second)
	if err != nil {
		t.Fatalf("second jwksForOIDCDiscovery returned error: %v", err)
	}
	if got := secondSource.RefreshTimeout; got != 9*time.Second {
		t.Fatalf("cached refresh timeout = %v, want caller configuration %v", got, 9*time.Second)
	}
	if got := secondSource.CacheTTL; got != time.Hour {
		t.Fatalf("cached cache ttl = %v, want caller configuration %v", got, time.Hour)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("discovery requests = %d, want 1 shared discovery fetch", got)
	}
}

func TestValidateJWTDiscoveredJWKSValidatesTokenEndToEnd(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "rsa-1")
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeOIDCDiscovery(t, w, oidcDiscoveryDoc(srv.URL, srv.URL+"/jwks", "RS256"))
		case "/jwks":
			writeJWKS(t, w, jwk)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &JWTConfig{
		OIDCDiscovery: []OIDCDiscoveryConfig{{Issuer: srv.URL, CacheTTL: time.Hour}},
		Issuers:       []string{srv.URL},
		AuthMode:      AuthModeStrict,
	}
	claims, err := ValidateJWT(mintRS256Token(t, privateKey, "rsa-1", srv.URL), cfg)
	if err != nil {
		t.Fatalf("ValidateJWT returned error: %v", err)
	}
	if claims.Subject != "alice" || claims.Issuer != srv.URL {
		t.Fatalf("claims = %+v, want alice/%s", claims, srv.URL)
	}
}

func TestValidateJWTExplicitJWKSBehaviorUnchangedWithDiscoveryConfigured(t *testing.T) {
	privateKey, jwk := generateRS256JWK(t, "rsa-1")
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJWKS(t, w, jwk)
	}))
	t.Cleanup(jwksServer.Close)

	var discoveryRequests atomic.Int32
	discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discoveryRequests.Add(1)
		writeOIDCDiscovery(t, w, oidcDiscoveryDoc("other", jwksServer.URL, "RS256"))
	}))
	t.Cleanup(discoveryServer.Close)

	cfg := &JWTConfig{
		JWKS: []JWKSConfig{{
			Issuer:  "issuer",
			JWKSURL: jwksServer.URL,
		}},
		OIDCDiscovery: []OIDCDiscoveryConfig{{
			Issuer:       "other",
			DiscoveryURL: discoveryServer.URL,
		}},
		Issuers:  []string{"issuer"},
		AuthMode: AuthModeStrict,
	}
	if _, err := ValidateJWT(mintRS256Token(t, privateKey, "rsa-1", "issuer"), cfg); err != nil {
		t.Fatalf("ValidateJWT with explicit JWKS returned error: %v", err)
	}
	if got := discoveryRequests.Load(); got != 0 {
		t.Fatalf("discovery requests = %d, want explicit JWKS validation to skip unmatched discovery source", got)
	}
}

func oidcDiscoveryDoc(issuer, jwksURI string, algorithms ...string) map[string]any {
	doc := map[string]any{
		"issuer":   issuer,
		"jwks_uri": jwksURI,
	}
	if algorithms != nil {
		doc["id_token_signing_alg_values_supported"] = algorithms
	}
	return doc
}

func writeOIDCDiscovery(t *testing.T, w http.ResponseWriter, doc map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		t.Fatal(err)
	}
}
