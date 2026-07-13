package protocol

import (
	"context"
	"time"
)

// runKeepalive sends periodic pings and closes idle connections.
// Activity is credited by successful ping/pong round trips and by inbound
// frames observed by the dispatch loop.
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
			last := c.lastActivity.Load()
			if time.Now().UnixNano()-last >= int64(c.opts.IdleTimeout) {
				c.requestDisconnect(ClosePolicy, CloseReasonIdleTimeout)
				return
			}

			// Bound Ping so a stuck peer cannot delay the next idle check.
			pingCtx, cancel := context.WithTimeout(ctx, c.opts.PingInterval)
			if err := c.ws.Ping(pingCtx); err == nil {
				c.MarkActivity()
			}
			cancel()

			// Avoid sending a redundant close if disconnect fired during Ping.
			select {
			case <-ctx.Done():
				return
			case <-c.closed:
				return
			default:
			}

			// Use a fresh clock sample because Ping may have blocked.
			last = c.lastActivity.Load()
			if time.Now().UnixNano()-last >= int64(c.opts.IdleTimeout) {
				c.requestDisconnect(ClosePolicy, CloseReasonIdleTimeout)
				return
			}
		}
	}
}
