# Hosted runtime v2 directions

Status: baseline direction note
Scope: later-version structural/runtime directions after the v1 hosted runtime
and v1.5 follow-ons are alive.

This document captures the bigger runtime/platform evolutions that should not
distort the v1 hosted-runtime contract or the v1.5 usability follow-ons.

Current framing:
- v1 makes Shunter a coherent hosted runtime/server.
- v1.5 makes that runtime more usable through declarations, exports, codegen,
  passive permissions/read-model metadata, and descriptive migration metadata.
- v2 is where Shunter can revisit larger runtime shape, operational control,
  module isolation, executable migration planning, and read/policy semantics.

Related docs:
- `docs/specs/hosted-runtime-v1-contract.md` defines the base runtime
  shape v2 must not retroactively distort.
- `docs/specs/hosted-runtime-v1.5-follow-ons.md` defines the
  transitional usability/platform surfaces v2 may later clean up.

The goal is not to commit to every v2 feature now.
The goal is to keep later structural pressure out of v1/v1.5 while preserving
the likely direction.

---

## 1. Current live baseline

V2 planning must start from the live codebase, not from the older docs-only
state.

The current root package imports as `github.com/ponchione/shunter` and exposes:
- `Module`, `Config`, `Runtime`, and `Build(...)`.
- code-first module registration for schema, reducers, lifecycle hooks,
  query/view declarations, module metadata, schema version, and descriptive
  migration metadata.
- `Runtime.Start`, `Close`, `Ready`, `Health`, `HTTPHandler`,
  `ListenAndServe`, `CallReducer`, `Read`, `Describe`, `ExportSchema`,
  `ExportContract`, and `ExportContractJSON`.
- `Host.ListenAndServe` for lifecycle-owned HTTP serving of multi-module
  hosts.
- one canonical full module contract artifact with deterministic JSON,
  default snapshot filename `shunter.contract.json`, passive
  permissions/read-model metadata, and descriptive migration metadata.
- reusable TypeScript client codegen from detached `ModuleContract` values or
  canonical contract JSON.
- `contractdiff` comparison and warning-policy tooling over canonical
  contracts.

The lower-level packages are also real:
- `schema` owns table/reducer/lifecycle registration and schema export.
- `store` owns committed state, transactions, indexes, validation, snapshots,
  and rollback.
- `commitlog` owns recovery, snapshots, durability workers, and compaction.
- `executor` owns reducer execution, lifecycle reducers, scheduling, and
  protocol command admission.
- `subscription` owns query predicates, subscription registration, delta
  evaluation, and fan-out.
- `protocol` owns the WebSocket-first SPEC-005 wire surface, auth admission,
  one-off SQL queries, subscribe/unsubscribe, reducer calls, and transport
  lifecycle.
- `query/sql` already contains a minimum SQL grammar accepted by the protocol
  for one-off and subscription reads.
- `auth` validates JWTs and derives identity, but it does not yet expose a
  general claims/permissions model for v1.5 permission metadata enforcement.

Important current absences:
- no generic CLI/control-plane package exists.
- no multi-module host exists.
- no out-of-process module runner exists.
- no executable migration runner exists.
- query/view declarations are exported metadata; they are not yet a complete
  named query execution system.
- permission/read-model metadata is exported and generated, but it is not
  runtime access-control enforcement.

These facts should shape v2. V2 should cleanly build on the live owner APIs and
contracts instead of inventing a parallel platform model.

---

## 2. V2 thesis

V2 should be the first place Shunter intentionally widens beyond the simple v1
hosted-runtime shape.

The v1 shape stays intentionally narrow:
- one runtime/server process
- one statically linked Go module
- one canonical WebSocket-first client surface
- top-level `shunter` API as the normal app/runtime surface
- local calls as secondary test/tool/admin APIs
- canonical contract export as the source of truth for tooling

V2 may explore:
- clearer runtime/module boundaries
- admin and CLI workflows around existing contract artifacts
- migration planning and validation before migration execution
- reconciliation of declared query/view metadata with the executable SQL
  protocol read surface
- policy/auth enforcement that grows from v1.5 metadata
- contract artifact splitting only if consumers genuinely diverge
- multi-module hosting
- out-of-process module execution

Those should be treated as structural/runtime evolution, not as requirements for
making v1 or v1.5 useful.

---

## 3. Boundary rules

V2 must preserve these working decisions unless a new failing regression or
real app pressure proves otherwise:
- `shunter.Module` remains the app-authored definition surface.
- `shunter.Build(...)` remains the normal path that turns a module into a
  runtime owner.
- `shunter.Runtime` remains the one object normal app code uses for lifecycle,
  serving, local calls, and exports.
- the WebSocket protocol remains the primary external client model.
- one-module hosting remains a supported simple mode even if multi-module
  hosting lands.
- statically linked Go modules remain the default authoring path.
- `ModuleContract` remains the canonical source of truth until there is a
  concrete consumer split.
- migration metadata remains non-blocking for normal startup until an explicit
  operator migration workflow is designed.

V2 must not start by adding:
- a cloud platform
- a multi-tenant fleet control plane
- mandatory multi-module hosting
- mandatory out-of-process execution
- mandatory dynamic plugins
- cross-language module authoring
- fully automatic data migrations for every schema change
- a broad standalone policy language detached from reducers, queries, and views

---

## 4. Decomposed v2 slice order

The V2 implementation planning tree is:

1. `V2-A`: runtime/module boundary hardening
2. `V2-B`: contract artifact admin and CLI workflows
3. `V2-C`: migration planning and validation
4. `V2-D`: declared read and SQL protocol convergence
5. `V2-E`: policy/auth enforcement foundation
6. `V2-F`: multi-module hosting exploration
7. `V2-G`: out-of-process module execution gate

This order is intentionally conservative:
- a clearer runtime/module boundary helps every later structural direction.
- admin/CLI workflows can be built from `ExportContractJSON`, `codegen`, and
  `contractdiff` without changing runtime shape.
- migration planning should precede migration execution.
- declared read execution should reconcile with the already-live protocol SQL
  path before the query/view surface grows.
- policy enforcement needs the read/write surfaces and auth claims model to be
  explicit.
- multi-module hosting should wait until per-module contracts, routing,
  lifecycle, and policy boundaries are crisp.
- process isolation should be gated behind the boundary design rather than
  picked as the first design move.

---

## 5. Runtime/module boundary

V1 stores the module snapshot, schema engine, registry, recovered state,
reducer registry, lifecycle workers, subscription graph, and protocol graph
inside one `Runtime`.

That shape works for v1, but v2 should make the boundary more explicit before
adding multi-module hosting or process isolation.

Potential value:
- clearer separation between app-authored module data and runtime-owned
  subsystem handles
- fewer implicit copies of module identity/declarations across runtime files
- safer lifecycle and resource ownership
- a future path toward multi-module and out-of-process hosting
- cleaner introspection/export discipline

Recommended v2 posture:
- start with internal boundary hardening and tests; do not start with dynamic
  loading or process RPC
- keep the public top-level API stable unless a concrete app-author use case
  requires a new exported type
- preserve defensive copies and detached descriptions/contracts
- keep lower-level packages available as advanced surfaces while making the
  root runtime the normal owner

The key v2 question is not "embedded or hosted?"
That is already locked: hosted-first.

The key v2 question is:
- how explicit should the host/module boundary become after the v1 in-process
  owner model has proven the API?

---

## 6. Admin, CLI, and control surfaces

V1 intentionally avoided a broad control plane.
V1.5 produced real reusable surfaces that make practical tooling possible:
- `Runtime.ExportContractJSON`
- `ModuleContract.MarshalCanonicalJSON`
- `codegen.GenerateFromJSON`
- `contractdiff.CompareJSON`
- `contractdiff.CheckPolicy`

V2 should build admin/CLI workflows around those concrete surfaces first.

Possible v2 admin/control features:
- contract diff and policy checks over committed snapshots
- codegen from canonical JSON
- runtime/module inspection commands inside app-owned binaries
- local admin calls over `Runtime.CallReducer` and `Runtime.Read`
- runtime health/readiness/status inspection
- migration plan review commands
- backup/restore commands after storage semantics are ready
- module lifecycle commands only if multi-module hosting exists

Recommended v2 posture:
- begin with JSON-file workflows that do not require dynamic module loading
- keep app-owned runtime commands as Go library helpers unless a generic CLI can
  actually load the target module safely
- prefer owner-operated/local workflows before cloud or multi-tenant
  assumptions
- make command outputs deterministic and CI-friendly

The v2 control surface should grow from the runtime's real introspection/export
APIs, not from a separate imagined product layer.

---

## 7. Migration systems

V1.5 migration metadata is descriptive only.
It gives app authors, tooling, generated contracts, and CI a way to reason
about schema/module evolution.
It does not execute data migrations.

V2 should first add migration planning and validation, not automatic execution.

Potential v2 migration planning features:
- compare previous and current `ModuleContract` snapshots
- combine author-declared metadata with inferred `contractdiff` changes
- emit an ordered, reviewable migration plan
- flag data rewrites, manual-review-needed changes, and missing metadata
- support dry-run/report output for CI
- optionally validate a plan against current stored state after the storage
  preflight contract is designed

Executable migration features should remain later until safety semantics are
proven:
- migration functions tied to schema/contract versions
- migration locks and serialization
- forward-only execution policy
- backup/restore integration
- rollback metadata or compensating migration notes

Recommended v2 posture:
- keep migration planning as explicit operator/tooling workflow
- do not make normal `Runtime.Start` an implicit migration orchestrator
- require dry-run/reporting before destructive changes
- keep manual-review-needed as a valid long-term outcome

Likely migration direction:
- v1.5: describe and diff
- v2: plan and validate
- later v2+/v3: execute only once safety semantics are proven

---

## 8. Declared reads and SQL protocol

The live codebase has two read-related surfaces:
- v1.5 `QueryDeclaration` and `ViewDeclaration` metadata attached to
  `Module`.
- protocol one-off and subscription reads backed by the `query/sql` grammar and
  subscription evaluator.

V2 should reconcile these before growing either side substantially.

Design questions:
- are named queries/views aliases over SQL strings?
- are they Go handlers?
- are they metadata-only declarations used by generated clients and docs?
- how do declaration permissions and read-model metadata apply to raw SQL
  protocol reads?
- do generated clients call named declarations, raw SQL, or both?

Recommended v2 posture:
- preserve the existing protocol SQL path while the named declaration model is
  clarified
- do not add broad SQL features merely to make declarations look complete
- define how declared reads, generated clients, contract export, and protocol
  execution relate before adding new read syntax
- keep raw SQL and named declarations from becoming permanently divergent
  source-of-truth surfaces

---

## 9. Policy/auth evolution

V1.5 permissions/read-model declarations stay narrow and attach to reducers,
queries, and views.
The live `auth` package validates identity but does not yet expose general
permission claims.

V2 may turn passive metadata into enforcement if real applications need it.

Possible v2 policy directions:
- identity claims beyond issuer/subject/hex identity
- reusable permission tags attached to reducers, queries, and views
- enforcement for local reducer calls, protocol reducer calls, declared reads,
  and raw SQL reads
- generated client metadata that reflects effective access requirements
- admin tooling to inspect exported policy metadata

Recommended v2 posture:
- keep enforcement close to declared read/write surfaces
- start with a small claims model that can be tested through existing auth and
  local-call paths
- preserve dev-friendly local defaults
- make stricter production policy explicit rather than surprising
- do not introduce a broad standalone policy framework before repeated
  patterns justify it

Policy should evolve from reducer/query/view usage, not from abstract platform
ambitions.

---

## 10. Contract artifact evolution

V1.5 uses one canonical JSON full module contract artifact.
That remains the right default.

V2 may need to revisit artifact shape only if platform consumers diverge.

Possible split points:
- public client contract
- admin/module contract
- runtime aggregate contract for multi-module hosting
- migration/diff report artifact
- generated human-readable docs

Recommended v2 posture:
- keep `ModuleContract` as the source of truth until real complexity forces a
  split
- if split artifacts appear, define which artifact is canonical for each
  consumer
- do not let generated human-readable docs become a competing source of truth
- preserve stable contract diffing for CI/review workflows

This is one of the known v1.5 cleanup areas.
The purpose of v2 is to make overlaps intentional: keep what is useful,
deprecate what is not.

---

## 11. Multi-module hosting

Multi-module hosting is a v2 exploration, not a v1/v1.5 requirement.

The question for v2 is whether one Shunter runtime/server process should be
able to host multiple modules at once.

Potential value:
- shared runtime/server process for related modules
- clearer composition for product suites
- common admin/introspection surface
- possible shared client/tooling entrypoint

Risks:
- module identity and routing become more complex
- auth/permissions need module scoping
- contract export/codegen needs multi-module namespacing
- recovery, migrations, and operational failure modes become harder
- lifecycle boundaries can blur if modules are not isolated clearly

Recommended v2 posture:
- do not assume multi-module hosting is automatically required
- prototype only after one-module v1/v1.5 is proven with real apps
- require explicit module identity, routing, data-dir, lifecycle, and contract
  namespacing before supporting it
- keep one-module hosting as a valid simple mode even if multi-module support
  lands

Likely cleanup question from v1/v1.5:
- does `shunter.contract.json` remain one artifact per module?
- or does v2 introduce a top-level runtime contract that references multiple
  module contracts?

Default answer for now:
- keep per-module contracts as the source of truth
- consider a runtime-level aggregate contract only if multi-module hosting
  becomes real

---

## 12. Out-of-process module execution

Out-of-process module execution is a possible v2+ direction, not a committed v2
requirement.

Potential value:
- fault isolation
- resource isolation
- stronger runtime/module separation
- future multi-language or sandboxed module paths
- cleaner operational supervision

Costs:
- runtime/module invocation protocol must be designed
- reducer/query invocation crosses a process boundary
- recovery and deterministic execution semantics become more complex
- local testing and hello-world ergonomics can get worse
- deployment becomes more operationally heavy

Recommended v2 posture:
- design and test the runtime/module boundary first
- only pursue out-of-process execution if real app usage shows the in-process
  model is unsafe or too limiting
- keep statically linked in-process modules as a supported simple path unless
  there is a concrete reason to remove them
- treat any process-boundary work as a gated prototype until transaction,
  reducer, subscription, and durability semantics are clearly preserved

In other words:
- stronger boundary first
- out-of-process execution only if the boundary proves it needs process
  isolation

---

## 13. Practical bottom line

V2 is where Shunter can become more structurally ambitious.

But the rule should stay simple:
- v1: make the hosted runtime real
- v1.5: make it usable and exportable
- v2: make it more explicit, operable, migration-aware, policy-aware, and
  composable where real apps prove the need
- later v2+/v3: add process isolation or migration execution only after the
  safety model is proven

The v2 direction should be guided by pressure from working Shunter-hosted apps,
not by speculative platform completeness.
