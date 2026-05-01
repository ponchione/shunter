# V3 Task 11: Gauntlet And Validation

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Tasks 01-10 complete

Objective: close V3 by proving the full SPEC-007 verification matrix across
no-op defaults, sink isolation, redaction, health, logging, metrics,
Prometheus, diagnostics, tracing, and runtime correctness.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` section 14
- all completed `docs/features/V3/0*.md` task files

Inspect:

```sh
rtk go test . -list 'Test.*(Observability|Health|Diagnostics|Metrics|Logging|Tracing|Recovery|Redaction|Prometheus|Runtime|Host)'
rtk go test ./commitlog -list 'Test.*(Recovery|Durability|Observability|Metrics)'
rtk go test ./executor -list 'Test.*(Executor|Reducer|Metrics|Logging|Tracing)'
rtk go test ./protocol -list 'Test.*(Protocol|Connection|Metrics|Logging|Tracing|Diagnostics)'
rtk go test ./subscription -list 'Test.*(Subscription|Fanout|Metrics|Logging|Tracing)'
rtk go test ./observability/prometheus -list 'Test.*'
```

## Target Behavior

Before marking V3 complete:

- build a coverage matrix that maps every SPEC-007 section 14 verification row
  to at least one test name
- keep the matrix row-granular: broad category names are not enough when
  section 14 lists separate edge cases
- add missing focused tests before adding or expanding broad gauntlet tests
- add a root runtime gauntlet only for cross-subsystem behavior that cannot be
  pinned well by focused package tests
- verify zero-value observability remains no-op across build/start/call/close
- verify sink panics never alter reducer results, protocol results,
  subscription behavior, or runtime lifecycle return values
- verify redaction covers logs, diagnostics, trace errors/attributes, and
  metric reason mapping
- verify metric labels never include raw SQL, reducer args, row payloads,
  request IDs, query IDs, connection IDs, raw error strings, identities, tokens,
  or signing keys
- verify diagnostics endpoints do not mount unless configured and exact-path
  behavior is pinned
- verify root package does not import Prometheus packages
- verify no production process-global logging remains except documented
  pre-runtime no-op fallback paths

## Tests To Add First

Add any missing coverage from the section 14 matrix. Prefer:

- focused unit tests for redaction, normalization, metrics recorder behavior,
  Prometheus family registration, and HTTP method/path/status rules
- root runtime tests for build/start/close, health, describe, and no-op
  behavior
- subsystem tests for protocol, executor, reducer, durability, subscription,
  fan-out, and tracing insertion points
- one cross-subsystem gauntlet for configured logger, recorder, tracer, and
  diagnostics handler operating together under normal runtime traffic

Do not duplicate every lower-level package edge case in the gauntlet if focused
tests already pin it. Record the mapping instead.

## Validation

Run the final completion gates:

```sh
rtk go fmt ./...
rtk go test ./commitlog ./executor ./protocol ./subscription ./store ./observability/prometheus -count=1
rtk go test . -run 'Test.*(Observability|Health|Diagnostics|Metrics|Logging|Tracing|Recovery|Redaction|Prometheus|Runtime|Host)' -count=1
rtk go vet ./commitlog ./executor ./protocol ./subscription ./store ./observability/prometheus .
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

If a validation command is not run, record the reason and the remaining risk in
this file before closing V3.

## Completion Notes

### Recorded Completion 2026-05-01

Coverage discovery:

- The required `go test -list` commands were run before adding coverage. They
  were repeated with `rtk proxy go test ... -list ...` because the native
  `rtk go test` summary elides list-only output.
- Existing task 01-10 coverage already pinned the matrix across no-op defaults,
  build/recovery, health, lifecycle logging, metrics, Prometheus, diagnostics,
  and tracing.
- The only focused gap found was direct assertion of runtime-scoped protocol
  and subscription log events/redaction for the section 14 rows that name those
  events.

Focused coverage added:

- `TestRuntimeStructuredLoggingProtocolAndSubscriptionEventsRedact` pins
  `protocol.connection_rejected`, `protocol.auth_failed`,
  `subscription.eval_error`, `protocol.backpressure`, and
  `subscription.client_dropped` event names, base component/runtime fields, and
  redacted bounded error fields.

No broad root gauntlet was added. The remaining section 14 rows map cleanly to
focused package-boundary or root unit tests, and adding another cross-subsystem
workload would duplicate already saturated runtime gauntlet coverage.

### SPEC-007 Section 14 Coverage Matrix

| # | Verification row | Test coverage |
|---|---|---|
| 1 | Zero `ObservabilityConfig` build/start/call/close succeeds with no panics | `TestZeroObservabilityConfigBuildStartCallCloseNoop` |
| 2 | Build validation failure emits `runtime.build_failed` and `runtime_errors_total{reason="build_failed"}` when sinks are configured | `TestBuildFailureObservabilityLabels` |
| 3 | Fresh bootstrap records successful recovery with recovered tx `0` and no damage/skips | `TestBuildFreshBootstrapRecordsRecoveryObservability`; `TestRuntimeHealthFreshBootstrapRecovery` |
| 4 | Invalid `RuntimeLabel` or `ReducerLabelMode` is rejected before runtime construction | `TestObservabilityRuntimeLabelNormalizationAndValidation`; `TestObservabilityReducerLabelModeNormalizationAndValidation` |
| 5 | Non-nil logger receives `runtime.ready` with required base fields and message equal to event | `TestRuntimeStructuredLoggingReadyAndClosed` |
| 6 | Startup failure logs `runtime.start_failed` and increments `runtime_errors_total{reason="start_failed"}` | `TestRuntimeStructuredLoggingStartFailedRedactsAndBoundsError`; `TestRuntimeMetricsFailureCountersUseMappedReasonsExactlyOnce` |
| 7 | Close failure logs `runtime.close_failed` and increments `runtime_errors_total{reason="close_failed"}` | `TestRuntimeStructuredLoggingCloseFailed`; `TestRuntimeMetricsFailureCountersUseMappedReasonsExactlyOnce` |
| 8 | Recovery success records `RecoveryHealth`, `recovery.completed`, `recovery_runs_total{result="success"}`, and recovered tx gauge | `TestBuildExistingRecoveryRecordsReportAndMetrics`; `TestRuntimeHealthFreshBootstrapRecovery`; `TestRecoveryMetricsCoreFamiliesAndLabels` |
| 9 | Recovery with damaged tail or skipped snapshots sets `Degraded=true` and logs `recovery.completed` at Warn | `TestBuildRecoverySkippedSnapshotMarksDegradedFactsAndWarns`; `TestRuntimeHealthRecoverySkippedSnapshotDegrades` |
| 10 | Multiple degraded conditions choose the section 9.3 primary reason deterministically | `TestRuntimeHealthDegradedReasonPriority`; `TestRuntimeStructuredLoggingHealthDegradedUsesPrimaryReason` |
| 11 | Health before start and after close reports configured capacities, zero absent depths, retained counters, and latched fatal facts | `TestRuntimeHealthExpandedBeforeStartAndAfterClose` |
| 12 | Runtime health reports executor fatal and durability fatal state without blocking | `TestRuntimeHealthExecutorAndDurabilityFatalAppearWithoutBlocking` |
| 13 | Runtime health reports protocol disabled as not ready but not degraded | `TestRuntimeHealthProtocolDisabledReadyDoesNotDegrade` |
| 14 | Nil or empty host health returns `Ready=false`, `Degraded=true`, and `Modules=[]` | `TestHostHealthExpandedNilEmptyAggregateAndDetached`; `TestHostDiagnosticsStatusSemantics` |
| 15 | Host health aggregates ready/degraded across modules and returns detached slices | `TestHostHealthExpandedNilEmptyAggregateAndDetached`; `TestHostHealthReportsPerModuleState`; `TestHostPreservesPerModuleContractsAndDetachedDescription` |
| 16 | Runtime state gauges are one-hot after build, start, failure, closing, and closed transitions | `TestRuntimeMetricsStateGaugesTrackLifecycleTransitions`; `TestRuntimeMetricsStateGaugesTrackFailureAndDegradedTransitions` |
| 17 | Metric recorder panic is recovered and does not recursively emit sink-failure observations | `TestObservabilitySinkFailureFallbacksAreBoundedAndNonRecursive`; `TestObservabilitySinkPanicsRecoveredBeforeRuntimeOperation` |
| 18 | Protocol connection open/close updates active connection gauge and accepted counter | `TestProtocolMetricsConnectionGaugeAndAcceptedCounter` |
| 19 | Protocol connection rejection maps exactly one rejection result and logs `protocol.connection_rejected` | `TestProtocolMetricsConnectionRejectionMapsExactlyOneResult`; `TestRuntimeStructuredLoggingProtocolAndSubscriptionEventsRedact` |
| 20 | Protocol auth failure increments rejected connection counter with `rejected_auth` and logs redacted error | `TestProtocolMetricsAuthFailureRecordsRejectedAuth`; `TestRuntimeStructuredLoggingProtocolAndSubscriptionEventsRedact` |
| 21 | Protocol malformed message increments `protocol_messages_total{result="malformed"}` with kind `unknown` or decoded kind | `TestProtocolMetricsMalformedMessageRecordsDecodedKind` |
| 22 | Reducer committed/user error/panic/internal/permission outcomes increment distinct reducer result labels | `TestReducerMetricsResultMappingsAndDeclaredLabels` |
| 23 | Reducer-name metric labels default to the declared reducer name | `TestReducerMetricsResultMappingsAndDeclaredLabels`; `TestSubsystemMetricsUseExactFamiliesAndLabels` |
| 24 | `ReducerLabelModeAggregate` emits reducer label `_all` | `TestReducerMetricsAggregateLabelModeUsesAll` |
| 25 | Executor submit rejection increments `executor_commands_total{result="rejected"}` | `TestExecutorMetricsSubmitRejectionAndInboxDepth` |
| 26 | Executor command duration histogram uses exact default buckets | `TestExecutorMetricsCommandDurationRecordsOnlyDequeuedCommands`; `TestDurationHistogramsExposeExactSpecBucketBoundaries` |
| 27 | Subscription eval error logs `subscription.eval_error` and increments eval error metric | `TestEvalErrorQueuesSubscriptionErrorAndDropsConnection`; `TestSubscriptionMetricsEvalDurationRecordsErrorResult`; `TestRuntimeStructuredLoggingProtocolAndSubscriptionEventsRedact` |
| 28 | Fan-out buffer-full logs `protocol.backpressure` or `subscription.client_dropped` and increments drop metric with `buffer_full` | `TestProtocolMetricsBackpressureRecordsInboundAndOutboundDirections`; `TestSubscriptionMetricsDroppedSignalFullUsesBufferFullReason`; `TestRuntimeStructuredLoggingProtocolAndSubscriptionEventsRedact` |
| 29 | Prometheus adapter with nil registerer uses a private registry and does not pollute globals | `TestNilRegistererUsesPrivateRegistryAndDoesNotPolluteGlobal` |
| 30 | Prometheus adapter with custom registry registers all families with default namespace `shunter` | `TestCustomRegistryRegistersAllFamiliesWithDefaultNamespace` |
| 31 | Prometheus adapter rejects invalid namespace and invalid const-label names | `TestInvalidNamespaceIsRejected`; `TestInvalidConstLabelNamesAreRejected` |
| 32 | Prometheus adapter rejects const labels that duplicate reserved Shunter labels | `TestConstLabelsDuplicatingReservedShunterLabelsAreRejected` |
| 33 | Metric recorder cannot receive labels outside `MetricLabels` by type | `TestSubsystemMetricsUseExactFamiliesAndLabels`; `TestRecorderWritesCountersGaugesAndHistogramsWithExpectedLabels` |
| 34 | Logs redact bearer tokens, reducer args, row payloads, SQL key/value fields, and signing keys | `TestObservabilityRedactionExamples`; `TestRuntimeStructuredLoggingStartFailedRedactsAndBoundsError`; `TestRuntimeStructuredLoggingReducerPanic`; `TestRuntimeStructuredLoggingProtocolAndSubscriptionEventsRedact` |
| 35 | JSON-shaped redaction replaces sensitive object/string values with valid `[redacted]` JSON strings | `TestObservabilityRedactionBoundaries` |
| 36 | Redacted error truncation respects UTF-8 boundaries and default 1024-byte limit | `TestObservabilityRedactionInvalidUTF8AndTruncation` |
| 37 | Raw SQL appears only in debug logs when explicitly enabled | `TestObservabilityDebugSQLBoundedWhenAllowed` |
| 38 | Debug raw SQL field is UTF-8 normalized and bounded by `ErrorMessageMaxBytes` | `TestObservabilityDebugSQLBoundedWhenAllowed` |
| 39 | `/healthz` absent when `MountHTTP=false` | `TestRuntimeDiagnosticsMountingAndHelperBehavior` |
| 40 | `RuntimeDiagnosticsHandler` serves diagnostics even when `MountHTTP=false` | `TestRuntimeDiagnosticsMountingAndHelperBehavior` |
| 41 | `/healthz` returns 200 for ready, degraded, and not-ready nonfailed runtimes | `TestRuntimeDiagnosticsHealthzAndReadyzStatusSemantics` |
| 42 | `/readyz` returns 200 only when ready and not degraded | `TestRuntimeDiagnosticsHealthzAndReadyzStatusSemantics` |
| 43 | Failed, closing, and closed runtimes return 503 from `/healthz` and `/readyz` | `TestRuntimeDiagnosticsHealthzAndReadyzStatusSemantics` |
| 44 | JSON endpoints reject POST with 405 and `Allow: GET, HEAD` | `TestDiagnosticsMethodHeadPathAndMetricsRules` |
| 45 | HEAD diagnostics use GET status and write no body | `TestDiagnosticsMethodHeadPathAndMetricsRules` |
| 46 | Diagnostics handlers reject trailing-slash and subpath variants with 404 | `TestDiagnosticsMethodHeadPathAndMetricsRules` |
| 47 | Nil runtime/host diagnostics return deterministic failed/empty payloads | `TestRuntimeDiagnosticsNilDebugAndRedaction`; `TestHostDiagnosticsStatusSemantics` |
| 48 | `/debug/shunter/runtime` returns `Runtime.Describe()` JSON without raw secrets | `TestRuntimeDiagnosticsNilDebugAndRedaction`; `TestRuntimeDiagnosticsMountedEndpointsAndProtocolRoute` |
| 49 | `HostDiagnosticsHandler` serves host health/debug endpoints and never serves `/subscribe` | `TestHostDiagnosticsHandlerEndpoints` |
| 50 | `/metrics` is mounted only when a metrics handler is configured | `TestDiagnosticsMethodHeadPathAndMetricsRules`; `TestHostDiagnosticsHandlerEndpoints`; `TestDiagnosticsMetricsPanicRecovered` |
| 51 | Tracing disabled with a non-nil tracer records no spans | `TestTracingDisabledWithTracerRecordsNoSpans` |
| 52 | Tracing enabled records required span names, required default attributes, and redacts attributes | `TestTracingRecordsRequiredSpansAndAttributes`; `TestTracingRedactsFailureErrorsAndExcludesSensitiveAttributes` |
| 53 | Tracing `StartSpan`, `AddEvent`, and `End` panics are recovered and nil spans are skipped | `TestTracingPanicsAndNilSpansDoNotChangeRuntimeResults` |
| 54 | Logger/metrics/tracer panics are recovered and do not change reducer results | `TestObservabilitySinkPanicsRecoveredBeforeRuntimeOperation`; `TestRuntimeStructuredLoggingLoggerPanicIsolation`; `TestTracingPanicsAndNilSpansDoNotChangeRuntimeResults` |

Accepted caveats:

- Row 33 is partly a compile-time API guarantee: `MetricsRecorder` accepts only
  `MetricLabels`, so free-form labels cannot be supplied by typed callers. The
  listed tests exercise that typed surface through root and Prometheus recorder
  paths.
- No runtime hardening gauntlet status update is needed in
  `docs/RUNTIME-HARDENING-GAUNTLET.md`; this slice closes V3 observability
  validation and does not add a new public runtime invariant or failing seed.

Final validation results:

```sh
rtk go fmt ./...
rtk go test ./commitlog ./executor ./protocol ./subscription ./store ./observability/prometheus -count=1
rtk go test . -run 'Test.*(Observability|Health|Diagnostics|Metrics|Logging|Tracing|Recovery|Redaction|Prometheus|Runtime|Host)' -count=1
rtk go vet ./commitlog ./executor ./protocol ./subscription ./store ./observability/prometheus .
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

Results: all commands passed. No final gate was skipped.
