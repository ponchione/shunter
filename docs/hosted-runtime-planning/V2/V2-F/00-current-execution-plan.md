# Hosted Runtime V2-F Current Execution Plan

Goal: explore multi-module hosting only after runtime/module boundaries,
contract workflows, read semantics, and policy foundations are explicit.

V2-F target:
- keep one-module hosting as the simple default
- make module identity and routing explicit before sharing a host process
- preserve per-module `ModuleContract` as the source of truth
- add a runtime-level aggregate only if it references per-module contracts
  cleanly

Task sequence:
1. Reconfirm one-module runtime lifecycle, network, contract, and config
   boundaries.
2. Add failing tests for module identity, routing, lifecycle, and data-dir
   isolation.
3. Implement the smallest host abstraction that can own multiple runtime
   instances without weakening `Runtime`.
4. Add aggregate introspection/contract output only if needed by the host.
5. Format and validate V2-F gates.

Scope boundaries:
- In scope: explicit module namespacing, per-module lifecycle, per-module data
  directories, route mounting, aggregate health/description.
- Out of scope: process isolation, dynamic module loading, cloud fleet
  management, cross-module transactions, shared-table semantics, automatic
  contract merging.

Immediate next V2 slice after V2-F: V2-G out-of-process module execution gate.
