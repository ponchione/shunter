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

// closeWithHandshake runs a bounded WebSocket close handshake.
// On return the connection is no longer usable.
func closeWithHandshake(ws *websocket.Conn, code websocket.StatusCode, reason string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = ws.CloseWithContext(ctx, code, truncateCloseReason(reason))
}
