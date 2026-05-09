package protocol

import (
	"context"
	"time"

	"github.com/ponchione/websocket"
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
				if err := c.writeBinary(ctx, frame); err != nil {
					logProtocolError(c.Observer, "unknown", "send_failed", err)
					return
				}
			case <-c.closed:
				for {
					select {
					case frame := <-c.OutboundCh:
						if err := c.writeBinary(ctx, frame); err != nil {
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

func (c *Conn) writeBinary(ctx context.Context, frame []byte) error {
	writeCtx, cancel := c.outboundWriteContext(ctx)
	defer cancel()
	return c.ws.Write(writeCtx, websocket.MessageBinary, frame)
}

func (c *Conn) outboundWriteContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.writeTimeout()
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *Conn) writeTimeout() time.Duration {
	if c != nil && c.opts != nil && c.opts.WriteTimeout > 0 {
		return c.opts.WriteTimeout
	}
	return DefaultProtocolOptions().WriteTimeout
}
