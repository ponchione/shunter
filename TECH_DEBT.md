# Technical debt

Open findings from the 2026-09-07 architecture/runtime audit, ranked by impact.
Code references were checked at commit `f893c6379aea7ec6d9dced8abe67b4c749e3e81e`;
use the named functions if line numbers move. Update an existing entry when work
addresses the same cause rather than adding a duplicate.

These entries concern Shunter's own guarantees. Static Go applications, narrow
SQL, the native protocol, and the absence of reference-runtime compatibility,
managed hosting, billing, distribution, or multilingual modules are not debt.

## TD-001: Caller replies can overtake earlier subscription deltas

**Status:** Open. **Evidence:** Reproduced ordering defect. **Priority:** First.

**Guarantee and consequence.** SPEC-003 §5.3 promises that clients cannot observe
commit N+1's delta before N's. A subscribed client can nevertheless receive a
later delete before an earlier insert, potentially leaving its cache with a row
that no longer exists on the server.

**Cause and entry points.** `Executor.postCommit` captures protocol callers'
updates and excludes those callers from ordinary fanout. The protocol adapter
then sends their heavy response independently of the fanout worker's earlier
light updates. Synchronous evaluation does not order these two delivery paths.

- [Ordering contract](working-docs/specs/003-executor/SPEC-003-executor.md#L465).
- [Caller extraction in postCommit](executor/executor.go#L1147) and
  [ProtocolInboxAdapter.deliverReducerResponse](executor/protocol_inbox_adapter.go#L442).
- [Light delivery](subscription/fanout_worker.go#L195) and
  [client cache application](typescript/client/src/index.ts#L1512).

**Reproduce.** Use the root `validChatModule` fixture, a real running runtime,
`httptest.NewServer(rt.HTTPHandler())`, and `protocolclient.Dial`:

1. Subscribe the client to `SELECT * FROM messages` and consume the initial state.
2. Wrap the runtime's `swappableFanOutSender` target with a one-shot gate before
   forwarding the next `SendTransactionUpdateLight`. Insert a row through a local
   reducer and wait until its light delivery reaches that gate.
3. Send a reducer call from the subscribed client that deletes that same row.
   Read the wire while the earlier insert is still paused; release the gate on
   cleanup.

The audit probe `TestAuditCallerDeltaCommitOrder` received a heavy
`TransactionUpdate` containing the delete before releasing the insert gate.
This deliberately controls goroutine scheduling; the resulting cache corruption
is inferred from the client's update algorithm, not separately reproduced in a
browser.

**Smallest useful remedy.** Give caller responses and subscription deltas one
ordered per-connection delivery path. Preserve initial-state-before-delta
ordering, one caller response per request, suppression flags, and disconnect
cleanup. Coordinate with TD-002; adding another blocking global queue is not a
solution. The existing
[protocol-owned-response test](executor/pipeline_test.go#L835) pins the current
ownership split and must be reconsidered alongside the behavioral regression.

**Done when.** A deterministic hosted regression proves that the delete cannot
overtake the paused insert and that applying both updates leaves an empty cache.
Cover alternating local/external writers and subscription admission without
duplicate caller updates. Run targeted root, executor, subscription, and protocol
tests, then race checks for the affected delivery paths.

## TD-002: Procedure delivery barriers can stall the global executor

**Status:** Open. **Evidence:** Reproduced progress failure. **Priority:** Second.

**Guarantee and consequence.** Bounded delivery should isolate slow consumers
without creating a cyclic wait with reducer execution. A procedure can instead
stop global reducer progress while health continues to report ready.

**Cause and entry points.** `HandleCallProcedure` releases `deliveryReady` only
after the procedure and its response finish. The single fanout worker waits on
that barrier for the caller. Once its bounded inbox fills, synchronous
`Manager.sendFanOut` blocks the executor. The procedure is waiting for that same
executor to finish its next reducer call.

- [Barrier lifetime](procedure.go#L198),
  [waitForDeliveryReady](subscription/fanout_worker.go#L224), and
  [blocking sendFanOut](subscription/eval.go#L83).
- [Fanout capacity uses ExecutorQueueCapacity](lifecycle.go#L175).
- [Health readiness checks flags, not progress](health.go#L182).

**Reproduce.** Extend
`TestProtocolProcedureReducerProducesOneResponseThenCallerLightDelta` in
[procedure_test.go](procedure_test.go#L404). Set `ExecutorQueueCapacity: 1`, keep
the procedure caller subscribed to `messages`, and make its procedure call the
insert reducer four times sequentially before returning. Check both the call's
completion and `Runtime.Health()` while it is pending.

The audit probe `TestAuditProcedureQueueProgress` timed out after one second.
Captured health showed `Ready=true`, `Degraded=false`, fanout depth/capacity
`1/1`, executor inbox depth `0`, and no executor, durability, or fanout fatal
error. The dependency cycle is supported by the code above, not just the timeout.

**Smallest useful remedy.** Confine deferred delivery to bounded per-client state
that can disconnect on overflow without stopping the worker or executor. Preserve
the existing procedure-response-before-caller-light-delta behavior unless that
contract is explicitly changed. Do not merely increase queue capacity. Add a
meaningful stalled-progress diagnostic; queue length and fatal flags alone miss
this failure. Coordinate delivery ownership with TD-001.

**Done when.** Multi-reducer procedures exceeding fanout capacity either complete
or trigger the defined client-local overflow outcome without blocking an unrelated
client's reducer. Cover caller disconnect, cancellation, procedure error, and
shutdown while delivery is deferred. Run root procedure/health tests and affected
executor, subscription, and protocol tests with relevant race checks.

## TD-003: Remote callers cannot request durable success

**Status:** Open. **Evidence:** Documented capability limitation, not a violation
of today's wire contract. **Priority:** Third.

**Goal and consequence.** Remote applications cannot request confirmation that a
successful state transition will survive a crash before taking an external
action. A caller-heavy `StatusCommitted` can precede fsync, even though non-caller
light fanout defaults to confirmed reads. Recently acknowledged caller state can
therefore disappear after a crash before persistence.

**Evidence and entry points.**

- [Explicit public protocol limitation](protocol/server_messages.go#L98) and
  [SPEC-004 §12.3](working-docs/specs/004-subscriptions/SPEC-004-subscriptions.md#L865).
- [Caller response delivery](executor/protocol_inbox_adapter.go#L442),
  [fanout confirmed-read policy](subscription/fanout_worker.go#L71), and
  [fsync before durable watermark publication](commitlog/durability.go#L629).
- The local API already exposes
  [Runtime.WaitUntilDurable](local.go#L256); the remote response does not expose
  an equivalent acknowledgement.

**Smallest useful remedy.** Add an explicit remote durable-success option or
acknowledgement using the existing durability waiter. Define failure and
interruption semantics, preserve existing fast-success behavior unless deliberately
versioned, and expose the distinction through the TypeScript client. Waiting
belongs in delivery, not in the serialized executor. Coordinate with TD-001.

**Done when.** With fsync held behind a deterministic gate, opted-in success is
withheld until durability advances. A failed durability waiter must never report
durable success; cancellation and reconnect must retain unknown-outcome semantics.
Recover acknowledged transactions after an abrupt process exit. Run targeted
commitlog, executor, protocol, and client checks, and update the protocol contract
and release-facing documentation when implementing the capability.

## TD-005: Snapshot publication occupies the executor through disk I/O

**Status:** Open. **Evidence:** Code-supported operational tradeoff; pause duration
not measured in the audit. **Priority:** Fifth.

**Goal and consequence.** A consistent snapshot needs a stable captured horizon,
but today's implementation also stops reducer progress throughout serialization,
file writes, fsync, and publication. Larger state or slower storage extends
application write pauses beyond capture. The current path favors simplicity and
correctness; the debt is its operational ceiling, not demonstrated data loss.

**Evidence and entry points.**

- [Runtime.CreateSnapshot](storage.go#L28) submits a capture closure calling the
  complete file writer.
- [Executor.handleCreateSnapshot](executor/executor.go#L528) waits for the durable
  horizon and synchronously runs that closure before processing more work.
- [FileSnapshotWriter.CreateSnapshot](commitlog/snapshot_io.go#L449) captures a
  detached body, then calls `createSnapshotFromBody`; the
  [publication path](commitlog/snapshot_io.go#L504) performs serialization, fsync,
  rename, and directory synchronization before returning.

**Smallest useful remedy.** Keep consistent detached capture serialized, then
publish that captured body outside the executor. Preserve horizon validation,
snapshot completion/error semantics, storage ownership through publication,
bounded concurrent snapshots, and safe interaction with compaction and shutdown.

**Done when.** Pause snapshot writing after capture and demonstrate that another
reducer commits while publication remains pending. Recovery must reconstruct the
captured horizon plus subsequent log entries; failed publication must not enable
unsafe compaction. Exercise close during publication and existing snapshot fault
tests. Run targeted root storage, executor snapshot, and commitlog recovery/
compaction tests, then measure pause time and peak memory with representative
state sizes.

## Audit evidence and limits

The assessment ran `rtk go test` across the root runtime, store, commitlog,
executor, subscription, protocol, auth, schema, query/sql, codegen, and
protocolclient packages: 5,302 cases passed. Selected executor/subscription race
checks passed 68 cases. TypeScript typechecking and two reconnect/interruption
test groups passed. These passes did not cover the two failing cross-package
scenarios above.

The two named audit probes used anonymous in-memory Go overlays and real hosted
runtime connections. Their source was not added to the repository; the setup and
observed failures are recorded above so later sessions can add durable regression
tests. Existing passing tests and previous audit records do not close these
entries. No sustained-load, physical power-loss, penetration, reference-runtime
execution, or release-qualification result is implied.

Reference code was inspected only for independent functionality comparison:
ordered [commit/subscription delivery](reference/SpacetimeDB/crates/core/src/subscription/module_subscription_actor.rs#L1738),
optional [durable client delivery](reference/SpacetimeDB/crates/core/src/client/client_connection.rs#L219),
and [snapshot fsync after capture](reference/SpacetimeDB/crates/engine/src/snapshot.rs#L237).
These ignored local paths may be absent in another checkout. They are research
references, not implementation dependencies or permission to copy source.
