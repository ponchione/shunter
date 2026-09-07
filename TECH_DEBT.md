# Technical debt

No open findings remain from the 2026-09-07 architecture/runtime audit.
Original code references were checked at commit
`f893c6379aea7ec6d9dced8abe67b4c749e3e81e`; historical evidence is retained below.
Broader future work remains in [working-docs/tech-debt.md](working-docs/tech-debt.md).

The audit concerns Shunter's own guarantees. Static Go applications, narrow
SQL, the native protocol, and the absence of reference-runtime compatibility,
managed hosting, billing, distribution, or multilingual modules are not debt.

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
records establish only the coverage exercised. No sustained-load, physical power-loss,
penetration, reference-runtime execution, or release-qualification result is
implied.

Reference code was inspected only for independent functionality comparison:
ordered [commit/subscription delivery](reference/SpacetimeDB/crates/core/src/subscription/module_subscription_actor.rs#L1738),
optional [durable client delivery](reference/SpacetimeDB/crates/core/src/client/client_connection.rs#L219),
and [snapshot fsync after capture](reference/SpacetimeDB/crates/engine/src/snapshot.rs#L237).
These ignored local paths may be absent in another checkout. They are research
references, not implementation dependencies or permission to copy source.
