# Reference App Contract: Task Board

Status: retired planning note
Scope: historical in-repo reference-app proposal.

## Current Decision

Do not implement this task-board app for v1. The maintained canary/reference
application is the external `opsboard-canary` repository, whose active contract
lives in its `OPSBOARD_CANARY_APP_SPEC.md`.

This file remains only to explain the roadmap change and prevent future agents
from recreating a duplicate in-repo application. Keep Phase 2 work focused on
making Shunter and the external canary agree on public API behavior, generated
contracts, auth, subscriptions, recovery, backup/restore, migrations, and
release qualification.

## Historical Context

The task-board contract was drafted before the external canary repository was
available. Its intended coverage was realistic but overlaps with the
operations-board domain already implemented by `opsboard-canary`: varied table
schemas, public/private read policies, sender visibility filters,
permission-aware reducers, declared queries, declared live views, raw SQL as an
escape hatch, generated TypeScript artifacts, strict auth, restart behavior,
and operator workflows.

Future v1 work should improve `opsboard-canary` instead of adding a second
application with the same purpose inside this repository.
