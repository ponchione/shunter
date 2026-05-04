# Shunter Design Decisions

This file keeps current implementation-facing Shunter design decisions that
are still cited by code or tests. It is not a history log. For active status,
prefer live code, tests, and the narrow spec section for the surface being
touched.

Reference paths are grounding only. Do not copy or port source from
`reference/SpacetimeDB/`.

This document is not a promise of SpacetimeDB client, wire, or business-model
compatibility. Shunter uses SpacetimeDB as a reference design for runtime
semantics, then owns the final contract for Shunter APIs and Shunter clients.

## Protocol Surface Ownership

Shunter's client protocol is Shunter-native. SpacetimeDB client/wire
compatibility is not a product goal.

Current contract:

- `v1.bsatn.shunter` is the only supported WebSocket subprotocol token.
- Protocol version policy is explicit in `protocol/version.go`: v1 is the
  minimum, current, and only supported version. A future v2 must add a new
  subprotocol token and negotiation pins rather than widening v1 semantics.
- Brotli is a reserved compression tag, but Shunter returns a distinct
  unsupported-brotli protocol error until real Shunter clients need it.
- Client and server protocol decoders reject trailing bytes after a valid
  message body.
- Energy accounting is not part of Shunter's product model. The protocol has no
  energy field, no out-of-energy status, and no quota/metering abstraction.
- Message-family and envelope details are Shunter-specific unless the protocol
  spec explicitly says otherwise.

Deferred unless Shunter clients need it:

- Brotli compression support.
- More machine-readable reducer failure classes beyond the current
  `Committed` / `Failed` outcome model.
- Wrapper-chain reshaping solely for SpacetimeDB wire resemblance.

Authoritative pins:

- `docs/specs/005-protocol/SPEC-005-protocol.md`
- `protocol/version_test.go`
- `protocol/golden_wire_test.go`
- `protocol/subprotocol_contract_test.go`
- `protocol/compression_wire_test.go`
- `protocol/close_codes_contract_test.go`
- `protocol/client_messages_test.go`
- `protocol/server_messages_test.go`
- `protocol/message_family_contract_test.go`

## Outcome Model

The reducer outcome protocol uses the reference-style heavy/light split.

Current contract:

- Callers observe reducer outcomes through `TransactionUpdate`.
- Non-callers with touched subscribed rows receive `TransactionUpdateLight`.
- Non-callers with no touched subscribed rows receive nothing.
- `ReducerCallResult` is removed from the protocol surface; its old tag is
  reserved and must not be reused.
- `TransactionUpdate` carries `UpdateStatus`, caller identity/connection,
  reducer call metadata, timestamp, and execution duration.
- `UpdateStatus` has `Committed` and `Failed` arms.
- Rejections that happen before a transaction opens return a synthetic
  failed `TransactionUpdate` with `TxID == 0`.

Shunter-specific decisions:

- Shunter is not a hosted billing product and has no energy/quota subsystem.
  The protocol has no energy field and no out-of-energy outcome arm.
- Failure strings remain Shunter-specific. Any future reducer-outcome work
  should be framed as a Shunter client-contract slice before changing the exact
  failure surface.

Authoritative pins:

- `protocol/message_family_contract_test.go`
- `protocol/transaction_update_wire_test.go`
- `executor/caller_metadata_test.go`
- `subscription/fanout_test.go`

## Protocol Rows Shape

The current row/update wire shape is Shunter's native protocol contract, not a
reference byte-compatible wrapper chain.

Current contract:

- Applied envelopes and transaction updates use Shunter's flat row/update
  payloads.
- `SubscriptionUpdate` wire layout is:
  `query_id`, `table_name`, `inserts`, `deletes`.
- Inserts are encoded before deletes.
- The flat per-entry `QueryID` is client-facing correlation data.
- Row batches use Shunter's per-row length prefix format from
  `protocol/rowlist.go`, not reference `BsatnRowList` / `RowSizeHint`.
- The remaining wrapper-chain differences are consequences of that inner
  row-list divergence.

Shunter-specific decisions:

- `SubscribeRows`, `DatabaseUpdate`, `TableUpdate`, `QueryUpdate`,
  `CompressableQueryUpdate`, `BsatnRowList`, and `RowSizeHint` remain absent.
- `TableUpdate.num_rows` and deletes-before-inserts reference ordering are
  not part of the Shunter v1 protocol.
- Removing the flat per-entry `QueryID` should happen only if Shunter clients
  need a different correlation model.
- Inner compression remains deferred because Shunter already has outer
  envelope compression.

Reopen only with a concrete Shunter client, migration, ergonomics, or bandwidth
trigger. Do not reopen solely to match SpacetimeDB's client wire shape.

Authoritative pins:

- `protocol/rows_shape_contract_test.go`
- `protocol/subscription_response_wire_test.go`
- `protocol/message_family_contract_test.go`
- `protocol/rowlist.go`
- `docs/specs/005-protocol/SPEC-005-protocol.md`

## Outbound Lag Policy

Current contract:

- `DefaultOutgoingBufferMessages` is `16 * 1024`.
- Outbound queue overflow disconnects the client.
- Fanout cleanup treats send-buffer overflow as a dropped-client path so
  subscription state is reclaimed.
- Incoming request backpressure is a Shunter-specific defensive limit and is
  not part of the outbound lag policy.

Accepted divergence:

- The reference lets the socket disappear without a clean close frame.
  Shunter sends WebSocket close code `1008` with reason `send buffer full`.

Reopen only if a real Shunter client requires different lag behavior.

Authoritative pins:

- `protocol/options.go`
- `protocol/options_test.go`
- `protocol/outbound_lag_policy_test.go`
- `protocol/backpressure_out_test.go`
- `subscription/fanout_worker.go`

## Read-View Lifetime Discipline

Raw committed read views are lower-level expert APIs. Higher-level runtime
paths own their snapshots internally; direct callers of the raw store APIs own
the read-view lifetime.

Current contract:

- `store.CommittedState.Snapshot()` / `store.CommittedReadView` callers must
  call `Close()` promptly when finished.
- A leaked raw committed snapshot can stall commits until the view is closed or
  the best-effort finalizer releases an unreachable leak.
- `Runtime.Read` exposes a callback-scoped read view and closes the underlying
  snapshot when the callback returns.
- `Runtime.Read` callbacks should not synchronously wait on reducer/write work
  while holding the read view.
- Subscription committed views are borrowed for the duration of the evaluator
  call and must not escape.
- `CommittedState.Table` and `StateView` rely on the documented envelope and
  single-executor discipline rather than becoming a general concurrent raw
  pointer surface.

Authoritative pins:

- `docs/specs/001-store/SPEC-001-store.md`
- `store/snapshot.go`
- `store/committed_state.go`
- `store/audit_regression_test.go`
- `store/committed_state_table_contract_test.go`
- `store/snapshot_iter_useafterclose_test.go`
- `subscription/eval_view_lifetime_test.go`
- `executor/pipeline_test.go`

## Commitlog Record Shape

The current commitlog wire is a canonical Shunter format, not a
reference byte-compatible on-disk format.

Current contract:

- Segment headers are 8 bytes: `SHNT`, version `1`, and zero reserved bytes.
- Shunter stores one physical record per transaction.
- Record headers are 14 bytes: `tx_id`, `record_type`, `flags`, and
  `data_len`.
- Record CRC is little-endian CRC32C over the record header plus payload.
- Payload is Shunter's versioned changeset format with deterministic table
  ordering.
- Reader/recovery tolerates an all-zero record-header tail as end of segment.
- Reserved bytes and record flags are strict.
- There is no epoch field, commit grouping, V0/V1 reference split, or
  reference records-buffer format.

Deferred unless Shunter needs them:

- Reference byte-compatible segment magic and commit grouping.
- Reference epoch field and writer `set_epoch` API.
- Reference V0/V1 compatibility.
- Checksum-algorithm vocabulary alignment.
- Forked-offset detection.
- Full reference records-buffer support, including reference transaction payload
  shape.
- Writer-side preallocation-friendly zero tails.

Reopen only with a concrete need to read reference-created logs, support
distributed epochs, or migrate the on-disk format.

Authoritative pins:

- `commitlog/wire_shape_test.go`
- `commitlog/replay_test.go`
- `commitlog/segment.go`
- `commitlog/changeset_codec.go`

## Scheduler Startup And Firing

Current contract:

- Startup replay scans `sys_scheduled`, enqueues past-due schedules, arms
  future schedules, and returns the maximum observed schedule id so
  post-restart allocation does not collide.
- Successful one-shot firings delete their schedule row.
- Successful interval firings advance from the intended fire time.
- Missing schedule rows during a cancel race are tolerated.
- Past-due replay preserves scan order rather than sorting by intended fire
  time.
- Failed one-shot firings leave the schedule row in place for retry.

Accepted deferrals:

- Reference-style `fn_start` clamping for the first repeated schedule time.
- Reference-style deletion of one-shot rows after panic.
- Sorting past-due startup replay by intended time.
- Reference commitlog workload labeling for scheduled firings.
- Startup ordering relative to lifecycle hooks.

Reopen only with workload evidence or a fresh Shunter-visible regression.

Authoritative pins:

- `executor/scheduler_replay_test.go`
- `executor/scheduler_firing_test.go`
- `executor/scheduler_worker_test.go`
- `executor/sys_scheduled_test.go`

## Protocol Executor Seam

Current contract:

- `protocol` owns a narrow `ExecutorInbox` interface for lifecycle,
  subscription, reducer-call, and disconnect handoff.
- `protocol` must not import `executor`; host/runtime wiring translates
  protocol-owned request structs into executor commands.
- The concrete adapter lives with executor/runtime wiring, currently
  `executor.ProtocolInboxAdapter`, because it depends on executor command
  vocabulary.
- Protocol tests should use fakes at this seam instead of starting the full
  executor unless the test is intentionally integration-level.

Rationale:

- The protocol package owns wire metadata such as request IDs, query IDs,
  connection IDs, and protocol response closures.
- The executor package owns transaction ordering, lifecycle reducers,
  subscription registration, and reducer execution.
- Keeping the seam narrow prevents accidental protocol-to-executor dependency
  cycles and keeps each package testable.

Authoritative pins:

- `protocol/lifecycle.go`
- `protocol/upgrade.go`
- `protocol/dispatch.go`
- `executor/protocol_inbox_adapter.go`
- `executor/protocol_inbox_adapter_test.go`

## WebSocket Transport Library

Current contract:

- Shunter uses `github.com/coder/websocket` for WebSocket transport.
- The dependency is replaced in `go.mod` with the Shunter fork
  `github.com/ponchione/websocket`.
- Protocol code relies on the context-first read/write API.
- App-level compression remains Shunter's protocol-envelope concern; per-frame
  WebSocket compression is not the default protocol shape.

Rationale:

- Context-aware transport operations fit Shunter's runtime/executor lifecycle.
- A smaller transport surface keeps protocol configuration narrow while the
  hosted runtime surface is still settling.

Authoritative pins:

- `go.mod`
- `protocol/upgrade.go`
- `protocol/conn.go`
- `protocol/compression_wire_test.go`
- `protocol/subprotocol_contract_test.go`

## Subscription Admission

Current contract:

- `subscription.Manager` is the single source of truth for live subscription
  sets, keyed by connection and wire query ID.
- `protocol` has no separate `SubscriptionTracker` state machine.
- Subscribe and unsubscribe executor commands carry a protocol-owned `Reply`
  closure.
- The executor invokes the `Reply` closure synchronously in the executor
  command path after manager admission and before returning to later commands.
- The `Reply` closure enqueues Applied or Error frames onto the connection's
  outbound FIFO. Later fan-out updates for the same connection enqueue after
  the Applied frame.
- Fan-out delivery does not perform a second wire-ID admission check. It relies
  on manager state, connection lookup, and sender backpressure/closed-connection
  errors.
- Disconnect first removes manager-owned subscriptions for the connection, then
  runs `OnDisconnect`.

Observable guarantees:

- `SubscribeApplied(query_id)` is enqueued before any later transaction update
  that references the same wire query ID on that connection.
- Disconnect between admission and outbound flush discards the result through
  the normal closed-connection send path.
- Duplicate wire query IDs are rejected by the subscription manager.

Authoritative pins:

- `protocol/admission_ordering_test.go`
- `protocol/send_responses.go`
- `protocol/send_txupdate.go`
- `protocol/disconnect.go`
- `executor/protocol_inbox_adapter.go`
- `subscription/manager.go`

## Read Authorization

Current contract:

- External raw SQL reads are governed by table read policy.
- Table read policy is default-private unless a table is explicitly public or
  permissioned.
- Unauthorized raw SQL table references are hidden during schema lookup and
  surface as the existing unresolved/private-table SQL errors.
- Raw SQL authorization applies to every referenced table, including joined or
  filtered tables that are not projected.
- Declared queries and declared views are named runtime-owned read surfaces.
  Generated declaration helpers call declared-read entry points by name rather
  than sending equivalent raw SQL.
- Declared read permission checks are separate from base-table raw SQL policy.
  This lets a module expose a safe declared read over private tables.
- Row-level visibility filters are applied before query evaluation can leak
  rows through joins, initial subscription state, or subscription deltas.
- `AllowAllPermissions` is the explicit admin/dev bypass for table policy,
  declared-read permissions, and row-level visibility.

Rationale:

- Declaration permissions alone are not sufficient because a client can send
  raw SQL equivalent to a declaration.
- Post-filtering only projected rows is not sufficient because joins can reveal
  whether hidden rows exist.
- Raw SQL, declared reads, and reducer/internal reads are distinct surfaces and
  should not share one overloaded permission field.

Authoritative pins:

- `schema/read_policy.go`
- `declared_read.go`
- `declared_read_catalog.go`
- `visibility_filters.go`
- `protocol/auth_read_admission_test.go`
- `protocol/visibility_expansion_test.go`
- `read_auth_gauntlet_test.go`
