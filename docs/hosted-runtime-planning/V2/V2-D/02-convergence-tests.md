# V2-D Task 02: Add Failing Read-Surface Convergence Tests

Parent plan: `docs/hosted-runtime-planning/V2/V2-D/00-current-execution-plan.md`

Objective: make the declaration-vs-SQL decision testable.

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
