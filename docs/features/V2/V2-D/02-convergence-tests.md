# V2-D Task 02: Add Failing Read-Surface Convergence Tests

Parent plan: `docs/features/V2/V2-D/00-current-execution-plan.md`

Objective: make the declaration-vs-SQL decision testable.

Status: complete.

Likely files:
- `module_declarations_test.go`
- `runtime_contract_test.go`
- `codegen/codegen_test.go`
- protocol read/subscription tests if protocol behavior changes
- new package tests if a read declaration compiler/resolver is introduced

Candidate tests:
- generated client output makes clear whether named declarations or raw SQL are
  the callable surface
- exported contracts carry any stable declaration execution metadata needed by
  clients
- raw SQL protocol reads continue to work when named declarations exist
- invalid declaration execution metadata fails at build time, not at first
  client call
- duplicate or conflicting declaration/read names fail clearly
- SQL parse errors retain existing protocol error shape

Test boundaries:
- do not require a full SQL/view system
- do not change auth/policy behavior
- do not add multi-module namespacing

Implemented tests:
- declaration descriptions and contracts include SQL metadata when present.
- invalid SQL-backed query/view declarations fail `Build` with
  `ErrInvalidDeclarationSQL`.
- subscription-only view SQL rejects projection and `LIMIT`.
- TypeScript codegen emits executable helpers only for declarations with SQL
  metadata.
- contractdiff reports declaration SQL additions/removals/changes.
- protocol SQL tests remained green with named declarations in the root package.
