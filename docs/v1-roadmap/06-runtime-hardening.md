# Runtime Hardening

Status: open
Owner: unassigned
Scope: correctness proof for Shunter runtime behavior under concurrency,
crashes, recovery, protocol traffic, visibility filtering, and malformed input.

## Goal

Raise confidence from "many package tests pass" to "the v1 runtime behavior is
proven against realistic failure modes."

The hardening work should build on `docs/RUNTIME-HARDENING-GAUNTLET.md` and
turn it into CI-backed or routinely runnable verification.

## Current State

Shunter has broad Go test coverage across packages. That is necessary but not
the same as black-box runtime proof. The remaining risk is in interactions:
reducers plus durability, visibility plus subscriptions, crash recovery plus
indexes, auth plus declared reads, and protocol clients under load.

## Required Hardening Areas

- Reducer transaction atomicity.
- Commitlog replay and snapshot recovery.
- Crash behavior during commit, snapshot, compaction, migration, and shutdown.
- Index consistency after recovery.
- Subscription initial snapshot consistency.
- Subscription delta correctness across inserts, deletes, updates, joins, and
  visibility filters.
- Authorization consistency across local and protocol entry points.
- Protocol robustness against malformed messages, slow clients, oversized
  payloads, compression errors, and disconnect races.
- Concurrency behavior with many reducer calls, queries, and subscriptions.
- Fuzzing for SQL parsing, BSATN decoding, protocol decoding, contract JSON,
  and schema validation.

## Implementation Work

- Convert the hardening gauntlet into concrete test files or command targets.
- Add black-box runtime tests that interact through public APIs and protocol
  messages rather than package internals.
- Add fixed-seed scenario tests for:
  - reducer success/failure
  - restart after every durability boundary
  - subscription setup during concurrent writes
  - visibility changes caused by reducer writes
  - declared query/view auth failures
- Add fuzz targets where parser/decoder boundaries are stable enough.
- Add race-enabled test guidance for packages that should be race-clean.
- Add soak/load tests that can run outside the normal short test loop.
- Record failing seeds and regression scenarios in durable fixtures.

## Verification

Normal verification:

```bash
rtk go test ./...
rtk go vet ./...
```

Hardening verification should also define slower commands for release
qualification. Candidate commands:

```bash
rtk go test -race ./...
rtk go test ./... -run Hardening
```

If the exact commands differ, update this document and the release checklist.

## Done Criteria

- Black-box runtime tests cover the main v1 app workflow.
- Crash/recovery behavior is covered by deterministic tests.
- Subscription correctness has scenario coverage beyond single-table happy
  paths.
- Fuzz targets exist for the stable parser/decoder boundaries.
- Release qualification includes a documented hardening command set.
- Known residual risks are documented with explicit workload limits or non-goals.

## Non-Goals

- Formal verification.
- Proving unsupported SQL shapes.
- Testing SpacetimeDB wire compatibility.
- Requiring every slow soak test to run on every local development cycle.

