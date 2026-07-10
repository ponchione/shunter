# Reduce Recovery Memory And Latency

Status: recommended evidence-driven refactor

Promotion trigger: current product-derived recovery measurements reproduce
material allocation or latency cost in commit-log tail replay.

Owners: commitlog, store, root recovery wiring, observability

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

- before/after CPU and memory profiles
- benchstat results for snapshot-only, segmented replay, and snapshot-plus-tail
  recovery
- no regression in recovery fault, fuzz, property, and gauntlet tests
- documented peak memory and time improvement on the promoted fixture
