package protocol

import (
	"context"
	"log"

	"github.com/coder/websocket"
)

// runOutboundWriter drains OutboundCh and writes each frame to the
// WebSocket as a binary message. Exits when OutboundCh is closed
// (disconnect teardown closes it), ctx is cancelled, or a write error
// occurs.
//
// FIFO order is guaranteed by the channel: frames enqueued first are
// dequeued and written first.
func (c *Conn) runOutboundWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-c.OutboundCh:
			if !ok {
				return
			}
			if err := c.ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
				log.Printf("protocol: outbound write failed for conn %x: %v", c.ID[:], err)
				return
			}
		}
	}
}
