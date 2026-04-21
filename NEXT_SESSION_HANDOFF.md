# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

## What just landed (2026-04-21, follow-up slice)

`:sender` caller-identity parameter parity on the narrow single-table SQL surface. Shunter now accepts `select * from s where id = :sender` and `select * from s where bytes = :sender` end-to-end and rejects `:sender` on any non-bytes column (the equivalent of the reference `check.rs:487-488` `select * from t where arr = :sender` rejection on Shunter's KindBytes-backed identity representation).

- Grounded anchors before edits:
  - `reference/SpacetimeDB/crates/expr/src/check.rs:435-440` for positive `:sender` shapes on identity / bytes columns.
  - `reference/SpacetimeDB/crates/expr/src/check.rs:487-488` for the rejection on non-identity / non-bytes columns (`select * from t where arr = :sender`).
- Production widening landed:
  - `query/sql/parser.go` tokenizes `:` + ident as `tokParam`, produces `Literal{Kind: LitSender}` only for `:sender` (case-insensitive), rejects any other `:name` parameter with `ErrUnsupportedSQL`.
  - `query/sql/coerce.go` adds `CoerceWithCaller(lit, kind, caller *[32]byte)`; `LitSender` materializes the caller identity as a fresh `types.NewBytes(caller[:])` on KindBytes columns and rejects on any other column kind. The legacy `Coerce` path rejects `LitSender` outright so callers that have not threaded caller identity cannot accidentally resolve it.
  - `protocol/handle_subscribe.go` extends `compileSQLQueryString`, `parseQueryString`, `compileSQLPredicateForRelations`, and `normalizeSQLFilterForRelations` to accept a `*types.Identity` caller and route coercion through `CoerceWithCaller`; `protocol/handle_subscribe_single.go` / `handle_subscribe_multi.go` / `handle_oneoff.go` thread `&conn.Identity` into the compile path.
- New parser / coerce / public-seam pins landed:
  - parser: `TestParseWhereSenderParameterOnIdentityColumn`, `TestParseWhereSenderParameterOnBytesColumn`, `TestParseWhereSenderParameterIsCaseInsensitive`, `TestParseWhereRejectsUnknownParameter`
  - coerce: `TestCoerceSenderWithoutCallerFails`, `TestCoerceSenderWithCallerToBytes`, `TestCoerceSenderRejectsNonBytesColumn`
  - protocol subscribe-single: `TestHandleSubscribeSingle_SenderParameterOnIdentityColumn`, `TestHandleSubscribeSingle_SenderParameterOnBytesColumn`, `TestHandleSubscribeSingle_SenderParameterOnStringColumnRejected`
  - protocol one-off: `TestHandleOneOffQuery_SenderParameterOnIdentityColumn`, `TestHandleOneOffQuery_SenderParameterOnBytesColumn`, `TestHandleOneOffQuery_SenderParameterOnStringColumnRejected`
- Scope kept narrow: no projection broadening, no `WHERE arr = :sender` path (non-bytes columns still reject), no `:other` parameter plumbing, no LIMIT / column-list widening, no multi-way join runtime work, no lifecycle/envelope churn. `SubscribeMulti` inherits the new compile path through the shared `compileSQLQueryString` seam but has no dedicated pin test (the narrow slice is covered by the subscribe-single path). Reopen if a specific `SubscribeMulti` `:sender` regression surfaces.
- Docs follow-through: `docs/current-status.md`, `docs/parity-phase0-ledger.md`, and `TECH-DEBT.md` now record the `:sender` parameter parity as a landed narrow SQL parity slice; the pinned tests are named in the ledger.

Verification run after landing the slice:
- `rtk go test ./query/sql -run 'TestParseWhereSenderParameter|TestParseWhereRejectsUnknownParameter|TestCoerceSender' -count=1`
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_SenderParameter|TestHandleOneOffQuery_SenderParameter' -count=1`
- `rtk go test ./query/sql ./protocol -count=1`
- `rtk go fmt ./query/sql ./protocol`
- `rtk go vet ./query/sql ./protocol`
- `rtk go test ./...`

Current clean-tree baseline:
- `Go test: 1193 passed in 10 packages`

Flaky test note: no known clean-tree intermittent tests remain after the 2026-04-21 subscription, scheduler, protocol lifecycle, message-family, and SQL/query-surface follow-through.

## Recommended next slice

Keep walking down the broader-SQL / query-surface parity backlog now that `:sender` on narrow single-table KindBytes columns is landed.

Best next grounded SQL options:
- parity extension of `:sender` into the narrow join-backed surface (`select * from s as r where r.bytes = :sender`, or a join filter whose leaf is `:sender` on an aliased relation). Same parser-level marker already lands; compile path already routes caller identity through `compileSQLPredicateForRelations` for both the `stmt.Join == nil` path and the join filter path — verify no residual gap, then add pins.
- a different reference-backed SQL shape from `reference/SpacetimeDB/crates/expr/src/check.rs` that is not yet pinned in Shunter.

Why this next:
- the `:sender` slice closed the last named narrow-SQL gap tied to `check.rs:435-440`.
- the next grounded reference-backed SQL shape is the cleanest continuation; broadening projection or parameter semantics beyond `:sender` is a bigger decision.
- this stays on externally visible SQL/query parity instead of reopening speculative Tier-B watch items with no failing pin.

If you do not take the SQL path next:
- prefer a concrete OI-004 lifecycle leak/hang site with a fresh failing test
- do not reopen the landed literal / quoted-identifier / `:sender` slices unless a regression appears

## Expected shape of the next session

1. Read the required startup docs in the listed order.
2. Treat the current worktree as landed SQL/query parity truth (quoted special-character identifiers, hex byte literals, float literals, `:sender` on narrow single-table KindBytes columns), not as unfinished envelope work.
3. Start with the next grounded SQL anchor from `reference/SpacetimeDB/crates/expr/src/check.rs` and the parity docs.
4. Preferred next slice: either `:sender` on the narrow join-backed surface, or another reference-backed SQL shape.
   - add failing parser/protocol/runtime pins first
   - verify the failure
   - implement the smallest parser/coercion/runtime widening that keeps unrelated SQL shapes rejected
   - re-run targeted tests, then `rtk go test ./...`
5. If the chosen slice turns out blocked by a wider runtime contract than expected, stop and choose the next narrow reference-backed SQL shape instead of broadening opportunistically.
6. Only after the suite is green, update the docs and this handoff again.


Prior closed anchors in the same calendar week (still landed, included here for continuity):
- OI-006 fanout per-subscriber slice-header aliasing sub-hazard — `docs/hardening-oi-006-fanout-aliasing.md`
- OI-005 `CommittedState.Table(id) *Table` raw-pointer contract pin — `docs/hardening-oi-005-committed-state-table-raw-pointer.md`
- OI-005 `StateView.ScanTable` iterator surface — `docs/hardening-oi-005-state-view-scan-aliasing.md`
- OI-004 dispatch-handler ctx sub-hazard — `docs/hardening-oi-004-dispatch-handler-context.md`
- OI-004 `forwardReducerResponse` ctx / Done lifecycle — `docs/hardening-oi-004-forward-reducer-response-context.md`
- OI-004 `ConnManager.CloseAll` disconnect-ctx sub-hazard — `docs/hardening-oi-004-closeall-disconnect-context.md`
- OI-004 outbound-writer supervision sub-hazard — `docs/hardening-oi-004-outbound-writer-supervision.md`
- OI-004 `superviseLifecycle` disconnect-ctx — `docs/hardening-oi-004-supervise-disconnect-context.md`
- OI-004 `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx — `docs/hardening-oi-004-sender-disconnect-context.md`
- OI-004 `watchReducerResponse` goroutine-leak escape route — `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
- OI-005 `StateView.SeekIndexRange` BTree-alias escape route — `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`
- OI-005 `StateView.SeekIndex` BTree-alias escape route — `docs/hardening-oi-005-state-view-seekindex-aliasing.md`
- OI-005 `CommittedSnapshot.IndexSeek` BTree-alias escape route — `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`
- OI-005 subscription-seam read-view lifetime sub-hazard — `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`
- OI-005 snapshot iterator mid-iter-close defense-in-depth sub-hazard — `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`
- OI-005 snapshot iterator use-after-Close sub-hazard — `docs/hardening-oi-005-snapshot-iter-useafterclose.md`
- OI-005 snapshot iterator GC retention sub-hazard — `docs/hardening-oi-005-snapshot-iter-retention.md`
- Phase 4 Slice 2 replay-horizon / validated-prefix (`P0-RECOVERY-001`) — `docs/parity-p0-recovery-001-replay-horizon.md`
- Phase 3 Slice 1 scheduled-reducer startup / firing ordering (`P0-SCHED-001`) — `docs/parity-p0-sched-001-startup-firing.md`
- Phase 2 Slice 3 lag / slow-client policy (`P0-SUBSCRIPTION-001`) — `docs/parity-phase2-slice3-lag-policy.md`

## Next realistic parity / hardening anchors

With `P0-RECOVERY-001`, `P0-SCHED-001`, `P0-SUBSCRIPTION-001` closed, all nine OI-005 enumerated sub-hazards closed, both enumerated OI-006 sub-hazards closed, and six OI-004 sub-hazards closed, the grounded options are:

### Option α — Broader SQL/query-surface parity beyond TD-142

This is now the best next grounded parity path.

What is still open:
- `docs/current-status.md`, `docs/spacetimedb-parity-roadmap.md`, and `docs/parity-phase0-ledger.md` now all agree that the remaining externally visible message-family follow-through is broader SQL/query-surface breadth rather than another `SubscriptionError` envelope tweak
- TD-142 plus the 2026-04-21 SQL/query follow-through (quoted special-character identifiers, hex byte literals, float literals, `:sender` on narrow single-table KindBytes columns) drained the named narrow slices, but broader accepted SQL/query shapes are still new parity work

Why prefer this now:
- the just-landed `:sender` slice closed the last named narrow-SQL gap tied to `check.rs:435-440` positive shapes
- externally visible parity still outranks speculative Tier-B watch items
- this keeps effort on client-visible behavior rather than reopening already-green message-family work

Likely code surfaces:
- `query/sql/parser.go`
- `query/sql/coerce.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_oneoff.go`
- `subscription/predicate.go`
- `subscription/validate.go`

Concrete shape:
- choose one exact remaining reference-backed SQL/query scenario
- add parser + public protocol/runtime pins first
- keep the slice narrow; do not reopen unrelated lifecycle or envelope work in the same session

### Option β — Continue Tier-B hardening

`TECH-DEBT.md` still carries:
- OI-004 remaining sub-hazards (other detached goroutines in `protocol/conn.go` / `lifecycle.go` / `outbound.go` / `keepalive.go`; `ClientSender.Send` no-ctx follow-on)
- OI-005: enumerated sub-hazards list now empty; OI-005 remains open as a theme because the envelope rule for raw `*Table` access is enforced by discipline and observational pins rather than machine-enforced lifetime.
- OI-006: enumerated sub-hazards list now empty; OI-006 remains open as a theme because the read-only row-payload contract is enforced by discipline and observational pins rather than machine-enforced immutability at the `types.ProductValue` boundary.
- OI-008 (top-level bootstrap missing)

Current judgment:
- do not force another OI-004 sub-slice unless a specific concrete leak site surfaces in live code or a failing test
- `ClientSender.Send` no-ctx remains a follow-on with no concrete consumer today
- the remaining detached-goroutine bullets are now watch items, not the best immediate slice

### Option γ — Format-level commitlog parity (Phase 4 Slice 2 follow-on)

With the replay-horizon / validated-prefix slice closed, the remaining commitlog parity work is format-level:
- offset index file (reference `src/index/indexfile.rs`, `src/index/mod.rs`)
- record / log shape compatibility (reference `src/commit.rs`, `src/payload/txdata.rs`)
- typed `error::Traversal` / `error::Open` enums
- snapshot / compaction visibility vs reference `repo::resume_segment_writer` contract

These are larger scope than a single narrow slice; each would need its own decision doc.

### Option δ — Pick one of the `P0-SCHED-001` deferrals

Each remaining scheduler deferral is a candidate for its own focused slice if workload evidence surfaces:
- `fn_start`-clamped schedule "now" (plumb reducer dispatch timestamp into `schedulerHandle`; ref `scheduler.rs:211-215`)
- one-shot panic deletion (second-commit post-rollback path; ref `scheduler.rs:445-455`)
- past-due ordering by intended time (sort in `scanAndTrackMaxWithContext`)

Prefer Option α over β/γ/δ unless live workload or reference evidence surfaces a stronger blocker.

## First, what you are walking into

The repo already has substantial implementation. Do not treat this as a docs-only project. Do not do a broad audit. Do not restart parity analysis from zero.

Your job is to continue from the current live state. Pick the next grounded anchor from `docs/spacetimedb-parity-roadmap.md`, `docs/parity-phase0-ledger.md`, or `TECH-DEBT.md`.

Clean-room reminder:
- parity target means matching externally meaningful behavior where required, not translating Rust source into Go
- `reference/SpacetimeDB/` stays research-only and read-only; do not copy, transliterate, or mechanically port code from it
- re-derive behavior from public docs, reference outcomes, and live Shunter contracts, then implement natively in Go

## Mandatory reading order

1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md`
8. `docs/parity-phase0-ledger.md`
9. `TECH-DEBT.md`
10. `docs/hardening-oi-006-row-payload-sharing.md` (closed slice — contract pin at the row-payload sharing seam)
11. `docs/hardening-oi-006-fanout-aliasing.md` (prior OI-006 sub-slice — slice-header isolation precedent)
12. `docs/hardening-oi-005-committed-state-table-raw-pointer.md` (prior OI-005 contract-pin precedent)
13. `docs/hardening-oi-005-state-view-scan-aliasing.md` (prior OI-005 sub-slice)
14. `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`
15. `docs/hardening-oi-005-state-view-seekindex-aliasing.md`
16. `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`
17. `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`
18. `docs/hardening-oi-004-dispatch-handler-context.md`
19. `docs/hardening-oi-004-forward-reducer-response-context.md`
20. `docs/hardening-oi-004-closeall-disconnect-context.md`
21. `docs/hardening-oi-004-supervise-disconnect-context.md`
22. `docs/hardening-oi-004-sender-disconnect-context.md`
23. `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
24. `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`
25. `docs/hardening-oi-005-snapshot-iter-useafterclose.md`
26. `docs/hardening-oi-005-snapshot-iter-retention.md`
27. `docs/parity-p0-recovery-001-replay-horizon.md`
28. `docs/parity-p0-sched-001-startup-firing.md`
29. `docs/parity-phase2-slice3-lag-policy.md`
30. the specific code surfaces for whichever anchor (α/β/γ/δ) you pick

## Shell discipline

Use `rtk` for shell commands. Examples:
- `rtk git status --short --branch`
- `rtk go test ./store -run 'TestName' -v`
- `rtk go test ./...`

## Important repo note

Keep `.hermes/plans/2026-04-18_073534-phase1-wire-level-parity.md` unless you deliberately update the contract that depends on it. A test expects it.

## What is already landed (do not reopen)

- Protocol conformance P0-PROTOCOL-001..004
- Delivery parity P0-DELIVERY-001..002
- Recovery invariant P0-RECOVERY-002
- TD-142 Slices 1–14 (all narrow SQL parity shapes, including join projection emitted onto the SELECT side)
- Phase 1.5 outcome model + caller metadata wiring
- Phase 2 Slice 3 lag / slow-client policy (2026-04-20) — `P0-SUBSCRIPTION-001`
- Phase 3 Slice 1 scheduled reducer startup / firing ordering (2026-04-20) — `P0-SCHED-001`
- Phase 4 Slice 2 replay-horizon / validated-prefix behavior (2026-04-20) — `P0-RECOVERY-001`
- OI-005 snapshot iterator GC retention sub-hazard (2026-04-20)
- OI-005 snapshot iterator use-after-Close sub-hazard (2026-04-20)
- OI-006 fanout per-subscriber slice-header aliasing sub-hazard (2026-04-20)
- OI-005 snapshot iterator mid-iter-close defense-in-depth sub-hazard (2026-04-20)
- OI-005 subscription-seam read-view lifetime sub-hazard (2026-04-20)
- OI-005 `CommittedSnapshot.IndexSeek` BTree-alias escape route (2026-04-20)
- OI-004 `watchReducerResponse` goroutine-leak escape route (2026-04-20)
- OI-005 `StateView.SeekIndex` BTree-alias escape route (2026-04-20)
- OI-005 `StateView.SeekIndexRange` BTree-alias escape route (2026-04-20)
- P1-07 executor response-channel contract + protocol-forwarding cancel-safe + Submit-time validation (2026-04-20, landed in commit `40b2152 baseline`)
- OI-004 `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx sub-hazard (2026-04-21)
- OI-004 `superviseLifecycle` disconnect-ctx sub-hazard (2026-04-21)
- OI-004 `ConnManager.CloseAll` disconnect-ctx sub-hazard (2026-04-21)
- OI-004 `forwardReducerResponse` ctx / Done lifecycle sub-hazard (2026-04-21)
- OI-004 dispatch-handler ctx sub-hazard (2026-04-21)
- OI-005 `StateView.ScanTable` iterator surface (2026-04-21)
- OI-005 `CommittedState.Table(id) *Table` raw-pointer contract pin (2026-04-21)
- OI-006 row-payload sharing contract pin (2026-04-21)
- broader SQL/query-surface parity follow-through (2026-04-21): quoted special-character identifiers, hex byte literals, float literals, `:sender` caller-identity parameter on narrow single-table KindBytes columns

## Suggested verification commands

Targeted:
- `rtk go test ./query/sql -run 'TestParseWhereSenderParameter|TestParseWhereRejectsUnknownParameter|TestCoerceSender' -count=1 -v`
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_SenderParameter|TestHandleOneOffQuery_SenderParameter' -count=1 -v`
- `rtk go test ./subscription -run 'TestEvalFanoutRowPayloadsSharedAcrossSubscribers' -race -count=3 -v`
- `rtk go test ./subscription -run 'TestEvalFanout' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedStateTable' -race -count=3 -v`
- `rtk go test ./store -run 'TestStateViewScanTableIteratesIndependentOfMidIterCommittedDelete' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestDispatchLoop_HandlerCtx' -race -count=3 -v`
- `rtk go test ./executor -run 'TestProtocolInboxAdapter_ForwardReducerResponse' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestCloseAll' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestSuperviseLifecycle' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestEnqueueOnConnOverflowDisconnect' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestWatchReducerResponse' -race -count=3 -v`
- `rtk go test ./...`

## Acceptance gate

Do not call the work done unless all are true:

- reference-backed or debt-anchored target shape was checked directly against reference material or current live code
- every newly accepted or rejected shape has focused tests
- already-landed parity pins still pass (including the `:sender` parser/coerce/protocol pins listed in `docs/parity-phase0-ledger.md`)
- full suite still passes. Clean-tree baseline is `Go test: 1193 passed in 10 packages`. No known clean-tree intermittent test remains after the 2026-04-21 follow-through.
- docs and handoff reflect the new truth exactly

## Deliverables for the next session

Either:
- code + tests closing the next reference-backed parity slice or Tier-B hardening sub-hazard

Or:
- a grounded blocker report naming the exact representation/runtime issue preventing a narrow landing

And in either case:
- update `TECH-DEBT.md` if any OI changes state
- update `docs/current-status.md`
- update `docs/parity-phase0-ledger.md` if a parity scenario moves
- update `NEXT_SESSION_HANDOFF.md`

## Final status snapshot right now

As of this handoff:
- `TD-142` fully drained
- Phase 2 Slice 3 closed — per-client outbound queue aligned to reference `CLIENT_CHANNEL_CAPACITY`; close-frame mechanism retained as intentional divergence
- Phase 3 Slice 1 closed — `P0-SCHED-001` scheduled-reducer startup / firing ordering narrow-and-pinned
- Phase 4 Slice 2 closed — `P0-RECOVERY-001` replay-horizon / validated-prefix behavior narrow-and-pinned
- P1-07 executor response-channel contract + protocol-forwarding cancel-safe + Submit-time validation landed
- OI-005 enumerated sub-hazards drained (iter GC retention, iter use-after-Close, iter mid-iter-close, subscription-seam read-view lifetime, IndexSeek BTree-alias, SeekIndex BTree-alias, SeekIndexRange BTree-alias, ScanTable iterator surface, `CommittedState.Table` raw-pointer contract pin)
- OI-006 enumerated sub-hazards drained (slice-header aliasing, row-payload sharing contract pin)
- OI-004 six sub-hazards closed (watchReducerResponse, sender overflow-disconnect ctx, superviseLifecycle disconnect-ctx, CloseAll disconnect-ctx, forwardReducerResponse ctx/Done lifecycle, dispatch-handler ctx, outbound-writer supervision)
- Phase 2 Slice 2 applied-envelope host execution duration + `SubscriptionError` optional-field / `TableID` follow-through closed
- broader SQL/query-surface parity follow-through (2026-04-21): reference-style double-quoted identifiers, query-builder-style parenthesized WHERE predicates, alias-qualified mixed-qualified/unqualified OR, hex byte literals, float literals on the narrow single-table / join-backed SQL surface, `:sender` caller-identity parameter on narrow single-table KindBytes columns, bare boolean `WHERE TRUE` all work end-to-end and are pinned
- Other detached-goroutine surfaces in `conn.go` / `lifecycle.go` / `keepalive.go` and the `ClientSender.Send` no-ctx follow-on remain open under OI-004
- next realistic anchors: broader SQL/query-surface parity (α), further Tier-B hardening (β), format-level commitlog parity (γ), individual scheduler deferrals (δ)
- targeted flaky-test cleanup is closed; no known clean-tree intermittent test remains
- 10 packages, clean-tree full-suite baseline `Go test: 1193 passed in 10 packages`
