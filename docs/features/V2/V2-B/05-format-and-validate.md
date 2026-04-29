# V2-B Task 05: Format And Validate The Slice

Parent plan: `docs/hosted-runtime-planning/V2/V2-B/00-current-execution-plan.md`

Objective: run contract workflow validation gates.

Commands:
- passed: `rtk go fmt ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`
- passed: `rtk go test ./codegen ./contractdiff ./contractworkflow ./cmd/shunter -count=1`
- passed: `rtk go test ./... -run 'Test.*(Contract|Codegen|Diff|Policy)' -count=1`
- passed: `rtk go vet ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`

Also include any new workflow package path in the format, focused test, and vet
commands.

Expand when root runtime contract export changed:
- `rtk go test . -run 'Test.*Contract' -count=1`
- `rtk go test ./... -count=1`

Validation checklist:
- passed: all generic workflows operate on JSON files
- passed: no dynamic module loading was introduced
- passed: generated output remains deterministic
- passed: policy warnings preserve non-strict and strict behavior
- passed: app-owned export remains based on `Runtime.ExportContractJSON`

Focused red-to-green proof:
- red: `rtk go test ./contractworkflow ./cmd/shunter -count=1`
- green: `rtk go test ./contractworkflow ./cmd/shunter -count=1`
