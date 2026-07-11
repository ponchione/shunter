# Working Docs

This directory holds implementation planning, baseline specs, audits, design
notes, benchmark baselines, historical release material, and future-work
trackers that should not sit beside the app-author documentation in `docs/`.
The presence or numbering of a document does not establish the next task.

## Contents

- `deferred-functionality-backlog.md` - intentionally deferred product,
  runtime, and test-surface work that is not active for the current slice.
- `actionable/` - only currently authorized, focused implementation slices;
  historical closed slices may remain clearly marked as such.
- `recommendations/` - optional, trigger-driven proposals awaiting explicit
  promotion into `actionable/` or another owned implementation plan.
- `dependency-considerations.md` - dependency policy and dependency decisions.
- `hosted-backend-roadmap.md` - current active-development direction for
  Shunter as an experimental Go-first backend/database runtime.
- `nesl-operational-use-thought-experiment.md` - public-information thought
  experiment for Shunter as a cross-system operational coordination layer at a
  vertically integrated construction materials company.
- `release-qualification.md` - historical and on-demand qualification ledger
  used only for explicitly authorized release work.
- `release-evidence/` - historical release material and evidence created on
  demand for intentional release work.
- `shunter-design-decisions.md` - consolidated implementation-facing design
  decisions.
- `tech-debt.md` - non-blocking future work retired from stale release
  roadmaps.
- `specs/` - numbered baseline subsystem contracts and scope notes.

Use live code and tests before working-doc prose when they disagree.
Neither `recommendations/` nor `actionable/` selects work by itself; a concrete
user-authorized goal must establish the active slice.
