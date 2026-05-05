# Application Readiness Generic Capabilities

Status: implementation planning spec
Scope: generic Shunter runtime capabilities needed by real production-shaped
applications.

This spec converts application pressure into Shunter work without making
Shunter app-specific. Domain tables, reducers, auth workflows, import scripts,
and REST compatibility layers belong in the application. Shunter should only
gain reusable runtime, schema, storage, query, migration, auth, and client
capabilities.

## 1. Principles

- Do not add domain names, domain reducers, or domain-specific read models to
  Shunter.
- Ship each type-system addition as a vertical slice across `types`, `schema`,
  `store`, `bsatn`, `query/sql`, contract export, contract diff, and codegen.
- Preserve existing wire tags and persisted encodings. Add new tags at the end
  of current enums/codecs unless a spec explicitly requires a migration.
- Prefer deterministic encodings and comparisons over convenient but ambiguous
  app-local behavior.
- Keep broad SQL compatibility out of scope. Add the smallest query features
  needed by declared reads and one-off reads.
- Keep application imports app-owned, but provide generic hooks and runtime
  APIs that make them safe and reviewable.

## 2. Remaining Implementation Order

Agents should implement remaining slices in this order unless a later task
explicitly says otherwise:

1. Composite index and reducer read ergonomics.
2. Fixed-point numeric convention or column metadata.
3. External JWT principal bridge.
4. App-owned migration and import hooks.
5. Ordered declared reads.
6. Client/codegen updates for the new surface.

Completed slices:

- UUID value kind.
- Canonical JSON value kind.
- Nullable column semantics.

The remaining slices build on the now-stable UUID, JSON, and nullable type
surface.

## 3. UUID Value Kind

Status: implemented.

### Requirement

Add a first-class UUID column kind. It must be generic and not tied to any
specific UUID version or application ID strategy.

### Runtime Contract

- Add `types.KindUUID`.
- Represent UUID values internally as 16 bytes, not as strings.
- Add constructors and accessors:
  - `types.NewUUID([16]byte) Value`
  - `types.ParseUUID(string) (Value, error)`
  - `Value.AsUUID() [16]byte`
  - `Value.UUIDString() string`
- Canonical text form is lowercase RFC 4122 hyphenated text.
- Comparisons and index ordering use lexicographic byte order.
- Hashing uses the 16 canonical bytes.
- `types.Value` copying must not expose mutable aliases.

### Schema And Reflection

- Re-export the kind from `schema`.
- Reflection should accept a Shunter-native UUID type once one exists.
- Reflection may also accept `[16]byte` if that does not create ambiguity with
  generic byte arrays.
- Do not add an external UUID dependency without a separate dependency review.

### Encoding And Query

- BSATN encodes UUID as exactly 16 bytes after the value-kind tag.
- SQL coercion accepts canonical UUID string literals for UUID columns.
- SQL coercion rejects malformed UUIDs with the existing unsupported/invalid
  literal error style.
- Contract export emits the type string `uuid`.
- TypeScript codegen emits a stable `UUID` alias, initially `string`.

### Tests

- Value construction, parse failures, text round trips.
- BSATN value and product round trips.
- Store insert/update/delete with primary and secondary UUID indexes.
- Query literal coercion and subscription filtering on UUID columns.
- Contract export, contract validation, diff, and TypeScript codegen.

### Non-Goals

- UUID generation helpers are optional. If added, they must be generic and use
  the standard library where possible.
- No application-specific ID aliases.

## 4. Canonical JSON Value Kind

Status: implemented.

### Requirement

Add a JSON column kind for application-owned structured payloads. This is not a
JSONB clone and does not include path indexes or JSON operators in the first
slice.

### Runtime Contract

- Add `types.KindJSON`.
- Store canonical JSON bytes.
- Constructors validate JSON before storing:
  - `types.NewJSON([]byte) (Value, error)`
  - `types.MustJSON([]byte) Value` only if the package already uses `Must*`
    conventions; otherwise skip it.
  - `Value.AsJSON() []byte`
- `AsJSON` returns a defensive copy.
- Canonicalization removes insignificant whitespace and sorts object keys.
- Arrays preserve order.
- Numbers must remain valid JSON numbers. Do not coerce all numbers through
  `float64` if that would lose precision.
- Duplicate object keys should be rejected if the canonicalizer can detect them
  cleanly. If not implemented in the first slice, document the exact behavior
  and add tests for it.

### Encoding And Query

- BSATN stores canonical JSON as length-prefixed bytes.
- Equality and indexing use canonical bytes.
- SQL coercion accepts string literals containing JSON.
- Do not add JSON path predicates, JSON containment, or partial JSON indexes in
  this slice.
- Contract export emits the type string `json`.
- TypeScript codegen maps JSON columns to `unknown` unless a future typed
  schema annotation exists.

### Tests

- Valid and invalid JSON constructors.
- Canonical equality for equivalent object key ordering and whitespace.
- Copy/alias tests for stored bytes.
- BSATN round trips.
- Store and index behavior.
- SQL literal coercion for valid and invalid JSON.
- Contract/codegen coverage.

## 5. Nullable Column Semantics

Status: implemented.

### Requirement

Make `ColumnDefinition.Nullable` real. Production schemas need optional text,
timestamps, IDs, JSON payloads, and numeric values without encoding every field
as an application sentinel.

### Runtime Contract

- Add a nullable value representation to `types.Value`.
- Null must carry the declared column kind so the row shape remains known.
  Recommended constructor: `types.NewNull(kind ValueKind) Value`.
- `Value.IsNull() bool` reports null state.
- Accessors on null values must panic or return a typed error consistently with
  existing accessor behavior.
- Store validation:
  - reject null for non-nullable columns
  - accept null for nullable columns only when the carried kind matches the
    column kind
- Index ordering treats null as a deterministic comparable sentinel before all
  non-null values.
- Unique indexes treat null as a value. That means only one identical nullable
  unique key is allowed. Applications that need SQL's multiple-null unique
  behavior should model that explicitly.

### Schema And Reflection

- Stop rejecting `ColumnDefinition.Nullable` during `Build`.
- Add reflection support for pointer fields where the pointed-to type maps to a
  supported Shunter kind.
- Preserve explicit tags for cases where pointer semantics would be ambiguous.
- Contract export includes `nullable: true`.
- Contract diff treats nullable changes as compatibility-relevant.

### Encoding And Query

- Product encoding for nullable columns includes a presence marker before the
  value payload.
- Non-nullable product encodings remain unchanged.
- SQL adds `IS NULL` and `IS NOT NULL`.
- `= NULL` and `!= NULL` should be rejected with a clear error; use `IS`.
- TypeScript codegen maps nullable columns to `T | null`.

### Tests

- Build accepts nullable columns and still rejects invalid nullable shapes.
- Store validation accepts/rejects null correctly.
- Insert/update/delete with nullable indexes.
- BSATN backwards compatibility for non-nullable rows.
- Snapshot/recovery with nullable rows.
- SQL `IS NULL` / `IS NOT NULL` filtering.
- Contract export, validation, diff, and TypeScript codegen.

## 6. Composite Index And Reducer Read Ergonomics

### Requirement

Composite secondary indexes exist structurally. Production modules need them to
be easy and safe to use from reducers and local reads.

### Runtime Contract

- Audit current store behavior for multi-column unique and non-unique indexes.
- Add missing tests for insert, update, delete, rollback, snapshot, and recovery
  with composite index keys.
- Add reducer-facing read helpers so reducers do not scan entire tables for
  common composite-key lookups.

Recommended reducer DB additions:

```go
type ReducerDB interface {
    SeekIndex(tableID uint32, indexID uint32, key []Value) iter.Seq2[RowID, ProductValue]
    SeekIndexRange(tableID uint32, indexID uint32, low, high IndexBound) iter.Seq2[RowID, ProductValue]
}
```

Exact type names should follow existing store naming. The important contract is
that reducers can seek a declared index by all indexed columns without reaching
through `Underlying()`.

### Schema And Contract

- Contract export already lists index columns by name; preserve declaration
  order.
- Contract validation should reject duplicate or unknown composite index
  columns with context-rich errors.
- Codegen should expose index metadata for clients once client-side cache/query
  helpers need it.

### Tests

- Multi-column unique conflict in committed state.
- Multi-column unique conflict within one transaction.
- Update moving a row between composite keys.
- Delete removes composite index entries.
- Snapshot/recovery preserves composite index behavior.
- Reducer helper returns only matching rows and does not expose aliases.

## 7. Fixed-Point Numeric Strategy

### Requirement

Applications need deterministic sortable numeric values for money, percentages,
scores, and timing. Shunter should not force apps into `float64` for persisted
business values.

### Initial Contract

Use scaled integer columns as the first production strategy:

- applications store fixed-point values in `KindInt64`, `KindUint64`,
  `KindInt128`, or `KindUint128`
- application code owns arithmetic and scale choice
- Shunter guarantees deterministic integer storage, indexing, query comparison,
  export, and codegen

### Optional Metadata Slice

Add column metadata for fixed-point scale only after the schema metadata surface
is designed:

```go
type FixedPointMetadata struct {
    Scale uint8
    Signed bool
}
```

If added, this metadata must be passive first. It informs contract export and
codegen but does not change the stored `ValueKind`.

### Non-Goals

- Do not add arbitrary precision decimal arithmetic in the first slice.
- Do not add floating-point rounding policies to Shunter.
- Do not make score, money, or time domain helpers part of Shunter.

### Tests

- Existing integer query/index tests should cover the storage behavior.
- If metadata is added, test contract export, diff, validation, and codegen.

## 8. External JWT Principal Bridge

### Requirement

Reducers need a generic way to see the external authenticated principal behind
the Shunter identity. This must support Supabase-like and other JWT providers
without naming any provider in Shunter APIs.

### Runtime Contract

- Keep `types.Identity` as Shunter's runtime identity.
- Add a generic principal shape to `types.CallerContext`, for example:

```go
type AuthPrincipal struct {
    Issuer string
    Subject string
    Audience []string
    Permissions []string
}
```

- Populate principal data from validated JWT claims on protocol calls.
- Add local call options for tests and app-owned HTTP adapters to supply
  principal data without forging raw tokens.
- Preserve current `WithIdentity` behavior.
- Do not require applications to use the external subject as their primary key.
  The app decides whether to store subject, derived identity, both, or neither.

### Auth Config

- Keep HS256 support.
- Add room for issuer and audience policy validation to be explicit and tested.
- Do not add provider-specific defaults.
- If asymmetric JWT algorithms are added, add them as generic algorithms with
  key parsing tests.

### Tests

- Protocol JWT validation populates identity and principal.
- Local reducer calls can supply principal data.
- Permission checks keep using normalized permission tags.
- Contract/codegen only changes if principal requirements become declared
  metadata later.

## 9. App-Owned Migration And Import Hooks

### Requirement

Applications need safe ways to initialize and evolve durable Shunter state.
Shunter should provide generic hooks and guardrails; application code owns the
actual migration logic.

### Runtime Contract

- Descriptive migration metadata remains review-only.
- Add executable migration hooks as an explicit separate surface.
- Hooks run under runtime ownership before the runtime reports ready.
- Hooks run with exclusive write access to durable state.
- Hooks receive from/to schema version, module metadata, and a transactional DB
  surface.
- Hooks must not use protocol connections or background goroutines.
- Startup must fail if a required migration path is missing.
- Startup must not silently rewrite state without an explicitly registered hook.

Suggested shape:

```go
type MigrationHook struct {
    FromSchemaVersion uint32
    ToSchemaVersion   uint32
    Name              string
    Run               func(*MigrationContext) error
}

type MigrationContext struct {
    Context context.Context
    FromSchemaVersion uint32
    ToSchemaVersion   uint32
    DB types.ReducerDB
}
```

Exact names can change to match existing module APIs.

### Import Helpers

- Provide an empty-state bootstrap hook path for app-owned imports.
- Large imports should be chunkable and observable.
- Import helpers should be usable from app-owned CLIs, not only the generic
  `shunter` CLI.

### Operational Rules

- Contract dry-run planning emits backup/restore guidance for blocking or
  data-rewrite changes before destructive migrations touch a durable `DataDir`.
- App-owned CLIs can call `shunter.CheckDataDirCompatibility` to validate a
  stopped or missing `DataDir` against the module schema before startup.
- Rollback semantics are out of scope until locking and crash-recovery behavior
  is fully specified.

### Tests

- Empty-state hook runs once before ready.
- Missing migration hook fails startup.
- Hook error leaves runtime not ready.
- Successful hook state is durable across restart.
- Hook cannot run after normal runtime start.

## 10. Ordered Declared Reads

### Requirement

Applications need stable ordered reads for leaderboards, feeds, histories, and
admin tables. Add ordering without expanding into full SQL compatibility.

### SQL Surface

Add bounded `ORDER BY` support:

```text
ORDER BY col [ASC|DESC] [, col [ASC|DESC] ...]
```

- Qualified columns are allowed when the query surface already permits that
  relation shape.
- Ordering runs after filtering and projection planning.
- `LIMIT` applies after ordering.
- Null ordering is deterministic: null sorts before non-null in ascending order
  and after non-null in descending order unless a later spec adds `NULLS FIRST`
  / `NULLS LAST`.
- Views/subscriptions may reject ordering initially if incremental ordered
  deltas are not implemented.

### Planner And Execution

- Prefer an index when a single-table ordered query can be satisfied by an
  index prefix and compatible filters.
- Fall back to in-memory sort for bounded result sizes.
- Keep memory behavior observable when large ordered scans happen.

### Tests

- Parser accepts/rejects supported/unsupported ordering forms.
- One-off query ordering over scalar, string, bytes, UUID, nullable, and JSON
  columns where supported.
- `ORDER BY ... LIMIT` returns stable results.
- Permission and visibility filters still apply before result return.
- Declared query contract/codegen preserves SQL text and validates it.

## 11. Materialized Read Model Helpers

### Requirement

Some production read models should be reducer-maintained tables rather than
runtime-computed SQL aggregates. Shunter should provide generic ergonomics, not
domain-specific materialized view behavior.

### Contract

- Keep materialized read models as normal tables.
- Add optional helper docs or package-level examples for reducer-maintained
  read tables.
- Add no automatic trigger system in the first slice.
- Add no background recomputation engine in the first slice.

### Useful Helpers

- Consistent naming guidance for private source tables and public read tables.
- Reducer DB index-seek helpers from section 6.
- Declared query/view metadata for exported materialized read tables.
- Tests showing reducer-maintained read rows produce normal subscription
  deltas.

## 12. Client And Codegen Updates

### Requirement

Every runtime type and declared-read feature exposed through `ModuleContract`
must be consumable by generated clients.

### TypeScript Contract

- `uuid` maps to `UUID`, initially a string alias.
- `json` maps to `unknown`.
- nullable columns map to `T | null`.
- fixed-point metadata, if added, maps to a branded integer alias or documented
  integer scale helper. Do not emit JavaScript floating-point conversions by
  default.
- ordered declared query SQL remains available through generated query
  metadata.

### React SDK Track

Keep React SDK work separate. A future React SDK may use the generated contract
surface, but this spec does not require React hooks or cache management.

### Tests

- Codegen snapshot tests for every new exported type.
- Contract validation rejects unknown type strings.
- Generated clients keep old contracts compatible when new features are absent.

## 13. Agent Work Rules

For any slice in this spec:

- Read the live package code and the narrow subsystem spec before editing.
- Update the relevant numbered spec only when the implementation changes a
  baseline contract.
- Add targeted package tests first, then broaden to cross-runtime tests when
  serialization, recovery, protocol, or codegen changes.
- Run `rtk go fmt` on touched Go files.
- Run targeted `rtk go test ./<package>` for touched packages.
- Run broader `rtk go test ./...` before claiming a vertical slice complete.
- Run `rtk go vet` for touched packages when exported APIs change.

## 14. Definition Of Done

A feature slice is done when:

- app-facing behavior is documented in code comments or a narrow spec section
  where appropriate
- old contracts still build and old tests still pass
- new schema/contract/codegen output is deterministic
- durable encoding and recovery behavior is tested
- visibility filters and permissions are tested for any new query path
- no application-domain concepts were added to Shunter
