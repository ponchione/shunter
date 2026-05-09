# Shunter Documentation

This directory holds current user-facing documentation for application authors
and operators. Implementation plans, subsystem specs, audits, source-reading
notes, and roadmap material live under `../working-docs/`.

## App-Author Learning Path

1. [Getting started](getting-started.md) gives the shortest path for embedding
   Shunter in a Go application.
2. [Concepts](concepts.md) defines the vocabulary used by the guides.
3. [How-to guides](how-to/README.md) cover specific integration tasks such as
   declaring modules, writing reducers, serving protocol traffic, configuring
   auth, persistence, contracts, and tests.
4. [Reference notes](reference/README.md) summarize config, lifecycle, and read
   surface choices that Go doc alone does not explain.

Use [authentication](authentication.md) for the full current auth contract,
[operations](operations.md) for backup/restore and release runbooks,
[performance envelopes](performance-envelopes.md) for the current advisory
benchmark snapshot, and [v1 compatibility](v1-compatibility.md) for the current
support matrix.

## Current Docs

- [Getting started](getting-started.md) - app-author onboarding flow.
- [Concepts](concepts.md) - modules, runtimes, reducers, reads, contracts,
  protocol serving, durable state, and trust boundaries.
- [How-to guides](how-to/README.md) - task-focused integration guides.
- [Reference notes](reference/README.md) - compact decision guides for exported
  app-facing surfaces.
- [Authentication](authentication.md) - dev/strict auth behavior, principals,
  permissions, visibility, key replacement, and production checklist.
- [Operations](operations.md) - `DataDir` lifecycle, backup/restore,
  migrations, upgrades, and release checklist.
- [Performance envelopes](performance-envelopes.md) - current advisory
  benchmark snapshot, workload fixtures, and known measurement gaps.
- [v1 compatibility](v1-compatibility.md) - support matrix for root APIs,
  protocol, contract JSON, codegen, read surfaces, and host behavior.

## Working Docs

Working docs are repository-internal implementation material. Consult them only
when an active task needs their contracts or when live code and Go doc do not
answer a dependency question.

- `../working-docs/specs/*/SPEC-*.md` - numbered subsystem implementation
  contracts.
- `../working-docs/specs/README.md` - scope note for the numbered subsystem
  contracts.
- `../working-docs/v1-roadmap/README.md` - active roadmap for remaining v1
  implementation drivers, auth coverage, hardening, performance status, and
  release qualification.
- `../working-docs/shunter-design-decisions.md` - consolidated implementation
  decisions that code and tests still cite.

## Maintenance Notes

Prefer live code, tests, and Go doc over stale prose. If a user-facing doc stops
being current, fold the current contract into the smallest active doc or delete
the obsolete page. Do not keep history-only files in `docs/`.
