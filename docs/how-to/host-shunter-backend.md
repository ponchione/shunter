# Host Shunter As A Backend

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
- `SHUNTER_AUTH_OIDC_ISSUERS` as semicolon-separated `issuer,jwks-url` pairs
- `SHUNTER_AUTH_OIDC_DISCOVERY_ISSUERS` as semicolon-separated `issuer` or
  `issuer,discovery-url` entries
- `SHUNTER_AUTH_EXTRA_CLAIMS` as comma-separated claim names
- `SHUNTER_AUTH_MAX_EXTRA_CLAIM_BYTES` as a decimal byte limit
- `SHUNTER_AUTH_MAX_EXTRA_CLAIMS_BYTES` as a decimal byte limit

Local development can use dev auth. Public protocol serving should use strict
auth with explicit issuer and audience policy plus local key material or
`AuthOIDCIssuers` explicit JWKS verification for RS256/ES256
identity-provider tokens. `AuthOIDCDiscoveryIssuers` is available for generic
OIDC providers when the app wants Shunter to resolve a discovery document into
a JWKS key source. These key-source settings do not replace
`SHUNTER_AUTH_ISSUERS` or `SHUNTER_AUTH_AUDIENCES`.
At most 32 extra claim names can be configured. Preserved values must be JSON
scalar, object, or array values no deeper than 16 levels. Unset or zero
extra-claim byte limits use the 4096-byte per-claim and 16384-byte total
defaults; negative values fail startup configuration.

Supabase is a delegated-auth provider in this model. Configure Supabase
asymmetric signing-key deployments with explicit JWKS verification:

```text
SHUNTER_AUTH_MODE=strict
SHUNTER_AUTH_OIDC_ISSUERS=https://<project-ref>.supabase.co/auth/v1,https://<project-ref>.supabase.co/auth/v1/.well-known/jwks.json
SHUNTER_AUTH_ISSUERS=https://<project-ref>.supabase.co/auth/v1
SHUNTER_AUTH_AUDIENCES=authenticated
SHUNTER_AUTH_EXTRA_CLAIMS=email,role,session_id,aal,is_anonymous
```

The browser or app owns Supabase login, refresh, and session lifecycle. Shunter
validates the bearer token and enforces `SHUNTER_AUTH_ISSUERS` and
`SHUNTER_AUTH_AUDIENCES`. Optional extra claims are bounded, copy-isolated
caller context for reducers and procedures. Supabase `role` is not a Shunter
permission; use the `permissions` JWT claim or local permission options for
Shunter permission checks. Explicit JWKS remains the preferred Supabase
asymmetric-signing-key configuration path; discovery is generic IdP support, not
a Supabase session or provider SDK integration.

## Standard App Layout

Use the hosted-chat example as the maintained template shape. A new app should
keep the Shunter module declaration in a normal Go package and expose small
app-owned binaries for serving, contract export, and offline maintenance:

```text
cmd/myapp/                  # starts the hosted backend
cmd/export-contract/        # exports shunter.contract.json from app.Module()
cmd/maintain/               # offline preflight and migration commands
internal/app/module.go      # app.Module(), tables, reducers, procedures, reads
frontend/                   # app-owned browser client, if present
```

The server binary should be small and explicit:

```go
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

Keep contract export in an app-owned binary because the generic Shunter CLI does
not dynamically load modules. Build the runtime against a temporary `DataDir`,
export the contract, and close the runtime before handing the artifact to
review or codegen.

Keep offline preflight and migration commands in an app-owned maintenance binary
for the same reason. Use `CheckDataDirCompatibilityReport` before changing a
persisted directory, and use `RunModuleDataDirMigrations` when registered module
migration hooks need to run outside normal serving.

For frontend apps, keep the generated binding file and the runtime package
versioned with the reviewed contract. Until public npm publishing is promoted,
resolve `@shunter/client` through a workspace dependency, `file:` dependency, or
locally packed tarball. SSR-capable frontends should create Shunter WebSocket
clients only from browser-owned lifecycle code; keep server-render paths limited
to generated metadata, types, and normal app data flow.

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
rtk go run ./cmd/shunter contract assert --contract examples/hosted-chat/shunter.contract.json --module hosted_chat --module-version v0.1.0 --contract-version 1 --tables 4 --reducers 1 --procedures 1 --queries 1 --views 1
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

rtk go run ./cmd/shunter query \
  --url http://127.0.0.1:3000 \
  --contract examples/hosted-chat/shunter.contract.json \
  --allow-dev-anonymous \
  --sql "SELECT * FROM messages ORDER BY id DESC LIMIT 10"
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
state transition. `Config.ProcedureResultMaxBytes` caps raw handler result bytes
at 64 MiB by default. Protocol delivery also applies the complete
`Protocol.MaxOutboundMessageSize` envelope and returns a correlated size error
when a result cannot fit.

Typecheck the frontend-shaped client:

```bash
cd examples/hosted-chat/frontend
rtk npm ci
rtk npm run typecheck
```

The generated client imports `@shunter/client`, connects to the Shunter
WebSocket endpoint, wraps the runtime client with `createModuleClient`, calls
reducers and procedures through generated typed facade methods, and subscribes
to declared views and event-table insert streams with generated row decoders.

## Deployment Checklist

Before deploying a static hosted app binary:

1. Build the app server with linker-stamped Shunter build metadata.
2. Export the module contract from the app-owned export binary.
3. Review the contract with `describe`, `contract validate`, `contract assert`,
   and, when upgrading, `contract diff`, `contract policy`, and `contract plan`.
4. Regenerate TypeScript bindings from the reviewed contract, keep WebSocket
   client creation in browser-only lifecycle code, and run the frontend
   typecheck or app-specific browser gate.
5. Run the app-owned maintenance preflight against the target offline `DataDir`.
6. Take an offline backup before any blocking, data-rewrite, or hook-driven
   migration.
7. Start the new binary, then verify live `health --url`, `describe --url`, one
   reducer or procedure call, and one declared read through the running-app CLI.
8. Archive the app binary version, Shunter build metadata, module contract,
   generated binding version, backup metadata, and command evidence together.

Keep dev anonymous auth limited to local development. Public protocol serving
should use strict auth with explicit issuer and audience policy plus local key
material, configured JWKS issuers, or configured OIDC discovery issuers.

## Release Gate

The hosted-chat gate exercises the example workflow:

```bash
rtk ./scripts/hosted-chat-gate.sh
```

It runs the Go example tests, builds the server, exports and describes the
contract, asserts contract-local surface counts, validates the contract
artifact, checks contract-local health, starts the example server on an
ephemeral local port, checks live `health` and `describe`, runs one CLI reducer
call, one CLI procedure call, one declared query, and one raw SQL read against
it, stops the server, runs offline backup and restore, restarts from the
restored `DataDir`, verifies recovered query results, regenerates TypeScript,
and typechecks the frontend example.
