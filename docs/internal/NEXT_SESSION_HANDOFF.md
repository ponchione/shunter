# Next Session Handoff

Use this file to start the next Shunter correctness / TECH-DEBT agent with no
prior chat context. Hosted-runtime planning uses
`docs/internal/HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead.

## Startup

Required reading before editing:

1. `RTK.md`
2. This file
3. `docs/internal/TECH-DEBT.md` only for the active issue section you are taking
4. `docs/RUNTIME-HARDENING-GAUNTLET.md` only when running the runtime hardening
   campaign

Then inspect live code with Go tools for the package you will touch:

```bash
rtk go list -json <pkg>
rtk go doc <pkg>
rtk go doc <pkg>.<Symbol>
```

Use `rtk` for every shell command, including git. Do not push unless explicitly
asked.

## Current Objective

OI-008 cleanup is closed. Staticcheck is now expected to be green:

```bash
rtk go tool staticcheck ./...
```

No fixed implementation slice is queued. The best next live TECH-DEBT target is
OI-007 replay-edge and scheduler restart behavior, unless the user explicitly
chooses a different open issue or asks for the runtime hardening gauntlet.

For OI-007, start from the issue section in `docs/internal/TECH-DEBT.md`, then
inspect only the relevant restart/replay code and tests:

- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`
- scheduler replay/firing tests under `executor/`

## Guardrails

- Do not reopen OI-002 or OI-003 without a fresh Shunter-visible failing
  example.
- OI-001 remaining work is conditional protocol follow-up only; do not widen it
  without a concrete client/runtime need.
- OI-005 is a lower-level raw read-view/snapshot lifetime discipline marker, not
  a hosted-runtime blocker.
- Keep SpacetimeDB reference usage scoped to design evidence. Shunter owns the
  final Go API, protocol, and runtime behavior.

## Validation

Use targeted tests first, then broaden when the touched surface warrants it:

```bash
rtk go fmt <touched packages>
rtk go test <touched packages> -count=1
rtk go vet <touched packages>
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

Pinned Staticcheck is no longer report-only after OI-008.
