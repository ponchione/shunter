# TECH-DEBT

This file tracks open issues only.
Resolved audit history belongs in git history and the narrow slice docs, not here.

Status conventions:
- open: confirmed issue or parity gap still requiring work
- deferred: intentionally not being closed now

Priority order:
1. externally visible parity gaps
2. correctness / concurrency bugs that undermine parity claims
3. capability gaps that block realistic usage
4. cleanup after parity direction is locked

## Open issues

### OI-001: Protocol surface is still not wire-close enough to SpacetimeDB

Status: open
Severity: high

Summary:
- legacy `v1.bsatn.shunter` admission is still accepted as a compatibility deferral
- brotli remains recognized-but-unsupported
- several message-family and envelope details remain intentionally divergent

Why this matters:
- protocol behavior is still one of the biggest blockers to serious parity claims
- even where semantics are close, the wire contract is still visibly Shunter-specific

Primary code surfaces:
- `protocol/upgrade.go`
- `protocol/compression.go`
- `protocol/tags.go`
- `protocol/wire_types.go`
- `protocol/client_messages.go`
- `protocol/server_messages.go`
- `protocol/send_responses.go`
- `protocol/send_txupdate.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A1
- `docs/parity-phase0-ledger.md`

### OI-002: Query and subscription behavior still diverges from the target runtime model

Status: open
Severity: high

Summary:
- many narrow SQL/query parity slices are now landed and pinned
- the surface is still intentionally narrower than the reference SQL path
- row-level security / per-client filtering remains absent
- broader query/subscription parity is still open beyond the landed narrow shapes

Why this matters:
- the system can look architecturally right while still behaving differently under realistic subscription workloads
- query-surface limitations still cap how close clients can get to reference behavior

Primary code surfaces:
- `query/sql/parser.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_oneoff.go`
- `subscription/predicate.go`
- `subscription/validate.go`
- `subscription/eval.go`
- `subscription/manager.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `executor/executor.go`
- `executor/scheduler.go`

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A2
- `docs/parity-phase0-ledger.md`

### OI-003: Recovery and store semantics still differ in user-visible ways

Status: open
Severity: high

Summary:
- value-model and changeset semantics remain simpler than the reference
- commitlog/recovery behavior is intentionally rewritten rather than format-compatible
- replay tolerance, sequencing, and snapshot/recovery behavior still need follow-through
- Phase 4 Slices 2α and 2β are closed; 2γ is the next open / deferred sub-slice

Why this matters:
- storage and recovery semantics are central to the operational-replacement claim
- sequencing and replay mismatches are the kind of differences users feel after crash/restart

Primary code surfaces:
- `types/`
- `bsatn/encode.go`
- `bsatn/decode.go`
- `store/commit.go`
- `store/recovery.go`
- `store/snapshot.go`
- `store/transaction.go`
- `commitlog/changeset_codec.go`
- `commitlog/segment.go`
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/snapshot_io.go`
- `commitlog/compaction.go`
- `executor/executor.go`

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A3
- `docs/parity-phase0-ledger.md`
- `docs/parity-phase4-slice2-offset-index.md`
- `docs/parity-phase4-slice2-errors.md`
- `docs/parity-phase4-slice2-record-shape.md`

### OI-004: Protocol lifecycle still needs hardening around goroutine ownership and shutdown

Status: open
Severity: high

Summary:
- several concrete sub-hazards were closed and pinned in narrow slice docs
- the remaining issue is the broader lifecycle/shutdown theme, not those already-closed sub-slices
- other detached goroutine sites and ownership seams remain watch items if a concrete leak site surfaces
- `ClientSender.Send` is still synchronous without its own ctx, but no concrete consumer currently requires widening that surface

Why this matters:
- lifecycle races and shutdown bugs undermine confidence even when nominal tests pass
- this is still one of the main blockers to calling the runtime trustworthy for serious private use

Primary code surfaces:
- `protocol/upgrade.go`
- `protocol/conn.go`
- `protocol/disconnect.go`
- `protocol/keepalive.go`
- `protocol/lifecycle.go`
- `protocol/outbound.go`
- `protocol/sender.go`
- `protocol/async_responses.go`

Source docs:
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
- `docs/hardening-oi-004-sender-disconnect-context.md`
- `docs/hardening-oi-004-supervise-disconnect-context.md`
- `docs/hardening-oi-004-closeall-disconnect-context.md`
- `docs/hardening-oi-004-forward-reducer-response-context.md`
- `docs/hardening-oi-004-dispatch-handler-context.md`
- `docs/hardening-oi-004-outbound-writer-supervision.md`

### OI-005: Snapshot and committed-read-view lifetime rules still need stronger safety guarantees

Status: open
Severity: high

Summary:
- the enumerated narrow sub-hazards were closed and pinned
- the remaining issue is the broader lifetime/ownership theme around read handles and raw access surfaces
- current safety still relies partly on discipline and observational pins rather than machine-enforced lifetime

Why this matters:
- long-lived or misused read views can distort concurrency assumptions
- this weakens confidence in subscription evaluation and recovery-side read paths

Primary code surfaces:
- `store/snapshot.go`
- `store/committed_state.go`
- `store/state_view.go`
- `subscription/eval.go`
- `executor/executor.go`

Source docs:
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-005-snapshot-iter-retention.md`
- `docs/hardening-oi-005-snapshot-iter-useafterclose.md`
- `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`
- `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`
- `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`
- `docs/hardening-oi-005-state-view-seekindex-aliasing.md`
- `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`
- `docs/hardening-oi-005-state-view-scan-aliasing.md`
- `docs/hardening-oi-005-committed-state-table-raw-pointer.md`

### OI-006: Subscription fanout still carries aliasing and cross-subscriber mutation risk concerns

Status: open
Severity: medium

Summary:
- the known narrow slice-header and row-payload-sharing sub-hazards were closed and pinned
- the remaining issue is broader fanout/read-only-discipline risk if future code introduces in-place mutation or shared-state assumptions

Why this matters:
- cross-subscriber mutation or aliasing bugs are subtle and can silently corrupt delivery behavior
- this weakens confidence in both parity and correctness claims

Primary code surfaces:
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-006-fanout-aliasing.md`
- `docs/hardening-oi-006-row-payload-sharing.md`

### OI-007: Recovery sequencing and replay-edge behavior still needs targeted parity closure

Status: open
Severity: medium

Summary:
- replay-horizon / validated-prefix parity slice is closed
- Phase 4 Slice 2α offset-index work is closed
- Phase 4 Slice 2β typed error categorization is closed — five category sentinels (`ErrTraversal`, `ErrOpen`, `ErrDurability`, `ErrSnapshot`, `ErrIndex`) + `wrapCategory` helper + `Is` methods on the nine typed structs; call-site wraps landed at every emission seam; leaf identity / back-compat preserved
- Phase 4 Slice 2γ (record / log on-disk shape parity) has its decision doc locked at `docs/parity-phase4-slice2-record-shape.md`: Session 1 audit completed reference/Shunter field-by-field delta (26 entries across structural / behavioral / semantic buckets) and decided to close 2γ as documented-divergence rather than byte-parity rewrite; Session 2 lands the 33-pin wire-shape contract suite. Named deferrals (reference magic, commit grouping, epoch field, V0/V1 split, zero-header EOS sentinel, checksum-algorithm negotiation, forked-offset detection, full records-buffer parity) carried forward as tracked tech debt
- remaining scheduler deferrals stay open

Why this matters:
- these gaps mainly show up under restart, crash, and replay conditions
- they materially affect the operational-replacement claim

Primary code surfaces:
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`

Source docs:
- `docs/parity-p0-recovery-001-replay-horizon.md`
- `docs/parity-p0-sched-001-startup-firing.md`
- `docs/parity-phase0-ledger.md`
- `docs/parity-phase4-slice2-offset-index.md`
- `docs/parity-phase4-slice2-errors.md`

### OI-008: The repo still lacks a coherent top-level engine/bootstrap story

Status: open
Severity: medium

Summary:
- there is still no single polished bootstrap surface or example app path that matches the implementation depth underneath it
- `schema.Engine.Start(...)` is not the same thing as a cohesive runtime bootstrap

Why this matters:
- embedding/developer experience still looks weaker than the implementation underneath it
- this makes replacement/usage judgment harder even when internals are substantial

Primary code surfaces:
- `schema/version.go`
- `README.md`
- repo root package layout

Source docs:
- `README.md`
- `docs/current-status.md`

## Deferred issues

### DI-001: Energy accounting remains a permanent parity deferral

Status: deferred
Severity: low

Summary:
- `EnergyQuantaUsed` remains pinned at zero because Shunter does not implement an energy/quota subsystem

Why this matters:
- this is an intentional parity gap and should stay explicit

Source docs:
- `docs/parity-phase1.5-outcome-model.md`
- `docs/parity-phase0-ledger.md`