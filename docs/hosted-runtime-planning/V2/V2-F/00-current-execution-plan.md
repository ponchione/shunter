# Hosted Runtime V2-F Current Execution Plan

Status: complete

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

## Completion Proof

- `HostRuntime` and `NewHost(...)` bind already-built runtimes to explicit
  module names and route prefixes without changing the one-module `Runtime`
  path.
- Host construction rejects nil runtimes, blank names, module-name/runtime
  identity mismatches, duplicate module names, overlapping route prefixes, and
  shared runtime data directories.
- `Host.Start` starts runtimes in registration order and closes already-started
  runtimes in reverse order if a later runtime fails to start.
- `Host.Close` closes every hosted runtime in reverse registration order.
- `Host.HTTPHandler` mounts each runtime's existing protocol handler below its
  explicit prefix, preserving the runtime's `/subscribe` behavior below that
  mount point.
- `Host.Health` and `Host.Describe` expose detached per-module diagnostics with
  module name, route prefix, data directory, and runtime health/description.
- Per-module `Runtime.ExportContract` remains unchanged and canonical; V2-F did
  not add an aggregate contract artifact or automatic contract merging.
- No process isolation, dynamic module loading, cross-module transactions,
  shared-table semantics, or global schema/reducer/subscription registry was
  added.

## Validation

- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Host|MultiModule|Runtime|Network|Contract)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./protocol ./subscription ./executor -count=1`
- `rtk go test ./... -count=1`
