package protocol

import (
	"context"
	"log"

	"github.com/coder/websocket"
)

// runOutboundWriter drains OutboundCh and writes each frame to the
// WebSocket as a binary message. Exits when ctx is cancelled, c.closed
// is signaled and any queued frames have been drained best-effort, or a
// write error occurs.
//
// FIFO order is guaranteed by the channel: frames enqueued first are
// dequeued and written first.
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
				log.Printf("protocol: outbound write failed for conn %x: %v", c.ID[:], err)
				return
			}
		case <-c.closed:
			for {
				select {
				case frame := <-c.OutboundCh:
					if err := c.ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
						log.Printf("protocol: outbound write failed for conn %x: %v", c.ID[:], err)
						return
					}
				default:
					return
				}
			}
		}
	}
}
