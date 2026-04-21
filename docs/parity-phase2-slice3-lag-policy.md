# Phase 2 Slice 3 — lag / slow-client policy decision

This document records the Phase 2 Slice 3 decision called out in
`docs/spacetimedb-parity-roadmap.md` §3 Phase 2 and §4 Slice 5. It is the
written-down companion to the parity pin that locks the chosen shape.

## Reference shape (target)

`reference/SpacetimeDB/crates/core/src/client/client_connection.rs`:

- Per-client outbound delivery goes through
  `ClientConnectionSender::send` at `client_connection.rs:394-431`.
- The outbound channel is a bounded `tokio::sync::mpsc::Sender<ClientUpdate>`
  with capacity `CLIENT_CHANNEL_CAPACITY = 16 * KB = 16384` slots
  (`client_connection.rs:657`, constructed at `client_connection.rs:719`).
- On `TrySendError::Full`: log warn, call `self.abort_handle.abort()`
  (aborts the tokio task running the per-client actor), set
  `cancelled = true` (relaxed atomic store), and return
  `Err(ClientSendError::Cancelled)` (`client_connection.rs:400-416`).
- Aborting the per-client actor future drops the websocket write loop
  without sending a clean WebSocket close frame; the peer observes the
  socket go away.
- Subsequent `send()` calls short-circuit on the `cancelled` atomic at
  `client_connection.rs:395-397` and return `Err(Cancelled)`.

`reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs`:

- The broadcast side (`SendWorker`) runs on an **unbounded** mpsc queue
  (`module_subscription_manager.rs:1835`). The broadcast-level queue
  never exerts backpressure — the pressure point is always the
  per-client bounded channel above.
- When broadcast fans out a transaction, it calls `send_to_client_v1`
  at `module_subscription_manager.rs:2160` which wraps
  `client.send_message()` and logs a warning on `Err`
  (`module_subscription_manager.rs:2166`). The actual client kick
  already happened inside `send_message` via `abort_handle.abort()`.
- `ClientInfo.dropped: Arc<AtomicBool>` (`module_subscription_manager.rs:167`)
  is a **separate** cleanup mechanism for per-subscription evaluation
  failures, not for send-queue-full. `SubscriptionManager::remove_dropped_clients`
  (`module_subscription_manager.rs:829`) lazily reclaims those entries.

Summary of reference policy: **disconnect-on-lag** at a 16384-message
per-client queue depth, no lazy slow-client heuristic, no
subscription-level drop-without-disconnect on send pressure.

## Shunter shape today (origin)

`protocol/options.go:39-48`:

- `DefaultProtocolOptions` sets `OutgoingBufferMessages = 256`.

`protocol/sender.go:85-109`:

- `connManagerSender.enqueueOnConn` performs a non-blocking select on
  `conn.OutboundCh`.
- On the `default:` arm (buffer full) it spawns
  `go conn.Disconnect(..., websocket.StatusPolicyViolation,
  "send buffer full", ...)` and returns
  `fmt.Errorf("%w: %x", ErrClientBufferFull, connID[:])`.

`subscription/fanout_worker.go:196-213`:

- On `errors.Is(err, ErrSendBufferFull)` the worker calls
  `markDropped(connID)` which drops the connection's confirmed-read
  state and signals the manager via the `dropped` channel for
  subscription-level cleanup.

`protocol/dispatch.go:158-167`:

- Incoming-side inflight semaphore is
  `opts.IncomingQueueMessages = 64` per-connection. Overflow closes
  with `ClosePolicy 1008 "too many requests"`.

Summary of current policy: **disconnect-on-lag** at a 256-message
per-client queue depth, explicit WebSocket close frame with status
`1008 (policy violation)` and a human-readable reason.

## Decision: emulate queue depth, accept close-code mechanism difference

The prompt framing anticipated a deep structural divergence ("deeper
queue with lazy slow-client handling that tolerates bursts and drops
laggards differently"). After reading the reference directly that
framing is wrong in two pieces:

1. The reference **does disconnect** when the per-client outbound
   queue fills. It is not a lazy "drop subscriptions but keep the
   connection" design.
2. The `dropped` AtomicBool cleanup path is a **per-subscription
   evaluation-failure** mechanism, not a slow-client mechanism. Shunter
   already has a structurally equivalent path via
   `FanOutMessage.Errors` + `SubscriptionError` delivery and the
   `markDropped` / dropped-channel seam to the manager.

So the actual parity gap is narrower than the prompt implied: the
per-client outbound queue depth differs (reference 16384 vs Shunter
256) and the close mechanism differs (reference aborts the per-client
tokio task, dropping the socket; Shunter sends an explicit 1008 close
frame with reason `"send buffer full"`).

Adopted now (closes Phase 2 Slice 3):

- Default `OutgoingBufferMessages` is raised from `256` to `16384` to
  match reference `CLIENT_CHANNEL_CAPACITY`. The constant is named
  `DefaultOutgoingBufferMessages` so the parity anchor is a named
  symbol, not a magic literal.
- Semantic behavior on overflow is unchanged: per-client outbound
  queue full → connection is torn down, any subscriptions for that
  connection are cleaned up lazily, caller-heavy invariant
  (`P0-DELIVERY-002`) is preserved for the remaining connected
  clients.
- Mechanism difference is pinned as intentional: Shunter sends a
  WebSocket close frame with `StatusPolicyViolation (1008)` and reason
  `"send buffer full"` rather than aborting the connection actor and
  letting the socket drop. The 1008 close frame is cleaner, lets
  clients distinguish "policy kick" from "abnormal drop", and the
  externally visible outcome (client observes disconnect, subscriptions
  reaped server-side) matches the reference.
- The dispatch-side inflight semaphore
  (`IncomingQueueMessages = 64`, overflow → `ClosePolicy 1008 "too
  many requests"`) stays unchanged. The reference does not have a
  parallel incoming-rate semaphore at this layer, so Shunter's choice
  is a defensive divergence that does not create a parity gap on
  outbound/slow-client behavior — it constrains inbound flooding,
  which is an orthogonal concern.

Deferred explicitly (mechanism differences the decision does not
close):

- Close mechanism on outbound overflow: reference aborts the tokio
  task without a clean close frame; Shunter sends `1008 "send buffer
  full"`. Kept as a conscious divergence because the 1008 frame is a
  clearer signal to clients and clients that speak the protocol will
  correctly reap on either shape. Revisited only if an observed client
  starts distinguishing the two.
- `cancelled` atomic fast-path: reference short-circuits subsequent
  `send()` calls via a relaxed-atomic check on the sender
  (`client_connection.rs:395-397`). Shunter's equivalent is `select`
  on `conn.closed` at `protocol/sender.go:94-98` + the `ConnManager`
  lookup returning `ErrConnNotFound` once Disconnect completes. Same
  outcome (further sends return an error promptly), different
  mechanism — does not warrant a rewrite.
- Lazy `dropped` AtomicBool cleanup: reference uses `Arc<AtomicBool>`
  + `remove_dropped_clients`. Shunter uses `FanOutMessage.Errors` +
  `markDropped` channel signal + manager-side unregister. Same
  externally visible contract (failed subscriptions surface a
  `SubscriptionError` and the server reclaims their state without
  tearing down the connection), different mechanism. Not a parity
  gap; left as-is.

## Why this option (and not the alternatives)

- **Full emulation** (deep queue + invent a slow-client heuristic not
  present in the reference) overruns the slice and invents hazards.
  The reference has no lazy slow-client heuristic; emulating one would
  be net-new behavior with its own tuning and concurrency burden.
- **Permanent divergence pin** (keep 256 as a smaller default and
  document it) closes nothing externally visible. A 64×-smaller
  outbound queue is a real behavioral mismatch — realistic bursty
  workloads that would tolerate reference-sized queuing will
  disconnect clients under Shunter. That is exactly the kind of
  "same architecture, different outcome" gap the parity roadmap is
  meant to close.
- **Emulate queue depth + pin mechanism difference** closes the
  actually-visible gap (lag tolerance) with a one-line default
  change, preserves Shunter's cleaner 1008 close mechanism, and does
  not invent new concurrency hazards. The already-present
  `markDropped` + dropped-channel plumbing carries the reference's
  cleanup contract.

## What this decision blocks / unblocks

Unblocks:

- `P0-SUBSCRIPTION-001` row in `docs/parity-phase0-ledger.md` moves
  from `open` to `closed (divergences explicit)` once the parity pin
  lands.
- `TECH-DEBT.md` OI-002 drops the "bounded fail-fast fanout / slow-
  client" bullet and retains only the remaining Tier A2 surface (SQL
  breadth + RLS).

Does not unblock:

- Phase 3 runtime parity (reducer outcome model, scheduling).
- Phase 4 durability parity (replay, TxID origin, snapshot invariants).
- RLS / per-client filtering (separate Phase 2 slice).

## Authoritative artifacts

- This document — written record of the decision and deferrals.
- `protocol/options.go` — `DefaultOutgoingBufferMessages` named
  constant, wired into `DefaultProtocolOptions`.
- `protocol/options_test.go::TestDefaultProtocolOptions` — updated to
  assert the reference-aligned default.
- `protocol/parity_lag_policy_test.go` — new parity pin asserting
  the default matches reference `CLIENT_CHANNEL_CAPACITY = 16 * 1024`.
- `protocol/backpressure_out_test.go` — left unchanged; these tests
  parameterize `OutgoingBufferMessages` explicitly, so they continue
  to pin the overflow-disconnect mechanism independently of the
  default.
- `docs/parity-phase0-ledger.md` — new `P0-SUBSCRIPTION-001` row.
- `docs/spacetimedb-parity-roadmap.md` — Tier A2 and Phase 2 Slice 3
  sections updated to note this slice closed.
