# In-Memory Store — Deep Research Note

Research into SpacetimeDB's in-memory storage subsystem (`crates/table/`, `crates/datastore/`, `crates/sats/`). Extracts algorithms, data structures, and architectural patterns for spec derivation. No Rust code is carried into specs.

---

## 1. CRATE: `crates/table/` — Row Storage, Page Layout, Indexes

### 1.1 Page-Based Storage Architecture

The fundamental storage unit is a **64 KiB page** (`page.rs`, `PAGE_SIZE = 65,536`).

Each page has a 64-byte header followed by a data segment of 65,472 bytes. The data segment uses a **two-pointer allocation strategy**:

- **Fixed-length rows** grow left-to-right from offset 0, tracked by a high-water mark (`FixedHeader.last`)
- **Variable-length granules** grow right-to-left from the end, tracked by `VarHeader.first`
- A page is full when the two HWMs meet

Freed slots are managed via a **freelist** threaded through the page — both fixed slots and var-len granules have separate freelists. The bitmap `FixedHeader.present_rows` tracks which fixed-row slots are occupied.

**FixedHeader fields:** `next_free` (freelist head), `last` (HWM), `num_rows`, `present_rows` (bitmap)  
**VarHeader fields:** `next_free`, `freelist_len`, `first` (HWM), `num_granules`

The page header carries `unmodified_hash: Option<blake3::Hash>`. It is populated only for pages unchanged since the last snapshot; any mutation clears it back to `None`.

### 1.2 BFLATN — Binary Format Layout For Table Networks

BFLATN is the on-page binary encoding. Key properties:

1. **Fixed-size row region**: The fixed portion of a row is laid out contiguously using native-width integers with appropriate alignment. Primitive fields are stored directly.

2. **VarLenRef (4 bytes)**: Any variable-length field (string, array) is represented in the fixed region as a `VarLenRef`: a 2-byte length + a 2-byte offset to the first 64-byte granule in a linked list within the same page.

3. **VarLenGranule (64 bytes)**: Header is a 16-bit packed field: low 6 bits store payload length and high 10 bits store the next-granule pointer. The remaining 62 bytes hold payload data. Long objects chain across multiple granules.

4. **Large blob sentinel**: If an object exceeds 65,535 bytes, `length = u16::MAX` and the granule pointer field is repurposed to hold a hash referencing an external `BlobStore`.

5. **Static layout optimization**: For types whose fixed BSATN length is known at compile time, encoding is implemented as a series of `memcpy` calls rather than per-field dispatch.

**Distinction from BSATN**: BFLATN includes padding and indirection optimized for page storage. BSATN is the canonical wire format — no padding, compact, fully determinstic. Conversion between BFLATN↔BSATN for fixed-size rows is implemented as memcpy chains.

### 1.3 RowPointer — Row Addressing

A `RowPointer` is a packed 64-bit integer:

```
bits  0     (1)  — reserved for PointerMap collision detection
bits  1–39 (39)  — PageIndex (identifies which page)
bits 40–55 (16)  — PageOffset (byte offset within page)
bits 56–63  (8)  — SquashedOffset: TX_STATE=0, COMMITTED_STATE=1
```

The `SquashedOffset` field distinguishes whether a row lives in the current transaction's insert tables (`TX_STATE`) or in the committed store (`COMMITTED_STATE`). This is how the unified read interface distinguishes which storage layer to address.

### 1.4 Type Layout System

Each `AlgebraicType` is annotated with a `Layout { size: u16, align: u16, fixed: bool }`. The `ProductTypeLayout` records each field's byte offset within the product, enabling direct field access without full deserialization.

`AlgebraicTypeLayout` classifies types into four categories for dispatch:
- `Primitive` — fixed, atomically-sized (bool, integers, floats)
- `Product` — struct, fixed or var-len depending on fields
- `Sum` — discriminated union, fixed or var-len
- `VarLen` — string or array; always represented as VarLenRef (4 bytes) in the fixed region

### 1.5 B-Tree and Hash Indexes

Indexes are **specialized by key type** to eliminate enum dispatch on the hot path. The following index variants exist:

| Variant | Structure | Use case |
|---|---|---|
| `BTreeIndex<K>` | `BTreeMap<K, Vec<RowPointer>>` | Non-unique, range queries |
| `UniqueBTreeIndex<K>` | `BTreeMap<K, RowPointer>` | Unique, range queries |
| `HashIndex<K>` | `HashMap<K, Vec<RowPointer>>` | Non-unique, equality only |
| `UniqueHashIndex<K>` | `HashMap<K, RowPointer>` | Unique, equality only |
| `UniqueDirectIndex` | `Vec<RowPointer>` | Integer PK as direct array index |

Key types for B-tree specialization: all scalar types, `String`, `ProductValue`, `AlgebraicValue`.

Index operations: `insert(key, ptr)`, `delete(key, ptr)`, `seek_point(key)` (returns iterator), `seek_range(range)` (returns iterator). Uniqueness violations return `Err(existing_ptr)` from `insert`.

### 1.6 Pointer Map

When a table has no unique constraints, a `PointerMap` enforces **set semantics** (no duplicate rows):

- Maps `RowHash → RowPointer` (primary) with a collision chain (`Vec<RowPointer>`) for hash collisions
- Uses the reserved bit in `RowPointer` to distinguish a direct pointer from a collision-chain index
- Automatically removed when the first unique index is added (unique index alone is sufficient for duplicate prevention)

### 1.7 Insert and Delete Algorithms

**Insert (`write_row_to_pages`):**
1. Compute number of var-len granules needed (0 for all-fixed rows)
2. Find a page with sufficient space in the gap between fixed and var-len HWMs; allocate new page if none
3. Encode row to BFLATN: memcpy chain for fixed-layout types; per-field dispatch otherwise; var-len fields written as granule chains
4. Return `RowPointer`

**Delete (`delete_internal_skip_pointer_map`):**
1. Mark fixed row slot as absent in the `present_rows` bitmap
2. Return all var-len granules to the freelist
3. Decrement `num_rows`

**Scan:**
- Full scan: iterate pages, scan `present_rows` bitmap on each page, yield `RowPointer` per live row
- Index point: `seek_point(key)` on appropriate index
- Index range: `seek_range(start..end)` on B-tree index

---

## 2. CRATE: `crates/datastore/` — Transaction Isolation and Mutation Journaling

### 2.1 Two-Level Storage Model

SpacetimeDB separates storage into two layers:

**CommittedState** — the durable view of all committed data:
- Holds all tables, all indexes, blob store
- Protected by an `RwLock`
- Rebuilt from snapshots + log replay on startup

**TxState** — transaction-local mutations:
- Created fresh for each transaction
- Contains insert tables and delete sets
- Discarded (rollback) or merged (commit)

### 2.2 TxState Structure

```
TxState {
    insert_tables: map[TableId → Table]
        — Rows inserted during this TX.
        — Each is a fresh Table instance. RowPointers have SquashedOffset=TX_STATE.
    
    delete_tables: map[TableId → set[RowPointer]]
        — RowPointers from committed state scheduled for deletion.
        — RowPointers have SquashedOffset=COMMITTED_STATE.
    
    blob_store: HashMap
        — Blobs referenced by rows in insert_tables.
    
    pending_schema_changes: list[PendingSchemaChange]
        — Schema changes applied immediately to committed state during TX,
          with backup data for rollback.
}
```

Schema changes are applied eagerly to committed state so the rest of the system doesn't need to be transaction-aware. On rollback, backup values restore the previous state.

### 2.3 Unified Read Interface (StateView)

Reads during a transaction go through a `StateView` that unifies both layers:

- Row lookup: check insert_tables first (TX_STATE), then committed state (skipping rows in delete_tables)
- Table scan: yield rows from insert_tables, then committed rows not in delete_tables
- Index lookup: check TX insert indexes, then committed indexes (filter deletes)

This hides the TX/committed distinction from query execution.

### 2.4 Write Operations During Transaction

**Insert:** Write row to `insert_tables[table_id]`. Update insert_table indexes. Add blobs to TX blob store.

**Delete committed row:** Add RowPointer to `delete_tables[table_id]`. Remove from committed-side index views (lazily, on reads).

**Delete TX-inserted row:** Remove directly from `insert_tables[table_id]`. Remove from insert_table indexes.

**Update:** Delete old row (committed or TX-inserted) + insert new row into insert_tables.

### 2.5 Commit Algorithm

1. While holding the transaction's committed-state write lock, apply deletes first
2. For each table in `delete_tables`: delete those rows from committed pages; update committed indexes
3. For each table in `insert_tables`: write all rows into committed table pages; update committed indexes
4. Transfer blobs from TX blob store to committed blob store
5. Finalize pending schema changes (drop backup values)
6. Produce `TxData` changeset (see below)
7. Downgrade or release the lock

### 2.6 TxData — The Changeset

`TxData` is the net-effect changeset produced at commit:

```
TxData {
    entries: map[TableId → TxDataTableEntry]
    tx_offset: optional u64    — position in commit log
}

TxDataTableEntry {
    table_name: string
    inserts: []ProductValue    — rows whose net effect is "inserted"
    deletes: []ProductValue    — rows whose net effect is "deleted"
    truncated: bool
    ephemeral: bool
}
```

**Net-effect semantics**: If a row is inserted and then deleted within the same transaction, it appears in neither `inserts` nor `deletes`. If a committed row is deleted and the identical row is re-inserted in the same transaction, the delete is canceled (undelete/no-op) rather than producing a delete+insert pair. If a committed row is deleted and a different row is inserted, the old row appears in `deletes` and the new row appears in `inserts`. This net effect is what downstream consumers observe.

### 2.7 Rollback

1. Drop all `insert_tables` (GC handles cleanup)
2. Clear `delete_tables` (no committed state has changed)
3. For each `PendingSchemaChange`: restore stored backup values to committed state
4. Release or downgrade the committed-state write lock held by the mutable transaction

Rollback is very cheap for insert-only transactions: just drop TxState.

### 2.8 Isolation Level

This locking datastore implementation provides **serializable** isolation. Mutable transactions take the committed-state write lock up front, and read-only transactions hold a shared read lock. The code explicitly ignores a requested lower isolation level because this implementation always provides the strongest one.

---

## 3. CRATE: `crates/sats/` — Algebraic Type System

### 3.1 Type Taxonomy

**Primitive types**: bool, i8/u8, i16/u16, i32/u32, i64/u64, i128/u128, i256/u256, f32/f64

**Composite types**:
- `ProductType` — ordered list of named fields (struct/record). Structurally typed (identity is shape, not name).
- `SumType` — discriminated union with named variants. Each variant has an optional payload type.

**Variable-length types**: `String` (UTF-8), `Array` (homogeneous)

Map-like structures exist as a notation/convention built from other SATS types, but there is not a first-class `Map` runtime variant alongside `String` and `Array` in `AlgebraicType`.

**Type references**: `AlgebraicTypeRef` — an index into a `Typespace`, enabling recursive type definitions.

### 3.2 AlgebraicValue

`AlgebraicValue` is the runtime representation of any SATS value:

```
AlgebraicValue =
  | Bool(bool)
  | I8(i8) | U8(u8) | ... | U128(Packed<u128>) | I256(Box<i256>) | ...
  | F32(TotalOrderF32) | F64(TotalOrderF64)
  | String(str)
  | Array(ArrayValue)
  | Product(ProductValue)
  | Sum(SumValue)
  | Min | Max          — sentinels for range scan bounds
```

`ProductValue` holds `Box<[AlgebraicValue]>` — ordered field values.  
`SumValue` holds a `(tag: u8, data: AlgebraicValue)` pair.  
`ArrayValue` is packed by element type: `ArrayValue::U64(Vec<u64>)`, `ArrayValue::String(Vec<str>)`, etc.

Floats use a totally-ordered wrapper (`decorum::Total<f64>`) so they can be used as B-tree keys.

### 3.3 BSATN Wire Encoding

BSATN is the canonical binary encoding for SATS values, used for commit log entries, network messages, and snapshot data.

Properties:
- **Deterministic**: same value always encodes identically
- **Compact**: no padding; variants not padded to same size
- **Type-required for decode**: the decoder needs the AlgebraicType to interpret bytes

Encoding rules:
- Scalars: little-endian (native on x86)
- Strings: 4-byte length prefix (UTF-8 byte count) + UTF-8 bytes
- Arrays: 4-byte element count + packed elements
- Products: fields concatenated in declaration order, no padding
- Sums: 1-byte tag + variant data

### 3.4 Layout Computation

Every `AlgebraicType` computes a `Layout { size, align, fixed }`. For composite types:
- Products: max alignment of fields; fields placed at next aligned offset
- Sums: max alignment of variants; discriminant + largest variant size
- VarLen types: always 4 bytes (the VarLenRef representation)

`ProductTypeLayout` records each field's byte `offset` within the product. This enables direct field access during BFLATN read/write without deserializing the full row.

### 3.5 Typespace

`Typespace` is a flat `Vec<AlgebraicType>`. An `AlgebraicTypeRef` is an index into it. Most deserialization operations take a `(Typespace, AlgebraicType)` pair as context. This decouples type definitions from value serialization.

---

## Key Design Insights for Shunter

### 1. Page-based storage is SpacetimeDB-specific — Go needs a different approach

SpacetimeDB's page layout (64 KiB pages, BFLATN encoding, var-len granule chains) is optimized for Rust ownership semantics and zero-copy BFLATN↔BSATN conversion. Go's GC already handles memory allocation; mimicking pages adds complexity without the benefit. Shunter should use Go-natural row storage: a slice of structs (or encoded byte slices) per table, with a freelist for deleted slots.

### 2. The two-level TX model (TxState + CommittedState) is the right abstraction

The separation between in-transaction state and committed state is clean and important. In Shunter, this maps naturally: the transaction context holds its own insert buffer and delete set; commit merges them into the main store.

### 3. Net-effect changeset is the subscription evaluator's input

`TxData.inserts` and `TxData.deletes` are net-effect, not a raw operation log. This is what the subscription evaluator consumes. The store must produce this exactly. Insert-then-delete within one TX = nothing. Delete-then-reinsert = old row deleted, new row inserted.

### 4. Index specialization matters, but Go handles it differently

SpacetimeDB specializes index implementations per key type to avoid runtime dispatch. In Go, a `btree.BTreeG[K]` with typed comparators achieves similar performance. The Shunter spec should define a typed index interface and let the implementation choose specialization.

### 5. Pointer Map / set semantics

SpacetimeDB uses a PointerMap to prevent duplicate rows when no unique constraint exists. Shunter should adopt the same default: tables enforce set semantics (no exact duplicates) unless configured otherwise.

### 6. RowPointer's SquashedOffset is a clever trick, not required in Go

The SquashedOffset field (TX vs COMMITTED) is needed in SpacetimeDB because RowPointers refer to pages in different address spaces. In Go, because we hold Go pointers (not page offsets), the lookup layer can distinguish TX vs committed rows by consulting the transaction context without encoding it in the row address.
