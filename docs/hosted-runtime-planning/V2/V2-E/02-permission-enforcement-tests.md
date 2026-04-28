# V2-E Task 02: Add Failing Permission Enforcement Tests

Parent plan: `docs/hosted-runtime-planning/V2/V2-E/00-current-execution-plan.md`

Objective: pin narrow enforcement behavior before implementation.

Likely files:
- `runtime_local_test.go`
- `runtime_network_test.go`
- `module_declarations_test.go`
- auth/protocol tests if claims parsing changes

Tests to add:
- reducers without permission metadata remain callable
- reducers with required permission tags reject callers without those tags
- local calls have an explicit default behavior in dev mode
- strict auth calls derive permissions only from validated claims
- permission-denied errors are distinguishable from missing reducer, user
  reducer failure, and internal executor errors
- exported contracts and generated clients still expose permission metadata
- read permissions remain deferred or are enforced according to the V2-D read
  model

Test boundaries:
- do not introduce a role database
- do not add tenant scoping
- do not require external IdP integration
- do not make dev/local tests require production auth setup
