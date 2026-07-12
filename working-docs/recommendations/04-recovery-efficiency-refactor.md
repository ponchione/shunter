# Reduce Recovery Memory And Latency

Status: completed 2026-07-12

Promotion trigger: current product-derived recovery measurements reproduce
material allocation or latency cost in commit-log tail replay.

Owners: commitlog, store, root recovery wiring, observability

## Result

The recovery refactor landed in `0b877f2`. Tail replay now stages changes by
touched table and publishes only after complete replay, snapshot byte slices
use the narrower decode path, and snapshot writes avoid per-row schema lookup.

A 10-sample comparison against qualified revision `bfef461` measured the large
snapshot-plus-tail fixture at 1,882.213 ms and 1,887.96 MiB/op before versus
9.245 ms and 10.34 MiB/op after. Segmented recovery improved from 293.554 ms
and 431.469 MiB/op to 3.413 ms and 2.698 MiB/op. Snapshot-only recovery also
improved, and snapshot creation allocations fell 37.21% with no statistically
significant latency change in the filesystem-heavy fixture.

Transient before/after CPU and allocation profiles confirm that per-record
whole-table cloning was removed. Recovery, fault, property, and repository
tests remain the correctness authority. Reproducible commands and full focused
results are in `../../docs/performance-envelopes.md`.

No further generic optimization is active. Application-scale recovery targets
remain dependent on a selected product workload.

## Why

Existing advisory measurements show that snapshot-plus-tail recovery can
allocate far more memory than the persisted dataset size. Recovery correctness
is well tested; the next refactor should preserve those contracts while
reducing transient representation, copying, and repeated rebuild work.

## Investigation

1. Profile `OpenAndRecover*`, record decoding, changeset decoding,
   `ApplyChangeset`, table/index rebuilds, and snapshot selection separately.
2. Attribute retained versus transient allocations with a representative
   snapshot-plus-tail fixture.
3. Identify repeated whole-state copies, row decoding, schema lookup, index-key
   construction, and per-record scratch allocation.
4. Check whether batching replay changes peak memory, correctness, or failure
   attribution.
5. Preserve deterministic ordering and existing error categories.

## Candidate Refactors

Implement only candidates supported by profiles:

- reusable bounded decode scratch buffers
- streaming changeset application instead of retaining decoded tails
- reduced row/value copying where ownership can remain explicit
- batched index maintenance or safe deferred rebuild where semantics permit
- narrower recovery report retention
- additional snapshot cadence guidance if a shorter tail is the safer answer

## Non-Goals

- changing commit-log or snapshot format merely for resemblance to a reference
  runtime
- weakening corruption detection, row limits, or copy isolation
- hiding a slow path by raising memory requirements
- optimizing synthetic replay while regressing crash/fault behavior

## Completion Evidence

- [x] before/after CPU and memory profiles
- [x] benchstat results for snapshot-only, segmented replay, and
  snapshot-plus-tail recovery
- [x] no regression in recovery fault, property, and repository-local gate
  coverage
- [x] documented memory and time improvement on the promoted fixture
