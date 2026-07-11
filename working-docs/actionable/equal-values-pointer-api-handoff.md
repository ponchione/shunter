# Pointer Equality API Handoff

Date: 2026-07-10

Status: implementation retained, fully validated, and intentionally uncommitted

## Where Work Stopped

The pointer-oriented equality slice is complete. Shunter now has the additive
API below adjacent to the existing `Value.Equal` implementation in
`types/value.go`:

```go
// EqualValues returns true if left and right have the same kind and value.
// No cross-kind coercion. It panics if either operand is nil.
func EqualValues(left, right *Value) bool
```

`Value.Equal` was left source-identical. `EqualValues` is a direct pointer
translation of its decision tree. The duplication is deliberate: the measured
Go compiler shape could not share the switch through helpers without changing
and slowing the compatibility-method path.

`protocol.oneOffJoinPairMatches` now calls:

```go
types.EqualValues(&leftRow[leftIndex], &rightRow[rightIndex])
```

Both explicit bounds checks and all filter, routing, sorting, projection, and
query behavior remain unchanged.

No implementation work remains in this slice unless review finds a problem.
The immediate continuation is review and, only with explicit authorization,
staging and committing the task-owned files.

## Why the Duplicate Was Retained

`types.Value` is 176 bytes. Calling the by-value method from the benchmark
requires two complete operand copies. Calling it from the one-off matcher
produced four 176-byte copy sequences because values were first copied into
matcher locals and then copied again into the call area.

The rejected shared-helper experiment compiled into small wrappers around
non-inlineable large helpers. It altered the established `Value.Equal` machine
code and regressed that compatibility path. In the retained design:

- `Value.Equal` remains the original direct by-value implementation.
- `EqualValues` directly reads through two pointers.
- neither entry routes through the other or through a shared dispatcher.
- exhaustive differential tests guard the duplicated semantics.

This is a measured compiler tradeoff, not a general preference for duplicated
logic. Revisit it only if a future Go compiler can share the implementation
without changing the compatibility-path code or performance.

## Task-Owned Changes

- `types/value.go`
  - adds adjacent `EqualValues`; the existing `Value.Equal` body is unchanged
- `types/value_test.go`
  - adds differential coverage for every `ValueKind`
  - covers equal, unequal, null/null, null/value, and value/null for every kind
  - covers cross-kind payloads, signed zero, canonical JSON, slice-backed
    values, nil pointer panics, and invalid internal kinds
- `types/value_bench_test.go`
  - was already untracked at startup with `BenchmarkValueEqual`
  - now also contains `BenchmarkEqualValues` using the unchanged shared case
    table
- `protocol/handle_oneoff.go`
  - already contained the task-owned direct-index optimization at startup
  - changes only the equality expression to use `types.EqualValues`
- `CHANGELOG.md`
  - adds the authorized unreleased API/performance entry

No protocol benchmark or benchmark input was changed.

## Compiler Evidence

- `Value.Equal`: inline cost 238, non-inlineable, 96-byte frame
- `EqualValues`: inline cost 238, non-inlineable, 96-byte frame
- `BenchmarkValueEqual.func1`: 384-byte frame, two 176-byte copies, one direct
  `Value.Equal` call
- `BenchmarkEqualValues.func1`: 56-byte frame, no full-value copies, one direct
  `EqualValues` call
- matcher stack reservation: 704 bytes before, 80 bytes after
- matcher copies: four 176-byte sequences before, none after
- `EqualValues` is called rather than inlined in the matcher
- both pointer operands and both matcher rows do not escape
- neither equality entry allocates on valid benchmark paths
- `bytes.Equal` and `slices.Equal` inline into the equality functions; runtime
  `memequal` calls remain where the compiler normally uses them

Normalized baseline and candidate `Value.Equal` disassembly was identical.
Both normalized streams hashed to:

```text
e3117292239839077df8d4b1cb6fca93d823cc67c5ecefd0480be6346328143e
```

## Microbenchmark Evidence

All observations in every series were 0 B/op and 0 allocs/op. Negative changes
mean faster. Paired candidate changes compare with the corresponding fresh
baseline series.

| Case | Baseline 1 | Paired 1 `Value.Equal` | Paired 1 `EqualValues` | Baseline 2 | Paired 2 `Value.Equal` | Paired 2 `EqualValues` |
|---|---:|---:|---:|---:|---:|---:|
| uint64 equal | 3.5895 | 3.5980 (+0.24%) | 1.2850 (-64.20%) | 3.5970 | 3.6080 (+0.31%) | 1.2790 (-64.44%) |
| uint64 unequal | 3.5945 | 3.5975 (+0.08%) | 1.2860 (-64.22%) | 3.5935 | 3.6240 (+0.85%) | 1.2860 (-64.21%) |
| string equal | 3.8870 | 3.8640 (-0.59%) | 2.0100 (-48.29%) | 3.8960 | 3.8875 (-0.22%) | 2.0065 (-48.50%) |
| string unequal | 3.6555 | 3.5915 (-1.75%) | 1.2960 (-64.55%) | 3.6455 | 3.6015 (-1.21%) | 1.2975 (-64.41%) |
| uint256 equal | 4.2410 | 4.0440 (-4.65%) | 1.3590 (-67.96%) | 4.0640 | 4.0470 (-0.42%) | 1.3630 (-66.46%) |
| uint256 unequal | 4.2415 | 4.0525 (-4.46%) | 1.3600 (-67.94%) | 4.0560 | 4.0465 (-0.23%) | 1.3605 (-66.46%) |
| null equal | 3.4395 | 3.4580 (+0.54%) | 1.0790 (-68.63%) | 3.4495 | 3.4530 (+0.10%) | 1.0860 (-68.52%) |
| bytes equal | 4.1755 | 4.1840 (+0.20%) | 2.5200 (-39.65%) | 4.1790 | 4.1885 (+0.23%) | 2.5640 (-38.65%) |
| bytes unequal | 3.6145 | 3.5705 (-1.22%) | 1.3090 (-63.78%) | 3.6295 | 3.5755 (-1.49%) | 1.3010 (-64.15%) |

The standalone pointer candidate medians were 1.2775, 1.2785, 2.0060,
1.2965, 1.3605, 1.3570, 1.0965, 2.5630, and 1.2970 ns/op in table order.

The final paired verification medians were:

| Case | `Value.Equal` | `EqualValues` |
|---|---:|---:|
| uint64 equal | 3.6050 | 1.2590 |
| uint64 unequal | 3.5995 | 1.2710 |
| string equal | 3.8685 | 2.0065 |
| string unequal | 3.5985 | 1.2985 |
| uint256 equal | 4.0485 | 1.3585 |
| uint256 unequal | 4.0620 | 1.3640 |
| null equal | 3.4580 | 1.0980 |
| bytes equal | 4.1935 | 2.5235 |
| bytes unequal | 3.5775 | 1.3155 |

The largest paired `Value.Equal` slowdown was 0.85%, below the 3% gate.
Both Uint64 pointer cases improved by roughly 64%, above the 30% gate.

## Protocol Evidence

| Run | Median ns/op | Median B/op | allocs/op | vs baseline 1 | vs baseline 2 |
|---|---:|---:|---:|---:|---:|
| Baseline 1 | 3,763,662 | 276,122 | 3,162 | - | - |
| Baseline 2 | 3,762,179 | 276,122 | 3,162 | - | - |
| After 1 | 2,398,636 | 276,122.5 | 3,162 | -36.27% | -36.24% |
| After 2 | 2,397,285.5 | 276,122 | 3,162 | -36.30% | -36.28% |
| Final verification | 2,433,992 | 276,123 | 3,162 | -35.33% | -35.30% |

CPU profile changes:

- matcher: 1.95 s flat / 2.29 s cumulative before; 0.40 s / 0.75 s after
- equality: `Value.Equal` 0.33 s flat/cumulative before; `EqualValues` 0.30 s
  flat/cumulative after
- equality source line: 1.65 s flat / 1.97 s cumulative before; 0.08 s /
  0.38 s after

Fixed-duration allocation-profile totals are not directly comparable because
the faster candidate completed more operations. Per-operation B/op and
allocs/op remained unchanged, and neither matcher nor equality appeared as an
allocation source.

## Validation Completed

Pre-change validation passed:

```text
rtk go test ./types -count=1                         161 tests passed
rtk go vet ./types                                   passed
rtk go tool staticcheck ./types                      passed
rtk go test ./protocol -run '<focused tests>' -count=1
                                                       5 tests passed
rtk go test ./protocol -count=1                      1,246 tests passed
rtk go vet ./protocol                                passed
rtk go tool staticcheck ./protocol                   passed
```

Candidate and final validation passed:

```text
rtk go test ./types -count=1                         307 tests passed
rtk go vet ./types                                   passed
rtk go tool staticcheck ./types                      passed
rtk go test ./protocol -run '<focused tests>' -count=1
                                                       5 tests passed
rtk go test ./protocol -count=1                      1,246 tests passed
rtk go vet ./protocol                                passed
rtk go tool staticcheck ./protocol                   passed
rtk git diff --check                                 passed
```

The final seven validation commands were run after the retained protocol and
changelog changes. Formatting passed when run separately for `types` and
`protocol`. A first combined named-file invocation was rejected because
`go fmt` requires named files to share a directory; it made no edits, and the
two correct directory-specific commands passed.

Deliberately omitted:

- `rtk go test ./...`: prohibited unless an unexpected cross-package
  dependency was discovered; none was
- qualification and canary gates: explicitly prohibited
- broad specs, historical evidence, and the ignored reference tree: not read

## Temporary Evidence

Compiler binaries and profiles were kept under `/tmp`:

```text
/tmp/shunter-equal-duplicate-baseline.test
/tmp/shunter-equal-duplicate-candidate.test
/tmp/shunter-equal-duplicate-protocol-baseline.test
/tmp/shunter-equal-duplicate-protocol-baseline.cpu
/tmp/shunter-equal-duplicate-protocol-baseline.mem
/tmp/shunter-equal-duplicate-protocol-after.test
/tmp/shunter-equal-duplicate-protocol-after.cpu
/tmp/shunter-equal-duplicate-protocol-after.mem
```

These are ephemeral. Regenerate them from the task commands if `/tmp` has been
cleared. Raw benchmark output was reported interactively and was not added to
the repository.

## Exact Repository State

Shunter:

```text
HEAD: 0f845c1c5f269218b7191c6203c8b88b6030797d
VERSION: v1.1.1-dev
branch: main, ahead of origin/main by 4
staged changes: none
```

Final status before this handoff file was created:

```text
 M AGENTS.md
 M CHANGELOG.md
 M protocol/handle_oneoff.go
 M types/value.go
 M types/value_test.go
 M working-docs/README.md
 M working-docs/actionable/README.md
 M working-docs/hosted-backend-roadmap.md
 M working-docs/recommendations/01-current-release-qualification.md
 M working-docs/recommendations/README.md
 M working-docs/release-qualification.md
?? types/value_bench_test.go
?? working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/
```

This handoff document is now one additional untracked file.

Pre-existing user-owned modifications that must be preserved:

- `AGENTS.md`
- `working-docs/README.md`
- `working-docs/actionable/README.md`
- `working-docs/hosted-backend-roadmap.md`
- `working-docs/recommendations/01-current-release-qualification.md`
- `working-docs/recommendations/README.md`
- `working-docs/release-qualification.md`

The untracked release-evidence directory is historical user-owned evidence and
must remain untouched.

opsboard-canary remained clean and read-only at:

```text
fedcbb6de9eabb539e561e814e9687f27ddb4fe6
```

No commit, staging, push, tag, publication, release, qualification, canary, or
pull-request action occurred.

## How to Continue Safely

1. Read `AGENTS.md`, `RTK.md`, and `VERSION` completely.
2. Confirm Shunter is still at `0f845c1c5f269218b7191c6203c8b88b6030797d`
   and opsboard-canary is clean at
   `fedcbb6de9eabb539e561e814e9687f27ddb4fe6`.
3. Confirm nothing is staged and distinguish the task-owned changes from the
   pre-existing user-owned documentation and evidence.
4. Review only the five task-owned files listed above plus this handoff.
5. If no code changes are made, do not repeat the full benchmark campaign just
   to reproduce already-recorded evidence.
6. If review changes Go behavior, rerun the seven targeted validation commands
   and the affected micro/protocol benchmarks before accepting it.
7. Do not stage or commit until the user explicitly authorizes that action.

This document records a completed slice; it does not select the next product
feature or authorize further implementation.
