package protocol

import (
	"context"
	"time"

	"github.com/ponchione/websocket"
)

type connectionTermination struct {
	code   websocket.StatusCode
	reason string
}

// Disconnect tears down a connection once: drop subscriptions, run
// OnDisconnect, remove the Conn, signal local goroutines, and close the socket.
func (c *Conn) Disconnect(ctx context.Context, code websocket.StatusCode, reason string, inbox ExecutorInbox, mgr *ConnManager) {
	if c == nil {
		return
	}
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
		c.outboundMu.Lock()
		c.outboundStopped = true
		c.outboundMu.Unlock()
		if c.closed != nil {
			close(c.closed)
		}
		c.closeTransport(code, reason)
		if c.Observer != nil {
			c.Observer.LogProtocolConnectionClosed(c.ID, closeReason(code, reason))
		}
	})
}

func (c *Conn) closeTransport(code websocket.StatusCode, reason string) {
	if c == nil {
		return
	}
	c.transportCloseOnce.Do(func() {
		c.outboundMu.Lock()
		c.outboundStopped = true
		c.outboundMu.Unlock()
		if c.ws != nil {
			go func() {
				closeWithHandshake(c.ws, code, reason, c.closeHandshakeTimeout())
				if c.cancelRead != nil {
					c.cancelRead()
				}
			}()
		} else if c.cancelRead != nil {
			c.cancelRead()
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
	case <-c.disconnectRequested:
	case <-dispatchDone:
	case <-keepaliveDone:
	case <-outboundDone:
	}
	termination := connectionTermination{code: code, reason: reason}
	if c.disconnectRequested != nil {
		select {
		case <-c.disconnectRequested:
			termination = c.disconnectRequest
		default:
		}
	}
	disconnectCtx, cancel := context.WithTimeout(ctx, c.disconnectTimeout())
	defer cancel()
	c.Disconnect(disconnectCtx, termination.code, termination.reason, inbox, mgr)
	<-dispatchDone
	<-keepaliveDone
	<-outboundDone
}
