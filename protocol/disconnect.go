package protocol

import (
	"context"
	"time"

	"github.com/ponchione/websocket"
)

// Disconnect tears down a connection once: drop subscriptions, run
// OnDisconnect, remove the Conn, signal local goroutines, and close the socket.
func (c *Conn) Disconnect(ctx context.Context, code websocket.StatusCode, reason string, inbox ExecutorInbox, mgr *ConnManager) {
	if c == nil {
		return
	}
	c.disconnectStarted.Store(true)
	c.closeOnce.Do(func() {
		if inbox != nil {
			if err := inbox.DisconnectClientSubscriptions(ctx, c.ID); err != nil {
				logProtocolError(c.Observer, "unknown", "disconnect_failed", err)
			}
			if err := inbox.OnDisconnect(ctx, c.ID, c.Identity, c.Principal.Copy()); err != nil {
				logProtocolError(c.Observer, "unknown", "disconnect_failed", err)
			}
		}
		if mgr != nil {
			mgr.Remove(c.ID)
			recordProtocolConnections(c.Observer, mgr.ActiveCount())
		}
		if c.cancelRead != nil {
			c.cancelRead()
		}
		if c.closed != nil {
			close(c.closed)
		}
		if c.ws != nil {
			go closeWithHandshake(c.ws, code, reason, c.closeHandshakeTimeout())
		}
		if c.Observer != nil {
			c.Observer.LogProtocolConnectionClosed(c.ID, closeReason(code, reason))
		}
	})
}

func (c *Conn) closeHandshakeTimeout() time.Duration {
	if c != nil && c.opts != nil && c.opts.CloseHandshakeTimeout > 0 {
		return c.opts.CloseHandshakeTimeout
	}
	return DefaultProtocolOptions().CloseHandshakeTimeout
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
	disconnectCtx, cancel := context.WithTimeout(ctx, c.disconnectTimeout())
	defer cancel()
	c.Disconnect(disconnectCtx, code, reason, inbox, mgr)
	<-dispatchDone
	<-keepaliveDone
	<-outboundDone
}
