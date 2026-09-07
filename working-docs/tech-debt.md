# Shunter Tech Debt

Status: future-work tracker; one measurement task and four deferred items.

No real product application is selected. The external `opsboard-canary` is
available as a sibling checkout and already exercises public Shunter APIs and
package-shaped client installs. Reuse its natural workflows; product-specific
validation waits for a selected app. A duplicate reference app is unnecessary.

## Canary Capacity Measurement

**Status:** Ready to scope. Combines fanout, application timing, and memory
measurement into one bounded study using the existing canary.

Choose a small, fixed matrix of dataset sizes, concurrent clients, read/write
mixes, and uneven subscription distributions on one documented host. Measure
reducer/read/delta latency and throughput, process RSS and Go heap during
sustained writes and snapshots, and offline backup/restore duration at the same
dataset sizes. Include datasets larger than the published backup/restore
fixtures; identify the workload and limits rather than calling it production
capacity without a real application.

**Done when:** Reproducible commands, seeds, revisions, hardware, fixture sizes,
repeated measurements, and observed limits are recorded in
[performance envelopes](../docs/performance-envelopes.md), using the existing
[benchmark workflow](../docs/benchmarks.md). Results remain advisory.

## Deferred

1. **Hard performance gates.** Revisit after a representative workload, stable
   measurement environment, and acceptable performance budgets are agreed.
   Then use repeated baseline comparisons and observed variance to set useful
   thresholds; collecting more history alone is not a task.
2. **Public npm publishing.** Revisit when an external consumer needs registry
   distribution. Private/local packaging already works. Resolve public package
   ownership, release authority, access/2FA, publish policy, metadata/licensing,
   provenance, and public artifact policy using the
   [client promotion checklist](../typescript/client/README.md).
3. **App scaffolding tooling.** Revisit when repeated app creation exposes
   concrete friction that the maintained hosted-chat template and
   [hosting guide](../docs/how-to/host-shunter-backend.md) do not address.
4. **Development watchers.** Revisit when real app work demonstrates costly
   manual rebuild/restart or TypeScript regeneration. Automate the observed
   bottleneck using existing tooling first.
