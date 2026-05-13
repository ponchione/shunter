# Shunter Tech Debt

Status: future-work tracker
Scope: known non-blocking follow-up moved out of the retired v1 roadmap.

This file tracks work that should remain visible without making old release
roadmaps look active. Keep entries concise and implementation-facing. Prefer
live code, tests, focused docs, and Go doc when they disagree with this file.

## Performance Gaps

`docs/performance-envelopes.md` owns the current benchmark snapshot. The
remaining measurement gaps are:

- benchmark rows for slow-reader WebSocket writer/write-timeout backpressure
- external canary-scale fanout and workload-derived fanout distributions
- external canary workload timing, including canary-scale backup/restore
- production-sized memory profiles beyond the current local fixtures
- enough historical runs to decide whether any row should become a hard
  release gate instead of advisory data

Keep measured snapshots in `docs/performance-envelopes.md`; keep this section
limited to the missing evidence.

## External Canary Maintenance

The external `opsboard-canary` repository remains the integration proving
ground. Keep it on public Shunter APIs and package-shaped `@shunter/client`
installs. It should continue covering strict auth, permissions, visibility,
reducers, declared reads, raw SQL escape hatches, subscriptions,
restart/rollback, contract export, generated TypeScript, offline
backup/restore, and one app-owned migration path.

Do not add a duplicate in-repo reference app unless the product direction
changes.

## Hardening Follow-Up

Keep hardening work tied to reproducible failures and focused regression
coverage:

- add corpus entries, seeds, traces, commands, or fixtures when new failures
  are found
- extend crash/fault coverage across snapshot, compaction, migration, recovery,
  and shutdown boundaries
- expand subscription correctness scenarios for joins, deletes, updates,
  visibility changes, caller-specific subscriptions, and concurrent writes
- keep race-enabled package guidance current as ownership changes
- keep soak/load tests outside the short local loop, with commands that make
  failures attributable
