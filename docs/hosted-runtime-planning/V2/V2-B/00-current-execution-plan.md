# Hosted Runtime V2-B Current Execution Plan

Goal: add owner-operated contract artifact workflows around the live
`ModuleContract`, `codegen`, and `contractdiff` packages.

V2-B target:
- make canonical JSON snapshots practical in local scripts and CI
- support diff/policy checks over previous and current contract JSON
- support TypeScript codegen from canonical contract JSON
- keep runtime/module export inside app-owned binaries unless a generic CLI can
  safely load the module

Task sequence:
1. Reconfirm contract export, codegen, and contractdiff package surfaces.
2. Add failing tests for JSON-file diff, policy, and codegen workflows.
3. Implement reusable command/workflow helpers over contract JSON files.
4. Add a minimal CLI entrypoint only for workflows that do not require dynamic
   module loading.
5. Format and validate V2-B gates.

Scope boundaries:
- In scope: contract JSON input/output, diff reports, policy warnings,
  TypeScript generation from JSON, deterministic command output.
- Out of scope: dynamic module loading, app runtime startup from a generic CLI,
  local reducer/query admin commands, cloud control plane, multi-module host
  commands.

Immediate next V2 slice after V2-B: V2-C migration planning and validation.
