# V2-A Task 04: Preserve Exports And Lifecycle Behavior

Parent plan: `docs/hosted-runtime-planning/V2/V2-A/00-current-execution-plan.md`

Objective: prove internal boundary cleanup did not change the runtime contract.

Checks to preserve:
- `Module.Describe` returns module identity, query/view metadata, migration
  metadata, and table migration metadata
- `Runtime.Describe` returns module description plus runtime health
- `Runtime.ExportSchema` returns detached schema export
- `Runtime.ExportContract` returns the canonical full module contract
- `Runtime.ExportContractJSON` stays deterministic and newline-terminated
- `Start` readiness and `Close` idempotency remain unchanged
- `HTTPHandler` and `ListenAndServe` still use the runtime-owned protocol graph
- local `CallReducer` and `Read` still require a ready runtime

Validation focus:
- root describe/export tests
- root lifecycle tests
- root network/local-call tests when touched by the cleanup
