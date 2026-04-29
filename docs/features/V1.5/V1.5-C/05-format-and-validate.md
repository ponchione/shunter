# V1.5-C Task 05: Format And Validate The Slice

Parent plan: `docs/features/V1.5/V1.5-C/00-current-execution-plan.md`

Objective: run client binding/codegen verification gates.

Commands:
- `rtk go fmt <touched packages>`
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet <touched packages>`

If V1.5-C creates a command package, include that package explicitly in the
format and vet commands.

Validation checklist:
- generator consumes canonical contract JSON
- generated output is deterministic
- TypeScript output covers tables, reducers, queries, and views
- unsupported languages fail clearly
- generator does not require a live runtime
- permissions metadata and migration metadata remain passive/reserved unless
  already populated by later slices
