# Plan: complete previously mentioned TECH-DEBT items

Goal
- Complete all items explicitly mentioned in the prior status message:
  - tiny smell batch: TD-069, TD-070, TD-076, TD-077, TD-078
  - open implementation items: TD-012, TD-004, TD-008, TD-021, TD-024
- Use test-first / regression-first changes for behavior and contract work.
- Finish with focused and full verification plus TECH-DEBT status updates.

Context
- Repo has live Go implementation despite docs claiming docs-first.
- Sequencing authority remains docs/EXECUTION-ORDER.md.
- Relevant slices:
  - SPEC-001 E5 Story 5.3 StateView
  - SPEC-006 E5 Story 5.6 schema compatibility at startup
  - SPEC-006 E6 Stories 6.1/6.2 schema export
  - SPEC-003 E3 Story 3.4 subscription command dispatch
  - SPEC-002 E5 Stories 5.1–5.4 snapshot I/O

Execution phases
1. Low-risk hygiene batch
   - auth/mint.go: collapse duplicate time.Now()
   - schema/registry.go: remove/resolve dead userTableCount handling
   - schema/errors.go / executor/registry.go: gofmt cleanup if still needed
   - executor/executor.go: rename local cap -> capacity
   - verify package / repo still green

2. TD-012 StateView
   - Add StateView tests first
   - Implement state_view.go with GetRow / ScanTable / SeekIndex / SeekIndexRange
   - Reuse from Transaction where appropriate
   - Verify store package and repo

3. TD-004 schema compatibility
   - Add tests first for matching / version mismatch / structural mismatch / nil snapshot
   - Add schema/version.go with SnapshotSchema, ErrSchemaMismatch, CheckSchemaCompatibility
   - Add non-invasive Engine.Start wiring via optional startup snapshot metadata on EngineOptions if available, otherwise nil/fresh-start compatible path
   - Verify schema package and repo

4. TD-008 schema export
   - Add export API / JSON tests first
   - Implement schema/export.go with export value types and Engine.ExportSchema
   - Verify stable ordering and detached snapshot semantics
   - Verify schema package and repo

5. TD-021 executor subscription command dispatch
   - Add failing executor tests first
   - Extend dispatch and SubscriptionManager surface as needed
   - Implement register/unregister/disconnect handlers with committed snapshot lifecycle guarantees
   - Verify executor/subscription/protocol consumers and repo

6. TD-024 commitlog snapshot I/O
   - Implement in internal order from spec:
     - integrity/constants/helpers
     - schema snapshot codec
     - writer
     - reader/listing
   - Add focused tests story by story
   - Verify commitlog package then full repo

7. Documentation closeout
   - Update TECH-DEBT statuses/summaries for completed items
   - Report exact files touched and any residual blockers if any remain

Likely files
- auth/mint.go
- schema/errors.go
- schema/build.go
- schema/registry.go
- schema/export.go
- schema/version.go
- schema/*_test.go
- store/state_view.go
- store/transaction.go
- store/*_test.go
- executor/executor.go
- executor/command.go
- executor/interfaces.go
- executor/*_test.go
- commitlog/schema_snapshot.go
- commitlog/snapshot_writer.go
- commitlog/snapshot_reader.go
- commitlog/*_test.go
- TECH-DEBT.md

Verification
- Focused package tests after each slice
- Final:
  - rtk go build ./...
  - rtk go vet ./...
  - rtk go test ./...

Risks / notes
- TD-024 is the largest slice and may expose missing SPEC-001 recovery/export helpers; if so, implement only what the snapshot stories require and avoid broad recovery widening.
- TD-004 Start() wiring may need a minimal engine option seam for supplying snapshot schema at startup; keep it narrow and spec-aligned.
- TD-021 may require executor/subscription interface alignment but should avoid prematurely implementing unrelated protocol behavior.
