# V1.5-B Task 05: Format And Validate The Slice

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-B/00-current-execution-plan.md`

Objective: run canonical contract export verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*Contract|Test.*Export.*JSON' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

Expand when contract code touches schema export or shared tooling:
- `rtk go test ./schema -count=1`
- `rtk go test ./... -count=1`

Validation checklist:
- contract contains schema, reducers, queries, and views
- contract values are detached snapshots
- canonical JSON output is deterministic
- `shunter.contract.json` is a documented default, not a runtime requirement
- client codegen, permission semantics, migration diffs, and executable
  migrations did not leak into V1.5-B

