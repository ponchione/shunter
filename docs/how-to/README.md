# How-To Guides

Status: rough draft
Scope: task-focused app-author documentation.

Use these pages when you already know what you want to do and need the current
Shunter integration path.

- `module-anatomy.md` - declare modules, tables, reducers, reads, metadata, and
  visibility filters.
- `reducer-patterns.md` - write reducers, use the transactional reducer DB, and
  avoid unsafe executor behavior.
- `reads-queries-views.md` - choose between local reads, declared queries, and
  declared live views.
- `serve-protocol-traffic.md` - expose Shunter protocol traffic through
  runtime-owned or app-owned HTTP serving.
- `configure-auth.md` - choose dev or strict auth and pass caller metadata.
- `persistence-and-shutdown.md` - use `DataDir`, shutdown cleanly, snapshot,
  compact, backup, restore, and run migrations.
- `contract-export-and-codegen.md` - export contracts, review compatibility,
  and generate TypeScript bindings.
- `testing-shunter-modules.md` - test modules through the root runtime API.

For a linear introduction, start with `../getting-started.md`.
