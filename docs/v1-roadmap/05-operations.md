# Operations, Backup, Restore, And Migrations

Status: open, runbook and release checklist added; external canary
backup/restore/migration flow added
Owner: unassigned
Scope: operator-facing workflows for data durability, backup/restore,
compaction, schema compatibility, migrations, and upgrades.

## Goal

Make Shunter safe to operate for real applications. A v1 operator should know
how to start, stop, back up, restore, compact, migrate, and upgrade an app
without guessing at hidden runtime behavior.

## Current State

Shunter has real durability primitives: commitlog, snapshots, compaction,
backup/restore helpers, data-dir compatibility checks, contract diffing,
contract policy, migration hooks, and app-owned runtime startup.

Current code and docs now include:

- `Runtime.WaitUntilDurable`, `Runtime.CreateSnapshot`, and
  `Runtime.CompactCommitLog`.
- `BackupDataDir` and `RestoreDataDir` offline helpers plus `shunter backup`
  and `shunter restore` CLI commands.
- `CheckDataDirCompatibility`, `Module.MigrationHook`,
  `RunDataDirMigrations`, and `RunModuleDataDirMigrations`.
- App-author backup, restore, migration, contract plan, and versioning guidance
  in `docs/how-to-use-shunter.md`.
- Durable `shunter.datadir.json` metadata that records Shunter build metadata
  separately from app module metadata and blocks mismatched module/schema usage.

The missing v1 work is turning those primitives into an opinionated operating
model with tested failure behavior.

## Operator Workflows To Define

1. Empty data-dir bootstrap.
2. Normal restart from existing data.
3. Graceful shutdown.
4. Crash recovery.
5. Manual snapshot creation.
6. Commitlog compaction.
7. Offline backup.
8. Restore into a fresh data directory.
9. Contract compatibility check before startup.
10. App-owned migration hook execution.
11. Failed migration recovery.
12. Shunter version upgrade.
13. App module version upgrade.

## v1 Decisions To Make

- Confirm the documented offline-only backup stance for v1, or explicitly
  design an online backup path.
- Decide snapshot retention defaults and whether Shunter owns cleanup.
- Decide whether compaction is manual, automatic, or app-configured.
- Decide whether migrations are only app-owned hooks or also have a Shunter
  ordered migration runner.
- Decide whether contract policy failures block startup by default.
- Decide what metadata is stored in data directories to distinguish Shunter
  runtime version from app module version.
- Decide the supported restore path when the target binary has a different
  module contract than the backup.

## Implementation Work

Completed or partially complete:

- Audit root backup/restore/migration APIs and `cmd/shunter` commands.
- Add backup/restore CLI commands and helper tests.
- Add app-author guidance for offline backup/restore, data-dir compatibility
  checks, migration hooks, contract plans, and release metadata stamping.
- Add an operator runbook under `docs/operations.md` covering `DataDir`
  lifecycle, offline backup/restore, migrations, upgrades, and release
  checklist.
- Add tests for backup/restore helper refusal cases, migration hook success and
  failure, startup after failed migration, and basic snapshot/compaction
  behavior.
- Add an integration test for offline backup/restore from a cleanly shut down
  runtime into a fresh data directory with recovered state verification.
- Add an integration test that restored data rejects an incompatible module
  contract without mutating the restored directory.
- Add release checklist items for `VERSION`, `CHANGELOG.md`, git tag, and
  linker-stamped build metadata to the operations runbook.
- Add a guardrail test that Shunter build metadata stays separate from
  app-owned `Module.Version(...)` contract metadata.
- Add durable data-dir metadata and compatibility tests for module-name
  mismatches and app module version updates.
- Add a reducer commit durability-failure recovery test proving an in-memory
  commit that fails before log append is not recovered after restart.
- Add runtime API tests for snapshot creation faults and compaction retry after
  a covered segment is deleted before sidecar cleanup.
- Ensure restore helper and CLI errors identify the backup source instead of
  generic source data-dir wording.
- Add an external canary workflow that backs up a stopped runtime, restores into
  a fresh data directory, runs an app-owned offline migration hook, verifies
  migration idempotence, and reads restored data through strict-auth protocol
  clients.

Remaining:

- Add the external canary operations command to release qualification once the
  release gate is pinned to a Shunter commit or tag.

## Verification

Run storage, commitlog, runtime, and command tests first, then:

```bash
rtk go test ./...
rtk go vet ./...
```

For any migration or durability behavior change, add a crash/recovery test or a
documented reason why the behavior cannot be tested at that layer.

## Done Criteria

- Operators have one documented path for backup, restore, migration, and
  upgrade.
- Data-dir compatibility checks are part of the recommended startup or release
  workflow.
- Backup and restore are covered by integration tests.
- Migration failure behavior is documented and tested.
- The external canary app demonstrates the full operational loop.

## Non-Goals

- Managed cloud operations.
- Automatic zero-downtime migrations.
- Database-level SQL migrations.
- Generic dynamic module loading by the `shunter` CLI.
