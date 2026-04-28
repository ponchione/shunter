# V2-E Task 05: Format And Validate The Slice

Parent plan: `docs/hosted-runtime-planning/V2/V2-E/00-current-execution-plan.md`

Objective: run policy/auth validation gates.

Commands:
- `rtk go fmt . ./auth ./protocol ./executor ./codegen`
- `rtk go test . -run 'Test.*(Permission|Auth|Reducer|Local|Network)' -count=1`
- `rtk go test ./auth ./protocol ./executor ./codegen -count=1`
- `rtk go vet . ./auth ./protocol ./executor ./codegen`

Expand when protocol/read behavior changed:
- `rtk go test ./query/sql ./subscription -count=1`
- `rtk go test ./... -count=1`

Validation checklist:
- permission metadata remains exported
- reducers enforce required permissions before transaction effects
- dev/local behavior is explicit
- strict auth behavior stays testable
- no broad policy framework or tenant model leaked into V2-E
