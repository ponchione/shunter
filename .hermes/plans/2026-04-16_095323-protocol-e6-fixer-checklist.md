# Protocol E6 fixer checklist

Goal
- Bring SPEC-005 Epic 6 (Backpressure & Graceful Disconnect) to a shipable state.
- Fix the concrete correctness bugs found in audit.
- Fill the highest-value spec/test gaps, especially around Story 6.3 and Story 6.4.
- End state must pass: `rtk go test ./protocol/ -count=1 -race`

Scope
- Stay inside Protocol E6 unless a compile-integrity follow-on forces a tiny adjacent change.
- Do not widen into unrelated protocol cleanup.
- Prioritize runtime correctness first, then spec-compliance tests, then lower-risk cleanup.

Repo/context reminders
- Use RTK for every shell command.
- Relevant docs:
  - `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/EPIC.md`
  - `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.1-outgoing-backpressure.md`
  - `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.2-incoming-backpressure.md`
  - `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.3-clean-close-network-failure.md`
  - `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.4-reconnection-verification.md`
  - `docs/superpowers/plans/2026-04-16-e6-backpressure-graceful-disconnect.md`
- Key files to touch/look at:
  - `protocol/conn.go`
  - `protocol/disconnect.go`
  - `protocol/sender.go`
  - `protocol/dispatch.go`
  - `protocol/close.go`
  - `protocol/outbound.go`
  - `protocol/keepalive.go`
  - `protocol/upgrade.go`
  - tests under `protocol/*_test.go`

Required validation commands
- Focused red/green loops:
  - `rtk go test ./protocol/ -run '<TestName>' -count=1`
- Final verification:
  - `rtk go test ./protocol/ -count=1 -race`
  - `rtk go build ./protocol/`

---

## Top-priority issues to fix

### 1) Critical: dispatch loop does not reliably exit on server-side Disconnect

Symptoms
- `rtk go test ./protocol/ -count=1 -race` currently fails at:
  - `protocol/reconnect_test.go:141-169`
  - `TestReconnectNoGoroutineLeakAfterDisconnect`
- Root cause: `runDispatchLoop()` blocks in `c.ws.Read(ctx)` and the production call site gives it `context.Background()`, so closing `c.closed` alone does not interrupt the read.

Relevant code
- `protocol/dispatch.go:51-147`
- `protocol/disconnect.go:35-49`
- `protocol/upgrade.go:183-194`
- `protocol/reconnect_test.go:141-169`
- `protocol/keepalive_test.go:233-248` (existing read-exit expectations)

What to look for
- Any server-side disconnect path that expects `c.closed` to unblock `runDispatchLoop()` without also closing the websocket transport.
- Whether `coder/websocket.Conn.Close` or `CloseNow` is the right primitive to break a blocked read promptly.
- Whether orderly close handshake and prompt goroutine exit can both be satisfied.

Recommended fix direction
- Separate two concerns:
  1. send best-effort Close frame / handshake
  2. guarantee transport teardown so blocking read/write goroutines exit promptly
- In `Disconnect`, after teardown bookkeeping, start close handshake, but also ensure the underlying websocket is forced to stop blocking reads/writes in bounded time.
- Most likely shape:
  - keep `closeWithHandshake(...)` for best-effort close frame
  - after timeout (or immediately after starting close in background), call `ws.CloseNow()` to break blocked reads/writes
- If using `CloseNow`, reason carefully about races with `ws.Close`; coder/websocket is designed for this pattern more than trying to rely on `c.closed` only.

Suggested implementation sketch
```go
// closeWithHandshake should return whether the handshake completed.
func closeWithHandshake(ws *websocket.Conn, code websocket.StatusCode, reason string, timeout time.Duration) bool {
    done := make(chan struct{})
    go func() {
        _ = ws.Close(code, truncateCloseReason(reason))
        close(done)
    }()
    timer := time.NewTimer(timeout)
    defer timer.Stop()
    select {
    case <-done:
        return true
    case <-timer.C:
        return false
    }
}
```

Then in `Disconnect`:
```go
if c.ws != nil {
    go func() {
        ok := closeWithHandshake(c.ws, code, reason, c.opts.CloseHandshakeTimeout)
        if !ok {
            c.ws.CloseNow()
            return
        }
        // Even if graceful close succeeded, CloseNow may still be acceptable
        // if blocked goroutines remain a concern; verify against tests/library docs.
    }()
}
```

Alternative acceptable design
- Make `closeWithHandshake` itself call `CloseNow()` on timeout and return.
- That may be cleaner because Story 6.3 explicitly wants force-close on handshake timeout.

Tests to add/adjust first (RED)
- Add or tighten a test proving `Disconnect()` causes a blocked `runDispatchLoop()` to exit promptly.
  - Existing failing test already does this: `protocol/reconnect_test.go:141-169`
  - Consider moving/renaming to `close_test.go` or `disconnect_test.go` if you want the intent clearer.
- Add a test proving a blocked outbound writer also exits after disconnect if needed.
- Add a test around handshake-timeout path verifying a blocked read is broken, not just that helper returns quickly.

Acceptance target
- `TestReconnectNoGoroutineLeakAfterDisconnect` passes reliably.
- No goroutine-leak style disconnect test fails under `-race`.

---

### 2) Critical: send-on-closed-channel panic window in outgoing sender

Symptoms
- `sender.enqueue` gets a `*Conn` from manager, then later non-blocking sends on `conn.OutboundCh`.
- `Disconnect` removes from manager and then closes `OutboundCh`.
- If disconnect happens after `Get()` but before the send, sending to a closed channel can panic.

Relevant code
- `protocol/sender.go:63-81`
- `protocol/disconnect.go:43-45`
- `protocol/conn.go:227-258`

What to look for
- Any path where `conn.OutboundCh` can be closed concurrently with sender enqueue.
- Whether channel-close is actually needed for writer exit vs another signal.

Recommended fix direction
Best fix: stop using `close(conn.OutboundCh)` as the primary shutdown signal for the writer.

Safer design
- Keep `OutboundCh` open for process lifetime of `Conn` object.
- Use `c.closed` and/or transport close to stop loops.
- Make sender reject sends if connection is closing.
- Make writer exit on `c.closed` or websocket error, not on `OutboundCh` close.

Concrete plan
1. In `Disconnect`, do NOT close `OutboundCh`.
2. In `sender.enqueue`, before sending, check whether `c.closed` is already closed.
3. In `sender.enqueue`, use nested select to avoid panic and reject closed connections:
```go
select {
case <-conn.closed:
    return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:]) // or a new ErrConnClosing
default:
}

select {
case <-conn.closed:
    return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:]) // or ErrConnClosing
case conn.OutboundCh <- wrapped:
    return nil
default:
    go conn.Disconnect(context.Background(), websocket.StatusPolicyViolation, "send buffer full", s.inbox, s.mgr)
    return fmt.Errorf("%w: %x", ErrClientBufferFull, connID[:])
}
```
4. Update writer exit semantics so it no longer depends on channel close.

Potential alternative
- Guard `OutboundCh` with a mutex or recover from panic in sender. Do not do this unless forced; it papers over the lifecycle issue.

Tests to add/adjust first (RED)
- Add a racey regression test that repeatedly calls `Send` while concurrently disconnecting, ensuring no panic.
- Existing tests to keep green:
  - `protocol/backpressure_out_test.go`
  - `protocol/sender_test.go`
  - `protocol/outbound_test.go`

Acceptance target
- No panic windows remain for sender/disconnect concurrency.
- `-race` stays green.

---

### 3) Important: Story 6.3 handshake-timeout contract is not implemented

Symptoms
- Spec says: after close-handshake timeout, forcefully close TCP connection.
- Current `closeWithHandshake()` only returns after timeout and leaves background `ws.Close()` running.

Relevant code
- `protocol/close.go:17-44`
- `protocol/close_test.go:166-190`

What to look for
- Whether any code path currently calls `CloseNow()` after timeout. It appears not.
- Whether timeout tests only assert helper return latency rather than transport teardown.

Recommended fix direction
- Make timeout path force close.
- Preferred implementation:
```go
func closeWithHandshake(ws *websocket.Conn, code websocket.StatusCode, reason string, timeout time.Duration) bool {
    done := make(chan struct{})
    go func() {
        _ = ws.Close(code, truncateCloseReason(reason))
        close(done)
    }()
    timer := time.NewTimer(timeout)
    defer timer.Stop()
    select {
    case <-done:
        return true
    case <-timer.C:
        ws.CloseNow()
        return false
    }
}
```

Tests to add/adjust first (RED)
- Upgrade `TestCloseWithHandshake_UnresponsivePeerTimesOut` so it proves more than “helper returned quickly”.
- Add a test proving the peer observes the connection as force-closed / reads unblock / server blocked goroutines exit.

Acceptance target
- Timeout path performs actual forced transport teardown.
- Story 6.3 AC for handshake timeout becomes genuinely implemented, not partially simulated.

---

## Secondary issues to fix after the blockers

### 4) Important: writer-drain semantics are weaker than Story 6.1 promises

Problem
- Story says queued frames should get a flush attempt before closing websocket.
- Current disconnect flow starts websocket close concurrently with writer shutdown behavior.

Relevant code
- `protocol/outbound.go:17-31`
- `protocol/disconnect.go:43-48`
- `protocol/backpressure_out_test.go`

What to decide
- Either:
  1. explicitly weaken the behavior and test only best-effort drain, or
  2. implement a bounded drain phase before force-closing.

Recommended direction
- Keep it best-effort but make the code actually attempt it in order:
  - signal `c.closed`
  - allow writer to keep draining queued messages for a short bounded window
  - then perform close handshake/force-close
- This may require a writer-done channel or connection-scoped writer context instead of raw background fire-and-forget.

Suggested design shape
- Add `writerDone chan struct{}` to `Conn` or launch wrapper in `upgrade.go`.
- `runOutboundWriter` closes writerDone on exit.
- `Disconnect` can wait briefly for writer to flush after closing the connection for new sends but before hard transport close.

Only do this if it can be implemented narrowly. Do not destabilize the whole lifecycle.

Tests to add
- A deterministic test that enqueues 1-2 frames, triggers overflow disconnect, and confirms queued frames are observed before final close when peer is responsive.

---

### 5) Important: incoming-backpressure nil-handler branch leaks semaphore token

Problem
- `runDispatchLoop()` acquires inflight semaphore before handler nil-check.
- If handler is nil, it closes protocol error and returns without releasing token.
- Because the connection exits immediately, this is not a live production leak, but it is sloppy and makes reasoning harder.

Relevant code
- `protocol/dispatch.go:97-145`

Recommended fix
- After decode but before acquire, identify handler function.
- Validate non-nil handler first, then acquire semaphore only when actual dispatch will happen.

Refactor sketch
```go
var run func()
switch m := msg.(type) {
case SubscribeMsg:
    if handlers.OnSubscribe == nil { ...close...; return }
    run = func() { handlers.OnSubscribe(ctx, c, &m) }
case UnsubscribeMsg:
    ...
}

select {
case c.inflightSem <- struct{}{}:
default:
    go closeWithHandshake(...)
    return
}

go func() {
    defer func() { <-c.inflightSem }()
    run()
}()
```

Tests
- Existing `TestDispatchLoop_NilHandlerCloses` should stay green.
- Add a new test that a nil-handler close does not leave the semaphore full if the loop were reused in test harnesses.
- This is low priority if time is tight.

---

### 6) Important: Story 6.4 tests are too thin and not end-to-end

Problem
- Current reconnect tests mostly create fresh `Conn`s and manually copy `Identity` or `ID`.
- They do not exercise real reconnect through `HandleSubscribe`, token validation, `InitialConnection`, or real subscription re-establishment.

Relevant code
- `protocol/reconnect_test.go:9-169`
- Good helpers to reuse:
  - `protocol/lifecycle_test.go:92-135` (`lifecycleServer`, `dialSubscribe`, `readOneBinary`)

Recommended fix direction
Replace or supplement the current reconnect tests with real server-based integration tests.

Must-have scenarios
1. Same token on reconnect => same `InitialConnection.Identity`
2. No subscriptions carry over after disconnect
3. Re-subscribe after reconnect => fresh `SubscribeApplied`
4. Reconnect with new `connection_id` => accepted and different ID
5. Reconnect with same `connection_id` => accepted
6. Reconnect after buffer overflow disconnect => works normally

If feasible, also test
7. Rows changed during disconnect => re-subscribe gets updated snapshot

Suggested helper pattern
- Stand up a `Server` with real JWT config and real `ConnManager`
- Connect via websocket using `dialSubscribe`
- Decode `InitialConnection`
- Use actual client messages (`SubscribeMsg`) and read server responses
- Disconnect cleanly or via overflow path
- Reconnect using same token / desired `connection_id`

If executor/state mocks are needed
- Use a thin fake executor/state implementation that can:
  - respond to subscribe with deterministic `SubscribeApplied`
  - optionally change returned rows between first and second subscription

Minimum acceptable if full end-to-end is too large
- At least cover same token => same identity and same/new connection_id through real upgrade path, not field mutation.

---

## Story-by-story gap fill checklist

### Story 6.1
- [ ] Eliminate sender/disconnect panic race.
- [ ] Decide whether queued-message flush is best-effort or stronger.
- [ ] Add/adjust tests for:
  - [ ] queued messages get a flush attempt before final close (if implemented)
  - [ ] overflow reason string if possible
  - [ ] no panic under send/disconnect race

### Story 6.2
- [ ] Refactor handler selection before semaphore acquire.
- [ ] Add test for close reason string `"too many requests"` if possible.
- [ ] Add more explicit semaphore-release test if practical.

### Story 6.3
- [ ] Make handshake timeout force-close transport.
- [ ] Make server-side disconnect reliably unblock read/write goroutines.
- [ ] Add/adjust tests for:
  - [ ] blocked dispatch loop exits after Disconnect
  - [ ] invalid gzip payload closes with 1002 on live dispatch path
  - [ ] CloseAll sends client-observed 1000 if practical
  - [ ] idle-timeout path triggers disconnect-sequence hooks if practical

### Story 6.4
- [ ] Replace field-mutation pseudo-reconnect tests with actual reconnect integration coverage.
- [ ] Add same-token identity check via real `InitialConnection` decoding.
- [ ] Add same/new `connection_id` acceptance through upgrade query params.
- [ ] Add reconnect-after-overflow using real overflow path.

---

## Concrete test additions recommended

### A. New regression test for server-side disconnect unblocking dispatch
Potential file: `protocol/disconnect_test.go`

```go
func TestDisconnectUnblocksDispatchRead(t *testing.T) {
    inbox := &fakeInbox{}
    mgr := NewConnManager()
    c, _, cleanup := loopbackConn(t, DefaultProtocolOptions())
    defer cleanup()
    mgr.Add(c)

    done := runDispatchAsync(c, context.Background(), &MessageHandlers{})

    c.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

    select {
    case <-done:
    case <-time.After(2 * time.Second):
        t.Fatal("dispatch loop did not exit after Disconnect")
    }
}
```

This overlaps the existing failing reconnect test but isolates the core contract.

### B. New panic-race regression for sender/disconnect
Potential file: `protocol/backpressure_out_test.go`

```go
func TestOutgoingBackpressure_SendConcurrentWithDisconnectDoesNotPanic(t *testing.T) {
    opts := DefaultProtocolOptions()
    c := testConnDirect(&opts)
    inbox := &fakeInbox{}
    mgr := NewConnManager()
    mgr.Add(c)
    s := NewClientSender(mgr, inbox)

    msg := SubscribeApplied{RequestID: 1, SubscriptionID: 1, TableName: "t"}

    done := make(chan struct{})
    go func() {
        defer close(done)
        for i := 0; i < 1000; i++ {
            _ = s.Send(c.ID, msg)
        }
    }()

    c.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)
    <-done
}
```

If there is still a send-on-closed-channel panic, this test or `-race` should expose it.

### C. Live invalid-gzip close-path test
Potential file: `protocol/close_test.go`

Based on existing unknown-compression test, but send `[]byte{CompressionGzip, TagSubscribe, 0x00, 0x01, 0x02}` with `conn.Compression = true` and assert 1002.

---

## Refactor guidance / guardrails

- Prefer small lifecycle changes over wide redesign.
- Keep `Disconnect` as the single teardown authority.
- Avoid adding recover-based panic suppression unless absolutely necessary.
- If you change writer shutdown semantics, re-check:
  - `protocol/outbound_test.go`
  - `protocol/backpressure_out_test.go`
  - `protocol/disconnect_test.go`
  - `protocol/reconnect_test.go`
- If you change dispatch concurrency behavior, re-check:
  - `protocol/backpressure_in_test.go`
  - `protocol/dispatch_test.go`
  - handlers in `handle_subscribe.go`, `handle_unsubscribe.go`, `handle_callreducer.go`, `handle_oneoff.go`

---

## Suggested execution order

1. Reproduce failures and isolate blocker
   - [ ] Run `rtk go test ./protocol/ -run TestReconnectNoGoroutineLeakAfterDisconnect -count=1 -race`
   - [ ] Confirm dispatch read is the stuck goroutine

2. Fix disconnect/transport teardown first
   - [ ] Add failing focused test if needed
   - [ ] Implement forceful unblock of read/write goroutines
   - [ ] Re-run focused disconnect/reconnect tests

3. Fix sender/disconnect channel race
   - [ ] Add panic-race regression test
   - [ ] Remove `OutboundCh` close as shutdown primitive or otherwise make send path safe
   - [ ] Re-run sender/backpressure tests

4. Fix handshake-timeout semantics
   - [ ] Make timeout path force close
   - [ ] Re-run close tests

5. Clean up semaphore/nil-handler path
   - [ ] Refactor dispatch selection/acquire ordering
   - [ ] Re-run dispatch/backpressure tests

6. Strengthen Story 6.4 verification
   - [ ] Add real reconnect integration tests
   - [ ] Keep scope tight; do not rebuild the executor if a focused fake can prove the contract

7. Final verification
   - [ ] `rtk go test ./protocol/ -count=1 -race`
   - [ ] `rtk go build ./protocol/`

---

## Definition of done
- No failures in `rtk go test ./protocol/ -count=1 -race`
- No send-on-closed-channel panic window remains
- Server-side disconnect reliably terminates dispatch/read and keepalive goroutines
- Handshake timeout actually force-closes the transport
- Story 6.4 has at least minimally credible end-to-end reconnect coverage through the real upgrade path
- New/changed tests are stable and not overly timing-fragile

---

## Short bug list for quick handoff
- Critical: `runDispatchLoop` can stay stuck in `ws.Read(context.Background())` after `Disconnect`; see `protocol/reconnect_test.go:141-169`
- Critical: `sender.enqueue` can send on a closed `OutboundCh`; see `protocol/sender.go:63-81` vs `protocol/disconnect.go:43-45`
- Important: `closeWithHandshake` does not force-close on timeout; see `protocol/close.go:17-44`
- Important: Story 6.4 tests are not real reconnect integration tests; see `protocol/reconnect_test.go:9-169`
- Minor/important hygiene: inflight semaphore token leak on nil-handler branch; see `protocol/dispatch.go:101-113`
