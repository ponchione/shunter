package protocol

import (
	"errors"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/types"
)

// Observer receives runtime-scoped protocol observations. Nil means no-op for
// package-level tests and custom servers that do not run under a Runtime.
type Observer interface {
	LogProtocolConnectionRejected(result string, err error)
	LogProtocolConnectionOpened(connID types.ConnectionID)
	LogProtocolConnectionClosed(connID types.ConnectionID, reason string)
	LogProtocolProtocolError(kind, reason string, err error)
	LogProtocolAuthFailed(reason string, err error)
	LogProtocolBackpressure(direction, reason string)
}

func logProtocolConnectionRejected(observer Observer, result string, err error) {
	if observer != nil {
		observer.LogProtocolConnectionRejected(result, err)
	}
}

func logProtocolError(observer Observer, kind, reason string, err error) {
	if observer != nil && err != nil {
		observer.LogProtocolProtocolError(kind, reason, err)
	}
}

func logProtocolBackpressure(observer Observer, direction, reason string) {
	if observer != nil {
		observer.LogProtocolBackpressure(direction, reason)
	}
}

func protocolErrorReason(message string) string {
	switch message {
	case "text frames not supported":
		return "text_frame"
	case "brotli unsupported":
		return "brotli_unsupported"
	case "unsupported message type":
		return "unsupported_message"
	case "too many requests":
		return "buffer_full"
	default:
		return "malformed"
	}
}

func closeReason(code websocket.StatusCode, reason string) string {
	switch {
	case reason == "send buffer full":
		return "buffer_full"
	case reason == "server shutdown":
		return "server_shutdown"
	case reason == "":
		return "normal"
	case code == CloseProtocol:
		return "protocol_error"
	case code == ClosePolicy:
		return "policy_violation"
	case code == CloseNormal:
		return "normal"
	default:
		return "unknown"
	}
}

func errorFromText(text string) error {
	if text == "" {
		return nil
	}
	return errors.New(text)
}
