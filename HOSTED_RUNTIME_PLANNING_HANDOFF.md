# Hosted Runtime Planning Handoff

Use this file as the primary cross-agent handoff for hosted-runtime planning or
implementation work.

Use `NEXT_SESSION_HANDOFF.md` instead for TECH-DEBT / correctness work.

## Current Target

Hosted-runtime V2 is complete through `V2-G`.

No `V2-H` target is queued. Future hosted-runtime work should start only from a
new explicit user target or a failing regression that proves drift.

V2-G out-of-process module execution gate is complete.
V2-F multi-module hosting exploration is complete.
V2-E policy/auth enforcement foundation is complete.
V2-D declared read and SQL protocol convergence is complete.
V2-C migration planning and validation is complete.
V2-B contract artifact admin and CLI workflows are complete.
V2-A runtime/module boundary hardening is complete.

V1.5-E migration metadata, contract diffs, and warning policy checks are
complete, which completes the initial V1.5 hosted-runtime follow-on plan.

V2 planning is now decomposed under `docs/hosted-runtime-planning/V2/`,
starting from the code-grounded source direction in
`docs/decomposition/hosted-runtime-v2-directions.md`.

Do not reopen V1-H, V1.5-A through V1.5-E, or V2-A through V2-G unless a new
failing regression proves drift.

## Execution Granularity

V2 work should proceed one lettered slice at a time, not one numbered task at a
time. A normal hosted-runtime handoff should complete the whole active `V2-*`
slice:
1. read the slice execution plan and task docs
2. run prerequisite inspection commands
3. add the planned failing tests
4. implement the slice
5. expose any required metadata/output surface
6. run the slice validation gates
7. update the slice plan and this handoff with live proof, validation results,
   and the next `V2-*` slice

Do not stop after a prerequisite or failing-test task unless there is a real
blocker, an ambiguous contract decision, or a validation failure that cannot be
resolved inside the active slice.

Do not hand off a V2 slice as complete until its focused tests, format command,
and required vet/broader validation gates have passed and are recorded here.

V2-A is complete. Its live proof is:
- `Runtime` now groups app-authored module identity, metadata, reducer
  declaration metadata, query/view declarations, migration metadata, and table
  migrations behind the private `moduleSnapshot`.
- `Build` creates that snapshot after module/config/schema/state/reducer
  validation and before returning the runtime.
- mutating the original `Module`, registration input slices, or returned
  `Runtime.Describe` values does not affect later runtime descriptions or
  contract exports.
- runtime-owned schema engine, registry, committed state, reducer registry,
  subscriptions, executor, scheduler, protocol graph, and lifecycle resources
  remain owned by `Runtime` fields separate from the module snapshot.
- `Runtime.Describe`, `Runtime.ExportSchema`, `Runtime.ExportContract`, and
  `Runtime.ExportContractJSON` remain detached public outputs.
- no new public structural API, multi-module behavior, process isolation,
  migration execution, or policy enforcement was added.

V2-A validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Boundary|Describe|Export|Contract|Lifecycle|Build)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -count=1`

V2-B is complete. Its live proof is:
- `contractworkflow.CompareFiles` diffs previous/current canonical
  `ModuleContract` JSON files through `contractdiff.CompareJSON`.
- `contractworkflow.CheckPolicyFiles` runs deterministic migration/contract
  policy checks through `contractdiff.CheckPolicy`, preserving default
  non-fatal warnings and strict failure status.
- `contractworkflow.GenerateFromFile` and `GenerateFile` generate TypeScript
  bindings from contract JSON through `codegen.GenerateFromJSON`.
- `contractworkflow.FormatDiff` and `FormatPolicy` render deterministic text
  and JSON workflow outputs.
- `cmd/shunter` exposes `contract diff`, `contract policy`, and
  `contract codegen` over existing JSON files only.
- generic CLI help documents that module export belongs in app-owned binaries
  via `Runtime.ExportContractJSON`.
- no dynamic module loading, generic module export, runtime startup,
  reducer/query admin commands, cloud control plane, or multi-module host
  commands were added.

V2-B validation passed:
- `rtk go fmt ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`
- `rtk go test ./codegen ./contractdiff ./contractworkflow ./cmd/shunter -count=1`
- `rtk go test ./... -run 'Test.*(Contract|Codegen|Diff|Policy)' -count=1`
- `rtk go vet ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`

V2-C is complete. Its live proof is:
- `contractdiff.Plan` and `PlanJSON` combine previous/current
  `ModuleContract` snapshots with `contractdiff` changes and policy warnings.
- `MigrationPlan` exposes deterministic summary counts, entries, warnings,
  severity, action, attached migration metadata, and classifications for
  review/CI use.
- `contractdiff.Compare` now reports index definition changes and
  migration-metadata changes alongside existing additive, breaking, and
  metadata-only contract changes.
- `PlanOptions.ValidateContracts` adds read-only contract consistency warnings
  for module/schema/contract version metadata without opening or mutating
  stored state.
- `MigrationPlan.Text` and `MarshalCanonicalJSON` render deterministic text
  and newline-terminated JSON.
- `contractworkflow.PlanFiles` and `FormatPlan` expose JSON-file workflows.
- `cmd/shunter contract plan` operates only on existing canonical JSON files.
- no executable migration runner, stored-state rewrite, startup-blocking
  migration enforcement, rollback, backup/restore, or runtime shape change was
  added.

V2-C validation passed:
- `rtk go fmt ./contractdiff ./contractworkflow ./cmd/shunter`
- `rtk go test ./contractdiff ./contractworkflow ./cmd/shunter -count=1`
- `rtk go test ./... -run 'Test.*(Migration|ContractDiff|Policy|Plan)' -count=1`
- `rtk go vet ./contractdiff ./contractworkflow ./cmd/shunter`

V2-D is complete. Its live proof is:
- `QueryDeclaration.SQL` and `ViewDeclaration.SQL` define optional executable
  SQL targets for named read declarations.
- metadata-only declarations remain supported, while generated TypeScript
  clients emit executable helpers only when SQL metadata exists.
- `Build` validates SQL-backed declarations against the protocol SQL compiler;
  query SQL uses one-off rules and view SQL uses subscription rules.
- `Runtime.ExportContract` and canonical contract JSON carry declaration SQL
  metadata when present.
- `codegen.Generate` emits `querySQL` and `viewSQL` maps and SQL-backed helper
  functions instead of calling a nonexistent named-query protocol feature.
- `contractdiff.Compare` reports declaration SQL additions as additive and
  declaration SQL removals/changes as breaking.
- raw SQL `OneOffQuery`, `SubscribeSingle`, and `SubscribeMulti` behavior is
  preserved.
- no broad SQL expansion, policy enforcement, multi-module routing, process
  isolation, or alternate read evaluator was added.

V2-D validation passed:
- `rtk go fmt . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`
- `rtk go test . -run 'Test.*(Declaration|Contract|Read|Query|View)' -count=1`
- `rtk go test ./protocol ./query/sql ./subscription -count=1`
- `rtk go test ./codegen ./contractdiff -count=1`
- `rtk go vet . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`
- `rtk go test ./... -count=1`

V2-E is complete. Its live proof is:
- `auth.Claims` now carries optional permission tags parsed from a narrow
  `permissions` JWT claim while preserving existing identity, issuer,
  audience, expiry, and hex-identity validation.
- `types.CallerContext` carries permission tags plus an explicit
  all-permissions flag used for trusted dev/anonymous paths.
- `Runtime.CallReducer` exposes `WithPermissions(...)`; `AuthModeDev` local
  calls retain a dev-friendly all-permissions default unless permissions are
  explicitly supplied, while strict local calls require explicit permission
  tags for protected reducers.
- Protocol upgrades copy validated claim permission tags into
  `UpgradeContext` and `Conn`; anonymous/dev protocol connections get the
  explicit all-permissions flag.
- `protocol.CallReducerRequest` and `executor.ProtocolInboxAdapter` forward
  permission context into executor reducer requests.
- `Build` copies reducer permission metadata into the runtime-owned executor
  reducer registry.
- The executor rejects external reducer calls missing required tags with
  `ErrPermissionDenied` and `StatusFailedPermission` before reducer user code
  or transaction creation.
- Permission metadata remains exported in canonical contracts and generated
  TypeScript clients.
- Read permission enforcement is deferred because SQL-backed generated helpers
  still execute through raw SQL protocol paths; correct raw SQL enforcement
  needs table/read-model policy rather than declaration-name checks alone.
- no tenant framework, role database, external IdP integration, broad policy
  language, multi-module scoping, or read policy engine was added.

V2-E validation passed:
- `rtk go fmt . ./auth ./protocol ./executor ./codegen ./types`
- `rtk go test . -run 'Test.*(Permission|Auth|Reducer|Local|Network)' -count=1`
- `rtk go test ./auth ./protocol ./executor ./codegen ./types -count=1`
- `rtk go vet . ./auth ./protocol ./executor ./codegen ./types`
- `rtk go test ./... -count=1`

V2-F is complete. Its live proof is:
- `HostRuntime` and `NewHost(...)` bind already-built single-module runtimes to
  explicit module names and route prefixes without changing `Runtime`,
  `Build`, or one-module app authoring.
- host construction rejects nil runtimes, blank names, module-name/runtime
  identity mismatches, duplicate module names, overlapping route prefixes, and
  shared runtime data directories.
- `Host.Start` starts runtimes in registration order and closes already-started
  runtimes in reverse order if a later runtime fails to start.
- `Host.Close` closes every hosted runtime in reverse registration order.
- `Host.HTTPHandler` mounts each runtime's existing `/subscribe` protocol
  handler below the module's explicit route prefix.
- `Host.Health` and `Host.Describe` expose detached per-module diagnostics with
  module name, route prefix, data directory, and runtime health/description.
- per-module `Runtime.ExportContract` remains unchanged and canonical; no
  aggregate contract artifact or contract merge was added.
- no process isolation, dynamic module loading, cross-module transactions,
  shared-table semantics, global schema registry, or shared reducer/subscription
  manager was added.

V2-F validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Host|MultiModule|Runtime|Network|Contract)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./protocol ./subscription ./executor -count=1`
- `rtk go test ./... -count=1`

V2-G is complete. Its live proof is:
- `internal/processboundary` records an internal, experimental invocation
  contract for reducer/lifecycle requests, caller identity, args, statuses,
  output bytes, user errors, boundary failures, lifecycle ordering,
  transaction policy, and subscription-update ownership.
- `InvocationResponse` validation requires explicit transaction semantics and
  rejects commit/rollback decisions when the boundary declares transaction
  mutation unsupported.
- `DefaultContract` defers out-of-process execution because
  `store.Transaction` commit/rollback semantics are host-local Go object
  semantics and subscriptions are evaluated from committed host state.
- lifecycle metadata captures current OnConnect insertion, reducer invocation,
  and rollback/commit behavior plus OnDisconnect invoke/cleanup/commit-cleanup
  behavior.
- subscription updates remain committed-state driven; process messages are not
  allowed to broadcast updates.
- canonical `ModuleContract` JSON remains unchanged and does not contain
  process-boundary metadata.
- no production process runner, child-process supervisor, dynamic module
  loading, cross-language SDK, executor routing change, or replacement of
  in-process module execution was added.

V2-G validation passed:
- `rtk go fmt . ./executor ./store ./subscription ./protocol ./internal/processboundary`
- `rtk go test ./executor ./store ./subscription ./protocol ./internal/processboundary -count=1`
- `rtk go test . -run 'Test.*(Runtime|Lifecycle|Local|Contract)' -count=1`
- `rtk go vet . ./executor ./store ./subscription ./protocol ./internal/processboundary`

The completed V1.5 proof below is historical context and should not be treated
as an active target.

V1.5-A is complete. Its live proof is:
- `QueryDeclaration` and `ViewDeclaration` exist in the root package.
- `Module.Query(...)` and `Module.View(...)` register module-owned query/view
  declarations fluently.
- declaration names are validated during `Build`; blank names and duplicate
  names are rejected, and query/view names share one namespace.
- `Module.Describe` and `Runtime.Describe` expose detached query/view
  declaration summaries through `Queries` and `Views`.
- `Runtime.ExportSchema` remains the lower-level schema/reducer export.

V1.5-A validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test(Module.*Declaration|Runtime.*Declaration|.*Describe.*Declaration)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

V1.5-B is complete. Its live proof is:
- `ModuleContract` exists in the root package as the canonical full module
  contract artifact.
- `Runtime.ExportContract()` combines module identity/version/metadata,
  schema export, reducers, queries, views, reserved permission/read-model and
  migration sections, and codegen/export metadata.
- `Runtime.ExportContractJSON()` and `ModuleContract.MarshalCanonicalJSON()`
  produce deterministic indented JSON with a trailing newline.
- `DefaultContractSnapshotFilename` is `shunter.contract.json`.
- contract export returns detached values and works before `Runtime.Start` and
  after `Runtime.Close`.
- permission and read-model sections were reserved metadata only; migration
  metadata was later populated in V1.5-E.

V1.5-B validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*Contract|Test.*Export.*JSON' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./schema -count=1`

V1.5-C is complete. Its live proof is:
- `codegen.Generate(...)` accepts detached `ModuleContract` values.
- `codegen.GenerateFromJSON(...)` accepts canonical `ModuleContract` JSON.
- `LanguageTypeScript` is the only supported generator target.
- generated TypeScript is deterministic and covers table row types, table
  subscription helpers, reducer raw-byte call helpers, declared query helpers,
  and declared view/subscription helpers.
- lifecycle reducers are exposed separately from normal callable reducer
  helpers.
- unsupported language values and unusable contract input fail clearly.
- no CLI command package was added in V1.5-C; the reusable package surface is
  the landed artifact.

V1.5-C validation passed:
- `rtk go fmt ./codegen`
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet ./codegen`

V1.5-D is complete. Its live proof is:
- `PermissionMetadata` and `ReadModelMetadata` exist in the root package.
- `Module.Reducer(...)` accepts `WithReducerPermissions(...)` without breaking
  existing two-argument reducer registrations.
- `QueryDeclaration` and `ViewDeclaration` carry passive `Permissions` and
  `ReadModel` metadata.
- `Runtime.ExportContract()` exports permission metadata for reducers,
  queries, and views plus read-model metadata for queries and views.
- absent permission/read-model metadata serializes deterministically as empty
  contract arrays.
- TypeScript codegen exposes passive `permissions` and `readModels` constants.
- no runtime access-control enforcement, policy engine, migration metadata, or
  runtime shape change was added by V1.5-D itself. V2-E later added narrow
  reducer permission enforcement from reducer metadata.

V1.5-D validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*Permission|Test.*ReadModel|Test.*Contract' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./codegen -count=1`
- `rtk go vet ./codegen`

V1.5-E is complete. Its live proof is:
- `MigrationMetadata`, compatibility constants, and classification constants
  exist in the root package.
- `Module.Migration(...)` exports descriptive module-level migration metadata.
- `Module.TableMigration(...)` plus `QueryDeclaration.Migration` and
  `ViewDeclaration.Migration` export declaration-level migration metadata.
- `Runtime.ExportContract()` and `Runtime.ExportContractJSON()` include
  deterministic migration metadata without changing runtime startup behavior.
- `contractdiff.Compare(...)` and `contractdiff.CompareJSON(...)` report
  deterministic additive, breaking, and metadata-only contract changes.
- `contractdiff.CheckPolicy(...)` reports warning/CI-oriented migration
  metadata findings, with warnings non-fatal by default and strict mode
  explicitly opt-in.
- no executable migration runner, state rewrite, or startup-blocking migration
  enforcement was added.

V1.5-E validation passed:
- `rtk go fmt . ./store ./contractdiff`
- `rtk go test ./... -run 'Test.*Migration|Test.*ContractDiff|Test.*Policy' -count=1`
- `rtk go test . -count=1`
- `rtk go test ./contractdiff -count=1`
- `rtk go test ./codegen -count=1`
- `rtk go test ./store -run TestRapidStoreCommitMatchesModel -count=50`
- `rtk go test ./... -count=1`
- `rtk go vet . ./store ./contractdiff ./codegen`

Former non-slice validation blocker resolved:
- `store.TestRapidStoreCommitMatchesModel` exposed a transaction undelete
  constraint bug. A failed undelete now checks transaction-local unique/set
  conflicts before canceling the pending delete.

## Current Hosted-Runtime State

V1-H, V1.5-A through V1.5-E, and V2-A through V2-G are audited as landed.

Live proof points:
- root package imports as `github.com/ponchione/shunter`
- `Module`, `Config`, `Runtime`, and `Build(...)` exist
- `Runtime.Start`, `Close`, `HTTPHandler`, `ListenAndServe`, local calls,
  describe, and schema export exist
- `Module` registration remains fluent and code-first
- `Module.Query(...)` and `Module.View(...)` register named read/view
  declarations
- `Module.Describe` exposes detached module identity plus `Queries` and `Views`
- `Runtime.Describe` exposes module identity/declarations plus runtime health
- `Runtime.ExportSchema` currently exposes lower-level schema/reducer metadata:
  `Version`, `Tables`, and `Reducers`
- `Runtime.ExportContract` exposes the full canonical module contract for
  codegen, reviewable JSON snapshots, migration metadata, and diff tooling
- `Runtime.ExportContractJSON` exposes deterministic canonical contract JSON
- `codegen.Generate` and `codegen.GenerateFromJSON` generate deterministic
  TypeScript client bindings from the canonical contract without starting a
  runtime
- reducer permission metadata is exported in the canonical contract, visible to
  generated TypeScript clients, and enforced for reducer calls by V2-E;
  query/view permission metadata and read-model metadata remain exported and
  passive pending a table/read-model policy design
- migration metadata is passive, exported in the canonical contract, and
  consumed by contract-diff/policy tooling only
- V2-A hardened the runtime/module boundary with a private `moduleSnapshot`
  and boundary tests without changing public app-author APIs
- V2-B added reusable JSON-file contract workflows in `contractworkflow` and a
  generic `cmd/shunter` CLI for `contract diff`, `contract policy`, and
  `contract codegen`
- V2-C added deterministic migration planning through `contractdiff.Plan`,
  `contractworkflow.PlanFiles`, and `cmd/shunter contract plan`
- V2-D added optional SQL-backed query/view declarations validated through the
  protocol SQL compiler, exported through contracts, reflected in codegen, and
  visible to contractdiff
- V2-E added narrow permission claim extraction, caller permission context,
  reducer permission enforcement for local/protocol external calls, and stable
  permission-denied results; read permission enforcement remains deferred to a
  future table/read-model policy surface
- V2-F added a root-package `Host` owner for already-built runtimes with
  explicit module names, route prefixes, data-dir collision checks,
  deterministic lifecycle cleanup, prefixed HTTP routing, and detached
  per-module health/description diagnostics
- V2-G added an internal `processboundary` contract gate and deferred
  production out-of-process execution until transaction mutation and
  subscription semantics have a dedicated design
- generic contract workflows operate only on existing canonical JSON files;
  app-owned export remains based on `Runtime.ExportContractJSON`
- root/runtime package tests are the live proof for hosted-runtime ownership,
  serving, local calls, describe, export, and lifecycle behavior
- the prior bundled hello-world command was removed because it no longer served
  a maintained product or integration purpose

Do not reopen V1-A through V1-H or V1.5-A through V1.5-E unless a new failing
regression proves drift.

## Startup Reading

Required:
1. `RTK.md`
2. this file
3. any explicitly assigned hosted-runtime slice or regression documents

No V2-H target is queued.

For V2-G audit only, start from:
- `docs/hosted-runtime-planning/V2/README.md`
- `docs/hosted-runtime-planning/V2/V2-G/00-current-execution-plan.md`
- `docs/hosted-runtime-planning/V2/V2-G/01-stack-prerequisites.md`
- `docs/hosted-runtime-planning/V2/V2-G/02-boundary-contract-tests.md`
- `docs/hosted-runtime-planning/V2/V2-G/03-prototype-or-defer.md`
- `docs/hosted-runtime-planning/V2/V2-G/04-decision-record.md`
- `docs/hosted-runtime-planning/V2/V2-G/05-format-and-validate.md`

For V1.5-E audit only, start from:
- `docs/hosted-runtime-planning/V1.5/README.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-E/00-current-execution-plan.md`
- `runtime_migration_test.go`
- `runtime_contract.go`
- `contractdiff/`

Open this only when V1.5-E audit docs or live code leave a contract, metadata,
or diff-tooling question unresolved:
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`

Do not read broad roadmap, ledger, or decomposition docs by default.

## Completed V1.5-E Scope

In scope:
- descriptive migration metadata types
- module-level migration/version compatibility summary
- declaration-level migration metadata where the V1.5-E task docs require it
- canonical contract export of migration metadata
- deterministic contract-diff tooling comparing current exports against a
  previous `shunter.contract.json`
- warning/CI policy checks for missing metadata, risky changes, and
  declared-vs-inferred mismatches

Out of scope:
- executable migration runners
- runtime-blocking migration metadata enforcement
- automatic stored-state rewrites, rollback, or deployment orchestration
- full SQL/view system
- query engine surface widening
- runtime shape changes
- multi-module hosting, out-of-process module execution, or control-plane work

Preserve WebSocket-first v1 runtime behavior.

## V2 Planning State

Current active V2 slice:
- none; `V2-G` is complete and no `V2-H` target is queued

Completed V2 slices:
- `V2-A`: runtime/module boundary hardening
- `V2-B`: contract artifact admin and CLI workflows
- `V2-C`: migration planning and validation
- `V2-D`: declared read and SQL protocol convergence
- `V2-E`: policy/auth enforcement foundation
- `V2-F`: multi-module hosting exploration
- `V2-G`: out-of-process module execution gate

V2 planning slices are:
1. `V2-A`: runtime/module boundary hardening
2. `V2-B`: contract artifact admin and CLI workflows
3. `V2-C`: migration planning and validation
4. `V2-D`: declared read and SQL protocol convergence
5. `V2-E`: policy/auth enforcement foundation
6. `V2-F`: multi-module hosting exploration
7. `V2-G`: out-of-process module execution gate

If new V2 implementation starts from an explicit user target, begin with:
- `docs/hosted-runtime-planning/V2/README.md`
- the selected slice `00-current-execution-plan.md`
- that slice's `01-stack-prerequisites.md`
- live code/package docs named by the slice

Do not invent slices later than V2-G without an explicit new target.

## Next Slice Notes

No next V1.5 or V2 slice is queued. V2-G completed as a defer decision for
production out-of-process execution because preserving transaction mutation,
lifecycle cleanup, durability, and subscription ordering needs a dedicated
design before a runner is justified.

Future hosted-runtime work should either be a new explicit target or a
regression fix. Preserve the rule that each handoff completes one full lettered
slice, including tests and validations.

## Coordination Notes

There may be concurrent gauntlet/dependency test work in the same worktree.
Avoid touching these unless the user explicitly redirects the work:
- `docs/RUNTIME-HARDENING-GAUNTLET.md`
- `go.mod`
- `go.sum`
- `runtime_gauntlet_test.go`
- rapid/fuzz-style test files under `bsatn/`, `commitlog/`, `query/sql/`, and
  `store/`

Do not push unless explicitly asked.

## Validation

For the active V2 slice, run and record the validation gates from that slice's
`05-format-and-validate.md`. Do not claim a V2 slice is complete until the
focused tests, required format command, required vet command, and any required
broader test run pass.

Completed V2-A validation:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Boundary|Describe|Export|Contract|Lifecycle|Build)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -count=1`

Completed V2-B validation:
- `rtk go fmt ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`
- `rtk go test ./codegen ./contractdiff ./contractworkflow ./cmd/shunter -count=1`
- `rtk go test ./... -run 'Test.*(Contract|Codegen|Diff|Policy)' -count=1`
- `rtk go vet ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`

Completed V2-C validation:
- `rtk go fmt ./contractdiff ./contractworkflow ./cmd/shunter`
- `rtk go test ./contractdiff ./contractworkflow ./cmd/shunter -count=1`
- `rtk go test ./... -run 'Test.*(Migration|ContractDiff|Policy|Plan)' -count=1`
- `rtk go vet ./contractdiff ./contractworkflow ./cmd/shunter`

Completed V2-D validation:
- `rtk go fmt . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`
- `rtk go test . -run 'Test.*(Declaration|Contract|Read|Query|View)' -count=1`
- `rtk go test ./protocol ./query/sql ./subscription -count=1`
- `rtk go test ./codegen ./contractdiff -count=1`
- `rtk go vet . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`
- `rtk go test ./... -count=1`

Completed V2-E validation:
- `rtk go fmt . ./auth ./protocol ./executor ./codegen ./types`
- `rtk go test . -run 'Test.*(Permission|Auth|Reducer|Local|Network)' -count=1`
- `rtk go test ./auth ./protocol ./executor ./codegen ./types -count=1`
- `rtk go vet . ./auth ./protocol ./executor ./codegen ./types`
- `rtk go test ./... -count=1`

Completed V2-F validation:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Host|MultiModule|Runtime|Network|Contract)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./protocol ./subscription ./executor -count=1`
- `rtk go test ./... -count=1`

Completed V2-G validation:
- `rtk go fmt . ./executor ./store ./subscription ./protocol ./internal/processboundary`
- `rtk go test ./executor ./store ./subscription ./protocol ./internal/processboundary -count=1`
- `rtk go test . -run 'Test.*(Runtime|Lifecycle|Local|Contract)' -count=1`
- `rtk go vet . ./executor ./store ./subscription ./protocol ./internal/processboundary`

Completed V1.5-E validation:
- `rtk go fmt <touched packages>`
- `rtk go test ./... -run 'Test.*Migration|Test.*ContractDiff|Test.*Policy' -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet <touched packages>`

Pinned Staticcheck is available as `rtk go tool staticcheck ./...` and is
expected to be green after OI-008 cleanup. Treat failures as real cleanup
findings unless a task explicitly narrows verification.

Do not claim a Go implementation slice is complete until the relevant Go
commands pass.

## Handoff Upkeep

For future hosted-runtime handoff updates:
- make the active target explicit
- keep startup reading minimal
- advance the active target only after the whole lettered slice, its tests, and
  its validation gates are complete
- record only future-relevant state, not closure archaeology
