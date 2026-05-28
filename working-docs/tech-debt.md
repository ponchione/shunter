# Shunter Tech Debt

Status: future-work tracker
Scope: known non-blocking follow-up moved out of the retired v1 roadmap.

This file tracks work that should remain visible without making old release
roadmaps look active. Keep entries concise and implementation-facing. Prefer
live code, tests, focused docs, and Go doc when they disagree with this file.

## Performance Gaps

`docs/performance-envelopes.md` owns the current benchmark snapshot. The
remaining measurement gaps are:

- product-app and external-canary-scale fanout, including workload-derived
  fanout distributions
- product-app and external-canary workload timing, including canary-scale
  backup/restore
- production-sized memory profiles beyond the current local fixtures
- enough historical runs to decide whether any row should become a hard
  release gate instead of advisory data

Keep measured snapshots in `docs/performance-envelopes.md`; keep this section
limited to the missing evidence.

## Product And External Canary Maintenance

Kickbrass may serve as the primary product adoption canary while it is being
implemented on Shunter. Use it to validate real app pressure: API ergonomics,
generated TypeScript shape, auth, procedures/service adapters, persistence,
deployment, backup/restore, and operational workflows.

Do not add artificial Kickbrass features only to improve Shunter coverage. If a
Shunter edge case is not natural product behavior, cover it in package tests,
hosted-chat, or a synthetic/external canary.

When the external `opsboard-canary` repository is available, keep it on public
Shunter APIs and package-shaped `@shunter/client` installs. It should continue
covering broad regression surfaces that product apps may not naturally touch:
strict auth, permissions, visibility, reducers, declared reads, raw SQL escape
hatches, subscriptions, restart/rollback, contract export, generated
TypeScript, offline backup/restore, and one app-owned migration path.

Do not add a duplicate in-repo reference app unless the product direction
changes.

## Hosted App Productization

Keep hosted-app productization work here until it becomes a concrete release
slice:

- publish-ready `@shunter/client` workflow, including package ownership,
  public install docs, release instructions, provenance decisions, npm publish
  command policy, version synchronization, `dist/` artifact policy, and public
  package metadata
- standard static hosted-app template and deployment/runbook guidance
- Nuxt/browser integration guide and SSR lifecycle guidance
- dev workflow automation, such as rebuild/restart and TypeScript regeneration
  watchers, if real app work proves the manual flow too expensive

## Hardening Follow-Up

Keep hardening work tied to reproducible failures and focused regression
coverage:

- add corpus entries, seeds, traces, commands, or fixtures when new failures
  are found
- extend crash/fault coverage across snapshot, compaction, migration, recovery,
  and shutdown boundaries
- expand subscription correctness scenarios for joins, deletes, updates, and
  concurrent writes
- keep race-enabled package guidance current as ownership changes
- keep soak/load tests outside the short local loop, with commands that make
  failures attributable
