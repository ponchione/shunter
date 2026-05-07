# Subscription Parity Handoff

Date: 2026-05-07

This note is a restart point for the subscription parity work in
`docs/v1-roadmap/11-subscription-parity.md`. It records what was audited or
advanced recently so a future session can avoid redoing the same broad pass.

## Recent Baseline

Start future work with the normal repo startup checks:

```bash
rtk git status --short
rtk git log --oneline -30
```

Relevant recent subscription commits:

- `d1f9c77 subscription: pin declared view index contracts`
- `b567695 subscription: document generic pruning completion`
- `9eda657 subscription: pin protocol lifecycle error paths`

There are also newer TypeScript/client commits in the history. Treat them as
baseline unless the next task explicitly touches generated client behavior.

## Workstream State

### Workstream 4: Index Contracts

Status: complete for the v1 live-read shapes currently marked supported.

Evidence added or confirmed:

- Raw subscribe rejects unindexed two-table and multi-way live joins before
  executor registration.
- Declared live views can compile richer shapes, but runtime/protocol
  subscriptions still route through `subscription.Manager.RegisterSet` and
  reject unindexed live joins before query/pruning state is registered.
- Accepted joins track the delta-index columns needed for evaluation.
- Unregister and disconnect remove registry, pruning, and delta-index refs.
- Shared join-edge refs remain live until the final overlapping subscription
  is removed.
- One-off reads remain broader than live reads; scan fallback there is not a
  live-subscription admission rule.

Key tests to inspect before changing this area:

- `TestSubscribeViewUnindexedJoinRejectedBeforeRegistration`
- `TestProtocolDeclaredViewUnindexedJoinRejected`
- `TestUnregisterSetKeepsSharedJoinEdgeRefsUntilLastSubscription`
- raw subscribe unindexed-join tests in `protocol/handle_subscribe_test.go`

### Workstream 5: Generic Candidate Pruning

Status: effectively complete for the current v1 traversal contract.

Evidence added or confirmed:

- `rtk go doc ./subscription` exposes no public fixed-hop path-edge API.
- Path traversal indexes are generic internal types:
  `joinPathTraversalEdge`, `joinPathTraversalIndex`, and
  `joinRangePathTraversalIndex`.
- Placement tests build expected edges with `mustJoinPathTraversalEdge` for
  three-hop through nine-hop paths and `joinPathTraversalMaxHops`.
- Beyond-limit traversal intentionally falls back to a direct existence edge,
  pinned by `TestMultiJoinPlacementSplitOrBeyondMaxHopFallsBackToExistenceEdge`.
- Candidate tests cover committed and same-transaction inserted/deleted rows,
  including non-key-preserving mismatch and overlap cases.

Do not reintroduce fixed-hop `JoinPathNEdge`/`JoinRangePathNEdge` wrappers.

### Workstream 6: Protocol Lifecycle Parity

Status: in progress.

Evidence added or confirmed:

- Handler reply closures now have direct `SubscriptionError` delivery tests for
  subscribe and unsubscribe paths.
- Applied envelopes are already covered for Single and Multi subscribe and
  unsubscribe paths.
- Initial applied frames are pinned before later fan-out on the same connection.
- Unsubscribe tests and gauntlet coverage verify removed queries do not receive
  later transaction updates.
- Disconnect cleanup now directly checks query sets, registry state, pruning
  indexes, delta-index refs, and active subscription-set counts.
- Backpressure, dropped-client signaling, fan-out worker cleanup, and stable
  wire-shape tests exist, but Workstream 6 has not had the final lifecycle pass
  needed to mark it complete.

Best next slice:

1. Audit Workstream 6 only; do not redo Workstreams 4 or 5 unless a lifecycle
   issue depends on them.
2. Focus on reducer/transaction ordering, disconnect/backpressure paths, and
   golden wire coverage for subscribe, unsubscribe, and declared-view subscribe.
3. If coverage is already sufficient, update Workstream 6 status in
   `11-subscription-parity.md` with concrete evidence and commit that docs
   slice. If gaps are found, add focused tests first.

Suggested focused files:

- `protocol/admission_ordering_test.go`
- `protocol/backpressure_out_test.go`
- `protocol/disconnect_test.go`
- `protocol/golden_wire_test.go`
- `protocol/handle_subscribe_test.go`
- `protocol/handle_unsubscribe_test.go`
- `declared_read_protocol_test.go`
- `subscription/fanout_worker_test.go`
- `subscription/register_set_test.go`

## Last Verification

After `9eda657`, these passed:

```bash
rtk go fmt ./protocol ./subscription
rtk go test ./protocol
rtk go test ./subscription
rtk go vet ./protocol ./subscription
rtk go test . ./query/... ./protocol ./subscription
rtk go vet . ./query/... ./protocol ./subscription
rtk git diff --check
```

The broad test run reported 3058 passed across the selected packages.

## Pickup Rules

- Keep `reference/SpacetimeDB/` read-only and research-only.
- Prefer Go-native tools before broad text search.
- Stage only files touched for the intended slice.
- Commit coherent slices.
- If no issue is found, do not make an empty commit; document the evidence only
  if it materially updates the roadmap.
