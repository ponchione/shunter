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

- OI-002 is closed for current query/subscription evidence.
- OI-003 is closed for current recovery/store evidence.
- `runtime_gauntlet_test.go` now carries short deterministic public-surface
  checks for reducer/read modeling, clean restart equivalence, protocol
  `CallReducer` restart equivalence, one-off reads and isolated one-off errors,
  same-connection one-off/subscription interleaving, subscription initial rows,
  subscribe-initial/one-off equivalence, live subscription deltas,
  multi-subscriber fanout parity, same-connection subscription multiplexing,
  same-connection `SubscribeMulti`/`SubscribeSingle` coexistence, predicate
  subscription deltas, rejected subscribe cleanup and same-connection recovery,
  rejected subscribe-multi cleanup and same-connection recovery,
  unsubscribe and unknown unsubscribe isolation including same-connection
  subscription preservation, mixed-surface protocol/runtime traces,
  multi-client mixed workloads with dual subscribers and resubscribe, protocol
  `CallReducer` read-your-writes one-offs, protocol `CallReducer` subscribed
  caller heavy deltas, heavy multiplexed caller deltas, and `NoSuccessNotify`
  subscribed-caller suppression, disconnect/reconnect fanout, live-client
  close/restart, subscribe/unsubscribe multi, repeated subscribe/unsubscribe
  cycles, rejected subscribe multi atomicity, panic rollback, unknown reducer
  admission failures, reserved lifecycle reducer rejection, one-shot scheduled
  reducer firing through the hosted runtime, cancel-before-fire, and clean
  restart firing for pre-close scheduled reducers, plus fixed-seed
  scheduled fire/cancel workloads and scheduled reducer failure rollback with
  no fanout, scheduled reducer panic rollback with no fanout, repeating
  scheduled reducer fire/cancel behavior, repeating schedule resume after clean
  restart, cancelled schedule persistence across clean restart, and
  transactional rollback of schedule creation, immediate and past-due scheduled
  one-shot firing, scheduled due-time ordering, scheduled predicate
  subscription deltas, scheduled multi-subscriber fanout parity, cancel
  idempotence and unknown-cancel no-effect controls, protocol `CallReducer`
  schedule/cancel coverage including clean restart, protocol rollback of
  schedule creation, scheduled fire isolation for unsubscribed clients, and
  scheduled `SubscribeMulti` deltas with unsubscribe controls,
  `NoSuccessNotify` protocol scheduling that still fires, protocol-created
  repeating schedule firing, and fixed-seed runtime/scheduler interleaving
  workloads. The short deterministic root runtime gauntlet is saturated for the
  current public hosted-runtime, protocol, and scheduler surfaces; remaining
  campaign work moves to crash/recovery, fault injection, fuzzing/corpus,
  metamorphic expansion, race/soak, and release-candidate runs. The
  crash/recovery campaign has started with a deterministic restart pin proving a
  damaged tail segment remains recoverable after recovery resumes into a fresh
  successor segment and the process restarts again, plus a snapshot/log boundary
  pin proving recovery fails loudly instead of skipping the first post-snapshot
  transaction when the log begins too late. Scheduler restart work has also
  started with a missed-timer replay overflow pin proving startup cannot block
  the dangling-client sweep when recovered due timers exceed executor inbox
  capacity, and a duplicate-replay pin proving the first scheduler scan after
  startup does not enqueue a recovered missed timer a second time, plus a
  failed-replay retry pin proving stale startup duplicate suppression cannot
  hide a failed recovered firing from the scheduler restart loop, and an
  overflowed-replay retry pin proving a recovered due schedule that did not
  fit during startup replay is still picked up and retried after a failed
  scheduler-run attempt, plus a scheduler-run pickup pin proving overflowed
  due rows from saturated startup replay are recovered by the first run-loop
  scan without duplicating the replay-queued row, plus a repeating-replay
  catch-up pin proving overdue recovered interval schedules advance one
  intended fixed-rate interval per fire across restart, an overflowed
  repeating-replay catch-up pin proving the same fixed-rate advancement after
  startup replay saturation, a panicking
  recovered one-shot retry pin, and a replay/external-admission ordering pin
  proving post-startup external reducers do not overtake recovered scheduled
  firings queued before the gate opens. Additional scheduler restart pins prove
  in-flight attempt tracking is established before an executor can complete a
  due command, and recovered repeating schedules that fail or panic retry the
  same intended fire time before advancing by one fixed-rate interval on the
  successful retry. Scheduler clock injection is now pinned across future
  wakeups, notify-driven rescans, and replayed future wakeups so recovered
  schedules cannot fire early just because their recovered due time predates
  the host wall clock. Additional replay-saturation pins prove skipped due
  rows remain retryable, the earliest future wakeup survives a saturated replay
  pass, and recovered repeating schedules that advance into the future after
  either replay or overflow wait for the scheduler clock before firing again.
  Follow-on restart/notify pins prove such advanced repeating rows can be
  cancelled before the next due time without a stale firing, and that a
  post-startup schedule earlier than a recovered future wakeup re-arms the
  scheduler without firing the later recovered row early. Additional recovered
  future cancellation pins prove one-shot rows can be cancelled while the
  scheduler is armed, and cancelling the earliest recovered future wakeup
  re-arms the scheduler to a later recovered row instead of retaining a stale
  early wakeup. Further notify/rearm pins prove a post-startup schedule later
  than a recovered due time cannot postpone the recovered row, and a rolled-back
  earlier schedule creation cannot replace the recovered future wakeup. Success
  path rearm pins prove a completed early recovered one-shot re-arms a later
  recovered row, and a recovered repeating row advanced into the future does
  not overtake an earlier recovered one-shot. Future-armed failure pins prove
  recovered one-shots that fail or panic at their due time retry before later
  recovered wakeups are allowed to fire, and the same ordering holds for
  recovered repeating rows while preserving fixed-rate advancement after the
  successful retry. Equal-time rearm pins prove multiple recovered future
  wakeups at the same timestamp all fire once, and a post-startup schedule at
  the same timestamp as a recovered row does not hide either firing. Equal-time
  repeating pins prove a recovered repeating row sharing a timestamp with a
  one-shot fires once, advances fixed-rate, and remains visible when the
  one-shot is created after startup at that same timestamp. Equal-time
  retry pins prove a failing or panicking recovered row retries without
  duplicating its same-timestamp recovered sibling, including repeating rows
  that must advance fixed-rate after the successful retry.

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
- pinned static analysis with `rtk go tool staticcheck ./...`; until OI-008
  cleanup clears known findings, record failures instead of treating it as a
  required green release-candidate gate
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
