# Next session handoff

Use this file to start the next build agent on the current Shunter TECH-DEBT / parity task with no prior chat context.

This is not the hosted-runtime planning handoff. Hosted-runtime planning lives in `HOSTED_RUNTIME_PLANNING_HANDOFF.md`.

## Current objective

Implement the next OI-002 / Tier A2 subscription-runtime parity slice:

`OI-008` / same-connection reused subscription-hash initial-snapshot elision: when a client subscribes to the same predicate hash twice on the same connection under different client `QueryID`s, the second `SubscribeMultiApplied` should carry an empty update rather than re-sending the full initial snapshot. This is already scoped out in `TECH-DEBT.md::OI-008` with reference anchors and a test-first plan; do not reopen closed P0-SUBSCRIPTION-001 through P0-SUBSCRIPTION-033 rows.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Required startup reading

Read in this order before editing:

1. `RTK.md`
2. `README.md`
3. `TECH-DEBT.md` — read OI-002 (campaign) and OI-008 (concrete slice + implementation notes). Do not switch to another tech-debt item from this handoff.
4. `docs/spacetimedb-parity-roadmap.md` — Tier A2.
5. `docs/parity-phase0-ledger.md` — `P0-SUBSCRIPTION-001` through `P0-SUBSCRIPTION-033` are closed baselines.
6. `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md`
7. `docs/decomposition/005-protocol/SPEC-005-protocol.md`

Then inspect the live code with Go tools before editing:

- `rtk go doc ./subscription.Manager`
- `rtk go doc ./subscription.RegisterSet`
- `rtk go list -json ./subscription ./protocol ./executor`

## Why this is the next path

`P0-SUBSCRIPTION-033` (one-off `COUNT(*) [AS] alias` + `LIMIT` aggregate composition) closed the last queued bounded SQL widening for OI-002. `OI-008` is the already-scoped-out named OI-002 residual with concrete reference anchors, a bounded implementation surface (the set-registration hot path), and a test-first plan already written in `TECH-DEBT.md::OI-008`. It does not widen the SQL surface; it fixes a standing-query-reuse bandwidth/latency bug on the subscribe path.

Reference anchors (from `TECH-DEBT.md::OI-008`):

- `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs::add_subscription_multi` lines 1083-1094: already-attached `(ConnID, query-hash)` paths skip adding the predicate to `new_queries`.
- `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_actor.rs` lines 1357-1369: empty `new_queries` becomes empty applied update data.
- reference test `test_subscribe_and_unsubscribe_with_duplicate_queries_multi` lines 2498-2535: the second same-connection reused-plan add has `second_one.is_empty()`.

Current Shunter behavior (to verify with a scout before editing):

- `subscription/register_set.go::RegisterSet` calls `initialQuery` for every deduped per-call predicate before checking whether the same connection already has that query hash live, so the second same-connection/different-QueryID subscribe re-emits the full initial rows.
- `subscription/query_state.go::addSubscriber` already tracks multiple internal `SubscriptionID`s per `(ConnID, hash)`.
- post-commit fanout already stamps each update with the stored client `QueryID` (closed under `P0-SUBSCRIPTION-030`).

## TDD-first implementation plan

Add or flip tests first, and confirm the important ones fail before production edits.

### 1. Manager-level regression in `subscription/register_set_test.go`

- First register for `(connA, queryID=1, predicate P)` returns initial inserts.
- Second register for `(connA, queryID=2, same predicate/hash P)` succeeds, records queryID=2, and returns an empty `Update`.
- A later register for `(connB, queryID=1, same predicate/hash P)` still returns initial inserts. This pins that the elision is same-connection only.

### 2. Preserve existing hard-error pins

- Duplicate `(connA, queryID=1)` subscribe still returns `ErrQueryIDAlreadyLive`.
- Non-existent unsubscribe still errors.

### 3. Protocol-level pin in `protocol/handle_subscribe_test.go` (or nearby multi-subscribe coverage)

- Two `SubscribeMulti` requests from the same connection using different `QueryID`s and equivalent SQL should produce a second `SubscribeMultiApplied` with empty `Update`.

### 4. Post-commit sanity pin (only if cheap)

- Show both `QueryID`s still receive future deltas after the second attachment. Do not redesign fanout if the existing `QueryID` metadata already satisfies this.

## Production edit shape

Primary files (per `TECH-DEBT.md::OI-008`):

- `subscription/register_set.go`
- `subscription/query_state.go`
- `subscription/manager.go`
- `protocol/handle_subscribe_multi.go`

Likely production changes:

1. Detect same-connection `(ConnID, hash)` reuse in `RegisterSet` BEFORE adding the newly allocated internal `SubscriptionID` via `addSubscriber` (after addSubscriber every new subscription would look like a reuse).
2. On reuse: still allocate and attach the new internal `SubscriptionID`, still populate `querySets[ConnID][QueryID]`, but skip `initialQuery` and leave `SubscriptionSetRegisterResult.Update` empty for that predicate.
3. Keep same-call hash deduplication unchanged (it already avoids duplicate predicates inside one register request).
4. Keep `ErrQueryIDAlreadyLive` for duplicate `(ConnID, QueryID)`.
5. Keep `UnregisterSet` semantics unchanged unless a test proves final-delta behavior needs a matching reference split.
6. If a later predicate in a multi-register request fails `initialQuery`, unwind all allocated subIDs, including same-connection reused-hash subIDs that skipped the snapshot.

## Out of scope

Do not include these in this slice:

- Any non-OI-002 tech-debt item.
- Reopening `P0-SUBSCRIPTION-001` through `P0-SUBSCRIPTION-033` without a new failing regression.
- Further SQL surface widening.
- Fanout/QueryID correlation changes (closed under `P0-SUBSCRIPTION-030`).
- Changing `UnregisterSet` / final-delta semantics unless a test forces it.
- Cross-connection elision (reference does not elide cross-connection, only same-connection).

## Validation commands

Run in this order:

```bash
rtk go test ./subscription -run 'TestRegisterSet.*Reused|TestRegisterSet.*Duplicate' -count=1 -v
rtk go test ./protocol -run 'TestHandleSubscribeMulti.*Reused|TestHandleSubscribeMulti.*Duplicate' -count=1 -v
rtk go fmt ./subscription ./protocol
rtk go test ./subscription ./protocol ./executor -count=1
rtk go vet ./subscription ./protocol ./executor
rtk go test ./... -count=1
```

If a regex misses renamed tests, run the specific new test names explicitly.

## Documentation follow-through after implementation

After the code/tests are green, update only the docs needed to record this slice:

- Add a new `P0-SUBSCRIPTION-034` row (or the next free number) to `docs/parity-phase0-ledger.md` covering the same-connection reused-hash initial-snapshot elision.
- Update `TECH-DEBT.md` OI-002 summary / execution note and mark `OI-008` as closed (or delete the OI-008 section if no residual remains).
- Update `docs/spacetimedb-parity-roadmap.md` A2 current state.
- Rewrite this `NEXT_SESSION_HANDOFF.md` again so the following session starts from the next real open OI-002 target, not from this just-closed slice.

Do not push unless explicitly asked.
