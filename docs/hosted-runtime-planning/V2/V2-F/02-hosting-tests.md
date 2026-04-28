# V2-F Task 02: Add Failing Multi-Module Hosting Tests

Parent plan: `docs/hosted-runtime-planning/V2/V2-F/00-current-execution-plan.md`

Objective: pin the host behavior before implementation.

Likely files:
- new host package/root tests
- root runtime network/lifecycle tests if a top-level host API is added

Tests to add:
- a host can register two built runtimes with distinct module names
- duplicate module names fail clearly
- each module uses an isolated data directory or explicit storage namespace
- starting the host starts each runtime in deterministic order or reports
  partial-start cleanup clearly
- closing the host closes every started runtime
- routing sends each module's WebSocket traffic to the correct runtime
- aggregate health reports per-module state
- per-module `ExportContract` remains unchanged
- any aggregate contract references, rather than mutates, per-module contracts

Test boundaries:
- do not implement cross-module reducer calls
- do not implement shared transactions
- do not implement process isolation
- do not merge schemas into one global schema
