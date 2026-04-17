# Lane B Session 8 — SPEC-002 Residue Cleanup Plan

> **Scope:** Docs-only. `docs/decomposition/002-commitlog/**` + cross-spec touch points (`docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.3-state-export.md`, `docs/decomposition/001-store/SPEC-001-store.md` §11) + `AUDIT_HANDOFF.md`. Live code (`store/`, `commitlog/`, `executor/`, `subscription/`, `protocol/`, `schema/`, `types/`, `bsatn/`) off-limits per Lane B §B.0. If a spec edit outruns live code → log Session 12+ drift in `TECH-DEBT.md` (TD-125/126/127 precedent).

**Goal:** Resolve every `open` SPEC-002 row in `AUDIT_HANDOFF.md §B.2` (CRIT/GAP/DIVERGE/NIT). Stop when all flipped to `closed` or `deferred — <reason>`.

**Stop rule:** no `open` SPEC-002 row remains; §B.5 cursor + intro block advanced to Session 9.

---

## Decisions pinned up front

- **§1.4 sequence-advance owner:** SPEC-001 Story 8.2 already owns the advance step (Session 7 landed `max(next, observed+1)` in the insert branch); this session just adds the cross-ref from SPEC-002 Story 6.4 + §6.1 step 6, and updates Story 6.4's sequence-restoration acceptance to point at it. No SPEC-001 edit needed.
- **§2.3 trailer (closed C2):** Cluster C already pinned the 3-byte `(type_tag, nullable, auto_increment)` trailer. No further edit.
- **§2.4 row_count width:** pick `uint32` (matches every other count field + live `commitlog/snapshot_io.go:332`). 4.3B rows/table is the v1 ceiling.
- **§2.6 / §5.6 restore-API naming:** name `Table.InsertRow(id, row)` as the bulk-restore primitive (already in SPEC-001 Story 2.2). No new `RestoreRow` / `RebuildIndexes` methods — live `store/recovery.go:37` calls `InsertRow` directly and `Table.InsertRow` already calls `insertIntoIndexes` (per `store/table.go:65`), so indexes rebuild incidentally during restore. SPEC-001 Story 8.3 + §11 document the contract; SPEC-002 Story 6.4 cross-refs. Avoids creating drift; aligns spec with live exactly.
- **§2.10 AppendMode in §6.4:** promote the three-state enum (declared in Story 6.1) to a normative §6.4 paragraph; tighten "MAY resume by creating a fresh next segment" → "MUST" for the `AppendByFreshNextSegment` case; leave Story 6.1 as the canonical home for the enum definition.
- **§2.11 schema-static invariant owner:** Story 3.1 (changeset encoder). Encoder is the single producer that crosses the snapshot/log boundary; one design-note line stating "encoder assumes static schema for the data-dir lifetime; mutating the registry mid-session is undefined behavior" closes ownership.
- **§2.12 snapshot retention:** declare deferred-v1 in §7 + §12 OQ#2; no new story. Audit explicitly permits "state in §7 / Story 7.2 that snapshot retention is out of scope for v1 and name the consequence."
- **§2.13 graceful-shutdown orchestration:** cross-ref from SPEC-002 §5.6 + Story 5.2 to SPEC-003 (executor shutdown). Don't add a SPEC-002 story — orchestration sits in SPEC-003. SPEC-003 audit §2.5 "Startup orchestration owner unspecified" + Session 9 (SPEC-003 residue) will own the executor side.
- **§3.x divergence block:** new `## 12. Divergences from SpacetimeDB` mirroring the SPEC-001 §12 shape Session 7 added. Push Open Questions → §13, Verification → §14. Six entries (§3.2–§3.7); §3.1 already closed (C1) and §3.3 is "OK as-is" per audit (already documented in §2.3). Final entry count = 6 minus already-documented overlap. After re-reading audit: §3.3 is already absorbed in §2.3 commentary — but Session 7 included absorbed entries in §12 anyway for completeness. Mirror that: include §3.3 as a brief entry pointing at §2.3, and one each for §3.2, §3.4, §3.5, §3.6, §3.7.
- **§4.1 schema_version duplication:** keep both copies (header is authoritative pre-Blake3-recompute; body copy is part of the schema codec output). Add a one-sentence §5.3 note declaring header authoritative and explaining the body copy as codec self-containment. Cluster A A3 already pinned header authority for header-vs-body disagreement.
- **§4.5 atomic vs mutex:** soften §4.3 struct to match live (`stateMu sync.Mutex` guarding `waiters` + `closing` + `fatalErr`; `durable` stays `atomic.Uint64`). Audit explicitly recommends this once `WaitUntilDurable` was adopted (Session 6).
- **§4.7 .log extension:** cosmetic — add a single sentence in §6.1 that segments are `.log` files (per §2.1 directory structure).
- **§4.8 fsync-policy placeholder:** add `FsyncMode` enum to §8 with `Batch` only in v1; cross-ref §12 OQ#3. Avoids breaking change later.
- **Commit grouping:** see §Commits at end. One per CRIT row. GAPs bundled by theme (three bundles: error catalog, ownership, restore-API). Divergence block = one commit. NITs bundled by file. Tracking-doc refresh = last.
- **Drift candidates:** none expected. The §2.4 row_count change matches live; §4.5 mutex change matches live; §2.6 sticks to live's `InsertRow` path. If §2.10 AppendMode promotion or §1.3 nextID layout edits create gaps with live, log TD-128/129. Verify at Task 14.

---

## Task 1 — CRIT §1.1 SnapshotInterval default contradicts itself

**Files:**
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §8 `SnapshotInterval` default

**Edit — §8 Configuration block:**

Replace:
```
    // SnapshotInterval: call CreateSnapshot after this many commits.
    // 0 = never snapshot automatically.
    // Default: 100_000.
    SnapshotInterval uint64
```
with:
```
    // SnapshotInterval: call CreateSnapshot after this many commits.
    // 0 = never snapshot automatically.
    // Default: 0 (no auto-snapshot). §5.6 explains why: synchronous
    // snapshot creation holds CommittedState.mu for read for
    // tens-to-hundreds of milliseconds, blocking commits. v1 recommends
    // graceful-shutdown snapshotting only; auto-interval is opt-in.
    SnapshotInterval uint64
```

**Grep:** `rtk grep -n "SnapshotInterval\|100_000\|100000" docs/decomposition/002-commitlog/` — confirm Story 4.1 deliverable already says `default 0` (it does at line 61); §5.6 already says recommended default 0; only §8 was the outlier.

**Commit:** `docs: Lane B SPEC-002 residue — §1.1 SnapshotInterval default = 0`

---

## Task 2 — CRIT §1.3 nextID section in §5.2 layout

**Files:**
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §5.2 file format

**Edit — §5.2 snapshot file layout:** insert new section between sequences and tables.

Replace:
```
  seq_count          : uint32 LE
  [ for each sequence, sorted by table_id ascending:
      table_id       : uint32 LE
      next_id        : uint64 LE
  ]

  table_count        : uint32 LE
```
with:
```
  seq_count          : uint32 LE
  [ for each sequence, sorted by table_id ascending:
      table_id       : uint32 LE
      next_id        : uint64 LE
  ]

  next_id_count      : uint32 LE
  [ for each table with internal RowID allocation state, sorted by table_id ascending:
      table_id       : uint32 LE
      next_id        : uint64 LE   — value of Table.NextID() at snapshot time (SPEC-001 Story 8.3)
  ]

  table_count        : uint32 LE
```

Append one sentence below the existing "Notes" bullet list:
```
- Per-table `next_id` (RowID allocation cursor) is serialized between sequences and tables so post-recovery `Table.AllocRowID()` resumes without collision. Source: SPEC-001 Story 8.3 `Table.NextID()` / `Table.SetNextID()` accessors. Distinct from the auto-increment sequence section above (which serializes user-visible auto-increment values via SPEC-001 Story 8.3 `Table.SequenceValue()` / `Table.SetSequenceValue()`).
```

**Grep:** `rtk grep -n "next_id\|NextID\|nextID" docs/decomposition/002-commitlog/` — confirm Story 5.2 / Story 5.3 already reference the section (they do); §5.2 was the only outlier.

**Commit:** `docs: Lane B SPEC-002 residue — §1.3 nextID section in §5.2 layout`

---

## Task 3 — CRIT §1.4 sequence-advance cross-ref in Story 6.4

**Files:**
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §6.1 step 6
- `docs/decomposition/002-commitlog/epic-6-recovery/story-6.4-open-and-recover.md` step 4 / acceptance

**Edit 1 — §6.1 step 6 Replay log:** extend step 6c sub-bullet.

Replace:
```
   c. Call `store.ApplyChangeset(committed, cs)` (SPEC-001 §5.8). Fatal error if it returns non-nil.
   d. Track `max_applied_tx_id`
```
with:
```
   c. Call `store.ApplyChangeset(committed, cs)` (SPEC-001 §5.8). Fatal error if it returns non-nil. `ApplyChangeset` itself advances each table's `Sequence.next` to `max(current, observed+1)` for any auto-increment column value seen on insert (SPEC-001 Story 8.2 algorithm step 2c) — recovery does not need a separate post-replay sweep. Snapshot-restored sequence values are reconciled against replay-advanced values via `SetSequenceValue`'s `max(current, provided)` rule (SPEC-001 Story 8.3).
   d. Track `max_applied_tx_id`
```

**Edit 2 — Story 6.4 acceptance:** the line "Sequences restored from snapshot, then advanced by replay" is already correct; extend it with mechanism.

Replace:
```
- [ ] Sequences restored from snapshot, then advanced by replay
```
with:
```
- [ ] Sequences restored from snapshot, then advanced by replay (mechanism: SPEC-001 Story 8.2 `ApplyChangeset` advances `Sequence.next` per insert; SPEC-001 Story 8.3 `SetSequenceValue` uses `max(current, provided)` so snapshot restore never rewinds replay-advanced values)
```

Append to Story 6.4 Design Notes:
```
- Sequence-advance ownership: SPEC-001 Story 8.2 `ApplyChangeset` is the single point that advances `Sequence.next` during replay. Story 6.4 does not run a separate post-replay sweep. The snapshot-restore order (load snapshot sequences via `SetSequenceValue` → replay → values further advanced by `ApplyChangeset`) and `SetSequenceValue`'s `max()` semantics together guarantee post-recovery `next` ≥ any value previously emitted, regardless of which side (snapshot or replay) saw the larger value.
```

**Grep:** `rtk grep -n "ApplyChangeset\|Sequence.next\|SetSequenceValue\|advanceRecoveredSequences" docs/decomposition/002-commitlog/` — confirm no surviving "post-replay sweep" or "advance step undefined" wording.

**Commit:** `docs: Lane B SPEC-002 residue — §1.4 sequence-advance cross-ref to SPEC-001 Story 8.2`

---

## Task 4 — GAP/NIT bundle: error catalog (§2.1, §2.2, §4.3)

Single commit covers two missing sentinels in §9, the §2.3/§6.4 cross-ref, and the EPICS.md error-table fill-in.

**Files:**
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §9 (add 2 rows), §2.3 (cross-ref), §6.4 (cross-ref)
- `docs/decomposition/002-commitlog/EPICS.md` Error Types table (add ErrTruncatedRecord row)

**Edit 1 — §9 Error Catalog:** add two rows. Insert `ErrSnapshotInProgress` between `ErrSnapshotIncomplete` and `ErrSnapshotHashMismatch`; insert `ErrTruncatedRecord` between `ErrChecksumMismatch` and `ErrRecordTooLarge`.

Add row after `ErrChecksumMismatch`:
```
| `ErrTruncatedRecord` | Partial record at segment tail (incomplete header, payload, or CRC). Distinguishes recoverable truncated tail from fatal mid-segment corruption. Raised by Story 2.4 SegmentReader.Next; consumed by Story 6.1 ScanSegments. |
```

Add row after `ErrSnapshotIncomplete`:
```
| `ErrSnapshotInProgress` | `CreateSnapshot` invoked while another snapshot is already running (Story 5.2 / Story 5.4 exclusivity). |
```

**Edit 2 — §2.3 Record Format invariants:** append after the existing invariant bullet "A record is valid only if all framing bytes, the full payload, and the trailing CRC are present.":
```
- Truncated tail records (partial framing, partial payload, or missing CRC at file end) are reported as `ErrTruncatedRecord` (§9), distinct from `ErrChecksumMismatch`. Recovery (§6.4) treats `ErrTruncatedRecord` on the active tail segment as the replay horizon; `ErrChecksumMismatch` in any sealed segment is fatal.
```

**Edit 3 — §6.4 Truncated Record and Resume Handling:** replace the opening sentence.

Replace:
```
A truncated tail record (partial write at crash time) produces a CRC mismatch or EOF while reading framing/payload. Recovery uses all prior valid records and treats the first invalid tail record as the replay horizon.
```
with:
```
A truncated tail record (partial write at crash time) is reported by the segment reader as `ErrTruncatedRecord` (§9, Story 2.4) — a sentinel distinct from `ErrChecksumMismatch`. Recovery uses all prior valid records and treats the first `ErrTruncatedRecord` on the active tail segment as the replay horizon. A `ErrChecksumMismatch` in a sealed (non-tail) segment is fatal per §6.5.
```

**Edit 4 — EPICS.md Error Types table:** insert row after `ErrChecksumMismatch`:
```
| `ErrTruncatedRecord` | Epic 2 |
```

**Grep:** `rtk grep -n "ErrSnapshotInProgress\|ErrTruncatedRecord" docs/decomposition/002-commitlog/` — confirm both sentinels are in §9, EPICS.md error table, and at least one cross-reference site.

**Commit:** `docs: Lane B SPEC-002 residue — §2.1/§2.2/§4.3 error catalog (ErrSnapshotInProgress, ErrTruncatedRecord)`

---

## Task 5 — GAP §2.4 row_count width

**Files:**
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §5.2 table section

**Edit — §5.2 table section:** change `row_count : uint64 LE` to `uint32 LE`.

Replace:
```
  table_count        : uint32 LE
  [ for each table, sorted by table_id ascending:
      table_id       : uint32 LE
      row_count      : uint64 LE
      [ for each row in deterministic primary-key order:
```
with:
```
  table_count        : uint32 LE
  [ for each table, sorted by table_id ascending:
      table_id       : uint32 LE
      row_count      : uint32 LE   — v1 ceiling: 4_294_967_295 rows per table; matches every other count field in this layout
      [ for each row in deterministic primary-key order:
```

**Grep:** `rtk grep -n "row_count" docs/decomposition/002-commitlog/` — confirm no surviving `uint64` for row_count in spec or stories.

**Commit:** `docs: Lane B SPEC-002 residue — §2.4 row_count uint32 (matches live + every other count field)`

---

## Task 6 — GAP §2.6 / §5.6 restore-API naming

**Files:**
- `docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.3-state-export.md` Deliverables + Design Notes
- `docs/decomposition/001-store/SPEC-001-store.md` §11 SPEC-002 subsection
- `docs/decomposition/002-commitlog/epic-6-recovery/story-6.4-open-and-recover.md` step 3 / Design Notes

**Edit 1 — SPEC-001 Story 8.3 Deliverables:** insert a "Bulk-restore primitives" subsection after the existing "Restore functions" subsection.

After the `SetSequenceValue` bullet and before the "Cross-spec contract note:", insert:
```

- **Bulk-restore primitives** for SPEC-002 recovery (Story 6.4):
  - `func (cs *CommittedState) RegisterTable(schema *TableSchema) error` (Story 5.1) — register tables from the snapshot's schema before populating rows.
  - `func (t *Table) InsertRow(id RowID, row ProductValue) error` (Story 2.2) — bulk-restore primitive. Each call rebuilds index entries for `row` via `insertIntoIndexes` (Story 4.2), so indexes do not need a separate rebuild step. Recovery loops `InsertRow` over snapshot rows in order; SPEC-001 does not expose a dedicated `RestoreRow` or `RebuildIndexes` surface because `InsertRow` already covers both responsibilities.
  - `func (t *Table) SetNextID(id uint64)` — call after restoring all snapshot rows so future `AllocRowID()` resumes past the snapshot horizon.
  - `func (t *Table) SetSequenceValue(val uint64)` — call once per snapshot-recorded sequence value before replay; replay's `ApplyChangeset` further advances via Story 8.2.
```

Append to Design Notes:
```
- The bulk-restore path is `RegisterTable → loop InsertRow → SetNextID → SetSequenceValue`. SPEC-002 Story 6.4 documents the orchestration; SPEC-001 owns the surface. No dedicated `RestoreRow` / `RebuildIndexes` methods exist — `InsertRow` is the single entry point and handles row + index in one call.
```

**Edit 2 — SPEC-001 §11 SPEC-002 subsection:** find the SPEC-002 export list and add the bulk-restore primitives. The current §11 SPEC-002 subsection lists `Snapshot()` (added in Session 7) and the replay surface. Append after the existing exports:
```

Bulk-restore surface (consumed by SPEC-002 Story 6.4 recovery):

    func (cs *CommittedState) RegisterTable(schema *TableSchema) error
    func (t *Table) InsertRow(id RowID, row ProductValue) error
    func (t *Table) SetNextID(id uint64)
    func (t *Table) SetSequenceValue(val uint64)
```

**Edit 3 — SPEC-002 Story 6.4 step 3:** name the SPEC-001 surface.

Replace step 3:
```
  3. Build initial CommittedState:
     - If snapshot: register tables from schema, populate rows from snapshot, restore sequences, restore per-table `nextID`, rebuild indexes
     - If no snapshot and segments begin at tx 1: register tables from schema (empty state)
     - If no snapshot and there are no segments and no snapshots: return `ErrNoData`
```
with:
```
  3. Build initial CommittedState (using SPEC-001 Story 8.3 bulk-restore surface):
     - If snapshot: for each table in the snapshot — `committed.RegisterTable(schema)`; for each row in the snapshot — `table.InsertRow(allocatedID, row)` (which also rebuilds index entries via SPEC-001 Story 4.2 `insertIntoIndexes`); `table.SetSequenceValue(snapshot.Sequences[tableID])`; `table.SetNextID(snapshot.NextIDs[tableID])`. No separate `RebuildIndexes` step is required because `InsertRow` handles indexes per-row.
     - If no snapshot and segments begin at tx 1: register tables from schema (empty state)
     - If no snapshot and there are no segments and no snapshots: return `ErrNoData`
```

Replace the "Index rebuild after snapshot load" Deliverables bullet:
```
- Index rebuild after snapshot load:
  - For each table: iterate all restored rows, insert into all indexes
  - This is O(rows × indexes) but only happens once at startup
```
with:
```
- Index rebuild after snapshot load:
  - Indexes are rebuilt incidentally during snapshot restore: SPEC-001 Story 2.2 `Table.InsertRow` calls `insertIntoIndexes` (Story 4.2) on every row, so the per-row restore loop already populates all indexes.
  - Cost is O(rows × indexes) but happens only once at startup as part of the restore loop, not as a second pass.
```

Update Design Notes — replace the "Index rebuild is the most expensive recovery step…" paragraph with:
```
- Index rebuild during recovery is the most expensive restore step for large datasets. Cost is O(N × I) where N = row count, I = indexes per table. For 1M rows with 4 indexes, that is 4M `insertIntoIndexes` calls, all fired from the per-row `InsertRow` loop. Acceptable at startup. SPEC-001 Story 8.3 names the surface; this story names the orchestration.
```

**Grep:** `rtk grep -n "RegisterTable\|InsertRow\|RestoreRow\|RebuildIndexes\|bulk-restore" docs/decomposition/001-store/ docs/decomposition/002-commitlog/` — confirm Story 8.3 names the four surface methods, Story 6.4 references them, and no surviving "RestoreRow"/"RebuildIndexes" references (those would create drift with live).

**Commit:** `docs: Lane B SPEC-002 residue — §2.6/§5.6 restore-API named (Story 8.3 + Story 6.4)`

---

## Task 7 — GAP §2.8 durable_horizon empty-segments rule

**Files:**
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §6.1 step 2

**Edit — §6.1 step 2:** append a clause to the existing rule.

Replace:
```
2. **Determine the durable replay horizon.** Scan segments in order and find the highest contiguous valid `tx_id` reachable from the earliest segment:
   - validate segment header magic/version
   - validate each record framing and CRC
   - validate contiguous `tx_id` sequence across records and across segment boundaries
   - if the active tail segment ends with a truncated record or CRC-mismatched partial tail write, stop at the last valid contiguous record
   - if a non-tail segment is corrupt, a segment is missing, or the sequence has a gap/fork/out-of-order record, return a hard recovery error
```
with:
```
2. **Determine the durable replay horizon.** Scan segments in order and find the highest contiguous valid `tx_id` reachable from the earliest segment:
   - if there are no segments, `durable_horizon = +∞` (any snapshot is eligible because there is no contradicting log history). Recovery proceeds with the snapshot as the final state and replays nothing; the executor resumes issuing TX IDs from `snapshot_tx_id + 1` (or returns `ErrNoData` if there is also no snapshot — see step 5)
   - validate segment header magic/version
   - validate each record framing and CRC
   - validate contiguous `tx_id` sequence across records and across segment boundaries
   - if the active tail segment ends with a truncated record or CRC-mismatched partial tail write, stop at the last valid contiguous record
   - if a non-tail segment is corrupt, a segment is missing, or the sequence has a gap/fork/out-of-order record, return a hard recovery error
```

**Grep:** `rtk grep -n "durable_horizon\|durableHorizon" docs/decomposition/002-commitlog/` — confirm Story 6.1 / Story 6.2 / Story 6.4 already handle the empty-segments branch (they do behaviorally; §6.1 was the only place the rule was undefined).

**Commit:** `docs: Lane B SPEC-002 residue — §2.8 durable_horizon = +∞ when segments empty`

---

## Task 8 — GAP §2.10 AppendMode in §6.4

**Files:**
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §6.4
- `docs/decomposition/002-commitlog/EPICS.md` dependency graph

**Edit 1 — §6.4 Resume rules block:** replace the four-bullet "Resume rules" list.

Replace:
```
Resume rules:
- the implementation MUST locate the last valid contiguous record before resuming writes
- if the invalid data is only in the writable tail segment and at least one valid record precedes it, the implementation MAY resume by creating a fresh next segment starting at `last_valid_tx_id + 1`
- the implementation MUST NOT assume it can safely overwrite arbitrary trailing bytes in-place without first proving the write position
- if the first record in the last segment is corrupt and there is no prior valid prefix in that segment, opening for append is a hard error until operator intervention or explicit reset
```
with:
```
Resume rules — recovery classifies the active tail segment into one of three append modes (`AppendMode` enum, declared in Story 6.1; consumed by Story 4.3 segment rotation):

- `AppendInPlace` — clean tail (every record valid through end of file). Durability worker opens the tail file for append starting after the last valid record. The implementation MUST locate the last valid contiguous record before resuming writes.
- `AppendByFreshNextSegment` — damaged tail with a valid prefix (at least one record valid, then `ErrTruncatedRecord` or trailing garbage). Durability worker MUST create a fresh next segment starting at `last_valid_tx_id + 1` rather than appending into the damaged file. The implementation MUST NOT assume it can safely overwrite arbitrary trailing bytes in-place.
- `AppendForbidden` — first record in the active tail segment is corrupt with no valid prefix. Opening for append is a hard recovery error until operator intervention or explicit reset.

Recovery (Story 6.4) computes the mode via `ScanSegments` (Story 6.1) and hands it to the durability worker startup (Story 4.3) as part of the `RecoveryResumePlan`. The Epic 6 → Epic 4 hand-off is normative.
```

**Edit 2 — EPICS.md Dependency Graph:** add the Epic 6 → Epic 4 arrow.

Replace:
```
Epic 2: Record Format & Segment I/O
  └── Epic 4: Durability Worker ← Epic 3
  └── Epic 6: Recovery ← Epic 3, Epic 5
  └── Epic 7: Log Compaction ← Epic 5
```
with:
```
Epic 2: Record Format & Segment I/O
  └── Epic 4: Durability Worker ← Epic 3, Epic 6 (RecoveryResumePlan / AppendMode hand-off)
  └── Epic 6: Recovery ← Epic 3, Epic 5
  └── Epic 7: Log Compaction ← Epic 5
```

**Grep:** `rtk grep -n "AppendMode\|AppendInPlace\|AppendByFreshNextSegment\|AppendForbidden\|RecoveryResumePlan" docs/decomposition/002-commitlog/` — confirm AppendMode is now in §6.4 normative text + Story 6.1 declaration + Story 4.3 consumer.

**Commit:** `docs: Lane B SPEC-002 residue — §2.10 AppendMode normative in §6.4 + EPICS dep graph`

---

## Task 9 — GAP bundle: ownership (§2.11, §2.12, §2.13, §5.2, §5.3, §5.4, §5.5)

Single commit. §5.x rows here all overlap §2.x; resolve at the same edit sites.

- §2.11 / §5.2: schema-static encoder invariant — Story 3.1 owner.
- §2.12 / §5.4: snapshot retention — declare deferred-v1 in §7 + §12 OQ#2.
- §2.13 / §5.5: graceful-shutdown orchestration — cross-ref SPEC-003 from Story 5.2 + §5.6.
- §5.3: sequence-advance-on-replay (overlaps §1.4 + SPEC-001 Story 8.2) — already closed by Task 3 cross-ref; flagged here for trace.

**Files:**
- `docs/decomposition/002-commitlog/epic-3-changeset-codec/story-3.1-changeset-encoder.md` Design Notes
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §7, §12 OQ#2, §5.6
- `docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.2-snapshot-writer.md` Design Notes

**Edit 1 — Story 3.1 Design Notes:** append.

```
- Schema-static invariant (SPEC-002 §3.4): the encoder assumes the schema registry is static for the data-dir lifetime. Mutating the registry mid-session (between snapshot writes or between commits) produces undefined output and is unsupported in v1. SPEC-002 has no schema-change record type (§3.4, §12 OQ#4); enforcement is implicit — the application owns not mutating a frozen registry. SPEC-006 §5.1 `Build()=freeze` is the structural guarantee; the encoder is the load-bearing consumer.
```

**Edit 2 — SPEC-002 §7 Log Compaction:** append a "Snapshot retention (deferred v1)" subsection at the end of §7.

```

**Snapshot retention (deferred v1):** This spec defines no automatic snapshot retention policy. After a snapshot lands and compaction sweeps superseded segments, the snapshot directory itself is never deleted by the engine. Operators are expected to prune `snapshots/{tx_id}/` directories out-of-band. Consequence: with `SnapshotInterval > 0`, snapshot directories accumulate without bound, and each is a full copy (no object dedup in v1). Retention policy choice (count-based / age-based / size-based) is tracked in §12 OQ#2; a Story under Epic 7 will own it once the policy is chosen. Until then, leaving snapshots in place is the documented v1 behavior.
```

**Edit 3 — SPEC-002 §12 OQ#2:** extend the existing bullet.

Replace:
```
2. **Multiple snapshot retention.** v1 should keep at least the newest two successful snapshots. Whether retention should be count-based, age-based, or size-based is deferred.
```
with:
```
2. **Multiple snapshot retention.** v1 should keep at least the newest two successful snapshots. Whether retention should be count-based, age-based, or size-based is deferred. v1 ships no automatic retention; see §7 "Snapshot retention (deferred v1)" for the documented consequence (operator-managed directory pruning until a policy lands). When chosen, the policy gets a dedicated Story under Epic 7.
```

**Edit 4 — SPEC-002 §5.6 Snapshot Trigger Policy:** append a "Graceful-shutdown orchestration" sentence after the existing "When to override" paragraph.

Replace:
```
**When to override:** Applications that require bounded recovery time and cannot guarantee graceful shutdown (e.g., processes that may be killed abruptly) may set `SnapshotInterval > 0` to trigger periodic snapshots, accepting the commit-latency cost. When using periodic mode, the executor MUST quiesce (stop accepting new writes) for the full duration of snapshot creation.
```
with:
```
**When to override:** Applications that require bounded recovery time and cannot guarantee graceful shutdown (e.g., processes that may be killed abruptly) may set `SnapshotInterval > 0` to trigger periodic snapshots, accepting the commit-latency cost. When using periodic mode, the executor MUST quiesce (stop accepting new writes) for the full duration of snapshot creation.

**Graceful-shutdown orchestration owner:** SPEC-002 exposes `CreateSnapshot` and `DurabilityHandle.Close` as the two shutdown-relevant calls, but the engine-level ordering — quiesce executor → flush in-flight commits to durable → final `CreateSnapshot` → `DurabilityHandle.Close` — is owned by SPEC-003 (Transaction Executor shutdown sequence; tracked via SPEC-003 audit §2.5 and Session 9). Story 5.2 implements `CreateSnapshot` correctness; sequencing it relative to executor lifecycle is not a SPEC-002 concern.
```

**Edit 5 — Story 5.2 Design Notes:** append.

```
- Graceful-shutdown ordering is owned by SPEC-003, not this story. SPEC-002 §5.6 pins the two-call contract (final `CreateSnapshot` → `DurabilityHandle.Close`); the engine-level orchestration that decides when to fire it (executor quiesce + in-flight flush) lands in SPEC-003 Session 9.
```

**Grep:** `rtk grep -n "schema-static\|graceful.shutdown\|snapshot retention" docs/decomposition/002-commitlog/` — confirm each topic has exactly one normative home.

**Commit:** `docs: Lane B SPEC-002 residue — §2.11/§2.12/§2.13/§5.2-§5.5 ownership GAPs`

---

## Task 10 — DIVERGE §3.2-§3.7 divergence block

**Files:**
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` — insert new §12, renumber Open Questions → §13, Verification → §14

**Edit 1 — insert new section** between current §11 (Interfaces to Other Subsystems) and current §12 (Open Questions):

```markdown
## 12. Divergences from SpacetimeDB

Shunter's clean-room spec intentionally departs from SpacetimeDB in several places. Each divergence below is grounded in `reference/SpacetimeDB/` behavior but is an explicit v1 choice. Future specs or implementations should not "add parity" without revisiting the tradeoff documented here. (§3.1 — BSATN naming — is documented inline at §3.1 / §3.3 as the canonical disclaimer; not repeated here.)

### 12.1 No offset index file; recovery performs a linear scan

SpacetimeDB maintains a per-segment offset index (`tx_offset → byte_pos`) so replay can seek in O(log) instead of scanning. Shunter has no offset index. Story 6.3 `ReplayLog` skips records by decoding framing and discarding when `tx_id ≤ fromTxID`; cost is O(total records since log origin), not O(records after snapshot).

Rationale: the recovery-time target in §10 (`< 5 s` for snapshot + 10k log records) is achievable without an index, and v1 prioritizes a single canonical replay path over a second indexed path that would have to be kept in sync. Revisit if recovery latency shows up as a bottleneck under long-history workloads with infrequent snapshots — in that case, an offset-index sidecar file (one per segment) is the cheapest fix.

### 12.2 Single TX per record vs SpacetimeDB 1–65535-TX commits

Shunter writes exactly one transaction per log record. SpacetimeDB's commit-record framing supports 1–65535 transactions per record (an `n` field that Shunter omits — see §2.3 "Why no `n`").

Rationale: per-record framing overhead (18 bytes) is small relative to per-transaction payload size (typical: ≥256 B); batching only helps when payloads are tiny. v1 prioritizes format simplicity over a throughput optimization that requires a second decode path. Revisit if profiling shows fsync is amortized away and record framing is now the bottleneck.

### 12.3 Replay strictness — any `ApplyChangeset` error is fatal

Story 6.3 (`ReplayLog`) treats any `ApplyChangeset` error during replay as fatal. SPEC-002 §6.5 is symmetric for log-history conditions (gaps, overlaps, out-of-order). SpacetimeDB's `replay_insert` tolerates idempotent duplicates for system-meta rows.

Rationale: fail-fast during recovery surfaces corrupt-log / schema-mismatch conditions immediately rather than masking them. Shunter has no system-meta tables that need duplicate-tolerance (system tables in v1 are deferred to SPEC-006 §3.3). The cost is that idempotent re-replay after a crash-during-replay will abort; paired with SPEC-001 §2.7 (`ApplyChangeset` is not idempotent) and SPEC-002's exactly-once replay guarantee, this is the intended shape.

### 12.4 First TxID is 1, not 0

The first committed transaction has `tx_id = 1`. `tx_id = 0` is reserved as the pre-commit sentinel returned by `DurableTxID()` before any fsync lands and surfaced through SPEC-005 `ReducerCallResult.TxID = 0` for failed-before-allocation cases (SPEC-005 §2.2 / §8.7). SpacetimeDB's `tx_offset` starts at 0.

Rationale: keeping `0` as a "no transaction" sentinel makes uninitialized-reads loud throughout the system (executor dequeue, durability handle, wire protocol, fan-out metadata). Cost is one offset bit of address space, which is irrelevant at v1 scale.

### 12.5 Single auto-increment sequence per table (implicit)

§5.2 sequences section stores one `(table_id, next_id)` pair per table; Story 5.3 `Sequences map[TableID]uint64`; SPEC-001 Story 8.1/8.3 models `Table.SequenceValue() (uint64, bool)` as a single counter. SpacetimeDB's `st_sequence` system table supports multiple sequences per table (one per auto-increment column).

Rationale: SPEC-006 §9 declares at most one `AutoIncrement` column per table in v1. Multi-sequence support requires schema changes to expose multiple counters per table and snapshot-format changes to serialize them. Both deferred. When v2 adds either, `(table_id, next_id)` becomes `(table_id, sequence_id, next_id)`.

### 12.6 No segment compression / sealed-immutable marker

Shunter has no segment compression and no sealed-immutable bit. Compaction (§7) is delete-only — segments fully covered by a snapshot are removed, never compressed. SpacetimeDB can mark sealed segments immutable and zstd-compress them.

Rationale: v1 deferred snapshot compression (§12 OQ#5) for the same reason — disk is cheap, and the format-stability cost of adding compression before the uncompressed format is proven outweighs the I/O savings at v1 scale. When compression lands, sealed segments and snapshots can share the same compression layer.
```

**Edit 2 — renumber:**
- `## 12. Open Questions` → `## 13. Open Questions`
- `## 13. Verification` → `## 14. Verification`

**Grep:** `rtk grep -n "^## " docs/decomposition/002-commitlog/SPEC-002-commitlog.md` — confirm §12 Divergences, §13 Open Questions, §14 Verification with no duplicates. Also `rtk grep -rn "SPEC-002.*§12\|SPEC-002.*§13\|SPEC-002.*§14" docs/decomposition/` — confirm no external cross-refs to the renumbered sections (none expected; pre-Session 8 SPEC-002 has §12 OQ + §13 Verification with no external citers).

**Commit:** `docs: Lane B SPEC-002 residue — §3.2–§3.7 divergence block`

---

## Task 11 — NIT bundle (§4.1, §4.4, §4.5, §4.6, §4.7, §4.8)

Single commit. Six small-scope edits. (§4.2 / §4.3 already closed via B3/B5; §4.6 EPICS dep arrow already landed in Task 8 — it's listed there for cohesion. Just NIT touches here.)

**Files:**
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §5.3 (header authority note), §4.3 (struct mutex), §6.1 (.log cross-ref), §8 (FsyncMode placeholder)
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.2-record-framing.md` (CRC docs)

**Edit 1 — §5.3 header authority note (§4.1 NIT):** append a sentence after the existing per-column trailer paragraph (the long one ending "match the live encoder at `commitlog/snapshot_io.go`."). Find the paragraph and add:

```

Note on `schema_version` storage: this field is also written into the snapshot file header (§5.2) before the Blake3 hash. The header copy is authoritative when the two disagree, allowing recovery to short-circuit a schema-mismatched snapshot before recomputing Blake3 (Cluster A A3 / SPEC-006 §6.1). The body copy here is part of the schema codec output to keep `EncodeSchemaSnapshot` self-contained for unit testing; recovery prefers the header.
```

**Edit 2 — §4.3 durabilityWorker struct (§4.5 NIT):** soften to match live.

Replace:
```go
type durabilityWorker struct {
    ch        chan durabilityItem    // bounded; capacity = ChannelCapacity (default 256)
    durable   atomic.Uint64          // last fsynced TxID; read by DurableTxID()
    fatalErr  atomic.Pointer[error]  // nil until first fatal write/sync/rotate error
    closing   atomic.Bool            // true after Close begins
    done      chan struct{}          // closed when goroutine exits
}
```
with:
```go
type durabilityWorker struct {
    ch        chan durabilityItem        // bounded; capacity = ChannelCapacity (default 256)
    durable   atomic.Uint64              // last fsynced TxID; read by DurableTxID() lock-free
    stateMu   sync.Mutex                 // guards waiters, fatalErr, closing
    waiters   map[TxID][]chan TxID       // pending WaitUntilDurable subscribers (§4.2)
    fatalErr  error                      // first latched fatal write/sync/rotate error
    closing   bool                       // true after Close begins
    done      chan struct{}              // closed when goroutine exits
}
```

Update the trailing paragraph "**Why `atomic.Uint64` for durable offset:**…" — replace with:
```
**Why `atomic.Uint64` for durable offset:** The executor reads `DurableTxID()` from its own goroutine. Atomic read/write avoids a mutex for this single integer on the hot path.

**Why `stateMu` for waiters / fatalErr / closing:** `WaitUntilDurable` (§4.2) needs a per-TxID subscriber map; `fatalErr` and `closing` are read by both the worker goroutine and external callers (`EnqueueCommitted`, `Close`). One mutex covers all three because they are touched together at lifecycle boundaries (fatal latch, close drain, waiter signal). Hot-path commits do not contend on `stateMu` — only `durable` (atomic) is touched per-batch.
```

**Edit 3 — Story 2.2 Design Notes (§4.4 NIT):** append.

```
- The `Record` struct is the in-memory form. On-disk framing prepends `crc` computed at write time (`ComputeRecordCRC`) and verified on read. CRC is intentionally not a struct field: storing it would let callers fabricate inconsistent values; recomputing on read is cheap (CRC32C is hardware-accelerated) and ties integrity to the on-disk bytes rather than a Go-side claim.
```

**Edit 4 — §6.1 segment file extension cross-ref (§4.7 NIT):** append a sentence to step 1.

Replace:
```
1. **Scan commit log segments first.** List `commitlog/` files sorted by name and validate that segment start TX IDs are strictly increasing.
```
with:
```
1. **Scan commit log segments first.** List `commitlog/*.log` files (the `.log` extension is the segment-file convention pinned in §2.1 directory layout) sorted by name and validate that segment start TX IDs are strictly increasing.
```

**Edit 5 — §8 FsyncMode placeholder (§4.8 NIT):** add a new field to the `CommitLogOptions` struct.

After the `DrainBatchSize` field and before `SnapshotInterval`, insert:
```

    // FsyncMode: how aggressively the durability worker fsyncs.
    // v1 ships only Batch (drain a batch of up to DrainBatchSize records,
    // then one fsync). PerTx (fsync after every record) is reserved for v2
    // when the durability/latency knob in §13 OQ#3 lands.
    // Default: Batch. Setting any other value in v1 returns ErrUnknownFsyncMode
    // at handle construction.
    FsyncMode FsyncMode
```

Add a new constant block immediately above the `CommitLogOptions` struct:
```go
type FsyncMode uint8

const (
    FsyncBatch FsyncMode = 0  // v1 default; see §4.4
    FsyncPerTx FsyncMode = 1  // reserved for v2; rejected by v1 NewDurabilityWorker
)
```

**Grep:** `rtk grep -n "stateMu\|FsyncMode\|FsyncBatch\|schema_version.*authoritative\|.log\b" docs/decomposition/002-commitlog/` — confirm each NIT lands at exactly one site.

**Commit:** `docs: Lane B SPEC-002 residue — §4.1/§4.4/§4.5/§4.7/§4.8 NIT bundle`

---

## Task 12 — Tracking-doc refresh (AUDIT_HANDOFF.md)

**Files:**
- `AUDIT_HANDOFF.md`

**Edits:**

1. **Top-of-file intro block** (lines 4–5) — advance Lane B cursor from "Session 8 (SPEC-002 residue cleanup)" to "Session 9 (SPEC-003 residue cleanup)".

2. **§B.2 SPEC-002 table** — flip every `open` row to `closed`. Rows: §1.1, §1.3, §1.4, §2.1, §2.2, §2.4, §2.6, §2.8, §2.10, §2.11, §2.12, §2.13, §3.2, §3.3, §3.4, §3.5, §3.6, §3.7, §4.1, §4.3, §4.4, §4.5, §4.6, §4.7, §4.8, §5.2, §5.3, §5.4, §5.5, §5.6.

3. **§B.3 Session 8 row** — update stop-rule cell from `All open SPEC-002 rows resolved/deferred` to:
```
**(closed)** All 30 open SPEC-002 rows resolved. CRIT: §1.1 SnapshotInterval default = 0 in §8; §1.3 nextID section in §5.2 layout; §1.4 sequence-advance cross-ref to SPEC-001 Story 8.2 in §6.1 + Story 6.4 (no separate post-replay sweep). GAPs: ErrSnapshotInProgress + ErrTruncatedRecord added to §9 + §2.3/§6.4 cross-refs (§2.1/§2.2/§4.3); row_count uint32 (§2.4); restore-API named (`InsertRow` is the bulk-restore primitive — SPEC-001 Story 8.3 + §11 + Story 6.4) (§2.6/§5.6); durable_horizon = +∞ when segments empty (§2.8); AppendMode normative in §6.4 + EPICS dep arrow Epic 6→4 (§2.10); schema-static encoder note Story 3.1 (§2.11/§5.2); snapshot retention deferred-v1 documented in §7 + §12 OQ#2 (§2.12/§5.4); graceful-shutdown ownership cross-ref to SPEC-003 in §5.6 + Story 5.2 (§2.13/§5.5). New §12 Divergences block (6 entries: offset index, single-TX/record, replay strictness, first TxID = 1, single sequence/table, no compression). NIT bundle: schema_version header authoritative note (§5.3), `stateMu` + waiters in §4.3 struct (§4.5), Record CRC docs Story 2.2 (§4.4), .log extension cross-ref §6.1 (§4.7), FsyncMode placeholder §8 (§4.8).
```

4. **§B.5 footer Cursor:** advance from Session 8 to Session 9.

**Commit:** `docs: close Lane B SPEC-002 residue — Session 8 tracking update`

---

## Task 13 — Verification pass

1. `rtk git status` — clean working tree (plan file is untracked and stays untracked).
2. `rtk grep -n "| open " AUDIT_HANDOFF.md` — no open SPEC-002 rows remain in §B.2 table.
3. `rtk git log --oneline -15` — confirm ~12 new commits landed:
   - §1.1 SnapshotInterval default
   - §1.3 nextID section
   - §1.4 sequence-advance cross-ref
   - §2.1/§2.2/§4.3 error catalog
   - §2.4 row_count
   - §2.6/§5.6 restore-API
   - §2.8 durable_horizon
   - §2.10 AppendMode
   - §2.11/§2.12/§2.13/§5.2-§5.5 ownership
   - §3.2-§3.7 divergence block
   - NIT bundle
   - tracking-doc refresh
4. `rtk grep -n "^## " docs/decomposition/002-commitlog/SPEC-002-commitlog.md` — confirm §12 Divergences, §13 Open Questions, §14 Verification exist with no duplicates.
5. `rtk grep -n "Session 8\|Session 9" AUDIT_HANDOFF.md` — cursor advanced.
6. `rtk grep -n "TD-128\|TD-129" TECH-DEBT.md` — confirm no new drift entries (or, if any landed, they carry live `file:line` citations per §B.0).

---

## Drift log (if edits outrun live code)

Possible drift candidates and their resolution this session:

- **§1.1 SnapshotInterval = 0** — live `commitlog/durability.go` `DefaultCommitLogOptions()` already returns `SnapshotInterval: 0` (per audit §1.1). Spec change matches live. No drift.
- **§1.3 nextID section** — live `commitlog/snapshot_io.go:308–319` already writes `next_id_count` + `(table_id, next_id)` pairs between sequences and tables (per audit §1.3). Spec change matches live. No drift.
- **§2.4 row_count uint32** — live `commitlog/snapshot_io.go:332` writes `uint32` (per audit §2.4). Spec change matches live. No drift.
- **§2.6 restore-API named as `InsertRow`** — live `store/recovery.go:37` calls `Table.InsertRow` directly; `Table.InsertRow` already calls `insertIntoIndexes` (per `store/table.go:65`). Spec deliberately does NOT introduce `RestoreRow` / `RebuildIndexes` to avoid drift. No drift.
- **§2.10 AppendMode promotion** — live exposes `RecoveryResumePlan` (`commitlog/recovery.go:11`) and routes durability startup through it (per audit §2.10). Spec promotion to MUST matches live's normative use. No drift.
- **§4.5 stateMu in §4.3 struct** — live `commitlog/durability.go:46` uses `stateMu sync.Mutex` for `waiters`/`closing`/`fatalErr`; `durable` is `atomic.Uint64` (verified via `rtk grep`). Spec change matches live. No drift.
- **§4.8 FsyncMode placeholder** — purely additive to spec; live has no `FsyncMode` field today. The placeholder's v1 default (`FsyncBatch = 0`) means `CommitLogOptions{}` zero-value still produces correct behavior, so live is forward-compatible without a code change. **Potential TD-128:** when live picks up `FsyncMode`, the `NewDurabilityWorker` constructor should validate `opts.FsyncMode == FsyncBatch` and return `ErrUnknownFsyncMode` for any other value. No live drift today (zero-value works); flag for Session 12+.

If Task 11 §4.8 is judged too aspirational (introducing a sentinel `ErrUnknownFsyncMode` that v1 spec-only and live cannot enforce), fall back to a softer wording: declare `FsyncMode` reserved-for-v2 in §13 OQ#3 only, no struct field. Decision: keep the field but log TD-128 as the implementation pickup item. This mirrors Session 4.5 precedent (TD-125/126/127 for sentinel-wrapping aspirations not yet enforced live).

**Provisional TD-128 entry** (add to TECH-DEBT.md if Task 11 lands as planned):
```
| TD-128 | commitlog/durability.go:63 | NewDurabilityWorker does not validate opts.FsyncMode against the v1 reserved values (FsyncBatch=0). SPEC-002 §8 declares FsyncPerTx=1 reserved for v2 with ErrUnknownFsyncMode rejection at construction. Current live accepts any uint8 silently. | Session 12+ |
```

If no TD-128 entry feels warranted (FsyncMode is purely a forward-compat reservation and live is correct because zero-value = Batch), skip it. Reassess at Task 13 verification.

---

## Self-review

- Every `open` SPEC-002 row in §B.2 has a task above:
  - CRIT (§1.1, §1.3, §1.4) → Tasks 1, 2, 3
  - GAP (§2.1, §2.2, §2.4, §2.6, §2.8, §2.10, §2.11, §2.12, §2.13) → Tasks 4, 5, 6, 7, 8, 9
  - DIVERGE (§3.2, §3.3, §3.4, §3.5, §3.6, §3.7) → Task 10
  - NIT (§4.1, §4.3, §4.4, §4.5, §4.6, §4.7, §4.8) → Task 11 (+ §4.6 EPICS dep arrow landed in Task 8)
  - GAP §5.x (§5.2, §5.3, §5.4, §5.5, §5.6) → overlap rows resolved at the same edits as their §1/§2 twins (Tasks 2, 3, 6, 9)
  - Count: 30 rows, all mapped.
- Commit groupings match natural seams per §B.0 ("one commit per resolved finding / coherent bundle; small reviewable chunks; not a session-sized mega-commit"). Targeting ~12 content commits + 1 tracking commit, mirroring Session 7's 11+1 cadence.
- No live-code directory touched in any task. All edits land in `docs/decomposition/002-commitlog/**`, two cross-spec docs in `docs/decomposition/001-store/**` (Story 8.3 + §11 only — narrow, audit-mandated), and `AUDIT_HANDOFF.md`.
- Cross-spec edits touch SPEC-001 Story 8.3 + SPEC-001 §11 only (Task 6 §2.6/§5.6). These are explicitly audit-flagged ("SPEC-001 Story 8.3 should name the restore surface — SPEC-002 Story 6.4 should reference those exact names"). No SPEC-001 edit risks reopening Session 7 closures (Story 8.2 / 8.3 / §11 already updated in Session 7; this session adds the bulk-restore subsection without re-touching the `max(current, provided)` bullet or the `AsBytes` / float ±0 / Bound work).
- Renumbering §12→§13 / §13→§14 in SPEC-002: verified via `rtk grep -rn "SPEC-002.*§12\|SPEC-002.*§13\|SPEC-002.*§14" docs/decomposition/` will be re-run at Task 13 verification. Pre-Session 8 SPEC-002 has §12 OQ + §13 Verification with no external cross-refs (confirmed by the absence of "SPEC-002 §12" / "SPEC-002 §13" in the residue tracker; only SPEC-001's §12/§13/§14 renumber created cross-ref churn in Session 7, which there were also none).
- AUDIT_HANDOFF.md row counts: SPEC-002 table currently has 25 numbered rows + 5 §5.x rows = 30 row-count claim is accurate (verified by reading lines 239–275).
- Section numbers, story IDs, and SPEC cross-refs used above all correspond to content read in preparation: SPEC-002 full text + EPICS.md + Stories 2.1/2.2/2.4/2.5/3.1/4.1/4.3/5.1/5.2/5.3/5.4/6.1/6.2/6.3/6.4 + SPEC-001 Story 5.1/8.3 + SPEC-AUDIT.md SPEC-002 section.
