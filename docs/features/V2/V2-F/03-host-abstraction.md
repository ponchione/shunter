# V2-F Task 03: Implement The Host Abstraction

Parent plan: `docs/features/V2/V2-F/00-current-execution-plan.md`

Objective: add a minimal owner for multiple runtime instances while preserving
the existing `Runtime` API.

Implementation direction:
- prefer a new host owner type over changing `Runtime` into a multi-module
  object
- require explicit module names and routing prefixes
- reject ambiguous duplicate module identities
- keep per-module lifecycle cleanup deterministic
- keep one-module apps on the existing `Runtime` path

Do not implement:
- cross-module transactions
- shared reducer registry
- shared subscription manager
- global schema registry
- dynamic module import
- process supervision
