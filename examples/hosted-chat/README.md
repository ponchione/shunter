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
rtk go run ./cmd/shunter contract assert --contract examples/hosted-chat/shunter.contract.json --module hosted_chat --module-version v0.1.0 --contract-version 1 --tables 3 --reducers 1 --procedures 1 --queries 1 --views 1
rtk go run ./cmd/shunter health --contract examples/hosted-chat/shunter.contract.json
```

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
```

Use `--token`, `--token-file`, or `SHUNTER_TOKEN` for non-development admin
commands. `--allow-dev-anonymous` is only for explicit local dev-auth runs.

Typecheck the frontend-shaped client:

```bash
cd examples/hosted-chat/frontend
npm install
npm run typecheck
```

The TypeScript example connects to `/subscribe`, calls the generated
`send_message` reducer helper and `send_system_message` procedure helper, and
subscribes to the generated `live_messages` view helper with decoded rows. The
procedure is intentionally shaped as a small service-adapter workflow: it runs
outside the reducer executor, validates procedure arguments, and then calls the
`send_message` reducer to make the durable state change.

## Release Gate

From the repository root:

```bash
rtk ./scripts/hosted-chat-gate.sh
```

The gate builds and tests the Go example, exports the contract, asserts
contract-local surface counts, validates the contract artifact, checks
contract-local health, starts a real server on an ephemeral local port, checks
live `health` and `describe`, runs one CLI reducer call, one CLI procedure
call, and one declared query against it, stops the server, runs offline backup
and restore, restarts from the restored `DataDir`, verifies recovered query
results, regenerates the TypeScript bindings, and runs the frontend typecheck.
