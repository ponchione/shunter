# TypeScript SDK Phase 3 Handoff

Date: 2026-05-07

This note is a restart point for the Phase 3 TypeScript SDK runtime work in
`docs/v1-roadmap/03-client-sdk.md` and
`docs/v1-roadmap/typescript-sdk-contract.md`.

## Startup

Start the next session with the normal repo rules:

```bash
rtk git status --short
rtk git log --oneline -12
```

Read `RTK.md` first, then `AGENTS.md`. Use `rtk` for every shell, git, Go, and
npm command. Keep the external canary repo read-only unless a Shunter-side SDK
fixture refresh explicitly requires it.

## Recent Phase 3 Commits

The latest SDK slice commits are:

- `8d1342c` Add generated TypeScript table row decoders
- `4a57947` Apply TypeScript table handle update cache
- `97c91c2` Add opt-in TypeScript reconnect resubscribe
- `194bec4` Document TypeScript SDK schema metadata blockers

Earlier relevant SDK commits in the same run include generated reducer/result
and table-handle foundations such as `2b7f05f`, `b13a579`, and `5d4e6dc`.

## Current State

The runtime and generated bindings now cover these Phase 3 SDK pieces:

- raw reducer/query/view/table request and response plumbing
- reducer result envelopes and generated `callXResult(...)` wrappers
- explicit reducer argument encoder hooks for caller-provided codecs
- raw declared-query result envelopes and decoded declared-query bridge using
  caller-provided table decoders
- raw RowList splitting and raw per-row byte convenience fields
- table subscription decoded row callbacks through `decodeRow`
- generated schema-aware table row decoders using the runtime BSATN product
  decoder
- generated `tableRowDecoders` maps and default generated table subscription
  helpers that use those decoders
- managed subscription handles with server-acknowledged unsubscribe
- managed table handles that refresh initial rows and apply RowList
  insert/delete deltas using raw row bytes as local row identity
- explicit opt-in reconnect with bounded attempts, token-provider refresh per
  attempt, and subscription replay after a fresh identity handshake

Docs were updated in:

- `CHANGELOG.md`
- `docs/v1-roadmap/03-client-sdk.md`
- `docs/v1-roadmap/typescript-sdk-contract.md`
- `docs/v1-roadmap/10-v1-execution-plan.md`
- `typescript/client/README.md`

## Known Blocker

Do not try to generate reducer argument/result codecs or declared-read
projection decoders inside the TypeScript runtime alone. The live exported
contract does not yet contain the needed schema metadata:

- `schema.ReducerExport` currently exports only `Name` and `Lifecycle`.
- `shunter.QueryDescription` and `shunter.ViewDescription` export declaration
  names and SQL, but not projected result row schemas.
- `shunter.ReadModelContractDeclaration` currently exports surface/name/table
  tags, not row shapes.

The next real slice for typed reducer codecs or declared query/view projection
decoders should start by extending `ModuleContract`/contract validation/goldens
with reducer product schemas and declared-read projection schemas.

## Suggested Next Slices

1. Contract metadata slice: export reducer argument/result product schemas and
   declared query/view projection row schemas, with compatibility tests and
   golden coverage.
2. Generated codec slice: use the new contract metadata to generate reducer
   BSATN arg/result codecs while preserving raw `Uint8Array` escape hatches.
3. Declared-read typed rows slice: generate query/view projection row types and
   decoders, then default generated `queryXResult(...)` wrappers where the
   projection shape is known.
4. Canary slice: wire `/home/gernsback/source/opsboard-canary` to the public
   SDK only after the Shunter-side generated output is sufficient.
5. Release qualification slice: pin the tested Shunter commit/tag and add the
   TypeScript SDK commands to the release checklist.

## Verification From This Handoff

The latest broad verification passed:

```bash
rtk npm --prefix typescript/client run test
rtk go test ./codegen
rtk go test ./...
rtk go vet ./...
rtk git diff --check
```

`rtk go test ./...` reported 5554 passed across 18 packages.

## Worktree

Before this handoff note, `rtk git status --short` was clean. No unrelated dirty
files were observed or modified during the final SDK verification.
