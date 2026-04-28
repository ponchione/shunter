# V2-D Task 03: Implement The Declared Read Model

Parent plan: `docs/hosted-runtime-planning/V2/V2-D/00-current-execution-plan.md`

Objective: implement the smallest coherent relationship between declarations
and executable reads.

Acceptable implementation directions:
- declarations remain metadata-only, and generated clients clearly use raw SQL
  helpers for execution
- declarations become named aliases over SQL strings validated at build time
- declarations become Go-defined read handlers with a separate execution path

Decision constraints:
- preserve the existing protocol SQL path unless there is a concrete reason to
  break it
- do not duplicate subscription evaluator behavior
- keep contract export deterministic
- keep generated clients honest about what can actually execute
- prefer one source of truth for named reads once a model is chosen

Do not implement:
- ORDER BY, subqueries, arbitrary aggregates, or broad SQL expansion unless the
  selected model explicitly needs them
- policy enforcement; V2-E owns that
- aggregate multi-module read routing; V2-F owns that
