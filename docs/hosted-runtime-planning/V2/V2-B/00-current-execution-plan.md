# Hosted Runtime V2-B Execution Plan

Goal: add owner-operated contract artifact workflows around the live
`ModuleContract`, `codegen`, and `contractdiff` packages.

Status: complete.

V2-B target:
- make canonical JSON snapshots practical in local scripts and CI
- support diff/policy checks over previous and current contract JSON
- support TypeScript codegen from canonical contract JSON
- keep runtime/module export inside app-owned binaries unless a generic CLI can
  safely load the module

Task sequence:
1. Reconfirmed contract export, codegen, and contractdiff package surfaces.
2. Added failing tests for JSON-file diff, policy, and codegen workflows.
3. Implemented reusable command/workflow helpers over contract JSON files.
4. Added a minimal CLI entrypoint only for workflows that do not require
   dynamic module loading.
5. Formatted and validated V2-B gates.

Scope boundaries:
- In scope: contract JSON input/output, diff reports, policy warnings,
  TypeScript generation from JSON, deterministic command output.
- Out of scope: dynamic module loading, app runtime startup from a generic CLI,
  local reducer/query admin commands, cloud control plane, multi-module host
  commands.

Immediate next V2 slice after V2-B: V2-C migration planning and validation.

Live proof:
- `contractworkflow.CompareFiles` diffs previous/current canonical contract
  JSON files through `contractdiff.CompareJSON`.
- `contractworkflow.CheckPolicyFiles` runs deterministic policy checks through
  `contractdiff.CheckPolicy`, preserving non-strict warnings and strict
  failure status.
- `contractworkflow.GenerateFromFile` and `GenerateFile` generate TypeScript
  bindings from contract JSON through `codegen.GenerateFromJSON`.
- `contractworkflow.FormatDiff` and `FormatPolicy` render deterministic text
  or JSON workflow output.
- `cmd/shunter` exposes `contract diff`, `contract policy`, and
  `contract codegen` over existing JSON files only.
- generic CLI help documents that contract export belongs in app-owned
  binaries via `Runtime.ExportContractJSON`; no dynamic module loading,
  runtime startup, reducer/query admin commands, or multi-module host commands
  were added.
