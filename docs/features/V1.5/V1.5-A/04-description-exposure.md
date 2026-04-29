# V1.5-A Task 04: Expose Declaration Metadata Through Narrow Descriptions

Parent plan: `docs/features/V1.5/V1.5-A/00-current-execution-plan.md`

Objective: make query/view declarations inspectable without implementing the
V1.5-B canonical contract.

Implementation target:
- extend `ModuleDescription` or add a dedicated declaration-description method
- optionally extend `RuntimeDescription` if built runtimes need the same
  declaration snapshot
- keep all returned slices/maps detached
- preserve existing module identity and runtime health behavior
- keep `Runtime.ExportSchema()` as the lower-level schema export until V1.5-B

Preferred shape:
- module description carries declaration summaries
- schema export remains schema/reducer-focused
- V1.5-B owns the full module contract that combines schema, reducers, queries,
  views, permissions, migrations, and codegen metadata

Validation checklist:
- `Module.Describe` remains safe on nil modules
- declaration slices cannot be mutated by callers to affect later descriptions
- existing describe/export tests still pass
- no `shunter.contract.json` output exists in this slice

