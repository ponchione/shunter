package protocol

import (
	"context"
	"time"

	"github.com/coder/websocket"
)

// Close codes used by the server (RFC 6455 + SPEC-005 §11.1).
const (
	CloseNormal   = websocket.StatusNormalClosure   // 1000: graceful shutdown
	CloseProtocol = websocket.StatusProtocolError   // 1002: unknown tag, malformed
	ClosePolicy   = websocket.StatusPolicyViolation // 1008: auth, buffer overflow, flood
	CloseInternal = websocket.StatusInternalError   // 1011: unexpected server error
)

// Wire close reason strings used by Shunter v1. Keep these short enough for
// WebSocket close frames and stable enough for clients to classify failures.
const (
	CloseReasonTextFrameUnsupported = "text frames not supported"
	CloseReasonBrotliUnsupported    = "brotli unsupported"
	CloseReasonMalformedMessage     = "malformed message"
	CloseReasonUnsupportedMessage   = "unsupported message type"
	CloseReasonMessageTooLarge      = "message too large"
	CloseReasonTooManyRequests      = "too many requests"
	CloseReasonSendBufferFull       = "send buffer full"
	CloseReasonIdleTimeout          = "idle timeout"
	CloseReasonServerShutdown       = "server shutdown"
)

// closeWithHandshake runs a bounded WebSocket close handshake.
// On return the connection is no longer usable.
func closeWithHandshake(ws *websocket.Conn, code websocket.StatusCode, reason string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = ws.CloseWithContext(ctx, code, truncateCloseReason(reason))
}
