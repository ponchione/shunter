package auth

import (
	"context"
	"slices"
)

// Validator validates JWTs with lifecycle-owned remote JWKS and OIDC
// discovery caches. A Validator is safe for concurrent use.
type Validator struct {
	config *JWTConfig
	caches *remoteJWTCacheSet
}

// NewValidator validates and defensively copies config. Remote cache entries
// remain reachable only for the lifetime of the returned Validator.
func NewValidator(config *JWTConfig) (*Validator, error) {
	if err := ValidateJWTConfig(config); err != nil {
		return nil, err
	}
	return &Validator{
		config: cloneJWTConfig(config),
		caches: newRemoteJWTCacheSet(defaultRemoteAuthCacheEntries, defaultRemoteAuthCacheIdleTTL),
	}, nil
}

// ValidateJWT validates tokenString and propagates ctx to any remote JWKS or
// OIDC discovery request and to waits for an existing in-flight request.
func (v *Validator) ValidateJWT(ctx context.Context, tokenString string) (*Claims, error) {
	if v == nil {
		return validateJWT(ctx, tokenString, nil, defaultRemoteJWTCaches)
	}
	return validateJWT(ctx, tokenString, v.config, v.caches)
}

func cloneJWTConfig(config *JWTConfig) *JWTConfig {
	if config == nil {
		return nil
	}
	out := *config
	out.SigningKey = slices.Clone(config.SigningKey)
	out.VerificationKeys = make([]JWTVerificationKey, len(config.VerificationKeys))
	for i, key := range config.VerificationKeys {
		out.VerificationKeys[i] = key
		out.VerificationKeys[i].Key = slices.Clone(key.Key)
	}
	out.JWKS = make([]JWKSConfig, len(config.JWKS))
	for i, source := range config.JWKS {
		out.JWKS[i] = source
		out.JWKS[i].Algorithms = slices.Clone(source.Algorithms)
	}
	out.OIDCDiscovery = make([]OIDCDiscoveryConfig, len(config.OIDCDiscovery))
	for i, source := range config.OIDCDiscovery {
		out.OIDCDiscovery[i] = source
		out.OIDCDiscovery[i].Algorithms = slices.Clone(source.Algorithms)
	}
	out.Issuers = slices.Clone(config.Issuers)
	out.Audiences = slices.Clone(config.Audiences)
	out.ExtraClaims = slices.Clone(config.ExtraClaims)
	return &out
}
