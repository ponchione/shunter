# Docs Guide

Keep this directory small. Do not add archives, stale handoffs, source-reading
logs, or one-off planning prompts.

## Startup Docs

Agent startup is intentionally narrow:

1. `RTK.md`
2. `docs/RUNTIME-HARDENING-GAUNTLET.md` only when running the runtime hardening
   test campaign
3. Only code, package docs, and narrow spec sections named by the task or by
   the surface being touched

Do not read broad specs by default.

## Current Docs

- `docs/RUNTIME-HARDENING-GAUNTLET.md` — test campaign for model-based,
  fault-injected, fuzzed, and soak-tested runtime confidence.
- `docs/PERFORMANCE-BENCHMARKS.md` — benchmark run instructions and current
  performance baselines for comparison across optimization work.
- `docs/dependency-considerations.md` — adopted dependency policy, dependency
  candidates, and explicit dependency rejections.
- `docs/shunter-design-decisions.md` — consolidated current Shunter design
  decisions that code and tests still cite.

## Baseline Specs

- `docs/specs/*/SPEC-*.md` — numbered subsystem implementation contracts.
- `docs/specs/hosted-runtime-*.md` and
  `docs/specs/APP-RUNTIME-LAYER-AND-USAGE-SURFACE.md` — compact hosted-runtime
  surface contracts.

## Cleanup Rule

Prefer live code and tests over docs. If a doc stops being current and is not a
baseline spec, delete it or fold its current contract into the smallest active
doc. Do not keep history-only files in this tree.
