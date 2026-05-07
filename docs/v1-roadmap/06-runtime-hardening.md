# Runtime Hardening

Status: open, partial gauntlet/fuzz coverage landed
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
the same as complete black-box runtime proof.

Current code reality:

- Root-level gauntlet tests exercise seeded reducer/read workloads, restart
  equivalence, snapshot isolation, close behavior, protocol lifecycle, strict
  auth, scheduler behavior, read authorization, malformed and oversized
  protocol frames, restart loops, storage faults, and subprocess crash recovery.
- `rc_app_workload_test.go` provides a release-candidate style public-runtime
  workload, but it is not a maintained user-facing reference app.
- Fuzz targets exist across JWT validation, BSATN decoding, protocol decoding,
  contract JSON, contract diff/plan JSON, schema build/read-policy handling,
  commitlog decoding/recovery, subscription hashing, observability config, and
  ID parsing.

The remaining risk is in interactions that are not yet systematically covered
by a release qualification harness: reducers plus durability, visibility plus
subscriptions, crash recovery plus indexes, auth plus declared reads, protocol
clients under load, and long-running soak workloads.

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

Completed or partially complete:

- Convert major parts of the hardening gauntlet into concrete root-level test
  files.
- Add black-box runtime tests that interact through public APIs and protocol
  messages rather than package internals.
- Add fixed-seed scenario tests for:
  - reducer success/failure
  - restart after several durability and protocol boundaries
  - subscription setup during concurrent writes
  - visibility and read-authorization behavior
  - declared query/view auth failures
- Add fuzz targets for many parser/decoder and contract boundaries.

Remaining:

- Extend crash/recovery coverage to every durability boundary called out by the
  gauntlet, including snapshot, compaction, migration, and shutdown faults.
- Add broader subscription correctness scenarios for joins, deletes, updates,
  and visibility changes under concurrent writes.
- Add race-enabled test guidance for packages that should be race-clean.
- Add soak/load tests that can run outside the normal short test loop.
- Record fixed seed sets, failing seeds, and regression scenarios in durable
  fixtures or corpus entries.

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
