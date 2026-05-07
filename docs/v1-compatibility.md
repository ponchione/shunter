# Shunter v1 Compatibility Matrix

Status: initial v1 contract slice
Scope: app-facing Go APIs, Shunter-native protocol, contract JSON, generated
TypeScript, read surfaces, local runtime APIs, and multi-module hosting.

This is Shunter's support matrix for the current v1 readiness line. It is not
a SpacetimeDB compatibility checklist. Shunter v1 is a Go-native hosted
runtime with reducer-owned writes, Shunter-native wire protocol, and
contract-driven clients.

## Support Levels

- **Stable for v1**: intended to keep source, wire, JSON, or generated-client
  compatibility through v1.x unless a new major version or explicit versioned
  artifact is introduced.
- **Preview/advanced**: usable and tested, but not required for normal v1 app
  development. Details may change before or during v1.x with release notes.
- **Internal implementation detail**: importable code may exist, but it is not
  a supported application contract.
- **Explicitly out of scope for v1**: not promised by v1 and should not shape
  app designs that need v1 compatibility.

Root package errors inherit the support level of the API surface that returns
them. Public methods on unexported concrete types are internal implementation
details.

## Root Package Support

Package: `github.com/ponchione/shunter`

| Surface | Support | v1 contract |
| --- | --- | --- |
| Module declaration | Stable for v1 | `NewModule`, `Module.Name`, `Module.Version`, `Module.VersionString`, `Module.Metadata`, `Module.MetadataMap`, `Module.SchemaVersion`, `Module.TableDef`, `Module.Reducer`, `Module.OnConnect`, `Module.OnDisconnect`, `Module.Query`, `Module.View`, `Module.VisibilityFilter`, `Module.Migration`, `Module.TableMigration`, `Module.Describe`, `WithReducerPermissions`, `QueryDeclaration`, `ViewDeclaration`, `VisibilityFilterDeclaration`, `ModuleDescription`, `QueryDescription`, `ViewDescription`, `PermissionMetadata`, `ReadModelMetadata`, `ReadModelSurfaceQuery`, and `ReadModelSurfaceView`. |
| Runtime lifecycle | Stable for v1 | `Build`, `Config`, `ProtocolConfig`, `AuthMode`, `AuthModeDev`, `AuthModeStrict`, `Runtime.Start`, `Runtime.Close`, `Runtime.Ready`, `Runtime.ModuleName`, `Runtime.Config`, `Runtime.HTTPHandler`, and `Runtime.ListenAndServe`. |
| Local reducers | Stable for v1 | `Runtime.CallReducer`, `ReducerCallOption`, `WithRequestID`, `WithIdentity`, `WithConnectionID`, `WithAuthPrincipal`, `WithPermissions`, `ReducerResult`, `ReducerStatus`, status constants, `ReducerDB`, `Value`, `ProductValue`, `AuthPrincipal`, `RowID`, and `TxID`. |
| Local committed reads | Stable for v1 | `Runtime.Read`, `LocalReadView`, `IndexKey`, `IndexBound`, `NewIndexKey`, `UnboundedLow`, `UnboundedHigh`, `Inclusive`, and `Exclusive`. The read view is callback-scoped only. |
| Declared reads | Stable for v1 | `Runtime.CallQuery`, `Runtime.SubscribeView`, `DeclaredReadOption`, `WithDeclaredReadIdentity`, `WithDeclaredReadConnectionID`, `WithDeclaredReadAuthPrincipal`, `WithDeclaredReadPermissions`, `WithDeclaredReadAllowAllPermissions`, `WithDeclaredReadRequestID`, `DeclaredQueryResult`, and `DeclaredViewSubscription`. Metadata-only declarations are not executable. |
| Contract export and validation | Stable for v1 | `Runtime.ExportSchema`, `Runtime.ExportContract`, `Runtime.ExportContractJSON`, `ModuleContract`, all nested contract structs, `ModuleContractVersion`, `ModuleContractFormat`, `DefaultContractSnapshotFilename`, `ValidateModuleContract`, `ModuleContract.Validate`, `ModuleContract.MarshalCanonicalJSON`, `MigrationSurfaceTable`, `MigrationSurfaceQuery`, and `MigrationSurfaceView`. |
| Build metadata | Stable for v1 | `Version`, `Commit`, `Date`, `BuildInfo`, and `CurrentBuildInfo`. These report Shunter runtime/tool build metadata, not app module metadata. |
| Health status classification | Stable for v1 | `HealthStatus`, health status constants, `HealthzStatusCode`, and `ReadyzStatusCode`. |
| Runtime diagnostics payloads and observability hooks | Preview/advanced | `Runtime.Health`, `Runtime.Describe`, `InspectRuntimeHealth`, `ClassifyRuntimeHealth`, `RuntimeDiagnosticsHandler`, `RuntimeHealth`, health sub-structs, `RuntimeDescription`, `ObservabilityConfig`, `RedactionConfig`, `MetricsConfig`, `MetricsRecorder`, `MetricName`, metric constants, `MetricLabels`, `ReducerLabelMode`, `DiagnosticsConfig`, `TracingConfig`, `Tracer`, `Span`, and `TraceAttr`. |
| Offline operations and migration hooks | Preview/advanced | `CheckDataDirCompatibility`, `Runtime.WaitUntilDurable`, `Runtime.CreateSnapshot`, `Runtime.CompactCommitLog`, `BackupDataDir`, `RestoreDataDir`, `Module.MigrationHook`, `MigrationHook`, `MigrationContext`, `MigrationRunResult`, `MigrationHookResult`, `RunDataDirMigrations`, `RunModuleDataDirMigrations`, `MigrationMetadata`, and migration compatibility/classification constants. Migration metadata is exported in stable contract JSON, but operational migration workflow is still advanced. |
| Multi-module host | Preview/advanced | `HostRuntime`, `NewHost`, `Host`, host lifecycle/serving/runtime lookup/health/description APIs, `HostDiagnosticsHandler`, and host health/description structs. See [Multi-Module Host](#multi-module-host). |
| Protocol adapter methods on `Runtime` | Internal implementation detail | `Runtime.HandleDeclaredQuery` and `Runtime.HandleSubscribeDeclaredView` are exported only so runtime-owned protocol wiring can delegate to declared-read APIs. App code should use `CallQuery`, `SubscribeView`, or the WebSocket protocol. |

## Lower-Level Packages

| Package | Support | v1 stance |
| --- | --- | --- |
| `schema` | Stable for v1 subset | App-authored schema declaration/export types used through root APIs: table and column definitions, table options/read policy, reducer handler/context types, value kind export strings, and schema export metadata. Engine/builder internals remain advanced. |
| `types` | Stable for v1 subset | Values, product rows, identities, connection IDs, transaction/row IDs, auth principal, reducer DB/context contracts, and value constructors/accessors used by root APIs, protocol rows, and BSATN. |
| `bsatn` | Stable for v1 | Shunter's binary value and product-row encoding for v1 protocol/runtime boundaries. It is Shunter-native and not a promise of another runtime's byte-for-byte format. |
| `codegen` | Stable for v1 TypeScript target | `Generate`, `GenerateFromJSON`, `GenerateTypeScript`, `Options`, and `LanguageTypeScript` for valid v1 `ModuleContract` inputs. |
| `protocol` | Preview/advanced Go API, stable wire contract | The wire protocol is stable as documented below. The Go package is a low-level client/server helper surface and may change where it is not part of the wire payload contract. |
| `contractdiff` and `contractworkflow` | Preview/advanced | Useful review and CLI workflow helpers, but v1 migration policy hardening is still in progress. |
| `auth` and `observability/prometheus` | Preview/advanced | Supported for Shunter-owned runtime wiring and advanced integrations. Production auth/ops policy is still a separate v1 readiness track. |
| `store`, `subscription`, `executor`, `commitlog`, and `query/sql` | Internal implementation detail | These are runtime implementation packages. Prefer root APIs, contract JSON, generated clients, or the Shunter protocol unless a task explicitly owns subsystem work. |
| `internal/*` | Internal implementation detail | Go-internal packages have no app compatibility promise. |
| `reference/SpacetimeDB/*` | Explicitly out of scope for v1 | Research-only, read-only reference material. Do not import, copy, or treat it as a Shunter compatibility target. |

## Protocol Compatibility

Support: **stable for v1 wire compatibility**.

- The only v1 WebSocket subprotocol token is `v1.bsatn.shunter`.
- `ProtocolVersionV1` is the minimum, current, and only supported protocol
  version. A future incompatible protocol must use a new negotiated token; it
  must not silently widen v1 semantics.
- The wire format is Shunter-native binary frames: `[tag byte][BSATN body]`,
  with the runtime's compression envelope where negotiated.
- Client-to-server stable message families: `SubscribeSingle`,
  `UnsubscribeSingle`, `SubscribeMulti`, `UnsubscribeMulti`, `CallReducer`,
  `OneOffQuery`, `DeclaredQuery`, and `SubscribeDeclaredView`.
- Server-to-client stable message families: `IdentityToken`,
  `SubscribeSingleApplied`, `UnsubscribeSingleApplied`,
  `SubscribeMultiApplied`, `UnsubscribeMultiApplied`, `SubscriptionError`,
  `TransactionUpdate`, `TransactionUpdateLight`, and `OneOffQueryResponse`.
- Tag `0` and tags `128` through `255` are reserved in v1. Server tag `7` is
  reserved for the retired reducer-call result envelope.
- Unknown, unassigned, reserved, malformed, or trailing-byte messages are fatal
  protocol errors in v1.
- Row batches and subscription updates use Shunter's row-list and flat update
  payloads. Reference wrapper chains, energy fields, and SpacetimeDB wire
  compatibility are out of scope for v1.

## ModuleContract JSON

Support: **stable for v1**.

Stable v1 producers emit `contract_version: 1` and the following top-level
fields:

- `module`: app module `name`, `version`, and string `metadata`
- `schema`: schema version, tables, columns, indexes, read policy, and reducers
- `queries` and `views`: declaration names and optional executable SQL
- `visibility_filters`: validated SQL, returned table name/ID, and
  caller-identity usage
- `permissions`: reducer/query/view permission metadata
- `read_model`: query/view read-model metadata
- `migrations`: descriptive module/table/query/view migration metadata
- `codegen`: `contract_format`, `contract_version`, and
  `default_snapshot_filename`

Compatibility rules:

- `ModuleContract.MarshalCanonicalJSON` is the canonical emitted JSON format.
- `ValidateModuleContract` validates the known v1 fields and SQL/read metadata.
- v1 readers must ignore unknown JSON fields so additive metadata can be
  introduced without breaking old consumers.
- v1 producers must not change the type or meaning of an existing known field
  without a new contract version.
- `Module.Version(...)` is app-owned module metadata in `module.version`; it is
  not the Shunter runtime/tool version.

## TypeScript Codegen

Support: **stable for v1 TypeScript contract generation**.

Generated TypeScript from `codegen.GenerateTypeScript` or the contract workflow
helpers includes:

- protocol metadata from the runtime constants
- row interfaces for exported tables
- `tables`, `TableName`, `TableRows`, and table read policies
- visibility filter metadata
- reducer and lifecycle reducer constants
- reducer call helper functions for non-lifecycle reducers
- query/view constants, executable query/view SQL maps, and declared-read
  helper functions for declarations with non-empty SQL
- permissions and read-model metadata
- type mappings for the current exported Shunter value kinds

Generated identifier normalization and collision suffixes are stable for v1
codegen output. Names are emitted as TypeScript-safe identifiers by splitting
on non-letter and non-digit separators, applying the category's camel or Pascal
case style, prefixing leading digits with `_`, suffixing reserved words with
`_`, and appending numeric collision suffixes in contract order.

Out of scope for v1 codegen:

- a maintained npm package or full client runtime
- React/cache/reconnect SDK behavior
- generated writes that bypass reducers
- SpacetimeDB client API compatibility

## Read Surface Matrix

| Read surface | Support | v1 SQL/read contract |
| --- | --- | --- |
| One-off raw SQL | Stable for v1 | Protocol `OneOffQuery` executes the Shunter SQL subset against a committed snapshot. Supported shapes include single-table reads, bounded joins/multi-way joins, column projections and aliases, `COUNT`/`SUM` aggregates including `COUNT(DISTINCT column)`, `ORDER BY`, `LIMIT`, and `OFFSET`. Raw SQL is governed by table read policy and visibility filters. There is no root-level local raw SQL API in v1. |
| Declared queries | Stable for v1 | `QueryDeclaration.SQL`, `Runtime.CallQuery`, and protocol `DeclaredQuery` use the one-off read executor with declaration-level permission metadata. Declared queries may expose private tables when the declaration permission allows the caller. Empty SQL is metadata-only and returns `ErrDeclaredReadNotExecutable` when executed. |
| Raw subscriptions | Stable for v1 | Protocol `SubscribeSingle` and `SubscribeMulti` register table-shaped live reads. They support table-shaped single-table and join/multi-way subscription predicates, including `SELECT *` for single tables and `SELECT table.*`/alias-shaped emitted relations for joins. Raw subscriptions reject column projections, aggregates, `ORDER BY`, `LIMIT`, and `OFFSET`. Raw subscription admission is governed by table read policy and visibility filters. |
| Declared live views | Stable for v1 | `ViewDeclaration.SQL`, `Runtime.SubscribeView`, and protocol `SubscribeDeclaredView` register named live views with declaration-level permissions. Supported live view shapes include table-shaped reads, table-shaped joins/multi-way joins, column projections over the emitted relation, single-table `ORDER BY`, `LIMIT`, and `OFFSET` initial snapshots, single-table `COUNT`/`SUM` aggregates, and join/cross-join `COUNT`/`SUM` aggregates including multi-way joins. Declared live views reject aggregate aliases without `AS`. |
| Local runtime reads | Stable for v1 | `Runtime.Read` is callback-scoped committed-state access. `Runtime.CallQuery` and `Runtime.SubscribeView` are the local declared-read APIs. A local ad hoc raw SQL API is out of scope for v1; declare a query or view instead. |

## Multi-Module Host

Support: **preview/advanced**.

`Host` composes already-built single-module runtimes under explicit route
prefixes. Each runtime keeps its own schema, data directory, lifecycle,
transactions, protocol route, and `ModuleContract`.

Stable expectations for the preview:

- route prefixes are explicit and must not overlap
- module names must match their built runtimes
- data directories must not collide
- `Host.Start`, `Host.Close`, and `Host.ListenAndServe` coordinate the hosted
  runtimes in registration order/reverse close order

Explicitly out of scope for v1:

- cross-module transactions
- cross-module SQL or subscriptions
- merged multi-module schema or contract artifacts
- a global reducer/query namespace
- dynamic module loading or upload

## Mismatches And Decisions

Resolved in this slice:

- The app-author guide said live views reject column projections and
  aggregates. Current code and tests support declared live-view column
  projections and narrow single-table `COUNT` aggregates
  (`declared_read.go`, `declared_read_catalog.go`,
  `subscription/projection.go`, `subscription/aggregate.go`,
  `declared_read_test.go`, and `subscription/aggregate_test.go`), so
  `docs/how-to-use-shunter.md` now documents that support and the remaining
  rejections.

Current code and docs now agree that declared live views accept single-table
`ORDER BY`, `LIMIT`, and `OFFSET` initial snapshots for table-shaped and
projected views while post-commit delivery remains row deltas over matching
rows.

The multi-module `Host` remains preview/advanced for v1. It is useful for
composition tests and explicit multi-runtime deployments, but normal v1 app
development should build one runtime per hosted module and should not depend on
cross-module behavior.

No lower-level package beyond the stable subsets listed in this matrix receives
a normal Go compatibility promise for v1. Runtime implementation packages stay
implementation details even when importable.

Open decisions before cutting `v1.0.0`:

- Add or confirm protocol, contract JSON, and TypeScript golden coverage for
  every stable payload shape in this document.
