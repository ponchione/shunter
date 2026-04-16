# SPEC-006 — Schema Definition

**Status:** Draft  
**Depends on:** SPEC-001 (`ValueKind`, `TableSchema`, column type system), SPEC-002 (`SnapshotSchema` and `schema_version` comparison at recovery — see §6.1), SPEC-003 (`ReducerHandler`, `ReducerContext` canonical declarations in `types/reducer.go`; schema re-exports for builder ergonomics), SPEC-005 (`ConnectionID` consumed by the `sys_clients` system table definition)  
**Depended on by:** SPEC-001 (store consumes `SchemaRegistry`), SPEC-002 (snapshot stores schema, compares `SchemaRegistry.Version()`), SPEC-003 (executor registers reducers / looks up handlers), SPEC-004 (`SchemaLookup`, `IndexResolver`), SPEC-005 (`SchemaLookup` for predicate validation + subscription registration)

---

## 1. Purpose and Scope

The schema definition system is the developer-facing API surface of Shunter. It provides:

- A way to declare tables as Go structs with struct tags (reflection path)
- An explicit builder API for programmatic registration (builder path)
- Reducer registration
- Engine assembly: combining schema, store, executor, commit log, and protocol into a running engine
- A defined interface for client code generation tools

This spec covers:
- Struct tag grammar and semantics
- Go type → Shunter column type mapping
- Index declaration (primary key, unique, secondary)
- Both registration paths (reflection and builder)
- Schema versioning and compatibility checking
- The `SchemaRegistry` interface consumed by other subsystems
- Client codegen tool interface

This spec does not cover:
- Table storage internals (SPEC-001)
- Commit log snapshot schema storage (SPEC-002)
- Executor internals (SPEC-003)

### 1.1 Go-package homes

Cross-spec engine identifier types (`RowID`, `Identity`, `ConnectionID`, `TxID`, `ColID`, `SubscriptionID`, `ScheduleID`) and the reducer runtime types (`ReducerHandler`, `ReducerContext`, `CallerContext`, `ReducerDB`, `ReducerScheduler`) are declared in the `types/` Go package — see SPEC-001 §1.1 and SPEC-003 §3.1. The `schema/` package re-exports `ReducerHandler`, `ReducerContext`, and `ValueKind` for ergonomic builder / registration call sites; those re-exports are not new declarations. If a future typed reducer layer (SPEC-006 §4.3) lands, it must reserve the sentinel name `ErrReducerArgsDecode` in §13 and live in `schema/` — but it, too, operates on the `types/`-declared handler signature.

---

## 2. Go Type Mapping

The following Go types may be used as struct field types in registered table structs. No other types are supported in v1.

| Go type | Shunter column type | Notes |
|---|---|---|
| `bool` | Bool | |
| `int8` | Int8 | |
| `uint8` | Uint8 | |
| `int16` | Int16 | |
| `uint16` | Uint16 | |
| `int32` | Int32 | |
| `uint32` | Uint32 | |
| `int64` | Int64 | |
| `uint64` | Uint64 | |
| `float32` | Float32 | NaN rejected on insert |
| `float64` | Float64 | NaN rejected on insert |
| `string` | String | UTF-8 |
| `[]byte` | Bytes | |

Named Go types whose underlying type is one of the scalar types above are also supported. Example: `type UnixNanos int64` is accepted and maps to `Int64`.

**Excluded in v1:** pointers, interfaces, maps, slices other than `[]byte`, non-embedded nested structs, arrays, time.Time (use `int64` Unix nanos), nullable/optional fields, `int` / `uint` (platform-width; use explicit widths).

**Recommended practice:** Define a named type or alias if semantic clarity is useful. The engine stores only the underlying scalar representation.

**`UnixNanos` helper (recommended):** All timestamps in Shunter are stored as `int64` Unix nanoseconds. Reducers that handle time SHOULD declare `type UnixNanos int64` in their schema package. This keeps the engine representation deterministic and timezone-neutral. The client codegen tool (§12) SHOULD emit a `UnixNanos` type with the following conversion helpers in generated client code:

```go
func (n UnixNanos) Time() time.Time          { return time.Unix(0, int64(n)).UTC() }
func FromTime(t time.Time) UnixNanos         { return UnixNanos(t.UnixNano()) }
```

**Auto-increment overflow:** `autoincrement` is allowed on any integer column type, but inserts fail with `ErrSequenceOverflow` once the next generated value would exceed the column's representable range.

---

## 3. Struct Tag Grammar

Table structs may annotate fields with a `shunter` struct tag. Fields without a tag are included as plain (non-indexed) columns.

```
Tag := `shunter:"<directive>[,<directive>...]"`

Directive :=
    | "primarykey"
    | "autoincrement"
    | "unique"
    | "index"
    | "index:<name>"
    | "name:<column-name>"
    | "-"
```

### 3.1 Directives

**`primarykey`**
Declares this column as the table's primary key. At most one `primarykey` column per table in v1. Implies `unique`. Example: `shunter:"primarykey"`.

**`autoincrement`**
Automatically assigns the next sequence value on insert when the field is the zero value for its type. Only valid on integer columns (`uint8`–`uint64`, `int8`–`int64`). Requires `primarykey` or `unique` on the same column (see §9 validation rules). If a non-zero value is provided, it is used as-is. Example: `shunter:"primarykey,autoincrement"`.

**`unique`**
Declares a unique secondary index on this column. Example: `shunter:"unique"`.

**`index`**
Declares a non-unique single-column secondary index on this column. Example: `shunter:"index"`.

**`index:<name>`**
Declares this column as part of a named secondary index. Multiple fields with the same `<name>` form one composite index. Index order matches field declaration order in the struct. Example: two fields tagged `shunter:"index:guild_score"` create a composite index on those two columns. May be combined with `unique`: `shunter:"unique,index:guild_score"`.

**`name:<column-name>`**
Overrides the column name used in the store, protocol, and codegen. Default is the field name converted to snake_case. Example: `shunter:"name:player_id,primarykey"`.

**`-`** (dash)
Excludes this field from the table schema entirely. The field still exists in the Go struct but is not stored or indexed. Example: `shunter:"-"`.

### 3.2 Tag Parsing Rules

- Tags are comma-separated, no spaces
- Order of directives within a tag does not matter
- `-` must appear alone; combining it with any other directive is a registration error
- Repeating the same directive twice is a registration error
- Combining contradictory directives (`primarykey` on two fields) is a registration error detected at engine startup
- Unknown directive names are registration errors (not silently ignored)
- Multiple tags with the same `index:<name>` across different fields form one multi-column index
- For a named composite index, either all participating fields specify `unique` or none do. Mixed usage is a registration error.
- `primarykey` may not be combined with `index` or `index:<name>`; v1 supports only single-column primary keys
- A plain `index` directive creates a single-column index named `<column_name>_idx`
- A plain `unique` directive on a non-primary-key column creates a single-column unique index named `<column_name>_uniq`
- A `primarykey` column creates the table's primary index named `pk`

### 3.3 Examples

```go
type Player struct {
    ID       uint64 `shunter:"primarykey,autoincrement"`
    Name     string `shunter:"index"`
    GuildID  uint64 `shunter:"index:guild_score"`
    Score    int64  `shunter:"index:guild_score"`
    CachedAt int64  `shunter:"-"` // excluded
}

type Session struct {
    Token     string `shunter:"primarykey"`
    PlayerID  uint64 `shunter:"index"`
    ExpiresAt int64
    UserAgent string `shunter:"name:ua"`
}
```

`Player` produces:
- Columns: `id`, `name`, `guild_id`, `score` — `cached_at` excluded
- Primary key: `id` (unique, autoincrement)
- Secondary index `name_idx` on `name`
- Composite index `guild_score` on (`guild_id`, `score`) — non-unique; column order = struct field order

---

## 4. Registration API

### 4.1 Reflection Path

The reflection path derives the `TableSchema` from a Go struct type using `reflect`. It is the primary API.

```go
// RegisterTable registers a table derived from the Go struct type T.
// T must be a struct. All exported fields with compatible types are included.
// Returns an error if the struct contains unsupported field types,
// contradictory tag directives, or a duplicate table name.
func RegisterTable[T any](b *Builder, opts ...TableOption) error
```

The table name defaults to the Go type name converted to snake_case (`PlayerSession` → `player_session`). Override with `WithTableName("players")`.

Column names default to snake_case conversion of the field name. Override per-field with `shunter:"name:<n>"`.

### 4.2 Builder Path

The builder path allows explicit table definition without struct tags. Useful for generated code, dynamic schemas, or when tag annotation is not desired.

Normative builder types:

```go
type TableOption struct {
    TableName string // optional override for the stored table name
}

func WithTableName(name string) TableOption

type TableDefinition struct {
    Name    string
    Columns []ColumnDefinition
    Indexes []IndexDefinition
}

type ColumnDefinition struct {
    Name          string
    Type          ValueKind
    PrimaryKey    bool
    AutoIncrement bool
}

type IndexDefinition struct {
    Name    string
    Columns []string
    Unique  bool
}
```

`ColumnDefinition.PrimaryKey` declares the table's single-column primary key. Composite primary keys are out of scope for v1. `IndexDefinition` is only for secondary indexes.

```go
b.TableDef(shunter.TableDefinition{
    Name: "player",
    Columns: []shunter.ColumnDefinition{
        {Name: "id",       Type: shunter.Uint64, PrimaryKey: true, AutoIncrement: true},
        {Name: "name",     Type: shunter.String},
        {Name: "guild_id", Type: shunter.Uint64},
        {Name: "score",    Type: shunter.Int64},
    },
    Indexes: []shunter.IndexDefinition{
        {Name: "name_idx",        Columns: []string{"name"},               Unique: false},
        {Name: "guild_score",     Columns: []string{"guild_id", "score"}, Unique: false},
    },
})
```

The builder path and reflection path may be mixed. Each table must be registered once.

### 4.3 Reducer Registration

```go
// RegisterReducer registers a named reducer.
// name must be unique. handler must not be nil.
// Lifecycle reducers (OnConnect, OnDisconnect) are registered separately.
b.Reducer("CreatePlayer", createPlayerReducer)

// RegisterOnConnect registers the OnConnect lifecycle reducer.
// At most one may be registered.
b.OnConnect(func(ctx *shunter.ReducerContext) error { ... })

// RegisterOnDisconnect registers the OnDisconnect lifecycle reducer.
b.OnDisconnect(func(ctx *shunter.ReducerContext) error { ... })
```

Reducer handlers use the `ReducerHandler` type from SPEC-003:
```go
type ReducerHandler func(ctx *ReducerContext, argBSATN []byte) ([]byte, error)
```

Typed reducer helpers are explicitly out of the v1 engine contract. Any future typed wrapper must be layered on top of explicit BSATN codecs rather than assumed implicit reflection.

v1 does not ship typed reducer adapters. The name `ErrReducerArgsDecode` is reserved for the future adapter layer's argument-decode sentinel, but no such sentinel is declared or produced by v1 code. SPEC-003 classifies any non-nil `ReducerHandler` error as `StatusFailedUser` via the generic handler-error path (§11) regardless of sentinel identity; once a typed adapter lands, it will wrap decode failures with `ErrReducerArgsDecode` and SPEC-003's classification stays unchanged.

---

## 5. Engine Builder

All registration flows through a `Builder`, which assembles the complete engine configuration and immutable schema registry.

```go
type Builder struct { /* unexported */ }

// NewBuilder creates a fresh builder.
func NewBuilder() *Builder

// TableDef registers a table via the builder path.
func (b *Builder) TableDef(def TableDefinition, opts ...TableOption) *Builder

// Reducer registers a named reducer.
func (b *Builder) Reducer(name string, h ReducerHandler) *Builder

// OnConnect registers the OnConnect lifecycle reducer.
func (b *Builder) OnConnect(h func(*ReducerContext) error) *Builder

// OnDisconnect registers the OnDisconnect lifecycle reducer.
func (b *Builder) OnDisconnect(h func(*ReducerContext) error) *Builder

// SchemaVersion sets an explicit schema version integer.
// Required; see §6.
func (b *Builder) SchemaVersion(v uint32) *Builder

type EngineOptions struct {
    DataDir                 string // required for commit log and snapshots
    ExecutorQueueCapacity   int    // 0 = default from SPEC-003
    DurabilityQueueCapacity int    // 0 = default from SPEC-002
    EnableProtocol          bool   // false = build core engine only
}

// Build validates all registrations and constructs the Engine.
// Returns an error if any registration is invalid, if SchemaVersion
// was not set, or if a required subsystem is missing.
func (b *Builder) Build(opts EngineOptions) (*Engine, error)
```

`Build` performs all validation synchronously before returning. If `Build` returns nil error, the engine is structurally valid and has an immutable `SchemaRegistry`, but it has not yet opened files, recovered state, started goroutines, or accepted network traffic.

`Engine.Start(ctx)` performs runtime initialization: open/recover the commit log, construct the committed store, start the executor and durability worker, restore scheduled reducers, and begin accepting protocol connections if `EnableProtocol` is true.

### 5.1 Freeze Semantics

`Build()` is the **freeze** point of the registration phase. Before `Build()`, every `Builder` mutator (`TableDef`, `Reducer`, `OnConnect`, `OnDisconnect`, `SchemaVersion`) is callable. After `Build()` returns successfully:

- The returned `SchemaRegistry` is immutable for the lifetime of the process.
- Subsequent calls to any `Builder` mutator on the same `Builder` instance return `ErrAlreadyBuilt` and do not mutate state. (`ErrAlreadyBuilt` is added to the §13 error catalog as part of Session 6 cleanup; see SPEC-006 audit §2.5.)
- A second call to `Build()` on the same `Builder` returns `ErrAlreadyBuilt` rather than re-running validation or appending duplicate system tables.

Implementations enforce freeze with a single boolean stored on `Builder`; thread-safety of mutator calls during the registration phase is the application's responsibility (registration is expected to run from a single goroutine at startup).

### 5.2 Engine Boot Ordering

The full engine bring-up sequence — the SPEC-003 audit §5.5 / SPEC-006 audit §1.4 ordering bleed-item — is:

1. **Schema registration.** Application calls `NewBuilder()` and then `TableDef` / `SchemaVersion` to register all user tables and the schema version.
2. **Reducer registration.** Application calls `Reducer`, `OnConnect`, and `OnDisconnect` for every handler.
3. **Freeze.** Application calls `Build(opts)`. System tables are appended, IDs are assigned, validation runs, and `*Engine` is returned with an immutable `SchemaRegistry`. After this point the registry is the canonical schema for SPEC-001/002/003/004/005.
4. **Subsystem construction.** `Engine.Start(ctx)` constructs `*store.CommittedState` (SPEC-001), the commit-log durability worker (SPEC-002), the executor (SPEC-003 — `NewExecutor` reads the frozen registry), the subscription manager (SPEC-004 — receives the registry as both `SchemaLookup` and `IndexResolver`), and the protocol layer (SPEC-005, when `EnableProtocol = true`).
5. **Recovery.** Commit log is opened and recovery runs against the new `CommittedState` (SPEC-002 §6).
6. **Scheduler replay.** The executor reads `sys_scheduled` rows from `CommittedState` and rearms timers (SPEC-003 §10.3).
7. **Dangling-client sweep.** The executor reads `sys_clients` rows and synthesizes `OnDisconnect` calls for any client present in the table without a live connection (SPEC-003 §5.3 / audit §2.2).
8. **Run.** Executor and durability worker enter their main loops; protocol layer begins accepting WebSocket upgrades.

Steps 1–3 are the SPEC-006 territory. Steps 4–8 are owned by SPEC-002/003/005 and cross-referenced from this section so the freeze contract is unambiguous: every consumer in steps 4+ may treat `SchemaRegistry` as fully populated and immutable.

---

## 6. Schema Versioning

### 6.1 Explicit Version Integer

Every engine build must declare a schema version:

```go
b.SchemaVersion(3)
```

The version is a `uint32` chosen by the application developer. It is stored in every snapshot (SPEC-002 §5.3) and compared at startup against the version stored in the latest snapshot.

**`SchemaRegistry.Version()` semantics.** `Version()` returns exactly the integer passed to `SchemaVersion()` at registration time. It is application-supplied, opaque to the engine, and never derived, hashed, or mutated by any subsystem after `Build()`. Two `SchemaRegistry` instances built from identical inputs return byte-equal `Version()` values; reload from snapshot does not alter the value.

**Snapshot storage authority.** SPEC-002 §5.2 (snapshot header) and §5.3 (schema body) currently both store the version integer; in case of disagreement during recovery the snapshot header is authoritative. The dual-storage collapse and on-disk byte-layout consequences are tracked in SPEC-002 audit §2.7 / §4.1 (Session 8 cleanup).

**Startup compatibility rule:** Recovery succeeds only if both of the following are true:
1. the registered schema version (i.e. `SchemaRegistry.Version()`) equals the snapshot schema version stored in the snapshot header
2. the embedded snapshot schema matches the registered schema exactly (table IDs, table names, column names/types/order, and index definitions)

If either check fails, startup returns `ErrSchemaMismatch` with structural diff details.

### 6.2 Monotonicity

Schema versions must only increase. There is no mechanism to detect non-monotonic changes in v1 — the developer is responsible for incrementing the version when the schema changes.

**Recommendation:** Treat the schema version like a database migration number. Increment it whenever a column or table is added, removed, or renamed.

### 6.3 v1 Schema Change Policy

v1 does not support online schema changes. If the registered schema differs from the snapshot schema, the only recovery is:

1. Wipe the data directory and start fresh (losing all data), or
2. Manually migrate the snapshot to the new schema (out of scope for v1 tooling)

Document this constraint prominently. Schema evolution support (add column with default, rename column) is a v2 design problem.

---

## 7. SchemaRegistry Interface

The schema-consumer surface is layered. SPEC-006 owns three interfaces; each downstream spec consumes the smallest one that fits.

- `SchemaLookup` — narrow read-only schema queries used by SPEC-004 (subscription validation) and SPEC-005 (protocol dispatch). Methods cover table existence, table-by-ID and table-by-name lookup, column metadata, and single-column index presence.
- `IndexResolver` — single-method index-ID resolution used by SPEC-004 Tier-2 candidate collection.
- `SchemaRegistry` — full surface used by SPEC-001/002/003 plus `Reducers()` / lifecycle handlers / `Version()`. Embeds `SchemaLookup` and `IndexResolver`.

All three are produced by `Build()` and are safe for concurrent use; they are immutable after `Build()` returns (see §5 freeze rules).

```go
// SchemaLookup is the narrow read-only schema surface consumed by
// SPEC-004 (subscription/validate) and SPEC-005 (protocol/handle_subscribe,
// protocol/upgrade). Concrete implementations may live in those packages
// for testing, but the canonical declaration is here. SchemaRegistry
// satisfies SchemaLookup.
type SchemaLookup interface {
    // Table returns the full schema for the given table ID.
    Table(id TableID) (*TableSchema, bool)

    // TableByName returns the table ID and full schema for the given name.
    // The 3-tuple shape exists so that wire handlers can resolve a name
    // to its TableID without a second lookup.
    TableByName(name string) (TableID, *TableSchema, bool)

    // TableExists reports whether the table ID is registered. Cheaper
    // than Table() when the schema body is not needed.
    TableExists(table TableID) bool

    // TableName returns the declared table name, or empty string if the
    // table ID is unknown. Used for wire/debug output.
    TableName(table TableID) string

    // ColumnExists reports whether the column index is valid for the table.
    ColumnExists(table TableID, col ColID) bool

    // ColumnType returns the ValueKind of the column. Behavior is undefined
    // when ColumnExists returns false; callers must check first.
    ColumnType(table TableID, col ColID) ValueKind

    // HasIndex reports whether a single-column index on (table, col) exists.
    // Used by SPEC-004 §7.1.1 join-side index validation.
    HasIndex(table TableID, col ColID) bool
}

// IndexResolver maps (table, column) → index ID when a single-column index
// on that column exists. Used by SPEC-004 Tier-2 candidate collection
// (§5 / `subscription.PruningIndexes`) to resolve the right-hand side of a
// join edge at evaluation time. SchemaRegistry satisfies IndexResolver;
// the resolver may also be supplied independently for tests.
type IndexResolver interface {
    IndexIDForColumn(table TableID, col ColID) (IndexID, bool)
}

// SchemaRegistry is the full read-only view of all registered tables,
// indexes, and reducers. It is consumed by SPEC-001 (store), SPEC-002
// (snapshot/recovery), and SPEC-003 (executor reducer lookup). Immutable
// after Build().
type SchemaRegistry interface {
    SchemaLookup
    IndexResolver

    // Tables returns all registered table IDs in stable order.
    Tables() []TableID

    // Reducer returns the handler for the given reducer name.
    Reducer(name string) (ReducerHandler, bool)

    // Reducers returns all registered reducer names in stable order
    // (excluding lifecycle).
    Reducers() []string

    // OnConnect returns the OnConnect handler, or nil if not registered.
    OnConnect() func(*ReducerContext) error

    // OnDisconnect returns the OnDisconnect handler, or nil if not registered.
    OnDisconnect() func(*ReducerContext) error

    // Version returns the application-supplied schema version. See §6.1.
    Version() uint32
}
```

`TableID` is a stable `uint32` assigned deterministically by the builder. User tables receive IDs first in registration order. Built-in system tables are appended afterward in fixed order: `sys_clients`, then `sys_scheduled`. The same registration inputs therefore produce the same IDs across runs.

**Consumer guidance.** Downstream packages should depend on the narrowest interface they need: predicate validation in `subscription/` should declare a local `SchemaLookup` interface satisfied by `SchemaRegistry`; protocol handlers needing only `TableByName` may declare a single-method local interface. The canonical type is the one declared here; local interfaces are documentation for consumer scope, not new types.

---

## 8. TableSchema and Related Types

These types are defined here and consumed by SPEC-001 and SPEC-002:

```go
type TableSchema struct {
    ID      TableID
    Name    string
    Columns []ColumnSchema
    Indexes []IndexSchema
}

type ColumnSchema struct {
    Index    int       // position in Columns (0-based)
    Name     string
    Type     ValueKind // from SPEC-001 §2.1
    Nullable bool      // always false in v1
}

type IndexSchema struct {
    ID      IndexID
    Name    string
    Columns []int // column indices into TableSchema.Columns, in key order
    Unique  bool
    Primary bool // at most one per table; implies Unique
}
```

A table has at most one `Primary` index, and in v1 that primary index must reference exactly one column. If no primary index is declared, the store uses set-semantics (see SPEC-001 §3.3).

---

## 9. Validation Rules

`Build()` enforces the following. Any violation is a returned error with a descriptive message:

**Table-level:**
- Table name must be non-empty, unique across all registered tables, and match `[A-Za-z][A-Za-z0-9_]*`
- At least one column required
- At most one `primarykey` column per table
- `autoincrement` only on integer-typed columns
- `autoincrement` requires `primarykey` (or `unique`) on the same column
- `SchemaVersion` must be called before `Build()` and must be greater than zero

**Column-level:**
- Column name must be non-empty and unique within the table
- Column name must match `[a-z][a-z0-9_]*` (automatically snake_cased from field name if not overridden)
- Column type must be one of the supported Go types (§2)
- Nullable columns are rejected in v1

**Index-level:**
- Index name must be non-empty and unique within the table
- An index must reference at least one column
- Every index column must refer to an existing column
- Composite index columns must be in declaration order (builder path: explicit; reflection path: struct field order)
- A multi-column index with `unique:true` enforces uniqueness on the combined key, not individual columns
- Primary indexes must reference exactly one column in v1
- Mixed `unique` vs non-`unique` declarations for the same named composite index are registration errors
- A single field may not combine `primarykey` with `index` or `index:<name>`
- `-` may not be combined with any other directive
- Duplicate directives on one field are registration errors

**Reducer-level:**
- Reducer name must be non-empty and unique
- Reducer names `"OnConnect"` and `"OnDisconnect"` are reserved for lifecycle hooks
- At most one `OnConnect`, at most one `OnDisconnect`

**Schema-level:**
- Built-in system tables (`sys_clients`, `sys_scheduled`) are registered automatically; user code must not register tables with those names

---

## 10. Built-In System Tables

The builder automatically registers two system tables. They are not declared by user code.

### 10.1 sys_clients

```go
type sysClient struct {
    ConnectionID []byte `shunter:"name:connection_id,primarykey"`
    Identity     []byte // 32-byte canonical form of Identity; see SPEC-001 §2.4
    ConnectedAt  int64  `shunter:"name:connected_at"` // Unix nanoseconds
}
```

The `connection_id` column stores 16 raw bytes (see `ConnectionID` type, SPEC-005 §2). The primary key uses lexicographic byte ordering (SPEC-001 §2.2). A 16-byte fixed-size field fits the `Bytes` ValueKind.

Inserted on connect, deleted on disconnect. Readable by reducer code. Changes produce subscription deltas like any user table.

### 10.2 sys_scheduled

```go
type sysScheduled struct {
    ScheduleID  uint64 `shunter:"name:schedule_id,primarykey,autoincrement"`
    ReducerName string `shunter:"name:reducer_name"`
    Args        []byte `shunter:"name:args"`           // BSATN-encoded args
    NextRunAtNs int64  `shunter:"name:next_run_at_ns"` // Unix nanoseconds (absolute)
    RepeatNs    int64  `shunter:"name:repeat_ns"`      // 0 = one-shot; >0 = repeat interval
}
```

Managed by the scheduler. Entries are inserted when `SchedulerHandle.Schedule()` is called from reducer code. One-shot entries are deleted only when the scheduled reducer transaction commits successfully. Repeating entries stay in place and update `next_run_at_ns` on successful commit. On scheduled reducer failure or crash-before-commit, the row remains as the durable source of truth.

---

## 11. Reflection Path Details

### 11.1 Field Discovery

Reflection visits exported struct fields in declaration order. For each field:
1. If the field has tag `shunter:"-"`: skip
2. If the field is an embedded (anonymous) non-pointer struct: recursively flatten its exported fields in declaration order
3. If the field is an embedded pointer-to-struct: registration error
4. If the field type is not in the supported type list (§2): registration error
5. Otherwise: parse tag directives, derive column schema

Embedded structs are flattened: fields of an embedded (anonymous) non-pointer struct are treated as if declared directly in the outer struct, in declaration order (embedded struct fields first, then outer fields).

Unexported fields are silently ignored.

### 11.2 Column Naming

Default column name: snake_case conversion of the Go field name (`PlayerID` → `player_id`). Override with `shunter:"name:player_id"` when a custom external name is needed.

### 11.3 Error Reporting

Reflection errors include the struct type name and field name for debuggability:

```
schema error: Player.CachedAt: field type *time.Time is not supported; use int64 for Unix nanoseconds
schema error: Player.ID: autoincrement requires primarykey or unique
schema error: Player: duplicate primarykey on fields ID and UID
```

---

## 12. Client Code Generation Interface

The `shunter-codegen` tool generates client-side type definitions from registered schemas. It is a build-time tool, not a runtime concern.

### 12.1 Schema Export

The engine exposes a schema dump function:

```go
// ExportSchema returns a serializable description of all registered tables
// and reducers, suitable for client code generation.
func (e *Engine) ExportSchema() *SchemaExport

type SchemaExport struct {
    Version  uint32
    Tables   []TableExport
    Reducers []ReducerExport
}

type TableExport struct {
    Name    string
    Columns []ColumnExport
    Indexes []IndexExport
}

type ColumnExport struct {
    Name string
    Type string // "bool", "int8", ... "string", "bytes"
}

type IndexExport struct {
    Name    string
    Columns []string
    Unique  bool
    Primary bool
}

type ReducerExport struct {
    Name      string
    Lifecycle bool // true for OnConnect / OnDisconnect
    // Args and return type are not introspectable in v1 (byte-oriented handler).
    // Typed reducer call codegen requires separate reducer metadata (future work).
}
```

### 12.2 Codegen Tool

`shunter-codegen` is invoked as:

```
shunter-codegen --lang typescript --schema schema.json --out ./generated/
```

It reads a `SchemaExport` (serialized to JSON) and produces:
- TypeScript: type definitions for all table row types, typed subscription helpers
- Future: Go client types, C# types, etc.

How `schema.json` is produced: the application binary exports its schema via a `--export-schema` flag or a `go generate` directive that calls `engine.ExportSchema()` and writes JSON. The exact mechanism is left to the application.

**v1 scope:** Codegen for typed reducer argument/return types is out of scope. Reducer argument types must be documented manually or via a separate annotation mechanism.

---

## 13. Error Catalog

| Error | Condition |
|---|---|
| `ErrDuplicateTableName` | Two tables registered with the same name |
| `ErrDuplicateReducerName` | Two reducers registered with the same name |
| `ErrReservedTableName` | User code registers `sys_clients` or `sys_scheduled` |
| `ErrUnsupportedFieldType` | Struct field type not in the supported set |
| `ErrDuplicatePrimaryKey` | Multiple `primarykey` declarations on the same table |
| `ErrAutoIncrementRequiresKey` | `autoincrement` without `primarykey` or `unique` |
| `ErrAutoIncrementType` | `autoincrement` on a non-integer column |
| `ErrSequenceOverflow` | Auto-increment would exceed the column type's range |
| `ErrEmptyTableName` | Table name is empty |
| `ErrInvalidColumnName` | Column name contains invalid characters |
| `ErrSchemaMismatch` | Schema version or structure differs from snapshot |
| `ErrSchemaVersionNotSet` | `Build()` called without `SchemaVersion()` |
| `ErrNoTables` | `Build()` called with no registered tables |
| `ErrColumnNotFound` | Column reference resolves to a name not present on the named table |

`ErrColumnNotFound` is the canonical schema-layer sentinel for column-name lookup misses against the `SchemaRegistry`. SPEC-001 and SPEC-004 re-export or reference it (SPEC-001 §9, SPEC-004 EPICS Epic 1); the declaration here is authoritative. Produced anywhere `SchemaRegistry.TableByName(...)` + column-name lookup fails: SPEC-004 predicate validation (Story 1.2), SPEC-004 subscription registration (Story 4.2), SPEC-001 integrity checks that reach the schema through the registry.

---

## 14. Open Questions

1. **Schema version auto-derivation.** Should Shunter automatically compute a schema fingerprint (hash of column names + types) and use it as the version, eliminating the need for manual `SchemaVersion(n)` calls? Risk: adds complexity and may produce false mismatches on field reordering. Recommendation: keep explicit version for v1.

2. **Typed reducer registration and codegen metadata.** The byte-oriented `ReducerHandler` signature does not expose typed reducer arguments/returns to codegen. Recommendation: add an explicit reducer metadata registration surface in v2 rather than implicit reflection.

3. **Future support for composite primary keys.** `IndexSchema` could represent them, but reflection tags and the builder path intentionally do not in v1. Recommendation: defer until the store/query surface proves a need.

4. **Automatic table pluralization.** This spec uses snake_case names but does not auto-pluralize (`player_session`, not `player_sessions`). Recommendation: keep table naming explicit rather than heuristic in v1.

---

## 15. Verification

| Test | What it verifies |
|---|---|
| Register struct with all supported field types, build → no error | Full type coverage |
| Register struct with `*time.Time` field → ErrUnsupportedFieldType | Unsupported type rejection |
| Register struct with two `primarykey` fields → ErrDuplicatePrimaryKey | Validation |
| Register struct with `autoincrement` on string → ErrAutoIncrementType | Validation |
| Register struct with embedded pointer-to-struct field → registration error | Embedding guardrail |
| Register two tables with same name → ErrDuplicateTableName | Duplicate detection |
| Build without SchemaVersion → ErrSchemaVersionNotSet | Required field |
| Reflection: `name:player_id` tag overrides column name | Name override |
| Reflection: `shunter:"-"` field excluded from schema | Exclusion |
| Reflection: unexported field silently ignored | Unexported field |
| Reflection: embedded non-pointer struct fields flattened in order | Embedding |
| Composite index: two fields with same `index:<name>` produce one multi-column index | Multi-column index |
| Composite unique index with mixed `unique` flags across fields → error | Composite uniqueness consistency |
| Plain `index` generates `<column>_idx`; plain `unique` generates `<column>_uniq` | Generated index naming |
| Builder path: explicit TableDefinition produces same schema as reflection path | Path equivalence |
| ExportSchema() produces correct JSON for all table and reducer registrations | Codegen interface |
| SchemaRegistry.Table() returns correct schema after Build() | Registry lookup |
| sys_clients and sys_scheduled exist in SchemaRegistry after Build() | System table registration |
| Register table named `sys_clients` → ErrReservedTableName | Reserved name protection |
