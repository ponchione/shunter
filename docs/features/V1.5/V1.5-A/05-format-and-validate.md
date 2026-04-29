# V1.5-A Task 05: Format And Validate The Slice

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-A/00-current-execution-plan.md`

Objective: run query/view declaration verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test . -run 'Test(Module.*Declaration|Runtime.*Declaration|.*Describe.*Declaration)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

Expand when root/runtime behavior changed beyond declaration metadata:
- `rtk go test ./... -count=1`

Validation checklist:
- declarations are module-owned
- descriptions return detached declaration metadata
- existing v1 module/build/lifecycle/local-call behavior stays green
- codegen, permissions metadata, migration metadata, and canonical JSON did not
  leak into V1.5-A

