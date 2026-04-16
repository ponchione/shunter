package protocol

import (
	"context"
	"log"

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
//  4. close(c.closed) — signals runDispatchLoop, runKeepalive, and the
//     Epic 4 write loop to exit. OutboundCh is closed alongside so
//     any writer goroutine blocked on a send unblocks cleanly.
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
			log.Printf("protocol: DisconnectClientSubscriptions for %x failed: %v", c.ID[:], err)
		}
		if err := inbox.OnDisconnect(ctx, c.ID, c.Identity); err != nil {
			log.Printf("protocol: OnDisconnect for %x failed: %v", c.ID[:], err)
		}
		mgr.Remove(c.ID)
		close(c.OutboundCh)
		close(c.closed)
		if c.ws != nil {
			go func() {
				_ = c.ws.Close(code, truncateCloseReason(reason))
			}()
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
func (c *Conn) superviseLifecycle(
	ctx context.Context,
	inbox ExecutorInbox,
	mgr *ConnManager,
	dispatchDone <-chan struct{},
	keepaliveDone <-chan struct{},
) {
	select {
	case <-dispatchDone:
	case <-keepaliveDone:
	}
	c.Disconnect(ctx, websocket.StatusNormalClosure, "", inbox, mgr)
	<-dispatchDone
	<-keepaliveDone
}
