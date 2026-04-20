# TECH-DEBT

This file tracks open issues only.

Resolved and doc-drift-only audit entries were intentionally removed during the 2026-04-20 cleanup so this file can stay focused on live work. Use git history if you need the old resolved ledger.

Status conventions:
- open: confirmed issue or parity gap still requiring work
- deferred: intentionally not being closed now

Priority order:
1. externally visible parity gaps
2. correctness / concurrency bugs that undermine parity claims
3. capability gaps that block realistic usage
4. cleanup that should wait until parity decisions are locked

## Open issues

### OI-001: Protocol surface is still not wire-close enough to SpacetimeDB

Status: open
Severity: high

Summary:
- The reference subprotocol token is preferred, but the legacy `v1.bsatn.shunter` token is still accepted.
- Brotli remains a recognized-but-unsupported compression mode.
- Several message-family and envelope details remain intentionally divergent.

Why this matters:
- Client-visible protocol behavior is still one of the biggest blockers to serious parity claims.
- Even where semantics are close, the wire contract is still visibly Shunter-specific in important places.

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
- `docs/parity-phase0-ledger.md` protocol conformance bucket

### OI-002: Query and subscription behavior still diverges from the target runtime model

Status: open
Severity: high

Summary:
- The SQL/query surface is still deliberately narrow.
- The subscription engine still exposes Shunter-specific behavior around slow clients, caller/durability seams, and broader parity semantics.
- Row-level security / per-client filtering remains absent.
- Join projection semantics now emit projected-width rows end-to-end (TD-142 Slice 14, 2026-04-20): `subscription.Join` carries `ProjectRight bool`, the canonical hash distinguishes `SELECT lhs.*` from `SELECT rhs.*`, and `evalQuery` / `initialQuery` / `evaluateOneOffJoin` all slice the LHS++RHS IVM fragments onto the SELECT side. Remaining anchors: lag / slow-client policy (Phase 2 Slice 3) and RLS. See `docs/parity-phase0-ledger.md` TD-142 row.

Why this matters:
- The system can look architecturally right while still behaving differently under realistic subscription workloads.
- Query-surface limitations still cap how close clients can get to reference behavior.

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
- `docs/parity-phase0-ledger.md` scheduler / recovery parity scenarios

### OI-003: Recovery and store semantics still differ in user-visible ways

Status: open
Severity: high

Summary:
- Value-model and changeset semantics remain simpler than the reference.
- Commitlog/recovery behavior is intentionally rewritten rather than format-compatible.
- Replay tolerance, sequencing, and snapshot/recovery behavior still need parity decisions and follow-through.

Why this matters:
- Storage and recovery semantics are central to the operational-replacement claim.
- Sequencing and replay mismatches are the kind of differences users feel only after a crash or restart.

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
- `docs/parity-phase0-ledger.md` recovery parity scenarios

### OI-004: Protocol lifecycle still needs hardening around goroutine ownership and shutdown

Status: open
Severity: high

Summary:
- Connection lifecycle code still relies on detached background goroutines and shutdown paths that are harder to reason about than a single owned lifecycle context.
- This is the main correctness/hardening theme still called out by the current-status and parity docs.

Why this matters:
- Lifecycle races and unsafe close behavior undermine confidence in the protocol even when nominal tests pass.
- This is one of the main blockers to calling the runtime trustworthy for serious private use.

Primary code surfaces:
- `protocol/upgrade.go`
- `protocol/conn.go`
- `protocol/disconnect.go`
- `protocol/keepalive.go`
- `protocol/lifecycle.go`
- `protocol/outbound.go`
- `protocol/sender.go`

Source docs:
- `docs/current-status.md` open hardening / correctness picture
- `docs/spacetimedb-parity-roadmap.md` Tier B

### OI-005: Snapshot and committed-read-view lifetime rules still need stronger safety guarantees

Status: open
Severity: high

Summary:
- Snapshot/read-view lifetime discipline is still treated as a sharp edge in the surrounding docs.
- This is an architectural correctness concern, not cosmetic cleanup.

Why this matters:
- Long-lived or misused read views can distort concurrency assumptions and make correctness depend on caller discipline.
- It also weakens confidence in subscription evaluation and recovery-side read paths.

Primary code surfaces:
- `store/snapshot.go`
- `store/committed_state.go`
- `store/state_view.go`
- `subscription/eval.go`
- `executor/executor.go`

Source docs:
- `docs/current-status.md` open hardening / correctness picture
- `docs/spacetimedb-parity-roadmap.md` Tier B

### OI-006: Subscription fanout still carries aliasing and cross-subscriber mutation risk concerns

Status: open
Severity: medium

Summary:
- Fanout and update assembly remain a live hardening concern around shared slices/maps and per-subscriber isolation.
- The parity docs treat this as one of the main non-cosmetic remaining risks.

Why this matters:
- Cross-subscriber mutation or aliasing bugs are subtle and can silently corrupt delivery behavior.
- This weakens confidence in both parity and basic correctness claims.

Primary code surfaces:
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/current-status.md` open hardening / correctness picture
- `docs/spacetimedb-parity-roadmap.md` Tier B

### OI-007: Recovery sequencing and replay-edge behavior still needs targeted parity closure

Status: open
Severity: medium

Summary:
- The scheduler startup-ordering scenario remains in progress.
- Replay-horizon / validated-prefix tolerance remains in progress.
- The already-closed snapshot+replay invariant work did not eliminate the broader sequencing/replay parity backlog.

Why this matters:
- These are the kinds of gaps that only show up under restart, crash, and replay conditions.
- They materially affect the “operational replacement” claim.

Primary code surfaces:
- `executor/scheduler.go`
- `executor/scheduler_worker.go`
- `executor/scheduler_replay_test.go`
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`

Source docs:
- `docs/parity-phase0-ledger.md` rows `P0-SCHED-001` and `P0-RECOVERY-001`

### OI-008: The repo still lacks a coherent top-level engine/bootstrap story

Status: open
Severity: medium

Summary:
- There is still no `main` package, `cmd/` entrypoint, example app, or single polished bootstrap surface.
- `schema.Engine.Start(...)` is a startup schema-compatibility check, not the unified runtime bootstrap implied by the original architecture sketches.

Why this matters:
- The subsystem work is real, but the developer-facing embedding story is still weaker than the implementation depth underneath it.
- This makes it harder to judge the project as a usable replacement even if many internals are already substantial.

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
- `EnergyQuantaUsed` remains pinned at zero because Shunter does not implement an energy/quota subsystem.

Why this matters:
- This is an intentional parity gap, but it should remain explicit so it does not get mistaken for accidental completeness.

Source docs:
- `docs/parity-phase1.5-outcome-model.md`
- `docs/parity-phase0-ledger.md`
