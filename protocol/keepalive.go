package protocol

import (
	"context"
	"time"
)

// runKeepalive is the per-connection keep-alive loop defined by
// SPEC-005 §5.4 (Story 3.5). Every PingInterval it sends a best-effort
// WebSocket Ping to the peer and samples lastActivity; when no
// inbound signal has been observed within IdleTimeout it closes the
// connection with StatusPolicyViolation.
//
// Activity sources:
//   - Successful Ping→Pong round-trip: MarkActivity is called when
//     coder/websocket's blocking Ping returns nil. Ping return
//     requires the SAME connection's read side to have consumed the
//     Pong control frame; runDispatchLoop drives that read side while
//     preserving the same activity contract.
//   - Any frame observed by runDispatchLoop. Every successful Read marks
//     activity, so active data traffic keeps the connection alive
//     even when Pongs are delayed.
//
// The loop exits when ctx is cancelled, c.closed is closed, or the
// idle threshold is crossed. The shared c.closed signal is the link
// to the disconnect pipeline (Story 3.6): any path that triggers a
// disconnect closes c.closed, which unblocks every goroutine owned by
// this connection — including this keep-alive loop.
func (c *Conn) runKeepalive(ctx context.Context) {
	pingTicker := time.NewTicker(c.opts.PingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case <-pingTicker.C:
			// Cap the Ping at PingInterval so a stuck peer cannot
			// delay the next idle check by more than one tick.
			// coder/websocket.Conn.Ping blocks until the matching
			// Pong arrives (or ctx fires); a returned nil means the
			// peer is alive, so credit it as activity.
			pingCtx, cancel := context.WithTimeout(ctx, c.opts.PingInterval)
			if err := c.ws.Ping(pingCtx); err == nil {
				c.MarkActivity()
			}
			cancel()

			// Re-check ctx / closed after the blocking Ping in case a
			// disconnect fired while we were waiting for a Pong. This
			// avoids sending a redundant close on an already-closing
			// socket.
			select {
			case <-ctx.Done():
				return
			case <-c.closed:
				return
			default:
			}

			// Use a fresh wall-clock sample: the ticker's timestamp
			// is stale after the blocking Ping, which would under-
			// count the elapsed-idle window.
			last := c.lastActivity.Load()
			if time.Now().UnixNano()-last >= int64(c.opts.IdleTimeout) {
				// coder/websocket.Conn.Close performs a close
				// handshake with a hard-coded 5 s internal wait. On
				// the idle-timeout path the peer is already silent,
				// so the handshake is never answered. Running Close
				// in a dedicated goroutine lets the close frame go
				// out while the keep-alive loop exits promptly; the
				// background goroutine unwinds when the internal
				// timeout fires or when the caller tears down the
				// socket.
				go closeWithHandshake(c.ws, ClosePolicy, "idle timeout", c.opts.CloseHandshakeTimeout)
				return
			}
		}
	}
}
