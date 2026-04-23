# Docs guide

This directory has three different doc classes. Do not treat them all as equally current.

## Active driver docs

Read these first when deciding what to work on now:
1. `docs/current-status.md` — short current-truth snapshot
2. `docs/project-brief.md` — original thesis / architecture intent
3. `docs/EXECUTION-ORDER.md` — implementation sequencing
4. `docs/spacetimedb-parity-roadmap.md` — active parity prioritization
5. `docs/parity-phase0-ledger.md` — compact scenario ledger
6. `docs/hosted-runtime-bootstrap.md` — current hosted bootstrap walkthrough
7. `TECH-DEBT.md` — open issues only
8. `NEXT_SESSION_HANDOFF.md` — current resume point only

## Stable reference / decision docs

These are worth keeping, but they are narrower than the active driver docs:
- `docs/parity-*.md` and `docs/parity-p0-*.md` — closed or targeted parity decision records
- `docs/hardening-oi-*.md` — narrow hardening slices backing open higher-level themes in `TECH-DEBT.md`
- `docs/adr/` — architectural decisions
- `docs/decisions/` — local design decisions
- `docs/archive/` — historical snapshots and source-reading notes that are not active drivers

## Decomposition docs

`docs/decomposition/` is the implementation decomposition and should stay usable as its own system of record.

## Cleanup rule

If a doc disagrees with live code or the active-driver set above, prefer:
1. live code
2. `docs/EXECUTION-ORDER.md`
3. `docs/spacetimedb-parity-roadmap.md`
4. `docs/parity-phase0-ledger.md`
5. `TECH-DEBT.md`

Older one-off assessments and old handoff docs should either be removed or clearly marked as historical snapshots.