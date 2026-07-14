package auth

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	defaultJWKSCacheTTL        = 10 * time.Minute
	defaultJWKSRefreshTimeout  = 5 * time.Second
	defaultJWKSRefreshCooldown = 30 * time.Second
	maxJWKSResponseBytes       = 1 << 20
)

var jwksCaches sync.Map // map[string]*jwksCache

type jwksCache struct {
	mu                  sync.Mutex
	expiresAt           time.Time
	lastForcedRefreshAt time.Time
	keys                []resolvedJWTVerificationKey
}

type jwksDocument struct {
	Keys []jwkDocumentKey `json:"keys"`
}

type jwkDocumentKey struct {
	KeyType   string `json:"kty"`
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
	N         string `json:"n"`
	E         string `json:"e"`
	Crv       string `json:"crv"`
	X         string `json:"x"`
	Y         string `json:"y"`
}

func validateJWKSConfig(config *JWTConfig) error {
	if config == nil {
		return fmt.Errorf("%w: config is required", ErrJWTInvalid)
	}
	for i, source := range config.JWKS {
		if strings.TrimSpace(source.Issuer) == "" {
			return fmt.Errorf("%w: jwks source %d: issuer is required", ErrJWTInvalid, i+1)
		}
		if err := validateJWKSURL(source.JWKSURL); err != nil {
			return fmt.Errorf("%w: jwks source %d: %w", ErrJWTInvalid, i+1, err)
		}
		if err := validateRemoteJWTAlgorithms(source.Algorithms); err != nil {
			return fmt.Errorf("%w: jwks source %d: %w", ErrJWTInvalid, i+1, err)
		}
		if source.CacheTTL < 0 {
			return fmt.Errorf("%w: jwks source %d: cache ttl must not be negative", ErrJWTInvalid, i+1)
		}
		if source.RefreshTimeout < 0 {
			return fmt.Errorf("%w: jwks source %d: refresh timeout must not be negative", ErrJWTInvalid, i+1)
		}
	}
	return nil
}

func validateJWKSURL(raw string) error {
	return validateRemoteAuthURL(raw, "jwks url")
}

func validateRemoteAuthURL(raw, label string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if u.Host == "" {
		return fmt.Errorf("%s host is required", label)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme != "http" {
		return fmt.Errorf("%s must use https", label)
	}
	if !isLoopbackJWKSHost(u.Hostname()) {
		return fmt.Errorf("%s must use https unless the host is loopback", label)
	}
	return nil
}

func validateRemoteJWTAlgorithms(algorithms []JWTAlgorithm) error {
	for _, alg := range algorithms {
		if alg != JWTAlgorithmRS256 && alg != JWTAlgorithmES256 {
			return fmt.Errorf("%w: %s", ErrJWTUnsupportedAlg, alg)
		}
	}
	return nil
}

func isLoopbackJWKSHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost":
		return true
	case "":
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func resolveJWKSVerificationKeys(config *JWTConfig, alg JWTAlgorithm, keyID, tokenIssuer string) ([]resolvedJWTVerificationKey, error) {
	if config == nil || (len(config.JWKS) == 0 && len(config.OIDCDiscovery) == 0) {
		return nil, nil
	}
	if err := validateJWKSConfig(config); err != nil {
		return nil, err
	}
	if err := validateOIDCDiscoveryConfig(config); err != nil {
		return nil, err
	}
	var out []resolvedJWTVerificationKey
	var lastErr error
	for _, source := range config.JWKS {
		if strings.TrimSpace(source.Issuer) != tokenIssuer {
			continue
		}
		if !jwksSourceAllowsAlgorithm(source, alg) {
			continue
		}
		keys, err := keysForJWKS(source, false)
		if err != nil {
			lastErr = err
			continue
		}
		matches := matchingJWKSVerificationKeys(keys, alg, keyID)
		if len(matches) == 0 && keyID != "" {
			keys, err = keysForJWKS(source, true)
			if err != nil {
				lastErr = err
				continue
			}
			matches = matchingJWKSVerificationKeys(keys, alg, keyID)
		}
		out = append(out, matches...)
	}
	for _, discovery := range config.OIDCDiscovery {
		if strings.TrimSpace(discovery.Issuer) != tokenIssuer {
			continue
		}
		if !oidcDiscoverySourceAllowsAlgorithm(discovery, alg) {
			continue
		}
		source, err := jwksForOIDCDiscovery(discovery)
		if err != nil {
			lastErr = err
			continue
		}
		if !jwksSourceAllowsAlgorithm(source, alg) {
			continue
		}
		keys, err := keysForJWKS(source, false)
		if err != nil {
			lastErr = err
			continue
		}
		matches := matchingJWKSVerificationKeys(keys, alg, keyID)
		if len(matches) == 0 && keyID != "" {
			keys, err = keysForJWKS(source, true)
			if err != nil {
				lastErr = err
				continue
			}
			matches = matchingJWKSVerificationKeys(keys, alg, keyID)
		}
		out = append(out, matches...)
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

func matchingJWKSVerificationKeys(keys []resolvedJWTVerificationKey, alg JWTAlgorithm, keyID string) []resolvedJWTVerificationKey {
	var out []resolvedJWTVerificationKey
	for _, key := range keys {
		if key.algorithm != alg {
			continue
		}
		if keyID != "" && key.keyID != keyID {
			continue
		}
		out = append(out, key)
	}
	return out
}

func jwksSourceAllowsAlgorithm(source JWKSConfig, alg JWTAlgorithm) bool {
	if alg != JWTAlgorithmRS256 && alg != JWTAlgorithmES256 {
		return false
	}
	if len(source.Algorithms) == 0 {
		return true
	}
	return slices.Contains(source.Algorithms, alg)
}

func keysForJWKS(source JWKSConfig, forceRefresh bool) ([]resolvedJWTVerificationKey, error) {
	cacheAny, _ := jwksCaches.LoadOrStore(jwksCacheKey(source), &jwksCache{})
	cache := cacheAny.(*jwksCache)
	cache.mu.Lock()
	defer cache.mu.Unlock()

	now := time.Now()
	cacheValid := len(cache.keys) != 0 && now.Before(cache.expiresAt)
	if forceRefresh && cacheValid && !cache.lastForcedRefreshAt.IsZero() &&
		now.Before(cache.lastForcedRefreshAt.Add(defaultJWKSRefreshCooldown)) {
		return cloneResolvedJWTVerificationKeys(cache.keys), nil
	}
	if !forceRefresh && cacheValid {
		return cloneResolvedJWTVerificationKeys(cache.keys), nil
	}
	if forceRefresh {
		// Record the attempt before remote I/O. Concurrent waiters serialize on
		// mu and reuse the current cache instead of each issuing another fetch.
		cache.lastForcedRefreshAt = now
	}
	keys, err := fetchJWKS(source)
	if err != nil {
		if len(cache.keys) != 0 && now.Before(cache.expiresAt) {
			return cloneResolvedJWTVerificationKeys(cache.keys), nil
		}
		return nil, err
	}
	ttl := source.CacheTTL
	if ttl == 0 {
		ttl = defaultJWKSCacheTTL
	}
	cache.keys = keys
	cache.expiresAt = now.Add(ttl)
	return cloneResolvedJWTVerificationKeys(keys), nil
}

func jwksCacheKey(source JWKSConfig) string {
	return jwtSourceCacheKey(source.Issuer, source.JWKSURL, source.Algorithms, source.CacheTTL)
}

func jwtSourceCacheKey(issuer, endpoint string, algorithms []JWTAlgorithm, ttl time.Duration) string {
	algs := slices.Clone(algorithms)
	slices.Sort(algs)
	var b strings.Builder
	b.WriteString(strings.TrimSpace(issuer))
	b.WriteString("\x00")
	b.WriteString(strings.TrimSpace(endpoint))
	b.WriteString("\x00")
	for _, alg := range algs {
		b.WriteString(string(alg))
		b.WriteString("\x00")
	}
	b.WriteString(ttl.String())
	return b.String()
}

func fetchJWKS(source JWKSConfig) ([]resolvedJWTVerificationKey, error) {
	timeout := source.RefreshTimeout
	if timeout == 0 {
		timeout = defaultJWKSRefreshTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(source.JWKSURL), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	resp, err := remoteAuthHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch jwks: unexpected HTTP status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read jwks: %w", err)
	}
	if len(data) > maxJWKSResponseBytes {
		return nil, fmt.Errorf("decode jwks: response exceeds %d bytes", maxJWKSResponseBytes)
	}
	var doc jwksDocument
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode jwks: trailing JSON value")
		}
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	if len(doc.Keys) == 0 {
		return nil, fmt.Errorf("decode jwks: keys is empty")
	}
	keys := make([]resolvedJWTVerificationKey, 0, len(doc.Keys))
	for _, raw := range doc.Keys {
		key, err := resolveJWK(raw)
		if err != nil {
			continue
		}
		if !jwksSourceAllowsAlgorithm(source, key.algorithm) {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("decode jwks: no supported verification keys")
	}
	return keys, nil
}

func resolveJWK(raw jwkDocumentKey) (resolvedJWTVerificationKey, error) {
	if raw.Use != "" && raw.Use != "sig" {
		return resolvedJWTVerificationKey{}, fmt.Errorf("jwk use %q is not sig", raw.Use)
	}
	switch raw.KeyType {
	case "RSA":
		if alg := jwkAlgorithm(raw, JWTAlgorithmRS256); alg != JWTAlgorithmRS256 {
			return resolvedJWTVerificationKey{}, fmt.Errorf("RSA jwk algorithm %q is not RS256", alg)
		}
		key, err := rsaPublicKeyFromJWK(raw)
		if err != nil {
			return resolvedJWTVerificationKey{}, err
		}
		return resolvedJWTVerificationKey{algorithm: JWTAlgorithmRS256, keyID: raw.KeyID, key: key}, nil
	case "EC":
		if alg := jwkAlgorithm(raw, JWTAlgorithmES256); alg != JWTAlgorithmES256 {
			return resolvedJWTVerificationKey{}, fmt.Errorf("EC jwk algorithm %q is not ES256", alg)
		}
		key, err := ecdsaPublicKeyFromJWK(raw)
		if err != nil {
			return resolvedJWTVerificationKey{}, err
		}
		return resolvedJWTVerificationKey{algorithm: JWTAlgorithmES256, keyID: raw.KeyID, key: key}, nil
	default:
		return resolvedJWTVerificationKey{}, fmt.Errorf("unsupported jwk kty %q", raw.KeyType)
	}
}

func jwkAlgorithm(raw jwkDocumentKey, fallback JWTAlgorithm) JWTAlgorithm {
	if raw.Algorithm == "" {
		return fallback
	}
	return JWTAlgorithm(raw.Algorithm)
}

func rsaPublicKeyFromJWK(raw jwkDocumentKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(raw.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(raw.E)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.BitLen() > 31 {
		return nil, fmt.Errorf("invalid RSA jwk")
	}
	key := &rsa.PublicKey{N: n, E: int(e.Int64())}
	if err := validateRSAPublicKey(key); err != nil {
		return nil, fmt.Errorf("invalid RSA jwk: %w", err)
	}
	return key, nil
}

func ecdsaPublicKeyFromJWK(raw jwkDocumentKey) (*ecdsa.PublicKey, error) {
	if raw.Crv != "P-256" {
		return nil, fmt.Errorf("ES256 requires P-256 jwk curve")
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(raw.X)
	if err != nil {
		return nil, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(raw.Y)
	if err != nil {
		return nil, err
	}
	if len(xBytes) > 32 || len(yBytes) > 32 {
		return nil, fmt.Errorf("invalid ECDSA jwk coordinate length")
	}
	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)
	point := make([]byte, 1+32+32)
	point[0] = 4
	copy(point[1+32-len(xBytes):33], xBytes)
	copy(point[33+32-len(yBytes):], yBytes)
	if _, err := ecdh.P256().NewPublicKey(point); err != nil {
		return nil, fmt.Errorf("invalid ECDSA jwk point")
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}

func cloneResolvedJWTVerificationKeys(in []resolvedJWTVerificationKey) []resolvedJWTVerificationKey {
	return slices.Clone(in)
}
