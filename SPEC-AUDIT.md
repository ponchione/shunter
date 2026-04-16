# Shunter Spec Audit

Running audit of Shunter's clean-room decomposition docs against SpacetimeDB behavior in `reference/SpacetimeDB/`. Findings are the source of truth for doc repair work; each item cites the offending doc and, where relevant, the SpacetimeDB path that was used to ground the call.

## Severity key

- **[CRITICAL]** — spec is incorrect, internally contradictory, or breaks an invariant an implementer would rely on.
- **[GAP]** — spec is silent on behavior a reasonable implementer must decide; divergence from reference that is not called out.
- **[DIVERGE]** — intentional simplification relative to SpacetimeDB; should be explicit in the spec so later phases do not try to "add parity" by accident.
- **[NIT]** — naming, cross-refs, redundant wording; low priority.
- **[OK]** — verified accurate or intentionally scoped out; listed when the check is non-obvious.

---

# SPEC-001 — In-Memory Relational Store

Audited files:

- `docs/decomposition/001-store/SPEC-001-store.md`
- `docs/decomposition/001-store/EPICS.md`
- `docs/decomposition/001-store/epic-{1..8}/EPIC.md`
- All `story-*.md` under those epics

SpacetimeDB reference: `reference/SpacetimeDB/crates/{sats,table,schema,datastore,snapshot,primitives}`.

---

## 1. Critical

### 1.1 [CRITICAL] Value equality / hash invariant broken for float ±0

- `SPEC-001-store.md` §2.2 equality: "Floats compare by numeric value".
- `epic-1-core-value-types/story-1.2-value-equality.md` deliverable: `v.f64 == other.f64` — in Go `-0.0 == 0.0` is `true`.
- `epic-1-core-value-types/story-1.3-value-ordering.md` acceptance: `Float64(-0.0).Compare(Float64(0.0)) == 0`.
- `epic-1-core-value-types/story-1.4-value-hashing.md` deliverable: `Float64: kind byte + 8-byte math.Float64bits encoding`. `Float64bits(-0.0) = 0x8000000000000000`, `Float64bits(0.0) = 0`.

Consequence: `a.Equal(b) == true` but `a.Hash() != b.Hash()` for `a = -0.0`, `b = +0.0`. This breaks the universal Equal→Hash contract the set-semantics `rowHashIndex` depends on (Story 4.4 uses `ProductValue.Hash64()` as primary bucket key, then `ProductValue.Equal` for dedupe). A duplicate row that differs only by ±0 on a column will land in a different bucket and will not be detected as a duplicate.

Fix options: (a) canonicalize `-0.0 → +0.0` in `NewFloat32`/`NewFloat64`; (b) hash `+0` for any `±0`; (c) change Compare to order `-0 < +0` AND change Equal to return `false` (matches SpacetimeDB's `decorum::Total` — see `reference/SpacetimeDB/crates/sats/src/algebraic_value.rs:13-16`). Pick one and apply consistently in Stories 1.2/1.3/1.4.

### 1.2 [CRITICAL] CommittedReadView.IndexRange requires Bound semantics the BTreeIndex layer does not provide

- `SPEC-001-store.md` §7.2 and `epic-7-read-only-snapshots/story-7.1-committed-read-view.md` define `IndexRange(tableID, indexID, lower, upper Bound)` where `Bound` carries `Inclusive bool`.
- `epic-3-btree-index-engine/story-3.3-range-scan.md` explicitly defers inclusive/exclusive semantics: "If Bound-based semantics (inclusive/exclusive per endpoint) are needed later, add a `SeekBounds(low, high Bound)` variant in a future story" and the half-open `[low, high)` form is the only API.
- `SPEC-001-store.md` §4.6 shows `SeekRange(low, high *IndexKey)` only — nil means unbounded, but there is no exclusive-upper or exclusive-lower representation for finite bounds (closed-open `[low, high)` is the only finite-bound form).

Consequence: Story 7.1 cannot be implemented against the Story 3.3 surface for e.g. `IndexRange(..., Exclusive(v), Inclusive(w))` when `v` and `w` share the same key order — there is no way to express "strictly greater than v" via `*IndexKey` alone for non-integer types (strings, bytes, floats). SPEC-004 predicate-range scans will hit the same wall.

Fix: either (a) promote `SeekBounds(low, high Bound)` into Epic 3 Story 3.3 as a v1 deliverable, and have `SeekIndexRange`/`IndexRange` both consume it; or (b) redefine `IndexRange`/`SeekIndexRange` to take half-open `*IndexKey` bounds and push "exclusive" semantics up into SPEC-004 (with the risk that SPEC-004 then must hand-roll post-filtering).

### 1.3 [CRITICAL] TxID ownership is internally contradictory

- `SPEC-001-store.md` §5.6: `Commit(...) (*Changeset, TxID, error)` — store allocates and returns TxID.
- `SPEC-001-store.md` §6.1 and `epic-6-commit-rollback-changeset/story-6.1-changeset-types.md`: "Do not define a new store-local `type TxID uint64` in this story; use the shared engine type so SPEC-001 and SPEC-002 point at one authoritative home" / "TxID is defined in SPEC-003 §6. SPEC-001 imports it as a cross-spec dependency."
- `epic-6-commit-rollback-changeset/story-6.2-commit.md`: "TxID counter: stored on CommittedState or a separate allocator. Monotonically increasing, never reused."

If the type is owned by SPEC-003 and the allocator lives on `CommittedState` (SPEC-001), then either (a) the executor doesn't own allocation at all and just imports the counter — in which case the sentence "owned by SPEC-003" is misleading; or (b) Commit should accept a caller-supplied TxID, not return one.

Fix: pick one model.
- Model A: executor allocates TxID, passes into `Commit(..., TxID) (*Changeset, error)`. Changeset embeds it.
- Model B: store owns allocation, signature stays `(*Changeset, TxID, error)`, and Story 6.1's "shared type" note is reduced to "TxID is the shared wire type; the store holds the counter."

Also: with Model B, returning TxID twice (once in Changeset.TxID, once in the signature) is redundant — drop one. This also affects SPEC-001 front matter (see §3.1 below: SPEC-003 is not listed as a dep).

### 1.4 [CRITICAL] Undelete-match rule contradicts itself between §5.5 and Story 5.4

- `SPEC-001-store.md` §5.5: "If `Insert` finds an identical committed row that is currently hidden by `tx.deletes`, it must cancel that delete and return the committed row's `RowID` instead of creating a new tx-local row." — "identical" = full row equality.
- `SPEC-001-store.md` §6.2 (Net-effect) table reinforces: "Committed row deleted and then re-inserted with identical value in the same TX → treated as undelete/no-op".
- `epic-5-transaction-layer/story-5.4-transaction-insert.md` step 3: "For PK tables: match by PK value".

Matching only by PK means: delete a committed row with `(pk=5, name="a")`, then insert `(pk=5, name="b")` → Story 5.4 says "undelete by PK match" → changeset empty → subscribers never see the name change. That violates §5.5 and §6.2 and silently corrupts observed state.

Fix: in Story 5.4, "For PK tables: locate the candidate via PK, then require full-row equality to trigger undelete. PK-match without row equality is a normal delete-plus-insert (fires `ErrPrimaryKeyViolation` against the pending delete? → no, the delete has been applied from the tx view; the insert proceeds and old row goes to Deletes, new row to Inserts)."

### 1.5 [CRITICAL] `AsBytes` has no defined return contract and can break immutability

- `SPEC-001-store.md` §2.2 invariants: "`Bytes` values are immutable byte slices. The store must copy caller-provided byte slices on insert unless it can prove exclusive ownership."
- `epic-1-core-value-types/story-1.1-valuekind-value-struct.md`: defines `NewBytes([]byte)` as copying input, but the spec for the accessor is just `AsBool() bool`, `AsInt8() int8`, etc. with no statement about `AsBytes`.

If `AsBytes` returns the internal `buf` directly, any caller can mutate a "stored" value because Go slice headers share backing storage. Every read path (scans, index extraction, changeset materialization) would leak a mutable view.

Fix: Story 1.1 must specify that `AsBytes` returns either a defensive copy or an explicitly read-only view. The cheap v1 contract is "the returned slice aliases internal storage; callers MUST NOT mutate it." Either choice, but say it.

---

## 2. Gaps

### 2.1 [GAP] Sequence recovery: replay does not advance `Sequence.next` past max observed value

- `epic-8-auto-increment-recovery/story-8.1-sequence.md`: `Sequence{next uint64}` plus `Reset(val uint64)`.
- `epic-8-auto-increment-recovery/story-8.2-apply-changeset.md` step 2c: "Insert rows with fresh RowID allocation". Silent on sequence-column handling.
- `epic-8-auto-increment-recovery/story-8.3-state-export.md`: `SetSequenceValue(val uint64)` "restore sequence counter". Relies on snapshot carrying the restored value.

Failure case: crash AFTER commitlog writes a transaction that generated sequence value `N` but BEFORE the next snapshot. Recovery restores `Sequence.next` from the older snapshot (say, to `N-5`). Then ApplyChangeset replays the committed insert carrying value `N` as data, but doesn't bump `Sequence.next`. After recovery, the next auto-assign reissues `N-4…N`, colliding with rows replayed from the log.

SpacetimeDB handles this with a separate `allocated` upper bound persisted in `st_sequence` (`reference/SpacetimeDB/crates/datastore/src/locking_tx_datastore/sequence.rs:9-195`) that advances ahead of consumed values.

Fix: Story 8.2 must either (a) specify that inserts with a non-zero sequence-column value advance `Sequence.next := max(Sequence.next, observed+1)`, or (b) introduce an `allocated` field alongside `next`, persisted via snapshot (Story 8.3), and have the commit path advance `allocated` before releasing the tx.

### 2.2 [GAP] `ErrTableNotFound` has no production site

- `SPEC-001-store.md` §9 lists `ErrTableNotFound` — "tableID references unknown table".
- No story shows where it's raised. `CommittedState.Table(id) (*Table, bool)` (Story 5.1) returns a boolean, not an error.
- `Transaction.Insert`/`Delete`/`Update` stories (5.4–5.6) never handle unknown `TableID`.

Fix: Story 5.4/5.5/5.6 must state that the first step is "look up table; if absent, return `ErrTableNotFound`". StateView methods (Story 5.3) must state that an unknown `TableID` yields an empty iterator (documented) OR an error (then signatures change).

### 2.3 [GAP] `ErrColumnNotFound` is declared but unused

- `SPEC-001-store.md` §9, `story-2.4-error-types.md`: "column name lookup miss".
- No story performs column-name lookup.

Resolution options: (a) drop from §9 and Story 2.4 — SPEC-001 v1 doesn't do name resolution; (b) keep and point to the consumer spec (SPEC-006 schema definitions, or SPEC-004 predicates). Currently it floats. If kept, add a one-liner explaining who raises it.

### 2.4 [GAP] `ErrInvalidFloat` has no declared production site in Story 1.1

- `epic-2-schema-table-storage/story-2.4-error-types.md` says ErrInvalidFloat is "from Epic 1 (NaN), listed here for catalog completeness".
- `epic-1-core-value-types/story-1.1-valuekind-value-struct.md` deliverables say "`NewFloat32`/`NewFloat64` — reject NaN, return `(Value, error)`" — but does not name the error sentinel.

Fix: Story 1.1 must state "returns `ErrInvalidFloat` on NaN". Resolves the forward-reference loop where the catalog lists an error whose producer doesn't bind the name.

### 2.5 [GAP] Snapshot close state not enforced

- `epic-7-read-only-snapshots/story-7.1-committed-read-view.md` acceptance criterion: "After `Close()`, snapshot methods panic or are otherwise not usable".
- `SPEC-001-store.md` §7.2 `CommittedSnapshot` struct shows no "closed" flag; deliverables do not define how "panic after close" is achieved.

Fix: Story 7.1 deliverable must state the mechanism (e.g., `closed atomic.Bool`, or zeroing the underlying pointer on Close so next access panics on nil). Without it, correctness is unspecified.

### 2.6 [GAP] `StateView.SeekIndexRange` may be insufficient for SPEC-004 predicate semantics

- `SPEC-001-store.md` §5.4 and Story 5.3: signature is `SeekIndexRange(..., low, high *IndexKey)` — only half-open `[low, high)` bounds, tx-visibility-aware.
- SPEC-004 will run subscription predicates against in-tx state (SPEC-001 §11 "The evaluator receives a `*Changeset` from the executor after each commit. It also receives a committed read view...") — but executor hot-path predicates running inside the transaction (e.g., RLS filters, reducer-triggered queries) will need the same inclusive/exclusive control as the snapshot path.

Related to 1.2. Decide once whether "exclusive range" is a first-class Bound concept across both paths, or only on the snapshot path.

### 2.7 [GAP] `ApplyChangeset` idempotency / partial-replay semantics undefined

- `epic-8-auto-increment-recovery/story-8.2-apply-changeset.md` mentions "Multiple ApplyChangeset calls in sequence (replaying a log) → cumulative state correct" — this assumes no overlap.
- Unstated: what if the SPEC-002 recovery path replays the same changeset twice (e.g., boundary bug, duplicate segment)? Spec says "Constraint violations during recovery are fatal". So second replay of the same insert → unique violation → crash. That's consistent with "fatal" but not explicit.

Fix: one sentence in Story 8.2 stating "`ApplyChangeset` is not idempotent. It is SPEC-002's responsibility to replay each committed changeset exactly once."

### 2.8 [GAP] Row-shape validation error name unreferenced in §9

- `epic-2-schema-table-storage/story-2.3-row-validation.md` uses `ErrRowShapeMismatch` for column-count mismatches.
- `EPICS.md` error table lists `ErrRowShapeMismatch` as "introduced in Epic 2".
- `SPEC-001-store.md` §9 error catalog does **not** list `ErrRowShapeMismatch`.

Fix: add `ErrRowShapeMismatch` to §9, or rename Story 2.3/2.4 to use `ErrTypeMismatch` for shape too. The catalog is the authoritative list for downstream consumers.

### 2.9 [GAP] Write-lock scope for Commit vs read-lock scope for Snapshot is restated inconsistently

- `SPEC-001-store.md` §5.2: `mu` is "held for write during commit and for read during concurrent read-only snapshot access".
- `SPEC-001-store.md` §7.2: "Commits block until all snapshots are closed." (true for RWLock)
- `story-6.2-commit.md` step 1 "Acquire write lock on CommittedState" + step 7 "Release write lock" — but `Commit` needs to build the `*Changeset` (step 6) before releasing the lock. SPEC-002 consumers take the changeset and process it outside the lock. If the SPEC-002 side holds a snapshot that was taken DURING the commit (impossible while write lock held, so no issue) but could hold a snapshot AFTER commit returns, the consumer contract is implicit.

Fix: state explicitly in §5.6 / Story 6.2 that the `*Changeset` is safe to use after `Commit` returns and the lock is released, because its contents are either freshly allocated `ProductValue` copies (for deleted rows) or rows whose committed-state pointers are now stable. Spec hints at this ("Changeset is immutable after creation") but doesn't say "safe to use after lock release."

---

## 3. Divergences from SpacetimeDB (should be documented)

### 3.1 [DIVERGE] NaN rejected on insert vs SpacetimeDB total-ordering via `decorum::Total`

- `SPEC-001-store.md` §2.2: "NaN is rejected on insert." + "comparator is total over stored values".
- SpacetimeDB: `F32 = decorum::Total<f32>` (`reference/SpacetimeDB/crates/sats/src/algebraic_value.rs:13-16`) — NaN is **allowed** and assigned a total-order position; `AlgebraicValue` derives `Eq`/`Ord`/`Hash` straight through.

Rationale to document: Shunter v1 wants bit-by-bit determinism without importing a decorum-style wrapper; the cost is that legitimate NaN-producing payloads (sensor telemetry, ML outputs) must be rejected at the boundary. Add a one-line note in §2.2 ("Unlike SpacetimeDB, which admits NaN via a total-ordering wrapper, Shunter v1 rejects NaN at construction. Revisit if workloads demand it.") so future maintainers don't re-add NaN support without understanding the tradeoff.

### 3.2 [DIVERGE] No composite types (Sum / Array / nested Product); RowID stable; decoded rows in memory

- `SPEC-001-store.md` §2.1 v1: scalars + `Bytes` only; no nesting.
- `SPEC-001-store.md` §2.3: RowID never reused within lifetime.
- `SPEC-001-store.md` §2.2: "Store rows as `ProductValue` (decoded)".

SpacetimeDB supports `Sum` (tagged unions, also used as `Option`-like Nullability), `Array`, and `Product` nesting (`algebraic_value.rs:27-113`). It stores rows as BFLATN packed pages with content-addressed blob store for large var-len data. RowPointers are **not** stable and can be reused across delete/insert cycles (`table.rs:163-182`).

Shunter's choices are all intentional v1 simplifications. Spec already calls out the storage layout tradeoff in §2.2, and RowID volatility across snapshot restore in §2.3. What's missing: one consolidated "v1 simplifications vs SpacetimeDB" block — either in §1 (Purpose and Scope) or §12 (Open Questions) — so a future schema team adding `Option<T>` columns knows it needs to revisit `Nullable`, `ValueKind`, and `ProductValue` simultaneously.

### 3.3 [DIVERGE] `rowHashIndex` scoped to "no PK" vs SpacetimeDB "no unique index at all"

- `SPEC-001-store.md` §3.3: "When a table has no primary key, a `rowHashIndex` maps the hash of a `ProductValue`…"
- `story-4.4-set-semantics.md`: "`NewTable` creates `rowHashIndex` only when `PrimaryIndex() == nil`".
- SpacetimeDB: `pointer_map` present iff **no unique index exists** of any kind (`reference/SpacetimeDB/crates/table/src/table.rs:79-84`).

Consequence: a Shunter table with a unique-but-not-primary index pays for both the unique-index lookup AND a row-hash lookup on every insert. Not incorrect — strictly redundant. Worth documenting so a perf pass later can tighten the condition to "no unique index of any kind" without a spec edit.

### 3.4 [DIVERGE] Multi-column primary key allowed in Shunter schema model

- `SPEC-001-store.md` §3.1 `IndexSchema`: `Columns []int` + `Primary bool` — nothing prevents a multi-column primary key.
- SpacetimeDB: `primary_key: Option<ColId>` on `TableSchema` — explicitly single-column (`reference/SpacetimeDB/crates/schema/src/schema.rs:175-181`).

If Shunter intends multi-column PKs, confirm that SPEC-006 (schema definition) actually registers them, and that SPEC-004 (subscription predicates) handles compound-key equality. If Shunter intends single-column PK only, tighten §3.1 rule to "primary key covers exactly one column" and require that in Story 2.1 validation.

### 3.5 [DIVERGE] Replay constraint violations "fatal" vs SpacetimeDB silently skipping

- `SPEC-001-store.md` §5.8 and `story-8.2-apply-changeset.md`: "Constraint violations during recovery are fatal: they indicate a corrupt log or schema mismatch that recovery cannot resolve."
- SpacetimeDB `replay_insert` silently ignores duplicates for system-meta rows and is generally tolerant during replay (`reference/SpacetimeDB/crates/datastore/src/locking_tx_datastore/committed_state.rs:620-691`).

Shunter's strict stance is defensible (fail-fast during recovery) but may bite on e.g. idempotent re-replay after a crash-during-replay. Worth a line acknowledging the choice and pointing to Story 8.2 open question.

### 3.6 [DIVERGE] `Changeset` has no `truncated`, `ephemeral`, or `tx_offset` flags

- `SPEC-001-store.md` §6.1: `Changeset{TxID, Tables}`, `TableChangeset{TableID, TableName, Inserts, Deletes}`.
- SpacetimeDB `TxData` carries `truncated: bool` (whole-table clear), `ephemeral: bool` (view-only table, skip durability), and `tx_offset: Option<u64>` (commitlog cursor). See `reference/SpacetimeDB/crates/datastore/src/traits.rs:181-398`.

Shunter v1 may not need `ephemeral` (no views yet) or `truncated` (no `TRUNCATE` reducer), and `tx_offset` is arguably the commitlog's bookkeeping. Fine to omit — but add a note in §6.1 or §12 that these are intentionally absent and when to reconsider (e.g., "if SPEC-004 ever grows ephemeral subscription-only tables, revisit the Changeset shape").

---

## 4. Internal consistency

### 4.1 [NIT] SPEC-001 front matter omits SPEC-003 as a dependency

- `SPEC-001-store.md` header: "Depends on: SPEC-006 (Schema Definition)".
- §6.1 imports `TxID` from SPEC-003 (per Story 6.1). §11 "SPEC-003 (Transaction Executor)" declares contract.
- `EXECUTION-ORDER.md` Phase 1 lists "SPEC-003 E1 contract slice" as a prerequisite for SPEC-001 Commit.

Fix: add SPEC-003 to the "Depends on" line, at least for the TxID type.

### 4.2 [NIT] Commit signature returns TxID twice

- `SPEC-001-store.md` §5.6: `Commit(committed, tx, schema) (*Changeset, TxID, error)`.
- §6.1: `Changeset{TxID, ...}`.

Redundant. Pick one. If keeping both, explain (e.g., "the explicit return is the authoritative value; the embedded copy is for consumers that only hold the Changeset"). Simpler to drop the bare return and let callers read `changeset.TxID`.

### 4.3 [NIT] ColID exists but schema uses raw `int`

- `SPEC-001-store.md` §2.5: `type ColID int`.
- §3.1: `ColumnSchema{Index int, ...}`, `IndexSchema{Columns []int, ...}` — both raw `int`.
- Story 2.1 repeats raw `int`.

Cosmetic inconsistency. Either adopt `ColID` throughout the schema structs, or drop `ColID` from §2.5 as purely a SPEC-004 concern (predicates). Since §2.5 says "SPEC-004 uses `ColID` in predicate types", the current state is "define here, use elsewhere" — tolerable but easy to miss.

### 4.4 [NIT] Performance section title vs open-question framing

- §10 header: "Performance Constraints".
- §12.4 open question: "Performance targets: The current latency goals should be treated as aspirational microbenchmark targets, not contractual SLAs."

"Constraints" suggests binding; body text demotes to aspirational. Rename §10 to "Performance Targets" to match the open-question framing and avoid accidental contractual reading.

### 4.5 [NIT] Story 1.1 "zero-initialized Value" status

- `story-1.1-valuekind-value-struct.md` design note: "The zero-initialized Go struct for `Value` is not part of the store contract. Valid stored values should come from explicit constructors…"
- Go `var v Value` yields `{kind: ValueKind(0), …}` — `ValueKind(0)` is `Bool` if Bool is the zeroth variant.

No behavior broken, but the implication is that `Value{}` looks like a valid bool (false). Two tightening options: (a) declare `ValueKind(0) = Invalid` and make variants start at 1; (b) add a `valid bool` field or similar. Either is a Story 1.1 deliverable-level tweak, not a design issue.

### 4.6 [NIT] `Nullable` flag is decorative but not marked as such in Story 2.1

- `story-2.1-schema-structs.md`: `Nullable bool // always false in v1`.
- Story 2.3: "'nullable' means SQL NULL, not Go zero. v1 has no NULL concept, so this check is a no-op."

Acceptance list in Story 2.1 does not test that Nullable=true is rejected or accepted. If Nullable=true is forbidden in v1, add a schema-validation check that rejects it. Otherwise, set expectations explicitly ("tolerated but ignored").

### 4.7 [NIT] Primary IndexID=0 rule is ambiguous for no-PK tables

- `SPEC-001-store.md` §4.2: "IndexID 0 is always the primary index if one exists; subsequent IDs are assigned in declaration order."
- Story 2.1: "If a primary index exists, its `IndexID` is `0`. Remaining index IDs are assigned in declaration order from the `TableSchema.Indexes` slice."

For a no-PK table, does the first declared secondary index get IndexID=0, or IndexID=1 (with 0 "reserved")? Both readings are defensible; pick one. Reserving 0 as "primary slot" is cleaner because it means "IndexID==0 ⇒ primary or missing". Declaration-order from 0 is simpler but collides semantically with the PK convention.

### 4.8 [NIT] Epic 7 blocks says "Nothing" but other specs consume it

- `epic-7-read-only-snapshots/EPIC.md`: "Blocks: Nothing (consumed by SPEC-004 subscription evaluator)".
- `EXECUTION-ORDER.md` cross-phase contracts: Epic 7 also feeds SPEC-003 E5 (post-commit snapshot acquisition) and SPEC-005 one-off queries.

Update to "Consumed by SPEC-003 post-commit, SPEC-004 subscription initial-state, SPEC-005 one-off queries."

### 4.9 [NIT] §11 executor contract restates `(cs *CommittedState) Snapshot()` outside Epic-7 context

- `SPEC-001-store.md` §11 (Transaction Executor interface) lists `func (cs *CommittedState) Snapshot()` among the exported names the executor may rely on.
- Snapshot is actually an Epic-7 / §7 concern, not a core executor API. Executor post-commit uses it; executor core does not.

Minor clean-up: move Snapshot out of the "SPEC-003" interface block and into the "SPEC-004 subscription evaluator" and "SPEC-002 commit log" blocks, or call it out as a separate "Shared concurrency primitives" section. Keeping it in §11 (Executor) is harmless but misleading.

---

## 5. Epic/story coverage

### 5.1 Verified good coverage

- Epic 1 stories cover §2.1–§2.5 end to end; story 1.6 bundles RowID + Identity + ColID.
- Epic 2 covers schema structs, validation, and bare row storage.
- Epic 3 covers IndexKey/Bound/BTree/range/multi-column; gap on Bound semantics flagged in 1.2.
- Epic 4 covers index maintenance, PK + unique constraints, set semantics, constraint errors.
- Epic 5 covers CommittedState, TxState, StateView, Insert/Delete/Update; undelete ambiguity flagged in 1.4.
- Epic 6 covers Changeset types, Commit, net-effect verification, Rollback.
- Epic 7 covers CommittedReadView + concurrency tests.
- Epic 8 covers Sequence, ApplyChangeset, state export; sequence-recovery gap flagged in 2.1.

### 5.2 [GAP] Spec §6.3 "consumers" receive the same Changeset — no concurrency contract

- §6.3: "`Changeset` is passed to: Subscription Evaluator … Commit Log … Both receive the same `Changeset` value. It is read-only after creation."

No story states whether both consumers read concurrently (they probably do — SPEC-002 persists, SPEC-004 evaluates, both on post-commit). If `TableChangeset.Inserts` is `[]ProductValue` and rows alias committed-state byte slices for `Bytes` columns, concurrent reads are safe. Story 6.1 should spell out "Changeset reads are safe from multiple goroutines; consumers must not mutate any `Value.buf`."

### 5.3 [GAP] No story covers the `Bytes` copy requirement at the Insert boundary

- §2.2: "The store must copy caller-provided byte slices on insert unless it can prove exclusive ownership."
- `story-1.1-valuekind-value-struct.md`: `NewBytes` copies.

But `Transaction.Insert` takes a `ProductValue`; if the caller hand-built a `ProductValue` containing a `Value` whose `buf` they retain and later mutate, the store is now aliasing mutable memory. Either Story 5.4 needs a "copy any Bytes Value whose provenance is not `NewBytes`" step, or the Value API must prevent construction of a Bytes value without going through `NewBytes`. The latter is achieved by unexported fields (already true in §2.2) — but deserialization paths (BSATN → Value) must also copy. Note this in Story 5.4 or in §2.2.

### 5.4 [GAP] Story 8.3 `SetNextID` / `SetSequenceValue` semantics asymmetric

- `story-8.3-state-export.md`: "SetNextID must set the counter to at least the provided value. If current counter is already higher (from ApplyChangeset allocations), keep the higher value."
- `SetSequenceValue` has no such "max" rule.

Consistency fix: Story 8.3 should state that `SetSequenceValue` also takes max(current, provided). If sequence state was advanced by replay (see 2.1), the restore-from-snapshot value may be stale.

---

## 6. Clean-room boundary

Overall: the decomposition docs are prose- and Go-typed; no Rust identifiers or verbatim SpacetimeDB names appear. Type names (`Value`, `ValueKind`, `ProductValue`, `Table`, `RowID`, `Identity`, `ColID`, `TableID`, `IndexID`, `IndexKey`, `Bound`, `BTreeIndex`, `CommittedState`, `TxState`, `StateView`, `Transaction`, `Changeset`, `TableChangeset`, `CommittedReadView`, `CommittedSnapshot`, `Sequence`) are idiomatic Go and conceptually similar to SpacetimeDB's but not copies.

- `AlgebraicValue` → `Value` (different design; tagged struct vs Rust enum).
- `RowPointer` → `RowID` (semantically different — Shunter's is stable uint64, SpacetimeDB's is page+offset).
- `MutTxId`/`TxId` → `Transaction` (method surface different).
- `TxData` → `Changeset` (structurally similar, simplified — see 3.6).
- `pointer_map` → `rowHashIndex` (same idea, different name).
- `decorum::Total<f32>` → (nothing — Shunter rejects NaN; see 3.1).

No story references SpacetimeDB file paths or Rust symbol names. No copied doc prose detected. Boundary looks clean.

One advisory note: `reference/SpacetimeDB` is present under `reference/` and `CLAUDE.md` forbids porting code. Spec docs are compliant. If an implementer cites reference while writing code, the rule to enforce is "behavior parity is OK, code lineage is not" — that belongs in a contributor doc, not this audit, but worth remembering before Phase 3 implementation starts.

---

## 7. Quick wins (suggested ordering for doc repair)

1. Fix the ±0 equality/hash bug (1.1) — a one-line canonicalization in Story 1.1.
2. Fix the Commit / TxID contradiction (1.3) — pick Model A or B in Story 6.1 + Story 6.2 + §5.6 + §6.1.
3. Fix undelete-match rule in Story 5.4 (1.4) — two-sentence edit.
4. Fix Bound / IndexRange split (1.2) — design decision, then edits to Stories 3.3, 5.3, 7.1.
5. Document `AsBytes` return contract (1.5) — one line in Story 1.1.
6. Add sequence-replay advance rule (2.1) — one paragraph in Story 8.2.
7. Plug missing error production sites (2.2, 2.3, 2.4) — a few lines across Stories 1.1, 5.4.
8. Add SPEC-003 to SPEC-001 front matter (4.1).
9. Add intentional-divergence block (3.x) — one subsection in §1 or §12.
10. Everything else (nits).

---

## 8. Spec-to-code sanity check (follow-up, not this pass)

The repo has a `commitlog/` directory with modified Go files, which implies SPEC-002 is partially implemented despite CLAUDE.md saying "docs-first." Out of scope for SPEC-001 audit but worth noting:

- `commitlog/{durability,recovery,replay,segment_scan,snapshot_io}.go` — modified.
- `.hermes/plans/2026-04-16_*` — recent plans for SPEC-002 work.

When we audit SPEC-002, we should reconcile its docs against both `/reference/SpacetimeDB/crates/{commitlog, durability, snapshot}` and the live `commitlog/` implementation.

---

# SPEC-002 — Commit Log

Audited files:

- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md`
- `docs/decomposition/002-commitlog/EPICS.md`
- `docs/decomposition/002-commitlog/epic-{1..7}/EPIC.md`
- All `story-*.md` under those epics

SpacetimeDB reference: `reference/SpacetimeDB/crates/{commitlog,durability,snapshot,datastore}`.

Live implementation cross-read: `commitlog/*.go` (partially implemented; used to spot doc drift, not graded as a code audit).

---

## 1. Critical

### 1.1 [CRITICAL] `SnapshotInterval` default contradicts itself between §8 and §5.6/Story 4.1

- `SPEC-002-commitlog.md` §8 Configuration: `SnapshotInterval uint64` — "Default: 100_000".
- `SPEC-002-commitlog.md` §5.6: "**recommended default is `SnapshotInterval = 0`** (no automatic interval-based snapshotting)".
- `epic-4-durability-worker/story-4.1-durability-handle.md` deliverable: `SnapshotInterval uint64 // default 0 (no auto-snapshot)`.
- Live `commitlog/durability.go` `DefaultCommitLogOptions()` returns `SnapshotInterval: 0`.

§8 prescribes the exact behavior §5.6 warns against ("synchronous snapshot creation holds `CommittedState.mu` for read during full state serialization … tens to hundreds of milliseconds, during which all commits block"). An implementer who wires defaults off §8's table and doesn't read §5.6 will get periodic commit-latency spikes on every 100_000th write.

Fix: set §8 default to `0`. §5.6 and Story 4.1 are correct; §8 is the outlier.

### 1.2 [CRITICAL] Decoded `Changeset.TxID` is never stamped; encode/decode contract silent

- `SPEC-001-store.md` §6.1: `Changeset{TxID, Tables}` — TxID is a real field.
- `SPEC-002-commitlog.md` §3.2 payload layout omits tx_id (frame-level field).
- `epic-3-changeset-codec/story-3.2-changeset-decoder.md` signature: `DecodeChangeset(data []byte, schema SchemaRegistry) (*Changeset, error)` — no txID input.
- `epic-6-recovery/story-6.3-log-replay.md` replay loop: decode → `ApplyChangeset(committed, cs)` — does not stamp `cs.TxID` from `record.TxID`.
- Live `commitlog/replay.go` matches: no stamp step.

So after recovery decode, `Changeset.TxID` is `0`. Any consumer that trusts it (subscription evaluator, metrics, logs) will see zero-valued TxIDs for replayed transactions. Since SPEC-001 §5.6 returns TxID from Commit and `§6.3` advertises Changeset as the shared consumer object, this is a substantive field, not a cosmetic one.

Fix options: (a) change Story 3.2 signature to `DecodeChangeset(txID TxID, data []byte, schema) (*Changeset, error)`; (b) make Story 6.3 explicitly stamp `cs.TxID = TxID(record.TxID)` before `ApplyChangeset`; (c) drop `TxID` from `Changeset` entirely and pass it alongside where consumers need it. Pick one; §3.2 should say which.

### 1.3 [CRITICAL] Snapshot file layout §5.2 omits the per-table `nextID` section Stories 5.2/5.3 require

- `SPEC-002-commitlog.md` §5.2 body layout: header → schema_len → schema → seq_count → sequences → table_count → tables (each with row_count + rows). No `nextID` section.
- `epic-5-snapshot-io/story-5.2-snapshot-writer.md` deliverable 3: "Table allocation section from SPEC-001 export hooks: sorted `(table_id, next_id)` pairs so future internal `RowID` allocation resumes correctly after restore".
- `epic-5-snapshot-io/story-5.3-snapshot-reader.md` `SnapshotData.NextIDs map[TableID]uint64`.
- Live `commitlog/snapshot_io.go:316` writes a third section: `next_id_count` + `(table_id: uint32, next_id: uint64)` pairs between sequences and tables.

§5.2 and Story 5.2 define two incompatible file formats. An implementer reading only §5.2 would produce a file the Story 5.3 reader cannot parse past the sequences block.

Fix: add the `nextID` section to §5.2 layout between sequences and tables:

```
next_id_count      : uint32 LE
[ for each table, sorted by table_id ascending:
    table_id       : uint32 LE
    next_id        : uint64 LE
]
```

Document it as SPEC-001 Story 8.3 state-export state.

### 1.4 [CRITICAL] Recovery sequence-advance step is undefined — SPEC-001 §2.1 gap silently inherited

- `SPEC-002-commitlog.md` §6.1 step 6 says only "decode → ApplyChangeset".
- `story-6.4-open-and-recover.md` acceptance: "Sequences restored from snapshot, then advanced by replay" — mechanism unspecified.
- SPEC-001 audit §2.1 flagged that `ApplyChangeset` (SPEC-001 §5.8, Story 8.2) does not bump `Sequence.next` past observed values. Without SPEC-001's Story 8.2 fixing this, SPEC-002 replay leaves `Sequence.next` stuck at the snapshot value.
- Live `commitlog/recovery.go:73` calls a helper `advanceRecoveredSequences(committed)` after `ReplayLog` returns. This helper is not surfaced in any story.

Consequence: spec permits an implementation that, after recovery, re-issues auto-increment values that are already present in replayed rows. The live impl works around it with an undocumented helper; spec readers won't.

Fix: name the owner of the advance step. Either (a) force the fix into SPEC-001 Story 8.2 so `ApplyChangeset` bumps the counter as a side effect; or (b) add a Story 6.4 step "after replay completes, walk every table with a sequence column and set `Sequence.next := max(current, observed_max + 1)`". Preferred: (a), single-place fix. Either way, SPEC-002 Story 6.4 should cross-reference the responsible SPEC-001 story.

---

## 2. Gaps

### 2.1 [GAP] `ErrSnapshotInProgress` defined but omitted from §9 error catalog

- `story-5.4-snapshot-integrity.md` defines `ErrSnapshotInProgress` (sentinel).
- `EPICS.md` error table lists it as introduced in Epic 5.
- `SPEC-002-commitlog.md` §9 error catalog does not.
- Live `commitlog/snapshot_io.go:213` returns it.

Fix: add `ErrSnapshotInProgress` to §9.

### 2.2 [GAP] `ErrTruncatedRecord` defined but not in §9 catalog and not named in §2.3/§6.4

- `story-2.4-segment-reader.md` and `story-2.5-segment-error-types.md` define `ErrTruncatedRecord` as the sentinel that distinguishes truncated tail from mid-segment corruption.
- §9 omits it entirely. §2.3 treats incomplete records implicitly ("A record is valid only if all framing bytes, the full payload, and the trailing CRC are present"). §6.4 says the symptom is "CRC mismatch or EOF" without naming the error.

Without a named error, Epic 6 consumers cannot reliably branch on "truncated tail (recoverable)" vs "`ErrChecksumMismatch` (fatal in sealed segment)". Story 2.4 depends on this distinction.

Fix: add `ErrTruncatedRecord` to §9 and reference it in §2.3 and §6.4.

### 2.3 [GAP] Schema snapshot §5.3 does not include per-column `Nullable` / `AutoIncrement` flags

- `SPEC-002-commitlog.md` §5.3 per-column trailer: `type_tag : uint8`. That is it.
- SPEC-001 `story-2.1-schema-structs.md` `ColumnSchema` has `Nullable bool`. SPEC-001 Story 8.1/8.3 implies an `AutoIncrement` attribute (at least one column per table may be auto-inc).
- Live `commitlog/snapshot_io.go:87` writes three trailing bytes per column: `byte(col.Type), boolByte(col.Nullable), boolByte(col.AutoIncrement)`. §5.3 says one.

Fix: either update §5.3 to specify the three-byte trailer (`type_tag, nullable, auto_increment`) and update Story 5.1 deliverables to match the live shape; or strip `Nullable`/`AutoIncrement` from the live encoder and declare the one-byte trailer authoritative. The latter is only defensible if SPEC-001 v1 truly has neither concept; per SPEC-001 audit §4.6, Nullable is tolerated-but-unused, so encoding it is optional but the spec must pick a side.

### 2.4 [GAP] `row_count` width disagreement: spec `uint64`, Story+impl `uint32`

- §5.2: `row_count : uint64 LE`.
- `story-5.2-snapshot-writer.md` acceptance: "row_count matches" — width not specified.
- Live `commitlog/snapshot_io.go:332` writes `uint32(len(rows))`.

Every other count field in the snapshot (seq_count, table_count, idx_count, col_count, table id, schema_len) is `uint32 LE`. The `uint64` for row_count is the odd one out. 2^32 rows per table is 4.3B, an acceptable v1 ceiling.

Fix: pick `uint32` in §5.2 for consistency; or change Story 5.2 + live impl to `uint64`.

### 2.5 [GAP] SPEC-002 front matter omits SPEC-003 and SPEC-006 dependencies

- Header "Depends on: SPEC-001 (In-Memory Store) for `Changeset` and `ProductValue` types".
- Spec and stories use `TxID` (owned by SPEC-003 per SPEC-001 §6.1 / SPEC-003 §6; see SPEC-001 audit §1.3 / §4.1).
- Story 6.2, Story 6.4, and §6.1 step 4b use `SchemaRegistry` and `SchemaRegistry.Version()` — SPEC-006 territory.

Fix: extend front matter to "Depends on: SPEC-001, SPEC-003 (TxID), SPEC-006 (SchemaRegistry)".

### 2.6 [GAP] Snapshot→CommittedState restore API is not named anywhere

- `story-6.4-open-and-recover.md` step 3: "register tables from schema, populate rows from snapshot, restore sequences, restore per-table `nextID`, rebuild indexes".
- SPEC-001 Epic 8 Story 8.3 defines getters (`NextID`, `SequenceValue`) and setters (`SetNextID`, `SetSequenceValue`) but not a bulk-row-load path.
- Story 6.4 silently assumes a `CommittedState.RegisterTable(...)` + bulk row append surface exists; SPEC-001 §5 does not name one. Live impl synthesizes it (`store.NewCommittedState()` + `committed.RegisterTable(...)` + internal append calls that bypass the `Transaction.Insert` path).

The recovery orchestration composes methods SPEC-001 never specifies. SPEC-001 also needs the index-rebuild entry point (Story 6.4 claims "rebuild indexes from those rows" but no SPEC-001 method is named).

Fix: SPEC-001 Story 8.3 (or a new Story 8.4) should name the restore surface — `CommittedState.RegisterTable(TableID, *Table)`, `Table.RestoreRow(ProductValue) RowID`, `Table.RebuildIndexes()`. SPEC-002 Story 6.4 should reference those exact names.

### 2.7 [GAP] `SchemaRegistry.Version()` contract is used but undefined

- `story-6.2-snapshot-selection.md` schema-match algorithm: "Schema version match (`SchemaRegistry.Version()`)".
- §6.1 step 4b: "schema version (from `SchemaRegistry.Version()`)".
- `SPEC-002` does not define the semantics of `Version()`: is it application-supplied? monotonic across registry mutations? byte-equal after reload?
- Snapshot file stores `schema_version` in **two** places: header (§5.2 byte 44–47) and inside the schema body (§5.3 first field, also uint32 LE).

When these disagree (corrupted snapshot, or a future schema migration path), which one wins? §6.1 says "compare to `SchemaRegistry.Version()`" — singular. Spec silent on authority.

Fix: (a) declare one storage location authoritative (suggested: the header, because it can be validated before Blake3 recompute); (b) drop `schema_version` from the other; (c) cross-reference SPEC-006 for `Version()` semantics.

### 2.8 [GAP] `durable_horizon` when segments are empty but a snapshot exists is undefined

- `SPEC-002-commitlog.md` §6.1 step 2: "find the highest contiguous valid `tx_id` reachable from the earliest segment". Undefined when there are no segments.
- §6.1 step 3: "Only snapshots with `tx_id <= durable_horizon` are candidates".
- `story-6.4-open-and-recover.md` acceptance: "No segments + valid snapshot → use snapshot as final state (snapshot_tx_id is the durable point)".
- Live `commitlog/recovery.go:37` sets `durableHorizon = types.TxID(^uint64(0))` when segments list is empty — effective +∞, a silent convention.

Fix: §6.1 step 2 should state "if no segments, `durable_horizon` = +∞ (any snapshot is eligible because there is no contradicting log history)", or equivalently skip the horizon filter on the no-segments branch.

### 2.9 [GAP] Per-TxID durability ack not in §4.2; live impl adds `WaitUntilDurable`

- §4.2 `DurabilityHandle` has `EnqueueCommitted` (fire-and-forget) + `DurableTxID() TxID` (monotonic getter). No event-based per-TxID ack.
- SPEC-003's client-ack path almost certainly needs "tell me when TX N is durable". Polling `DurableTxID()` in a loop is the only spec-compliant option.
- Live `commitlog/durability.go:181` adds `WaitUntilDurable(txID TxID) <-chan TxID` with a waiters map.

If the executor must block per-TX until durable, the handle needs a dedicated API. If polling is intentional, state it.

Fix: either add `WaitUntilDurable` to §4.2 and Story 4.1, or add a §4.2 note that executors must poll `DurableTxID` at their cadence.

### 2.10 [GAP] `AppendMode` three-state enum lives in Story 6.1 but not in §6.4

- `SPEC-002-commitlog.md` §6.4 "Truncated Record and Resume Handling" uses only "MAY resume by creating a fresh next segment".
- `story-6.1-segment-scanning.md` defines `AppendMode {AppendInPlace, AppendByFreshNextSegment, AppendForbidden}` as a hard enum and marks it a normative deliverable.
- Story 4.3 and Story 6.4 treat those three states as mandatory behavior (Story 4.3 acceptance: "Recovery resume mode with damaged writable tail opens a fresh next segment at `last_valid_tx_id + 1`").
- Live impl exposes `RecoveryResumePlan` (`commitlog/recovery.go:11`) and routes durability startup through it.

The story decomposition committed to a stricter contract than §6.4's "MAY" text permits.

Fix: promote `AppendMode` (or a spec-level equivalent) into §6.4; change "MAY" to "MUST" for the fresh-next-segment case; document the resume-plan hand-off Epic 6 → Epic 4.

### 2.11 [GAP] No story owns the "schema is static for data-dir lifetime" invariant on the write side

- §3.4 states a hard invariant: schema never changes between snapshot and commit log records.
- Only Story 3.2 (decoder) and Story 6.2 (recovery schema compare) mention it. No writer-side story states that the encoder refuses to run if the registered schema has mutated mid-session.
- SPEC-002 v1 has no schema-change record type (§3.4, §12 OQ #4), so enforcement is implicit.

Fix: add a one-liner to Story 3.1 (or to Story 4.2) stating the encoder assumes static schema and produces undefined output if the registry has changed since snapshot.

### 2.12 [GAP] Snapshot retention policy deferred but no story owns the decision

- §12 OQ #2: "v1 should keep at least the newest two successful snapshots. Whether retention should be count-based, age-based, or size-based is deferred."
- Epic 7 has Story 7.1 (segment coverage) and Story 7.2 (segment compaction) only. No snapshot-GC story.
- Risk: `snapshots/{tx_id}/` directories accumulate forever; with object-hardlinking disabled (Shunter doesn't do dedup) each snapshot is a full copy.

Fix: either add a snapshot-retention story under Epic 7 or state in §7 / Story 7.2 that snapshot retention is out of scope for v1 and name the consequence.

### 2.13 [GAP] Graceful-shutdown snapshot flow has no orchestration story

- §5.6: "The engine SHOULD call `CreateSnapshot` exactly once on graceful shutdown — immediately before closing the durability worker, while no new commits are being accepted."
- No story owns the shutdown ordering (quiesce executor → final CreateSnapshot → DurabilityWorker.Close()).
- Belongs partly to SPEC-003 (executor shutdown sequence) but it is SPEC-002-coupled; ownership is unclear.

Fix: cross-reference SPEC-003's shutdown story from Story 5.2, or add a dedicated integration story owning the ordering.

---

## 3. Divergences from SpacetimeDB (should be documented)

### 3.1 [DIVERGE] "BSATN" name imported from SpacetimeDB but the encoding is a Shunter rewrite

- `SPEC-002-commitlog.md` §3.3 is titled "ProductValue Encoding (BSATN Codec — canonical reference)" and marked as the source of truth for Shunter.
- The acronym BSATN = "Binary SpacetimeDB Algebraic Type Notation". The Shunter encoding has its own tag numbering (0–12 for 13 scalars) and omits Sum/Array/nested-Product entirely. It is not wire-compatible with SpacetimeDB's encoding.
- Live code lives at `bsatn/`, reinforcing the identity.

Risk: a future reader (or security/legal review) could read "BSATN" as implying format compatibility or code lineage from SpacetimeDB. It is neither.

Fix options: (a) rename — e.g. "SVF" (Shunter Value Format) or similar — for a clean break; (b) keep the name but prefix §3.3 with one sentence: "Shunter's encoding is of the same family as SpacetimeDB's BSATN but not byte-compatible. Tag numbering and type coverage are Shunter-specific." Either is fine; the current state (no disclaimer) is not.

### 3.2 [DIVERGE] No offset index file; recovery performs linear scan

- SpacetimeDB keeps a per-segment offset index (tx_offset → byte pos) so replay can seek in O(log) instead of scanning.
- Shunter has no offset index. `story-6.3-log-replay.md` skips records by decoding framing and discarding when `tx_id ≤ fromTxID`. Cost is O(total records since log origin), not O(records after snapshot).
- Performance §10 target "Recovery (snapshot load + 10k log records) < 5 s" is achievable but fragile with a long history and no snapshot compaction.

Fix: add a one-line note in §12 Open Questions acknowledging the deferred optimization and the recovery-time implication.

### 3.3 [DIVERGE] Single TX per record vs SpacetimeDB 1–65535-TX commits

Already documented in §2.3 ("Why no `n`…"). OK as-is.

### 3.4 [DIVERGE] Replay strictness — any `ApplyChangeset` error is fatal

- `story-6.3-log-replay.md`: "ApplyChangeset errors during replay are fatal".
- `SPEC-002-commitlog.md` §6.5: gaps/overlaps/out-of-order are hard recovery errors.
- SpacetimeDB `replay_insert` tolerates idempotent duplicates for system-meta rows; Shunter does not.

Consistent with SPEC-001 audit §3.5 stance but should be called out in SPEC-002 §1 or §12 as an intentional tightening (fail-fast during recovery).

### 3.5 [DIVERGE] First TxID is 1, not 0

- §6.1 step 5: "if the earliest remaining log record has `tx_id = 1`, start from empty".
- SpacetimeDB `tx_offset` starts at 0.

Minor, but SPEC-002 should state it explicitly (e.g., §2.3 "the first committed transaction has `tx_id = 1`; `tx_id = 0` is reserved as the pre-commit sentinel used by `DurableTxID()`").

### 3.6 [DIVERGE] Single auto-increment sequence per table (implicit)

- §5.2 sequences section stores one `(table_id, next_id)` pair per table.
- Story 5.3 `Sequences map[TableID]uint64`.
- SpacetimeDB `st_sequence` supports multiple sequences per table (one per auto-inc column).

SPEC-001 Story 8.1/8.3 also models one sequence per table (`Table.SequenceValue() (uint64, bool)`). Consistent within Shunter v1.

Fix: note in §5.1 that v1 allows at most one auto-increment column per table; revisit when a second is needed.

### 3.7 [DIVERGE] No segment compression / sealed-immutable marker

- SpacetimeDB can mark sealed segments immutable and block-compress them with zstd.
- Shunter has no compression path and no sealed-immutable bit; compaction is delete-only.

Fine for v1. §12 OQ #5 covers snapshot compression; extend that bullet to cover segments as well.

---

## 4. Internal consistency

### 4.1 [NIT] `schema_version` stored twice: snapshot header (§5.2) and schema body (§5.3)

See 2.7 for consequence. Header copy lets the reader reject a schema-mismatched snapshot before Blake3 recompute — useful. Body copy duplicates it. Pick one.

### 4.2 [NIT] `TxID` type leaks as bare `uint64` in Story 2.2 and live impl

- §4.2 interface uses `TxID TxID`.
- `story-2.2-record-framing.md` Record struct: `TxID TxID`.
- Live `commitlog/segment.go:35` Record.TxID is `uint64`; `commitlog/durability.go:135` `EnqueueCommitted(txID uint64, ...)`.

Consistent with SPEC-001 audit §1.3 / §4.1: TxID ownership is ambiguous, so the impl hand-rolls `uint64` and crosses the boundary at call sites. Not a spec problem per se, but documenting SPEC-003 as a dep (see 2.5) + standardizing on `types.TxID` everywhere will eliminate the drift.

### 4.3 [NIT] §9 catalog missing four errors that stories introduce

Consolidated from 2.1, 2.2:

- `ErrSnapshotInProgress` (Story 5.4)
- `ErrTruncatedRecord` (Story 2.4)

Also: EPICS.md error table and SPEC-002 §9 mostly agree, except these two. Reconcile.

### 4.4 [NIT] `Record` struct docs imply CRC is a field, but it is not

- `story-2.2-record-framing.md` Record struct has no CRC field; §2.3 record layout shows CRC on disk.
- Convention is correct (CRC is recomputed on read, not stored in memory) but the doc does not explain the split. One sentence would help: "`Record` is the in-memory form; on-disk framing prepends `crc` computed at write time and verified at read time."

### 4.5 [NIT] §4.4 write loop claims `atomic.Uint64` for state; live impl uses a mutex for waiters

- §4.3/§4.4 describe the atomic-only approach for durable offset.
- Live `commitlog/durability.go:46` has `stateMu sync.Mutex` guarding `waiters map[uint64][]chan TxID`, `closing`, `fatalErr`. This supports `WaitUntilDurable` (2.9 above) and the close-path drain.

Once the spec adopts `WaitUntilDurable` (or mandates polling), revisit §4.3 to clarify that the atomic is only for the counter; other state needs its own guard.

### 4.6 [NIT] EPICS.md dep graph omits the recovery → durability AppendMode hand-off

- `EPICS.md` dep graph: Epic 2 → Epic 4 and Epic 2 → Epic 6 (and Epic 7). No line from Epic 6 back to Epic 4 for the resume-plan contract Story 6.4 + Story 4.3 share.

Fix: add an arrow Epic 6 → Epic 4 (or a footnote) documenting the resume-plan hand-off.

### 4.7 [NIT] §2.1 diagram shows `.log` extension; §6.1 talks about segments without mentioning the extension

Cosmetic. Names are consistent in stories.

### 4.8 [NIT] §8 has no fsync-policy knob and §12 OQ #3 hints one may come

Current v1 is fixed batch-sync. If the OQ is real, even a placeholder `FsyncMode enum{Batch, PerTx}` (unused in v1) would prevent a later breaking change. Minor.

---

## 5. Epic/story coverage

### 5.1 Verified good coverage

- Epic 1 covers §3.3 Value/ProductValue encode/decode end-to-end.
- Epic 2 covers §2.1–§2.4 (segment header, record framing, writer, reader, error types).
- Epic 3 covers §3.1–§3.2 (Changeset codec).
- Epic 4 covers §4.1–§4.6 (durability worker, rotation, failure handling).
- Epic 5 covers §5.1–§5.6 (snapshot writer, reader, schema section, integrity, lockfile, in-progress detection).
- Epic 6 covers §6.1–§6.5 (scanning, snapshot selection, replay, orchestration, error types).
- Epic 7 covers §7 (segment coverage, compaction).

### 5.2 [GAP] §5.2 has no story for the `nextID` section

Already flagged in 1.3 as CRITICAL. Listed here so the coverage table is not misread as "Epic 5 covers §5 in full".

### 5.3 [GAP] Sequence-advance-on-replay has no story

Already flagged in 1.4 as CRITICAL. Belongs either in SPEC-001 Story 8.2 (preferred) or as a new step in SPEC-002 Story 6.4.

### 5.4 [GAP] Snapshot retention has no story (see 2.12)

### 5.5 [GAP] Graceful-shutdown snapshot orchestration has no story (see 2.13)

### 5.6 [GAP] Snapshot→CommittedState bulk-restore API has no owner (see 2.6)

---

## 6. Clean-room boundary

Overall: clean. The decomposition is Go-typed, prose is original, and the only explicit SpacetimeDB callouts (`SPEC-002-commitlog.md` §2.3 "Why no epoch field", "Why no `n`") are behavioral-divergence explainers — not copied code or Rust identifiers. Naming (`DurabilityHandle`, `DurabilityWorker`, `SegmentWriter`, `SegmentReader`, `SnapshotWriter`, `SnapshotReader`, `OpenAndRecover`, `Record`, `SegmentInfo`, `SnapshotData`, `ErrBadMagic`, `ErrHistoryGap`) is idiomatic Go and does not echo Rust naming.

Concept→name map against reference:

- `Commitlog::commit` / `Durability::append_tx` → `DurabilityHandle.EnqueueCommitted`.
- Commit record (min_tx_offset + epoch + n + payload_len + payload + CRC) → Record (tx_id + record_type + flags + data_len + payload + CRC). Epoch and n explicitly dropped.
- Segment offset index → (absent; see 3.2).
- Snapshot `Blake3` + BSATN body → Shunter snapshot: `Blake3` over body + Shunter-defined body layout.
- `SnapshotRepository::latest_snapshot()` / `.lock` detection → `ListSnapshots` + `HasLockFile`.

One clean-room caveat:

### 6.1 "BSATN" name borrowed verbatim (see 3.1)

The acronym is SpacetimeDB terminology. The Shunter encoding is not compatible. Recommend a naming disclaimer or rename.

Everything else (no Rust identifiers, no copied prose, no file-path citations into the Rust tree) is compliant with `CLAUDE.md`.

---

## 7. Quick wins (suggested ordering for doc repair)

1. Fix `SnapshotInterval` default in §8 to `0` (1.1) — one-character edit.
2. Add `nextID` section to §5.2 layout (1.3) — short paragraph.
3. Decide on `Changeset.TxID` stamping and document in Story 3.2 or 6.3 (1.2).
4. Resolve sequence-advance-on-replay ownership (1.4) — either SPEC-001 Story 8.2 edit or SPEC-002 Story 6.4 step.
5. Add `ErrSnapshotInProgress` and `ErrTruncatedRecord` to §9 (2.1, 2.2).
6. Add SPEC-003 and SPEC-006 to SPEC-002 front matter (2.5).
7. Reconcile `row_count` width (2.4) — pick `uint32`.
8. Decide `Nullable`/`AutoIncrement` column trailer (2.3) — match live impl or strip it.
9. Collapse duplicate `schema_version` storage (2.7 / 4.1).
10. Promote `AppendMode` into §6.4 (2.10).
11. Add BSATN name disclaimer in §3.3 (3.1).
12. Name the CommittedState bulk-restore API in SPEC-001 Story 8.3/8.4 and reference it from Story 6.4 (2.6).
13. Everything else (nits and DIVERGE notes).

---

## 8. Spec-to-code drift (follow-up, not this pass)

Live `commitlog/` is ahead of the spec in several places. After the spec fixes above land, reconcile:

- `DurabilityWorker.WaitUntilDurable` (`durability.go:181`) — not in §4.2. Either add to spec or remove from impl.
- `OpenAndRecoverDetailed` / `RecoveryResumePlan` (`recovery.go:11,30`) — normative mechanism for §6.4's fresh-next-segment resume; spec says "MAY".
- `advanceRecoveredSequences` (`recovery.go:73`) — addresses SPEC-001 §2.1 gap silently; no doc.
- Per-column Nullable/AutoIncrement trailer (`snapshot_io.go:87`) — three bytes vs spec's one.
- `row_count uint32` (`snapshot_io.go:332`) — vs spec `uint64`.
- `nextID` section between sequences and tables (`snapshot_io.go:308–319`) — not in §5.2.
- `EnqueueCommitted(txID uint64, ...)` vs spec `(txID TxID, ...)` — downgrade to bare uint64 throughout.

Recommend: fix the spec first (above), then a single drift-reconciliation pass to either upstream live behavior into the spec or realign impl.

---

# SPEC-003 — Transaction Executor

Audited files:

- `docs/decomposition/003-executor/SPEC-003-executor.md`
- `docs/decomposition/003-executor/EPICS.md`
- `docs/decomposition/003-executor/epic-{1..7}/EPIC.md`
- All `story-*.md` under those epics

SpacetimeDB reference: `reference/SpacetimeDB/crates/{core/src/{host,db,subscription,util},datastore/src/{locking_tx_datastore,system_tables,traits.rs}}` (executor, reducer dispatch, scheduler, lifecycle, durability handoff).

Live implementation cross-read: `executor/*.go` (substantially implemented; used to spot drift, not graded as a code audit).

---

## 1. Critical

### 1.1 [CRITICAL] Commit signature is contradicted three ways inside SPEC-003 alone

SPEC-001 audit §1.3 flagged that SPEC-001 §5.6 (`Commit(...) (*Changeset, TxID, error)`) disagrees with §6.1 ("TxID is defined in SPEC-003 §6"). SPEC-003 repeats the confusion rather than picking a model:

- `SPEC-003-executor.md` §6: "The executor receives `maxAppliedTxID` at startup and initializes its internal counter to `maxAppliedTxID + 1`. The next committed transaction receives `maxAppliedTxID + 1`." → executor allocates (Model A).
- `SPEC-003-executor.md` §13.2: "The executor stores this value and increments it atomically on each successful commit to assign the next TxID." → Model A.
- `SPEC-003-executor.md` §4.4 code snippet: `changeset, txID, commitErr := store.Commit(committed, tx, schema)` — 3-return, store allocates (Model B).
- `SPEC-003-executor.md` §13.1 exported interface: `func Commit(...) (*Changeset, TxID, error)` — Model B.
- `story-4.3-commit-path.md` Deliverables code: `changeset, commitErr := store.Commit(...); txID := e.nextTxID; e.nextTxID++; changeset.TxID = txID` — 2-return, executor allocates (Model A).
- `SPEC-001-store.md` §5.6 and §13.1 still ship the 3-return signature.
- Live `executor/executor.go:384` calls `store.Commit(e.committed, tx)` — 2-return, Model A.

So §6/§13.2 and Story 4.3 + live impl all pick Model A, while §4.4, §13.1, and the downstream SPEC-001 contract they cite pick Model B. Either model works, but three specs cannot disagree.

Fix: pick Model A (matches the live impl and matches SpacetimeDB, where `commit_tx` returns `TxOffset` but Shunter already decided to have the executor own the counter because `max_applied_tx_id` is handed to it at recovery). Then:
- rewrite SPEC-003 §4.4 pseudocode to `changeset, commitErr := store.Commit(committed, tx, schema)`, `txID := e.nextTxID; e.nextTxID++; changeset.TxID = txID`;
- rewrite SPEC-003 §13.1 `Commit` signature to 2-return;
- coordinate with the SPEC-001 audit §1.3 quick-win — SPEC-001 §5.6 + §13.1 must drop the TxID return and state "caller supplies TxID via `changeset.TxID` or a separate set step."

If the project prefers Model B instead, then SPEC-003 §6 and §13.2 must stop claiming the executor is the allocator, and Story 4.3 must flip to `changeset, txID, err := store.Commit(...)`. Either way, one model, one signature, everywhere.

### 1.2 [CRITICAL] Scheduled-reducer firing has no defined carrier for `schedule_id` / `IntendedFireAt`

- `SPEC-003-executor.md` §9.4 says "the scheduler enqueues an internal reducer call into the executor inbox" and that the executor then deletes or advances the `sys_scheduled` row in the same transaction.
- `story-6.3-timer-wakeup.md` shows the enqueued `CallReducerCmd{Request: ReducerRequest{ReducerName, Args, Source, Caller}}` — **no `schedule_id` and no intended fire time** in the request.
- `story-6.4-firing-semantics.md` requires the executor to (a) look up the correct `sys_scheduled` row (by `schedule_id`) and (b) advance `next_run_at_ns = intended_fire_time + repeat_ns` (fixed-rate, not "now+interval").
- Neither field exists in `ReducerRequest` as specified by `story-1.2-reducer-types.md` / SPEC-003 §3.3.
- Live `executor/reducer.go:25-27` adds `ScheduleID` and `IntendedFireAt int64` to `ReducerRequest` to make this possible.

Story 6.4 cannot be implemented against the Story 1.2 / §3.3 `ReducerRequest` shape. An implementer reading only the docs has no way to know which `sys_scheduled` row fired or what intended fire time to advance from. Worse, "fixed-rate from intended fire time" is silently impossible without the `IntendedFireAt` value — the timer knows it, the executor never sees it.

Fix: one of
- (a) add `ScheduleID ScheduleID` and `IntendedFireAt int64` to `ReducerRequest` (Story 1.2 + §3.3) and document that they are populated iff `Source == CallSourceScheduled`;
- (b) introduce a dedicated `ScheduledCallCmd` command type that carries the schedule metadata alongside the reducer request, and have Story 6.3 enqueue it instead of `CallReducerCmd`.

Live impl already went with (a). Bring the spec into agreement.

### 1.3 [CRITICAL] DurabilityHandle contract mismatches both §7 and SPEC-002

SPEC-003 §7 and `story-1.4-subsystem-interfaces.md` define:

```go
type DurabilityHandle interface {
    EnqueueCommitted(txID TxID, changeset *Changeset)
    DurableTxID() TxID
    Close() (TxID, error)
}
```

- SPEC-002 audit §2.9 flagged that the live `commitlog/durability.go:181` adds `WaitUntilDurable(txID TxID) <-chan TxID` and that SPEC-002 §4.2 needs to either adopt it or declare polling as the contract.
- Live `executor/interfaces.go:19-22` defines its own `DurabilityHandle` as `{EnqueueCommitted, WaitUntilDurable}` — no `DurableTxID`, no `Close`. Live `executor/executor.go:455-457` calls `e.durability.WaitUntilDurable(txID)` inline in the post-commit pipeline, feeding the returned channel into `PostCommitMeta.TxDurable` for SPEC-004's confirmed-read gating.

So the doc says `DurableTxID` + `Close`, the live impl exposes neither on the executor side and uses `WaitUntilDurable` instead, and the feature that actually depends on it (subscription confirmed-read gating, per SPEC-004 E6 in `REMAINING.md`) is hard-wired in a way SPEC-003 does not describe. Ripple effect: an implementer picking up SPEC-003 Epic 5 and the Story 5.1 pipeline cannot produce a correct post-commit ordering because the spec doesn't mention `TxDurable` channels, doesn't mention `PostCommitMeta`, and doesn't explain that subscription evaluators need an ack path.

Fix:
- add `WaitUntilDurable(txID TxID) <-chan TxID` to §7 and Story 1.4;
- drop `Close() (TxID, error)` from the DurabilityHandle surface executor talks to — `Close` is an engine-lifecycle concern and §7 conflates it with the hot-path interface;
- document in §8 and Story 5.1 that `EvalAndBroadcast` receives a `PostCommitMeta` (or equivalent structure) carrying at minimum `TxDurable <-chan TxID` and optionally caller identification for reply routing.

Coordinate with SPEC-002 audit §2.9 so SPEC-002 §4.2 and SPEC-003 §7 agree on the same method set.

### 1.4 [CRITICAL] §5 post-commit step order contradicts Story 5.1 on snapshot acquisition timing

- `SPEC-003-executor.md` §5 enumerates post-commit steps as: (1) hand changeset to durability, (2) evaluate subscriptions against a stable committed read view, (3) hand deltas to protocol, (4) send reducer response, (5) drain dropped clients, (6) dequeue next.
- §5.2 says "The executor acquires a snapshot immediately after commit" — *before* durability handoff by a natural reading, and in any case before step 2.
- `story-5.1-ordered-pipeline.md` Deliverables step 1: `EnqueueCommitted`, step 2: `view := store.Snapshot()` — *after* durability handoff.
- Live `executor/executor.go:455-468` also acquires the view after durability handoff.

§5.2's "immediately after commit" wording, followed by §5 step 1 being durability, could be read two ways: "snapshot belongs to step 2 (between 1 and the eval)" or "snapshot is taken before durability handoff so the view represents the exact post-commit state and is not affected by durability-induced serialization." The latter matters if durability handoff could ever block past the next commit (it doesn't in v1 because the executor is single-threaded, but the doc is silent on *why* ordering is OK).

Fix: state explicitly in §5 and §5.2 that "the executor acquires the read view after `EnqueueCommitted` returns (queue admission only) and before `EvalAndBroadcast`, then closes it before the reducer response is sent." This matches Story 5.1 and the live impl, and removes the ambiguity.

### 1.5 [CRITICAL] OnDisconnect cleanup tx is an unbounded TxID sink with no defined identity / CallSource / panic handling

- `SPEC-003-executor.md` §10.4 failure path: "run a separate internal cleanup transaction that deletes the `sys_clients` row anyway".
- `story-7.3-on-disconnect.md` Failure Path step 3 says "Begin new cleanup transaction … commit cleanup transaction … run post-commit pipeline for cleanup commit."
- Live `executor/lifecycle.go:89-115` implements the same pattern: rollback reducer tx, start a fresh `store.NewTransaction`, delete row, commit, run post-commit with `source: CallSourceLifecycle`.

Several contracts are unstated:

1. **CallSource for the cleanup tx.** The cleanup is not a reducer call — no `Handler` runs — yet post-commit is invoked. Live shoehorns `CallSourceLifecycle` because that's the closest match, but the enum is supposed to describe how a reducer call was triggered, not "synthetic non-reducer cleanup commit." The spec and story do not say which value to use, so every implementer will pick differently.
2. **TxID allocation on cleanup.** A cleanup commit consumes the next TxID from the executor counter (live increments `e.nextTxID`). This is correct (clients subscribed to `sys_clients` see the delete with a real TxID), but it means an OnDisconnect failure produces **two** TxIDs — one rolled back, one committed. Story 6.1/6.4 use the sequence mechanism, so a rolled-back reducer tx that allocated a `schedule_id` and then the cleanup tx's commit leaves gaps in both TxID and ScheduleID. Spec silent.
3. **Cleanup tx panics.** If the cleanup-delete or commit itself panics, Story 7.3 says "that's an internal error but not necessarily fatal." But §5.4 says post-commit panics are always executor-fatal. The two rules collide: is a panic during the cleanup-commit's post-commit pipeline fatal or not?
4. **Re-entry from fatal state.** Once the reducer tx fails and we're running the cleanup tx, if the executor is already in fatal state (for example, a prior post-commit panic latched `e.fatal = true`), should the cleanup tx run at all? Story 7.3 and §10.4 silent. Live `executor/executor.go:208-216` short-circuits any `CallReducerCmd` when fatal but does not prevent `OnDisconnectCmd` from proceeding — meaning the cleanup commit runs even though the engine has declared itself broken.

Fix:
- define a `CallSourceSystem` (or reuse `CallSourceLifecycle`, but say so explicitly) for cleanup commits in §9–§10, and document that this source is used for any executor-synthesized write tx that is not a reducer call;
- state in §10.4 that cleanup-commit post-commit panics follow §5.4 — they are fatal — and in Story 7.3 that a failing cleanup is NOT silently dropped;
- specify the fatal-state interaction: either "once fatal, all write commands including cleanup are rejected" or "cleanup always attempts even when fatal because leaking `sys_clients` rows is worse than rejecting new writes." Pick one.

---

## 2. Gaps

### 2.1 [GAP] `init` lifecycle reducer is absent

- `SPEC-003-executor.md` §10 defines `LifecycleOnConnect` and `LifecycleOnDisconnect` only.
- `story-1.1-foundation-types.md` `LifecycleKind` enum: `LifecycleNone`, `LifecycleOnConnect`, `LifecycleOnDisconnect`.
- SpacetimeDB has a third lifecycle reducer, `Init`, invoked exactly once during database initialization before any `client_connected` can fire (`reference/SpacetimeDB/crates/core/src/host/module_host.rs:508-535`, per the reference survey).
- Shunter also has no "module first-boot" hook defined elsewhere. Either `init` is intentionally scoped out, or it was lost.

Fix: state the decision in §10 — either "Shunter v1 omits `init`; applications that need one-time bootstrap must use a normal reducer triggered by deployment scripts" or "`init` is reserved and will be added in v2." Add a parallel line in EPIC 7 scope. Without it, a SPEC-006 author adding schema-migration reducers will not know whether to expect a runtime `init` path.

### 2.2 [GAP] Dangling-client cleanup on restart is undefined

- SpacetimeDB tracks `ConnectedClients` after a crash and invokes `OnDisconnect` for each stale `st_clients` row during module startup (`crates/core/src/db/relational_db.rs:86-95`).
- `SPEC-003-executor.md` §10 and Stories 7.1–7.4 describe OnConnect / OnDisconnect only in the live protocol-layer path. No mention of what happens to `sys_clients` rows that survived a crash.
- Story 6.5 covers startup replay for `sys_scheduled`. No parallel story for `sys_clients`.

Consequence: after a crash, connections that were live at crash time stay in `sys_clients` indefinitely. Applications subscribed to `sys_clients` see phantom "connected" clients until a human cleans up manually, and OnDisconnect never fires for them (so application-owned state tied to those rows leaks too).

Fix: add a story to Epic 7 (or a new Epic 7.5) covering "on executor startup, iterate surviving `sys_clients` rows and invoke OnDisconnect for each," and cross-reference SPEC-002 recovery ordering (replay must complete before this sweep runs, and SPEC-003 Epic 3 startup ordering must place this between recovery and first-accept).

### 2.3 [GAP] Typed-adapter error mapping unowned

- `story-4.2-execute-phase.md` Acceptance: "Typed-adapter argument decode failures from SPEC-006 surface as ordinary reducer errors for rollback/status mapping."
- `story-4.4-rollback-and-failure.md` Deliverables: "Typed-adapter argument decode failures from SPEC-006 are treated as ordinary reducer errors in this story's status mapping."
- `SPEC-003-executor.md` §3.1: "SPEC-006 may provide typed registration helpers that decode arguments into Go structs and re-encode return values, but the executor runtime contract is byte-oriented."

The contract is stated three times but the actual error shape — what a typed adapter returns to the executor, whether it wraps with a specific sentinel (`ErrArgsDecode`?), whether the user sees the raw underlying error or something normalized — is nowhere defined. SPEC-006 is "depended on by" SPEC-003, so the responsibility for the sentinel naming falls on whichever spec the adapter lives in.

Fix: either (a) add `ErrReducerArgsDecode` to the SPEC-003 §11 catalog and state "typed adapters wrap decode failures with this sentinel; executor Story 4.4 classifies it as `StatusFailedUser`"; or (b) defer to SPEC-006 and add a one-liner in §3.1 "the sentinel for typed-adapter decode failures is defined in SPEC-006; SPEC-003 treats any non-nil handler error as `StatusFailedUser` regardless of sentinel identity." Pick one.

### 2.4 [GAP] Scheduler → executor wakeup ordering is inconsistent across stories

- `story-6.2-transactional-schedule.md` Design Notes: "`timerNotify` is called after the surrounding transaction commits (not during Schedule/Cancel). The post-commit pipeline or the executor itself calls it."
- `story-6.3-timer-wakeup.md` Design Notes: "Notify is called after each commit by the post-commit pipeline. This ensures newly created schedules are picked up promptly."
- `story-5.1-ordered-pipeline.md` and `story-5.2-dropped-client-drain.md` enumerate post-commit steps (durability, snapshot, eval, close view, response, drain dropped clients). **No step calls `Scheduler.Notify()`**.
- Live `executor/scheduler.go` defines `schedulerHandle.timerNotify` as a field but the code paths around schedule insertions never populate it (`executor/scheduler.go:61-67` returns a handle with no `timerNotify` set, `insertSchedule` never calls one). So in the current impl, a newly-inserted sys_scheduled row is only picked up when the worker rescans on its next timer tick.

Either the docs overstate what happens (there is no post-commit `Notify`), or the post-commit story is missing a step, or both. The semantic matters: a `Schedule(at: now+10ms)` call should fire in ~10ms, but without `Notify` the worker may sleep past the intended time and fire tens of seconds late depending on its current wakeup schedule.

Fix: add step "7. Notify scheduler if changeset touched `sys_scheduled` (non-blocking send to `Scheduler.Notify()`)" to Story 5.1 and §5, OR remove the notify claims from Stories 6.2/6.3 and document explicitly "schedules inserted in this transaction will be picked up on the next timer rescan (up to one scan interval of latency)."

### 2.5 [GAP] Startup orchestration owner unspecified

- `story-6.5-startup-replay.md` step 1: "Called during executor startup, after recovery but before accepting external commands."
- `story-3.1-executor-struct.md` `NewExecutor(cfg, registry, store, durability, subs, recoveredTxID)` does not take a scheduler.
- Epic 3 Story 3.2 `Run(ctx)` processes commands immediately.

Who owns the sequence "NewExecutor → Scheduler.ReplayFromCommitted → Scheduler.Run goroutine → Executor.Run → first-accept"? No story names it. If external protocol-layer code calls `e.Submit` before replay has enqueued past-due scheduled commands, those commands will be processed out-of-order relative to past-due schedules (schedules will land after the first external reducer).

Fix: name the orchestration owner. Either (a) NewExecutor's contract says "after construction, caller MUST call `ReplayFromCommitted` before calling `Run`" and Story 3.1 adds that to Design Notes; or (b) add a new Story 6.6 "engine-level startup sequence: recovery → executor construction → scheduler replay → scheduler.Run → executor.Run → first-accept." Same concern applies to the §2.2 dangling-client sweep from 2.2 above.

### 2.6 [GAP] OnConnect / OnDisconnect command identity conflicts with §2.4 single-command model

- `SPEC-003-executor.md` §2.4: "Scheduled reducers and lifecycle reducers use `CallReducerCmd` with an internal call source."
- `story-7.2-on-connect.md` Deliverables: `func (e *Executor) handleOnConnect(connID ConnectionID, identity Identity)` — a bespoke handler, not a `CallReducerCmd` dispatch path.
- `story-7.3-on-disconnect.md` same — bespoke `handleOnDisconnect`.
- Live `executor/command.go:61-79` defines `OnConnectCmd` and `OnDisconnectCmd` as separate `ExecutorCommand` types with their own dispatch arms in `executor/executor.go:227-230`.

§2.4 says "no special command types needed" — live impl and the two stories both contradict it. The difference is substantive: the insert-row-then-run-reducer flow (§10.3) is not expressible as a single `CallReducerCmd` because the insert happens BEFORE the reducer runs, and `handleCallReducer` has no hook for "pre-reducer insert synthetic row."

Fix: update §2.4 and Story 1.3 to add `OnConnectCmd` and `OnDisconnectCmd` to the command set, or — if single-command is the real design — spell out in §10.3/§10.4 how a `CallReducerCmd` with `CallSourceLifecycle` drives the insert/delete synthesis. Live picked the former; the spec should either match or push back.

### 2.7 [GAP] No pre-handler scheduled-row validation on firing

- `story-6.4-firing-semantics.md` step 2 "Execute reducer handler" happens before step 3 "delete/advance `sys_scheduled` row". Live `executor/executor.go:372-381` confirms the order: reducer runs first, then `advanceOrDeleteSchedule`, then commit.
- Live's `advanceOrDeleteSchedule` tolerates a missing row — "concurrent Cancel raced the firing — the reducer still commits (at-least-once semantics)" (`executor/scheduler.go:35`).
- Story 6.4 does not document this race. It also does not document: what happens if the `reducer_name` in `sys_scheduled` is no longer registered (Story 6.3 looks up by name via executor registry; `handleCallReducer` returns `ErrReducerNotFound`; the schedule row remains and will fire again next tick, infinite loop). What happens if the `args` in the row are no longer decodable against the current typed adapter.

Fix: Story 6.4 should enumerate the edge cases the firing pipeline must handle:
- schedule row missing at firing time → reducer still runs, mutation is a no-op, commit proceeds;
- reducer name not in registry → respond `ErrReducerNotFound`, rollback, **delete the row anyway** (otherwise the scheduler loops on it forever) — OR mark it as quarantined and require manual intervention;
- typed-adapter decode failure → same treatment as above (user error, but how do we stop the retry loop?).

Pick a policy and state it.

### 2.8 [GAP] `Schedule` / `ScheduleRepeat` "first fire" timing disagreement

- `SPEC-003-executor.md` §9.3: `ScheduleRepeat(reducerName string, args []byte, interval time.Duration) (ScheduleID, error)` — no initial-delay parameter.
- `story-6.2-transactional-schedule.md` ScheduleRepeat: "`next_run_at_ns` = now + interval (or first fire time)" — parenthetical hints at an ambiguity the story never resolves.
- Live `executor/scheduler.go:93`: "first := time.Now().Add(interval).UnixNano()" — first fire is exactly `now + interval`.

A user wanting "fire every 5s starting immediately" cannot express it with this API. A user wanting "fire every 5s starting in 30s" also cannot. Fine for v1 — but the "(or first fire time)" parenthetical in Story 6.2 should be deleted or turned into a real `ScheduleRepeatAt(reducerName, args, firstFire time.Time, interval time.Duration)` overload.

Fix: Story 6.2 should say "first fire is `now + interval`. A variant that takes a separate first-fire timestamp is deferred; document here if/when it's added." Either remove the parenthetical or turn it into a concrete API.

### 2.9 [GAP] `Rollback` is not in the SPEC-001 contract listed by §13.1

- `SPEC-003-executor.md` §13.1 lists only `NewTransaction`, `Commit`, and `(cs *CommittedState) Snapshot()`.
- `story-4.4-rollback-and-failure.md` says "Rollback is implicit: transaction-local state is garbage collected. No explicit undo log or compensating writes."
- Live `executor/executor.go:343, 358, 374, 386` calls `store.Rollback(tx)` explicitly. SPEC-001 §5.6 defines `Rollback(tx)` as a required method.

If Rollback is required on the store side (SPEC-001 does require it — the call must drop any per-tx sequence allocations, index rollbacks, etc.) then SPEC-003 §13.1 must list it. Story 4.4's "implicit rollback" description misleads an implementer who reads only SPEC-003: they may skip `store.Rollback(tx)` calls and leak tx-local state across reducer failures.

Fix: add `Rollback(tx *Transaction)` to §13.1 and mention it explicitly in Story 4.4 Deliverables ("discard transaction = `store.Rollback(tx)`; do not rely on GC alone").

### 2.10 [GAP] `ErrReducerNotFound` status classification is inconsistent

- `story-1.5-error-types.md` defines the sentinel.
- `story-4.1-begin-phase.md` step 2: "Look up reducer in registry → if not found, respond `ErrReducerNotFound`, return" — no status specified.
- Live `executor/executor.go:307-310` responds with `StatusFailedInternal` for unknown reducer name.
- An unknown reducer name is a user-facing concern (client typo'd the name, wrong schema on client) — `StatusFailedUser` is the closer match. `StatusFailedInternal` is for engine faults.

Fix: Story 4.1 should name the status (`StatusFailedUser`) and live impl should align. Same treatment for `ErrLifecycleReducer`.

### 2.11 [GAP] Inbox close-vs-shutdown-flag race not described

- `story-3.5-shutdown.md` steps: Shutdown → set flag → close inbox → wait for Run to finish.
- `story-3.3-submit-methods.md` Submit: checks `shutdown` flag, then sends on the channel.
- Between the flag check and the channel send, another goroutine can observe the flag as unset, try to send, and `Shutdown` can close the channel — producing a send-on-closed-channel panic in the caller goroutine.
- Live `executor/executor.go:132-149` uses `submitMu sync.RWMutex` (Submit holds the RLock, Shutdown holds the WLock during close). This solves the race but the mechanism is not in the story.

Fix: Story 3.3 / 3.5 must specify the synchronization (RWMutex, or atomic + recover, or a ChannelClose-via-context) that makes the flag-then-send race safe. Otherwise a naive implementer will ship the race.

### 2.12 [GAP] No guidance for the scheduler-response dump channel

- Live `executor/scheduler.go:43` uses a `respCh chan ReducerResponse` drained by a background goroutine so scheduled-reducer responses go somewhere (no external caller holds a ResponseCh for scheduled commands).
- `story-6.3-timer-wakeup.md` enqueues `CallReducerCmd{ResponseCh: internalResponseCh}` without specifying what `internalResponseCh` is, who drains it, and what happens if the drain goroutine dies.
- If the response channel is nil, live `executor/executor.go:284-290` treats the send as a no-op — but the live scheduler explicitly uses a buffered drain channel, suggesting someone realized nil-channel sends would be a silent swallow that loses error information.

Fix: Story 6.3 must either (a) specify a dedicated per-scheduler drain (the live pattern), or (b) document "pass nil ResponseCh; executor silently swallows response; log any non-success status from the post-commit path instead." The ambiguity leaves the error-visibility story undefined.

---

## 3. Divergences from SpacetimeDB (should be documented)

### 3.1 [DIVERGE] Fixed-rate repeat semantics vs SpacetimeDB's explicit-reschedule model

- `SPEC-003-executor.md` §9.5: repeating schedules advance `next_run_at_ns = intended_fire_time + repeat_ns` automatically; v1 does not use "completion time plus interval."
- SpacetimeDB: one-shot rows are deleted before firing; repeats are opt-in — the reducer must explicitly request a reschedule, which re-inserts the row (per reference survey, `reference/SpacetimeDB/crates/core/src/host/scheduler.rs:352-358, 448`).

Shunter's model is simpler (periodic cron-like behavior without reducer cooperation) and fine for SodorYard. Worth a one-liner in §9 saying "Unlike SpacetimeDB, Shunter's `ScheduleRepeat` is system-managed: the reducer does not need to re-register. A scheduled reducer that wants to stop its own repeat calls `Cancel` on its own `schedule_id`."

### 3.2 [DIVERGE] Unbounded reducer dispatch queue vs bounded inbox

- `SPEC-003-executor.md` §2.2: "`inbox` MUST be bounded."
- SpacetimeDB: reducer dispatch uses an unbounded mpsc channel (per reference survey, `crates/core/src/util/jobs.rs:291`).

Shunter's bounded inbox + optional `ErrExecutorBusy` reject policy is a deliberate tightening. Worth a one-liner in §2.2 or §12 acknowledging the divergence: "SpacetimeDB's executor uses an unbounded dispatch queue. Shunter bounds the inbox to make OOM-under-flood impossible at the cost of explicit backpressure or caller rejection."

### 3.3 [DIVERGE] Server-stamped timestamp at dequeue vs supplied-at-call

- `SPEC-003-executor.md` §3.3: "The executor, not the caller, sets `Caller.Timestamp` when the command is dequeued. Caller-provided timestamps must be ignored."
- SpacetimeDB: the caller stamps `Timestamp::now()` at submit/enqueue time (per reference survey, `crates/core/src/host/module_host.rs:618-627`).

Shunter's dequeue-time stamping is the correct model for ordering (timestamp monotonically agrees with inbox order), which SpacetimeDB does not strictly guarantee. Worth a one-liner in §3.3 — this is a real semantic upgrade over SpacetimeDB, not just a code style choice, and downstream specs (SPEC-005 logs, replay) may rely on it.

### 3.4 [DIVERGE] Post-commit failure is *always* fatal vs per-step recoverability

- `SPEC-003-executor.md` §5.4 and `story-5.3-fatal-state.md`: any panic in durability handoff, snapshot, evaluation, or delta handoff latches the executor into fatal state forever.
- SpacetimeDB: post-commit durability failures generally crash the module host; subscription-broadcast failures can log-and-continue per-subscriber without module crash.

Shunter's harsher policy is defensible for v1 (simpler invariants, no half-broadcast states) but means that a buggy subscription evaluator kills the entire executor for a transient failure. Worth stating in §5.4 "v1 treats every post-commit panic as fatal; partial recovery (e.g. skip one subscriber, continue) is out of scope."

### 3.5 [DIVERGE] Shunter owns `init` semantics via "no init"

See 2.1 above. Listed here as well because the absence is itself a divergence that should be documented, not just a gap.

### 3.6 [DIVERGE] Scheduled-row mutation atomic with reducer writes vs SpacetimeDB's pre-fire delete

- Shunter deletes/advances the `sys_scheduled` row in the same transaction as the reducer's writes, so if the reducer rolls back, the row stays pending for retry.
- SpacetimeDB deletes one-shot rows **before** firing the reducer (per reference survey, `crates/core/src/host/scheduler.rs:448`), so a failing reducer loses its one-shot slot.

Shunter's choice is objectively better (true at-least-once with retry) but less efficient (a reducer loop retrying on every rescan). Worth a line in §9.4 "Shunter retries failed scheduled reducers indefinitely until success, `Cancel`, or manual row removal. A misbehaving scheduled reducer will consume executor time on every rescan."

---

## 4. Internal consistency

### 4.1 [NIT] SPEC-003 front matter misdeclares SPEC-002 as "depended on by"

- `SPEC-003-executor.md` header: "Depends on: SPEC-001, SPEC-004, SPEC-006. **Depended on by:** SPEC-002, SPEC-005."
- SPEC-003 §7 and Story 1.4 define `DurabilityHandle` as an interface SPEC-002 implements. So SPEC-002 is also a **runtime dependency** of SPEC-003 — the executor cannot run without one. Without a back-edge in the header, dep-graph readers miss the relationship.
- Also: SPEC-003 §8 and Story 1.4 depend on `SubscriptionManager` (SPEC-004), matching "Depends on: SPEC-004". But `SubscriptionRegisterRequest` and `CommittedReadView` from SPEC-001 are also passed through — OK, already listed.

Fix: change header to "Depends on: SPEC-001, SPEC-002 (DurabilityHandle), SPEC-004, SPEC-006. Depended on by: SPEC-005." SPEC-002's dep is bidirectional (SPEC-002 consumes `TxID` from SPEC-003; SPEC-003 consumes `DurabilityHandle` from SPEC-002) but that's a solvable circular via interface declaration in whichever spec lands first.

### 4.2 [NIT] `CallerContext.Timestamp` type vs SPEC-005 wire format

- `story-1.2-reducer-types.md`: `Timestamp time.Time`.
- SPEC-005 logs and wire messages will serialize this. `time.Time` has a monotonic reading whose value depends on process start; serializing round-trips only the wall clock. Story should mention "for serialization and durability, only the UTC wall-clock portion is meaningful; monotonic readings are stripped."

Minor, but silence here can bite when replay stamps start diverging from expectations.

### 4.3 [NIT] §11 error catalog omits sentinels implied by stories

- Story 4.4 wraps commit failures with `ErrCommitFailed` (sentinel) via `%w`. Good — listed.
- `ErrReducerArgsDecode` implied by Stories 4.2/4.4 but not in §11 (see 2.3).
- No sentinel for "`SchedulerHandle` used after the surrounding reducer returned." Story 1.4 says "handle is per-call"; if an application captures it and calls it later, the contract is undefined.
- No sentinel for "schema changed under a running executor." Story 2.1 says "immutable after executor start" but frozen registry only affects reducers, not schema.

Fix: either add sentinels for the constraints the spec claims to enforce, or state in Story 1.4 and Story 2.1 that "contract violations are programming errors; no sentinel is provided because detection is out of scope."

### 4.4 [NIT] `Executor` struct names `store` but §13.1 names `CommittedState`

- `story-3.1-executor-struct.md`: `store *CommittedState`.
- Live `executor/executor.go:37`: `committed *store.CommittedState`.
- SPEC-001 §5 uses the name `CommittedState`.

Pick one ("store" vs "committed") in story prose so the impl does not have to swap idioms.

### 4.5 [NIT] `SubscriptionManager.Register` read-view ownership

- `SPEC-003-executor.md` §8 and `story-1.4-subsystem-interfaces.md`: `Register(req, view CommittedReadView) (result, error)`.
- `story-3.4-command-dispatch.md` step "Close snapshot" implies caller owns the view.
- Story 1.4 does not say whether SubscriptionManager may retain the view past the call. If it retains (for lazy evaluation), `view.Close()` in Story 3.4 would invalidate retained state.

Fix: one-liner in §8: "view lifetime is the Register call; SubscriptionManager MUST NOT retain `view` past return. Any snapshot-derived state Register wants to keep must be copied."

### 4.6 [NIT] `Executor.fatal` lock scope vs struct declaration

- `story-3.1-executor-struct.md` field: `fatal bool`.
- `story-5.3-fatal-state.md` sets it inside a `defer recover`; Story 3.3 `Submit` reads it without a lock.
- Live `executor/executor.go:40` uses `fatal atomic.Bool` to make concurrent reads safe.

Fix: Story 3.1 should declare `fatal atomic.Bool` (or equivalent) rather than raw `bool`, so Story 3.3's lock-free read is unambiguously correct.

### 4.7 [NIT] `ScheduleID` and `SubscriptionID` are defined in Story 1.1 but never reference SPEC-005 / SPEC-001 homes

- Live `executor/types.go:8,13` aliases them to `types.ScheduleID` and `types.SubscriptionID` defined in a `types` package.
- Story 1.1 defines them as executor-package types.
- SPEC-005 owns the wire format; SPEC-003 re-declaring the type is fine, but the cross-link should say "name SPEC-005 §... for the wire byte layout."

Fix: Story 1.1 Design Notes: add one line per ID type saying which spec owns its wire format / canonical shape.

### 4.8 [NIT] Performance section title mirrors SPEC-001 audit §4.4

Same issue: §12 is titled "Performance Constraints" and the first line demotes to "engineering targets, not correctness requirements." Rename to "Performance Targets" for consistency with SPEC-001 §10 and to avoid contractual-reading risk.

### 4.9 [NIT] Story 1.3 `ResponseCh` on every command

- Every command type in Story 1.3 has a `ResponseCh`. Scheduled reducers (Story 6.3) use a drain channel; lifecycle reducers (Stories 7.2/7.3) sometimes do. Some commands (internal cleanup) may not need a ResponseCh at all.
- Live `executor/executor.go:284-289` `sendReducerResponse` accepts nil. Story 1.3 does not describe nil-ResponseCh semantics.

Fix: Story 1.3 Design Notes: "nil ResponseCh is permitted; executor silently drops the response. Callers that need delivery guarantees MUST supply a channel."

---

## 5. Epic/story coverage

### 5.1 Verified good coverage

- Epic 1 covers §2.2–§2.4, §3.1–§3.3, §6, §7, §8, §9.3, §11 (types + interfaces).
- Epic 2 covers §3.2 and §10.1 (registry + lifecycle reservation + freeze).
- Epic 3 covers §2.1–§2.5 and §4.1 (inbox, run loop, dispatch, shutdown).
- Epic 4 covers §3.4, §3.5, §4.2–§4.6 (begin → execute → commit → rollback).
- Epic 5 covers §5.1–§5.4 (durability handoff, snapshot, eval, drain, fatal).
- Epic 6 covers §9 (sys_scheduled, handle, timer, firing, replay) — gaps flagged in 1.2, 2.4, 2.7, 2.8, 2.12.
- Epic 7 covers §10 — gaps flagged in 1.5, 2.2, 2.6.

### 5.2 [GAP] No story owns `max_applied_tx_id` hand-off from SPEC-002

Mentioned in §6 and §13.2 but no Epic 3 story names the integration point (recovered value in, counter initialized, first-accept gated on initialization). Story 3.1 mentions `recoveredTxID` as a constructor arg. Who extracts it from SPEC-002's `OpenAndRecover`? See 2.5.

### 5.3 [GAP] No story owns the dangling-client sweep on startup

See 2.2.

### 5.4 [GAP] No story owns read-routing documentation placement

Story 3.4 Deliverables mentions "Executor/package docs on read routing" and the live code has godoc comments describing it. Fine, but the actual decision ("what IS atomic vs observational") is a §2.5 concern and needs one place to live. Currently scattered across §2.5, Story 3.4 Design Notes, and the unspecified "Executor/package docs".

### 5.5 [GAP] No story on reducer/schema registration ordering at engine-boot

Story 2.2 says "Freeze before executor construction." Story 3.1 panics if not frozen. But who orchestrates: schema → SPEC-006 register reducers → freeze → NewExecutor → scheduler replay → dangling-client sweep → Run? Same question as 2.5.

---

## 6. Clean-room boundary

Overall: the SPEC-003 decomposition is prose- and Go-typed; no Rust identifiers or verbatim SpacetimeDB names appear. Type and method names (`Executor`, `ReducerContext`, `ReducerHandler`, `ReducerRegistry`, `ExecutorCommand`, `CallReducerCmd`, `DurabilityHandle`, `SubscriptionManager`, `SchedulerHandle`, `sys_scheduled`, `sys_clients`, `OnConnect`, `OnDisconnect`, `CallSource`, `ReducerStatus`, `LifecycleKind`) are idiomatic Go. They conceptually parallel SpacetimeDB's Rust surface but use different names, different granularity, and different concurrency models.

Concept → name map against reference:

- `ModuleHost::call_reducer` / `CallReducerParams` → `Executor` + `ReducerContext` + `CallReducerCmd` (single-goroutine vs Tokio LocalSet — structurally different).
- `ReducersMap` → `ReducerRegistry` (same idea, different lookup story).
- `Durability::request_durability` → `DurabilityHandle.EnqueueCommitted` (same contract at the interface level; see 1.3 for the hot-path divergence).
- `st_scheduled` → `sys_scheduled` (name changed — good).
- `st_clients` → `sys_clients` (name changed — good).
- `init` / `client_connected` / `client_disconnected` → `OnConnect` / `OnDisconnect` (init dropped; see 2.1/3.5).
- `SchedulerActor` + `DelayQueue` → `Scheduler` goroutine + `time.Timer` + wakeup channel.
- `commit_tx` returning `TxOffset` → `store.Commit` + executor-owned TxID counter (see 1.1).

No doc cites reference file paths; no Rust identifiers leaked; no copied prose detected.

One clean-room note from earlier audits applies here too: the `BSATN` name disclaimer SPEC-002 audit §3.1 recommends should also cover SPEC-003 §3.1, §3.2, and Story 1.2 which all reference "BSATN-encoded reducer arguments." The decision to keep or rename "BSATN" needs to land once and propagate; SPEC-003 should not be the last spec to get the disclaimer.

---

## 7. Quick wins (suggested ordering for doc repair)

1. Pick one Commit signature (1.1) — coordinated edit across SPEC-001 §5.6/§13, SPEC-003 §4.4/§13.1, Story 4.3. Two-hour doc pass, closes the SPEC-001 audit §1.3 carry-over.
2. Add `ScheduleID` and `IntendedFireAt` to `ReducerRequest` (1.2) — Story 1.2 + §3.3 update.
3. Reconcile DurabilityHandle with live impl (1.3) — add `WaitUntilDurable`, drop `Close` from §7, define `PostCommitMeta` or equivalent in §8, coordinate with SPEC-002 audit §2.9.
4. Clarify post-commit snapshot timing (1.4) — two-sentence edit in §5.2 and Story 5.1.
5. Resolve OnDisconnect cleanup tx semantics (1.5) — CallSource, TxID counter, panic handling, fatal-state interaction. Medium edit in §10.4 + Story 7.3.
6. Decide on `init` lifecycle (2.1) — one paragraph in §10 either adopting it or declaring it out of scope.
7. Add dangling-client sweep story (2.2) — new Epic 7 story.
8. Add `OnConnectCmd` / `OnDisconnectCmd` to the command set (2.6) — Story 1.3 + §2.4 edit.
9. Fix Scheduler.Notify flow (2.4) — add step 7 to §5 / Story 5.1 or remove the notify claims.
10. Add startup orchestration story (2.5) — new Epic 3 or Epic-cross story.
11. Add `store.Rollback` to §13.1 (2.9).
12. Name typed-adapter decode error owner (2.3).
13. Define scheduled-firing edge cases (2.7).
14. Everything else (nits).

---

## 8. Spec-to-code drift (follow-up, not this pass)

Live `executor/` is generally ahead of the spec. After the spec fixes above land, reconcile:

- `ReducerRequest.ScheduleID` + `IntendedFireAt` (`executor/reducer.go:25-27`) — not in §3.3 / Story 1.2 (1.2).
- `OnConnectCmd` / `OnDisconnectCmd` distinct command types (`executor/command.go:61-79`) — contradicts §2.4 (2.6).
- `DurabilityHandle.WaitUntilDurable` (`executor/interfaces.go:21`) — not in §7; `DurableTxID` / `Close` in §7 are not in the live interface (1.3).
- `SubscriptionManager.EvalAndBroadcast(..., meta PostCommitMeta)` (`executor/interfaces.go:36`) — spec signature has no `meta` param.
- Executor-owned schedule-ID sequence via `store.Sequence` (`executor/executor.go:90`, `executor/scheduler.go:98`) — Story 6.1/6.2 say the ID comes from SPEC-001's autoincrement on the table; live uses a parallel in-memory allocator.
- `ReducerContext`, `CallerContext`, `ReducerHandler` defined in package `types`, not `executor` (`executor/types.go:8,13`) — Story 1.2 puts them in `executor/reducer.go`.
- `nextTxID uint64` (`executor/executor.go:39`) — Story 3.1 says `TxID`; TxID bleed consistent with SPEC-001/002 audits (SPEC-002 audit §4.2).
- `fatal atomic.Bool` (`executor/executor.go:40`) vs Story 3.1 `fatal bool` (4.6).
- `submitMu sync.RWMutex` around Submit/Shutdown (`executor/executor.go:43`) — not in Story 3.3/3.5 (2.11).
- `drainResponses` background goroutine in Scheduler (`executor/scheduler.go:206-237`) — not in Story 6.3 (2.12).
- Handle-lifetime enforcement (`SchedulerHandle` captures `*Transaction`) is not enforced at runtime; Story 1.4 claims "handle is per-call" but live does not detect post-return use.

Recommend: fix the spec first (above), then a single drift-reconciliation pass for SPEC-003 Epic 1/3/5 to realign impl and docs in lock-step with the SPEC-001/002 reconciliation already pending.

---

# SPEC-004 — Subscription Evaluator

Audited files:

- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md`
- `docs/decomposition/004-subscriptions/EPICS.md`
- `docs/decomposition/004-subscriptions/PHASE-5-DEFERRED.md`
- `docs/decomposition/004-subscriptions/epic-{1..6}/EPIC.md`
- All `story-*.md` under those epics

SpacetimeDB reference: `reference/SpacetimeDB/crates/core/src/subscription/{module_subscription_actor.rs,module_subscription_manager.rs,execution_unit.rs,delta.rs,tx.rs,query.rs}` and `reference/SpacetimeDB/crates/subscription/src/lib.rs`.

Live implementation cross-read: `subscription/*.go` (substantially implemented through Epic 6; used to spot doc drift, not graded as a code audit).

---

## 1. Critical

### 1.1 [CRITICAL] `EvalAndBroadcast` signature cannot populate the §8.1 `FanOutMessage`

- `SPEC-004-subscriptions.md` §10.1 and `epic-4-subscription-manager/story-4.5-manager-interface.md` both declare:
  ```go
  EvalAndBroadcast(txID TxID, changeset *Changeset, view CommittedReadView)
  ```
- `SPEC-004-subscriptions.md` §8.1 defines `FanOutMessage` fields `TxDurable <-chan TxID`, `CallerConnID *ConnectionID`, `CallerResult *ReducerCallResult`. The executor is the only authority with those values (TxDurable comes from SPEC-002's durability worker, caller identity comes from the originating `CallReducerCmd`).
- The `EvalAndBroadcast` signature has no carrier for any of those inputs. The evaluator therefore cannot construct a `FanOutMessage` with non-zero caller metadata or a durability channel — the only contract it accepts is `(txID, changeset, view)`.
- Live `subscription/manager.go:53` and `executor/interfaces.go:36` both add a `meta subscription.PostCommitMeta` parameter; `executor/executor.go:455-468` builds `PostCommitMeta{TxDurable: e.durability.WaitUntilDurable(txID)}` and live `subscription/eval.go:25-40` copies `meta.TxDurable`, `meta.CallerConnID`, `meta.CallerResult` into `FanOutMessage`.
- This is the downstream half of SPEC-003 audit §1.3: SPEC-003 already names `PostCommitMeta` (or equivalent) as the delivery contract; SPEC-004 is the upstream owner and never defines the type or updates the signature.

Fix: declare `PostCommitMeta` in §10.1 (fields `TxDurable <-chan TxID`, `CallerConnID *ConnectionID`, `CallerResult *ReducerCallResult`); extend the `SubscriptionManager.EvalAndBroadcast` signature and Story 4.5 interface to take `meta PostCommitMeta`; re-cite from SPEC-003 §8 / Story 1.4.

### 1.2 [CRITICAL] `SubscriptionRegisterRequest` has no client-identity field, so parameterized hashing per §3.4 is unreachable

- §3.4 defines two hash modes: non-parameterized (structure only) and parameterized ("hash of the predicate structure + client identity"). Story 1.3 implements `ComputeQueryHash(pred Predicate, clientID *Identity) QueryHash`.
- §4.1 defines `SubscriptionRegisterRequest{ConnID, SubscriptionID, Predicate, RequestID}`. Story 4.5 re-declares the same shape.
- Story 4.2 step 2 says "Compute query hash via `ComputeQueryHash`" but Register has no access to an `Identity` — `ConnectionID` is a per-connection handle, not a caller Identity.
- Live `subscription/manager.go:11-17` adds `ClientIdentity *types.Identity` to the request; live `subscription/register.go:19` passes it to `ComputeQueryHash`. `PHASE-5-DEFERRED.md` §"parameterized query-hash reachability in registration" claims this was fixed — in code — but the spec docs were not updated to match.
- SpacetimeDB's equivalent: `QueryHash::from_string(sql, identity, has_param)` in `reference/SpacetimeDB/crates/core/src/subscription/execution_unit.rs:33` — takes `identity` explicitly; Shunter's hash wants the same input but §4.1 never plumbs it in.

Fix: add `ClientIdentity *Identity` to `SubscriptionRegisterRequest` in §4.1 and Story 4.5; document in Story 4.2 step 2 that `ClientIdentity` is forwarded into `ComputeQueryHash`; clarify that a nil `ClientIdentity` produces a non-parameterized hash.

### 1.3 [CRITICAL] `FanOutMessage` shape in §8.1 omits `TxID` and `Errors`, making Story 5.1 step 5 unimplementable

- §8.1 `FanOutMessage` struct: `{TxDurable, Fanout, CallerConnID, CallerResult}`.
- §7.2 algorithm step 5: "Send `FanOutMessage{TxDurable: durableNotify, Fanout: fanout}` to `FanOutWorker.inbox`."
- §10.2 `TransactionUpdate` is `{TxID, Updates []SubscriptionUpdate}`. The fan-out worker must stamp each outgoing `TransactionUpdate` with `TxID`, but the message carrying `Fanout` does not carry `TxID`. The worker has no other source for the transaction id.
- §11.1 says evaluation errors send `SubscriptionError` "to affected clients" — but §8.1 `FanOutMessage` has no error channel. Story 5.1 acceptance criterion "evaluation error sends `SubscriptionError`" has no documented transport.
- Live `subscription/fanout.go:12-36` adds `TxID types.TxID` and `Errors map[types.ConnectionID][]SubscriptionError` to `FanOutMessage`. Live `subscription/fanout_worker.go:108-114` iterates `msg.Errors` before `msg.Fanout` during delivery.

Fix: add `TxID TxID` and `Errors map[ConnectionID][]SubscriptionError` to `FanOutMessage` in §8.1; rewrite Story 5.1 step 5 accordingly; define the in-FanOutMessage error payload as the §10.2 type (see 2.4 below for the shape).

### 1.4 [CRITICAL] §11.1 per-subscription eval-error recovery contradicts SPEC-003 §5.4 "post-commit panic is fatal"

- §11.1: "If delta computation fails for a subscription … Do **not** abort the evaluation loop — other subscriptions are unaffected." Story 5.1 acceptance: "Evaluation error for one subscription → others still evaluated."
- SPEC-003 §5.4 and `story-5.3-fatal-state.md` (see SPEC-003 audit §3.4): any panic in post-commit steps — including the subscription `EvalAndBroadcast` call — latches the executor into fatal state forever.
- Live `subscription/eval.go:235 evalQuerySafe` wraps each per-candidate evaluation in `defer recover()` and reports the panic via `SubscriptionError` + `Unregister`. The executor's outer post-commit panic recovery (`executor/executor.go`) then never fires for eval-internal panics. But §11.3 also says invariant violations (negative dedup counts, orphaned hashes, subscriber/client inconsistencies) MUST panic — those would be caught by `evalQuerySafe` too and turned into a per-query `SubscriptionError` + Unregister, which is the opposite of "bug, should be fatal."
- Two rules collide: (a) §11.3 "invariant violations are bugs, should panic", (b) §11.1 / Story 5.1 "errors are per-subscription, continue the loop", (c) SPEC-003 §5.4 "panics during post-commit are fatal." All three hold today; no reconciliation.

Fix: pick a model and document it in both SPEC-004 §11 and SPEC-003 §5.4:
- Model A: only business-logic errors are recoverable (type mismatch, corrupted index detected via explicit check); actual `recover()` is forbidden in the eval hot path. Story 5.1 "evaluation error" becomes "error return from `evalDelta`", not a recovered panic.
- Model B: `EvalAndBroadcast` owns a localized `recover()` boundary per candidate query. §11.3 invariants become logged + per-query kills, not panics. SPEC-003 §5.4 must explicitly exclude panics inside `EvalAndBroadcast` from the fatal latch.

Either way, SPEC-004 §11 must line up with SPEC-003 §5.4.

### 1.5 [CRITICAL] `SubscriptionUpdate.TableID` has no defined meaning for join subscriptions

- §10.2 `SubscriptionUpdate{SubscriptionID, TableID, TableName, Inserts, Deletes}` — one `TableID` per update.
- §6.2 / §6.3 / Story 3.3: join deltas produce concatenated rows (LHS columns ++ RHS columns). One delta row corresponds to a pair `(T1 row, T2 row)`.
- Neither §10.2 nor Story 4.5 nor Story 6.2 states which `TableID` / `TableName` the join update carries. A protocol decoder (SPEC-005) receiving a `SubscriptionUpdate` for a join would need both tables' schemas to decode `Inserts[i]` — but the message has only one `TableID`.
- Live `subscription/eval.go:268-281` picks `TableID = p.Left` and constructs `TableName = left_name + "+" + right_name`. That is a concrete choice, but it is a live-only convention.

Fix: pin the convention in §10.2 and Story 3.3/4.5. Two reasonable options:
- (a) For joins, `TableID` is `Join.Left`, `TableName` is a composite string naming both tables; the wire format must describe the joined row shape out-of-band.
- (b) For joins, emit two `SubscriptionUpdate` entries per join delta row (one per table) — but that loses the "joined" semantics.
- (c) Add a `JoinedTableIDs []TableID` field to `SubscriptionUpdate` so joined updates declare the ordered table schema.

SpacetimeDB's v2 path uses per-`query_set_id` grouping (`reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:2078-2091`) that sidesteps the single-table-id assumption. Shunter's v1 `(TableID, TableName)` model needs an explicit joined-row decoder contract.

### 1.6 [CRITICAL] Story 4.1 `subscribers map[ConnectionID]SubscriptionID` cannot hold multiple subs per connection

- Story 4.1 Deliverables:
  ```go
  queryState { ..., subscribers map[ConnectionID]SubscriptionID, refCount int }
  ```
  The map shape collapses every connection to at most one SubscriptionID per query.
- §4.1 allows a single client to register the same predicate under two different subscription IDs (request IDs echoed in `SubscribeApplied`). §7.4 / Story 5.3 memoized encoding says "Two clients with structurally identical predicates … share the same query hash" — implies a single conn can also have two such registrations.
- Live `subscription/query_state.go:13` is `subscribers map[ConnectionID]map[SubscriptionID]struct{}`; live `subscription/eval.go:70-77` iterates `for connID, subIDs := range qs.subscribers` to fan out one update per subscription. `PHASE-5-DEFERRED.md` §"multiple same-query subscriptions on one connection" claims the code was fixed; the doc was not.

Fix: Story 4.1 `subscribers` must be `map[ConnectionID]map[SubscriptionID]struct{}`, and Story 4.3 `removeSubscriber` must walk the inner map. Story 4.4 `DisconnectClient` unchanged.

---

## 2. Gaps

### 2.1 [GAP] `CommittedReadView` lifetime contract across `Register` and `EvalAndBroadcast` not pinned

- §10.1 says: "Callers must honor the `CommittedReadView` lifetime contract from SPEC-001: materialize the needed rows promptly, close the view before blocking work, and never hold it across network I/O or channel waits." But:
  - Story 4.2 "Registration runs inside an executor command" — the executor closes the view. OK at the register level.
  - Story 5.1 "must not be retained into fan-out or durability waits" — design note, not a normative clause.
  - §8.1 fan-out decoupling: the evaluator sends `FanOutMessage` to the worker. If `DeltaView.committed` kept a reference to `view`, and the fan-out worker later reached into it, the executor may have already released the view.
- Live `subscription/delta_view.go:168 CommittedView()` exposes the underlying view, and `subscription/eval.go` builds a `DeltaView` wrapping `view`. Fan-out receives `FanOutMessage` only (no view) — so the ownership is correct in practice, but the spec never says "EvalAndBroadcast must not retain the view beyond return" as a hard rule.

Fix: §10.1 must state "SubscriptionManager MUST NOT retain `view` past `Register` return / `EvalAndBroadcast` return. Delta and fanout payloads materialize rows into `ProductValue` slices before the call returns." This mirrors the executor-side requirement in SPEC-003 audit §4.5.

### 2.2 [GAP] `DroppedClients()` channel capacity, close, blocking, and dedup contract missing

- §8.5 says fan-out "signals dropped client IDs on the channel returned by `DroppedClients()`". Story 4.5 lists the method but no capacity.
- Story 6.3: "Dropped channel full → log warning, don't block fan-out" (non-blocking send). No capacity declared.
- Live `subscription/manager.go:94` sets `dropped: make(chan ConnectionID, 64)`; live `signalDropped` uses `select { case ch <- id: default: }`.
- Unspecified: (a) channel capacity, (b) whether duplicate drops for the same connID are allowed (live allows — two fan-out attempts on one client may both trigger `markDropped`), (c) whether the channel is closed on SubscriptionManager shutdown, (d) whether the executor should drain after every commit or batch.
- SPEC-003 audit §4.5 flagged the same ambiguity from the executor-drain side.

Fix: §8.5 should state capacity default (match §9.1 "default 64"), explicit drop-on-full policy, "duplicate ConnectionIDs may appear; drainer must treat drops as idempotent", and "channel is closed only after both the fan-out worker and the Manager have stopped; the executor MUST drain before stop." Update Story 4.5 acceptance accordingly.

### 2.3 [GAP] `PostCommitMeta` / `FanOutMessage` / `SubscriptionError` / `ReducerCallResult` / `IndexResolver` not declared in §10

- §10 "Interfaces to Other Subsystems" lists `SubscriptionManager`, `SubscriptionUpdate`, `TransactionUpdate`, `CommitFanout`. It omits:
  - `PostCommitMeta` (needed once 1.1 is fixed).
  - `FanOutMessage` (defined only in §8.1 body; not in the interface list).
  - `SubscriptionError` — referenced by §11.1 but never defined. §10.2 type list omits it. Live `subscription/fanout.go:50` has `{SubscriptionID, QueryHash, Predicate, Message}`; protocol layer (SPEC-005) cannot encode it without a shape.
  - `ReducerCallResult` — used in §8.1 `FanOutMessage` and §8.2 step 4 as `CallerResult *ReducerCallResult`, but not declared. The spec defers to SPEC-005 §8.7. Live `subscription/fanout.go:60` defines a forward-declared type `{RequestID uint32, Status uint8, TxID TxID, Error string, Energy uint64, TransactionUpdate []SubscriptionUpdate}`.
  - `IndexResolver` — used by Story 2.4 `CollectCandidatesForTable(..., resolver IndexResolver)` and Story 3.3 join fragment evaluation. Live `subscription/placement.go:27` defines `IndexResolver.IndexIDForColumn(TableID, ColID) (IndexID, bool)` locally; no spec owns it.

Fix: add a §10.4 "Ancillary types" block declaring all five shapes, or redistribute: `PostCommitMeta` + `FanOutMessage` + `SubscriptionError` into §8 / §10.1 (since they flow through the executor seam); `ReducerCallResult` into §10.2 as a forward-declaration mirroring SPEC-005 §8.7; `IndexResolver` into §10.3 (SPEC-001-adjacent) or §10.4 with a cross-reference to SPEC-006 schema registration.

### 2.4 [GAP] `SubscriptionError` delivery path + payload undefined

- §11.1: "Send a `SubscriptionError` message to all clients subscribed to that query."
- §8.2 fan-out algorithm enumerates durability gating, per-connection TxUpdate, caller special case — no `SubscriptionError` step.
- §10.2 type catalog omits `SubscriptionError`.
- Story 5.1 acceptance: "Evaluation error sends `SubscriptionError` to all clients subscribed to that query" — mechanism undefined.
- Story 6.1 / 6.2 / 6.3 don't list a delivery path. Story 6.1 `FanOutWorker` struct has `sender ClientSender` — SPEC-005 sender is expected to have a `SendSubscriptionError` method, but that expectation is nowhere stated.
- Live `subscription/fanout_worker.go:108-114` delivers errors before updates via `sender.SendSubscriptionError(connID, subID, message)`. Live `subscription/errors.go:43-47` defines `ErrSendBufferFull` / `ErrSendConnGone` that Story 6.3 never names.

Fix: declare `SubscriptionError` shape (SubscriptionID, QueryHash, Predicate, Message) in §10.2; add step 0 "Deliver pending `SubscriptionError` entries for each ConnID" to §8.2; name the protocol-side method in Story 6.1 (likely `ClientSender.SendSubscriptionError`) and cross-reference SPEC-005.

### 2.5 [GAP] `ReducerCallResult` forward-declaration shape not pinned

- §8.1 `FanOutMessage.CallerResult *ReducerCallResult` — no shape.
- Story 6.2 Design Notes: "ReducerCallResult delivery shape is defined by SPEC-005. This story follows that contract."
- Live `subscription/fanout.go:60-67`: forward-declared type with `{RequestID, Status, TxID, Error, Energy, TransactionUpdate []SubscriptionUpdate}`.
- SPEC-005 §8.7 (per `PHASE-5-DEFERRED.md` cross-ref) ought to own it, but until SPEC-004 either declares its contract or cites the exact SPEC-005 field order, downstream implementers pick shapes from Shunter's live code.

Fix: add a one-line "forward-declared from SPEC-005 §8.7; fields `RequestID uint32, Status uint8, TxID TxID, Error string, Energy uint64, TransactionUpdate []SubscriptionUpdate`" line in §8.1 or §10.2. When SPEC-005 lands, convert to an import. Remove the silent duplication.

### 2.6 [GAP] `FanOutSender` / `ClientSender` naming and method surface split

- §8.1 names the protocol seam `ClientSender` (SPEC-005 terminology). §8.2 / Story 6.1 repeat `ClientSender`. Story 6.3 uses `sender.SendTransactionUpdate(connID, &txUpdate)` and sentinel `ErrClientBufferFull`.
- Live `subscription/fanout_worker.go:20-27` declares a `FanOutSender` interface local to this package:
  ```go
  type FanOutSender interface {
      SendTransactionUpdate(connID, txID, updates) error
      SendReducerResult(connID, result *ReducerCallResult) error
      SendSubscriptionError(connID, subID, message) error
  }
  ```
  Signatures differ from Story 6.3 (`SendTransactionUpdate(connID, txID, updates)` vs `SendTransactionUpdate(connID, &txUpdate)`), and sentinel names differ (`ErrSendBufferFull` / `ErrSendConnGone` vs `ErrClientBufferFull`).
- SPEC-005 will eventually own the authoritative contract; SPEC-004 either imports it or declares its own fan-out-facing interface. Today it does neither: the doc references `ClientSender` but the live code lives on a different interface with different method shapes.

Fix: pick one name (`ClientSender` in the doc, `FanOutSender` in live) and use it consistently; spell out the three-method surface SPEC-004 actually needs (`SendTransactionUpdate`, `SendReducerResult`, `SendSubscriptionError`) in §8 or Story 6.1; pin the two sentinels (`ErrSendBufferFull`, `ErrSendConnGone`, or the SPEC-005 names); remove the other path.

### 2.7 [GAP] `IndexResolver` interface has no declared home

- Story 2.4 signature: `CollectCandidatesForTable(..., resolver IndexResolver)`. Story 3.3 Deliverables: "Use `DeltaView.DeltaIndexScan` for delta-side lookups; use `DeltaView.CommittedIndexScan` for committed-side lookups" — but Story 3.3 does not say where `resolver` comes from for the committed-side `IndexID` resolution inside `EvalJoinDeltaFragments`.
- Neither SPEC-001 §7.2 (CommittedReadView) nor SPEC-006 (schema registry) declares an `IndexResolver` surface.
- Live `subscription/placement.go:27`:
  ```go
  type IndexResolver interface {
      IndexIDForColumn(table TableID, col ColID) (IndexID, bool)
  }
  ```
  Manager constructed with a resolver (`NewManager(schema SchemaLookup, resolver IndexResolver, ...)`). The resolver is wired by the executor at startup.

Fix: either (a) add `IndexResolver` to SPEC-006 with the signature above; (b) add it as a §10.3 type on the SPEC-001 boundary alongside `CommittedReadView`; (c) declare it in SPEC-004 §10 as "constructed and supplied by the caller; SPEC-006 is the expected provider." Pick one.

### 2.8 [GAP] `ErrJoinIndexUnresolved`, `ErrSendBufferFull`, `ErrSendConnGone` not in §11 / Story 4.5 / EPICS.md catalog

- Live `subscription/errors.go` declares:
  - `ErrJoinIndexUnresolved` — "validation confirmed a join-side index exists but the runtime `IndexResolver` could not produce an IndexID" (registration-time; `register.go:67`).
  - `ErrSendBufferFull` — fan-out backpressure (`fanout_worker.go:148`).
  - `ErrSendConnGone` — fan-out delivery to disconnected client (`fanout_worker.go:150`).
- Spec §11.2 lists only `ErrInitialRowLimit`, `ErrTooManyTables`, `ErrUnindexedJoin`. Story 4.5 error list omits all three. `EPICS.md` error table likewise.
- `PHASE-5-DEFERRED.md` §D notes `ErrJoinIndexUnresolved` was added to code but the catalog was not updated.

Fix: add all three sentinels to §11 (splitting into §11.2 registration, §11.3 delivery); update Story 4.5 list; update `EPICS.md` error table (introducing epic: Epic 4 for `ErrJoinIndexUnresolved`, Epic 6 for the two delivery errors).

### 2.9 [GAP] Story 5.2 top-level `CollectCandidates` helper is doc-only; the live eval inlines per-tier tiering

- Story 5.2 Deliverables: `CollectCandidates(indexes *PruningIndexes, changeset *Changeset, committed CommittedReadView) map[QueryHash]struct{}` — top-level orchestration wrapper.
- Story 2.4 Deliverables: `CollectCandidatesForTable(indexes, table, rows, committed, resolver)` — per-table helper with `resolver`.
- Live `subscription/eval.go:160 collectCandidates` is a method on `*Manager` that inlines the tier-1/2/3 logic, calls `m.indexes.Value.Lookup` / `m.indexes.JoinEdge.Lookup` / `m.indexes.Table.Lookup` directly — does not call either shared helper.
- Story 5.2 top-level signature also omits `IndexResolver`, which Story 2.4 needs.

Fix: reconcile. Either (a) remove the top-level `CollectCandidates` from Story 5.2, document that Manager owns tier orchestration inline, and keep `CollectCandidatesForTable` as a shared entry point for external test callers; or (b) rewire the live `Manager.collectCandidates` to call the shared helpers with explicit resolver plumbing.

### 2.10 [GAP] Caller-result delivery when caller's `Fanout` slice is empty is unspecified

- §8.2 step 4: "Special case: if this commit came from `CallReducer`, the caller connection's update slice is routed into `ReducerCallResult.transaction_update` instead of also receiving a standalone `TransactionUpdate`."
- Implicit assumption: `msg.Fanout[*CallerConnID]` is non-nil. What happens for a reducer call that mutated nothing (or mutated rows no subscription covers): the caller is not present in `Fanout`, but `ReducerCallResult` must still be delivered with a success status.
- Live `subscription/fanout_worker.go:118-143`: `callerUpdates = msg.Fanout[*msg.CallerConnID]` returns nil, then `result.TransactionUpdate = callerUpdates` (nil) if `Status == 0`, `nil` otherwise, then `SendReducerResult` is invoked regardless. Also, when `Status != 0`, `TransactionUpdate = nil` — failed reducer carries no updates. Neither policy is stated in §8.2.

Fix: §8.2 must spell out: "Caller reducer delivery happens even when `CommitFanout[CallerConnID]` is empty; when present, the caller's subscription updates are attached to `ReducerCallResult.TransactionUpdate` on Status==0; on non-success Status the caller's subscription updates (if any) are dropped."

### 2.11 [GAP] Initial row limit's meaning for joins undefined

- §4.1 / Story 4.2: `ErrInitialRowLimit` "when initial result exceeds configurable max".
- For a join subscription, "initial result" is a joined row set. Is the cap applied to joined-row count, LHS-row count, or max(|LHS|, |RHS|)?
- Live `subscription/register.go:52` counts joined rows (one increment per joined pair) — a 100×100 join caps at the joined product.

Fix: Story 4.2 should state "row limit applies to the materialized result set as returned to the client (joined rows count once)."

### 2.12 [GAP] `PostCommitMeta.TxDurable` for empty-fanout transactions — still mandatory?

- §7.2 step 5 sends `FanOutMessage` regardless. Story 5.1 step 5 same.
- But step 1 "If no active subscriptions: return immediately." So an empty-fanout case still results in zero `FanOutMessage` sent.
- §8.2 step 1: "Wait for `TxDurable` (if confirmed reads required by any client)." When the fan-out map is empty, there are no clients — the wait is a no-op. Fine.
- The caller case: a reducer call with no subscriptions active still needs `ReducerCallResult` delivered. If `EvalAndBroadcast` early-returns with "no active subscriptions", the caller never gets its reducer response via fan-out.
- Live `subscription/eval.go:26`: `if !m.registry.hasActive() || changeset == nil || changeset.IsEmpty() { return }` — the caller-side `ReducerCallResult` is silently dropped in this branch because `FanOutMessage` is never constructed.

Fix: §7.2 step 1 must state: "When no subscriptions are active **and no caller reducer result is pending**, return without sending `FanOutMessage`. Otherwise a `FanOutMessage` with empty `Fanout` and populated `CallerResult` must be emitted so the caller's response still goes out." Or: move caller-response delivery out of the subscription seam into an executor-only path (then §8.2 step 4 should be demoted to a cross-reference).

### 2.13 [GAP] `PruningIndexes.CollectCandidatesForTable` tier-2 silent skip when resolver is nil

- Story 2.4: "`resolver` is consulted for Tier 2 RHS index lookup; when nil, Tier 2 is skipped".
- Live `subscription/placement.go:102-129` also silently skips when `resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)` returns `(_, false)`. `PHASE-5-DEFERRED.md` §D calls this "intentional pruning false negative."
- But a Join subscription post-A3 validation has already confirmed an index exists on one side. If the resolver disagrees at eval time, tier-2 pruning produces a false negative — the subscription still fires via tier 3, but only because `TableIndex.Lookup` catches it. For a Join subscription placed into tier-2 (not tier-3), a resolver miss means the query is **not** evaluated — silently broken.

Fix: Story 2.4 / §5.4 invariant must say: "If `PlaceSubscription` chose tier-2 for a (query, table) pair, the resolver MUST produce an IndexID for the corresponding RHS join column at eval time. Resolver disagreement is a programming error (see §11.3); pruning MUST NOT silently skip tier-2 for a query it previously placed there." Alternatively: tier-2 resolver miss → fall back to re-running against tier-3 semantics inline, not silent skip.

### 2.14 [GAP] SPEC-004 has no "Depends on" front matter

- Prior specs (SPEC-001/002/003) declare `Depends on: ...` and `Depended on by: ...` in the header.
- SPEC-004 has a §1 Purpose and jumps straight into concepts. SPEC-003 audit §4.1 pattern applies.
- Dependencies used in the body: SPEC-001 (`CommittedReadView`, `Changeset`, `ProductValue`, `Bound`), SPEC-003 (`TxID`, `ConnectionID`, `Identity`), SPEC-005 (`ClientSender` / `ReducerCallResult` / backpressure), SPEC-006 (`SchemaLookup`, `IndexResolver`).

Fix: add a standardized front-matter block listing all four.

---

## 3. Divergences from SpacetimeDB (should be documented)

### 3.1 [DIVERGE] Go predicate builder vs SpacetimeDB SQL subset

- SpacetimeDB subscriptions are compiled from SQL (`reference/SpacetimeDB/crates/subscription/src/lib.rs:476 SubscriptionPlan::compile`, delegating to `compile_subscription` in the query crate). RLS is applied at compile time via `SchemaViewer` (`reference/SpacetimeDB/crates/core/src/subscription/query.rs:29`).
- SPEC-004 §3 / §12.1 chooses a Go predicate builder for v1. Already documented as a v1 choice — the SQL parser is called out as a "v2 sugar layer". Good.
- Worth an additional one-liner: "SpacetimeDB's subscription SQL is a restricted SELECT surface; joins must be index-based (same rule as §3.3 rule 2). Shunter's predicate constraints (§3.3) match that index-only-join constraint, so a future SQL parser compiling to these predicate structs is a viable path without widening the evaluator."

### 3.2 [DIVERGE] Bounded fan-out channel + disconnect-on-lag vs unbounded MPSC + lazy-mark

- SpacetimeDB fan-out worker uses `mpsc::UnboundedReceiver<SendWorkerMessage>` (`reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:1718`) and marks slow clients via `Arc<AtomicBool>` checked at the next subscription operation (`1759-1763, 1997, 1910-1912`). Dropped clients are cleaned up lazily on next `add_subscription` / unsubscribe (`829, 968-972`).
- Shunter bounds the fan-out inbox to 64 (§8.4 / Story 6.1 / live `fanout_worker.go`) and disconnects-on-buffer-full (§8.4 / Story 6.3). Dropped clients signaled on a separate channel drained by the executor (§8.5).
- Trade-off: Shunter cannot silently tolerate a slow client (forces early disconnect) but has a hard memory bound; SpacetimeDB's unbounded queue can grow without reply.

Worth stating explicitly in §8 / §12: "Unlike SpacetimeDB's unbounded fan-out MPSC with lazy atomic-bool marking, Shunter bounds the fan-out inbox and disconnects the client as soon as its outbound buffer overflows. The trade-off: harder fail-fast with no silent slowdown; deeper-queued backpressure is out of scope for v1."

### 3.3 [DIVERGE] No row-level security / per-client predicate filtering

- SpacetimeDB applies RLS at compile time via `SchemaViewer::new(tx, auth)` (`reference/SpacetimeDB/crates/core/src/subscription/query.rs:29,33`): tables + event-table access filtered per caller. Parameterized queries also include caller identity in the hash (`execution_unit.rs:33-47`).
- Shunter has no RLS concept anywhere. Per-client filtering relies entirely on the predicate the client submits plus parameterized hashing.
- §12 Open Design Decisions does not mention RLS as deferred.

Fix: add §12.4 "Row-Level Security" stating "v1 does not apply additional per-caller filtering beyond the submitted predicate. SpacetimeDB's SchemaViewer-based RLS is out of scope for v1; applications that need it must filter at the reducer boundary."

### 3.4 [DIVERGE] Post-fragment bag dedup (§6.3) vs SpacetimeDB's in-fragment count tracking

- SpacetimeDB's `eval_delta` tracks insert/delete counts during fragment evaluation to emit correct multiplicities (`reference/SpacetimeDB/crates/core/src/subscription/delta.rs:57-112`) — bag semantics enforced inside the fragment computation.
- Shunter emits all 8 fragments fully, then reconciles in `ReconcileJoinDelta` (Story 3.4) using a `(insertCounts, deleteCounts)` map per row encoding.
- End result is the same. Different implementation, different allocation profile. Fine as long as Shunter's invariant "rows produced by fragments are materialized before dedup" holds — §6.3 states it.

Worth a one-liner in §6.3: "SpacetimeDB folds cancellation inside fragment computation; Shunter materializes all 8 fragments and dedups post-hoc. The two approaches agree on the final delta under bag semantics."

### 3.5 [DIVERGE] `PostCommitMeta.TxDurable` flows through the subscription seam rather than an engine-level broadcast

- SpacetimeDB's fan-out worker awaits `tx_offset` from a `oneshot::Receiver` supplied at broadcast time (`reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:1661-1665, 1857-1866`).
- Shunter routes TxDurable through `PostCommitMeta.TxDurable` in the executor, into `FanOutMessage.TxDurable`, consumed by `FanOutWorker` (§8.2 step 1). Same mechanism, different routing.
- Ripple: SPEC-003 §7 needs to expose `WaitUntilDurable(txID) <-chan TxID` on `DurabilityHandle` — SPEC-003 audit §1.3 already flagged this.

OK as-is once SPEC-003 §7 is fixed. Worth a cross-reference in §8.1 / §12.3.

---

## 4. Internal consistency

### 4.1 [NIT] §10.1 and Story 4.5 mirror each other on the wrong `EvalAndBroadcast` signature

Already flagged in 1.1. Noted here because the inconsistency is against live, not between the two documents — they agree with each other and both disagree with the implementation.

### 4.2 [NIT] §8.1 fan-out sender type name `ClientSender` vs live `FanOutSender`

See 2.6. Cosmetic until SPEC-005 lands, but worth aligning ahead of that spec so one name wins.

### 4.3 [NIT] §3.4 hash input "predicate structure + client identity" vs Story 1.3 "append client identity bytes after predicate bytes"

- §3.4 bullet: "parameterized predicates: hash of the predicate structure + client identity".
- Story 1.3: "For parameterized: append client identity bytes after predicate bytes."
- Live `subscription/hash.go:77-79`: `enc.buf = append(enc.buf, clientID[:]...)` — append before single `blake3.Sum256(enc.buf)`.
- Minor: §3.4 reads as "compute the non-parameterized hash, then combine with identity"; Story 1.3 and live read as "include identity in the canonical bytestream before hashing." Both produce the same result with blake3 but the semantics differ if anyone later adds a cheap hash (xxhash of bytes is not the same as xxhash of structure XOR'd with identity).

Fix: §3.4 should match Story 1.3's wording — "the canonical bytestream is the predicate encoding optionally followed by the client identity; the hash is blake3-256 over that bytestream."

### 4.4 [NIT] `CommitFanout` ownership across the channel

- §8.1 and Story 6.1: `FanOutMessage.Fanout CommitFanout` passed through a bounded channel.
- §7.2 step 5 emits the fanout and (by implication) gives the worker ownership. But §7.2 doesn't say the executor MUST NOT reuse the same map after sending, and Story 5.1 Design Notes don't pin it either.
- Live `subscription/fanout_worker.go:119-130` contains an explicit comment: "Skip (not delete) caller during iteration to avoid mutating the shared Fanout map" — the live worker assumes the map might be shared. Safer for immutability but the doc is silent.

Fix: §7.2 step 5 / §8.1 should state: "`FanOutMessage.Fanout` is transferred to the fan-out worker; the evaluator MUST NOT retain or mutate the map after send. The fan-out worker reads without mutation."

### 4.5 [NIT] `SubscriptionUpdate` carries `TableName` but `TableChangeset` already has one

- §10.1 cross-reference: `tc.TableName` from SPEC-001 §6.1.
- §10.2 `SubscriptionUpdate{TableID, TableName, ...}` — duplicates the TableChangeset name.
- Minor denormalization; SPEC-005 wire format likely wants the name inline. But call it out so the duplication is intentional.

### 4.6 [NIT] §7 "EvalTransaction" vs §10.1 "EvalAndBroadcast" vs live / Story naming

- §7.1–§7.3 refer to the algorithm as `EvalTransaction`.
- §7.2 step 5 names the channel-send step, which includes broadcast — but the method being described is still called `EvalTransaction` inside §7.
- §10.1 interface method: `EvalAndBroadcast`. Story 4.5 and Story 5.1 use `EvalAndBroadcast`. Live: `EvalAndBroadcast`.

Fix: rename §7 heading to `EvalAndBroadcast` and update the intra-section references (or inline-note that "EvalTransaction" is a shorthand for the evaluation phase of `EvalAndBroadcast`).

### 4.7 [NIT] `QueryHash` type listed nowhere in §10 type catalog

- §3.4 introduces `QueryHash` (32-byte blake3). Stories 1.3, 2.1, 2.2, 2.3, 4.1 all consume it. Not in §10.2.
- Fix: §10.2 should mention `QueryHash` (or reference Story 1.3) as a subscription-public type that SPEC-005 may also need for observability.

### 4.8 [NIT] §9.1 evaluation-latency targets vs Story 5.4 benchmark labels

- §9.1: "Evaluation latency (single-table, 1K subs) < 1 ms" and "< 5 ms for 10K subs".
- Story 5.4 Acceptance: "1K subscriptions, 1 table change → < 1 ms" / "10K subscriptions, 1 table change → < 5 ms". OK.
- §9.1 also lists "Join fragment evaluation < 10 ms per subscription". Story 5.4: "Join fragment benchmark: < 10 ms per affected join subscription." OK but "per subscription" vs "per affected join subscription" — minor.
- Fix: use "per affected subscription" everywhere. Same nit pattern as SPEC-001 audit §4.4 on "constraints" vs "targets".

### 4.9 [NIT] `activeColumns` type in §6.4 (map[TableID][]ColID) vs §7.2 description

- §6.4 `NewDeltaView(committed, changeset, activeColumns map[TableID][]ColID)`. OK — actual shape in Story 3.1.
- §7.2 says "Build delta indexes for columns referenced by active subscriptions" — the computation of `activeColumns` is not named. Story 5.1 Design Notes says "scan all active queries, collect the set of (table, index) pairs they reference" — but the delta indexes are keyed by `ColID` (Story 3.1, §6.4), not `IndexID`.
- Live `subscription/eval.go:110 collectActiveColumns` uses ColID. Story 5.1 wording "(table, index) pairs" is misleading.

Fix: Story 5.1 Design Notes: "scan all active queries, collect the set of `(TableID, []ColID)` pairs they reference."

---

## 5. Epic/story coverage

### 5.1 Verified good coverage

- Epic 1 covers §3.1–§3.4 (predicate tree, validation, hash); internal-consistency nits flagged above.
- Epic 2 covers §5.1–§5.4 (three-tier pruning); `PHASE-5-DEFERRED.md` §A records already-landed doc fixes.
- Epic 3 covers §6.1–§6.4 (DeltaView, IVM fragments, bag dedup, allocation discipline).
- Epic 4 covers §4.1–§4.3, §10.1 (register, unregister, disconnect, interface) — critical gaps 1.2 / 1.6 / 2.3 / 2.4 / 2.5 above.
- Epic 5 covers §7.1–§7.4, §9.1 (eval loop, candidate collection, memoized encoding, benchmarks) — critical gap 1.1 / 1.3 / 1.4 / 2.9 / 2.12.
- Epic 6 covers §8.1–§8.5 (fan-out, assembly, backpressure, confirmed reads) — critical gaps 1.3 / 1.5 / 2.2 / 2.5 / 2.6 / 2.10.

### 5.2 [GAP] No story owns the Manager ↔ FanOutWorker wiring

- Live `subscription/manager.go:108 DroppedChanSend()` hands the write end of the dropped channel to the FanOutWorker, so both the Manager (eval-error path) and the FanOutWorker (send-failure path) write to the same channel; the executor drains one channel (§8.5 intent).
- No story owns this wiring. Story 4.5 defines `DroppedClients()` read side; Story 6.1 defines `FanOutWorker` with its own `dropped chan<- ConnectionID`; nobody documents the shared-channel topology.

Fix: add a subsection in §8.5 or a new Story 6.5 "Manager/FanOutWorker wiring" documenting: both components share one channel, Manager exposes the write end, FanOutWorker accepts it, the executor drains the read end.

### 5.3 [GAP] No story covers `activeColumns` computation policy when a subscription is unregistered mid-eval

- Story 5.1 builds `DeltaView` once per eval. If `evalQuerySafe` panics and `handleEvalError` unregisters a subscription mid-loop, the `activeColumns` snapshot used for DeltaView construction is stale — the DeltaView may still have scratch indexes for columns no longer referenced. Not a correctness bug (extra indexes are wasted, not wrong) but undocumented.

Fix: one line in Story 5.1 Design Notes: "activeColumns is captured at DeltaView construction; mid-eval subscription removal does not invalidate the DeltaView."

### 5.4 [GAP] No story covers the empty-fanout caller-response case

See 2.12. Story 5.1 early-exit path does not reconcile with Story 6.2 caller delivery path.

### 5.5 [GAP] SubscriptionError delivery has no owner story

See 2.4. §11.1 delivers; no story names the method.

---

## 6. Clean-room boundary

Overall: the SPEC-004 decomposition is Go-typed and prose-original. No Rust identifiers or SpacetimeDB file paths appear in the spec/epic/story prose. Type names (`Predicate`, `ColEq`, `ColRange`, `And`, `AllRows`, `Join`, `Bound`, `QueryHash`, `ValueIndex`, `JoinEdge`, `JoinEdgeIndex`, `TableIndex`, `DeltaView`, `DeltaIndexes`, `SubscriptionManager`, `SubscriptionRegisterRequest`, `SubscriptionRegisterResult`, `SubscriptionUpdate`, `TransactionUpdate`, `CommitFanout`, `FanOutWorker`, `FanOutMessage`, `SubscriptionError`, `ReducerCallResult`) are idiomatic Go; they conceptually parallel SpacetimeDB's Rust surface but use different names and granularity.

Concept → name map against reference:

- `ModuleSubscriptions` / `SubscriptionManager` (Rust) → `SubscriptionManager` (Go); single goroutine vs Tokio.
- `Plan { plans, hash, sql }` (`module_subscription_manager.rs:62-65`) → `queryState { hash, predicate, subscribers, refCount }` — Shunter's v1 plan is the validated predicate itself (Story 4.1 Design Notes); SpacetimeDB's plan is a compiled `SubscriptionPlan`.
- `SearchArguments` (`module_subscription_manager.rs:268`) → `ValueIndex` (Shunter Tier 1).
- `JoinEdges` (`module_subscription_manager.rs:445`) → `JoinEdgeIndex` (Shunter Tier 2).
- `DeltaTx` (`subscription/tx.rs:84`) → `DeltaView` (Shunter) — the `DeltaStore`-trait divergence is already documented in §6.4 and `PHASE-5-DEFERRED.md` §B.
- `SendWorker` (`module_subscription_manager.rs:1716, 1840`) → `FanOutWorker`.
- `QueryHash::from_string(sql, identity, has_param)` (`execution_unit.rs:33`) → `ComputeQueryHash(pred, *Identity)`. Hash algorithm is BLAKE3 in both; the canonical serialization input differs (Rust hashes SQL text; Shunter hashes the canonical predicate tree).
- `TransactionUpdate` (Rust) → `TransactionUpdate` (Go); SpacetimeDB v2 groups by `(client, query_set_id, table_name)` (`module_subscription_manager.rs:2078-2091`), Shunter groups by `(client, SubscriptionID)` (§8.3 "preserve one `SubscriptionUpdate` entry per subscription").

No story cites SpacetimeDB file paths; no Rust identifiers leaked. Clean-room boundary holds.

One clean-room caveat carries over from SPEC-002 audit §3.1: SPEC-004 §3.3 / §6.3 implicitly leans on the BSATN encoder as "canonical byte encoding of Value" — `encodeRowKey` in live `subscription/delta_dedup.go` and `encodeValueKey` in `subscription/value_index.go` reuse the same canonical encoder. If the BSATN rename / disclaimer from SPEC-002 audit §3.1 lands, SPEC-004 needs the same disclaimer (or explicit note that the Shunter encoder is internal-only and not wire-compatible with SpacetimeDB).

---

## 7. Quick wins (suggested ordering for doc repair)

1. Add `meta PostCommitMeta` to `EvalAndBroadcast` in §10.1 + Story 4.5 (1.1). Define `PostCommitMeta` shape. Coordinated with SPEC-003 audit §1.3.
2. Add `ClientIdentity *Identity` to `SubscriptionRegisterRequest` in §4.1 + Story 4.5 (1.2). Wire through Story 4.2.
3. Add `TxID` and `Errors` to `FanOutMessage` in §8.1 (1.3). Extend Story 5.1 algorithm step 5.
4. Define `SubscriptionError` shape in §10.2 and §11.1 (2.4). Add `SendSubscriptionError` to the fan-out sender contract.
5. Pin `SubscriptionUpdate.TableID` convention for joins in §10.2 / Story 3.3 (1.5).
6. Fix `subscribers` shape in Story 4.1 to `map[ConnectionID]map[SubscriptionID]struct{}` (1.6).
7. Reconcile §11.1 per-subscription recovery with SPEC-003 §5.4 fatal post-commit (1.4). Pick Model A or B.
8. Add `ErrJoinIndexUnresolved`, `ErrSendBufferFull`, `ErrSendConnGone` to §11 and Story 4.5 / `EPICS.md` (2.8).
9. Declare `PostCommitMeta`, `FanOutMessage`, `SubscriptionError`, `ReducerCallResult`, `IndexResolver` in §10 (2.3).
10. Pin `DroppedClients()` channel capacity, close, duplicate semantics in §8.5 / Story 4.5 (2.2).
11. Pin `FanOutSender` / `ClientSender` interface shape in §8 / Story 6.1 and fix sentinel naming (2.6).
12. Add `CommittedReadView` no-retain rule to §10.1 (2.1).
13. Fix `CollectCandidates` vs `CollectCandidatesForTable` vs live inline layout (2.9).
14. Fix `activeColumns` wording in Story 5.1 (4.9).
15. Add `Depends on: SPEC-001, SPEC-003, SPEC-005, SPEC-006` front matter (2.14).
16. Add §3 divergence documentation for RLS absence (3.3).
17. Everything else (nits).

---

## 8. Spec-to-code drift (follow-up, not this pass)

Live `subscription/` is substantially ahead of the spec (Epics 1–6 all land per `REMAINING.md`). After the spec fixes above land, reconcile:

- `SubscriptionRegisterRequest.ClientIdentity *types.Identity` (`subscription/manager.go:16`) — not in §4.1 (1.2).
- `SubscriptionManager.EvalAndBroadcast(..., meta PostCommitMeta)` (`subscription/manager.go:53`, `executor/interfaces.go:36`) — not in §10.1 (1.1).
- `PostCommitMeta` struct (`subscription/fanout.go:41-45`) — not in §10 (2.3).
- `FanOutMessage.TxID`, `FanOutMessage.Errors` (`subscription/fanout.go:12-36`) — not in §8.1 (1.3).
- `SubscriptionError` shape (`subscription/fanout.go:50-55`) — not in §10.2 (2.4).
- `ReducerCallResult` forward-declaration (`subscription/fanout.go:60-67`) — shape not in §8.1 / §10.2 (2.5).
- `queryState.subscribers map[ConnectionID]map[SubscriptionID]struct{}` (`subscription/query_state.go:13`) — Story 4.1 shape wrong (1.6).
- `IndexResolver` interface (`subscription/placement.go:27-29`) — no spec home (2.7).
- `FanOutSender` interface + sentinels `ErrSendBufferFull` / `ErrSendConnGone` (`subscription/fanout_worker.go:20-27`, `subscription/errors.go:43-47`) — spec uses `ClientSender` / `ErrClientBufferFull` (2.6).
- `ErrJoinIndexUnresolved` (`subscription/errors.go:28-31`) — not in §11 / Story 4.5 catalog (2.8).
- `Manager.DroppedChanSend()` shared-channel wiring (`subscription/manager.go:108`) — no story (5.2).
- `SubscriptionUpdate.TableID = Join.Left` / composite name convention (`subscription/eval.go:268-281`) — not in §10.2 (1.5).
- `evalQuerySafe` per-candidate panic recovery (`subscription/eval.go:235-243`) — contradicts SPEC-003 §5.4 (1.4).
- Empty-active early return drops caller response (`subscription/eval.go:26`) — reducer-caller delivery lost when no subs active (2.12).
- Initial query row-limit counts joined rows (`subscription/register.go:52`) — spec silent (2.11).
- Allocation discipline partial: `encoderPool` and `dedupPool` landed (`subscription/hash.go:39-41`, `subscription/delta_dedup.go:19-28`); `PHASE-5-DEFERRED.md` §C still lists 4 KiB buffer pool and DeltaView slice pooling as open.

Recommend: fix the spec first (above), then a single drift-reconciliation pass to realign `subscription/` naming and error catalog with the repaired SPEC-004 in lock-step with the pending SPEC-001/002/003 reconciliations.
