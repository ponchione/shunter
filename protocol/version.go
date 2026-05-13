package protocol

import "fmt"

// ProtocolVersion identifies a negotiated Shunter client protocol version.
type ProtocolVersion uint16

const (
	// ProtocolVersionUnknown is never negotiated; it is the zero value for
	// missing or invalid version data.
	ProtocolVersionUnknown ProtocolVersion = 0
	// ProtocolVersionV1 is the original Shunter-native BSATN protocol and the
	// minimum version still accepted for v1 clients.
	ProtocolVersionV1 ProtocolVersion = 1
	// ProtocolVersionV2 adds parameterized declared-read request messages.
	ProtocolVersionV2 ProtocolVersion = 2
)

const (
	// MinSupportedProtocolVersion is the oldest protocol version this server
	// accepts.
	MinSupportedProtocolVersion = ProtocolVersionV1
	// CurrentProtocolVersion is the newest protocol version this server emits.
	CurrentProtocolVersion = ProtocolVersionV2
)

// SubprotocolV1 is the Shunter-native WebSocket subprotocol token,
// and remains supported for v1 clients.
const SubprotocolV1 = "v1.bsatn.shunter"

// SubprotocolV2 is the Shunter-native WebSocket subprotocol token for
// parameterized declared-read request messages.
const SubprotocolV2 = "v2.bsatn.shunter"

var protocolSubprotocols = map[ProtocolVersion]string{
	ProtocolVersionV1: SubprotocolV1,
	ProtocolVersionV2: SubprotocolV2,
}

var protocolVersionsBySubprotocol = map[string]ProtocolVersion{
	SubprotocolV1: ProtocolVersionV1,
	SubprotocolV2: ProtocolVersionV2,
}

// SupportedProtocolVersions returns the supported protocol versions in server
// preference order.
func SupportedProtocolVersions() []ProtocolVersion {
	return []ProtocolVersion{ProtocolVersionV2, ProtocolVersionV1}
}

// SupportedSubprotocols returns the WebSocket subprotocol tokens accepted by
// the server, in the order used for negotiation.
func SupportedSubprotocols() []string {
	versions := SupportedProtocolVersions()
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		if token, ok := SubprotocolForVersion(version); ok {
			out = append(out, token)
		}
	}
	return out
}

// SubprotocolForVersion returns the WebSocket token for a supported protocol
// version.
func SubprotocolForVersion(version ProtocolVersion) (string, bool) {
	token, ok := protocolSubprotocols[version]
	return token, ok
}

// ProtocolVersionForSubprotocol returns the protocol version represented by a
// supported WebSocket subprotocol token.
func ProtocolVersionForSubprotocol(token string) (ProtocolVersion, bool) {
	version, ok := protocolVersionsBySubprotocol[token]
	return version, ok
}

func (v ProtocolVersion) String() string {
	switch v {
	case ProtocolVersionUnknown:
		return "unknown"
	case ProtocolVersionV1:
		return "v1"
	case ProtocolVersionV2:
		return "v2"
	default:
		return fmt.Sprintf("v%d", uint16(v))
	}
}
