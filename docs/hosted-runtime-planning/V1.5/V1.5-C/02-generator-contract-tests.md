# V1.5-C Task 02: Add Failing Generator Contract Tests

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-C/00-current-execution-plan.md`

Objective: pin generator behavior against a checked-in or in-test canonical
contract fixture before implementation.

Likely files:
- create a codegen package if one does not exist
- create `cmd/shunter-codegen` only if CLI wiring is in scope for this slice
- add test fixtures under the chosen package's `testdata/`

Tests to add:
- generator accepts canonical contract JSON as input
- unsupported language values fail clearly
- TypeScript output includes table row types
- TypeScript output includes reducer call names
- TypeScript output includes declared query bindings
- TypeScript output includes declared view/subscription bindings
- generated output is deterministic for the same input contract
- generator does not require a running `Runtime`

Test boundaries:
- do not require typed reducer argument schemas unless V1.5-B contract already
  provides them explicitly
- do not generate server/module implementation
- do not scaffold frontend frameworks
- do not make codegen depend on migration policy checks

