# V1.5-E Task 05: Add Warning And CI-Oriented Policy Checks

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-E/00-current-execution-plan.md`

Objective: turn metadata plus inferred diffs into warnings that apps can use in
review or CI.

Implementation target:
- warn when migration metadata is missing for changed surfaces
- warn when a risky inferred change is declared as compatible
- warn when declared metadata says breaking but inferred diff is only additive
- warn when previous-version reference is missing where project policy requires
  it
- keep runtime startup independent from these warnings

Tests to add:
- declared-vs-inferred compatibility mismatch reports a warning
- missing metadata reports a warning without failing by default
- optional strict/CI mode can fail on warnings if such a mode is added
- runtime build/start tests remain unaffected by risky metadata

Policy guidance:
- default library behavior should report findings
- project CI may choose whether warnings are fatal
- Shunter runtime should not refuse to start solely because migration metadata is
  absent or risky in V1.5

