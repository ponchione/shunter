# V2-D Task 05: Format And Validate The Slice

Parent plan: `docs/features/V2/V2-D/00-current-execution-plan.md`

Objective: run declared-read convergence validation gates.

Status: complete.

Commands:
- `rtk go fmt . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`
- `rtk go test . -run 'Test.*(Declaration|Contract|Read|Query|View)' -count=1`
- `rtk go test ./protocol ./query/sql ./subscription -count=1`
- `rtk go test ./codegen ./contractdiff -count=1`
- `rtk go vet . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`

Expand when protocol or generated contract behavior changed:
- `rtk go test ./... -count=1`

Validation checklist:
- raw SQL protocol reads still work
- named declaration behavior is honest and deterministic
- contract/codegen output matches runtime behavior
- no broad SQL expansion leaked into V2-D
- policy enforcement remains deferred to V2-E

Validation passed:
- `rtk go fmt . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`
- `rtk go test . -run 'Test.*(Declaration|Contract|Read|Query|View)' -count=1`
- `rtk go test ./protocol ./query/sql ./subscription -count=1`
- `rtk go test ./codegen ./contractdiff -count=1`
- `rtk go vet . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`
- `rtk go test ./... -count=1`
