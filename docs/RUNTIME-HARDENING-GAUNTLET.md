# Runtime Hardening Gauntlet

This document defines the runtime hardening test campaign for proving Shunter
is ready for serious application testing. It is not a feature roadmap. It is
the abuse suite Shunter must survive after major implementation work lands.

The campaign should find unnamed bugs, not only prevent known regressions. That
means testing Shunter through public surfaces, comparing behavior against a
simple independent model, injecting faults, and running long randomized
workloads with reproducible seeds.

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

- normal correctness:

```bash
rtk go test ./... -count=1
rtk go vet ./...
rtk go tool staticcheck ./...
```

- focused public-surface hardening:

```bash
rtk go test . -run 'RuntimeGauntlet|ReleaseCandidateExampleApp' -count=1
rtk go test ./... -run 'RuntimeGauntlet|ReleaseCandidateExampleApp|ShortSoak' -count=1
```

- targeted race qualification for the packages with the highest concurrency
  risk:

```bash
rtk go test -race . ./executor ./protocol ./subscription ./store ./commitlog -count=1
```

Run the race set whenever a slice changes runtime concurrency, reducer
execution, protocol connection lifecycle, subscription fanout/pruning, store
index mutation, commitlog recovery, snapshot, or compaction behavior. Add a
package to this command when it starts owning shared goroutines, shared mutable
state, or durability coordination. Keep package-specific race runs separate
from active fuzzing so failures are attributable.

- fuzz corpus replay is part of normal `rtk go test ./...`; active fuzzing
  should run package-at-a-time so failures are easy to attribute. Start with
  the trust-boundary packages:

```bash
rtk go test ./auth -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./bsatn -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./protocol -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./commitlog -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./schema -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./codegen -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./contractdiff -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./subscription -run '^$' -fuzz Fuzz -fuzztime=30s
```

This command set is the current documented release-candidate gate. It still
needs fixed seed/corpus artifacts, broader crash/fault coverage, and a maintained
reference-app workload before it is sufficient for a real `v1.0.0`.

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
