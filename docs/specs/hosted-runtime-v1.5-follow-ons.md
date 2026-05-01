# Hosted runtime v1.5 follow-ons

Status: baseline follow-on contract; initial implementation has landed
Scope: important near-follow-on runtime/platform features that should land after the core v1 hosted runtime is alive.

This document covers the strong v1.5 additions already identified in the hosted-runtime decisions.
The purpose of v1.5 is not to redefine the hosted runtime.
The purpose is to make the v1 runtime meaningfully more usable for real product and application development.

Current framing:
- v1 makes Shunter a real hosted runtime/server
- v1.5 makes that runtime much more usable for application developers and product work
- v1.5 should improve ergonomics and platform usefulness without reopening the core v1 runtime boundary

Related docs:
- `docs/specs/hosted-runtime-v1-contract.md` defines the base hosted-runtime contract v1.5 extends
- `docs/specs/hosted-runtime-v2-directions.md` parks larger structural/runtime evolution

---

## 1. Primary purpose of v1.5

v1.5 should be a developer-ergonomics and platform-usability follow-on release.

It is not primarily about:
- major runtime-shape changes
- multi-module hosting
- control-plane expansion
- out-of-process module execution

It is primarily about making the hosted runtime practical and pleasant to build on.

In plain English:
- v1 makes Shunter real
- v1.5 makes Shunter much nicer to use for real apps

---

## 2. Core v1.5 themes

The main themes of v1.5 should be:
- clearer query/read declaration surfaces
- better client/binding export surfaces
- better product-facing read/permission declaration surfaces
- better schema/module evolution framing
- codegen becoming a first-class tooling surface

These are near-follow-on platform features, not distant optional extras.
They matter because a hosted runtime is not very useful if every app still has to rebuild too much glue around reads, exports, permissions, and frontend integration.

Priority within v1.5 should be:
1. query/view declarations
2. canonical contract export plus codegen/binding export surfaces
3. permissions/read-model declarations
4. migration policy metadata

Reasoning:
- a stronger declared read/query surface makes the rest of the platform more coherent
- codegen becomes much more valuable once there is a clearer thing to export
- permissions/read models and migration framing should follow the core read/export shape rather than precede it
- migration policy metadata should improve visibility and compatibility discipline without adding runtime migration execution semantics in v1.5

For v1.5, "query/view declarations" should mean:
- named read queries
- named declarative live views/subscriptions

These should be code-first declarations in Go.
String/DSL-based declarations may exist later, but they should not define the primary v1.5 model.

That is intentionally narrower than a full SQL/view system.
A fuller SQL/view model should be treated as a later-version direction, likely v2+, after the declared read/view layer is alive and useful.

---

## 3. v1.5 boundary rule

v1.5 should extend the hosted runtime, not reopen its core shape.

That means v1.5 should not change these v1 decisions:
- hosted-first runtime/server identity
- one-runtime / one-module primary model
- top-level `shunter` API as the normal surface
- WebSocket-first external client model
- narrow runtime config boundary
- statically linked Go module model in v1

Instead, v1.5 should make those decisions more useful in practice.

### 3.1 Codegen balance for v1.5

Codegen in v1.5 should strike a practical balance.

Primary target:
- frontend/client bindings generated from module schema plus declared reducer/query/view surfaces

Secondary targets:
- typed internal clients for tests, tools, and admin/maintenance scripts
- machine-readable contract/export artifacts for docs, inspection, CI, and downstream generators

Not a v1.5 priority:
- generating the server/module implementation itself
- broad framework scaffolding
- trying to solve every language target at once

The center of gravity should stay on client bindings plus clean exported contracts.

### 3.2 Permissions/read-model scope for v1.5

Permissions/read-model declarations in v1.5 should stay narrow.
They should attach to declared reducers, queries, and views rather than expand immediately into a broad standalone policy framework.

That keeps the model practical:
- permissions stay close to the read/write surfaces they govern
- export/codegen can understand them more easily
- the runtime does not need a giant generalized auth system before the declared read/view layer is useful

A broader policy framework can remain a later-version direction.

### 3.3 Migration policy metadata scope for v1.5

Migration policy metadata in v1.5 should be descriptive first.
It should help app authors, tooling, generated contracts, and CI understand schema/module evolution without making the runtime responsible for executing migrations yet.

The v1.5 surface should allow code-first metadata such as:
- module version
- schema/contract version
- previous-version reference
- minimal compatibility level: compatible, breaking, or unknown
- optional detailed change classification such as additive, deprecated, data-rewrite-needed, or manual-review-needed
- human migration notes

Migration metadata should attach in two layers:
- module-level metadata for the overall version and compatibility summary
- optional declaration-level metadata on schema/table/query/view declarations for precise change classification

The module-level layer gives exported contracts and tooling one obvious summary of the module's evolution state.
The declaration-level layer gives codegen, docs, and CI enough precision to identify risky or review-worthy changes without requiring executable migrations.

This metadata should be exported through the module/contract artifact so downstream tools can inspect it.
It should not imply that Shunter v1.5 runs ordered migration functions, rewrites stored state, handles rollback/forward execution, or owns deployment migration orchestration.

Migration metadata should be non-blocking at runtime in v1.5.
The runtime should not refuse to start solely because migration metadata is missing or marks a change as risky.
Tooling, contract export, and CI may warn on missing/risky metadata, and project policy may choose to fail CI on those warnings.

Migration metadata should combine author-declared intent with tool-inferred contract diffs.
Author-declared metadata captures intent, compatibility notes, and manual-review signals.
Tool-inferred diffs compare exported module/contract artifacts to catch forgotten table, field, query, or view changes.
Tooling should warn when inferred changes and declared metadata disagree.

Exported contract snapshots should be supported in two forms:
- build artifacts for tooling, generated clients, docs, and downstream inspection
- stable repo-committed snapshots for apps that want PR review and CI-based migration checks

The source-of-truth exported contract format should be canonical JSON.
Human-readable renderings may be generated later for docs/review, but they should not be the canonical contract artifact.

The canonical v1.5 contract snapshot should be a full module contract artifact, not only a public client surface file.
It should include:
- public client surface: tables, reducers, queries, and views
- module and schema/contract versions
- migration metadata
- permissions/read-model declarations
- codegen/export metadata

v1.5 should distinguish module version from schema/contract version.
Module version identifies the app/backend package release.
Schema/contract version identifies the exported data/client compatibility surface.
Full semantic-versioning policy should be deferred until Shunter has more real contract evolution history.

Keeping this as one canonical artifact is the simpler v1.5 default because the v1.5 surfaces are tightly linked.
A later version may split client-facing and admin/module-facing artifacts if that becomes cleaner.

The recommended default repo-committed snapshot path should be `shunter.contract.json`.
Apps and tooling may configure a different output path when their repo layout needs it.
The default exists to keep docs, CI examples, and review workflows simple without making the path part of the runtime shape.

Committing contract snapshots should be recommended for apps that want migration discipline, but v1.5 should not require every project to commit generated artifacts immediately.

Executable migration plans/runners should be deferred beyond v1.5 as a later dedicated migration/runtime epic after the hosted runtime and declared read/view layer are stable.

### 3.4 Transitional "both" surfaces in v1.5

Some v1.5 decisions intentionally support two forms at once because Shunter is still moving from a minimal hosted runtime into a fuller platform surface.
These "both" cases should be documented explicitly so later versions can decide whether to keep, narrow, or remove one side.

Current intentional "both" cases:
- migration metadata attaches both at module level and optionally at declaration level
- migration metadata uses both author-declared intent and tool-inferred contract diffs
- exported contract snapshots exist both as build artifacts and as optional repo-committed snapshots
- v1.5 uses one full module contract artifact, while v2+ may later split client-facing and admin/module-facing artifacts
- canonical JSON is the source of truth, while generated human-readable renderings may be added later for docs/review

For each future cleanup pass, ask:
- did one side become clearly primary?
- is the secondary side still serving tests, tooling, review, or compatibility?
- can the secondary side be deprecated without losing migration safety or developer ergonomics?

The goal is to avoid accidental permanent dual surfaces while still allowing pragmatic transitional overlap in v1.5.

---

## 4. Canonical v1.5 target shape

This section summarizes the intended v1.5 surface as a concrete target rather than an open decision list.

### 4.1 Query/view declarations

The first v1.5 priority is a code-first declared read surface.

The intended model is:
- named read queries for request/response-style reads
- named declarative live views/subscriptions for realtime client state
- declarations registered on the module, alongside schema and reducers
- exportable metadata so codegen and contract snapshots can understand the read surface

This should not become a full SQL/view system in v1.5.
A fuller query language, SQL-like view model, or broad relational view layer should remain v2+ unless the simpler declared-read model proves insufficient.

### 4.2 Codegen and binding export

The second v1.5 priority is making the declared module surface useful outside the Go process.

Primary output:
- frontend/client bindings generated from the module schema plus declared reducers, queries, and views

Secondary outputs:
- typed internal clients for tests, tools, and admin/maintenance scripts
- machine-readable module contracts for docs, inspection, CI, and downstream generators

The center of gravity should be client bindings plus clean exported contracts.
Generating the server/module implementation itself, broad framework scaffolding, or every possible language target should wait.

### 4.3 Permissions/read-model declarations

The third v1.5 priority is narrow permission and read-model metadata.

These declarations should attach to:
- reducers
- named queries
- named views/subscriptions

They should answer questions such as:
- who may call this reducer?
- who may read this query/view?
- what exported/client binding metadata is needed to represent that policy?

They should not become a broad standalone policy framework in v1.5.
The right v1.5 goal is enough declared policy surface for read/write exports, generated clients, and review tooling to stop guessing.

### 4.4 Migration policy metadata

The fourth v1.5 priority is descriptive migration policy metadata.

The locked shape is:
- descriptive/exported metadata first
- no executable migration runner in v1.5
- module-level version/compatibility summary
- optional declaration-level change metadata on schema/table/query/view declarations
- author-declared intent plus tool-inferred contract diffs
- non-blocking at runtime
- warnings through tooling/export/CI, with optional project policy to fail CI

Minimal required compatibility level:
- `compatible`
- `breaking`
- `unknown`

Optional detailed change classifications may include:
- `additive`
- `deprecated`
- `data-rewrite-needed`
- `manual-review-needed`

The minimal compatibility level gives tooling one simple required field.
The optional classifications give humans and CI richer signal without forcing a rigid migration taxonomy too early.

### 4.5 Contract snapshot

The canonical v1.5 contract snapshot should be a full module contract artifact in canonical JSON.

Default repo-committed path:
- `shunter.contract.json`

The path should be configurable for apps that need a different repo layout.
The default is a documentation/CI/review convention, not part of the runtime shape.

The artifact should include:
- module identity and module version
- schema/contract version
- public client surface: tables, reducers, queries, and views
- permission/read-model declarations
- migration metadata
- codegen/export metadata

The artifact may exist as:
- a build artifact for tooling and downstream generation
- an optional repo-committed snapshot for apps that want PR review and CI-based migration checks

Repo-committed snapshots should be recommended for apps that care about migration discipline, but should not be mandatory in v1.5.

---

## 5. Suggested implementation order

A clean v1.5 implementation order is:

1. Add named query/view declaration metadata to the module model.
2. Extend module export so schema, reducers, queries, and views appear in one machine-readable contract.
3. Add canonical JSON contract snapshot output.
4. Add frontend/client binding generation from the exported contract.
5. Add narrow permissions/read-model metadata on reducers, queries, and views.
6. Add descriptive migration metadata at module and declaration levels.
7. Add contract-diff tooling that compares current export against a previous `shunter.contract.json`.
8. Add warning/CI-oriented policy checks for missing metadata, risky changes, and declared-vs-inferred mismatches.

This keeps the dependency chain sane:
- reads/views define the surface
- exports make the surface inspectable
- codegen consumes the export
- permissions annotate the exported read/write surface
- migration metadata and diff checks use the same canonical artifact

---

## 6. Explicit v1.5 non-goals

v1.5 should not include:
- a full SQL/view system
- executable migration runners
- runtime-blocking migration metadata enforcement
- broad standalone policy/auth framework
- server/module implementation generation
- all-language SDK generation
- multi-module hosting
- out-of-process module execution
- cloud/control-plane expansion
- changing the v1 hosted-first, one-runtime/one-module shape

Those are later-version concerns unless real app usage proves one must move earlier.

---

## 7. Future cleanup notes

The main future cleanup risk is that pragmatic v1.5 overlap becomes permanent by accident.

Known cleanup candidates:
- module-level plus declaration-level migration metadata
- author-declared plus tool-inferred migration classification
- build-artifact plus repo-committed contract snapshots
- one full module contract artifact versus possible future split client/admin artifacts
- canonical JSON plus possible generated human-readable renderings

For each later cleanup, prefer this rule:
- keep the side that has become the stable source of truth
- keep secondary forms only when they serve a clear audience: tests, tooling, review, compatibility, or human docs
- deprecate secondary forms when they duplicate the primary path without adding safety or ergonomics

The intended direction is not dual surfaces forever.
The intended direction is controlled transitional overlap while v1.5 discovers which platform surfaces become durable.
