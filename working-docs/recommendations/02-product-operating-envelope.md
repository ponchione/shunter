# Establish A Product Operating Envelope

Status: recommended after current-line qualification

Promotion trigger: Kickbrass or another real product has a stable workflow that
can be measured without artificial product features.

Owners: product app, root runtime, subscription, protocol, commitlog,
operations

## Why

Shunter has extensive synthetic benchmarks, but production suitability depends
on real transaction shapes, subscription fanout, row widths, connection
behavior, restart history, and maintenance practice. A product-derived envelope
should determine later optimization and admission policy.

## Outcome

A short, reproducible statement of the workload Shunter is expected to support
on a named deployment class, including recovery and maintenance expectations.

## Measure

- reducer transaction rate and burst shape
- active connections and subscriptions per connection
- fanout distribution, including hot views and low-selectivity views
- initial subscription row and byte sizes
- steady-state and peak memory
- commit-log growth per day or representative operating period
- clean startup and crash-recovery time at realistic log/snapshot horizons
- snapshot, compaction, backup, and restore duration
- reconnect frequency and subscription replay cost
- tail latency for operator-visible state changes

## Method

1. Capture a sanitized or generated workload model derived from real product
   actions.
2. Keep raw payloads and customer data out of the repository.
3. Define one small and one expected deployment fixture before considering a
   stress fixture.
4. Record hardware, toolchain, Shunter commit, commands, and statistical method.
5. Separate correctness gates from advisory performance evidence.
6. Promote a threshold into a hard release gate only after enough historical
   runs show that it is stable and meaningful.

## Non-Goals

- benchmarks invented solely to justify a planned optimization
- comparisons with SpacetimeDB without a controlled equivalent workload
- a promise of distributed or multi-region scale
- checking large raw telemetry streams into Shunter

## Completion Evidence

- workload description and reproducible command set
- current results in `docs/performance-envelopes.md` or linked release evidence
- documented expected operating range and known failure/slowdown boundaries
- follow-up work selected from measured bottlenecks rather than intuition
