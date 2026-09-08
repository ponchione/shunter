# Completion audit

Each item names evidence inspected against the current state. No missing item is
being replaced with a narrower passing claim.

| Requirement | Evidence and result |
|---|---|
| Read startup instructions in both trees and parents/nested paths | Root Shunter AGENTS.md/RTK.md read; no additional parent/nested/Canary instruction files found. TECH_DEBT.md and four named prior investigation documents read. No reference-tree source used. |
| Preserve actual working-tree baseline, relevant untracked files, prior evidence | `source-manifest.json`, source tarballs, baseline patches/status; `preservation-check.json`: 3113 unchanged files, four authorized existing-file edits with originals archived, all 76 Canary inputs unchanged. HEAD/index unchanged. |
| Isolated experimental sources and exact archived fixtures | Four detached worktrees; `frozen-inputs.json` covers 1562 files; all 12 fixture files and archive hash verified; reconstruction independently verified before module relocation. |
| Maintained entry point and explicit separate load mode | `scripts/measure-canary-capacity`, `docs/benchmarks.md`, `maintained-harness.patch`; smoke 1 exercised the supported script. Closed-loop names and original repetition order retained. Final shell syntax passed. |
| Intended arrivals and complete offered-window rates | Every raw result records all 3200 due offsets before phase start; independent `analyze.py` checks exact formulas/choices. Both 0.8s and 3.2s complete windows retained. |
| Actual dispatch/completion, after-window work, read p95/p99 milliseconds | `metrics.json`, `metrics.md`, per-run raw results and benchmark output; independently recomputed percentile/rate boundaries; unit/nearest-rank tests passed. |
| Scheduling lateness, undispatched queue, active waits, total backlog | Exact arrival/dispatch/terminal event sweep in maintained reporter; targeted backlog/deadline test includes a never-dispatched operation and a timeout without reply. Raw source preserves one request/client and common origin. |
| Error/timeout/rejection/deadline accounting and unknown writes | Separate raw flags/counts; synthetic deadline test verifies unknown write is not rejected. All measured errors/timeouts/rejections/unknowns/undispatched counts zero. Failure path preserves data and reconciles audit outcomes. |
| Response, row decoding and exhaustive correctness cost separate | Maintained receipt/handoff/decode-start/decode-end/validation stamps; diagnostic frame/outer-decode/routing stamps; full equality separate from decoding; no renamed historical metric boundary. |
| Same choices/order/history/mix/fanout/snapshots across comparisons | Common choice hash in all 12 runs, 1637 writes and 1563 reads, fixed fixture bytes, raw per-client order/dispatch checks, exactly two successful snapshots and eight subscriptions/project. |
| Complete state, accepted outcomes and ordered drain outside latency where possible | All raw FINAL_CHECK records pass; 19644 accepted writes reconciled with complete recovered values/audit IDs/allocator bounds. `analysis-check.json` independently checks all 157152 deliveries and identical complete project streams. |
| Memory boundaries and retained instrumentation accounted | Separate process TotalAlloc, post-GC retained heap, sampled peaks; fixed server counters, 512000-byte client operation and 393216-byte delivery arrays; diagnostic 4-MiB static buffer/process reported separately from heap/B/op. |
| End-to-end path traced before interpreting probes | README path description and isolated patches cover raw Send, handler/CallQuery, row/envelope encoding, enqueue/dequeue/write, client frame/decode/route/pending queue and benchmark handoff/decode/validation. No reducer adapter optimization. |
| Request plus connection correlation, bounded diagnostic-only capture | `diagnostic-shunter.patch`, `diagnostic-canary.patch`, raw probes: all 6252 reads have 16 stages exactly once, no drops/parser/missing/duplicate failures; only identities/stages/timestamps logged. Overflow and concurrency tests passed under race. |
| Comparable clocks, monotonic durations, honest socket residual | Per-process monotonic origins never subtracted across processes; before/after wall-clock brackets and all event skew retained. 44.760us combined observed mapping spread and 65 signed write/receipt overlaps documented. |
| Frozen same-request cohort and representative timelines | Contract froze >=p99 primary and >=p95 secondary rules; `correlation.json` distributions and `timelines.md` include first/median/max of each 16-read primary cohort. No percentile subtraction or aggregate profile attribution. |
| Attribution or explicit remaining boundary and smallest next experiment | README establishes measured socket-write-start/frame-receipt and individual client/query/queue intervals, leaves transport/buffering/scheduling causality unresolved, proposes only a separately authorized read-readiness/return boundary experiment. |
| Premeasurement hypotheses, exact matrix/hashes/order/repetitions/deadlines/stops | `contract.md` hash frozen in `frozen-matrix.json` before first measured command; `frozen-inputs.json`, fixture/source manifests, per-run command and host files. |
| Eight ordinary/four diagnostic cap; balanced/interleaved serial runs | All 12 directories have successful terminal exits, order ordinary A/diagnostic/ordinary B for four cells. No workload overlaps with task builds/tests/analysis. No measured retries or replacement samples. |
| Smoke/race/validation accounting | Two smoke invocations (one actual 320-op workload, one skipped); separate 320-op race workload and targeted validation explicitly excluded and retained. No added anchor/overload/profiling cells. |
| Finite requests, drain, process, resource bounds | 30s requests, 90s completion/drain, 170s Go test, 180s outer process, sampled 1GiB server RSS stop. All checks pass; no dependent cell continued after a correctness failure (none occurred). |
| Per-run values/ranges and statistical/product limits | README ordinary ranges, 12-row metrics table, raw adverse samples/maxima, overhead comparison. Two repetitions/~16 p99-tail reads explicitly exploratory; no product budget invented. |
| Required formatting/vet/Staticcheck/race checks | Every `check-*/exit.json` is zero; final maintained tests/fmt/vet and shell syntax cover delivered files. Canary uses Shunter-pinned Staticcheck. Reconstruction test also passes. |
| Scope and stop decisions | Original runtime/application files and TECH_DEBT.md remain as supplied. No adoption, extra profiling, PERF-03/07 resolution, policy/default/version/release change, staging, commit, push or PR. Twelve-invocation measurement budget exhausted. |
| Reviewable artifacts and self-contained handoff | README, contract/checklist, source/fixture/final manifests, reproduction/check/analysis scripts, maintained/measured/final-review/instrumentation patches, raw results/errors, component/timeline/overhead reports. |

Final review restored the closed-loop shell repetition order, corrected a reporter
comment, and fixed missing-memory/early-resource-cancellation failure reporting.
Final semantic reporter replay against all 12 archived results passes. `final-review-vs-measured.patch` makes this distinction explicit;
measured Go behavior/source copies and frozen hashes remain intact.
