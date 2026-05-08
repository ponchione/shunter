# Shunter v1 Roadmap

Status: active implementation driver
Scope: remaining work required before cutting a real `v1.0.0`.

Shunter v1 should stay focused on self-hosted Go applications with
reducer-owned writes, durable state, typed clients, permission-aware reads, and
reliable live updates. Do not broaden v1 into SpacetimeDB wire compatibility,
cloud hosting, dynamic module upload, broad SQL compatibility, or
multi-language client generation.

## Source Of Truth

Use this file for remaining v1 implementation order. Use the focused current
docs below for settled contracts and runbooks:

- `docs/v1-compatibility.md` - stable API, protocol, contract JSON, generated
  TypeScript, read-surface, and host support matrix.
- `docs/authentication.md` and `docs/AUTH-COVERAGE.md` - strict/dev auth
  behavior and current coverage.
- `docs/operations.md` - operator workflow for data dirs, backup/restore,
  migrations, upgrades, and release checklist.
- `docs/RUNTIME-HARDENING-GAUNTLET.md` - release-candidate hardening command
  set and test campaign.
- `docs/PERFORMANCE-BENCHMARKS.md` - benchmark commands, baselines, and
  coverage audit.
- `typescript/client/README.md` - current `@shunter/client` runtime behavior.

## Audit Result

The previous roadmap folder was a mix of settled decisions, implementation
logs, and stale handoffs. This cleanup deleted those files and folded only the
live work back here.

- Contract freeze, SQL/read scope, production auth policy, operations runbook,
  and in-process trust-model decisions are now owned by the current docs above.
- The retired in-repo task-board app proposal is stale. The maintained v1
  canary/reference app remains the external `opsboard-canary` repository.
- Subscription support tracking is complete for the current v1 live-read
  support matrix. Raw subscriptions remain narrower than declared live views,
  unindexed live joins reject before registration, generic path traversal
  pruning is internal, and remaining subscription work belongs to hardening,
  client decoding, and performance envelopes below.
- TypeScript handoff notes were deleted because `typescript/client/README.md`,
  package tests, and this roadmap now carry the active state.

## Current Baseline

The repo already has the core runtime, storage, protocol, schema, subscription,
SQL/read, auth, contract, codegen, backup/restore, migration-hook,
observability, fuzz, benchmark, and gauntlet foundations.

Current v1 decisions already settled:

- `Host` is preview/advanced for v1, not required for normal app development.
- Lower-level runtime packages remain implementation details unless the v1
  compatibility matrix names a stable subset.
- v1 protocol uses the Shunter-native `v1.bsatn.shunter` token and BSATN wire
  frames.
- v1 strict auth is HS256 JWT, issuer/audience allowlist capable,
  restart-based for key replacement, and uses the `permissions` claim.
- Backup and restore are offline-only for v1.
- Shunter uses an in-process app-trust model for reducers, lifecycle hooks,
  scheduled reducers, and migration hooks.
- SQL writes, grouped aggregates, outer joins, subqueries, broad scalar
  functions, and transaction-control SQL are out of scope for v1.

## Remaining Drivers

### 1. TypeScript SDK Completion

The checked-in `typescript/client` runtime already covers connection state,
token handling, IdentityToken decoding, raw reducer/query/view/table request
plumbing, RowList splitting, generated table row decoders, managed table
handles, declared-view row handles when RowList row bytes are available,
acknowledged unsubscribe, opt-in reconnect with resubscription, generated
reducer product codecs, and generated declared-read row decoders/helpers.

Tasks:

- Broaden SDK tests for state transitions, auth failure, reducer/query/view
  success and failure, initial rows, deltas, unsubscribe, reconnect, close
  during in-flight work, and protocol mismatch.
- Wire the external canary app through the public SDK only.

Verification:

```bash
rtk npm --prefix typescript/client run test
rtk go test ./codegen ./...
rtk go vet ./...
```

### 2. External Canary Release Gate

The external `opsboard-canary` repository is the v1 proving ground. Do not add
a duplicate in-repo reference app.

Tasks:

- Keep the canary on public Shunter APIs for normal operation.
- Keep coverage for strict auth, permissions, private/public tables,
  sender-based visibility, reducers, declared queries/views, raw SQL escape
  hatches, subscriptions, restart/rollback, contract export, generated
  TypeScript, offline backup/restore, and one app-owned migration path.
- Replace handwritten client protocol helpers with the public TypeScript SDK
  when the SDK typed surfaces are ready.
- Add canary commands to the release qualification checklist and pin them to
  the intended Shunter commit or tag.

Verification from the canary checkout:

```bash
rtk make canary-quick
rtk make canary-full
```

### 3. Hardening Qualification

The hardening command set exists, but release readiness still needs durable
coverage artifacts and broader fault scenarios.

Tasks:

- Add fixed seed sets and regression corpus entries for gauntlet workloads.
- Extend crash/fault coverage across snapshot, compaction, migration, and
  shutdown boundaries.
- Expand subscription correctness scenarios for joins, deletes, updates,
  visibility changes, caller-specific subscriptions, and concurrent writes.
- Keep race-enabled package guidance current as ownership changes.
- Add soak/load tests that run outside the normal short local loop.

Release-candidate verification is owned by
`docs/RUNTIME-HARDENING-GAUNTLET.md`. Keep this roadmap and that doc aligned
when the command set changes.

### 4. Performance Envelopes

Benchmark coverage exists, but v1 still needs published workload limits.

Tasks:

- Add deterministic small/medium/large fixtures.
- Add or expand benchmarks for reducer throughput, indexed lookup/range scans,
  replay/recovery, restore latency, network-level subscription workloads,
  varied-query fanout, large initial snapshots, memory profiles, and the
  external canary workload.
- Publish an envelope table under `docs/` with command, machine notes, data
  size, commit hash, and whether each threshold is advisory or release-gating.
- Keep app-author indexing guidance aligned with measured scan, predicate,
  subscription, and join limits.

Run raw benchmark commands directly as documented in `RTK.md` and
`docs/PERFORMANCE-BENCHMARKS.md`.

### 5. Release Candidate

Tasks:

- Run the release qualification command set, TypeScript SDK tests, and canary
  gates.
- Resolve or document residual risks with explicit workload limits or
  non-goals.
- Update `CHANGELOG.md` for release-facing behavior.
- Move `VERSION` from `-dev` to the release version only for the release
  commit.
- Build release binaries with linker-stamped `Version`, `Commit`, and `Date`.
- Tag released versions with `vX.Y.Z`.

Exit criteria:

- `README.md`, `docs/README.md`, `docs/v1-compatibility.md`,
  `docs/operations.md`, this roadmap, and the external canary agree on the
  supported v1 story.
- Every stable protocol, contract JSON, generated TypeScript, auth, operation,
  read, and live-update promise has tests, fixtures, or an explicit preview
  boundary.

## Maintenance Rules

- Keep this document short and implementation-facing.
- Delete stale handoffs instead of archiving them.
- Update this file only when remaining v1 work changes.
- Use live code and tests before roadmap prose when they disagree.
- Keep `reference/SpacetimeDB/` read-only and research-only.
