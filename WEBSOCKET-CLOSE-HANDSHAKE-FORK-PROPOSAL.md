# WebSocket Close Handshake Timeout Limitation and Fork Proposal

Date: 2026-04-16
Status: Discussion document for engineering review
Scope: Shunter `protocol/` package, SPEC-005 Epic 6 Story 6.3, current `github.com/coder/websocket` transport decision

## Executive summary

Shunter currently uses `github.com/coder/websocket` as the transport library for the `protocol/` package.

That choice was reasonable and remains defensible for most of the protocol surface:
- context-first API fits Shunter's Go architecture well
- current backpressure, teardown, reconnect, and race-safety work is good
- the current implementation passes `rtk go build ./protocol/` and `rtk go test ./protocol/ -count=1 -race`

However, there is one real transport-level limitation in the library that matters to SPEC-005 Story 6.3:

Shunter cannot currently implement the strongest reading of:
- send WebSocket Close frame
- wait only `CloseHandshakeTimeout`
- if peer does not answer, forcibly tear down the transport immediately

using only the public `coder/websocket` API.

This is not a hypothetical concern. It was reproduced with a live experiment and confirmed by reading the library source.

The practical effect is:
- Shunter can guarantee bounded application-layer teardown latency
- Shunter can attempt a standards-compliant Close handshake
- Shunter cannot currently guarantee exact transport destruction at the configured timeout when a peer is unresponsive

This is not an immediate blocker to the product semantics we are trying to build. SpacetimeDB's reference implementation appears to prioritize bounded closing/draining and server-side cleanup rather than a proven exact hard transport kill at timeout. But this is still a real operational and transport-control gap.

If we decide the exact transport behavior matters enough to own, the cleanest non-hacky path is likely a targeted fork of `github.com/coder/websocket` with one new timeout-aware close primitive, rather than a protocol rewrite or unsafe workaround.

## Current repo state and why this document exists

Relevant Shunter files:
- `docs/decisions/ws-library.md`
- `protocol/close.go`
- `protocol/disconnect.go`
- `protocol/dispatch.go`
- `protocol/outbound.go`
- `protocol/reconnect_test.go`
- `protocol/close_test.go`
- `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.3-clean-close-network-failure.md`

Relevant current dependency:
- `go.mod:6` → `github.com/coder/websocket v1.8.14`

We already patched Shunter to document the current transport limitation explicitly:
- `protocol/close.go` now states that `closeWithHandshake()` only provides bounded Shunter-side waiting, not a proven forced transport teardown at timeout
- `protocol/close_test.go` now names the timeout behavior honestly
- the Story 6.3 decomposition doc now carries an implementation note describing the limitation

That documentation makes the situation honest, but it does not solve it.

This document is the full engineering handoff for deciding whether to fork the library and what that fork should look like.

## Existing transport decision

Current recorded decision:
- `docs/decisions/ws-library.md`

Summary of that decision:
- use `github.com/coder/websocket`
- reject `github.com/gorilla/websocket`
- reason: context-first API, smaller surface, good fit for embedded Go usage

This document does not claim that the original decision was wrong.
It claims that the decision has one specific high-value limitation around close-handshake timeout enforcement that may justify either:
- living with the limitation, or
- forking the library to add the missing primitive

## The exact issue

### Desired behavior

SPEC-005 Story 6.3 conceptually wants:
1. server initiates a Close handshake
2. peer has a bounded amount of time to echo Close
3. if peer does not respond in time, the transport is force-closed
4. local server cleanup remains prompt and reliable

The strongest operational reading of this is:
- actual underlying transport should be torn down at or immediately after the configured timeout, not some longer internal library timeout

### What Shunter can do today

Shunter can do all of the following correctly:
- initiate a best-effort Close frame
- bound its own waiting in `protocol/close.go`
- make `Disconnect` idempotent
- remove subscriptions before `OnDisconnect`
- remove the connection from `ConnManager`
- stop outbound acceptance using `c.closed`
- unblock dispatch reads via connection-scoped read cancellation
- avoid send-on-closed-channel panics
- exit goroutines cleanly under race testing

These are the important correctness and lifecycle guarantees, and they are currently in good shape.

### What Shunter cannot do today with the public library API

Shunter cannot guarantee:
- if `CloseHandshakeTimeout` elapses, the underlying socket/transport is immediately and forcibly torn down

This is because `coder/websocket`'s `Conn.Close()` owns the close lifecycle once it starts, and a later `Conn.CloseNow()` does not preempt it.

## Evidence

### Evidence 1: library docs and source

`github.com/coder/websocket` close source:
- `/home/gernsback/go/pkg/mod/github.com/coder/websocket@v1.8.14/close.go`

Relevant implementation:
- `close.go:86-127` → `func (c *Conn) Close(code StatusCode, reason string) error`
- `close.go:130-154` → `func (c *Conn) CloseNow() error`
- `close.go:157-228` → close handshake internals
- `close.go:230-251` → goroutine wait behavior
- `close.go:324-326` → `casClosing()` gate

Important details from source:
- `Conn.Close()`:
  - performs the close handshake
  - then calls `c.close()`
  - then waits for goroutines
- `Conn.CloseNow()`:
  - also starts with the same closing gate
  - if closing has already started, it waits rather than preempting
- `casClosing()` is a one-shot ownership gate implemented with `c.closing.Swap(true)`

This means:
- once `Close()` wins the closing gate, later close-related calls are effectively waiters, not interrupters

Relevant transport teardown implementation:
- `/home/gernsback/go/pkg/mod/github.com/coder/websocket@v1.8.14/conn.go:151-168`
- `c.close()` actually closes `c.closed` and calls `c.rwc.Close()`

In other words, exact hard transport teardown lives behind internals that are not exposed as a public, timeout-aware primitive.

### Evidence 2: live experiment

A live experiment was run to test the obvious strategy:
- start `serverWS.Close(...)` in one goroutine
- wait ~100ms
- call `serverWS.CloseNow()`
- observe whether `CloseNow()` aborts the in-flight close handshake

Observed output:
- `close_returned_in=5.004262923s closeNow_call_duration=4.903961187s`

Interpretation:
- the in-flight `Close()` still waited about 5 seconds
- the later `CloseNow()` did not force immediate teardown
- instead, it effectively waited behind the same close lifecycle

This is the key empirical proof that the public API does not support the behavior we want.

### Evidence 3: Shunter code and docs

Current Shunter close helper:
- `protocol/close.go`

Current documented limitation:
- `protocol/close.go:17-37`
- `protocol/close_test.go` renamed timeout test
- `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.3-clean-close-network-failure.md`

This reflects the real state of the implementation:
- bounded helper return latency
- not exact forced transport teardown

## How much this matters to Shunter

This issue matters, but not equally across all axes.

### High importance areas that are already in good shape

These matter more to the product than exact transport hard-close timing:
- server-side disconnect bookkeeping
- subscription cleanup correctness
- `OnDisconnect` firing reliably
- outgoing backpressure behavior
- incoming backpressure behavior
- sender/disconnect race safety
- goroutine/task exit reliability
- reconnect semantics and identity behavior

These are core semantic and runtime correctness properties for the system we are building.

### Medium importance area affected by this issue

This issue most directly affects:
- transport control during server-initiated closing with an unresponsive or malicious peer

Potential real-world impact:
- resource retention tail on dead peers
- less predictable shutdown under load
- inability to assert exact transport teardown timing in the spec/runtime

### Does this block fidelity to SpacetimeDB?

Probably not in the strongest sense.

## SpacetimeDB reference context

The SpacetimeDB reference implementation is in:
- `reference/SpacetimeDB/`

Relevant files:
- `reference/SpacetimeDB/crates/client-api/src/routes/subscribe.rs`

Important findings:

1. Close handshake timeout is modeled as bounded draining time
- `subscribe.rs:368-374`
- `close_handshake_timeout` is documented as:
  - "For how long to keep draining the incoming messages until a client close is received."

2. Their actor docs describe a bounded closing phase, not a proven transport kill primitive
- `subscribe.rs:523-547`
- they keep polling until either:
  - peer close arrives, or
  - timeout elapses

3. Their receive-side implementation explicitly times out waiting for client close
- `subscribe.rs:820-839`
- on timeout they log `timeout waiting for client close` and move on

Interpretation:
- SpacetimeDB seems to strongly guarantee bounded actor/session teardown and protocol-closing behavior
- it does not obviously present itself as guaranteeing exact underlying transport destruction at the timeout boundary

This reduces the urgency of the issue from a semantic-fidelity perspective.
It does not remove the operational concern.

## Options considered

### Option A: keep current behavior forever

Pros:
- no library maintenance
- current code stays simple
- currently aligned with bounded server-side teardown semantics

Cons:
- transport-control gap remains
- shutdown timing remains partly at the mercy of library internals
- exact Story 6.3 hard-close semantics remain unattainable

### Option B: switch libraries

Most plausible alternative:
- `github.com/gorilla/websocket`

Why it is attractive:
- exposes more direct transport control
- `Conn.Close()` closes the underlying network connection immediately
- `WriteControl()` lets caller send an explicit Close control frame with deadline
- underlying net.Conn is exposed

Why it is costly:
- Shunter currently leans heavily on context-based API shape from coder
- migration would require rewriting read/write cancellation patterns and many tests
- moderate library migration for a very narrow missing feature

Conclusion:
- viable, but wider than necessary if the only missing feature is timeout-aware forced close

### Option C: hack around coder internals from Shunter

Examples:
- reflection into unexported state
- unsafe tricks
- assumptions about private fields

Pros:
- maybe quick

Cons:
- fragile
- unacceptable maintenance and correctness risk
- exactly the kind of thing we should avoid

Conclusion:
- reject

### Option D: fork `github.com/coder/websocket`

Pros:
- smallest surface-area fix for the exact missing capability
- preserves current context-first API and existing Shunter integration shape
- avoids full migration to a new library
- can be made explicit and non-hacky with a proper new public method

Cons:
- introduces fork maintenance burden
- requires touching the library's close/concurrency core
- must be thoroughly tested

Conclusion:
- most targeted serious fix if we decide this capability matters enough to own

## Recommendation

Recommended path if we decide to invest:
- fork `github.com/coder/websocket`
- add a new timeout-aware close API
- do not change existing `Close()` semantics silently
- keep Shunter on the fork rather than migrating libraries immediately

This is the most robust and least hacky path to the functionality we want.

## Proposed fork scope

### Goal

Add a public API that allows callers to:
1. start a standards-compliant close handshake
2. wait only as long as the caller specifies
3. force transport teardown if the peer does not answer in time
4. preserve existing library semantics for all current callers of `Close()` and `CloseNow()`

### Non-goals

- do not redesign the whole library
- do not rewrite unrelated read/write APIs
- do not change existing `Close()` behavior for current users
- do not expose unsafe/raw internals if a targeted public API is enough

## Proposed API surface

Two plausible shapes.

### Preferred API: explicit timeout method

Option 1:
- `func (c *Conn) CloseWithTimeout(code StatusCode, reason string, timeout time.Duration) error`

Semantics:
- send Close frame
- wait for peer close only until `timeout`
- if peer does not answer, forcibly close underlying transport
- wait for goroutines to unwind
- return nil on normal close, timeout-related error only if desired by API design

### Alternative API: context-based close

Option 2:
- `func (c *Conn) CloseWithContext(ctx context.Context, code StatusCode, reason string) error`

Semantics:
- send Close frame
- wait for peer close while `ctx` is live
- if `ctx` expires/cancels before peer close, forcibly close transport
- wait for goroutines to unwind

### Why not change existing `Close()`?

Because existing callers will reasonably assume its current semantics:
- full library-owned close handshake path
- fixed internal timeouts
- no caller-managed timeout semantics

Changing that silently is a compatibility risk.
A new method is clearer and safer.

## Proposed implementation strategy

### Core observation

Today both `Close()` and `CloseNow()` are gated by the same one-shot `casClosing()` ownership path.
That is why `CloseNow()` cannot interrupt an in-flight `Close()`.

### Desired implementation shape

Refactor close internals into distinct phases:
1. claim closing ownership
2. send close frame
3. wait for peer close with caller-controlled timeout
4. force transport teardown with `c.close()` if timeout elapses
5. wait for goroutines

### Likely internal refactor

Current rough structure:
- `Close()`
  - `casClosing()`
  - `closeHandshake()`
  - `c.close()`
  - `waitGoroutines()`

Likely future structure:
- internal helper to write close frame without taking the whole existing blocking path
- internal helper to wait for handshake under caller-controlled timeout/context
- timeout-aware close method that can call `c.close()` directly if waiting expires

### Important design requirement

The new timeout-aware method must own the close lifecycle from the beginning.
It should not try to "rescue" an already-running `Close()` via `CloseNow()`.
That pattern has already been shown not to work.

## High-level pseudocode for the fork

This is conceptual, not drop-in code.

```go
func (c *Conn) CloseWithTimeout(code StatusCode, reason string, timeout time.Duration) error {
    if c.casClosing() {
        if err := c.waitGoroutines(); err != nil {
            return err
        }
        return net.ErrClosed
    }

    // Best effort send of the close frame.
    if err := c.writeCloseWithTimeout(code, reason, timeout); err != nil && !errors.Is(err, net.ErrClosed) {
        // transport likely already broken; continue to hard close
    }

    // Wait for peer close only up to caller timeout.
    waitErr := c.waitCloseHandshakeWithTimeout(timeout)
    if waitErr != nil {
        // IMPORTANT: real transport close, not another public Close*/CAS path
        _ = c.close()
        return combine(waitErr, c.waitGoroutines())
    }

    err := c.close()
    err2 := c.waitGoroutines()
    return combine(err, err2)
}
```

The exact code will differ, but the essential point is:
- timeout-aware close must be a first-class internal path
- it cannot be layered naively on top of current `Close()` and `CloseNow()` behavior

## Test plan for the fork

Minimum required library tests.

### 1. Responsive close still works
- peer replies with Close promptly
- new API returns quickly
- no unexpected errors

### 2. Unresponsive peer times out and transport is actually closed
- peer never replies
- new API returns soon after configured timeout
- underlying transport is unusable immediately afterward
- blocked read/write goroutines unwind

### 3. Timeout-aware close preempts the old problem
- reproduce current failing pattern conceptually
- ensure timeout-aware method does not inherit `Close()`'s 5s wait

### 4. Repeated close calls remain sane
- first `CloseWithTimeout()` wins
- later `Close()` / `CloseNow()` / `CloseWithTimeout()` calls do not corrupt state

### 5. Race/concurrency coverage
- concurrent reader active during timeout close
- concurrent writer active during timeout close
- close while control frames are in flight

### 6. Shunter integration proof
Inside Shunter after vendor/fork wiring:
- replace `protocol/close.go` helper to call new timeout-aware method
- run:
  - `rtk go build ./protocol/`
  - `rtk go test ./protocol/ -count=1 -race`

## Estimated effort

This is not a huge codebase-wide change, but it is central concurrency code.

Library size snapshot:
- about 46 Go files
- about 7,529 lines of Go
- about 3,062 lines of tests

### Effort estimate

Optimistic:
- 1 focused workday for proof + implementation + targeted tests

Realistic:
- 1.5 to 3 days for a trustworthy implementation and validation pass

Breakdown:
- design and close-path spelunking: 2–4 hours
- implementation: 2–5 hours
- stabilization and tests: 3–8 hours

## Risk assessment

### Main technical risks

1. Read-side locking interactions
- `waitCloseHandshake()` currently owns `readMu`
- timeout refactor can accidentally deadlock or race with read-side close handling

2. Write-side close state interactions
- `write.go` tracks `closeSentErr` / `closeReceivedErr`
- sending close and then forcing transport teardown must not corrupt state invariants

3. Goroutine unwinding semantics
- must preserve guarantee that once timeout fires, all goroutines can unwind reliably

4. API semantics ambiguity
- new method must be documented precisely so callers know whether timeout means:
  - hard close happened
  - or merely local wait stopped

### Why the risk is still acceptable

- the issue is concentrated in a small number of files
- the behavior is already understood well enough to explain clearly
- the desired capability is narrow and testable
- this is much less risky than a full library migration or transport rewrite

## Why forking is preferable to migrating immediately

A migration to `github.com/gorilla/websocket` is plausible, but it is a broader decision than the issue itself requires.

Shunter currently benefits from coder's API shape:
- context-based `Read(ctx)` / `Write(ctx, ...)`
- simple `Ping(ctx)`
- clean integration with the rest of the Go codebase

A fork preserves those advantages while solving the exact missing feature.

If we later decide the fork is undesirable, we can still revisit a library migration with more information.

## Integration proposal for Shunter if the fork exists

1. Replace module dependency
- switch `go.mod` from upstream `github.com/coder/websocket` to the forked module or replace directive

2. Update `protocol/close.go`
- replace the current helper with a call to the new timeout-aware close method

3. Keep current disconnect architecture
- no need to redesign `Disconnect`, `runDispatchLoop`, `runOutboundWriter`, or reconnect semantics around this change
- they are already doing the right things at the application layer

4. Re-run protocol verification
- `rtk go build ./protocol/`
- `rtk go test ./protocol/ -count=1 -race`

## Proposed decision

Recommended decision to spec out:

- Keep current Shunter behavior and docs for now.
- Open a design/spec for a small fork of `github.com/coder/websocket`.
- Add a new timeout-aware close API rather than mutating `Close()` semantics.
- Use that fork only if we decide exact close-timeout transport enforcement is worth owning.

This keeps us honest today and gives us a credible non-hacky path forward.

## Suggested next engineering questions

1. Do we want exact transport-force-close semantics strongly enough to own a fork?
2. If yes, should the fork be:
   - a private maintenance fork, or
   - a small upstreamable patch we can carry temporarily?
3. Which API shape is preferred?
   - `CloseWithTimeout(...)`
   - `CloseWithContext(...)`
4. What are the operational requirements that justify the work?
   - connection volume
   - shutdown behavior expectations
   - adversarial or unreliable client assumptions
5. Is bounded application-layer teardown sufficient for v1, with the fork deferred until evidence says otherwise?

## Final recommendation

If the head engineer wants a robust and useful answer without hacks:
- do not use unsafe/reflection tricks
- do not rewrite the whole transport stack
- do not migrate libraries yet just for this one gap

Instead:
- treat the current documented limitation as acceptable for now
- and if exact behavior matters enough, fork `github.com/coder/websocket` and add one explicit timeout-aware close primitive

That is the narrowest serious fix, the easiest to reason about, and the least disruptive to the rest of Shunter's protocol implementation.
