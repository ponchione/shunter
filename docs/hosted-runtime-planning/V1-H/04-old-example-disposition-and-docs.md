# V1-H Task 04: Demote or replace the old manual example and update docs

Parent plan: `docs/hosted-runtime-planning/V1-H/2026-04-23_214356-hosted-runtime-v1h-hello-world-replacement-v1-proof-implplan.md`

Objective: make the hosted-runtime example the normal discoverable path.

Files likely to touch:
- old example path under `cmd/`
- `README.md`
- `docs/hosted-runtime-bootstrap.md`

Implementation requirements:
- either move the old manual example to an advanced/internal reference path, rewrite the old path as the hosted example, or delete it after preserving coverage
- update docs so the hosted example is the first path a new user sees
- keep docs concise; do not build a full tutorial site
