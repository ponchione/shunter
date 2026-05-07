# Reference Application

Status: open, external canary app exists; operational gaps remain
Owner: unassigned
Scope: one maintained external canary/reference application that proves the
normal Shunter v1 developer and operator workflows.

## Goal

Maintain a realistic Shunter application that exercises the runtime the way
real users will. This app should be the proving ground for API ergonomics,
generated clients, auth, migrations, subscriptions, backup/restore, and
operations.

The reference app should not be a toy with one table and one reducer. It should
be small enough to maintain, but rich enough to expose missing v1 capabilities.

## Current State

Shunter has implementation and package-level coverage, plus the
release-candidate workload in `rc_app_workload_test.go`. That root workload is
useful runtime proof, but it remains test-only.

The maintained app target is the external `opsboard-canary` repository, not a
new app inside this repository. It defines an operations-board domain with
public Shunter imports only, strict auth, varied read policies, sender-based
visibility filters, reducers, declared queries, declared live views, raw SQL
escape-hatch coverage, subscriptions, restart and rollback workflows,
contract export, and committed TypeScript generated artifacts.

Do not add a duplicate in-repo task-board app for v1. The previous in-repo
task-board contract is retained only as a retired planning note:
[`reference-app-taskboard-contract.md`](reference-app-taskboard-contract.md).
The active app contract lives with `opsboard-canary` in its
`OPSBOARD_CANARY_APP_SPEC.md`.

SpacetimeDB's reference material is useful here because its product experience
is centered around modules, generated clients, local cache, reducer calls, and
subscriptions. Shunter should borrow that lesson without copying SpacetimeDB's
module language/runtime model.

## App Requirements

The external canary app should include and keep testing:

- 5-8 tables with varied schema shapes.
- At least one private table and one public table.
- At least one visibility filter using the sender identity.
- Several reducers with validation and permission checks.
- At least one scheduled reducer or lifecycle hook if this remains part of v1.
- Declared one-off queries.
- Declared live views.
- Raw SQL usage only where it demonstrates an intentional escape hatch.
- TypeScript generated client artifacts.
- A small browser or Node client that connects, authenticates, calls reducers,
  subscribes to views, and updates local state.
- Backup, restore, and migration examples.
- A seed/load script or deterministic scenario for tests and demos.

## Implementation Work

Completed or partially complete:

- Adopt the external `opsboard-canary` repository as the maintained v1
  canary/reference app instead of creating a duplicate in-repo example.
- Confirm the app uses public Shunter package APIs for normal operation.
- Confirm the app covers strict auth, permission metadata, private/public
  tables, sender visibility, reducers, declared queries/views, raw SQL,
  subscriptions, restart, rollback, contract export, and generated TypeScript
  fixture checks.

Remaining:

- Fix canary-repository dependency hygiene so its public-package workflow runs
  cleanly against the sibling Shunter checkout.
- Add the missing backup/restore workflow to the canary app.
- Add one app-owned migration path to the canary app.
- Replace handwritten protocol-client helpers with the v1 TypeScript runtime
  SDK after that SDK exists.
- Add a release qualification step that runs the canary against the intended
  Shunter commit or tag.
- Keep canary docs showing the normal app-author loop: define schema, add
  reducer, add query/view, export contract, generate client, run app, migrate
  data, and backup/restore.

## Verification

The external canary app should become a normal release-qualification target once
stable. From the `opsboard-canary` checkout, the intended commands are:

```bash
rtk make canary-quick
rtk make canary-full
```

If the client uses the future TypeScript runtime SDK, add the repo-appropriate
typecheck/build command and document it here after that package exists.

The app should also have at least one black-box test that starts from an empty
data directory, performs a scenario, shuts down, restarts, and verifies state
and subscriptions after recovery.

## Done Criteria

- A maintained external canary/reference app exists and is documented as the v1
  proving ground.
- The app is documented as the recommended v1 starting point.
- The app exercises auth, reducers, declared reads, subscriptions,
  contract/codegen, persistence, backup/restore, and a migration path.
- The app fails loudly when public ergonomics regress.
- The app does not depend on private test-only helpers for normal operation.

## Non-Goals

- A large template ecosystem.
- Cloud deployment automation.
- Multiple language clients before the first TypeScript path is solid.
- A showcase UI that hides weak runtime ergonomics behind custom glue.
