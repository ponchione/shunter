# Shunter Operations Runbook

This runbook describes the supported operator path for the current v1 line. It
is intentionally conservative: Shunter is embedded in an app-owned Go binary,
the app owns process supervision, and Shunter owns only the runtime state inside
`Config.DataDir`.

## Operating Model

- Run one Shunter runtime against one `DataDir` at a time.
- Treat `DataDir` as runtime-owned state. Do not edit, merge, or partially copy
  files inside it.
- Use separate data directories for separate applications, modules, tenants, or
  incompatible schema lines.
- Export module contracts from an app-owned binary that links the module.
  The generic `shunter` CLI does not dynamically load app modules.
- Use reducers as the write boundary. SQL mutation is not a v1 operator tool.

Current v1 operations are offline-first. Shunter has no online backup API, no
managed retention daemon, no zero-downtime migration orchestrator, and no
dynamic module loader.

## Data Directory Lifecycle

### Empty Bootstrap

For a fresh deployment:

1. Choose a durable `DataDir` outside temporary directories.
2. Build the runtime with the module and config that will own that directory.
3. Start the runtime.
4. Export and archive the module contract as a review artifact.

Missing or empty data directories are treated as compatible fresh starts.
Application startup may optionally call `CheckDataDirCompatibility` before
building the runtime, but `Build` also validates the module schema against
recovered durable state.

Successful builds write `shunter.datadir.json` inside the data directory. This
metadata records Shunter build metadata separately from app-owned module
metadata. Shunter uses it as a guardrail against opening a data directory with a
different module name, schema version, or contract metadata version; app module
version changes are recorded but do not by themselves block startup.

### Normal Restart

For a normal restart:

1. Start the same app module schema line against the existing `DataDir`.
2. Let `Build` and recovery validate schema compatibility.
3. Start the runtime only after recovery succeeds.

If recovery or schema validation fails, do not rewrite the data directory in
place. Capture the error, preserve the directory, and restore from a known-good
backup or restart with a compatible app binary.

### Graceful Shutdown

For a planned stop:

1. Stop admitting new reducer calls and protocol traffic.
2. Wait for important committed transactions with `Runtime.WaitUntilDurable`
   when the app needs a durable acknowledgement boundary.
3. Call `Runtime.Close` and wait for it to return.
4. Only after `Close` returns should offline backup, restore, or migration tools
   operate on the directory.

`Close` is the runtime-owned shutdown boundary. Process supervisors should treat
errors from startup, readiness, and shutdown as service failures that need
operator attention.

### Crash Recovery

For an unclean process exit:

1. Restart the app binary against the same `DataDir`.
2. Let recovery replay commit log state and validate snapshots.
3. Check runtime health/readiness before admitting traffic.

Recovery is expected to recover safe history or fail loudly. Do not delete log
or snapshot files manually to force startup unless you have first preserved a
full copy of the damaged directory for investigation.

## Snapshot And Compaction

Use `Runtime.CreateSnapshot` to write a full snapshot at the current committed
transaction horizon. The call is synchronous and can block commits while state
is serialized, so quiesce writes first when creating a maintenance point.

Use `Runtime.CompactCommitLog(snapshotTxID)` only with a completed snapshot TX
ID returned by `CreateSnapshot`. Compaction deletes sealed commit log segments
that are fully covered by that snapshot.

Current policy:

- Snapshot creation is app/operator initiated.
- Commit log compaction is manual.
- Shunter does not own automatic snapshot retention cleanup.
- Keep backups as complete `DataDir` copies, not selected snapshot or segment
  files.

## Backup

Backup is offline-only for v1.

Supported maintenance/recovery sequence:

1. Stop admitting new traffic.
2. Wait for the application-required durability boundary and stop the serving
   runtime cleanly.
3. Use an app-owned maintenance process to recover the existing DataDir without
   starting normal runtime services, schedulers, startup migration hooks, or
   protocol serving. It creates a snapshot, waits for the returned TX ID,
   compacts only against that completed snapshot ID, and closes cleanly. Missing
   DataDirs and invalid command formats must fail before mutation.
4. Copy the complete `DataDir` offline with `BackupDataDir` or `shunter backup`.
5. Restore into a fresh DataDir and run app-owned compatibility preflight.
6. Start the compatible app and verify application-visible state before
   admitting traffic.
7. Store the matching app contract, app module version, Shunter version,
   commit, backup timestamp, and copied `shunter.datadir.json` with the backup.

Every step is fail-stop: do not continue after snapshot, compaction, close,
copy, restore, or preflight failure. Preserve the complete source DataDir and
diagnostics. The app/process supervisor owns traffic drain, scheduling,
retention, RPO/RTO, backup storage, and restart admission; Shunter owns the
runtime state operations and their safety checks.

The canonical repository drill is `TestMaintenanceRecoveryDrill` in
`examples/hosted-chat/cmd/maintain`; the hosted-chat gate runs the same
app-owned preparation, backup, restore, preflight, restart, and declared-read
verification sequence.

Go helper:

```go
if err := shunter.BackupDataDir("./data/chat", "./backups/chat-2026-05-04"); err != nil {
	return err
}
```

CLI helper:

```bash
rtk go run ./cmd/shunter backup --data-dir ./data/chat --out ./backups/chat-2026-05-04
```

`BackupDataDir` copies into a same-parent staging directory, syncs every copied
file and directory, and atomically publishes the requested output only after
the staged tree is complete. A failed backup leaves no output path that can be
mistaken for a usable backup and can be retried. It refuses symlink sources,
existing output paths, nested destinations inside the source data directory,
symlink entries, unsupported special files, and source trees that change while
being copied. Staged directories remain owner-private and writable during the
copy; final source directory modes are applied deepest-first after verification.
This preserves readable, read-only directory trees without exposing incomplete
staged contents through their final permissions.

## Restore

Restore is also offline-only.

Recommended flow:

1. Stop the runtime that owns the destination directory.
2. Restore the complete backup into a missing or empty `DataDir`.
3. Start a compatible app binary against the restored directory.
4. Verify health/readiness and run app-level smoke checks before admitting
   normal traffic.

Go helper:

```go
if err := shunter.RestoreDataDir("./backups/chat-2026-05-04", "./data/chat"); err != nil {
	return err
}
```

CLI helper:

```bash
rtk go run ./cmd/shunter restore --backup ./backups/chat-2026-05-04 --data-dir ./data/chat
```

`RestoreDataDir` uses the same staged, directory-synced publication. A failed
restore leaves an initially missing destination missing and preserves an
initially empty destination as empty; partial restored state is never published.
It refuses symlink backup sources, symlink or non-directory destinations,
nested destinations inside the backup source, symlink entries, unsupported
special files, and non-empty destinations. Restore into a fresh directory, then
let `Build` validate compatibility.

## Contract Review And Upgrade

Shunter has two separate version concepts:

- Shunter runtime/tool version: `VERSION`, `shunter.CurrentBuildInfo()`, and
  `shunter version`.
- App module version: `Module.Version(...)` exported into `ModuleContract`.

Before deploying a module/schema change:

1. Export the previous and current module contracts.
2. Run `contract diff`, `contract policy`, and `contract plan`.
3. Treat blocking policy failures as release blockers unless an app-owned
   migration plan explicitly handles them.
4. Take an offline backup before any blocking or data-rewrite migration.
5. Deploy the app binary with linker-stamped Shunter build metadata.

Useful commands:

```bash
rtk go run ./cmd/shunter describe --contract shunter.contract.json
rtk go run ./cmd/shunter describe --contract shunter.contract.json --section tables --format json
rtk go run ./cmd/shunter contract validate --contract shunter.contract.json --format json
rtk go run ./cmd/shunter contract assert --contract shunter.contract.json --module chat --module-version v0.1.0 --contract-version 1 --schema-version 1 --tables 1 --reducers 1 --format json
rtk go run ./cmd/shunter health --contract shunter.contract.json --format json
rtk go run ./cmd/shunter contract diff --previous old.json --current shunter.contract.json
rtk go run ./cmd/shunter contract policy --previous old.json --current shunter.contract.json --strict
rtk go run ./cmd/shunter contract plan --previous old.json --current shunter.contract.json --validate
```

The generic CLI operates on existing contract JSON files and offline data
directories. `describe` gives a local summary of the exported app surface; it
does not inspect a running module or load module code. Use `--section` to
focus human or JSON output on one contract surface, and use JSON `counts` when
operator scripts need stable inventory data. Use `contract assert` when a gate
needs explicit module, module-version, contract-version, schema-version, or
surface-count expectations after local contract validation. JSON assertion
entries expose `value_type` plus typed `expected_string`/`actual_string` or
`expected_number`/`actual_number` fields for script-friendly comparisons, plus
`assertion_count` and `failure_count` aggregate fields. `contract
validate` reports whether the local contract artifact validates.
`health --contract` reports the same local artifact status in a health-shaped
envelope; it is not a live health or readiness probe.

## Migrations

Current v1 migrations are app-owned hooks, not a general SQL migration engine.

Recommended flow:

1. Stop runtime ownership of the target `DataDir`.
2. Take an offline backup.
3. Run `CheckDataDirCompatibilityReport`, `CheckDataDirCompatibility`, or a
   module-linked preflight helper before changing data.
4. Run `RunModuleDataDirMigrations` or `RunDataDirMigrations` from an app-owned
   maintenance binary that links the module.
5. Restart the normal app binary and verify state through public reads.

The DataDir compatibility report is the runtime-backed preflight. It classifies
fresh directories, exact matches, safe additive schema changes, and blocked
changes without mutating the directory. Safe additive startup currently covers
schema-version-only drift, added tables, and appended non-unique/non-primary
indexes. Recovery preserves existing table IDs by name and assigns new tables
fresh IDs above the recovered snapshot maximum. Row-shape changes, table drops,
and new unique or primary constraints remain blocked until an app-owned
migration plan rewrites or validates the stored data. Additive schema-version
changes require a selected snapshot; log-only recovery with no selected snapshot
is blocked because table ID reconciliation cannot be proven safe.

Preflight helpers must be app-owned binaries so they can link the app module
directly; the hosted-chat example keeps the maintained shape under
`examples/hosted-chat/cmd/maintain`.

Registered startup migration hooks run during runtime startup. Offline
migration binaries can run the same registered hooks without starting normal
runtime services.

Migration hooks should be deterministic, idempotent at the app level, and
explicit about failure. A failed hook blocks startup or returns an error from
the offline runner; do not continue deployment until the failure is understood.

## Release Checklist

Before cutting a Shunter release:

1. Confirm `VERSION` uses the intended v-prefixed SemVer value.
2. Confirm `go.mod` names the intended supported Go version and pinned
   `toolchain` value.
3. Update `CHANGELOG.md` for release-facing behavior.
4. Record release qualification evidence before tagging. The record should
   capture the Shunter ref, operator, date, command evidence, final result, and
   accepted residual risks.
5. Run and record the core Go checks:

```bash
rtk go test ./...
rtk go vet ./...
rtk go tool staticcheck ./...
```

6. Run and record the TypeScript runtime checks:

```bash
rtk npm --prefix typescript/client run test
rtk npm --prefix typescript/client run build
rtk npm --prefix typescript/client run pack:dry-run
rtk npm --prefix typescript/client run smoke:package
```

These TypeScript commands qualify the private/local package workflow. They do
not authorize public npm publishing. Public publishing requires a separate
promotion record that settles package ownership, release authority, npm access
policy, publish commands, package metadata including licensing, version
synchronization, and the `dist/` artifact rule.

7. Run the hosted example gate:

```bash
rtk ./scripts/hosted-chat-gate.sh
```

8. Refresh [performance envelopes](performance-envelopes.md) with the current
   advisory benchmark snapshot, host notes, Shunter commit, and remaining
   measurement gaps.

9. Build release binaries with linker variables for Shunter build metadata:

```bash
rtk go build -ldflags "-X github.com/ponchione/shunter.Version=v1.0.0 -X github.com/ponchione/shunter.Commit=<git-sha> -X github.com/ponchione/shunter.Date=<utc-rfc3339>" ./cmd/shunter
```

10. Run the built command's `version` output and verify the stamped Shunter
   version, commit, date, and Go version.
11. Tag released versions with `vX.Y.Z`.
12. Keep normal post-release development on a `-dev` version unless cutting a
   release.

## Unsupported Operations

These are outside the current v1 operations contract:

- online backups of an actively written `DataDir`
- merging a backup into an existing data directory
- partial restore of selected tables or segments
- automatic zero-downtime migrations
- SQL `INSERT`, `UPDATE`, or `DELETE` as migration tools
- dynamic module loading through `cmd/shunter`
- editing commit log, snapshot, or recovery metadata by hand
