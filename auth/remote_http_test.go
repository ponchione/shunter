package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJWKSRedirectPolicy(t *testing.T) {
	_, jwk := generateRS256JWK(t, "redirect-key")
	t.Run("valid cross-host loopback redirect", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJWKS(t, w, jwk)
		}))
		t.Cleanup(target.Close)
		origin := redirectTestServer(t, target.URL)
		if _, err := fetchJWKS(JWKSConfig{Issuer: "issuer", JWKSURL: origin.URL}); err != nil {
			t.Fatalf("fetchJWKS valid redirect: %v", err)
		}
	})

	t.Run("reject non-loopback http target", func(t *testing.T) {
		origin := redirectTestServer(t, "http://example.com/jwks")
		_, err := fetchJWKS(JWKSConfig{Issuer: "issuer", JWKSURL: origin.URL})
		assertRemoteAuthRedirectError(t, err, "must use https unless the host is loopback")
	})

	t.Run("reject https downgrade to loopback", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJWKS(t, w, jwk)
		}))
		t.Cleanup(target.Close)
		origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusFound)
		}))
		t.Cleanup(origin.Close)
		installRemoteAuthTestHTTPClient(t, origin.Client())
		_, err := fetchJWKS(JWKSConfig{Issuer: "issuer", JWKSURL: origin.URL})
		assertRemoteAuthRedirectError(t, err, "must not downgrade https to http")
	})

	t.Run("reject redirect loop at bounded limit", func(t *testing.T) {
		origin := redirectLoopTestServer(t)
		_, err := fetchJWKS(JWKSConfig{Issuer: "issuer", JWKSURL: origin.URL})
		assertRemoteAuthRedirectError(t, err, "redirect limit exceeded")
	})
}

func TestOIDCDiscoveryRedirectPolicy(t *testing.T) {
	t.Run("valid cross-host loopback redirect", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeOIDCDiscovery(t, w, oidcDiscoveryDoc("issuer", "https://issuer.example/jwks", "RS256"))
		}))
		t.Cleanup(target.Close)
		origin := redirectTestServer(t, target.URL)
		if _, err := fetchOIDCDiscovery(OIDCDiscoveryConfig{Issuer: "issuer", DiscoveryURL: origin.URL}); err != nil {
			t.Fatalf("fetchOIDCDiscovery valid redirect: %v", err)
		}
	})

	t.Run("reject non-loopback http target", func(t *testing.T) {
		origin := redirectTestServer(t, "http://example.com/.well-known/openid-configuration")
		_, err := fetchOIDCDiscovery(OIDCDiscoveryConfig{Issuer: "issuer", DiscoveryURL: origin.URL})
		assertRemoteAuthRedirectError(t, err, "must use https unless the host is loopback")
	})

	t.Run("reject https downgrade to loopback", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeOIDCDiscovery(t, w, oidcDiscoveryDoc("issuer", "https://issuer.example/jwks", "RS256"))
		}))
		t.Cleanup(target.Close)
		origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusFound)
		}))
		t.Cleanup(origin.Close)
		installRemoteAuthTestHTTPClient(t, origin.Client())
		_, err := fetchOIDCDiscovery(OIDCDiscoveryConfig{Issuer: "issuer", DiscoveryURL: origin.URL})
		assertRemoteAuthRedirectError(t, err, "must not downgrade https to http")
	})

	t.Run("reject redirect loop at bounded limit", func(t *testing.T) {
		origin := redirectLoopTestServer(t)
		_, err := fetchOIDCDiscovery(OIDCDiscoveryConfig{Issuer: "issuer", DiscoveryURL: origin.URL})
		assertRemoteAuthRedirectError(t, err, "redirect limit exceeded")
	})
}

func redirectTestServer(t *testing.T, target string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func redirectLoopTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func installRemoteAuthTestHTTPClient(t *testing.T, base *http.Client) {
	t.Helper()
	previous := remoteAuthHTTPClient
	client := *base
	client.CheckRedirect = previous.CheckRedirect
	remoteAuthHTTPClient = &client
	t.Cleanup(func() { remoteAuthHTTPClient = previous })
}

func assertRemoteAuthRedirectError(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("redirect error = %v, want text %q", err, contains)
	}
}
