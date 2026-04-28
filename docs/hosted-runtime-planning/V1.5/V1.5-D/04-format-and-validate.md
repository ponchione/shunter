# V1.5-D Task 04: Format And Validate The Slice

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-D/00-current-execution-plan.md`

Objective: run permission/read-model metadata verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*Permission|Test.*ReadModel|Test.*Contract' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

Expand when codegen output changes:
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./... -count=1`

Validation checklist:
- metadata attaches to reducers, queries, and views
- metadata is exported in the canonical contract
- generated clients/docs can inspect metadata when applicable
- runtime behavior remains non-blocking unless explicitly designed
- migration metadata and diff tooling did not leak into V1.5-D

