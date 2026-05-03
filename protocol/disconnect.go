package protocol

import (
	"context"

	"github.com/coder/websocket"
)

// Disconnect tears down a connection once: drop subscriptions, run
// OnDisconnect, remove the Conn, signal local goroutines, and close the socket.
func (c *Conn) Disconnect(ctx context.Context, code websocket.StatusCode, reason string, inbox ExecutorInbox, mgr *ConnManager) {
	c.disconnectStarted.Store(true)
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

// superviseLifecycle disconnects when dispatch, keepalive, or outbound exits.
// The disconnect context is bounded so a hung inbox cannot stall local cleanup.
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
