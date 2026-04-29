# V1.5-A Task 03: Implement Query/View Declaration API

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-A/00-current-execution-plan.md`

Objective: add the smallest root-package declarations that can be registered on
`*shunter.Module` and later exported in a contract artifact.

Implementation target:
- add query declaration types in the root package or a narrow internal helper
- add view declaration types in the root package or a narrow internal helper
- add fluent `Module` methods for registering declarations
- preserve detached-copy behavior for all exported metadata
- keep declaration storage module-owned before `Build`
- copy declaration metadata into `Runtime` only if runtime descriptions need it

The API should be code-first. String query text may appear as metadata if it is
already the smallest useful bridge to the existing query layer, but string/DSL
authoring should not become the primary V1.5 model in this slice.

Suggested minimum metadata:
- declaration name
- declaration kind: query or view
- referenced table names or a deliberately deferred equivalent
- argument metadata only if the implementation can declare it explicitly without
  reflection magic
- result row/source metadata only if the implementation can expose it without
  widening the runtime

Validation guidance:
- reject empty declaration names
- reject duplicate names deterministically
- keep error messages module-author-facing
- reuse existing validation patterns where they fit

Non-goals:
- full SQL/view system
- new query optimizer behavior
- generated client bindings
- canonical contract JSON
- permissions metadata
- migration metadata

