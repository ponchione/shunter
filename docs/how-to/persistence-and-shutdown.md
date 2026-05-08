# Persistence And Shutdown

Status: rough draft
Scope: `DataDir`, startup, shutdown, snapshots, compaction, backup, restore,
and migrations.

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

Do not run two runtimes against the same `DataDir`.

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

Recommended flow:

1. Stop accepting traffic.
2. Optionally create a snapshot and compact covered log segments.
3. Close the runtime.
4. Copy the entire `DataDir`.
5. Store the matching module contract and build metadata next to the backup.

```go
if err := shunter.BackupDataDir("./data/chat", "./backups/chat-2026-05-04"); err != nil {
	return err
}
```

```bash
rtk go run ./cmd/shunter backup --data-dir ./data/chat --out ./backups/chat-2026-05-04
```

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

Restore refuses to merge into a non-empty destination.

## Compatibility Preflight

Use `CheckDataDirCompatibility` in app-owned maintenance tools when you want a
schema compatibility check before starting normal runtime services.

```go
if err := shunter.CheckDataDirCompatibility(mod, shunter.Config{DataDir: "./data/chat"}); err != nil {
	return err
}
```

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

See `docs/operations.md` for the operator runbook and release checklist.
