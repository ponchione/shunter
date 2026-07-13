package shunter

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ponchione/shunter/auth"
)

// AuthMode selects the root runtime authentication behavior.
type AuthMode int

const (
	// AuthModeDev is the default development authentication mode.
	AuthModeDev AuthMode = iota
	// AuthModeStrict requires explicit production authentication configuration.
	AuthModeStrict
)

// AuthAlgorithm identifies a JWT signing algorithm accepted by Shunter's local
// verification path.
type AuthAlgorithm = auth.JWTAlgorithm

const (
	AuthAlgorithmHS256 AuthAlgorithm = auth.JWTAlgorithmHS256
	AuthAlgorithmRS256 AuthAlgorithm = auth.JWTAlgorithmRS256
	AuthAlgorithmES256 AuthAlgorithm = auth.JWTAlgorithmES256
)

// AuthVerificationKey is one locally configured JWT verification key. Key is
// an HMAC secret of at least 32 bytes for HS256 and a PEM-encoded public key or
// certificate for RS256/ES256. KeyID optionally matches the token header's
// `kid` value.
type AuthVerificationKey = auth.JWTVerificationKey

// AuthOIDCIssuer configures remote JWKS verification for one trusted OIDC/JWT
// issuer. Issuer and audience claims are still enforced by AuthIssuers and
// AuthAudiences.
type AuthOIDCIssuer = auth.JWKSConfig

// AuthOIDCDiscoveryIssuer configures OIDC discovery-document lookup for one
// issuer. Discovery resolves to a JWKS key source; issuer and audience claims
// are still enforced by AuthIssuers and AuthAudiences.
type AuthOIDCDiscoveryIssuer = auth.OIDCDiscoveryConfig

// Config contains hosted-runtime build, startup, protocol, and authentication
// options. Zero values keep the local/dev path easy to boot unless a serving or
// strict-auth path requires additional fields.
type Config struct {
	DataDir                 string
	ExecutorQueueCapacity   int
	DurabilityQueueCapacity int
	EnableProtocol          bool
	ListenAddr              string
	AuthMode                AuthMode

	// AuthSigningKey is the legacy HS256 signing/verification secret. Non-empty
	// keys must be at least 32 bytes.
	AuthSigningKey           []byte
	AuthVerificationKeys     []AuthVerificationKey
	AuthOIDCIssuers          []AuthOIDCIssuer
	AuthOIDCDiscoveryIssuers []AuthOIDCDiscoveryIssuer
	AuthIssuers              []string
	AuthAudiences            []string
	AuthExtraClaims          []string
	AuthMaxExtraClaimBytes   int
	AuthMaxExtraClaimsBytes  int

	AnonymousTokenIssuer   string
	AnonymousTokenAudience string
	AnonymousTokenTTL      time.Duration

	// OneOffQueryMaxRows caps rows returned by hosted raw and declared queries.
	// Zero uses 100,000 rows.
	OneOffQueryMaxRows int
	// OneOffQueryMaxBytes caps the encoded RowList bytes returned by hosted raw
	// and declared queries. Zero uses 64 MiB.
	OneOffQueryMaxBytes int
	// SubscriptionInitialRowLimit caps rows evaluated for an initial or final
	// subscription snapshot. Zero uses 100,000 rows.
	SubscriptionInitialRowLimit int
	// SubscriptionMaxMultiJoinRelations caps live multi-way join relation
	// count. Zero leaves the current unlimited behavior.
	SubscriptionMaxMultiJoinRelations int
	// SubscriptionMaxMultiJoinRowsPerRelation caps committed input rows per
	// relation for live multi-way joins. Zero leaves the current unlimited
	// behavior.
	SubscriptionMaxMultiJoinRowsPerRelation int

	Protocol      ProtocolConfig
	Observability ObservabilityConfig
}

// ConfigFromEnv returns a hosted-app Config populated from Shunter-scoped
// environment variables. Blank or unset variables leave the same local
// development defaults used by the runtime.
//
// Supported variables:
//   - SHUNTER_DATA_DIR
//   - SHUNTER_LISTEN_ADDR
//   - SHUNTER_ENABLE_PROTOCOL
//   - SHUNTER_AUTH_MODE: dev or strict
//   - SHUNTER_AUTH_SIGNING_KEY
//   - SHUNTER_AUTH_ISSUERS: comma-separated
//   - SHUNTER_AUTH_AUDIENCES: comma-separated
//   - SHUNTER_AUTH_OIDC_ISSUERS: semicolon-separated issuer,jwks-url pairs
//   - SHUNTER_AUTH_OIDC_DISCOVERY_ISSUERS: semicolon-separated issuer or issuer,discovery-url entries
//   - SHUNTER_AUTH_EXTRA_CLAIMS: comma-separated extra claim names
//   - SHUNTER_AUTH_MAX_EXTRA_CLAIM_BYTES
//   - SHUNTER_AUTH_MAX_EXTRA_CLAIMS_BYTES
func ConfigFromEnv() Config {
	cfg, err := ConfigFromEnvE()
	if err != nil {
		panic(err)
	}
	return cfg
}

// ConfigFromEnvE is the error-returning form of ConfigFromEnv.
func ConfigFromEnvE() (Config, error) {
	var cfg Config
	cfg.DataDir = strings.TrimSpace(os.Getenv("SHUNTER_DATA_DIR"))
	cfg.ListenAddr = strings.TrimSpace(os.Getenv("SHUNTER_LISTEN_ADDR"))
	if value := strings.TrimSpace(os.Getenv("SHUNTER_ENABLE_PROTOCOL")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("SHUNTER_ENABLE_PROTOCOL: %w", err)
		}
		cfg.EnableProtocol = enabled
	}
	if value := strings.TrimSpace(os.Getenv("SHUNTER_AUTH_MODE")); value != "" {
		switch strings.ToLower(value) {
		case "dev", "development":
			cfg.AuthMode = AuthModeDev
		case "strict":
			cfg.AuthMode = AuthModeStrict
		default:
			return Config{}, fmt.Errorf("SHUNTER_AUTH_MODE: unsupported auth mode %q", value)
		}
	}
	if value := os.Getenv("SHUNTER_AUTH_SIGNING_KEY"); value != "" {
		cfg.AuthSigningKey = []byte(value)
	}
	cfg.AuthIssuers = splitEnvList(os.Getenv("SHUNTER_AUTH_ISSUERS"))
	cfg.AuthAudiences = splitEnvList(os.Getenv("SHUNTER_AUTH_AUDIENCES"))
	issuers, err := parseOIDCIssuerEnv(os.Getenv("SHUNTER_AUTH_OIDC_ISSUERS"))
	if err != nil {
		return Config{}, fmt.Errorf("SHUNTER_AUTH_OIDC_ISSUERS: %w", err)
	}
	cfg.AuthOIDCIssuers = issuers
	discoveryIssuers, err := parseOIDCDiscoveryIssuerEnv(os.Getenv("SHUNTER_AUTH_OIDC_DISCOVERY_ISSUERS"))
	if err != nil {
		return Config{}, fmt.Errorf("SHUNTER_AUTH_OIDC_DISCOVERY_ISSUERS: %w", err)
	}
	cfg.AuthOIDCDiscoveryIssuers = discoveryIssuers
	cfg.AuthExtraClaims = splitEnvListPreserveEmpty(os.Getenv("SHUNTER_AUTH_EXTRA_CLAIMS"))
	if cfg.AuthMaxExtraClaimBytes, err = parseOptionalIntEnv("SHUNTER_AUTH_MAX_EXTRA_CLAIM_BYTES"); err != nil {
		return Config{}, err
	}
	if cfg.AuthMaxExtraClaimsBytes, err = parseOptionalIntEnv("SHUNTER_AUTH_MAX_EXTRA_CLAIMS_BYTES"); err != nil {
		return Config{}, err
	}
	if err := auth.ValidateJWTExtraClaimsConfig(&auth.JWTConfig{
		ExtraClaims:         cfg.AuthExtraClaims,
		MaxExtraClaimBytes:  cfg.AuthMaxExtraClaimBytes,
		MaxExtraClaimsBytes: cfg.AuthMaxExtraClaimsBytes,
	}); err != nil {
		return Config{}, fmt.Errorf("SHUNTER_AUTH_EXTRA_CLAIMS: %w", err)
	}
	return cfg, nil
}

func splitEnvList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitEnvListPreserveEmpty(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = strings.TrimSpace(part)
	}
	return out
}

func parseOptionalIntEnv(name string) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

func parseOIDCIssuerEnv(value string) ([]AuthOIDCIssuer, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	entries := strings.Split(value, ";")
	out := make([]AuthOIDCIssuer, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ",", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("entry %q must be issuer,jwks-url", entry)
		}
		issuer := strings.TrimSpace(parts[0])
		jwksURL := strings.TrimSpace(parts[1])
		if issuer == "" || jwksURL == "" {
			return nil, fmt.Errorf("entry %q must include issuer and jwks-url", entry)
		}
		out = append(out, AuthOIDCIssuer{Issuer: issuer, JWKSURL: jwksURL})
	}
	return out, nil
}

func parseOIDCDiscoveryIssuerEnv(value string) ([]AuthOIDCDiscoveryIssuer, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	entries := strings.Split(value, ";")
	out := make([]AuthOIDCDiscoveryIssuer, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ",", 2)
		issuer := strings.TrimSpace(parts[0])
		if issuer == "" {
			return nil, fmt.Errorf("entry %q must include issuer", entry)
		}
		var discoveryURL string
		if len(parts) == 2 {
			discoveryURL = strings.TrimSpace(parts[1])
			if discoveryURL == "" {
				return nil, fmt.Errorf("entry %q must include discovery-url when a comma is present", entry)
			}
		}
		out = append(out, AuthOIDCDiscoveryIssuer{Issuer: issuer, DiscoveryURL: discoveryURL})
	}
	return out, nil
}

// ProtocolConfig exposes narrow top-level WebSocket protocol tuning. Zero
// values use protocol package defaults.
type ProtocolConfig struct {
	PingInterval           time.Duration
	IdleTimeout            time.Duration
	CloseHandshakeTimeout  time.Duration
	WriteTimeout           time.Duration
	DisconnectTimeout      time.Duration
	OutgoingBufferMessages int
	IncomingQueueMessages  int
	MaxMessageSize         int64
}
