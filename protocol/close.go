package protocol

import (
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

// closeWithHandshake starts a WebSocket Close handshake and waits up to
// timeout for the peer's echo. If the peer does not respond in time the
// caller returns immediately, but the underlying coder/websocket Close
// call continues in the background and may still block for its own
// internal handshake window.
//
// Runs synchronously — callers that cannot block should invoke in a
// goroutine.
//
// IMPORTANT LIMITATION:
// Story 6.3 wants "send Close, then force-close TCP after
// CloseHandshakeTimeout if no echo arrives." coder/websocket v1.8.14
// does not expose a public API that can enforce that exactly. A live
// experiment showed that calling Conn.CloseNow after Conn.Close has
// started does NOT preempt the in-flight Close; both calls still wait
// for the library's internal close path to finish.
//
// So this helper only guarantees a bounded wait for Shunter's own
// control flow. It does NOT guarantee immediate transport teardown at
// timeout. Callers should treat it as "best-effort close initiation,
// then return" rather than a true hard-close timeout implementation.
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
