# Reference App Contract: Task Board

Status: proposed implementation contract
Scope: maintained v1 reference application proving Shunter's normal app-author,
client, and operator workflows.

## Purpose

The reference app should be a small collaborative task board. It is not a
showcase UI. Its job is to prove that a normal app can define a module, run a
runtime, enforce auth, call reducers, consume declared reads, generate a
TypeScript client, survive restarts, and operate backup/restore/migration flows
through public Shunter surfaces.

## Module Shape

Module name: `taskboard`

Tables:

- `users`: public directory of known identities.
  Columns: `identity`, `display_name`, `created_at`.
- `boards`: public board metadata.
  Columns: `id`, `name`, `created_by`, `created_at`, `archived`.
- `board_members`: private membership and role data.
  Columns: `id`, `board_id`, `identity`, `role`, `joined_at`.
- `lists`: public board list metadata.
  Columns: `id`, `board_id`, `name`, `position`.
- `tasks`: private task state with sender-owned visibility.
  Columns: `id`, `board_id`, `list_id`, `title`, `body`, `owner_identity`,
  `position`, `done`, `updated_at`.
- `comments`: private task comments.
  Columns: `id`, `task_id`, `author_identity`, `body`, `created_at`.
- `audit_events`: private operational history.
  Columns: `id`, `actor_identity`, `action`, `object_type`, `object_id`,
  `created_at`.

Required read policy coverage:

- At least `users`, `boards`, and `lists` are readable without private table
  permissions.
- `board_members`, `tasks`, `comments`, and `audit_events` require declared
  reads, permissions, or visibility filters.
- At least one visibility filter must use `:sender`; `tasks.owner_identity =
  :sender` is the minimum acceptable case.

## Reducers

Reducers should be small, validation-heavy, and permission-aware:

- `upsert_user`: create or update the caller's display name.
- `create_board`: create a board, initial list, membership row, and audit event.
- `invite_member`: add a member when the caller has board admin permission.
- `create_list`: add a list to a board.
- `create_task`: add a task with owner, list, and ordering validation.
- `move_task`: move a task between lists or positions.
- `assign_task`: change task owner.
- `complete_task`: mark a task done and emit an audit event.
- `comment_task`: append a comment to a visible task.
- `archive_completed_tasks`: scheduled reducer or lifecycle-driven maintenance
  only if scheduled/lifecycle reducers remain in the v1 surface.

Permission tags should be simple and visible in the generated contract:

- `profile:write`
- `boards:write`
- `boards:admin`
- `tasks:write`
- `comments:write`
- `tasks:read`
- `audit:read`

## Declared Reads

Declared queries:

- `my_open_tasks`: tasks where `owner_identity = :sender` and `done = false`,
  ordered by `updated_at DESC` with a small `LIMIT`.
- `my_recent_comments`: comments authored by `:sender`, ordered by
  `created_at DESC`.
- `board_list_summary`: a small join over boards and lists.
- `my_task_count`: `COUNT(*) AS total` over open tasks visible to the sender.

Declared live views:

- `live_my_open_tasks`: projected task rows for the sender's open tasks.
- `live_my_recent_comments`: comment rows visible to the sender.
- `live_board_list_summary`: table-shaped join view over public board/list
  metadata.
- `live_my_task_count`: aggregate view for sender-owned open tasks if the final
  declared-live aggregate contract remains stable.

Raw SQL should appear only in one explicit escape-hatch example, such as an
operator/debug query that is not part of the normal client workflow.

## Client Workflow

The maintained client should be a small browser or Node client using generated
TypeScript plus the public TypeScript runtime SDK once that SDK exists.

Required flows:

- connect with strict auth
- handle protocol version mismatch clearly
- call reducers through typed helpers
- call declared queries through typed helpers
- subscribe to declared live views
- maintain local view/cache state from initial rows and deltas
- unsubscribe idempotently
- reconnect and resubscribe according to the SDK policy

The client must not hand-write Shunter protocol message handlers in normal app
code.

## Operator Workflow

The example must demonstrate:

- empty data-dir bootstrap
- contract export
- TypeScript generation
- deterministic seed/load scenario
- clean shutdown and restart recovery
- offline backup with `BackupDataDir` or `shunter backup`
- restore into a fresh data directory
- one app-owned migration hook or offline migration binary

## Test Scenarios

At minimum, black-box tests should:

- start from an empty `DataDir`
- seed users, boards, lists, tasks, comments, and audit events through reducers
- verify permission failures and visibility-filtered reads
- subscribe to live views and verify initial rows plus deltas
- shut down, restart, and verify state and subscriptions
- export the contract and compare generated TypeScript output to a fixture
- run the backup/restore path into a fresh directory
- run one migration path and verify post-migration reads

## Non-Goals

- billing, payments, uploads, rich text, or third-party integrations
- a large UI framework showcase
- handwritten protocol-client glue
- dynamic module loading by `cmd/shunter`
- SpacetimeDB API, wire, or client compatibility
