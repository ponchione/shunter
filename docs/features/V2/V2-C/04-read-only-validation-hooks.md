# V2-C Task 04: Add Read-Only Validation Hooks

Parent plan: `docs/hosted-runtime-planning/V2/V2-C/00-current-execution-plan.md`

Objective: add optional preflight validation only where it can remain read-only
and deterministic.

Possible validation checks:
- current contract schema version and module metadata are internally
  consistent
- previous/current contract versions are ordered according to project policy
- data-rewrite-needed entries are flagged as not executable by Shunter yet
- stored state can be opened read-only for shape inspection only if the storage
  API supports it cleanly

Guardrails:
- no mutation of `store.CommittedState`
- no new commitlog records
- no snapshot writes
- no compaction
- no runtime startup blocking

If read-only stored-state validation is not clean with current APIs:
- defer it
- keep V2-C focused on contract-level planning
- record the needed storage seam for a later slice
