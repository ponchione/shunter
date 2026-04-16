# TD-023 DecodeChangeset API alignment plan

Goal
- Align commitlog.DecodeChangeset with the documented two-argument public API while keeping row-size enforcement owned by commitlog defaults.

Scope
- Stay inside SPEC-002 Epic 3 / Story 3.2 and the TD-023 public API drift.
- No broader codec redesign; keep current runtime behavior and tests intact except where API alignment requires call-site updates.

Files
- Modify: commitlog/api_contract_test.go
- Modify: commitlog/changeset_codec.go
- Modify: commitlog/commitlog_test.go
- Modify: commitlog/phase4_acceptance_test.go
- Modify: TECH-DEBT.md

Plan
1. Add failing API-contract/runtime tests that:
   - compile against DecodeChangeset(data, reg)
   - prove the public decoder enforces DefaultCommitLogOptions().MaxRowBytes without the caller passing a limit
2. Run the focused commitlog tests and confirm failure for the missing public signature.
3. Implement the minimal fix:
   - change exported DecodeChangeset to the documented two-arg signature
   - move explicit max-row logic into an unexported helper, preserving current behavior internally
4. Update existing call sites/tests to use the public two-arg API, while keeping one focused test on the internal helper if needed for non-default explicit-limit coverage.
5. Re-run focused commitlog tests, then full build/vet/test verification.
6. Mark TD-023 resolved in TECH-DEBT.md (do not delete it).

Verification commands
- rtk go test ./commitlog -run 'TestCommitlogPublicAPIContractCompiles|TestDecodeChangesetUsesDefaultMaxRowBytes' -count=1
- rtk go test ./commitlog
- rtk go build ./...
- rtk go vet ./...
- rtk go test ./...
