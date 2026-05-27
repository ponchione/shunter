# Running-App Admin CLI Reference

Status: implemented v1 CLI surface
Scope: `shunter call`, `shunter procedure`, `shunter query`,
`shunter health --url`, and `shunter describe --url` commands against a
running Shunter app.

Shunter's generic CLI can target running app servers for declared reducers,
procedures, and declared queries. It still does not dynamically load app
modules; the running server is the app-owned Go binary that links the module.

Running-app admin commands are explicit about transport, auth, input encoding,
and operator risk.

## Commands

Call a reducer:

```bash
shunter call --url http://127.0.0.1:3000 --contract shunter.contract.json --token "$TOKEN" send_message '{"author":"Ada","body":"hello"}'
```

Call a procedure:

```bash
shunter procedure --url http://127.0.0.1:3000 --contract shunter.contract.json --token "$TOKEN" send_system_message '{"body":"hello"}'
```

Run a declared query:

```bash
shunter query --url http://127.0.0.1:3000 --contract shunter.contract.json --token "$TOKEN" recent_messages
```

Check live runtime health:

```bash
shunter health --url http://127.0.0.1:3000
```

Describe the live runtime diagnostics snapshot:

```bash
shunter describe --url http://127.0.0.1:3000
```

Development anonymous auth must be explicit:

```bash
shunter query --url http://127.0.0.1:3000 --contract shunter.contract.json --allow-dev-anonymous recent_messages
```

These commands target declared app surfaces:

- `call` invokes a named reducer exported in the contract.
- `procedure` invokes a named procedure exported in the contract.
- `query` invokes a named declared query exported in the contract.
- `health --url` checks the mounted runtime diagnostics `/healthz` endpoint.
- `describe --url` reads the mounted runtime diagnostics debug snapshot.
- Declared views remain subscription-oriented. `query` targets declared
  queries, not live view subscriptions.

The contract path stays required for `call`, `procedure`, and `query` even
when the app can expose its own contract. The CLI uses the local contract to
validate names, permissions metadata, parameter schemas, and generated-style
argument encoding before sending any request.
Use local contract helpers such as `contractworkflow.FindReducer`,
`contractworkflow.FindProcedure`, and `contractworkflow.FindQuery` for name
validation before dialing a running app. Load the artifact with
`contractworkflow.LoadContractFile` so malformed or semantically invalid local
contract JSON fails before any transport work.

## Transport Decision

`call`, `procedure`, and `query` use the existing Shunter WebSocket protocol.
`http://` and `https://` URLs are normalized to the `/subscribe` WebSocket
endpoint.
`health --url` and `describe --url` use diagnostics HTTP endpoints mounted by
the app.
For both protocol and diagnostics commands, query strings and URL fragments are
stripped before the request is made. A root URL targets the default endpoint;
a URL ending in `/subscribe` is treated as the app's protocol URL and is
rewritten to the corresponding diagnostics endpoint for `health --url` and
`describe --url`.

Reasons:

- The protocol already has reducer-call, procedure-call, and declared-query
  message families.
- Strict auth already protects protocol connections.
- Adding generic HTTP management endpoints would require separate enablement,
  auth, request-size, CSRF, logging, and deployment documentation.

There are no reducer, procedure, or query HTTP admin endpoints. If those routes
are later added, they must be opt-in and documented as a distinct management
surface, not enabled implicitly by `shunter.Run`.

## Package Boundaries

The implemented package split is:

- `protocolclient`: owns WebSocket dialing, subprotocol negotiation, bearer
  token presentation, request IDs, bounded waits for responses, protocol
  message encode/decode, and connection shutdown.
- `cmd/shunter`: owns flags, environment fallback, terminal output, exit
  status, file reading, and command help.
- `contractworkflow`: owns loading and validating a `ModuleContract` from JSON
  for CLI use, plus contract-aware JSON argument conversion.
- contract argument encoding helpers: own JSON object to `types.ProductValue`
  conversion from exported product schemas, then delegate BSATN byte encoding
  to existing runtime encoding code.

`protocolclient` stays focused on typed transport. Operator policy,
interactive confirmation, local contract discovery, and command output remain
under `cmd/shunter` or contract workflow helpers.

The existing `protocol` package remains the shared wire-codec and server
transport package. The client package reuses exported message types and
`EncodeClientMessage` / `DecodeServerMessage` rather than duplicating frame
formats. Server admission, subscription fanout, and runtime lifecycle stay out
of the client package.

## Timeout Behavior

Running-app commands use a bounded end-to-end deadline, not separate unbounded
dial, write, and read waits.

- `--timeout` defaults to 10 seconds.
- The command derives a context with deadline before dialing.
- The same deadline applies to WebSocket dial, token handshake, request write,
  response wait, and close.
- Timeout returns a non-zero exit status and includes the target URL, command
  kind, and reducer, procedure, or query name in the error.
- Reducer calls are not retried automatically. If a query retry is added later,
  it must be explicit and limited to transport failures before the request is
  accepted.

The client package surfaces timeout errors distinctly enough for CLI tests to
assert them without parsing human text.

## Auth Requirements

Running-app commands require an explicit credential flag, environment variable,
or the development-only `--allow-dev-anonymous` flag. They do not silently rely
on dev anonymous auth for operator writes.

- `--token <jwt>` for direct bearer token input.
- `--token-file <path>` for local automation.
- `SHUNTER_TOKEN` as the environment fallback.
- `--allow-dev-anonymous` for explicit tokenless development connections only.

When multiple sources are supplied, command-line token sources win over the
environment, and any resolved token wins over `--allow-dev-anonymous`.
Missing-token errors state that a token is required for running-app admin
commands.

`cmd/shunter` resolves the credential source and passes only the selected token
to the client package. The client package attaches the token using
the same protocol authentication path expected by normal clients and does not
read environment variables itself.

## Encoding Rules

JSON is the ergonomic CLI input format, but the wire payload must still follow
the contract's reducer, procedure, or query parameter schema.

- Reject unknown reducer, procedure, or query names before connecting.
- Reject missing contract product schemas for JSON argument mode.
- Encode JSON objects to BSATN product rows by declared column name.
- Support `--args`, `--args-file`, and `--args-hex`; positional JSON arguments
  are also accepted.
- Decode rows through contract row schemas when present.
- Emit text by default and JSON with `--format json`.

The CLI does not infer reducer or procedure argument formats when the contract
does not declare them. In that case, operators must use raw bytes mode or the
app should export product schemas.

Generated TypeScript already provides the user-facing model for contract-aware
encoding: helpers know the reducer, procedure, or declared-read schema, encode
strongly typed arguments, and decode rows through contract metadata. The Go CLI
uses the same contract semantics without depending on generated TypeScript
artifacts at runtime.

For the CLI, Go helpers convert JSON values to `types.Value` /
`types.ProductValue` using exported product schemas and then delegate binary
encoding to `bsatn` or the existing protocol row encoders. This keeps the CLI
independent of frontend builds while preserving the same field names, required
fields, type checks, and row decoding rules that generated clients use.

Avoid CLI-specific BSATN writers. If a needed primitive cannot be represented
through current `types.Value` or `bsatn` helpers, add the missing shared helper
with tests rather than open-coding binary layouts under `cmd/shunter`.

## Safety Defaults

Reducer calls are writes. The `call` command is conservative by default:

- Require an explicit reducer name and payload.
- Print the target URL, module name, and reducer before sending in text mode.
- Return non-zero for reducer errors, rejected auth, protocol errors, malformed
  responses, stale contract mismatches, or timeouts.
- Use a bounded default timeout.
- Never retry reducer calls automatically.

Procedures may perform external work and can call reducers. `procedure` must
use the same auth and timeout rules as `call`. Declared queries are reads, but
they can still expose private data. `query` uses the same auth and timeout
rules as the write-oriented commands.

## Error Contract

Running-app commands use the same broad exit-code shape as the existing CLI:

- Exit `0` for successful calls, procedures, or queries.
- Exit `1` for runtime failures after flags and local inputs are valid,
  including auth rejection, connection failure, timeout, protocol errors,
  reducer errors, procedure errors, query errors, malformed server responses,
  stale contract mismatches, and response decoding failures.
- `health --url` prints the structured `/healthz` payload even when the
  runtime reports `failed` through HTTP 503, then exits `1`.
- Exit `2` for local command misuse, including missing required flags, invalid
  flag values, malformed JSON input, unknown reducer, procedure, or query
  names, missing token sources, and schema-less JSON argument mode.

Text output can stay terse, but JSON output is stable enough for operator
automation. Failed JSON output includes:

- `status`: `"error"`.
- `scope`: `"running_app"`.
- `command`: `"call"`, `"procedure"`, or `"query"`.
- `target_url`: the requested app URL.
- `surface`: the reducer, procedure, or query name when known.
- `error_code`: a stable snake_case classifier such as `missing_token`,
  `unknown_surface`, `timeout`, `auth_rejected`, `protocol_error`,
  `reducer_error`, `procedure_error`, `query_error`, or `decode_error`.
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
objects. If deeper diagnostics are needed, add an explicit
verbose or trace mode that redacts credentials before printing.

## Non-Goals

These remain outside the running-app admin CLI surface:

- SQL DML or arbitrary SQL mutation.
- Dynamic module publish, update, or loading.
- Generic HTTP management endpoints.
- Stored admin profiles or credential keychains.
- App-owned custom reducer or procedure argument codecs.
- Multi-module host discovery.

## Implemented Coverage

- Contract-driven JSON-to-product encoding tests live in `contractworkflow`.
- `protocolclient` covers explicit token handling, anonymous opt-in, timeout
  classification, reducer calls, procedure calls, and declared-query responses.
- `cmd/shunter` tests cover token source precedence, development anonymous
  opt-in, missing tokens, unknown names, malformed JSON, argument source
  exclusivity, raw hex args, JSON output, file-backed args, runtime reducer and
  procedure/query errors, malformed protocol responses, and query row decoding.
- The hosted-chat gate starts a real example server, runs one reducer call, one
  procedure call, and one declared query.

## Maintenance Test Strategy

When changing this surface, keep the coverage layered:

- Unit-test JSON-to-product conversion with the same schema shapes exported by
  hosted-chat: successful object input, missing required fields, unknown
  fields, type mismatches, null handling, arrays or objects where scalars are
  required, and deterministic column ordering.
- Unit-test `protocolclient` with `httptest` and the real WebSocket protocol
  handler where possible. Cover missing or rejected tokens, subprotocol
  negotiation failure, malformed server frames, reducer failures, procedure
  failures, declared query responses, context cancellation, and timeouts.
- Command tests should exercise flag precedence, `SHUNTER_TOKEN` fallback,
  `--token-file`, missing token errors, schema-less JSON rejection, raw bytes
  mode, reducer/procedure/query failures, text output, JSON output, and exit
  status.
- Hosted-chat gate coverage should start a real example server on an ephemeral
  address, export the matching contract, run one reducer call, one procedure
  call, and one declared query, then shut the server down cleanly.

Keep tests asserting structured errors or JSON fields whenever practical. Human
text can stay terse, but it should not be the only contract for automation.
