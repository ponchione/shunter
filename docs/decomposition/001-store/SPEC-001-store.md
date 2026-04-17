# SPEC-001 — In-Memory Relational Store

**Status:** Draft  
**Depends on:** SPEC-006 (Schema Definition) for type and table registration APIs; SPEC-003 (Transaction Executor) for the shared `TxID` type consumed by `Commit` and embedded in `Changeset`  
**Depended on by:** SPEC-003 (Transaction Executor), SPEC-004 (Subscription Evaluator)

---

## 1. Purpose and Scope

The in-memory store is the central data layer of Shunter. It holds the entire working dataset in RAM and provides:

- Typed table storage with schema-defined columns
- Primary key uniqueness enforcement and secondary indexes
- Transactional isolation between concurrent readers and a single writer
- Changeset production at commit time: the net-effect set of row insertions and deletions that downstream consumers (subscription evaluator, commit log) observe

This spec does not cover:
- Schema registration (covered by SPEC-006)
- Snapshot serialization format (covered by SPEC-002)
- Reducer execution lifecycle (covered by SPEC-003)
- Subscription evaluation (covered by SPEC-004)

### 1.1 Canonical Go-package homes

The cross-spec engine identifier types — `RowID`, `Identity`, `ConnectionID`, `ColID`, `TxID`, `SubscriptionID` — are declared in the `types/` Go package (`types/types.go`), which is the single authoritative home. SPEC-001 §2 defines contract semantics; SPEC-003 owns the `TxID` contract (§6 of SPEC-003). Other specs that reference these types do not redeclare them.

---

## 2. Type System

### 2.1 Column Types

Shunter supports the following column value types:

**Scalar types** — flat column types supported in v1:

| Go type | Shunter name | Notes |
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
| `float32` | Float32 | Ordered IEEE-754 value; NaN is rejected on insert |
| `float64` | Float64 | Ordered IEEE-754 value; NaN is rejected on insert |
| `string` | String | UTF-8; variable length |
| `[]byte` | Bytes | Variable length |

**Composite types**:
- No nested structs or arrays in v1. Columns are flat scalar values only.

### 2.2 Row Representation

A row is a `ProductValue`: an ordered slice of column values matching the table's schema.

```go
// ProductValue is an ordered, schema-aligned list of column values.
// Index i corresponds to column i in the table's ColumnSchema.
type ProductValue []Value

// Value is a tagged union of all v1 column types.
// Fields not used by the current kind MUST be zero values.
// Values are immutable once inserted into a row.
type Value struct {
    kind ValueKind

    b   bool
    i64 int64
    u64 uint64
    f32 float32
    f64 float64
    str string
    buf []byte
}
```

`Value` is a data type, not an interface. The store relies on predictable equality, ordering, hashing, and low-overhead key extraction. A tagged struct is easier to compare, hash, and embed in index keys than an interface-based design.

**Required invariants:**
- Each `Value`'s `kind` must match exactly one populated payload field.
- `String` values are immutable UTF-8 strings.
- `Bytes` values are immutable byte slices. The store must copy caller-provided byte slices on insert unless it can prove exclusive ownership.
- `ProductValue` contents are immutable once inserted into the store or a transaction buffer.

**Equality rules:**
- Bools compare by boolean value.
- Signed integers compare by signed numeric value.
- Unsigned integers compare by unsigned numeric value.
- Strings compare by byte-for-byte UTF-8 contents.
- Bytes compare by byte-for-byte contents.
- Floats compare by numeric value; NaN is not a legal stored value.

**Ordering rules (used by B-tree indexes):**
- `false < true`
- signed integers by numeric value
- unsigned integers by numeric value
- strings lexicographically by UTF-8 bytes
- bytes lexicographically by raw byte values
- floats by numeric value; because NaN is rejected, the comparator is total over stored values

**Hashing rules (used by set-semantics duplicate prevention):**
- Hash over `(kind, canonical payload bytes)`.
- Strings hash their UTF-8 bytes.
- Bytes hash raw byte contents.
- Floats hash a canonical bit encoding after canonicalizing `-0.0 → +0.0` so that the Equal→Hash contract holds for signed zero (IEEE `-0.0 == +0.0`, so hashes must collide).

**Recommendation:** Store rows as `ProductValue` (decoded) in the in-memory store rather than as raw BSATN bytes. This simplifies index key extraction, predicate evaluation, and delta delivery. BSATN encoding happens at the boundary: when writing to the commit log or sending to clients.

Alternative considered: store rows as `[]byte` (BSATN encoded) and decode on demand. This reduces per-row memory but adds decode cost on every read, predicate evaluation, and index build. Rejected for v1; can be revisited for memory-constrained environments.

Additional tradeoff: decoded rows consume more RAM than compact bytes, especially for strings and byte slices with Go slice/string headers. That is acceptable in v1 because the main store requirement is simple, direct access for indexing, transaction reads, and delta production. Memory-focused representations should be revisited only if profiling shows store memory overhead, rather than subscription evaluation or commit-log encoding, is the dominant bottleneck.

### 2.3 RowID

A `RowID` is a `uint64` that uniquely identifies a row within a table for the lifetime of the store. RowIDs are never reused after deletion.

```go
type RowID uint64
```

RowIDs are allocated by a per-table monotonic counter. They are used as index values in secondary indexes (indexes store RowID → ProductValue lookup), and in the delete set of a transaction.

Within a transaction, newly inserted rows receive a provisional `RowID` immediately from the same per-table counter. If the transaction rolls back, the consumed RowIDs are not reused. Gaps in RowID allocation are therefore allowed.

**Not a stable external identifier**: RowIDs are internal. Clients identify rows by their primary key value. RowIDs may change across snapshot restore.

---

### 2.4 Identity

```go
type Identity [32]byte // declared in types/types.go
```

`Identity` is the 32-byte canonical client identifier. It is derived from `(issuer, subject)` JWT claims at authentication time. The derivation algorithm is outside the scope of v1 specs; the only required property is that the same `(iss, sub)` pair always produces the same `Identity`. Derivation helpers (`DeriveIdentity`, `Hex`, `ParseIdentityHex`, `IsZero`) live alongside the type in `types/identity.go`. Other specs (notably SPEC-005) reference this declaration rather than redeclaring the type.

---

### 2.5 ColID

```go
type ColID int
```

`ColID` is the zero-based column index within a `TableSchema.Columns` slice. It is the same integer as `ColumnSchema.Index`. The named type exists to distinguish it from raw `int` in function signatures. SPEC-004 uses `ColID` in predicate types.

---

## 3. Table Structure

### 3.1 Schema

Each table is defined by a `TableSchema`:

```go
type TableSchema struct {
    ID      TableID
    Name    string
    Columns []ColumnSchema
    Indexes []IndexSchema
}

type ColumnSchema struct {
    Index         int
    Name          string
    Type          ValueKind
    Nullable      bool  // reserved; MUST be false in v1 — SPEC-006 §9 reserves ErrNullableColumn for this rule, but the v1 builder cannot set Nullable=true so explicit rejection is deferred to a Session 12+ drift item (see SPEC-006 §13 / TECH-DEBT). Canonical declaration in SPEC-006 §8.
    AutoIncrement bool  // per-column auto-increment; SPEC-006 §9 enforces integer type + PrimaryKey/Unique
}

type IndexSchema struct {
    ID      IndexID
    Name    string
    Columns []int    // column indices into TableSchema.Columns
    Unique  bool
    Primary bool     // at most one per table; implies Unique
}
```

**Primary key rules:**
- At most one primary key per table
- Primary key must be unique
- If no primary key is declared, the table uses set semantics (no exact duplicate rows allowed, enforced via row hash)

### 3.2 In-Memory Table

Each table is a `Table` struct:

```go
type Table struct {
    schema       *TableSchema
    rows         map[RowID]ProductValue      // live rows
    nextID       uint64                      // monotonically increasing RowID counter
    indexes      []*Index                    // one per IndexSchema
    rowHashIndex map[uint64][]RowID          // optional; only for set-semantics tables
}
```

**Storage model:** `map[RowID]ProductValue` is simple, supports O(1) insert and delete by RowID, and allows iteration. For v1, this is the recommended approach. A slice-with-bitmap (like SpacetimeDB's page layout) would reduce GC pressure on very large tables but adds complexity; defer to v2.

### 3.3 Set Semantics (Duplicate Prevention)

When a table has no primary key, a `rowHashIndex` maps the hash of a `ProductValue` to one or more candidate RowIDs:
1. Hash the row using the value-hashing rules from §2.2
2. Look up the candidate bucket `rowHashIndex[rowHash]`
3. Compare the candidate rows for exact equality
4. If an exact duplicate exists: reject insert with `ErrDuplicateRow`
5. Otherwise append the new RowID to the bucket

A hash bucket may contain multiple RowIDs due to collisions. Implementations MUST NOT assume the hash is unique.

When a primary key exists, the unique primary index enforces row uniqueness for v1. The rowHashIndex is not created.

---

## 4. Indexes

### 4.1 Index Types

Two index types are supported in the abstract interface, but only one is recommended for v1:

**BTree index** — ordered index for point lookup, range scans, and stable key ordering:
- Backed by a B-tree implementation with a caller-supplied comparator for `IndexKey`
- Supports: point lookup, range scan (lower/upper bound), iteration in key order
- Used for: primary keys and all secondary indexes in v1

**Hash index** — equality-only index for a possible future optimization:
- Supports: point lookup only
- May be added in v2 if profiling shows a clear need for equality-only acceleration

**Recommendation:** Use BTree for all indexes in v1. The implementation surface is simpler, it covers more query patterns, and it avoids maintaining two index backends before profiling justifies them.

### 4.2 Index Structure

```go
type Index struct {
    schema *IndexSchema
    btree  *BTreeIndex
}

// uniqueness and primary-ness are derived from schema.Unique / schema.Primary

// IndexID is a stable uint32 that identifies an index within a table.
// Assigned by the store in the order IndexSchema entries appear in TableSchema.Indexes.
// IndexID 0 is always the primary index if one exists; subsequent IDs are assigned in
// declaration order.
type IndexID uint32

// IndexKey is the materialized key for one index entry.
// Single-column indexes still use the same representation with len(parts)==1.
type IndexKey struct {
    parts []Value
}

// BTreeIndex maps a key (extracted from one or more columns) to one or more RowIDs.
// For unique indexes, at most one RowID exists per key.
type BTreeIndex struct {
    tree ordered_map[IndexKey][]RowID
}
```

`ordered_map` is placeholder notation. The implementation may use any B-tree package or custom tree so long as it supports:
- comparator-supplied key ordering
- point lookup
- bounded range scans
- ordered iteration

### 4.3 Multi-Column Index Keys

A multi-column index key is an `IndexKey{parts: []Value}` compared lexicographically:
1. Compare values at position 0 using the ordering rules from §2.2
2. If equal, compare position 1, and so on
3. If all compared positions are equal and lengths match, the keys are equal

Additional requirements:
- Single-column indexes still materialize a one-element `IndexKey`
- `[]byte` columns use raw lexicographic byte ordering
- NaN is never present because it is rejected on insert
- For non-unique indexes, duplicate RowIDs under the same key are yielded in ascending `RowID` order

### 4.4 Bound

```go
// Bound represents one endpoint of an index range scan.
type Bound struct {
    Value     Value
    Inclusive bool   // true = closed (<=/>= ); false = open (</>)
    Unbounded bool   // true = no limit on this side; Value is ignored
}
```

A nil `*IndexKey` in `SeekRange` (§4.6) maps to `Bound{Unbounded: true}`.

---

### 4.5 Index Maintenance

On every insert/delete, all indexes are updated:

**Insert row (rowID, row):**
1. For each index: extract key columns from row; insert `key → rowID` into index
2. If unique index and key already present: reject insert, return `ErrUniqueConstraintViolation`
3. If rowHashIndex present: append rowID to the hash bucket after exact-duplicate check

**Delete row (rowID, row):**
1. For each index: extract key from row; remove `key → rowID` from index
2. If rowHashIndex present: remove the specific rowID from the hash bucket and delete the bucket if it becomes empty

Index maintenance is synchronous. No deferred/lazy maintenance in v1.

### 4.6 Index Scan API

```go
// Seek returns all RowIDs with key exactly matching the given value.
func (idx *Index) Seek(key IndexKey) []RowID

// SeekRange returns all RowIDs with key in [low, high).
// A nil low bound means unbounded below.
// A nil high bound means unbounded above.
// Half-open convenience wrapper over SeekBounds.
func (idx *Index) SeekRange(low, high *IndexKey) iter.Seq[RowID]

// SeekBounds returns all RowIDs with key in the range specified by Bound
// semantics (§4.4). Used by CommittedReadView.IndexRange (§7.2) and SPEC-004
// predicate scans that require exclusive endpoints on string/bytes/float keys
// where "strictly greater than v" cannot be expressed via *IndexKey alone.
func (idx *Index) SeekBounds(low, high Bound) iter.Seq[RowID]

// Scan iterates all RowIDs in key order.
func (idx *Index) Scan() iter.Seq[RowID]
```

---

## 5. Transaction Model

### 5.1 Overview

The store uses a **two-layer transaction model** matching the architectural requirements of the single-goroutine executor (SPEC-003):

- **CommittedState** — the authoritative state after all committed transactions
- **TxState** — the local mutation buffer for the in-progress transaction

A transaction sees a unified view of both layers via a `StateView`.

### 5.2 CommittedState

```go
type CommittedState struct {
    tables map[TableID]*Table    // all committed tables
    mu     sync.RWMutex          // guards tables for snapshot reads
}
```

CommittedState is mutated only at commit time, by the single writer goroutine. `mu` is held for write during commit and for read during concurrent read-only snapshot access (for short-lived committed-state reads; see §7).

### 5.3 TxState

```go
type TxState struct {
    inserts map[TableID]map[RowID]ProductValue   // tx-local rows keyed by provisional RowID
    deletes map[TableID]map[RowID]struct{}       // committed RowIDs deleted by this TX
}
```

TxState does not hold a full `Table` instance. Instead, it keeps:
- tx-local inserted rows keyed by provisional `RowID`
- a delete set of committed `RowID`s hidden by this transaction

This avoids duplicating committed index structures for v1. Reads over tx-local inserts are linear scans of the insert map values. If insert-heavy transactions become a bottleneck, a lightweight tx-local index may be added in v2.

**Visibility invariants:**
- A `RowID` present in `tx.inserts[tableID]` is visible to the transaction unless later deleted in the same transaction.
- A `RowID` present in `tx.deletes[tableID]` refers to a row that exists in committed state and is hidden from the transaction.
- A committed `RowID` and a tx-local provisional `RowID` are both represented by the same `RowID` type; the transaction distinguishes them by checking `tx.inserts` first, then committed state, then `tx.deletes`.

**Invariant:** All tx-local RowIDs allocated during a transaction are strictly greater than any committed RowID that existed when the transaction began. This holds because both classes draw from the same per-table monotonically increasing counter with no reuse. An implementer MUST NOT rely on check order alone to distinguish RowID classes — the ordering invariant is what makes the check-order approach correct.

**Design decision:** SpacetimeDB maintains a full `Table` with indexes inside TxState. That makes large insert-then-query transactions faster. Shunter v1 uses linear scans of tx-local inserts instead. Rationale: most reducers insert small numbers of rows, and the extra indexing complexity is not justified before profiling.

### 5.4 StateView — Unified Read Interface

```go
// RowIterator iterates (RowID, ProductValue) pairs from a scan.
// Equivalent to iter.Seq2[RowID, ProductValue].
type RowIterator = iter.Seq2[RowID, ProductValue]

type StateView struct {
    committed *CommittedState
    tx        *TxState
}

// GetRow returns the row for the given table and RowID,
// or (nil, false) if not found or deleted.
func (sv *StateView) GetRow(tableID TableID, rowID RowID) (ProductValue, bool)

// ScanTable iterates all rows visible to this transaction.
// Yields (RowID, ProductValue) pairs.
// Order is undefined.
func (sv *StateView) ScanTable(tableID TableID) iter.Seq2[RowID, ProductValue]

// SeekIndex performs a point lookup on the given index.
// Includes tx-local inserted rows that match the key.
func (sv *StateView) SeekIndex(tableID TableID, indexID IndexID, key IndexKey) iter.Seq[RowID]

// SeekIndexRange performs a half-open [low, high) range scan.
func (sv *StateView) SeekIndexRange(tableID TableID, indexID IndexID, low, high *IndexKey) iter.Seq[RowID]

// SeekIndexBounds performs a range scan with Bound endpoints (§4.4).
// Required for SPEC-004 predicate scans that need exclusive endpoints on
// non-integer keys; delegates to the committed index's SeekBounds and filters
// tx-local inserts by Bound comparison.
func (sv *StateView) SeekIndexBounds(tableID TableID, indexID IndexID, low, high Bound) iter.Seq[RowID]
```

**GetRow implementation:**
1. If `rowID` exists in `tx.inserts[tableID]`, return that row
2. If `rowID` exists in `tx.deletes[tableID]`, return not found
3. Look up `rowID` in `committed.tables[tableID].rows`

**ScanTable implementation:**
1. Yield all committed `(RowID, ProductValue)` pairs not present in `tx.deletes[tableID]`
2. Yield all tx-local inserted `(RowID, ProductValue)` pairs from `tx.inserts[tableID]`
3. The combined iteration order is undefined; callers must not depend on it

**SeekIndex implementation:**
1. Query the committed index
2. Filter committed results through `tx.deletes[tableID]`
3. Linear-scan `tx.inserts[tableID]` and yield any tx-local rows whose extracted index key equals `key`
4. Yield committed and tx-local RowIDs together; duplicates are impossible because committed rows and tx-local inserted rows use distinct RowIDs

**SeekIndexRange implementation:**
1. Query the committed B-tree range and filter deleted RowIDs
2. Linear-scan tx-local inserts and include any row whose extracted key lies in `[low, high)` using the same comparator as the committed index

**SeekIndexBounds implementation:**
1. Query the committed index via `Index.SeekBounds(low, high)` and filter deleted RowIDs
2. Linear-scan tx-local inserts and include any row whose extracted key satisfies both `Bound` endpoints per §4.4 (inclusive/exclusive/unbounded)

### 5.5 Transaction API

```go
type Transaction struct {
    state   *StateView
    tx      *TxState
    schema  SchemaRegistry    // from SPEC-006
}

// Insert allocates a provisional RowID immediately, adds the row to tx-local state,
// and enforces constraints against committed rows not deleted in this TX plus tx-local inserts.
func (t *Transaction) Insert(tableID TableID, row ProductValue) (RowID, error)

// Delete removes a visible row by RowID.
// If rowID is tx-local, it is removed from tx.inserts.
// If rowID is committed, it is added to tx.deletes.
// Returns ErrRowNotFound if rowID is not visible in the transaction view.
func (t *Transaction) Delete(tableID TableID, rowID RowID) error

// Update replaces a visible row: Delete(rowID) + Insert(newRow).
// If newRow is identical to a committed row deleted earlier in the same transaction,
// the store may cancel the delete instead of creating a fresh insert.
func (t *Transaction) Update(tableID TableID, rowID RowID, newRow ProductValue) (RowID, error)

// View returns the unified StateView for read operations.
func (t *Transaction) View() *StateView
```

**Mutation semantics:**
- `Insert` must check uniqueness and set-semantics constraints against:
  - committed rows not present in `tx.deletes`
  - tx-local rows in `tx.inserts`
- If `Insert` finds an identical committed row that is currently hidden by `tx.deletes`, it must cancel that delete and return the committed row's `RowID` instead of creating a new tx-local row.
- `Delete` of a tx-local row removes it from `tx.inserts`, causing insert-then-delete to collapse to no-op.
- `Delete` of a committed row adds it to `tx.deletes` unless later canceled by an identical reinsert.

### 5.6 Commit

Commit is called by the Transaction Executor (SPEC-003) after a reducer returns successfully:

```go
func Commit(committed *CommittedState, tx *Transaction) (*Changeset, error)
```

The function body accesses `tx.tx` (the embedded `*TxState`) internally. The `SchemaRegistry` is reachable through `committed` when validation needs it.

**TxID ownership (Model A).** The executor allocates and owns the monotonic `TxID` counter (see SPEC-003 §13.2 — recovered from SPEC-002's `max_applied_tx_id` at boot, advanced atomically per commit). `Commit` does not allocate or return a `TxID`. The caller assigns `changeset.TxID = callerSuppliedTxID` on the returned `Changeset` before handing it to durability (`DurabilityHandle.EnqueueCommitted`) or the subscription evaluator. SPEC-001 owns the `Changeset` type; SPEC-003 (`types/`) owns the `TxID` type; the `Changeset.TxID` field is stamped by the caller, not by the store.

**Required invariant:** Commit is atomic from the executor's point of view. If `Commit` returns a non-nil error, committed state MUST be unchanged.

**Algorithm:**
1. Acquire write lock on CommittedState
2. Validate that all remaining commit-time checks still pass against the current committed state
3. For each table with deletes:
   a. Read the deleted row values from committed state and append them to the pending changeset delete list
   b. Remove rows from `committed.tables[tableID].rows`
   c. Update all committed indexes and rowHash buckets
4. For each table with tx-local inserts:
   a. Insert rows into `committed.tables[tableID].rows` using their already-assigned provisional RowIDs
   b. Update all committed indexes and rowHash buckets
5. Build the `Changeset` with `TxID` zero-valued (see §6.1)
6. Release write lock
7. Return `Changeset`; the caller stamps `changeset.TxID`

Deletes are applied before inserts so that update/replace flows do not fail spuriously on uniqueness checks when a transaction removes an old key and inserts a replacement key in the same commit.

### 5.7 Rollback

```go
func Rollback(tx *Transaction)
```

Discard TxState entirely. No committed state is modified. No cleanup needed beyond GC. O(1).

### 5.8 ApplyChangeset

```go
// ApplyChangeset applies a replayed Changeset directly to CommittedState during recovery.
// It does not go through the transaction lifecycle (no TxState, no constraint checks).
// Constraint violations during recovery are fatal: they indicate a corrupt log or schema
// mismatch that recovery cannot resolve.
// Called only by SPEC-002 recovery (OpenAndRecover).
func ApplyChangeset(committed *CommittedState, cs *Changeset) error
```

**Algorithm:**
1. Acquire write lock on CommittedState
2. For each `TableChangeset` in `cs.Tables`:
   a. For each delete row: look up by primary key (or row hash for set-semantics tables), remove from committed table and all indexes
   b. For each insert row: assign a fresh RowID from the per-table counter, insert into committed table and all indexes
3. Release write lock
4. Return error if any table is unknown or any row has wrong column count/type

---

## 6. Changeset

### 6.1 Structure

The `Changeset` is the net-effect output of a committed transaction:

```go
type Changeset struct {
    TxID    TxID
    Tables  map[TableID]*TableChangeset
}

// TxID is declared by SPEC-003 (§6) and lives in the `types/` Go package (SPEC-001 §2.4).
// SPEC-001 imports it as a cross-spec dependency. The executor allocates `TxID`
// (Model A, see §5.6) and stamps `Changeset.TxID` after `Commit` returns; `Commit`
// itself never assigns this field.

type TableChangeset struct {
    TableID   TableID
    TableName string
    Inserts   []ProductValue    // rows whose net effect is "now present"
    Deletes   []ProductValue    // rows whose net effect is "now absent"
}
```

### 6.2 Net-Effect Semantics

The changeset captures net effects, not the raw operation log:

- Row inserted and deleted within the same TX → appears in neither Inserts nor Deletes
- Committed row deleted and then re-inserted with identical value in the same TX → treated as undelete/no-op; appears in neither Inserts nor Deletes
- Row deleted from committed state, different row inserted → deleted row in Deletes, new row in Inserts
- Row inserted (no previous version) → in Inserts only
- Row from committed state deleted (no replacement) → in Deletes only
- Update is represented at store level as delete-old plus insert-new unless the operation collapses to undelete/no-op under the rule above

**Implementation:**
- `Inserts` = tx-local rows that survive to commit and were not collapsed into an undelete/no-op case
- `Deletes` = committed rows named in `tx.deletes[tableID]`, materialized to `ProductValue` before removal, excluding any delete canceled by an identical reinsert

The store does not emit a separate `Updates` list in v1. SPEC-004 must derive subscription-level update vs remove+add behavior by comparing keys/predicate membership across the provided row values.

### 6.3 Consumers

The `Changeset` is passed to:
1. **Subscription Evaluator** (SPEC-004) — to compute per-client deltas
2. **Commit Log** (SPEC-002) — to serialize and persist the transaction

Both receive the same `Changeset` value. It is read-only after creation.

---

## 7. Read-Only Snapshot Access

### 7.1 Purpose

When a new client subscribes, the subscription evaluator needs to deliver the initial result set — all rows currently matching the subscription query. This read happens concurrently with the transaction executor processing writes.

### 7.2 Mechanism

```go
// CommittedReadView is the interface for read-only access to committed state.
// Obtained via CommittedState.Snapshot(). Must be closed when no longer needed.
type CommittedReadView interface {
    // Close releases the snapshot read lock. Must be called exactly once.
    Close()

    // TableScan returns all rows in the table in undefined order.
    TableScan(tableID TableID) RowIterator

    // IndexScan returns all rows whose indexed column equals value, via point lookup.
    // The index is identified by IndexID (SPEC-001 §4.2).
    IndexScan(tableID TableID, indexID IndexID, value Value) RowIterator

    // IndexRange returns all rows whose indexed column falls within [lower, upper].
    // Uses Bound semantics from SPEC-001 §4.4.
    IndexRange(tableID TableID, indexID IndexID, lower, upper Bound) RowIterator

    // RowCount returns the number of rows in the committed table.
    RowCount(tableID TableID) uint64
}

// Snapshot returns a point-in-time read-only view of committed state.
// The snapshot is valid until Close() is called.
// Multiple snapshots may coexist.
func (cs *CommittedState) Snapshot() CommittedReadView

type CommittedSnapshot struct {
    tables map[TableID]*Table    // shallow copy of table map at snapshot time
    mu     *sync.RWMutex         // held as read lock until Close()
}

func (s *CommittedSnapshot) TableScan(tableID TableID) RowIterator
func (s *CommittedSnapshot) IndexScan(tableID TableID, indexID IndexID, value Value) RowIterator
func (s *CommittedSnapshot) IndexRange(tableID TableID, indexID IndexID, lower, upper Bound) RowIterator
func (s *CommittedSnapshot) RowCount(tableID TableID) uint64
func (s *CommittedSnapshot) Close()
```

`IndexScan` implementation: extract the `IndexID` → `*Index` mapping from `TableSchema`, then call the existing `Index.Seek` logic, filtering results to rows matching `value`.

`IndexRange` implementation: call `Index.SeekRange(lower, upper)` using the `Bound` type.

**Implementation:** `Snapshot()` acquires a read lock on `CommittedState.mu` and returns a view over the current table map. The lock is held until `Close()`. The transaction executor acquires a write lock at commit, so commits block until all snapshots are closed.

**Operational rule:** A snapshot may be used only for short in-process materialization work. Callers MUST NOT hold a snapshot while performing network I/O, client encoding, waiting on channels, subscription registration bookkeeping, or any other blocking work. Materialize the needed rows first, then close the snapshot, then continue downstream processing.

**Executor interaction rule:** Initial subscription query execution that must be atomic with subscription registration MUST run as an executor command (SPEC-003 / SPEC-004). Direct snapshots are appropriate only for read-only operations that do not require that atomic registration guarantee.

**Tradeoff:** Long-running initial-state reads delay commits. That is acceptable in v1 only because snapshot users are required to copy out the needed data and close promptly. An alternative (read-copy-update / epoch-based reclamation) avoids blocking commits entirely but adds complexity; deferred to v2.

---

## 8. Auto-Increment / Sequence

Tables with an `autoincrement` column (see SPEC-006) maintain a per-table sequence:

```go
type Sequence struct {
    next uint64    // next value to issue
    mu   sync.Mutex
}
```

On insert, if the row's sequence column is zero, it is replaced with the next sequence value. Sequence values are monotonically increasing and are persisted in the commit log (the current sequence value is included in snapshots).

---

## 9. Error Catalog

| Error | Condition |
|---|---|
| `ErrTableNotFound` | tableID references unknown table |
| `ErrColumnNotFound` | re-exported from SPEC-006 §13 — column name lookup miss against the `SchemaRegistry`; SPEC-001 re-exports so store-layer integrity paths can construct and match the sentinel without importing SPEC-006 directly |
| `ErrTypeMismatch` | column value has wrong type for column schema |
| `ErrRowShapeMismatch` | row column count does not match `TableSchema.Columns`; raised by Story 2.3 `ValidateRow` |
| `ErrPrimaryKeyViolation` | insert would duplicate existing primary key |
| `ErrUniqueConstraintViolation` | insert would duplicate unique index key (non-PK) |
| `ErrDuplicateRow` | exact row already exists (set-semantics check, no PK) |
| `ErrRowNotFound` | delete or update targets RowID not visible in the transaction view |
| `ErrNullNotAllowed` | null value in non-nullable column (v1: all columns non-nullable) |
| `ErrInvalidFloat` | attempted insert/update with NaN in a float column |

---

## 10. Performance Constraints

These are aspirational microbenchmark targets for a first implementation, not contractual SLAs. They should be used to detect major regressions and obvious design mistakes, then recalibrated once a real implementation exists:

| Operation | Target |
|---|---|
| Single-row insert (no index update) | < 500 ns |
| Single-row insert (4 indexes) | < 2 µs |
| Single-row delete | < 1 µs |
| Full scan of 100k-row table | < 10 ms |
| Index point lookup | < 200 ns |
| Index range scan (1k rows returned) | < 500 µs |
| Commit (100 inserts, no deletes) | < 500 µs |
| Rollback | < 10 µs |

---

## 11. Interfaces to Other Subsystems

### SPEC-006 (Schema Definition)
The store receives a `SchemaRegistry` at startup. Tables are defined by `TableSchema` values registered before the engine starts. The store does not modify schemas at runtime in v1.

`SchemaRegistry` is defined in SPEC-006 §7. The store uses only the `Table` and `Tables` methods, but accepts the full interface so callers can pass the engine's `SchemaRegistry` directly without wrapping.

### SPEC-003 (Transaction Executor)
The executor creates a `Transaction`, calls reducer code against it, then calls `Commit` or `Rollback`. The executor owns the single write goroutine. The store does not enforce single-writer access; the executor is responsible for serialization.

The executor may rely on the following store guarantees:
- `Insert` returns the provisional `RowID` immediately
- `Delete` and `Update` operate on rows visible through the transaction's `StateView`
- `Commit` returns a net-effect `Changeset` after applying deletes before inserts
- `Rollback` discards tx-local state and leaves committed state unchanged

```go
// Exported by store:
func NewTransaction(committed *CommittedState, schema SchemaRegistry) *Transaction
func Commit(committed *CommittedState, tx *Transaction) (*Changeset, error)
func Rollback(tx *Transaction)
func (cs *CommittedState) Snapshot() CommittedReadView
```

`Commit` does not allocate or return a `TxID`. The executor allocates `TxID` (Model A; see §5.6, SPEC-003 §13.2) and stamps `changeset.TxID` on the returned value before handing the `Changeset` to durability and the subscription evaluator.

### SPEC-004 (Subscription Evaluator)
The evaluator receives a `*Changeset` from the executor after each commit. It also receives a committed read view for post-commit evaluation. Direct calls to `CommittedState.Snapshot()` are appropriate only for read-only operations that do not require atomic registration semantics.

The evaluator may rely on the following:
- for each `tc := range Changeset.Tables`, `tc.Inserts` are rows newly present after commit for that table
- for each `tc := range Changeset.Tables`, `tc.Deletes` are rows newly absent after commit for that table
- identical delete+reinsert collapses to no-op and therefore emits neither insert nor delete
- no separate update list exists in v1; update detection is derived from row values and subscription predicate semantics
- snapshot scans and index seeks see only committed state, never tx-local state

### SPEC-002 (Commit Log)
The commit log receives a `*Changeset` and serializes it for durability. It also drives snapshot creation using `CommittedState.Snapshot()`.

For snapshot/recovery coupling, the commit log may rely on the store exposing enough state to reconstruct future RowID and sequence allocation:
- all committed table rows
- per-table `nextID`
- per-table sequence state (`Sequence.next`) for any auto-increment column

---

## 12. Open Questions

1. **In-TX index for large insert batches**: If a reducer inserts 10k rows and immediately queries them by secondary index, the linear-scan TxState becomes expensive. Add an opt-in TX-local index per table in v2 when profiling reveals the need.

2. **Table size limit**: No explicit limit in v1. The operating assumption is that the entire dataset fits in RAM. OOM is handled at the OS level. Document this explicitly.

3. **Concurrent snapshot duration**: Currently, long initial-sync reads block commits. Should a future design replace lock-held snapshots with RCU/epoch reclamation? Deferred to v2 after real workload evidence.

4. **Performance targets**: The current latency goals should be treated as aspirational microbenchmark targets, not contractual SLAs. Recalibrate them once an implementation exists and profiling data is available.

---

## 13. Verification

Each of the following must be testable independently of other subsystems.

| Test | What it verifies |
|---|---|
| Insert into table, scan, find row | Basic row storage and retrieval |
| Insert duplicate primary key → error | PK uniqueness enforcement |
| Insert exact duplicate (no PK) → error | Set-semantics enforcement |
| Row-hash collision with non-equal rows | Collision buckets do not cause false duplicate rejection |
| Insert tx-local row, delete same RowID in same TX, commit → empty changeset | Tx-local provisional RowID semantics and net-effect collapse |
| Insert, delete, scan → row absent | Delete removes from storage |
| Insert row A (committed), delete row A in TX, insert identical row A, commit → empty changeset | Undelete/no-op semantics |
| Insert row A (committed), delete row A in TX, insert row B, commit | Changeset has B in Inserts, A in Deletes |
| Update row so indexed column changes | Delete+insert update path maintains index correctness |
| B-tree index: insert 10k rows, range scan, verify count and order | Index correctness |
| B-tree range with nil low or nil high bound | Open-bound range semantics |
| Unique index: insert duplicate → error | Index uniqueness enforcement |
| Bytes column: insert keys with differing byte prefixes, range/point seek returns lexicographic order | Raw byte ordering semantics |
| Reject NaN insert/update | Float validity enforcement |
| Rollback: insert rows, rollback, scan committed state → empty | Rollback leaves no trace |
| Rollback after provisional RowID allocation, then insert new row | RowID gaps are allowed and not reused |
| Concurrent snapshot: hold snapshot, attempt commit, verify commit blocks until snapshot close | Snapshot locking semantics |
| Multi-column index: insert rows, seek by compound key | Multi-column index correctness |
| Auto-increment: insert rows without PK value, verify unique sequential values | Sequence correctness |
