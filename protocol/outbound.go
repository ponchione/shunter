package protocol

import (
	"context"

	"github.com/coder/websocket"
)

// runOutboundWriter drains OutboundCh to the WebSocket in FIFO order.
// It exits on context cancellation, connection close, or write error.
func (c *Conn) runOutboundWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		select {
		case <-ctx.Done():
			return
		case frame := <-c.OutboundCh:
			if err := c.ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
				logProtocolError(c.Observer, "unknown", "send_failed", err)
				return
			}
		case <-c.closed:
			for {
				select {
				case frame := <-c.OutboundCh:
					if err := c.ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
						logProtocolError(c.Observer, "unknown", "send_failed", err)
						return
					}
				default:
					return
				}
			}
		}
	}
}
