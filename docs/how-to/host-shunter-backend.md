# Host Shunter As A Backend

Status: current v1 app-author guidance
Scope: standard static Go app server path for TypeScript frontends.

The hosted app model is a normal Go binary that links your module and Shunter.
Shunter is the backend boundary for application state; it is not a managed
cloud database and it does not dynamically load arbitrary module code.

## Server Entrypoint

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ponchione/shunter"
	"example.com/myapp/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := shunter.ConfigFromEnv()
	cfg.EnableProtocol = true
	cfg.Observability.Diagnostics.MountHTTP = true
	if cfg.DataDir == "" {
		cfg.DataDir = "./data/myapp"
	}

	if err := shunter.Run(ctx, app.Module(), cfg); err != nil {
		log.Fatal(err)
	}
}
```

`Run` builds the runtime, starts protocol serving on `Config.ListenAddr`, and
closes the runtime when the context is canceled. Use `Build`,
`Runtime.Start`, and `Runtime.HTTPHandler` only when your app needs custom HTTP
routing or more direct lifecycle control.

## Environment

`ConfigFromEnv` reads a small Shunter-scoped environment surface:

- `SHUNTER_DATA_DIR`
- `SHUNTER_LISTEN_ADDR`
- `SHUNTER_ENABLE_PROTOCOL`
- `SHUNTER_AUTH_MODE` with `dev` or `strict`
- `SHUNTER_AUTH_SIGNING_KEY`
- `SHUNTER_AUTH_ISSUERS` as comma-separated values
- `SHUNTER_AUTH_AUDIENCES` as comma-separated values

Local development can use dev auth. Public protocol serving should use strict
auth with explicit issuer, audience, and local key material. JWKS/OIDC discovery
is still future work.

## Example Workflow

Run the canonical example:

```bash
rtk go run ./examples/hosted-chat/cmd/hosted-chat
```

Export the app contract from the app-owned binary:

```bash
rtk go run ./examples/hosted-chat/cmd/export-contract --out examples/hosted-chat/shunter.contract.json
```

Generate TypeScript bindings:

```bash
rtk go run ./cmd/shunter contract codegen \
  --contract examples/hosted-chat/shunter.contract.json \
  --language typescript \
  --out examples/hosted-chat/frontend/src/generated/hosted_chat.ts
```

Inspect the generated contract before handing it to frontend code:

```bash
rtk go run ./cmd/shunter describe --contract examples/hosted-chat/shunter.contract.json
rtk go run ./cmd/shunter describe --contract examples/hosted-chat/shunter.contract.json --format json
rtk go run ./cmd/shunter contract validate --contract examples/hosted-chat/shunter.contract.json
rtk go run ./cmd/shunter contract assert --contract examples/hosted-chat/shunter.contract.json --module hosted_chat --module-version v0.1.0 --contract-version 1 --tables 3 --reducers 1 --procedures 1 --queries 1 --views 1
rtk go run ./cmd/shunter health --contract examples/hosted-chat/shunter.contract.json
```

Call the running example server through the generic admin CLI:

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

Use `--token`, `--token-file`, or `SHUNTER_TOKEN` for normal running-app admin
commands. `--allow-dev-anonymous` is only for explicit local development
against dev-auth apps.

`shunter contract validate --contract` validates the local contract artifact
for release gates and review scripts.

`shunter contract assert --contract` validates the local contract artifact and
then checks explicit module, module-version, contract-version, schema-version,
and surface-count expectations for release gates.
When run with `--format json`, it includes `assertion_count` and
`failure_count` aggregate fields so gates can check totals without walking the
full assertion list.

`shunter health --contract` validates the local contract artifact only. It
does not check a running Shunter server or protocol endpoint.
Use `shunter health --url` and `shunter describe --url` for live server
diagnostics; the running app must mount diagnostics HTTP endpoints.
The running-app CLI accepts root app URLs and `/subscribe` protocol URLs for
live diagnostics, strips query strings and fragments, and rewrites them to the
mounted diagnostics endpoints.

## Procedures For Service Adapters

Reducers remain the mutation boundary for durable app state. Use procedures
for client-callable workflows that may need external I/O before deciding what
state change to request: geocoding, email delivery, payment-provider calls,
search indexing, object-storage validation, or similar app-owned services.

Procedure handlers run outside the serialized reducer executor. They receive a
`ProcedureContext` with the caller identity, connection ID, auth principal, and
permission tags copied from the protocol or local call. When a procedure needs
to mutate Shunter state, call `ctx.CallReducer`; the reducer runs on the
executor only for that reducer transaction and sees the same caller context.

```go
mod.Procedure("send_system_message", sendSystemMessage,
	shunter.WithProcedureArgs(shunter.ProductSchema{
		Columns: []shunter.ProductColumn{{Name: "body", Type: "string"}},
	}),
)

func sendSystemMessage(ctx *shunter.ProcedureContext, args []byte) ([]byte, error) {
	body := decodeAndValidate(args)

	// External I/O, if needed, belongs here before opening a reducer
	// transaction.
	reducerArgs := encodeSendMessageArgs("System", body)
	res, err := ctx.CallReducer("send_message", reducerArgs)
	if err != nil {
		return nil, err
	}
	if res.Error != nil {
		return nil, res.Error
	}
	return res.ReturnBSATN, nil
}
```

Procedure permissions are checked before the handler runs. Reducer permissions
are checked again when `ctx.CallReducer` submits the reducer, so grant both the
procedure permission and any reducer permission needed for that workflow.

A procedure failure returns to the caller as a procedure response error. It does
not roll back reducer transactions that the procedure already committed; design
multi-step service workflows so each reducer call is an intentional durable
state transition.

Typecheck the frontend-shaped client:

```bash
cd examples/hosted-chat/frontend
npm install
npm run typecheck
```

The generated client imports `@shunter/client`, connects to the Shunter
WebSocket endpoint, calls reducers and procedures with generated BSATN argument
encoders, and subscribes to declared views with generated row decoders.

## Release Gate

The hosted-chat gate exercises the example workflow:

```bash
rtk ./scripts/hosted-chat-gate.sh
```

It runs the Go example tests, builds the server, exports and describes the
contract, asserts contract-local surface counts, validates the contract
artifact, checks contract-local health, starts the example server on an
ephemeral local port, checks live `health` and `describe`, runs one CLI reducer
call, one CLI procedure call, and one declared query against it, stops the
server, runs offline backup and restore, restarts from the restored `DataDir`,
verifies recovered query results, regenerates TypeScript, and typechecks the
frontend example.
