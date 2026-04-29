# V2-B Task 04: Add The Minimal CLI Entrypoint

Parent plan: `docs/hosted-runtime-planning/V2/V2-B/00-current-execution-plan.md`

Objective: expose only the generic contract-file workflows that the current
codebase can honestly support.

Candidate commands:
- done: diff previous and current contract JSON
- done: check migration/contract policy over previous and current contract JSON
- done: generate TypeScript bindings from contract JSON

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

Implemented entrypoint:
- `cmd/shunter`

Implemented commands:
- `shunter contract diff --previous old.json --current current.json [--format text|json]`
- `shunter contract policy --previous old.json --current current.json [--strict] [--require-previous-version] [--format text|json]`
- `shunter contract codegen --contract shunter.contract.json --language typescript --out client.ts`

Command boundary proof:
- help text states that the generic CLI works only on existing
  `ModuleContract` JSON files.
- help text points module export at app-owned binaries using
  `Runtime.ExportContractJSON`.
- no runtime startup, module import/plugin loading, or interactive prompt was
  added.
