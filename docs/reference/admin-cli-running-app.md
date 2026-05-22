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
