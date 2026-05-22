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

	"github.com/ponchione/shunter"
	"example.com/myapp/internal/app"
)

func main() {
	cfg := shunter.ConfigFromEnv()
	cfg.EnableProtocol = true
	if cfg.DataDir == "" {
		cfg.DataDir = "./data/myapp"
	}

	if err := shunter.Run(context.Background(), app.Module(), cfg); err != nil {
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
rtk go run ./cmd/shunter contract assert --contract examples/hosted-chat/shunter.contract.json --module hosted_chat --module-version v0.1.0 --contract-version 1 --tables 3 --reducers 1 --queries 1 --views 1
rtk go run ./cmd/shunter health --contract examples/hosted-chat/shunter.contract.json
```

`shunter contract validate --contract` validates the local contract artifact
for release gates and review scripts.

`shunter contract assert --contract` validates the local contract artifact and
then checks explicit module, module-version, contract-version, schema-version,
and surface-count expectations for release gates.

`shunter health --contract` validates the local contract artifact only. It
does not check a running Shunter server or protocol endpoint.

Typecheck the frontend-shaped client:

```bash
cd examples/hosted-chat/frontend
npm install
npm run typecheck
```

The generated client imports `@shunter/client`, connects to the Shunter
WebSocket endpoint, calls reducers with generated BSATN argument encoders, and
subscribes to declared views with generated row decoders.

## Release Gate

The hosted-chat gate exercises the example workflow:

```bash
rtk ./scripts/hosted-chat-gate.sh
```

It runs the Go example tests, builds the server, exports and describes the
contract, asserts contract-local surface counts, validates the contract
artifact, checks contract-local health, regenerates TypeScript, and typechecks
the frontend example.
