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

The server uses `shunter.ConfigFromEnv`, enables protocol serving, and calls
`shunter.Run(context.Background(), app.Module(), cfg)`.

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
rtk go run ./cmd/shunter health --contract examples/hosted-chat/shunter.contract.json
```

Typecheck the frontend-shaped client:

```bash
cd examples/hosted-chat/frontend
npm install
npm run typecheck
```

The TypeScript example connects to `/subscribe`, calls the generated
`send_message` reducer helper, and subscribes to the generated `live_messages`
view helper with decoded rows.

## Release Gate

From the repository root:

```bash
rtk ./scripts/hosted-chat-gate.sh
```

The gate builds and tests the Go example, exports the contract, checks
describe JSON counts, validates contract-local health, regenerates the
TypeScript bindings, and runs the frontend typecheck.
