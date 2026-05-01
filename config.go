package shunter

import "time"

// AuthMode selects the root runtime authentication behavior.
type AuthMode int

const (
	// AuthModeDev is the default development authentication mode.
	AuthModeDev AuthMode = iota
	// AuthModeStrict requires explicit production authentication configuration in
	// later hosted-runtime slices.
	AuthModeStrict
)

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

	AuthSigningKey []byte
	AuthAudiences  []string

	AnonymousTokenIssuer   string
	AnonymousTokenAudience string
	AnonymousTokenTTL      time.Duration

	Protocol      ProtocolConfig
	Observability ObservabilityConfig
}

// ProtocolConfig exposes narrow top-level WebSocket protocol tuning. Zero
// values use protocol package defaults.
type ProtocolConfig struct {
	PingInterval           time.Duration
	IdleTimeout            time.Duration
	CloseHandshakeTimeout  time.Duration
	DisconnectTimeout      time.Duration
	OutgoingBufferMessages int
	IncomingQueueMessages  int
	MaxMessageSize         int64
}
