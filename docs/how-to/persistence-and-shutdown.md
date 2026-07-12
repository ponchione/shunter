# Persistence And Shutdown

Shunter persists runtime-owned state under `Config.DataDir`. Treat that
directory as an implementation-owned unit: copy it as a whole, restore it as a
whole, and never edit selected files by hand.

## Choose A DataDir

```go
rt, err := shunter.Build(app.Module(), shunter.Config{
	DataDir: "./data/chat",
})
```

Use separate data directories for separate applications, modules, tenants, or
incompatible schema lines.

A blank `DataDir` uses the runtime default `./shunter-data`. Set an explicit
directory for real services and tests that need predictable ownership. Do not
run two runtimes against the same `DataDir`.

## Startup

At startup, `Build` validates the module, opens or initializes durable state,
checks schema compatibility, and constructs runtime catalogs. `Start` begins
runtime-owned workers and lifecycle.

```go
rt, err := shunter.Build(mod, cfg)
if err != nil {
	return err
}
defer rt.Close()

if err := rt.Start(ctx); err != nil {
	return err
}
```

If startup recovery or schema validation fails, preserve the data directory and
investigate. Do not delete selected log or snapshot files to force startup.

## Graceful Shutdown

For a planned stop:

1. Stop admitting reducer calls and protocol traffic.
2. Wait for important transaction durability if the app needs that boundary.
3. Call `Runtime.Close`.
4. Run offline backup, restore, or migration tools only after `Close` returns.

```go
if err := rt.WaitUntilDurable(ctx, txID); err != nil {
	return err
}
if err := rt.Close(); err != nil {
	return err
}
```

`Close` shuts down runtime-owned lifecycle, durability, executor,
subscription, and protocol resources.

## Snapshot And Compaction

Create a snapshot when the app wants a full durable state image at the current
committed transaction horizon.

```go
snapshotTxID, err := rt.CreateSnapshot()
if err != nil {
	return err
}
if err := rt.CompactCommitLog(snapshotTxID); err != nil {
	return err
}
```

Snapshot creation is synchronous and can block commits while state is
serialized. Service processes should quiesce writes first.

`CompactCommitLog` only deletes sealed commit log segments fully covered by the
completed snapshot TX ID.

## Backup

Current v1 backup is offline-only.

Supported maintenance/recovery flow:

1. Stop accepting traffic.
2. Wait for required transactions to become durable, then stop the serving
   runtime cleanly.
3. In an app-owned maintenance process, recover the existing DataDir without
   starting normal runtime services, schedulers, startup migration hooks, or
   protocol serving. Create a snapshot, wait for its returned TX ID to be
   durable, and compact only with that completed snapshot TX ID.
4. Close the recovered maintenance state and require success before continuing.
5. Copy the entire `DataDir` offline.
6. Restore into a fresh DataDir, run module-linked compatibility preflight,
   start the compatible app, and verify application-visible reads before
   admitting traffic.
7. Retain the complete DataDir copy, matching module contract and module
   version, Shunter version/commit, backup timestamp, and build metadata.

The hosted-chat `prepare-backup` command and `TestMaintenanceRecoveryDrill`
provide the repository-local example. Preparation requires an existing
DataDir; a missing path or invalid output format fails before mutation.
Snapshot, compaction, close, backup, restore, or preflight errors stop the
sequence; preserve the source DataDir and the error for investigation rather
than deleting individual artifacts.

```go
if err := shunter.BackupDataDir("./data/chat", "./backups/chat-2026-05-04"); err != nil {
	return err
}
```

```bash
rtk go run ./cmd/shunter backup --data-dir ./data/chat --out ./backups/chat-2026-05-04
```

Backup refuses symlink sources, existing output paths, nested
destinations inside the source `DataDir`, symlink entries, unsupported special
files, and source files that change while being copied.

## Restore

Restore is also offline-only.

Recommended flow:

1. Stop the runtime that owns the destination directory.
2. Restore the complete backup into a missing or empty `DataDir`.
3. Start a compatible app binary against the restored directory.
4. Verify health and app-level smoke checks before admitting traffic.

```go
if err := shunter.RestoreDataDir("./backups/chat-2026-05-04", "./data/chat"); err != nil {
	return err
}
```

```bash
rtk go run ./cmd/shunter restore --backup ./backups/chat-2026-05-04 --data-dir ./data/chat
```

Restore refuses symlink backup sources, symlink or non-directory destinations,
nested destinations inside the backup source, symlink entries, unsupported
special files, and non-empty destinations.

## Compatibility Preflight

Use `CheckDataDirCompatibility` in app-owned maintenance tools when you want a
schema compatibility check before starting normal runtime services. Missing or
empty directories are compatible because `Build` can initialize fresh state
there.

```go
if err := shunter.CheckDataDirCompatibility(mod, shunter.Config{DataDir: "./data/chat"}); err != nil {
	return err
}
```

Use `CheckDataDirCompatibilityReport` when deployment tooling needs a
machine-readable preflight result. The report classifies exact matches, fresh
directories, safe additive changes, and blocked changes. Safe additive startup
currently covers schema-version-only drift, added tables, and appended
non-unique/non-primary indexes. Recovery preserves existing table IDs by name
and assigns new tables fresh IDs above the recovered snapshot maximum.
Row-shape changes, table drops, and new unique or primary constraints require
an app-owned migration plan. If recovery has durable log data but cannot select
a snapshot, schema-version drift is blocked because there is no persisted schema
map to reconcile table IDs safely.

Preflight CLIs must be app-owned binaries because they need to link the module
declarations directly:

```go
report, err := shunter.CheckDataDirCompatibilityReport(mod, shunter.Config{
	DataDir: "./data/chat",
})
if err != nil {
	return err
}
_ = report.Status
```

The hosted-chat example includes this shape under
`examples/hosted-chat/cmd/maintain`.

## Migrations

Migration hooks are app-owned code, not a general SQL migration engine.

Registered module hooks run during runtime startup after recovery and
durability are available, but before normal reducer, subscription, and protocol
readiness.

Offline tools can run registered hooks explicitly:

```go
result, err := shunter.RunModuleDataDirMigrations(ctx, mod, shunter.Config{
	DataDir: "./data/chat",
})
if err != nil {
	return err
}
_ = result.DurableTxID
```

Take an offline backup before data-rewrite migrations.

## More Detail

See [Operations](../operations.md) for the operator runbook and release
checklist.
