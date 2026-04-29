# V2.5 Task 03: Shared Permission Checker

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Depends on:
- Task 01 stack reconfirmation

Objective: extract a shared permission-checking rule so reducer enforcement and
read authorization use the same semantics.

## Required Context

Read:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- `docs/features/V2/V2-E/00-current-execution-plan.md`

Inspect:

```sh
rtk go doc ./types.CallerContext
rtk go doc ./executor.ErrPermissionDenied
rtk rg -n "missingRequiredPermission|callerHasPermission|AllowAllPermissions|Permissions" executor protocol types *.go
```

## Target Behavior

Define one shared helper for required permission checks.

Semantics:

- if `AllowAllPermissions` is true, authorization succeeds
- otherwise every non-empty required permission must be present in the caller's
  permission set
- the helper returns the first missing required permission when authorization
  fails
- order of missing-permission reporting should match current reducer behavior

Use this shared helper from existing reducer permission enforcement without
changing reducer behavior.

The helper should be usable by protocol/runtime read authorization without
creating an import cycle. If package placement is ambiguous, prefer a small
package under an existing low-level package boundary such as `types` or a new
internal package that both `executor` and `protocol` can import.

## Tests To Add First

Add focused failing tests for:

- `AllowAllPermissions` bypasses required permissions
- all required permissions present succeeds
- one missing permission fails and reports the first missing tag
- empty required strings are ignored consistently with current reducer logic
- reducer permission tests still pass through the new helper

## Validation

Run at least:

```sh
rtk go fmt ./types ./executor ./protocol .
rtk go test ./types ./executor ./protocol -count=1
rtk go test . -run 'Test.*(Permission|Reducer|Auth|Local|Network)' -count=1
rtk go vet ./types ./executor ./protocol .
```

Expand if package placement touches other consumers.

## Completion Notes

- Helper location: `types/permissions.go`.
- Helper function: `types.MissingRequiredPermission(caller CallerContext, required []string) (string, bool)`.
- Reducer enforcement now calls the shared helper from
  `executor.(*Executor).handleCallReducer`; reducer permission behavior did not
  change. `AllowAllPermissions` still bypasses required permissions, empty
  required strings are ignored, every non-empty required permission must be
  present, and the first missing required permission is reported in the existing
  `ErrPermissionDenied` error shape.
- Added focused helper tests in `types/permissions_test.go`.

Validation run:

```sh
rtk go fmt ./types ./executor ./protocol .
rtk go test ./types ./executor ./protocol -count=1
rtk go test . -run 'Test.*(Permission|Reducer|Auth|Local|Network)' -count=1
rtk go vet ./types ./executor ./protocol .
```
