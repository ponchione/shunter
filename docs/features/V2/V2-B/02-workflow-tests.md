# V2-B Task 02: Add Failing Contract Workflow Tests

Parent plan: `docs/features/V2/V2-B/00-current-execution-plan.md`

Objective: pin the admin/CLI behavior around existing contract artifacts.

Likely files:
- new workflow/helper package tests
- command tests only if a command entrypoint is added
- JSON fixtures under the chosen package testdata directory

Tests to add:
- done: diffing previous and current canonical JSON returns deterministic
  additive,
  breaking, and metadata-only changes
- done: policy checks return deterministic warnings and strict-mode failure
  status
- done: TypeScript codegen from JSON writes deterministic output
- done: malformed JSON returns a clear error
- done: unsupported language returns the existing codegen sentinel
- done: missing input paths and unwritable output paths produce clear errors
- done: command help documents that module export belongs in app-owned binaries

Test boundaries:
- do not start a runtime
- do not load an arbitrary app module dynamically
- do not add migration plan semantics
- do not add multi-module aggregate contracts

Added test coverage:
- `contractworkflow/contractworkflow_test.go`
- `cmd/shunter/main_test.go`

Initial red proof:
- `rtk go test ./contractworkflow ./cmd/shunter -count=1` failed with missing
  `CompareFiles`, `FormatDiff`, `FormatPolicy`, `GenerateFile`,
  `GenerateFromFile`, format constants, helper functions, and CLI `run`.
