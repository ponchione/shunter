# Hosted Chat Example

This example is the canonical hosted-app shape for Shunter Phase 1. It links a
Go module into a normal static Go server, exports a module contract from an
app-owned binary, generates TypeScript bindings, and typechecks a small
frontend-shaped client.

## Run The Backend

```bash
rtk go run ./examples/hosted-chat/cmd/hosted-chat
```

Useful local configuration:

```bash
SHUNTER_DATA_DIR=./examples/hosted-chat/data \
SHUNTER_LISTEN_ADDR=127.0.0.1:3000 \
rtk go run ./examples/hosted-chat/cmd/hosted-chat
```

The server uses `shunter.ConfigFromEnv`, enables protocol serving and
diagnostics HTTP, and calls `shunter.Run` with a context canceled by interrupt
or SIGTERM.

## Export And Generate

Export the contract from the app-owned binary:

```bash
rtk go run ./examples/hosted-chat/cmd/export-contract --out examples/hosted-chat/shunter.contract.json
```

Generate TypeScript bindings from the exported contract:

```bash
rtk go run ./cmd/shunter contract codegen \
  --contract examples/hosted-chat/shunter.contract.json \
  --language typescript \
  --out examples/hosted-chat/frontend/src/generated/hosted_chat.ts
```

Inspect the exported app surface with the generic CLI:

```bash
rtk go run ./cmd/shunter describe --contract examples/hosted-chat/shunter.contract.json
rtk go run ./cmd/shunter describe --contract examples/hosted-chat/shunter.contract.json --section reducers --format json
rtk go run ./cmd/shunter contract validate --contract examples/hosted-chat/shunter.contract.json
rtk go run ./cmd/shunter contract assert --contract examples/hosted-chat/shunter.contract.json --module hosted_chat --module-version v0.1.0 --contract-version 1 --tables 4 --reducers 1 --procedures 1 --queries 1 --views 1
rtk go run ./cmd/shunter health --contract examples/hosted-chat/shunter.contract.json
```

Run module-aware offline maintenance from the app-owned binary:

```bash
rtk go run ./examples/hosted-chat/cmd/maintain preflight \
  --data-dir ./examples/hosted-chat/data \
  --format json

rtk go run ./examples/hosted-chat/cmd/maintain prepare-backup \
  --data-dir ./examples/hosted-chat/data \
  --format json

rtk go run ./examples/hosted-chat/cmd/maintain migrate \
  --data-dir ./examples/hosted-chat/data
```

The generic `shunter` CLI can copy offline `DataDir` directories, but
schema-aware preflight, snapshot/compaction preparation, and migration commands
must live in an app-owned binary so they can link `app.Module()` directly.
Run `prepare-backup` only after the serving process has stopped: it opens the
existing DataDir without starting normal runtime services, schedulers, startup
migration hooks, or protocol serving; creates a snapshot; waits for that
horizon; compacts the covered log prefix; and closes before reporting success.
Missing DataDirs and invalid output formats fail before mutation.

With the backend running, call the reducer and read the declared query through
the running-app CLI:

```bash
rtk go run ./cmd/shunter health \
  --url http://127.0.0.1:3000

rtk go run ./cmd/shunter describe \
  --url http://127.0.0.1:3000

rtk go run ./cmd/shunter call \
  --url http://127.0.0.1:3000 \
  --contract examples/hosted-chat/shunter.contract.json \
  --allow-dev-anonymous \
  send_message '{"author":"Ada","body":"hello"}'

rtk go run ./cmd/shunter procedure \
  --url http://127.0.0.1:3000 \
  --contract examples/hosted-chat/shunter.contract.json \
  --allow-dev-anonymous \
  send_system_message '{"body":"hello from a procedure"}'

rtk go run ./cmd/shunter query \
  --url http://127.0.0.1:3000 \
  --contract examples/hosted-chat/shunter.contract.json \
  --allow-dev-anonymous \
  recent_messages

rtk go run ./cmd/shunter query \
  --url http://127.0.0.1:3000 \
  --contract examples/hosted-chat/shunter.contract.json \
  --allow-dev-anonymous \
  --sql "SELECT * FROM messages ORDER BY id DESC LIMIT 10"
```

Use `--token`, `--token-file`, or `SHUNTER_TOKEN` for non-development admin
commands. `--allow-dev-anonymous` is only for explicit local dev-auth runs.

Typecheck the frontend-shaped client:

```bash
cd examples/hosted-chat/frontend
rtk npm ci
rtk npm run typecheck
```

The TypeScript example asserts generated contract compatibility, connects to
`/subscribe`, uses an optional browser token-provider path when
`hosted-chat-token` is present in `localStorage`, enables bounded reconnect,
creates the generated module client facade, subscribes to transient
`message_events` inserts, keeps a decoded `live_messages` managed handle, and
calls the generated `send_message` reducer facade and `send_system_message`
procedure facade. The procedure is
intentionally shaped as a small service-adapter workflow: it runs outside the
reducer executor, validates procedure arguments, and then calls the
`send_message` reducer to make the durable state change.

## Release Gate

From the repository root:

```bash
rtk ./scripts/hosted-chat-gate.sh
```

The gate builds and tests the Go example, exports the contract, asserts
contract-local surface counts, validates the contract artifact, checks
contract-local health, runs app-owned fresh preflight, starts a real server on
an ephemeral local port, checks live `health` and `describe`, runs one CLI
reducer call, one CLI procedure call, one declared query, and one raw SQL read
against it, stops the server, runs app-owned compatible preflight and no-op
migration, creates a snapshot and compacts its covered log prefix, runs offline
backup and restore, preflights the restored DataDir, restarts from it, verifies
recovered query results, regenerates the TypeScript bindings, and runs the
frontend typecheck.
