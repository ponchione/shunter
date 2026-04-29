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

Completed 2026-04-29.

Final coverage added:

- Added `runtime_read_auth_gauntlet_test.go` with a public-surface strict-auth
  runtime/protocol gauntlet that composes table read policy, declared reads,
  row-level visibility, subscriptions, joins, aggregates, limits, contract
  metadata, and protocol error behavior in one hosted runtime.
- The end-to-end gauntlet now proves:
  - default-private `secrets` rejects raw one-off and raw subscription reads
    while declared query/view reads over that private table succeed with
    declaration permissions.
  - public `messages` allows raw one-off and raw subscription reads while
    visibility filters restrict rows by caller identity plus a public filter.
  - permissioned `audit_logs` rejects callers without `audit:read` and accepts
    callers with the tag.
  - raw SQL equivalent to a declared read does not inherit declaration
    permissions, and SQL-shaped unknown declared names fail as unknown declared
    reads.
  - private tables referenced only as non-projected join participants are
    rejected before execution.
  - `SubscribeMulti` containing an unauthorized member returns an error and
    registers no visible query.
  - one-off reads, subscription initial rows, and subscription deltas expose
    only caller-visible rows.
  - two clients with different identities see different visible rows for the
    same raw SQL.
  - multiple filters OR together, self-joins apply visibility per alias, and a
    filtered non-projected join side cannot leak rows through participation.
  - aggregate count and limit operate after visibility filtering.
  - anonymous/dev protocol `AllowAllPermissions` bypasses private table policy,
    permissioned table policy, and visibility filters.
  - exported contracts include table read policy, declared read permissions,
    and visibility filter metadata for the gauntlet module.
- Existing focused coverage from Tasks 04, 06, 08, and 09 remains the pin for
  lower-level edge cases including generated declared-read callbacks,
  malformed policy metadata validation, contract diff/migration classification,
  raw SQL parse/type/error ordering, reducer permission behavior, and protocol
  lifecycle/backpressure/close behavior.

Behavior gaps found:

- No V2.5 behavior gaps were found.
- No read execution behavior, reducer permission behavior, or raw SQL admission
  behavior was changed for Task 10.
- The only adjustment during validation was making the new gauntlet's row-set
  assertions order-insensitive where SQL result order is not part of the
  contract; the limit assertion still proves the limited row is caller-visible.

Validation commands run:

```sh
rtk rg -n "Gauntlet|Protocol|Subscribe|OneOff|Generated|Contract|Visibility|Permission" runtime_gauntlet_test.go protocol codegen contractdiff .
rtk go list -json ./...
rtk go fmt .
rtk go test . -run 'TestRuntimeGauntletReadAuthorization' -count=1
rtk go test ./schema ./protocol ./subscription ./executor ./codegen ./contractdiff ./contractworkflow ./cmd/shunter -count=1
rtk go test . -run 'Test.*(Permission|Auth|Read|Query|View|Subscribe|Contract|Codegen|Gauntlet|Visibility)' -count=1
rtk go fmt ./...
rtk go vet ./...
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

Final validation status:

- Focused package gate passed: `1586` tests across `8` packages.
- Focused root read/auth/gauntlet gate passed: `272` tests.
- Full repository gate passed: `2960` tests across `16` packages.
- `rtk go vet ./...` reported no issues.
- `rtk go tool staticcheck ./...` passed.
