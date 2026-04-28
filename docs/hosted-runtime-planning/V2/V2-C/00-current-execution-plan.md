# Hosted Runtime V2-C Current Execution Plan

Status: complete

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
1. Complete: reconfirmed migration metadata, contractdiff, and store/recovery
   boundaries.
2. Complete: added failing tests for migration plan reports.
3. Complete: implemented deterministic migration planning over contract
   snapshots.
4. Complete: added contract-level validation hooks that do not mutate stored
   state.
5. Complete: formatted and validated V2-C gates.

Live proof:
- `contractdiff.Plan` and `PlanJSON` build deterministic migration plans from
  previous/current `ModuleContract` values or canonical JSON.
- `MigrationPlan` exposes summary counts, plan entries, policy warnings,
  severity, action, attached migration metadata, and author classifications.
- `contractdiff.Compare` now reports index definition and migration-metadata
  changes in addition to prior additive, breaking, and metadata-only changes.
- `contractworkflow.PlanFiles` and `FormatPlan` render deterministic text and
  newline-terminated JSON from existing contract files.
- `cmd/shunter contract plan` exposes the workflow over existing JSON files.
- no migration execution, stored-state mutation, startup-blocking enforcement,
  rollback, backup, restore, or runtime shape change was added.

Validation passed:
- `rtk go fmt ./contractdiff ./contractworkflow ./cmd/shunter`
- `rtk go test ./contractdiff ./contractworkflow ./cmd/shunter -count=1`
- `rtk go test ./... -run 'Test.*(Migration|ContractDiff|Policy|Plan)' -count=1`
- `rtk go vet ./contractdiff ./contractworkflow ./cmd/shunter`

Scope boundaries:
- In scope: plan/report data model, deterministic JSON/text output,
  metadata-vs-diff mismatch reporting, dry-run validation.
- Out of scope: executing migration functions, rewriting stored state,
  startup-blocking migration enforcement, rollback orchestration,
  backup/restore implementation.

Historical sequencing note: later hosted-runtime slices have since landed. Do
not treat this completed V2-C plan as a live handoff; use
`HOSTED_RUNTIME_PLANNING_HANDOFF.md` for current hosted-runtime status.
