# V2-C Task 03: Implement The Migration Plan Model

Parent plan: `docs/hosted-runtime-planning/V2/V2-C/00-current-execution-plan.md`

Objective: produce a deterministic migration plan/report from old and current
contracts.

Implementation direction:
- accept old and current `ModuleContract` values or canonical JSON
- call `contractdiff.Compare` or `CompareJSON`
- include policy warnings from `contractdiff.CheckPolicy`
- group changes by surface: contract, schema, table, reducer, query, view,
  permission, read-model, migration metadata
- carry author-declared migration metadata into plan entries
- expose a stable severity/action model suitable for CI

Output posture:
- "review required" is a valid result
- "execution unsupported" should be explicit for data rewrite entries
- plan output should be deterministic for repository diffs

Do not implement:
- ordered executable function registry
- automatic rollback
- startup migration lock
- stored-state rewrite
