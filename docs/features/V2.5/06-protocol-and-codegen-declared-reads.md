# V2.5 Task 06: Protocol And Codegen For Declared Reads

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Depends on:
- Task 05 declared read catalog and runtime API

Objective: make generated declaration helpers call named declared read
surfaces, and add the protocol/runtime wiring needed for those named reads to
work for external clients.

## Required Context

Read:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- `docs/features/V2.5/05-declared-read-catalog-and-runtime-api.md`

Inspect:

```sh
rtk go doc ./protocol.ClientMessage
rtk go doc ./protocol.Server
rtk go doc ./protocol.Conn
rtk rg -n "DecodeClientMessage|Encode|Tag|OneOffQuery|SubscribeSingle|SubscribeMulti|handleOneOffQuery|handleSubscribe" protocol
rtk rg -n "querySQL|viewSQL|runQuery|subscribeView|permissions|Generate" codegen
rtk rg -n "QueryDescription|ViewDescription|PermissionContract|ExportContract" .
```

## Target Behavior

Generated declaration helpers must stop executing declarations by sending raw
SQL strings.

Current undesired shape:

```ts
return runQuery("SELECT * FROM messages");
return subscribeView("SELECT * FROM messages");
```

Required shape:

```ts
return runDeclaredQuery("recent_messages");
return subscribeDeclaredView("live_messages");
```

The exact callback names may follow existing generated TypeScript style, but
the generated helper must pass the declaration name to the runtime/client
adapter. Declaration SQL can remain exported as metadata, but it is not the
execution authority.

## Protocol Or Runtime Wiring

Add real named-read wiring. Acceptable implementation shapes:

- new protocol client messages for declared query and declared subscribe
- a generated client adapter API that routes declaration names to runtime
  endpoints
- an internal compatibility layer that keeps raw SQL helpers available while
  declarations use named callbacks

The server must receive the declaration name. Matching raw SQL text to a
declaration is not allowed.

Named declared read protocol behavior:

- unknown declaration returns unknown-declared-read error
- missing declaration permission returns permission-denied error
- successful declared query executes the cataloged SQL
- successful declared view registers the cataloged subscription/read plan
- raw SQL endpoints remain raw SQL endpoints and do not inherit declaration
  permissions

Preserve existing raw SQL protocol behavior and error text.

## Tests To Add First

Add focused failing tests for:

- generated query helper calls named declared query callback
- generated view helper calls named declared view callback
- generated raw SQL helper, if present, remains separate from declared helper
- declared helper does not pass declaration SQL as execution input
- protocol/runtime named query succeeds with correct declaration permission
- protocol/runtime named view succeeds with correct declaration permission
- missing permission is reported as permission denied
- unknown name is reported as unknown declared read
- raw SQL equivalent does not use declaration permission
- existing raw `OneOffQuery` and `Subscribe` tests still pass

## Validation

Run at least:

```sh
rtk go fmt ./protocol ./codegen . ./executor
rtk go test ./codegen -count=1
rtk go test ./protocol -count=1
rtk go test . -run 'Test.*(Declaration|Read|Query|View|Permission|Codegen|Protocol)' -count=1
rtk go vet ./protocol ./codegen . ./executor
```

Run `rtk go test ./... -count=1` because protocol tags/codegen/runtime changes
are cross-cutting.

## Completion Notes

When complete, update this file with:

- generated callback API shape
- protocol/runtime message or adapter names
- compatibility notes for raw SQL callbacks
- validation commands run

Completed 2026-04-29.

Protocol/server declared-read surface names:

- Client messages:
  - `protocol.DeclaredQueryMsg`
  - `protocol.SubscribeDeclaredViewMsg`
- Client tags:
  - `protocol.TagDeclaredQuery`
  - `protocol.TagSubscribeDeclaredView`
- Server/runtime seam:
  - `protocol.DeclaredReadHandler`
  - `Runtime.HandleDeclaredQuery`
  - `Runtime.HandleSubscribeDeclaredView`

Generated TypeScript callback/helper behavior:

- Raw SQL callback types remain:
  - `QueryRunner = (sql: string) => Promise<Uint8Array>`
  - `ViewSubscriber = (sql: string) => Promise<() => void>`
- Declared-read callback types are separate:
  - `DeclaredQueryRunner = (name: string) => Promise<Uint8Array>`
  - `DeclaredViewSubscriber = (name: string) => Promise<() => void>`
- Executable generated query helpers now call
  `runDeclaredQuery("<query_declaration_name>")`.
- Executable generated view helpers now call
  `subscribeDeclaredView("<view_declaration_name>")`.
- `querySQL` and `viewSQL` continue to export declaration SQL metadata, but
  generated declaration helpers do not pass that SQL as execution input.

Raw SQL separation behavior:

- Raw `OneOffQueryMsg`, `SubscribeSingleMsg`, and `SubscribeMultiMsg` remain
  raw SQL surfaces.
- Declared reads are routed by explicit declaration name only.
- Raw SQL equivalent to a declaration still goes through raw SQL table-read
  admission and does not inherit declaration permissions.
- No exact-SQL matching or declaration inference was added.

Validation commands run:

```sh
rtk go test ./codegen -count=1
rtk go test ./protocol -run 'Test.*(Declared|Tags|ClientMessage)' -count=1
rtk go test . -run 'TestProtocol.*Declared|TestProtocolRawSQLEquivalentDoesNotUseDeclarationPermission' -count=1
rtk go test ./protocol -count=1
rtk go test . -run 'Test.*(Declaration|Read|Query|View|Permission|Codegen|Protocol)' -count=1
rtk go vet ./protocol ./codegen . ./executor
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

Follow-up remaining for row-level visibility tasks:

- Declared reads still execute the cataloged SQL without row-level visibility
  expansion.
- Tasks 07/08 still need authored visibility filter declarations and
  relation-aware visibility expansion for one-off reads and subscriptions.
