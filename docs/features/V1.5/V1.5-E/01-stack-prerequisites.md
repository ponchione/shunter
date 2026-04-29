# V1.5-E Task 01: Reconfirm Migration Metadata Prerequisites

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-E/00-current-execution-plan.md`

Objective: verify migration metadata and diff tooling can build on the
canonical contract without changing runtime startup semantics.

Checks:
- `rtk go doc . Runtime.ExportContract`
- inspect the V1.5-B contract JSON tests
- inspect the V1.5-D metadata attachment patterns

Prerequisite conclusions to record in Task 01:
- canonical contract JSON is the source of truth for diffs
- `shunter.contract.json` is the recommended snapshot path
- migration metadata is descriptive and exported
- runtime startup must not fail solely because migration metadata is missing or
  risky
- tooling/CI may warn or fail based on project policy

