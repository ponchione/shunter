# OI-002 A2 sender-parameter hash identity implementation plan

> For Hermes: use subagent-driven-development if executing later. This is a planning-only artifact; re-check live code/tests before implementation.

Goal: close the next bounded OI-002 A2 normalization/hash seam by preserving `:sender` parameter provenance across SQL compile -> subscribe registration so accepted `:sender` queries hash/dedup as parameterized subscriptions instead of collapsing onto the same runtime identity as an equivalent concrete bytes literal.

Architecture:
- Keep execution and validation semantics unchanged. `:sender` already compiles/executed correctly as caller bytes on subscribe/one-off paths.
- Fix only the hash-identity seam: preserve whether an accepted SQL predicate came from caller-bound parameter syntax, and thread that metadata into subscription registration.
- Do not widen SQL grammar, re-open join ordering/multiplicity, or redesign one-off execution.

Tech stack: Go, existing SQL parser/compile path, protocol -> executor adapter seam, subscription registry/hash path, RTK-based tests.

Stale-plan note:
- This plan supersedes the now-closed OI-002 A2 join-ordering direction from:
  - `.hermes/plans/2026-04-22_174158-oi-002-a2-join-projected-side-ordering.md`
  - `.hermes/plans/2026-04-22_182211-oi-002-a2-projected-join-delta-ordering.md`
- Those plans are historical only; live handoff/docs now point at predicate normalization / validation drift instead.

---

## Confirmed live mismatch

Grounded scout result:
- `query/sql/parser.go` preserves `:sender` as `sql.LitSender`.
- `protocol/handle_subscribe.go::compileSQLQueryString(...)` immediately coerces that marker into concrete `types.NewBytes(caller[:])` values via `coerceLiteral(...)` / `sql.CoerceWithCaller(...)`.
- After that coercion step, the runtime predicate model (`subscription.ColEq` / `ColNe` / `ColRange`) no longer retains whether the value came from a literal bytes token or from `:sender`.
- `subscription.ComputeQueryHash(...)` already has a parameterized-hash seam (`clientID *types.Identity`) and `subscription/manager_test.go::TestRegisterParameterizedHashUsesClientIdentity` pins that manager-level contract.
- But the live protocol subscribe path drops that identity seam entirely:
  - `protocol.RegisterSubscriptionSetRequest` has no client-identity/hash metadata field.
  - `protocol/handle_subscribe_single.go` and `protocol/handle_subscribe_multi.go` do not pass any such metadata.
  - `executor/protocol_inbox_adapter.go` therefore builds `subscription.SubscriptionSetRegisterRequest` with `ClientIdentity == nil` for all protocol subscriptions.
- Result: accepted SQL `WHERE bytes = :sender` normalizes to the same runtime predicate/hash identity as a concrete bytes literal with the same payload, even though the subscription layer already models parameterized hash identity as distinct runtime meaning.

Why this is the right next slice:
- It is an already-accepted SQL shape (`:sender` on bytes/identity columns).
- The drift is squarely in normalization/hash identity, not grammar widening.
- It is protocol-adjacent and bounded to compile/register/hash seams.

Scope boundary:
- In scope: subscribe registration hash identity for accepted `:sender` SQL shapes.
- Out of scope: one-off execution changes, SQL widening, join ordering, multiplicity, fan-out delivery, wire-format changes, hardening OIs.

---

## Files likely to change

Code:
- Modify: `protocol/handle_subscribe.go`
- Modify: `protocol/handle_subscribe_single.go`
- Modify: `protocol/handle_subscribe_multi.go`
- Modify: `protocol/lifecycle.go`
- Modify: `executor/protocol_inbox_adapter.go`
- Modify: `subscription/manager.go`
- Modify: `subscription/register_set.go`

Tests:
- Modify: `protocol/handle_subscribe_test.go`
- Modify: `executor/protocol_inbox_adapter_test.go`
- Modify: `subscription/manager_test.go`
- Possibly modify: `subscription/hash_test.go` if a direct canonical-hash pin is the clearest way to lock the literal-vs-parameterized distinction.

Docs:
- Modify: `TECH-DEBT.md`
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`
- Modify: `NEXT_SESSION_HANDOFF.md`

---

## Proposed implementation shape

Prefer a sidecar parameterization signal over changing predicate execution semantics.

Recommended minimal model:
1. Extend the protocol/executor/subscription register-request path with per-predicate hash metadata, not a single request-global client identity.
2. Have `compileSQLQueryString(...)` return both:
   - the compiled `subscription.Predicate`
   - whether the SQL predicate tree used caller-bound `:sender`
3. On subscribe registration:
   - for a predicate compiled from `:sender`, attach `&conn.Identity` as that predicate's hash identity
   - for a predicate compiled only from concrete literals, attach `nil`
4. In `subscription.RegisterSet(...)`, compute each predicate hash using its paired identity sidecar instead of one request-global value.

Why per-predicate sidecar is safer than a request-global identity:
- `SubscribeMulti` can legally mix literal-only queries and `:sender` queries in one batch.
- A single request-global `ClientIdentity` would accidentally parameterize literal-only queries too, defeating intended cross-client dedup for those shapes.
- A parallel slice (`PredicateHashIdentities []*types.Identity`) keeps the slice narrow and avoids changing predicate evaluation, validation, or wire behavior.

Do not do this in the slice:
- do not add new predicate runtime types just to remember `:sender`
- do not make all protocol subscriptions client-identity-scoped
- do not change one-off query behavior or row results
- do not widen `:sender` onto unsupported column kinds

---

## Task 1: Add failing protocol-level pins for `:sender` provenance

Objective: prove the live subscribe compile path loses parameterization metadata even though it already compiles the accepted query successfully.

Files:
- Modify: `protocol/handle_subscribe_test.go`

Step 1: Add a failing subscribe-single pin for `:sender` metadata.

Suggested test name:
- `TestHandleSubscribeSingle_SenderParameterCarriesHashIdentity`

Test shape:
- Reuse the existing accepted single-table shape `SELECT * FROM s WHERE bytes = :sender`.
- Assert the executor-facing `RegisterSubscriptionSetRequest` carries:
  - one compiled predicate of the existing runtime type (`subscription.ColEq`)
  - one matching per-predicate hash-identity entry equal to `conn.Identity`
- Keep the existing value assertion too: the runtime predicate value should still be caller bytes.

Expected current result:
- FAIL because the request type does not yet carry any hash-identity/provenance field.

Step 2: Add a literal-control test.

Suggested test name:
- `TestHandleSubscribeSingle_LiteralBytesDoesNotCarryHashIdentity`

Test shape:
- Use an accepted literal query such as `SELECT * FROM s WHERE bytes = 0x0102`.
- Assert the same executor-facing request carries `nil` hash identity for that predicate.

Step 3: Run the focused protocol tests.

Run:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_(SenderParameterCarriesHashIdentity|LiteralBytesDoesNotCarryHashIdentity)' -count=1`

---

## Task 2: Add failing mixed-batch pin for SubscribeMulti

Objective: prove the fix must be per-predicate, not request-global.

Files:
- Modify: `protocol/handle_subscribe_test.go`

Step 1: Add a failing mixed multi-query pin.

Suggested test name:
- `TestHandleSubscribeMulti_MixedLiteralAndSenderParameterCarriesPerPredicateHashIdentity`

Test shape:
- Submit two accepted queries in one batch:
  - literal-only: `SELECT * FROM s WHERE u32 = 7`
  - parameterized: `SELECT * FROM s WHERE bytes = :sender`
- Assert the executor-facing request carries two predicates and a parallel hash-identity slice where:
  - entry 0 is `nil`
  - entry 1 equals `conn.Identity`

Why this matters:
- It prevents a false “easy fix” that marks the whole set as parameterized.

Step 2: Run just the new multi test.

Run:
- `rtk go test ./protocol -run 'TestHandleSubscribeMulti_MixedLiteralAndSenderParameterCarriesPerPredicateHashIdentity' -count=1`

---

## Task 3: Add failing adapter-forwarding pins

Objective: prove protocol metadata survives into the subscription-manager request.

Files:
- Modify: `executor/protocol_inbox_adapter_test.go`

Step 1: Add a failing adapter test for sender-parameter forwarding.

Suggested test name:
- `TestProtocolInboxAdapter_RegisterSubscriptionSet_ForwardsPerPredicateHashIdentity`

Test shape:
- Build a `protocol.RegisterSubscriptionSetRequest` with one predicate and one non-nil hash-identity sidecar.
- Capture the resulting `subscription.SubscriptionSetRegisterRequest` submitted to the fake manager.
- Assert the sidecar arrives unchanged.

Step 2: Add a mixed-batch forwarding test.

Suggested test name:
- `TestProtocolInboxAdapter_RegisterSubscriptionSet_ForwardsMixedPerPredicateHashIdentity`

Test shape:
- Two predicates, sidecars `[nil, &id]`.
- Assert the manager request preserves order and nil/non-nil distinction.

Step 3: Run focused adapter tests.

Run:
- `rtk go test ./executor -run 'TestProtocolInboxAdapter_RegisterSubscriptionSet_Forwards.*HashIdentity' -count=1`

---

## Task 4: Add failing manager-level identity pins for mixed sets

Objective: pin the intended runtime hash behavior once per-predicate identities exist.

Files:
- Modify: `subscription/manager.go`
- Modify: `subscription/register_set.go`
- Modify: `subscription/manager_test.go`

Step 1: Add a failing mixed-register-set dedup test.

Suggested test name:
- `TestRegisterSet_MixedHashIdentitiesOnlyParameterizeMarkedPredicates`

Test shape:
- Register the same two-predicate set on two different connections:
  - predicate A: literal-only, same runtime predicate on both registrations
  - predicate B: same runtime predicate, but one registration carries identity A and the other carries identity B
- Expected manager result after both registrations:
  - one shared query state for predicate A
  - two distinct query states for predicate B

Step 2: Add a parameterized-vs-literal collision test.

Suggested test name:
- `TestRegisterSet_ParameterizedSenderHashDiffersFromLiteralEquivalent`

Test shape:
- Construct two runtime-identical `subscription.ColEq` predicates with the same concrete bytes value.
- Register one with nil sidecar and one with non-nil identity sidecar.
- Assert they do not collapse onto the same `QueryHash` / query state.

This is the key pin for the confirmed mismatch.

Step 3: Run manager-focused tests.

Run:
- `rtk go test ./subscription -run 'TestRegister(Set_.*HashIdentitiesOnlyParameterizeMarkedPredicates|Set_ParameterizedSenderHashDiffersFromLiteralEquivalent|ParameterizedHashUsesClientIdentity)' -count=1`

---

## Task 5: Implement the narrow fix

Objective: preserve `:sender` provenance for hashing without changing execution semantics.

Files:
- Modify: `protocol/handle_subscribe.go`
- Modify: `protocol/handle_subscribe_single.go`
- Modify: `protocol/handle_subscribe_multi.go`
- Modify: `protocol/lifecycle.go`
- Modify: `executor/protocol_inbox_adapter.go`
- Modify: `subscription/manager.go`
- Modify: `subscription/register_set.go`

Implementation steps:
1. In `protocol/handle_subscribe.go`, extend `compiledSQLQuery` with a hash-provenance flag, for example:
   - `HashIdentity *types.Identity` is too early because compile should stay pure relative to request ownership.
   - Prefer `UsesCallerIdentity bool`.
2. Add a small SQL-predicate walker in `protocol/handle_subscribe.go` that returns true when any leaf literal is `sql.LitSender`.
   - Reuse the parsed `stmt.Predicate`; do not change parser types unless absolutely necessary.
3. Extend `protocol.RegisterSubscriptionSetRequest` in `protocol/lifecycle.go` with a parallel metadata slice, for example:
   - `PredicateHashIdentities []*types.Identity`
4. In `handleSubscribeSingle` and `handleSubscribeMulti`:
   - populate the slice alongside `Predicates`
   - set entry `i` to `&conn.Identity` only when the compiled query used caller identity
   - otherwise set entry `i` to `nil`
5. Extend `subscription.SubscriptionSetRegisterRequest` similarly with the same parallel slice.
6. In `executor/protocol_inbox_adapter.go`, forward the slice from protocol request to subscription request unchanged.
7. In `subscription/register_set.go`, replace request-global hash use with per-predicate sidecars:
   - dedup loop: `ComputeQueryHash(p, req.PredicateHashIdentities[i])`
   - allocation loop: same paired identity per predicate
8. Keep `ValidatePredicate(...)`, `initialQuery(...)`, one-off query execution, and compiled predicate value shapes unchanged.

Guardrails:
- preserve existing request/response ordering and reply behavior
- preserve runtime predicate types (`ColEq`, `Join`, `Or`, etc.)
- preserve nil behavior when no parameterization is present
- validate slice-length mismatches defensively; reject malformed internal requests rather than silently pairing wrong identities to predicates

---

## Task 6: Validate the narrow slice

Objective: prove the new hash/provenance seam closes without regressing accepted `:sender` execution.

Step 1: Run the new focused tests.

Run:
- `rtk go test ./protocol -run 'TestHandleSubscribe(Single|Multi)_.*HashIdentity' -count=1`
- `rtk go test ./executor -run 'TestProtocolInboxAdapter_RegisterSubscriptionSet_Forwards.*HashIdentity' -count=1`
- `rtk go test ./subscription -run 'TestRegister(Set_.*HashIdentitiesOnlyParameterizeMarkedPredicates|Set_ParameterizedSenderHashDiffersFromLiteralEquivalent|ParameterizedHashUsesClientIdentity)' -count=1`

Step 2: Re-run nearby accepted-shape `:sender` tests to ensure execution behavior is unchanged.

Run:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_SenderParameter|TestHandleOneOffQuery_SenderParameter' -count=1`

Step 3: Run broader package coverage.

Run:
- `rtk go test ./protocol/... ./subscription/... ./executor/... -count=1`

Step 4: Run full suite.

Run:
- `rtk go test ./... -count=1`

Optional if exported/internal request contracts changed in a way vet can help with:
- `rtk go vet ./protocol ./executor ./subscription`

---

## Task 7: Update docs and handoff in the same session

Objective: record the closed seam and point the next session at the next real A2 residual.

Files:
- Modify: `TECH-DEBT.md`
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`
- Modify: `NEXT_SESSION_HANDOFF.md`

Doc updates to make:
- mark the `:sender` parameter provenance / parameterized-hash seam as closed if tests land cleanly
- describe the exact closure narrowly: accepted `:sender` subscribe shapes now preserve parameterized hash identity without changing execution semantics
- name the next residual only after a fresh live scout; likely candidates remain in accepted-shape normalization/validation drift, not join ordering

---

## Risks and tradeoffs

1. Request-shape churn across protocol/executor/subscription seams
- Risk: adding sidecar metadata touches multiple boundary structs.
- Mitigation: keep it a parallel slice with nil defaults, plus focused adapter tests.

2. Over-parameterizing literal-only queries
- Risk: a request-global identity field would break intended dedup for mixed/literal queries.
- Mitigation: make the metadata per-predicate from the start.

3. Accidental execution-path changes
- Risk: using provenance to change predicate evaluation rather than hashing.
- Mitigation: keep all runtime predicate values and validation logic unchanged; only hash pairing changes.

4. Mixed-set indexing mistakes
- Risk: sidecar slice length/order drift could pair wrong identities to wrong predicates.
- Mitigation: validate lengths and add explicit mixed-order tests.

---

## Open questions to resolve during implementation scout

These should be answered before editing, but they do not block the overall plan:
1. Best field name for the parallel sidecar on protocol/subscription requests:
   - `PredicateHashIdentities`
   - `HashClientIdentities`
   - `ParameterizedClientIDs`
2. Whether to add a tiny direct helper in `protocol/handle_subscribe.go` like `sqlPredicateUsesCallerIdentity(pred sql.Predicate) bool` versus hanging a helper off `query/sql`.
   - Prefer protocol-local helper unless multiple packages need it immediately.
3. Whether `subscription/hash_test.go` needs a direct canonical-hash pin in addition to manager-level pins.
   - Likely optional if manager tests already lock the user-visible dedup outcome.

---

## Expected end state

After this slice:
- accepted subscribe SQL using `:sender` still compiles to the same executable runtime predicates as today
- one-off execution behavior is unchanged
- subscribe registration preserves parameterized hash identity for `:sender` predicates
- a literal bytes query no longer collides semantically with the parameterized `:sender` form just because normalization erased provenance
- mixed SubscribeMulti batches parameterize only the marked predicates, not the whole set
- docs/handoff point at the next real A2 residual instead of reopening closed join-ordering work
