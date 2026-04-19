# Tighten the call-reducer adapter seam so committed caller responses carry the real caller-visible update payload

## Goal

Close the remaining adapter-level honesty gap after the Phase 1.5 landing: when a protocol-originated reducer call commits, the protocol-facing response path must carry the same caller-visible heavy `TransactionUpdate` payload the caller should actually observe, including the real `StatusCommitted.Update` delta, rather than a synthesized shell that only mirrors reducer status.

## Current grounded state

Relevant live code/doc facts:

- `protocol/lifecycle.go` documents `CallReducerRequest.ResponseCh` as carrying the heavy `TransactionUpdate` after fan-out evaluation, specifically so `StatusCommitted.Update` reflects the caller-visible row delta.
- `protocol/async_responses.go` says the response watcher only delivers an already-populated heavy envelope.
- `subscription/fanout_worker.go` + `protocol/fanout_adapter.go` already know how to build the correct heavy caller envelope from:
  - `subscription.CallerOutcome`
  - the caller’s actual `[]subscription.SubscriptionUpdate`
- But `executor/protocol_inbox_adapter.go` still does this today:
  - creates `respCh := make(chan ReducerResponse, 1)`
  - forwards that through `forwardReducerResponse(...)`
  - maps `StatusCommitted` to `protocol.StatusCommitted{}` via `reducerStatusToProtocol`
- That means committed adapter responses currently lose the real caller-visible update payload, and also bypass the existing heavy-envelope construction path.
- `executor/protocol_inbox_adapter_test.go:416+` currently pins the old synthesized behavior instead of the intended contract.

Put plainly: the docs/commentary say “real heavy envelope,” but the adapter still emits “bridged status shell.”

## Scope boundary

This slice should stay narrow:

- Fix the protocol↔executor call-reducer adapter seam.
- Do not redesign the whole reducer execution model.
- Do not broaden into scheduler/lifecycle semantics except where shared types force minimal changes.
- Preserve existing failure-path behavior for pre-commit / pre-admission rejections unless the seam change naturally consolidates them.

## Recommended implementation approach

Use one source of truth for caller-visible heavy-envelope assembly and thread the real caller delta through the adapter path.

Recommended shape:

1. Keep `ReducerResponse` for generic executor callers that only need status/TxID/return bytes.
2. Add an adapter-only committed-caller payload path that carries:
   - the committed `subscription.CallerOutcome`
   - the caller’s real `[]subscription.SubscriptionUpdate`
3. Reuse the same heavy-envelope assembly logic already used by `protocol/fanout_adapter.go` instead of re-synthesizing a parallel status mapping in `executor/protocol_inbox_adapter.go`.

This keeps the slice honest and narrow:
- executor-internal generic response semantics stay intact
- protocol adapter gets the richer payload it actually promises
- heavy-envelope assembly logic stays centralized

## Design options considered

### Option A — Widen `ReducerResponse`
Add caller-update payload directly to `ReducerResponse`.

Pros:
- simple conceptual path

Cons:
- pollutes executor-wide response type used by scheduler/lifecycle/tests
- makes every reducer caller pay for protocol-specific delivery concerns
- broadens the slice more than necessary

Recommendation: avoid unless the codebase already strongly couples all reducer responses to protocol delivery.

### Option B — Add an adapter-only side channel/hook on external reducer calls
Add an optional hook or side-channel on `ReducerRequest` / post-commit metadata used only when the protocol adapter submits external calls.

Pros:
- narrowest behavior change
- preserves generic `ReducerResponse`
- matches the user’s framing: this is an adapter seam fix

Cons:
- requires a small executor↔subscription seam addition so the actual caller updates can be surfaced back to the adapter path

Recommendation: best fit.

### Option C — Make the adapter consume a richer adapter-specific response object instead of `ReducerResponse`
Introduce a dedicated executor package type for protocol-facing call results, populated only for `CallReducer` via the protocol adapter.

Pros:
- explicit and honest contract
- avoids overloading generic reducer response semantics

Cons:
- still needs a way to extract caller updates from the evaluation path
- slightly more invasive than a hook if it changes command signatures

Recommendation: acceptable if the implementation reads more cleanly than a hook, but keep it adapter-specific.

## Recommended concrete design

### 1. Introduce an adapter-specific committed payload carrier
Add a small executor-owned type for the protocol adapter path only, something like:

- `CommittedCallerPayload`
  - `Outcome subscription.CallerOutcome`
  - `Updates []subscription.SubscriptionUpdate`

or a slightly more protocol-ready variant if needed.

This type should not replace `ReducerResponse`; it should complement it for the external protocol call path.

### 2. Thread a callback or writable slot through post-commit metadata
The key missing data is the caller’s actual `[]subscription.SubscriptionUpdate`, which only becomes available inside subscription evaluation.

Recommended narrow mechanism:

- extend `subscription.PostCommitMeta` with an optional callback or writable destination for caller updates, for example:
  - `CaptureCallerUpdates func([]SubscriptionUpdate)`
  - or `CallerUpdatesSink *[]SubscriptionUpdate`
- have `subscription.Manager.EvalAndBroadcast(...)` populate it from the computed `fanout[*CallerConnID]` before enqueueing the fan-out message

Why here:
- `EvalAndBroadcast` already computes the exact per-connection fanout map
- extracting the caller’s slice there is cheap and authoritative
- avoids recomputing deltas elsewhere

### 3. Capture the committed caller payload inside executor post-commit
In `executor.postCommit(...)`:

- when `opts.source == CallSourceExternal`, install the callback/sink in `subscription.PostCommitMeta`
- after `EvalAndBroadcast(...)` returns, the executor has:
  - `meta.CallerOutcome` (already assembled)
  - the actual caller updates captured from evaluation
- use that to satisfy the adapter-specific response path for committed calls

Important sequencing invariant:
- keep the capture on the synchronous executor goroutine during `EvalAndBroadcast`
- do not race the fan-out worker or require async coordination to learn the caller updates

### 4. Refactor heavy-envelope assembly into a reusable helper
Right now `protocol/fanout_adapter.go` has the correct assembly path:
- `buildUpdateStatus(...)`
- `reducerCallInfoFrom(...)`

Refactor this into a shared helper usable by both:
- `FanOutSenderAdapter.SendTransactionUpdateHeavy(...)`
- the protocol inbox adapter’s committed response path

Target outcome:
- one codepath defines how `CallerOutcome + callerUpdates` becomes heavy `TransactionUpdate`
- no duplicated status mapping logic
- remove or deprecate `reducerStatusToProtocol(...)` for committed responses

### 5. Rewrite `ProtocolInboxAdapter.CallReducer` response forwarding
Replace the current “listen for `ReducerResponse`, synthesize shell” flow with:

- failure/non-commit paths:
  - still map to heavy `StatusFailed` using existing reducer status/error info
  - keep current synthetic behavior for pre-commit failure cases unless the richer seam naturally carries them too
- committed path:
  - build the heavy `TransactionUpdate` from the real committed payload (`CallerOutcome + Updates`)
  - send that exact message on `req.ResponseCh`

Desired end state:
- `req.ResponseCh` gets the same logically correct heavy envelope the docs promise
- `StatusCommitted.Update` contains the caller-visible delta, not an empty shell

### 6. Remove stale misleading adapter comments / helpers
Once the seam is fixed:

- update comments in `executor/protocol_inbox_adapter.go` to match reality
- delete or narrow `reducerStatusToProtocol(...)` if it is no longer needed for committed paths
- ensure `protocol/async_responses.go` comments remain true

## Step-by-step execution plan

### Step 1 — Lock the intended behavior in tests first
Add/adjust tests before code changes:

1. `executor/protocol_inbox_adapter_test.go`
   - replace the current “translated reducer response” commit-success pin with a test that expects:
     - `protocol.StatusCommitted`
     - non-shell `StatusCommitted.Update`
     - preserved caller metadata (`CallerIdentity`, `CallerConnectionID`, `ReducerCall`)
   - keep a failure-path test proving `StatusFailed` still flows correctly

2. Add a committed-path adapter test that proves the real update payload survives, e.g. one inserted row produces one `SubscriptionUpdate` inside `StatusCommitted.Update`.

3. If needed, add a unit test at the subscription seam proving the caller-update capture hook/sink is populated from the same fanout map entry that would be delivered to the caller.

This makes the seam gap explicit before implementation.

### Step 2 — Add the minimal capture hook at the subscription seam
Likely files:
- `subscription/fanout.go`
- `subscription/eval.go`
- maybe tests under `subscription/`

Tasks:
- extend `PostCommitMeta` with optional caller-update capture support
- in `EvalAndBroadcast`, when `CallerConnID` is present, extract `fanout[*CallerConnID]` and write it into the hook/sink before enqueuing `FanOutMessage`
- preserve current fanout behavior for non-callers and existing worker delivery

### Step 3 — Thread that capture support through executor post-commit
Likely file:
- `executor/executor.go`

Tasks:
- install the hook/sink in `meta` for `CallSourceExternal`
- after `EvalAndBroadcast`, use captured updates together with `meta.CallerOutcome`
- make the committed external response path able to hand both pieces to the protocol adapter response channel

### Step 4 — Refactor heavy-envelope assembly to shared helper(s)
Likely files:
- `protocol/fanout_adapter.go`
- `executor/protocol_inbox_adapter.go`
- possibly a new helper file under `protocol/`

Tasks:
- extract reusable helper for “caller outcome + caller updates -> heavy `TransactionUpdate`”
- update `FanOutSenderAdapter` to use it
- update the protocol inbox adapter to use it for committed responses

### Step 5 — Remove shell-only committed translation
Likely file:
- `executor/protocol_inbox_adapter.go`

Tasks:
- stop treating committed `ReducerResponse` as sufficient for protocol response construction
- replace `StatusCommitted{}` shell synthesis with real committed payload construction
- keep failure mapping intact for non-commit outcomes

### Step 6 — Run focused verification
Run focused tests covering executor, subscription, and protocol seam behavior.

## Files likely to change

Most likely:

- `executor/protocol_inbox_adapter.go`
- `executor/protocol_inbox_adapter_test.go`
- `executor/executor.go`
- `subscription/fanout.go`
- `subscription/eval.go`
- `protocol/fanout_adapter.go`

Possibly:

- a new helper file under `protocol/` for shared heavy-envelope construction
- `subscription/*_test.go` if a new capture hook needs dedicated tests
- `protocol/async_responses.go` comments only, if wording must be tightened

## Validation plan

### Focused unit tests

Run at minimum:

- `rtk go test ./executor -run ProtocolInboxAdapter`
- `rtk go test ./subscription -run Fanout`
- `rtk go test ./protocol -run 'CallReducer|FanOut|TransactionUpdate'`

### Broader regression pass

Then run:

- `rtk go test ./executor ./subscription ./protocol`

### Behavioral checks to confirm

1. Committed external reducer call returns heavy `TransactionUpdate` with:
   - `StatusCommitted`
   - real `Update` payload
   - correct reducer metadata

2. Failed reducer call still returns heavy `StatusFailed`.

3. Empty changeset / no-active-subscription committed call still returns a committed heavy envelope, but with the correct empty update payload shape rather than a fabricated shell.

4. NoSuccessNotify behavior remains correct:
   - if the protocol response watcher is still the committed delivery path, ensure success suppression is not accidentally broken
   - if caller heavy delivery is owned elsewhere, ensure this seam does not reintroduce duplicate success delivery

5. Non-caller light delivery behavior is unchanged.

## Risks and tradeoffs

### 1. Duplicate caller delivery risk
The repo now has both:
- call-reducer response watching in `protocol/async_responses.go`
- caller-heavy delivery support in the fan-out layer

Before implementation, verify which path is authoritative in production wiring. The fix must not cause two committed heavy messages to be delivered to the caller.

Mitigation:
- add/adjust a regression test around single committed caller delivery if the current suite does not already pin it.

### 2. Overcoupling executor internals to protocol types
If the fix directly threads `protocol.TransactionUpdate` deep into executor core, the slice becomes broader and less clean.

Mitigation:
- prefer adapter-owned or executor-owned intermediate payloads and only build protocol wire structs at the protocol edge.

### 3. Hooking the wrong delta source
If the committed payload is reconstructed from the changeset instead of the evaluated per-connection fanout, it may differ from the caller-visible delta.

Mitigation:
- capture from `EvalAndBroadcast`’s computed `fanout` map, not from raw changeset data.

### 4. Test pin drift
Current adapter tests pin the wrong shell behavior.

Mitigation:
- rewrite those tests first so the intended contract is executable.

## Open questions to resolve during implementation

1. Is `watchReducerResponse` supposed to be the only caller-heavy delivery path, or is it now just a transport helper for an envelope produced elsewhere?
2. Should the committed adapter response be suppressed under `CallReducerFlagsNoSuccessNotify`, or is that suppression solely a fan-out-worker concern in the current architecture?
3. Is it cleaner here to use:
   - a callback/sink on `PostCommitMeta`, or
   - a dedicated adapter response object returned from the executor path?

My recommendation is to decide in favor of the smallest change that:
- captures caller updates from `EvalAndBroadcast`
- reuses shared heavy-envelope assembly
- does not broaden generic `ReducerResponse`

## Definition of done

This slice is done when all of the following are true:

- `protocol.CallReducerRequest.ResponseCh` receives a fully honest committed heavy `TransactionUpdate`
- committed responses include the real caller-visible `StatusCommitted.Update` payload
- adapter code no longer synthesizes a committed shell from `ReducerResponse` alone
- fan-out adapter and call-reducer adapter share one heavy-envelope assembly path
- focused executor/subscription/protocol tests pass and the old shell-pinning test is replaced
