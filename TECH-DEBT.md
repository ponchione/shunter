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

Audit notes:
- `SPEC-006 E2` (`schema/tag.go`, `schema/tag_test.go`) appears operationally aligned with the tag-parser stories. No new debt logged from that slice at this time.
- `SPEC-006 E1` is mostly aligned operationally (`schema/types.go`, `schema/typemap.go`, `schema/naming.go`, `schema/valuekind_export.go`), but one concrete contract gap was found and logged below: no live `ErrSequenceOverflow` sentinel is defined anywhere even though the spec/decomposition assigns that contract to this foundation slice.
- The narrowed `SPEC-003` Phase-1 contract slice (`E1.1 + E1.2 + minimal E1.4`) appears operationally present enough for early dependency gating: foundation enums/IDs exist, reducer request/response types exist, and a minimal scheduler interface shell exists. The remaining meaningful executor gap is still the broader Epic 1 surface already tracked as `TD-002`, rather than a new blocker inside the intentionally narrowed slice.
- `SPEC-006 E3.1` builder core appears operationally aligned: `NewBuilder`, `TableDef`, `SchemaVersion`, `EngineOptions`, and chaining behavior are implemented and covered by tests. I have not logged a separate builder-core debt item from that slice.
- `SPEC-006 E4` reflection-path registration is mostly present (`schema/reflect.go`, `schema/reflect_build.go`, `schema/register_table.go`), but one concrete contract gap was found and logged below: anonymous embedded fields are processed before `shunter:"-"` exclusion, so excluded embedded structs are still flattened and excluded embedded pointer-to-struct fields still error.
- `SPEC-006 E3.2` reducer registration is functionally present (`schema/builder.go`, `schema/validate_schema.go`, `schema/registry.go`), but one API-surface gap was found and logged below: the schema package does not expose `ReducerHandler` / `ReducerContext` aliases even though SPEC-006 presents reducer registration as part of the schema-facing API surface.
- `SPEC-006 E5` validation/build work is largely present, but two concrete contract gaps are now confirmed: Story 5.6 startup schema compatibility checking is missing entirely, and Story 5.4's read-only `SchemaRegistry` contract is violated because `Table(...)` / `TableByName(...)` return mutable pointers into internal state.
- Verification runs completed during audit:
  - `rtk go test ./schema`
  - `rtk go test ./schema ./executor`
  - `rtk go test ./schema ./store ./executor`
  - `rtk go test ./executor`
  - earlier broad pass: `rtk go test ./types ./bsatn ./schema ./store ./subscription ./executor ./commitlog`

## Open items

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
