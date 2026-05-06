# v1 Contract Freeze

Status: open, initial support matrix added
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

Shunter already exposes a broad root API through package `github.com/ponchione/shunter`:

- `Module`, `Config`, `Runtime`, `Build`, `NewHost`
- reducer, query, view, schema, migration, backup, restore, and contract helpers
- HTTP/WebSocket serving helpers

Lower-level packages also expose useful APIs, including `schema`, `store`,
`subscription`, `protocol`, `query/sql`, `commitlog`, `contractdiff`,
`contractworkflow`, and `codegen`. Some of these are probably stable enough to
document; others should remain implementation-facing.

The docs and live code are close but not fully aligned. One concrete example:
live view projection support must be stated consistently across the app-author
guide, declared-read validation, protocol behavior, generated contracts, and
tests.

Current slice note: the initial support matrix records declared live-view
column projection support and narrow single-table `COUNT` aggregate support.
The app-author guide has been updated to match current declared-read and
subscription behavior.

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

- Add or update a compact v1 compatibility document under `docs/`.
- Audit exported identifiers in root `shunter` and classify them as stable,
  preview, deprecated, or internal-by-convention.
- Add package comments where support level is ambiguous.
- Add contract/protocol compatibility tests for every stable payload shape.
- Add golden contract fixtures for representative modules.
- Ensure `contractdiff` treats stable-field changes consistently with the v1
  compatibility policy.
- Ensure generated TypeScript output has a documented stability boundary.
- Make docs, package comments, README, and tests agree on live view projection,
  aggregate, order, limit, and offset support.

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
