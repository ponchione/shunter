# Story 6.3: `shunter-codegen` Tool Contract

**Epic:** [Epic 6 — Schema Export & Codegen Interface](EPIC.md)
**Spec ref:** SPEC-006 §12, §12.2
**Depends on:** Story 6.4
**Blocks:** Nothing (terminal story)

---

## Summary

Define the v1 `shunter-codegen` CLI contract that consumes exported `SchemaExport` JSON and generates client artifacts at build time. This is a tooling story, not a runtime-engine story.

## Deliverables

- `cmd/shunter-codegen` CLI entrypoint with the v1 contract:
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
  - Reducer names surfaced as callable identifiers, but reducer argument/return typing remains out of scope in v1

## Acceptance Criteria

- [ ] `shunter-codegen --lang typescript --schema schema.json --out ./generated/` accepts a valid `SchemaExport` JSON file and exits successfully
- [ ] Missing `--schema` or `--out` flag → non-zero exit with descriptive usage error
- [ ] `--lang go` (or any non-`typescript` value) → non-zero exit with unsupported-language error
- [ ] Unreadable or malformed `schema.json` → non-zero exit with descriptive parse/read error
- [ ] Generated output includes table row/type definitions and typed subscription helpers for all exported tables
- [ ] Generated output does not claim typed reducer argument/return schemas in v1

## Design Notes

- The application is still responsible for producing `schema.json` via `Engine.ExportSchema()` (for example through a `--export-schema` flag or `go generate`). This story starts at the point where the JSON file already exists, matching SPEC-006 §12.2.
- Keeping the tool contract separate from `Engine.ExportSchema()` prevents runtime engine concerns from leaking into build-time codegen implementation.
- Limiting v1 to `typescript` keeps the contract small while still leaving room for future generators (`go`, `csharp`, etc.) under the same CLI surface.
