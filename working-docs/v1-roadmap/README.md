# Shunter v1 Roadmap

Status: active v1 release qualification driver
Scope: remaining release work, qualification gates, canary expectations,
hardening follow-up, and performance envelope maintenance.

This file is the entry point for continuing v1 work. It is not a status
archive. Keep settled compatibility, auth, operations, and performance details
in the focused docs listed below, and keep this roadmap limited to remaining
drivers and release gates.

Shunter v1 stays focused on self-hosted Go applications with reducer-owned
writes, durable state, typed clients, permission-aware reads, and reliable live
updates. Do not broaden v1 into SpacetimeDB wire compatibility, cloud hosting,
dynamic module upload, broad SQL compatibility, SQL writes, or multi-language
client generation.

## Source Of Truth

Resolve disagreements in this order:

1. Task-specific user or release instruction.
2. Live code and tests.
3. Focused docs for the surface being changed.
4. Numbered specs only when a cross-subsystem contract needs them.
5. `README.md` for product intent.

Focused docs:

- `docs/v1-compatibility.md` - stable API, protocol, contract JSON, generated
  TypeScript, read-surface, and host support matrix.
- `docs/authentication.md` - strict/dev auth behavior for app authors and
  operators.
- `docs/operations.md` - operator workflow for data dirs, backup/restore,
  migrations, upgrades, and release checklist.
- `docs/performance-envelopes.md` - current advisory benchmark snapshot and
  known measurement gaps.
- `typescript/client/README.md` - current `@shunter/client` runtime behavior.
- `working-docs/shunter-design-decisions.md` - implementation-facing decisions
  that code and tests still cite.
- `working-docs/specs/` - numbered subsystem contracts for targeted
  cross-subsystem questions.

The maintained v1 canary/reference app is the external `opsboard-canary`
repository. Do not add a duplicate in-repo reference app.

## V1 Boundaries

- `Host` is preview/advanced for v1, not required for normal app development.
- Lower-level runtime packages remain implementation details unless
  `docs/v1-compatibility.md` names a stable subset.
- v1 protocol uses the Shunter-native `v1.bsatn.shunter` token and BSATN wire
  frames.
- Strict auth is HS256 JWT with issuer/audience allowlists, restart-based key
  replacement, and permission admission through the `permissions` claim.
- Backup and restore are offline-only for v1.
- Shunter uses an in-process app-trust model for reducers, lifecycle hooks,
  scheduled reducers, and migration hooks.

## Remaining Drivers

### 1. TypeScript SDK Local Package Foundation

The v1 TypeScript target is a release-quality package-shaped SDK, not a
repo-internal runtime copied by downstream apps. The default v1 distribution
path is private local/workspace use through `file:` or workspace dependencies,
not public npm publishing. This keeps the frontend integration clean without
requiring npm account, organization, namespace, or public supply-chain
decisions for a single-user v1.

Use SpacetimeDB's TypeScript package shape as a quality reference for package
boundaries, generated-binding integration, and install-time ergonomics, while
preserving Shunter's narrower v1 scope and Shunter-native protocol.

The package should be strong enough that generated bindings and the external
canary can depend on it the same way private Shunter applications will. React
and other framework adapters remain post-v1 unless explicitly pulled into the
release, but the package layout should leave room for future subpaths without
forcing a breaking package reshape.

Work order:

1. Package shape:
   Make `typescript/client` a private package-shaped SDK. Use a stable local
   import name, initially `@shunter/client` with `"private": true`, even if the
   public npm scope is unavailable. Public npm publishing is optional/post-v1
   and requires choosing an owned scope first.
2. Build artifacts:
   Ship built JavaScript plus `.d.ts` output, source maps where useful, and a
   narrow package `files` list. Prefer a clean ESM-first package with
   browser/import/default export conditions; add CJS only if the compatibility
   work justifies owning it.
3. Public exports:
   Keep the root export focused on the stable v1 client runtime. Reserve the
   package layout for future framework subpaths, but do not expose adapter
   subpaths until those adapters are supported.
4. Generated-binding integration:
   Keep the stable local package name as the default generated runtime import.
   Add a TypeScript codegen option for the runtime import specifier so
   downstream projects can import from an app-scoped package path, an owned
   future npm scope, or a direct vendored path if needed.
5. Generated metadata:
   Export generated-contract metadata alongside `shunterProtocol` so clients
   can detect stale bindings and wrong-module or wrong-protocol pairings.
   Include at least contract format/version, module name/version, and protocol
   metadata.
6. Runtime ergonomics:
   Make the generated TypeScript API ergonomic around the stable v1 surfaces:
   reducer calls and result decoding, declared queries, declared live views,
   table handles, RowList decoding, protocol errors, auth/token handling,
   connection lifecycle, acknowledged unsubscribe, opt-in reconnect, and
   resubscription cache boundaries.
7. Package smoke tests:
   Add a fixture that builds, packs, installs from a local tarball or `file:`
   dependency, and imports the SDK without reaching into repo source. The
   fixture should also compile generated bindings that import the packaged
   runtime.
8. Canary path:
   Keep the external canary app wired through the package-shaped SDK only,
   using the same local tarball, workspace, or `file:` install path expected for
   private downstream apps before v1 is cut.
9. Documentation:
   Document the supported runtime target, local/workspace install path,
   reconnect semantics, stale-binding checks, version mapping, and explicit v1
   non-goals.

Supported target:

- Browser and Electron renderer environments with standard Web APIs.
- Non-browser hosts are responsible for providing a compatible
  `webSocketFactory` when global `WebSocket` is absent.

Private downstream install path:

- Vendor the SDK runtime from a pinned Shunter release.
- Install it through a workspace, local tarball, or `file:` dependency that
  still resolves as the stable local package name.
- Generate bindings against the default import or an explicitly configured
  local import specifier.

Future public npm path:

- Public npm publishing is not required for v1.
- Before publishing publicly, choose and control an owned package scope instead
  of relying on the unavailable `shunter` npm organization.
- If the public package name changes, use the generated runtime import option
  to transition apps deliberately.

Reconnect contract:

- Reconnect and resubscription are opt-in.
- Disconnected intervals are cache boundaries.
- Clients that need a fresh authoritative view should re-read or use the
  replayed initial snapshot after reconnect rather than assuming continuous
  delta delivery across the gap.

V1 non-goals:

- Framework adapters such as React hooks.
- Server-side SDK APIs.
- Broad Node/Deno/Bun/Workers support matrix.
- SpacetimeDB client API or wire compatibility.

Acceptance criteria:

- `npm pack` contains only intended package artifacts and metadata.
- A clean fixture app can install the packed package through a local tarball,
  workspace, or `file:` dependency and import it without repo-relative paths.
- Generated TypeScript imports the stable local package name by default and can
  compile against an override runtime import specifier.
- Generated bindings expose contract/protocol metadata for compatibility
  checks.
- SDK tests cover connection lifecycle, auth token handling, reducer calls,
  declared queries/views, table handles, RowList decoding, unsubscribe
  acknowledgement, reconnect/resubscription, and protocol error handling.
- The external canary or an equivalent smoke fixture exercises the packaged SDK
  through generated bindings.

Verification:

```bash
rtk npm --prefix typescript/client run test
rtk npm --prefix typescript/client run build
rtk npm --prefix typescript/client pack --dry-run
rtk go test ./codegen
```

### 2. External Canary Release Gate

The external `opsboard-canary` repository is the v1 proving ground.

Tasks:

- Keep the canary on public Shunter APIs for normal operation.
- Keep coverage for strict auth, permissions, private/public tables,
  sender-based visibility, reducers, declared queries/views, raw SQL escape
  hatches, subscriptions, restart/rollback, contract export, generated
  TypeScript, offline backup/restore, and one app-owned migration path.
- Record the Shunter commit or release tag under test and the
  `opsboard-canary` commit used for each release qualification run.

Verification from the canary checkout:

```bash
rtk make canary-quick
rtk make canary-full
```

### 3. Hardening Qualification

Keep hardening work focused on reusable, reproducible failures. New failures
should leave behind a seed, corpus entry, trace, command, or fixture.

Tasks:

- Continue adding regression corpus entries to the named gauntlet seed sets in
  `gauntlet_seed_corpus_test.go` as new failures are found.
- Extend crash/fault coverage across snapshot, compaction, migration, and
  shutdown boundaries.
- Expand subscription correctness scenarios for joins, deletes, updates,
  visibility changes, caller-specific subscriptions, and concurrent writes.
- Keep race-enabled package guidance current as ownership changes.
- Add soak/load tests that run outside the normal short local loop.

Release-candidate hardening commands:

```bash
rtk go test ./... -count=1
rtk go vet ./...
rtk go tool staticcheck ./...
rtk go test ./internal/gauntlettests -run 'RuntimeGauntlet|ReleaseCandidateExampleApp' -count=1
rtk go test ./... -run 'RuntimeGauntlet|ReleaseCandidateExampleApp|ShortSoak' -count=1
rtk go test -race . ./executor ./protocol ./subscription ./store ./commitlog -count=1
```

Run the race set whenever a slice changes runtime concurrency, reducer
execution, protocol connection lifecycle, subscription fanout/pruning, store
index mutation, commitlog recovery, snapshot, or compaction behavior.

Fuzz corpus replay is part of normal `rtk go test ./...`. Active fuzzing should
run package-at-a-time so failures are attributable. Active fuzz targets live in
`auth`, `bsatn`, `protocol`, `commitlog`, `schema`, `codegen`,
`contractdiff`, and `subscription`.

### 4. Performance Envelopes

`docs/performance-envelopes.md` owns the current advisory benchmark snapshot.
This roadmap should track only the remaining measurement work and the commands
needed to refresh the snapshot.

Tasks:

- Keep `docs/performance-envelopes.md` current with command, machine notes,
  data size, commit hash, and whether each row is advisory or release-gating.
- Fill remaining deterministic small/medium/large fixtures where benchmark
  coverage still depends on one-off local shapes.
- Add or expand benchmarks for indexed lookup/range scans, slow-reader
  WebSocket writer/write-timeout backpressure paths, workload-derived or
  canary fanout distributions, app-level/canary reducer throughput, external
  canary workload timing, canary-scale backup/restore timing, and
  production-sized memory profiles.
- Keep app-author indexing guidance aligned with measured scan, predicate,
  subscription, and join limits.
- Define hard performance thresholds only when there is enough measured history
  to make them release-gating rather than advisory.

Run Go benchmarks directly, not through RTK:

```bash
go test -run '^$' -bench . -benchmem . ./executor ./protocol ./commitlog ./subscription
go test -run '^$' -bench . -benchmem -count=10 . ./executor ./protocol ./commitlog ./subscription > /tmp/shunter-v1.0.0-bench.txt
```

RTK may summarize benchmark commands and suppress the raw
`Benchmark... ns/op B/op allocs/op` rows. Normal tests, vet, staticcheck, git,
and shell commands still use RTK.

### 5. Release Candidate

Tasks:

- Run the hardening command set, TypeScript SDK tests, benchmark refresh, and
  canary gates.
- Resolve or document residual risks with explicit workload limits or
  non-goals.
- Update `CHANGELOG.md` for release-facing behavior.
- Move `VERSION` from `-dev` to the release version only for the release
  commit.
- Build release binaries with linker-stamped `Version`, `Commit`, and `Date`.
- Tag released versions with `vX.Y.Z`.

Exit criteria:

- `README.md`, `docs/README.md`, `docs/v1-compatibility.md`,
  `docs/authentication.md`, `docs/operations.md`,
  `docs/performance-envelopes.md`, this roadmap, and the external canary agree
  on the supported v1 story.
- The TypeScript SDK can be installed from a local tarball, workspace, or
  `file:` dependency under the stable local package name, imported by generated
  bindings without repo-relative paths, and exercised through the external
  canary or an equivalent package-install smoke fixture.
- Every stable protocol, contract JSON, generated TypeScript, auth, operation,
  read, and live-update promise has tests, fixtures, or an explicit preview
  boundary.
- Failures from hardening, fuzzing, and canary runs are reproducible from a
  seed, trace, command, or fixture.

## Maintenance Rules

- Keep this document implementation-facing.
- Delete stale handoffs instead of archiving them.
- Update this file only when remaining v1 work, release gates, hardening, or
  performance status changes.
- Keep current coverage summaries in focused docs, not in this roadmap.
- Use live code and tests before roadmap prose when they disagree.
- Keep `reference/SpacetimeDB/` read-only and research-only.
