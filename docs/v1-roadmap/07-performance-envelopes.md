# Performance Envelopes

Status: open, benchmark baseline exists
Owner: unassigned
Scope: benchmarked and documented workload limits for Shunter v1.

## Goal

Publish realistic v1 performance envelopes so application authors know where
Shunter is expected to work well, where they need indexes or schema changes,
and where the runtime is not yet designed to scale.

The goal is not to win synthetic benchmarks. The goal is to avoid an undefined
production support surface.

## Current State

Shunter has benchmark guidance in `docs/PERFORMANCE-BENCHMARKS.md` and real
store, index, query, subscription, commitlog, snapshot, and protocol
implementation. The current benchmark snapshot covers protocol compression,
commitlog snapshot creation, and subscription evaluator hot paths. Additional
benchmarks exist for reducer commit round trips, scheduler scanning, one-off
query common paths, and subscription fanout/candidate collection.

Some advanced live query paths, especially table-shaped multi-way joins,
intentionally favor correctness and broad semantics over fully incremental
planning.

v1 still needs measured workload limits and tests that detect severe
regressions. The current benchmark document is a baseline log, not a published
support envelope.

## Workload Dimensions

Measure and document at least:

- rows per table
- row size
- number of tables
- number of indexes
- number of connected clients
- subscriptions per client
- total live subscriptions
- reducer calls per second
- one-off query latency
- declared query latency
- subscription initial snapshot latency
- subscription delta fanout latency
- commitlog replay time
- snapshot creation time
- restore time
- memory use during large joins and initial snapshots

## v1 Decisions To Make

- Decide the recommended maximum table size for unindexed scans.
- Decide the required indexing guidance for subscription predicates and joins.
- Decide whether multi-way live joins are v1-supported generally or supported
  only under documented size/index constraints.
- Decide benchmark thresholds that should fail CI versus thresholds that are
  advisory release notes.
- Decide whether benchmark output becomes part of release artifacts.

## Implementation Work

Completed or partially complete:

- Add benchmark run guidance and a 2026-04-30 cleanup baseline under `docs/`.
- Add benchmark coverage for protocol compression, commitlog snapshots,
  subscription evaluation/fanout, reducer commit round trips, scheduler scans,
  and one-off query common paths.

Remaining:

- Audit existing benchmarks and identify missing v1 workflows.
- Add or expand benchmarks for:
  - reducer write throughput
  - indexed lookup and range scans
  - one-off joins
  - declared queries
  - raw subscriptions
  - declared live views
  - multi-way live joins
  - initial subscription snapshots
  - fanout with many clients
  - recovery from commitlog and snapshot
- Add small/medium/large benchmark fixtures with deterministic data.
- Add a performance envelope table under `docs/`.
- Add schema/indexing guidance to the app-author docs.
- Add benchmark runs for the reference app.

## Verification

Run Go benchmarks directly when raw benchmark lines are needed, per `RTK.md`:

```bash
go test -bench . ./...
```

Use `rtk go test ./...` for correctness after benchmark-related code changes:

```bash
rtk go test ./...
```

Record benchmark command, machine notes, data size, and commit hash when
updating published baselines.

## Done Criteria

- v1 docs include concrete workload envelopes.
- Benchmark coverage maps to the documented envelopes.
- The reference app has at least one benchmark or load scenario.
- Indexing requirements for live subscriptions and joins are clear.
- Known expensive query shapes are documented rather than implied.

## Non-Goals

- Matching SpacetimeDB performance.
- Supporting arbitrary large analytical SQL workloads.
- Making every live query shape fully incremental before v1.
- Hiding performance limits behind vague "depends on workload" language.
