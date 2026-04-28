# Hosted Runtime V1.5-E Current Execution Plan

Goal: make schema/module evolution visible and reviewable without executing
migrations.

Task sequence:
1. Reconfirm contract, metadata, and snapshot surfaces.
2. Add failing tests for descriptive migration metadata.
3. Implement module-level and declaration-level migration metadata.
4. Add contract-diff tooling that compares current export to a previous
   `shunter.contract.json`.
5. Add warning/CI-oriented policy checks for missing metadata, risky changes,
   and declared-vs-inferred mismatches.
6. Format and validate V1.5-E gates.

Task progress:
- Task 01 complete: contract, metadata, and snapshot prerequisites were
  reconfirmed against live code.
- Task 02 complete: migration metadata tests cover module-level metadata,
  table/query/view declaration metadata, canonical JSON, and non-blocking
  runtime startup.
- Task 03 complete: descriptive migration metadata is exported through the
  canonical contract without adding migration execution.
- Task 04 complete: `contractdiff` compares canonical contracts and reports
  deterministic additive, breaking, and metadata-only changes.
- Task 05 complete: `contractdiff` policy checks report migration metadata
  warnings and support explicit strict/CI failure mode.
- Task 06 complete: V1.5-E format, test, and vet gates passed.

V1.5-E target:
- descriptive/exported metadata first
- no executable migration runner
- module-level version/compatibility summary
- optional declaration-level change metadata
- author-declared intent plus tool-inferred contract diffs
- runtime startup remains non-blocking for migration metadata

V1.5-E completes the initial V1.5 plan.

Validation passed:
- `rtk go fmt . ./store ./contractdiff`
- `rtk go test ./... -run 'Test.*Migration|Test.*ContractDiff|Test.*Policy' -count=1`
- `rtk go test . -count=1`
- `rtk go test ./contractdiff -count=1`
- `rtk go test ./codegen -count=1`
- `rtk go test ./store -run TestRapidStoreCommitMatchesModel -count=50`
- `rtk go test ./... -count=1`
- `rtk go vet . ./store ./contractdiff ./codegen`
