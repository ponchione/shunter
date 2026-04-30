# Docs Guide

Keep this directory small. Do not add archives, stale handoffs, source-reading
logs, or one-off planning prompts.

## Startup Docs

Agent startup is intentionally narrow:

1. `RTK.md`
2. The active root handoff:
   - `NEXT_SESSION_HANDOFF.md` for future correctness / TECH-DEBT regressions
   - `HOSTED_RUNTIME_PLANNING_HANDOFF.md` for hosted-runtime work
   - `docs/RUNTIME-HARDENING-GAUNTLET.md` for the post-tech-debt runtime
     hardening test campaign
3. `TECH-DEBT.md` only for the active issue section named by the handoff or
   task
4. Only code, package docs, and narrow spec sections named by that handoff or
   by the slice being touched

Do not read broad roadmap, ledger, or full decomposition specs by default.

## Current Docs

- `docs/hosted-runtime-implementation-roadmap.md` — historical hosted-runtime
  phase tracker through completed V2.5; current active state belongs in the
  root handoff.
- `docs/RUNTIME-HARDENING-GAUNTLET.md` — post-tech-debt test campaign for
  model-based, fault-injected, fuzzed, and soak-tested runtime confidence.
- `docs/dependency-considerations.md` — adopted dependency policy, dependency
  candidates, and explicit dependency rejections.
- `docs/shunter-design-decisions.md` — consolidated current Shunter design
  decisions that code and tests still cite.
- `TECH-DEBT.md` — open issue list and follow-up ownership.

## Legacy Redirects

- `docs/parity-decisions.md` — short redirect kept for older links. Do not add
  new content there; update `docs/shunter-design-decisions.md` instead.

## Baseline Specs

- `docs/specs/` — broad baseline specs, execution-order docs, and archived
  hosted-runtime direction notes.
- `docs/features/` — feature implementation slices, task plans, and completion
  notes.
- `docs/adr/` and `docs/decisions/` — durable architecture decisions.

## Cleanup Rule

Prefer live code and tests over docs. If a doc stops being current and is not a
baseline spec, delete it or fold its current contract into the smallest active
doc. Do not keep history-only files in this tree.
