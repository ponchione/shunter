# Functionality Gap To-Do List

This is the active work list for hardening Shunter's current self-hosted v1
runtime. Work it from top to bottom. Every unchecked item is intended to be
completed in this scope.

Current scope:
- Keep Shunter as a self-hosted Go runtime embedded by an application process.
- Harden the existing runtime, protocol, durability, schema, contract, and docs
  surfaces.
- Do not add standalone server/control-plane, managed service, dynamic module
  loading, publish/update, broad SQL, or distributed database behavior here.
- Deferred product and platform expansion work lives in
  `working-docs/deferred-functionality-backlog.md`.

Clean-room rules:
- Do not copy source, structure, comments, tests, or identifiers from the
  ignored `reference/` tree.
- Do not treat reference-runtime wire compatibility, byte-for-byte format
  compatibility, client interoperability, or source compatibility as goals.
- Implement Shunter-owned behavior. Reference-runtime comparisons are only
  capability signals.
- Prefer live Shunter code and tests over specs when they disagree.
- Read `RTK.md` before running commands. Use `rtk` for shell, Go, git, and
  search commands.

Completion rules:
- Finish one numbered item at a time unless two items are explicitly grouped.
- Keep edits inside the named owner packages unless the item says otherwise.
- Add regression tests for behavior changes.
- Run the validation commands listed under the item.
- When an item is complete, change `[ ]` to `[x]` and add a brief `Done:` note
  with changed files and validation commands.
- If an item is blocked by an unexpected code reality, leave it unchecked and
  add a brief `Blocked:` note with the exact blocker.

## Critical Correctness Work

1. [x] Bound JWT issuer and subject sizes.

Done: Added fixed issuer/subject byte limits and `ErrJWTClaimTooLarge` in
`auth/jwt.go`, with exact-limit and limit-plus-one regression tests in
`auth/jwt_test.go`. Validation passed: `rtk go test ./auth`;
`rtk go vet ./auth`.

Owner: `auth`

Read first:
- `auth/jwt.go`
- Existing `auth` JWT tests
- Protocol auth tests only if protocol error mapping changes

Required work:
- Add Shunter-owned byte limits for required JWT `iss` and `sub` claims.
- Use byte length with `len(string)`, not rune count.
- Use deterministic defaults of `MaxIssuerBytes = 1024` and
  `MaxSubjectBytes = 1024`.
- Add an error sentinel such as `ErrJWTClaimTooLarge` so tests and callers can
  use `errors.Is`.
- Validate `sub` and `iss` after existing required-claim checks and before
  issuer allowlist matching, `hex_identity` derivation, `Claims.Principal`, or
  any returned `Claims` assignment.
- Add exact-limit accepted and limit-plus-one rejected tests for both claims.

Do not:
- Do not add OIDC, JWKS, asymmetric keys, key rotation, or extra-claim
  propagation here.
- Do not change identity derivation.
- Do not make the limits root-runtime configurable in this item.

Done when:
- Tokens with `iss` and `sub` of exactly 1024 bytes validate when other policy
  checks pass.
- Tokens with either claim at 1025 bytes fail with `ErrJWTClaimTooLarge`.
- Oversized claim tests do not log or assert on the full oversized string.

Validation:
- `rtk go test ./auth`
- `rtk go vet ./auth`
- `rtk go test ./protocol` only if protocol auth behavior changes

2. [x] Reject multiple `AutoIncrement` columns per table.

Done: Added `ErrMultipleAutoIncrement` in `schema/errors.go`, table-level
auto-increment counting in `schema/validate_structure.go`, and a regression in
`schema/audit_regression_test.go` for two keyed auto-increment columns.
Validation passed: `rtk go test ./schema`; `rtk go vet ./schema`.

Owner: `schema`

Read first:
- `schema/validate_structure.go`
- `schema/errors.go`
- `schema/audit_regression_test.go`
- `store/table.go` and `store/transaction.go` for downstream context only

Required work:
- Add a schema error sentinel such as `ErrMultipleAutoIncrement`.
- Count `AutoIncrement` columns per table in `validateStructure`.
- Reject more than one auto-increment column even when every flagged column is
  integer, non-null, and backed by either a primary key or single-column unique
  index.
- Keep existing checks for nullable, non-integer, and unkeyed auto-increment
  columns.
- Add a regression test with two otherwise valid keyed auto-increment columns.
- Keep `sys_scheduled` valid; it has exactly one auto-increment column.

Do not:
- Do not add multi-sequence support.
- Do not edit `store`, `commitlog`, snapshot recovery, or contract export.
- Do not loosen primary-key or unique-index requirements for the one allowed
  auto-increment column.

Done when:
- Single auto-increment tables still build.
- Multiple auto-increment columns fail deterministically at schema build time.
- Invalid schemas no longer reach downstream sequence code.

Validation:
- `rtk go test ./schema`
- `rtk go vet ./schema`

3. [x] Reject invalid UTF-8 string values before persistence.

Done: Added `types.ErrInvalidUTF8` and UTF-8 validation helpers in
`types/value.go`; made `store/validate.go` reject invalid `String` and
`ArrayString` values by column; made `bsatn/encode.go` reject invalid direct
string encoding with rollback; added regressions in `types/value_test.go`,
`store/store_test.go`, and `bsatn/bsatn_test.go`. Validation passed:
`rtk go test ./types ./store ./bsatn`; `rtk go vet ./types ./store ./bsatn`;
`rtk go test ./...`.

Owner: `types`, `store`, `bsatn`

Read first:
- `types/value.go`
- `types/value_test.go`
- `store/validate.go`
- `bsatn/encode.go`
- `bsatn/decode.go`
- Existing BSATN invalid-string decode tests

Required work:
- Add a `types` error sentinel such as `ErrInvalidUTF8`.
- Keep `NewString`, `NewArrayString`, and owned-constructor signatures stable
  for this first fix.
- Add validation helpers in `types`.
- Make `store.ValidateRow` reject invalid UTF-8 for `KindString` and every
  `KindArrayString` element. The error should wrap `types.ErrInvalidUTF8` and
  name the affected column.
- Make `bsatn.AppendValue` and `EncodeValue` reject invalid UTF-8 for direct
  value encoding so callers cannot bypass row validation and produce
  undecodable payloads.
- Preserve `bsatn.DecodeValue` invalid-string rejection.
- Add tests for invalid standalone strings, invalid array-string elements, row
  validation failure, and encode failure.

Do not:
- Do not change constructor return types.
- Do not normalize, replace, or repair invalid UTF-8. Reject it.
- Do not change bytes or JSON validation as a side effect.
- Do not add nested values, identity column types, or duration API changes.

Done when:
- Reducer/store inserts with invalid UTF-8 string data fail before commit.
- BSATN encoding invalid string values returns an error and keeps existing
  rollback-on-error buffer behavior.
- Valid Unicode strings still round-trip.

Validation:
- `rtk go test ./types ./store ./bsatn`
- `rtk go vet ./types ./store ./bsatn`
- `rtk go test ./...` if validation changes affect protocol, contract, or root
  runtime behavior

4. [x] Prevent invalid scheduled reducer retry loops.

Done: Wired `schedulerHandle` target validation through `ReducerRegistry` in
`executor/scheduler.go`, rejecting unknown reducers with `ErrReducerNotFound`
and lifecycle targets with `ErrLifecycleReducer` before schedule ID allocation;
added startup cleanup for invalid recovered `sys_scheduled` rows before replay;
updated scheduler/startup/replay tests in `executor/*_test.go` to cover invalid
targets, ID preservation, and recovered-row cleanup. Validation passed:
`rtk go test ./executor`; `rtk go vet ./executor`; `rtk go test ./...`.

Owner: `executor`

Read first:
- `executor/scheduler.go`
- `executor/executor.go`
- `executor/registry.go`
- `executor/scheduler_test.go`
- `executor/scheduler_firing_test.go`
- `executor/startup_test.go`

Required work:
- Validate scheduled reducer targets when `Schedule` and `ScheduleRepeat`
  create rows.
- The target must exist in `ReducerRegistry` and must be a normal reducer with
  `LifecycleNone`.
- Validate before consuming a schedule ID or inserting into `sys_scheduled`.
- Preserve existing `ErrInvalidScheduleInterval` behavior for non-positive
  repeat intervals.
- Return errors wrapping `ErrReducerNotFound` for unknown reducer names and
  `ErrLifecycleReducer` for lifecycle targets such as `OnConnect` and
  `OnDisconnect`.
- Add tests proving invalid schedule attempts insert no rows and do not consume
  a schedule ID.
- Handle recovered bad `sys_scheduled` rows so unknown or lifecycle targets do
  not requeue forever. Delete, quarantine, or back off permanently invalid rows
  through a Shunter-owned path, and test the chosen behavior.
- Preserve retry semantics for user reducer errors and reducer panics.

Do not:
- Do not redesign scheduled tables, typed scheduled procedures, volatile
  scheduling, or exported schedule metadata.
- Do not run lifecycle reducers through the scheduled reducer path.
- Do not change crash-recovered `OnDisconnect` semantics.
- Do not make reducer lookup case-insensitive.

Done when:
- New schedules for unknown reducers and lifecycle reducers fail before commit.
- Recovered bad schedule rows cannot create a tight retry loop.
- User-error and panic schedule tests still prove retryable rows.
- Valid one-shot and repeating schedules keep fixed-rate semantics.

Validation:
- `rtk go test ./executor`
- `rtk go vet ./executor`
- `rtk go test ./...` if reducer registry interfaces or root scheduler wiring
  change

5. [x] Make contract diffing compare durable schema identity.

Done: Added durable table/column/index identity fields to `schema.SchemaExport`
and root contract copy/validation helpers; split product-schema columns from
table columns; made `contractdiff` treat table/column/index order, IDs, column
indexes, and `AutoIncrement` changes as breaking; updated metamorphic coverage,
contract/codegen JSON fixtures, and root/schema/contractdiff regressions.
Audit follow-up: strengthened `contract_validate.go` to reject invalid
contract `AutoIncrement` metadata and added regressions in `contract_test.go`.
Validation passed: `rtk go test ./contractdiff`;
`rtk go test ./contractworkflow ./cmd/shunter`; `rtk go test . ./schema`;
`rtk go vet ./contractdiff ./schema .`; `rtk go test ./...`.

Owner: `contractdiff`, `schema`, root contract/export helpers

Read first:
- `contractdiff/contractdiff.go`
- `contractdiff/metamorphic_test.go`
- `contractdiff/plan.go`
- `schema/export.go`
- `schema/types.go`
- `contract.go`
- `contract_validate.go`
- Root contract/export tests

Required work:
- Align `ModuleContract` and `SchemaExport` with runtime durable schema
  identity. Do not close this by saying contractdiff is advisory.
- Surface durable identity fields needed by diff tooling: table IDs, column
  indexes, column `AutoIncrement`, index IDs, and index column ordinals as
  represented by live `schema.TableSchema`.
- Preserve existing human-friendly names and column-name lists used by codegen
  and diagnostics.
- Update contract validation and deep-copy helpers for new exported fields.
- Compare table order/ID, column order/index, index order/ID, and
  `AutoIncrement` flag changes as breaking durable schema changes.
- Adjust metamorphic tests so order invariance still applies to declaration
  lists that are truly order-insensitive, but not to durable table, column, or
  index identity.
- Add direct regression tests for table reorder, column reorder, index reorder,
  and auto-increment flag changes through both `Compare` and `Plan`.

Do not:
- Do not implement an executable migration planner.
- Do not make runtime snapshot compatibility looser to match old contractdiff
  behavior.
- Do not hide durable identity fields from JSON if contractdiff depends on JSON
  artifacts as a release gate.
- Do not add CLI publish/update or control-plane behavior here.

Done when:
- A contract generated from schema reorder differs in `Compare` and produces
  blocking or breaking plan output.
- An `AutoIncrement` flag change differs in `Compare` and produces blocking or
  breaking plan output.
- Existing additive/breaking classifications for name-visible changes remain
  deterministic.

Validation:
- `rtk go test ./contractdiff`
- `rtk go test ./contractworkflow ./cmd/shunter` if contract workflow text,
  JSON, or CLI output changes
- `rtk go test . ./schema` if schema export or root contract export changes
- `rtk go vet ./contractdiff ./schema .` when exported contract structs change

6. [x] Use offset indexes for safe snapshot-covered startup recovery.

Done: Added recovery-only indexed snapshot-boundary scanning in
`commitlog/recovery_index_scan.go`, leaving `ScanSegments` linear for fallback
and genesis recovery; refactored segment listing/continuity helpers in
`commitlog/segment_scan.go`; wired recovery report construction through the new
selection path in `commitlog/recovery.go`; added regressions in
`commitlog/recovery_offset_index_test.go` for valid indexed snapshot-covered
prefix skips and missing/corrupt/non-monotonic/stale sidecar fallback.
Validation passed: `rtk go test ./commitlog`; `rtk go vet ./commitlog`;
`rtk go test ./...`.

Owner: `commitlog`

Read first:
- `commitlog/segment_scan.go`
- `commitlog/recovery.go`
- `commitlog/offset_index.go`
- `commitlog/rapid_replay_test.go`
- Recovery and segment-scan tests covering damaged tails and index fallback

Required work:
- Keep the existing full linear validation path for no-snapshot recovery,
  missing indexes, corrupt indexes, stale indexes, and any case where an
  indexed shortcut cannot be proven safe.
- Use offset indexes only as advisory accelerators after recovery has selected
  or is evaluating a valid snapshot boundary.
- Validate any chosen indexed record by seeking to the indexed offset, reading
  the segment record, checking TxID/order expectations, and scanning the suffix
  needed for durable-horizon, damaged-tail, and resume-plan metadata.
- Preserve `SegmentInfo`, `AppendMode`, damaged-tail reporting, history-gap
  errors, and resume-plan behavior for existing callers.
- Add a benchmark or regression fixture with large retained logs, a snapshot
  beyond early records, and valid `*.idx` sidecars.
- Add fallback tests proving corrupt, missing, non-monotonic, or stale sidecars
  recover through the current linear path without data loss.

Do not:
- Do not implement raw commitlog streaming, mirroring append, compression,
  preallocation, size accounting, or import/export APIs.
- Do not make `ScanSegments` trust an offset index for recovery from genesis.
- Do not weaken checksum, record-type, flag, TxID-contiguity, or damaged-tail
  validation for records that recovery may replay.

Done when:
- Existing recovery tests pass without behavior regressions.
- A new test or benchmark demonstrates snapshot-covered prefixes can avoid full
  record-by-record scanning when valid indexes are present.
- Corrupt or stale sidecars are advisory failures unless the segment is invalid
  under existing rules.

Validation:
- `rtk go test ./commitlog`
- `rtk go vet ./commitlog`
- `rtk go test ./...` if recovery report fields, snapshot selection, or root
  runtime recovery behavior changes

## Documentation and Stale-Guidance Cleanup

7. [x] Update SPEC-002 offset-index guidance.

Done: Updated `working-docs/specs/002-commitlog/SPEC-002-commitlog.md`
§12.1 to describe advisory per-segment `*.idx` sidecars, replay/startup
snapshot-boundary acceleration, and linear fallback for unsafe or absent
indexes. Validation passed: `rtk git diff --check -- working-docs/specs`.

Owner: `working-docs/specs` and `commitlog` docs

Required work:
- Find the SPEC-002 section that still says Shunter has no offset index.
- Update it to reflect live code: per-segment `*.idx` sidecars exist, replay can
  seek with them after a selected snapshot, and index failures fall back to
  linear scan.
- Keep the wording implementation-facing and concise.

Do not:
- Do not describe offset indexes as required for correctness.
- Do not claim reference-runtime format compatibility.

Validation:
- `rtk git diff --check -- working-docs/specs`

8. [x] Update SPEC-005 SQL read-surface guidance.

Done: Updated `working-docs/specs/005-protocol/SPEC-005-protocol.md`
§7.1.1, §7.4, and §16 to describe the shared SQL parser, per-read-surface
feature gates, table-shaped raw subscription limits, current one-off/declared
read support, declared parameters, and SQL non-goals. Audit follow-up:
clarified declared live-view `ORDER BY`/`LIMIT`/`OFFSET` gating as
single-table, non-aggregate, initial-snapshot-only behavior. Validation passed:
`rtk git diff --check -- working-docs/specs`.

Owner: `working-docs/specs`, `query/sql`, `internal/queryplan`, `protocol`

Required work:
- Update the stale SQL subset text to match live code.
- Document that live code gates features by read surface.
- Include current support for aliases, qualified stars, projections,
  `COUNT`/`SUM`, joins including multi-way/self/cross joins, boolean/null
  predicates, `ORDER BY`, `LIMIT`, `OFFSET`, quoted identifiers, `:sender`, and
  declared parameters.
- State that raw subscriptions remain table-shaped and reject projections,
  aggregates, ordering, limits, and offsets.
- State current non-goals: DML, `SET`/`SHOW`, scalar functions, arithmetic,
  subqueries, set operations, outer/natural joins, group/having, JSON-path,
  full-text, transactions, and procedures.

Do not:
- Do not add parser features while updating docs.
- Do not describe not-yet-implemented DML/admin SQL as implemented.

Validation:
- `rtk git diff --check -- working-docs/specs`

9. [x] Update the stale visibility-filter implementation comment.

Done: Updated the `VisibilityFilterDeclaration` comment in
`visibility_filters.go` to state that validated visibility metadata already
applies to raw one-off reads, raw subscriptions, declared queries, and declared
views. Validation passed: `rtk go test ./protocol`; `rtk go vet ./protocol`.

Owner: `protocol`

Required work:
- Find the comment in `visibility_filters.go` that says later work applies
  stored filter metadata to read execution.
- Update it to match live behavior: visibility filters already apply to raw
  one-off reads, raw subscriptions, declared queries, and declared views.

Do not:
- Do not change visibility semantics in this documentation-only item.

Validation:
- `rtk go test ./protocol`
- `rtk go vet ./protocol`

10. [x] Reconcile declared live-view documentation with current v1 behavior.

Done: Updated `docs/reference/read-surface.md` and
`docs/how-to/reads-queries-views.md` to document declared live-view joins,
multi-way joins, `COUNT`/`SUM` aggregate views including multi-way aggregates,
and initial-snapshot-only `ORDER BY`/`LIMIT`/`OFFSET` behavior with no
maintained top-N/windowed live views. Audit follow-up: clarified that
`ORDER BY`/`LIMIT`/`OFFSET` are supported only for single-table,
non-aggregate declared live views. Validation passed:
`rtk git diff --check -- docs`.

Owner: docs under `docs/reference` and `docs/how-to`

Required work:
- Check live declared-view support for joins, multi-way joins, `COUNT`, `SUM`,
  and multi-way aggregate live views.
- Update `docs/reference/read-surface.md` and
  `docs/how-to/reads-queries-views.md` to describe the current implemented
  behavior.
- Make the docs explicit that `ORDER BY`, `LIMIT`, and `OFFSET` affect initial
  snapshots only.
- Make the docs explicit that maintained top-N/windowed live views are not
  implemented.

Do not:
- Do not imply maintained top-N or maintained windowed live views exist.
- Do not change code in this documentation item.

Validation:
- `rtk git diff --check -- docs`

## Explicit Non-Goals For This Active List

These are not active work items for this list:
- Reference-runtime wire compatibility.
- Reference-runtime byte-for-byte format compatibility.
- Reference client interoperability.
- Source compatibility with the reference runtime.
- Managed cloud hosting.
- Distributed database behavior.
- Dynamic module hosting.
- Publish/update workflows.
- Standalone server/control-plane work.
- SQL DML/admin statements.
- Maintained live top-N/windowed views.
- Planner-level cross-table visibility/RLS.
- Production OIDC/JWKS/asymmetric-key auth.
- Commitlog mirroring/compression/streaming.
- Rich nested schema/value types.
