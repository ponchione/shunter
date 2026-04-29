# V1.5-E Task 04: Add Contract-Diff Tooling

Parent plan: `docs/features/V1.5/V1.5-E/00-current-execution-plan.md`

Objective: compare a current canonical contract to a previous
`shunter.contract.json` and infer schema/module surface changes.

Implementation target:
- read old and new canonical contract JSON
- compare module/schema/contract versions
- compare tables and columns
- compare reducers
- compare declared queries
- compare declared views
- compare relevant permission/read-model metadata when present
- report inferred changes in a stable, review-friendly format

Tests to add:
- additive table/column/query/view changes are detected
- removed or type-changed surfaces are detected as breaking or review-worthy
- metadata-only changes are reported separately from data/client surface changes
- identical contracts produce no changes
- malformed contract JSON fails clearly
- output ordering is deterministic

Tooling guidance:
- keep the diff engine usable as a library for tests and CI
- add CLI wiring only if needed for the V1.5-E workflow
- do not mutate contract files during diffing

