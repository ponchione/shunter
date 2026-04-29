# OpsBoard Canary App Spec

Status: proposed external canary repository
Target repository name: `shunter-opsboard-canary`
Primary purpose: prove that a separate application can import Shunter and run
realistic hosted-runtime workflows through public APIs.

This document is written for a fresh agent working in a new repository. Treat it
as the implementation contract for the first external Shunter canary app.

## Objective

Build a small but non-trivial operations-board application that imports
`github.com/ponchione/shunter`, defines a real module, serves the hosted runtime,
drives protocol clients, and verifies realistic workflows.

The canary app should answer these questions:

- Can an outside Go module use Shunter without reaching into Shunter internals?
- Are schema, reducers, declared reads, raw reads, subscriptions, auth,
  visibility, scheduler behavior, persistence, contract export, and codegen
  usable together?
- Which missing or awkward Shunter surfaces block realistic app development?

This is not a product demo. It is a sustained integration pressure fixture.

## Hard Constraints

- Keep this app in a separate repository from Shunter.
- Do not vendor or copy Shunter source.
- Do not import Shunter `internal` packages or Shunter test helpers.
- Allowed Shunter imports are public packages only:
  - `github.com/ponchione/shunter`
  - `github.com/ponchione/shunter/auth`
  - `github.com/ponchione/shunter/bsatn`
  - `github.com/ponchione/shunter/codegen`
  - `github.com/ponchione/shunter/protocol`
  - `github.com/ponchione/shunter/schema`
  - `github.com/ponchione/shunter/types`
- Prefer a `go.work` workspace or a temporary `replace` directive during local
  development:

```sh
go mod edit -replace github.com/ponchione/shunter=../shunter
```

- CI should eventually test against a pinned Shunter commit or tag without a
  local replace.
- Do not add a frontend in the first milestone. A scripted protocol workflow is
  the first user.
- Use strict auth for the main workflow. Dev/anonymous mode may be used only for
  narrow smoke tests.
- Every workflow failure must print enough context to reproduce it: seed,
  operation index, caller, reducer/query/subscription, expected rows, observed
  rows, and data directory.

## Non-Goals

- Do not build a polished project-management product.
- Do not implement a general Shunter client SDK.
- Do not add features to Shunter as part of the canary repo unless a failing
  canary workflow proves the need first.
- Do not work around Shunter defects by duplicating runtime internals.
- Do not make broad SQL support a canary requirement. Use the SQL surface
  Shunter already supports.

## Repository Layout

Create the new repository with this layout:

```text
shunter-opsboard-canary/
  go.mod
  go.work                 # optional local-only workspace file; do not require in CI
  Makefile
  README.md
  contracts/
    shunter.contract.json # generated, committed after deterministic
                          # contract export is stable
  generated/
    typescript/           # generated bindings, committed once stable
  artifacts/              # ignored; traces, temporary contracts, logs
  cmd/
    server/
      main.go             # starts the canary runtime
    workflow/
      main.go             # runs scripted protocol workflows against server or in-process
    export-contract/
      main.go             # writes contracts/shunter.contract.json
    codegen/
      main.go             # regenerates generated/typescript from contract
  internal/
    app/
      module.go           # Shunter module definition
      schema.go           # table ids, schema definitions, indexes
      reducers.go         # reducer handlers
      reads.go            # declared queries/views and visibility filters
      rows.go             # row constructors and row decoders
      permissions.go      # permission tag constants
    client/
      protocol_client.go  # thin WebSocket protocol client
      tokens.go           # strict-auth JWT minting for test callers
      rows.go             # row decoding helpers for protocol responses
    model/
      model.go            # independent expected-state model
      apply.go            # model transition logic
      visible.go          # model visibility/read logic
    workflows/
      smoke_test.go       # in-process temp-dir workflow
      auth_visibility_test.go
      restart_test.go
      scheduler_test.go
      workflow.go         # shared scripted workflow runner
      trace.go            # reproducible trace artifact format
```

The first pass may omit `generated/typescript/` and the optional frontend, but
the contract/codegen commands must exist by Milestone 3.

## Module Setup

Initialize:

```sh
go mod init github.com/ponchione/shunter-opsboard-canary
go get github.com/ponchione/shunter
go get github.com/coder/websocket
go get github.com/golang-jwt/jwt/v5
```

During local development next to a Shunter checkout:

```sh
go mod edit -replace github.com/ponchione/shunter=../shunter
go mod tidy
```

Add a `Makefile` with at least:

```makefile
.PHONY: test smoke contract codegen race

test:
	go test ./... -count=1

smoke:
	go test ./internal/workflows -run TestSmokeWorkflow -count=1

contract:
	go run ./cmd/export-contract > contracts/shunter.contract.json

codegen:
	go run ./cmd/codegen

race:
	go test -race ./internal/workflows -count=1
```

## Application Domain

The application is an issue tracker / operations board.

Domain objects:

- users
- projects
- project members
- tickets
- comments
- notifications
- audit log entries
- ticket reminders

Keep the app deliberately small. The value comes from cross-surface behavior,
not from product breadth.

## Identity Model

Use Shunter strict auth in the main workflow.

Callers:

- `admin`
- `alice`
- `bob`
- `carol`
- `auditor`
- `outsider`

Derive caller identities with `auth.DeriveIdentity(issuer, subject)` and store
the identity as `types.Identity.Hex()` in application rows.

Use string identity columns rather than bytes columns for app-level ownership
checks because current visibility filters compare `:sender` against string
columns in existing Shunter coverage.

Required token helper:

- package: `internal/client`
- function: `MintStrictToken(subject string, permissions ...string) (token string, identity types.Identity, err error)`
- implementation: use `github.com/golang-jwt/jwt/v5`
- claims:
  - `iss`: fixed canary issuer, for example `opsboard-canary`
  - `sub`: caller subject
  - `aud`: fixed audience, for example `opsboard`
  - `iat`: deterministic or current time
  - `exp`: short but sufficient test TTL
  - `identity`: derived identity hex
  - `permissions`: string array

Runtime strict auth config:

```go
shunter.Config{
    DataDir:        dataDir,
    EnableProtocol: true,
    ListenAddr:     listenAddr,
    AuthMode:       shunter.AuthModeStrict,
    AuthSigningKey: []byte("opsboard-canary-dev-signing-key"),
    AuthAudiences:  []string{"opsboard"},
}
```

## Permission Tags

Define constants in `internal/app/permissions.go`:

```go
const (
    PermAdminWrite  = "admin:write"
    PermProjectWrite = "project:write"
    PermTicketWrite = "ticket:write"
    PermCommentWrite = "comment:write"
    PermAuditRead   = "audit:read"
)
```

Main caller permissions:

- `admin`: all permissions
- `alice`: `project:write`, `ticket:write`, `comment:write`
- `bob`: `ticket:write`, `comment:write`
- `carol`: `comment:write`
- `auditor`: `audit:read`
- `outsider`: no permissions

Reducer permission metadata must use `shunter.WithReducerPermissions(...)`.
Declared audit reads must use `shunter.PermissionMetadata{Required:
[]string{PermAuditRead}}`.

## Schema

Define stable table IDs in registration order. User table IDs start at zero in
current Shunter module registration, matching existing runtime gauntlet usage.

```go
const (
    TableUsers schema.TableID = iota
    TableProjects
    TableProjectMembers
    TableTickets
    TableComments
    TableNotifications
    TableAuditLog
    TableTicketReminders
)
```

Every table must have a primary key. Prefer `uint64` IDs and string identity
columns.

### users

Read policy: public.

Columns:

- `id` `uint64` primary key
- `identity` `string` unique index
- `handle` `string` unique index
- `display_name` `string`
- `role` `string`
- `active` `bool`

Indexes:

- unique `users_identity` on `identity`
- unique `users_handle` on `handle`

### projects

Read policy: public.

Columns:

- `id` `uint64` primary key
- `slug` `string` unique index
- `name` `string`
- `visibility` `string`
- `owner_identity` `string`
- `created_at_ns` `int64`

Allowed `visibility` values:

- `public`
- `internal`

### project_members

Read policy: default private. Do not mark public.

Columns:

- `id` `uint64` primary key
- `project_id` `uint64`
- `user_identity` `string`
- `role` `string`
- `created_at_ns` `int64`

Indexes:

- `project_members_project` on `project_id`
- `project_members_user` on `user_identity`

### tickets

Read policy: public, with visibility filters.

Columns:

- `id` `uint64` primary key
- `project_id` `uint64`
- `number` `uint64`
- `title` `string`
- `status` `string`
- `priority` `string`
- `visibility` `string`
- `reporter_identity` `string`
- `assignee_identity` `string`
- `updated_at_ns` `int64`

Allowed `status` values:

- `open`
- `in_progress`
- `blocked`
- `closed`

Allowed `priority` values:

- `low`
- `normal`
- `high`

Allowed `visibility` values:

- `public`
- `internal`

Indexes:

- `tickets_project` on `project_id`
- `tickets_assignee` on `assignee_identity`
- `tickets_reporter` on `reporter_identity`
- unique `tickets_project_number` on `project_id`, `number`

### comments

Read policy: public, with visibility filters.

Columns:

- `id` `uint64` primary key
- `ticket_id` `uint64`
- `project_id` `uint64`
- `author_identity` `string`
- `visibility` `string`
- `body` `string`
- `created_at_ns` `int64`

Allowed `visibility` values:

- `public`
- `internal`

Indexes:

- `comments_ticket` on `ticket_id`
- `comments_author` on `author_identity`

### notifications

Read policy: default private. Expose only through declared read/view paths with
visibility filtering.

Columns:

- `id` `uint64` primary key
- `recipient_identity` `string`
- `ticket_id` `uint64`
- `kind` `string`
- `body` `string`
- `delivered` `bool`
- `created_at_ns` `int64`

Indexes:

- `notifications_recipient` on `recipient_identity`

### audit_log

Read policy: permissioned with `audit:read`.

Columns:

- `id` `uint64` primary key
- `actor_identity` `string`
- `action` `string`
- `target_table` `string`
- `target_id` `uint64`
- `body` `string`
- `created_at_ns` `int64`

Indexes:

- `audit_actor` on `actor_identity`
- `audit_target` on `target_table`, `target_id`

### ticket_reminders

Read policy: default private.

Columns:

- `id` `uint64` primary key
- `ticket_id` `uint64`
- `recipient_identity` `string`
- `schedule_id` `uint64`
- `due_at_ns` `int64`
- `fired` `bool`

Indexes:

- `reminders_ticket` on `ticket_id`
- `reminders_recipient` on `recipient_identity`

## Visibility Filters

Register visibility filters in `internal/app/reads.go`.

Current Shunter visibility filters must be single-table table-returning SQL.
Do not use joins, projections, aggregates, or limits in these filters.

Required filters:

```text
ticket_reporter:
  SELECT * FROM tickets WHERE reporter_identity = :sender

ticket_assignee:
  SELECT * FROM tickets WHERE assignee_identity = :sender

ticket_public:
  SELECT * FROM tickets WHERE visibility = 'public'

comment_author:
  SELECT * FROM comments WHERE author_identity = :sender

comment_public:
  SELECT * FROM comments WHERE visibility = 'public'

notification_recipient:
  SELECT * FROM notifications WHERE recipient_identity = :sender
```

The workflow must prove multiple filters OR together for `tickets` and
`comments`.

## Declared Reads

Current Shunter declared reads are named and non-parameterized. This canary
must work within that constraint. Use fixed seed IDs for named read coverage,
and use raw SQL for dynamic ad hoc reads where table policy permits it.

Queries:

- `my_profile`
  - SQL: `SELECT * FROM users WHERE identity = :sender`
  - no permissions
- `my_tickets`
  - SQL: `SELECT * FROM tickets WHERE assignee_identity = :sender`
  - no permissions
- `alpha_board`
  - SQL: `SELECT * FROM tickets WHERE project_id = 1`
  - no permissions
- `ticket_100_detail`
  - SQL: `SELECT * FROM tickets WHERE id = 100`
  - no permissions
- `audit_feed`
  - SQL: `SELECT * FROM audit_log`
  - permissions: `audit:read`

Views:

- `live_alpha_board`
  - SQL: `SELECT * FROM tickets WHERE project_id = 1`
  - no permissions
- `live_ticket_100_comments`
  - SQL: `SELECT * FROM comments WHERE ticket_id = 100`
  - no permissions
- `my_notifications`
  - SQL: `SELECT * FROM notifications WHERE recipient_identity = :sender`
  - no permissions
- `live_audit_feed`
  - SQL: `SELECT * FROM audit_log`
  - permissions: `audit:read`

Required declared-read assertions:

- Unknown declared read name fails and does not fall back to raw SQL.
- Raw SQL equivalent to `audit_feed` does not inherit declared permissions.
- `auditor` can call/subscribe audit declared reads.
- `alice` cannot call/subscribe audit declared reads.
- Private `notifications` is readable through `my_notifications`, but raw SQL
  `SELECT * FROM notifications` fails for strict-auth users.

## Reducers

Reducers must parse JSON args. This is deliberate: Shunter reducer argument
schema/export is not yet a canary goal, and JSON keeps workflow traces readable.

Each reducer must:

- validate required fields
- reject invalid enum values
- write an audit row for committed business actions
- return an error before mutation when validation fails
- never retain `*schema.ReducerContext`
- never perform network or disk I/O

Required reducers:

### bootstrap_demo

Permission: `admin:write`

Args:

```json
{"now_ns": 1000}
```

Behavior:

- inserts deterministic users for `admin`, `alice`, `bob`, `carol`, `auditor`,
  and `outsider`
- inserts project `1` with slug `alpha`
- inserts fixed ticket `100` in project `1`
- inserts at least one public ticket and one internal ticket
- inserts one public comment and one internal comment
- inserts audit rows for bootstrap actions

This reducer must be idempotent enough for tests to call it once per empty data
directory. It may reject a second call with a clear user error.

### create_project

Permission: `project:write`

Args:

```json
{"id":2,"slug":"beta","name":"Beta","visibility":"internal","now_ns":1100}
```

Behavior:

- inserts a project
- inserts caller as owner/member
- writes audit row

### add_project_member

Permission: `project:write`

Args:

```json
{"id":10,"project_id":1,"user_identity":"...","role":"member","now_ns":1200}
```

Behavior:

- rejects if project missing
- inserts member
- writes audit row

### create_ticket

Permission: `ticket:write`

Args:

```json
{
  "id":101,
  "project_id":1,
  "number":2,
  "title":"Investigate failed webhook",
  "priority":"high",
  "visibility":"internal",
  "assignee_identity":"...",
  "now_ns":1300
}
```

Behavior:

- reporter is `ctx.Caller.Identity.Hex()`
- initial status is `open`
- inserts ticket
- if assignee is non-empty, inserts a notification for assignee
- writes audit row

### assign_ticket

Permission: `ticket:write`

Args:

```json
{"ticket_id":101,"assignee_identity":"...","now_ns":1400}
```

Behavior:

- rejects if ticket missing
- updates assignee and `updated_at_ns`
- inserts notification for new assignee
- writes audit row

### change_ticket_status

Permission: `ticket:write`

Args:

```json
{"ticket_id":101,"status":"in_progress","now_ns":1500}
```

Behavior:

- rejects invalid status
- updates status and `updated_at_ns`
- inserts notification for reporter and assignee when they differ from caller
- writes audit row

### add_comment

Permission: `comment:write`

Args:

```json
{"id":501,"ticket_id":101,"visibility":"internal","body":"Looking now","now_ns":1600}
```

Behavior:

- rejects if ticket missing
- author is `ctx.Caller.Identity.Hex()`
- copies `project_id` from ticket
- inserts comment
- inserts notification for reporter and assignee when they differ from caller
- writes audit row

### close_ticket

Permission: `ticket:write`

Args:

```json
{"ticket_id":101,"now_ns":1700}
```

Behavior:

- sets status `closed`
- writes audit row

### reopen_ticket

Permission: `ticket:write`

Args:

```json
{"ticket_id":101,"now_ns":1800}
```

Behavior:

- sets status `open`
- writes audit row

### schedule_ticket_reminder

Permission: `ticket:write`

Args:

```json
{
  "id":700,
  "ticket_id":101,
  "recipient_identity":"...",
  "delay_ms":50,
  "now_ns":1900
}
```

Behavior:

- inserts a `ticket_reminders` row
- uses `ctx.Scheduler.Schedule("fire_ticket_reminder", args, dueTime)`
- updates the row with returned schedule ID if needed
- writes audit row

### fire_ticket_reminder

Permission: none for the first canary pass.

Reason: current Shunter does not expose an internal-only scheduled reducer
surface in the public app API. Keep this reducer idempotent and safe. Record
this as product pressure if it becomes awkward.

Args:

```json
{"reminder_id":700}
```

Behavior:

- no-ops if reminder missing or already fired
- marks reminder fired
- inserts notification for reminder recipient
- writes audit row with actor `system`

### fail_after_audit_probe

Permission: `admin:write`

Purpose: prove rollback.

Behavior:

- attempts to insert an audit row, then returns an error
- workflow must prove the audit row did not commit

### panic_after_ticket_probe

Permission: `admin:write`

Purpose: prove panic rollback.

Behavior:

- attempts to insert a ticket, then panics
- workflow must prove the ticket did not commit

## Protocol Client

Implement a thin client in `internal/client` using `github.com/coder/websocket`
and Shunter `protocol` package codecs.

The client must support:

- dialing a runtime URL with bearer token
- `CallReducer(name string, args any, requestID uint32, flags byte)`
- `OneOff(sql string, messageID []byte)`
- `DeclaredQuery(name string, messageID []byte)`
- `SubscribeSingle(sql string, requestID uint32, queryID uint32)`
- `SubscribeMulti(sqls []string, requestID uint32, queryID uint32)`
- `SubscribeDeclaredView(name string, requestID uint32, queryID uint32)`
- `UnsubscribeSingle(requestID uint32, queryID uint32)`
- `UnsubscribeMulti(requestID uint32, queryID uint32)`
- reading and decoding server frames with `protocol.DecodeServerMessage`

Use these public message types:

- `protocol.CallReducerMsg`
- `protocol.OneOffQueryMsg`
- `protocol.DeclaredQueryMsg`
- `protocol.SubscribeSingleMsg`
- `protocol.SubscribeMultiMsg`
- `protocol.SubscribeDeclaredViewMsg`
- `protocol.UnsubscribeSingleMsg`
- `protocol.UnsubscribeMultiMsg`

Frame encoding must use `protocol.EncodeClientMessage`.

Row decoding:

- use `protocol.DecodeRowList` for row batches
- use `bsatn.DecodeProductValue` with schema from the exported contract or
  runtime description
- convert rows into app structs before asserting

## Independent Model

Implement `internal/model` as an independent app-level model. It must not call
Shunter store, executor, subscription, or protocol internals.

The model should store maps keyed by primary key:

- `Users map[uint64]User`
- `Projects map[uint64]Project`
- `Members map[uint64]ProjectMember`
- `Tickets map[uint64]Ticket`
- `Comments map[uint64]Comment`
- `Notifications map[uint64]Notification`
- `AuditLog map[uint64]AuditEntry`
- `Reminders map[uint64]Reminder`

The model must implement:

- reducer transition functions matching committed reducer behavior
- no mutation for failed reducers
- no mutation for panic reducers
- visible ticket/comment/notification rows for a caller identity
- expected audit visibility for `audit:read`
- expected subscription deltas as `after - before`

Do not over-generalize the model. It only needs to cover the canary workflows.

## Required Workflows

All workflows must run in tests under `internal/workflows`.

### TestSmokeWorkflow

Runs an in-process runtime with strict auth and temp data directory.

Steps:

1. Build module.
2. Start runtime.
3. Export contract and validate it can be marshaled.
4. Dial `admin`, `alice`, `bob`, `carol`, `auditor`, and `outsider`.
5. `admin` calls `bootstrap_demo`.
6. Assert model and runtime state agree via raw public reads and declared reads.
7. Close all clients and runtime cleanly.

### TestAuthVisibilityWorkflow

Required assertions:

- `outsider` raw `SELECT * FROM tickets` sees only public tickets.
- `alice` sees tickets where she is reporter or assignee plus public tickets.
- `bob` sees tickets assigned to him plus public tickets.
- `auditor` can raw query `SELECT * FROM audit_log`.
- `alice` cannot raw query `SELECT * FROM audit_log`.
- `alice` cannot raw query `SELECT * FROM notifications`.
- `bob` can subscribe to declared view `my_notifications` and sees only his
  notifications.
- `alice` cannot call declared query `audit_feed`.
- `auditor` can call declared query `audit_feed`.
- unknown declared query name `SELECT * FROM audit_log` fails as unknown
  declared read, not as raw SQL.

### TestSubscriptionWorkflow

Required assertions:

- `bob` subscribes to `live_alpha_board`.
- `alice` creates ticket `101` assigned to `bob`.
- `bob` receives an insert/update delta for ticket `101`.
- `carol` does not see internal ticket `101` until assigned or public.
- `alice` assigns ticket `101` to `carol`.
- `bob` receives a delete or changed visibility effect if no longer visible.
- `carol` receives an insert/update for newly visible ticket.
- `bob` subscribes to `my_notifications`.
- assigning/commenting creates notification deltas only for intended recipient.

### TestSubscribeMultiAtomicWorkflow

Required assertions:

- `alice` sends SubscribeMulti with:
  - `SELECT * FROM tickets`
  - `SELECT * FROM project_members`
- request fails because `project_members` is private.
- no query from that SubscribeMulti remains registered.
- a later reducer mutation does not deliver deltas for the rejected batch.
- same connection can recover by successfully subscribing afterward.

### TestDeclaredReadWorkflow

Required assertions:

- `my_profile` returns exactly caller profile.
- `my_tickets` applies both declaration predicate and visibility.
- `alpha_board` applies visibility per caller.
- `ticket_100_detail` does not expose internal ticket to unrelated caller.
- `live_ticket_100_comments` applies comment visibility.
- raw SQL equivalent to a private declared notifications view fails.

### TestRestartWorkflow

Required assertions:

1. Start runtime in temp data directory.
2. Bootstrap and run several reducers.
3. Close runtime.
4. Rebuild runtime against the same data directory.
5. Assert:
   - users/projects/tickets/comments/audit rows recovered
   - declared reads still return expected rows
   - raw reads still enforce policy
   - new subscriptions after restart get correct initial rows

### TestSchedulerWorkflow

Required assertions:

- `alice` schedules reminder for `bob`.
- Runtime remains live until reminder fires.
- `bob` receives notification delta.
- A second reminder scheduled before restart fires after restart.
- Fired reminders do not fire twice.

### TestRollbackWorkflow

Required assertions:

- `fail_after_audit_probe` returns failed reducer result.
- audit row attempted by failed reducer is absent.
- `panic_after_ticket_probe` returns failed reducer result.
- ticket attempted by panicking reducer is absent.
- subscribers do not receive deltas from failed/panicking reducers.

## Command-Line Behavior

### cmd/server

Required flags:

- `--data-dir`
- `--listen`
- `--strict`
- `--signing-key`
- `--audience`

Behavior:

- builds the OpsBoard module
- starts the runtime
- serves protocol
- handles SIGINT/SIGTERM with clean `Runtime.Close`

### cmd/workflow

Required modes:

- `--in-process`
- `--addr`
- `--data-dir`
- `--seed`
- `--trace-out`

Behavior:

- in-process mode starts its own runtime
- addr mode connects to an already running server
- runs the same scripted workflows as tests
- writes trace artifact on failure

### cmd/export-contract

Behavior:

- builds module
- exports contract JSON
- writes to stdout
- must be deterministic

### cmd/codegen

Behavior:

- reads `contracts/shunter.contract.json`
- writes TypeScript bindings under `generated/typescript`
- uses Shunter public codegen package

## Contract And Codegen Requirements

The canary must commit a generated contract once deterministic.

Contract assertions:

- table read policies are present
- `project_members`, `notifications`, and `ticket_reminders` are private
- `audit_log` is permissioned with `audit:read`
- declared query/view permissions are present for audit reads
- visibility filter metadata includes expected filters and `uses_caller_identity`
- migration metadata is present enough for diff tooling to have stable input

Codegen assertions:

- generated declared query helpers call named declared query callbacks
- generated declared view helpers call named declared view callbacks
- raw SQL helper surfaces remain separate from declared read helpers
- generated metadata includes table read policy and visibility filter maps

Do not build app logic on generated TypeScript in Milestone 1. Generation is a
contract surface check first.

## Milestones

### Milestone 0: Skeleton

Deliver:

- repo layout
- Go module
- Makefile
- empty module builds
- `go test ./...` passes

Exit criteria:

- no Shunter internal imports
- Shunter dependency can be local-replaced

### Milestone 1: Module And Contract

Deliver:

- schema tables
- reducers registered
- declared reads registered
- visibility filters registered
- strict-auth config helper
- contract export command

Exit criteria:

- `go test ./internal/app -count=1` passes
- `go run ./cmd/export-contract` emits deterministic JSON

### Milestone 2: Protocol Client

Deliver:

- strict token minting
- WebSocket dial
- message encode/decode
- reducer call
- one-off query
- declared query
- single and declared subscriptions

Exit criteria:

- protocol smoke test can bootstrap data and read visible rows

### Milestone 3: Core Workflows

Deliver:

- smoke workflow
- auth/visibility workflow
- subscription workflow
- SubscribeMulti atomic workflow
- declared read workflow

Exit criteria:

- `go test ./internal/workflows -count=1` passes
- failure traces are useful

### Milestone 4: Restart And Scheduler

Deliver:

- restart workflow
- scheduler reminder workflow
- rollback workflow

Exit criteria:

- `go test ./internal/workflows -run 'Test.*(Restart|Scheduler|Rollback)' -count=1` passes

### Milestone 5: Contract/Codegen Loop

Deliver:

- committed `contracts/shunter.contract.json`
- generated TypeScript bindings
- tests asserting generated declared read helpers use named callbacks

Exit criteria:

- `make contract`
- `make codegen`
- `git diff --exit-code contracts generated` after regeneration

### Milestone 6: Stress Seeds

Deliver:

- small deterministic randomized workflow generator
- checked-in seed list
- trace replay command

Exit criteria:

- `go test ./internal/workflows -run TestSeededWorkflow -count=1` passes
- failures can be replayed by seed and operation index

## Definition Of Done For First Useful Canary

The first useful version is done when:

- the app lives in a separate repository
- it imports Shunter only through public packages
- strict-auth workflows pass
- read policy and visibility are proven against multiple identities
- declared reads and raw SQL are both used and distinguished
- at least one subscription workflow checks initial rows and deltas
- restart preserves state and read behavior
- scheduler reminder fires before and after restart
- contract export and codegen run
- every failure emits a reproducible trace

This is enough to start using the canary as external pressure on Shunter.

## Expected Shunter Pressure Points To Record

Do not solve these inside the canary by reaching into Shunter internals. Record
them as Shunter follow-up issues if they hurt.

- Declared reads are named and non-parameterized.
- Reducer argument schemas are not generated as typed client bindings.
- Scheduled reducers are not currently internal-only from the app author's
  perspective.
- The generated table helper surface may mention private tables even though
  server-side admission enforces read policy.
- Protocol client ergonomics are low-level; the app must currently assemble
  messages and decode rows itself.
- Visibility filters are intentionally single-table and cannot express
  membership joins directly.

These are useful findings, not reasons to block the canary.

## Agent Implementation Instructions

A fresh agent building the new repository should:

1. Create the repo skeleton exactly as described.
2. Implement Milestone 0 and run `go test ./... -count=1`.
3. Implement Milestone 1 before writing protocol workflow code.
4. Keep every reducer deterministic except scheduler due-time behavior.
5. Use fixed IDs for the initial workflow. Do not add ID allocation complexity
   until the fixed workflow passes.
6. Add tests before broad refactors.
7. Keep the first frontend out of scope.
8. If Shunter behavior blocks a required workflow, reduce it to a failing test
   or trace and document the Shunter gap instead of hiding it.

The canary should become boring infrastructure: run it often, keep it small,
and let it expose the next real Shunter feature or hardening need.
