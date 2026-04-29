# V1.5-E Task 06: Format And Validate The Slice

Parent plan: `docs/features/V1.5/V1.5-E/00-current-execution-plan.md`

Objective: run migration metadata and contract-diff verification gates.

Commands:
- `rtk go fmt <touched packages>`
- `rtk go test ./... -run 'Test.*Migration|Test.*ContractDiff|Test.*Policy' -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet <touched packages>`

If V1.5-E creates a command package, include that package explicitly in the
format and vet commands.

Validation checklist:
- migration metadata is exported through the canonical contract
- contract diff output is deterministic
- warning policy checks distinguish report-only findings from strict CI failures
- runtime startup remains non-blocking for migration metadata
- no executable migration runner or implicit startup migration behavior landed
