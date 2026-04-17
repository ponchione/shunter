# Lane B Session 7 — SPEC-001 Residue Cleanup Plan

> **Scope:** Docs-only. `docs/decomposition/001-store/**` + `AUDIT_HANDOFF.md`. Live code (`store/`, `commitlog/`, `executor/`, `subscription/`, `protocol/`, `schema/`, `types/`, `bsatn/`) off-limits per Lane B §B.0. If a spec edit outruns live code → log Session 12+ drift in `TECH-DEBT.md` (TD-125/126/127 precedent).

**Goal:** Resolve every `open` SPEC-001 row in `AUDIT_HANDOFF.md §B.2` (CRIT/GAP/DIVERGE/NIT). Stop when all flipped to `closed` or `deferred — <reason>`.

**Stop rule:** no `open` SPEC-001 row remains; §B.5 cursor + intro block advanced to Session 8.

---

## Decisions pinned up front

- **§1.1 float ±0:** canonicalize `-0 → +0` before hashing (Option b from audit). Keep Equal/Compare as-is (already IEEE `==`). Reason: smallest delta — Story 1.2/1.3 are already consistent with IEEE; only Story 1.4 (hash) needs a canonicalization step.
- **§1.2 Bound:** promote `SeekBounds(low, high Bound)` into Story 3.3 as v1 deliverable (audit Option a). Keep half-open `SeekRange(*IndexKey)` as convenience. Add `SeekBounds` to SPEC-001 §4.6. CommittedReadView's `IndexRange(..., Bound, Bound)` calls into `SeekBounds`.
- **Divergence block location:** new `## 12. Divergences from SpacetimeDB`, push Open Questions → §13, Verification → §14. No external refs to §12/§13 exist; no external cross-ref updates needed (verified `rtk grep` above).
- **§4.3 ColID:** keep raw `int` in schema structs (§3.1) and explicitly call out §2.5 that `ColID` is the SPEC-004 predicate-side name for the same integer. Lightest edit; no churn in Story 2.1 / SPEC-006. 
- **§4.9 Snapshot placement:** move `CommittedState.Snapshot()` out of the "SPEC-003 (Transaction Executor)" subsection in §11 and add it to the "SPEC-002 (Commit Log)" + "SPEC-004 (Subscription Evaluator)" subsections. Do not create a new "shared concurrency primitives" section (out-of-scope churn).
- **Commit grouping:** see §Commits at end. One per CRIT row. GAPs bundled by theme (two bundles). Divergence block = one commit. NITs = one commit. Tracking-doc refresh = last.

---

## Task 1 — CRIT §1.1 float ±0 hash canonicalization

**Files:**
- `docs/decomposition/001-store/SPEC-001-store.md` §2.2 hashing-rules bullet
- `docs/decomposition/001-store/epic-1-core-value-types/story-1.4-value-hashing.md` Float32/Float64 encoding bullets + acceptance

**Edit 1 — SPEC-001 §2.2 "Hashing rules"** (around line 107–110):

Replace:
```
- Floats hash a canonical bit encoding of the exact stored value.
```
with:
```
- Floats hash a canonical bit encoding after canonicalizing `-0.0 → +0.0` so that the Equal→Hash contract holds for signed zero (IEEE `-0.0 == +0.0`, so hashes must collide).
```

**Edit 2 — Story 1.4 deliverable bullets:**

Replace:
```
- Float32: kind byte + 4-byte `math.Float32bits` encoding
- Float64: kind byte + 8-byte `math.Float64bits` encoding
```
with:
```
- Float32: kind byte + 4-byte `math.Float32bits` encoding of the value after canonicalizing `-0.0 → +0.0` (`if v == 0 { v = 0 }` before taking bits). Required because `Float32bits(-0.0) != Float32bits(+0.0)` but Story 1.2 Equal returns true for the pair.
- Float64: same canonicalization before `math.Float64bits`.
```

**Edit 3 — Story 1.4 acceptance:** append:
```
- [ ] `Float32(-0.0).Hash64() == Float32(+0.0).Hash64()`
- [ ] `Float64(-0.0).Hash64() == Float64(+0.0).Hash64()`
```

**Grep:** `rtk grep "Float.*bits\|±0\|canonicaliz" docs/decomposition/001-store/` — verify no lingering "Float64bits(v.f64)" without canonicalization in other stories.

**Commit:** `docs: Lane B SPEC-001 residue — §1.1 float ±0 hash canonicalization`

---

## Task 2 — CRIT §1.2 IndexRange Bound semantics

**Files:**
- `docs/decomposition/001-store/SPEC-001-store.md` §4.6 + §5.4 SeekIndexRange + §7.2 (already Bound, verify)
- `docs/decomposition/001-store/epic-3-btree-index-engine/story-3.3-range-scan.md` — add SeekBounds deliverable
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.3-state-view.md` — tie-in
- `docs/decomposition/001-store/epic-7-read-only-snapshots/story-7.1-committed-read-view.md` — already uses Bound; note SeekBounds as the primitive

**Edit 1 — Story 3.3 deliverables:** add after the existing `SeekRange` bullet:

```go
- `func (idx *BTreeIndex) SeekBounds(low, high Bound) iter.Seq[RowID]`
  - Bound-parameterized variant of SeekRange. `Unbounded=true` endpoint → no limit on that side.
  - `Inclusive=true` → closed endpoint (`<=` / `>=`); `Inclusive=false` → open (`<` / `>`).
  - Used by `CommittedReadView.IndexRange` (SPEC-001 §7.2) and the SPEC-004 predicate scan path where strict bounds on string/bytes/float keys cannot be expressed through half-open `*IndexKey`.
- `SeekRange(low, high *IndexKey)` retains half-open `[low, high)` semantics as the simpler convenience wrapper; equivalent to `SeekBounds(Bound{Value: *low, Inclusive: true}, Bound{Value: *high, Inclusive: false})` with `nil` → `Bound{Unbounded: true}`.
```

Append acceptance criteria:
```
- [ ] SeekBounds with `Bound{Value: v, Inclusive: false}` on both sides yields keys in `(low, high)`
- [ ] SeekBounds with one `Bound{Unbounded: true}` endpoint yields unbounded-on-that-side
- [ ] SeekBounds and SeekRange produce identical results for the half-open case
```

Remove the "If Bound-based semantics (inclusive/exclusive per endpoint) are needed later, add a `SeekBounds` variant in a future story" line from Design Notes (now delivered in v1).

**Edit 2 — SPEC-001 §4.6:** add the new signature below the existing three:

```go
// SeekBounds returns all RowIDs with key in the range specified by Bound semantics (§4.4).
// Used by CommittedReadView.IndexRange (§7.2) and SPEC-004 predicate scans that need
// exclusive endpoints on non-integer keys.
func (idx *Index) SeekBounds(low, high Bound) iter.Seq[RowID]
```

**Edit 3 — SPEC-001 §5.4 SeekIndexRange block:** append one sentence after the existing description:

```
For in-transaction predicate paths that require exclusive endpoints on string/bytes/float keys (SPEC-004 §2.6), StateView exposes a Bound-parameterized variant `SeekIndexBounds(tableID, indexID, low, high Bound)` that delegates to the committed index's `SeekBounds` and filters tx-local inserts by Bound comparison.
```

Also add `SeekIndexBounds` as a new method on `StateView`:

```go
// SeekIndexBounds performs a range scan with Bound endpoints (SPEC-001 §4.4).
// Required for SPEC-004 predicate scans that need exclusive endpoints.
func (sv *StateView) SeekIndexBounds(tableID TableID, indexID IndexID, low, high Bound) iter.Seq[RowID]
```

**Edit 4 — Story 5.3 deliverables:** mirror the new signature (brief entry + acceptance).

**Edit 5 — Story 7.1:** already uses Bound; in Design Notes add: "IndexRange implementation calls `Index.SeekBounds(low, high)` (Story 3.3 v1 deliverable)." Also cross-ref §2.6 resolution.

**Grep:** `rtk grep -n "SeekRange\|SeekBounds\|IndexRange\|SeekIndexRange\|SeekIndexBounds" docs/decomposition/001-store/`. Confirm:
- BTreeIndex has SeekRange + SeekBounds
- StateView has SeekIndexRange + SeekIndexBounds
- CommittedReadView has IndexRange (Bound)

**Commit:** `docs: Lane B SPEC-001 residue — §1.2/§2.6 Bound-parameterized range scans`

---

## Task 3 — CRIT §1.4 undelete full-row equality

**Files:**
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.4-transaction-insert.md` step 3

**Edit — Story 5.4 step 3 algorithm:**

Replace:
```
3. **Undelete check** (set-semantics and PK tables):
   - If an identical committed row exists in `tx.deletes`, cancel the delete and return the committed RowID
   - For PK tables: match by PK value
   - For no-PK tables: match by full row equality
```
with:
```
3. **Undelete check** (set-semantics and PK tables):
   - For PK tables: locate the candidate committed row via PK value, then require **full-row equality** (`ProductValue.Equal`) to trigger undelete. PK-match-without-row-equality is NOT an undelete — the delete stays in `tx.deletes` and the insert proceeds as a new tx-local row (old row lands in changeset Deletes, new row in Inserts).
   - For no-PK tables: match by full row equality directly against candidates in `tx.deletes`.
   - On full-row-equal match: cancel that delete (remove the committed RowID from `tx.deletes[tableID]`) and return the committed RowID. No new tx-local row created.
```

Append acceptance criterion:
```
- [ ] Insert with matching PK but different non-PK columns on a tx-deleted committed row → committed row stays in tx.deletes; new row added to tx.inserts; commit emits both delete and insert
```

**Grep:** `rtk grep -n "undelete\|match by PK" docs/decomposition/001-store/` — confirm no residue of "PK match alone ⇒ undelete".

**Commit:** `docs: Lane B SPEC-001 residue — §1.4 undelete requires full-row equality`

---

## Task 4 — CRIT §1.5 AsBytes alias contract

**Files:**
- `docs/decomposition/001-store/epic-1-core-value-types/story-1.1-valuekind-value-struct.md`

**Edit — Story 1.1 Accessor bullet:**

Replace:
```
- Accessor per kind:
  `AsBool() bool`, `AsInt8() int8`, etc.
  - Panic on kind mismatch (caller bug, not user error)
```
with:
```
- Accessor per kind:
  `AsBool() bool`, `AsInt8() int8`, etc.
  - Panic on kind mismatch (caller bug, not user error)
  - `AsBytes() []byte` returns a slice aliasing the Value's internal `buf`. Callers MUST NOT mutate the returned slice. The immutability invariant in SPEC-001 §2.2 relies on the Value being constructed via `NewBytes` (which copies input) and never handed a mutable view afterwards. If a caller needs a mutable copy, use `append([]byte(nil), v.AsBytes()...)`.
  - `AsString() string` returns the stored string (Go strings are already immutable; no aliasing concern).
```

Append acceptance criterion:
```
- [ ] `AsBytes` returns a non-nil slice whose length and content match the NewBytes input; documented as read-only.
```

**Commit:** `docs: Lane B SPEC-001 residue — §1.5 AsBytes alias contract`

---

## Task 5 — GAP bundle A: error-catalog production sites (§2.2, §2.4, §2.8)

Single commit covers three related "error declared but no producer named" findings.

**Files:**
- `docs/decomposition/001-store/SPEC-001-store.md` §9 (add ErrRowShapeMismatch row)
- `docs/decomposition/001-store/epic-1-core-value-types/story-1.1-valuekind-value-struct.md` (name ErrInvalidFloat)
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.4-transaction-insert.md` + `story-5.5-transaction-delete.md` + `story-5.6-transaction-update.md` (name ErrTableNotFound at step 0)
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.3-state-view.md` (document unknown-TableID → empty iterator)

**Edit 1 — SPEC-001 §9 catalog** — add row between `ErrTypeMismatch` and `ErrPrimaryKeyViolation`:

```
| `ErrRowShapeMismatch` | row column count or ValueKind list does not match TableSchema.Columns; raised by Story 2.3 ValidateRow |
```

**Edit 2 — Story 1.1** — replace:
```
- `NewFloat32` / `NewFloat64` — reject NaN, return `(Value, error)`
```
with:
```
- `NewFloat32` / `NewFloat64` — reject NaN, return `(Value, error)`. The error is `ErrInvalidFloat` (SPEC-001 §9). Bound to this constructor; no other call site produces it.
```

Append acceptance:
```
- [ ] `NewFloat32(NaN)` returns `ErrInvalidFloat`
- [ ] `NewFloat64(NaN)` returns `ErrInvalidFloat`
```

**Edit 3 — Story 5.4 algorithm** — prepend step 0:
```
0. Look up table by TableID via `committed.Table(tableID)`. If not found, return `(0, ErrTableNotFound)`.
```

Mirror in Story 5.5 (Delete) + Story 5.6 (Update). Each story gets a step 0 naming `ErrTableNotFound`.

**Edit 4 — Story 5.3 StateView design note** — append:
```
- Unknown `TableID` passed to `GetRow`, `ScanTable`, `SeekIndex`, `SeekIndexRange`, or `SeekIndexBounds` yields an empty result (no error, no panic). Error-returning shape is reserved for the mutation-side `Transaction.Insert/Delete/Update` boundary (§9 `ErrTableNotFound`).
```

Append acceptance:
```
- [ ] Unknown TableID: ScanTable returns empty iterator; GetRow returns (nil, false); SeekIndex returns empty iterator
```

**Grep:** `rtk grep -n "ErrTableNotFound\|ErrInvalidFloat\|ErrRowShapeMismatch" docs/decomposition/001-store/`. Confirm each sentinel has at least one named producer.

**Commit:** `docs: Lane B SPEC-001 residue — §2.2/§2.4/§2.8 error catalog production sites`

---

## Task 6 — GAP §2.5 snapshot close enforcement

**Files:**
- `docs/decomposition/001-store/epic-7-read-only-snapshots/story-7.1-committed-read-view.md`
- `docs/decomposition/001-store/SPEC-001-store.md` §7.2 CommittedSnapshot struct

**Edit 1 — SPEC-001 §7.2 CommittedSnapshot struct:**

Replace:
```go
type CommittedSnapshot struct {
    tables map[TableID]*Table    // shallow copy of table map at snapshot time
    mu     *sync.RWMutex         // held as read lock until Close()
}
```
with:
```go
type CommittedSnapshot struct {
    tables map[TableID]*Table    // shallow copy of table map at snapshot time
    mu     *sync.RWMutex         // held as read lock until Close()
    closed atomic.Bool           // true after Close(); subsequent method calls panic
}
```

Append one sentence below the struct: `All read methods check \`closed\` on entry and panic with a "use after Close" message if set. This is a correctness invariant, not a defensive check — callers that reach for a closed snapshot have a lifecycle bug.`

**Edit 2 — Story 7.1 deliverables:** extend the `CommittedSnapshot` bullet:

```go
type CommittedSnapshot struct {
    tables map[TableID]*Table   // shallow copy of table map at snapshot time
    mu     *sync.RWMutex        // held as read lock until Close()
    closed atomic.Bool          // set on Close(); post-close calls panic
}
```

Add deliverable bullet:
```
- Every read method (`TableScan`, `IndexScan`, `IndexRange`, `RowCount`) checks `closed.Load()` at entry and panics with "snapshot used after Close" if set.
- `Close()` sets `closed.Store(true)` and releases RLock. Calling Close() a second time panics (exactly-once contract).
```

**Commit:** `docs: Lane B SPEC-001 residue — §2.5 snapshot close enforcement`

---

## Task 7 — GAP §2.7 ApplyChangeset non-idempotent

**Files:**
- `docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.2-apply-changeset.md`
- `docs/decomposition/001-store/SPEC-001-store.md` §5.8

**Edit 1 — Story 8.2 Design Notes** — append:
```
- `ApplyChangeset` is NOT idempotent. Replaying the same changeset twice will cause uniqueness-constraint violations (the second delete fails because the row is already gone; the second insert fails because it collides with the first). It is SPEC-002's recovery path responsibility to replay each committed changeset exactly once — the "replay the log" acceptance criterion assumes no overlap. A boundary bug that replays a segment twice is itself a fatal corrupt-log condition.
```

**Edit 2 — SPEC-001 §5.8 ApplyChangeset block** — append one sentence:
```
`ApplyChangeset` is not idempotent; SPEC-002's recovery path must replay each committed changeset exactly once (see SPEC-002 §6.x recovery contract).
```

**Commit:** `docs: Lane B SPEC-001 residue — §2.7 ApplyChangeset non-idempotent`

---

## Task 8 — GAP §2.1 sequence replay advance

**Files:**
- `docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.2-apply-changeset.md`

**Edit — Story 8.2 algorithm step 2c (insert branch):** append sub-step:

```
- If the row contains a value in an auto-increment column (SPEC-006 §9 / Story 8.1): advance the table's `Sequence.next` to `max(Sequence.next, observed+1)`. This prevents post-recovery auto-assign from reissuing a sequence value already persisted in a replayed insert (see audit §2.1).
```

Append acceptance:
```
- [ ] Replay insert with sequence-column value N; post-replay next auto-assign yields ≥ N+1
- [ ] Replay inserts with sequence values N, N-3, N-7 (out of order); post-replay next yields N+1
- [ ] Snapshot-restored `Sequence.next = N-5`, then replay insert with sequence-column value N; post-replay next yields N+1 (not N-4)
```

Design-note addendum:
```
- The alternative model (SpacetimeDB's `allocated` upper bound stored alongside `next`) is deferred to v2. The v1 approach leans on replay to advance the counter, which requires exactly-once replay (see §2.7).
```

**Commit:** `docs: Lane B SPEC-001 residue — §2.1 sequence replay advances next`

---

## Task 9 — GAP bundle B: concurrency + bytes-copy + state-export (§2.9, §5.2, §5.3, §5.4)

Single commit — all four bind contract-level safety at the same boundaries.

**Files:**
- `docs/decomposition/001-store/SPEC-001-store.md` §5.6 (Commit) + §6.3 (Consumers)
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.1-changeset-types.md`
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.2-commit.md`
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.4-transaction-insert.md`
- `docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.3-state-export.md`

**Edit 1 — §5.6 Commit** — after the "Required invariant: Commit is atomic..." paragraph, add:
```
**Post-return safety:** the returned `*Changeset` is safe to use after `Commit` returns and the write lock has been released. Its `ProductValue` entries are either freshly allocated copies (deleted rows materialized from committed state before removal) or rows inserted at commit time whose underlying pointers are now stable in committed state. SPEC-002 (commit log) and SPEC-004 (subscription evaluator) may consume the same `*Changeset` concurrently on separate goroutines; neither is permitted to mutate any `Value.buf`. See §6.3.
```

**Edit 2 — §6.3 Consumers** — replace:
```
Both receive the same `Changeset` value. It is read-only after creation.
```
with:
```
Both receive the same `Changeset` value. It is read-only after creation. **Concurrency contract:** consumers may read the `Changeset` concurrently from separate goroutines; no consumer may mutate any field, including `Value.buf` on any `ProductValue` entry. `ProductValue` rows in `Inserts` may alias committed-state backing memory; rows in `Deletes` are freshly allocated copies taken at delete time. Consumer mutation is a correctness bug, not merely a performance concern.
```

**Edit 3 — Story 6.1** — append to Design Notes:
```
- Concurrency: the Changeset value is handed to SPEC-002 and SPEC-004 simultaneously via the post-commit pipeline (SPEC-003 §5). Both consumers read-only. The Value API's unexported `buf` field prevents accidental mutation; callers that bypass the API (e.g., unsafe pointer tricks) are out of contract.
```

**Edit 4 — Story 6.2** — append to Design Notes:
```
- After the write lock releases, the returned `*Changeset` is safe to share with post-commit consumers. Deleted-row `ProductValue` entries are copied out of committed state before the delete removes them, so they remain valid after the lock release.
```

**Edit 5 — Story 5.4 algorithm** — extend step 7 ("Store in tx.inserts"):
```
7. Store in tx.inserts. The provided `ProductValue` must either (a) have been constructed through `NewBytes` for all Bytes columns (which copies input — see Story 1.1), or (b) be sourced from a code path the caller can prove has exclusive ownership of any Bytes backing memory. The store does not re-copy at the Insert boundary; the Value API's unexported `buf` field is the single copy point. BSATN decode paths (SPEC-002 replay, SPEC-005 reducer argument decode) MUST route Bytes columns through `NewBytes` to enter the store safely.
```

Append to Design Notes:
```
- Bytes ownership: the contract in SPEC-001 §2.2 ("store must copy caller-provided byte slices on insert unless it can prove exclusive ownership") is implemented by funneling all Value construction through `NewBytes` at serialization boundaries. Insert itself does not copy, because the Value struct's unexported `buf` prevents a caller from constructing a mutable-aliasing Value without going through the constructor.
```

**Edit 6 — Story 8.3** — fix the SetSequenceValue asymmetry:

Replace:
```
- `func (t *Table) SetSequenceValue(val uint64)` — restore sequence counter
```
with:
```
- `func (t *Table) SetSequenceValue(val uint64)` — restore sequence counter. Sets the counter to `max(current, val)`, matching `SetNextID` semantics. Rationale: if replay has already advanced the sequence past the snapshot-stored value (audit §2.1), the higher value wins.
```

Append acceptance:
```
- [ ] SetSequenceValue with val < current → counter unchanged
- [ ] SetSequenceValue with val > current → counter set to val
- [ ] Round-trip snapshot-restore → replay → SetSequenceValue → counter reflects max of snapshot value and replay-advanced value
```

Design-note update — replace the "SetNextID must set the counter to at least the provided value..." paragraph with:
```
- `SetNextID` and `SetSequenceValue` both take `max(current, provided)`. Symmetric by design: if ApplyChangeset has already advanced the counter during replay, the restore setter must not rewind it.
```

**Grep:** `rtk grep -n "SetNextID\|SetSequenceValue\|Changeset.*read-only\|post-return" docs/decomposition/001-store/` — confirm matches the new wording.

**Commit:** `docs: Lane B SPEC-001 residue — §2.9/§5.2/§5.3/§5.4 concurrency + bytes copy + export symmetry`

---

## Task 10 — DIVERGE §3.1–§3.6 divergence block

**Files:**
- `docs/decomposition/001-store/SPEC-001-store.md` — insert new §12, renumber Open Questions → §13, Verification → §14

**Edit 1 — insert new section** between current §11 (Interfaces) and current §12 (Open Questions):

```markdown
## 12. Divergences from SpacetimeDB

Shunter's clean-room spec intentionally departs from SpacetimeDB in several places. Each divergence below is grounded in `reference/SpacetimeDB/` behavior but is an explicit v1 choice. Future specs or implementations should not "add parity" without revisiting the tradeoff documented here.

### 12.1 NaN rejected at construction vs total-ordering via `decorum::Total`

SpacetimeDB admits NaN via a total-ordering wrapper (`decorum::Total<f32>` / `decorum::Total<f64>`), assigning it a fixed ordinal position so `AlgebraicValue` derives `Eq`/`Ord`/`Hash` uniformly. Shunter v1 rejects NaN at `NewFloat32` / `NewFloat64` (§2.1 + Story 1.1), returning `ErrInvalidFloat`.

Rationale: v1 wants bit-by-bit determinism without importing a decorum-style wrapper. Legitimate NaN-producing payloads (sensor telemetry, ML outputs) must be sanitized at the boundary. Revisit if workloads demand stored NaN.

### 12.2 No composite types; RowID is stable; rows stored decoded

Shunter v1 supports only flat scalar + String + Bytes columns (§2.1). No `Sum` (tagged unions / `Option`-like Nullability), no `Array`, no nested `Product`. Rows are stored decoded as `ProductValue` in `map[RowID]ProductValue` (§3.2); `RowID` is a per-table `uint64` that is never reused within a process lifetime (§2.3).

SpacetimeDB supports `Sum`, `Array`, and `Product` nesting, stores rows as BFLATN-packed pages with a content-addressed blob store for large var-len data, and reuses row pointers across delete/insert cycles (page+offset volatility).

Rationale: the subscription evaluator needs fast predicate evaluation against decoded row values, and v1 targets in-memory workloads where RAM overhead of decoded rows is acceptable. A future schema layer adding `Option<T>` must revisit `Nullable`, `ValueKind`, and `ProductValue` simultaneously.

### 12.3 `rowHashIndex` scoped to "no PK" vs SpacetimeDB "no unique index"

Shunter's `rowHashIndex` (§3.3) is created only when no primary key exists. A table with a unique-but-not-primary index pays for both the unique-index lookup and a row-hash lookup on every insert. SpacetimeDB maintains its equivalent (`pointer_map`) iff no unique index of any kind exists.

Rationale: the `rowHashIndex` condition in v1 is stated in terms of the primary key because the builder API (SPEC-006) makes primary-key presence the primary signal. Strictly redundant but not incorrect. A perf pass may tighten the condition to "no unique index of any kind" without a spec edit; flagged here so the tightening is deliberate.

### 12.4 Multi-column primary key allowed

`IndexSchema.Columns []int` with `Primary bool` (§3.1) permits any number of columns in a primary key. SpacetimeDB's `TableSchema.primary_key` is `Option<ColId>` — explicitly single-column.

Rationale: v1 supports composite PKs because the subscription predicate layer (SPEC-004) already assumes compound-key equality is expressible; restricting the store to single-column PKs would force awkward synthetic-ID workarounds for natural composite keys (e.g., `(tenant_id, entity_id)`).

### 12.5 Replay constraint violations are fatal vs SpacetimeDB silent skip

`ApplyChangeset` treats any constraint violation during recovery as fatal (§5.8 + Story 8.2). SpacetimeDB's `replay_insert` silently ignores duplicates for system-meta rows and is generally tolerant during replay.

Rationale: fail-fast during recovery surfaces corrupt-log / schema-mismatch conditions immediately rather than masking them. The cost is that idempotent re-replay after a crash-during-replay will abort; paired with §2.7 (ApplyChangeset is not idempotent) and SPEC-002's exactly-once replay guarantee, this is the intended shape.

### 12.6 `Changeset` has no `truncated` / `ephemeral` / `tx_offset` flags

Shunter's `Changeset` carries `{TxID, Tables}` only (§6.1). SpacetimeDB's `TxData` additionally carries `truncated: bool` (whole-table clear), `ephemeral: bool` (view-only table, skip durability), and `tx_offset: Option<u64>` (commitlog cursor).

Rationale: v1 has no `TRUNCATE` reducer (truncation decomposes to per-row deletes), no ephemeral tables (all tables are durable), and the commitlog owns its own cursor bookkeeping (SPEC-002 — `tx_offset` is not part of the store↔evaluator contract). Revisit if SPEC-004 grows ephemeral subscription-only tables or SPEC-002 exposes a TRUNCATE fast-path.
```

**Edit 2 — renumber:**
- `## 12. Open Questions` → `## 13. Open Questions`
- `## 13. Verification` → `## 14. Verification`

**Grep:** `rtk grep -n "^## \|§12\|§13\|§14" docs/decomposition/001-store/SPEC-001-store.md`. Confirm section numbers unique + no internal stale refs.

**Commit:** `docs: Lane B SPEC-001 residue — §3.1–§3.6 divergence block`

---

## Task 11 — NIT bundle (§4.3, §4.4, §4.5, §4.7, §4.8, §4.9)

Single commit — six small-scope edits.

**Files:**
- `docs/decomposition/001-store/SPEC-001-store.md` §2.5 (ColID note), §4.2 (IndexID 0 rule), §10 (rename), §11 (Snapshot placement)
- `docs/decomposition/001-store/epic-1-core-value-types/story-1.1-valuekind-value-struct.md` (Invalid zero)
- `docs/decomposition/001-store/epic-2-schema-table-storage/story-2.1-schema-structs.md` (IndexID 0 rule cross-ref)
- `docs/decomposition/001-store/epic-7-read-only-snapshots/EPIC.md` (blocks text)

**Edit 1 — §2.5 ColID** — append sentence:
```
The schema structs in §3.1 use raw `int` for column indices to match idiomatic Go slice indexing. `ColID` is the typed alias used by SPEC-004 predicate types where column-identity is load-bearing in signatures; the two are the same integer value. Do not re-type `ColumnSchema.Index` or `IndexSchema.Columns[]` to `ColID` — that churn is out of scope for v1.
```

**Edit 2 — §4.2 IndexID rules** — replace:
```
// IndexID 0 is always the primary index if one exists; subsequent IDs are assigned in
// declaration order.
```
with:
```
// IndexID 0 is reserved for the primary index. On tables with no primary key, IndexID 0
// is unused; the first declared secondary index receives IndexID 1, and subsequent
// secondary indexes get IDs in declaration order starting from 1. This keeps
// "IndexID == 0 ⇒ primary or absent" a stable invariant across all tables.
```

**Edit 3 — Story 2.1** — match §4.2 language (one-sentence update).

**Edit 4 — §10 header** — rename `## 10. Performance Constraints` → `## 10. Performance Targets` (one-word change, matches §13 OQ framing).

**Edit 5 — §11 Snapshot relocation:**

Remove from the SPEC-003 export list:
```go
func (cs *CommittedState) Snapshot() CommittedReadView
```

And from the exported-functions block under SPEC-003, keep only Commit/Rollback/NewTransaction.

Add `Snapshot()` to both the SPEC-002 and SPEC-004 subsections' exported-function enumeration (each subsection already references Snapshot behaviorally; make the export explicit). Concretely — under SPEC-002 block, after the existing bullet list, add:
```
Exported by store (concurrency primitive; used by SPEC-002 snapshot creation):

    func (cs *CommittedState) Snapshot() CommittedReadView
```

Under SPEC-004 block, add the same (marked "used by SPEC-004 initial-state delivery").

**Edit 6 — Story 1.1** — add Invalid zero:

Under Deliverables → `ValueKind`, replace:
```
- `ValueKind` — integer enum with 13 variants:
  `Bool, Int8, Uint8, Int16, Uint16, Int32, Uint32, Int64, Uint64, Float32, Float64, String, Bytes`
```
with:
```
- `ValueKind` — integer enum with 14 variants:
  `Invalid (= 0), Bool, Int8, Uint8, Int16, Uint16, Int32, Uint32, Int64, Uint64, Float32, Float64, String, Bytes`
  - `ValueKind(0) = Invalid`. A zero-initialized `Value` (`var v Value`) has `kind = Invalid`; Equal/Compare/Hash/As* all treat Invalid as "not a valid stored value" and panic on access. Valid variants start at 1 so the zero-value Go struct is unambiguously not a stored value.
```

Update Design Notes: replace the "zero-initialized Go struct ... not part of the store contract" paragraph with:
```
- The zero-initialized Go struct for `Value` has `kind = Invalid (= 0)`. Equal / Compare / Hash / As* on an Invalid-kind Value panic, making accidental use of `Value{}` a loud error rather than a silent "bool false".
```

Append acceptance:
```
- [ ] `var v Value` then `v.Kind() == Invalid`
- [ ] Accessor or Equal / Compare / Hash on Invalid-kind Value panics
```

**Edit 7 — EPIC 7 blocks** — replace:
```
**Blocks:** Nothing (consumed by SPEC-004 subscription evaluator)
```
with:
```
**Blocks:** Nothing. Consumed by SPEC-003 (post-commit snapshot acquisition for subscription fan-out), SPEC-004 (subscription initial-state delivery), and SPEC-005 (one-off queries).
```

**Commit:** `docs: Lane B SPEC-001 residue — §4.3/§4.4/§4.5/§4.7/§4.8/§4.9 NIT bundle`

---

## Task 12 — Tracking-doc refresh (AUDIT_HANDOFF.md)

**Files:**
- `AUDIT_HANDOFF.md`

**Edits:**

1. **Top-of-file intro block** (lines 4–5) — advance Lane B cursor from "Session 7 (SPEC-001 residue cleanup)" to "Session 8 (SPEC-002 residue cleanup)".

2. **§B.2 SPEC-001 table** — flip every `open` row to `closed` (no deferrals expected; if any unexpected one surfaces during execution, mark `deferred — <reason>`). Rows: §1.1, §1.2, §1.4, §1.5, §2.1, §2.2, §2.4, §2.5, §2.6, §2.7, §2.8, §2.9, §3.1–§3.6, §4.3, §4.4, §4.5, §4.7, §4.8, §4.9, §5.2, §5.3, §5.4.

3. **§B.3 Session 7 row** — update stop-rule cell:
```
**(closed)** All 23 open SPEC-001 rows resolved. CRIT fixes: float ±0 hash canonicalization (Story 1.4), Bound-parameterized SeekBounds (Story 3.3 / §4.6), undelete full-row equality (Story 5.4), AsBytes alias contract (Story 1.1). GAPs: ErrTableNotFound/ErrInvalidFloat/ErrRowShapeMismatch producers named; snapshot close enforcement; ApplyChangeset non-idempotent + sequence-advance-on-replay; Changeset post-return concurrency contract; Bytes copy boundary; SetSequenceValue max-rule. New §12 Divergences block (6 entries). NIT bundle: ColID rationale, §10 rename, ValueKind Invalid zero, IndexID 0 reservation, Epic 7 blocks, §11 Snapshot placement.
```

4. **§B.5 footer Cursor:** advance from Session 7 to Session 8.

**Commit:** `docs: close Lane B SPEC-001 residue — Session 7 tracking update`

---

## Task 13 — Verification pass

1. `rtk git status` — clean working tree (plan file is untracked and stays untracked).
2. `rtk grep -n "open " AUDIT_HANDOFF.md` — no open SPEC-001 rows remain in §B.2 table.
3. `rtk git log --oneline -15` — confirm ~9 new commits landed:
   - §1.1 float
   - §1.2 Bound
   - §1.4 undelete
   - §1.5 AsBytes
   - §2.2/§2.4/§2.8 error producers
   - §2.5 snapshot close
   - §2.7 ApplyChangeset
   - §2.1 sequence replay
   - §2.9/§5.2/§5.3/§5.4 concurrency bundle
   - §3.1–§3.6 divergence block
   - NIT bundle
   - tracking-doc refresh
4. `rtk grep -n "^## " docs/decomposition/001-store/SPEC-001-store.md` — confirm §12 Divergences, §13 Open Questions, §14 Verification exist with no duplicates.
5. `rtk grep -n "Session 7\|Session 8" AUDIT_HANDOFF.md` — cursor advanced.

---

## Drift log (if edits outrun live code)

Possible drift candidates if the specs cross into aspirational territory:

- **Potential TD-128:** Story 3.3 SeekBounds as v1 deliverable. No live `store/` exists yet (repo is docs-first); when Phase 3 implementation lands, confirm SeekBounds is implemented rather than deferred.
- **Potential TD-129:** Story 1.1 ValueKind(0)=Invalid. No live impl; clean.
- **Potential TD-130:** Story 8.2 sequence-advance-on-replay. No live impl; clean.

No live code exists in the affected directories (per `rtk ls store/` which is empty). So no drift entries needed this session. Confirm at verification step.

---

## Self-review

- Every `open` SPEC-001 row in §B.2 has a task above: CRIT (1.1/1.2/1.4/1.5), GAP (2.1/2.2/2.4/2.5/2.6/2.7/2.8/2.9/5.2/5.3/5.4), DIVERGE (3.1/3.2/3.3/3.4/3.5/3.6), NIT (4.3/4.4/4.5/4.7/4.8/4.9). Count: 23 rows, all mapped.
- Commit groupings match natural seams per AUDIT_HANDOFF §B.0 ("one commit per resolved finding / coherent bundle; small reviewable chunks; not a session-sized mega-commit").
- No live-code directory touched (`store/`, etc.). All edits land in `docs/decomposition/001-store/**` or `AUDIT_HANDOFF.md`.
- Renumbering §12→§13 / §13→§14 has no external SPEC consumers (`rtk grep` over `docs/decomposition/` confirmed); safe.
- Section numbers, story IDs, and SPEC cross-refs used above all correspond to content I read in preparation (SPEC-001 full text + 8 key stories + EPICS.md).
