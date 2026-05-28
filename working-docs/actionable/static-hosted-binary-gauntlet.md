# Static Hosted Binary Gauntlet

Status: implemented static hosted-binary gate slice
Primary backlog item: `deferred-functionality-backlog.md` item 4
Related deferred items: item 1 only as a boundary warning, item 3 only for
offline backup/restore coverage

## Purpose

Create a focused black-box gate for the supported hosted-app product shape:
a normal Go application binary that links an app module and Shunter, starts
with `shunter.Run` or equivalent runtime wiring, accepts Shunter protocol
traffic, persists to a `DataDir`, shuts down cleanly, and recovers after
restart.

The gauntlet should prove the static hosted-server boundary without creating a
generic Shunter daemon. It should exercise a built app binary from the outside
where practical, using app-owned contract export and the generic running-app
CLI exactly as an operator or frontend integration would.

## Current Boundary

Shunter's current product direction is the static self-hosted app server:

- app module code remains linked into a Go binary
- `shunter.Run` is the app-facing entrypoint for the common case
- `Runtime.HTTPHandler` and `Host` cover app-owned routing or multi-runtime
  mounting
- `cmd/shunter` can inspect or operate on running app servers over HTTP and
  the Shunter protocol
- `cmd/shunter` does not dynamically load modules or own a `start` daemon

Existing evidence:

- `scripts/hosted-chat-gate.sh` already builds the hosted-chat server binary,
  starts it on an ephemeral port, uses live `health` and `describe`, calls a
  reducer, calls a procedure, runs a declared query, runs raw SQL, stops the
  server, runs app-owned preflight and migrate, backs up and restores the
  `DataDir`, restarts from the restored copy, regenerates TypeScript, and
  typechecks the frontend.
- `internal/gauntlettests` covers embedded runtime, protocol, read auth,
  recovery, crash, and storage-fault workloads.
- `release-qualification.md` includes
  `rtk bash scripts/static-hosted-binary-gate.sh` in the current minimum
  command set.

Implementation anchors:

- `run.go` exposes `Run(ctx, mod, cfg)` for the static linked-app entrypoint.
- `network.go` owns `Runtime.ListenAndServe`, `Runtime.HTTPHandler`,
  protocol routing, `/subscribe`, and auth config wiring.
- `diagnostics.go` owns `/healthz`, `/readyz`,
  `/debug/shunter/runtime`, and optional metrics endpoints.
- `examples/hosted-chat/cmd/hosted-chat/main.go` is the canonical app
  binary: it calls `ConfigFromEnv`, enables diagnostics and protocol, chooses
  a default `DataDir`, and enters `shunter.Run`.
- `cmd/shunter` already has running-app commands for health, describe, call,
  procedure, query, and raw SQL reads over a running app server.
- `internal/gauntlettests/gauntlet_test.go` and
  `internal/gauntlettests/read_auth_gauntlet_test.go` exercise strict auth
  and protocol behavior through runtime handlers, not built app binaries.
- `internal/gauntlettests/rc_app_workload_test.go` exercises strict auth and
  live declared-view restart behavior for the release-candidate app fixture.
- `internal/gauntlettests/runtime_crash_gauntlet_test.go` covers unclean
  process recovery through test-owned runtime setup.
- `internal/gauntlettests/static_hosted_binary_strict_auth_test.go` now builds
  the hosted-chat binary and covers strict auth, live subscription with clean
  restart, restored `DataDir` restart, and unclean process kill/restart.
- `scripts/static-hosted-binary-gate.sh` is the named static hosted-binary
  gate; it runs the hosted-chat binary gauntlets and the broader hosted-chat
  workflow gate.

Closed gaps:

- Strict-auth configuration is now proven on a freshly built hosted-chat binary
  with valid, missing, malformed, wrong-issuer, and wrong-audience token paths.
- Live WebSocket subscription behavior is now covered against the black-box
  hosted-chat binary process.
- Unclean crash/recovery is now covered against the black-box hosted-chat
  binary by using `/healthz` readiness before protocol setup, waiting for a
  persistent authenticated connection's admission tx to become durable,
  committing through that same connection, waiting for the next durable tx,
  killing the process without a graceful close, and querying recovered state
  after restart with the same `DataDir`.
- `scripts/static-hosted-binary-gate.sh` is the named gate command.
- Build/runtime metadata is asserted through the binary-created `DataDir`
  metadata without adding new product commands.

## Non-Goals

Do not add:

- `shunter start`
- app module dynamic loading
- publish/update/reset/delete commands
- a managed control plane
- a generic server lifecycle API owned by `cmd/shunter`
- distributed process supervision
- online backup orchestration

The gauntlet should treat process supervision as app/operator owned. Shunter's
responsibility is that the linked runtime behaves correctly when the app
process is started, stopped, and restarted.

## Actionable Outcomes

1. Establish a named static-hosted-binary gate that can be pointed at from
   release qualification and future implementation tasks.
2. Keep the gate black-box at the app-server boundary: build a binary, run it,
   talk to it through HTTP/protocol/CLI surfaces, inspect only generated or
   exported artifacts.
3. Add missing hosted-server coverage that is not already covered by
   package-level tests:
   - strict-auth startup and successful authenticated traffic
   - rejected unauthenticated or malformed-token traffic
   - live declared-view or table subscription through a real WebSocket
   - clean shutdown and restart with durable state preserved
   - restored `DataDir` restart and query verification
   - built-binary version/build-info visibility where applicable
4. Keep the existing hosted-chat gate useful. Either extend it carefully or
   create a new gate that reuses hosted-chat fixtures while separating slower
   release-only work from the short local loop.

## Candidate Shape

Preferred first implementation:

- keep `scripts/hosted-chat-gate.sh` as the broad release gate
- add focused pieces to it only when they are cheap and deterministic
- create package tests for protocol-level details that are easier and clearer
  in Go than shell
- add a new script only if the current gate becomes hard to scan or too slow

Possible split:

- `scripts/hosted-chat-gate.sh`: maintained release gate for the canonical
  static hosted-app workflow
- `internal/gauntlettests`: Go-level black-box tests that need strict auth,
  protocol subscriptions, token minting, or lower-level assertions
- `docs/how-to/host-shunter-backend.md`: user-facing command examples after
  the gate shape stabilizes

Avoid putting complex protocol clients into shell. Use shell for process and
CLI orchestration; use Go tests or the TypeScript runtime tests when protocol
message ordering and live subscription details need exact assertions.

First missing-work split:

- Add a cheap strict-auth branch to the hosted-chat gate if token minting can
  be delegated to Go or an existing helper.
- Add binary-level live subscription coverage as a Go gauntlet that launches
  the hosted-chat binary as a child process.
- Keep crash/recovery as package gauntlet coverage unless release testing
  needs an installed-binary crash proof.
- Rename or wrap the release command only after the extra coverage exists, so
  the gate name describes real behavior rather than an aspiration.

## Coverage Matrix

Minimum viable static binary gate:

| Area | Evidence | Current coverage | Gap |
| --- | --- | --- | --- |
| App binary builds | `rtk go build ./examples/hosted-chat/cmd/hosted-chat` | hosted-chat gate | keep |
| App-owned contract export | `cmd/export-contract` | hosted-chat gate | keep |
| Contract artifact review | `describe`, `validate`, `assert` | hosted-chat gate | keep |
| Runtime startup | start built binary with temp `DataDir` and ephemeral port | hosted-chat gate | keep |
| Live diagnostics | running-app `health` and `describe` | hosted-chat gate | keep |
| Reducer call | `shunter call` | hosted-chat gate | keep |
| Procedure call | `shunter procedure` | hosted-chat gate | keep |
| Declared query | `shunter query recent_messages` | hosted-chat gate | keep |
| Raw SQL read | `shunter query --sql` | hosted-chat gate | keep, because raw SQL is a documented escape hatch |
| Live subscription | declared view or table subscription over WebSocket | static hosted-binary Go gauntlet | covered |
| Strict auth | valid token succeeds, invalid token fails | static hosted-binary Go gauntlet | covered |
| Offline preflight | app-owned `maintain preflight` | hosted-chat gate | keep |
| Offline migration | app-owned `maintain migrate` | hosted-chat gate | keep |
| Offline backup/restore | `shunter backup`, `shunter restore` | hosted-chat gate | keep |
| Restart after restore | query restored data | hosted-chat gate | keep |
| Graceful stop | signal process and wait | hosted-chat gate | keep |
| Unclean stop | kill process after durable commit | static hosted-binary Go gauntlet | covered |

## Implementation Notes

### Binary startup

Use an ephemeral port and a temp `DataDir`. The existing gate gets a free port
with a tiny Python snippet wrapped by `rtk`. If this remains in shell, keep it
small and deterministic.

Server readiness should not be a fixed sleep. Poll a real readiness signal:

- `shunter health --url`
- `shunter describe --url`
- or a harmless declared query through the CLI

If startup fails, print the server log before exiting. The current hosted-chat
gate already does this.

### Strict auth branch

The strict-auth smoke should use the current supported configuration:

- `SHUNTER_AUTH_MODE=strict`
- `SHUNTER_AUTH_SIGNING_KEY` for HS256 local verification, or a local
  configured verification key
- `SHUNTER_AUTH_ISSUERS`
- `SHUNTER_AUTH_AUDIENCES`

The test should prove both sides:

- valid token can call a reducer or declared query
- invalid issuer, invalid audience, missing token, or malformed token is
  rejected

Prefer Go for minting test tokens. Shell should not hand-roll JWTs.

Minimum assertions:

- missing token is rejected in strict mode
- malformed token is rejected before reducer/procedure execution
- wrong issuer or audience is rejected
- valid token can call `send_message` or a similarly stable reducer
- valid token can execute at least one declared read
- server logs do not include token material

Relevant files:

- `auth/mint.go`
- `auth/jwt.go`
- `cmd/shunter/running_app.go`
- `cmd/shunter/running_app_test.go`
- `internal/gauntlettests/read_auth_gauntlet_test.go`

### Live subscription branch

Add a binary-level live subscription smoke for at least one stable surface:

- subscribe to hosted-chat `recent_messages` declared view or the underlying
  table if the view is awkward
- wait for subscribe applied and initial rows
- call `send_message` through the running-app CLI or protocol client
- assert an update arrives on the subscribed connection
- unsubscribe and verify the acknowledgement path if the client helper exposes
  it cleanly

Use Go protocol helpers when possible:

- `protocolclient`
- `protocol` message codecs
- existing test helpers in `internal/gauntlettests`

Do not add a new public CLI command solely for subscriptions unless there is a
separate operator need.

Minimum assertions:

- initial subscription result is received
- a later reducer call produces a subscription update without reconnecting
- unsubscribe or connection close is clean
- restart against the same `DataDir` can still serve the declared view

### Restart branches

Clean restart:

1. Start binary.
2. Commit data through a reducer or procedure.
3. Stop via signal and wait for the process to exit.
4. Start the same binary against the same `DataDir`.
5. Query and assert the committed rows.

Restore restart:

1. Stop runtime.
2. Run `shunter backup`.
3. Run `shunter restore` into an empty destination.
4. Start binary against restored destination.
5. Query and assert the committed rows.

Unclean restart:

1. Start binary in strict auth with a temp `DataDir`.
2. Commit data through the protocol reducer path.
3. Wait for `/healthz` to report the next durable tx.
4. Kill the process.
5. Start the same binary against the same `DataDir`.
6. Query `recent_messages` and assert the committed row recovered.

## Staging

Stage A: strengthen hosted-chat gate without broadening product surface.

- assert built-binary build info if the binary exposes it through current
  diagnostics or CLI output
- keep dev-anonymous happy-path coverage as the short local loop
- add strict-auth smoke only if it remains deterministic and fast

Stage B: add a Go binary gauntlet for protocol-sensitive behavior.

- build or locate the hosted-chat binary
- launch it as a child process with temp `DataDir` and ephemeral port
- mint tokens in Go for strict-auth branches
- use protocol helpers for live subscription assertions
- leave process supervision and restart orchestration test-owned

Stage C: decide release-gate naming.

- keep `scripts/hosted-chat-gate.sh` if it remains the canonical command
- otherwise add a thin `scripts/static-hosted-binary-gate.sh` wrapper that
  calls the concrete hosted-chat and Go gauntlet commands
- update release qualification only after the command is stable

## Risks

- Shell-based protocol clients can become brittle; prefer Go protocol helpers
  for live subscription ordering.
- Strict-auth branches can leak token material in logs if failures print full
  commands or environment; keep test tokens short-lived and avoid echoing
  secrets.
- A gate that becomes too slow will stop being run locally. Keep expensive
  crash or installed-binary work release-only and documented.
- Hosted-chat is a fixture, not a generic daemon. Do not make the test require
  app-module loading or app lifecycle ownership by `cmd/shunter`.

## Likely Touched Files

- `scripts/hosted-chat-gate.sh`
- `internal/gauntlettests/*`
- `cmd/shunter/running_app_test.go`
- `examples/hosted-chat/cmd/hosted-chat/main.go`
- `examples/hosted-chat/cmd/maintain/main.go`
- `docs/how-to/host-shunter-backend.md`
- `working-docs/release-qualification.md`

## Validation

Targeted:

```bash
rtk go test ./internal/gauntlettests -run TestStaticHostedBinaryGauntletHostedChat -count=1
rtk go test ./internal/gauntlettests ./cmd/shunter ./examples/hosted-chat/...
rtk bash scripts/static-hosted-binary-gate.sh
```

If strict auth or protocol behavior changes:

```bash
rtk go test ./auth ./protocol ./protocolclient ./internal/gauntlettests
rtk go vet ./auth ./protocol ./protocolclient ./internal/gauntlettests
```

Before promoting to release qualification:

```bash
rtk go test ./...
rtk go vet ./...
rtk go tool staticcheck ./...
rtk bash scripts/static-hosted-binary-gate.sh
```

If the gate becomes part of the minimum release set, update
`working-docs/release-qualification.md` with the exact command and rationale.

## Completion Criteria

This slice is complete when:

- there is one documented command that exercises the static hosted-binary path
- the command uses a built app binary, not only embedded runtime APIs
- it covers live server diagnostics and at least one write/read workflow
- it covers offline backup/restore and restored restart
- strict auth and live subscription coverage either exist in the gate or have
  explicit targeted Go tests referenced by the gate
- `release-qualification.md` names the command if it is release-blocking
