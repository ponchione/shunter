# Story 6.3: `shunter-codegen` Tool Contract

**Epic:** [Epic 6 — Schema Export & Codegen Interface](EPIC.md)
**Spec ref:** SPEC-006 §12, §12.2
**Depends on:** Story 6.4
**Blocks:** Nothing (terminal story)

---

## Summary

Define the v1 `shunter-codegen` CLI contract that consumes exported `SchemaExport` JSON and generates client artifacts at build time. This is a tooling story, not a runtime-engine story, and in the current docs-first repo it is a forward contract rather than an already-landed command package.

## Deliverables

- Future implementation target: `cmd/shunter-codegen` CLI entrypoint with the v1 contract below. The package path is intentionally forward-declared here; this story does not claim the command already exists in the current repo.
  ```text
  shunter-codegen --lang typescript --schema schema.json --out ./generated/
  ```

- Flag contract:
  - `--lang` is required; v1 accepts `typescript` only
  - `--schema` is required; it must point to a readable JSON file containing `SchemaExport`
  - `--out` is required; it is the target output directory for generated artifacts

- Tool behavior:
  1. Parse flags and reject missing/unknown values with a non-zero exit status
  2. Read `SchemaExport` JSON from `--schema`
  3. Validate that the export includes version/tables/reducers in the expected shape
  4. Generate language-specific client artifacts into `--out`
  5. Exit non-zero with a descriptive error if the schema file is unreadable or invalid

- v1 generation scope:
  - TypeScript row/type definitions for all exported tables
  - Typed subscription helper surface for exported tables
  - Reducer names surfaced as callable identifiers, but reducer argument/return typing remains out of scope in v1; generated reducer wrappers accept raw BSATN bytes (`Uint8Array`) and return raw bytes / promise-wrapped raw bytes depending on transport style

- Fixture contract for acceptance tests:
  - Minimal schema input: one user table plus one normal reducer and one lifecycle reducer
  - Expected generated TypeScript includes:
    - a row/type export for the table
    - a typed subscription helper for that table
    - a reducer wrapper whose argument surface is `Uint8Array` rather than a typed object
    - lifecycle reducers excluded from the normal callable reducer helper set

## Acceptance Criteria

- [ ] A future `shunter-codegen --lang typescript --schema schema.json --out ./generated/` implementation accepts a valid `SchemaExport` JSON file and exits successfully
- [ ] Missing `--schema` or `--out` flag → non-zero exit with descriptive usage error
- [ ] `--lang go` (or any non-`typescript` value) → non-zero exit with unsupported-language error
- [ ] Unreadable or malformed `schema.json` → non-zero exit with descriptive parse/read error
- [ ] Generated output includes table row/type definitions and typed subscription helpers for all exported tables
- [ ] Generated output exposes reducer wrappers as raw-byte (`Uint8Array`) call surfaces rather than typed reducer-argument objects
- [ ] Lifecycle reducers do not appear in the normal callable reducer helper set
- [ ] Generated output does not claim typed reducer argument/return schemas in v1

## Design Notes

- The application is still responsible for producing `schema.json` via `Engine.ExportSchema()` (for example through a `--export-schema` flag or `go generate`). This story starts at the point where the JSON file already exists, matching SPEC-006 §12.2.
- Because reducer argument schemas are absent from `ReducerExport`, the generated reducer helper surface must stay explicitly byte-oriented in v1. Pretending reducer inputs are typed would mis-spec the export contract.
- Keeping the tool contract separate from `Engine.ExportSchema()` prevents runtime engine concerns from leaking into build-time codegen implementation.
- Limiting v1 to `typescript` keeps the contract small while still leaving room for future generators (`go`, `csharp`, etc.) under the same CLI surface.
