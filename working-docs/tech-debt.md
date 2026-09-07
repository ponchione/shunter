# Shunter Tech Debt

Status: future-work tracker; one measurement task, four deferred items, and
one hardening check that requires gap confirmation.

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

## Focused Hardening Checks

These are candidate coverage gaps, not confirmed defects. Check existing tests
before implementation. Close an entry if they already establish its invariant;
otherwise add the smallest deterministic regression for the missing case.

### Joined Subscription Convergence Across Reconnect

Check a joined subscription when concurrent writers update/delete join rows,
delivery is paused, and a subscriber disconnects and resubscribes. At a known
commit boundary, compare the live row multiset with fresh query evaluation for
the same caller; detect missing or repeated delta application and stale delivery
from the old connection. Reuse
[caller delivery ordering tests](../caller_delivery_ordering_test.go) and the
[join evaluation checks](../subscription/eval_test.go).

**Done when:** Existing coverage is identified or one deterministic hosted
scenario establishes convergence after reconnect and passes under the race
detector. Additional join shapes require a specific uncovered case.
