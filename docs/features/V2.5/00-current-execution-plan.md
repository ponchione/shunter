# Hosted Runtime V2.5 Current Execution Plan

Status: complete

Goal: complete read authorization so external reads have comparable behavior
to Shunter's reducer permission enforcement and to the reference system's
private table / auth-aware query model.

Primary design authority:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`

## Target

V2.5 must make these statements true:

- raw SQL can only resolve/read tables allowed by the caller's table read
  policy
- unauthorized tables look unresolved to raw SQL callers
- private or unauthorized tables are rejected even when they appear only inside
  joins
- `QueryDeclaration` and `ViewDeclaration` are real named runtime read
  surfaces
- declaration permissions are enforced on named declared reads
- generated declaration helpers call named read surfaces, not raw SQL strings
- row-level visibility filters are attached to tables and applied before
  reads can leak data through query evaluation
- contracts, codegen, and diffs expose the relevant policy metadata

## Task Sequence

1. Reconfirm current stack and reference constraints.
2. Add schema table read policy, default-private semantics, and export metadata.
3. Extract shared permission checking for reducer and read authorization.
4. Enforce table policy in raw SQL one-off and subscription admission.
5. Add a runtime read catalog and local/runtime APIs for named declared reads.
6. Add protocol and generated-client support for named declared reads.
7. Add visibility filter declarations and build-time validation.
8. Refactor or extend read planning so visibility filters apply per relation
   before joins and evaluation.
9. Extend contracts, diffs, and migration policy classification.
10. Add gauntlet tests and run final validation gates.

## Phase Boundaries

Phase A, tasks 01-04, may land before declared reads and row-level visibility if
it fully protects raw SQL table access.

Phase B, tasks 05-06, must not weaken raw SQL table policy. Named declared
reads may intentionally expose data from private base tables, but only through a
named declaration path whose permissions are checked.

Phase C, tasks 07-08, must not post-filter only projected rows. Visibility must
apply to every relation participating in a read.

Tasks 09-10 close the feature. Do not call V2.5 complete without them.

## Completion Proof

Completed 2026-04-29.

V2.5 is complete. The final Task 10 gauntlet adds
`runtime_read_auth_gauntlet_test.go`, a public-surface strict-auth hosted
runtime/protocol test that composes raw table policy, named declared reads,
row-level visibility, contract metadata, joins, aggregates, limits,
subscriptions, deltas, `SubscribeMulti` atomicity, and the anonymous/dev
`AllowAllPermissions` bypass.

Final validation passed:

```sh
rtk go fmt ./...
rtk go test ./schema ./protocol ./subscription ./executor ./codegen ./contractdiff ./contractworkflow ./cmd/shunter -count=1
rtk go test . -run 'Test.*(Permission|Auth|Read|Query|View|Subscribe|Contract|Codegen|Gauntlet|Visibility)' -count=1
rtk go vet ./...
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

No V2.5 behavior gaps remain documented from the final gauntlet. Task 10 did
not change read execution behavior, reducer permission behavior, or raw SQL
admission behavior.

Accepted closeout caveats:

- Some lower-level checks remain intentionally covered by focused package tests
  rather than repeated in the final runtime gauntlet: generated declared-read
  callbacks, malformed metadata validation, contract diff and migration
  classification, raw SQL parse/type/error ordering, reducer permission
  behavior, and protocol lifecycle behavior.
- Generated TypeScript may still expose table-level raw SQL helper surfaces for
  private/default-private or permissioned tables. Those helpers do not grant
  execution authority; raw SQL admission remains server-enforced by table
  policy and visibility.

These caveats do not keep V2.5 open. Reopen this feature only for a confirmed
read-authorization behavior regression.

## Validation Posture

Each worker should:
- inspect live Go symbols with `rtk go doc` before editing unfamiliar code
- add failing tests before implementation
- keep changes scoped to the assigned task
- use `rtk go fmt` on touched packages
- run targeted package tests first
- run broader root/full tests when behavior crosses protocol, runtime, schema,
  subscription, contracts, or codegen

Final completion gates:

```sh
rtk go fmt ./...
rtk go test ./protocol ./schema ./subscription ./executor ./codegen ./contractdiff ./contractworkflow -count=1
rtk go test . -run 'Test.*(Permission|Auth|Read|Query|View|Subscribe|Contract|Codegen|Gauntlet)' -count=1
rtk go vet ./protocol ./schema ./subscription ./executor ./codegen ./contractdiff ./contractworkflow .
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```
