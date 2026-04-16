package protocol

import (
	"time"

	"github.com/coder/websocket"
)

// Close codes used by the server (RFC 6455 + SPEC-005 §11.1).
const (
	CloseNormal   = websocket.StatusNormalClosure  // 1000: graceful shutdown
	CloseProtocol = websocket.StatusProtocolError   // 1002: unknown tag, malformed
	ClosePolicy   = websocket.StatusPolicyViolation // 1008: auth, buffer overflow, flood
	CloseInternal = websocket.StatusInternalError   // 1011: unexpected server error
)

// closeWithHandshake sends a Close frame and waits up to timeout for
// the peer's echo. If the peer does not respond in time the caller
// returns immediately; the coder/websocket internal close handshake
// (up to 10 s) continues in the background and will tear down the TCP
// connection on its own.
//
// Runs synchronously — callers that cannot block should invoke in a
// goroutine.
//
// Design note: coder/websocket.Conn.Close uses a one-shot CAS gate
// that prevents a concurrent CloseNow from interrupting it. The only
// way to bound the wait from outside is to let Close run in a
// background goroutine and select on a timeout. This matches the
// fire-and-forget pattern already used in Disconnect and keepalive
// idle-close paths.
func closeWithHandshake(ws *websocket.Conn, code websocket.StatusCode, reason string, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		_ = ws.Close(code, truncateCloseReason(reason))
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}
