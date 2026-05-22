# Next Agent Prompt: Hosted Backend Continuation

You are working in `/home/ponchione/source/shunter`.

The product direction is hosted Shunter in the static-Go-app sense: Shunter
should feel like the application's backend/database runtime to frontend and app
developers, while the module is still linked into a normal Go binary. Do not
pursue managed cloud hosting, dynamic module upload, broad SQL compatibility,
or reference-runtime compatibility in this slice.

## Start By Reading

1. `RTK.md`
2. `AGENTS.md`
3. `README.md`
4. `docs/getting-started.md`
5. `docs/how-to/host-shunter-backend.md`
6. `docs/how-to/contract-export-and-codegen.md`
7. `docs/operations.md`
8. `docs/reference/admin-cli-running-app.md`

Also inspect the current working tree before editing. Known working-doc changes
may exist and should not be swept into your work unless you intentionally own
them:

- `working-docs/README.md`
- `working-docs/hosted-backend-roadmap.md`

Do not read broad numbered specs unless the code/docs you touch require them.
Use live code and tests as the source of truth.

## Current State

Recent hosted-backend work includes:

- `8c108f4 Add hosted app runtime path`
  - Added `shunter.Run`, `ConfigFromEnv`, `ConfigFromEnvE`, and the hosted app
    runtime entrypoint shape.
- `6232c8e Add contract describe CLI`
  - Added `shunter describe --contract <file> [--format text|json]`.
- `ccceea1 Improve contract describe CLI`
  - Added `--section all|tables|reducers|queries|views|visibility`.
  - Added JSON `counts`.
  - Added hosted-chat release gate assertions for describe JSON.
- `9060578 Add contract health CLI`
  - Added `shunter health --contract <file> [--format text|json]`.
  - This is explicitly local contract-artifact validation only; it does not
    check a running server.
- `a728ad4 Document running app CLI shape`
  - Added the design note for future `shunter call` and `shunter query`.
  - The note requires explicit auth/token handling and prefers the existing
    WebSocket protocol before any new HTTP management endpoints.
- `43bcefc Add contract validate CLI`
  - Added `shunter contract validate --contract <file> [--format text|json]`.
  - Wired hosted-chat gate coverage for validation JSON.

The canonical hosted example is `examples/hosted-chat`. It should remain the
release gate for small hosted-backend CLI and TypeScript workflow slices.

## Mission

Keep moving Shunter toward the hosted-backend product boundary while staying in
a small, finishable slice with tests, docs, and a commit.

Prioritize developer workflow surfaces that are honest about what they do:

- Local contract commands operate only on `ModuleContract` JSON.
- Offline data commands operate only on stopped/offline `DataDir` directories.
- Running-app commands require an explicit protocol client, auth/token design,
  timeout behavior, and live-server tests before implementation.

## Good Next Slices

Choose one small slice.

### Option 1: Contract Assertions Command

Add a local command for release gates:

```bash
shunter contract assert --contract shunter.contract.json \
  --module hosted_chat \
  --tables 3 \
  --reducers 1 \
  --queries 1 \
  --views 1
```

Scope:

- Local contract JSON only.
- Validate the contract before checking assertions.
- Support count assertions for tables, columns, indexes, reducers, queries,
  views, and visibility filters.
- Support optional module name and schema version assertions.
- Text output can be terse; JSON output should be machine-readable.
- Add hosted-chat gate usage to replace or complement shell `grep` assertions.

Avoid a large assertion language or JSONPath-style matcher.

### Option 2: Protocol-Client Spike As Docs Only

Extend `docs/reference/admin-cli-running-app.md` with an implementation sketch
for a future protocol client package.

Scope:

- Define package boundaries, timeout behavior, token handling, and test
  strategy.
- Identify how to reuse generated/client encoding versus adding CLI-specific
  JSON-to-BSATN encoding.
- Keep it design-only. Do not add running-app commands yet.

### Option 3: Improve Contract Validate

If `contract assert` is too large, improve `contract validate`:

- Add `--summary` or `--section` if it materially helps release gates.
- Add stronger JSON fields for validation metadata.
- Add hosted-chat gate coverage for those exact fields.

Keep this smaller than a policy engine.

## Guardrails

- Use `rtk` for all shell commands.
- Before unfamiliar Go edits, inspect with Go tooling first:
  - `rtk go doc <pkg>`
  - `rtk go list -json <pkg>`
  - `rtk go list -json ./...`
- Keep `reference/` read-only and research-only.
- Do not add dynamic module loading.
- Do not add HTTP management endpoints unless auth, enablement, docs, and tests
  are explicitly scoped.
- Do not implement broad SQL, SQL DML, PGWire, JWKS/OIDC, event tables,
  procedures, or dynamic publish/update in this slice.
- Preserve existing user/uncommitted changes unless you intentionally own them.

## Verification Expectations

Run targeted checks first, then the full required checks before finishing:

```bash
rtk go fmt ./...
rtk go test ./...
rtk go vet ./...
rtk go tool staticcheck ./...
```

If you touch the hosted-chat gate, generated TypeScript, or frontend workflow,
also run:

```bash
rtk ./scripts/hosted-chat-gate.sh
```

If you touch TypeScript client code, run this from `typescript/client`:

```bash
rtk npm test
```

If you touch `examples/hosted-chat/frontend`, run this from that directory:

```bash
rtk npm run typecheck
```

The hosted-chat gate currently builds `./examples/hosted-chat/cmd/hosted-chat`
and may leave a root `hosted-chat` binary. Remove that generated artifact before
committing.

## Completion Criteria

Before reporting done:

- Implementation or doc slice is committed.
- Docs are updated for any CLI or workflow change.
- Hosted-chat gate passes if touched.
- Full Go verification passes.
- TypeScript checks pass if relevant.
- Final report includes:
  - commit hash
  - files changed
  - command/API or doc surface added
  - verification results
  - remaining gaps for the following agent
