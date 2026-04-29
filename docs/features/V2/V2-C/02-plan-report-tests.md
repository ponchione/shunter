# V2-C Task 02: Add Failing Migration Plan Report Tests

Parent plan: `docs/features/V2/V2-C/00-current-execution-plan.md`

Objective: pin deterministic planning behavior before implementation.

Likely files:
- new migration planning package tests
- JSON fixtures under the chosen package testdata directory
- optional command/workflow tests if V2-B added command helpers

Tests to add:
- additive table/column/query/view changes produce reviewable plan entries
- breaking removals or incompatible type/index changes produce blocking plan
  entries
- metadata-only changes are represented separately from schema/client changes
- author-declared compatibility disagreement with inferred diff is reported
- `manual-review-needed` and `data-rewrite-needed` classifications are surfaced
- missing previous/current metadata produces warnings, not a runtime-startup
  decision
- plan JSON is deterministic and newline-terminated if JSON output is exposed

Test boundaries:
- do not execute user migration functions
- do not open or rewrite a live data directory unless Task 04 explicitly adds a
  read-only preflight hook
- do not change `Runtime.Start`
