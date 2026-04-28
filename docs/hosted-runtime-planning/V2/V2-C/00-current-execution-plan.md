# Hosted Runtime V2-C Current Execution Plan

Goal: add migration planning and validation on top of v1.5 contracts and diffs
without executing migrations or blocking normal runtime startup.

V2-C target:
- combine previous/current `ModuleContract` snapshots with `contractdiff`
  reports
- produce a deterministic migration plan/report artifact
- identify additive, breaking, metadata-only, data-rewrite-needed, and
  manual-review-needed changes
- preserve explicit operator review as the default safety posture

Task sequence:
1. Reconfirm migration metadata, contractdiff, and store/recovery boundaries.
2. Add failing tests for migration plan reports.
3. Implement deterministic migration planning over contract snapshots.
4. Add optional validation hooks that do not mutate stored state.
5. Format and validate V2-C gates.

Scope boundaries:
- In scope: plan/report data model, deterministic JSON/text output,
  metadata-vs-diff mismatch reporting, dry-run validation.
- Out of scope: executing migration functions, rewriting stored state,
  startup-blocking migration enforcement, rollback orchestration,
  backup/restore implementation.

Immediate next V2 slice after V2-C: V2-D declared read and SQL protocol
convergence.
