# V2-F Task 04: Add Aggregate Introspection Only If Needed

Parent plan: `docs/features/V2/V2-F/00-current-execution-plan.md`

Objective: expose multi-module host diagnostics without replacing per-module
contracts.

Candidate outputs:
- host description with per-module names, routes, and health
- per-module contract export map
- aggregate contract index that references module contract artifacts

Guardrails:
- `ModuleContract` remains canonical for each module
- aggregate output must not become a hidden second source of truth
- contractdiff should continue to work per module
- codegen should continue to consume module contracts unless an aggregate
  generator is explicitly designed

If aggregate output is not needed:
- leave host diagnostics as health/description only
- record that per-module contracts remain sufficient

Recorded outcome:
- V2-F added host diagnostics as `Host.Health` and `Host.Describe` only.
- Per-module `Runtime.ExportContract` remains the canonical artifact for diff,
  policy, and codegen workflows.
- No aggregate contract artifact or contract merge was added.
