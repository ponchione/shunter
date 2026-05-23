# Running-App Admin CLI Shape

Status: implemented v1 CLI surface
Scope: `shunter call` and `shunter query` commands against a running Shunter
app.

Shunter's generic CLI can target running app servers for declared reducers and
declared queries. It still does not dynamically load app modules; the running
server is the app-owned Go binary that links the module.

Running-app admin commands are explicit about transport, auth, input encoding,
and operator risk.

## Commands

Call a reducer:

```bash
shunter call --url http://127.0.0.1:3000 --contract shunter.contract.json --token "$TOKEN" send_message '{"author":"Ada","body":"hello"}'
```

Run a declared query:

```bash
shunter query --url http://127.0.0.1:3000 --contract shunter.contract.json --token "$TOKEN" recent_messages
```

Development anonymous auth must be explicit:

```bash
shunter query --url http://127.0.0.1:3000 --contract shunter.contract.json --allow-dev-anonymous recent_messages
```

These commands target declared app surfaces:

- `call` invokes a named reducer exported in the contract.
- `query` invokes a named declared query exported in the contract.
- Declared views remain subscription-oriented and should not be folded into
  `query` unless a future one-shot view read is deliberately added.

The contract path stays required even when the app can expose its own contract.
The CLI uses the local contract to validate names, permissions metadata,
parameter schemas, and generated-style argument encoding before sending any
request.
Use local contract helpers such as `contractworkflow.FindReducer` and
`contractworkflow.FindQuery` for name validation before dialing a running app.
Load the artifact with `contractworkflow.LoadContractFile` so malformed or
semantically invalid local contract JSON fails before any transport work.

## Transport Decision

The commands use the existing Shunter WebSocket protocol. `http://` and
`https://` URLs are normalized to the `/subscribe` WebSocket endpoint.

Reasons:

- The protocol already has reducer-call and declared-query message families.
- Strict auth already protects protocol connections.
- Adding generic HTTP management endpoints would require separate enablement,
  auth, request-size, CSRF, logging, and deployment documentation.

There are no new server endpoints. If HTTP admin routes are later added, they
must be opt-in and documented as a distinct management surface, not enabled
implicitly by `shunter.Run`.

## Package Boundaries

The package split is:

- `protocolclient`: owns WebSocket dialing, subprotocol negotiation, bearer
  token presentation, request IDs, bounded waits for responses, protocol
  message encode/decode, and connection shutdown.
- `cmd/shunter`: owns flags, environment fallback, terminal output, exit
  status, file reading, and command help.
- `contractworkflow` or a narrow internal helper: owns loading and validating a
  `ModuleContract` from JSON for CLI use.
- A contract argument encoding helper: owns JSON object to `types.ProductValue`
  conversion from exported product schemas, then delegates BSATN byte encoding
  to existing runtime encoding code.

Do not make `protocolclient` responsible for operator policy, interactive
confirmation, local contract discovery, or command output. It should be a
typed transport helper that can also support app-owned maintenance binaries
later.

The existing `protocol` package should remain the shared wire-codec and server
transport package. A client package can reuse exported message types and
`EncodeClientMessage` / `DecodeServerMessage` rather than duplicating frame
formats. Server admission, subscription fanout, and runtime lifecycle should
stay out of the client package.

## Timeout Behavior

Running-app commands use a bounded end-to-end deadline, not separate unbounded
dial, write, and read waits.

- `--timeout` defaults to 10 seconds.
- The command derives a context with deadline before dialing.
- The same deadline applies to WebSocket dial, token handshake, request write,
  response wait, and close.
- Timeout returns a non-zero exit status and includes the target URL, command
  kind, and reducer or query name in the error.
- Reducer calls are not retried automatically. If a query retry is added later,
  it must be explicit and limited to transport failures before the request is
  accepted.

The client package should surface timeout errors distinctly enough for CLI tests
to assert them without parsing human text.

## Auth Requirements

Running-app commands require an explicit credential flag, environment variable,
or the development-only `--allow-dev-anonymous` flag. They do not silently rely
on dev anonymous auth for operator writes.

- `--token <jwt>` for direct bearer token input.
- `--token-file <path>` for local automation.
- `SHUNTER_TOKEN` as the environment fallback.
- `--allow-dev-anonymous` for explicit tokenless development connections only.

When multiple sources are supplied, command-line flags should win over the
environment. Errors should say that a token is required for running-app admin
commands.

`cmd/shunter` resolves the credential source and passes only the selected token
to the client package. The client package attaches the token using
the same protocol authentication path expected by normal clients and should not
read environment variables itself.

## Encoding Rules

JSON is the ergonomic CLI input format, but the wire payload must still follow
the contract's reducer or query parameter schema.

- Reject unknown reducer or query names before connecting.
- Reject missing contract product schemas for JSON argument mode.
- Encode JSON objects to BSATN product rows by declared column name.
- Support `--args`, `--args-file`, and `--args-hex`; positional JSON arguments
  are also accepted.
- Decode rows through contract row schemas when present.
- Emit text by default and JSON with `--format json`.

The CLI should not infer reducer argument formats when the contract does not
declare them. In that case, operators must use raw bytes mode or the app should
export product schemas.

Generated TypeScript already provides the user-facing model for contract-aware
encoding: helpers know the reducer or declared-read schema, encode strongly
typed arguments, and decode rows through contract metadata. The Go CLI should
reuse the same contract semantics, but it should not depend on generated
TypeScript artifacts at runtime.

For the CLI, prefer adding a Go helper that converts JSON values to
`types.Value` / `types.ProductValue` using exported product schemas and then
delegates binary encoding to `bsatn` or the existing protocol row encoders.
This keeps the CLI independent of frontend builds while preserving the same
field names, required fields, type checks, and row decoding rules that generated
clients use.

Avoid CLI-specific BSATN writers. If a needed primitive cannot be represented
through current `types.Value` or `bsatn` helpers, add the missing shared helper
with tests rather than open-coding binary layouts under `cmd/shunter`.

## Safety Defaults

Reducer calls are writes. The first `call` implementation should be
conservative:

- Require an explicit reducer name and payload.
- Print the target URL, module name, and reducer before sending in text mode.
- Return non-zero for reducer errors, rejected auth, protocol errors, malformed
  responses, stale contract mismatches, or timeouts.
- Use a bounded default timeout.
- Never retry reducer calls automatically.

Declared queries are reads, but they can still expose private data. `query`
must use the same auth and timeout rules as `call`.

## Error Contract

Running-app commands use the same broad exit-code shape as the existing CLI:

- Exit `0` for successful calls or queries.
- Exit `1` for runtime failures after flags and local inputs are valid,
  including auth rejection, connection failure, timeout, protocol errors,
  reducer errors, query errors, malformed server responses, stale contract
  mismatches, and response decoding failures.
- Exit `2` for local command misuse, including missing required flags, invalid
  flag values, malformed JSON input, unknown reducer or query names, missing
  token sources, and schema-less JSON argument mode.

Text output can stay terse, but JSON output must be stable enough for operator
automation. Failed JSON output should include:

- `status`: `"error"`.
- `scope`: `"running_app"`.
- `command`: `"call"` or `"query"`.
- `target_url`: the requested app URL.
- `surface`: the reducer or query name when known.
- `error_code`: a stable snake_case classifier such as `missing_token`,
  `unknown_surface`, `timeout`, `auth_rejected`, `protocol_error`,
  `reducer_error`, `query_error`, or `decode_error`.
- `message`: a human-readable summary.

Example missing-token JSON error:

```json
{
  "status": "error",
  "scope": "running_app",
  "command": "call",
  "target_url": "http://127.0.0.1:3000",
  "surface": "send_message",
  "error_code": "missing_token",
  "message": "token is required for running-app admin commands"
}
```

Example timeout JSON error:

```json
{
  "status": "error",
  "scope": "running_app",
  "command": "query",
  "target_url": "http://127.0.0.1:3000",
  "surface": "recent_messages",
  "error_code": "timeout",
  "message": "query recent_messages timed out before a response was received"
}
```

Do not put bearer tokens, raw request payloads, or decoded private rows in error
objects. If a future implementation needs deeper diagnostics, add an explicit
verbose or trace mode that redacts credentials before printing.

## Non-Goals For The First Slice

Do not include these in the first implementation:

- SQL DML or arbitrary SQL mutation.
- Dynamic module publish, update, or loading.
- Generic HTTP management endpoints.
- JWKS/OIDC discovery.
- Stored admin profiles or credential keychains.
- App-owned custom reducer argument codecs.
- Multi-module host discovery.

## Implemented Coverage

- Contract-driven JSON-to-product encoding tests live in `contractworkflow`.
- `protocolclient` covers explicit token handling, anonymous opt-in, timeout
  classification, reducer calls, and declared-query responses.
- `cmd/shunter` tests cover token sources, missing tokens, unknown names,
  malformed JSON, JSON output, file-backed args, and query row decoding.
- The hosted-chat gate starts a real example server, runs one reducer call, and
  runs one declared query.

## Test Strategy

Build the feature in layers:

- Unit-test JSON-to-product conversion with the same schema shapes exported by
  hosted-chat: successful object input, missing required fields, unknown
  fields, type mismatches, null handling, arrays or objects where scalars are
  required, and deterministic column ordering.
- Unit-test `protocolclient` with `httptest` and the real WebSocket protocol
  handler where possible. Cover missing or rejected tokens, subprotocol
  negotiation failure, malformed server frames, reducer failures, declared
  query responses, context cancellation, and timeouts.
- Command tests should exercise flag precedence, `SHUNTER_TOKEN` fallback,
  `--token-file`, missing token errors, schema-less JSON rejection, raw bytes
  mode, text output, JSON output, and exit status.
- Hosted-chat gate coverage should start a real example server on an ephemeral
  address, export the matching contract, run one reducer call, run one declared
  query, and shut the server down cleanly.

Tests should assert structured errors or JSON fields whenever practical. Human
text can stay terse and should not be the only contract for automation.
