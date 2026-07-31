package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

type oidcDiscoveryCache struct {
	mu                  sync.Mutex
	expiresAt           time.Time
	source              JWKSConfig
	inFlight            *remoteAuthFlight
	lastErr             error
	retryAfter          time.Time
	consecutiveFailures uint
	lastUsed            time.Time
}

type oidcDiscoveryDocument struct {
	Issuer                           string    `json:"issuer"`
	JWKSURI                          string    `json:"jwks_uri"`
	IDTokenSigningAlgValuesSupported *[]string `json:"id_token_signing_alg_values_supported"`
}

func validateOIDCDiscoveryConfig(config *JWTConfig) error {
	if config == nil {
		return fmt.Errorf("%w: config is required", ErrJWTInvalid)
	}
	for i, source := range config.OIDCDiscovery {
		normalized, err := normalizeOIDCDiscoveryConfig(source)
		if err != nil {
			return fmt.Errorf("%w: oidc discovery source %d: %w", ErrJWTInvalid, i+1, err)
		}
		if err := validateOIDCDiscoveryURL(normalized.DiscoveryURL); err != nil {
			return fmt.Errorf("%w: oidc discovery source %d: %w", ErrJWTInvalid, i+1, err)
		}
		if err := validateRemoteJWTAlgorithms(normalized.Algorithms); err != nil {
			return fmt.Errorf("%w: oidc discovery source %d: %w", ErrJWTInvalid, i+1, err)
		}
		if normalized.CacheTTL < 0 {
			return fmt.Errorf("%w: oidc discovery source %d: cache ttl must not be negative", ErrJWTInvalid, i+1)
		}
		if normalized.RefreshTimeout < 0 {
			return fmt.Errorf("%w: oidc discovery source %d: refresh timeout must not be negative", ErrJWTInvalid, i+1)
		}
	}
	return nil
}

func normalizeOIDCDiscoveryConfig(source OIDCDiscoveryConfig) (OIDCDiscoveryConfig, error) {
	source.Issuer = strings.TrimSpace(source.Issuer)
	source.DiscoveryURL = strings.TrimSpace(source.DiscoveryURL)
	if source.Issuer == "" {
		return OIDCDiscoveryConfig{}, fmt.Errorf("issuer is required")
	}
	if source.DiscoveryURL == "" {
		discoveryURL, err := defaultOIDCDiscoveryURL(source.Issuer)
		if err != nil {
			return OIDCDiscoveryConfig{}, err
		}
		source.DiscoveryURL = discoveryURL
	}
	return source, nil
}

func defaultOIDCDiscoveryURL(issuer string) (string, error) {
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme == "" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("discovery url is required when issuer is not a URL")
	}
	return strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration", nil
}

func validateOIDCDiscoveryURL(raw string) error {
	return validateRemoteAuthURL(raw, "oidc discovery url")
}

func oidcDiscoverySourceAllowsAlgorithm(source OIDCDiscoveryConfig, alg JWTAlgorithm) bool {
	return remoteSourceAllowsAlgorithm(source.Algorithms, alg)
}

func jwksForOIDCDiscovery(source OIDCDiscoveryConfig) (JWKSConfig, error) {
	return jwksForOIDCDiscoveryContext(context.Background(), defaultRemoteJWTCaches, source)
}

func jwksForOIDCDiscoveryContext(ctx context.Context, caches *remoteJWTCacheSet, source OIDCDiscoveryConfig) (JWKSConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if caches == nil {
		caches = defaultRemoteJWTCaches
	}
	normalized, err := normalizeOIDCDiscoveryConfig(source)
	if err != nil {
		return JWKSConfig{}, err
	}
	if err := validateOIDCDiscoveryURL(normalized.DiscoveryURL); err != nil {
		return JWKSConfig{}, err
	}
	if err := validateRemoteJWTAlgorithms(normalized.Algorithms); err != nil {
		return JWKSConfig{}, err
	}
	if normalized.CacheTTL < 0 {
		return JWKSConfig{}, fmt.Errorf("cache ttl must not be negative")
	}
	if normalized.RefreshTimeout < 0 {
		return JWKSConfig{}, fmt.Errorf("refresh timeout must not be negative")
	}

	cacheKey := oidcDiscoveryCacheKey(normalized)
	cache := caches.discoveryCache(cacheKey)
	for {
		now := time.Now()
		cache.mu.Lock()
		cache.lastUsed = now
		if cache.source.JWKSURL != "" && now.Before(cache.expiresAt) {
			resolved := oidcDiscoveryJWKSConfig(cache.source, normalized)
			cache.mu.Unlock()
			return resolved, nil
		}
		if cache.inFlight != nil {
			flight := cache.inFlight
			flight.waiters++
			cache.mu.Unlock()
			if err := waitForOIDCDiscoveryFlight(ctx, cache, flight); err != nil {
				return JWKSConfig{}, err
			}
			continue
		}
		if cache.lastErr != nil && now.Before(cache.retryAfter) {
			err := cache.lastErr
			cache.mu.Unlock()
			return JWKSConfig{}, err
		}

		fetchCtx, cancel := context.WithCancel(context.Background())
		flight := &remoteAuthFlight{done: make(chan struct{}), cancel: cancel, waiters: 1}
		cache.inFlight = flight
		cache.mu.Unlock()
		go refreshOIDCDiscoveryCache(fetchCtx, cache, cacheKey, normalized, flight)
		if err := waitForOIDCDiscoveryFlight(ctx, cache, flight); err != nil {
			return JWKSConfig{}, err
		}
	}
}

func waitForOIDCDiscoveryFlight(ctx context.Context, cache *oidcDiscoveryCache, flight *remoteAuthFlight) error {
	select {
	case <-flight.done:
		return nil
	case <-ctx.Done():
		cache.mu.Lock()
		if cache.inFlight == flight {
			flight.waiters--
			if flight.waiters == 0 {
				flight.cancel()
			}
		}
		cache.mu.Unlock()
		return ctx.Err()
	}
}

func refreshOIDCDiscoveryCache(ctx context.Context, cache *oidcDiscoveryCache, cacheKey string, source OIDCDiscoveryConfig, flight *remoteAuthFlight) {
	defer flight.cancel()
	resolved, err := fetchOIDCDiscoveryContext(ctx, source)
	completedAt := time.Now()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.inFlight != flight {
		return
	}
	cache.inFlight = nil
	if err != nil {
		cache.consecutiveFailures++
		cache.lastErr = err
		cache.retryAfter = completedAt.Add(remoteAuthRetryDelay(cacheKey, cache.consecutiveFailures))
	} else {
		ttl := source.CacheTTL
		if ttl == 0 {
			ttl = defaultJWKSCacheTTL
		}
		cache.source = resolved
		cache.expiresAt = completedAt.Add(ttl)
		cache.lastErr = nil
		cache.retryAfter = time.Time{}
		cache.consecutiveFailures = 0
	}
	close(flight.done)
}

func (c *oidcDiscoveryCache) touch(now time.Time) {
	c.mu.Lock()
	c.lastUsed = now
	c.mu.Unlock()
}

func (c *oidcDiscoveryCache) idleBefore(cutoff time.Time) bool {
	lastUsed, evictable := c.evictionState()
	return evictable && lastUsed.Before(cutoff)
}

func (c *oidcDiscoveryCache) evictionState() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastUsed, c.inFlight == nil
}

// oidcDiscoveryJWKSConfig overlays caller-owned operational settings on the
// discovery-derived source. Discovery cache entries are shared process-wide,
// so they must not retain one runtime's refresh behavior for another runtime.
func oidcDiscoveryJWKSConfig(discovered JWKSConfig, source OIDCDiscoveryConfig) JWKSConfig {
	discovered = cloneJWKSConfig(discovered)
	discovered.CacheTTL = source.CacheTTL
	discovered.RefreshTimeout = source.RefreshTimeout
	return discovered
}

func oidcDiscoveryCacheKey(source OIDCDiscoveryConfig) string {
	return jwtSourceCacheKey(source.Issuer, source.DiscoveryURL, source.Algorithms, source.CacheTTL)
}

func fetchOIDCDiscovery(source OIDCDiscoveryConfig) (JWKSConfig, error) {
	return fetchOIDCDiscoveryContext(context.Background(), source)
}

func fetchOIDCDiscoveryContext(ctx context.Context, source OIDCDiscoveryConfig) (JWKSConfig, error) {
	timeout := source.RefreshTimeout
	if timeout == 0 {
		timeout = defaultJWKSRefreshTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(source.DiscoveryURL), nil)
	if err != nil {
		return JWKSConfig{}, fmt.Errorf("fetch oidc discovery: %w", err)
	}
	resp, err := remoteAuthHTTPClient.Do(req)
	if err != nil {
		return JWKSConfig{}, fmt.Errorf("fetch oidc discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return JWKSConfig{}, fmt.Errorf("fetch oidc discovery: unexpected HTTP status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSResponseBytes+1))
	if err != nil {
		return JWKSConfig{}, fmt.Errorf("read oidc discovery: %w", err)
	}
	if len(data) > maxJWKSResponseBytes {
		return JWKSConfig{}, fmt.Errorf("decode oidc discovery: response exceeds %d bytes", maxJWKSResponseBytes)
	}

	var doc oidcDiscoveryDocument
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return JWKSConfig{}, fmt.Errorf("decode oidc discovery: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return JWKSConfig{}, fmt.Errorf("decode oidc discovery: trailing JSON value")
		}
		return JWKSConfig{}, fmt.Errorf("decode oidc discovery: %w", err)
	}

	issuer := strings.TrimSpace(source.Issuer)
	if doc.Issuer == "" {
		return JWKSConfig{}, fmt.Errorf("decode oidc discovery: issuer is required")
	}
	if doc.Issuer != issuer {
		return JWKSConfig{}, fmt.Errorf("decode oidc discovery: issuer %q does not match configured issuer %q", doc.Issuer, issuer)
	}
	jwksURI := strings.TrimSpace(doc.JWKSURI)
	if jwksURI == "" {
		return JWKSConfig{}, fmt.Errorf("decode oidc discovery: jwks_uri is required")
	}
	if err := validateJWKSURL(jwksURI); err != nil {
		return JWKSConfig{}, fmt.Errorf("decode oidc discovery: %w", err)
	}
	algorithms, err := oidcDiscoveryAlgorithms(source.Algorithms, doc.IDTokenSigningAlgValuesSupported)
	if err != nil {
		return JWKSConfig{}, fmt.Errorf("decode oidc discovery: %w", err)
	}
	return JWKSConfig{
		Issuer:     issuer,
		JWKSURL:    jwksURI,
		Algorithms: algorithms,
	}, nil
}

func oidcDiscoveryAlgorithms(configured []JWTAlgorithm, advertised *[]string) ([]JWTAlgorithm, error) {
	if advertised == nil {
		if len(configured) == 0 {
			return nil, nil
		}
		return slices.Clone(configured), nil
	}
	advertisedSet := make(map[JWTAlgorithm]struct{}, len(*advertised))
	for _, raw := range *advertised {
		advertisedSet[JWTAlgorithm(strings.TrimSpace(raw))] = struct{}{}
	}
	allowed := configured
	if len(allowed) == 0 {
		allowed = []JWTAlgorithm{JWTAlgorithmRS256, JWTAlgorithmES256}
	}
	out := make([]JWTAlgorithm, 0, len(allowed))
	seen := make(map[JWTAlgorithm]struct{}, len(allowed))
	for _, alg := range allowed {
		if _, ok := advertisedSet[alg]; !ok {
			continue
		}
		if _, ok := seen[alg]; ok {
			continue
		}
		seen[alg] = struct{}{}
		out = append(out, alg)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no supported id token signing algorithms", ErrJWTUnsupportedAlg)
	}
	return out, nil
}

func cloneJWKSConfig(in JWKSConfig) JWKSConfig {
	in.Algorithms = slices.Clone(in.Algorithms)
	return in
}
