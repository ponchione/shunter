# V1.5-A Task 01: Reconfirm Query/View Declaration Prerequisites

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-A/00-current-execution-plan.md`

Objective: verify V1.5-A is stacked on the live v1 root package and does not
reopen V1-A through V1-G.

Status: complete.

Checks:
- `rtk go doc . Module`
- `rtk go doc . Module.Describe`
- `rtk go doc . Runtime`
- `rtk go doc . Runtime.Describe`
- `rtk go doc . Runtime.ExportSchema`
- `rtk go doc ./schema SchemaExport`

Read only if needed:
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`
- `docs/decomposition/hosted-runtime-version-phases.md`
- `docs/hosted-runtime-implementation-roadmap.md`

Prerequisite conclusions to record in Task 01:
- the live `Module` registration style is fluent and code-first
- `Module.Describe` currently returns detached module identity metadata only
- `Runtime.Describe` currently returns module identity plus runtime health
- `Runtime.ExportSchema` currently returns lower-level schema/reducer export
- V1.5-A should add declaration metadata, not canonical JSON or codegen

Recorded conclusions:
- `github.com/ponchione/shunter` is still the root package import path.
- `Module` keeps fluent code-first registration methods such as `TableDef`,
  `Reducer`, `OnConnect`, `OnDisconnect`, `Metadata`, and `Version`.
- `Module.Describe` returns `ModuleDescription{Name, Version, Metadata}` as a
  detached module identity snapshot.
- `Runtime.Describe` returns `RuntimeDescription{Module, Health}`, combining the
  module identity snapshot with runtime health.
- `Runtime.ExportSchema` returns `*schema.SchemaExport`, whose current exported
  surface is `Version`, `Tables`, and `Reducers`.
- V1.5-A should extend module-owned declaration metadata and narrow description
  exposure only; canonical contract JSON, codegen, permissions, migrations, and
  query engine widening remain out of scope.

Verification run:
- `rtk go doc . Module`
- `rtk go doc . Module.Describe`
- `rtk go doc . Runtime`
- `rtk go doc . Runtime.Describe`
- `rtk go doc . Runtime.ExportSchema`
- `rtk go doc ./schema SchemaExport`
