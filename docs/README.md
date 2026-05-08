# Docs Guide

Keep this directory small. Do not add archives, stale handoffs, source-reading
logs, or one-off planning prompts.

## Startup Docs

Agent startup is intentionally narrow:

1. `RTK.md`
2. Only code, package docs, and narrow spec sections named by the task or by
   the surface being touched

Do not read broad specs by default.

## Current Docs

- `docs/getting-started.md` — first-pass app-author path for embedding
  Shunter in a Go application.
- `docs/concepts.md` — vocabulary and mental model for modules, runtimes,
  reducers, reads, contracts, protocol serving, and durable state.
- `docs/how-to/` — rough-draft task guides for module declarations, reducers,
  reads, protocol serving, auth, persistence, contracts/codegen, and testing.
- `docs/reference/` — compact reference notes for config, lifecycle, and read
  surface choices that Go doc alone does not teach.
- `docs/authentication.md` — current dev/strict auth behavior, principal
  derivation, permission mapping, visibility, key replacement, and production
  checklist.
- `docs/AUTH-COVERAGE.md` — audit of current strict-auth tests across protocol,
  local reducers, read admission, subscriptions, and visibility filters.
- `docs/PERFORMANCE-BENCHMARKS.md` — benchmark run instructions and current
  performance baselines for comparison across optimization work.
- `docs/dependency-considerations.md` — adopted dependency policy, dependency
  candidates, and explicit dependency rejections.
- `docs/future-features.md` — working list of Shunter-native feature tracks to
  revisit as real applications put pressure on the runtime.
- `docs/how-to-use-shunter.md` — app-author guide for embedding Shunter,
  declaring modules, running a runtime, serving protocol traffic, and exporting
  contracts.
- `docs/operations.md` — operator runbook for `DataDir` lifecycle,
  backup/restore, migrations, upgrades, and release checklist.
- `docs/shunter-design-decisions.md` — consolidated current Shunter design
  decisions that code and tests still cite.
- `docs/v1-compatibility.md` — current v1 support matrix for root APIs,
  protocol, contract JSON, codegen, read surfaces, and host status.
- `docs/v1-roadmap/README.md` — single active roadmap for remaining v1
  implementation drivers and release qualification.

## Baseline Specs

- `docs/specs/*/SPEC-*.md` — numbered subsystem implementation contracts.
- `docs/specs/hosted-runtime-*.md` and
  `docs/specs/APP-RUNTIME-LAYER-AND-USAGE-SURFACE.md` — compact hosted-runtime
  surface contracts.

## Cleanup Rule

Prefer live code and tests over docs. If a doc stops being current and is not a
baseline spec, delete it or fold its current contract into the smallest active
doc. Do not keep history-only files in this tree.
