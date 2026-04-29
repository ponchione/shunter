# V1.5-A Task 02: Add Failing Declaration Metadata Tests

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-A/00-current-execution-plan.md`

Objective: pin the public module-owned query/view declaration behavior before
implementation.

Likely files:
- create `module_declarations_test.go`
- extend `runtime_describe_test.go` only if runtime description exposure is part
  of the chosen narrow API

Tests to add:
- a module can register a named read query declaration
- a module can register a named live view/subscription declaration
- declaration names must be non-empty
- duplicate query names are rejected or surfaced as build-time errors
- duplicate view names are rejected or surfaced as build-time errors
- query names and view names use one shared namespace unless the implementation
  deliberately documents separate namespaces
- returned declaration descriptions are detached from module internals
- declarations remain visible before and after `Build`
- declarations do not affect existing table/reducer registration behavior

Test boundaries:
- do not assert codegen output
- do not assert permissions metadata
- do not assert migration metadata
- do not require the query engine to execute new syntax
- do not require `shunter.contract.json`

