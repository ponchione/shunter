# V2-B Task 05: Format And Validate The Slice

Parent plan: `docs/hosted-runtime-planning/V2/V2-B/00-current-execution-plan.md`

Objective: run contract workflow validation gates.

Commands:
- `rtk go fmt ./codegen ./contractdiff`
- `rtk go test ./codegen ./contractdiff -count=1`
- `rtk go test ./... -run 'Test.*(Contract|Codegen|Diff|Policy)' -count=1`
- `rtk go vet ./codegen ./contractdiff`

Also include any new workflow package path in the format, focused test, and vet
commands.

Expand when root runtime contract export changed:
- `rtk go test . -run 'Test.*Contract' -count=1`
- `rtk go test ./... -count=1`

Validation checklist:
- all generic workflows operate on JSON files
- no dynamic module loading was introduced
- generated output remains deterministic
- policy warnings preserve non-strict and strict behavior
- app-owned export remains based on `Runtime.ExportContractJSON`
