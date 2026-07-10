# Qualify The Current Development Line

Status: recommended first step

Promotion trigger: immediate release-owner decision to qualify or cut the
current `v1.1.1-dev` line.

Owners: root runtime, CLI, TypeScript client, release process

## Why

The latest formal canary record predates substantial hosted-app,
authentication, subscription, codegen, and performance work. Current local
commands pass, but the durable release ledger must bind qualification to the
exact commit and external canary state being released.

## Outcome

A recorded decision to release, defer, or reject the current development line,
with current evidence and residual risks.

## Work

1. Run the minimum command set from `../release-qualification.md` at a clean,
   recorded Shunter commit.
2. Run the external `opsboard-canary` quick and full gates at a recorded clean
   or explicitly documented commit.
3. Refresh representative performance rows when current behavior differs from
   the `v1.1.0` snapshot or when a claimed improvement needs evidence.
4. Record environment, toolchain, commands, logs, and failures in a new release
   qualification record.
5. Review the unreleased changelog as a user-facing release boundary rather
   than treating every merged change as equally notable.
6. Decide whether to tag the release or keep the source line on `-dev` with an
   explicit blocker.

## Non-Goals

- changing runtime behavior merely to make qualification pass
- public npm publication without the separate package-governance decision
- claiming production scale from local qualification

## Completion Evidence

- clean worktree and exact Shunter commit recorded
- all required in-repo commands pass
- external canary commands pass or have an accepted, explicit residual risk
- release ledger contains the new record and evidence paths
- release/tag decision is explicit
