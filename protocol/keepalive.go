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
	idleTimer := time.NewTimer(nonNegativeDuration(c.idleRemaining(time.Now())))
	defer idleTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case <-idleTimer.C:
			remaining := c.idleRemaining(time.Now())
			if remaining <= 0 {
				c.requestDisconnect(ClosePolicy, CloseReasonIdleTimeout)
				return
			}
			resetTimer(idleTimer, remaining)
		case <-pingTicker.C:
			remaining := c.idleRemaining(time.Now())
			if remaining <= 0 {
				c.requestDisconnect(ClosePolicy, CloseReasonIdleTimeout)
				return
			}

			// Bound Ping by both its cadence and the current idle deadline so a
			// stuck peer cannot delay timeout enforcement.
			pingCtx, cancel := context.WithTimeout(ctx, min(c.opts.PingInterval, remaining))
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

			// Use a fresh deadline because Ping may have blocked or its Pong may
			// have moved activity forward.
			remaining = c.idleRemaining(time.Now())
			if remaining <= 0 {
				c.requestDisconnect(ClosePolicy, CloseReasonIdleTimeout)
				return
			}
			resetTimer(idleTimer, remaining)
		}
	}
}

func (c *Conn) idleRemaining(now time.Time) time.Duration {
	last := time.Unix(0, c.lastActivity.Load())
	return last.Add(c.opts.IdleTimeout).Sub(now)
}

func nonNegativeDuration(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

func resetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(nonNegativeDuration(d))
}
