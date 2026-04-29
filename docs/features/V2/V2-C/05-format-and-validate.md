# V2-C Task 05: Format And Validate The Slice

Parent plan: `docs/hosted-runtime-planning/V2/V2-C/00-current-execution-plan.md`

Objective: run migration planning validation gates.

Commands:
- `rtk go fmt ./contractdiff`
- `rtk go test ./contractdiff -count=1`
- `rtk go test ./... -run 'Test.*(Migration|ContractDiff|Policy|Plan)' -count=1`
- `rtk go vet ./contractdiff`

Also include any new migration planning package path in the format, focused
test, and vet commands.

Expand when root contract export or storage preflight changed:
- `rtk go test . -run 'Test.*Migration' -count=1`
- `rtk go test ./store ./commitlog -count=1`
- `rtk go test ./... -count=1`

Validation checklist:
- no executable migration runner was added
- normal runtime startup remains non-blocking
- plan output is deterministic
- manual-review-needed remains a valid outcome
- stored state is not mutated by planning or validation

Recorded V2-C validation:
- `rtk go fmt ./contractdiff ./contractworkflow ./cmd/shunter`
- `rtk go test ./contractdiff ./contractworkflow ./cmd/shunter -count=1`
- `rtk go test ./... -run 'Test.*(Migration|ContractDiff|Policy|Plan)' -count=1`
- `rtk go vet ./contractdiff ./contractworkflow ./cmd/shunter`

Checklist result:
- no executable migration runner was added
- normal runtime startup remains non-blocking
- plan text and JSON output are deterministic
- `manual-review-needed` and `data-rewrite-needed` classifications are surfaced
- stored state is not opened or mutated by planning or validation
