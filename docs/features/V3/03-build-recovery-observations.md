# V3 Task 03: Build And Recovery Observations

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Task 02 observability foundation

Objective: persist recovery facts on runtime construction and emit build and
recovery observations, including fresh bootstrap and build failures before a
runtime is returned.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` sections 2, 4.1,
  6.2, 8.1, 9, and 14
- `docs/features/V3/02-observability-foundation.md`

Inspect:

```sh
rtk go doc . Build
rtk go doc ./commitlog OpenAndRecoverWithReport
rtk go doc ./commitlog RecoveryReport
rtk go doc ./commitlog SkippedSnapshotReport
rtk go doc ./commitlog SegmentInfo
rtk grep -n 'openOrBootstrapState|OpenAndRecover|RecoveryReport|resumePlan|Build' build.go runtime.go commitlog
```

## Target Behavior

Make `Build` and committed-state bootstrap/recovery observable:

- create the normalized observability object early enough to report validation
  and build failures when usable sinks are configured
- use runtime label `"default"` if runtime label validation itself fails
- use module label `"unknown"` until module identity is established, then the
  validated module name
- record `runtime.build_failed` and
  `runtime_errors_total{reason="build_failed"}` once for observable build
  failures
- preserve the full recovery report facts needed by `Runtime.Health`, logs, and
  metrics
- treat fresh bootstrap with no durable files as a successful recovery run with
  recovered transaction ID `0`, no selected durable snapshot, no damaged tail
  segments, and no skipped snapshots
- record `recovery.completed` at Info for clean recovery/bootstrap and Warn
  when damaged tails or skipped snapshots exist
- record `recovery.failed` and `recovery_runs_total{result="failed"}` when
  recovery prevents `Build` from returning a runtime
- set `recovery_recovered_tx_id`, `recovery_damaged_tail_segments`, and
  `recovery_skipped_snapshots_total` from the final report when metrics are
  enabled

Do not widen commitlog recovery semantics. This task only preserves and reports
facts that recovery already computes or that fresh bootstrap can describe
deterministically.

## Tests To Add First

Add focused failing tests for:

- build validation failure logs `runtime.build_failed` and increments
  `runtime_errors_total{reason="build_failed"}` when sinks are configured
- invalid runtime label build failure uses runtime label `"default"` in the
  build-failure observation
- pre-module validation failure uses module label `"unknown"`
- post-module validation failure uses the validated module name
- fresh bootstrap records successful recovery with recovered tx `0`, no damage,
  and no skipped snapshots
- recovery success records `RecoveryHealth`, `recovery.completed`,
  `recovery_runs_total{result="success"}`, and recovered tx gauge
- recovery failure records `recovery.failed` and failed recovery run counter
- damaged tail or skipped snapshots mark recovery facts that later health can
  classify as degraded

Prefer existing recovery tests in `commitlog` for report facts and root runtime
tests for build-time observability.

## Validation

Run at least:

```sh
rtk go fmt . ./commitlog
rtk go test ./commitlog -run 'Test.*Recovery' -count=1
rtk go test . -run 'Test.*(Build|Recovery|Observability|Bootstrap|RuntimeLabel)' -count=1
rtk go vet . ./commitlog
```

Expand to `rtk go test ./... -count=1` if recovery or build signatures change
for downstream packages.

## Completion Notes

When complete, update this file with:

- where recovery facts are stored
- how fresh bootstrap is represented
- exact build-failure label behavior
- tests added or updated
- validation commands run

### Recorded Completion 2026-05-01

Recovery facts:

- `Runtime` now stores `runtimeRecoveryFacts` with `ran`, `succeeded`, and a
  detached copy of the final `commitlog.RecoveryReport`.
- `Runtime.recoveredTxID` and `Runtime.resumePlan` still carry the executor and
  durability startup facts, while `Runtime.recovery.report` keeps the broader
  operator-facing report for later health, logging, and metrics slices.
- recovery facts include helper classification for later health degradation
  when recovery succeeded with damaged tail segments or skipped snapshots.

Fresh bootstrap:

- `openOrBootstrapState` now uses `commitlog.OpenAndRecoverWithReport`.
- when no durable files exist, Shunter still creates the initial snapshot needed
  for restart compatibility, but the retained and emitted recovery report is
  the deterministic fresh-bootstrap shape: recovered tx `0`, no selected
  durable snapshot, no durable log, no damaged tail segments, and no skipped
  snapshots.

Build-failure labels and observations:

- build-time observability is created before module validation.
- pre-module validation failures use module label `"unknown"`.
- once a non-empty module name is validated, subsequent build failures use that
  module name.
- invalid runtime label failures still emit through configured logger/metrics
  with runtime label `"default"`.
- observable build failures emit `runtime.build_failed` and increment
  `runtime_errors_total{component="runtime", reason="build_failed"}` once.

Recovery observations:

- successful recovery/bootstrap emits `recovery.completed`; it is Info for
  clean recovery and Warn when damaged tail segments or skipped snapshots are
  present.
- recovery failures that prevent `Build` from returning emit `recovery.failed`
  and increment `recovery_runs_total{component="commitlog", result="failed"}`.
- successful recovery/bootstrap increments
  `recovery_runs_total{component="commitlog", result="success"}` and sets
  `recovery_recovered_tx_id` plus `recovery_damaged_tail_segments`.
- each skipped snapshot increments `recovery_skipped_snapshots_total` using the
  fixed SPEC-007 reason mapping; current commitlog reasons map
  `past_durable_horizon` to `newer_than_log` and `read_failed` to
  `read_failed`.

Tests added:

- build validation failure observations for pre-module, invalid runtime-label,
  and post-module failure labels.
- fresh bootstrap recovery facts, log event, and metrics.
- existing-snapshot recovery facts and metrics.
- recovery failure observations and build-failure coupling.
- skipped-snapshot degraded facts, Warn-level `recovery.completed`, and skipped
  snapshot metric reason mapping.

Validation:

```sh
rtk go fmt . ./commitlog
rtk go test . -run 'TestBuild.*(Observability|Recovery|Bootstrap)' -count=1
rtk go test ./commitlog -run 'Test.*Recovery' -count=1
rtk go test . -run 'Test.*(Build|Recovery|Observability|Bootstrap|RuntimeLabel)' -count=1
rtk go vet . ./commitlog
rtk go test ./... -count=1
```
