# Hosted Runtime V3 Observability Planning

Status: planned
Scope: implementation-facing decomposition for SPEC-007 logging,
observability, health, diagnostics, metrics, Prometheus, and tracing.

Primary authority:
- `docs/specs/007-observability/SPEC-007-observability.md`

V3 turns the SPEC-007 contract into worker-sized implementation tasks. It starts
from the live V2.5 hosted runtime and must keep observability strictly additive:
logging, metrics, tracing, diagnostics, and sink failures must not change
runtime correctness, transaction ordering, protocol compatibility, durability,
subscription behavior, or reducer semantics.

## Phase Order

1. V3-A: observability foundation
   - tasks 01-04
   - adds the public config/API, redaction, sink isolation, recovery facts, and
     expanded health snapshots
2. V3-B: runtime signal wiring
   - tasks 05-07
   - replaces process-global production logs and instruments lifecycle,
     recovery, protocol, executor, reducer, durability, and subscription
     metrics
3. V3-C: operator surfaces
   - tasks 08-10
   - adds the Prometheus adapter, HTTP diagnostics, and optional tracing hooks
4. Cross-cutting closeout
   - task 11
   - proves redaction, cardinality, no-op defaults, sink isolation, endpoint
     contracts, and final validation gates

Do not skip phase A. Later instrumentation depends on the normalized
runtime-scoped observability object, redaction helpers, and recovery/health
facts.

## Task Files

1. `00-current-execution-plan.md`
2. `01-stack-prerequisites.md`
3. `02-observability-foundation.md`
4. `03-build-recovery-observations.md`
5. `04-health-and-descriptions.md`
6. `05-structured-logging.md`
7. `06-metrics-core-lifecycle-recovery.md`
8. `07-subsystem-metrics.md`
9. `08-prometheus-adapter.md`
10. `09-http-diagnostics.md`
11. `10-tracing-hooks.md`
12. `11-gauntlet-and-validation.md`

## Boundary Rules

V3 must:
- keep zero-value `ObservabilityConfig` a no-op for logs, metrics, traces, and
  HTTP diagnostics
- observe build-time failures when configured sinks are available, even when no
  `Runtime` is returned
- recover logger, metrics, tracer, and diagnostics sink panics at the
  observability boundary
- avoid calling configured sinks while holding locks that protect mutable
  runtime subsystem state, except for in-memory no-op paths
- keep metrics low-cardinality by construction with the fixed `MetricLabels`
  struct
- redact and bound errors before logs, diagnostics, metrics reasons, or trace
  error surfaces can expose them
- keep health snapshots cheap, detached, synchronous, and bounded by hosted
  module count
- keep Prometheus outside the root package under `observability/prometheus`

V3 must not:
- change reducer, scheduler, durability, subscription, protocol, schema, or
  transaction semantics to make observation easier
- import Prometheus packages from the root `shunter` package
- introduce process-global production logging
- add free-form metric label maps
- expose raw SQL, reducer args, row payloads, tokens, authorization headers,
  signing keys, or raw unbounded errors in logs, metrics, traces, or health
  payloads
- make diagnostics endpoints scan user tables, wait on durability, wait for
  goroutines, or perform network I/O
- copy source or structure from `reference/SpacetimeDB`

## Completion Definition

V3 is complete when:
- the public SPEC-007 API exists with the required zero-value behavior
- build, recovery, lifecycle, health, logs, metrics, HTTP diagnostics,
  Prometheus, and tracing satisfy the SPEC-007 contracts
- production `log.Printf` usage is removed or isolated behind runtime-scoped
  observability where a runtime exists, with package-level no-op fallback only
  for pre-runtime paths
- every SPEC-007 section 14 verification row is mapped to a passing test or to
  a documented non-applicable reason approved in the task notes
- final validation gates in task 11 pass

## Validation Posture

Each worker should:
- inspect live Go symbols with `rtk go doc` before editing unfamiliar code
- add failing tests before implementation
- keep changes scoped to the assigned task
- use `rtk go fmt` on touched packages
- run targeted package tests first
- run `rtk go vet` for touched packages when exported APIs, interfaces, or
  behavior changed
- expand to `rtk go test ./... -count=1` and pinned Staticcheck when a task
  changes root runtime behavior, shared subsystem contracts, or final V3 state
