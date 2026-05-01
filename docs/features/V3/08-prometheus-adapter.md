# V3 Task 08: Prometheus Adapter

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Task 06 metrics core, lifecycle, and recovery
- Task 07 subsystem metrics

Objective: add `observability/prometheus` as the optional Prometheus adapter
for the Shunter metrics model without importing Prometheus from the root
package.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` sections 6, 7, and
  14
- `docs/features/V3/06-metrics-core-lifecycle-recovery.md`
- `docs/features/V3/07-subsystem-metrics.md`

Inspect:

```sh
rtk go doc . MetricsRecorder
rtk go doc . MetricName
rtk go doc . MetricLabels
rtk go list -json .
rtk grep -n 'prometheus|MetricsRecorder|MetricName|MetricLabels' . go.mod go.sum
```

## Target Behavior

Add package:

```go
github.com/ponchione/shunter/observability/prometheus
```

Implement the SPEC-007 adapter API:

- `Config`
  - `Namespace`
  - `Registerer`
  - `Gatherer`
  - `ConstLabels`
- `Adapter`
- `New`
- `(*Adapter).Recorder`
- `(*Adapter).Handler`

Adapter requirements:

- empty namespace normalizes to `"shunter"`
- non-empty namespace validates as a Prometheus metric-name prefix
- nil registerer creates a private registry
- the adapter never uses `prometheus.DefaultRegisterer` unless the caller
  passes it explicitly
- gatherer selection follows SPEC-007 section 7, including returning an error
  when a non-nil registerer has no gatherer and does not satisfy
  `prometheus.Gatherer`
- const labels are copied
- const label names validate as Prometheus labels
- const labels cannot duplicate Shunter reserved labels
- all metric families from SPEC-007 section 6 are registered exactly once
- collector registration failure makes `New` return an error and leaves the
  adapter unusable
- duration histograms use the exact default buckets from SPEC-007 section 6
- unknown metric names are ignored rather than panicking
- `Handler()` serves Prometheus text format from the configured gatherer

The root `github.com/ponchione/shunter` package must not import Prometheus
packages.

## Tests To Add First

Add focused failing tests for:

- nil registerer uses a private registry and does not pollute global
  Prometheus registration
- custom registry registers all families with default namespace `shunter`
- non-nil registerer without a usable gatherer is rejected when `Gatherer` is
  not supplied
- custom namespace prefixes every family
- invalid namespace is rejected
- invalid const-label names are rejected
- const labels that duplicate reserved Shunter labels are rejected
- const labels are copied at adapter construction
- duration histograms expose exact SPEC-007 bucket boundaries
- recorder ignores unknown metric names
- recorder writes counters, gauges, and histograms with the expected labels
- `Handler()` exposes gathered metrics in Prometheus text format
- root package import graph does not include Prometheus packages

## Validation

Run at least:

```sh
rtk go fmt ./observability/prometheus .
rtk go test ./observability/prometheus -count=1
rtk go test . -run 'Test.*(Metrics|Observability|Import)' -count=1
rtk go vet ./observability/prometheus .
rtk go list -json . ./observability/prometheus
```

Expand to `rtk go test ./... -count=1` if dependency or metrics API changes
affect other packages.

## Completion Notes

When complete, update this file with:

- adapter API and files added
- Prometheus dependency behavior
- registered family coverage
- validation commands run
