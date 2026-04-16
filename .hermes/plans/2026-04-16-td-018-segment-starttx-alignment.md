# TD-018 Segment startTx alignment plan

Goal
- Make commitlog SegmentWriter enforce that the first appended record in a segment matches the segment's declared startTx.

Scope
- Stay inside SPEC-002 Epic 2 / Story 2.3 and the TD-018 regression surface.
- No recovery or broader durability-worker refactors.

Files
- Modify: commitlog/phase4_acceptance_test.go
- Modify: commitlog/segment.go
- Modify: TECH-DEBT.md

Plan
1. Add a failing regression test in commitlog/phase4_acceptance_test.go that:
   - creates a segment with startTx 100
   - verifies first append with tx 99 or 1 fails
   - verifies first append with tx 100 succeeds
   - verifies the written segment reopens with StartTxID()==100 and first record TxID==100
2. Run the focused commitlog test and confirm it fails for the expected reason.
3. Implement the minimal fix in SegmentWriter.Append:
   - when lastTx == 0, require rec.TxID == startTx
   - otherwise preserve existing strict monotonic > lastTx validation
4. Re-run focused commitlog tests, then full build/vet/test verification.
5. Mark TD-018 resolved in TECH-DEBT.md with updated current-behavior evidence.

Verification commands
- rtk go test ./commitlog -run TestSegmentWriterEnforcesStartTxAlignment -count=1
- rtk go test ./commitlog
- rtk go build ./...
- rtk go vet ./...
- rtk go test ./...
