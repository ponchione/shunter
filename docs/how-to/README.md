# How-To Guides

Status: current v1 app-author guidance
Scope: task-focused app-author documentation.

Use these pages when you already know what you want to do and need the current
Shunter integration path.

Recommended order for a new app integration:

1. [Host Shunter as a backend](host-shunter-backend.md) - run the standard
   static Go backend server and follow the hosted-chat example workflow.
2. [Module anatomy](module-anatomy.md) - declare modules, tables, reducers,
   procedures, reads, metadata, lifecycle hooks, migrations, and visibility
   filters.
3. [Reducer patterns](reducer-patterns.md) - write reducers, use the
   transactional reducer DB, and avoid unsafe executor behavior.
4. [Reads, queries, and views](reads-queries-views.md) - choose between local
   reads, declared queries, and declared live views.
5. [Configure auth](configure-auth.md) - choose dev or strict auth and pass
   caller metadata.
6. [Serve protocol traffic](serve-protocol-traffic.md) - expose Shunter
   protocol traffic through runtime-owned or app-owned HTTP serving.
7. [Persistence and shutdown](persistence-and-shutdown.md) - use `DataDir`,
   shutdown cleanly, snapshot, compact, backup, restore, and run migrations.
8. [Contract export and codegen](contract-export-and-codegen.md) - export
   contracts, review compatibility, and generate TypeScript bindings.
9. [Use generated TypeScript clients](typescript-client.md) - install the local
   SDK runtime package, connect clients, call reducers and procedures, read
   declared queries, subscribe to views/tables, and opt into reconnect.
10. [Testing Shunter modules](testing-shunter-modules.md) - test modules through
   the root runtime API.

For a linear introduction, start with [Getting started](../getting-started.md).
For concise API decision notes, use [Reference notes](../reference/README.md).
