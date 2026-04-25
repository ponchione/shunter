# Docs Guide

Keep this directory small. Do not add archives, stale handoffs, source-reading
logs, or one-off planning prompts.

## Startup Docs

Agent startup is intentionally narrow:

1. `RTK.md`
2. The active root handoff:
   - `NEXT_SESSION_HANDOFF.md` for parity / TECH-DEBT work
   - `HOSTED_RUNTIME_PLANNING_HANDOFF.md` for hosted-runtime work
3. Only code, package docs, and narrow spec sections named by that handoff or
   by the slice being touched

Do not read broad roadmap, ledger, or full decomposition specs by default.

## Current Docs

- `docs/hosted-runtime-implementation-roadmap.md` — hosted-runtime phase tracker
  while that implementation track is active.
- `docs/parity-decisions.md` — consolidated current parity decisions that code
  and tests still cite.
- `TECH-DEBT.md` — open issue list and follow-up ownership.

## Baseline Specs

- `docs/decomposition/` — implementation specs, epics, and tasks.
- `docs/hosted-runtime-planning/` — active hosted-runtime implementation plans.
- `docs/adr/` and `docs/decisions/` — durable architecture decisions.

## Cleanup Rule

Prefer live code and tests over docs. If a doc stops being current and is not a
baseline spec, delete it or fold its current contract into the smallest active
doc. Do not keep history-only files in this tree.
