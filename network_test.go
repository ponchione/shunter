package shunter

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/protocol"
)

func TestBuildProtocolOptionsUsesDefaultsForZeroConfig(t *testing.T) {
	opts, err := buildProtocolOptions(ProtocolConfig{})
	if err != nil {
		t.Fatalf("buildProtocolOptions returned error: %v", err)
	}
	want := protocol.DefaultProtocolOptions()
	if opts != want {
		t.Fatalf("options = %+v, want %+v", opts, want)
	}
}

func TestBuildProtocolOptionsAppliesOverrides(t *testing.T) {
	opts, err := buildProtocolOptions(ProtocolConfig{
		PingInterval:           time.Second,
		IdleTimeout:            2 * time.Second,
		CloseHandshakeTimeout:  3 * time.Second,
		WriteTimeout:           4 * time.Second,
		DisconnectTimeout:      5 * time.Second,
		OutgoingBufferMessages: 17,
		IncomingQueueMessages:  18,
		MaxMessageSize:         19,
	})
	if err != nil {
		t.Fatalf("buildProtocolOptions returned error: %v", err)
	}
	if opts.PingInterval != time.Second ||
		opts.IdleTimeout != 2*time.Second ||
		opts.CloseHandshakeTimeout != 3*time.Second ||
		opts.WriteTimeout != 4*time.Second ||
		opts.DisconnectTimeout != 5*time.Second ||
		opts.OutgoingBufferMessages != 17 ||
		opts.IncomingQueueMessages != 18 ||
		opts.MaxMessageSize != 19 {
		t.Fatalf("override mapping failed: %+v", opts)
	}
}

func TestBuildProtocolOptionsRejectsNegativeValues(t *testing.T) {
	cases := []ProtocolConfig{
		{PingInterval: -time.Second},
		{IdleTimeout: -time.Second},
		{CloseHandshakeTimeout: -time.Second},
		{WriteTimeout: -time.Second},
		{DisconnectTimeout: -time.Second},
		{OutgoingBufferMessages: -1},
		{IncomingQueueMessages: -1},
		{MaxMessageSize: -1},
	}
	for _, cfg := range cases {
		if _, err := buildProtocolOptions(cfg); err == nil {
			t.Fatalf("buildProtocolOptions(%+v) succeeded; want error", cfg)
		}
	}
}

func TestStartConfiguresProtocolSQLQueryLimits(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir:             t.TempDir(),
		EnableProtocol:      true,
		ListenAddr:          "127.0.0.1:0",
		OneOffQueryMaxRows:  123,
		OneOffQueryMaxBytes: 456,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if rt.protocolServer == nil {
		t.Fatal("protocol server is nil after Start")
	}
	want := protocol.SQLQueryLimits{MaxRows: 123, MaxBytes: 456}
	if got := rt.protocolServer.SQLQueryLimits; got != want {
		t.Fatalf("SQLQueryLimits = %+v, want %+v", got, want)
	}
}

func TestBuildAuthConfigDevGeneratesAnonymousMintConfig(t *testing.T) {
	jwtCfg, mintCfg, err := buildAuthConfig(Config{AuthMode: AuthModeDev})
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if jwtCfg.AuthMode != auth.AuthModeAnonymous {
		t.Fatalf("auth mode = %v, want anonymous", jwtCfg.AuthMode)
	}
	if len(jwtCfg.SigningKey) == 0 || mintCfg == nil || len(mintCfg.SigningKey) == 0 {
		t.Fatal("dev auth did not configure signing/minting")
	}
	if string(jwtCfg.SigningKey) != string(mintCfg.SigningKey) {
		t.Fatal("dev auth JWT and mint signing keys differ")
	}
}

func TestBuildAuthConfigDevMapsExtraClaims(t *testing.T) {
	jwtCfg, _, err := buildAuthConfig(Config{
		AuthMode:                AuthModeDev,
		AuthExtraClaims:         []string{"email"},
		AuthMaxExtraClaimBytes:  12,
		AuthMaxExtraClaimsBytes: 34,
	})
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if len(jwtCfg.ExtraClaims) != 1 || jwtCfg.ExtraClaims[0] != "email" {
		t.Fatalf("ExtraClaims = %#v, want email", jwtCfg.ExtraClaims)
	}
	if jwtCfg.MaxExtraClaimBytes != 12 || jwtCfg.MaxExtraClaimsBytes != 34 {
		t.Fatalf("extra claim limits = %d/%d, want 12/34", jwtCfg.MaxExtraClaimBytes, jwtCfg.MaxExtraClaimsBytes)
	}
}

func TestBuildAuthConfigDevMintedTokenValidatesWithConfiguredAudiences(t *testing.T) {
	jwtCfg, mintCfg, err := buildAuthConfig(Config{
		AuthMode:      AuthModeDev,
		AuthAudiences: []string{"app"},
	})
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if mintCfg.Audience != "app" {
		t.Fatalf("mint audience = %q, want configured audience app", mintCfg.Audience)
	}

	token, _, err := auth.MintAnonymousToken(mintCfg)
	if err != nil {
		t.Fatalf("MintAnonymousToken returned error: %v", err)
	}
	if _, err := auth.ValidateJWT(token, jwtCfg); err != nil {
		t.Fatalf("minted token did not validate against runtime JWT config: %v", err)
	}
}

func TestBuildAuthConfigDevMintedTokenValidatesWithConfiguredIssuers(t *testing.T) {
	jwtCfg, mintCfg, err := buildAuthConfig(Config{
		AuthMode:    AuthModeDev,
		AuthIssuers: []string{"external"},
	})
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if !slices.Contains(jwtCfg.Issuers, mintCfg.Issuer) {
		t.Fatalf("JWT issuers = %#v, want anonymous token issuer %q accepted", jwtCfg.Issuers, mintCfg.Issuer)
	}

	token, _, err := auth.MintAnonymousToken(mintCfg)
	if err != nil {
		t.Fatalf("MintAnonymousToken returned error: %v", err)
	}
	if _, err := auth.ValidateJWT(token, jwtCfg); err != nil {
		t.Fatalf("minted token did not validate against runtime JWT config: %v", err)
	}
}

func TestBuildAuthConfigDevExplicitAnonymousAudienceIsAccepted(t *testing.T) {
	jwtCfg, mintCfg, err := buildAuthConfig(Config{
		AuthMode:               AuthModeDev,
		AuthAudiences:          []string{"app"},
		AnonymousTokenAudience: "anonymous-app",
	})
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if !slices.Contains(jwtCfg.Audiences, "anonymous-app") {
		t.Fatalf("JWT audiences = %#v, want explicit anonymous audience accepted", jwtCfg.Audiences)
	}

	token, _, err := auth.MintAnonymousToken(mintCfg)
	if err != nil {
		t.Fatalf("MintAnonymousToken returned error: %v", err)
	}
	if _, err := auth.ValidateJWT(token, jwtCfg); err != nil {
		t.Fatalf("minted token did not validate against runtime JWT config: %v", err)
	}
}

func TestBuildAuthConfigDevRejectsNegativeAnonymousTokenTTL(t *testing.T) {
	_, _, err := buildAuthConfig(Config{AuthMode: AuthModeDev, AnonymousTokenTTL: -time.Second})
	if !errors.Is(err, ErrAnonymousTokenTTLInvalid) {
		t.Fatalf("error = %v, want ErrAnonymousTokenTTLInvalid", err)
	}
}

func TestBuildAuthConfigStrictRequiresVerificationMaterial(t *testing.T) {
	_, _, err := buildAuthConfig(Config{AuthMode: AuthModeStrict})
	if !errors.Is(err, ErrAuthSigningKeyRequired) {
		t.Fatalf("error = %v, want ErrAuthSigningKeyRequired", err)
	}
}

func TestBuildAuthConfigRejectsWeakHS256Keys(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "legacy signing key",
			cfg:  Config{AuthMode: AuthModeStrict, AuthSigningKey: []byte("too-short")},
		},
		{
			name: "explicit verification key",
			cfg: Config{AuthMode: AuthModeStrict, AuthVerificationKeys: []AuthVerificationKey{{
				Algorithm: AuthAlgorithmHS256,
				Key:       []byte("too-short"),
			}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := buildAuthConfig(tt.cfg)
			if !errors.Is(err, auth.ErrJWTInvalid) {
				t.Fatalf("buildAuthConfig error = %v, want auth.ErrJWTInvalid", err)
			}
		})
	}
}

func TestBuildAuthConfigStrictMapsIssuersAudiencesAndCopiesKey(t *testing.T) {
	key := []byte("test-secret-0123456789abcdef012345")
	issuers := []string{"issuer"}
	audiences := []string{"app"}
	extraClaims := []string{"email"}
	discoveryAlgorithms := []AuthAlgorithm{AuthAlgorithmRS256}
	cfg := Config{
		AuthMode:       AuthModeStrict,
		AuthSigningKey: key,
		AuthOIDCDiscoveryIssuers: []AuthOIDCDiscoveryIssuer{{
			Issuer:       "https://discovery.example",
			DiscoveryURL: "https://discovery.example/.well-known/openid-configuration",
			Algorithms:   discoveryAlgorithms,
		}},
		AuthIssuers:             issuers,
		AuthAudiences:           audiences,
		AuthExtraClaims:         extraClaims,
		AuthMaxExtraClaimBytes:  12,
		AuthMaxExtraClaimsBytes: 34,
	}
	jwtCfg, mintCfg, err := buildAuthConfig(cfg)
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if jwtCfg.AuthMode != auth.AuthModeStrict || mintCfg != nil {
		t.Fatalf("unexpected strict config: jwt=%+v mint=%+v", jwtCfg, mintCfg)
	}
	key[0] = 'X'
	issuers[0] = "mutated"
	audiences[0] = "mutated"
	extraClaims[0] = "mutated"
	discoveryAlgorithms[0] = AuthAlgorithmES256
	if string(jwtCfg.SigningKey) == string(key) {
		t.Fatal("signing key was not defensively copied")
	}
	if jwtCfg.Issuers[0] == issuers[0] {
		t.Fatal("issuers were not defensively copied")
	}
	if jwtCfg.Audiences[0] == audiences[0] {
		t.Fatal("audiences were not defensively copied")
	}
	if jwtCfg.ExtraClaims[0] == extraClaims[0] {
		t.Fatal("extra claims were not defensively copied")
	}
	if len(jwtCfg.OIDCDiscovery) != 1 || jwtCfg.OIDCDiscovery[0].Algorithms[0] != AuthAlgorithmRS256 {
		t.Fatalf("OIDCDiscovery = %#v, want detached original discovery config", jwtCfg.OIDCDiscovery)
	}
	if jwtCfg.MaxExtraClaimBytes != 12 || jwtCfg.MaxExtraClaimsBytes != 34 {
		t.Fatalf("extra claim limits = %d/%d, want 12/34", jwtCfg.MaxExtraClaimBytes, jwtCfg.MaxExtraClaimsBytes)
	}
}

func TestBuildAuthConfigStrictAcceptsVerificationKeysWithoutSigningKey(t *testing.T) {
	key := []byte("rotation-secret-0123456789abcdef01")
	cfg := Config{
		AuthMode: AuthModeStrict,
		AuthVerificationKeys: []AuthVerificationKey{
			{Algorithm: AuthAlgorithmHS256, KeyID: "active", Key: key},
		},
		AuthIssuers:   []string{"issuer"},
		AuthAudiences: []string{"app"},
	}
	jwtCfg, mintCfg, err := buildAuthConfig(cfg)
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if jwtCfg.AuthMode != auth.AuthModeStrict || mintCfg != nil {
		t.Fatalf("unexpected strict config: jwt=%+v mint=%+v", jwtCfg, mintCfg)
	}
	if len(jwtCfg.SigningKey) != 0 {
		t.Fatalf("SigningKey = %q, want empty when only verification keys configured", string(jwtCfg.SigningKey))
	}
	if len(jwtCfg.VerificationKeys) != 1 || jwtCfg.VerificationKeys[0].KeyID != "active" {
		t.Fatalf("VerificationKeys = %#v, want active key", jwtCfg.VerificationKeys)
	}

	key[0] = 'X'
	if string(jwtCfg.VerificationKeys[0].Key) == string(key) {
		t.Fatal("verification key material was not defensively copied")
	}
}

func TestBuildAuthConfigStrictAcceptsOIDCIssuersWithoutLocalKey(t *testing.T) {
	cfg := Config{
		AuthMode: AuthModeStrict,
		AuthOIDCIssuers: []AuthOIDCIssuer{{
			Issuer:     "https://issuer.example",
			JWKSURL:    "https://issuer.example/.well-known/jwks.json",
			Algorithms: []AuthAlgorithm{AuthAlgorithmRS256},
		}},
		AuthIssuers:   []string{"https://issuer.example"},
		AuthAudiences: []string{"app"},
	}
	jwtCfg, mintCfg, err := buildAuthConfig(cfg)
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if jwtCfg.AuthMode != auth.AuthModeStrict || mintCfg != nil {
		t.Fatalf("unexpected strict config: jwt=%+v mint=%+v", jwtCfg, mintCfg)
	}
	if len(jwtCfg.JWKS) != 1 || jwtCfg.JWKS[0].Issuer != "https://issuer.example" {
		t.Fatalf("JWKS = %#v, want configured issuer", jwtCfg.JWKS)
	}
}

func TestBuildAuthConfigStrictAcceptsOIDCDiscoveryIssuersWithoutLocalKey(t *testing.T) {
	cfg := Config{
		AuthMode: AuthModeStrict,
		AuthOIDCDiscoveryIssuers: []AuthOIDCDiscoveryIssuer{{
			Issuer:     "https://issuer.example",
			Algorithms: []AuthAlgorithm{AuthAlgorithmRS256},
		}},
		AuthIssuers:   []string{"https://issuer.example"},
		AuthAudiences: []string{"app"},
	}
	jwtCfg, mintCfg, err := buildAuthConfig(cfg)
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if jwtCfg.AuthMode != auth.AuthModeStrict || mintCfg != nil {
		t.Fatalf("unexpected strict config: jwt=%+v mint=%+v", jwtCfg, mintCfg)
	}
	if len(jwtCfg.OIDCDiscovery) != 1 || jwtCfg.OIDCDiscovery[0].Issuer != "https://issuer.example" {
		t.Fatalf("OIDCDiscovery = %#v, want configured discovery issuer", jwtCfg.OIDCDiscovery)
	}
	if len(jwtCfg.JWKS) != 0 {
		t.Fatalf("JWKS = %#v, want discovery to stay separate from explicit JWKS config", jwtCfg.JWKS)
	}
}

func TestBuildAuthConfigStrictDoesNotPromoteDiscoveryIssuerToIssuerPolicy(t *testing.T) {
	jwtCfg, _, err := buildAuthConfig(Config{
		AuthMode:       AuthModeStrict,
		AuthSigningKey: []byte("test-secret-0123456789abcdef012345"),
		AuthOIDCDiscoveryIssuers: []AuthOIDCDiscoveryIssuer{{
			Issuer: "https://issuer.example",
		}},
	})
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if len(jwtCfg.Issuers) != 0 {
		t.Fatalf("Issuers = %#v, want discovery config not to populate issuer policy", jwtCfg.Issuers)
	}
	if len(jwtCfg.OIDCDiscovery) != 1 || jwtCfg.OIDCDiscovery[0].Issuer != "https://issuer.example" {
		t.Fatalf("OIDCDiscovery = %#v, want configured discovery source preserved", jwtCfg.OIDCDiscovery)
	}
}

func TestBuildAuthConfigStrictRejectsInvalidVerificationKey(t *testing.T) {
	_, _, err := buildAuthConfig(Config{
		AuthMode: AuthModeStrict,
		AuthVerificationKeys: []AuthVerificationKey{
			{Algorithm: AuthAlgorithmRS256, KeyID: "bad", Key: []byte("not pem")},
		},
	})
	if !errors.Is(err, auth.ErrJWTInvalid) {
		t.Fatalf("error = %v, want auth.ErrJWTInvalid", err)
	}
}

func TestConfigFromEnvParsesOIDCIssuers(t *testing.T) {
	t.Setenv("SHUNTER_AUTH_OIDC_ISSUERS", "https://issuer.example,https://issuer.example/jwks.json; https://other.example, https://other.example/jwks")

	cfg, err := ConfigFromEnvE()
	if err != nil {
		t.Fatalf("ConfigFromEnvE returned error: %v", err)
	}
	if len(cfg.AuthOIDCIssuers) != 2 {
		t.Fatalf("AuthOIDCIssuers = %#v, want two entries", cfg.AuthOIDCIssuers)
	}
	if cfg.AuthOIDCIssuers[0].Issuer != "https://issuer.example" || cfg.AuthOIDCIssuers[0].JWKSURL != "https://issuer.example/jwks.json" {
		t.Fatalf("first AuthOIDCIssuer = %#v", cfg.AuthOIDCIssuers[0])
	}
	if cfg.AuthOIDCIssuers[1].Issuer != "https://other.example" || cfg.AuthOIDCIssuers[1].JWKSURL != "https://other.example/jwks" {
		t.Fatalf("second AuthOIDCIssuer = %#v", cfg.AuthOIDCIssuers[1])
	}
}

func TestConfigFromEnvParsesOIDCDiscoveryIssuers(t *testing.T) {
	t.Setenv("SHUNTER_AUTH_OIDC_DISCOVERY_ISSUERS", "https://issuer.example; issuer-alias, https://issuer.example/.well-known/openid-configuration")

	cfg, err := ConfigFromEnvE()
	if err != nil {
		t.Fatalf("ConfigFromEnvE returned error: %v", err)
	}
	if len(cfg.AuthOIDCDiscoveryIssuers) != 2 {
		t.Fatalf("AuthOIDCDiscoveryIssuers = %#v, want two entries", cfg.AuthOIDCDiscoveryIssuers)
	}
	if cfg.AuthOIDCDiscoveryIssuers[0].Issuer != "https://issuer.example" || cfg.AuthOIDCDiscoveryIssuers[0].DiscoveryURL != "" {
		t.Fatalf("first AuthOIDCDiscoveryIssuer = %#v, want issuer-only entry", cfg.AuthOIDCDiscoveryIssuers[0])
	}
	if cfg.AuthOIDCDiscoveryIssuers[1].Issuer != "issuer-alias" ||
		cfg.AuthOIDCDiscoveryIssuers[1].DiscoveryURL != "https://issuer.example/.well-known/openid-configuration" {
		t.Fatalf("second AuthOIDCDiscoveryIssuer = %#v, want issuer plus discovery URL", cfg.AuthOIDCDiscoveryIssuers[1])
	}
}

func TestConfigFromEnvRejectsMalformedOIDCDiscoveryIssuer(t *testing.T) {
	tests := []string{
		",https://issuer.example/.well-known/openid-configuration",
		"https://issuer.example, ",
	}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SHUNTER_AUTH_OIDC_DISCOVERY_ISSUERS", value)
			if _, err := ConfigFromEnvE(); err == nil {
				t.Fatal("ConfigFromEnvE succeeded with malformed OIDC discovery issuer entry")
			}
		})
	}
}

func TestConfigFromEnvParsesExtraClaimsAndLimits(t *testing.T) {
	t.Setenv("SHUNTER_AUTH_EXTRA_CLAIMS", " email,role, https://claims.example/session ")
	t.Setenv("SHUNTER_AUTH_MAX_EXTRA_CLAIM_BYTES", "4097")
	t.Setenv("SHUNTER_AUTH_MAX_EXTRA_CLAIMS_BYTES", "16385")

	cfg, err := ConfigFromEnvE()
	if err != nil {
		t.Fatalf("ConfigFromEnvE returned error: %v", err)
	}
	if len(cfg.AuthExtraClaims) != 3 ||
		cfg.AuthExtraClaims[0] != "email" ||
		cfg.AuthExtraClaims[1] != "role" ||
		cfg.AuthExtraClaims[2] != "https://claims.example/session" {
		t.Fatalf("AuthExtraClaims = %#v, want trimmed configured claims", cfg.AuthExtraClaims)
	}
	if cfg.AuthMaxExtraClaimBytes != 4097 || cfg.AuthMaxExtraClaimsBytes != 16385 {
		t.Fatalf("extra claim limits = %d/%d, want 4097/16385", cfg.AuthMaxExtraClaimBytes, cfg.AuthMaxExtraClaimsBytes)
	}
}

func TestConfigFromEnvRejectsInvalidExtraClaims(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "empty name", env: map[string]string{"SHUNTER_AUTH_EXTRA_CLAIMS": "email,,role"}},
		{name: "duplicate", env: map[string]string{"SHUNTER_AUTH_EXTRA_CLAIMS": "email, email"}},
		{name: "owned", env: map[string]string{"SHUNTER_AUTH_EXTRA_CLAIMS": "sub"}},
		{name: "negative per claim", env: map[string]string{"SHUNTER_AUTH_MAX_EXTRA_CLAIM_BYTES": "-1"}},
		{name: "negative total", env: map[string]string{"SHUNTER_AUTH_MAX_EXTRA_CLAIMS_BYTES": "-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			if _, err := ConfigFromEnvE(); err == nil {
				t.Fatal("ConfigFromEnvE succeeded with invalid extra claim config")
			}
		})
	}
}

func TestConfigFromEnvParsesOIDCIssuerURLWithComma(t *testing.T) {
	t.Setenv("SHUNTER_AUTH_OIDC_ISSUERS", "https://issuer.example,https://issuer.example/jwks.json?keys=a,b")

	cfg, err := ConfigFromEnvE()
	if err != nil {
		t.Fatalf("ConfigFromEnvE returned error: %v", err)
	}
	if len(cfg.AuthOIDCIssuers) != 1 {
		t.Fatalf("AuthOIDCIssuers = %#v, want one entry", cfg.AuthOIDCIssuers)
	}
	if got, want := cfg.AuthOIDCIssuers[0].JWKSURL, "https://issuer.example/jwks.json?keys=a,b"; got != want {
		t.Fatalf("JWKSURL = %q, want %q", got, want)
	}
}

func TestConfigFromEnvRejectsMalformedOIDCIssuer(t *testing.T) {
	t.Setenv("SHUNTER_AUTH_OIDC_ISSUERS", "https://issuer.example")

	if _, err := ConfigFromEnvE(); err == nil {
		t.Fatal("ConfigFromEnvE succeeded with malformed OIDC issuer entry")
	}
}

func TestRuntimeConfigDefensivelyCopiesAuthSlices(t *testing.T) {
	key := []byte("strict-runtime-secret-0123456789ab")
	verificationKey := []byte("runtime-verification-secret-012345")
	oidcAlgorithms := []AuthAlgorithm{AuthAlgorithmRS256}
	discoveryAlgorithms := []AuthAlgorithm{AuthAlgorithmES256}
	issuers := []string{"issuer"}
	audiences := []string{"app"}
	extraClaims := []string{"email"}
	cfg := Config{
		DataDir:        t.TempDir(),
		AuthMode:       AuthModeStrict,
		AuthSigningKey: key,
		AuthVerificationKeys: []AuthVerificationKey{
			{Algorithm: AuthAlgorithmHS256, KeyID: "runtime", Key: verificationKey},
		},
		AuthOIDCIssuers: []AuthOIDCIssuer{
			{Issuer: "oidc", JWKSURL: "https://oidc.example/jwks.json", Algorithms: oidcAlgorithms},
		},
		AuthOIDCDiscoveryIssuers: []AuthOIDCDiscoveryIssuer{
			{Issuer: "discovery", DiscoveryURL: "https://discovery.example/.well-known/openid-configuration", Algorithms: discoveryAlgorithms},
		},
		AuthIssuers:     issuers,
		AuthAudiences:   audiences,
		AuthExtraClaims: extraClaims,
	}

	rt, err := Build(validChatModule(), cfg)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	key[0] = 'X'
	verificationKey[0] = 'X'
	oidcAlgorithms[0] = AuthAlgorithmES256
	discoveryAlgorithms[0] = AuthAlgorithmRS256
	issuers[0] = "mutated"
	audiences[0] = "mutated"
	extraClaims[0] = "mutated"

	got := rt.Config()
	if string(got.AuthSigningKey) != "strict-runtime-secret-0123456789ab" {
		t.Fatalf("Config AuthSigningKey = %q, want original key", got.AuthSigningKey)
	}
	if len(got.AuthAudiences) != 1 || got.AuthAudiences[0] != "app" {
		t.Fatalf("Config AuthAudiences = %#v, want original audience", got.AuthAudiences)
	}
	if len(got.AuthIssuers) != 1 || got.AuthIssuers[0] != "issuer" {
		t.Fatalf("Config AuthIssuers = %#v, want original issuer", got.AuthIssuers)
	}
	if len(got.AuthVerificationKeys) != 1 || string(got.AuthVerificationKeys[0].Key) != "runtime-verification-secret-012345" {
		t.Fatalf("Config AuthVerificationKeys = %#v, want detached original verification key", got.AuthVerificationKeys)
	}
	if len(got.AuthOIDCIssuers) != 1 || got.AuthOIDCIssuers[0].Algorithms[0] != AuthAlgorithmRS256 {
		t.Fatalf("Config AuthOIDCIssuers = %#v, want detached original OIDC issuer", got.AuthOIDCIssuers)
	}
	if len(got.AuthOIDCDiscoveryIssuers) != 1 || got.AuthOIDCDiscoveryIssuers[0].Algorithms[0] != AuthAlgorithmES256 {
		t.Fatalf("Config AuthOIDCDiscoveryIssuers = %#v, want detached original OIDC discovery issuer", got.AuthOIDCDiscoveryIssuers)
	}
	if len(got.AuthExtraClaims) != 1 || got.AuthExtraClaims[0] != "email" {
		t.Fatalf("Config AuthExtraClaims = %#v, want detached original extra claim", got.AuthExtraClaims)
	}

	got.AuthSigningKey[0] = 'Y'
	got.AuthVerificationKeys[0].Key[0] = 'Y'
	got.AuthVerificationKeys[0].KeyID = "changed"
	got.AuthOIDCIssuers[0].Algorithms[0] = AuthAlgorithmES256
	got.AuthOIDCDiscoveryIssuers[0].Algorithms[0] = AuthAlgorithmRS256
	got.AuthIssuers[0] = "changed"
	got.AuthAudiences[0] = "changed"
	got.AuthExtraClaims[0] = "changed"

	again := rt.Config()
	if string(again.AuthSigningKey) != "strict-runtime-secret-0123456789ab" {
		t.Fatalf("second Config AuthSigningKey = %q, want detached original key", again.AuthSigningKey)
	}
	if len(again.AuthAudiences) != 1 || again.AuthAudiences[0] != "app" {
		t.Fatalf("second Config AuthAudiences = %#v, want detached original audience", again.AuthAudiences)
	}
	if len(again.AuthIssuers) != 1 || again.AuthIssuers[0] != "issuer" {
		t.Fatalf("second Config AuthIssuers = %#v, want detached original issuer", again.AuthIssuers)
	}
	if len(again.AuthVerificationKeys) != 1 ||
		again.AuthVerificationKeys[0].KeyID != "runtime" ||
		string(again.AuthVerificationKeys[0].Key) != "runtime-verification-secret-012345" {
		t.Fatalf("second Config AuthVerificationKeys = %#v, want detached original verification key", again.AuthVerificationKeys)
	}
	if len(again.AuthOIDCIssuers) != 1 || again.AuthOIDCIssuers[0].Algorithms[0] != AuthAlgorithmRS256 {
		t.Fatalf("second Config AuthOIDCIssuers = %#v, want detached original OIDC issuer", again.AuthOIDCIssuers)
	}
	if len(again.AuthOIDCDiscoveryIssuers) != 1 || again.AuthOIDCDiscoveryIssuers[0].Algorithms[0] != AuthAlgorithmES256 {
		t.Fatalf("second Config AuthOIDCDiscoveryIssuers = %#v, want detached original OIDC discovery issuer", again.AuthOIDCDiscoveryIssuers)
	}
	if len(again.AuthExtraClaims) != 1 || again.AuthExtraClaims[0] != "email" {
		t.Fatalf("second Config AuthExtraClaims = %#v, want detached original extra claim", again.AuthExtraClaims)
	}
}

func TestRuntimeListenAddrDefaultsWhenBlank(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if got := rt.listenAddr(); got != defaultListenAddr {
		t.Fatalf("listenAddr() = %q, want %q", got, defaultListenAddr)
	}
}

func TestRuntimeListenAddrKeepsExplicitValue(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir:    t.TempDir(),
		ListenAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if got := rt.listenAddr(); got != "127.0.0.1:0" {
		t.Fatalf("listenAddr() = %q, want explicit listen address", got)
	}
}

func TestServingHTTPServerSetsDefensiveTimeouts(t *testing.T) {
	handler := http.NotFoundHandler()
	srv := newServingHTTPServer(handler)

	if srv.Handler == nil {
		t.Fatal("server handler was not preserved")
	}
	if srv.ReadHeaderTimeout != defaultHTTPReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, defaultHTTPReadHeaderTimeout)
	}
	if srv.IdleTimeout != defaultHTTPIdleTimeout {
		t.Fatalf("IdleTimeout = %v, want %v", srv.IdleTimeout, defaultHTTPIdleTimeout)
	}
}

func TestRuntimeStartStrictAuthWithoutSigningKeyFails(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), EnableProtocol: true, AuthMode: AuthModeStrict})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	err = rt.Start(context.Background())
	if !errors.Is(err, ErrAuthSigningKeyRequired) {
		t.Fatalf("Start error = %v, want ErrAuthSigningKeyRequired", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after strict-auth startup failure")
	}
}

func TestRuntimeStartProtocolDisabledStrictAuthWithoutSigningKeySucceeds(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), AuthMode: AuthModeStrict})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start with protocol disabled returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	if rt.protocolServer != nil || rt.protocolConns != nil || rt.protocolInbox != nil {
		t.Fatal("protocol graph was initialized with EnableProtocol=false")
	}
}

func TestHTTPHandlerReturnsServiceUnavailableBeforeStart(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), EnableProtocol: true})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
	rec := httptest.NewRecorder()

	rt.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if rt.Ready() {
		t.Fatal("HTTPHandler started lifecycle; want composable handler only")
	}
}

func TestHTTPHandlerDoesNotRouteSubscribeWhenProtocolDisabled(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for protocol-disabled runtime", rec.Code)
	}
	if rt.protocolServer != nil || rt.protocolConns != nil || rt.protocolInbox != nil {
		t.Fatal("protocol graph was initialized with EnableProtocol=false")
	}
}

func TestHTTPHandlerRoutesSubscribeAfterStart(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), EnableProtocol: true})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code == http.StatusServiceUnavailable {
		t.Fatal("handler still gated after Start")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want protocol HTTP rejection 400", rec.Code)
	}
	if rt.protocolServer == nil || rt.protocolConns == nil || rt.protocolInbox == nil {
		t.Fatal("protocol graph was not initialized")
	}
}

func TestHTTPHandlerReturnsServiceUnavailableAfterClose(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), EnableProtocol: true})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestListenAndServeStartsRuntimeAndStopsOnContextCancel(t *testing.T) {
	rt := buildValidTestRuntime(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- rt.serve(ctx, ln) }()

	eventually(t, func() bool { return rt.Ready() })
	cancel()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit after context cancellation")
	}
	if rt.Ready() {
		t.Fatal("runtime still ready after serve cancellation")
	}
}

func TestListenAndServeDuplicateCallReturnsRuntimeServing(t *testing.T) {
	addr := reserveRuntimeListenAddr(t)
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), ListenAddr: addr})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- rt.ListenAndServe(ctx) }()

	eventually(t, func() bool {
		rt.mu.Lock()
		serving := rt.serving
		rt.mu.Unlock()
		return serving
	})

	err = rt.ListenAndServe(context.Background())
	if !errors.Is(err, ErrRuntimeServing) {
		t.Fatalf("duplicate ListenAndServe error = %v, want ErrRuntimeServing", err)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("first ListenAndServe returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first ListenAndServe did not exit after cancellation")
	}
}

func TestListenAndServeAfterClosePreservesClosedError(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	err = rt.serve(context.Background(), ln)
	if !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("serve after Close error = %v, want ErrRuntimeClosed", err)
	}
}

func TestListenAndServeRejectsNULListenAddrBeforeNetListen(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir:    t.TempDir(),
		ListenAddr: "127.0.0.1:0\x00",
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	err = rt.ListenAndServe(context.Background())
	if err == nil {
		t.Fatal("ListenAndServe succeeded with NUL listen address")
	}
	assertErrorMentions(t, err, "NUL")
	rt.mu.Lock()
	serving := rt.serving
	rt.mu.Unlock()
	if serving {
		t.Fatal("runtime serving flag remained set after listen-address rejection")
	}
	if rt.Ready() {
		t.Fatal("runtime started after listen-address rejection")
	}
}

func TestRuntimeNetworkReplacesNoopFanOutSender(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), EnableProtocol: true})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	sender, ok := rt.fanOutSender.(*swappableFanOutSender)
	if !ok {
		t.Fatalf("fanOutSender = %T, want *swappableFanOutSender", rt.fanOutSender)
	}
	if _, ok := sender.Target().(noopFanOutSender); ok {
		t.Fatal("fan-out sender still points at noop after Start/protocol wiring")
	}
	if _, ok := sender.Target().(*protocol.FanOutSenderAdapter); !ok {
		t.Fatalf("fan-out sender target = %T, want protocol-backed adapter", sender.Target())
	}
}

func TestRuntimeProtocolDisabledKeepsNoopFanOutSender(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	sender, ok := rt.fanOutSender.(*swappableFanOutSender)
	if !ok {
		t.Fatalf("fanOutSender = %T, want *swappableFanOutSender", rt.fanOutSender)
	}
	if _, ok := sender.Target().(noopFanOutSender); !ok {
		t.Fatalf("fan-out sender target = %T, want noop when protocol is disabled", sender.Target())
	}
}

func TestRuntimeCloseClearsProtocolGraphBeforeExecutorResources(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), EnableProtocol: true})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if rt.protocolConns == nil || rt.protocolInbox == nil || rt.executor == nil {
		t.Fatal("protocol/executor resources missing before Close")
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if rt.protocolConns != nil || rt.protocolInbox != nil || rt.protocolServer != nil || rt.protocolSender != nil || rt.fanOutSender != nil {
		t.Fatalf("protocol resources not cleared after Close")
	}
	if rt.executor != nil || rt.durability != nil || rt.scheduler != nil {
		t.Fatalf("lifecycle resources not cleared after Close")
	}
}

func reserveRuntimeListenAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-timeout.C:
			t.Fatal("condition was not met before deadline")
		case <-ticker.C:
		}
	}
}
