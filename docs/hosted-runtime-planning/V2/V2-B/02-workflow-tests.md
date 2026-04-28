# V2-B Task 02: Add Failing Contract Workflow Tests

Parent plan: `docs/hosted-runtime-planning/V2/V2-B/00-current-execution-plan.md`

Objective: pin the admin/CLI behavior around existing contract artifacts.

Likely files:
- new workflow/helper package tests
- command tests only if a command entrypoint is added
- JSON fixtures under the chosen package testdata directory

Tests to add:
- diffing previous and current canonical JSON returns deterministic additive,
  breaking, and metadata-only changes
- policy checks return deterministic warnings and strict-mode failure status
- TypeScript codegen from JSON writes deterministic output
- malformed JSON returns a clear error
- unsupported language returns the existing codegen sentinel
- missing input paths and unwritable output paths produce clear errors
- command help documents that module export belongs in app-owned binaries

Test boundaries:
- do not start a runtime
- do not load an arbitrary app module dynamically
- do not add migration plan semantics; V2-C owns that
- do not add multi-module aggregate contracts
