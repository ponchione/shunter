# V2-E Task 03: Implement Claims And Reducer Enforcement

Parent plan: `docs/features/V2/V2-E/00-current-execution-plan.md`

Objective: enforce reducer permission metadata through the smallest claims
model that fits the current auth and caller paths.

Implementation direction:
- define where permission tags live in validated caller context
- parse permission tags from JWT claims only after preserving existing identity
  validation semantics
- make local-call permission behavior explicit and testable
- check permissions before reducer execution commits any transaction
- return stable permission-denied errors
- preserve passive metadata export for tooling

Guardrails:
- do not change reducer handler signatures unless there is no smaller option
- do not make `AuthModeDev` unexpectedly strict
- do not hide permission-denied inside user reducer failure status
- do not add query/view read enforcement until Task 04

## Completion Notes

- Permission tags live on validated `auth.Claims` and are copied into
  `types.CallerContext` for reducer execution.
- Reducer handler signatures were unchanged.
- `AuthModeDev` local calls and anonymous/dev protocol connections use an
  explicit all-permissions flag; strict mode uses supplied local permissions or
  validated JWT claim tags.
- Reducer permission checks run in the executor after reducer lookup and before
  transaction creation.
- Permission denial returns `ErrPermissionDenied` with
  `StatusFailedPermission`, not `StatusFailedUser`.
