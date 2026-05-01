# Runtime Hardening Gauntlet

This document defines the post-tech-debt test campaign for proving Shunter is
ready for serious application testing. It is not a feature roadmap. It is the
abuse suite Shunter must survive after the known TECH-DEBT items and major
implementation work are landed.

The campaign should find unnamed bugs, not only prevent known regressions. That
means testing Shunter through public surfaces, comparing behavior against a
simple independent model, injecting faults, and running long randomized
workloads with reproducible seeds.

Current status:

- OI-002 and OI-003 are closed for current query/subscription and recovery/store
  evidence. Reopen them only from a fresh Shunter-visible failing example.
- The deterministic root runtime gauntlet in `runtime_gauntlet_test.go` is
  saturated for the current hosted-runtime, protocol, subscription, reducer,
  scheduler, and clean-restart surfaces.
- Public hosted-runtime crash coverage now exercises real subprocess exits
  without `Runtime.Close`, including confirmed-durable protocol recovery,
  caller-acknowledged-before-confirmed durability restart coherence, and
  scheduled wakeup recovery after unclean process exit.
- Public hosted-runtime storage-fault coverage now restarts through safe
  zero-filled active-segment tails, damaged snapshot fallback to a complete log,
  and corrupt segment fail-loud behavior through the `shunter.Build` boundary.
- The current runtime-owned public surface is complete for this campaign. Treat
  new runtime gauntlet work as regression-driven unless a new public invariant,
  feature surface, or failing seed appears.
- Public hosted-runtime/protocol restart coverage also pins rejected reducer
  attempts immediately before restart so failed/panicking reducers cannot
  recover ghost rows or block reuse of the rejected primary key after restart.
- Rejected protocol control-plane requests before restart are pinned through
  one-off query, single subscribe, multi subscribe, single unsubscribe, and
  multi unsubscribe errors so they cannot leave recovered subscriptions or
  corrupt follow-up protocol reads.
- Malformed protocol frames before restart are checked to close only the
  offending connection while leaving the runtime able to commit, restart, query,
  and fan out subscription deltas.
- Protocol transport read-limit failures before restart now get the same
  isolation check through an oversized frame and recovered post-restart fanout.
- Idle protocol clients are also checked through the same restart recovery path,
  pinning keepalive timeout isolation before recovered reads and deltas.
- Declared query and declared view protocol paths are checked across clean
  restart over private base tables, including rejected declared-read
  control-plane requests before restart and live declared-view delta fanout
  after recovery.
- Strict-auth protocol coverage now re-dials after clean restart, rejects
  unauthenticated clients, preserves identity derivation, and verifies
  post-restart reducer fanout.
- Dev-auth protocol coverage now reuses a minted anonymous bearer token with an
  explicit connection ID across clean restart, verifies that disconnected
  subscriptions do not recover as ghost subscribers, and resubscribes before
  checking post-restart fanout.
- Auth validation now has a fixed-seed concurrent JWT validation soak over the
  public `ValidateJWT` surface, checking stable claims, derived identity,
  audience, and permissions under worker/op labels. Permission enforcement now
  also has a fixed-seed metamorphic matrix for grant ordering, duplicate grants,
  empty required entries, and first-missing stability. JWT validation also has a
  bounded generated-claim fuzz corpus over malformed tokens, missing claims,
  audience and identity mismatches, unsupported algorithms, bad signatures, and
  accepted-claim replay determinism.
- The root gauntlet also includes a short fixed-seed concurrent read/reducer
  soak with protocol query probes and compact seed/reader/operation labels.
- Store read-view race coverage now includes a fixed-seed snapshot/commit
  soak that checks concurrent snapshots only observe complete committed
  prefixes with seed/reader/op/runtime-config labels. Store metamorphic
  coverage now also compares different commit orders for independent
  transactions through public committed snapshots and indexes, including
  mixed independent update/delete/insert transactions. Recovery replay
  coverage also compares direct `ApplyChangeset` orderings for independent
  generated changesets.
- A fixed-seed protocol subscription-churn race soak now keeps a stable
  subscriber checking reducer deltas while transient protocol clients
  subscribe and unsubscribe concurrently, validating each observed snapshot
  against committed history with seed/worker/op/runtime-config labels.
- Protocol transport race coverage now also sends concurrently through
  `ClientSender` while `ConnManager.CloseAll` tears down multiple connections,
  accepting only delivered sends or post-teardown `ErrConnNotFound` results
  under fixed seed/worker/op labels. `ConnManager` add/get/remove map
  lifecycle now also has a fixed-seed concurrent short soak, and concurrent
  `CloseAll` plus direct `Conn.Disconnect` callers are checked for idempotent
  teardown and stable disconnect callback ordering.
- A fixed-seed protocol metamorphic trace now compares one long-lived
  subscription with per-operation subscribe/unsubscribe cycles, requiring
  matching deltas, final unsubscribe rows, and one-off query probes.
- A compact fixed-seed protocol restart-loop soak now drives reducer traces
  across repeated clean restarts, probing one-off reads and subscription
  initial snapshots after each restart.
- Protocol RowList plus client/server message decoding now have bounded fuzz
  seed corpora that check malformed-input categorization and accepted-input
  canonical round trips. Protocol compression envelopes now also have a
  bounded generated-body fuzz corpus that checks none/gzip round trips and
  brotli/unknown-mode error categorization.
- BSATN standalone value and product-value decoding now have bounded
  public-surface fuzz corpora across scalar and variable-length payloads,
  checking malformed-input categorization plus accepted-value/row canonical
  re-encoding.
- Runtime type primitives now have fixed-seed metamorphic coverage for
  identity and connection ID hex parsing, including case variants and
  invalid-input categorization. `ProductValue` batch copy coverage now checks
  source/copy detachment across row and nested bytes mutations.
- Subscription query hashing now has a bounded fuzz corpus for same-table
  canonicalization laws, self-join filter alias identity, and client
  parameterization. A short fixed-seed concurrent hash determinism soak also
  exercises pooled canonical encoders with compact worker/iteration/seed
  failure labels. Subscription fanout worker race coverage now also churns
  confirmed-read policy and client removal concurrently with an in-flight
  delivery using fixed seed/worker/op labels.
- Schema registry and export surfaces now have a fixed-seed concurrent
  read/export soak over the documented immutable registry, checking stable
  lookup results, detached snapshots, reducers, lifecycle hooks, and exported
  schema equivalence under worker/op labels. The schema builder public surface
  also has a bounded fuzz corpus comparing `BuildPreview` and `Build`
  acceptance/export equivalence while checking registry lookup and export
  detachment invariants.
- Module contract JSON validation now has a bounded public-surface fuzz corpus
  that accepts canonical exported contracts, rejects malformed inputs, and
  checks deterministic canonical re-marshalling after JSON round trips.
- TypeScript client code generation now has bounded public-surface fuzz
  coverage over contract JSON, including invalid-input categorization,
  deterministic accepted output, canonical JSON input equivalence, and
  identifier collision/reserved-word corpus seeds.
- Contract diff and migration-plan tooling now has fixed-seed metamorphic
  coverage requiring declaration-order changes to preserve diff text and
  canonical migration-plan JSON under seed/iteration labels.
- Contract diff and migration-plan JSON entry points now have a bounded fuzz
  corpus that checks invalid-contract error categorization plus deterministic
  and canonical-input-equivalent diff/plan output for accepted contracts.
- SQL parser and literal coercion fuzzing now drive arbitrary bounded query
  text through the public `Parse` surface and generated literal/kind pairs
  through `CoerceWithCaller`, checking unsupported-error categorization and
  deterministic accepted results. Parser metamorphic coverage now also checks
  that harmless predicate parenthesization and whitespace layout changes
  produce equivalent parse trees.
- The scheduler restart campaign has pinned replay overflow, duplicate replay,
  retry ordering, fixed-rate repeating catch-up, recovered future wakeups,
  cancellation/rearm behavior, startup idempotence, and external admission
  ordering. Scheduler worker coverage also guards stale scheduled-attempt
  completions so mismatched intended fire times cannot clear active in-flight
  attempts, and now churns concurrent wakeup notifications plus completion
  callbacks to reject duplicate in-flight due rows under a fixed seed.
- Commitlog recovery/metamorphic coverage now includes rapid damaged-tail
  resume equivalence, snapshot replay with and without offset indexes, and
  boundary-segment compaction equivalence. Full-log recovery is also checked
  across generated segment split choices. Fault coverage now also checks safe
  zero-filled tails on full logs and selected-snapshot tails, unsafe
  zero-header/nonzero tails on selected-snapshot replay tails, corrupt sealed
  predecessor segments, and corrupt newest snapshot fallback to an older valid
  snapshot plus log; torn rollover segments are replaced through the recovery
  resume plan, and corrupt offset indexes, including indexes pointing at safe
  zero-tail sentinels, fall
  back to linear replay. The fuzz corpus now includes combined snapshot plus
  segment recovery-boundary artifacts, selected-snapshot safe and unsafe tail
  padding seeds, valid, partial, safe-tail-pointing, and sentinel-corrupt
  indexed replay boundary sidecars, standalone schema-snapshot decode
  boundaries, categorized record-decoder rejection, and accepted recovery report
  invariants.
  Snapshot-only recovery now
  also checks that the returned fresh
  resume plan can append a tail that is replayed on the next restart, and
  snapshot recovery handles a header-only rollover segment immediately after
  the snapshot horizon. Header-only active-segment resume plans are also
  exercised through durability append and a second recovery. Segment creation,
  scanning, and recovery now reject
  bootstrap tx 0 segment starts as unsafe history, and terminal max-tx
  snapshot horizons are rejected by the writer or fail before recovery can
  return an overflowing zero resume plan.
  Bootstrap tx 0 snapshots are also checked to reject impossible row or counter
  state through writer, reader, and recovery-report paths.
  Snapshot reader validation now rejects zero next-id and autoincrement sequence
  counters before recovery can restore regressed allocators.
  Snapshot header faults are categorized as snapshot read failures while
  preserving their underlying bad-magic, bad-version, or bad-flags leaf errors.
  Truncated snapshot payloads now receive the same snapshot category while
  retaining their underlying EOF cause. Temp snapshot short writes and
  short write-at failures now also leave no selectable snapshot while complete
  log replay recovers the committed state.
  Snapshot next-id values below the restored row allocator are rejected at read
  time and remain fail-loud during selection instead of falling back.
  Autoincrement sequence values below restored row values now receive the same
  read-time and selection fail-loud treatment.
  Fresh recovery resume plans now reject mismatched segment-start and next-tx
  values before creating a segment or publishing a false durable horizon.
  Append-in-place resume plans now also validate next-tx against missing or
  existing segment state before opening durability admission.
  Unsafe history checks now also cover generated snapshot/log boundary gaps and
  missing-base log suffixes.
  Compaction retry
  coverage now checks covered orphan sidecar cleanup without changing recovery,
  snapshot-only retry sync failures after orphan sidecar cleanup,
  full-horizon compaction without changing active-segment resume semantics, and
  future-snapshot rejection without mutating replayable log or sidecars. Pure
  compaction planner fuzzing now checks arbitrary bounded active/sealed ranges
  against deterministic delete/retain partition invariants. Offset-index
  append/truncate/reopen behavior now has a fixed-seed model soak with compact
  operation traces, writer cadence/truncate/sync behavior has a fixed-seed
  model soak, durability recovery now compares single-segment and forced-rollover
  traces for equivalent recovered rows and horizons, mutable reopen fuzzing now
  checks that ignored sidecar tails cannot be resurrected by later appends,
  advisory offset-index create/open failures
  during initial durability startup and rollover are checked to leave the log
  recoverable, and segment-reader indexed seeks are fuzzed against linear seek
  results over generated TxID ranges.
- Release-candidate validation now includes a compact public hosted-runtime
  task-board workload under strict auth, with app reducers, private-table
  declared query/view reads, clean restart recovery, rejected duplicate and
  permission-denied operations before and after restart, and fixed seed/op
  labels comparing observed rows against an independent model. A companion
  strict-auth protocol path drives the same task-board app through WebSocket
  reducer calls, declared query/view reads, live declared-view deltas, rejected
  duplicate mutations before and after restart, and restart recovery.
- Contract artifact CLI release-candidate coverage now checks a rejected
  codegen target fails before mutating an existing output file, with a stable
  trace label for reproduction.
- Contract artifact generation now writes through a synced temporary file,
  rename, and parent-directory sync; workflow fault coverage pins injected
  parent-sync failures as fail-loud without leaking temporary artifacts. CLI
  release-candidate coverage pins unsupported read-output formats plus malformed
  or semantically invalid codegen inputs as non-mutating failures.
- Remaining non-runtime campaign work should move to broader crash/recovery,
  fault injection, fuzzing/corpus, metamorphic, race/soak, and
  release-candidate coverage unless a new invariant or failing seed appears.

## Goals

- Prove that Shunter preserves committed state correctly across reducers,
  reads, subscriptions, snapshots, logs, and restarts.
- Prove that client-visible behavior is stable: accepted operations produce
  correct rows and updates; rejected operations fail before mutating state or
  registering subscriptions.
- Find concurrency, recovery, protocol, query, and lifecycle bugs before
  application authors find them.
- Leave behind reusable harnesses and corpora, not a one-time manual test pass.

## Non-Goals

- Do not re-open SpacetimeDB wire/client compatibility as a success criterion.
- Do not widen SQL or runtime features as part of this campaign.
- Do not rely on package internals as the main proof. Internal unit tests remain
  useful, but the gauntlet should primarily drive Shunter like an application or
  client would.

## Core Invariants

Every randomized or scenario test should check one or more of these invariants:

- Reducer success mutates committed state exactly once.
- Reducer failure does not mutate committed state.
- One-off reads return the same rows as the model for the supported query
  surface.
- Subscription initial snapshots match equivalent one-off reads where the
  syntax and row shape overlap.
- Subscription deltas equal `after - before` for the subscribed predicate.
- Unsubscribe stops future updates without corrupting other subscriptions.
- Rejected queries do not execute and do not register subscriptions.
- Disconnect, reconnect, and backpressure do not corrupt committed state or
  unrelated client fanout.
- Snapshot plus replay reaches the same state as uninterrupted execution.
- Full-log replay reaches the same state as the live runtime reached before
  shutdown.
- Corrupt or unsafe recovery input fails loudly instead of silently accepting
  damaged history.
- Scheduler/timer effects are replayed or resumed according to the documented
  Shunter contract.

## Harness Shape

Build a black-box harness around the hosted runtime and protocol layer. The
harness should be able to run deterministic, seed-driven workloads against a
real Shunter runtime while maintaining a simpler model of expected behavior.

The model should be intentionally boring:

- in-memory tables keyed by declared primary keys
- direct row mutation for reducer effects
- simple query evaluation for Shunter's supported SQL subset
- subscription registrations with model-computed initial rows and deltas
- a model clock or deterministic scheduler driver for timer scenarios

Avoid sharing Shunter internals with the model. The point is to compare two
independent implementations of the same public contract.

The root runtime test package uses `go.uber.org/goleak` through package-level
`TestMain`; gauntlet tests run under that check. Future gauntlet slices should
prefer explicit runtime/client cleanup and avoid per-test `goleak.VerifyNone`
or broad ignore rules unless a dependency has a documented benign goroutine.

## Workload Operations

The harness should generate and replay mixes of:

- runtime start, close, restart, and recovery
- reducer calls that insert, update, delete, return user errors, or panic
- local `Runtime.Read` calls
- protocol one-off queries
- subscribe single, subscribe multi, unsubscribe single, and unsubscribe multi
- client disconnects and reconnects
- slow clients and backpressure
- scheduler/timer registration and firing
- snapshot and commitlog rotation boundaries, once those controls exist

Every generated workload must print or persist enough information to reproduce
the failure: seed, operation index, runtime config, schema, workload operation,
and observed vs expected result.

## Test Families

### 1. Public-Surface Model Tests

Drive Shunter through hosted-runtime APIs and protocol clients. Compare every
observable result with the model:

- reducer outcomes
- final table contents
- one-off query rows
- subscription initial rows
- subscription deltas
- protocol errors
- lifecycle errors

Start with small schemas and short traces, then increase table counts, row
counts, predicate complexity, client count, and trace length.

### 2. Recovery And Crash Matrix

Run the same model workload with forced interruption points:

- after log append, before sync
- after sync, before state publication
- during snapshot write
- after snapshot write, before manifest update
- during segment rollover
- during compaction
- during scheduler/timer activity

After restart, compare recovered state, replay horizon, scheduler state, and
client-visible readiness against the model or documented failure mode.

### 3. Fault Injection

Inject storage and IO failures where Shunter crosses durability boundaries:

- short writes
- fsync failure
- rename failure
- open/read failure
- truncated record
- damaged segment tail
- damaged snapshot
- missing snapshot/log file
- zero-filled preallocated tail

Expected outcomes must be explicit: recover, ignore safe tail, or fail loudly.

### 4. Fuzzing

Add Go fuzzers for trust-boundary parsers and decoders:

- SQL parse and literal coercion
- BSATN encode/decode
- protocol client/server message decode
- RowList decode
- commitlog record decode
- snapshot decode
- subscription hash/canonicalization

Fuzz targets should assert no panic, bounded resource use, deterministic
round-trips where applicable, and clear rejection for malformed input.

### 5. Metamorphic Tests

Use equivalent executions to expose hidden state bugs:

- uninterrupted workload vs restart halfway
- full-log replay vs snapshot plus replay
- subscribe initial snapshot vs equivalent one-off query
- indexed path vs equivalent allowed scan path
- same independent reducer calls in different orders
- same logical query with harmless parenthesization or predicate reordering
- repeated subscribe/unsubscribe cycles vs a single long-lived subscription

### 6. Concurrency And Soak

Run long workloads under the race detector and stress loops:

- many clients subscribing and unsubscribing while reducers commit
- slow outbound clients under fanout pressure
- disconnect during send
- close during reducer execution
- reads around commit publication
- scheduler firing while clients connect and disconnect
- restart loops with active workload generation

Soak failures should produce compact artifacts rather than only logs: seed,
trace, runtime config, final model state, final Shunter-observed state, and any
panic/fatal error.

## Release-Candidate Gauntlet

Before calling a major build ready, run:

- `rtk go test ./... -count=1`
- pinned static analysis with `rtk go tool staticcheck ./...`; after OI-008
  cleanup, this is a required green release-candidate gate
- targeted package tests with `-race` for runtime, executor, protocol,
  subscription, store, and commitlog
- fuzz corpus replay
- randomized model workloads across a fixed seed set
- crash/recovery matrix across representative schemas
- fault-injection tests for commitlog and snapshot boundaries
- multi-client protocol soak
- at least one real example app workload using the public hosted-runtime API

The fixed seed set should be checked in once it starts finding meaningful
coverage. New bug seeds should become regression cases or corpus entries.

## Exit Criteria

The campaign is ready to replace ad hoc manual testing when:

- failures are reproducible from a seed or trace
- the model covers reducers, reads, subscriptions, restart, and recovery
- every core invariant above is checked by at least one automated test family
- crash and fault tests distinguish safe recovery from unsafe history
- CI can run a short deterministic version
- longer stress/fuzz/soak jobs can run outside the default local path

The campaign is successful when new bugs are routinely converted into small
regression tests or fuzz corpus entries, and major runtime changes are judged by
whether they survive this gauntlet rather than by whether they pass only the
package unit suite.
