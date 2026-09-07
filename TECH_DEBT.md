# Technical debt

Open findings from the 2026-09-07 architecture/runtime audit, ranked by impact.
Code references were checked at commit `f893c6379aea7ec6d9dced8abe67b4c749e3e81e`;
use the named functions if line numbers move. Update an existing entry when work
addresses the same cause rather than adding a duplicate.

These entries concern Shunter's own guarantees. Static Go applications, narrow
SQL, the native protocol, and the absence of reference-runtime compatibility,
managed hosting, billing, distribution, or multilingual modules are not debt.

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
- [Caller response delivery](subscription/fanout_worker.go),
  [fanout confirmed-read policy](subscription/fanout_worker.go#L71), and
  [fsync before durable watermark publication](commitlog/durability.go#L629).
- The local API already exposes
  [Runtime.WaitUntilDurable](local.go#L256); the remote response does not expose
  an equivalent acknowledgement.

**Smallest useful remedy.** Add an explicit remote durable-success option or
acknowledgement using the existing durability waiter. Define failure and
interruption semantics, preserve existing fast-success behavior unless deliberately
versioned, and expose the distinction through the TypeScript client. Waiting
belongs in delivery, not in the serialized executor. Preserve the existing
ordering of caller replies and subscription deltas.

**Done when.** With fsync held behind a deterministic gate, opted-in success is
withheld until durability advances. A failed durability waiter must never report
durable success; cancellation and reconnect must retain unknown-outcome semantics.
Recover acknowledged transactions after an abrupt process exit. Run targeted
commitlog, executor, protocol, and client checks, and update the protocol contract
and release-facing documentation when implementing the capability.

## Audit evidence and limits

The assessment ran `rtk go test` across the root runtime, store, commitlog,
executor, subscription, protocol, auth, schema, query/sql, codegen, and
protocolclient packages: 5,302 cases passed. Selected executor/subscription race
checks passed 68 cases. TypeScript typechecking and two reconnect/interruption
test groups passed. Those historical passes did not cover every cross-package
ordering or progress failure found by the audit.

The original audit probes used anonymous in-memory Go overlays and real hosted
runtime connections. Permanent regression tests for resolved findings live
alongside the affected packages. Historical passing tests and previous audit
records do not close remaining entries. No sustained-load, physical power-loss,
penetration, reference-runtime execution, or release-qualification result is
implied.

Reference code was inspected only for independent functionality comparison:
ordered [commit/subscription delivery](reference/SpacetimeDB/crates/core/src/subscription/module_subscription_actor.rs#L1738),
optional [durable client delivery](reference/SpacetimeDB/crates/core/src/client/client_connection.rs#L219),
and [snapshot fsync after capture](reference/SpacetimeDB/crates/engine/src/snapshot.rs#L237).
These ignored local paths may be absent in another checkout. They are research
references, not implementation dependencies or permission to copy source.
