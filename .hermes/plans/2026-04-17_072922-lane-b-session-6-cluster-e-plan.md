# Lane B Session 6 — Cluster E (Post-Commit Fan-Out Shapes) Plan

> **Scope:** Docs-only. `docs/decomposition/**` + `AUDIT_HANDOFF.md`. Live code (`subscription/`, `executor/`, `commitlog/`, `protocol/`, `store/`, `schema/`, `types/`, `bsatn/`) off-limits per Lane B rule; if a spec edit outruns live code, log as Session 12+ drift in `TECH-DEBT.md`.

**Goal:** Resolve Cluster E — declare each of the five post-commit fan-out types in one canonical spec with consumer cross-refs; reconcile `DurabilityHandle` / `WaitUntilDurable`; resolve eval-error-vs-fatal contradiction.

**Stop rule (from AUDIT_HANDOFF.md §B.4):** All five type shapes declared in one spec each with cross-refs from consumers; eval-error recovery model resolved (E7); `WaitUntilDurable` either added to SPEC-002 §4.2 or removed from impl with deferred-debt note.

## Current-state summary

Already-declared types (no re-declaration needed, just cross-ref alignment):

- `PostCommitMeta` — SPEC-004 §10.1 (canonical). Shape: `{TxDurable <-chan TxID, CallerConnID *ConnectionID, CallerResult *ReducerCallResult}`. Matches live `executor/interfaces.go:36` signature.
- `FanOutMessage` — SPEC-004 §8.1 (canonical). Shape: `{TxDurable, Fanout, Errors, CallerResult}`.
- `SubscriptionError` (Go) — SPEC-004 §10.2 (canonical). Wire — SPEC-005 §8.4.
- `ReducerCallResult` (Go forward-decl) — SPEC-004 §10.2. Wire authoritative — SPEC-005 §8.7.
- `FanOutSender` — SPEC-004 §8.1 (canonical; 3 methods). `ClientSender` — SPEC-005 §13 (currently 2 methods; live adds `Send(connID, any)`).
- `DurabilityHandle` — SPEC-003 §7 + SPEC-002 §4.2 (both: 3 methods; no `WaitUntilDurable`). Live `executor/interfaces.go:19-22` narrow shape with `EnqueueCommitted` + `WaitUntilDurable`; live `commitlog/durability.go:181` implements `WaitUntilDurable`.

Gaps to close:

1. SPEC-003 §8 `SubscriptionManager.EvalAndBroadcast` signature is stale (no `PostCommitMeta` arg). SPEC-004 §10.1 has the up-to-date 4-arg form; live matches SPEC-004.
2. SPEC-005 §13 `ClientSender` missing `Send(connID, any)` method. Live has it. Adapter (`FanOutSenderAdapter`) pattern is real and undocumented in spec.
3. SPEC-002 §4.2 + SPEC-003 §7 `DurabilityHandle` lacks `WaitUntilDurable`; live has it.
4. SPEC-004 §11.1 per-sub eval-error recovery contradicts SPEC-003 §5.4 fatal rule.
5. SPEC-005 §8.4 `SubscriptionError.request_id = 0` collision with client-chosen request_id=0 (audit §2.4) — unpinned semantics.
6. SPEC-005 §3.9 `ReducerCallResult.status` enum DIVERGE not yet in a divergence block.
7. SPEC-004 §10.2 `ReducerCallResult` forward-decl vs SPEC-005 §8.7 wire shape parity check.
8. SPEC-004 §2.12 `PostCommitMeta.TxDurable` for empty-fanout transactions unpinned.

---

## Task 1: E6 — `DurabilityHandle` + `WaitUntilDurable`

**Files:**
- Modify: `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §4.2
- Modify: `docs/decomposition/003-executor/SPEC-003-executor.md` §7

**Decision:** Add `WaitUntilDurable(txID types.TxID) <-chan types.TxID` to both SPEC-002 §4.2 and SPEC-003 §7 `DurabilityHandle`. Live has it and serves the `FanOutMessage.TxDurable` channel; removing from impl would break confirmed-read opt-in. Keep `DurableTxID()` and `Close()` as commitlog-owner-level methods; document that the executor-side narrow consumer only uses `EnqueueCommitted` + `WaitUntilDurable` (one clarifying sentence; do not split into two interfaces in spec — live `executor/interfaces.go` does split it, track as drift).

- [ ] **Step 1: Edit SPEC-002 §4.2**

In `docs/decomposition/002-commitlog/SPEC-002-commitlog.md`, inside the `DurabilityHandle` interface block (around line 211-226), add `WaitUntilDurable` between `DurableTxID` and `Close`:

```go
// WaitUntilDurable returns a channel that receives txID when the
// transaction is durably persisted. If txID == 0, returns nil.
// If txID is already durable, the returned channel is pre-filled
// and closed. Used by the subscription fan-out worker to implement
// confirmed-read delivery (SPEC-004 §8).
WaitUntilDurable(txID TxID) <-chan TxID
```

Append one sentence after the interface block (before §4.3) explaining that the executor's narrow consumer surface uses only `EnqueueCommitted` + `WaitUntilDurable`; the full handle is owned by the commit-log subsystem for its own lifecycle. Cross-ref SPEC-003 §7.

- [ ] **Step 2: Edit SPEC-003 §7**

In `docs/decomposition/003-executor/SPEC-003-executor.md`, inside the `DurabilityHandle` interface block (around line 488-500), add the same `WaitUntilDurable` method. Also update the "Contract" bullet list to include: `WaitUntilDurable(0)` returns nil; `WaitUntilDurable(txID>0)` never blocks and always returns a channel (ready or pending).

- [ ] **Step 3: Grep for stale refs**

Run:
```
rtk rg -n 'DurabilityHandle|WaitUntilDurable' docs/decomposition/
```

Expected: every `DurabilityHandle` Go block now shows `WaitUntilDurable`. No residue saying "DurabilityHandle has 3 methods".

- [ ] **Step 4: Commit**

```
rtk git add docs/decomposition/002-commitlog/SPEC-002-commitlog.md docs/decomposition/003-executor/SPEC-003-executor.md
```
Commit message body: `docs: Lane B Cluster E — E6 DurabilityHandle.WaitUntilDurable`.

---

## Task 2: E1 — `PostCommitMeta` shape + `EvalAndBroadcast` signature

**Files:**
- Modify: `docs/decomposition/003-executor/SPEC-003-executor.md` §8 (SubscriptionManager)
- Modify: `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §10.1 (pin TxDurable-on-empty-fanout semantics)

**Decision:** SPEC-004 §10.1 is canonical for `PostCommitMeta`. SPEC-003 §8 `SubscriptionManager.EvalAndBroadcast` must take `meta PostCommitMeta` as the fourth arg to match SPEC-004 and live. Pin SPEC-004 §2.12 audit item (TxDurable for empty fanout): executor still supplies a non-nil channel whenever a tx was actually committed, even if the fanout map is empty, because `CallerResult` delivery may still need durability gating.

- [ ] **Step 1: Fix SPEC-003 §8 signature**

In `docs/decomposition/003-executor/SPEC-003-executor.md` around line 524, replace:
```go
EvalAndBroadcast(txID TxID, changeset *Changeset, view CommittedReadView)
```
with:
```go
EvalAndBroadcast(txID TxID, changeset *Changeset, view CommittedReadView, meta PostCommitMeta)
```

Add a sentence after the interface block: "`PostCommitMeta` is declared in SPEC-004 §10.1 and carries `TxDurable`, `CallerConnID`, `CallerResult` into the evaluator."

- [ ] **Step 2: Pin TxDurable-on-empty-fanout in SPEC-004 §10.1**

In `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md`, after the `PostCommitMeta` Go block (line 648), append a normative paragraph:

```
`TxDurable` is non-nil for every post-commit invocation, regardless of
whether `Fanout` is empty. The executor allocates the channel from the
durability handle before calling `EvalAndBroadcast`; an empty fan-out
tx with a caller-reducer result still needs durability gating for the
caller's `ReducerCallResult`. `TxDurable == nil` is reserved for the
zero-value `PostCommitMeta` used by tests that bypass the executor.
```

This closes audit §2.12.

- [ ] **Step 3: Grep for stale `EvalAndBroadcast` signatures**

```
rtk rg -n 'EvalAndBroadcast' docs/decomposition/
```

Expected: all doc occurrences show 4 args (three args + `meta PostCommitMeta`). If any 3-arg form remains, update it.

- [ ] **Step 4: Commit**

```
rtk git add docs/decomposition/003-executor/SPEC-003-executor.md docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md
```

---

## Task 3: E2 — `FanOutMessage` cross-ref

**Files:**
- Modify: `docs/decomposition/005-protocol/SPEC-005-protocol.md` §13 (SPEC-004 subsection)

**Decision:** SPEC-004 §8.1 is canonical. SPEC-005 §13 currently summarizes the delivery contract but doesn't cite the shape's home. Add one cross-ref sentence.

- [ ] **Step 1: Add cross-ref in SPEC-005 §13**

In `docs/decomposition/005-protocol/SPEC-005-protocol.md`, around line 605 (the "`FanOutMessage`" paragraph), insert after "...`TransactionUpdate`":

```
The `FanOutMessage` Go shape is declared in SPEC-004 §8.1; SPEC-005
does not redeclare the struct.
```

- [ ] **Step 2: Commit bundled with Task 4**

---

## Task 4: E5 — `ClientSender` / `FanOutSender` naming + `Send(connID, any)`

**Files:**
- Modify: `docs/decomposition/005-protocol/SPEC-005-protocol.md` §13 (ClientSender block)
- Modify: `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §8.1 / §10.2 (ensure cross-ref to adapter)

**Decision:** Distinct contracts; live adapter pattern stays. Add `Send(connID, any)` to SPEC-005 §13 `ClientSender`. Document that `FanOutSenderAdapter` (live `protocol/fanout_adapter.go`) satisfies SPEC-004's `FanOutSender` by wrapping `ClientSender.Send` for `SubscriptionError` delivery.

- [ ] **Step 1: Add `Send(connID, any)` to SPEC-005 §13 `ClientSender`**

In `docs/decomposition/005-protocol/SPEC-005-protocol.md` around line 617-625, prepend a `Send` method:

```go
type ClientSender interface {
    // Send encodes msg and enqueues the frame on the connection's
    // outbound channel. Used for direct server→client response
    // messages that do not have a dedicated typed method:
    // SubscribeApplied, UnsubscribeApplied, SubscriptionError,
    // OneOffQueryResult. Returns ErrClientBufferFull if the
    // client's outgoing buffer is full.
    Send(connID ConnectionID, msg any) error

    // SendTransactionUpdate queues a standalone post-commit delta for a client.
    // Returns ErrClientBufferFull if the client's outgoing buffer is full.
    SendTransactionUpdate(connID ConnectionID, update *TransactionUpdate) error

    // SendReducerResult queues the caller-visible reducer outcome, including
    // the caller's embedded transaction_update subset.
    SendReducerResult(connID ConnectionID, result *ReducerCallResult) error
}
```

Append a paragraph below the block describing the adapter pattern:

```
SPEC-004 §8.1 declares a narrower `FanOutSender` (three methods:
`SendTransactionUpdate`, `SendReducerResult`, `SendSubscriptionError`).
The protocol layer satisfies that contract with a thin adapter over
`ClientSender`, routing `SendSubscriptionError` through the generic
`Send(connID, msg)` path with a protocol-wire `SubscriptionError`
value. The two interfaces are intentionally distinct: `ClientSender`
is the cross-subsystem delivery surface; `FanOutSender` is the
subscription-side seam that hides protocol-package concerns from the
subscription package.
```

- [ ] **Step 2: SPEC-004 §10.2 cross-ref to adapter**

In `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` around line 705 ("The fan-out worker talks to the protocol layer through the narrow `FanOutSender` seam..."), append:

```
The adapter is the protocol package's `FanOutSenderAdapter`; it
converts subscription-domain values (`[]SubscriptionUpdate`,
`*ReducerCallResult`, raw message strings) into protocol-wire
structs before calling `ClientSender` (SPEC-005 §13). Delivery errors
are mapped back to `ErrSendBufferFull` / `ErrSendConnGone`
subscription-layer sentinels so the fan-out worker can react without
importing protocol types.
```

- [ ] **Step 3: Grep for stale method lists**

```
rtk rg -n 'ClientSender interface|FanOutSender interface' docs/decomposition/
```

Expected: SPEC-005 `ClientSender` now lists 3 methods starting with `Send`. SPEC-004 `FanOutSender` still lists 3 methods (`SendTransactionUpdate`, `SendReducerResult`, `SendSubscriptionError`).

- [ ] **Step 4: Commit (bundled E2 + E5)**

```
rtk git add docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md docs/decomposition/005-protocol/SPEC-005-protocol.md
```
Commit message body: `docs: Lane B Cluster E — E2 FanOutMessage xref + E5 ClientSender.Send`.

---

## Task 5: E3 — `SubscriptionError` shape + delivery + request_id=0 collision

**Files:**
- Modify: `docs/decomposition/005-protocol/SPEC-005-protocol.md` §8.4 (pin request_id semantics; note Go→wire field mapping gap)
- Modify: `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §10.2 (Go→wire mapping note) + §11.1 (delivery channel)

**Decision:**
- Go shape (SPEC-004 §10.2) carries `{SubscriptionID, QueryHash, Predicate, Message}`; wire shape (SPEC-005 §8.4) carries `{request_id, subscription_id, error}`. These are intentionally different: the Go struct is the internal diagnostic value; the wire frame is the client-facing projection. Pin this mapping explicitly.
- RequestID=0 collision: SPEC-005 §8.4 already says "0 if error occurred during re-evaluation". Add a normative rule that clients MUST NOT treat `request_id = 0` on `SubscriptionError` as correlatable to a pending `Subscribe`; any error with `request_id != 0` is correlated; `request_id = 0` means spontaneous (post-register) failure. Clients that supply `request_id = 0` on `Subscribe` cannot distinguish correlated vs spontaneous errors — document as a client-side footgun and resolve by recommending `request_id >= 1` (matches the general monotonic-request-id convention).
- Delivery channel: SPEC-004 §11.1 says "send a `SubscriptionError` message" — pin that this goes through the `FanOutSender.SendSubscriptionError` seam (which under the hood calls `ClientSender.Send(connID, protocol.SubscriptionError{...})`).

- [ ] **Step 1: Edit SPEC-005 §8.4**

In `docs/decomposition/005-protocol/SPEC-005-protocol.md` around line 371-382, append after the wire field block:

```
**Go↔wire mapping.** The subscription evaluator's internal
`SubscriptionError` Go value (SPEC-004 §10.2) carries additional
diagnostic fields (`QueryHash`, `Predicate`) that are not on the
wire; the protocol adapter projects Go→wire by emitting
`error = Message`. Only `subscription_id` and `error` round-trip.

**`request_id = 0` semantics.** A `SubscriptionError` with
`request_id = 0` is a spontaneous post-register failure (eval-time
error, join-index resolution, etc.) that is not correlated with any
pending `Subscribe` RPC. A `SubscriptionError` with `request_id != 0`
MUST echo the `request_id` of the triggering `Subscribe`. Clients
that choose `request_id = 0` on `Subscribe` accept that correlated
registration failures and spontaneous failures are indistinguishable;
recommend `request_id >= 1` for robust client-side correlation.
```

- [ ] **Step 2: Edit SPEC-004 §10.2**

After the `SubscriptionError` Go block (around line 690), append:

```
The wire form (SPEC-005 §8.4) projects only `SubscriptionID` and
`Message`; `QueryHash` and `Predicate` are retained in the Go value
for server-side logging and are not sent to clients.
```

- [ ] **Step 3: Edit SPEC-004 §11.1**

In `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §11.1 (around line 730-736), rewrite step 2:

```
2. Emit the error through the `FanOutSender.SendSubscriptionError`
   seam (SPEC-004 §8.1) for each affected client. The protocol
   adapter (SPEC-005 §13) translates this into a wire-format
   `SubscriptionError` delivered via `ClientSender.Send`; the wire
   `request_id` is `0` for these spontaneous failures (see SPEC-005
   §8.4 `request_id` semantics).
```

- [ ] **Step 4: Commit**

```
rtk git add docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md docs/decomposition/005-protocol/SPEC-005-protocol.md
```
Commit message body: `docs: Lane B Cluster E — E3 SubscriptionError shape + delivery`.

---

## Task 6: E4 — `ReducerCallResult` shape parity + status DIVERGE

**Files:**
- Verify: `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §10.2 forward-decl matches SPEC-005 §8.7 wire
- Modify: `docs/decomposition/005-protocol/SPEC-005-protocol.md` — find/create divergence block; add §3.9 audit item

**Decision:**
- Check parity: SPEC-004 §10.2 has `{RequestID uint32, Status uint8, TxID TxID, Error string, Energy uint64, TransactionUpdate []SubscriptionUpdate}`. SPEC-005 §8.7 wire: `{request_id, status, tx_id, error, energy, transaction_update}`. Parity OK. Just add a cross-ref that makes authority explicit.
- Status enum DIVERGE: SpacetimeDB uses `Committed/Failed/OutOfEnergy`; Shunter uses `0=committed, 1=failed_user, 2=failed_panic, 3=not_found`. Add to SPEC-005 divergence block (if missing).

- [ ] **Step 1: Add authority note in SPEC-004 §10.2**

After the `ReducerCallResult` forward-decl (around line 701), ensure the existing "SPEC-005 §8.7 owns the concrete wire shape" sentence remains but extend:

```
The Go struct fields are a one-to-one mapping of SPEC-005 §8.7 wire
fields (see field table there for LE widths and status-enum values).
The subscription evaluator never constructs the wire form directly;
the protocol adapter (`FanOutSenderAdapter.SendReducerResult`,
SPEC-005 §13) encodes it.
```

- [ ] **Step 2: Find SPEC-005 divergence home**

Grep for existing divergence/SpacetimeDB comparison block:

```
rtk rg -n 'Divergence|vs SpacetimeDB|Shunter differs|Clean-room' docs/decomposition/005-protocol/SPEC-005-protocol.md
```

If no dedicated block exists, add a `## Divergence from SpacetimeDB` section under §15 Open Questions (before §16 Verification). If a block exists, append into it.

- [ ] **Step 3: Add status-enum DIVERGE entry**

Insert the entry:

```
### ReducerCallResult.status enum

SpacetimeDB's wire protocol encodes `status` as a tagged union of
`{Committed, Failed(msg), OutOfEnergy}`. Shunter uses a flat
`uint8` enum: `0 = committed`, `1 = failed_user`, `2 = failed_panic`,
`3 = not_found`. Rationale: v1 has no energy model (SPEC-005 §3.10),
and `not_found` (unregistered reducer name) is a first-class outcome
distinct from runtime failure — the flat enum preserves that
distinction without a tagged-union encoding. Reference: SPEC-AUDIT
SPEC-005 §3.9.
```

- [ ] **Step 4: Commit**

```
rtk git add docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md docs/decomposition/005-protocol/SPEC-005-protocol.md
```
Commit message body: `docs: Lane B Cluster E — E4 ReducerCallResult authority + status DIVERGE`.

---

## Task 7: E7 — per-sub eval-error vs SPEC-003 §5.4 fatal

**Files:**
- Modify: `docs/decomposition/003-executor/SPEC-003-executor.md` §5.4
- Modify: `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §11.1 (already edited in Task 5; cross-ref SPEC-003)

**Decision:** Distinguish two classes:
- **Per-query evaluation errors** (corrupted index for one query, type mismatch in one predicate, index resolution failure): handled per SPEC-004 §11.1 — log, send `SubscriptionError`, unregister that query, continue. Not fatal.
- **Subsystem failures** (subscription manager panics, invariant violations per SPEC-004 §11.3, durability handle panics, delta-handoff panics): executor-fatal per SPEC-003 §5.4.

Edit SPEC-003 §5.4 to make the distinction explicit.

- [ ] **Step 1: Rewrite SPEC-003 §5.4**

In `docs/decomposition/003-executor/SPEC-003-executor.md` §5.4 (around line 444-457), replace the "Normative rule" bullet with:

```
Normative rule:
- If the durability subsystem, the subscription manager, or the
  delta-handoff path **panics** or signals an invariant violation
  after commit, the executor MUST transition the engine into a
  failed state and reject future write commands until restart.
- **Per-query evaluation errors** are NOT fatal. SPEC-004 §11.1
  scopes these: a single subscription's delta computation failing
  (corrupted index, type mismatch, join-index resolution) is logged,
  surfaced to affected clients via `SubscriptionError`, and the
  offending query is unregistered — the executor continues. The
  post-commit pipeline as a whole does not fail; only that query's
  subscribers see failure.
- The dividing line: if `SubscriptionManager.EvalAndBroadcast`
  returns normally, the executor continues. If it panics, the
  executor is fatal. Internal per-query failures that the manager
  catches and converts to `SubscriptionError` deliveries are
  normal returns.
```

- [ ] **Step 2: Confirm SPEC-004 §11.1 cross-refs SPEC-003 §5.4**

In `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §11.1, after the (already-edited) step 2 and the existing steps 3–4, append:

```
This recovery model is the contracted normal-return path from
`SubscriptionManager.EvalAndBroadcast`. The contrasting fatal-panic
rule in SPEC-003 §5.4 applies only when the evaluator or fan-out
subsystem panics or violates an invariant (see §11.3).
```

- [ ] **Step 3: Commit**

```
rtk git add docs/decomposition/003-executor/SPEC-003-executor.md docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md
```
Commit message body: `docs: Lane B Cluster E — E7 per-query eval-error vs fatal panic`.

---

## Task 8: Update AUDIT_HANDOFF.md tracking

**Files:**
- Modify: `AUDIT_HANDOFF.md`

**Per §B.5 procedure:** change statuses from `in-cluster` to `closed`; add resolution paragraph in §B.1 Cluster E; mark cluster heading `(closed Session 6)`; update §B.3 Session 6 row; update the top-of-file "Lane B cursor" line; update `CLAUDE-HANDOFF.md` to reflect new cursor (Session 7 — SPEC-001 residue).

- [ ] **Step 1: Edit §B.1 Cluster E**

Change the heading `#### Cluster E — Post-commit fan-out shapes (Session 6 — newly identified)` to `#### Cluster E — Post-commit fan-out shapes (closed Session 6)`.

Rewrite each E1–E7 bullet from `— <audit-refs>.` to `— **Resolved:** <one-line resolution summary>. <cross-ref sites>.`. Example template:

```
- **E1 `PostCommitMeta` shape** — **Resolved:** canonical declaration in SPEC-004 §10.1; SPEC-003 §8 `SubscriptionManager.EvalAndBroadcast` signature aligned to 4-arg form; TxDurable-on-empty-fanout semantics pinned.
```

Write one bullet per E1–E7.

- [ ] **Step 2: Update §B.2 status column**

For every row in §B.2 with status `in-cluster E1`, `in-cluster E2`, `in-cluster E3`, `in-cluster E4`, `in-cluster E5`, `in-cluster E6`, `in-cluster E7` (inspect rows in SPEC-002/003/004/005 tables), change to `closed`.

Specifically these rows:
- SPEC-002 §2.9 (E6)
- SPEC-003 §1.3 (E6), §3.4 (E7)
- SPEC-004 §1.1 (E1/E2), §1.3 (E2), §1.4 (E7), §2.3 (A2/E1/E2/E3/E4 — change E portions to closed; A2 already closed), §2.4 (E3), §2.5 (E4), §2.6 (E5), §2.12 (E1), §3.5 (E1), §4.1 (E1), §4.2 (E5), §5.5 (E3)
- SPEC-005 §1.1 (E3/E5), §1.2 (E2), §1.5 (E5), §2.4 (E3), §3.9 (E4)

- [ ] **Step 3: Update §B.3 Session 6 row stop-rule**

Change the stop rule text from "Five type shapes pinned..." to `**(closed)** Five shapes canonicalized: PostCommitMeta (SPEC-004 §10.1), FanOutMessage (SPEC-004 §8.1), SubscriptionError (SPEC-004 §10.2 Go / SPEC-005 §8.4 wire), ReducerCallResult (SPEC-004 §10.2 Go forward-decl / SPEC-005 §8.7 wire), ClientSender+FanOutSender (SPEC-005 §13 / SPEC-004 §8.1). E6 WaitUntilDurable added to SPEC-002 §4.2 + SPEC-003 §7. E7 per-query recovery vs fatal panic distinction pinned in SPEC-003 §5.4 + SPEC-004 §11.1.`

- [ ] **Step 4: Update top-of-file cursor line**

Change `Cursor: Session 6 (Cluster E — post-commit fan-out shapes).` (line 502) to `Cursor: Session 7 (SPEC-001 residue cleanup).`

Also update lines 2–5 (the Lane A/B intro block) to move the Lane B cursor from Session 6 to Session 7.

- [ ] **Step 5: Update `CLAUDE-HANDOFF.md`**

Rewrite the "Recommended next move" section to point to Session 7 (SPEC-001 residue cleanup); update the "Repo state" section commit list; update the "Two lanes coexist" cursor line.

- [ ] **Step 6: Commit**

```
rtk git add AUDIT_HANDOFF.md CLAUDE-HANDOFF.md
```
Commit message body: `docs: close Lane B Cluster E — Session 6 tracking update`.

---

## Task 9: Verification pass

- [ ] **Step 1: Grep for Cluster E artifacts**

```
rtk rg -n 'in-cluster E[1-7]' AUDIT_HANDOFF.md
```
Expected: no matches.

```
rtk rg -n 'Cluster E' AUDIT_HANDOFF.md
```
Expected: cluster heading marked `(closed Session 6)`.

- [ ] **Step 2: Grep for type-shape consistency**

```
rtk rg -n 'PostCommitMeta|FanOutMessage|SubscriptionError|ReducerCallResult|ClientSender|FanOutSender|DurabilityHandle|WaitUntilDurable' docs/decomposition/ | rtk wc -l
```
Expected: reasonable count (>20). Verify no Go block still shows a 3-method `DurabilityHandle` (should all show 4 methods now).

- [ ] **Step 3: `rtk git status` should be clean after Task 8 commit**

```
rtk git status
```
Expected: `working tree clean`.

- [ ] **Step 4: `rtk git log --oneline -10`**

Confirm five new commits landed in this session (Tasks 1, 2, 4+5 bundled are separate — actually 4 commits: Task 1, Task 2, Task 4+5+3 bundled into two, Task 6, Task 7, Task 8 = six commits. Adjust if bundled differently during execution).

---

## Drift log (if edits outrun live code)

If during execution any edit lands a contract the live code does not yet satisfy, add an entry to `TECH-DEBT.md` like TD-125/126/127 precedent. Expected candidates:

- **TD-128 (likely):** live `executor/interfaces.go:19` declares a narrow 2-method `DurabilityHandle` (EnqueueCommitted + WaitUntilDurable); SPEC-003 §7 + SPEC-002 §4.2 now declare the full 4-method handle. Realign impl in Session 12+ by merging the executor-side interface or widening `commitlog.DurabilityHandle` export.
- **TD-129 (possible):** live protocol `SubscriptionError` struct (`protocol/subscription_error.go` or similar — verify) omits `request_id` field; wire spec mandates it. If live lacks `RequestID`, log drift.

These are not fixes in this session — Lane B forbids live code touches. Log only.

---

## Self-review

Spec coverage check:
- E1 PostCommitMeta shape — Task 2 ✓
- E2 FanOutMessage shape — Task 3 ✓
- E3 SubscriptionError shape + delivery — Task 5 ✓
- E4 ReducerCallResult — Task 6 ✓
- E5 ClientSender/FanOutSender — Task 4 ✓
- E6 DurabilityHandle + WaitUntilDurable — Task 1 ✓
- E7 Per-sub eval-error vs fatal — Task 7 ✓
- AUDIT_HANDOFF.md update — Task 8 ✓
- Handoff doc — Task 8 ✓
- Verification — Task 9 ✓

Placeholder scan: no "TBD", "fill in", or "similar to Task N" without content. All replacement text written out.

Type consistency: `PostCommitMeta` used consistently in Task 2 (SPEC-004 §10.1) and Task 2 (SPEC-003 §8 cross-ref). `ClientSender` method-set consistent between Task 4 step 1 (3 methods) and Task 4 step 3 grep expectation. `FanOutSender` method-set consistent (3 methods everywhere).
