# Performance Benchmarks

This document tracks benchmark snapshots for performance work. Keep entries
newest-first and include enough environment detail to compare future runs.

## How To Run

Run Go benchmarks directly, not through RTK:

```bash
go test -run '^$' -bench . -benchmem ./protocol ./commitlog ./subscription
```

RTK may summarize benchmark commands and suppress the raw
`Benchmark... ns/op B/op allocs/op` rows. Normal tests, vet, staticcheck, git,
and shell commands still use RTK.

For decisions that depend on small differences, run multiple samples and compare
with `benchstat`:

```bash
go test -run '^$' -bench . -benchmem -count=10 ./protocol ./commitlog ./subscription > /tmp/shunter-bench-new.txt
```

Record the exact command, CPU, OS/arch, and any relevant code changes. Treat
single-run results as directional, not as release gates.

## 2026-04-30 Performance Cleanup Baseline

Command:

```bash
go test -run '^$' -bench . -benchmem ./protocol ./commitlog ./subscription
```

Environment:

- `goos`: `linux`
- `goarch`: `amd64`
- CPU: `Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz`
- Scope: protocol compression, commitlog snapshot, subscription evaluator
- Context: after the wide performance cleanup that reduced BSATN/RowList
  buffering, subscription pruning allocations, BTreeIndex write shifts, and
  snapshot lock-held encoding work.

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `BenchmarkWrapCompressedGzip-12` | 37655 | 270 | 3 |
| `BenchmarkUnwrapCompressedGzip-12` | 8634 | 4725 | 7 |
| `BenchmarkCreateSnapshotLarge-12` | 64121239 | 1872629 | 9487 |
| `BenchmarkEvalEqualitySubs1K-12` | 10002 | 869 | 12 |
| `BenchmarkEvalEqualitySubs10K-12` | 5165 | 864 | 12 |
| `BenchmarkRegisterUnregister-12` | 12620 | 3824 | 34 |
| `BenchmarkRegisterSetInitialQueryAllRows-12` | 347748 | 72776 | 62 |
| `BenchmarkProjectedRowsBeforeLargeBags-12` | 5603712 | 892110 | 12320 |
| `BenchmarkFanOut1KClientsSameQuery-12` | 1155945 | 296511 | 2031 |
| `BenchmarkJoinFragmentEval-12` | 580740 | 49188 | 210 |
| `BenchmarkDeltaIndexConstruction-12` | 176698 | 4169 | 501 |
| `BenchmarkCandidateCollection-12` | 6224 | 480 | 3 |

Notes:

- Equality subscription evaluation and candidate collection are the current
  healthiest hot paths.
- Large bag diff and delta index construction remain the clearest allocation
  targets.
- Snapshot creation is still latency-heavy, but allocation pressure and lock-held
  encoding work were reduced by the cleanup.
