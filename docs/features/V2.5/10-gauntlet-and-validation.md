# V2.5 Task 10: Gauntlet And Final Validation

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Depends on:
- Tasks 02-09 complete

Objective: prove the full read authorization feature works end to end across
runtime, protocol, subscriptions, declared reads, generated clients, contracts,
and row-level visibility.

## Required Context

Read:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- completion notes from tasks 02-09
- `docs/RUNTIME-HARDENING-GAUNTLET.md` if extending runtime gauntlet tests

Inspect:

```sh
rtk rg -n "Gauntlet|Protocol|Subscribe|OneOff|Generated|Contract|Visibility|Permission" runtime_gauntlet_test.go protocol codegen contractdiff .
rtk go list -json ./...
```

## Required End-To-End Coverage

Add or confirm tests proving:

Table access:

- default-private table rejects raw one-off
- default-private table rejects raw subscription
- public table allows raw one-off and raw subscription
- permissioned table rejects/accepts based on caller tags
- `AllowAllPermissions` bypasses table policy
- unauthorized table inside join is rejected even when not projected
- `SubscribeMulti` unauthorized member registers no queries

Declared reads:

- declared query over private table succeeds with declaration permission
- declared view over private table subscribes with declaration permission
- missing declaration permission fails
- unknown declared read name fails without raw SQL fallback
- raw SQL equivalent does not inherit declaration permissions
- generated helpers use named declared read callbacks

Row-level visibility:

- one-off initial read sees only visible rows
- subscription initial state sees only visible rows
- subscription deltas are caller-visible only
- two clients with different identities see different rows
- multiple filters OR together
- self-join applies visibility per alias
- non-projected filtered join side cannot leak through participation
- aggregate and limit respect visibility
- `AllowAllPermissions` bypasses visibility

Contracts and tooling:

- contract export includes policy metadata
- contract validation rejects invalid policy metadata
- contract diff classifies policy changes
- codegen exposes/uses declared read metadata correctly
- CLI contract workflows still work

Regression:

- existing reducer permission behavior is unchanged
- existing raw SQL parse/type/error ordering remains pinned
- existing unknown table and SQL-wrapped subscription error text remains pinned
- protocol close/backpressure/lifecycle tests remain green

## Validation Gates

Run focused gates first:

```sh
rtk go test ./schema ./protocol ./subscription ./executor ./codegen ./contractdiff ./contractworkflow ./cmd/shunter -count=1
rtk go test . -run 'Test.*(Permission|Auth|Read|Query|View|Subscribe|Contract|Codegen|Gauntlet|Visibility)' -count=1
```

Then run final gates:

```sh
rtk go fmt ./...
rtk go vet ./...
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

If Staticcheck reports failures caused by pre-existing unrelated work, record
the exact output and owner rather than hiding it.

## Completion Notes

When complete, update:

- this file with validation outputs
- `docs/features/V2.5/00-current-execution-plan.md` with completion proof
- `HOSTED_RUNTIME_PLANNING_HANDOFF.md` with the final V2.5 state

V2.5 is not complete until this task's final validation passes or any residual
failure is explicitly documented as unrelated with concrete evidence.

