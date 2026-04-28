# V1.5-C Task 04: Keep Secondary Artifacts Separate

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-C/00-current-execution-plan.md`

Objective: add secondary generator outputs only when they are small, useful, and
clearly separate from the first frontend binding target.

Allowed secondary targets:
- typed internal clients for tests
- typed internal clients for tools/admin scripts
- downstream generator metadata

Rules:
- generated client bindings must still come from the canonical contract
- secondary artifacts must not become a second source of truth
- test/admin helper generation should live behind explicit package or CLI
  boundaries
- do not add broad framework scaffolding
- do not add all-language SDK generation

If secondary targets make the slice too large, defer them and complete V1.5-C
with the frontend/client binding target only.

