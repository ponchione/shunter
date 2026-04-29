# V2-A Task 05: Format And Validate The Slice

Parent plan: `docs/features/V2/V2-A/00-current-execution-plan.md`

Objective: run boundary-hardening verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Boundary|Describe|Export|Contract|Lifecycle|Build)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

Expand when runtime ownership or shared package contracts changed:
- `rtk go test ./... -count=1`

Validation checklist:
- no new public structural API unless justified by the slice tests
- one-module runtime behavior remains unchanged
- contract JSON remains deterministic
- module and export snapshots remain detached
- no multi-module, process-isolation, migration-execution, or policy-enforcement
  behavior leaked into V2-A

Validation result 2026-04-28: passed.
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Boundary|Describe|Export|Contract|Lifecycle|Build)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -count=1`
