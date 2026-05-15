package shunter

import (
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
// an HMAC secret for HS256 and a PEM-encoded public key or certificate for
// RS256/ES256. KeyID optionally matches the token header's `kid` value.
type AuthVerificationKey = auth.JWTVerificationKey

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

	AuthSigningKey       []byte
	AuthVerificationKeys []AuthVerificationKey
	AuthIssuers          []string
	AuthAudiences        []string

	AnonymousTokenIssuer   string
	AnonymousTokenAudience string
	AnonymousTokenTTL      time.Duration

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
