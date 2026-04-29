# V1.5-E Task 03: Implement Descriptive Migration Metadata

Parent plan: `docs/features/V1.5/V1.5-E/00-current-execution-plan.md`

Objective: add exported migration metadata without implying Shunter executes
migrations in V1.5.

Implementation target:
- add module-level migration metadata
- add optional declaration-level metadata on schema/table/query/view declarations
- export metadata through the canonical contract
- keep metadata non-blocking at runtime
- make missing metadata serializes deterministically

Compatibility levels:
- `compatible`
- `breaking`
- `unknown`

Optional classifications:
- `additive`
- `deprecated`
- `data-rewrite-needed`
- `manual-review-needed`

Metadata fields may include:
- module version
- schema/contract version
- previous-version reference
- compatibility level
- detailed classifications
- human migration notes

Non-goals:
- ordered migration functions
- stored-state rewrites
- rollback/forward execution
- deployment migration orchestration

