# Working Docs

This directory holds implementation planning, baseline specs, audits, design
notes, benchmark baselines, and future-work trackers that should not sit beside
the app-author documentation in `docs/`.

## Contents

- `deferred-functionality-backlog.md` - intentionally deferred product,
  runtime, and test-surface work that is not active for the current slice.
- `actionable/` - focused implementation slices promoted from the deferred
  backlog for near-term work.
- `recommendations/` - proposed continued-development slices awaiting explicit
  promotion into `actionable/` or another owned implementation plan.
- `dependency-considerations.md` - dependency policy and dependency decisions.
- `hosted-backend-roadmap.md` - current hosted-backend product direction for
  Shunter as a Go-first backend/database runtime.
- `nesl-operational-use-thought-experiment.md` - public-information thought
  experiment for Shunter as a cross-system operational coordination layer at a
  vertically integrated construction materials company.
- `release-qualification.md` - durable release qualification evidence ledger
  and current minimum command set.
- `release-evidence/` - historical command logs referenced by release
  qualification records.
- `shunter-design-decisions.md` - consolidated implementation-facing design
  decisions.
- `tech-debt.md` - non-blocking future work retired from stale release
  roadmaps.
- `specs/` - numbered baseline subsystem contracts and scope notes.

Use live code and tests before working-doc prose when they disagree.
