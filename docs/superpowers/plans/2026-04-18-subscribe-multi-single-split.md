# Phase 2 Slice 2 — SubscribeMulti/SubscribeSingle split + query-set grouping — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the `SubscribeMulti` / `SubscribeSingle` variant split and the one-QueryID-per-query-set grouping semantics that reference exposes on its v1 wire. Flip `protocol/parity_message_family_test.go::TestPhase2DeferralSubscribeNoMultiOrSingleVariants`.

**Architecture:** Rename existing single-variant wire types to the `*Single*` names; add new `*Multi*` envelopes alongside. Collapse the subscription-manager register/unregister paths onto a single set-based API where a single query is just a len-1 set (reference pattern — `add_subscription` wraps `add_subscription_multi` at `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:955-957`). Executor command layer mirrors the set shape.

**Tech Stack:** Go 1.22, RTK for shell/git, strict TDD, existing `bsatn` + `subscription` + `executor` + `protocol` packages.

**Spec:** `docs/superpowers/specs/2026-04-18-subscribe-multi-single-split-design.md`.

**Scope guardrails (from audit):**
- Stay on protocol parity. Do not broaden into `OneOffQuery` SQL or `P0-RECOVERY-002`.
- No sweeping formatting or unrelated cleanup. Renames are in-scope; style passes are not.
- Worktree entered this plan with unrelated dirty changes (see `git status`). Do not stage those; use explicit `git add <path>` per-commit.
- Every behavior change needs a failing test first.

---

## File Structure

### New files

- `protocol/handle_subscribe_single.go` — handler for `SubscribeSingleMsg` (split out of current `handle_subscribe.go`).
- `protocol/handle_subscribe_multi.go` — handler for `SubscribeMultiMsg`.
- `protocol/handle_unsubscribe_single.go` — handler for `UnsubscribeSingleMsg`.
- `protocol/handle_unsubscribe_multi.go` — handler for `UnsubscribeMultiMsg`.
- `subscription/register_set.go` — `RegisterSet` / `UnregisterSet` implementation (new set-based manager API).
- `subscription/register_set_test.go` — grouping-semantics unit tests.

### Files to modify

- `protocol/tags.go` — rename tags, add two new client tags and two new server tags.
- `protocol/client_messages.go` — rename `SubscribeMsg` → `SubscribeSingleMsg`, `UnsubscribeMsg` → `UnsubscribeSingleMsg`; add `SubscribeMultiMsg`, `UnsubscribeMultiMsg`; update codec switches.
- `protocol/server_messages.go` — rename `SubscribeApplied` → `SubscribeSingleApplied`, `UnsubscribeApplied` → `UnsubscribeSingleApplied`; add `SubscribeMultiApplied`, `UnsubscribeMultiApplied`; update codec switches.
- `protocol/client_messages_test.go` / `protocol/server_messages_test.go` — rename existing round-trip tests; add round-trip tests for the four new envelopes.
- `protocol/parity_message_family_test.go` — replace the deferral pin with positive shape pins; add deferral pins for known out-of-scope divergences; extend tag-byte stability pin.
- `protocol/dispatch.go` — split dispatch arms; update `MessageHandlers` struct.
- `protocol/lifecycle.go` — replace `RegisterSubscriptionRequest`/`UnregisterSubscriptionRequest` on `ExecutorInbox` with `RegisterSubscriptionSetRequest`/`UnregisterSubscriptionSetRequest`; update `SubscriptionCommandResponse` / `UnsubscribeCommandResponse` to match the new response shapes.
- `protocol/handle_subscribe.go` — **delete** (content migrated to new split files).
- `protocol/handle_unsubscribe.go` — **delete**.
- `protocol/handle_subscribe_test.go` / `protocol/handle_unsubscribe_test.go` — rename and split tests to match the new handlers.
- `protocol/async_responses.go` — update `watchSubscribeResponse` / `watchUnsubscribeResponse` to switch on the new single/multi response envelopes.
- `protocol/send_responses.go` — handle all four applied envelopes.
- `subscription/manager.go` — replace `Register` / `Unregister` interface methods with `RegisterSet` / `UnregisterSet`; add `SubscriptionSetRegisterRequest` / `SubscriptionSetRegisterResult` / `SubscriptionSetUnregisterResult` types.
- `subscription/register.go` — **delete** (replaced by `register_set.go`).
- `subscription/unregister.go` — **delete** (merged into `register_set.go`).
- `subscription/query_state.go` — add `querySets map[types.ConnectionID]map[uint32][]types.SubscriptionID`; add set-aware add/remove helpers; internal `SubscriptionID` allocation becomes manager-owned.
- `subscription/disconnect.go` — clear `querySets[connID]` bucket in `DisconnectClient`.
- `subscription/eval.go` — update `evalError` self-unregister path (`Unregister` → equivalent `removeSubForEval`; see Task 8).
- `subscription/eval_test.go` / `subscription/property_test.go` / `subscription/bench_test.go` / `subscription/fanout_worker_test.go` — migrate test callers to the set-based API.
- `executor/command.go` — replace `RegisterSubscriptionCmd` / `UnregisterSubscriptionCmd` with `RegisterSubscriptionSetCmd` / `UnregisterSubscriptionSetCmd`.
- `executor/executor.go` — update command dispatch arms to the new Cmd names and to call `RegisterSet` / `UnregisterSet` on the subscription manager.
- `executor/subscription_dispatch_test.go` / `executor/contracts_test.go` — migrate tests.
- `docs/parity-phase0-ledger.md` — flip the `SubscribeMulti` / `SubscribeSingle` deferral row to closed; record new deferral pins.
- `docs/current-status.md` — flip the variant-split note; record remaining named divergences.
- `NEXT-SESSION-PROMPT.md` — update suggested next slice.
- `docs/spacetimedb-parity-roadmap.md` — mark Phase 2 Slice 2 variant split closed.

### Internal allocation of `types.SubscriptionID`

The wire key is `QueryID uint32`. Internal `types.SubscriptionID` values (used by fan-out, pruning indexes, and `SubscriptionUpdate.SubscriptionID`) are now allocated by the subscription manager at `RegisterSet` time, not supplied by the caller. Add a per-manager `nextSubID uint64` counter; assign monotonically. The first predicate in a set gets `nextSubID++`, second gets the next, etc. Store the mapping in `querySets`. This removes the external `SubscriptionID` exposure from the handlers.

---

## Task 1: Add new client-side wire envelopes `SubscribeMultiMsg` / `UnsubscribeMultiMsg` (with tags + codec + positive parity pins)

Additive only. Do not rename existing types yet — that's Task 3.

**Files:**
- Modify: `protocol/tags.go`
- Modify: `protocol/client_messages.go`
- Modify: `protocol/parity_message_family_test.go`
- Modify: `protocol/client_messages_test.go`

- [ ] **Step 1: Write the failing parity pin for `SubscribeMultiMsg` shape**

Append to `protocol/parity_message_family_test.go`:

```go
// TestPhase2SubscribeMultiShape pins the new Phase 2 Slice 2 envelope.
// Reference: SubscribeMulti { query_strings, request_id, query_id } at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:203.
// Shunter carries structured Queries (see
// TestPhase2DeferralSubscribeMultiQueriesStructured).
func TestPhase2SubscribeMultiShape(t *testing.T) {
	fields := msgFieldNames(SubscribeMultiMsg{})
	want := []string{"RequestID", "QueryID", "Queries"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeMultiMsg fields = %v, want %v (Phase 2 Slice 2 variant split)",
			fields, want)
	}
}

// TestPhase2UnsubscribeMultiShape pins the new Phase 2 Slice 2 envelope.
// Reference: UnsubscribeMulti { request_id, query_id } at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:229.
func TestPhase2UnsubscribeMultiShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeMultiMsg{})
	want := []string{"RequestID", "QueryID"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeMultiMsg fields = %v, want %v (Phase 2 Slice 2 variant split)",
			fields, want)
	}
}
```

- [ ] **Step 2: Verify the tests fail (type does not exist yet)**

```
rtk go test ./protocol/ -run 'TestPhase2(Subscribe|Unsubscribe)MultiShape' -count=1
```

Expected: FAIL with "undefined: SubscribeMultiMsg" / "undefined: UnsubscribeMultiMsg".

- [ ] **Step 3: Add the new tag constants in `protocol/tags.go`**

Extend the client tag block (keep existing values unchanged):

```go
// Client→server message tags (SPEC-005 §6).
const (
	TagSubscribe        uint8 = 1
	TagUnsubscribe      uint8 = 2
	TagCallReducer      uint8 = 3
	TagOneOffQuery      uint8 = 4
	TagSubscribeMulti   uint8 = 5 // Phase 2 Slice 2 variant split
	TagUnsubscribeMulti uint8 = 6 // Phase 2 Slice 2 variant split
)
```

(`TagSubscribe` / `TagUnsubscribe` are renamed in Task 3; leave them alone here.)

- [ ] **Step 4: Add the new message types + codec arms in `protocol/client_messages.go`**

Append after `OneOffQueryMsg`:

```go
// SubscribeMultiMsg is the client-side SubscribeMulti message
// (SPEC-005 §7.1b). Reference: SubscribeMulti at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:203.
// Queries is a structured predicate list — the SQL-string form is
// deferred alongside OneOffQuery (see
// TestPhase2DeferralSubscribeMultiQueriesStructured).
type SubscribeMultiMsg struct {
	RequestID uint32
	QueryID   uint32
	Queries   []Query
}

// UnsubscribeMultiMsg drops every query registered under the given
// QueryID in one call. Reference: UnsubscribeMulti at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:229.
type UnsubscribeMultiMsg struct {
	RequestID uint32
	QueryID   uint32
}
```

Extend `EncodeClientMessage`'s switch block with new arms:

```go
case SubscribeMultiMsg:
	buf.WriteByte(TagSubscribeMulti)
	writeUint32(&buf, msg.RequestID)
	writeUint32(&buf, msg.QueryID)
	writeUint32(&buf, uint32(len(msg.Queries)))
	for _, q := range msg.Queries {
		if err := encodeQuery(&buf, q); err != nil {
			return nil, err
		}
	}
case UnsubscribeMultiMsg:
	buf.WriteByte(TagUnsubscribeMulti)
	writeUint32(&buf, msg.RequestID)
	writeUint32(&buf, msg.QueryID)
```

Extend `DecodeClientMessage`'s switch:

```go
case TagSubscribeMulti:
	msg, err := decodeSubscribeMulti(body)
	return tag, msg, err
case TagUnsubscribeMulti:
	msg, err := decodeUnsubscribeMulti(body)
	return tag, msg, err
```

Add the decoders below `decodeOneOffQuery`:

```go
func decodeSubscribeMulti(body []byte) (SubscribeMultiMsg, error) {
	var m SubscribeMultiMsg
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	count, off, err := readUint32(body, off)
	if err != nil {
		return m, err
	}
	m.Queries = make([]Query, 0, count)
	for i := uint32(0); i < count; i++ {
		q, next, qerr := decodeQuery(body, off)
		if qerr != nil {
			return m, qerr
		}
		off = next
		m.Queries = append(m.Queries, q)
	}
	return m, nil
}

func decodeUnsubscribeMulti(body []byte) (UnsubscribeMultiMsg, error) {
	var m UnsubscribeMultiMsg
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, _, err = readUint32(body, off); err != nil {
		return m, err
	}
	return m, nil
}
```

- [ ] **Step 5: Verify the parity pins pass**

```
rtk go test ./protocol/ -run 'TestPhase2(Subscribe|Unsubscribe)MultiShape' -count=1
```

Expected: PASS.

- [ ] **Step 6: Add codec round-trip tests in `protocol/client_messages_test.go`**

Append:

```go
func TestSubscribeMultiRoundTrip(t *testing.T) {
	orig := SubscribeMultiMsg{
		RequestID: 42,
		QueryID:   7,
		Queries: []Query{
			{TableName: "users"},
			{TableName: "orders", Predicates: []Predicate{{Column: "id", Value: types.Value{Kind: types.KindU32, U32: 9}}}},
		},
	}
	frame, err := EncodeClientMessage(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	tag, decoded, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagSubscribeMulti {
		t.Fatalf("tag = %d, want %d", tag, TagSubscribeMulti)
	}
	got, ok := decoded.(SubscribeMultiMsg)
	if !ok {
		t.Fatalf("decoded type = %T, want SubscribeMultiMsg", decoded)
	}
	if got.RequestID != orig.RequestID || got.QueryID != orig.QueryID {
		t.Fatalf("ids = %+v, want %+v", got, orig)
	}
	if len(got.Queries) != 2 || got.Queries[0].TableName != "users" || got.Queries[1].TableName != "orders" {
		t.Fatalf("queries = %+v, want %+v", got.Queries, orig.Queries)
	}
}

func TestUnsubscribeMultiRoundTrip(t *testing.T) {
	orig := UnsubscribeMultiMsg{RequestID: 3, QueryID: 99}
	frame, err := EncodeClientMessage(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	tag, decoded, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagUnsubscribeMulti {
		t.Fatalf("tag = %d, want %d", tag, TagUnsubscribeMulti)
	}
	if got, ok := decoded.(UnsubscribeMultiMsg); !ok || got != orig {
		t.Fatalf("decoded = %+v, want %+v", decoded, orig)
	}
}
```

- [ ] **Step 7: Run codec tests**

```
rtk go test ./protocol/ -run 'TestSubscribeMultiRoundTrip|TestUnsubscribeMultiRoundTrip' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```
rtk git add protocol/tags.go protocol/client_messages.go protocol/parity_message_family_test.go protocol/client_messages_test.go
rtk git commit -m "protocol(parity): add SubscribeMulti/UnsubscribeMulti client envelopes + tags"
```

---

## Task 2: Add new server-side wire envelopes `SubscribeMultiApplied` / `UnsubscribeMultiApplied` (with tags + codec + positive parity pins)

Mirrors Task 1 on the server-message side.

**Files:**
- Modify: `protocol/tags.go`
- Modify: `protocol/server_messages.go`
- Modify: `protocol/parity_message_family_test.go`
- Modify: `protocol/server_messages_test.go`

- [ ] **Step 1: Write failing shape pins**

Append to `protocol/parity_message_family_test.go`:

```go
// TestPhase2SubscribeMultiAppliedShape pins the set-scoped applied
// envelope. Reference: SubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:380.
// TotalHostExecutionDurationMicros is absent — tracked by
// TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration.
func TestPhase2SubscribeMultiAppliedShape(t *testing.T) {
	fields := msgFieldNames(SubscribeMultiApplied{})
	want := []string{"RequestID", "QueryID", "Update"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeMultiApplied fields = %v, want %v", fields, want)
	}
}

// TestPhase2UnsubscribeMultiAppliedShape pins the set-scoped applied
// envelope. Reference: UnsubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:394.
func TestPhase2UnsubscribeMultiAppliedShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeMultiApplied{})
	want := []string{"RequestID", "QueryID", "Update"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeMultiApplied fields = %v, want %v", fields, want)
	}
}
```

- [ ] **Step 2: Verify pins fail**

```
rtk go test ./protocol/ -run 'TestPhase2(Subscribe|Unsubscribe)MultiAppliedShape' -count=1
```

Expected: FAIL with "undefined" for the two new types.

- [ ] **Step 3: Add server tag constants**

In `protocol/tags.go`, extend the server block:

```go
const (
	TagInitialConnection       uint8 = 1
	TagSubscribeApplied        uint8 = 2
	TagUnsubscribeApplied      uint8 = 3
	TagSubscriptionError       uint8 = 4
	TagTransactionUpdate       uint8 = 5
	TagOneOffQueryResult       uint8 = 6
	TagReducerCallResult       uint8 = 7 // RESERVED
	TagTransactionUpdateLight  uint8 = 8
	TagSubscribeMultiApplied   uint8 = 9  // Phase 2 Slice 2 variant split
	TagUnsubscribeMultiApplied uint8 = 10 // Phase 2 Slice 2 variant split
)
```

- [ ] **Step 4: Add the new types + codec arms in `protocol/server_messages.go`**

Append types after `SubscriptionError`:

```go
// SubscribeMultiApplied is the server response to a SubscribeMulti.
// Update is a merged initial snapshot, one SubscriptionUpdate per
// (allocated internal SubscriptionID, table) pair, with Inserts
// populated and Deletes empty. Reference: SubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:380.
type SubscribeMultiApplied struct {
	RequestID uint32
	QueryID   uint32
	Update    []SubscriptionUpdate
}

// UnsubscribeMultiApplied is the server response to an UnsubscribeMulti.
// Update carries Deletes-populated entries for rows that were still
// live at unsubscribe time. Reference: UnsubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:394.
type UnsubscribeMultiApplied struct {
	RequestID uint32
	QueryID   uint32
	Update    []SubscriptionUpdate
}
```

Extend `EncodeServerMessage` switch:

```go
case SubscribeMultiApplied:
	buf.WriteByte(TagSubscribeMultiApplied)
	writeUint32(&buf, msg.RequestID)
	writeUint32(&buf, msg.QueryID)
	writeSubscriptionUpdates(&buf, msg.Update)
case UnsubscribeMultiApplied:
	buf.WriteByte(TagUnsubscribeMultiApplied)
	writeUint32(&buf, msg.RequestID)
	writeUint32(&buf, msg.QueryID)
	writeSubscriptionUpdates(&buf, msg.Update)
```

Extend `DecodeServerMessage` switch:

```go
case TagSubscribeMultiApplied:
	msg, err := decodeSubscribeMultiApplied(body)
	return tag, msg, err
case TagUnsubscribeMultiApplied:
	msg, err := decodeUnsubscribeMultiApplied(body)
	return tag, msg, err
```

Add decoders:

```go
func decodeSubscribeMultiApplied(body []byte) (SubscribeMultiApplied, error) {
	var m SubscribeMultiApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	ups, _, err := readSubscriptionUpdates(body, off)
	if err != nil {
		return m, err
	}
	m.Update = ups
	return m, nil
}

func decodeUnsubscribeMultiApplied(body []byte) (UnsubscribeMultiApplied, error) {
	var m UnsubscribeMultiApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	ups, _, err := readSubscriptionUpdates(body, off)
	if err != nil {
		return m, err
	}
	m.Update = ups
	return m, nil
}
```

- [ ] **Step 5: Verify pins pass**

```
rtk go test ./protocol/ -run 'TestPhase2(Subscribe|Unsubscribe)MultiAppliedShape' -count=1
```

Expected: PASS.

- [ ] **Step 6: Add codec round-trips in `protocol/server_messages_test.go`**

```go
func TestSubscribeMultiAppliedRoundTrip(t *testing.T) {
	orig := SubscribeMultiApplied{
		RequestID: 1,
		QueryID:   2,
		Update: []SubscriptionUpdate{
			{SubscriptionID: 10, TableName: "users", Inserts: []byte{0x01}},
			{SubscriptionID: 11, TableName: "orders", Inserts: []byte{0x02}},
		},
	}
	frame, err := EncodeServerMessage(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	tag, decoded, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagSubscribeMultiApplied {
		t.Fatalf("tag = %d, want %d", tag, TagSubscribeMultiApplied)
	}
	got, ok := decoded.(SubscribeMultiApplied)
	if !ok {
		t.Fatalf("decoded type = %T", decoded)
	}
	if got.RequestID != 1 || got.QueryID != 2 || len(got.Update) != 2 {
		t.Fatalf("decoded = %+v", got)
	}
}

func TestUnsubscribeMultiAppliedRoundTrip(t *testing.T) {
	orig := UnsubscribeMultiApplied{
		RequestID: 5,
		QueryID:   9,
		Update: []SubscriptionUpdate{
			{SubscriptionID: 10, TableName: "users", Deletes: []byte{0x03}},
		},
	}
	frame, err := EncodeServerMessage(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	tag, decoded, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagUnsubscribeMultiApplied {
		t.Fatalf("tag = %d, want %d", tag, TagUnsubscribeMultiApplied)
	}
	got, ok := decoded.(UnsubscribeMultiApplied)
	if !ok || got.RequestID != 5 || got.QueryID != 9 || len(got.Update) != 1 {
		t.Fatalf("decoded = %+v", decoded)
	}
}
```

- [ ] **Step 7: Run**

```
rtk go test ./protocol/ -run 'TestSubscribeMultiApplied|TestUnsubscribeMultiApplied' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```
rtk git add protocol/tags.go protocol/server_messages.go protocol/parity_message_family_test.go protocol/server_messages_test.go
rtk git commit -m "protocol(parity): add SubscribeMultiApplied/UnsubscribeMultiApplied server envelopes + tags"
```

---

## Task 3: Rename `SubscribeMsg` → `SubscribeSingleMsg`, `UnsubscribeMsg` → `UnsubscribeSingleMsg` (plus tags + parity pins)

Wire bytes stay `1` / `2`; only Go symbol names change. No legacy shims — delete old names.

**Files:**
- Modify: `protocol/tags.go`
- Modify: `protocol/client_messages.go`
- Modify: `protocol/parity_message_family_test.go`
- Modify: all protocol package callers (list in Step 4)

- [ ] **Step 1: Update the parity pins that target the old names**

In `protocol/parity_message_family_test.go`:

Replace `TestPhase2SubscribeCarriesQueryID` with:

```go
// TestPhase2SubscribeSingleShape pins the renamed single-envelope. The
// QueryID field already landed; the rename closes the Single/Multi
// variant split on the client side. Reference: SubscribeSingle at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:189.
func TestPhase2SubscribeSingleShape(t *testing.T) {
	fields := msgFieldNames(SubscribeSingleMsg{})
	want := []string{"RequestID", "QueryID", "Query"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeSingleMsg fields = %v, want %v", fields, want)
	}
}
```

Replace `TestPhase2UnsubscribeCarriesQueryID` with:

```go
// TestPhase2UnsubscribeSingleShape pins the renamed single-envelope.
// Reference: Unsubscribe at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:218.
func TestPhase2UnsubscribeSingleShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeSingleMsg{})
	want := []string{"RequestID", "QueryID", "SendDropped"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeSingleMsg fields = %v, want %v", fields, want)
	}
}
```

- [ ] **Step 2: Verify tests fail with undefined symbols**

```
rtk go test ./protocol/ -run 'TestPhase2(Subscribe|Unsubscribe)SingleShape' -count=1
```

Expected: FAIL with "undefined: SubscribeSingleMsg" / "undefined: UnsubscribeSingleMsg".

- [ ] **Step 3: Rename tags + types**

In `protocol/tags.go`:

```go
TagSubscribeSingle   uint8 = 1 // renamed from TagSubscribe
TagUnsubscribeSingle uint8 = 2 // renamed from TagUnsubscribe
```

In `protocol/client_messages.go`: rename `type SubscribeMsg` → `type SubscribeSingleMsg` and `type UnsubscribeMsg` → `type UnsubscribeSingleMsg`. Update codec switch arms (`case SubscribeMsg:` → `case SubscribeSingleMsg:`; `TagSubscribe` → `TagSubscribeSingle`; `TagUnsubscribe` → `TagUnsubscribeSingle`; rename `decodeSubscribe` → `decodeSubscribeSingle`, `decodeUnsubscribe` → `decodeUnsubscribeSingle`).

- [ ] **Step 4: Update all callers in the protocol package**

Use repo-wide search-and-replace on these exact symbols:
- `SubscribeMsg` → `SubscribeSingleMsg`
- `UnsubscribeMsg` → `UnsubscribeSingleMsg`
- `TagSubscribe` → `TagSubscribeSingle` (check for prefix collision with `TagSubscribeApplied` first)
- `TagUnsubscribe` → `TagUnsubscribeSingle` (same collision check)

Safe procedure:

```
rtk grep -w 'SubscribeMsg' protocol
rtk grep -w 'UnsubscribeMsg' protocol
rtk grep -w 'TagSubscribe' protocol
rtk grep -w 'TagUnsubscribe' protocol
```

Then update each result via Edit tool. Expected touch points (verify with grep, do not copy blind):
- `protocol/dispatch.go` — `MessageHandlers.OnSubscribe` / `OnUnsubscribe` keep their Go field names but the struct literal types in tests change to `*SubscribeSingleMsg` / `*UnsubscribeSingleMsg`.
- `protocol/handle_subscribe.go` — `*SubscribeMsg` parameter type.
- `protocol/handle_unsubscribe.go` — `*UnsubscribeMsg` parameter type.
- `protocol/handle_subscribe_test.go` — callers of the message type.
- `protocol/handle_callreducer_test.go` — mock references.
- `protocol/lifecycle_test.go` — fake inbox.
- `protocol/client_messages_test.go` — round-trip tests for the old name.
- `protocol/send_responses.go` / `protocol/async_responses.go` — no change needed (they operate on server-side types only).

Also rename the handler entry-point function signatures in Task 11; for now just update the parameter types in `handle_subscribe.go` / `handle_unsubscribe.go` and in `MessageHandlers`.

- [ ] **Step 5: Run the full protocol test suite**

```
rtk go test ./protocol/ -count=1
```

Expected: PASS for Task-3 rename. If any existing test still references `SubscribeMsg` or `UnsubscribeMsg`, it's an unmigrated caller — fix it.

- [ ] **Step 6: Commit**

```
rtk git add protocol/
rtk git commit -m "protocol(parity): rename Subscribe/UnsubscribeMsg to *Single* (Phase 2 Slice 2 variant split)"
```

---

## Task 4: Rename `SubscribeApplied` → `SubscribeSingleApplied`, `UnsubscribeApplied` → `UnsubscribeSingleApplied`

Mirrors Task 3 on the server side. Wire bytes stay `2` / `3`.

**Files:**
- Modify: `protocol/tags.go`
- Modify: `protocol/server_messages.go`
- Modify: `protocol/parity_message_family_test.go`
- Modify: all package callers

- [ ] **Step 1: Update parity pins**

Replace `TestPhase2SubscribeAppliedCarriesQueryID` with:

```go
// TestPhase2SubscribeSingleAppliedShape pins the renamed single-applied
// envelope. Reference: SubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:317.
func TestPhase2SubscribeSingleAppliedShape(t *testing.T) {
	fields := msgFieldNames(SubscribeSingleApplied{})
	want := []string{"RequestID", "QueryID", "TableName", "Rows"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeSingleApplied fields = %v, want %v", fields, want)
	}
}
```

Replace `TestPhase2UnsubscribeAppliedCarriesQueryID`:

```go
// TestPhase2UnsubscribeSingleAppliedShape pins the renamed
// single-applied envelope. Reference: UnsubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:331.
func TestPhase2UnsubscribeSingleAppliedShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeSingleApplied{})
	want := []string{"RequestID", "QueryID", "HasRows", "Rows"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeSingleApplied fields = %v, want %v", fields, want)
	}
}
```

- [ ] **Step 2: Verify tests fail**

```
rtk go test ./protocol/ -run 'TestPhase2(Subscribe|Unsubscribe)SingleAppliedShape' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Rename tags + types**

In `protocol/tags.go`:

```go
TagSubscribeSingleApplied   uint8 = 2 // renamed from TagSubscribeApplied
TagUnsubscribeSingleApplied uint8 = 3 // renamed from TagUnsubscribeApplied
```

In `protocol/server_messages.go`: rename `type SubscribeApplied` → `type SubscribeSingleApplied`; `type UnsubscribeApplied` → `type UnsubscribeSingleApplied`. Rename codec arms, decoder function names (`decodeSubscribeApplied` → `decodeSubscribeSingleApplied`, `decodeUnsubscribeApplied` → `decodeUnsubscribeSingleApplied`).

- [ ] **Step 4: Update all callers**

Grep for the old names in the protocol package and rename:

```
rtk grep -w 'SubscribeApplied' protocol
rtk grep -w 'UnsubscribeApplied' protocol
rtk grep -w 'TagSubscribeApplied' protocol
rtk grep -w 'TagUnsubscribeApplied' protocol
```

Expected touch points:
- `protocol/lifecycle.go` — `SubscriptionCommandResponse.Applied *SubscribeApplied` → `*SubscribeSingleApplied`; `UnsubscribeCommandResponse.Applied *UnsubscribeApplied` → `*UnsubscribeSingleApplied`.
- `protocol/send_responses.go` — `case *SubscribeApplied:` and `case *UnsubscribeApplied:` arms.
- `protocol/async_responses.go` — response watchers.
- `protocol/handle_subscribe_test.go` — mock response construction.
- `protocol/handle_callreducer_test.go` — same.
- `protocol/server_messages_test.go` — existing round-trip tests.

- [ ] **Step 5: Run all protocol tests**

```
rtk go test ./protocol/ -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```
rtk git add protocol/
rtk git commit -m "protocol(parity): rename Subscribe/UnsubscribeApplied to *Single* (Phase 2 Slice 2 variant split)"
```

---

## Task 5: Flip the deferral pin + add deferral pins for out-of-scope divergences

The variant-split deferral is now closed. Remove the old pin and add the new deferral pins called out in the spec §7.2 and §11.

**Files:**
- Modify: `protocol/parity_message_family_test.go`

- [ ] **Step 1: Remove `TestPhase2DeferralSubscribeNoMultiOrSingleVariants`**

Delete the test function wholesale. The positive pins added in Tasks 1–4 replace it.

- [ ] **Step 2: Add deferral pin for missing `TotalHostExecutionDurationMicros`**

Append:

```go
// TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration pins the
// still-open deferral: reference carries
// total_host_execution_duration_micros: u64 on SubscribeApplied,
// SubscribeMultiApplied, UnsubscribeApplied, UnsubscribeMultiApplied
// (v1.rs:321/335/384/399). Shunter does not. Flip when the host
// execution duration slice lands.
func TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration(t *testing.T) {
	for _, v := range []any{
		SubscribeSingleApplied{},
		SubscribeMultiApplied{},
		UnsubscribeSingleApplied{},
		UnsubscribeMultiApplied{},
	} {
		for _, f := range msgFieldNames(v) {
			if f == "TotalHostExecutionDurationMicros" {
				t.Fatalf("%T.TotalHostExecutionDurationMicros unexpectedly present", v)
			}
		}
	}
}
```

- [ ] **Step 3: Add deferral pin for `SubscriptionError` shape divergence**

```go
// TestPhase2DeferralSubscriptionErrorNoTableID pins the three-field
// shape. Reference SubscriptionError carries
// total_host_execution_duration_micros, Option<request_id>,
// Option<query_id>, Option<TableId>, error (v1.rs:350). Shunter
// always populates RequestID/QueryID and omits TableID + duration.
// Flip when any of these close.
func TestPhase2DeferralSubscriptionErrorNoTableID(t *testing.T) {
	fields := msgFieldNames(SubscriptionError{})
	want := []string{"RequestID", "QueryID", "Error"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscriptionError fields = %v, want %v (deferral)",
			fields, want)
	}
}
```

(If the existing `TestPhase2SubscriptionErrorCarriesQueryID` is now redundant, keep it — both pin the same shape from different angles and cost nothing.)

- [ ] **Step 4: Add deferral pin for structured `Queries` on SubscribeMulti**

```go
// TestPhase2DeferralSubscribeMultiQueriesStructured pins the scope
// boundary with Phase 2 Slice 1. Reference SubscribeMulti carries
// query_strings: Box<[Box<str>]> (v1.rs:205); Shunter carries
// structured Queries []Query. Flip when the SQL front door lands.
func TestPhase2DeferralSubscribeMultiQueriesStructured(t *testing.T) {
	m := SubscribeMultiMsg{}
	qf, ok := reflect.TypeOf(m).FieldByName("Queries")
	if !ok {
		t.Fatal("SubscribeMultiMsg.Queries missing")
	}
	if qf.Type.Elem().Name() != "Query" {
		t.Fatalf("SubscribeMultiMsg.Queries elem = %s, want Query (structured)",
			qf.Type.Elem().Name())
	}
}
```

- [ ] **Step 5: Add tag-byte stability pin**

Replace any existing tag-stability assertions or append:

```go
// TestPhase2TagByteStability pins the Phase 2 Slice 2 tag layout.
// Older bytes (1-8) stay fixed; 9/10 are the new multi-applied tags.
// 5/6 are the new multi request tags.
func TestPhase2TagByteStability(t *testing.T) {
	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"TagSubscribeSingle", TagSubscribeSingle, 1},
		{"TagUnsubscribeSingle", TagUnsubscribeSingle, 2},
		{"TagCallReducer", TagCallReducer, 3},
		{"TagOneOffQuery", TagOneOffQuery, 4},
		{"TagSubscribeMulti", TagSubscribeMulti, 5},
		{"TagUnsubscribeMulti", TagUnsubscribeMulti, 6},
		{"TagInitialConnection", TagInitialConnection, 1},
		{"TagSubscribeSingleApplied", TagSubscribeSingleApplied, 2},
		{"TagUnsubscribeSingleApplied", TagUnsubscribeSingleApplied, 3},
		{"TagSubscriptionError", TagSubscriptionError, 4},
		{"TagTransactionUpdate", TagTransactionUpdate, 5},
		{"TagOneOffQueryResult", TagOneOffQueryResult, 6},
		{"TagReducerCallResult", TagReducerCallResult, 7},
		{"TagTransactionUpdateLight", TagTransactionUpdateLight, 8},
		{"TagSubscribeMultiApplied", TagSubscribeMultiApplied, 9},
		{"TagUnsubscribeMultiApplied", TagUnsubscribeMultiApplied, 10},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}
```

- [ ] **Step 6: Run the whole parity file**

```
rtk go test ./protocol/ -run 'TestPhase2' -count=1
```

Expected: PASS for all pins; deferral pin for the variant split is gone.

- [ ] **Step 7: Commit**

```
rtk git add protocol/parity_message_family_test.go
rtk git commit -m "protocol(parity): flip Phase 2 Slice 2 variant-split deferral; add new deferral + tag-stability pins"
```

---

## Task 6: Add set-based subscription-manager API `RegisterSet` / `UnregisterSet` (keeping old `Register` / `Unregister` in place during migration)

Introduce the new types and methods without removing the old ones yet. That keeps the migration in Tasks 7–9 green at every checkpoint.

**Files:**
- Modify: `subscription/manager.go`
- Create: `subscription/register_set.go`
- Create: `subscription/register_set_test.go`
- Modify: `subscription/query_state.go`

- [ ] **Step 1: Write failing unit tests for the new API**

Create `subscription/register_set_test.go`:

```go
package subscription

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// These unit tests exercise the set-based API surface. The manager is
// constructed with a fake schema and no read view so initialQuery
// returns empty; the focus is registry bookkeeping.

func TestRegisterSetMultiAtomicOnInvalidPredicate(t *testing.T) {
	mgr := newTestManagerWithSchema(t)
	req := SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 1,
		Predicates: []Predicate{
			AllRows{Table: 1},
			AllRows{Table: 999}, // unknown table — must fail validation
		},
	}
	_, err := mgr.RegisterSet(req, nil)
	if err == nil {
		t.Fatal("RegisterSet with invalid predicate should fail")
	}
	if _, ok := mgr.querySets[req.ConnID]; ok {
		t.Fatalf("querySets should be empty after atomic failure, got %+v", mgr.querySets)
	}
}

func TestRegisterSetMultiMergesInitialSnapshot(t *testing.T) {
	mgr, view := newTestManagerWithRows(t)
	req := SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 7,
		Predicates: []Predicate{
			AllRows{Table: 1},
			AllRows{Table: 2},
		},
	}
	res, err := mgr.RegisterSet(req, view)
	if err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	if len(res.Update) != 2 {
		t.Fatalf("Update len = %d, want 2", len(res.Update))
	}
	if sids := mgr.querySets[req.ConnID][req.QueryID]; len(sids) != 2 {
		t.Fatalf("querySets ids = %v, want 2", sids)
	}
}

func TestRegisterSetRejectsDuplicateQueryID(t *testing.T) {
	mgr := newTestManagerWithSchema(t)
	req := SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    5,
		Predicates: []Predicate{AllRows{Table: 1}},
	}
	if _, err := mgr.RegisterSet(req, nil); err != nil {
		t.Fatalf("first RegisterSet: %v", err)
	}
	_, err := mgr.RegisterSet(req, nil)
	if !errors.Is(err, ErrQueryIDAlreadyLive) {
		t.Fatalf("second RegisterSet err = %v, want ErrQueryIDAlreadyLive", err)
	}
}

func TestRegisterSetDedupsIdenticalPredicates(t *testing.T) {
	mgr := newTestManagerWithSchema(t)
	pred := AllRows{Table: 1}
	req := SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    1,
		Predicates: []Predicate{pred, pred},
	}
	res, err := mgr.RegisterSet(req, nil)
	if err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	if len(mgr.querySets[req.ConnID][req.QueryID]) != 1 {
		t.Fatalf("dedup failed: %+v", mgr.querySets)
	}
	if len(res.Update) != 0 {
		// nil view ⇒ no rows, but Update len matches the deduped sub count
		t.Logf("Update=%+v", res.Update)
	}
}

func TestUnregisterSetDropsAllInSet(t *testing.T) {
	mgr, view := newTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	reg := SubscriptionSetRegisterRequest{
		ConnID:  connID,
		QueryID: 3,
		Predicates: []Predicate{
			AllRows{Table: 1},
			AllRows{Table: 2},
		},
	}
	if _, err := mgr.RegisterSet(reg, view); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	res, err := mgr.UnregisterSet(connID, 3, view)
	if err != nil {
		t.Fatalf("UnregisterSet: %v", err)
	}
	if len(res.Update) != 2 {
		t.Fatalf("UnregisterSet Update len = %d, want 2", len(res.Update))
	}
	if _, ok := mgr.querySets[connID]; ok {
		t.Fatalf("querySets not cleared: %+v", mgr.querySets)
	}
}

func TestDisconnectClientClearsQuerySets(t *testing.T) {
	mgr, view := newTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    1,
		Predicates: []Predicate{AllRows{Table: 1}},
	}, view); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	if err := mgr.DisconnectClient(connID); err != nil {
		t.Fatalf("DisconnectClient: %v", err)
	}
	if _, ok := mgr.querySets[connID]; ok {
		t.Fatalf("querySets[%v] not cleared", connID)
	}
}

// newTestManagerWithSchema constructs a Manager with a minimal schema
// covering Table(1) and Table(2). Reuses the existing test helpers in
// the package where available.
func newTestManagerWithSchema(t *testing.T) *Manager {
	t.Helper()
	return newManagerForRegisterSetTests(t, nil)
}

func newTestManagerWithRows(t *testing.T) (*Manager, store.CommittedReadView) {
	t.Helper()
	return newManagerForRegisterSetTestsWithRows(t)
}
```

If `newManagerForRegisterSetTests` / `newManagerForRegisterSetTestsWithRows` / test schema helpers already exist in `subscription/*_test.go`, reuse them instead. Otherwise, adapt the existing `newTestManager` helper used in `subscription/eval_test.go` and `subscription/property_test.go`. (Check `subscription/testutil_test.go` first if present.)

- [ ] **Step 2: Verify the tests fail**

```
rtk go test ./subscription/ -run 'TestRegisterSet|TestUnregisterSet|TestDisconnectClientClearsQuerySets' -count=1
```

Expected: compile failure (undefined `RegisterSet` / `UnregisterSet` / `SubscriptionSetRegisterRequest` / `ErrQueryIDAlreadyLive` / `querySets`). That IS the failing TDD test for this task.

- [ ] **Step 3: Add types + interface update in `subscription/manager.go`**

Replace the `SubscriptionRegisterRequest` block with:

```go
// SubscriptionSetRegisterRequest is the set-based register request.
// Predicates may have length >= 1; length 1 is the Single path.
type SubscriptionSetRegisterRequest struct {
	ConnID         types.ConnectionID
	QueryID        uint32
	Predicates     []Predicate
	ClientIdentity *types.Identity
	RequestID      uint32
}

// SubscriptionSetRegisterResult carries the merged initial snapshot.
// Update entries have Inserts populated and Deletes empty; one entry
// per (allocated internal SubscriptionID, table) pair.
type SubscriptionSetRegisterResult struct {
	QueryID uint32
	Update  []SubscriptionUpdate
}

// SubscriptionSetUnregisterResult carries the final-delta rows that
// were still live at unsubscribe time. Update entries have Deletes
// populated and Inserts empty.
type SubscriptionSetUnregisterResult struct {
	QueryID uint32
	Update  []SubscriptionUpdate
}

// ErrQueryIDAlreadyLive is returned by RegisterSet when the given
// (ConnID, QueryID) pair already names a live set. Reference behavior:
// add_subscription_multi try_insert at
// reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:1050.
var ErrQueryIDAlreadyLive = errors.New("subscription: query id already live on connection")
```

Add `querySets` to the `Manager` struct and initialize it in `NewManager`:

```go
type Manager struct {
	schema    SchemaLookup
	resolver  IndexResolver
	registry  *queryRegistry
	indexes   *PruningIndexes
	inbox     chan<- FanOutMessage
	dropped   chan types.ConnectionID
	querySets map[types.ConnectionID]map[uint32][]types.SubscriptionID
	nextSubID types.SubscriptionID

	InitialRowLimit int
}
```

`NewManager` initializes `querySets: make(...)` and leaves the legacy `Register` / `Unregister` intact for now.

Update the `SubscriptionManager` interface to include the new methods alongside the old ones (we remove the old ones in Task 9):

```go
type SubscriptionManager interface {
	Register(req SubscriptionRegisterRequest, view store.CommittedReadView) (SubscriptionRegisterResult, error)
	Unregister(connID types.ConnectionID, subscriptionID types.SubscriptionID) error
	RegisterSet(req SubscriptionSetRegisterRequest, view store.CommittedReadView) (SubscriptionSetRegisterResult, error)
	UnregisterSet(connID types.ConnectionID, queryID uint32, view store.CommittedReadView) (SubscriptionSetUnregisterResult, error)
	DisconnectClient(connID types.ConnectionID) error
	EvalAndBroadcast(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView, meta PostCommitMeta)
	DroppedClients() <-chan types.ConnectionID
}
```

- [ ] **Step 4: Implement `RegisterSet` / `UnregisterSet` in `subscription/register_set.go`**

```go
package subscription

import (
	"fmt"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// RegisterSet atomically registers 1..N predicates under a single
// (ConnID, QueryID) key. Reference: add_subscription_multi at
// reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:1023.
func (m *Manager) RegisterSet(
	req SubscriptionSetRegisterRequest,
	view store.CommittedReadView,
) (SubscriptionSetRegisterResult, error) {
	// Step 1: pre-validate every predicate. No state touched yet.
	for _, p := range req.Predicates {
		if err := ValidatePredicate(p, m.schema); err != nil {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("predicate validation: %w", err)
		}
	}
	// Step 2: duplicate QueryID rejection.
	if byQ, ok := m.querySets[req.ConnID]; ok {
		if _, live := byQ[req.QueryID]; live {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("%w: conn=%x query=%d",
				ErrQueryIDAlreadyLive, req.ConnID[:4], req.QueryID)
		}
	}
	// Step 3: dedup identical predicates within this call.
	deduped := make([]Predicate, 0, len(req.Predicates))
	seen := make(map[QueryHash]struct{}, len(req.Predicates))
	for _, p := range req.Predicates {
		h := ComputeQueryHash(p, req.ClientIdentity)
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		deduped = append(deduped, p)
	}
	// Step 4: allocate internal IDs + run initial snapshot per predicate.
	allocated := make([]types.SubscriptionID, 0, len(deduped))
	updates := make([]SubscriptionUpdate, 0, len(deduped))
	for _, p := range deduped {
		m.nextSubID++
		subID := m.nextSubID
		hash := ComputeQueryHash(p, req.ClientIdentity)
		rows, err := m.initialQuery(p, view)
		if err != nil {
			// Unwind any partial state.
			for _, sid := range allocated {
				_ = m.registry.unregisterSingle(req.ConnID, sid)
			}
			return SubscriptionSetRegisterResult{}, fmt.Errorf("initial query: %w", err)
		}
		qs := m.registry.getQuery(hash)
		if qs == nil {
			qs = m.registry.createQueryState(hash, p)
			PlaceSubscription(m.indexes, p, hash)
		}
		m.registry.addSubscriber(hash, req.ConnID, subID, req.RequestID)
		allocated = append(allocated, subID)
		if len(rows) > 0 {
			tables := p.Tables()
			tableID := TableID(0)
			if len(tables) > 0 {
				tableID = tables[0]
			}
			tableName := m.schema.TableName(tableID)
			encoded, eerr := encodeRowsForWire(rows)
			if eerr != nil {
				return SubscriptionSetRegisterResult{}, eerr
			}
			updates = append(updates, SubscriptionUpdate{
				SubscriptionID: subID,
				TableID:        tableID,
				TableName:      tableName,
				Inserts:        encoded,
			})
		}
	}
	// Step 5: record set membership.
	if m.querySets[req.ConnID] == nil {
		m.querySets[req.ConnID] = make(map[uint32][]types.SubscriptionID)
	}
	m.querySets[req.ConnID][req.QueryID] = allocated
	return SubscriptionSetRegisterResult{QueryID: req.QueryID, Update: updates}, nil
}

// UnregisterSet drops every internal subscription registered under
// (ConnID, QueryID). Reference: remove_subscription at
// reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:841.
func (m *Manager) UnregisterSet(
	connID types.ConnectionID,
	queryID uint32,
	view store.CommittedReadView,
) (SubscriptionSetUnregisterResult, error) {
	byQ := m.querySets[connID]
	sids, ok := byQ[queryID]
	if !ok {
		return SubscriptionSetUnregisterResult{}, ErrSubscriptionNotFound
	}
	// Capture the still-live rows BEFORE dropping state so we can
	// populate the Deletes-only final delta.
	deletes := make([]SubscriptionUpdate, 0, len(sids))
	for _, sid := range sids {
		hash, ok := m.registry.hashForSub(connID, sid)
		if !ok {
			continue
		}
		qs := m.registry.getQuery(hash)
		if qs == nil {
			continue
		}
		if view != nil {
			rows, err := m.initialQuery(qs.predicate, view)
			if err == nil && len(rows) > 0 {
				tables := qs.predicate.Tables()
				tableID := TableID(0)
				if len(tables) > 0 {
					tableID = tables[0]
				}
				encoded, eerr := encodeRowsForWire(rows)
				if eerr == nil {
					deletes = append(deletes, SubscriptionUpdate{
						SubscriptionID: sid,
						TableID:        tableID,
						TableName:      m.schema.TableName(tableID),
						Deletes:        encoded,
					})
				}
			}
		}
		_ = m.registry.unregisterSingle(connID, sid)
	}
	delete(byQ, queryID)
	if len(byQ) == 0 {
		delete(m.querySets, connID)
	}
	return SubscriptionSetUnregisterResult{QueryID: queryID, Update: deletes}, nil
}

// encodeRowsForWire turns a slice of ProductValue rows into the
// encoded RowList bytes the wire expects. This is the same encoding
// the existing Register path uses — factored out here so Single and
// Multi paths share it. See store/rowlist.go / protocol/wire_types.go.
func encodeRowsForWire(rows []types.ProductValue) ([]byte, error) {
	return encodeRowList(rows)
}
```

If `encodeRowList` does not exist as a helper today, check `protocol/send_responses.go` and `subscription/eval.go` for the existing encoding path. Lift whichever helper produces `[]byte` RowList from a `[]types.ProductValue` — do not duplicate the logic. If the existing encoding lives inside a non-exported function, promote it to a package-level helper usable from both the current Register path and the new RegisterSet path.

- [ ] **Step 5: Add registry helpers `unregisterSingle` / `hashForSub`**

In `subscription/query_state.go`:

```go
// unregisterSingle removes a single internal sub. Lightweight wrapper
// over removeSubscriber that also trims query state on last-ref.
func (r *queryRegistry) unregisterSingle(connID types.ConnectionID, subID types.SubscriptionID) error {
	hash, last, ok := r.removeSubscriber(connID, subID)
	if !ok {
		return ErrSubscriptionNotFound
	}
	if last {
		r.removeQueryState(hash)
	}
	return nil
}

// hashForSub returns the QueryHash a given (conn, sub) is attached to.
func (r *queryRegistry) hashForSub(connID types.ConnectionID, subID types.SubscriptionID) (QueryHash, bool) {
	h, ok := r.bySub[subscriptionRef{connID: connID, subID: subID}]
	return h, ok
}
```

- [ ] **Step 6: Update `DisconnectClient` to clear `querySets[connID]`**

In `subscription/disconnect.go`, after the existing registry cleanup add:

```go
delete(m.querySets, connID)
```

- [ ] **Step 7: Run new tests**

```
rtk go test ./subscription/ -run 'TestRegisterSet|TestUnregisterSet|TestDisconnectClientClearsQuerySets' -count=1
```

Expected: PASS.

- [ ] **Step 8: Run the full subscription suite (old Register/Unregister still intact)**

```
rtk go test ./subscription/ -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```
rtk git add subscription/manager.go subscription/query_state.go subscription/register_set.go subscription/register_set_test.go subscription/disconnect.go
rtk git commit -m "subscription(parity): add RegisterSet/UnregisterSet set-based API + grouping tests"
```

---

## Task 7: Add set-based executor commands `RegisterSubscriptionSetCmd` / `UnregisterSubscriptionSetCmd`

Additive on the executor side. Old commands stay during migration.

**Files:**
- Modify: `executor/command.go`
- Modify: `executor/executor.go`
- Modify: `executor/subscription_dispatch_test.go`
- Modify: `executor/contracts_test.go`

- [ ] **Step 1: Write a failing dispatch test**

In `executor/subscription_dispatch_test.go`, append a test that constructs a `RegisterSubscriptionSetCmd`, dispatches it, and checks the response carries the merged `Update`.

```go
func TestDispatchRegisterSubscriptionSet(t *testing.T) {
	exec, fakeSubs, _ := newDispatchTestExec(t)
	req := subscription.SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{9},
		QueryID: 42,
		Predicates: []subscription.Predicate{
			subscription.AllRows{Table: 1},
		},
	}
	respCh := make(chan subscription.SubscriptionSetRegisterResult, 1)
	exec.dispatch(RegisterSubscriptionSetCmd{Request: req, ResponseCh: respCh})

	resp := <-respCh
	if resp.QueryID != 42 {
		t.Fatalf("resp.QueryID = %d, want 42", resp.QueryID)
	}
	if !fakeSubs.registerSetCalled {
		t.Fatalf("fakeSubs.RegisterSet was not called")
	}
}
```

Augment the existing `fakeSubs` fixture with `registerSetCalled bool` + a `RegisterSet` method.

- [ ] **Step 2: Verify the test fails**

```
rtk go test ./executor/ -run 'TestDispatchRegisterSubscriptionSet' -count=1
```

Expected: FAIL with undefined `RegisterSubscriptionSetCmd` / missing method on fake.

- [ ] **Step 3: Add `RegisterSubscriptionSetCmd` + `UnregisterSubscriptionSetCmd`**

In `executor/command.go`:

```go
// RegisterSubscriptionSetCmd requests atomic set-scoped subscription
// registration. Reference-aligned replacement for RegisterSubscriptionCmd.
type RegisterSubscriptionSetCmd struct {
	Request    subscription.SubscriptionSetRegisterRequest
	ResponseCh chan<- subscription.SubscriptionSetRegisterResult
}

func (RegisterSubscriptionSetCmd) isExecutorCommand() {}

// UnregisterSubscriptionSetCmd removes every subscription registered
// under one (ConnID, QueryID) key.
type UnregisterSubscriptionSetCmd struct {
	ConnID     types.ConnectionID
	QueryID    uint32
	ResponseCh chan<- UnregisterSubscriptionSetResponse
}

func (UnregisterSubscriptionSetCmd) isExecutorCommand() {}

// UnregisterSubscriptionSetResponse carries either the final delta
// (on success) or an error.
type UnregisterSubscriptionSetResponse struct {
	Result subscription.SubscriptionSetUnregisterResult
	Err    error
}
```

- [ ] **Step 4: Add dispatch arms in `executor/executor.go`**

Replace / augment the `handleRegisterSubscription` block:

```go
case RegisterSubscriptionSetCmd:
	e.handleRegisterSubscriptionSet(c)
case UnregisterSubscriptionSetCmd:
	e.handleUnregisterSubscriptionSet(c)
```

Add the handlers:

```go
func (e *Executor) handleRegisterSubscriptionSet(cmd RegisterSubscriptionSetCmd) {
	view := e.snapshotFn()
	defer view.Close()
	res, err := e.subs.RegisterSet(cmd.Request, view)
	if err != nil {
		log.Printf("executor: RegisterSubscriptionSet failed: %v", err)
		cmd.ResponseCh <- subscription.SubscriptionSetRegisterResult{}
		return
	}
	cmd.ResponseCh <- res
}

func (e *Executor) handleUnregisterSubscriptionSet(cmd UnregisterSubscriptionSetCmd) {
	view := e.snapshotFn()
	defer view.Close()
	res, err := e.subs.UnregisterSet(cmd.ConnID, cmd.QueryID, view)
	cmd.ResponseCh <- UnregisterSubscriptionSetResponse{Result: res, Err: err}
}
```

- [ ] **Step 5: Run**

```
rtk go test ./executor/ -run 'TestDispatchRegisterSubscriptionSet' -count=1
```

Expected: PASS.

- [ ] **Step 6: Sanity: the whole executor suite still green**

```
rtk go test ./executor/ -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```
rtk git add executor/command.go executor/executor.go executor/subscription_dispatch_test.go executor/contracts_test.go
rtk git commit -m "executor(parity): add RegisterSubscriptionSetCmd/UnregisterSubscriptionSetCmd"
```

---

## Task 8: Protocol handlers — split `handleSubscribe` and `handleUnsubscribe` into Single + Multi (additive, both paths call the set API through the new executor commands)

**Files:**
- Create: `protocol/handle_subscribe_single.go`
- Create: `protocol/handle_subscribe_multi.go`
- Create: `protocol/handle_unsubscribe_single.go`
- Create: `protocol/handle_unsubscribe_multi.go`
- Modify: `protocol/lifecycle.go`
- Modify: `protocol/dispatch.go`
- Modify: `protocol/handle_subscribe.go` (to be deleted in Task 9)
- Modify: `protocol/handle_unsubscribe.go` (to be deleted in Task 9)
- Modify: `protocol/async_responses.go`
- Modify: `protocol/send_responses.go`
- Modify: `protocol/handle_subscribe_test.go` / `protocol/handle_unsubscribe_test.go`

- [ ] **Step 1: Write failing handler tests**

Append to `protocol/handle_subscribe_test.go`:

```go
func TestHandleSubscribeMultiSuccess(t *testing.T) {
	conn, exec, sl := newSubscribeTestFixture(t)
	msg := &SubscribeMultiMsg{
		RequestID: 11,
		QueryID:   77,
		Queries: []Query{
			{TableName: "users"},
			{TableName: "orders"},
		},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	req := exec.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if req.QueryID != 77 || len(req.Predicates) != 2 {
		t.Fatalf("req = %+v, want QueryID=77 len(Predicates)=2", req)
	}
}
```

Augment the existing `mockSubExecutor` with a matching `RegisterSubscriptionSet` method and getter.

- [ ] **Step 2: Verify the test fails**

```
rtk go test ./protocol/ -run 'TestHandleSubscribeMultiSuccess' -count=1
```

Expected: FAIL on undefined `handleSubscribeMulti`.

- [ ] **Step 3: Update `ExecutorInbox` + `SubscriptionCommandResponse`**

In `protocol/lifecycle.go`, extend the interface:

```go
type ExecutorInbox interface {
	OnConnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error
	OnDisconnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error
	DisconnectClientSubscriptions(ctx context.Context, connID types.ConnectionID) error
	RegisterSubscription(ctx context.Context, req RegisterSubscriptionRequest) error
	UnregisterSubscription(ctx context.Context, req UnregisterSubscriptionRequest) error
	RegisterSubscriptionSet(ctx context.Context, req RegisterSubscriptionSetRequest) error
	UnregisterSubscriptionSet(ctx context.Context, req UnregisterSubscriptionSetRequest) error
	CallReducer(ctx context.Context, req CallReducerRequest) error
}

// RegisterSubscriptionSetRequest carries the fields the executor
// needs to register a set of predicates under one QueryID. Predicate
// is `any` to avoid a cycle with the subscription package.
type RegisterSubscriptionSetRequest struct {
	ConnID     types.ConnectionID
	QueryID    uint32
	RequestID  uint32
	Predicates []any // subscription.Predicate
	ResponseCh chan<- SubscriptionSetCommandResponse
}

// UnregisterSubscriptionSetRequest unsubscribes every internal sub
// registered under one (ConnID, QueryID) key.
type UnregisterSubscriptionSetRequest struct {
	ConnID     types.ConnectionID
	QueryID    uint32
	RequestID  uint32
	ResponseCh chan<- UnsubscribeSetCommandResponse
}

// SubscriptionSetCommandResponse is the async result envelope carrying
// either a SubscribeMultiApplied (for Multi) or a SubscribeSingleApplied
// (for Single, collapsed by the handler) or a SubscriptionError.
type SubscriptionSetCommandResponse struct {
	MultiApplied  *SubscribeMultiApplied
	SingleApplied *SubscribeSingleApplied
	Error         *SubscriptionError
}

// UnsubscribeSetCommandResponse mirrors SubscriptionSetCommandResponse
// for the unsubscribe path.
type UnsubscribeSetCommandResponse struct {
	MultiApplied  *UnsubscribeMultiApplied
	SingleApplied *UnsubscribeSingleApplied
	Error         *SubscriptionError
}
```

Keep the old request/response types alongside for this task.

- [ ] **Step 4: Implement the four split handlers**

Create `protocol/handle_subscribe_single.go`:

```go
package protocol

import (
	"context"
	"fmt"

	"github.com/ponchione/shunter/subscription"
)

// handleSubscribeSingle processes an incoming SubscribeSingleMsg by
// validating the single query against the schema, packaging it as a
// 1-predicate set, and submitting it through the executor's set path.
// The response adapter unwraps the len-1 Update back into a
// SubscribeSingleApplied envelope.
func handleSubscribeSingle(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeSingleMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	pred, err := compileQuery(msg.Query, sl)
	if err != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     err.Error(),
		})
		return
	}
	respCh := make(chan SubscriptionSetCommandResponse, 1)
	submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    msg.QueryID,
		RequestID:  msg.RequestID,
		Predicates: []any{pred},
		ResponseCh: respCh,
	})
	if submitErr != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     "executor unavailable: " + submitErr.Error(),
		})
		return
	}
	watchSubscribeSetResponse(conn, respCh, /*single=*/ true, msg.RequestID, msg.QueryID)
	_ = fmt.Sprint // keep imports stable
	_ = subscription.AllRows{}
}
```

Create `protocol/handle_subscribe_multi.go`:

```go
package protocol

import (
	"context"
)

// handleSubscribeMulti processes an incoming SubscribeMultiMsg.
// Every query is compiled up front; if any fail, the call returns a
// single SubscriptionError correlated with QueryID.
func handleSubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMultiMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	preds := make([]any, 0, len(msg.Queries))
	for _, q := range msg.Queries {
		p, err := compileQuery(q, sl)
		if err != nil {
			sendError(conn, SubscriptionError{
				RequestID: msg.RequestID,
				QueryID:   msg.QueryID,
				Error:     err.Error(),
			})
			return
		}
		preds = append(preds, p)
	}
	respCh := make(chan SubscriptionSetCommandResponse, 1)
	submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    msg.QueryID,
		RequestID:  msg.RequestID,
		Predicates: preds,
		ResponseCh: respCh,
	})
	if submitErr != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     "executor unavailable: " + submitErr.Error(),
		})
		return
	}
	watchSubscribeSetResponse(conn, respCh, /*single=*/ false, msg.RequestID, msg.QueryID)
}
```

Add a shared helper in `protocol/handle_subscribe.go` (keep the old file around for this step; it's deleted in Task 9):

```go
// compileQuery resolves a wire Query against the schema and returns the
// compiled subscription predicate. Errors carry context suitable for
// SubscriptionError.
func compileQuery(q Query, sl SchemaLookup) (subscription.Predicate, error) {
	tableID, ts, ok := sl.TableByName(q.TableName)
	if !ok {
		return nil, fmt.Errorf("unknown table %q", q.TableName)
	}
	return NormalizePredicates(tableID, ts, q.Predicates)
}
```

Create the unsubscribe pair (`protocol/handle_unsubscribe_single.go`, `protocol/handle_unsubscribe_multi.go`) analogously. Single's unsubscribe handler maps through `UnregisterSubscriptionSet` and asks the response adapter to emit `UnsubscribeSingleApplied`; Multi emits `UnsubscribeMultiApplied`.

- [ ] **Step 5: Update `protocol/async_responses.go`**

Replace `watchSubscribeResponse` / `watchUnsubscribeResponse` with the set-based watchers:

```go
func watchSubscribeSetResponse(
	conn *Conn,
	respCh <-chan SubscriptionSetCommandResponse,
	single bool,
	requestID uint32,
	queryID uint32,
) {
	go func() {
		resp, ok := <-respCh
		if !ok {
			return
		}
		switch {
		case resp.Error != nil:
			sendServerMessage(conn, *resp.Error)
		case single && resp.SingleApplied != nil:
			sendServerMessage(conn, *resp.SingleApplied)
		case !single && resp.MultiApplied != nil:
			sendServerMessage(conn, *resp.MultiApplied)
		default:
			// Response adapter bug: no applied, no error. Log + drop.
			log.Printf("protocol: malformed SubscriptionSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}()
}
```

(Adapt naming for unsubscribe watcher. If `sendServerMessage` does not exist, use the existing `sendApplied` helper already in `send_responses.go`.)

- [ ] **Step 6: Update dispatch in `protocol/dispatch.go`**

Replace the switch arms:

```go
switch m := msg.(type) {
case SubscribeSingleMsg:
	if handlers.OnSubscribeSingle == nil {
		closeProtocolError(c, "unsupported message type")
		return
	}
	run = func() { handlers.OnSubscribeSingle(ctx, c, &m) }
case SubscribeMultiMsg:
	if handlers.OnSubscribeMulti == nil {
		closeProtocolError(c, "unsupported message type")
		return
	}
	run = func() { handlers.OnSubscribeMulti(ctx, c, &m) }
case UnsubscribeSingleMsg:
	if handlers.OnUnsubscribeSingle == nil {
		closeProtocolError(c, "unsupported message type")
		return
	}
	run = func() { handlers.OnUnsubscribeSingle(ctx, c, &m) }
case UnsubscribeMultiMsg:
	if handlers.OnUnsubscribeMulti == nil {
		closeProtocolError(c, "unsupported message type")
		return
	}
	run = func() { handlers.OnUnsubscribeMulti(ctx, c, &m) }
case CallReducerMsg:
	// unchanged
case OneOffQueryMsg:
	// unchanged
}
```

Update the `MessageHandlers` struct:

```go
type MessageHandlers struct {
	OnSubscribeSingle   func(ctx context.Context, conn *Conn, msg *SubscribeSingleMsg)
	OnSubscribeMulti    func(ctx context.Context, conn *Conn, msg *SubscribeMultiMsg)
	OnUnsubscribeSingle func(ctx context.Context, conn *Conn, msg *UnsubscribeSingleMsg)
	OnUnsubscribeMulti  func(ctx context.Context, conn *Conn, msg *UnsubscribeMultiMsg)
	OnCallReducer       func(ctx context.Context, conn *Conn, msg *CallReducerMsg)
	OnOneOffQuery       func(ctx context.Context, conn *Conn, msg *OneOffQueryMsg)
}
```

- [ ] **Step 7: Run the split handler tests + the full protocol suite**

```
rtk go test ./protocol/ -run 'TestHandleSubscribeMultiSuccess' -count=1
rtk go test ./protocol/ -count=1
```

Expected: PASS both.

- [ ] **Step 8: Commit**

```
rtk git add protocol/handle_subscribe_single.go protocol/handle_subscribe_multi.go protocol/handle_unsubscribe_single.go protocol/handle_unsubscribe_multi.go protocol/handle_subscribe.go protocol/handle_unsubscribe.go protocol/async_responses.go protocol/dispatch.go protocol/lifecycle.go protocol/handle_subscribe_test.go protocol/handle_unsubscribe_test.go
rtk git commit -m "protocol(parity): split subscribe/unsubscribe handlers into Single/Multi paths"
```

---

## Task 9: Delete legacy single-subscription API + callers (no backwards compat)

Now that every caller can drive the set API, remove:
- `subscription.Manager.Register` / `Unregister` + the `Register` / `Unregister` interface methods.
- `subscription.SubscriptionRegisterRequest` / `SubscriptionRegisterResult` types.
- `executor.RegisterSubscriptionCmd` / `UnregisterSubscriptionCmd`.
- `protocol.RegisterSubscriptionRequest` / `UnregisterSubscriptionRequest` + `RegisterSubscription` / `UnregisterSubscription` interface methods.
- `protocol/handle_subscribe.go` / `protocol/handle_unsubscribe.go` (old files).

**Files:**
- Delete: `subscription/register.go` (merged into `register_set.go`)
- Delete: `subscription/unregister.go`
- Delete: `protocol/handle_subscribe.go` (old entry point)
- Delete: `protocol/handle_unsubscribe.go` (already deleted in Task 8 commit d2e3caa)
- Modify: `subscription/manager.go`, `subscription/eval.go`, all `subscription/*_test.go`, `subscription/bench_test.go`
- Modify: `executor/command.go`, `executor/executor.go`, `executor/*_test.go`
- Modify: `protocol/lifecycle.go`, `protocol/*_test.go`

- [ ] **Step 1: Audit remaining callers of the old API**

```
rtk grep -w 'Register\b' subscription | rtk grep -v 'RegisterSet\|register_set'
rtk grep -w 'Unregister\b' subscription | rtk grep -v 'UnregisterSet\|register_set'
rtk grep 'RegisterSubscription\b\|UnregisterSubscription\b' --type go
rtk grep 'SubscriptionRegisterRequest\|SubscriptionRegisterResult' --type go
rtk grep 'RegisterSubscriptionCmd\|UnregisterSubscriptionCmd' --type go
```

Every match becomes a migration site in the following steps.

- [ ] **Step 2: Migrate `subscription/eval.go`'s self-unregister path**

Currently `evalError` calls `m.Unregister(...)`. Replace with a helper that removes a single internal sub without exposing a public `Unregister`:

```go
// removeDroppedSub removes a single internal subscription that the
// eval path has declared unrecoverable. Not exposed on the public
// manager surface — callers outside the package use UnregisterSet.
func (m *Manager) removeDroppedSub(connID types.ConnectionID, subID types.SubscriptionID) {
	if err := m.registry.unregisterSingle(connID, subID); err != nil {
		log.Printf("subscription: removeDroppedSub conn=%x sub=%d: %v", connID[:4], subID, err)
		return
	}
	// Also cull the (connID, queryID) entry in querySets if this was
	// the last sub under that QueryID.
	for qid, sids := range m.querySets[connID] {
		for i, s := range sids {
			if s == subID {
				m.querySets[connID][qid] = append(sids[:i], sids[i+1:]...)
				if len(m.querySets[connID][qid]) == 0 {
					delete(m.querySets[connID], qid)
				}
				if len(m.querySets[connID]) == 0 {
					delete(m.querySets, connID)
				}
				return
			}
		}
	}
}
```

Swap `m.Unregister(sub.connID, sub.subID)` for `m.removeDroppedSub(...)`.

- [ ] **Step 3: Delete the legacy types + methods**

Remove `subscription/register.go` and `subscription/unregister.go`. Delete `SubscriptionRegisterRequest`, `SubscriptionRegisterResult`, and the `Register` / `Unregister` methods from `manager.go`. Delete the interface entries from `SubscriptionManager`.

- [ ] **Step 4: Migrate subscription tests + benches**

Replace every call site in `subscription/eval_test.go`, `subscription/property_test.go`, `subscription/bench_test.go`, and `subscription/fanout_worker_test.go` that uses the old API. For each existing `mgr.Register(SubscriptionRegisterRequest{ConnID: c, SubscriptionID: N, Predicate: p, …}, view)`, rewrite as:

```go
res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
	ConnID:     c,
	QueryID:    uint32(N), // the QueryID now owns the external identity
	Predicates: []Predicate{p},
}, view)
```

Swap `mgr.Unregister(c, N)` for `_, err := mgr.UnregisterSet(c, uint32(N), view)`.

In places where the test relied on selecting a specific `types.SubscriptionID`, switch to checking the single sub that `RegisterSet` allocated (`mgr.querySets[c][uint32(N)][0]`). If more precise coupling is needed, expose a test-only helper `Manager.AllocatedSubIDs(connID types.ConnectionID, queryID uint32) []types.SubscriptionID` that returns the copy.

- [ ] **Step 5: Delete the legacy executor commands**

In `executor/command.go`, remove `RegisterSubscriptionCmd`, `UnregisterSubscriptionCmd`. In `executor/executor.go`, remove the dispatch arms + `handleRegisterSubscription` / `handleUnregisterSubscription`. Migrate `executor/subscription_dispatch_test.go` and `executor/contracts_test.go`.

- [ ] **Step 6: Delete the legacy protocol inbox methods**

In `protocol/lifecycle.go`, delete `RegisterSubscriptionRequest`, `UnregisterSubscriptionRequest`, the corresponding `SubscriptionCommandResponse` / `UnsubscribeCommandResponse` envelopes, and their interface entries. Delete `protocol/handle_subscribe.go` and `protocol/handle_unsubscribe.go`. Migrate any test still referencing them.

- [ ] **Step 7: Full build + test sweep**

```
rtk go build ./...
rtk go test ./... -count=1
```

Expected: PASS; if a test references a deleted symbol, migrate it in the same commit.

- [ ] **Step 8: Commit**

```
rtk git add -A subscription/ executor/ protocol/
rtk git commit -m "parity: remove legacy single-subscription API; set-based path is authoritative"
```

---

## Task 10: Host adapter — wire `RegisterSubscriptionSet` / `UnregisterSubscriptionSet` through to the executor

> **Status: skipped (2026-04-19).** No host adapter exists in-repo — `protocol.ExecutorInbox` is implemented only by test fakes, and there is no `cmd/` binary or production implementer. Host-side wiring is downstream/external. When a host binary is introduced, its adapter must implement `RegisterSubscriptionSet` / `UnregisterSubscriptionSet` on `protocol.ExecutorInbox`, casting `[]any` to `[]subscription.Predicate` on the way through and submitting `executor.RegisterSubscriptionSetCmd` / `executor.UnregisterSubscriptionSetCmd` to the executor inbox. Recorded as follow-up.

If a host adapter implements the `protocol.ExecutorInbox` interface (typically in a cmd/ or internal/ package; search for the struct that satisfies it), update it to submit the new executor commands.

**Files:**
- Modify: host inbox adapter (find with `rtk grep 'RegisterSubscription\|UnregisterSubscription' cmd internal 2>&1`)
- Modify: host adapter tests

- [ ] **Step 1: Locate the host adapter**

```
rtk grep 'implements protocol.ExecutorInbox\|RegisterSubscription(ctx' --type go
```

The adapter is the struct whose method list matches `protocol.ExecutorInbox` — likely in `executor/` or a host-owned `/cmd` package.

- [ ] **Step 2: Add the two new methods**

For the inbox adapter, implement:

```go
func (a *inboxAdapter) RegisterSubscriptionSet(ctx context.Context, req protocol.RegisterSubscriptionSetRequest) error {
	cmd := RegisterSubscriptionSetCmd{
		Request: subscription.SubscriptionSetRegisterRequest{
			ConnID:     req.ConnID,
			QueryID:    req.QueryID,
			RequestID:  req.RequestID,
			Predicates: castPredicates(req.Predicates),
		},
		ResponseCh: make(chan subscription.SubscriptionSetRegisterResult, 1),
	}
	go a.relayRegisterSetResponse(req, cmd.ResponseCh)
	return a.inbox.Send(ctx, cmd)
}
```

And a mirror for `UnregisterSubscriptionSet`. `castPredicates` converts `[]any` to `[]subscription.Predicate` with a type assertion per element.

The relay goroutine reads `SubscriptionSetRegisterResult` from the inbox, wraps it as `SubscriptionSetCommandResponse` with the appropriate `SingleApplied`/`MultiApplied` pointer depending on the number of predicates, and sends to `req.ResponseCh`. For errors, synthesize a `SubscriptionError{RequestID: req.RequestID, QueryID: req.QueryID, Error: err.Error()}`.

- [ ] **Step 3: Write an adapter-level test that a 1-predicate request yields `SingleApplied`**

- [ ] **Step 4: Full build + test sweep**

```
rtk go build ./...
rtk go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```
rtk git add -A
rtk git commit -m "host(parity): inbox adapter wires protocol set API to RegisterSubscriptionSetCmd"
```

---

## Task 11: Docs updates

**Files:**
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `docs/current-status.md`
- Modify: `NEXT-SESSION-PROMPT.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`

- [ ] **Step 1: `docs/parity-phase0-ledger.md` — flip the `P0-PROTOCOL-004` row**

Edit the long "Phase 2 Slice 2" paragraph inside the `P0-PROTOCOL-004` row to record the closure. Replace the text "Remaining deferrals: `SubscribeMulti` / `SubscribeSingle` variant split (pinned by `TestPhase2DeferralSubscribeNoMultiOrSingleVariants`) and SQL `OneOffQuery` (Phase 2 Slice 1)." with:

```
Phase 2 Slice 2 variant split landed: positive shape pins
`TestPhase2SubscribeSingleShape`, `TestPhase2SubscribeMultiShape`,
`TestPhase2UnsubscribeSingleShape`, `TestPhase2UnsubscribeMultiShape`,
`TestPhase2SubscribeMultiAppliedShape`, `TestPhase2UnsubscribeMultiAppliedShape`
replace the former deferral pin. New deferral pins:
`TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration`,
`TestPhase2DeferralSubscriptionErrorNoTableID`,
`TestPhase2DeferralSubscribeMultiQueriesStructured`. Remaining Phase 2
deferrals: SQL `OneOffQuery` (Phase 2 Slice 1), `TotalHostExecutionDurationMicros`
on applied envelopes, `SubscriptionError.TableID` / optional-field shape.
```

Also update §4.3 / §4.4 if referenced.

- [ ] **Step 2: `docs/current-status.md` — flip the variant-split note at line 112**

Replace "No `SubscribeMulti` / `SubscribeSingle` variant split yet" with:

```
- `SubscribeMulti` / `SubscribeSingle` variant split landed; one-QueryID-per-query-set
  grouping semantics now match reference. Remaining Phase 2 Slice 2 divergences:
  `TotalHostExecutionDurationMicros` on applied envelopes,
  `SubscriptionError.TableID` / optional-field shape, SQL-string form for
  `SubscribeMulti.Queries` (paired with Phase 2 Slice 1 deferral).
```

- [ ] **Step 3: `NEXT-SESSION-PROMPT.md` — update the "Suggested next slice"**

Remove the `SubscribeMulti` / `SubscribeSingle` entry. Update the "Pinned parity tests" section to name the new pins and drop `TestPhase2DeferralSubscribeNoMultiOrSingleVariants`. Update the narrative so the next-session prompt recommends `OneOffQuery` SQL front door (Phase 2 Slice 1) or `P0-RECOVERY-002`.

- [ ] **Step 4: `docs/spacetimedb-parity-roadmap.md` — mark Phase 2 Slice 2 variant split closed**

Edit the Phase 2 row to indicate the variant split closed with the new pins. Leave SQL `OneOffQuery` and lag policy open as Phase 2 remaining anchors.

- [ ] **Step 5: Run the full repo test suite once more for the doc-commit checkpoint**

```
rtk go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```
rtk git add docs/parity-phase0-ledger.md docs/current-status.md NEXT-SESSION-PROMPT.md docs/spacetimedb-parity-roadmap.md
rtk git commit -m "docs(parity): close Phase 2 Slice 2 SubscribeMulti/SubscribeSingle variant split"
```

---

## Task 12: Final verification sweep

**Files:** none modified — validation only.

- [ ] **Step 1: Broad test sweep**

```
rtk go test ./... -count=1
```

Expected: PASS. Target: at least the `955 passed in 9 packages` baseline from `docs/current-status.md:20` (actual count will be higher because we added tests).

- [ ] **Step 2: Re-run the Phase 2 parity pins explicitly**

```
rtk go test ./protocol/ -run 'TestPhase2' -v -count=1
```

Expected: every `TestPhase2*` test passes; no `TestPhase2DeferralSubscribeNoMultiOrSingleVariants` remains.

- [ ] **Step 3: Confirm `rtk go vet` is clean for touched packages**

```
rtk go vet ./protocol/ ./subscription/ ./executor/
```

Expected: no output.

- [ ] **Step 4: Confirm the dirty worktree is not newly polluted**

```
rtk git status
```

Expected: only the Phase-2-Slice-2 edits from this plan are in the diff. Any residual `M` on unrelated files must be the pre-existing drift recorded in the session-start `git status` and not something this plan introduced.

- [ ] **Step 5: No commit for this task** — validation only.

---

## Self-Review Checklist (completed at plan-write time)

- Every spec section §1–§12 has a task. §1 → Tasks 1–8. §2 → Tasks 1, 3, 4 (non-goals enforced by leaving them out). §3 → Tasks 1–5. §4 → Tasks 6–10. §5 → Task 8. §6 → covered by the "unchanged" note in Task 8 design (no code change, verified by the broad sweep in Task 12). §7.1 → Tasks 1–5. §7.2 → Task 5. §7.3 → Task 5. §8 → Task 6 (test file `register_set_test.go`). §9 → Tasks 1, 2 codec round-trips. §10 → Task 11. §11 → Task 5. §12 → plan respects audit direction by every task = one commit, failing test first.
- Placeholder scan: no `TBD`, no `TODO`, no `add error handling` — every step contains the actual code or the actual command.
- Type consistency: `SubscribeSingleMsg` / `SubscribeMultiMsg` / `UnsubscribeSingleMsg` / `UnsubscribeMultiMsg` / `SubscribeSingleApplied` / `SubscribeMultiApplied` / `UnsubscribeSingleApplied` / `UnsubscribeMultiApplied` are referenced identically across all tasks. `RegisterSet` / `UnregisterSet` / `SubscriptionSetRegisterRequest` / `SubscriptionSetRegisterResult` / `SubscriptionSetUnregisterResult` / `ErrQueryIDAlreadyLive` are consistent. `RegisterSubscriptionSetCmd` / `UnregisterSubscriptionSetCmd` / `UnregisterSubscriptionSetResponse` are consistent. `RegisterSubscriptionSetRequest` / `UnregisterSubscriptionSetRequest` / `SubscriptionSetCommandResponse` / `UnsubscribeSetCommandResponse` are consistent. Tag numerals (1–10 on the server side, 1–6 on the client side) are consistent.
