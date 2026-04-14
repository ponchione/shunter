# Story 3.6: OnDisconnect & Cleanup

**Epic:** [Epic 3 — WebSocket Transport & Connection Lifecycle](EPIC.md)
**Spec ref:** SPEC-005 §5.3, §11
**Depends on:** Story 3.3
**Blocks:** Epic 6 (disconnect handling)

---

## Summary

When a connection closes (any reason), remove all subscriptions, run `OnDisconnect` reducer, and clean up connection state.

## Deliverables

- Disconnect sequence (runs once per connection via `sync.Once`):
  1. Remove all subscriptions for this connection: send `DisconnectClientSubscriptionsCmd` to executor inbox
  2. Run `OnDisconnect` reducer via executor
  3. If `OnDisconnect` returns error → log it, continue disconnect
  4. Remove connection from `ConnManager`
  5. Close `OutboundCh` and `closed` channel to unblock write loop and keepalive goroutine
  6. Close WebSocket connection

- `func (c *Conn) Disconnect(ctx context.Context, executor ExecutorInbox)` — executes the disconnect sequence

- Ordering guarantee: subscriptions removed BEFORE `OnDisconnect` runs (per spec §5.3)

## Acceptance Criteria

- [ ] Clean close → `OnDisconnect` fires
- [ ] Idle timeout close → `OnDisconnect` fires
- [ ] `OnDisconnect` error → logged, disconnect proceeds
- [ ] All subscriptions removed before `OnDisconnect` runs
- [ ] Clean disconnect removes the `sys_clients` row via executor-side disconnect handling
- [ ] Connection removed from `ConnManager` after disconnect
- [ ] Double-disconnect is safe (idempotent via `sync.Once`)
- [ ] Write loop and keepalive goroutine exit after disconnect
- [ ] No goroutine leaks after disconnect

## Design Notes

- `sync.Once` ensures disconnect runs exactly once even if triggered from multiple goroutines (read loop error, keepalive timeout, explicit close).
- The spec says `sys_clients` row is removed on disconnect. That's handled by the executor as part of `DisconnectClientSubscriptionsCmd` or `OnDisconnect` — the protocol layer doesn't directly manage `sys_clients`.
- Subscription removal before `OnDisconnect` ensures the reducer sees the connection with no active subscriptions. Any state cleanup the reducer does can reference the identity but not active subscriptions.
