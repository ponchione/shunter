# Go Performance and Idiomatic Cleanup Punchlist

Date: 2026-04-20
Source: wide, shallow live-code audit of the current Shunter Go codebase
Baseline checked:
- `rtk go test ./...` -> pass (`1102 passed in 10 packages`)
- `rtk go vet ./...` -> clean

Purpose:
- Capture the most obvious, relatively bounded cleanup items surfaced by a broad audit
- Prioritize fixes that reduce avoidable allocation, remove obviously non-idiomatic hot-path structure, and improve maintainability without broad architecture churn
- This is not a parity roadmap replacement and not a deep profiling report

Priority legend:
- P0 = best immediate payoff / low ambiguity
- P1 = worthwhile next pass
- P2 = good cleanup but lower urgency

Difficulty legend:
- S = small
- M = medium
- L = large

Payoff legend:
- High = likely noticeable allocation/latency/maintainability improvement
- Medium = worthwhile but probably localized
- Low = mostly code health / API ergonomics

---

## P0-01: Stop materializing full table scans in subscription initial query paths

Priority: P0
Difficulty: S-M
Payoff: High

Primary files:
- `subscription/register_set.go`

Primary functions:
- `initialQuery`
- `iterateAll`

Problem:
- `iterateAll` eagerly converts `view.TableScan(...)` into a slice.
- `initialQuery` then sometimes builds another temporary slice on top of that before probing joins.
- This creates avoidable allocation and GC churn on subscribe / initial-state paths.

Concrete evidence:
- `subscription/register_set.go:86-93`
- `subscription/register_set.go:118-124`
- `subscription/register_set.go:147-164`
- `subscription/register_set.go:187-194`

Fix direction:
1. Delete the `iterateAll` helper from the hot path.
2. Range directly over `view.TableScan(tableID)` where possible.
3. In join initial-query branches, avoid building `ls` / `rs` temp slices unless there is a proven semantic need.
4. Keep row-limit enforcement exactly as-is.

Expected result:
- Lower transient allocation during register/subscribe.
- Less GC pressure on larger initial snapshots.
- Simpler, more obviously streaming code.

Verification:
- Existing register/subscribe tests still pass.
- Add/adjust a benchmark around initial query / register path if missing.

---

## P0-02: Replace quadratic `projectedRowsBefore` matching with keyed multiset logic

Priority: P0
Difficulty: M
Payoff: High

Primary files:
- `subscription/eval.go`
- possibly reuse helpers in `subscription/delta_dedup.go`

Primary functions:
- `projectedRowsBefore`
- `productValuesEqual`

Problem:
- `projectedRowsBefore` does nested matching between current rows and inserted rows.
- Current shape is O(n*m) with a `used[]` bitmap.
- This is avoidable because the codebase already has canonical row-key / multiset-style dedup logic elsewhere.

Concrete evidence:
- `subscription/eval.go:419-447`
- related pattern already exists in `subscription/delta_dedup.go`

Fix direction:
1. Rework `projectedRowsBefore` to use a canonical row key plus multiplicity counts.
2. Reuse existing canonical row encoding approach if practical instead of introducing a second row-key scheme.
3. Preserve bag semantics exactly.
4. Remove or reduce `productValuesEqual` use in this path if it becomes redundant.

Expected result:
- Better scaling for projected delta reconstruction.
- More uniform logic across delta-reconciliation code.

Verification:
- Existing delta / join / cross-join tests pass.
- Add a targeted benchmark comparing old/new behavior on larger row sets.

---

## P0-03: Stream snapshot write path instead of buffering the entire snapshot in memory

Priority: P0
Difficulty: L
Payoff: High

Primary files:
- `commitlog/snapshot_io.go`

Primary functions:
- `CreateSnapshot`
- `buildSnapshotContent`

Problem:
- Snapshot creation builds the full snapshot body in memory, then wraps it in another output buffer.
- Each row also gets encoded through a fresh row buffer before being copied again into the body.
- This scales poorly with snapshot size and inflates peak memory.

Concrete evidence:
- `commitlog/snapshot_io.go:274-363`

Fix direction:
1. Replace full-buffer assembly with a streaming writer pipeline.
2. Write header/body to file incrementally.
3. Compute hash while streaming instead of after assembling the full body slice.
4. Reuse row encoding buffers where practical.
5. Keep snapshot format unchanged unless there is an explicit format-change decision.

Expected result:
- Lower peak RSS during snapshot creation.
- Less copying and buffer churn.
- Better path to larger snapshots.

Verification:
- Snapshot read/write tests stay green.
- Add a size-oriented benchmark or at least a large-snapshot regression test.

---

## P0-04: Stream snapshot read path instead of `os.ReadFile` whole-file loading

Priority: P0
Difficulty: M-L
Payoff: High

Primary files:
- `commitlog/snapshot_io.go`

Primary functions:
- `ReadSnapshot`
- `findTableSchema`

Problem:
- `ReadSnapshot` reads the full snapshot file into memory with `os.ReadFile`.
- It then allocates additional slices for schema bytes and each row payload.
- Decode cost rises with snapshot size and duplicates data unnecessarily.

Concrete evidence:
- `commitlog/snapshot_io.go:380-488`
- `commitlog/snapshot_io.go:545-551`

Fix direction:
1. Decode from an `io.Reader` / file handle instead of `os.ReadFile`.
2. Parse the fixed header directly from the stream.
3. Use bounded scratch buffers for row payload decoding where practical.
4. Build a one-time schema lookup map instead of repeated linear `findTableSchema` scans.

Expected result:
- Lower peak memory during recovery/snapshot load.
- Simpler scaling behavior under large persisted state.

Verification:
- Existing snapshot/recovery tests pass.
- Add a regression test that exercises larger synthetic snapshots.

---

## P1-05: Reduce per-message allocation in gzip compression/decompression paths

Priority: P1
Difficulty: M
Payoff: Medium-High

Primary files:
- `protocol/compression.go`
- `protocol/dispatch.go`

Primary functions:
- `WrapCompressed`
- `UnwrapCompressed`
- `runDispatchLoop`

Problem:
- Outbound gzip allocates a new buffer and gzip writer per message.
- Inbound gzip does `io.ReadAll(gr)`.
- Dispatch then reconstructs a new `[tag][body]` frame slice after decompression.

Concrete evidence:
- `protocol/compression.go:63-119`
- `protocol/dispatch.go:90-109`

Fix direction:
1. Consider pooling gzip writers/readers if safe and worthwhile.
2. Avoid the extra reframing copy by teaching the decode path to work from `(tag, body)` directly.
3. Optionally add a small-frame threshold to skip gzip when compression is negotiated but payloads are tiny.
4. Preserve current wire behavior.

Expected result:
- Lower CPU and allocation overhead on compressed traffic.

Verification:
- Compression tests remain green.
- Add/expand protocol compression benchmarks.

---

## P1-06: Split oversized orchestration functions into narrower helpers

Priority: P1
Difficulty: M
Payoff: Medium

Primary files / functions:
- `protocol/upgrade.go` -> `HandleSubscribe`
- `executor/executor.go` -> `handleCallReducer`, `postCommit`
- `subscription/register_set.go` -> `initialQuery`
- `commitlog/snapshot_io.go` -> `ReadSnapshot`

Problem:
- Several functions mix validation, orchestration, error translation, resource handling, and business logic.
- This is a maintainability smell and makes targeted perf work harder.

Hotspot evidence:
- `protocol.HandleSubscribe` ~130 LOC
- `executor.handleCallReducer` ~125 LOC
- `subscription.initialQuery` ~113 LOC
- `commitlog.ReadSnapshot` ~110 LOC

Fix direction:
1. Extract phase-specific helpers without changing behavior.
2. Keep helpers private and semantic, not generic utility noise.
3. Prefer extraction along natural boundaries:
   - auth / params / upgrade / lifecycle-start in protocol
   - reducer lookup / execute / finalize / commit handoff in executor
   - join-initial-query left/right probe helpers in subscription
   - header / schema / row sections in snapshot decoding

Expected result:
- Easier future perf work.
- Lower change amplification.
- Better local readability.

Verification:
- No behavior change; existing tests should be sufficient.

---

## P1-07: Audit and minimize response-channel send blocking assumptions in executor paths

Priority: P1
Difficulty: M
Payoff: Medium

Primary files:
- `executor/executor.go`
- `executor/protocol_inbox_adapter.go`
- scheduler/executor tests around response channels

Primary functions:
- `sendReducerResponse`
- `sendProtocolCallReducerResponse`
- `sendCallReducerResponse`

Problem:
- Executor response sends are unconditional blocking sends when channels are non-nil.
- Current callers appear to use buffered channels in tests and adapter code, so this is not an identified bug today.
- But for a library/runtime, this is a sharp edge: a caller can stall executor progress by handing in an unbuffered or unread channel.

Concrete evidence:
- `executor/executor.go:310-333`
- `executor/protocol_inbox_adapter.go` currently uses buffered size-1 channels

Fix direction:
1. Decide whether blocking response channels are part of the contract or accidental.
2. If accidental, harden by documenting required buffering or by changing the response delivery mechanism.
3. At minimum, add explicit tests that pin the intended contract.

Expected result:
- Better executor robustness and clearer API expectations.

Verification:
- Add contract tests for buffered/unbuffered response behavior.

---

## P1-08: Tighten protocol lifecycle goroutine ownership and context usage

Priority: P1
Difficulty: M-L
Payoff: Medium

Primary files:
- `protocol/upgrade.go`
- `protocol/dispatch.go`
- `protocol/keepalive.go`
- `protocol/lifecycle.go`
- `protocol/outbound.go`

Problem:
- `HandleSubscribe` spawns several detached goroutines with `context.Background()`.
- Some of this is already called out by repo debt docs as a hardening concern.
- This is not just style: it can complicate shutdown reasoning and prolong resources after request-context cancellation.

Concrete evidence:
- `protocol/upgrade.go:197-211`
- `protocol/dispatch.go:169-172` per-message goroutine fanout

Fix direction:
1. Introduce a connection-owned lifecycle context rather than raw `context.Background()` spawns.
2. Ensure all per-connection goroutines are children of that lifecycle context.
3. Preserve current teardown semantics and tests.

Expected result:
- Cleaner shutdown ownership.
- Easier reasoning about lifecycle and leak risk.

Verification:
- Existing lifecycle/disconnect tests pass.
- Add leak-sensitive lifecycle tests if practical.

---

## P2-09: Revisit public `types.Value` panic-heavy accessor surface

Priority: P2
Difficulty: M
Payoff: Low-Medium

Primary files:
- `types/value.go`

Primary functions:
- `AsBool`, `AsInt*`, `AsUint*`, `AsFloat*`, `AsString`, `AsBytes`
- `Compare`
- `mustKind`

Problem:
- Public accessors panic on wrong-kind use.
- `Compare` panics on cross-kind comparison.
- This is tolerable for internal invariant enforcement, but less idiomatic for a library-facing value type.

Concrete evidence:
- `types/value.go:199-209`
- `types/value.go:242-269`

Fix direction:
1. Decide whether `types.Value` is intended as internal-only or user-facing library surface.
2. If user-facing, consider adding safe alternatives (`TryAsX`, checked compare helpers).
3. Keep panic-only helpers for internal fast paths if needed.

Expected result:
- More idiomatic API surface.
- Fewer crashy caller footguns.

Verification:
- Add tests for any new safe API.
- Avoid forcing broad call-site churn unless the repo wants that direction.

---

## P2-10: Replace repeated linear schema lookup in snapshot decode with a map

Priority: P2
Difficulty: S
Payoff: Low-Medium

Primary files:
- `commitlog/snapshot_io.go`

Primary functions:
- `ReadSnapshot`
- `findTableSchema`

Problem:
- Snapshot decode linearly scans schema tables for each table block.
- Easy to fix; not huge today, but unnecessary.

Concrete evidence:
- `commitlog/snapshot_io.go:466-480`
- `commitlog/snapshot_io.go:545-551`

Fix direction:
1. Build `map[schema.TableID]*schema.TableSchema` once after decoding schema.
2. Use it for the row sections.
3. Remove `findTableSchema` if no longer needed.

Expected result:
- Small decode simplification and small perf improvement.

Verification:
- Existing snapshot tests should cover behavior.

---

## Suggested execution order

Best immediate sequence:
1. P0-01 materialized scans in `subscription/register_set.go`
2. P0-02 quadratic `projectedRowsBefore`
3. P2-10 schema lookup map during snapshot decode
4. P0-04 streamed snapshot read path
5. P0-03 streamed snapshot write path
6. P1-05 compression path allocations
7. P1-06 function refactors where they help the chosen fixes
8. P1-07 executor response-channel contract hardening
9. P1-08 protocol lifecycle context ownership
10. P2-09 public `types.Value` API ergonomics

---

## Notes / guardrails

- Do not turn this into broad speculative refactoring.
- Prefer narrow patches with measurable allocation/complexity wins.
- For the snapshot and compression work, benchmark or at least add large-regression tests before and after changes.
- Keep parity-sensitive protocol behavior unchanged unless a separate parity decision explicitly says otherwise.
- Any executor / lifecycle hardening should preserve the currently tested semantics before chasing style cleanups.

---

# Slice plan for follow-through work

This section is intended as the handoff plan for a fresh agent. It is deliberately implementation-facing: exact files, target behaviors, likely pitfalls, and validation commands.

Planning assumptions:
- Work should stay narrow and behavior-preserving unless a slice explicitly says otherwise.
- The repo currently passes `rtk go test ./...` and `rtk go vet ./...`; do not regress that baseline.
- For these cleanup slices, prefer smallest-safe change first, then benchmark/regression reinforcement second.
- Existing decomposition/spec docs remain the correctness anchor even when this plan is performance-motivated.

Recommended execution order for the next agent:
1. Slice A — subscription initial-query de-materialization
2. Slice B — `projectedRowsBefore` multiset rewrite
3. Slice C — snapshot decode schema-map micro-cleanup
4. Slice D — streamed snapshot reader
5. Slice E — streamed snapshot writer
6. Slice F — compression-path allocation reduction

Why this order:
- A/B are the narrowest, easiest-to-verify wins in hot subscription code.
- C is a tiny cleanup that simplifies D.
- D should land before E because the current write format must keep reading cleanly while internals change.
- F is useful, but less urgent than the snapshot and subscription paths.

## Cross-slice repo research notes

Subscription registration / initial query contract:
- Spec: `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.2-register.md`
- Important requirement: registration executes inside one executor command with no gap between initial-row materialization and activation.
- Important requirement: do not hold committed-read views across network I/O or unrelated blocking work.
- Existing tests: `subscription/register_set_test.go`
- Existing benchmarks: `subscription/bench_test.go` (`BenchmarkRegisterUnregister` and others)

Join-delta / bag-semantics contract:
- Spec: `docs/decomposition/004-subscriptions/epic-3-deltaview-delta-computation/story-3.3-join-delta-fragments.md`
- Important requirement: full bag semantics; cancellation fragments are required for correctness.
- Existing tests: `subscription/eval_test.go`, `subscription/delta_*_test.go`
- Existing helper to reuse conceptually: `subscription/delta_dedup.go`

Snapshot reader/writer contract:
- Epic: `docs/decomposition/002-commitlog/epic-5-snapshot-io/EPIC.md`
- Stories:
  - writer: `docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.2-snapshot-writer.md`
  - reader: `docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.3-snapshot-reader.md`
  - integrity: `docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.4-snapshot-integrity.md`
- Existing tests: `commitlog/snapshot_test.go`, `commitlog/snapshot_select_test.go`, `commitlog/recovery_test.go`
- Current implementation consolidates what decomposition expected as separate writer/reader files into `commitlog/snapshot_io.go`; keep scope narrow unless there is a compelling reason to split files.

## Slice A — subscription initial-query de-materialization

Goal:
- Remove avoidable slice materialization from the registration bootstrap path without changing registration semantics, row-limit behavior, or join projection behavior.

Primary files:
- `subscription/register_set.go`
- optionally `subscription/register_set_test.go`
- optionally `subscription/bench_test.go`

Current code facts to preserve:
- `initialQuery` returns `[]types.ProductValue` because `RegisterSet` needs a materialized initial result to place into `SubscriptionUpdate`.
- The avoidable issue is not the final result slice; it is the extra whole-table temp slices used during scan/probe.
- `iterateAll` currently turns `CommittedReadView.TableScan` into a slice for convenience.
- Join initial-query paths currently create temporary `ls` / `rs` slices before probing the opposite side.

Target changes:
1. Delete `iterateAll` from hot-path use.
2. Rewrite single-table initial query branch to range directly over `view.TableScan(t)`.
3. Rewrite join bootstrap branch to iterate the driving table directly instead of first collecting all rows in `ls` / `rs`.
4. Keep `add(row)` as the single place enforcing `InitialRowLimit`.
5. Do not change output row ordering guarantees beyond what current tests/spec already permit.

Likely implementation shape:
- Replace patterns like:
  - build temp slice from `iterateAll(view, p.Left)`
  - range temp slice
- With direct iterator loops:
  - `for _, lrow := range view.TableScan(p.Left) { ... }`
- Remove `iterateAll` if it becomes unused.

Tests to run first:
- `rtk go test ./subscription -run 'RegisterSet|UnregisterSet|DisconnectClient'`

Tests to add or tighten if missing:
- register path with `InitialRowLimit` still fails identically when exceeded mid-set
- join initial query still projects correct side for aliased/self-join forms if coverage is thin

Benchmarks to run after change:
- `rtk go test ./subscription -bench RegisterUnregister -benchmem`

Slice completion criteria:
- No semantic change in registration results
- No helper remains that fully materializes `TableScan` only for convenience in this path
- Benchmem is at least no worse, ideally lower allocs/op

Main risk:
- Accidentally changing row-limit timing or join result order in edge cases

## Slice B — `projectedRowsBefore` multiset rewrite

Goal:
- Replace the current quadratic row-removal logic with canonical-key multiset accounting while preserving bag semantics exactly.

Primary files:
- `subscription/eval.go`
- possibly `subscription/delta_dedup.go`
- optionally `subscription/eval_test.go`
- optionally `subscription/bench_test.go`

Current code facts to preserve:
- `projectedRowsBefore` is used by `evalCrossJoinProjectedDelta`.
- The current algorithm:
  - snapshots current rows
  - scans inserted rows with nested equality matching
  - appends deleted rows afterward
- This is correctness-sensitive because multiplicity matters.

Target changes:
1. Introduce canonical row-key counting for inserted rows.
2. Walk current rows once, decrementing counts when a matching inserted row cancels one occurrence.
3. Keep unmatched current rows.
4. Append deleted rows unchanged.
5. Remove `productValuesEqual` if it becomes unused; otherwise keep it only where still necessary.

Preferred reuse strategy:
- Reuse the same canonical-row-key concept already used in `subscription/delta_dedup.go` (`encodeRowKey`) rather than inventing a second equivalence mechanism.
- If reuse would create awkward coupling, factor the keying helper into a small shared internal helper in the `subscription` package.

Tests to run first:
- `rtk go test ./subscription -run 'Eval|CrossJoin|Join|Delta'`

Tests to add or tighten if missing:
- a focused unit test for duplicate-row multiplicity in `projectedRowsBefore`
- a case where inserted rows cancel only some duplicates, not all
- a case where deleted rows are preserved after cancellation accounting

Benchmarks to run after change:
- Existing subscription benchmarks, plus add a small benchmark specifically for the projected-before reconstruction path if practical.

Slice completion criteria:
- Existing join/cross-join delta tests pass unchanged
- New multiplicity-focused regression tests pass
- Nested O(n*m) matching removed from this path

Main risk:
- Getting bag semantics subtly wrong for duplicates

## Slice C — snapshot decode schema-map micro-cleanup

Goal:
- Land the smallest low-risk cleanup that prepares the snapshot reader for the larger streaming rewrite.

Primary files:
- `commitlog/snapshot_io.go`
- optionally `commitlog/snapshot_test.go`

Current code facts to preserve:
- `ReadSnapshot` decodes schema first, then row sections.
- `findTableSchema` linearly scans tables during row decode.

Target changes:
1. Build a `map[schema.TableID]*schema.TableSchema` immediately after schema decode.
2. Replace per-table `findTableSchema` scans with map lookup.
3. Remove `findTableSchema` if it becomes dead.

Tests to run:
- `rtk go test ./commitlog -run 'Snapshot|SelectSnapshot'`

Why do this as its own slice:
- It is tiny, safe, and makes the subsequent reader rewrite more straightforward.
- It gives the follow-on agent a clean intermediate checkpoint.

Slice completion criteria:
- Behavior identical
- Tiny diff
- No broad refactor

## Slice D — streamed snapshot reader

Goal:
- Replace full-file snapshot loading with a streaming decode path while preserving the on-disk format and all current integrity checks.

Primary files:
- `commitlog/snapshot_io.go`
- `commitlog/snapshot_test.go`
- `commitlog/snapshot_select_test.go`
- possibly `commitlog/recovery_test.go`

Current code facts to preserve:
- Public API is `ReadSnapshot(dir string) (*SnapshotData, error)`.
- Hash verification currently compares stored Blake3 hash with `ComputeSnapshotHash(data[52:])` over the payload bytes after the fixed header.
- Snapshot selection intentionally falls back from corrupt snapshots to older valid ones.
- Snapshot format includes:
  - magic
  - version
  - reserved bytes
  - txID
  - schemaVersion
  - payload hash
  - payload starting with `schemaLen`

Important design constraint:
- Do not silently change file format in this slice.
- The objective is internal decoding strategy only.

Recommended implementation approach:
1. Open the snapshot file as `*os.File`.
2. Read and validate the fixed-size header directly from the file.
3. Stream the remaining payload through an `io.TeeReader` or equivalent to compute Blake3 incrementally while decoding.
4. Decode schema section, sequence section, nextID section, and row sections directly from the stream.
5. Compare computed payload hash with stored header hash only after payload decode completes.
6. Return the same public errors as today for bad magic/version/hash/truncation.

Open implementation detail to settle before coding:
- Whether to do a two-pass reader (verify hash by streaming payload first, then seek back and decode) or a one-pass decode while hashing with `io.TeeReader`.
- Preferred answer: one-pass decode while hashing, because it avoids re-reading and still keeps memory bounded.

Tests to run first:
- `rtk go test ./commitlog -run 'Snapshot|SelectSnapshot|Recover'`

Tests to add or tighten if missing:
- truncated snapshot payload during a row section still fails cleanly
- corrupt hash still surfaces `SnapshotHashMismatchError`
- valid snapshot still round-trips identically after streamed decode

Bench/regression suggestion:
- Add a larger synthetic snapshot test fixture rather than a strict benchmark if benchmark setup is too noisy.

Slice completion criteria:
- `ReadSnapshot` no longer uses `os.ReadFile`
- payload decode does not require whole-file buffering
- all snapshot/recovery tests remain green

Main risks:
- Hash verification ordering mistakes
- accidental format drift
- silent changes in error surface on truncated/corrupt files

## Slice E — streamed snapshot writer

Goal:
- Replace whole-snapshot assembly buffering with incremental file writing while preserving the existing file format and lockfile/integrity semantics.

Primary files:
- `commitlog/snapshot_io.go`
- `commitlog/snapshot_test.go`
- possibly `commitlog/recovery_test.go`

Current code facts to preserve:
- `CreateSnapshot` currently uses `buildSnapshotContent` then writes the whole content slice.
- The writer enforces single in-progress snapshot via mutex/flag and uses lockfile protocol.
- Existing tests already pin:
  - round-trip read/write
  - lockfile behavior
  - concurrent snapshot rejection
  - hash mismatch detection after corruption

Recommended implementation approach:
1. Keep `CreateSnapshot` public behavior the same.
2. Replace `buildSnapshotContent` with a staged writer pipeline that writes directly to the target file.
3. Reserve/write header with placeholder hash, stream payload while hashing, then patch header hash before final sync/close.
4. Reuse scratch buffers for row encoding instead of allocating a new `bytes.Buffer` per row.
5. Keep directory sync and lockfile cleanup semantics intact.

Open implementation detail to settle before coding:
- Easiest safe approach is likely:
  - write provisional header
  - stream payload and compute hash
  - seek back to fill hash field
  - sync file
- If patching header in place is awkward, a temp-file strategy is acceptable only if it preserves current lockfile/atomicity expectations.

Tests to run first:
- `rtk go test ./commitlog -run 'Snapshot|ConcurrentSnapshot|Recover'`

Tests to add or tighten if missing:
- a larger snapshot round-trip test with many rows
- optional regression that checks snapshot file remains readable by existing `ReadSnapshot`

Slice completion criteria:
- Snapshot writer no longer assembles the full final snapshot in memory before write
- File format unchanged
- Existing snapshot/recovery tests remain green

Main risks:
- header/hash patching bugs
- losing sync/flush durability semantics
- temp-file/rename behavior accidentally diverging from current selection logic

## Slice F — compression-path allocation reduction

Goal:
- Reduce obvious avoidable allocations in compressed message handling without changing protocol wire behavior.

Primary files:
- `protocol/compression.go`
- `protocol/dispatch.go`
- possibly decode helpers in `protocol/client_messages.go`
- relevant protocol tests / compression tests

Current code facts to preserve:
- Compression tags and error handling are parity-sensitive.
- `runDispatchLoop` currently reconstructs a new `[tag][body]` frame after decompression so `DecodeClientMessage` can keep its current signature.
- Brotli remains recognized-but-unsupported by current policy.

Recommended implementation order inside this slice:
1. Lowest-risk first: avoid reframing copy by letting decode work from `(tag, body)` or by adding an internal helper.
2. Then consider gzip writer/reader pooling if it can be done safely without correctness risk.
3. Treat small-frame thresholding as optional and defer it unless a benchmark shows value.

Tests to run first:
- `rtk go test ./protocol -run 'Compression|Dispatch|Backpressure|Lifecycle'`

Benchmarks to add if practical:
- focused compression bench for wrap/unwrap allocs

Slice completion criteria:
- No protocol behavior drift
- Reduced allocs in compressed path or at least removed obviously redundant copy step

Main risks:
- changing error mapping for malformed/compressed frames
- introducing state leakage with pooled gzip objects

## Validation checklist for every slice

For each slice, the executing agent should do all of the following before claiming completion:

1. Run targeted tests for touched package(s)
2. Run `rtk go vet` on touched package(s) if exported behavior or interfaces changed
3. Run `rtk go test ./...` once the slice is stable
4. Run relevant benchmark(s) with `-benchmem` when the slice claims allocation/perf improvement
5. Record in commit/notes:
   - what changed
   - what behavior was intentionally preserved
   - what benchmark/test evidence supports the slice

## Suggested handoff note for the next agent

If you are picking up this document fresh, start with Slice A and do not broaden scope. Read these files first:
- `subscription/register_set.go`
- `subscription/register_set_test.go`
- `subscription/bench_test.go`
- `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.2-register.md`

Then execute the smallest behavior-preserving diff that removes extra scan materialization. Do not start with snapshot streaming until A/B/C are complete and green.
