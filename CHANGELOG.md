# Changelog

Shunter uses source versions from `VERSION` and release tags named `vX.Y.Z`.

## Unreleased

- Strict JWT extra-claim preservation now rejects excessive configured claim
  counts and non-JSON or overly deep JSON claim values before exposing caller
  context to reducers and procedures.
- Documented that `@shunter/client` npm ownership and release policy remain
  unsettled, so the TypeScript runtime package stays private and current app
  consumption remains workspace, `file:`, or locally packed tarball based.
- Added a deferred `@shunter/client` npm publishing policy proposal while
  keeping npm publishing and package metadata behavior unchanged.
- Generated TypeScript `shunterContract` metadata now records the normalized
  generation profile and runtime import target for release traceability.
- TypeScript public-profile codegen now uses explicit table
  `sdk.visibility` metadata to hide internal, private, and system table helper
  surfaces while preserving declared query/view helpers and declared-read row
  codecs.
- Contract table exports now include explicit `sdk.visibility` metadata for
  public, internal, private, and system table classification, and TypeScript
  codegen accepts explicit `internal`, `full`, or `public` profiles through Go
  options and `shunter contract codegen --profile`.
- Protocol one-off SQL ordered reads now avoid per-row ORDER BY key slices on
  single-table sorts and defer ordered join projection until after windowing,
  reducing allocation traffic on hot query paths.
- Strict protocol auth failures now complete the WebSocket upgrade when a
  supported Shunter subprotocol is offered, then close with 1008 and
  `auth-token rejected by admission` so browser clients can classify token
  rejection.
- TypeScript runtime WebSocket close handling now classifies auth/token
  rejection close reasons as `ShunterAuthError` and does not retry them through
  reconnect.
- Detached contract validation now rejects names that duplicate after trimming
  surrounding whitespace, and schema engine startup checks now retain a
  build-time copy of recovered snapshot metadata.
- Schema export type parsing now shares the same canonical mapping used for
  export formatting, and fixed-size identity/connection ID hex parsing now
  decodes directly into destination arrays without changing invalid-input
  zero-value behavior.
- JWT validation now parses each token's unverified header and issuer in one
  pass before signature verification.
- Strict JWT validation can now preserve explicitly configured, bounded extra
  claims as copy-isolated reducer/procedure caller context, including
  Supabase-style delegated-auth claims without mapping provider `role` values
  to Shunter permissions.
- Strict auth now supports generic OIDC discovery issuers as a separate
  key-discovery path that resolves discovery documents into JWKS verification
  sources while preserving explicit JWKS configuration unchanged.
- Non-caller transaction-update fanout now treats connections that disappear
  during delivery as skipped, matching the documented missing-connection
  behavior while avoiding a duplicate manager lookup.
- Scheduler ID allocation now atomically consumes the last non-zero ID and
  reports exhaustion without ever inserting `schedule_id = 0`.
- Recovery `ApplyChangeset` now treats a nil changeset as an empty no-op,
  matching the existing nil changeset helpers.
- Ordered live-view initial materialization now avoids per-row ORDER BY key
  copies and only encodes deterministic tie-break keys when comparisons need
  them, reducing allocations for bounded ordered windows.
- Contract diff and migration-plan tooling now report procedure additions,
  removals, argument/result schema drift, and procedure permission drift.
- One-off `COUNT(*)` over unfiltered cross joins now rejects overflowing
  row-count products instead of silently wrapping the aggregate result.
- Contract workflow JSON argument decoding now accepts the decimal-string
  integer forms emitted by contract-aware JSON row rendering, including full
  128-bit and 256-bit integer values.
- Store index-key copies now reuse `types.Value.Copy`, avoiding redundant JSON
  reparse/canonicalization while preserving defensive-copy isolation.
- Declared live views now support the bounded multi-way inner-join projection
  shape used by flattened leaderboard streams: equality joins and filters with
  aliased projected columns from joined relations, plus projected before/after
  bag deltas for transactions that change multiple joined relations.
- Generated TypeScript clients now include a module-bound
  `createModuleClient` facade that groups reducer, procedure, declared-query,
  declared-view, table subscription, and event-table insert helpers around a
  connected runtime client; hosted-chat now uses this facade in its frontend
  gate.
- Added a module-linked hosted-chat maintenance command for offline DataDir
  compatibility preflight and registered migration-hook execution, and extended
  the hosted-chat gate to exercise fresh and compatible preflight flows.
- Added hosted-app DataDir compatibility reports and safe additive recovery for
  schema-version-only drift, added tables, and appended non-unique/non-primary
  indexes while keeping row-shape changes, table drops, and new unique/primary
  constraints blocked for app-owned migrations.
- Hardened the hosted-chat TypeScript frontend cleanup path so subscription
  unsubscribe and client close steps fail with bounded diagnostics instead of
  hanging the example gate indefinitely.
- TypeScript managed declared-view handles now accept subscription initial
  updates that omit delete row lists, treating absent deletes as empty while
  preserving raw-handle behavior.
- Declared single-table, non-aggregate live views with `ORDER BY`, `LIMIT`, or
  `OFFSET` now maintain window membership after commits, emitting delete/insert
  row deltas when rows leave or enter the live window. Equal `ORDER BY` keys
  and unordered `LIMIT`/`OFFSET` windows use Shunter's deterministic row-payload
  tie-break order.
- Added strict-auth JWKS/OIDC issuer verification for RS256 and ES256 tokens,
  including on-demand key fetch, cached key reuse, keyed unknown-`kid` refresh,
  HTTPS-by-default URL validation, root config/env wiring, and app-author docs.
- Strict-auth JWKS validation now refreshes cached issuers on an unknown token
  `kid` even when unrelated local verification keys are configured.
- Added the root `Module.EventTable` declaration helper, documented app-facing
  event-table reducer usage, and wired hosted-chat to emit transient
  `message_events`.
- Generated TypeScript clients now emit event-table subscription helpers, and
  the TypeScript SDK can mark table subscriptions as event-table streams so
  managed handles do not retain transient inserts as cached state.
- Fixed event-table metadata preservation in contract-derived schema lookups and
  startup snapshot compatibility checks.
- Fixed event-table subscription evaluation so transient inserts participate in
  joins, aggregates, multi-table deltas, and fan-out delivery even though the
  rows are not retained in committed snapshots.
- Added initial event-table support: tables can be declared transient through
  schema metadata, exported in contracts, surfaced in generated TypeScript
  metadata, emitted through commit changesets, and excluded from committed
  state/recovery persistence.
- Added running-app `shunter query --sql` for contract-decoded, read-only
  one-off SQL queries over the Shunter WebSocket protocol.
- Extended the hosted-chat release gate to prove Phase 1/2 closure through
  clean server stop, offline backup/restore, restart from restored state, and
  recovered declared-query results.
- The hosted-chat example now cancels `shunter.Run` on interrupt or SIGTERM so
  the hosted runtime can shut down cleanly under normal process termination.
- Procedure protocol responses now use the runtime client sender path so
  outbound backpressure triggers the same disconnect handling as other
  running-app responses.
- Added first-class procedure declarations, local and WebSocket procedure
  calls, contract assertions, TypeScript generated procedure helpers, and a
  hosted-chat procedure gate.
- Fixed the TypeScript client procedure caller to resolve with the procedure
  result bytes instead of the full protocol response frame.
- Added running-app `shunter health --url` and `shunter describe --url`
  diagnostics checks, including `/subscribe` URL rewriting, query/fragment
  stripping, and structured failed-health output from `/healthz`; extended the
  hosted-chat gate to exercise them against a live server.
- Added `shunter call` and `shunter query` for contract-validated reducer calls
  and declared-query reads against running app servers over the Shunter
  WebSocket protocol.
- Fixed running-app CLI credential precedence so `SHUNTER_TOKEN` is still used
  when `--allow-dev-anonymous` is present; the anonymous mode is only used when
  no token source resolves.
- Added explicit `--allow-dev-anonymous` support for running-app CLI commands
  and `protocolclient.Options.AllowAnonymous` for local dev-auth workflows
  without silently weakening token-required admin defaults.
- Extended the hosted-chat gate to start a real server, run one CLI reducer
  call, and verify one declared-query response.
- Added `contractworkflow.DecodeReducerResult` and
  `DecodeReducerResultJSONRow` for contract-aware decoding of local reducer
  return BSATN bytes.
- Added `contractworkflow.ReducerResultSchema` for reducer result metadata
  lookup from local contracts.
- Added `contractworkflow.PrepareReducerCallRequest` for contract-validated
  reducer request preparation without coupling contract workflow code to the
  protocol client package.
- Added `protocolclient.DialAndCallReducer` for one-shot reducer calls with
  explicit bearer-token transport and automatic connection close.
- Added `contractworkflow.PrepareDeclaredQueryRequest` for contract-validated
  declared-query request preparation without coupling contract workflow code to
  the protocol client package.
- Added `contractworkflow.JSONQueryRows` helpers for contract-aware
  declared-query JSON rows with query and table metadata.
- Changed contract-aware declared-query JSON row rendering to emit `int64`,
  `uint64`, `timestamp`, and `duration` values as decimal strings, matching the
  generated TypeScript `bigint` surface without losing JSON precision.
- Changed `contractworkflow.EncodeOptionalQueryArguments` to mirror runtime
  declared-read parameter semantics: empty parameter schemas are treated as
  no-parameter queries, and non-empty parameter schemas require supplied JSON
  arguments.
- Added `contractworkflow.DecodeQueryResponseJSONRows` to compose
  declared-query response decoding with contract-aware JSON row rendering.
- Added `contractworkflow.ProductValueToJSONRow` and
  `DecodedQueryRowsToJSONRows` for contract-aware JSON rendering of decoded
  product rows.
- Added `contractworkflow.DecodeQueryResponse` for contract-aware decoding of
  single-table declared-query protocol responses.
- Added `contractworkflow.DecodeQueryRows` and `QueryRowSchema` for decoding
  declared-query RowList payloads through local contract row metadata.
- Added `contractworkflow.EncodeSurfaceArguments` for future running-app CLI
  code to select reducer or declared-query JSON argument encoding by contract
  surface kind.
- Added typed `protocolclient` reducer-call and parameterized declared-query
  request helpers for future running-app admin commands.
- Added a typed `protocolclient.DeclaredQuery` helper for schema-less or
  no-parameter declared-query requests.
- Added `contractworkflow.EncodeReducerArguments` and `EncodeQueryArguments`
  for named contract-surface JSON argument encoding.
- Added `contractworkflow.EncodeProductValueArguments` for contract-schema JSON
  argument conversion directly to schema-aware BSATN bytes.
- Added command-specific examples to `shunter contract assert --help`.
- Added `assertion_count` and `failure_count` to `contract assert --format json`
  output for aggregate release-gate checks.
- Added `contract assert` help examples and test coverage for zero-assertion
  contract inventory checks.
- Changed `shunter contract assert --format json` assertion entries to expose
  typed expected/actual string or number fields for script-friendly gates.
- Added `shunter contract assert --contract` for local ModuleContract release
  gates with module, module-version, contract-version, schema-version, and
  app-surface count assertions in text or JSON form.
- Added the hosted-backend app path with `shunter.Run`, `ConfigFromEnv`, a
  canonical `examples/hosted-chat` app, TypeScript generation workflow, and a
  hosted-chat release gate script.
- Added `shunter describe --contract` for local ModuleContract inventory in
  text or JSON form, and wired the hosted-chat gate to exercise it.
- Added `shunter health --contract` for contract-local validation status in
  text or JSON form, without implying a running server probe.
- Added `shunter contract validate --contract` for explicit local
  ModuleContract validation in text or JSON form.
- Protocol compression-envelope decoding now applies the default message-size
  limit in `UnwrapCompressed`, preventing unbounded gzip expansion by default.
- Commit-log `DecodeRecord` now applies the default max payload limit when
  callers pass zero, rejecting oversized headers before payload allocation.
- Protocol dispatch now recovers panics from detached message-handler
  goroutines, records an internal protocol error, and closes the connection
  with 1011 instead of letting one bad handler crash the process.
- TypeScript client unsubscribe request IDs now avoid pending subscription
  request IDs, preventing response routing ambiguity after explicit low IDs.
- Protocol option defaults and validation now share one normalization path
  between runtime config and transport setup.
- Protocol dispatch now treats a nil handler table as unsupported messages
  instead of panicking, and client-message count guards reject invalid offsets
  before count math.
- Commit-log segment, offset-index, and snapshot opens now verify the opened
  file still matches the pre-open regular-file check, closing a path
  replacement race.
- Protocol upgrade auth now rejects malformed `Authorization` headers before
  considering query-token fallback or anonymous token minting.
- SQL bytes coercion no longer treats escaped string text beginning with
  uppercase `X'` as a hex literal; proper `X'..'` hex tokens still decode
  normally.
- SQL bytes coercion now accepts string literals with uppercase `0X` hex
  prefixes, matching existing `0X...` token handling.
- Strict protocol auth now supports local multi-key JWT verification through
  `Config.AuthVerificationKeys`, including HS256, RS256, ES256, and optional
  `kid` matching for overlapping key rotation.
- Live multi-way joins now support opt-in production guardrails through
  `Config.SubscriptionMaxMultiJoinRelations` and
  `Config.SubscriptionMaxMultiJoinRowsPerRelation`.
- Multi-way join row guardrails now run before post-commit evaluation even when
  pruning would otherwise skip the live view.
- Restored the source and private TypeScript client package metadata to the
  post-`v1.1.0` `v1.1.1-dev` development line.
- New commit-log segments, offset indexes, snapshot files, and snapshot lock
  markers now use owner-only permissions so persisted application state is not
  world-readable under a permissive process umask.
- Newly-created runtime DataDirs now use owner-only directory permissions so
  persisted state paths are not group- or world-traversable.
- Snapshot creation now rejects symlinked transaction snapshot directories
  before writing lock, temporary, or published snapshot artifacts.
- SQL decimal and exponent literals now stay floating-point unless their source
  text is exactly integral, avoiding integer coercion through `float64`
  boundary rounding.
- SQL parsing now rejects excessively nested predicates with a normal
  unsupported-SQL error, and the TypeScript client rejects impossible BSATN
  string-array counts before walking the partial payload.
- BSATN decoding now rejects oversized variable-length payloads and
  string-array shapes before reading payload bytes.
- Dev anonymous auth now rejects negative token TTLs instead of silently
  minting non-expiring tokens, and JSON values reject non-UTF-8 byte payloads
  before canonicalization.
- Commit-log rotation now avoids wrapping the next segment start to tx_id 0
  after the terminal tx_id is durably written.
- Commit-log recovery and append-resume scans now reject segment history that
  wraps from the maximum tx_id back to tx_id 0.
- Scheduled repeat timestamps now reject or terminate at the int64 nanosecond
  boundary instead of wrapping the next run into the past.

## v1.1.0 - 2026-05-13

- Declared query and view parameters now work end to end: Go declarations
  attach typed parameter schemas through `WithQueryParameters` and
  `WithViewParameters`, SQL validation checks declared placeholders, local
  runtime calls bind ordered `ProductValue` parameters, and protocol v2 carries
  BSATN-encoded declared-read parameter payloads.
- Generated TypeScript bindings now emit typed declared-read params
  interfaces, BSATN params encoders, and parameterized declared query/view
  helpers while preserving no-parameter helper signatures and hiding encoded
  params from generated helper options.
- The private `@shunter/client` package metadata now tracks the stable
  `v1.1.0` release as npm version `1.1.0`.
- Moved stale v1 roadmap follow-up into `working-docs/tech-debt.md` and
  restored the source version to a post-release development marker.
- Hardened offline DataDir backup/restore file copies against replaced source
  entries and preserved file permissions after process umask filtering.
- Contract workflow outputs now create new generated files with owner-writable
  `0644` permissions while still preserving existing output file modes.
- Host data-directory conflict checks now resolve symlink aliases before
  comparing registered runtimes.
- Runtime and Host HTTP serving now set defensive read-header and idle timeouts.
- Runtime and Host serving now reject NUL-containing listen addresses before
  calling the Go networking stack.
- Protocol auth rejection responses now return generic client-safe messages
  while preserving detailed causes for internal observation.
- Commit-log segment appends and durability enqueue now reject skipped
  transaction IDs before writing unrecoverable history gaps.
- Snapshot schema decoding now rejects nullable auto-increment columns before
  row recovery can inspect incompatible values.
- The preferred Go toolchain is now pinned to Go 1.26.3, which includes fixes
  for standard-library vulnerabilities reported against Go 1.26.2.
- Added a structured release qualification ledger and wired the release
  checklist to record Shunter/canary refs, command evidence, operator/date,
  result, and residual risks before tagging.
- Added a protocol benchmark row for slow-reader WebSocket backpressure while
  preserving unrelated healthy-client fanout delivery.
- Added protocol regression coverage for caller-specific declared-view
  visibility deltas, including a row moving from one caller's visible set to
  another's.
- Release version reporting now keeps the source or linker-stamped Shunter
  version authoritative instead of replacing it with Go VCS pseudo-version
  metadata when they match.

## v1.0.1 - 2026-05-12

- Made the private `@shunter/client` TypeScript SDK package-shaped for local
  workspace, `file:`, and tarball installs, with built ESM and declaration
  artifacts plus package smoke coverage.
- Generated TypeScript bindings now support runtime import overrides and
  `shunterContract` metadata for stale-binding and protocol compatibility
  checks.
- Hardened generated TypeScript runtime imports for package-scoped and
  app-scoped local SDK installs.
- Hardened DataDir backup/restore containment checks to resolve symlinked
  destination parents before rejecting nested copies.
- Dev auth now includes the anonymous token issuer in configured issuer
  allowlists so minted anonymous tokens validate on reconnect.

## v1.0.0 - 2026-05-12

- Promoted the Shunter v1 compatibility line to the stable `v1.0.0` release
  version after release qualification.
- Release qualification covers the Go hardening command set, TypeScript SDK
  tests, and the external `opsboard-canary` quick/full gates.

## v0.1.1 - 2026-05-12

- Added network-level protocol coverage for slow WebSocket readers and
  write-timeout backpressure, proving an unread client does not block or
  corrupt unrelated fanout delivery.
- Runtime startup now requires rebuilding a fresh Runtime before retrying when
  a startup migration mutates in-memory state but fails before durability
  confirmation.
- Runtime startup now refreshes recovered state and commit-log resume state
  after a failed startup that already durably committed migration hooks, so
  non-dirty migration failures can be retried on the same Runtime.
- Snapshot creation through `Runtime.CreateSnapshot` now captures the current
  committed horizon and snapshot body under one read lock.
- Executor response delivery now uses nonblocking sends, and subscription
  register/unregister commands skip snapshot acquisition when already canceled.
- Commit-log CRC and store value/index hash paths now avoid per-call hasher
  allocations.
- Subscription fanout backpressure now waits directly on inbox capacity,
  shutdown, or context cancellation instead of polling with short timers.
- Hardened protocol outbound delivery against externally closed outbound queues.
- Commit log durability workers now reject non-positive segment sizes and drain
  batch sizes at startup.
- Live subscription filtered cross-join deltas now skip committed table scans
  for unchanged sides of the join.
- Store index seeks avoid redundant RowID slice cloning, and bounded B-tree
  scans reuse their upper bound key.
- Observability error redaction now bounds the raw scan window before
  redacting/truncating long error strings.
- Live subscription initial row limits now apply after supported
  `ORDER BY`/`OFFSET`/`LIMIT` initial-snapshot windowing, and streaming
  single-table `LIMIT` snapshots stop scanning once enough rows are gathered.
- Ordered live subscription initial snapshots now bound their retained row window
  when `LIMIT`, `OFFSET`, or `InitialRowLimit` makes the final result bounded.
- Protocol server writes now use a configurable `ProtocolConfig.WriteTimeout`
  and negotiated gzip skips small frames below the compression threshold.
- Store commit changesets now emit per-table insert/delete rows in stable RowID
  order.
- Subscription fanout now records blocked enqueue duration and supports an
  optional fanout enqueue context.
- Multi-way live subscription deltas now expand from changed relation rows
  instead of diffing full before/after join products.
- Runtime durability waits no longer report success when a pending durability
  waiter is closed without confirming the requested transaction.
- Protocol, BSATN, and commit-log encoders now reject oversized length-prefixed
  payloads instead of silently truncating lengths to `uint32`.
- Release qualification now explicitly includes the external `opsboard-canary`
  gates and recording the Shunter/canary commits used for the run.
- Recovery now rejects directory artifacts named as the first commit-log
  segment instead of treating the data directory as empty, while preserving
  rollover directory cleanup behavior.
- Added `Config.AuthIssuers` for strict JWT issuer allowlist validation.
- Strict JWT validation now rejects future `iat` claims.
- Contract diff and workflow policy checks now classify stable v1 contract
  permission/codegen drift according to the compatibility matrix.
- Runtime builds now write `shunter.datadir.json` metadata that keeps Shunter
  build version and app module version separate and rejects mismatched module
  data directories.
- Live subscription join deltas now avoid per-row committed table rescans when only the changed side's join column is indexed.
- Live subscription initial join snapshots now choose the lower-cost indexed scan side, including filter-reduced candidate sets, when both join columns are indexed.
- Live subscription initial join snapshots now use indexed required equality/range filters to skip unnecessary join probes.
- Live subscription multi-way joins now use local equality/range filter pruning for distinct relation tables instead of table-wide fallback.
- Live subscription multi-way joins now use indexed join-condition existence pruning for distinct relation tables, including same-transaction opposite-side changes.
- Live subscription multi-way joins now use alias-aware local equality/range pruning for repeated relation tables when every relation instance has a required local filter.
- Live subscription multi-way joins now use indexed join-condition existence pruning for repeated relation tables when every relation instance has an indexed condition edge.
- Live subscription multi-way joins now combine alias-local filter pruning with indexed join-condition existence pruning for repeated relation tables.
- Live subscription multi-way joins now use indexed existence pruning for required filter-level column equalities, including repeated relation aliases.
- Live subscription multi-way joins now use per-relation indexed existence pruning for disjunctive filter-level column equalities when every OR branch covers that relation.
- Live subscription multi-way joins now combine local value/range pruning with indexed column-equality existence pruning for mixed-branch OR filters when every OR branch covers that relation.
- Live subscription multi-way joins now use indexed remote value/range filter-edge pruning for required relation-local filters, including repeated relation aliases.
- Live subscription multi-way joins now use two-hop indexed path-edge pruning for non-key-preserving remote value/range filters.
- Live subscription multi-way joins now use three-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use four-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use five-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use six-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use seven-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use eight-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use bounded generic path-edge pruning for longer non-key-preserving remote value/range filters without adding fixed per-hop index types.
- Live subscription multi-way split-OR branches now use branch-local column-equality connectors to place remote value/range filter-edge pruning.
- Live subscription multi-way split-OR column-equality branches now fall back instead of keeping partial existence-edge pruning when any branch lacks indexed coverage.
- Live subscription multi-way joins now split OR filters with alias-local value/range branches on directly joined relation instances into local and filter-edge pruning placements.
- Live subscription multi-way joins now split OR filters across same-key transitive condition paths into endpoint-local and remote filter-edge pruning placements.
- Live subscription join value/range filter-edge pruning now admits candidates from same-transaction opposite-side inserts and deletes.
- Live subscription joins now use direct split-OR local/filter-edge pruning when the OR is a required child of an AND filter.
- Live subscription joins now use indexed existence-edge pruning for direct column-equality branches inside split OR filters.
- Live subscription joins now use remote filter/existence branch edges for split OR filters whose changed side has no local branch.
- Live subscription range pruning lookups now return each candidate hash once when overlapping range branches from the same query match a value.
- Live subscriptions and declared views now support two-table column-equality join filters, including inner-join `WHERE` column comparisons and cross-join `WHERE` equality lowering with literal filters.
- Live subscription join candidate pruning now uses required range filters on the opposite joined side when that side's join column is indexed.
- Live subscription cross-side OR pruning now treats not-equals filters as split range placements instead of falling back to broad join-existence candidates.
- Live subscription delta candidate pruning now uses range predicates instead of table-wide fallback when the predicate shape is safely range-constrained.
- Live subscription initial and final snapshots now use matching single-column indexes for equality and compound single-table filters.
- One-off and declared single-table queries now use matching composite indexes for multi-column `ORDER BY`, including mixed directions.
- One-off and declared multi-way join queries now use matching single-column indexes when probing joined relations.
- One-off and declared aggregate queries now ignore null inputs for `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(nullable_numeric_column)`, returning `NULL` for nullable sums with no non-null inputs.
- Declared live views now support column projections over their emitted relation, including projected initial rows and subscription deltas.
- Declared live-view projection deltas now suppress no-op insert/delete replacement rows after the final projected shape is applied.
- Declared live views now support single-table `COUNT(*)` and `COUNT(column)` aggregate rows, including visibility-filtered counts and delete-old/insert-new deltas when the count changes.
- Declared live views now support single-table `SUM(column)` aggregate rows for numeric columns, including nullable sum semantics and delete-old/insert-new deltas when the sum changes.
- Declared live views now support single-table `COUNT(DISTINCT column)` aggregate rows, including visibility-filtered distinct counts and delete-old/insert-new deltas when the distinct count changes.
- Declared live views now support two-table indexed join `COUNT(*)` aggregate rows, including visibility-filtered counts and delete-old/insert-new deltas when the count changes.
- Declared live views now support two-table indexed join `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` aggregate rows, including visibility-filtered values and delete-old/insert-new deltas when aggregate values change.
- Declared live views now support two-table cross-join `COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` aggregate rows, including visibility-filtered values and delete-old/insert-new deltas when aggregate values change.
- Declared live views now support multi-way join `COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` aggregate rows, including visibility-filtered values and delete-old/insert-new deltas when aggregate values change.
- Declared live views now support single-table `ORDER BY` initial snapshots for table-shaped and projected views while retaining row-delta semantics after commits.
- Declared live views now support single-table `LIMIT` initial snapshots for table-shaped and projected views while retaining row-delta semantics after commits.
- Declared live views now support single-table `OFFSET` initial snapshots for table-shaped and projected views while retaining row-delta semantics after commits.
- One-off and declared SQL queries support multi-way joins, and table-shaped multi-way joins now work in live subscriptions and executable declared views.
- Generated TypeScript clients now include a table-name-to-row-type map and a table subscriber callback type derived from it.
- Added canonical JSON column values with schema export, BSATN encoding, SQL literal coercion, store/index support, subscription hashing, contract validation, and TypeScript `unknown` codegen.
- Added nullable column semantics across `types.Value`, schema reflection/export, schema-aware row BSATN, store validation/indexing, SQL `IS NULL` predicates, snapshots/recovery, contract diff, and TypeScript `T | null` codegen.
- Hardened composite secondary index behavior across unique enforcement, reducer index seeks, snapshot/recovery rebuilds, and detached contract validation.
- Documented fixed-point persisted values as an app-owned scaled integer convention over deterministic integer column kinds.
- Added generic auth principals on reducer caller context, populated from validated protocol JWT claims and local call options without changing Shunter identity semantics.
- Protocol reducer failure strings now label app reducer errors, app panics,
  permission denials, and Shunter runtime failures.
- Added the initial `@shunter/client` TypeScript runtime foundation in
  `typescript/client` and updated generated TypeScript bindings to import its
  shared runtime types.
- Added `@shunter/client` protocol compatibility helpers and a managed
  subscription handle primitive with idempotent unsubscribe behavior.
- Added a minimal `createShunterClient` TypeScript WebSocket lifecycle shell
  with token query propagation, subprotocol negotiation, state callbacks,
  `connect()`, `close()`, and idempotent `dispose()`.
- Added TypeScript decoding for the initial server `IdentityToken` frame so
  `createShunterClient().connect()` resolves with identity and connection ID
  metadata.
- Added raw TypeScript reducer request encoding and a connected-client
  `callReducer` send path for the v1 `CallReducerMsg` wire shape.
- Added minimal TypeScript reducer response correlation for full-update
  `callReducer` calls, resolving on committed `TransactionUpdate` frames and
  rejecting on failed reducer updates.
- Added a TypeScript reducer result helper that wraps heavy
  `TransactionUpdate` frames in committed/failed result envelopes.
- Added raw TypeScript declared-query request encoding and
  `OneOffQueryResponse` correlation for byte-level generated query helpers.
- Added a TypeScript raw declared-query result helper that exposes table names,
  raw RowList bytes, split row byte arrays, message ID, duration, and raw frame.
- Added raw TypeScript declared-view subscription request encoding,
  `SubscribeMultiApplied`/`SubscriptionError` correlation, and an idempotent
  `UnsubscribeMulti` send path.
- Added raw TypeScript table subscription request encoding,
  `SubscribeSingleApplied`/`SubscriptionError` correlation, and an idempotent
  `UnsubscribeSingle` send path.
- Added raw TypeScript subscription update callback plumbing for accepted
  declared-view/table subscriptions, including `TransactionUpdateLight`
  decoding.
- TypeScript declared-view/table unsubscribe promises now settle on matching
  unsubscribe acknowledgements or subscription errors instead of resolving
  immediately after send.
- Added TypeScript raw RowList decoding for live server row-batch payloads,
  including per-row byte arrays on decoded one-off query and table initial-row
  envelopes.
- TypeScript raw subscription updates now expose optional decoded insert/delete
  row byte arrays when their payloads are RowList envelopes.
- TypeScript declared-view and table subscriptions can now opt into managed
  subscription handles backed by server-acknowledged unsubscribe paths.
- Generated TypeScript clients now export module-scoped aliases for the
  reducer result envelope and raw declared-query result envelope.
- TypeScript table subscriptions now accept caller-supplied row decoders for
  decoded initial-row and update callbacks while preserving raw callbacks.
- TypeScript declared-query results can now be decoded with caller-supplied
  table row decoders while preserving raw declared-query result helpers.
- Generated TypeScript table and declared-query helpers now expose/pass through
  row decoder option surfaces for decoded table rows and query results.
- TypeScript table subscription handles now hold decoded initial rows when
  callers pass both `returnHandle: true` and a row decoder.
- Generated TypeScript bindings now include schema-aware BSATN table row
  decoders and default generated table subscription helpers to those decoders.
- TypeScript managed table subscription handles now apply RowList insert/delete
  updates using raw row bytes as local row identity.
- The TypeScript runtime now supports explicit opt-in reconnect with bounded
  retry, token-provider refresh per attempt, and subscription replay after a
  fresh identity handshake.
- Hardened the TypeScript runtime lifecycle around stale WebSocket events,
  reconnect token failures, caller close/dispose during reconnect attempts,
  and unsubscribe cleanup during reconnect or failed unsubscribe paths.
- TypeScript declared-view and table subscriptions now stop delivering updates
  as soon as caller unsubscribe begins, even before the server acknowledgement.
- Generated TypeScript reducer helpers now include full-update result-envelope
  wrappers alongside the existing raw byte helpers.
- TypeScript reducer result helpers now convert connected-client reducer
  failures into failed result envelopes for generated helper callers.
- The TypeScript runtime now treats missing or unsupported connected server
  message tags as protocol failures instead of silently ignoring them.
- The TypeScript runtime now fails connected clients on unscoped subscription
  evaluation errors so pending operations and live handles settle explicitly.
- TypeScript table subscription `onRows` and `onInitialRows` callbacks now
  receive raw row bytes when no row decoder is supplied.
- TypeScript declared-view and table subscriptions now reject explicit
  request/query IDs that are pending, active, or awaiting unsubscribe
  acknowledgement.
- TypeScript declared-view and table subscriptions now skip occupied
  request/query IDs when auto-allocating subscription IDs.
- The TypeScript runtime now fails connected clients on scoped subscription
  errors for already accepted subscriptions when no pending operation matches.
- TypeScript reducer calls now reject explicit request IDs that would collide
  with an in-flight full-update reducer response.
- TypeScript reducer calls and declared queries now skip occupied in-flight IDs
  when auto-allocating reducer request IDs and declared-query message IDs.
- TypeScript declared queries now expose a stable validation error code when
  callers reuse an in-flight message ID.
- TypeScript table `onRawRows` callbacks now receive cloned row bytes so raw
  callback mutation cannot corrupt decoded initial rows or managed handles.
- TypeScript token providers now fail before WebSocket creation when they
  resolve to non-string values.
- The TypeScript runtime now defines explicit reducer argument encoder helpers
  for callers that map typed reducer args to raw `Uint8Array` payloads.
- TypeScript declared-view subscriptions now accept the server's single-table
  initial acknowledgement shape used by table-returning declared views.

## v0.1.0 - 2026-05-05

- First Shunter release suitable for use as a normal Go module dependency.
- Published Shunter's WebSocket fork as `github.com/ponchione/websocket v1.8.15-shunter.1` and removed the downstream-only `replace` requirement.
- Added `Runtime.WaitUntilDurable` for app-owned durable acknowledgements.
- Added root `IndexBound`, `Inclusive`, `Exclusive`, `UnboundedLow`, `UnboundedHigh`, and index key helpers.
- Added indexed local reads through `LocalReadView.SeekIndex` and `LocalReadView.SeekIndexRange`.
- Added indexed reducer reads through `ReducerDB.SeekIndex` and `ReducerDB.SeekIndexRange`.
- Added root aliases for reducer-facing `ReducerDB`, `Value`, `ProductValue`, `RowID`, and `TxID`.
- Gzip-negotiated protocol connections now gzip-compress post-handshake server messages while keeping client messages uncompressed.
- Pinned one-off aggregate empty-input semantics for `COUNT` and `SUM`.
- Added `shunter.CheckDataDirCompatibility` for app-owned offline startup schema preflights.
- Added `shunter.RunDataDirMigrations` for app-owned offline executable migrations.
- Added `shunter.RunModuleDataDirMigrations` for offline execution of hooks registered with `Module.MigrationHook`.
- Added app-owned startup migration hooks through `Module.MigrationHook`.
- Tightened generated TypeScript client callback types to use contract-derived table, reducer, and executable declared query/view name unions.
- Improved startup snapshot schema mismatch diagnostics to report multiple structural differences in one failure.
- Added backup/restore guidance to dry-run contract migration plans for blocking or data-rewrite changes.
- Added reusable offline `DataDir` backup and restore helpers for app-owned binaries.
- Added generic CLI commands for offline runtime `DataDir` backup and restore.
- Added runtime snapshot creation and commit log compaction helpers for app-owned maintenance workflows.
- Blocked schema-version drift during log-only recovery when no snapshot can be
  selected, preventing additive migrations from replaying old table IDs through
  an unreconciled current registry.
- Added reusable runtime and host health/readiness inspection helpers.
- Added `Host.ListenAndServe` for app-owned serving of multi-module hosts.
- Added app-owned contract export and runtime-to-codegen file helpers in `contractworkflow`.
- Added narrow `OFFSET <unsigned-integer>` support for one-off SQL and declared queries.
- Added narrow `ORDER BY` support for unique projection output names in one-off SQL and declared queries.
- Added multi-column `ORDER BY` support for one-off SQL and executable declared queries.
- Added aggregate output alias `ORDER BY` support for one-off SQL and executable declared queries.
- One-off single-table `ORDER BY <column> ASC`/`DESC` can now scan a matching single-column index while rechecking filters and visibility.
- Added narrow `COUNT(<column>)` support for one-off SQL and declared queries.
- Added narrow `SUM(<numeric-column>)` support for one-off SQL and declared queries.
- Reject commits before state mutation when the executor TxID allocator is exhausted.
- Reject reducer registration before reducer ID allocation can wrap.
- Fixed store index keys so caller-owned `bytes` and `arrayString` values cannot mutate committed index entries after insert.
