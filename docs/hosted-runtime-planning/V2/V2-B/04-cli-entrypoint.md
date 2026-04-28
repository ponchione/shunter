# V2-B Task 04: Add The Minimal CLI Entrypoint

Parent plan: `docs/hosted-runtime-planning/V2/V2-B/00-current-execution-plan.md`

Objective: expose only the generic contract-file workflows that the current
codebase can honestly support.

Candidate commands:
- diff previous and current contract JSON
- check migration/contract policy over previous and current contract JSON
- generate TypeScript bindings from contract JSON

Command boundaries:
- do not claim to export the current app contract from a generic binary
- document that app-owned binaries can call `Runtime.ExportContractJSON`
- keep command output deterministic for CI
- keep stderr/stdout behavior testable
- avoid interactive prompts

If a CLI entrypoint is deferred:
- record the reason in this task file
- leave the reusable workflow package complete
- point app-owned binaries at the helper API
