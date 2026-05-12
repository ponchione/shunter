# How-To Guides

Status: current v1 app-author guidance
Scope: task-focused app-author documentation.

Use these pages when you already know what you want to do and need the current
Shunter integration path.

Recommended order for a new app integration:

1. [Module anatomy](module-anatomy.md) - declare modules, tables, reducers,
   reads, metadata, lifecycle hooks, migrations, and visibility filters.
2. [Reducer patterns](reducer-patterns.md) - write reducers, use the
   transactional reducer DB, and avoid unsafe executor behavior.
3. [Reads, queries, and views](reads-queries-views.md) - choose between local
   reads, declared queries, and declared live views.
4. [Configure auth](configure-auth.md) - choose dev or strict auth and pass
   caller metadata.
5. [Serve protocol traffic](serve-protocol-traffic.md) - expose Shunter
   protocol traffic through runtime-owned or app-owned HTTP serving.
6. [Persistence and shutdown](persistence-and-shutdown.md) - use `DataDir`,
   shutdown cleanly, snapshot, compact, backup, restore, and run migrations.
7. [Contract export and codegen](contract-export-and-codegen.md) - export
   contracts, review compatibility, and generate TypeScript bindings.
8. [Use generated TypeScript clients](typescript-client.md) - install the local
   SDK runtime package, connect clients, call reducers, read declared queries,
   subscribe to views/tables, and opt into reconnect.
9. [Testing Shunter modules](testing-shunter-modules.md) - test modules through
   the root runtime API.

For a linear introduction, start with [Getting started](../getting-started.md).
For concise API decision notes, use [Reference notes](../reference/README.md).
