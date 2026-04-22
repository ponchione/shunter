# Phase 2 Slice 4 — applied / light / committed rows-shape parity audit

Records the decision for the remaining rows-shape divergence called
out in `NEXT_SESSION_HANDOFF.md` under OI-001 A1 protocol wire-close.
Covers both handoff items — applied-envelope rows shape
(`SubscribeRows` wrapper) and `TransactionUpdateLight.Update`
(`DatabaseUpdate` wrapper) — together because they share the same
inner wrapper chain (`TableUpdate` → `CompressableQueryUpdate` →
`QueryUpdate` → `BsatnRowList`) and the same load-bearing rationale.

Written clean-room. Reference paths below are cited for grounding
only; do not copy or transliterate Rust source.

Matches the closed-as-documented-divergence pattern established by
`docs/parity-phase4-slice2-record-shape.md` (2γ) and the subprotocol
retention decision in `protocol/parity_subprotocol_test.go`.

## Reference shape (target)

`reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`.

### Outer envelopes

```
SubscribeApplied<F> (v1.rs:317)
  request_id                         : u32
  total_host_execution_duration_micros: u64
  query_id                           : QueryId (u32)
  rows                               : SubscribeRows<F>

UnsubscribeApplied<F> (v1.rs:331)
  request_id                         : u32
  total_host_execution_duration_micros: u64
  query_id                           : QueryId (u32)
  rows                               : SubscribeRows<F>

SubscribeMultiApplied<F> (v1.rs:380)
  request_id                         : u32
  total_host_execution_duration_micros: u64
  query_id                           : QueryId (u32)
  update                             : DatabaseUpdate<F>

UnsubscribeMultiApplied<F> (v1.rs:394)
  request_id                         : u32
  total_host_execution_duration_micros: u64
  query_id                           : QueryId (u32)
  update                             : DatabaseUpdate<F>

TransactionUpdateLight<F> (v1.rs:493)
  request_id                         : u32
  update                             : DatabaseUpdate<F>

UpdateStatus<F>::Committed (v1.rs:526)
  (enum payload)                     : DatabaseUpdate<F>
```

Outer field order for the four applied envelopes + light + heavy is
already pinned by:

- `protocol/parity_applied_envelopes_test.go`
- `protocol/parity_transaction_update_test.go`
- `protocol/parity_message_family_test.go`

Only the inner rows payload is still diverged.

### Inner wrapper chain

```
SubscribeRows<F> (v1.rs:305)
  table_id   : TableId (u32)
  table_name : RawIdentifier (Box<str>)
  table_rows : TableUpdate<F>

DatabaseUpdate<F> (v1.rs:541)
  tables     : Vec<TableUpdate<F>>

TableUpdate<F> (v1.rs:570)
  table_id   : TableId (u32)
  table_name : RawIdentifier (Box<str>)
  num_rows   : u64
  updates    : SmallVec<[F::QueryUpdate; 1]>

CompressableQueryUpdate<F> (v1.rs:623)  — F::QueryUpdate for BSATN
  enum discriminant : u8
    Uncompressed(QueryUpdate<F>) = 0
    Brotli(Bytes)                = 1
    Gzip(Bytes)                  = 2

QueryUpdate<F> (v1.rs:631)
  deletes : F::List   — deletes-first ordering
  inserts : F::List

BsatnRowList (common.rs:62)             — F::List for BSATN
  size_hint  : RowSizeHint (enum)
  rows_data  : Bytes (flat byte buffer, no per-row length prefix)

RowSizeHint (common.rs:86)
  enum discriminant : u8
    FixedSize(RowSize = u16)         = 0
    RowOffsets(Arc<[RowOffset = u64]>) = 1
```

## Shunter shape today

`protocol/wire_types.go`, `protocol/server_messages.go`,
`protocol/rowlist.go`.

### Outer envelopes

```
SubscribeSingleApplied (server_messages.go:48)
  RequestID                        : uint32
  TotalHostExecutionDurationMicros : uint64
  QueryID                          : uint32
  TableName                        : string
  Rows                             : []byte   — EncodeRowList payload

UnsubscribeSingleApplied (server_messages.go:66)
  RequestID                        : uint32
  TotalHostExecutionDurationMicros : uint64
  QueryID                          : uint32
  HasRows                          : bool     — Shunter-local optionality
  Rows                             : []byte   — present iff HasRows

SubscribeMultiApplied (server_messages.go:106)
  RequestID                        : uint32
  TotalHostExecutionDurationMicros : uint64
  QueryID                          : uint32
  Update                           : []SubscriptionUpdate

UnsubscribeMultiApplied (server_messages.go:122)
  RequestID                        : uint32
  TotalHostExecutionDurationMicros : uint64
  QueryID                          : uint32
  Update                           : []SubscriptionUpdate

TransactionUpdateLight (server_messages.go:153)
  RequestID                        : uint32
  Update                           : []SubscriptionUpdate

StatusCommitted (server_messages.go:167)
  Update                           : []SubscriptionUpdate
```

### Inner SubscriptionUpdate

```
SubscriptionUpdate (wire_types.go:26)
  SubscriptionID : uint32  — Shunter-local; no reference analogue
  TableName      : string
  Inserts        : []byte  — EncodeRowList payload (inserts first)
  Deletes        : []byte  — EncodeRowList payload

writeSubscriptionUpdates wire layout (server_messages.go:667):
  count                  : u32 LE
  for each:
    subscription_id      : u32 LE
    table_name           : Box<str>
    inserts              : Bytes
    deletes              : Bytes
```

### Inner RowList

```
EncodeRowList wire layout (rowlist.go:9)
  row_count              : u32 LE
  for each row:
    row_len              : u32 LE
    row_data             : [row_len]byte
```

SPEC-005 §3.4 (`docs/decomposition/005-protocol/SPEC-005-protocol.md:132`)
documents this simpler per-row-length-prefix layout as a deliberate
Shunter choice against reference `BsatnRowList` / `RowSizeHint`, with
the revisit trigger "if row delivery bandwidth is a bottleneck for
fixed-schema tables" deferred to v2.

## Delta audit

| # | Site | Reference shape | Shunter shape | Category |
|---|---|---|---|---|
| 1 | `SubscribeSingleApplied.rows` | `SubscribeRows { table_id, table_name, table_rows: TableUpdate }` | flat `TableName + Rows []byte` | rows-wrapper elision (inner BsatnRowList still divergent) |
| 2 | `UnsubscribeSingleApplied.rows` | `SubscribeRows` (required) | flat `HasRows + Rows []byte` (optional) | optional-flag smuggle + rows-wrapper elision |
| 3 | `SubscribeMultiApplied.update` | `DatabaseUpdate { tables: Vec<TableUpdate> }` | `[]SubscriptionUpdate` | DatabaseUpdate elision; extra Shunter-local `SubscriptionID` field |
| 4 | `UnsubscribeMultiApplied.update` | `DatabaseUpdate` | `[]SubscriptionUpdate` | same as #3 |
| 5 | `TransactionUpdateLight.update` | `DatabaseUpdate` | `[]SubscriptionUpdate` | same as #3 |
| 6 | `StatusCommitted` (payload of `UpdateStatus::Committed`) | `DatabaseUpdate` | `[]SubscriptionUpdate` | same as #3 |
| 7 | inner `QueryUpdate` field order | `deletes, inserts` | `Inserts, Deletes` on `SubscriptionUpdate` (inserts first) | inner field-order flip |
| 8 | inner `TableUpdate.num_rows` | `u64` sum of rows across updates | absent | missing field (recomputable from payload) |
| 9 | inner `CompressableQueryUpdate` tagged union | u8 tag + `Uncompressed(QueryUpdate)` \| `Brotli(Bytes)` \| `Gzip(Bytes)` | absent; rows payload is raw | missing envelope (compression is handled at the outer `SERVER_MSG_COMPRESSION_TAG` level instead — see `protocol/compression.go` and `P0-PROTOCOL-002`) |
| 10 | inner `F::List` row batch | `BsatnRowList { size_hint, rows_data }` with `RowSizeHint::{FixedSize(u16) \| RowOffsets([u64])}` | `EncodeRowList` per-row-length-prefix layout | deliberate SPEC-005 §3.4 divergence, deferred to v2 |

## Decision: close as documented divergence

**Every row of the delta audit collapses onto delta #10** — the
`BsatnRowList` vs Shunter per-row-length-prefix layout. Closing any
of #1-#9 in isolation produces a wire that is still not reference-
parity at the rows payload level, because the inner `F::List`
encoding remains distinct. The only meaningful close is a coordinated
close across #1-#10 simultaneously, which is a multi-phase rewrite
spanning `rowlist.go`, every emit site, every `parity_*_test.go` byte
shape, and an on-the-wire migration story.

### Rationale for not closing now

1. **Load-bearing inner divergence.** SPEC-005 §3.4
   (`docs/decomposition/005-protocol/SPEC-005-protocol.md:132-143`)
   explicitly adopts the per-row-length-prefix layout as Shunter's
   row batch format and names `BsatnRowList` / `RowSizeHint` as a v2
   revisit conditional on bandwidth evidence. Wrapping this inner in
   reference-style outer structs (`SubscribeRows`, `TableUpdate`,
   `CompressableQueryUpdate`) without also closing #10 produces a
   hybrid wire that is neither Shunter's documented SPEC-005 shape
   nor reference parity — a strictly worse state than either end.

2. **No operational-replacement trigger yet.** Byte-parity on the
   rows payload is only load-bearing for a client that wants to
   consume Shunter-emitted and reference-emitted streams with the
   same decoder. No such consumer exists today. The inner
   `BsatnRowList` revisit trigger named in SPEC-005 §3.4 ("row
   delivery bandwidth bottleneck for fixed-schema tables") has not
   surfaced either.

3. **Coordination cost.** Closing #1-#10 together would restructure:
   - the `SubscriptionUpdate` wire type and every emit site
     (`protocol/fanout_adapter.go`, `executor/protocol_inbox_adapter.go`,
     `protocol/send_txupdate.go`, `handle_subscribe_*.go`,
     `handle_unsubscribe_*.go`);
   - the rowlist codec (`protocol/rowlist.go`);
   - every existing byte-shape pin
     (`parity_applied_envelopes_test.go`,
     `parity_transaction_update_test.go`,
     `send_txupdate_test.go`, `handle_subscribe_test.go`,
     `handle_unsubscribe_test.go`);
   - the `SubscriptionID` plumbing currently carried on the wire per
     update (reference has no analogue — it correlates by QueryID at
     the envelope level, not per-TableUpdate);
   - the caller-side heavy/light split in
     `subscription/fanout_worker.go`.

   This is an order of magnitude larger than a single wire-close
   slice should be. Parity work at this size belongs in its own
   multi-slice phase with an explicit SPEC-005 §3.4 revisit doc.

4. **Deliberate architectural divergence in #3 / #6 / #9.**
   - `SubscriptionID` on `SubscriptionUpdate` (delta #3) is
     load-bearing for Shunter's per-connection subscription
     accounting and is not accidental — removing it would force the
     executor and fan-out worker to rederive correlation from query
     ids and table names.
   - `StatusCommitted` (delta #6) is emitted by the heavy envelope
     for the caller only; Shunter routes the same row delta to
     non-callers via `TransactionUpdateLight`, so a single
     `DatabaseUpdate` wrapper type would have to satisfy both
     heavy/light emit sites simultaneously.
   - `CompressableQueryUpdate` (delta #9) duplicates functionality
     already pinned at the outer envelope level by
     `P0-PROTOCOL-002`. Introducing a second compression layer
     inside the rows payload without retiring the outer one is net
     negative; retiring the outer one is a separate
     `P0-PROTOCOL-002` reopen.

### What this slice produces

1. **This decision doc** as the locked audit and rationale for the
   rows-shape cluster. Every reference/Shunter delta is named,
   categorized, and attributed to either the SPEC-005 §3.4 deferral
   (#10) or a downstream consequence of it (#1-#9).

2. **A rolled-up pin file** (`protocol/parity_rows_shape_test.go`)
   that latches the current Shunter flat-rows shape across all six
   affected envelopes + the inner `SubscriptionUpdate` layout +
   rowlist format as a **canonical contract**. Individual byte-shape
   pins continue to live in `parity_applied_envelopes_test.go` and
   `parity_transaction_update_test.go`; the new file adds:
   - a dedicated byte-shape pin for `TransactionUpdateLight`
     (previously only exercised by round-trip integration tests);
   - a field-order cross-check table covering all six envelopes
     with the decision-doc reference embedded in the comment so
     future readers land on this doc without having to grep;
   - an explicit `SubscriptionUpdate` inner layout pin (inserts-
     before-deletes, Shunter-local `SubscriptionID` presence).

3. **A ledger update** flipping the rows-shape line under the Phase
   2 protocol bucket from `open` (implicit, tracked only in OI-001)
   to `closed (divergences recorded)`, citing this doc.

4. **A tech-debt update** naming this slice closed under OI-001 and
   carrying the coordinated BsatnRowList + wrapper-chain close as a
   named deferral rather than an implicit gap.

5. **A handoff update** removing the two rows-shape items from
   `NEXT_SESSION_HANDOFF.md` "Known remaining divergences" and
   recording the closed-state one-liner.

### What this slice does *not* produce

- No wire change. `EncodeServerMessage` / `DecodeServerMessage`
  bytes are unchanged for every affected envelope.
- No new wire types. `SubscribeRows`, `DatabaseUpdate`,
  `TableUpdate`, `QueryUpdate`, `CompressableQueryUpdate`,
  `BsatnRowList`, `RowSizeHint` remain absent from Go.
- No changes to `protocol/rowlist.go` or per-row length prefixing.
- No changes to `SubscriptionUpdate` field order, field set, or
  serialization.
- No changes to emit sites in `fanout_adapter.go`,
  `send_txupdate.go`, or `executor/protocol_inbox_adapter.go`.
- No on-the-wire migration, no compatibility window, no dual-
  decode path.

These deferrals are explicit and named below.

## Pin plan

All new pins land in `protocol/parity_rows_shape_test.go`. Existing
byte-shape pins remain authoritative; the new file adds a rolled-up
cross-link and fills the one gap (TransactionUpdateLight byte shape).

### Envelope field-order cross-check

1. `TestParityRowsShapeEnvelopesFlatShape` — table-driven, one entry
   per affected envelope (Subscribe{Single,Multi}Applied,
   Unsubscribe{Single,Multi}Applied, TransactionUpdateLight,
   StatusCommitted), asserting the Shunter flat field names match
   the current layout exactly. Comment cites this decision doc as
   the rationale for why the field list is what it is.

### `TransactionUpdateLight` byte shape (new)

2. `TestParityTransactionUpdateLightWireShape` — construct a frame
   with RequestID + one SubscriptionUpdate, assert the wire bytes
   exactly match `[Tag][request_id: u32 LE][writeSubscriptionUpdates]`,
   round-trip through DecodeServerMessage. Matches the pin style in
   `parity_applied_envelopes_test.go`.

### Inner `SubscriptionUpdate` layout

3. `TestParitySubscriptionUpdateInnerLayout` — pins
   `writeSubscriptionUpdates` produces the
   `count + (subscription_id, table_name, inserts, deletes)` wire
   layout. Locks the Shunter-local `SubscriptionID` field presence
   and the inserts-before-deletes order (delta #7) as explicit
   contract, not accident.

### Inner rowlist pin re-reference

4. `TestParityRowsShapeRowListFormatReference` — import-time pin
   that `EncodeRowList` / `DecodeRowList` remain the per-row-length-
   prefix layout cited in SPEC-005 §3.4. Comment points at
   `docs/decomposition/005-protocol/SPEC-005-protocol.md:132-143`
   and at delta #10 of this doc.

## Acceptance gate

Close this slice only when all of:

- every pin listed above is landed and passing;
- `rtk go fmt`, `rtk go vet`, `rtk go test ./protocol/...` clean;
- `docs/parity-phase0-ledger.md` gains a Phase 2 Slice 4 row citing
  this doc;
- `TECH-DEBT.md` OI-001 references this doc as the closed rows-
  shape sub-slice;
- `docs/spacetimedb-parity-roadmap.md` Phase 2 section notes the
  closed rows-shape slice;
- `NEXT_SESSION_HANDOFF.md` "Current state" gains a one-liner for
  the rows-shape close, and the "Known remaining divergences" list
  is pruned of the two rows-shape items (they move to the carried-
  forward deferral list under this doc).

## Out-of-scope follow-ons (explicitly deferred by this slice)

Each of the following is a named reference-parity gap that is *not*
closed by Phase 2 Slice 4. Any future slice that wants to close one
must open its own decision doc.

- **`BsatnRowList` / `RowSizeHint` rows-data layout parity**
  (delta #10). SPEC-005 §3.4 deferral. Revisit trigger: row-delivery
  bandwidth bottleneck on fixed-schema tables, or a named consumer
  that needs to decode reference-emitted streams.

- **`SubscribeRows` / `TableUpdate` / `DatabaseUpdate` wrapper
  introduction** (deltas #1-#6, #8). Unblocked only by the above.
  Would restructure `SubscriptionUpdate`, emit sites, and every
  existing byte-shape pin. Needs its own decision doc when the
  inner layout close lands.

- **`CompressableQueryUpdate` inner tagged union** (delta #9).
  Duplicates outer-envelope compression (`P0-PROTOCOL-002`).
  Deferred; closing requires either retiring outer compression or
  accepting the double-layer as intentional.

- **`SubscriptionID` field removal from `SubscriptionUpdate`**
  (delta #3 consequence). Reference has no per-TableUpdate
  subscription id — correlation happens at the envelope QueryID
  level. Removing `SubscriptionID` on the wire requires the fan-out
  worker and per-connection subscription accounting to rederive
  correlation. Nontrivial; carry as a separate slice under OI-002
  rather than OI-001.

- **`inserts`/`deletes` field-order flip inside `QueryUpdate`**
  (delta #7). Cosmetic on its own but part of the wrapper-chain
  close. Do not close as a standalone micro-slice — that produces a
  mid-rewrite mismatch between the inserts-first wire and the
  deletes-first reference without delivering any parity value.

- **`TableUpdate.num_rows` recomputation** (delta #8). Missing
  field; trivially derivable from the payload. Adds 8 bytes per
  table. Close with the wrapper-chain slice.

- **`ServerMessage` variant tag byte value parity**
  (`TagIdentityToken = 1` vs reference variant-index 3). Pinned as
  an intentional divergence by
  `protocol/parity_message_family_test.go` `TestPhase2TagByteStability`.
  Out of scope for this slice.

- **`InitialSubscription` and `ProcedureResult`/`CallProcedure`
  variants**. Reference-only ServerMessage arms. `InitialSubscription`
  is reference-deprecated ("will be removed when we switch to
  `SubscribeSingle`"); `ProcedureResult`/`CallProcedure` are newer
  reference features without a Shunter analogue. Out of scope for
  OI-001 A1 and for this slice.

## Clean-room reminder

Reference citations above are grounding only. The new pins follow
the existing `protocol/parity_*_test.go` conventions (`bytes.Buffer`
round-trips, explicit byte-by-byte construction against reference
layouts, `msgFieldNames` reflection helpers). Do not copy reference
Rust field order or struct layouts into Go; this slice's job is to
pin the current distinct Shunter shape as canonical, not to import
the reference shape.
