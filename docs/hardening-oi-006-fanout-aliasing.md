# OI-006 — fanout per-subscriber slice aliasing (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-006
(subscription fanout aliasing / cross-subscriber mutation risk) landed
2026-04-20.

Follows the same shape as the prior OI-005 sub-slices
(`docs/hardening-oi-005-snapshot-iter-retention.md`,
`docs/hardening-oi-005-snapshot-iter-useafterclose.md`).

## Sharp edge

`subscription/eval.go::evaluate` builds one `[]SubscriptionUpdate`
per query hash and then distributes it across every subscriber of
that query. Before this slice the per-subscriber distribution loop
was:

```go
for connID, subIDs := range qs.subscribers {
    for subID := range subIDs {
        for _, u := range updates {
            u.SubscriptionID = subID
            fanout[connID] = append(fanout[connID], u)
        }
    }
}
```

`u` is value-copied per subscriber and `SubscriptionID` is
overwritten, but `u.Inserts` and `u.Deletes` (`[]types.ProductValue`)
are still slice headers that reference the **same backing array**
across every subscriber for that query.

Consequence: any downstream consumer that mutates one subscriber's
`Inserts` / `Deletes` slice — replacing an element, appending past
the visible length, or appending within shared capacity — silently
corrupts every other subscriber's view of the same commit. The
invariant "downstream must treat these as read-only" was load-bearing
but unpinned; nothing in code or tests asserted it.

The downstream consumers today (`subscription/fanout_worker.go`,
`protocol/fanout_adapter.go::encodeRows` and
`encodeSubscriptionUpdateMemoized`) only read these slices, so the
hazard has not surfaced as a production bug. But the contract was
fragile: a future caller adding any in-place mutation would silently
corrupt other subscribers' deliveries with no test to catch it.

## Fix

The per-subscriber distribution loop now clones each
`SubscriptionUpdate.Inserts` and `.Deletes` slice header per
subscriber:

```go
for connID, subIDs := range qs.subscribers {
    for subID := range subIDs {
        for _, u := range updates {
            cloned := u
            cloned.SubscriptionID = subID
            if len(cloned.Inserts) > 0 {
                cloned.Inserts = append([]types.ProductValue(nil), cloned.Inserts...)
            }
            if len(cloned.Deletes) > 0 {
                cloned.Deletes = append([]types.ProductValue(nil), cloned.Deletes...)
            }
            fanout[connID] = append(fanout[connID], cloned)
        }
    }
}
```

Each subscriber now owns an independent slice header backed by an
independent array. Element-replace (`s[i] = x`) and append on one
subscriber's slice no longer leak into another subscriber's view.

Row payloads (`types.ProductValue`, itself `[]Value`) are still
shared. That sharing is governed by the post-commit row-immutability
contract: rows produced by the store are not mutated in place after a
commit completes. Deepening the copy down to row contents would cost
work proportional to row width × row count × subscriber count for no
client-visible benefit under that contract.

Diff surface:
- `subscription/eval.go::evaluate` — the inner per-update loop now
  clones `Inserts` / `Deletes` slice headers per subscriber.

## Pinned by

Two focused tests in `subscription/eval_fanout_aliasing_test.go`,
one each for `Inserts` and `Deletes`. Each registers two subscribers
on different connection IDs against the same query, runs one
`EvalAndBroadcast`, asserts the two subscribers' slice elements have
distinct addresses, then exercises both failure modes:

- replace subscriber A's element 0 wholesale and assert subscriber
  B's element 0 is unchanged
- append onto subscriber A's slice and assert subscriber B's `len`
  is unchanged

Tests:
- `TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers`
- `TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers`

Both fail without the fix (the address-equality assertion fires
first), pass with it.

## Scope

This slice closes one specific sub-hazard of OI-006 (cross-subscriber
slice-header aliasing on `Inserts` / `Deletes`). It does **not**
close:

- aliasing of the row payloads themselves (`types.ProductValue`).
  Reference contract: rows are immutable post-commit; no in-tree
  consumer mutates row contents. Deepening the copy would be
  work-proportional with no client-visible benefit and is not
  required by the parity model.
- the `SubscriptionUpdate` struct's `TableName` string sharing
  (immutable strings; not a hazard).
- broader fanout assembly hazards in `subscription/fanout.go`,
  `subscription/fanout_worker.go`, or
  `protocol/fanout_adapter.go` if any of those grow new mutation
  paths in the future.

OI-006 stays open for those broader fanout hazards. The narrow
sub-hazard closed here is the slice-header aliasing in `evaluate`.

## Authoritative artifacts

- This document.
- `subscription/eval.go` — fix surface.
- `subscription/eval_fanout_aliasing_test.go` — new focused tests.
- `TECH-DEBT.md` — OI-006 updated with sub-hazard closed + pin
  anchors.
- `docs/current-status.md` — hardening / correctness bullet
  refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
