# Epic 7: Lifecycle Reducers & Client Management

**Parent:** [SPEC-003-executor.md](../SPEC-003-executor.md) §10  
**Blocked by:** Epic 4 (Reducer Execution), Epic 5 (Post-Commit Pipeline)  
**Blocks:** Nothing

---

## Stories

| Story | File | Summary |
|---|---|---|
| 7.1 | [story-7.1-sys-clients-table.md](story-7.1-sys-clients-table.md) | sys_clients schema, system table registration |
| 7.2 | [story-7.2-on-connect.md](story-7.2-on-connect.md) | OnConnect flow: insert row + run reducer + commit or full rollback |
| 7.3 | [story-7.3-on-disconnect.md](story-7.3-on-disconnect.md) | OnDisconnect flow: run reducer + delete row; cleanup tx on failure |
| 7.4 | [story-7.4-invocation-protection.md](story-7.4-invocation-protection.md) | Integration verification for the lifecycle guard implemented in Epic 4 |
| 7.5 | [story-7.5-startup-dangling-client-sweep.md](story-7.5-startup-dangling-client-sweep.md) | Startup-only cleanup of crash-leftover `sys_clients` rows before first accept |

## Implementation Order

```
Story 7.1 (Table schema)
  ├── Story 7.2 (OnConnect)
  ├── Story 7.3 (OnDisconnect)
  ├── Story 7.4 (Invocation protection verification) — after 7.2, 7.3
  └── Story 7.5 (Startup dangling-client sweep) — after 7.3 and scheduler replay
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 7.1 | `executor/sys_clients.go` |
| 7.2–7.3 | `executor/lifecycle.go`, `executor/lifecycle_test.go` |
| 7.4 | `executor/dispatch_reducer.go` (guard added to existing file) |
| 7.5 | `engine/startup.go`, `executor/lifecycle.go`, startup integration tests |
