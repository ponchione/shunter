# Running-App Admin CLI Shape

Status: design note, not implemented
Scope: future `shunter call` and `shunter query` commands against a running
Shunter app.

Shunter's generic CLI currently operates on local contract JSON files and
offline `DataDir` directories. It does not dynamically load app modules and it
does not provide running-app reducer or query commands yet.

Future running-app admin commands must be explicit about transport, auth, input
encoding, and operator risk before implementation.

## Intended Commands

The likely first commands are:

```bash
shunter call --url http://127.0.0.1:3000 --contract shunter.contract.json --token "$TOKEN" send_message '{"author":"Ada","body":"hello"}'
shunter query --url http://127.0.0.1:3000 --contract shunter.contract.json --token "$TOKEN" recent_messages '{}'
```

These commands would target declared app surfaces:

- `call` invokes a named reducer exported in the contract.
- `query` invokes a named declared query exported in the contract.
- Declared views remain subscription-oriented and should not be folded into
  `query` unless a future one-shot view read is deliberately added.

The contract path stays required even when the app can expose its own contract.
The CLI should use the local contract to validate names, permissions metadata,
parameter schemas, and generated-style argument encoding before sending any
request.

## Transport Decision

Use the existing Shunter WebSocket protocol first unless a separate HTTP admin
API is deliberately designed.

Reasons:

- The protocol already has reducer-call and declared-query message families.
- Strict auth already protects protocol connections.
- Adding generic HTTP management endpoints would require separate enablement,
  auth, request-size, CSRF, logging, and deployment documentation.

The first implementation should avoid new server endpoints. If HTTP admin
routes are later added, they must be opt-in and documented as a distinct
management surface, not enabled implicitly by `shunter.Run`.

## Package Boundaries

The first implementation should add a small Go protocol client package before
adding CLI commands. Keep it separate from the server-side protocol lifecycle
code so command behavior can be tested without starting the full CLI.

Suggested split:

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

Running-app commands need a bounded end-to-end deadline, not separate unbounded
dial, write, and read waits.

Minimum behavior:

- Add `--timeout` with a conservative default.
- Derive a context with deadline before dialing.
- Apply the same deadline to WebSocket dial, token handshake, request write,
  response wait, and close.
- Return a non-zero exit status on timeout and include the target URL, command
  kind, and reducer or query name in the error.
- Do not retry reducer calls automatically. If a query retry is added later, it
  must be explicit and limited to transport failures before the request is
  accepted.

The client package should surface timeout errors distinctly enough for CLI tests
to assert them without parsing human text.

## Auth Requirements

Running-app commands must require an explicit credential flag or environment
variable. They must not silently rely on dev anonymous auth for operator writes.

Minimum shape:

- `--token <jwt>` for direct bearer token input.
- `--token-file <path>` for local automation.
- `SHUNTER_TOKEN` as the environment fallback.

When multiple sources are supplied, command-line flags should win over the
environment. Errors should say that a token is required for running-app admin
commands. Development conveniences can be added later only with an explicit
`--allow-dev-anonymous` flag.

`cmd/shunter` should resolve the credential source and pass only the selected
token to the client package. The client package should attach the token using
the same protocol authentication path expected by normal clients and should not
read environment variables itself.

## Encoding Rules

JSON is the ergonomic CLI input format, but the wire payload must still follow
the contract's reducer or query parameter schema.

Implementation rules:

- Reject unknown reducer or query names before connecting.
- Reject missing contract product schemas for JSON argument mode.
- Encode JSON objects to BSATN product rows by declared column name.
- Preserve raw bytes mode as an explicit escape hatch, for example
  `--args-hex` or `--args-file`.
- Decode rows through contract row schemas when present.
- Emit JSON by default for machine readability, with text output as a review
  convenience.

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

Future running-app commands should use the same broad exit-code shape as the
existing CLI:

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

## Implementation Checklist

Before implementing `call` or `query`:

1. Add contract-driven JSON-to-product encoding tests.
2. Add a protocol client path with strict timeout and auth handling.
3. Add command tests for missing tokens, unknown names, schema-less JSON mode,
   malformed JSON, reducer errors, and query row decoding.
4. Add hosted-chat gate coverage against a real running example server.
5. Document the command as a running-app client, separate from local
   contract-only commands such as `describe` and `health --contract`.

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
