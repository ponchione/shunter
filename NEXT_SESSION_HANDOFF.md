# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

For provenance of closed slices, use `git log` — this file tracks only current state and forward motion.

## Current state

- All OIs referenced in the 2026-04-22 audit chain (OI-008 through OI-012) are closed.
- Follow-on queue item for subscription `IndexRange` migration is closed.
- Follow-on queue item for subscription fan-out wiring in `cmd/shunter-example` is closed.
- Closed-slice provenance, detailed verification history, and implementation narratives live in `rtk git log`.
- Before starting a new slice, verify any remembered closure claim against live code; this file is intentionally current-state only.

## Current active constraint from the last closed slice

- `cmd/shunter-example` still passes `nil` to `Executor.Startup` for scheduler wiring because `executor.Scheduler` still needs an exported accessor for the executor's unexported inbox channel.
- The example remains on anonymous auth; strict auth wiring is still out of scope for this queue.

## Next session: pick one narrow slice from the follow-on queue

OI-008 / OI-009 / OI-010 / OI-011 / OI-012 are all closed. Follow-on queue items #1 (IndexRange consumer migration) and #1 (subscription fan-out wiring in `cmd/shunter-example`) are both closed. No remaining `open` OIs. Pick one from the queue below, open no more than one at a time.

## Follow-on queue

1. **Expose executor inbox for scheduler wiring** — `NewScheduler(inbox chan<- ExecutorCommand, ...)` reaches the executor's unexported `inbox`. Production embedders that want sys_scheduled replay need an exported accessor (e.g. `Executor.SchedulerFor(tableID)` or `Executor.Inbox()`). Lets the OI-008 example pass a real `*Scheduler` to `Startup`.

Pick scope before starting. Do not open multiple OIs at once.

## Startup notes

- Read `CLAUDE.md` first, then `RTK.md` for command rules, then `docs/EXECUTION-ORDER.md` for sequencing.
- Use `git log` for slice provenance; this file is current-state only.
- Before changing a file, verify against live code — memory/ledger claims can drift.
