# TECH-DEBT

This file tracks grounded implementation/spec drift and audit findings discovered during code-vs-spec review.

Status conventions:
- open: confirmed mismatch or missing coverage
- resolved: fixed in code and/or docs
- doc-drift: implementation is acceptable, docs should be updated

## Audit phase plan

Current planned audit sequence follows `docs/EXECUTION-ORDER.md` Phase 1 foundation order, keeping the intentional contract-slice exceptions:
1. `SPEC-001 E1` Core Value Types — audited
2. `SPEC-006 E2` Struct Tag Parser — audited
3. `SPEC-006 E1` Schema Types & Type Mapping — audited
4. `SPEC-003 E1.1 + E1.2 + minimal E1.4 contract slice` — audited for early-gate sufficiency
5. `SPEC-006 E3.1` Builder core — audited
6. `SPEC-006 E4` Reflection-path registration — audited
7. `SPEC-006 E3.2` Reducer registration — audited
8. `SPEC-006 E5` Validation/Build/SchemaRegistry — in progress; confirmed gaps now include missing Story 5.6 schema compatibility checking at startup and mutable `SchemaRegistry` table lookups that violate the read-only contract
9. `SPEC-006 E6` Schema export — audited
10. `SPEC-001 E2` Schema & table storage — audited
11. `SPEC-001 E3` B-tree index engine — audited
12. `SPEC-001 E4` Table indexes & constraints — audited
13. `SPEC-001 E5` Transaction layer — audited
14. `SPEC-001 E6` Commit, rollback & changeset — audited
15. `SPEC-001 E7` Read-only snapshots — audited
16. `SPEC-001 E8` Auto-increment & recovery — audited
17. `SPEC-002 E1` BSATN codec — audited
18. `SPEC-002 E2` Record format & segment I/O — audited
19. `SPEC-003 E2` Reducer Registry — audited
20. `SPEC-003 E3` Executor Core — audited
21. `SPEC-003 E4` Reducer Transaction Lifecycle — audited
22. `SPEC-002 E3` Changeset Codec — audited
23. `SPEC-002 E5` Snapshot I/O — audited
24. `SPEC-002 E4` Durability Worker — audited

Audit notes:
- `SPEC-006 E2` (`schema/tag.go`, `schema/tag_test.go`) appears operationally aligned with the tag-parser stories. No new debt logged from that slice at this time.
- `SPEC-006 E1` is mostly aligned operationally (`schema/types.go`, `schema/typemap.go`, `schema/naming.go`, `schema/valuekind_export.go`), but one concrete contract gap was found and logged below: no live `ErrSequenceOverflow` sentinel is defined anywhere even though the spec/decomposition assigns that contract to this foundation slice.
- The narrowed `SPEC-003` Phase-1 contract slice (`E1.1 + E1.2 + minimal E1.4`) appears operationally present enough for early dependency gating: foundation enums/IDs exist, reducer request/response types exist, and a minimal scheduler interface shell exists. The remaining meaningful executor gap is still the broader Epic 1 surface already tracked as `TD-002`, rather than a new blocker inside the intentionally narrowed slice.
- `SPEC-006 E3.1` builder core appears operationally aligned: `NewBuilder`, `TableDef`, `SchemaVersion`, `EngineOptions`, and chaining behavior are implemented and covered by tests. I have not logged a separate builder-core debt item from that slice.
- `SPEC-006 E4` reflection-path registration is mostly present (`schema/reflect.go`, `schema/reflect_build.go`, `schema/register_table.go`), but one concrete contract gap was found and logged below: anonymous embedded fields are processed before `shunter:"-"` exclusion, so excluded embedded structs are still flattened and excluded embedded pointer-to-struct fields still error.
- `SPEC-006 E3.2` reducer registration is functionally present (`schema/builder.go`, `schema/validate_schema.go`, `schema/registry.go`), but one API-surface gap was found and logged below: the schema package does not expose `ReducerHandler` / `ReducerContext` aliases even though SPEC-006 presents reducer registration as part of the schema-facing API surface.
- `SPEC-006 E5` validation/build work is largely present, but two concrete contract gaps are now confirmed: Story 5.6 startup schema compatibility checking is missing entirely, and Story 5.4's read-only `SchemaRegistry` contract is violated because `Table(...)` / `TableByName(...)` return mutable pointers into internal state.
- `SPEC-006 E6` schema export is not implemented at all in live code: there is no `schema/export.go`, no export value types, no `Engine.ExportSchema()`, no JSON-contract tests, and no `cmd/shunter-codegen` tool surface. I logged the primary engine-surface gap below.
- `SPEC-001 E2` schema-backed table storage is operationally present (`store/table.go`, `store/validate.go`, `store/store_test.go`), but one important contract gap was found and logged below: inserted rows are not detached from caller-owned `ProductValue` slices, so stored rows remain externally mutable.
- `SPEC-001 E3` B-tree index engine is mostly present (`store/index_key.go`, `store/btree_index.go`, related tests), but Story 3.1's public `Bound` helper contract is entirely missing even though later range semantics docs still reference it. I logged the concrete API-surface gap below.
- `SPEC-001 E4` table indexes & constraints appear operationally aligned, but one concrete spec-vs-implementation drift item was found: Story 4.1 documents an `Index.unique` field, while the live implementation derives uniqueness solely from `IndexSchema.Unique` and omits the redundant field. I logged that below as doc drift rather than a product bug.
- `SPEC-001 E5` transaction-layer behavior is mostly present (`store/committed_state.go`, `store/tx_state.go`, `store/transaction.go`, `store/snapshot.go`, related tests), but one concrete contract gap was found and logged below: Story 5.3's public `StateView` surface is entirely missing even though transaction behavior currently inlines some of that logic.
- `SPEC-001 E6` commit/changeset behavior is mostly present (`store/changeset.go`, `store/commit.go`, related tests), but one concrete rollback contract gap was found and logged below: `Rollback` marks a flag that is never enforced, so rolled-back transactions remain reusable and can still commit mutations.
- `SPEC-001 E7` snapshot support is operationally present enough for basic row-count and commit-blocking behavior, but one concrete API-contract gap was found and logged below: `CommittedReadView` does not expose the documented `IndexScan` / Bound-based range surface.
- `SPEC-001 E8` has partial recovery support (`Sequence`, `ApplyChangeset`, `NextID`/`SetNextID`), but one major feature gap was found and logged below: autoincrement is not integrated into tables/transactions at all, so zero-valued inserts into autoincrement columns are not rewritten.
- `SPEC-002 E1` BSATN codec is mostly present (`bsatn/encode.go`, `bsatn/decode.go`, `bsatn/errors.go`, tests), but one concrete row-decoder contract gap was found and logged below: `DecodeProductValue` accepts extra encoded values silently instead of treating them as a row-shape mismatch.
- `SPEC-002 E2` record/segment I/O is operationally present enough for happy-path framing, CRC, and reader/writer behavior (`commitlog/segment.go`, `commitlog/errors.go`, `commitlog/commitlog_test.go`, `commitlog/phase4_acceptance_test.go`), but two concrete contract gaps were found and logged below: the public exported error/reader API does not match the documented surface, and `SegmentWriter` does not enforce that the first appended tx matches the segment's declared `startTx`.
- `SPEC-003 E2` reducer registry behavior is broadly present (`executor/registry.go`, registry-related executor tests, `executor/phase4_acceptance_test.go`), but one important immutability gap was found and logged below: `Lookup`/`All` return mutable internal reducer pointers, so callers can still mutate the supposedly frozen registry after startup.
- `SPEC-003 E3` executor core is partially present (`executor/executor.go`, `executor/executor_test.go`, `executor/phase4_acceptance_test.go`), but two concrete contract gaps were found and logged below: `SubmitWithContext` ignores reject-on-full semantics, and Story 3.4's subscription-command dispatch path is still absent.
- `SPEC-003 E4` reducer transaction lifecycle is operationally present in behavior (`executor/executor.go`, `executor/phase4_acceptance_test.go`, related lifecycle tests), but one important public contract gap was found and logged below: `ReducerContext` still exposes `DB` and `Scheduler` as `any`, not the typed `*Transaction` / `SchedulerHandle` surface the spec/decomposition promise.
- `SPEC-002 E3` changeset codec behavior is mostly present (`commitlog/changeset_codec.go`, `commitlog/commitlog_test.go`, `commitlog/phase4_acceptance_test.go`), but one concrete API drift was found and logged below: `DecodeChangeset` requires a third `maxRowBytes` argument even though the documented decoder surface takes only `(data, schema)`.
- `SPEC-002 E5` snapshot I/O is not implemented in live code beyond a few snapshot-related error types in `commitlog/errors.go`; the schema snapshot codec, snapshot writer/reader, lockfile helpers, and public snapshot constants/APIs are all absent. I logged the primary missing-surface gap below.
- `SPEC-002 E4` durability worker is partially present (`commitlog/durability.go`, `commitlog/commitlog_test.go`, `commitlog/phase4_acceptance_test.go`), but two concrete gaps were found and logged below: `NewDurabilityWorker` recreates/truncates an existing active segment instead of opening/resuming it, and `CommitLogOptions` is missing the documented `SnapshotInterval` field.
- Verification runs completed during audit:
  - `rtk go test ./schema`
  - `rtk go test ./schema ./executor`
  - `rtk go test ./schema ./store ./executor`
  - `rtk go test ./executor`
  - `rtk go test ./commitlog`
  - earlier broad pass: `rtk go test ./types ./bsatn ./schema ./store ./subscription ./executor ./commitlog`

## Open items

### TD-026: SPEC-002 E4 `CommitLogOptions` is missing the documented `SnapshotInterval` field

Status: open
Severity: medium
First found: SPEC-002 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4h (`SPEC-002 E4: Durability Worker`)

Summary:
- Story 4.1 and SPEC-002 §8 document `CommitLogOptions.SnapshotInterval uint64` with a default of 0.
- Live `CommitLogOptions` includes `MaxSegmentSize`, `MaxRecordPayloadBytes`, `MaxRowBytes`, `ChannelCapacity`, and `DrainBatchSize`, but no `SnapshotInterval` field.
- This leaves the documented durability/snapshot policy surface incomplete even before periodic snapshotting behavior is implemented.

Why this matters:
- This is a public API contract gap in the documented options surface.
- The spec explicitly uses `SnapshotInterval` to describe when periodic snapshots should trigger, and Story 4.1 assigns that field to the durability-worker option struct.
- Callers written against the documented configuration contract do not compile today.

Related code:
- `commitlog/durability.go:11-18`
  - live `CommitLogOptions` has 5 fields and omits `SnapshotInterval`
- `commitlog/durability.go:20-29`
  - `DefaultCommitLogOptions()` therefore cannot return the documented default for `SnapshotInterval`

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-4-durability-worker/story-4.1-durability-handle.md:52-65`
  - documents `CommitLogOptions` including `SnapshotInterval uint64` and `DefaultCommitLogOptions()`
- `docs/decomposition/002-commitlog/epic-4-durability-worker/story-4.1-durability-handle.md:71-72`
  - acceptance criteria require documented defaults
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:529-540`
  - option catalog includes `SnapshotInterval`
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:410-414`
  - recommended default policy is `SnapshotInterval = 0`

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./commitlog`
- Targeted compile repro against the documented field failed:
  - `rtk go test ./.tmp_commitlog_e4_api`
  - observed error:
    - `opts.SnapshotInterval undefined (type commitlog.CommitLogOptions has no field or method SnapshotInterval)`

Recommended resolution options:
1. Preferred code fix:
   - add `SnapshotInterval uint64` to `CommitLogOptions`
   - set the default to 0 in `DefaultCommitLogOptions()`
   - add option-surface tests even if periodic snapshot behavior is implemented later
2. Alternative doc fix:
   - if periodic snapshot triggering is intentionally deferred beyond this slice, update Story 4.1 / SPEC-002 option docs so the option is not advertised yet
   - this is weaker than matching the current documented surface

Suggested follow-up tests:
- compile-time API test for `CommitLogOptions.SnapshotInterval`
- default-options test asserting `SnapshotInterval == 0`
- future integration test proving periodic snapshot triggering uses this field once snapshot I/O exists

### TD-025: SPEC-002 E4 `NewDurabilityWorker` recreates/truncates an existing active segment instead of opening/resuming it

Status: open
Severity: high
First found: SPEC-002 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4h (`SPEC-002 E4: Durability Worker`)

Summary:
- Story 4.1 says `NewDurabilityWorker` should "create or open active segment," but live code always calls `CreateSegment(dir, startTxID)`.
- `CreateSegment` uses `os.Create`, which truncates any existing segment file for that start TxID.
- As a result, constructing a durability worker against a directory that already has the active segment can silently discard previously written durable records.

Why this matters:
- This is a durability correctness bug, not just API drift.
- Even before full recovery wiring lands, the worker constructor should not destroy an existing segment it is supposed to open/resume.
- It also conflicts with Story 4.3's resume-after-crash ownership, which expects fresh-tail decisions to be based on recovery results, not unconditional truncation by constructor.

Related code:
- `commitlog/durability.go:51-64`
  - `NewDurabilityWorker(...)` always calls `CreateSegment(dir, startTxID)`
- `commitlog/segment.go:173-189`
  - `CreateSegment(...)` uses `os.Create(path)`, truncating an existing file with that name
- no alternate open/resume path exists in the live `commitlog` package

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-4-durability-worker/story-4.1-durability-handle.md:47-50`
  - constructor should create or open active segment
- `docs/decomposition/002-commitlog/epic-4-durability-worker/story-4.3-segment-rotation.md:23-25`
  - resume-after-crash logic owns opening a fresh next segment only when recovery says the writable tail must not be reused
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:428-434`
  - recovery determines valid replay horizon and damaged-tail handling before future writes resume

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./commitlog`
- Targeted runtime repro demonstrated truncation of an existing segment:
  - `rtk go run /tmp/commitlog_e4_reopen_repro.go`
  - created a segment with one record, size 29 bytes
  - constructed `NewDurabilityWorker(dir, 1, opts)` against the same directory
  - observed output: `before_size=29 after_size=8`
  - the existing segment was truncated back to header-only size instead of being opened/resumed

Recommended resolution options:
1. Preferred code fix:
   - teach `NewDurabilityWorker` to open/resume an existing active segment when appropriate rather than always calling `CreateSegment`
   - reserve fresh-segment creation for brand-new logs or explicit fresh-tail resume decisions from recovery/rotation logic
   - add tests covering both create-new and reopen-existing cases
2. Alternative temporary guard:
   - if reopen/resume is not ready yet, fail constructor when the target segment file already exists instead of truncating it
   - that still leaves resume incomplete, but it avoids silent data loss

Suggested follow-up tests:
- existing active segment is reopened without truncation
- brand-new directory still creates a fresh segment successfully
- damaged-tail resume path creates a fresh next segment only when recovery explicitly requests it
- `DurableTxID` initial value matches resume state once reopen logic exists

### TD-024: SPEC-002 E5 snapshot I/O surface is almost entirely unimplemented

Status: open
Severity: high
First found: SPEC-002 Epic 5 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4g (`SPEC-002 E5: Snapshot I/O`)

Summary:
- Epic 5's snapshot I/O surface is largely absent from live code.
- The repo currently has only snapshot-related error types in `commitlog/errors.go`; there are no snapshot codec, writer, reader, lockfile helper, or snapshot-listing implementations.
- This leaves the entire snapshot/recovery handoff contract effectively unimplemented for SPEC-002 E5.

Why this matters:
- Snapshot I/O is the prerequisite for bounded recovery and later log compaction. Without it, Epic 6 recovery cannot load snapshots and Epic 7 compaction cannot reason about safe segment deletion.
- This is not a small API drift item; it is a missing implementation surface across all four Epic 5 stories.
- Package tests still pass only because nothing in the current test suite exercises the promised snapshot API.

Related code:
- `commitlog/errors.go:13-15`
  - defines `ErrSnapshotIncomplete`, `ErrSnapshotInProgress`, and `SnapshotHashMismatchError`
- `commitlog/errors.go:59-66`
  - hash-mismatch typed error exists
- `commitlog/` file list contains only:
  - `changeset_codec.go`
  - `commitlog_test.go`
  - `durability.go`
  - `errors.go`
  - `phase4_acceptance_test.go`
  - `segment.go`
- There is no live `schema_snapshot.go`, `snapshot_writer.go`, `snapshot_reader.go`, or equivalent implementation file

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-5-snapshot-io/EPIC.md:13-34`
  - Epic 5 defines schema codec, writer, reader, and integrity work as concrete deliverables
- `docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.1-schema-snapshot-codec.md:16-18,47-52`
  - requires `EncodeSchemaSnapshot` / `DecodeSchemaSnapshot`
- `docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.2-snapshot-writer.md:16-24`
  - requires `SnapshotWriter` / `CreateSnapshot`
- `docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.3-snapshot-reader.md:16-46`
  - requires `SnapshotData`, `ReadSnapshot`, and `ListSnapshots`
- `docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.4-snapshot-integrity.md:16-30`
  - requires `ComputeSnapshotHash`, lockfile helpers, and snapshot constants (`SnapshotMagic`, `SnapshotVersion`, `SnapshotHeaderSize`)

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./commitlog`
- Targeted public-API compile repro against the documented Epic 5 surface failed:
  - `rtk go test ./.tmp_commitlog_e5_api`
  - observed missing symbols:
    - `undefined: commitlog.SnapshotMagic`
    - `undefined: commitlog.SnapshotVersion`
    - `undefined: commitlog.ReadSnapshot`
    - `undefined: commitlog.ListSnapshots`
    - `undefined: commitlog.EncodeSchemaSnapshot`
    - `undefined: commitlog.DecodeSchemaSnapshot`
- This is a missing-feature surface, not just naming drift

Recommended resolution options:
1. Preferred code fix:
   - implement Epic 5 in the intended `commitlog` package with the documented public entrypoints and integrity helpers
   - add writer/reader tests plus compile-time API coverage so the surface is guarded
   - wire schema/sequence/nextID export-import through the SPEC-001 recovery hooks as the stories require
2. Planning/doc fallback:
   - if snapshot work is intentionally deferred, mark SPEC-002 E5 explicitly incomplete in planning/audit docs so later recovery/compaction epics are not treated as ready on the basis of absent primitives

Suggested follow-up tests:
- compile-time API test for `SnapshotMagic`, `SnapshotVersion`, `EncodeSchemaSnapshot`, `DecodeSchemaSnapshot`, `ReadSnapshot`, `ListSnapshots`
- end-to-end write/read snapshot round-trip including schema, sequences, nextID state, and rows
- lockfile/concurrent snapshot tests returning `ErrSnapshotInProgress` and skipping incomplete snapshots
- hash mismatch test returning `ErrSnapshotHashMismatch`

### TD-023: SPEC-002 E3 `DecodeChangeset` public signature does not match the documented decoder surface

Status: open
Severity: medium
First found: SPEC-002 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4f (`SPEC-002 E3: Changeset Codec`)

Summary:
- The changeset encoder/decoder behavior is broadly implemented and tested, including deterministic table ordering, zero-count tables, version checks, unknown-table rejection, and row-size enforcement.
- But the exported decoder signature does not match the documented API.
- Story 3.2 documents `func DecodeChangeset(data []byte, schema SchemaRegistry) (*Changeset, error)`, while live code exports `DecodeChangeset(data []byte, reg schema.SchemaRegistry, maxRowBytes uint32)`.

Why this matters:
- This is a public API contract gap: callers written against the documented changeset codec surface do not compile.
- The extra `maxRowBytes` parameter also shifts policy ownership from the commitlog package onto every caller, even though the spec/decomposition presents row-size enforcement as part of the codec/commitlog contract.
- Existing tests stay green because they use the live implementation signature rather than guarding the documented boundary.

Related code:
- `commitlog/changeset_codec.go:68-69`
  - live decoder signature is `DecodeChangeset(data []byte, reg schema.SchemaRegistry, maxRowBytes uint32)`
- `commitlog/changeset_codec.go:147-149`
  - row-length enforcement uses the caller-supplied `maxRowBytes`
- `commitlog/commitlog_test.go:145`
  - tests pass `DefaultCommitLogOptions().MaxRowBytes`
- `commitlog/phase4_acceptance_test.go:241,261,269,273`
  - acceptance tests also use the live three-argument signature

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-3-changeset-codec/story-3.2-changeset-decoder.md:16-27`
  - documents `func DecodeChangeset(data []byte, schema SchemaRegistry) (*Changeset, error)` and assigns row-size enforcement to the decoder behavior
- `docs/decomposition/002-commitlog/epic-3-changeset-codec/story-3.2-changeset-decoder.md:29-38`
  - acceptance criteria include `ErrRowTooLarge` and schema-aware decode behavior, but not an extra caller-supplied limit argument
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:129,174-180`
  - codec policy is described as part of the commitlog payload format and schema-aware recovery path

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./commitlog`
- Targeted public-API compile repro failed against the documented signature:
  - `rtk go test ./.tmp_commitlog_e3_api`
  - observed error:
    - `not enough arguments in call to commitlog.DecodeChangeset`
    - `have (nil, schema.SchemaRegistry)`
    - `want ([]byte, schema.SchemaRegistry, uint32)`
- Runtime codec behavior itself is mostly present; this is API drift rather than a current functional break in the tested call sites

Recommended resolution options:
1. Preferred code fix:
   - expose the documented two-argument `DecodeChangeset(data, schema)` entrypoint
   - source the row-size policy from commitlog-owned defaults/options internally rather than requiring every caller to pass it explicitly
   - if a lower-level helper with explicit limit is still useful, keep it unexported or add it as a clearly separate advanced API
2. Alternative doc fix:
   - if the project intentionally wants callers to provide `maxRowBytes`, update Story 3.2 and nearby SPEC-002 docs to describe that explicit third parameter and its ownership clearly
   - that would formalize the current surface instead of leaving it as silent drift

Suggested follow-up tests:
- compile-time API test for the documented two-argument `DecodeChangeset`
- runtime test proving the public decoder still enforces the intended max-row limit without per-caller policy mistakes
- test that any low-level explicit-limit helper, if retained, stays behaviorally identical to the public decoder for default options

### TD-022: SPEC-003 E4 `ReducerContext` still exposes `DB` and `Scheduler` as `any` instead of typed contracts

Status: open
Severity: medium
First found: SPEC-003 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4e (`SPEC-003 E4: Reducer Transaction Lifecycle`)

Summary:
- Live reducer execution behavior is mostly present: lifecycle guard, reducer lookup, dequeue-time timestamping, tx creation, panic recovery, rollback, commit, and TxID assignment all exist.
- But the public `ReducerContext` type used by reducers still exposes `DB any` and `Scheduler any`.
- SPEC-003 Stories 4.1/4.2 build on the earlier executor contract that promises `DB *Transaction` and `Scheduler SchedulerHandle`, so reducer authors should not need type assertions to use the core runtime surface.

Why this matters:
- This is a public contract gap in the reducer execution API, not just an internal implementation detail.
- Reducer authors written to the documented runtime contract cannot compile direct `ctx.DB` or `ctx.Scheduler` usage today.
- The current `any` fields also weaken the lifetime/ownership guarantees the docs try to express, because the typed boundary is erased at the public API surface.

Related code:
- `types/reducer.go:8-17`
  - `ReducerContext` defines `DB any` and `Scheduler any`
- `executor/executor.go:236-242`
  - runtime populates those fields with a `*store.Transaction` and scheduler handle, but only behind `any`
- `executor/phase4_acceptance_test.go:197-241`
  - tests must type-assert `ctx.DB.(*store.Transaction)` rather than using the documented typed field directly
- `executor/contracts_test.go:96-108`
  - the minimal contract test constructs `types.ReducerContext` with `DB: nil` and does not guard the stronger typed field contract

Related spec / decomposition docs:
- `docs/decomposition/003-executor/SPEC-003-executor.md:233-238`
  - defines `ReducerContext` with `DB *Transaction` and `Scheduler SchedulerHandle`
- `docs/decomposition/003-executor/epic-1-core-types/story-1.2-reducer-types.md:56-70`
  - acceptance criteria explicitly require `ReducerContext` to reference `Transaction` and `SchedulerHandle`
- `docs/decomposition/003-executor/epic-4-reducer-execution/story-4.1-begin-phase.md:27-35`
  - begin phase constructs a typed `ReducerContext` with `DB: tx` and `Scheduler: ...`
- `docs/decomposition/003-executor/epic-4-reducer-execution/story-4.2-execute-phase.md:47-62`
  - execution docs and guardrails are written against the typed reducer runtime surface

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./executor`
- Targeted public-API compile repro failed when using the documented typed fields directly:
  - `rtk go test ./.tmp_executor_e4_ctxtyping`
  - observed errors:
    - `ctx.DB.Insert undefined (type any has no field or method Insert)`
    - `ctx.Scheduler.Cancel undefined (type any has no field or method Cancel)`
- Runtime behavior itself is present, but the public reducer contract is weaker than documented because callers must downcast from `any`

Recommended resolution options:
1. Preferred code fix:
   - move the canonical `ReducerContext` ownership back to the executor surface described by SPEC-003, with typed `DB` and `Scheduler` fields
   - if package cycles are the blocker, introduce a narrow shared interface/type owner rather than keeping the public reducer API erased as `any`
   - add compile-time contract tests proving reducer authors can call the documented `DB` / `Scheduler` methods directly
2. Alternative doc fix:
   - if the project intentionally wants an erased `any`-based reducer context, update SPEC-003 and downstream schema docs to describe that explicit type-assertion requirement
   - this would be a meaningful weakening of the current runtime contract and likely not the intended end state

Suggested follow-up tests:
- compile-time reducer contract test that direct `ctx.DB` transaction methods compile
- compile-time reducer contract test that direct `ctx.Scheduler` methods compile
- regression test ensuring typed adapter / reducer registration surfaces keep using the same canonical `ReducerContext` owner

### TD-021: SPEC-003 E3 subscription-command dispatch path is still missing

Status: open
Severity: high
First found: SPEC-003 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4d (`SPEC-003 E3: Executor Core`)

Summary:
- Story 3.4 requires executor-core dispatch routing for subscription register/unregister/disconnect commands, but live `dispatch(...)` only handles reducer and lifecycle commands.
- The supporting command types and handler methods are also absent: there is no `RegisterSubscriptionCmd`, `UnregisterSubscriptionCmd`, `DisconnectClientSubscriptionsCmd`, `handleRegisterSubscription`, `handleUnregisterSubscription`, or `handleDisconnectClientSubscriptions`.
- The current `SubscriptionManager` interface is post-commit-only (`EvalAndBroadcast`, `DroppedClients`, `DisconnectClient`) and does not expose the registration/unregistration surface Story 3.4 needs.

Why this matters:
- Story 3.4 is the executor-core owner of the atomic registration path that SPEC-003 uses to guarantee subscription registration ordering against commits.
- Without this dispatch path, Epic 3 is not just thin — the registration-sensitive read boundary described in SPEC-003 §2.5 is still unimplemented.
- This gap is broader than the earlier missing-command-shell debt: even if the command types existed, the executor still lacks the snapshot-acquire/register/close routing logic required by the decomposition.

Related code:
- `executor/command.go:5-40`
  - defines only `CallReducerCmd`, `OnConnectCmd`, and `OnDisconnectCmd`; no subscription command types exist
- `executor/executor.go:183-205`
  - `dispatch(...)` handles only `CallReducerCmd`, `OnConnectCmd`, and `OnDisconnectCmd`, then logs unknown command types
- `executor/interfaces.go:28-41`
  - `SubscriptionManager` exposes post-commit eval/drop-client methods only; no `Register`/`Unregister` registration API
- repo-wide search found no `handleRegisterSubscription`, `handleUnregisterSubscription`, or `handleDisconnectClientSubscriptions` implementation
- `executor/phase4_acceptance_test.go` and `executor/executor_test.go`
  - current tests exercise reducer execution/run-loop behavior, but none cover subscription command routing or snapshot-close behavior

Related spec / decomposition docs:
- `docs/decomposition/003-executor/epic-3-executor-core/story-3.4-command-dispatch.md:16-59`
  - requires the complete type switch plus the three subscription handlers and snapshot-close guarantees
- `docs/decomposition/003-executor/SPEC-003-executor.md:116-131`
  - executor minimum command set includes `RegisterSubscriptionCmd`, `UnregisterSubscriptionCmd`, and `DisconnectClientSubscriptionsCmd`
- `docs/decomposition/003-executor/SPEC-003-executor.md:141-148`
  - registration-sensitive reads must execute through the executor queue for atomicity
- `docs/decomposition/003-executor/EPICS.md:66-72`
  - Epic 3 includes command dispatch routing and shutdown on top of the bounded inbox/run loop

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./executor`
- Live dispatch surface is narrower than the story/spec:
  - unknown command types are only logged
  - there is no subscription registration path in the executor core today
  - no test currently guards the required snapshot acquisition/closure contract for registration commands

Recommended resolution options:
1. Preferred code fix:
   - add the missing subscription command types and request/result contracts at the executor boundary
   - extend `SubscriptionManager` with the registration/unregistration methods Story 3.4 needs
   - implement the three handlers with `CommittedState.Snapshot()` acquisition, `Register(...)` delegation, guaranteed `Close()`, and response delivery
   - add acceptance tests for snapshot closure on both success and error paths
2. If intentionally deferring subscription-core work:
   - record explicitly that Story 3.4 remains incomplete and that the current Epic 3 implementation only covers reducer/lifecycle dispatch, not subscription routing
   - this would avoid overstating Epic 3 completeness while later phases still depend on the missing path

Suggested follow-up tests:
- `RegisterSubscriptionCmd` acquires committed snapshot, calls manager, closes snapshot, and sends result
- `RegisterSubscriptionCmd` closes snapshot even when manager returns error
- `UnregisterSubscriptionCmd` and `DisconnectClientSubscriptionsCmd` delegate and return errors correctly
- unknown command type logs but does not panic

### TD-020: SPEC-003 E3 `SubmitWithContext` ignores reject-on-full policy and returns context timeout instead of `ErrExecutorBusy`

Status: open
Severity: medium
First found: SPEC-003 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4d (`SPEC-003 E3: Executor Core`)

Summary:
- `Submit(...)` honors `rejectMode` and returns `ErrExecutorBusy` on a full inbox when configured to reject.
- `SubmitWithContext(...)` does not mirror that behavior. It ignores `rejectMode` entirely and always waits on the send until either space becomes available or the caller context expires.
- That means the two submission APIs diverge under the same executor configuration.

Why this matters:
- Story 3.3 says `SubmitWithContext` is "same as Submit" plus caller-context cancellation while waiting.
- Under reject-on-full mode, callers should get the same immediate backpressure signal (`ErrExecutorBusy`) rather than being forced into timeout-based detection.
- This is a concrete behavioral contract gap in the public submission API, not just missing test coverage.

Related code:
- `executor/executor.go:121-138`
  - `Submit(...)` checks `e.rejectMode` and returns `ErrExecutorBusy` on a full inbox
- `executor/executor.go:140-153`
  - `SubmitWithContext(...)` does not check `e.rejectMode`; it only selects between inbox send and `ctx.Done()`
- repo-wide tests do not include a `SubmitWithContext` reject-on-full case

Related spec / decomposition docs:
- `docs/decomposition/003-executor/epic-3-executor-core/story-3.3-submit-methods.md:17-31`
  - `SubmitWithContext` is documented as the same policy as `Submit`, but with context cancellation support while waiting
- `docs/decomposition/003-executor/epic-3-executor-core/story-3.3-submit-methods.md:35-40`
  - acceptance criteria include full-inbox reject behavior and context-cancel-while-blocking behavior
- `docs/decomposition/003-executor/EPICS.md:71-78`
  - Epic 3 backpressure contract includes both blocking and `ErrExecutorBusy` reject modes
- `docs/decomposition/003-executor/SPEC-003-executor.md:86-88,655-656`
  - bounded inbox may block or return `ErrExecutorBusy`; shutdown/busy semantics are part of the core error surface

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./executor`
- Targeted runtime repro showed the mismatch directly:
  - `rtk go run /tmp/executor_e3_submitctx_repro.go`
  - executor configured with `rejectMode=true` and a full inbox
  - observed output: `err=context deadline exceeded elapsed_ms=50`
  - expected per Story 3.3: immediate `ErrExecutorBusy`

Recommended resolution options:
1. Preferred code fix:
   - make `SubmitWithContext(...)` honor `rejectMode` the same way `Submit(...)` does
   - in reject mode, return `ErrExecutorBusy` immediately on full inbox
   - in blocking mode, keep the current context-aware wait semantics
2. Alternative doc fix:
   - if the intended behavior is for `SubmitWithContext` to always block-until-context regardless of reject mode, update Story 3.3 to describe that divergence explicitly
   - this seems less desirable because it makes the two submission APIs inconsistent under one executor configuration

Suggested follow-up tests:
- `SubmitWithContext` on full inbox with `rejectMode=true` returns `ErrExecutorBusy` immediately
- `SubmitWithContext` on full inbox with `rejectMode=false` blocks until either space opens or context is cancelled
- `Submit` and `SubmitWithContext` match on shutdown/fatal handling under the same executor state

### TD-019: SPEC-003 E2 frozen reducer registry remains externally mutable through `Lookup` and `All`

Status: open
Severity: high
First found: SPEC-003 Epic 2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4c (`SPEC-003 E2: Reducer Registry`)

Summary:
- The registry implements `Freeze()` and rejects new registrations afterward, but it does not actually make registered reducer entries immutable.
- `Lookup(...)`, `LookupLifecycle(...)`, and `All()` all hand out pointers to the live internal `RegisteredReducer` structs stored in the map.
- Callers can mutate those returned structs after freeze, changing reducer names/lifecycle metadata in-place and bypassing the registry's validation rules entirely.

Why this matters:
- Story 2.2 and SPEC-003 treat freeze as the point where registration becomes immutable and concurrent reads become safe because the registry no longer changes.
- With the current pointer aliasing, post-start callers can still rewrite reducer metadata without going through `Register(...)`, so the freeze guarantee is only partial.
- This is not just a cosmetic encapsulation issue: downstream lifecycle lookup behavior can change after startup if a caller mutates a returned reducer's `Lifecycle` field.

Related code:
- `executor/registry.go:6-9`
  - registry stores `map[string]*RegisteredReducer`
- `executor/registry.go:49`
  - `Register(...)` stores the address of the local reducer struct directly in the map
- `executor/registry.go:53-57`
  - `Lookup(...)` returns the live internal pointer
- `executor/registry.go:60-67`
  - `LookupLifecycle(...)` returns the live internal pointer found during map iteration
- `executor/registry.go:69-76`
  - `All()` returns a slice of the same live internal pointers
- `executor/phase4_acceptance_test.go:43-78` and `executor/executor_test.go:113-155`
  - tests cover duplicate names, lifecycle name rules, and freeze rejection, but do not verify immutability of returned registry entries

Related spec / decomposition docs:
- `docs/decomposition/003-executor/SPEC-003-executor.md:184-187`
  - registration rules include uniqueness, reserved lifecycle names, and immutability after executor start
- `docs/decomposition/003-executor/EPICS.md:41-43`
  - Epic 2 explicitly includes `Freeze()` to make the registry immutable after startup
- `docs/decomposition/003-executor/epic-2-reducer-registry/story-2.1-registry.md:30-47`
  - `Lookup`/`All` are the public read APIs, and the design notes say lookup is safe for concurrent reads after freeze because the map is immutable
- `docs/decomposition/003-executor/epic-2-reducer-registry/story-2.2-lifecycle-validation.md:12-31`
  - freeze is defined as registry immutability after startup

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./executor`
- Targeted runtime repro showed the immutability leak is live:
  - `rtk go run /tmp/executor_registry_mutation_repro.go`
  - observed output: `lookup_ok=true name_after_lookup_mutation=MUTATED lifecycle_ok=true lifecycle_name=MUTATED`
  - this demonstrates that mutating the reducer returned from `Lookup("A")` changed the stored reducer name, and mutating the reducer returned via `All()` changed lifecycle lookup results after freeze

Recommended resolution options:
1. Preferred code fix:
   - store reducers by value internally or clone them on both insert and readout
   - make `Lookup`, `LookupLifecycle`, and `All` return detached copies so callers cannot mutate frozen internal state
   - add regression tests proving post-freeze mutations of returned values do not affect later lookups
2. Alternative contract change:
   - if pointer-returning APIs are intentional, update SPEC-003 Epic 2 docs to drop the stronger immutability/concurrent-read claim and describe the registry as internally mutable through returned handles
   - this would be a significant weakening of the current contract and likely not the desired direction

Suggested follow-up tests:
- mutate the reducer returned by `Lookup(...)` after `Freeze()` and assert a fresh lookup still returns the original metadata
- mutate the first entry returned by `All()` after `Freeze()` and assert `LookupLifecycle(...)` is unchanged
- verify `NewExecutor(...)` plus lifecycle dispatch continue to observe the originally registered lifecycle reducers even if caller-held copies are mutated

### TD-018: SPEC-002 E2 `SegmentWriter` does not enforce segment startTx alignment

Status: open
Severity: high
First found: SPEC-002 Epic 2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4b (`SPEC-002 E2: Record format & segment I/O`)

Summary:
- `CreateSegment(dir, startTxID)` records a segment start offset in the filename and writer state, but `Append(...)` only checks `tx_id > lastTx`.
- For a fresh segment, there is no validation that the first appended record's `tx_id` equals the segment's declared `startTx`.
- That allows creation of segments whose filename/start metadata says one thing while the first durable record contains a different transaction ID.

Why this matters:
- Story 2.3 defines `startTx` as "first TX ID in this segment," and later recovery/compaction stories rely on filename-derived start TX metadata as real history boundaries.
- If the first record in `00000000000000000100.log` can actually be tx 1, recovery-side ordering and coverage logic can be misled before Epic 6/7 code even runs.
- This is a durability correctness contract gap, not just missing polish around writer validation.

Related code:
- `commitlog/segment.go:163-170`
  - `SegmentWriter` stores both `startTx` and `lastTx`
- `commitlog/segment.go:173-189`
  - `CreateSegment(...)` names the file from `startTxID` and stores `startTx`
- `commitlog/segment.go:192-201`
  - `Append(...)` validates only strict monotonic increase relative to `lastTx`; it never checks first-record equality against `startTx`
- `commitlog/phase4_acceptance_test.go:152-222`
  - reader/writer tests cover EOF/truncation/corruption but do not assert first-record alignment with segment start metadata

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.3-segment-writer.md:18-35`
  - defines `startTx` as the first TX ID in the segment and requires `CreateSegment(dir, startTxID)` plus monotonic append validation
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.4-segment-reader.md:16-27`
  - reader `startTx` is defined as coming from the filename
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:39-53`
  - segment filenames are part of the on-disk ordering model
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:428-429`
  - recovery begins by validating segment start TX IDs from filenames in sorted order

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./commitlog`
- Targeted runtime repro showed the mismatch is live:
  - `rtk go run /tmp/commitlog_e2_starttx_repro.go`
  - observed output: `start_tx=100 first_record_tx=1 err=<nil>`
  - expected per Story 2.3: first record in that segment should have tx 100, or append should fail

Recommended resolution options:
1. Preferred code fix:
   - teach `SegmentWriter.Append(...)` to require `rec.TxID == sw.startTx` when `sw.lastTx == 0`
   - keep the existing strict-increase check for subsequent appends
   - add regression tests covering both aligned and misaligned first-appends
2. Alternative defensive fix:
   - if the writer should remain low-level, add a dedicated constructor or seal-time validation that ensures the first record actually matches the declared segment start before any reader/recovery path can consume it
   - if this route is chosen, document clearly which layer owns that invariant

Suggested follow-up tests:
- first append with `tx_id != startTx` returns an error
- first append with `tx_id == startTx` succeeds and reads back with matching `StartTxID()` / first record tx
- rotated segments opened by the durability worker start at exactly `previousLastTx + 1`

### TD-017: SPEC-002 E2 exported error/reader API does not match the documented surface

Status: open
Severity: medium
First found: SPEC-002 Epic 2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4b (`SPEC-002 E2: Record format & segment I/O`)

Summary:
- The implementation has working typed errors and a segment reader, but the exported API shape diverges from the decomposition/spec surface.
- Docs name typed errors as `ErrBadVersion`, `ErrUnknownRecordType`, `ErrChecksumMismatch`, and `ErrRecordTooLarge`, but live code exports `BadVersionError`, `UnknownRecordTypeError`, `ChecksumMismatchError`, and `RecordTooLargeError` instead.
- Docs also specify `func (sr *SegmentReader) Next() (*Record, error)`, while live code exports `Next(maxPayload uint32)`.

Why this matters:
- This is a public API contract gap: consumers written against the documented commitlog surface do not compile.
- The mismatch is broader than naming style; the spec/decomposition treats these names and signatures as shared contracts consumed by later recovery work.
- Package tests still pass because they use the implementation's actual names/signature rather than guarding the documented boundary.

Related code:
- `commitlog/errors.go:19-48`
  - exports `BadVersionError`, `UnknownRecordTypeError`, `ChecksumMismatchError`, and `RecordTooLargeError`
- `commitlog/segment.go:52-68`
  - `ReadSegmentHeader(...)` returns `*BadVersionError`
- `commitlog/segment.go:106-155`
  - `DecodeRecord(...)` returns `*ChecksumMismatchError`, `*UnknownRecordTypeError`, and `*RecordTooLargeError`
- `commitlog/segment.go:249-259`
  - `SegmentReader.Next(maxPayload uint32)` requires a max-payload argument not present in the documented API
- `commitlog/commitlog_test.go:54-67` and `commitlog/phase4_acceptance_test.go:59-150`
  - tests exercise the live names/signature only

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.1-segment-header.md:26-39`
  - bad version is documented as `ErrBadVersion`
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.2-record-framing.md:41-58`
  - names `ErrUnknownRecordType`, `ErrChecksumMismatch`, and `ErrRecordTooLarge`
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.4-segment-reader.md:25-42`
  - documents `OpenSegment(...)` plus `func (sr *SegmentReader) Next() (*Record, error)`
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.5-segment-error-types.md:16-29`
  - assigns the typed-error field sets to `ErrBadVersion`, `ErrUnknownRecordType`, `ErrChecksumMismatch`, and `ErrRecordTooLarge`

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./commitlog`
- Targeted public-API compile repro failed against the documented symbols/signature:
  - `rtk go test ./.tmp_commitlog_e2_api`
  - observed errors:
    - `undefined: commitlog.ErrBadVersion`
    - `undefined: commitlog.ErrUnknownRecordType`
    - `undefined: commitlog.ErrChecksumMismatch`
    - `undefined: commitlog.ErrRecordTooLarge`
    - `not enough arguments in call to sr.Next`

Recommended resolution options:
1. Preferred code fix:
   - expose the documented typed errors at the advertised names (either by renaming the structs or exporting compatible aliases/wrappers)
   - add a no-argument `SegmentReader.Next()` entrypoint that enforces the configured/default max payload internally, keeping `Next(maxPayload)` private or as a lower-level helper if needed
   - add compile-time contract coverage so future drift is caught
2. Alternative doc fix:
   - if the repo intentionally prefers `BadVersionError`-style names and an explicit `Next(maxPayload)` API, update the Epic 2 decomposition/spec docs to describe that surface consistently before later recovery stories treat the current docs as canonical

Suggested follow-up tests:
- compile-time API test for the documented error type names
- compile-time API test for `SegmentReader.Next()` with no arguments
- runtime test proving whichever public `Next` surface remains still enforces `MaxRecordPayloadBytes` without caller footguns

### TD-001: Invalid-float error contract drift across `types` and `store`

Status: open
Severity: medium
First found: SPEC-001 Epic 1 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 1 / Step 1a (`SPEC-001 E1: Core Value Types`)

Summary:
- NaN rejection behavior is implemented for float values, but the documented error contract is not aligned with the live constructor path.
- The spec/decomposition says `ErrInvalidFloat` is part of the SPEC-001 error surface, but the actual `types.NewFloat32` / `types.NewFloat64` constructors return ad-hoc `fmt.Errorf(...)` errors instead of a stable sentinel or typed error.
- The only `ErrInvalidFloat` sentinel currently lives in `store/errors.go`, which is not where the rejecting constructors are implemented.

Why this matters:
- Callers cannot reliably use `errors.Is(..., ErrInvalidFloat)` against the actual constructor failure path today.
- The ownership boundary is muddy: value construction happens in `types`, while the documented invalid-float error lives in `store`.
- This is likely to create downstream inconsistency in BSATN decode, schema/store validation, and future protocol/executor paths that construct float `Value`s.

Related code:
- `types/value.go:110-122`
  - `NewFloat32` rejects NaN via `fmt.Errorf("shunter: NaN is not a valid Float32 value")`
  - `NewFloat64` rejects NaN via `fmt.Errorf("shunter: NaN is not a valid Float64 value")`
- `store/errors.go:8-19`
  - defines `ErrInvalidFloat = errors.New("invalid float value")`
- `bsatn/decode.go:87-92`
  - decode path depends on `types.NewFloat32` / `types.NewFloat64`
- `types/value_test.go:198-209`
  - tests currently verify only that an error is returned, not the stable error contract

Related spec / decomposition docs:
- `docs/EXECUTION-ORDER.md:157-160`
  - Phase 1 / Step 1a establishes SPEC-001 E1 as the first foundation slice
- `docs/decomposition/001-store/EPICS.md:7-30`
  - Epic 1 scope includes NaN rejection on float construction
- `docs/decomposition/001-store/EPICS.md:268-284`
  - error table says `ErrInvalidFloat` is introduced in Epic 1
- `docs/decomposition/001-store/epic-1-core-value-types/story-1.1-valuekind-value-struct.md:34-56`
  - constructors must reject NaN
- `docs/decomposition/001-store/SPEC-001-store.md:641-654`
  - SPEC-001 error catalog includes `ErrInvalidFloat`

Current observed behavior:
- Functional behavior: correct
  - NaN is rejected in both constructors.
- Contract behavior: drift
  - no stable exported error from the constructor path
  - spec implies a reusable error contract that code does not currently provide

Recommended resolution options:
1. Preferred code fix:
   - move ownership of invalid-float error contract to `types`, or introduce a shared lower-level error that `types` can return directly
   - update `NewFloat32` / `NewFloat64` to return that stable error via wrapping or direct sentinel use
   - add tests asserting `errors.Is(err, ErrInvalidFloat)` on NaN constructor failure
2. Alternative doc fix:
   - if the design intent is "any error is fine, only rejection matters," then update SPEC-001 decomposition/spec docs to remove the stronger `ErrInvalidFloat` contract from Epic 1
   - if this route is chosen, also update the SPEC-001 error catalog to clarify ownership and where that error can actually originate

Suggested follow-up tests:
- `types`: assert NaN constructor failures match the canonical invalid-float error contract
- `bsatn`: assert float decode failure on NaN preserves the same canonical error classification
- `store`: if store-level row validation can also detect invalid float states, ensure both layers agree on error classification

Audit notes:
- This finding came from the first audit pass over SPEC-001 Epic 1 only.
- Verification run passed at audit time: `rtk go test ./types ./bsatn ./schema ./store ./subscription ./executor ./commitlog`
- Passing tests establish operational health, not full spec-contract completeness.

### TD-002: SPEC-003 Epic 1 command/interface/error surface is only partially defined

Status: open
Severity: medium
First found: Phase 1 planning pass while moving from schema foundations toward the executor contract slice
Execution-order context:
- `docs/EXECUTION-ORDER.md:157-160` explicitly allows a narrowed executor contract slice in Phase 1: `SPEC-003 E1.1 + E1.2 + minimal E1.4 contract slice`
- This debt item is therefore about the fuller `SPEC-003 Epic 1` decomposition surface remaining incomplete, not about the minimal Phase 1 exception itself

Summary:
- The current executor package has the core reducer request/response types and `SchedulerHandle`, but it does not yet define the full Epic 1 command/interface/error contract described in the decomposition docs.
- Missing pieces include subscription command shells, durability/subscription interfaces, and the `ErrCommitFailed` sentinel.
- This matters because later epics and cross-spec dependencies talk about these contracts as stable shared surfaces even before their full behavior lands.

Why this matters:
- The execution-order exception only narrows what must exist for the earliest Phase 1 gate. It does not make the fuller Epic 1 contract disappear.
- Leaving these contracts implicit or absent increases the chance that later phases will grow ad-hoc signatures instead of converging on the spec-owned interface surface.
- The current `executor/contracts_test.go` verifies only a narrower subset, so package tests can stay green while the broader Epic 1 contract remains incomplete.

Related code:
- `executor/command.go:3-15`
  - defines `ExecutorCommand` and `CallReducerCmd` only
  - missing `RegisterSubscriptionCmd`, `UnregisterSubscriptionCmd`, `DisconnectClientSubscriptionsCmd`
- `executor/interfaces.go:5-12`
  - defines `SchedulerHandle` only
  - missing `DurabilityHandle` and `SubscriptionManager`
- `executor/errors.go:5-12`
  - defines 6 sentinels but omits `ErrCommitFailed`
- `executor/contracts_test.go:11-133`
  - exercises a reduced contract subset and does not guard the missing command/interface/error definitions
- `executor/executor.go:23-254`
  - already implements later-epic runtime behavior, but without the full Epic 1 shared contract surface specified by the docs

Related spec / decomposition docs:
- `docs/decomposition/003-executor/EPICS.md:7-27`
  - Epic 1 scope includes command types, subsystem interfaces, and 7 error sentinels
- `docs/decomposition/003-executor/epic-1-core-types/EPIC.md:13-18`
  - Stories 1.3, 1.4, and 1.5 explicitly own the missing command, interface, and error surfaces
- `docs/decomposition/003-executor/epic-1-core-types/story-1.3-command-types.md:30-61`
  - requires `RegisterSubscriptionCmd`, `UnregisterSubscriptionCmd`, and `DisconnectClientSubscriptionsCmd`
- `docs/decomposition/003-executor/epic-1-core-types/story-1.4-subsystem-interfaces.md:16-59`
  - requires `DurabilityHandle` and `SubscriptionManager` alongside `SchedulerHandle`
- `docs/decomposition/003-executor/epic-1-core-types/story-1.5-error-types.md:16-38`
  - requires 7 sentinels including `ErrCommitFailed`

Current observed behavior:
- Minimal Phase 1 reducer/runtime contract: partially present and good enough for some downstream compilation paths
- Full SPEC-003 Epic 1 surface: incomplete relative to decomposition docs
- Operational status: package tests still pass (`rtk go test ./executor`), so this is a spec-completeness gap rather than a current build break

Recommended resolution options:
1. Preferred code fix:
   - add the missing command shell types in `executor/command.go`
   - add the missing `DurabilityHandle` and `SubscriptionManager` interfaces in `executor/interfaces.go`
   - add `ErrCommitFailed` to `executor/errors.go`
   - extend `executor/contracts_test.go` to assert the complete Epic 1 surface exists and satisfies the intended signatures
2. Alternative doc clarification:
   - if the project intends to keep only the narrowed execution-order slice for now, add an explicit note in the executor decomposition or TECH-DEBT trail that Stories 1.3/1.4/1.5 are intentionally partial pending later subscription/durability integration
   - this would reduce the current ambiguity between "minimal contract slice landed" and "Epic 1 fully landed"

Suggested follow-up tests:
- compile-time assertions that each command type satisfies `ExecutorCommand`
- interface shape tests for `DurabilityHandle` and `SubscriptionManager`
- `errors.Is` coverage for all seven executor sentinels including `ErrCommitFailed`
- a targeted contract test that distinguishes the minimal Phase 1 slice from the full Epic 1 surface so future audits can classify the gap cleanly

### TD-003: `ErrSequenceOverflow` is specified but not defined anywhere in live code

Status: open
Severity: medium
First found: SPEC-006 Epic 1 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 1 / Step 1c (`SPEC-006 E1: Schema Types & Type Mapping`)

Summary:
- The schema/type-mapping slice correctly provides `AutoIncrementBounds(...)`, but the documented paired error contract `ErrSequenceOverflow` does not exist anywhere in the repository's Go code.
- The decomposition explicitly assigns that contract to SPEC-006 Epic 1 as the schema-owned bounds/error surface consumed by SPEC-001 auto-increment logic.

Why this matters:
- The auto-increment bounds contract is only half surfaced today: callers can ask what the bounds are, but there is no canonical sentinel for overflow.
- This creates ambiguity about which package owns overflow classification and prevents future `errors.Is(..., ErrSequenceOverflow)` checks from being standardized.
- The missing sentinel weakens the shared boundary between schema validation metadata and store/runtime auto-increment behavior.

Related code:
- `schema/valuekind_export.go:31-55`
  - implements `AutoIncrementBounds(k ValueKind) (min int64, max uint64, ok bool)`
- `schema/validate_structure.go:62`
  - uses `AutoIncrementBounds` only to validate whether a type is integer-eligible
- `schema/errors.go:5-17`
  - defines several schema validation errors, but no `ErrSequenceOverflow`
- `store/sequence.go:5-37`
  - sequence implementation exists, but no overflow error contract is defined there either
- Repository-wide search for `ErrSequenceOverflow` returned no Go-code matches

Related spec / decomposition docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md:68`
  - says inserts fail with `ErrSequenceOverflow` when auto-increment exceeds the type range
- `docs/decomposition/006-schema/SPEC-006-schema.md:579`
  - error catalog lists `ErrSequenceOverflow`
- `docs/decomposition/006-schema/EPICS.md:19`
  - Epic 1 scope includes auto-increment numeric bounds metadata used to enforce `ErrSequenceOverflow`
- `docs/decomposition/006-schema/EPICS.md:234`
  - error table assigns `ErrSequenceOverflow` to Epic 1
- `docs/decomposition/006-schema/epic-1-schema-types/story-1.4-valuekind-export-bounds.md:20-37`
  - explicitly ties `AutoIncrementBounds` to the `ErrSequenceOverflow` contract

Current observed behavior:
- `AutoIncrementBounds` exists and is well-tested
- no canonical overflow sentinel exists yet in `schema`, `store`, or any shared package
- this is a spec-contract gap, not a current test failure

Recommended resolution options:
1. Preferred code fix:
   - define `ErrSequenceOverflow` in the canonical owning package for this contract
   - use that sentinel from the eventual store-side auto-increment enforcement path
   - add tests asserting overflow failures wrap the canonical sentinel
2. Alternative doc fix:
   - if ownership should belong to SPEC-001/store rather than SPEC-006/schema, update the SPEC-006 spec/decomposition error ownership text so the bounds contract remains in schema but the runtime error ownership moves explicitly to store

Suggested follow-up tests:
- store-side sequence overflow tests for every integer `ValueKind`
- `errors.Is` coverage for the chosen canonical `ErrSequenceOverflow` sentinel
- cross-package test proving the auto-increment runtime path and schema bounds metadata agree on overflow behavior

### TD-016: SPEC-002 E1 `DecodeProductValue` does not reject extra encoded columns

Status: open
Severity: medium
First found: SPEC-002 Epic 1 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4a (`SPEC-002 E1: BSATN codec`)

Summary:
- The BSATN value codec is broadly implemented and tested, and `DecodeProductValueFromBytes` correctly rejects trailing bytes.
- But `DecodeProductValue(r, schema)` decodes exactly `len(schema.Columns)` values and then returns success without checking whether the reader still contains more encoded columns.
- Story 1.3 says a row with more values than the schema expects should be treated as a row-shape mismatch. In live code, extra values are silently left unread.

Why this matters:
- This is a contract gap in the row decoder itself, not just a missing convenience wrapper.
- Callers that use `DecodeProductValue` directly on a framed stream can incorrectly accept malformed rows that contain extra encoded values, leaving trailing data to confuse higher layers.
- The spec/decomposition explicitly distinguishes both "too few" and "too many" values as row-shape failures.

Related code:
- `bsatn/decode.go:121-135`
  - `DecodeProductValue(...)` decodes `len(ts.Columns)` values and returns success immediately
- `bsatn/decode.go:137-151`
  - only `DecodeProductValueFromBytes(...)` checks for trailing bytes after row decode
- `bsatn/phase4_acceptance_test.go:145-178`
  - tests cover short rows and trailing bytes in `DecodeProductValueFromBytes`, but not extra-column acceptance in `DecodeProductValue`

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-1-bsatn-codec/story-1.3-product-value-codec.md:20-29`
  - requires fewer OR more values than schema to be treated as row-shape/length mismatch
- `docs/decomposition/002-commitlog/epic-1-bsatn-codec/story-1.3-product-value-codec.md:34-39`
  - acceptance criteria explicitly include schema expects 3 columns but encoded row has 4 → error
- `docs/decomposition/002-commitlog/epic-1-bsatn-codec/story-1.4-bsatn-error-types.md:18-21`
  - row-shape and row-length error taxonomy for malformed rows

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./bsatn`
- Targeted runtime repro from the audit showed silent acceptance of an extra encoded value:
  - schema has 2 columns
  - buffer contains 3 encoded values
  - `DecodeProductValue(...)` returned `row_len=2 err=<nil> trailing=9`
  - the third encoded value remained unread instead of causing a row-shape failure

Recommended resolution options:
1. Preferred code fix:
   - make the row-level decoding path reject extra encoded values when the caller expects a full row payload
   - one approach: keep `DecodeProductValueFromBytes` as the strict entrypoint and tighten docs/callers so direct `DecodeProductValue` is only used with an exact row-limited reader
   - alternatively, change `DecodeProductValue` semantics or add a strict variant and update callers/tests accordingly
2. Minimum test fix required either way:
   - add a regression test covering a schema with N columns and encoded data for N+1 values
   - assert the chosen strict API returns row-shape/length failure rather than silently succeeding

Suggested follow-up tests:
- schema expects 2 columns, encoded stream has 3 values → strict row decode fails
- short row still fails with the correct shape/length classification
- framed row decode paths in later commitlog stories use the strict variant so extra trailing columns cannot slip through

### TD-015: SPEC-001 E8 auto-increment sequence is not integrated into table/transaction inserts

Status: open
Severity: high
First found: SPEC-001 Epic 8 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3g (`SPEC-001 E8: Auto-Increment & Recovery`)

Summary:
- The repo has a standalone `Sequence` type and some recovery helpers, but auto-increment is not wired into table or transaction behavior.
- Tables do not gain a `sequence` or `sequenceCol`, and `Transaction.Insert(...)` does not rewrite zero values in autoincrement columns.
- As a result, inserting a row with `0` in an autoincrement column preserves `0` instead of assigning the next sequence value, which misses the central behavior of Story 8.1.

Why this matters:
- Story 8.1 is not just a utility-type story; it requires observable insert-time behavior for autoincrement columns.
- Without that integration, schemas that declare `AutoIncrement` currently validate but do not behave per spec.
- This also leaves Story 8.3 incomplete in practice because there is no per-table sequence state to export or restore.

Related code:
- `store/sequence.go:5-37`
  - standalone `Sequence` exists and works in isolation
- `store/table.go:10-17`
  - `Table` has no `sequence` or `sequenceCol` fields
- `store/transaction.go:31-85`
  - `Insert(...)` validates, checks constraints, allocates RowID, and stores the row as-is; it never rewrites a zero-valued autoincrement column
- `store/recovery.go:67-72`
  - `TableExportState` mentions `SequenceValue`, but there is no live table sequence field backing it

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.1-sequence.md:32-48`
  - requires `Table` sequence integration and zero-means-auto-assign behavior in `Transaction.Insert`
- `docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.3-state-export.md:23-29,37-42`
  - expects per-table sequence state accessors/restore hooks
- `docs/decomposition/001-store/SPEC-001-store.md:628-645`
  - store spec defines autoincrement behavior and overflow expectations tied to inserted rows, not just a helper object

Current observed behavior:
- Existing targeted Epic 8 tests still pass:
  - `rtk go test ./store -run 'TestSequenceMonotonic|TestSequenceReset|TestApplyChangeset|TestApplyChangesetDeletesByPrimaryKeyNotStoredRowID|TestApplyChangesetDeletesByRowEqualityForSetSemanticsTables'`
- Targeted runtime repro from the audit showed the feature gap directly:
  - build a schema table with `PrimaryKey: true, AutoIncrement: true` on `id`
  - insert row `{id: 0, name: "job-a"}` through `Transaction.Insert(...)`
  - observed output:
    - `inserted row pk=0 provisionalRowID=1`
  - expected per spec: row PK should be rewritten to the next sequence value rather than staying `0`

Recommended resolution options:
1. Preferred code fix:
   - add optional sequence state to `Table` (`sequence`, `sequenceCol`)
   - initialize it from schema autoincrement metadata in `NewTable`
   - in `Transaction.Insert`, rewrite zero values in the sequence column before constraint checks
   - add `SequenceValue` / `SetSequenceValue` accessors to complete Story 8.3
2. If deferring the full feature intentionally:
   - document Story 8.1 / 8.3 as incomplete in planning/decomposition notes so autoincrement columns are not mistaken as working end-to-end today

Suggested follow-up tests:
- zero-valued insert into autoincrement column produces 1, then 2, then 3
- non-zero autoincrement column value is preserved as explicit caller choice
- export/restore of sequence state round-trips correctly once sequence integration exists
- overflow classification uses the canonical `ErrSequenceOverflow` contract once that separate gap is resolved

### TD-014: SPEC-001 E7 `CommittedReadView` is missing the documented `IndexScan` and Bound-based `IndexRange` API

Status: open
Severity: medium
First found: SPEC-001 Epic 7 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3f (`SPEC-001 E7: Read-Only Snapshots`)

Summary:
- The snapshot subsystem exists and basic behavior works: snapshots hold a read lock, row-count reads work, and commit blocks until snapshots close.
- But the public `CommittedReadView` interface in live code does not match Story 7.1's documented surface.
- Specifically, the spec requires `IndexScan(tableID, indexID, value Value)` and `IndexRange(tableID, indexID, lower, upper Bound)`, while live code exposes `IndexSeek(..., key IndexKey)` and `IndexRange(..., low, high *IndexKey)` instead.

Why this matters:
- This is a public API contract gap, not just an internal refactor. Consumers written to the documented snapshot interface do not compile.
- The missing `IndexScan` convenience and Bound-based range endpoints are part of how the spec expects callers (especially subscription-side consumers) to perform committed reads.
- The mismatch is amplified by the earlier Epic 3 gap: the `Bound` helper surface is also absent, so the documented range-read API cannot be expressed at all.

Related code:
- `store/snapshot.go:12-18`
  - live `CommittedReadView` exposes `IndexSeek` and `IndexRange(... *IndexKey)`
- `store/snapshot.go:41-63`
  - implementation provides exact-key lookup by `IndexKey` and range lookup by `*IndexKey`
- repo-wide search found no snapshot-side `IndexScan` method
- repo-wide search found no snapshot-side Bound-based overload/variant

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-7-read-only-snapshots/story-7.1-committed-read-view.md:16-24`
  - requires `IndexScan(tableID, indexID, value Value)` and `IndexRange(... Bound, Bound)` on `CommittedReadView`
- `docs/decomposition/001-store/epic-7-read-only-snapshots/story-7.1-committed-read-view.md:48-54`
  - documents point lookup as row-resolving index scan and range scan as Bound-derived traversal
- `docs/decomposition/001-store/epic-7-read-only-snapshots/story-7.1-committed-read-view.md:63-68`
  - acceptance criteria explicitly cover `IndexScan` and Bound/unbounded range behavior

Current observed behavior:
- Existing targeted snapshot tests pass:
  - `rtk go test ./store -run 'TestSnapshotPointInTime|TestSnapshotCloseSafe|TestSnapshotBlocksCommitUntilClose'`
- Targeted public-API compile repro from the audit failed:
  - temporary package referenced `CommittedReadView.IndexScan(...)` and `CommittedReadView.IndexRange(..., store.UnboundedLow(), store.UnboundedHigh())`
  - `rtk go test ./.tmp_store_snapshot_api` failed with:
    - `v.IndexScan undefined`
    - `undefined: store.UnboundedLow`
    - `undefined: store.UnboundedHigh`

Recommended resolution options:
1. Preferred code fix:
   - extend `CommittedReadView` with the documented `IndexScan` and Bound-based `IndexRange` API
   - implement those methods on `CommittedSnapshot`, resolving RowIDs to rows as the spec describes
   - keep `IndexSeek`/`*IndexKey` helpers internally if useful, but expose the documented public contract too
2. Alternative doc fix:
   - if the project intentionally prefers raw `IndexKey`-based snapshot APIs, update Story 7.1 and nearby docs to reflect that simplification explicitly
   - this would also need coordinated cleanup of the remaining Bound references

Suggested follow-up tests:
- compile-time/public API test for `CommittedReadView.IndexScan` and Bound-based `IndexRange`
- snapshot tests covering PK/non-existent point scans via `IndexScan`
- snapshot range tests covering unbounded/inclusive/exclusive semantics once Bound exists

### TD-013: SPEC-001 E6 rollback does not make transactions unusable

Status: open
Severity: high
First found: SPEC-001 Epic 6 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3e (`SPEC-001 E6: Commit, Rollback & Changeset`)

Summary:
- `Rollback(tx)` currently only sets `tx.rolledBack = true`.
- No transaction operation checks that flag, and rollback does not clear or detach `TxState`.
- As a result, a rolled-back transaction can still accept new mutations and can still be committed successfully, directly violating Story 6.4.

Why this matters:
- Story 6.4 requires rollback to discard the transaction and make subsequent `Insert` / `Delete` / `Update` / `Commit` panic or return error.
- The current behavior is not just incomplete cleanup; it enables silent reuse of an invalid transaction object, which can produce committed mutations after callers think the transaction was discarded.
- This is a correctness bug in the transaction lifecycle contract, not just missing defensive polish.

Related code:
- `store/commit.go:55-58`
  - `Rollback(tx)` only sets `tx.rolledBack = true`
- `store/transaction.go:31-247`
  - `Insert`, `Delete`, `Update`, and read paths do not check `rolledBack`
- `store/commit.go:12-52`
  - `Commit(...)` also does not check `rolledBack`

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.4-rollback.md:16-21`
  - rollback should clear/discard tx state and make the transaction unusable afterward
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.4-rollback.md:23-30`
  - acceptance criteria require no committed-state effect and require using a transaction after rollback to panic or return error
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.2-commit.md:41`
  - atomicity/design intent assumes discarded transactions are not later reused to mutate committed state

Current observed behavior:
- Existing targeted commit tests still pass:
  - `rtk go test ./store -run 'TestCommitApplies|TestCommitNetEffectInsertDelete|TestCommitProducesLocalChangesetsAcrossTransactions|TestApplyChangeset|TestTransactionUpdate'`
- Targeted runtime repro from the audit showed rollback reuse is live:
  - create tx
  - insert row
  - call `Rollback(tx)`
  - call `tx.Insert(...)` again
  - call `Commit(cs, tx)`
- observed output:
  - `post-rollback insert rowID=2 err=<nil>`
  - `post-rollback commit err=<nil> empty=false`
  - `committed rows after reuse=2`

Recommended resolution options:
1. Preferred code fix:
   - make rollback discard tx-local state (`tx.tx = nil` or equivalent cleared sentinel state)
   - add a reusable guard checked by `Insert`, `Delete`, `Update`, `GetRow`, `ScanTable`, and `Commit` so a rolled-back transaction panics or returns a deterministic error
   - add targeted regression tests for post-rollback method calls
2. Alternative implementation shape:
   - if panic is undesirable, return a stable sentinel error like `ErrTransactionClosed` from all post-rollback operations
   - whichever route is chosen, it must be consistent and tested

Suggested follow-up tests:
- rollback after inserts/deletes leaves committed state unchanged
- post-rollback `Insert`, `Delete`, `Update`, and `Commit` each fail deterministically
- provisional RowIDs consumed before rollback are not reused by subsequent fresh transactions

### TD-012: SPEC-001 E5 Story 5.3 `StateView` API is entirely missing

Status: open
Severity: medium
First found: SPEC-001 Epic 5 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3d (`SPEC-001 E5: Transaction Layer`)

Summary:
- The repository implements much of the transaction behavior directly inside `Transaction` methods, and targeted transaction tests pass.
- But Story 5.3's documented `StateView` surface does not exist at all: there is no `StateView` type, no `NewStateView`, and no public `GetRow`/`ScanTable`/`SeekIndex`/`SeekIndexRange` unified read abstraction.
- This leaves a major documented Epic 5 contract missing even though some equivalent logic is currently inlined elsewhere.

Why this matters:
- The decomposition makes `StateView` the explicit seam that merges committed state and tx-local state and then feeds Stories 5.4–5.6.
- Without the named abstraction, the repo lacks the documented reusable transaction-visibility layer and cannot satisfy consumers or tests written to the specified API surface.
- Inlining pieces of the logic inside `Transaction` is operationally workable, but it is thinner than spec and leaves index-query parts of Story 5.3 absent as a first-class interface.

Related code:
- repo-wide search under `store/` found no `type StateView` and no `NewStateView`
- `store/transaction.go:31-247`
  - `Transaction.Insert`, `Delete`, `Update`, `GetRow`, and `ScanTable` inline visibility logic against `CommittedState` + `TxState`
- `store/snapshot.go:10-86`
  - committed read-view/snapshot exists, but it is a different contract from the transaction-local `StateView` described in Story 5.3
- repo-wide search under `store/` found no `SeekIndex` / `SeekIndexRange` methods on a transaction-layer view object

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.3-state-view.md:21-49`
  - defines `StateView`, `NewStateView`, `GetRow`, `ScanTable`, `SeekIndex`, and `SeekIndexRange`
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.3-state-view.md:50-63`
  - acceptance criteria explicitly cover the unified read contract and nil-map handling
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.4-transaction-insert.md:16-26,43`
  - Transaction story expects `StateView` to exist and be wrapped by `Transaction`
- `docs/decomposition/001-store/epic-5-transaction-layer/EPIC.md:15-18,23-28`
  - Story 5.3 is a first-class dependency for Stories 5.4–5.6

Current observed behavior:
- Existing targeted transaction tests pass:
  - `rtk go test ./store -run 'TestTransactionInsertVisible|TestTransactionDeleteCollapse|TestTransactionCommittedDelete|TestTransactionUpdate|TestTransactionInsertUndeletesCommittedPrimaryKey|TestTransactionInsertUndeletesCommittedSetSemanticsRow|TestCommitDeleteIdenticalReinsertCollapsesToEmptyChangeset'`
- Targeted public-API compile repro from the audit failed:
  - temporary package referenced `store.StateView` and `store.NewStateView(nil, nil)`
  - `rtk go test ./.tmp_store_stateview_api` failed with undefined symbol errors for both

Recommended resolution options:
1. Preferred code fix:
   - add `state_view.go` implementing `StateView` and `NewStateView`
   - move the shared committed+tx visibility logic there, including `SeekIndex` and `SeekIndexRange`
   - have `Transaction` wrap/use that abstraction so the implementation matches the documented layering
2. Alternative doc fix:
   - if the project intentionally wants to inline visibility logic into `Transaction`, update the Epic 5 decomposition to remove/promote Story 5.3 accordingly
   - this would still require reconciling the missing `SeekIndex` / `SeekIndexRange` public contract explicitly

Suggested follow-up tests:
- compile-time/public API test for `StateView` and `NewStateView`
- direct `StateView` tests for `GetRow`, `ScanTable`, `SeekIndex`, and `SeekIndexRange`
- regression test proving empty/nil per-table tx maps are handled without panic

### TD-011: SPEC-001 E4 Story 4.1's documented `Index.unique` field appears to be stale doc drift

Status: doc-drift
Severity: low
First found: SPEC-001 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3c (`SPEC-001 E4: Table Indexes & Constraints`)

Summary:
- The live implementation of table indexes and constraints appears operationally correct: index wrappers are created, synchronous maintenance works, PK/unique/set-semantics enforcement is present, and targeted tests pass.
- However, Story 4.1 still specifies an `Index` struct with a separate `unique bool` field populated from schema.
- Live code omits that field and reads uniqueness directly from `idx.schema.Unique` / `idx.schema.Primary` instead.

Why this is classified as doc drift, not a product bug:
- The behavior the field was meant to support is present.
- Using `IndexSchema` as the single source of truth is simpler and avoids redundant state that could drift.
- I found no acceptance-criteria failure caused by the missing field; the mismatch is in the documented struct shape, not the operational constraint behavior.

Related code:
- `store/index.go:8-19`
  - `Index` contains only `schema *schema.IndexSchema` and `btree *BTreeIndex`
  - `NewIndex(...)` does not copy `schema.Unique` into a separate field
- `store/table.go:155-175`
  - uniqueness / PK enforcement reads `idx.schema.Unique` and `idx.schema.Primary` directly
- `store/commit.go:86-97`
  - commit revalidation also reads schema uniqueness directly

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-4-table-indexes-constraints/story-4.1-index-wrapper.md:16-27`
  - documents `Index` with fields `schema`, `unique`, and `btree`
- `docs/decomposition/001-store/epic-4-table-indexes-constraints/story-4.1-index-wrapper.md:51-52`
  - acceptance criteria explicitly mention each `Index` having the correct unique flag

Current observed behavior:
- Targeted constraint/index tests passed during audit:
  - `rtk go test ./store -run 'TestTablePKViolation|TestTableSetSemantics|TestTableDeleteReinsert|TestTransactionInsertUndeletesCommittedPrimaryKey|TestTransactionInsertUndeletesCommittedSetSemanticsRow|TestCommitDeleteIdenticalReinsertCollapsesToEmptyChangeset'`
- No grounded runtime failure was found in Epic 4 behavior itself; the mismatch is limited to the documented internal struct shape.

Recommended resolution:
- Update Story 4.1 to describe the current simpler shape:
  - `Index` wraps `IndexSchema` + `BTreeIndex`
  - uniqueness and primary-ness are derived from `schema.Unique` / `schema.Primary`
- If the project actually wants a cached `unique` field for performance or clarity, add it deliberately and test it; otherwise the docs should stop promising the redundant field.

Suggested follow-up checks:
- When the docs are patched, re-scan nearby acceptance text for any other references assuming duplicated `unique` state on `Index`
- Keep Epic 4 tests focused on observable behavior (constraint enforcement / index maintenance), not internal redundant fields

### TD-010: SPEC-001 E3 is missing the documented `Bound` type and helper constructors

Status: open
Severity: medium
First found: SPEC-001 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3b (`SPEC-001 E3: B-Tree Index Engine`)

Summary:
- The core `IndexKey`, `BTreeIndex`, `SeekRange`, `Scan`, and multi-column behavior are implemented and tested.
- But Story 3.1 explicitly requires a public `Bound` type plus `UnboundedLow`, `UnboundedHigh`, `Inclusive`, and `Exclusive` helper constructors, and none of those symbols exist in live code.
- This leaves the documented range-endpoint API incomplete and creates a mismatch between Story 3.1 and the later Story 3.3 note that still references Bound semantics.

Why this matters:
- Even though the current runtime path uses `SeekRange(low, high *IndexKey)` with nil for unbounded endpoints, the decomposition/spec still defines `Bound` as part of the Epic 3 public contract.
- This is a real API-surface gap: consumers written to the documented Story 3.1 surface do not compile.
- The missing helper type also leaves the codebase without a clean forward-compatible place to express inclusive/exclusive endpoint semantics if the planned future variant lands.

Related code:
- `store/index_key.go:5-42`
  - implements `IndexKey`, `NewIndexKey`, `Len`, `Part`, `Compare`, `Equal`, but no `Bound`
- `store/btree_index.go:85-110`
  - `SeekRange(low, high *IndexKey)` is implemented directly using nil for unbounded endpoints
- repo-wide search under `store/` returned no `Bound`, `UnboundedLow`, `UnboundedHigh`, `Inclusive`, or `Exclusive`

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-3-btree-index-engine/story-3.1-index-key.md:35-48`
  - requires the `Bound` struct and four convenience constructors
- `docs/decomposition/001-store/epic-3-btree-index-engine/story-3.1-index-key.md:56-57`
  - acceptance criteria explicitly cover Bound construction semantics
- `docs/decomposition/001-store/epic-3-btree-index-engine/story-3.3-range-scan.md:26-28`
  - later story still references Bound semantics even though the current signature uses `*IndexKey`

Current observed behavior:
- Existing targeted index tests pass:
  - `rtk go test ./store -run 'TestIndexKeyCompare|TestIndexKeyMultiColumn|TestIndexKeyPrefixOrdering|TestBTreeInsertSeek|TestBTreeRemove|TestBTreeSeekRange|TestBTreeScan|TestExtractKey|TestBTreeSeekRangeNilBoundsAndMultiColumnBytesOrdering'`
- Targeted public-API compile repro from the audit failed:
  - temporary package referenced `store.Bound`, `store.UnboundedLow()`, `store.UnboundedHigh()`, `store.Inclusive(...)`, and `store.Exclusive(...)`
  - `rtk go test ./.tmp_store_bound_api` failed with undefined symbol errors for all of them

Recommended resolution options:
1. Preferred code fix:
   - add the `Bound` type and helper constructors in `store/index_key.go` (or another Epic 3-owned file)
   - keep the current `SeekRange(*IndexKey, *IndexKey)` API if desired, but surface the documented helper contract so later evolution has a stable home
   - add compile-time/public API tests for the new symbols
2. Alternative doc fix:
   - if the project intentionally simplified away `Bound`, update Story 3.1 and any later references so the public contract is consistently the nil-or-`*IndexKey` API only
   - this would be doc-drift cleanup rather than a code feature gap, but it needs to be made explicit

Suggested follow-up tests:
- compile-time API test for `Bound` and the four helper constructors
- direct constructor tests asserting unbounded/inclusive/exclusive flags are set correctly
- if a future `SeekBounds` variant is added, acceptance tests for mixed inclusive/exclusive endpoint semantics

### TD-009: SPEC-001 E2 table storage does not detach inserted `ProductValue`s from caller mutation

Status: resolved
Severity: high
First found: SPEC-001 Epic 2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3a (`SPEC-001 E2: Schema & Table Storage`)

Summary:
- The store's table layer keeps the caller's `ProductValue` slice directly when inserting rows.
- Because `ProductValue` is a slice, mutating the caller-owned row after `InsertRow(...)` changes the stored row in place.
- `GetRow(...)` also returns that same stored slice directly, so callers can mutate committed table contents simply by editing the returned row.

Why this matters:
- SPEC-001 explicitly requires `ProductValue` contents to be immutable once inserted into the store or a transaction buffer.
- The spec also requires caller-provided bytes to be copied on insert unless exclusive ownership can be proven. The current table layer does not create a detached row copy at insert time, so slice-level mutation is observable even when individual `Value` bytes are internally copied.
- This breaks the basic storage contract for E2 and creates a hidden aliasing hazard for every later subsystem built on table reads/writes.

Related code:
- `store/table.go:52-73`
  - `InsertRow(...)` stores `row` directly in `t.rows[id]` with no `ProductValue.Copy()` or equivalent detach step
- `store/table.go:102-105`
  - `GetRow(...)` returns the stored `ProductValue` directly
- `types/product_value.go:46-58`
  - a deep-copy helper already exists (`ProductValue.Copy()`), but the store layer does not use it here
- `docs/decomposition/001-store/SPEC-001-store.md:83-84`
  - spec invariants say bytes are copied on insert and `ProductValue` contents are immutable once inserted

Related spec / decomposition docs:
- `docs/decomposition/001-store/SPEC-001-store.md:80-84`
  - required immutability invariants for inserted values/rows
- `docs/decomposition/001-store/epic-2-schema-table-storage/story-2.2-table-row-storage.md:25-37`
  - table storage story owns the insert/get/delete/scan surface where the aliasing occurs
- `docs/decomposition/001-store/epic-2-schema-table-storage/story-2.3-row-validation.md:12-34`
  - validation is intentionally separate; storage still owns what gets retained after insert

Current observed behavior:
- Existing targeted package tests still pass:
  - `rtk go test ./store -run 'TestTableInsertGetDelete|TestTableScan|TestValidateRow|TestAllocRowIDNeverResets'`
- Targeted runtime repro from the audit:
  - insert `row := ProductValue{NewString("hello")}`
  - mutate `row[0] = NewString("mutated-after-insert")`
  - `GetRow(...)` then returns `mutated-after-insert`
  - mutate the row returned by `GetRow(...)`
  - subsequent `GetRow(...)` returns `mutated-via-getrow`
  - observed output:
    - `after caller mutation: mutated-after-insert`
    - `after getrow mutation: mutated-via-getrow`

Recommended resolution options:
1. Preferred code fix:
   - copy rows on insertion into table storage (`row.Copy()`), and return detached copies from `GetRow(...)`, `DeleteRow(...)`, and `Scan()` if the contract is meant to be fully read-only to callers
   - at minimum, ensure inserted rows are detached from caller-owned memory so post-insert mutation cannot rewrite stored state
2. Follow-up design clarification:
   - decide whether row retrieval APIs are also intended to be immutable snapshots; if yes, defensive copies are needed on read paths too
   - if read-path mutability is intentionally allowed, update the spec/docs because that is not how the current invariants read

Suggested follow-up tests:
- mutate the caller-owned `ProductValue` after `InsertRow(...)` and assert stored data is unchanged
- mutate the `ProductValue` returned from `GetRow(...)` and assert future reads are unchanged
- repeat with `Bytes` columns to ensure both row-slice and byte-slice immutability guarantees hold together

### TD-008: SPEC-006 E6 engine-side schema export surface is entirely missing

Status: open
Severity: high
First found: SPEC-006 Epic 6 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 2 / Step 2e (`SPEC-006 E6: Schema export`)

Summary:
- The schema export epic is absent from live code.
- There are no export value types (`SchemaExport`, `TableExport`, `ColumnExport`, `IndexExport`, `ReducerExport`) and `Engine` does not implement `ExportSchema()`.
- This means the repo currently has no engine-side path to produce the `schema.json` contract required for codegen/tooling.

Why this matters:
- Epic 6 is the only documented bridge from the immutable runtime schema into the external codegen/tooling interface.
- Without `ExportSchema()`, even a minimal future `shunter-codegen` CLI would have no blessed engine contract to consume.
- This is not just a missing convenience method; it blocks the entire schema-export/codegen surface promised by SPEC-006 §12.

Related code:
- repo search under `schema/` found no `export.go`
- repo search under `schema/` found no `SchemaExport`, `TableExport`, `ColumnExport`, `IndexExport`, `ReducerExport`, or `ExportSchema` symbols
- `schema/build.go:8-19`
  - `Engine` exists but exposes only `Registry()` and `Start(...)`
- repo search found no `cmd/shunter-codegen` tree either, reinforcing that the export/codegen epic has not started

Related spec / decomposition docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md:505-560`
  - defines the schema export surface and the export structs
- `docs/decomposition/006-schema/epic-6-schema-export/story-6.1-export-types.md:16-67`
  - requires all export value types with JSON-friendly fields
- `docs/decomposition/006-schema/epic-6-schema-export/story-6.2-export-schema.md:18-36`
  - requires `func (e *Engine) ExportSchema() *SchemaExport`
- `docs/decomposition/006-schema/epic-6-schema-export/story-6.4-export-json-contract.md:16-26`
  - requires JSON round-trip/snapshot semantics for exported values
- `docs/decomposition/006-schema/epic-6-schema-export/story-6.3-codegen-tool-contract.md:16-49`
  - depends on the exported `schema.json` interface

Current observed behavior:
- `rtk go test ./schema` still passes because there are no export-surface tests in the package today
- targeted public-API compile repro from the audit failed:
  - temporary package referenced `schema.SchemaExport`, `schema.TableExport`, `schema.ColumnExport`, `schema.IndexExport`, `schema.ReducerExport`, and `(*schema.Engine).ExportSchema()`
  - `rtk go test ./.tmp_schema_export_api` failed with undefined symbol errors for all of them

Recommended resolution options:
1. Preferred code fix:
   - add `schema/export.go` implementing the export value types and `Engine.ExportSchema()` traversal over `SchemaRegistry`
   - add `schema/export_test.go` and `schema/export_json_test.go` covering ordering, lifecycle reducer export, JSON round-trip, and detached snapshot semantics
   - implement the downstream `cmd/shunter-codegen` contract once the engine export surface exists
2. If deferring implementation intentionally:
   - document Epic 6 as not yet started in TECH-DEBT / planning docs so the current green test surface is not mistaken for feature completeness

Suggested follow-up tests:
- compile-time/public API test for all export types and `Engine.ExportSchema()`
- export ordering test covering user tables, system tables, reducers, and lifecycle reducers
- JSON round-trip test for `SchemaExport`
- snapshot-detachment test proving mutations to the returned export value do not affect the registry

### TD-007: SPEC-006 E5 `SchemaRegistry` table lookups are mutable, violating the read-only contract

Status: open
Severity: high
First found: SPEC-006 Epic 5 Story 5.4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 2 / Step 2d (`SPEC-006 E5: Validation, Build & SchemaRegistry`)

Summary:
- `SchemaRegistry` is documented and commented as a read-only, immutable, concurrent-safe view, but `Table(...)` and `TableByName(...)` return pointers to the registry's internal `TableSchema` storage.
- Callers can mutate the returned `TableSchema` and its nested column/index slices, and those mutations are then visible to later readers of the same registry.
- This directly breaks Story 5.4's immutability contract and undermines the concurrency guarantee that depends on no post-build mutation.

Why this matters:
- Downstream subsystems are supposed to consume `SchemaRegistry` as frozen metadata. Mutable lookup results let any caller rewrite table names, columns, and index definitions after `Build()`.
- The current interface is not merely "not deeply immutable" in theory; the mutation is observable immediately in live code.
- This creates a hidden shared-state hazard for SPEC-001/002/003 consumers, which are meant to trust the registry as stable schema truth.

Related code:
- `schema/registry.go:18-21`
  - registry stores `tables []TableSchema` and maps IDs/names to pointers into that slice
- `schema/registry.go:40-43`
  - `byID` / `byName` are populated with `&r.tables[i]`
- `schema/registry.go:59-66`
  - `Table(...)` and `TableByName(...)` return those internal pointers directly
- `schema/build.go:8-16`
  - engine/registry are described as immutable in public comments

Related spec / decomposition docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md:332-360`
  - `SchemaRegistry` is the produced contract for downstream systems and is described as immutable after `Build()`
- `docs/decomposition/006-schema/epic-5-validation-build/story-5.4-schema-registry.md:14-20`
  - Story 5.4 defines the registry as a read-only, immutable view with lookup maps populated once
- `docs/decomposition/006-schema/epic-5-validation-build/story-5.4-schema-registry.md:31-37`
  - the concurrency strategy is immutability, not locking

Current observed behavior:
- Existing tests still pass:
  - `rtk go test ./schema -run 'TestBuild|TestRegistry|TestBuildSystemTablesMatchSpecExactly|TestRegistryReducersPreserveRegistrationOrderAndFreshSlice'`
- Targeted runtime repro from the audit:
  - build a registry with table `players`
  - call `reg.TableByName("players")`, then mutate `ts.Name` and `ts.Columns[0].Name`
  - a later `reg.Table(schema.TableID(0))` returns the mutated values
  - observed output:
    - `before: players id`
    - `after: mutated mutated_col`

Recommended resolution options:
1. Preferred code fix:
   - make `Table(...)` / `TableByName(...)` return defensive copies of `TableSchema`, including copied `Columns` and `Indexes` slices
   - keep internal registry storage private and never expose pointers into it
   - add regression tests proving caller mutation of returned schemas does not affect future lookups
2. Alternative API redesign:
   - change `SchemaRegistry` lookup methods to return value copies instead of pointers
   - this is a bigger cross-spec change and should be reflected in SPEC-006 / downstream docs if chosen

Suggested follow-up tests:
- mutate the result of `Table(...)` and assert a subsequent `Table(...)` call is unchanged
- mutate the result of `TableByName(...)` and assert a subsequent `TableByName(...)` call is unchanged
- specifically verify nested slice immutability by mutating returned `Columns` / `Indexes` entries

### TD-006: SPEC-006 E3.2 does not expose schema-facing `ReducerHandler` / `ReducerContext` aliases

Status: open
Severity: medium
First found: SPEC-006 Epic 3 Story 3.2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 2 / Step 2c (`SPEC-006 E3.2: Reducer registration`)

Summary:
- Reducer registration behavior is implemented in `schema/builder.go`, and validation/registry wiring are present, but the schema package does not expose `ReducerHandler` or `ReducerContext` at all.
- The current public signatures use `types.ReducerHandler` and `*types.ReducerContext` directly, which leaks the lower-level `types` package through the schema-facing API.
- SPEC-006 Story 3.2 explicitly assigns a schema-surface reducer contract: either re-export the SPEC-003 reducer types from schema or define aliases there until the executor-owned package exists.

Why this matters:
- The decomposition/spec treats reducer registration as part of the schema builder API, not as a requirement for callers to import an internal/shared `types` package.
- This is an API-shape mismatch, not just a naming preference: code written to the documented `schema` surface cannot compile today.
- Leaving the low-level package exposed here weakens the intended ownership boundary between schema registration and executor/runtime internals.

Related code:
- `schema/builder.go:11-12`
  - builder lifecycle fields are typed as `func(*types.ReducerContext) error`
- `schema/builder.go:20-23`
  - reducer entries store `types.ReducerHandler`
- `schema/builder.go:90-115`
  - public `Reducer`, `OnConnect`, and `OnDisconnect` methods all use `types.*` in their signatures
- `schema/registry.go:11-14,23-26,75-92`
  - `SchemaRegistry` and implementation also expose `types.ReducerHandler` / `*types.ReducerContext`
- `types/reducer.go:6-18`
  - canonical reducer types currently live only in `types`

Related spec / decomposition docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md:225-246`
  - SPEC-006 presents reducer registration as part of the schema API using `ReducerHandler` / `ReducerContext` in the schema-facing examples
- `docs/decomposition/006-schema/epic-3-builder-registration/story-3.2-reducer-registration.md:18-30`
  - Story 3.2 deliverable requires a `ReducerHandler` type alias re-exported from SPEC-003 or defined here if SPEC-003 is not yet built
- `docs/decomposition/006-schema/epic-3-builder-registration/story-3.2-reducer-registration.md:43-45`
  - design notes treat lifecycle vs ordinary reducer signatures as intentional API surface owned by this slice
- `docs/EXECUTION-ORDER.md:176`
  - execution order explicitly calls this slice out as the producer of `Reducer`, `OnConnect`, `OnDisconnect` registration

Current observed behavior:
- Operational behavior is otherwise healthy:
  - `rtk go test ./schema -run 'TestBuilder|TestRegistry|TestBuildDuplicateReducerName|TestBuildReducerReservedName'`
    passed during audit
- Public-API compile repro from the audit:
  - temporary package using `var _ schema.ReducerHandler` and `var _ *schema.ReducerContext`
  - `rtk go test ./.tmp_schema_api_audit`
    failed with:
    - `undefined: schema.ReducerHandler`
    - `undefined: schema.ReducerContext`

Recommended resolution options:
1. Preferred code fix:
   - add schema-package aliases such as `type ReducerHandler = types.ReducerHandler` and `type ReducerContext = types.ReducerContext`
   - update public schema signatures and registry interfaces to use the schema-owned names
   - add compile-time tests proving callers can use reducer registration via `schema.ReducerHandler` / `*schema.ReducerContext`
2. Alternative doc fix:
   - if the project intentionally wants reducer registration to expose `types.*`, update SPEC-006 §4.3 and Story 3.2 to document that leakage explicitly
   - this would still be a less clean public API than the current decomposition promises

Suggested follow-up tests:
- compile-time API test that `schema.ReducerHandler` and `schema.ReducerContext` exist
- builder/registry tests using only schema-package names in public signatures
- a regression test preventing future reintroduction of `types.*` into schema-facing examples/contracts

### TD-005: SPEC-006 E4 does not honor `shunter:"-"` on anonymous embedded fields

Status: open
Severity: medium
First found: SPEC-006 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 2 / Step 2b (`SPEC-006 E4: Reflection path`)

Summary:
- The reflection path mostly implements Story 4.1/4.2/4.3, but `discoverFields` handles anonymous embedding before it parses the `shunter` tag.
- That ordering violates SPEC-006 §11.1, which requires `shunter:"-"` exclusion to run first for every field.
- As a result, an anonymous embedded non-pointer struct tagged `shunter:"-"` is still flattened into the schema, and an anonymous embedded pointer-to-struct tagged `shunter:"-"` still fails registration instead of being skipped.

Why this matters:
- The spec's ordered field-discovery contract is not just stylistic; it defines which reflected fields are part of the public schema surface.
- Today, callers cannot use `shunter:"-"` to suppress an embedded helper/base struct even though the spec says exclusion happens before embedding logic.
- The missing case is easy to miss because the current tests cover exclusion and embedding separately, but not exclusion on an anonymous embedded field.

Related code:
- `schema/reflect.go:31-65`
  - skips unexported fields, then immediately processes anonymous fields before tag parsing
  - `ParseTag(...)` is not called until after the embedded-pointer error / recursive flattening path
- `schema/register_table.go:20-30`
  - `RegisterTable[T]` depends directly on `discoverFields`, so the bad ordering affects the public API
- `schema/reflect_test.go:71-118`
  - has coverage for `shunter:"-"` on ordinary fields and for embedded pointer rejection, but no combined anonymous-embedded exclusion case

Related spec / decomposition docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md:478-483`
  - field-discovery order requires `shunter:"-"` skip before anonymous-embedding handling
- `docs/decomposition/006-schema/SPEC-006-schema.md:485-487`
  - flattened embedding and unexported-field behavior are separate rules after the ordered per-field decision
- `docs/decomposition/006-schema/epic-4-reflection-engine/story-4.1-field-discovery.md:26-35`
  - Story 4.1 deliverable lists `shunter:"-"` skip before embedded non-pointer recursion / embedded pointer rejection
- `docs/decomposition/006-schema/epic-4-reflection-engine/story-4.3-register-table-integration.md:16-21`
  - `RegisterTable[T]` is supposed to expose the reflection pipeline faithfully through the public API

Current observed behavior:
- Existing package tests still pass: `rtk go test ./schema`
- Targeted runtime repro from the audit:
  - `ExcludedEmbedded struct { Embedded \`shunter:"-"\`; Name string }` registers as columns `[id name]` instead of skipping the embedded fields
  - `ExcludedEmbeddedPtr struct { *Embedded \`shunter:"-"\`; Name string }` returns `schema error: ExcludedEmbeddedPtr.Embedded: embedded pointer-to-struct is not supported` instead of skipping the excluded field

Recommended resolution options:
1. Preferred code fix:
   - in `discoverFields`, parse the tag before anonymous-embedding handling
   - if `td.Exclude` is true, skip the field immediately regardless of whether it is ordinary or anonymous
   - preserve the current path/error context for non-excluded embedded pointer failures
2. Test fix required alongside code fix:
   - add reflection-path tests for excluded anonymous embedded struct and excluded anonymous embedded pointer-to-struct cases
   - add a public `RegisterTable` integration test proving the built schema omits excluded embedded fields

Suggested follow-up tests:
- `discoverFields` should skip `Embedded \`shunter:"-"\`` entirely
- `discoverFields` should skip `*Embedded \`shunter:"-"\`` instead of erroring
- `RegisterTable` + `Build` should produce only non-excluded outer fields when an embedded helper struct is tagged out

### TD-004: SPEC-006 Story 5.6 schema compatibility checking is entirely missing

Status: open
Severity: high
First found: SPEC-006 Epic 5 audit
Execution-order context:
- not on the earliest critical path for Phase 1, but it is part of the current implemented `validation/build` surface and is explicitly required by Epic 5 before schema/runtime startup can be considered spec-complete

Summary:
- The repo implements most of SPEC-006 validation/build orchestration, system-table registration, and schema registry behavior, but the entire schema-version compatibility layer from Story 5.6 is absent.
- There is no `version.go`, no `CheckSchemaCompatibility(...)`, no `SnapshotSchema` type, no `ErrSchemaMismatch`, and `Engine.Start(...)` is a stub that does not compare registered schema against snapshot state.

Why this matters:
- The spec requires startup to reject incompatible schema/snapshot combinations using both version and structural comparison.
- Without this layer, the current engine surface has no guardrail against schema drift at runtime once snapshot/recovery work lands.
- This is more than a doc mismatch: it is an unimplemented contract that other subsystems (especially SPEC-002 recovery) are expected to rely on.

Related code:
- `schema/build.go:8-19`
  - `Engine` exists and `Start(ctx)` is currently a stub returning nil
- `schema/build.go:21-110`
  - `Build()` orchestration is implemented, but no startup compatibility hook exists
- `schema/registry.go:5-96`
  - `SchemaRegistry` implementation exists and could support comparison, but no comparison function is present
- repo search results:
  - no `schema/version.go`
  - no `CheckSchemaCompatibility`
  - no `SnapshotSchema`
  - no `ErrSchemaMismatch`

Related spec / decomposition docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md:305-312`
  - startup requires matching schema version and exact structural match or `ErrSchemaMismatch`
- `docs/decomposition/006-schema/SPEC-006-schema.md:321-326`
  - v1 schema mismatch policy is startup failure, not online migration
- `docs/decomposition/006-schema/epic-5-validation-build/story-5.6-schema-version-check.md:18-41`
  - requires `CheckSchemaCompatibility`, `SnapshotSchema`, `ErrSchemaMismatch`, and running comparison during `Engine.Start()`
- `docs/decomposition/006-schema/epic-5-validation-build/EPIC.md:20,30-42`
  - Story 5.6 is a named deliverable with dedicated file ownership

Current observed behavior:
- `Build()` works and tests pass for validation, system tables, ID assignment, and registry behavior
- `Engine.Start()` is still a no-op stub
- no runtime schema compatibility comparison exists at all

Recommended resolution options:
1. Preferred code fix:
   - implement `schema/version.go` with `SnapshotSchema`, `ErrSchemaMismatch`, and `CheckSchemaCompatibility(...)`
   - wire the compatibility check into `Engine.Start()` at the point where snapshot metadata becomes available from SPEC-002
   - add tests for version mismatch, structural mismatch, and nil/no-snapshot success paths
2. Temporary doc clarification if intentionally deferred:
   - record explicitly in docs/TECH-DEBT that Story 5.6 is deferred until SPEC-002 snapshot schema types exist, so current `Start()` should not be treated as spec-complete runtime startup

Suggested follow-up tests:
- matching version + identical structure → nil error
- different version, same structure → `ErrSchemaMismatch`
- same version, table/column/index structural diff → `ErrSchemaMismatch` with detail
- no snapshot / fresh start → compatible
- `Engine.Start()` invokes compatibility check once snapshot metadata is available

---

## Code review audit (2026-04-15)

This section is distinct from the per-spec audit above. It is a broad code-quality sweep across all live Go packages (`auth`, `bsatn`, `commitlog`, `executor`, `protocol`, `schema`, `store`, `subscription`, `types`) — non-test code only — looking for:

1. **Correctness bugs** — races, durability hazards, broken invariants, error swallowing, byte-order/size handling
2. **Go idiom misuse** — non-idiomatic error chains, `any` where generics fit, hand-rolled stdlib equivalents, panics in library code, missing `Stringer`/docs
3. **Duplication** — parallel switch ladders, copy-pasted helpers, repeated kind/predicate walks

Methodology: six parallel `code-reviewer` agents, one per package slice, plus a cross-package duplication sweep. Findings are grounded by direct file reads; speculative items are flagged `FOLLOWUP`.

ID scheme: `TD-027` through `TD-12x`, grouped by package. Severity tags inline: `BUG` (broken / data-loss / race), `SMELL` (works but wrong shape), `DUP` (duplication), `FOLLOWUP` (verify before fixing).

Verification commands not re-run for this pass — these are static-read findings. Each item should be verified by adding a focused test or compile-only repro before remediation.

### A. Concurrency & durability — critical

These are correctness-fatal under concurrent load or restart and are the highest-priority items in this pass.

- **TD-027** [BUG] `executor/executor.go:121-153` — `Submit` / `SubmitWithContext` read `e.fatal` and `e.shutdown` (written by executor goroutine in `postCommit` and by `Shutdown`) with no atomics or mutex. `Shutdown` also closes the inbox while concurrent `Submit` callers may still be sending. Data race + "send on closed channel" panic risk under shutdown contention. Fix: convert flags to `atomic.Bool`; gate inbox sends through a `sync.Once`-backed close path that returns `ErrExecutorShutdown` instead of touching a possibly-closed channel.

- **TD-028** [BUG] `commitlog/durability.go:69-87` — `EnqueueCommitted` reads `fatalErr`/`closing` under the lock, releases it, then sends on `dw.ch`. Worker may set `fatalErr` between the unlock and send → next enqueue blocks forever (channel full, no reader). `Close` calls `close(dw.ch)` while a producer holds a slot mid-send → "send on closed channel" panic. Fix: send under the same lock, or use a non-blocking send with a "closed" sentinel checked first.

- **TD-029** [BUG] `commitlog/durability.go:128-138` — Drain loop's `if !ok { break }` only breaks the inner `select`, not the enclosing `for`. A closed channel during drain spins, appending zero-valued items until the count expires. Fix: replace `break` with `goto process` (or restructure as a labeled loop) so a closed channel exits drain immediately.

- **TD-030** [BUG] `commitlog/durability.go:51-65` — Confirms TD-025 from the earlier audit. `NewDurabilityWorker` calls `CreateSegment` → `os.Create`, which truncates an existing segment file on restart. Any committed records in the existing segment for `startTxID` are silently lost. Fix: probe for existing file, open with `O_RDWR|O_APPEND`, scan to last valid record, restore `lastTx`/`size`, and resume.

- **TD-031** [BUG] `store/commit.go:38-50` — Insert phase mutates committed state in place via `table.InsertRow`. Mid-loop failure (e.g. unique violation introduced by another tx that revalidate missed) leaves all prior deletes and N-1 inserts already applied, function returns error, committed state is half-mutated. No rollback. Atomicity broken. Fix: stage both phases into a buffer and apply atomically, or snapshot/restore committed state on failure.

- **TD-032** [BUG] `store/commit.go:55-58` — `Rollback` only sets a flag that nothing reads. A transaction can be rolled back, then `Commit(cs, tx)` immediately succeeds and applies it. Fix: either remove `Rollback` or have `Commit` short-circuit on `tx.rolledBack`.

- **TD-033** [BUG] `store/commit.go:30-36` — Delete phase does `if oldRow, ok := table.DeleteRow(rowID); ok` and silently skips RowIDs not found. A delete recorded against a row that vanished between tx-start and commit produces an empty changeset entry with no error, and `revalidateCommit` only revalidates inserts. Fix: return error when `DeleteRow` returns `false`, or revalidate deletes before mutating.

- **TD-034** [BUG] `store/committed_state.go:24-33` — `RegisterTable` mutates `tables` map without holding the write lock; `Table()` reads without lock. Both nominally "called only at startup" but Go's race detector flags any unsynchronized map access. Fix: take `cs.mu.Lock()` in `RegisterTable` and `RLock()` in `Table()`, or document and use a `sync.Once`-style init phase.

- **TD-035** [BUG] `store/snapshot.go:28-31` — `Snapshot()` acquires `RLock` and stores no goroutine ownership. A leaked snapshot (no `Close`) silently blocks all commits forever. Same-goroutine `Lock` after `RLock` deadlocks (no recursive locking). Fix: add finalizer or document the contract more loudly with a runtime guard.

- **TD-036** [BUG] `executor/registry.go:6-9` — `ReducerRegistry` has zero synchronization. `Register` writes the map; `Lookup`/`LookupLifecycle`/`All` are called from the executor goroutine but also from any caller that holds the registry. The `frozen` flag is a plain bool. If anyone calls `Register` concurrently with `Lookup` (e.g. registration races startup), this races. Fix: document "single-threaded build, frozen before publish" + `Freeze()` happens-before fence (atomic.Bool), or guard with `sync.RWMutex`.

- **TD-037** [BUG] `executor/scheduler_worker.go:75-78` — `defer timer.Stop()` is registered inside the `for` loop in `Run`, so timers accumulate across every iteration and only release when `Run` returns. Long-running schedulers leak one timer per scan. Fix: stop the timer explicitly before the next iteration (wrap each iteration in a func, or call `timer.Stop()` after the select).

- **TD-038** [BUG] `executor/scheduler_worker.go:160` — `s.inbox <- cmd` blocks indefinitely with no `ctx.Done()` arm. If the executor is shut down or saturated while scan is enqueuing, the scheduler goroutine cannot exit (Run's outer select can never observe ctx cancellation). Fix: `select { case s.inbox <- cmd: case <-ctx.Done(): return }`; thread ctx into `enqueue`.

- **TD-039** [BUG] `executor/scheduler_worker.go:166-177` — `drainResponses` returns on `ctx.Done` leaving any in-flight executor goroutine writing to a now-unread buffered channel blocked forever once the buffer fills. Fix: keep draining until both ctx is done and respCh is empty (drain-with-timeout, or have executor stop sending via shutdown ordering).

- **TD-040** [BUG] `protocol/disconnect.go:44` — `close(c.OutboundCh)` is unsafe: any future writer goroutine sending on this channel will panic. The fan-out worker (Phase 8) and Epic 4 write loop are documented to send on `OutboundCh`; closing it from `Disconnect` (a non-sender) without synchronization sets up a "send on closed channel" panic the moment those layers land. Test at `disconnect_test.go:44` enforces the wrong invariant. Fix: drop the `close(OutboundCh)` here entirely; let the writer goroutine drain on `<-c.closed` and close the channel itself, OR add a `sendMu` + `sendClosed bool` guard on every send.

- **TD-041** [BUG] `protocol/upgrade.go:177-184` — Read pump and keepalive goroutines use `context.Background()`, severing them from `r.Context()` and any engine shutdown context. When the host process shuts down or the request context cancels, the goroutines keep running until the peer drops or idle timeout fires. Fix: derive a per-connection context from a long-lived server context (add `Server.ctx`), pass that to both pumps, and cancel it inside `Disconnect` so the read pump's `c.ws.Read(ctx)` unblocks immediately.

- **TD-042** [BUG] `protocol/disconnect.go:46-48` and `protocol/keepalive.go:80-82` — Two detached close goroutines, no `sync.WaitGroup` to track them, no use of `CloseHandshakeTimeout` from `ProtocolOptions` (defined at `options.go:26`, never read). On engine shutdown these accumulate (one per connection ever opened in this process if peers ignore Close handshakes for the full 5 s). Fix: track via `WaitGroup` on `Server`; use `CloseHandshakeTimeout` to bound the close.

- **TD-043** [BUG] `protocol/lifecycle.go:74-90` — Race between `mgr.Add(c)` (line 74) and the `mgr.Remove(c.ID)` on write-failure (lines 82, 87). A concurrent fan-out goroutine that resolves the ID between `Add` and the failed `Write` will deliver onto a `Conn` whose socket is about to close, and the delivery write races the close. The comment at line 71 ("Register before first send") flags the very window that is broken. Fix: hold a `Conn.admitMu` (write-locked here, read-locked by senders) across the entire admit-or-close transition.

- **TD-044** [BUG] `protocol/keepalive.go:48` — `context.WithTimeout(ctx, c.opts.PingInterval)` uses the ping cadence as the ping deadline. If `PingInterval == IdleTimeout/2` (default 15 s / 30 s) a stuck peer keeps `Ping` blocked for a full interval before the idle check runs, doubling the effective idle window. Fix: derive ping timeout from a separate option, or cap at `IdleTimeout - (now - lastActivity)`.

- **TD-045** [BUG] `subscription/manager.go:67-73` — `evaluate` mutates loop variable `u` (`u.SubscriptionID = subID`) and appends it. Because `u.Inserts`/`u.Deletes` slices alias the same backing array across every `(connID, subID)` tuple, downstream code that retains and mutates one connection's `Inserts` (e.g. encoding into a buffer that filters by predicate per-client, future row-redaction layer) corrupts every other subscriber. Silent cross-subscriber data leakage. Fix: `slices.Clone` the row slices, or document slice-immutable contract aggressively.

### B. Logic & error-handling bugs

- **TD-046** [BUG] `bsatn/decode.go:138-152` — `DecodeProductValueFromBytes` fabricates `RowShapeMismatchError{Got: len(ts.Columns)+1}` when trailing bytes exist. The `+1` is a lie — there could be many trailing bytes representing many extra columns, or fewer than one column's worth of garbage. Callers reading `Got` and computing `Got - Expected` get a meaningless `1`. Fix: introduce `ErrTrailingBytes = errors.New("bsatn: trailing bytes after row")` and return that wrapped with `r.pos`/`len(r.data)` for diagnostics.

- **TD-047** [BUG] `bsatn/decode.go:142-146` — When `DecodeProductValueFromBytes` catches `*RowShapeMismatchError` it discards the rich error and returns the bare `ErrRowLengthMismatch` sentinel, losing the table name and column counts. Callers can't render diagnostic messages or test for specific shape mismatches. Fix: return the original `shapeErr` (already implements `Error()`), or `fmt.Errorf("%w: %v", ErrRowLengthMismatch, shapeErr)`.

- **TD-048** [BUG] `auth/jwt.go:78` — `fmt.Errorf("%w: %v", ErrJWTInvalid, err)` discards the underlying error's `Unwrap` chain. The inner `err` from `jwt.Parse` may be `jwt.ErrTokenExpired`, `jwt.ErrTokenSignatureInvalid`, etc. — callers can no longer use `errors.Is(err, jwt.ErrTokenExpired)` to distinguish expired from malformed. SPEC-005 §4.3 likely wants different HTTP responses for expired vs malformed tokens. Fix: `fmt.Errorf("%w: %w", ErrJWTInvalid, err)` (Go 1.20+ multi-`%w`) so both unwrap chains are preserved.

- **TD-049** [BUG] `schema/tag.go:30-37` — Duplicate-key detection over-collapses `index` and `index:<name>` into the same key `"index"`. `unique,index:guild_score` triggers the duplicate trap on a second `index:`; `index,index:foo` is rejected as "duplicate" rather than the targeted "plain+named both appear" error. Fix: separate the dup-set into per-base-key buckets and key on whether the directive is parametric, or detect duplicates only for atoms (`primarykey`, `unique`, `name`, `-`) and let the index-combination rules handle index variants.

- **TD-050** [BUG] `schema/build.go:84-94` — Inner triple loop (`indexes -> columns -> td.Columns` linear search) for resolving column-name to index is O(I·C·C). When a column name is missing, the inner loop silently leaves `cols[k]==0` (pointing at the first column) instead of failing — `validateStructure` already guarantees referenced columns exist, so this is latent, but a future refactor will re-introduce the bug. Fix: precompute `colIdx := map[string]int{}` and panic on missing (invariant violation post-validation).

- **TD-051** [BUG] `store/recovery.go:36-39` — `ApplyChangeset` allocates a fresh RowID per replayed insert via `table.AllocRowID()`, but does not advance `nextID` past the maximum RowID seen during replay if inserts and deletes interleave. When recovery finishes, the next runtime insert can collide with a previously-allocated-and-deleted ID if the snapshot persisted a higher `nextID` that wasn't restored. Fix: explicitly seed `nextID` after replay or ensure recovery snapshot restores it via `SetNextID`. Tracked partially in `TableExportState` but never wired.

- **TD-052** [BUG] `commitlog/segment.go:243-244` — `OpenSegment` calls `fmt.Sscanf(base, "%d.log", &startTx)` and discards both the count and error. A malformed filename silently yields `startTx=0`, masking corruption. Fix: check the return values and error out on parse failure.

- **TD-053** [BUG] `executor/executor.go:208-218` — `handleCallReducer` writes to `cmd.ResponseCh` without checking `nil`. `dispatchSafely`'s panic path (line 175) checks `cmd.ResponseCh != nil`, proving nil is a contemplated input → guaranteed nil-panic on the happy path. Fix: early `if cmd.ResponseCh == nil { return }` or use a `respondReducer` helper mirroring `respondLifecycle`.

- **TD-054** [BUG] `executor/executor.go:262` — `fmt.Errorf("%v: %w", panicked, ErrReducerPanic)` formats the panic value with `%v` (lossy: a panic with `error` value loses its chain). Fix: when `panicked` is itself an `error`, wrap it: `fmt.Errorf("reducer panicked: %w (sentinel: %w)", err, ErrReducerPanic)` or `errors.Join`.

- **TD-055** [BUG] `executor/scheduler.go:115-127` — `Cancel` returns `false` when the schedule exists but `tx.Delete` failed, indistinguishable from "not found." Fix: change signature to `(bool, error)` or log the delete error before returning false.

- **TD-056** [BUG] `executor/lifecycle.go:165-174` — `deleteSysClientsRow` ranges `tx.ScanTable` and calls `tx.Delete` *during* iteration; if `ScanTable` returns a live (non-snapshot) iterator, deleting the row mutates the underlying structure during traversal. Returning immediately after the first delete is the only thing that saves it. Fix: collect the rowID first, then delete, or document `ScanTable`'s mutation safety.

- **TD-057** [BUG] `executor/registry.go:60-67` — `LookupLifecycle` does an O(n) scan; with two lifecycle slots and `Register` already knowing the kind, it's lossy: if two reducers somehow shared a kind, iteration order returns an arbitrary one. Fix: maintain a `lifecycle [2]*RegisteredReducer` array indexed by `LifecycleKind`, populated in `Register`.

- **TD-058** [BUG] `subscription/delta_dedup.go:18-66` — `ReconcileJoinDelta` tallies signed multiplicities asymmetrically across iteration order. SPEC-004 §6.3 requires net (inserts − deletes) per row across all 4+4 fragments; current code emits different fragments depending on whether `(insert, delete, insert)` or `(insert, insert, delete)` arrives. Fix: tally net counts (`net[key] += inserts; net[key] -= deletes`) in one pass, then emit `+net` to inserts or `-net` to deletes.

- **TD-059** [BUG] `subscription/manager.go:79-92` — `handleEvalError` calls `m.signalDropped(connID)` for *every* subscriber on a query when one query panics. A single broken predicate disconnects every client subscribed to it, including clients with healthy other subscriptions. SPEC-004 §11.1 (and the enclosing recovery story) drops the *subscription*, not the *connection*. Fix: emit `SubscriptionError` only; let the protocol layer decide whether to disconnect, or signal a finer-grained drop unit.

- **TD-060** [BUG] `subscription/placement.go:36-72` — `PlaceSubscription`/`RemoveSubscription` call `findColEqs(pred, t)` which does not descend into `Join.Filter` for the *Join itself*. For a `Join` whose filter is a `ColEq` on the LHS table, the placement walks `And` correctly but a `Join`-only ColEq is found and the subscription is placed in Tier 1, not Tier 2 → JoinEdge index never populated for that subscription, and Tier 2 candidate collection (lines 102-129) misses it on RHS-only changes. Fix: place a join-bearing predicate in *both* Tier 1 and Tier 2, or document and assert the §5.4 invariant precisely.

- **TD-061** [BUG] `subscription/register.go:69-95` — `initialQuery` for `Join` only consults `m.resolver.IndexIDForColumn(p.Right, p.RightCol)`. If validation accepted a join because only the *Left* side has an index (`HasIndex(p.Left, p.LeftCol)` returned true at `validate.go:114`), this hard-fails at registration with `ErrJoinIndexUnresolved` even though the schema is consistent. Fix: try LHS-indexed traversal as a fallback (drive from RHS scan, probe LHS index) before failing.

- **TD-062** [BUG] `protocol/compression.go:36-46` — `EncodeFrame` swallows the `WrapCompressed` error and silently downgrades to uncompressed instead of returning an error. The "we never panic in delivery" comment hides that the client negotiated gzip and is now receiving a frame the spec says must be gzipped. Fix: return `([]byte, error)` and let the caller decide.

- **TD-063** [BUG] `protocol/options.go:66` — `panic` on `crypto/rand.Read` failure inside library code; comment justifies it but library functions should return errors. Fix: return `(types.ConnectionID, error)`; callers in `upgrade.go:214` already have an error path.

- **TD-064** [FOLLOWUP] `bsatn/decode.go:98,111` — `make([]byte, n)` where `n` is an attacker-controlled `uint32` from the wire. A malicious peer can send `n = 0xFFFFFFFF` and force a 4 GiB allocation before the `io.ReadFull` fails. Verify whether SPEC-005 / project-brief caps message size at the transport layer (websocket frame max). If not, add `if n > MaxStringLen { return ..., ErrStringTooLarge }`.

- **TD-065** [FOLLOWUP] `auth/jwt.go:67-76` — Keyfunc returns `config.SigningKey` for any HMAC method (HS256/HS384/HS512). Comment says "v1 supports HS256 only" but the type assertion accepts any `*jwt.SigningMethodHMAC`. Verify intent; if HS256 only, check `t.Method.Alg() == "HS256"` explicitly.

### C. Idiom & style smells

- **TD-066** [SMELL] `types/value.go:203-207` — `mustKind` panics on type mismatch. Acceptable for in-package programmer-error catches, but no `Try`/error-returning variant exists for callers consuming untrusted `Value`s constructed via reflection or generic code paths (e.g. SQL planner). Future callers will either swallow panics with `recover` or duplicate the kind check. Fix: add `(Value).TryAsXxx() (T, error)` accessors or `(T, bool)` ala map lookup; keep `AsXxx` as the panic-on-misuse convenience.

- **TD-067** [SMELL] `types/value.go:128-132` and `:196-201` — `NewBytes` defensively copies input, `AsBytes` also copies on every read. Two copies per round-trip is wasteful for read-mostly workloads. Fix: keep defensive copy in `NewBytes`; add `(Value).BytesView() []byte` for read-only access where caller promises not to mutate; document the contract.

- **TD-068** [SMELL] `auth/jwt.go:90,94,102,106,109` — Repeated `mc[k].(string/float64)` type assertions with discarded `ok`. Five sites; mild duplication. Fix: add `func stringClaim(m jwt.MapClaims, k string) string` and `floatClaim`. Note: `mc["exp"].(float64)` is safe in practice because `encoding/json` decodes all numbers as `float64` — leave a comment.

- **TD-069** [SMELL] `auth/mint.go:42,45` — Two `time.Now()` calls a microsecond apart. Fix: `now := time.Now(); claims["iat"] = now.Unix(); if config.Expiry > 0 { claims["exp"] = now.Add(config.Expiry).Unix() }`.

- **TD-070** [SMELL] `schema/registry.go:54` — `_ = userTableCount` discards a parameter the constructor demands. Either the user/system split is meant to be queryable on `SchemaRegistry` (in which case the field/method is missing and `Tables()` returns them mixed) or the parameter is dead and should be dropped. The interface comment "user tables first, then system" promises an ordering guarantee with no API to consume it. Fix: add `UserTables()`/`SystemTables()` accessors or remove the parameter and the comment.

- **TD-071** [SMELL] `schema/validate_schema.go:36-44` — Multiple `fmt.Errorf("...")` calls without `%w` wrapping (e.g. `"reducer name must not be empty"`, `"OnConnect handler must not be nil"`, `"duplicate OnConnect registration"`). Callers can't `errors.Is/As`. Fix: define `ErrEmptyReducerName`, `ErrNilReducerHandler`, `ErrDuplicateLifecycleHandler`, etc., and wrap with `%w`.

- **TD-072** [SMELL] `schema/validate_schema.go:25-26` — `tableNamePattern.MatchString` failure is reported with `ErrEmptyTableName` even though the table name isn't empty — it's structurally invalid. Wrong sentinel. Fix: introduce `ErrInvalidTableName` and wrap.

- **TD-073** [SMELL] `schema/typemap.go:24` — `t == byteSliceType || (t.Kind() == reflect.Slice && t.Elem() == byteElemType)` — the first clause is the redundant one (named-slice types like `type X []byte` satisfy the second clause). Drop one clause or document the dual check.

- **TD-074** [SMELL] `schema/valuekind_export.go:25` — `k >= 0` is always true for a `ValueKind` derived from `int`, but `int` is signed. Negative inputs return `""` silently; schema package then builds `""` type strings into export. Fix: return `("",false)` or panic on out-of-range.

- **TD-075** [SMELL] `schema/reflect.go:53` — Comment claims "Non-struct anonymous field — fall through to normal processing", but anonymous non-struct fields will then try `f.Type` → `GoTypeToValueKind`, and `f.Name` is the type name (e.g. `int64`), which `ToSnakeCase` then mangles. Either explicitly support promoted scalar embeds with a column-name override or reject with a clear error.

- **TD-076** [SMELL] `schema/errors.go:9` — Misaligned struct-tag-style declaration (`ErrAutoIncrementRequiresKey` is shifted) — file wasn't `gofmt`'d. Run `gofmt -w`.

- **TD-077** [SMELL] `executor/registry.go:7-9` — gofmt diff: extra blank padding in struct (`reducers  map`, `frozen    bool`) — leftover from a deleted field.

- **TD-078** [SMELL] `executor/executor.go:64-67` — variable named `cap` shadows the predeclared `cap` builtin. Rename to `capacity`.

- **TD-079** [SMELL] `executor/executor.go:204` — `default: log.Printf("executor: unknown command type %T", cmd)` silently drops unknown commands. A future command with a `ResponseCh` could deadlock its caller. Should panic in dev or return an error to whatever channel the command carries.

- **TD-080** [SMELL] `executor/executor.go:60-102` — `NewExecutor` takes 5 positional parameters including a magic `recoveredTxID uint64` and panics on multiple invariants. Convert to functional options or split: `NewExecutor(cfg, deps)` plus a separate `Recover(txID)` step.

- **TD-081** [SMELL] `executor/executor.go:386-389` — `isUserCommitError` hardcodes three error sentinels. Add a `store.IsConstraintError(err) bool` upstream so this list doesn't drift.

- **TD-082** [SMELL] `executor/executor.go:392-400` — No-op fakes (`noopDurability`, `noopSubs`) live in production code. Move to a `noop.go` file or `internal/executortest`.

- **TD-083** [SMELL] `executor/executor.go:69,73` — `dur := cfg.Durability; if dur == nil { dur = noopDurability{} }` repeated for `subs`. Silently substituting a no-op for a missing durability handle makes mis-wired prod builds undetectable. Require non-nil and panic at construction; let test helpers pass explicit no-ops.

- **TD-084** [SMELL] `executor/errors.go` and exported types throughout (`ScheduleID`, `CallSource`, `ReducerStatus`, `LifecycleKind`) — `String()` method missing on every enum, so `log.Printf("status=%d")` patterns proliferate (see `scheduler_worker.go:173`). Add `Stringer` impls or use `go:generate stringer`.

- **TD-085** [SMELL] `store/transaction.go:14-17` — `Transaction` has no `Context` field. All methods are non-cancellable; a long-running transaction over a deleted table or huge scan can't be aborted. Add `ctx context.Context` per the SDK conventions.

- **TD-086** [SMELL] `store/errors.go:8-19` — Sentinel errors are declared but `transaction.go:35,143,154,159,178,183` use `fmt.Errorf("%w: %d", ErrTableNotFound, tableID)` ad hoc. Should use a `TableNotFoundError` struct (consistent with `TypeMismatchError` style already in the file).

- **TD-087** [SMELL] `commitlog/durability.go:69-88` — Function panics for programmer errors AND for runtime conditions (channel send-on-closed-channel race after close). Panicking in a library on a runtime durability failure is not idiomatic Go; return an error or expose a `Wait()`/error channel.

- **TD-088** [SMELL] `store/changeset.go:9-20` — `Changeset.TxID` is `types.TxID` but `commit.go:22` always assigns `0` and `durability.go:32` carries `txID uint64` separately. The field is dead/misleading. Either populate it in `Commit` or remove it.

- **TD-089** [SMELL] `subscription/delta_view.go:120-147` — `DeltaIndexScan` panics when the column has no built index but returns nil silently when the table has no rows. Inconsistent contract: callers cannot distinguish "no rows" from "you forgot activeColumns". Fix: return `(rows, error)` or precondition-check at construction.

- **TD-090** [SMELL] `subscription/eval.go:222-228` — `evalQuerySafe` mixes named returns with explicit `return updates, nil` while a deferred `recover` writes to the named `err`. Behavior is correct (defer-set values win) but brittle and confusing. Fix: use bare `return` so defer-set values are returned cleanly.

- **TD-091** [SMELL] `subscription/manager.go:50-51` — `memo := make(map[QueryHash]*memoizedResult)` is built and only assigned to (`memo[hash] = ...`) but never read. Either wire it into the fanout payload now or delete.

- **TD-092** [SMELL] `subscription/eval.go:42` — `_ = txID` discards a parameter the signature requires; the `FanOutMessage` uses `txID` directly. Either log/use or drop from `evaluate`'s signature.

- **TD-093** [SMELL] `subscription/register.go:35` and `register.go:73-79` — `_ = qs` after the dedup branch (unused); IIFE wrapping `iterateAll` to convert `iter.Seq2` into a slice is dead weight (`iterateAll` already returns a slice).

- **TD-094** [SMELL] `subscription/eval.go:236-239` — `evalPanic.Error()` does not include the panic cause. Use `fmt.Sprintf("subscription: evaluation panic for query %s: %v", e.hash, e.cause)`.

- **TD-095** [SMELL] `protocol/disconnect.go:38,41` — `log.Printf` inside library code; no logger injection. Fix: take a `*slog.Logger` on `Server`/`Conn` and route through it.

- **TD-096** [SMELL] `protocol/client_messages.go:241` and `protocol/compression.go:95,100` — `fmt.Errorf("%w: %v", ...)` chains the sentinel with `%v` instead of `%w`, so the underlying decode error can't be unwrapped past the sentinel. Fix: `errors.Join(ErrMalformedMessage, err)` or use `%w` for both.

- **TD-097** [SMELL] `protocol/options.go:26` — `CloseHandshakeTimeout` is defined and never read. Either wire it through into the close paths in `disconnect.go` and `keepalive.go` or remove it.

- **TD-098** [FOLLOWUP] `schema/build.go:107` — `b.built = true` is set on the caller-visible builder before the `Engine` is returned; if `newSchemaRegistry` panics, `b` is permanently sealed. Verify whether `Build()` is intended to be retryable on partial failure; if not, document the one-shot semantics; if yes, set `b.built` only on the successful `return`.

### D. Duplication patterns

- **TD-099** [DUP] **Six parallel kind ladders over `ValueKind`.** Sites: `bsatn/encode.go:29-101` (encode switch), `bsatn/decode.go:35-119` (decode switch), `types/value.go:33-47` (`kindNames`), `:217-235` (`Equal`), `:244-268` (`Compare`), `:289-312` (`writePayload`), `:316-334` (`payloadLen`), `schema/typemap.go` (Go-kind switch), `schema/valuekind_export.go` (export-string table), `schema/validate_structure.go:122` (`isValidValueKind` range check). Adding a 14th kind requires editing ~9 sites; the compiler warns on none (default arms swallow). Fix: define a single `var kindInfo = [...]struct { name string; payloadLen int; ... }` table indexed by `ValueKind`, replace all switches; add a test asserting `len(kindNames) == lastKind+1`.

- **TD-100** [DUP] **Hand-rolled little-endian wire helpers in three packages.** Sites: `bsatn/{encode,decode}.go`, `commitlog/segment.go`, `protocol/{client_messages.go:249-294, server_messages.go:329-340}`. *Internal dup within `protocol`*: `writeUint32`/`readUint32` in `client_messages.go` and `writeUint64`/`readUint64` in `server_messages.go` are the same shape. Each package ships its own `(value, off, err)` triple-return helper and bounds check, repeated ~20 times across both message files. Fix: extract an `internal/wire` package with offset-owning `Reader`/`Writer` structs.

- **TD-101** [DUP] **Three nested-hashset structures in `subscription`.** Sites: `subscription/value_index.go:24-36`, `subscription/join_edge_index.go:34-57`, `subscription/table_index.go:18-25` — three parallel set-of-hash maps with copy-pasted Add/Remove/Lookup/empty-cleanup logic. Cleanup ladder in `value_index.go:81-100` is replicated almost verbatim in `join_edge_index.go:73-89`. Fix: generic `nestedHashSet[K1, K2 comparable]` would eliminate ~150 LOC.

- **TD-102** [DUP] **Three predicate type-switch walks in `subscription`.** Sites: `subscription/delta_single.go:32-58`, `subscription/placement.go:150-182`, `subscription/eval.go:107-127`. Fix: single `Visit(pred, Visitor)` (or `iter.Seq[Predicate]`) — one place to update when new predicate kinds land.

- **TD-103** [DUP] **Three Tier-2 join-traversal loops in `subscription`.** Sites: `subscription/placement.go`, `subscription/eval.go:146-217`, `subscription/delta_join.go`. Each does LHS row → IndexSeek RHS → bounds-check `RHSFilterCol` → lookup. Fix: extract `forEachJoinMatch(view, edge, lhsRow, fn)`.

- **TD-104** [DUP] **"Commit + Rollback-on-error + assign txID + postCommit" tail duplicated.** Sites: `executor/lifecycle.go:52-62` and `executor/executor.go:293-311`. Fix: extract `commitAndPostCommit(tx, ret, ch) (txID, error)`.

- **TD-105** [DUP] **"Build CallerCtx / ReducerCtx + deferred panic recover + classify outcome" duplicated.** Sites: `executor/lifecycle.go:231-265` and `executor/executor.go:126-157`. Fix: extract `runReducer(rr, tx, caller, args) ([]byte, ReducerStatus, error)` shared between lifecycle and CallReducer pipelines.

- **TD-106** [DUP] **"Find schedule row by ID" loop duplicated.** Sites: `executor/scheduler.go:36-52` (`advanceOrDeleteSchedule`) and `executor/scheduler.go:115-127` (`Cancel`). Both share the "match by `SysScheduledColScheduleID == target`" predicate. Fix: extract `findScheduleRowID(tx, tableID, id) (rowID, row, bool)`.

- **TD-107** [DUP] **Unique-violation check duplicated across transaction and commit paths.** Sites: `store/transaction.go:42-86` (Insert constraint checks: committed unique, tx-local unique, hash-set) and `store/commit.go` (`revalidateInsertAgainstCommitted`). Two copies will drift. Fix: extract `checkUniqueAgainst(table, idx, key, txState)` helper.

- **TD-108** [DUP] **`store/snapshot.go:33-79` — five methods all `t, ok := s.cs.Table(id); if !ok { return zero }`.** Also no `s.closed` check before delegating to `s.cs` — using a closed snapshot reads from state without holding the read lock. Fix: extract `withTable[T](id, zero, fn)` helper.

- **TD-109** [DUP] **`commitlog/changeset_codec.go:38-62` — Inserts and Deletes loops are byte-for-byte identical** (count + per-row length-prefixed bytes). Fix: extract `writeRowList(buf, rows)` and `readRowList(data, ts, max)`.

- **TD-110** [DUP] **`commitlog/segment.go:75-83` and `:87-104` — `ComputeRecordCRC` and `EncodeRecord` redundantly serialize the 14-byte header twice.** Fix: shared header-bytes helper.

- **TD-111** [DUP] **`commitlog/segment.go:108-114, 128-132, 136-140` — three identical `if err == io.ErrUnexpectedEOF { return nil, ErrTruncatedRecord }` blocks.** Fix: wrap once at the top using `errors.Is`.

- **TD-112** [DUP] **`protocol/{client,server}_messages.go` — four parallel encode/decode switch ladders** for client and server messages. Fix: define `type ClientMessage interface { Tag() uint8; encodeBody(*bytes.Buffer) error }` (and matching `ServerMessage`), attach methods, drop the switches.

- **TD-113** [DUP] **`types/connection_id.go:11` and `types/identity.go:10` — `IsZero` identical except for the array length.** Three-line dup. Acceptable as-is given only two sites; flag if a third array-id type is added.

### E. Cross-cutting themes

These are not single sites but pattern-level observations that should inform refactor priority:

- **`%w` discipline is weak across the repo.** Nine `fmt.Errorf("...: %v", err)` sites that should be `%w` (or `errors.Join`). Inconsistency makes `errors.Is/As`-based dispatch impossible at higher layers (e.g. protocol mapping JWT errors to HTTP statuses).

- **`any` typing on hot APIs** (`types/reducer.go:15-16` `DB`/`Scheduler`, `executor` command default case, several protocol message decoders). Confirms TD-022 and extends. Defeats compile-time safety on the most-used surface.

- **Sentinel-error-vs-typed-error inconsistency.** `store/errors.go` and `bsatn/errors.go` use `errors.New` sentinels heavily; `schema/errors.go` mixes sentinels with `Errorf` strings; `protocol/errors.go` is mostly sentinels but key sites like `client_messages.go:241` break the chain. Pick one convention per package and document it.

- **Library code panics in three packages** (`commitlog/durability.go`, `protocol/options.go`, `types/value.go` accessor `mustKind`). At least the durability and options panics should be returned errors.

- **Six executor goroutines + two protocol pumps + one scheduler + one durability worker = nine long-lived goroutines, none of which use `sync.WaitGroup` for shutdown bookkeeping.** Shutdown ordering is documented nowhere; race-detector runs would likely surface several of the items above.

- **Subscription pkg has the largest refactor leverage**: TD-101, TD-102, TD-103 together would shrink ~500 LOC of duplication and remove the most likely future-drift source in a 2.3k-LOC package.

- **No `gofmt` gate visible.** TD-076 (`schema/errors.go`) and TD-077 (`executor/registry.go`) both indicate `gofmt -w` was not run before commit.

### F. Remediation priority

Recommended order (highest impact first):

1. **TD-027 through TD-045** (concurrency + durability) — correctness-fatal under load or restart; passing tests do not catch these
2. **TD-031, TD-032, TD-033** (commit non-atomicity, dead `Rollback`, silent delete-skip) — data-loss class
3. **TD-046 through TD-063** (logic bugs) — visible to users as wrong results or wrong errors
4. **TD-099, TD-100, TD-101** (largest dup patterns) — removing these unlocks safer future edits
5. **TD-066 through TD-098** (idiom smells) — opportunistic cleanup
6. **TD-064, TD-065, TD-098** (FOLLOWUPs) — verify before fixing

Each item should land with at least one focused test or compile-only repro before fix, per the audit method already established in this file.
