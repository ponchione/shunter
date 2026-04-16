# E4 Client Message Dispatch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the stub read pump with a full client message dispatch loop and four typed handlers (Subscribe, Unsubscribe, CallReducer, OneOffQuery).

**Architecture:** Direct dispatch on the read goroutine — no intermediate channel. Handlers validate wire-format inputs and submit commands to the executor inbox. Response delivery to clients is E5's job; E4 only sends validation-error responses and OneOffQuery results directly. Protocol owns string→ID name resolution via a `SchemaLookup` interface.

**Tech Stack:** Go, coder/websocket, httptest (for WebSocket integration tests)

---

## File Map

| File | Responsibility | Task |
|---|---|---|
| `protocol/dispatch.go` | MessageHandlers struct, runDispatchLoop, sendError helper | 1 |
| `protocol/dispatch_test.go` | Dispatch loop tests via httptest WebSocket | 1 |
| `protocol/handle_subscribe.go` | SchemaLookup interface, NormalizePredicates, handleSubscribe | 2 |
| `protocol/handle_subscribe_test.go` | Subscribe handler unit tests with mocks | 2 |
| `protocol/handle_unsubscribe.go` | handleUnsubscribe | 3 |
| `protocol/handle_callreducer.go` | handleCallReducer | 3 |
| `protocol/handle_callreducer_test.go` | Unsubscribe + CallReducer handler tests | 3 |
| `protocol/handle_oneoff.go` | CommittedStateAccess interface, handleOneOffQuery | 4 |
| `protocol/handle_oneoff_test.go` | OneOffQuery handler tests | 4 |
| `protocol/conn.go` | Add `IsActive(id)` method to SubscriptionTracker | 3 |
| `protocol/lifecycle.go` | Extend ExecutorInbox with 3 new methods | 2 |
| `protocol/upgrade.go` | Wire runDispatchLoop into default upgrade handler | 5 |
| `protocol/disconnect.go` | Update superviseLifecycle param name | 5 |

## Dependencies Between Tasks

```
Task 1 (dispatch loop)
  ├── Task 2 (subscribe handler) — needs MessageHandlers
  ├── Task 3 (unsubscribe + callreducer) — needs MessageHandlers
  └── Task 4 (oneoff query) — needs MessageHandlers
Task 5 (wiring) — needs Tasks 1-4
```

Tasks 2, 3, 4 are independent of each other.

---

### Task 1: Dispatch Loop & MessageHandlers

**Files:**
- Create: `protocol/dispatch.go`
- Create: `protocol/dispatch_test.go`

#### Step 1.1: Write MessageHandlers struct and sendError helper

- [ ] **Create `protocol/dispatch.go` with types and helpers**

```go
package protocol

import (
	"context"
	"log"

	"github.com/coder/websocket"
)

// MessageHandlers holds the per-message-type handler functions wired by
// the host. A nil field means the message type is not supported on this
// connection — the dispatch loop closes with 1002 if it encounters one.
type MessageHandlers struct {
	OnSubscribe   func(ctx context.Context, conn *Conn, msg *SubscribeMsg)
	OnUnsubscribe func(ctx context.Context, conn *Conn, msg *UnsubscribeMsg)
	OnCallReducer func(ctx context.Context, conn *Conn, msg *CallReducerMsg)
	OnOneOffQuery func(ctx context.Context, conn *Conn, msg *OneOffQueryMsg)
}

// sendError encodes a server message, wraps it in the connection's
// compression envelope, and pushes it to the outbound queue. If encoding
// fails or the queue is full it logs and drops — the caller is already
// in an error path and cannot retry.
func sendError(conn *Conn, msg any) {
	frame, err := EncodeServerMessage(msg)
	if err != nil {
		log.Printf("protocol: sendError encode failed: %v", err)
		return
	}
	wrapped := EncodeFrame(frame[0], frame[1:], conn.Compression, CompressionNone)
	select {
	case conn.OutboundCh <- wrapped:
	default:
		log.Printf("protocol: sendError dropped (outbound full) for conn %x", conn.ID[:])
	}
}

// closeProtocolError sends a WebSocket close frame with status 1002
// (protocol error). Runs in a goroutine because coder/websocket.Close
// blocks on the close handshake.
func closeProtocolError(conn *Conn, reason string) {
	go func() {
		_ = conn.ws.Close(websocket.StatusProtocolError, truncateCloseReason(reason))
	}()
}
```

- [ ] **Verify it compiles**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 1.2: Write failing dispatch loop test — binary frame with valid tag dispatches

- [ ] **Create `protocol/dispatch_test.go` with first test**

```go
package protocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testConn creates a Conn with a test websocket via httptest. Returns
// the server-side Conn and a client-side *websocket.Conn for sending
// frames. The caller must close the client conn when done.
func testConnPair(t *testing.T, opts *ProtocolOptions) (*Conn, *websocket.Conn) {
	t.Helper()
	if opts == nil {
		o := DefaultProtocolOptions()
		opts = &o
	}
	var serverConn *websocket.Conn
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		serverConn = c
		close(ready)
		// Keep handler alive until test finishes.
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	client, _, err := websocket.Dial(ctx, srv.URL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { client.CloseNow() })

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server accept")
	}

	conn := NewConn(
		GenerateConnectionID(),
		[32]byte{1},
		"test-token",
		false, // no compression
		serverConn,
		opts,
	)
	return conn, client
}

func TestDispatchLoop_ValidSubscribe(t *testing.T) {
	conn, client := testConnPair(t, nil)

	var got *SubscribeMsg
	var mu sync.Mutex
	handlers := &MessageHandlers{
		OnSubscribe: func(ctx context.Context, c *Conn, msg *SubscribeMsg) {
			mu.Lock()
			got = msg
			mu.Unlock()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		conn.runDispatchLoop(ctx, handlers)
		close(done)
	}()

	// Send a valid Subscribe frame.
	frame, err := EncodeClientMessage(SubscribeMsg{
		RequestID:      1,
		SubscriptionID: 42,
		Query:          Query{TableName: "players"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Write(context.Background(), websocket.MessageBinary, frame); err != nil {
		t.Fatal(err)
	}

	// Give dispatch loop time to process.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("OnSubscribe was not called")
	}
	if got.SubscriptionID != 42 {
		t.Fatalf("SubscriptionID = %d, want 42", got.SubscriptionID)
	}
}
```

- [ ] **Run test to verify it fails**

Run: `rtk go test ./protocol/ -run TestDispatchLoop_ValidSubscribe -v`
Expected: FAIL — `conn.runDispatchLoop` undefined

#### Step 1.3: Implement runDispatchLoop

- [ ] **Add runDispatchLoop to `protocol/dispatch.go`**

```go
// runDispatchLoop replaces runReadPump (Story 3.5) with the full
// message-dispatching read loop (Epic 4, Story 4.1). Every successful
// read marks activity per SPEC-005 §5.4.
//
// Exit conditions: ctx cancelled, c.closed closed, or ws.Read error.
func (c *Conn) runDispatchLoop(ctx context.Context, handlers *MessageHandlers) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		default:
		}

		typ, frame, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		c.MarkActivity()

		// Reject text frames per SPEC-005 §3.2.
		if typ == websocket.MessageText {
			closeProtocolError(c, "text frames not supported")
			return
		}

		// Decompress if compression was negotiated.
		var tag uint8
		var body []byte
		if c.Compression {
			var unwrapErr error
			tag, body, unwrapErr = UnwrapCompressed(frame)
			if unwrapErr != nil {
				closeProtocolError(c, "malformed message")
				return
			}
			// Reconstruct frame as [tag][body] for DecodeClientMessage.
			reframed := make([]byte, 1+len(body))
			reframed[0] = tag
			copy(reframed[1:], body)
			frame = reframed
		}

		_, msg, decodeErr := DecodeClientMessage(frame)
		if decodeErr != nil {
			reason := "malformed message"
			if errors.Is(decodeErr, ErrUnknownMessageTag) {
				reason = "unknown message tag"
			}
			closeProtocolError(c, reason)
			return
		}

		switch m := msg.(type) {
		case SubscribeMsg:
			if handlers.OnSubscribe == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			handlers.OnSubscribe(ctx, c, &m)
		case UnsubscribeMsg:
			if handlers.OnUnsubscribe == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			handlers.OnUnsubscribe(ctx, c, &m)
		case CallReducerMsg:
			if handlers.OnCallReducer == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			handlers.OnCallReducer(ctx, c, &m)
		case OneOffQueryMsg:
			if handlers.OnOneOffQuery == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			handlers.OnOneOffQuery(ctx, c, &m)
		}
	}
}
```

Add `"errors"` to the import block in `dispatch.go`.

- [ ] **Run test to verify it passes**

Run: `rtk go test ./protocol/ -run TestDispatchLoop_ValidSubscribe -v`
Expected: PASS

#### Step 1.4: Write test for text frame rejection

- [ ] **Add test to `dispatch_test.go`**

```go
func TestDispatchLoop_TextFrameCloses(t *testing.T) {
	conn, client := testConnPair(t, nil)
	handlers := &MessageHandlers{}

	done := make(chan struct{})
	go func() {
		conn.runDispatchLoop(context.Background(), handlers)
		close(done)
	}()

	// Send a text frame.
	if err := client.Write(context.Background(), websocket.MessageText, []byte("hello")); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
		// dispatch loop exited — correct
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit after text frame")
	}
}
```

- [ ] **Run test**

Run: `rtk go test ./protocol/ -run TestDispatchLoop_TextFrameCloses -v`
Expected: PASS

#### Step 1.5: Write test for unknown tag

- [ ] **Add test to `dispatch_test.go`**

```go
func TestDispatchLoop_UnknownTagCloses(t *testing.T) {
	conn, client := testConnPair(t, nil)
	handlers := &MessageHandlers{}

	done := make(chan struct{})
	go func() {
		conn.runDispatchLoop(context.Background(), handlers)
		close(done)
	}()

	// Send a binary frame with unknown tag 0xFF.
	if err := client.Write(context.Background(), websocket.MessageBinary, []byte{0xFF, 0x00}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit after unknown tag")
	}
}
```

- [ ] **Run test**

Run: `rtk go test ./protocol/ -run TestDispatchLoop_UnknownTagCloses -v`
Expected: PASS

#### Step 1.6: Write test for nil handler closes connection

- [ ] **Add test to `dispatch_test.go`**

```go
func TestDispatchLoop_NilHandlerCloses(t *testing.T) {
	conn, client := testConnPair(t, nil)
	// OnSubscribe is nil — dispatch loop should close.
	handlers := &MessageHandlers{}

	done := make(chan struct{})
	go func() {
		conn.runDispatchLoop(context.Background(), handlers)
		close(done)
	}()

	frame, _ := EncodeClientMessage(SubscribeMsg{
		RequestID:      1,
		SubscriptionID: 1,
		Query:          Query{TableName: "t"},
	})
	if err := client.Write(context.Background(), websocket.MessageBinary, frame); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit for nil handler")
	}
}
```

- [ ] **Run test**

Run: `rtk go test ./protocol/ -run TestDispatchLoop_NilHandlerCloses -v`
Expected: PASS

#### Step 1.7: Write test for malformed body closes connection

- [ ] **Add test to `dispatch_test.go`**

```go
func TestDispatchLoop_MalformedBodyCloses(t *testing.T) {
	conn, client := testConnPair(t, nil)
	handlers := &MessageHandlers{
		OnSubscribe: func(ctx context.Context, c *Conn, msg *SubscribeMsg) {},
	}

	done := make(chan struct{})
	go func() {
		conn.runDispatchLoop(context.Background(), handlers)
		close(done)
	}()

	// Valid Subscribe tag (1) but truncated body.
	if err := client.Write(context.Background(), websocket.MessageBinary, []byte{TagSubscribe}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit for malformed body")
	}
}
```

- [ ] **Run test**

Run: `rtk go test ./protocol/ -run TestDispatchLoop_MalformedBodyCloses -v`
Expected: PASS

#### Step 1.8: Write test for MarkActivity on each frame

- [ ] **Add test to `dispatch_test.go`**

```go
func TestDispatchLoop_MarksActivity(t *testing.T) {
	conn, client := testConnPair(t, nil)
	handlers := &MessageHandlers{
		OnCallReducer: func(ctx context.Context, c *Conn, msg *CallReducerMsg) {},
	}

	// Record initial activity.
	before := conn.lastActivity.Load()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		conn.runDispatchLoop(ctx, handlers)
		close(done)
	}()

	// Wait a bit then send a frame.
	time.Sleep(10 * time.Millisecond)
	frame, _ := EncodeClientMessage(CallReducerMsg{
		RequestID:   1,
		ReducerName: "test",
		Args:        nil,
	})
	if err := client.Write(context.Background(), websocket.MessageBinary, frame); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	after := conn.lastActivity.Load()
	if after <= before {
		t.Fatalf("lastActivity not updated: before=%d after=%d", before, after)
	}
}
```

- [ ] **Run test**

Run: `rtk go test ./protocol/ -run TestDispatchLoop_MarksActivity -v`
Expected: PASS

#### Step 1.9: Run full protocol test suite

- [ ] **Verify no regressions**

Run: `rtk go test ./protocol/ -count=1`
Expected: all tests pass (86 existing + new dispatch tests)

#### Step 1.10: Commit

- [ ] **Commit Story 4.1**

```bash
rtk git add protocol/dispatch.go protocol/dispatch_test.go
rtk git commit -m "protocol(4.1): frame reader and tag dispatch loop

Replace runReadPump with runDispatchLoop: decode tag byte, dispatch to
MessageHandlers, reject text frames and unknown tags with close 1002.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Subscribe Handler

**Files:**
- Create: `protocol/handle_subscribe.go`
- Create: `protocol/handle_subscribe_test.go`
- Modify: `protocol/lifecycle.go` — extend ExecutorInbox

#### Step 2.1: Extend ExecutorInbox with RegisterSubscription

- [ ] **Add method to ExecutorInbox in `protocol/lifecycle.go`**

Add to the `ExecutorInbox` interface:

```go
	// RegisterSubscription submits a subscription registration to the
	// executor inbox. Returns an error only if the inbox rejects
	// submission (full/shutdown). Response delivery is E5's job.
	RegisterSubscription(ctx context.Context, req RegisterSubscriptionRequest) error
```

Add the request type below the interface (still in `lifecycle.go`):

```go
// RegisterSubscriptionRequest carries validated subscription data from
// the protocol layer to the executor inbox. The protocol layer has
// already resolved string names to internal IDs and compiled the
// predicate into the subscription.Predicate model.
type RegisterSubscriptionRequest struct {
	ConnID         types.ConnectionID
	SubscriptionID uint32
	RequestID      uint32
	Predicate      any // subscription.Predicate — typed as any to avoid import cycle
}
```

- [ ] **Update the mock in existing lifecycle tests if needed**

Existing tests in `protocol/lifecycle_test.go` use a mock `ExecutorInbox`. Add the new method to the mock so compilation doesn't break:

Find the mock struct and add:

```go
func (m *mockExecutorInbox) RegisterSubscription(ctx context.Context, req RegisterSubscriptionRequest) error {
	return nil
}
```

(Also add stubs for `UnregisterSubscription` and `CallReducer` — we'll flesh these out in Task 3.)

```go
func (m *mockExecutorInbox) UnregisterSubscription(ctx context.Context, connID types.ConnectionID, subID uint32) error {
	return nil
}

func (m *mockExecutorInbox) CallReducer(ctx context.Context, req CallReducerRequest) error {
	return nil
}
```

And add the `CallReducerRequest` type to `lifecycle.go`:

```go
// CallReducerRequest carries validated CallReducer data from protocol
// to executor. Args is raw BSATN — type validation is the executor's
// job.
type CallReducerRequest struct {
	ConnID      types.ConnectionID
	Identity    types.Identity
	RequestID   uint32
	ReducerName string
	Args        []byte
}
```

And add `UnregisterSubscription` and `CallReducer` to the `ExecutorInbox` interface:

```go
	// UnregisterSubscription submits a subscription removal. Returns
	// error only on inbox rejection.
	UnregisterSubscription(ctx context.Context, connID types.ConnectionID, subID uint32) error
	// CallReducer submits a reducer invocation. Returns error only on
	// inbox rejection. Lifecycle names must be filtered before calling.
	CallReducer(ctx context.Context, req CallReducerRequest) error
```

- [ ] **Verify compilation**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 2.2: Write SchemaLookup interface and NormalizePredicates

- [ ] **Create `protocol/handle_subscribe.go`**

```go
package protocol

import (
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// SchemaLookup resolves wire-format string names to internal IDs. The
// protocol layer uses this to compile client predicates into the
// subscription.Predicate model before submitting to the executor.
type SchemaLookup interface {
	TableByName(name string) (schema.TableID, *schema.TableSchema, bool)
}

// NormalizePredicates converts wire-format predicates (string column
// names, types.Value literals) into a subscription.Predicate tree.
//
// Rules (SPEC-005 §7.1.1, v1 subset):
//   - Empty predicates → AllRows
//   - Single predicate → ColEq
//   - Multiple predicates → left-associative And tree
//   - Range predicates are rejected (not v1)
func NormalizePredicates(
	tableID schema.TableID,
	ts *schema.TableSchema,
	preds []Predicate,
) (subscription.Predicate, error) {
	if len(preds) == 0 {
		return subscription.AllRows{Table: tableID}, nil
	}

	eqs := make([]subscription.Predicate, 0, len(preds))
	for _, p := range preds {
		col, ok := ts.Column(p.Column)
		if !ok {
			return nil, fmt.Errorf("unknown column %q on table %q", p.Column, ts.Name)
		}
		eqs = append(eqs, subscription.ColEq{
			Table:  tableID,
			Column: types.ColID(col.Index),
			Value:  p.Value,
		})
	}

	if len(eqs) == 1 {
		return eqs[0], nil
	}

	// Left-associative And tree: And{And{P1, P2}, P3}
	result := subscription.And{Left: eqs[0], Right: eqs[1]}
	for i := 2; i < len(eqs); i++ {
		result = subscription.And{Left: result, Right: eqs[i]}
	}
	return result, nil
}
```

- [ ] **Verify compilation**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 2.3: Write NormalizePredicates tests

- [ ] **Create `protocol/handle_subscribe_test.go`**

```go
package protocol

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// --- Mock SchemaLookup ---

type mockSchemaLookup struct {
	tables map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}
}

func (m *mockSchemaLookup) TableByName(name string) (schema.TableID, *schema.TableSchema, bool) {
	entry, ok := m.tables[name]
	if !ok {
		return 0, nil, false
	}
	return entry.id, entry.schema, true
}

func newMockSchema(name string, id schema.TableID, cols ...schema.ColumnSchema) *mockSchemaLookup {
	ts := &schema.TableSchema{ID: id, Name: name, Columns: cols}
	return &mockSchemaLookup{
		tables: map[string]struct {
			id     schema.TableID
			schema *schema.TableSchema
		}{
			name: {id: id, schema: ts},
		},
	}
}

func TestNormalizePredicates_Empty(t *testing.T) {
	pred, err := NormalizePredicates(1, &schema.TableSchema{ID: 1, Name: "t"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	allRows, ok := pred.(subscription.AllRows)
	if !ok {
		t.Fatalf("got %T, want AllRows", pred)
	}
	if allRows.Table != 1 {
		t.Fatalf("table = %d, want 1", allRows.Table)
	}
}

func TestNormalizePredicates_Single(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   5,
		Name: "players",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint32},
		},
	}
	preds := []Predicate{{Column: "id", Value: types.NewUint32(42)}}
	pred, err := NormalizePredicates(5, ts, preds)
	if err != nil {
		t.Fatal(err)
	}
	colEq, ok := pred.(subscription.ColEq)
	if !ok {
		t.Fatalf("got %T, want ColEq", pred)
	}
	if colEq.Table != 5 || colEq.Column != 0 {
		t.Fatalf("ColEq = {Table:%d, Column:%d}, want {5, 0}", colEq.Table, colEq.Column)
	}
}

func TestNormalizePredicates_ThreePredicates(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   1,
		Name: "events",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "a", Type: types.KindUint32},
			{Index: 1, Name: "b", Type: types.KindUint32},
			{Index: 2, Name: "c", Type: types.KindUint32},
		},
	}
	preds := []Predicate{
		{Column: "a", Value: types.NewUint32(1)},
		{Column: "b", Value: types.NewUint32(2)},
		{Column: "c", Value: types.NewUint32(3)},
	}
	pred, err := NormalizePredicates(1, ts, preds)
	if err != nil {
		t.Fatal(err)
	}
	// Expect And{And{ColEq(a), ColEq(b)}, ColEq(c)}
	outer, ok := pred.(subscription.And)
	if !ok {
		t.Fatalf("got %T, want And", pred)
	}
	inner, ok := outer.Left.(subscription.And)
	if !ok {
		t.Fatalf("outer.Left got %T, want And", outer.Left)
	}
	_ = inner.Left.(subscription.ColEq)  // P1
	_ = inner.Right.(subscription.ColEq) // P2
	_ = outer.Right.(subscription.ColEq) // P3
}

func TestNormalizePredicates_UnknownColumn(t *testing.T) {
	ts := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: types.KindUint32},
	}}
	preds := []Predicate{{Column: "nosuch", Value: types.NewUint32(1)}}
	_, err := NormalizePredicates(1, ts, preds)
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
}
```

- [ ] **Run tests**

Run: `rtk go test ./protocol/ -run TestNormalizePredicates -v`
Expected: all PASS

#### Step 2.4: Write handleSubscribe

- [ ] **Add handleSubscribe to `protocol/handle_subscribe.go`**

```go
// handleSubscribe processes a client Subscribe message (SPEC-005 §7.1).
// Validates wire-format inputs, normalizes predicates, reserves the
// subscription in the tracker, and submits to the executor. Validation
// failures send SubscriptionError directly. Success response
// (SubscribeApplied) is E5's responsibility.
func handleSubscribe(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	subID := msg.SubscriptionID

	// 1. Reserve subscription_id (rejects duplicate pending/active).
	if err := conn.Subscriptions.Reserve(subID); err != nil {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          err.Error(),
		})
		return
	}

	// From here, release on any validation failure.
	releaseOnError := true
	defer func() {
		if releaseOnError {
			_ = conn.Subscriptions.Remove(subID)
		}
	}()

	// 2. Resolve table name.
	tableID, ts, ok := sl.TableByName(msg.Query.TableName)
	if !ok {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          fmt.Sprintf("unknown table %q", msg.Query.TableName),
		})
		return
	}

	// 3-5. Normalize predicates (validates columns + v1 subset).
	pred, err := NormalizePredicates(tableID, ts, msg.Query.Predicates)
	if err != nil {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          err.Error(),
		})
		return
	}

	// 6. Submit to executor.
	if err := executor.RegisterSubscription(ctx, RegisterSubscriptionRequest{
		ConnID:         conn.ID,
		SubscriptionID: subID,
		RequestID:      msg.RequestID,
		Predicate:      pred,
	}); err != nil {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          "executor unavailable: " + err.Error(),
		})
		return
	}

	// Submission succeeded — subscription stays pending. E5 activates.
	releaseOnError = false
}
```

- [ ] **Verify compilation**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 2.5: Write handleSubscribe tests

- [ ] **Add handler tests to `handle_subscribe_test.go`**

```go
// --- Mock ExecutorInbox for subscribe tests ---

type mockSubExecutor struct {
	mu          sync.Mutex
	registerReq *RegisterSubscriptionRequest
	registerErr error

	// Stubs for other interface methods.
}

func (m *mockSubExecutor) OnConnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error {
	return nil
}
func (m *mockSubExecutor) OnDisconnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error {
	return nil
}
func (m *mockSubExecutor) DisconnectClientSubscriptions(ctx context.Context, connID types.ConnectionID) error {
	return nil
}
func (m *mockSubExecutor) UnregisterSubscription(ctx context.Context, connID types.ConnectionID, subID uint32) error {
	return nil
}
func (m *mockSubExecutor) CallReducer(ctx context.Context, req CallReducerRequest) error {
	return nil
}

func (m *mockSubExecutor) RegisterSubscription(ctx context.Context, req RegisterSubscriptionRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registerReq = &req
	return m.registerErr
}

func testConnDirect(opts *ProtocolOptions) *Conn {
	if opts == nil {
		o := DefaultProtocolOptions()
		opts = &o
	}
	return &Conn{
		ID:            GenerateConnectionID(),
		Identity:      [32]byte{1},
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 16),
		opts:          opts,
		closed:        make(chan struct{}),
	}
}

func TestHandleSubscribe_Valid(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("players", 5,
		schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint32},
	)
	exec := &mockSubExecutor{}

	msg := &SubscribeMsg{
		RequestID:      1,
		SubscriptionID: 42,
		Query: Query{
			TableName:  "players",
			Predicates: []Predicate{{Column: "id", Value: types.NewUint32(7)}},
		},
	}
	handleSubscribe(context.Background(), conn, msg, exec, sl)

	// Subscription should be pending in tracker.
	if !conn.Subscriptions.IsActiveOrPending(42) {
		t.Fatal("subscription 42 should be tracked")
	}
	// Executor should have received the request.
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.registerReq == nil {
		t.Fatal("executor did not receive RegisterSubscription")
	}
	if exec.registerReq.SubscriptionID != 42 {
		t.Fatalf("subID = %d, want 42", exec.registerReq.SubscriptionID)
	}
}

func TestHandleSubscribe_DuplicateID(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Subscriptions.Reserve(42) // already pending

	sl := newMockSchema("players", 1)
	exec := &mockSubExecutor{}
	msg := &SubscribeMsg{RequestID: 1, SubscriptionID: 42, Query: Query{TableName: "players"}}
	handleSubscribe(context.Background(), conn, msg, exec, sl)

	// Should have sent SubscriptionError.
	select {
	case frame := <-conn.OutboundCh:
		_, decoded, err := DecodeServerMessage(frame)
		if err != nil {
			t.Fatalf("decode error: %v", err)
		}
		subErr, ok := decoded.(SubscriptionError)
		if !ok {
			t.Fatalf("got %T, want SubscriptionError", decoded)
		}
		if subErr.SubscriptionID != 42 {
			t.Fatalf("subID = %d, want 42", subErr.SubscriptionID)
		}
	default:
		t.Fatal("no error sent to OutboundCh")
	}
}

func TestHandleSubscribe_UnknownTable(t *testing.T) {
	conn := testConnDirect(nil)
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{}} // empty — no tables
	exec := &mockSubExecutor{}
	msg := &SubscribeMsg{RequestID: 1, SubscriptionID: 10, Query: Query{TableName: "nosuch"}}
	handleSubscribe(context.Background(), conn, msg, exec, sl)

	select {
	case <-conn.OutboundCh:
		// SubscriptionError sent — correct
	default:
		t.Fatal("no error sent for unknown table")
	}
	// Subscription should NOT be tracked.
	if conn.Subscriptions.IsActiveOrPending(10) {
		t.Fatal("subscription 10 should have been released")
	}
}

func TestHandleSubscribe_ExecutorReject(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1)
	exec := &mockSubExecutor{registerErr: errors.New("inbox full")}
	msg := &SubscribeMsg{RequestID: 1, SubscriptionID: 20, Query: Query{TableName: "t"}}
	handleSubscribe(context.Background(), conn, msg, exec, sl)

	select {
	case <-conn.OutboundCh:
		// error sent
	default:
		t.Fatal("no error sent on executor reject")
	}
	if conn.Subscriptions.IsActiveOrPending(20) {
		t.Fatal("subscription 20 should have been released after executor reject")
	}
}
```

- [ ] **Run tests**

Run: `rtk go test ./protocol/ -run TestHandleSubscribe -v`
Expected: all PASS

#### Step 2.6: Run full suite

- [ ] **Verify no regressions**

Run: `rtk go test ./protocol/ -count=1`
Expected: all pass

#### Step 2.7: Commit

- [ ] **Commit Story 4.2**

```bash
rtk git add protocol/handle_subscribe.go protocol/handle_subscribe_test.go protocol/lifecycle.go
rtk git commit -m "protocol(4.2): subscribe handler with predicate normalization

SchemaLookup interface for string→ID resolution. NormalizePredicates
compiles wire predicates to subscription.Predicate (AllRows, ColEq,
left-associative And). handleSubscribe validates, reserves tracker,
submits to executor. ExecutorInbox extended with RegisterSubscription,
UnregisterSubscription, CallReducer.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Unsubscribe & CallReducer Handlers

**Files:**
- Create: `protocol/handle_unsubscribe.go`
- Create: `protocol/handle_callreducer.go`
- Create: `protocol/handle_callreducer_test.go`
- Modify: `protocol/conn.go` — add `IsActive` to SubscriptionTracker

#### Step 3.1: Add IsActive method to SubscriptionTracker

- [ ] **Add to `protocol/conn.go`**

```go
// IsActive reports whether id is tracked AND in the SubActive state.
// Used by handleUnsubscribe to reject unsubscribe of pending
// subscriptions per SPEC-005 §9.1.
func (t *SubscriptionTracker) IsActive(id uint32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.subs[id]
	return ok && st == SubActive
}
```

- [ ] **Verify compilation**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 3.2: Write handleUnsubscribe

- [ ] **Create `protocol/handle_unsubscribe.go`**

```go
package protocol

import (
	"context"
	"fmt"
)

// handleUnsubscribe processes a client Unsubscribe message (SPEC-005
// §7.2). Validates subscription state, submits to executor, removes
// from tracker. UnsubscribeApplied delivery is E5's job.
func handleUnsubscribe(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeMsg,
	executor ExecutorInbox,
) {
	subID := msg.SubscriptionID

	// Must be active — pending and missing are both rejected.
	if !conn.Subscriptions.IsActive(subID) {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          fmt.Sprintf("%v: id=%d", ErrSubscriptionNotFound, subID),
		})
		return
	}

	if err := executor.UnregisterSubscription(ctx, conn.ID, subID); err != nil {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          "executor unavailable: " + err.Error(),
		})
		return
	}

	// Remove from tracker immediately. Read loop is single-goroutine
	// so no race with concurrent messages on the same connection.
	_ = conn.Subscriptions.Remove(subID)
}
```

- [ ] **Verify compilation**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 3.3: Write handleCallReducer

- [ ] **Create `protocol/handle_callreducer.go`**

```go
package protocol

import "context"

// lifecycleReducerNames is the set of reducer names that cannot be
// invoked by clients (SPEC-005 §7.3). Blocked at the protocol layer
// so the executor never sees them from a client call path.
var lifecycleReducerNames = map[string]bool{
	"OnConnect":    true,
	"OnDisconnect": true,
}

// handleCallReducer processes a client CallReducer message (SPEC-005
// §7.3). Rejects lifecycle reducer names with status=3 (not_found),
// submits valid calls to the executor. ReducerCallResult delivery for
// actual executions is E5's job.
func handleCallReducer(
	ctx context.Context,
	conn *Conn,
	msg *CallReducerMsg,
	executor ExecutorInbox,
) {
	// Reject lifecycle reducer names at protocol layer.
	if lifecycleReducerNames[msg.ReducerName] {
		sendError(conn, ReducerCallResult{
			RequestID: msg.RequestID,
			Status:    3, // not_found
			Error:     "lifecycle reducer cannot be called externally",
		})
		return
	}

	if err := executor.CallReducer(ctx, CallReducerRequest{
		ConnID:      conn.ID,
		Identity:    conn.Identity,
		RequestID:   msg.RequestID,
		ReducerName: msg.ReducerName,
		Args:        msg.Args,
	}); err != nil {
		sendError(conn, ReducerCallResult{
			RequestID: msg.RequestID,
			Status:    3, // not_found (executor rejected)
			Error:     "executor unavailable: " + err.Error(),
		})
		return
	}
}
```

- [ ] **Verify compilation**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 3.4: Write tests for Unsubscribe and CallReducer

- [ ] **Create `protocol/handle_callreducer_test.go`**

```go
package protocol

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ponchione/shunter/types"
)

// --- Mock ExecutorInbox for unsubscribe/callreducer tests ---

type mockDispatchExecutor struct {
	mu             sync.Mutex
	unregisterConn types.ConnectionID
	unregisterSub  uint32
	unregisterErr  error

	callReducerReq *CallReducerRequest
	callReducerErr error
}

func (m *mockDispatchExecutor) OnConnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error {
	return nil
}
func (m *mockDispatchExecutor) OnDisconnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error {
	return nil
}
func (m *mockDispatchExecutor) DisconnectClientSubscriptions(ctx context.Context, connID types.ConnectionID) error {
	return nil
}
func (m *mockDispatchExecutor) RegisterSubscription(ctx context.Context, req RegisterSubscriptionRequest) error {
	return nil
}

func (m *mockDispatchExecutor) UnregisterSubscription(ctx context.Context, connID types.ConnectionID, subID uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unregisterConn = connID
	m.unregisterSub = subID
	return m.unregisterErr
}

func (m *mockDispatchExecutor) CallReducer(ctx context.Context, req CallReducerRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callReducerReq = &req
	return m.callReducerErr
}

// --- Unsubscribe tests ---

func TestHandleUnsubscribe_Active(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Subscriptions.Reserve(10)
	conn.Subscriptions.Activate(10)

	exec := &mockDispatchExecutor{}
	msg := &UnsubscribeMsg{RequestID: 1, SubscriptionID: 10}
	handleUnsubscribe(context.Background(), conn, msg, exec)

	// Should be removed from tracker.
	if conn.Subscriptions.IsActiveOrPending(10) {
		t.Fatal("subscription 10 should be removed")
	}
	// Executor should have received the unregister.
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.unregisterSub != 10 {
		t.Fatalf("unregisterSub = %d, want 10", exec.unregisterSub)
	}
}

func TestHandleUnsubscribe_Pending(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Subscriptions.Reserve(10) // pending, not active

	exec := &mockDispatchExecutor{}
	msg := &UnsubscribeMsg{RequestID: 1, SubscriptionID: 10}
	handleUnsubscribe(context.Background(), conn, msg, exec)

	// Should send error — cannot unsubscribe pending.
	select {
	case <-conn.OutboundCh:
		// error sent
	default:
		t.Fatal("no error sent for pending subscription unsubscribe")
	}
	// Subscription should still be tracked (pending).
	if !conn.Subscriptions.IsActiveOrPending(10) {
		t.Fatal("pending subscription should not be removed on failed unsubscribe")
	}
}

func TestHandleUnsubscribe_NotFound(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}
	msg := &UnsubscribeMsg{RequestID: 1, SubscriptionID: 99}
	handleUnsubscribe(context.Background(), conn, msg, exec)

	select {
	case <-conn.OutboundCh:
		// error sent
	default:
		t.Fatal("no error sent for unknown subscription")
	}
}

func TestHandleUnsubscribe_ExecutorReject(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Subscriptions.Reserve(10)
	conn.Subscriptions.Activate(10)

	exec := &mockDispatchExecutor{unregisterErr: errors.New("shutdown")}
	msg := &UnsubscribeMsg{RequestID: 1, SubscriptionID: 10}
	handleUnsubscribe(context.Background(), conn, msg, exec)

	select {
	case <-conn.OutboundCh:
		// error sent
	default:
		t.Fatal("no error sent on executor reject")
	}
	// Subscription should still be tracked — executor didn't process it.
	if !conn.Subscriptions.IsActiveOrPending(10) {
		t.Fatal("subscription should remain tracked after executor reject")
	}
}

// --- CallReducer tests ---

func TestHandleCallReducer_Valid(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}
	msg := &CallReducerMsg{RequestID: 1, ReducerName: "add_player", Args: []byte{0x01}}
	handleCallReducer(context.Background(), conn, msg, exec)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.callReducerReq == nil {
		t.Fatal("executor did not receive CallReducer")
	}
	if exec.callReducerReq.ReducerName != "add_player" {
		t.Fatalf("reducerName = %q, want add_player", exec.callReducerReq.ReducerName)
	}
}

func TestHandleCallReducer_OnConnect(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}
	msg := &CallReducerMsg{RequestID: 1, ReducerName: "OnConnect"}
	handleCallReducer(context.Background(), conn, msg, exec)

	select {
	case frame := <-conn.OutboundCh:
		// Should be ReducerCallResult with status=3.
		// Decode to verify.
		_, decoded, err := DecodeServerMessage(frame)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		result, ok := decoded.(ReducerCallResult)
		if !ok {
			t.Fatalf("got %T, want ReducerCallResult", decoded)
		}
		if result.Status != 3 {
			t.Fatalf("status = %d, want 3", result.Status)
		}
	default:
		t.Fatal("no response sent for lifecycle reducer")
	}

	// Executor should NOT have received anything.
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.callReducerReq != nil {
		t.Fatal("executor should not receive lifecycle reducer call")
	}
}

func TestHandleCallReducer_OnDisconnect(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}
	msg := &CallReducerMsg{RequestID: 1, ReducerName: "OnDisconnect"}
	handleCallReducer(context.Background(), conn, msg, exec)

	select {
	case <-conn.OutboundCh:
		// error sent — correct
	default:
		t.Fatal("no response sent for OnDisconnect")
	}
}

func TestHandleCallReducer_ExecutorReject(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{callReducerErr: errors.New("busy")}
	msg := &CallReducerMsg{RequestID: 1, ReducerName: "do_thing"}
	handleCallReducer(context.Background(), conn, msg, exec)

	select {
	case <-conn.OutboundCh:
		// error sent
	default:
		t.Fatal("no response sent on executor reject")
	}
}
```

- [ ] **Run tests**

Run: `rtk go test ./protocol/ -run "TestHandleUnsubscribe|TestHandleCallReducer" -v`
Expected: all PASS

#### Step 3.5: Run full suite

- [ ] **Verify no regressions**

Run: `rtk go test ./protocol/ -count=1`
Expected: all pass

#### Step 3.6: Commit

- [ ] **Commit Story 4.3**

```bash
rtk git add protocol/handle_unsubscribe.go protocol/handle_callreducer.go protocol/handle_callreducer_test.go protocol/conn.go
rtk git commit -m "protocol(4.3): unsubscribe and callreducer handlers

handleUnsubscribe validates active state, submits to executor, removes
from tracker. handleCallReducer blocks lifecycle names at protocol
layer, submits valid calls. IsActive added to SubscriptionTracker.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: OneOffQuery Handler

**Files:**
- Create: `protocol/handle_oneoff.go`
- Create: `protocol/handle_oneoff_test.go`

#### Step 4.1: Write CommittedStateAccess interface and handleOneOffQuery

- [ ] **Create `protocol/handle_oneoff.go`**

```go
package protocol

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// CommittedStateAccess provides read-only snapshot access to committed
// state. Used by handleOneOffQuery to execute point-in-time queries
// without going through the executor (SPEC-005 §7.4).
type CommittedStateAccess interface {
	Snapshot() store.CommittedReadView
}

// handleOneOffQuery processes a client OneOffQuery message (SPEC-005
// §7.4). Executes a read-only query against a committed state snapshot.
// The snapshot is released before sending the response.
func handleOneOffQuery(
	ctx context.Context,
	conn *Conn,
	msg *OneOffQueryMsg,
	stateAccess CommittedStateAccess,
	sl SchemaLookup,
) {
	// Resolve table.
	tableID, ts, ok := sl.TableByName(msg.TableName)
	if !ok {
		sendError(conn, OneOffQueryResult{
			RequestID: msg.RequestID,
			Status:    1,
			Error:     fmt.Sprintf("unknown table %q", msg.TableName),
		})
		return
	}

	// Validate predicate columns.
	for _, p := range msg.Predicates {
		if _, colOK := ts.Column(p.Column); !colOK {
			sendError(conn, OneOffQueryResult{
				RequestID: msg.RequestID,
				Status:    1,
				Error:     fmt.Sprintf("unknown column %q on table %q", p.Column, ts.Name),
			})
			return
		}
	}

	// Build column matchers for filtering.
	type colMatcher struct {
		colIdx int
		value  types.Value
	}
	matchers := make([]colMatcher, 0, len(msg.Predicates))
	for _, p := range msg.Predicates {
		col, _ := ts.Column(p.Column) // already validated
		matchers = append(matchers, colMatcher{colIdx: col.Index, value: p.Value})
	}

	// Snapshot + scan + filter.
	view := stateAccess.Snapshot()
	var rows [][]byte
	for _, pv := range view.TableScan(tableID) {
		if matchesAll(pv, matchers) {
			var buf bytes.Buffer
			if err := bsatn.EncodeProductValue(&buf, pv); err != nil {
				view.Close()
				sendError(conn, OneOffQueryResult{
					RequestID: msg.RequestID,
					Status:    1,
					Error:     "encode error: " + err.Error(),
				})
				return
			}
			rows = append(rows, buf.Bytes())
		}
	}
	view.Close() // Release before network write.

	encoded := EncodeRowList(rows)
	sendError(conn, OneOffQueryResult{
		RequestID: msg.RequestID,
		Status:    0,
		Rows:      encoded,
	})
}

// matchesAll returns true if the ProductValue satisfies all matchers.
func matchesAll(pv types.ProductValue, matchers []colMatcher) bool {
	for _, m := range matchers {
		if m.colIdx >= len(pv) {
			return false
		}
		if !pv[m.colIdx].Equal(m.value) {
			return false
		}
	}
	return true
}
```

- [ ] **Verify compilation**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 4.2: Write handleOneOffQuery tests

- [ ] **Create `protocol/handle_oneoff_test.go`**

```go
package protocol

import (
	"context"
	"iter"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// --- Mock CommittedStateAccess ---

type mockSnapshot struct {
	rows map[schema.TableID][]types.ProductValue
}

func (s *mockSnapshot) TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for i, pv := range s.rows[id] {
			if !yield(types.RowID(i), pv) {
				return
			}
		}
	}
}

func (s *mockSnapshot) IndexSeek(schema.TableID, schema.IndexID, store.IndexKey) []types.RowID {
	return nil
}
func (s *mockSnapshot) IndexRange(schema.TableID, schema.IndexID, *store.IndexKey, *store.IndexKey) iter.Seq[types.RowID] {
	return func(func(types.RowID) bool) {}
}
func (s *mockSnapshot) GetRow(schema.TableID, types.RowID) (types.ProductValue, bool) {
	return nil, false
}
func (s *mockSnapshot) RowCount(schema.TableID) int { return 0 }
func (s *mockSnapshot) Close()                      {}

type mockStateAccess struct {
	snap *mockSnapshot
}

func (m *mockStateAccess) Snapshot() store.CommittedReadView {
	return m.snap
}

func TestHandleOneOffQuery_Valid(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("players", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "name", Type: types.KindString},
	)
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewString("alice")},
				{types.NewUint32(2), types.NewString("bob")},
				{types.NewUint32(3), types.NewString("carol")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{
		RequestID: 7,
		TableName: "players",
		Predicates: []Predicate{
			{Column: "id", Value: types.NewUint32(2)},
		},
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	select {
	case frame := <-conn.OutboundCh:
		_, decoded, err := DecodeServerMessage(frame)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		result, ok := decoded.(OneOffQueryResult)
		if !ok {
			t.Fatalf("got %T, want OneOffQueryResult", decoded)
		}
		if result.Status != 0 {
			t.Fatalf("status = %d, want 0 (error: %s)", result.Status, result.Error)
		}
		rows, err := DecodeRowList(result.Rows)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("got %d rows, want 1", len(rows))
		}
	default:
		t.Fatal("no response sent")
	}
}

func TestHandleOneOffQuery_NoMatches(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("players", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint32},
	)
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {{types.NewUint32(1)}},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{
		RequestID:  1,
		TableName:  "players",
		Predicates: []Predicate{{Column: "id", Value: types.NewUint32(999)}},
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	select {
	case frame := <-conn.OutboundCh:
		_, decoded, _ := DecodeServerMessage(frame)
		result := decoded.(OneOffQueryResult)
		if result.Status != 0 {
			t.Fatalf("status = %d, want 0", result.Status)
		}
		rows, _ := DecodeRowList(result.Rows)
		if len(rows) != 0 {
			t.Fatalf("got %d rows, want 0", len(rows))
		}
	default:
		t.Fatal("no response sent")
	}
}

func TestHandleOneOffQuery_EmptyPredicates(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("players", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint32},
	)
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1)},
				{types.NewUint32(2)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{RequestID: 1, TableName: "players"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	select {
	case frame := <-conn.OutboundCh:
		_, decoded, _ := DecodeServerMessage(frame)
		result := decoded.(OneOffQueryResult)
		if result.Status != 0 {
			t.Fatalf("status = %d, want 0", result.Status)
		}
		rows, _ := DecodeRowList(result.Rows)
		if len(rows) != 2 {
			t.Fatalf("got %d rows, want 2", len(rows))
		}
	default:
		t.Fatal("no response sent")
	}
}

func TestHandleOneOffQuery_UnknownTable(t *testing.T) {
	conn := testConnDirect(nil)
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{}}
	stateAccess := &mockStateAccess{snap: &mockSnapshot{}}
	msg := &OneOffQueryMsg{RequestID: 1, TableName: "nosuch"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	select {
	case frame := <-conn.OutboundCh:
		_, decoded, _ := DecodeServerMessage(frame)
		result := decoded.(OneOffQueryResult)
		if result.Status != 1 {
			t.Fatalf("status = %d, want 1", result.Status)
		}
	default:
		t.Fatal("no error sent for unknown table")
	}
}

func TestHandleOneOffQuery_UnknownColumn(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1, schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint32})
	stateAccess := &mockStateAccess{snap: &mockSnapshot{}}
	msg := &OneOffQueryMsg{
		RequestID:  1,
		TableName:  "t",
		Predicates: []Predicate{{Column: "nosuch", Value: types.NewUint32(1)}},
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	select {
	case frame := <-conn.OutboundCh:
		_, decoded, _ := DecodeServerMessage(frame)
		result := decoded.(OneOffQueryResult)
		if result.Status != 1 {
			t.Fatalf("status = %d, want 1", result.Status)
		}
	default:
		t.Fatal("no error sent for unknown column")
	}
}
```

- [ ] **Run tests**

Run: `rtk go test ./protocol/ -run TestHandleOneOffQuery -v`
Expected: all PASS

#### Step 4.3: Run full suite

- [ ] **Verify no regressions**

Run: `rtk go test ./protocol/ -count=1`
Expected: all pass

#### Step 4.4: Commit

- [ ] **Commit Story 4.4**

```bash
rtk git add protocol/handle_oneoff.go protocol/handle_oneoff_test.go
rtk git commit -m "protocol(4.4): oneoff query handler

handleOneOffQuery reads committed state snapshot, filters by predicates,
encodes RowList, sends OneOffQueryResult directly. Snapshot released
before network write. CommittedStateAccess interface decouples from
concrete store type.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Wiring — Replace runReadPump, Update Supervisor

**Files:**
- Modify: `protocol/upgrade.go` — swap runReadPump for runDispatchLoop
- Modify: `protocol/disconnect.go` — rename param in superviseLifecycle

#### Step 5.1: Update upgrade.go to use runDispatchLoop

- [ ] **Replace runReadPump call in `protocol/upgrade.go`**

In the `HandleSubscribe` method, replace the goroutine block (around line 174-184):

Old:
```go
		pumpDone := make(chan struct{})
		keepaliveDone := make(chan struct{})
		go func() {
			c.runReadPump(context.Background())
			close(pumpDone)
		}()
		go func() {
			c.runKeepalive(context.Background())
			close(keepaliveDone)
		}()
		go c.superviseLifecycle(context.Background(), s.Executor, s.Conns, pumpDone, keepaliveDone)
```

New:
```go
		dispatchDone := make(chan struct{})
		keepaliveDone := make(chan struct{})
		handlers := s.buildMessageHandlers()
		go func() {
			c.runDispatchLoop(context.Background(), handlers)
			close(dispatchDone)
		}()
		go func() {
			c.runKeepalive(context.Background())
			close(keepaliveDone)
		}()
		go c.superviseLifecycle(context.Background(), s.Executor, s.Conns, dispatchDone, keepaliveDone)
```

- [ ] **Add buildMessageHandlers method to Server**

Add to `protocol/upgrade.go`:

```go
// buildMessageHandlers constructs the MessageHandlers that wire each
// client message type to the appropriate handler function, closing over
// the Server's dependencies (executor, schema, state).
//
// The Schema and State fields are added to Server here but the actual
// handler implementations were written in Tasks 2-4. This wiring is
// what activates them.
func (s *Server) buildMessageHandlers() *MessageHandlers {
	return &MessageHandlers{
		OnSubscribe: func(ctx context.Context, conn *Conn, msg *SubscribeMsg) {
			if s.Schema != nil {
				handleSubscribe(ctx, conn, msg, s.Executor, s.Schema)
			}
		},
		OnUnsubscribe: func(ctx context.Context, conn *Conn, msg *UnsubscribeMsg) {
			handleUnsubscribe(ctx, conn, msg, s.Executor)
		},
		OnCallReducer: func(ctx context.Context, conn *Conn, msg *CallReducerMsg) {
			handleCallReducer(ctx, conn, msg, s.Executor)
		},
		OnOneOffQuery: func(ctx context.Context, conn *Conn, msg *OneOffQueryMsg) {
			if s.Schema != nil && s.State != nil {
				handleOneOffQuery(ctx, conn, msg, s.State, s.Schema)
			}
		},
	}
}
```

- [ ] **Add Schema and State fields to Server struct**

In `protocol/upgrade.go`, add to the `Server` struct:

```go
	// Schema provides table name→ID resolution for Subscribe and
	// OneOffQuery handlers. Required for dispatch to work.
	Schema SchemaLookup
	// State provides read-only snapshot access for OneOffQuery.
	// Required for OneOffQuery to work.
	State CommittedStateAccess
```

- [ ] **Verify compilation**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 5.2: Update superviseLifecycle param name

- [ ] **Rename in `protocol/disconnect.go`**

In `superviseLifecycle`, rename `readPumpDone` to `dispatchDone` in the parameter and body:

Old:
```go
func (c *Conn) superviseLifecycle(
	ctx context.Context,
	inbox ExecutorInbox,
	mgr *ConnManager,
	readPumpDone <-chan struct{},
	keepaliveDone <-chan struct{},
) {
	select {
	case <-readPumpDone:
	case <-keepaliveDone:
	}
	c.Disconnect(ctx, inbox, mgr)
	<-readPumpDone
	<-keepaliveDone
}
```

New:
```go
func (c *Conn) superviseLifecycle(
	ctx context.Context,
	inbox ExecutorInbox,
	mgr *ConnManager,
	dispatchDone <-chan struct{},
	keepaliveDone <-chan struct{},
) {
	select {
	case <-dispatchDone:
	case <-keepaliveDone:
	}
	c.Disconnect(ctx, inbox, mgr)
	<-dispatchDone
	<-keepaliveDone
}
```

- [ ] **Verify compilation**

Run: `rtk go build ./protocol/`
Expected: clean compile

#### Step 5.3: Run full test suite

- [ ] **Verify everything still passes**

Run: `rtk go test ./protocol/ -count=1`
Expected: all pass

Run: `rtk go test ./... -count=1 -short`
Expected: all pass (cross-package verification)

#### Step 5.4: Commit

- [ ] **Commit wiring**

```bash
rtk git add protocol/upgrade.go protocol/disconnect.go
rtk git commit -m "protocol(4.wire): wire dispatch loop into upgrade handler

Replace runReadPump with runDispatchLoop in HandleSubscribe. Add Schema
and State fields to Server. buildMessageHandlers closes over server
dependencies to route each message type to its handler.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Post-Implementation Checklist

- [ ] All existing tests pass: `rtk go test ./... -count=1 -short`
- [ ] No compiler warnings: `rtk go vet ./protocol/`
- [ ] Verify test count increased from 86 to ~110+ in protocol package
- [ ] Each story has its own commit with conventional prefix
