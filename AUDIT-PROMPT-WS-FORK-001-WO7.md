# Audit Prompt — SPEC-WS-FORK-001 WO-7 Shunter Integration

You are an independent auditor. Another agent claims to have integrated the
`github.com/ponchione/websocket v1.8.14-shunter.1` fork into Shunter and wired
`CloseWithContext` into the shutdown path. Your job is to verify — not to fix,
not to extend, not to opine on style. Pass/fail with evidence.

## Repo

- Working tree: `/home/gernsback/source/shunter`
- Spec: `SPEC-WS-FORK-001-v2-close-with-context.md` (§WO-7 is the contract)
- Shell rule: every command prefixed with `rtk` (see `RTK.md`).

## Checks (each must PASS)

### 1. Fork pinned

- `go.mod` contains exactly:
  `replace github.com/coder/websocket => github.com/ponchione/websocket v1.8.14-shunter.1`
- `go.sum` has entries for `github.com/ponchione/websocket v1.8.14-shunter.1`.
- `rtk go doc github.com/coder/websocket Conn.CloseWithContext` prints the
  method signature and doc. If missing, the fork is not actually pinned.

### 2. `protocol/close.go` rewrite

Must match the spec's WO-7 Step 3 body. Specifically:

- Imports: `context`, `time`, `github.com/coder/websocket`. No others.
- `closeWithHandshake` body is a `context.WithTimeout` + one
  `ws.CloseWithContext(ctx, code, truncateCloseReason(reason))` call.
- The old `done := make(chan struct{})` / `go func()` / `time.NewTimer` pattern
  is gone.
- The "IMPORTANT LIMITATION" comment block is gone.
- Close-code constants (`CloseNormal`, `CloseProtocol`, `ClosePolicy`,
  `CloseInternal`) are unchanged.

### 3. Close-call-site audit

Grep the tree for `.Close(` and `.CloseNow(` on `*websocket.Conn`. Confirm:

- **Funneled through `closeWithHandshake`:** `protocol/disconnect.go:49`,
  `protocol/dispatch.go:43`, `protocol/dispatch.go:146`,
  `protocol/keepalive.go:77`. These must still be `go closeWithHandshake(...)`
  invocations with `c.opts.CloseHandshakeTimeout`.
- **Left on plain `Close()` deliberately:** `protocol/upgrade.go:200`,
  `protocol/lifecycle.go:116`, `:134`, `:139`. These are handshake-reject
  paths, not shutdown paths — they must NOT have been swapped.
- Test files (`*_test.go`) may use `.Close` / `.CloseNow` freely; ignore.

If any production shutdown site is still using raw `ws.Close(...)` (not
through `closeWithHandshake`), FAIL. If any rejection path was swapped to
`CloseWithContext`, FAIL — scope creep.

### 4. Timeout-test tightening

`protocol/close_test.go` must contain a test named
`TestCloseWithHandshake_HardTeardownOnTimeout` (the prior
`...DoesNotGuaranteeTransportForceClose` name must be gone). That test must:

- Use an unresponsive peer (client that does NOT read).
- Call `closeWithHandshake` with a ~100ms timeout.
- Assert elapsed is bounded (≤ `timeout + ~400ms`).
- Assert that a subsequent `conn.ws.Write(...)` returns a non-nil error —
  this is the hard-teardown claim. If the Write assertion is missing, FAIL;
  the whole point of the fork is that assertion.

### 5. Story 6.3 doc

`docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.3-clean-close-network-failure.md`:

- The "Implementation note (current transport limitation)" section and its
  `Conn.CloseNow()` / "best-effort" language are gone.
- A new section documents `CloseWithContext` usage and links
  `SPEC-WS-FORK-001-v2-close-with-context.md` via a relative path that
  resolves (check with `ls` against the resolved path).

### 6. Build, vet, tests

Run from repo root:

```
rtk go build ./...
rtk go vet ./...
rtk go test ./... -race -count=1 -timeout=300s
rtk go test ./protocol/ -run 'TestCloseWithHandshake|TestDisconnect|TestCloseAll|TestClientInitiatedClose' -race -count=10 -timeout=300s
```

All four must exit 0. The second `go test` is a stability check: every one
of the 10 runs must PASS. Flakes fail the audit.

### 7. Scope hygiene

`rtk git diff --stat` should show exactly these files touched for WO-7:

- `go.mod`
- `go.sum`
- `protocol/close.go`
- `protocol/close_test.go`
- `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.3-clean-close-network-failure.md`

The working tree contains unrelated pre-existing modifications and
untracked files from earlier work — ignore those. Only fail on WO-7-related
files that should NOT have changed (e.g. other `protocol/*.go` files being
edited beyond close.go / close_test.go), or on files in the list above
whose diff goes beyond what §WO-7 specifies.

## Reporting format

For each of the 7 checks: `PASS` or `FAIL`, one line of evidence (command
output excerpt, line number, or grep result). At the end: overall verdict.
No prose. No suggestions. No "while I was there" observations.

If any check fails, quote the exact failing output. Do not attempt to fix.

## Out of scope

- Do not run `go mod tidy`, do not edit any file, do not create commits.
- Do not audit the fork itself — that was WO-1 through WO-6.
- Do not audit unrelated pre-existing repo state.
