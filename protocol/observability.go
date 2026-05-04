package protocol

import (
	"errors"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/types"
)

// Observer receives runtime-scoped protocol observations. Nil means no-op for
// package-level tests and custom servers that do not run under a Runtime.
type Observer interface {
	RecordProtocolConnections(active int)
	RecordProtocolMessage(kind, result string)
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

func recordProtocolConnections(observer Observer, active int) {
	if observer != nil {
		observer.RecordProtocolConnections(active)
	}
}

func recordProtocolMessage(observer Observer, kind, result string) {
	if observer != nil {
		observer.RecordProtocolMessage(kind, result)
	}
}

func logProtocolConnectionOpened(observer Observer, connID types.ConnectionID) {
	if observer != nil {
		observer.LogProtocolConnectionOpened(connID)
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

func protocolKindFromTag(tag uint8) string {
	switch tag {
	case TagSubscribeSingle:
		return "subscribe_single"
	case TagUnsubscribeSingle:
		return "unsubscribe_single"
	case TagCallReducer:
		return "call_reducer"
	case TagOneOffQuery:
		return "one_off_query"
	case TagSubscribeMulti:
		return "subscribe_multi"
	case TagUnsubscribeMulti:
		return "unsubscribe_multi"
	case TagDeclaredQuery:
		return "declared_query"
	case TagSubscribeDeclaredView:
		return "subscribe_declared_view"
	default:
		return "unknown"
	}
}

func protocolKindFromMessage(msg any) string {
	switch msg.(type) {
	case SubscribeSingleMsg:
		return "subscribe_single"
	case SubscribeMultiMsg:
		return "subscribe_multi"
	case SubscribeDeclaredViewMsg:
		return "subscribe_declared_view"
	case UnsubscribeSingleMsg:
		return "unsubscribe_single"
	case UnsubscribeMultiMsg:
		return "unsubscribe_multi"
	case CallReducerMsg:
		return "call_reducer"
	case OneOffQueryMsg:
		return "one_off_query"
	case DeclaredQueryMsg:
		return "declared_query"
	default:
		return "unknown"
	}
}

func protocolErrorReason(message string) string {
	switch message {
	case CloseReasonTextFrameUnsupported:
		return "text_frame"
	case CloseReasonBrotliUnsupported:
		return "brotli_unsupported"
	case CloseReasonUnsupportedMessage:
		return "unsupported_message"
	case CloseReasonTooManyRequests:
		return "buffer_full"
	default:
		return "malformed"
	}
}

func closeReason(code websocket.StatusCode, reason string) string {
	switch {
	case reason == CloseReasonSendBufferFull:
		return "buffer_full"
	case reason == CloseReasonServerShutdown:
		return "server_shutdown"
	case reason == CloseReasonIdleTimeout:
		return "idle_timeout"
	case reason == "":
		return "normal"
	case code == CloseProtocol:
		return "protocol_error"
	case code == ClosePolicy:
		return "policy_violation"
	case code == CloseInternal:
		return "internal_error"
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
