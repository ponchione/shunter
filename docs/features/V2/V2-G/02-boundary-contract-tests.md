# V2-G Task 02: Add Failing Process Boundary Contract Tests

Parent plan: `docs/features/V2/V2-G/00-current-execution-plan.md`

Objective: prove the minimal interface needed for out-of-process execution
before building a runner.

Likely files:
- new process-boundary package tests if a prototype package is created
- executor/store/subscription tests only if a seam is introduced there

Tests to add:
- reducer invocation request/response can represent caller identity, args,
  status, output, and user error
- boundary failures are distinguishable from user reducer failures
- transaction commit/rollback semantics are explicitly represented or declared
  unsupported
- lifecycle hooks have clear ordering and failure behavior
- subscription updates remain driven by committed state, not process messages
- process-boundary metadata does not affect canonical module contracts unless
  intentionally added

Test boundaries:
- do not start real child processes unless Task 03 explicitly chooses that
  prototype
- do not add cross-language support
- do not replace in-process reducer execution

## Added Tests

Added `internal/processboundary/contract_test.go`:
- `TestInvocationRequestAndResponseRepresentReducerCall` verifies reducer
  invocation request/response JSON can carry caller identity, connection ID,
  permission tags, args, status, output, and explicit transaction semantics.
- `TestInvocationFailuresDistinguishUserAndBoundaryFailures` verifies user
  reducer failures and boundary/transport failures have separate status and
  failure classes.
- `TestValidateInvocationResponseRequiresExplicitTransactionSemantics` verifies
  every response must either declare transaction semantics or explicitly mark
  them unsupported.
- `TestDefaultContractDeclaresLifecycleAndSubscriptionSemantics` verifies
  OnConnect and OnDisconnect ordering/failure behavior and requires
  committed-state-driven subscription updates.
- `TestValidateContractRejectsProcessDrivenSubscriptionUpdates` rejects any
  contract that lets process messages broadcast subscription updates.

Added `TestRuntimeExportContractOmitsProcessBoundaryMetadata` in
`runtime_contract_test.go` to prove V2-G process-boundary metadata does not
change canonical module contract JSON.
