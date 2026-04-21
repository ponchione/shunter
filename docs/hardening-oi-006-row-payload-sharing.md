# OI-006 — fanout row-payload sharing contract pin (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-006
(subscription fanout aliasing / cross-subscriber mutation risk) landed
2026-04-21.

Closes the OI-006 row-payload sharing sub-hazard: `types.ProductValue`
(itself `[]Value`) backing arrays are shared across subscribers of the
same query for both `SubscriptionUpdate.Inserts` and `.Deletes`.
Sharing is governed by the post-commit row-immutability contract and is
intentional — a deep copy would cost work proportional to row width ×
row count × subscriber count for no client-visible benefit — but the
contract was load-bearing and unpinned.

Follows the contract-pin shape of the 2026-04-21 OI-005 slice
`docs/hardening-oi-005-committed-state-table-raw-pointer.md` (contract
comment + observational pins, no production-code semantic change).

## Sharp edge

`subscription/eval.go::evaluate` distributes one
`[]SubscriptionUpdate` per query hash to every subscriber of that
query. The 2026-04-20 OI-006 slice-header sub-slice
(`docs/hardening-oi-006-fanout-aliasing.md`) closed slice-header
aliasing on `Inserts` / `Deletes`: each subscriber now owns an
independent outer `[]types.ProductValue` so element-replace
(`s[i] = x`) and append on one subscriber no longer leak into another.

Row payloads — each `types.ProductValue`, itself `[]Value` — are still
shared across subscribers by design. The per-subscriber clone at
`subscription/eval.go:123-128` copies the outer slice headers but the
inner `ProductValue` slice headers point at the same `[]Value` backing
array:

```go
cloned.Inserts = append([]types.ProductValue(nil), cloned.Inserts...)
```

`append` copies `ProductValue` slice-header values into the new outer
backing array. Each copied header still references the original
`[]Value` backing array, so `&updA[0].Inserts[0][0] ==
&updB[0].Inserts[0][0]` holds across subscribers even after the
slice-header fix.

This sharing is intentional under the post-commit row-immutability
contract: rows produced by the store after commit completion are not
mutated in place by any downstream consumer. Deepening the copy to
independent `[]Value` backing arrays per subscriber would cost work
proportional to row width × row count × subscriber count for no
client-visible benefit under the contract.

The downstream consumers today — `subscription/fanout_worker.go` (the
delivery loop at `deliver`), `protocol/fanout_adapter.go::encodeRows`
(`bsatn.EncodeProductValue` on each row, read-only), and
`encodeSubscriptionUpdateMemoized` (memoizes encoded bytes, not row
values) — all satisfy the read-only contract. But the contract was
unwritten: a future consumer that mutated `Value` elements in place
during encoding (e.g., rewriting a timestamp column to its UTC
normalization before bsatn-encoding) would silently corrupt every
other subscriber's view of the same commit with no test to catch it.

Three hazards the contract prevents but that were never asserted:

- **In-place Value mutation on any downstream path**: a consumer that
  mutated `updates[i].Inserts[j][k]` or `.Deletes[j][k]` during
  delivery/encoding would leak the mutation into every other
  subscriber's `SubscriptionUpdate` for the same query.
- **ProductValue header mutation on a shared backing**: a consumer
  that grew one subscriber's ProductValue via `append` within shared
  cap, then mutated the newly-visible tail, could corrupt peer
  ProductValues that still alias the same underlying `[]Value`. This
  is a narrower variant of the same hazard shape.
- **Store-side mutation after commit**: a store-layer change that
  mutated already-committed rows in place (e.g., lazy field
  normalization on read) would be externally indistinguishable from
  an in-place fanout mutation — the post-commit row-immutability
  contract is the store-side counterpart of the downstream read-only
  contract.

## Fix

Narrow contract pin, no production-code semantic change:

1. Contract comment on `subscription/eval.go::evaluate` per-subscriber
   fanout loop — extends the existing OI-006 comment to name the
   post-commit row-immutability contract, enumerate the three hazards
   the contract prevents, and pin the read-only discipline that every
   downstream consumer must satisfy.
2. Contract comment on `subscription/fanout_worker.go::FanOutSender` —
   declares that `callerUpdates` / `updates` slices passed into
   `SendTransactionUpdateHeavy` / `SendTransactionUpdateLight` are
   read-only: row payloads (`types.ProductValue`) are shared across
   subscribers and in-place mutation corrupts peers.
3. Contract comment on `protocol/fanout_adapter.go::encodeRows` —
   declares the read-only row-iteration contract at the bsatn-encode
   boundary so any future consumer that touches row contents past
   `encodeRows` inherits a visible contract.
4. Pin tests in `subscription/eval_fanout_row_payload_sharing_test.go`
   that assert the observable invariants making the contract
   auditable:
   - `TestEvalFanoutRowPayloadsSharedAcrossSubscribersForInserts` —
     asserts that `&updA[0].Inserts[0][0] == &updB[0].Inserts[0][0]`
     across two subscribers of the same query (sharing is intentional)
     and then pins the hazard shape by mutating
     `updA[0].Inserts[0][1]` in place and asserting the change is
     visible in `updB[0].Inserts[0][1]` (mutation leaks between
     subscribers).
   - `TestEvalFanoutRowPayloadsSharedAcrossSubscribersForDeletes` —
     same pair of pins for the `Deletes` side.

No production code path changes. The contract held before this slice;
the slice makes the contract asserted and visible to future changes.

Diff surface:
- `subscription/eval.go::evaluate` — contract comment extension.
- `subscription/fanout_worker.go::FanOutSender` — contract comment.
- `protocol/fanout_adapter.go::encodeRows` — contract comment.
- `subscription/eval_fanout_row_payload_sharing_test.go` — two new
  focused tests.

## Scope / limits

This is a contract pin, not a deep-copy enforcement mechanism:

- The pins document the post-commit row-immutability contract
  observationally. They do not prevent a future change from mutating
  a shared row payload in place during delivery or encoding. Catching
  that class of bug would require either per-subscriber deep-copy of
  `Value` backing arrays (work proportional to row width × row count ×
  subscriber count, unjustified under the contract) or an immutability
  wrapper around `types.ProductValue` at the fanout boundary.
- The store-side counterpart of this contract — that already-committed
  rows are not mutated in place by the store itself — is enforced by
  the existing single-writer executor discipline and the
  `CommittedSnapshot` open→Close RLock lifetime (OI-005 envelopes).
  Breaking either would reopen this sub-hazard even with the pin.

## Pinned by

Two focused tests in
`subscription/eval_fanout_row_payload_sharing_test.go`:

- `TestEvalFanoutRowPayloadsSharedAcrossSubscribersForInserts` — two
  subscribers on different connection IDs, same query, one insert;
  asserts inner `[]Value` backing-array pointer identity across
  subscribers, then mutates subscriber A's `Value` at column 1 and
  asserts the mutation is visible in subscriber B's view. Fails if a
  future change deep-copies row payloads per subscriber (the
  identity assertion fires) or if a future change severs the
  shared-backing contract without updating the test (the mutation
  assertion fires).
- `TestEvalFanoutRowPayloadsSharedAcrossSubscribersForDeletes` — same
  pair of pins for the `Deletes` side.

Both pass under `-race -count=3`.

Already-landed OI-005 / OI-006 pins unchanged and still passing:
- `TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers` (2026-04-20
  slice-header isolation — asserts outer-slice independence; this
  slice documents the complement: inner backing-array sharing is
  intentional).
- `TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers`
- `TestCommittedStateTableSameEnvelopeReturnsSamePointer`
- `TestCommittedStateTableRetainedPointerIsStaleAfterReRegister`
- `TestCommittedStateTableSnapshotEnvelopeHoldsRLockUntilClose`
- `TestStateViewScanTableIteratesIndependentOfMidIterCommittedDelete`
- `TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join`
- `TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable`

## Remaining OI-006 sub-hazards

- broader fanout assembly hazards in `subscription/fanout.go`,
  `subscription/fanout_worker.go`, and `protocol/fanout_adapter.go`
  if any of those grow new mutation paths in the future. The
  contract-pin comments added here name the read-only discipline so
  a future mutation introduction is visibly unsafe.

OI-006 stays open as a theme because the read-only contract is
enforced by discipline and observational pins rather than
machine-enforced immutability at the `types.ProductValue` boundary.

## Authoritative artifacts

- This document.
- `subscription/eval.go::evaluate` — contract comment extension.
- `subscription/fanout_worker.go::FanOutSender` — contract comment.
- `protocol/fanout_adapter.go::encodeRows` — contract comment.
- `subscription/eval_fanout_row_payload_sharing_test.go` — two new
  focused tests.
- `TECH-DEBT.md` — OI-006 updated with this sub-hazard closed + pin
  anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
