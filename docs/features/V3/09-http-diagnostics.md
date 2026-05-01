# V3 Task 09: HTTP Diagnostics

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Task 04 health and description expansion
- Task 08 Prometheus adapter, unless using a test-only metrics handler first

Objective: expose SPEC-007 runtime and host diagnostics endpoints with exact
method, status, payload, path, and metrics mounting behavior.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` sections 4, 9, 10,
  10.1, 10.2, 10.3, 10.4, 11, and 14
- `docs/features/V3/04-health-and-descriptions.md`

Inspect:

```sh
rtk go doc . Runtime.HTTPHandler
rtk go doc . Host.HTTPHandler
rtk go doc . Runtime.Health
rtk go doc . Host.Health
rtk grep -n 'HTTPHandler|HandleSubscribe|ServeHTTP|healthz|readyz|debug/shunter|metrics' *.go protocol host.go network.go
```

## Target Behavior

Add runtime diagnostics:

- `Runtime.HTTPHandler()` continues to serve `/subscribe` as before
- when `Config.Observability.Diagnostics.MountHTTP=false`, SPEC-007 endpoints
  are absent and return `404 Not Found`
- when mounted, runtime handler serves `/healthz`, `/readyz`,
  `/debug/shunter/runtime`, and `/metrics` only when a metrics handler is
  configured

Add explicit helpers:

```go
func RuntimeDiagnosticsHandler(r *Runtime) http.Handler
func HostDiagnosticsHandler(h *Host, metrics http.Handler) http.Handler
```

Helper behavior:

- `RuntimeDiagnosticsHandler` serves runtime diagnostics regardless of the
  runtime's `MountHTTP` setting
- `HostDiagnosticsHandler` serves host diagnostics and never serves
  `/subscribe`
- host `/metrics` is mounted only when the helper receives a non-nil metrics
  handler

Endpoint rules:

- JSON diagnostics accept only `GET` and `HEAD`
- unsupported methods return `405 Method Not Allowed` with
  `Allow: GET, HEAD`
- `GET` returns `Content-Type: application/json` and should set
  `Cache-Control: no-store`
- `HEAD` returns the same status and headers as `GET` with no body
- exact paths only; trailing slash and subpath variants return `404`
- runtime and host status codes follow SPEC-007 section 10.2
- debug endpoints return `Runtime.Describe()` or `Host.Describe()` JSON
- nil runtime and nil host inputs produce deterministic failed/empty payloads
- `RuntimeDiagnosticsHandler(nil)` debug output includes a zero runtime
  description plus the failed health object
- `RuntimeDiagnosticsHandler` uses the runtime's configured metrics handler;
  `HostDiagnosticsHandler` mounts `/metrics` only from its explicit metrics
  argument
- payloads obey redaction and nil-slice `[]` requirements
- diagnostics handler panics, including delegated metrics handler panics, do
  not escape the HTTP boundary

## Tests To Add First

Add focused failing tests for:

- `/healthz` absent when `MountHTTP=false`
- runtime diagnostics mount when `MountHTTP=true` without changing
  `/subscribe`
- `RuntimeDiagnosticsHandler` serves diagnostics even when `MountHTTP=false`
- `/healthz` returns 200 for ready, degraded, and not-ready nonfailed runtimes
- `/readyz` returns 200 only when ready and not degraded
- failed, closing, and closed runtimes return 503 from `/healthz` and
  `/readyz`
- JSON endpoints reject POST with 405 and `Allow: GET, HEAD`
- HEAD diagnostics use GET status and write no body
- trailing-slash and subpath variants return 404
- nil runtime diagnostics return failed health/readiness payloads and debug
  payload with a zero runtime description, failed health object, and status 200
- nil or empty host diagnostics return deterministic failed/empty payloads
- `/debug/shunter/runtime` returns `Runtime.Describe()` JSON without secrets
- `HostDiagnosticsHandler` serves host endpoints and never serves `/subscribe`
- `/metrics` is mounted only when a metrics handler is configured
- delegated metrics handler panics are recovered at the diagnostics boundary
- query strings do not affect routing

## Validation

Run at least:

```sh
rtk go fmt .
rtk go test . -run 'Test.*(Diagnostics|Healthz|Readyz|HTTP|Metrics|Host|Runtime|Describe)' -count=1
rtk go vet .
```

Expand to `rtk go test ./... -count=1` if HTTP routing changes touch protocol
or host behavior broadly.

## Completion Notes

When complete, update this file with:

- exported handler helpers added
- runtime handler mounting behavior
- endpoint/status/payload test coverage
- validation commands run
