package protocol

import (
	"context"

	"github.com/coder/websocket"
)

// Disconnect runs the SPEC-005 §5.3 / §11 teardown sequence exactly
// once for this connection, even if it is invoked from multiple
// goroutines (read-loop error, keep-alive idle timeout, engine
// shutdown). Idempotency is enforced by Conn.closeOnce.
//
// Ordering is spec-mandated (Story 3.6 AC + §5.3):
//
//  1. DisconnectClientSubscriptions — remove every subscription for
//     this connection BEFORE the reducer runs, so OnDisconnect sees
//     the connection with no active subscriptions.
//  2. OnDisconnect — executor-side lifecycle reducer + sys_clients
//     cleanup. Errors are logged and do NOT veto the remaining
//     teardown (disconnect cannot be vetoed — SPEC-003 §10.4).
//  3. ConnManager.Remove — drop the ConnectionID pointer so no
//     further fan-out resolution finds this connection.
//  4. close(c.closed) — signals runDispatchLoop, runKeepalive, sender
//     paths, and the outbound writer to exit. OutboundCh is not closed;
//     c.closed is the lifecycle signal.
//  5. c.ws.Close — drives the WebSocket Close handshake. Fired in a
//     background goroutine because coder/websocket.Conn.Close has a
//     hard-coded 5 s internal handshake wait; blocking the caller
//     for that long would serialise disconnects unnecessarily.
//
// DisconnectClientSubscriptions and OnDisconnect are dispatched with
// the caller's ctx so engine shutdown can bound the teardown window.
func (c *Conn) Disconnect(ctx context.Context, code websocket.StatusCode, reason string, inbox ExecutorInbox, mgr *ConnManager) {
	c.closeOnce.Do(func() {
		if err := inbox.DisconnectClientSubscriptions(ctx, c.ID); err != nil {
			logProtocolError(c.Observer, "unknown", "disconnect_failed", err)
		}
		if err := inbox.OnDisconnect(ctx, c.ID, c.Identity); err != nil {
			logProtocolError(c.Observer, "unknown", "disconnect_failed", err)
		}
		mgr.Remove(c.ID)
		recordProtocolConnections(c.Observer, mgr.ActiveCount())
		if c.cancelRead != nil {
			c.cancelRead()
		}
		close(c.closed)
		if c.ws != nil {
			go closeWithHandshake(c.ws, code, reason, c.opts.CloseHandshakeTimeout)
		}
		if c.Observer != nil {
			c.Observer.LogProtocolConnectionClosed(c.ID, closeReason(code, reason))
		}
	})
}

// superviseLifecycle watches runDispatchLoop + runKeepalive and invokes
// Disconnect exactly once when the first of them exits. Used by the
// default Upgraded handler (HandleSubscribe) to convert a goroutine
// exit into the full SPEC-005 §5.3 teardown.
//
// The supervisor runs in its own goroutine; RunLifecycle + the
// HTTP handler have already returned by the time it fires.
// Disconnect's closeOnce guarantees that a second exit signal is
// a no-op, so the supervisor can safely wait for both goroutines
// before returning.
//
// contract: the incoming ctx is hardcoded to context.Background() at the only
// production call site (upgrade.go HandleSubscribe), so the
// inbox.DisconnectClientSubscriptions / inbox.OnDisconnect calls
// threaded through c.Disconnect would be non-cancellable if the
// executor dispatch or inbox drain hung. The supervisor now
// derives a bounded ctx from c.opts.DisconnectTimeout (default 5 s)
// and defers its cancel; a hung inbox returns ctx.Err() after the
// timeout and Disconnect proceeds to steps 3-5 of the SPEC-005
// §5.3 teardown unconditionally. Pin test:
// TestSuperviseLifecycleBoundsDisconnectOnInboxHang.
func (c *Conn) superviseLifecycle(
	ctx context.Context,
	code websocket.StatusCode,
	reason string,
	inbox ExecutorInbox,
	mgr *ConnManager,
	dispatchDone <-chan struct{},
	keepaliveDone <-chan struct{},
	outboundDone <-chan struct{},
) {
	select {
	case <-dispatchDone:
	case <-keepaliveDone:
	case <-outboundDone:
	}
	disconnectCtx, cancel := context.WithTimeout(ctx, c.opts.DisconnectTimeout)
	defer cancel()
	c.Disconnect(disconnectCtx, code, reason, inbox, mgr)
	<-dispatchDone
	<-keepaliveDone
	<-outboundDone
}
