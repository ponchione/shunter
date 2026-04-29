# V2-D Task 04: Update Contract And Codegen Metadata

Parent plan: `docs/features/V2/V2-D/00-current-execution-plan.md`

Objective: expose any stable declared-read execution metadata through existing
tooling.

Status: complete.

Update only if Task 03 adds durable metadata:
- `ModuleContract`
- canonical JSON normalization
- TypeScript generator output
- contractdiff classification
- migration policy warnings if read execution metadata affects compatibility

Preserve if declarations remain metadata-only:
- existing contract shape unless a clarification field is required
- generated query/view helper semantics
- raw-byte reducer call helpers
- lifecycle reducer separation

Compatibility checks:
- adding execution metadata should be classified as additive where possible
- removing or changing a declared read execution target should be visible to
  contract diff tooling
- generated clients must not call a server feature that the runtime cannot
  serve

Implemented updates:
- `QueryDescription.SQL` and `ViewDescription.SQL` export declaration SQL in
  contract JSON when present.
- TypeScript codegen emits `querySQL` and `viewSQL` maps and executable helpers
  only for SQL-backed declarations.
- contractdiff classifies SQL metadata additions as additive and removals or
  changes as breaking.
