# V1.5-E Task 02: Add Failing Migration Metadata Tests

Parent plan: `docs/features/V1.5/V1.5-E/00-current-execution-plan.md`

Objective: pin descriptive migration metadata before diff tooling is added.

Likely files:
- add root package tests for module-level metadata
- add declaration metadata tests for schema/query/view surfaces
- add contract export tests

Tests to add:
- module-level migration metadata declares module version
- module-level migration metadata declares schema/contract version
- module-level migration metadata declares previous-version reference when set
- compatibility level accepts `compatible`, `breaking`, and `unknown`
- detailed classifications can include `additive`, `deprecated`,
  `data-rewrite-needed`, and `manual-review-needed`
- declaration-level migration metadata can attach to tables, queries, and views
- migration metadata appears in canonical contract JSON
- missing migration metadata does not block runtime build/start

Test boundaries:
- do not execute migrations
- do not require runtime startup enforcement
- do not implement contract diffs in this task

