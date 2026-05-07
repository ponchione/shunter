# Shunter Operations Runbook

Status: current v1 operations guidance
Scope: app-owned operation of a single Shunter runtime `DataDir`.

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

Recommended flow:

1. Stop admitting new traffic.
2. Optionally create a snapshot and compact covered log segments.
3. Call `Runtime.Close` and wait for success.
4. Copy the complete `DataDir` with `BackupDataDir` or `shunter backup`.
5. Store the matching app contract, app module version, Shunter version, commit,
   backup timestamp, and copied `shunter.datadir.json` next to the backup.

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

`BackupDataDir` refuses to copy into an existing output directory and refuses to
copy into a nested path inside the source data directory.

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

`RestoreDataDir` refuses to merge into a non-empty destination. Restore into a
fresh directory, then let `Build` validate compatibility.

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
rtk go run ./cmd/shunter contract diff --previous old.json --current shunter.contract.json
rtk go run ./cmd/shunter contract policy --previous old.json --current shunter.contract.json --strict
rtk go run ./cmd/shunter contract plan --previous old.json --current shunter.contract.json --validate
```

The generic CLI operates on existing contract JSON files. It does not inspect a
running module or load module code.

## Migrations

Current v1 migrations are app-owned hooks, not a general SQL migration engine.

Recommended flow:

1. Stop runtime ownership of the target `DataDir`.
2. Take an offline backup.
3. Run `CheckDataDirCompatibility` or a contract plan before changing data.
4. Run `RunModuleDataDirMigrations` or `RunDataDirMigrations` from an app-owned
   maintenance binary that links the module.
5. Restart the normal app binary and verify state through public reads.

Registered startup migration hooks run during runtime startup. Offline
migration binaries can run the same registered hooks without starting normal
runtime services.

Migration hooks should be deterministic, idempotent at the app level, and
explicit about failure. A failed hook blocks startup or returns an error from
the offline runner; do not continue deployment until the failure is understood.

## Release Checklist

Before cutting a Shunter release:

1. Confirm `VERSION` uses the intended v-prefixed SemVer value.
2. Update `CHANGELOG.md` for release-facing behavior.
3. Run the release qualification commands documented by the hardening plan.
4. Build release binaries with linker variables for Shunter build metadata:

```bash
rtk go build -ldflags "-X github.com/ponchione/shunter.Version=v1.0.0 -X github.com/ponchione/shunter.Commit=<git-sha> -X github.com/ponchione/shunter.Date=<utc-rfc3339>" ./cmd/shunter
```

5. Run the built command's `version` output and verify the stamped Shunter
   version, commit, date, and Go version.
6. Tag released versions with `vX.Y.Z`.
7. Keep normal post-release development on a `-dev` version unless cutting a
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
