# SPEC-001 — Epic Decomposition

Source: [SPEC-001-store.md](./SPEC-001-store.md)

---

## Epic 1: Core Value Types

**Spec sections:** §2.1, §2.2, §2.3, §2.4, §2.5

Build the foundational type system everything else sits on.

**Scope:**
- `ValueKind` enum (Bool, Int8..Uint64, Float32, Float64, String, Bytes)
- `Value` tagged-union struct with kind + payload fields
- `ProductValue` (ordered `[]Value`)
- `RowID` (uint64), `Identity` ([32]byte), `ColID` (int)
- Value equality comparison (per §2.2 rules)
- Value ordering comparison (per §2.2 rules — used by B-tree later)
- Value hashing (per §2.2 rules — used by set-semantics later)
- NaN rejection on float construction
- Immutability contract: `Bytes` values copy on insert

**Testable outcomes:**
- Round-trip each ValueKind through construction + equality
- Ordering: `false < true`, signed/unsigned numeric, lexicographic string/bytes
- Hashing: equal values produce equal hashes, NaN rejected
- ProductValue equality and hashing over composite rows

**Dependencies:** None. This is the leaf.

---

## Epic 2: Schema & Table Storage

**Spec sections:** §3.1, §3.2, §2.3 (RowID allocation)

Bare table that stores rows by RowID. No indexes, no constraints yet.

**Scope:**
- `TableSchema`, `ColumnSchema`, `IndexSchema` structs
- `TableID`, `IndexID` types
- `Table` struct: `rows map[RowID]ProductValue`, `nextID uint64`, `schema *TableSchema`
- Row insert: allocate monotonic RowID, store in map
- Row delete: remove from map
- Row lookup by RowID
- Full table scan (unordered iteration)
- Type validation on insert (column count, ValueKind matches ColumnSchema.Type)

**Testable outcomes:**
- Insert row, get RowID back, look up by RowID — matches
- Delete row, look up — not found
- RowID monotonically increases, gaps allowed after delete
- Insert with wrong column count (`ErrRowShapeMismatch`) or type — error
- Full scan yields all live rows

**Dependencies:** Epic 1 (Value types)

---

## Epic 3: B-Tree Index Engine

**Spec sections:** §4.1, §4.2, §4.3, §4.4, §4.6

The index data structure, standalone. Not wired to tables yet.

**Scope:**
- `IndexKey` struct (parts []Value)
- `Bound` struct (Value, Inclusive, Unbounded)
- `BTreeIndex` backed by ordered map with comparator
- IndexKey lexicographic comparison using Value ordering from Epic 1
- Multi-column key comparison
- `Seek(key)` — point lookup, returns []RowID
- `SeekRange(low, high)` — bounded range scan, returns iter.Seq[RowID]
- `Scan()` — full ordered iteration
- Insert key→RowID mapping
- Remove key→RowID mapping
- For non-unique keys: multiple RowIDs per key, yielded in ascending RowID order

**Testable outcomes:**
- Insert 10k keys, range scan returns correct count and order
- Point lookup on unique key returns single RowID
- Point lookup on non-unique key returns all RowIDs in ascending order
- Nil/unbounded low or high bound works correctly
- Multi-column key: (A,1) < (A,2) < (B,1)
- Remove key→RowID, seek no longer returns it

**Dependencies:** Epic 1 (Value types, ordering)

---

## Epic 4: Table Indexes & Constraints

**Spec sections:** §4.5, §3.3, §3.1 (PK rules)

Wire indexes into tables. Add constraint enforcement.

**Scope:**
- `Index` struct wrapping `BTreeIndex` + `IndexSchema`
- Table gets `indexes []*Index` field
- Synchronous index maintenance on insert/delete
- Primary key: at most one per table, implies unique
- Unique index enforcement: reject insert on duplicate key → `ErrUniqueConstraintViolation` / `ErrPrimaryKeyViolation`
- Set semantics (no-PK tables): `rowHashIndex map[uint64][]RowID`
- Hash-bucket duplicate detection with exact equality check
- `ErrDuplicateRow` on exact duplicate in set-semantics table
- Key extraction from row given column indices

**Testable outcomes:**
- Insert duplicate PK → `ErrPrimaryKeyViolation`
- Insert duplicate unique index key → `ErrUniqueConstraintViolation`
- Insert exact duplicate row (no PK) → `ErrDuplicateRow`
- Hash collision with non-equal rows: both accepted
- After delete, previously conflicting key re-insertable
- Index seeks return correct RowIDs after insert/delete cycles

**Dependencies:** Epic 2 (Table), Epic 3 (BTreeIndex)

---

## Epic 5: Transaction Layer

**Spec sections:** §5.1–§5.5

TxState overlay on CommittedState. Unified read path.

**Scope:**
- `CommittedState` struct with `tables map[TableID]*Table` and `sync.RWMutex`
- `TxState` struct: `inserts map[TableID]map[RowID]ProductValue`, `deletes map[TableID]map[RowID]struct{}`
- `StateView` struct: merged read over committed + tx-local
- `GetRow` — check tx.inserts, then tx.deletes, then committed
- `ScanTable` — committed rows minus deletes, plus tx-local inserts
- `SeekIndex` — committed index filtered by deletes + linear scan of tx inserts
- `SeekIndexRange` — same pattern for range scans
- `Transaction` struct wrapping StateView + TxState + SchemaRegistry
- `Transaction.Insert` — provisional RowID, constraint check against both layers
- `Transaction.Delete` — tx-local removal or committed-row hide
- `Transaction.Update` — delete + insert with undelete optimization
- Insert-then-delete within same TX collapses to no-op
- Delete-committed-then-reinsert-identical collapses to undelete (cancel the delete)

**Testable outcomes:**
- Insert in TX, read via StateView — visible
- Delete committed row in TX, read — not found
- Delete committed row in TX, scan — excluded
- SeekIndex returns committed + tx-local results, excludes deleted
- Insert then delete same row in TX — not visible, no trace
- Delete committed row, reinsert identical — undelete (cancel the delete)
- Two transactions don't see each other's uncommitted state

**Dependencies:** Epic 2 (Table), Epic 3 (BTreeIndex), Epic 4 (Constraints)

---

## Epic 6: Commit, Rollback & Changeset

**Spec sections:** §5.6, §5.7, §6.1–§6.3

Commit algorithm. Net-effect changeset production.

**Scope:**
- `Changeset` struct: `TxID`, `Tables map[TableID]*TableChangeset`
- `TableChangeset` struct: `TableID`, `TableName`, `Inserts []ProductValue`, `Deletes []ProductValue`
- `Commit()` algorithm:
  1. Acquire write lock
  2. Re-validate remaining commit-time checks against current committed state
  3. Apply deletes to committed state + indexes (deletes before inserts)
  4. Apply tx-local inserts to committed state + indexes
  5. Build net-effect changeset
  6. Assign TxID, release lock
- `Rollback()` — discard TxState, O(1)
- Net-effect semantics:
  - Insert+delete in same TX → neither list
  - Delete+reinsert identical → neither list (undelete)
  - Pure insert → Inserts only
  - Pure delete → Deletes only
- Changeset is immutable after creation

**Testable outcomes:**
- Commit 100 inserts — changeset has 100 Inserts, 0 Deletes
- Delete 5 committed rows — changeset has 0 Inserts, 5 Deletes
- Insert+delete within TX — empty changeset
- Delete committed + reinsert identical — empty changeset
- Delete old + insert new — old in Deletes, new in Inserts
- After commit, committed state reflects all changes
- Rollback — committed state unchanged, scan returns pre-TX rows
- Commit is atomic: error → no state change

**Dependencies:** Epic 5 (Transaction layer)

---

## Epic 7: Read-Only Snapshots

**Spec sections:** §7.1, §7.2

Concurrent read access for subscription initial state delivery.

**Scope:**
- `CommittedReadView` interface: `Close()`, `TableScan()`, `IndexScan()`, `IndexRange()`, `RowCount()`
- `CommittedSnapshot` struct: shallow table map copy + held read lock
- `CommittedState.Snapshot()` — acquires RLock, returns CommittedSnapshot
- `Close()` — releases RLock
- Multiple concurrent snapshots allowed
- Commit blocks while any snapshot is open (write lock contention)
- Snapshot callers must materialize rows and close before network/blocking work

**Testable outcomes:**
- Snapshot sees committed state at point-in-time
- Commit after snapshot taken: snapshot still sees old state
- Hold snapshot, attempt commit in another goroutine — commit blocks until Close()
- Multiple snapshots coexist
- IndexScan and IndexRange return correct rows through snapshot
- RowCount matches actual row count

**Dependencies:** Epic 4 (Indexes), Epic 6 (CommittedState after commit)

---

## Epic 8: Auto-Increment & Recovery

**Spec sections:** §8, §5.8

Sequences, RowID/sequence export hooks, and changeset replay for crash recovery.

**Scope:**
- `Sequence` struct: `next uint64`, `sync.Mutex`
- On insert: if sequence column value is zero, replace with `Sequence.next++`
- Monotonic guarantee, persisted via snapshot
- `ApplyChangeset()` — replay a Changeset directly to CommittedState
  - No TxState, no constraint checks
  - Deletes by PK or row hash
  - Inserts with fresh RowID allocation
  - Constraint violation during recovery is fatal (corrupt log)
- Snapshot/export hooks for SPEC-002 recovery
  - expose per-table `nextID`
  - expose per-table sequence state
  - expose restore setters for both so snapshot restore can resume allocation correctly
- Sequence state restoration from snapshot

**Testable outcomes:**
- Insert with zero sequence column → auto-assigned, monotonically increasing
- Insert with non-zero sequence column → kept as-is
- ApplyChangeset with inserts — rows appear in committed state
- ApplyChangeset with deletes — rows removed from committed state
- ApplyChangeset with bad table ID → error
- ApplyChangeset with wrong row shape → error
- Sequence survives round-trip: snapshot → restore → next value continues
- RowID allocation survives round-trip: snapshot/export → restore → next RowID continues without collision

**Dependencies:** Epic 6 (Commit/Changeset), Epic 4 (Indexes for recovery)

---

## Dependency Graph

```
Epic 1: Core Value Types
  ├── Epic 2: Schema & Table Storage
  │     └── Epic 4: Table Indexes & Constraints ← Epic 3
  │           └── Epic 5: Transaction Layer
  │                 └── Epic 6: Commit, Rollback & Changeset
  │                       ├── Epic 7: Read-Only Snapshots
  │                       └── Epic 8: Auto-Increment & Recovery
  └── Epic 3: B-Tree Index Engine
```

## Error Types

Errors are introduced where first needed, not as a separate epic:

| Error | Introduced in |
|---|---|
| `ErrTableNotFound` | Epic 2 |
| `ErrColumnNotFound` | Epic 2 |
| `ErrTypeMismatch` | Epic 2 |
| `ErrRowShapeMismatch` | Epic 2 |
| `ErrPrimaryKeyViolation` | Epic 4 |
| `ErrUniqueConstraintViolation` | Epic 4 |
| `ErrDuplicateRow` | Epic 4 |
| `ErrRowNotFound` | Epic 5 |
| `ErrNullNotAllowed` | Epic 2 |
| `ErrInvalidFloat` | Epic 1 |
