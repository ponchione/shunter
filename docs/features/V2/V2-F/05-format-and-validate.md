# V2-F Task 05: Format And Validate The Slice

Parent plan: `docs/hosted-runtime-planning/V2/V2-F/00-current-execution-plan.md`

Objective: run multi-module hosting validation gates.

Status: complete

Commands:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Host|MultiModule|Runtime|Network|Contract)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

Also include any new host package path in the format, focused test, and vet
commands.

Expand when routing, protocol, or contracts changed:
- `rtk go test ./protocol ./subscription ./executor -count=1`
- `rtk go test ./... -count=1`

Validation checklist:
- one-module runtime path remains valid
- module names and routes are explicit
- data directories or storage namespaces do not collide
- per-module contracts remain canonical
- no process isolation or dynamic loading leaked into V2-F

Validation results:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Host|MultiModule|Runtime|Network|Contract)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./protocol ./subscription ./executor -count=1`
- `rtk go test ./... -count=1`
