# V2-G Task 05: Format And Validate The Slice

Parent plan: `docs/hosted-runtime-planning/V2/V2-G/00-current-execution-plan.md`

Objective: run process-boundary gate validation.

Commands:
- `rtk go fmt . ./executor ./store ./subscription ./protocol`
- `rtk go test ./executor ./store ./subscription ./protocol -count=1`
- `rtk go test . -run 'Test.*(Runtime|Lifecycle|Local|Contract)' -count=1`
- `rtk go vet . ./executor ./store ./subscription ./protocol`

Also include any new boundary package path in the format, focused test, and vet
commands.

Expand if any shared runtime path changed:
- `rtk go test ./... -count=1`

Validation checklist:
- in-process module execution remains supported
- boundary failures are classified clearly
- no production process runner was implied by an experimental prototype
- decision record is complete
- future work is explicitly kept, deferred, or rejected
