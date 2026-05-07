# v1 Contract Freeze

Status: open, v1 support decisions settled; coverage and policy audit remain
Owner: unassigned
Scope: supported Shunter v1 API, protocol, contract JSON, generated client
shape, and read/query behavior.

## Goal

Define the exact compatibility contract that a `v1.0.0` release promises to
applications, generated clients, operators, and protocol clients.

The desired outcome is a small, explicit support matrix. A user should be able
to answer:

- Which Go APIs are stable?
- Which protocol messages and payload shapes are stable?
- Which contract JSON fields are stable?
- Which SQL/query/view shapes are supported?
- Which lower-level packages are implementation details?
- What counts as a breaking change after v1?

Initial matrix: [`docs/v1-compatibility.md`](../v1-compatibility.md)

## Current State

Shunter already exposes a broad root API through package
`github.com/ponchione/shunter`:

- `Module`, `Config`, `Runtime`, `Build`, `NewHost`
- reducer, query, view, schema, migration, backup, restore, and contract helpers
- HTTP/WebSocket serving helpers

Lower-level packages also expose useful APIs, including `schema`, `store`,
`subscription`, `protocol`, `query/sql`, `commitlog`, `contractdiff`,
`contractworkflow`, and `codegen`. Some of these are probably stable enough to
document; others should remain implementation-facing.

The docs and live code are closer than the original roadmap assumed. Past drift
around live-view projection, aggregate, order, limit, and offset support has
been reconciled in the compatibility matrix and app-author guide. The remaining
contract work is a final audit: every stable promise needs an explicit fixture,
test, or documented preview boundary.

Current slice note: the initial support matrix records the root API support
levels, stable v1 protocol token, stable `ModuleContract` JSON fields,
TypeScript codegen boundary, read-surface matrix, multi-module host preview
status, and the separation between app module metadata and Shunter
runtime/tool version metadata.

Current v1 decisions now settled in `docs/v1-compatibility.md`: `Host` remains
preview/advanced for v1, and no importable lower-level package beyond the
stable subsets listed in the matrix receives a normal Go compatibility promise.
Generated TypeScript identifier normalization and collision suffixes are also
stable v1 codegen output.

Code reality as of this roadmap cleanup:

- `docs/v1-compatibility.md` is the current compatibility matrix and is linked
  from `docs/README.md`.
- Protocol, contract, and TypeScript compatibility tests and a representative
  contract fixture exist, but the matrix still needs a final audit against every
  stable payload shape.
- Generated TypeScript is a stable contract-generation target, not a full SDK.
- Offline operations and migration hooks are preview/advanced in the matrix,
  even though helpers and CLI commands already exist.

## v1 Decisions To Make

1. Decide which packages are part of the v1 public support surface.
2. Decide whether lower-level packages keep normal Go compatibility promises or
   are documented as advanced/internal despite being importable.
3. Decide the v1 protocol version token and the compatibility policy for future
   protocol changes.
4. Decide the stable `ModuleContract` JSON schema, including unknown-field and
   forward-compatibility behavior.
5. Decide the supported read surfaces:
   - one-off raw SQL
   - declared queries
   - raw subscriptions
   - declared live views
   - local `Runtime.Read`
6. Decide the v1 stance on multi-module `Host`: supported v1 feature, preview
   composition helper, or non-goal.
7. Decide how v1 communicates app-owned module metadata versus Shunter runtime
   version metadata.

## Implementation Work

Completed or partially complete:

- Add or update a compact v1 compatibility document under `docs/`.
- Audit exported identifiers in root `shunter` and classify them as stable,
  preview, deprecated, or internal-by-convention.
- Document the protocol token, contract JSON compatibility rules, read surfaces,
  generated TypeScript boundary, and multi-module host preview status.
- Add protocol, contract, and TypeScript compatibility tests and representative
  contract fixtures for the already-pinned surfaces.
- Make app-author docs and the compatibility matrix agree on current declared
  live-view projection, aggregate, order, limit, and offset behavior.
- Clarify package comments for runtime implementation packages that remain
  importable but are not v1 app-facing compatibility surfaces.

Remaining:

- Re-audit every exported root identifier and lower-level package after the
  next implementation slices land.
- Keep package comments aligned with the compatibility matrix when support
  levels change.
- Confirm protocol, contract JSON, and TypeScript golden coverage for every
  stable payload shape in the matrix.
- Ensure `contractdiff` treats stable-field changes consistently with the final
  v1 compatibility policy.
- Keep generated TypeScript identifier normalization compatibility tests current
  when codegen naming changes.

## Verification

Run targeted tests for touched packages first, then broaden:

```bash
rtk go test ./...
rtk go vet ./...
```

If contract/codegen/protocol fixtures are added, include an intentional fixture
change review in the PR or commit message.

## Done Criteria

- A single v1 compatibility doc exists and is linked from `README.md` or
  `docs/README.md`.
- Every public root API is classified.
- Protocol and contract JSON shapes have compatibility tests or golden fixtures.
- Declared read and raw SQL support are documented in one place and cited by
  app-author docs.
- Any known preview surfaces are explicitly labeled and are not required for
  normal v1 app development.

## Non-Goals

- SpacetimeDB wire compatibility.
- Broad SQL database compatibility.
- Cloud/control-plane compatibility guarantees.
- Freezing every importable lower-level package if the v1 thesis does not need
  it.
