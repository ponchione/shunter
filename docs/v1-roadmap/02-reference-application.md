# Reference Application

Status: open
Owner: unassigned
Scope: one maintained end-to-end application that proves the normal Shunter v1
developer and operator workflows.

## Goal

Build and maintain a realistic Shunter application that exercises the runtime
the way real users will. This app should be the proving ground for API
ergonomics, generated clients, auth, migrations, subscriptions, backup/restore,
and operations.

The reference app should not be a toy with one table and one reducer. It should
be small enough to maintain, but rich enough to expose missing v1 capabilities.

## Current State

Shunter has implementation and package-level coverage, but the public docs note
limited onboarding and no maintained hello-world or tutorial. That leaves a gap:
the runtime can pass package tests while the whole app-author workflow still
feels unclear.

SpacetimeDB's reference material is useful here because its product experience
is centered around modules, generated clients, local cache, reducer calls, and
subscriptions. Shunter should borrow that lesson without copying SpacetimeDB's
module language/runtime model.

## App Requirements

The reference app should include:

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

Good candidate domains:

- collaborative task board
- issue tracker
- multiplayer lobby/match state
- inventory/order workflow

Avoid domains that require large unrelated product work such as billing,
payments, rich text, media upload, or third-party integrations.

## Implementation Work

- Choose the app domain and write a one-page app contract.
- Add the app under an examples or integration-test location agreed with the
  repo layout.
- Build the server as an app-owned binary using normal `shunter.Module` and
  `shunter.Runtime` APIs.
- Generate TypeScript artifacts from the module contract.
- Build a minimal client that uses the generated artifacts.
- Add integration tests that run the app, call reducers, observe subscriptions,
  and verify recovery from persisted data.
- Add docs that show app authors the normal development loop:
  - define schema
  - add reducer
  - add query/view
  - export contract
  - generate client
  - run app
  - migrate data
  - backup and restore

## Verification

The reference app should become a normal CI target once stable:

```bash
rtk go test ./...
```

If the client uses TypeScript, add the repo-appropriate typecheck/build command
and document it here after the package layout exists.

The app should also have at least one black-box test that starts from an empty
data directory, performs a scenario, shuts down, restarts, and verifies state
and subscriptions after recovery.

## Done Criteria

- A maintained reference app exists in-repo.
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

