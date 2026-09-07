# Shunter Tech Debt

Status: future-work tracker; four deferred items.

No real product application is selected. The external `opsboard-canary` is
available as a sibling checkout and already exercises public Shunter APIs and
package-shaped client installs. Reuse its natural workflows; product-specific
validation waits for a selected app. A duplicate reference app is unnecessary.

The completed canary capacity study is recorded in
[performance envelopes](../docs/performance-envelopes.md#2026-09-07-external-canary-capacity).

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
