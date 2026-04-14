# Shunter

Read `RTK.md` first for command rules, then `docs/project-brief.md`, then `docs/EXECUTION-ORDER.md`.

## Current Repo State

This repo is still docs-first. There is no implementation code yet. The working source of truth is the docs tree: project brief, spec docs, decomposition epics/stories, and execution order.

## What to Use

- `docs/project-brief.md` = product and architectural intent
- `docs/EXECUTION-ORDER.md` = implementation sequencing and dependency gates
- `docs/decomposition/` = executable epic/story breakdown
- `reference/SpacetimeDB/` = read-only research input only

## Rules

- Keep the clean-room boundary: do not copy or port Rust code from `reference/SpacetimeDB/`.
- Treat `docs/EXECUTION-ORDER.md` as the sequencing authority when choosing what lands next.
- Keep the repo lean. Do not invent extra structure before the work actually needs it.
- When editing docs, keep them tight and operational.
- For any shell or git command, follow `RTK.md`: prefix it with `rtk`.

## Practical Default

Before starting a task, identify the exact spec/epic/story slice it belongs to and work only that slice unless explicitly asked to widen scope.