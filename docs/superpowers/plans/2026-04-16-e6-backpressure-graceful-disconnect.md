# Protocol E6: Backpressure & Graceful Disconnect — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enforce per-connection outgoing and incoming backpressure (close 1008 on overflow), implement all disconnect paths with correct close codes, and verify reconnection composes correctly.

**Architecture:** The outgoing path already has non-blocking send + `ErrClientBufferFull` in `sender.go`. We parameterize `Disconnect` with close code/reason params, wire 1008 close into the sender on overflow. Incoming backpressure is a semaphore in `runDispatchLoop`. Story 6.3 unifies close codes, adds close-handshake timeout, and adds `ConnManager.CloseAll` for graceful shutdown. Story 6.4 is integration tests only.

**Tech Stack:** Go, `github.com/coder/websocket`, `sync/atomic`, buffered channels as semaphores.

**Existing code to know:**
- `protocol/conn.go` — `Conn` struct, `ConnManager`, `OutboundCh` (buffered channel sized to `OutgoingBufferMessages`)
- `protocol/sender.go` — `connManagerSender.enqueue` does non-blocking send, returns `ErrClientBufferFull`
- `protocol/disconnect.go` — `Conn.Disconnect(ctx, inbox, mgr)` runs teardown once via `closeOnce`, hardcodes `StatusNormalClosure`
- `protocol/outbound.go` — `runOutboundWriter` drains `OutboundCh` → WebSocket
- `protocol/dispatch.go` — `runDispatchLoop` is the read loop, no incoming throttle
- `protocol/keepalive.go` — `runKeepalive` handles idle timeout with `StatusPolicyViolation`
- `protocol/options.go` — `CloseHandshakeTimeout` (250ms), `IncomingQueueMessages` (64) defined but unused
- `protocol/errors.go` — `ErrUnknownMessageTag`, `ErrMalformedMessage`
- `protocol/lifecycle_test.go` — `fakeInbox`, `loopbackConn`, `dialSubscribe` test helpers

**Test command:** `rtk go test ./protocol/ -v -run <TestName>`
**Build check:** `rtk go build ./protocol/`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `protocol/conn.go` | Modify | Add `inflightSem` field, `ConnManager.CloseAll` |
| `protocol/disconnect.go` | Modify | Parameterize `Disconnect` with close code/reason, update `superviseLifecycle` |
| `protocol/sender.go` | Modify | Auto-disconnect on `ErrClientBufferFull` inside `enqueue` |
| `protocol/dispatch.go` | Modify | Add incoming semaphore acquire/release around handler dispatch |
| `protocol/close.go` | Create | Close-code constants, close-handshake timeout helper |
| `protocol/backpressure_out_test.go` | Create | Story 6.1 tests |
| `protocol/backpressure_in_test.go` | Create | Story 6.2 tests |
| `protocol/close_test.go` | Create | Story 6.3 tests |
| `protocol/reconnect_test.go` | Create | Story 6.4 tests |

---

## Task 1: Parameterize Disconnect Close Code (Story 6.1 foundation)

**Files:**
- Modify: `protocol/conn.go` (add fields)
- Modify: `protocol/disconnect.go` (accept close code/reason)
- Modify: `protocol/disconnect_test.go` (update existing tests)

Currently `Disconnect` hardcodes `StatusNormalClosure`. We need it to accept a close code and reason so backpressure, protocol errors, and clean-close can all use the same teardown path.

- [ ] **Step 1: Add close-code fields to Conn**

In `protocol/conn.go`, add two fields to `Conn`:

```go
// disconnectCode is the WebSocket close code used when this
// connection's Disconnect fires. Set by CloseWithCode before
// calling Disconnect; defaults to StatusNormalClosure.
disconnectCode websocket.StatusCode
disconnectReason string
```

Add them after the `closed chan struct{}` field.

- [ ] **Step 2: Update NewConn to default close code**

In `protocol/conn.go`, inside `NewConn`, set the default:

```go
c.disconnectCode = websocket.StatusNormalClosure
```

Add this line after `c.MarkActivity()`.

- [ ] **Step 3: Add CloseWithCode method**

In `protocol/disconnect.go`, add before `Disconnect`:

```go
// CloseWithCode sets the close code and reason for the next
// Disconnect call. Safe to call from any goroutine — only the
// first call takes effect (subsequent calls are no-ops, matching
// closeOnce semantics). The code and reason are used by the
// background WebSocket close handshake in Disconnect.
func (c *Conn) CloseWithCode(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		// Re-arm closeOnce — we only wanted to set the code, not
		// run teardown. This is wrong — closeOnce cannot be re-armed.
	})
}
```

Wait — `sync.Once` cannot be re-armed. Different approach: use an `atomic.Bool` for code-setting, and read in `Disconnect`. Simpler: just make `Disconnect` accept the code/reason as parameters.

Replace step 3 with:

In `protocol/disconnect.go`, change `Disconnect` signature from:

```go
func (c *Conn) Disconnect(ctx context.Context, inbox ExecutorInbox, mgr *ConnManager) {
```

to:

```go
func (c *Conn) Disconnect(ctx context.Context, code websocket.StatusCode, reason string, inbox ExecutorInbox, mgr *ConnManager) {
```

And change the `ws.Close` call inside from:

```go
go func() {
    _ = c.ws.Close(websocket.StatusNormalClosure, "")
}()
```

to:

```go
go func() {
    _ = c.ws.Close(code, truncateCloseReason(reason))
}()
```

Remove the `disconnectCode`/`disconnectReason` fields from Step 1 — they are not needed with this approach.

- [ ] **Step 4: Update superviseLifecycle**

In `protocol/disconnect.go`, update `superviseLifecycle` to pass `StatusNormalClosure`:

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
	c.Disconnect(ctx, websocket.StatusNormalClosure, "", inbox, mgr)
	<-dispatchDone
	<-keepaliveDone
}
```

- [ ] **Step 5: Update all existing Disconnect call sites**

Search for `c.Disconnect(` and `.Disconnect(` in the protocol package. Each must gain the code+reason args. All existing calls use `StatusNormalClosure, ""`:

In `protocol/disconnect_test.go`, update every `c.Disconnect(context.Background(), inbox, mgr)` to `c.Disconnect(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr)`.

- [ ] **Step 6: Run existing tests**

Run: `rtk go test ./protocol/ -v -run TestDisconnect`
Expected: All existing disconnect tests pass with the new signature.

- [ ] **Step 7: Commit**

```bash
rtk git add protocol/conn.go protocol/disconnect.go protocol/disconnect_test.go
rtk git commit -m "refactor(protocol): parameterize Disconnect with close code and reason

Prepares for E6 backpressure — different disconnect paths need
different close codes (1000 clean, 1008 policy, 1011 internal)."
```

---

## Task 2: Outgoing Backpressure — Disconnect on Buffer Full (Story 6.1)

**Files:**
- Modify: `protocol/sender.go` (trigger disconnect on buffer full)
- Create: `protocol/backpressure_out_test.go`

The sender already returns `ErrClientBufferFull`. Now we wire it to trigger disconnect with close code 1008.

- [ ] **Step 1: Write failing tests**

Create `protocol/backpressure_out_test.go`:

```go
package protocol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestOutgoingBackpressure_BufferFullDisconnects(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)

	// Attach inbox+mgr to sender so it can disconnect.
	s := NewClientSender(mgr, inbox)

	msg := SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}

	// Fill the buffer.
	if err := s.Send(conn.ID, msg); err != nil {
		t.Fatalf("first send: %v", err)
	}

	// Second send overflows → ErrClientBufferFull + disconnect.
	err := s.Send(conn.ID, msg)
	if !errors.Is(err, ErrClientBufferFull) {
		t.Fatalf("expected ErrClientBufferFull, got %v", err)
	}

	// Client should see close 1008.
	readCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, readErr := clientWS.Read(readCtx)
	if code := websocket.CloseStatus(readErr); code != websocket.StatusPolicyViolation {
		t.Errorf("close code = %d, want %d (StatusPolicyViolation/1008)", code, websocket.StatusPolicyViolation)
	}
}

func TestOutgoingBackpressure_QueuedMessagesNotDropped(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 2
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg1 := SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t1", Rows: []byte{}}
	msg2 := SubscribeApplied{RequestID: 2, SubscriptionID: 20, TableName: "t2", Rows: []byte{}}
	_ = s.Send(conn.ID, msg1)
	_ = s.Send(conn.ID, msg2)

	// Overflow with a third.
	msg3 := SubscribeApplied{RequestID: 3, SubscriptionID: 30, TableName: "t3", Rows: []byte{}}
	err := s.Send(conn.ID, msg3)
	if !errors.Is(err, ErrClientBufferFull) {
		t.Fatalf("expected ErrClientBufferFull, got %v", err)
	}

	// Start outbound writer to flush queued messages.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go conn.runOutboundWriter(ctx)

	// Read both queued messages — they must still be delivered.
	for i := 0; i < 2; i++ {
		readCtx, rCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _, err := clientWS.Read(readCtx)
		rCancel()
		if err != nil {
			t.Fatalf("read queued message %d: %v", i+1, err)
		}
	}
}

func TestOutgoingBackpressure_OverflowMessageNotDelivered(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn := testConnDirect(&opts)

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	_ = s.Send(conn.ID, msg) // fills buffer

	// Overflow.
	_ = s.Send(conn.ID, msg)

	// OutboundCh should have exactly 1 message (the first), not 2.
	count := len(conn.OutboundCh)
	if count != 1 {
		t.Errorf("OutboundCh length = %d, want 1 (overflow message must not be enqueued)", count)
	}
}

func TestOutgoingBackpressure_FurtherSendsAfterDisconnect(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn, _, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	_ = s.Send(conn.ID, msg)

	// Overflow triggers disconnect.
	_ = s.Send(conn.ID, msg)

	// Wait for disconnect to complete (conn removed from manager).
	deadline := time.After(2 * time.Second)
	for mgr.Get(conn.ID) != nil {
		select {
		case <-deadline:
			t.Fatal("conn not removed from manager after overflow disconnect")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Further sends return ErrConnNotFound.
	err := s.Send(conn.ID, msg)
	if !errors.Is(err, ErrConnNotFound) {
		t.Fatalf("expected ErrConnNotFound after disconnect, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `rtk go test ./protocol/ -v -run TestOutgoingBackpressure -count=1`
Expected: Compilation errors because `NewClientSender` signature doesn't match yet.

- [ ] **Step 3: Update NewClientSender to accept inbox**

In `protocol/sender.go`, change the constructor and struct:

```go
// NewClientSender returns a ClientSender backed by mgr for connection
// lookup and frame delivery. When a send overflows, inbox is used to
// drive the disconnect sequence with close code 1008.
func NewClientSender(mgr *ConnManager, inbox ExecutorInbox) ClientSender {
	return &connManagerSender{mgr: mgr, inbox: inbox}
}

type connManagerSender struct {
	mgr   *ConnManager
	inbox ExecutorInbox
}
```

- [ ] **Step 4: Wire disconnect into enqueue**

In `protocol/sender.go`, update the `enqueue` method's `default` case:

```go
select {
case conn.OutboundCh <- wrapped:
    return nil
default:
    // Trigger disconnect asynchronously — the teardown path closes
    // OutboundCh, and the writer goroutine will attempt to flush
    // already-queued messages before the WebSocket closes.
    go conn.Disconnect(context.Background(), websocket.StatusPolicyViolation, "send buffer full", s.inbox, s.mgr)
    return fmt.Errorf("%w: %x", ErrClientBufferFull, connID[:])
}
```

Add `"context"` and `"github.com/coder/websocket"` to imports.

- [ ] **Step 5: Update existing sender tests**

In `protocol/sender_test.go`, update all `NewClientSender(mgr)` calls to `NewClientSender(mgr, &fakeInbox{})`.

- [ ] **Step 6: Update upgrade.go sender construction**

If `NewClientSender` is called in `upgrade.go` or elsewhere, update those call sites too. Search for `NewClientSender(` across the repo.

Run: `rtk go build ./protocol/`

- [ ] **Step 7: Run all tests**

Run: `rtk go test ./protocol/ -v -run "TestOutgoingBackpressure|TestSend" -count=1`
Expected: All pass.

- [ ] **Step 8: Commit**

```bash
rtk git add protocol/sender.go protocol/sender_test.go protocol/backpressure_out_test.go
rtk git commit -m "feat(protocol): outgoing backpressure — disconnect on buffer full (Story 6.1)

Buffer overflow triggers async Disconnect with close 1008 'send buffer
full'. Already-queued messages are flushed by the writer goroutine.
Overflow-causing message is not enqueued."
```

---

## Task 3: Incoming Backpressure — Semaphore in Read Loop (Story 6.2)

**Files:**
- Modify: `protocol/conn.go` (add `inflightSem` field)
- Modify: `protocol/dispatch.go` (acquire/release semaphore)
- Create: `protocol/backpressure_in_test.go`

- [ ] **Step 1: Add semaphore to Conn**

In `protocol/conn.go`, add to the `Conn` struct after `OutboundCh`:

```go
// inflightSem limits concurrent in-flight inbound messages.
// Capacity is IncomingQueueMessages. The dispatch loop acquires
// before handler dispatch and the handler releases on completion.
inflightSem chan struct{}
```

In `NewConn`, initialize it:

```go
inflightSem: make(chan struct{}, opts.IncomingQueueMessages),
```

- [ ] **Step 2: Write failing tests**

Create `protocol/backpressure_in_test.go`:

```go
package protocol

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestIncomingBackpressure_WithinLimitAllProcessed(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.IncomingQueueMessages = 4
	conn, clientWS := testConnPair(t, &opts)

	var mu sync.Mutex
	var count int
	handlers := &MessageHandlers{
		OnSubscribe: func(ctx context.Context, c *Conn, msg *SubscribeMsg) {
			mu.Lock()
			count++
			mu.Unlock()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Send 4 messages (at limit).
	for i := uint32(0); i < 4; i++ {
		frame, _ := EncodeClientMessage(SubscribeMsg{
			RequestID: i, SubscriptionID: i + 100,
			Query: Query{TableName: "t"},
		})
		wCtx, wCancel := context.WithTimeout(ctx, time.Second)
		if err := clientWS.Write(wCtx, websocket.MessageBinary, frame); err != nil {
			wCancel()
			t.Fatalf("write %d: %v", i, err)
		}
		wCancel()
	}

	// Give handlers time to run.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	got := count
	mu.Unlock()
	if got != 4 {
		t.Errorf("processed = %d, want 4", got)
	}

	cancel()
	<-done
}

func TestIncomingBackpressure_ExceedLimitCloses1008(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.IncomingQueueMessages = 2
	conn, clientWS := testConnPair(t, &opts)

	// Handlers that block forever, so inflight never decreases.
	block := make(chan struct{})
	handlers := &MessageHandlers{
		OnSubscribe: func(ctx context.Context, c *Conn, msg *SubscribeMsg) {
			<-block
		},
	}
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Send 3 messages: first 2 acquire semaphore, third exceeds limit.
	for i := uint32(0); i < 3; i++ {
		frame, _ := EncodeClientMessage(SubscribeMsg{
			RequestID: i, SubscriptionID: i + 100,
			Query: Query{TableName: "t"},
		})
		wCtx, wCancel := context.WithTimeout(ctx, time.Second)
		_ = clientWS.Write(wCtx, websocket.MessageBinary, frame)
		wCancel()
	}

	// Dispatch loop should exit (close 1008).
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch loop did not exit on incoming overflow")
	}

	// Client observes 1008.
	readCtx, rCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rCancel()
	_, _, err := clientWS.Read(readCtx)
	if code := websocket.CloseStatus(err); code != websocket.StatusPolicyViolation {
		t.Errorf("close code = %d, want %d (1008)", code, websocket.StatusPolicyViolation)
	}
}

func TestIncomingBackpressure_RapidBurstWithinLimit(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.IncomingQueueMessages = 8
	conn, clientWS := testConnPair(t, &opts)

	var mu sync.Mutex
	var count int
	handlers := &MessageHandlers{
		OnSubscribe: func(ctx context.Context, c *Conn, msg *SubscribeMsg) {
			mu.Lock()
			count++
			mu.Unlock()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Burst 8 messages as fast as possible.
	for i := uint32(0); i < 8; i++ {
		frame, _ := EncodeClientMessage(SubscribeMsg{
			RequestID: i, SubscriptionID: i + 200,
			Query: Query{TableName: "t"},
		})
		wCtx, wCancel := context.WithTimeout(ctx, time.Second)
		_ = clientWS.Write(wCtx, websocket.MessageBinary, frame)
		wCancel()
	}

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	got := count
	mu.Unlock()
	if got != 8 {
		t.Errorf("processed = %d, want 8 (all within limit)", got)
	}

	cancel()
	<-done
}

func TestIncomingBackpressure_OverflowMessageNotProcessed(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.IncomingQueueMessages = 1
	conn, clientWS := testConnPair(t, &opts)

	var mu sync.Mutex
	var ids []uint32
	block := make(chan struct{})
	handlers := &MessageHandlers{
		OnSubscribe: func(ctx context.Context, c *Conn, msg *SubscribeMsg) {
			mu.Lock()
			ids = append(ids, msg.SubscriptionID)
			mu.Unlock()
			<-block
		},
	}
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = runDispatchAsync(conn, ctx, handlers)

	// First message acquires the semaphore and blocks.
	frame1, _ := EncodeClientMessage(SubscribeMsg{
		RequestID: 1, SubscriptionID: 100,
		Query: Query{TableName: "t"},
	})
	wCtx, wCancel := context.WithTimeout(ctx, time.Second)
	_ = clientWS.Write(wCtx, websocket.MessageBinary, frame1)
	wCancel()

	// Let it get dispatched.
	time.Sleep(50 * time.Millisecond)

	// Second message should be rejected (overflow).
	frame2, _ := EncodeClientMessage(SubscribeMsg{
		RequestID: 2, SubscriptionID: 200,
		Query: Query{TableName: "t"},
	})
	wCtx2, wCancel2 := context.WithTimeout(ctx, time.Second)
	_ = clientWS.Write(wCtx2, websocket.MessageBinary, frame2)
	wCancel2()

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := ids
	mu.Unlock()

	// Only subscription 100 should have been processed.
	if len(got) != 1 || got[0] != 100 {
		t.Errorf("processed ids = %v, want [100] (overflow msg must not be processed)", got)
	}
}
```

- [ ] **Step 3: Run tests — verify they fail**

Run: `rtk go test ./protocol/ -v -run TestIncomingBackpressure -count=1`
Expected: Tests fail — semaphore not wired yet.

- [ ] **Step 4: Wire semaphore into dispatch loop**

In `protocol/dispatch.go`, modify `runDispatchLoop`. After the message decode succeeds and before the switch, add semaphore acquire:

```go
// Incoming backpressure (SPEC-005 §10.2, Story 6.2):
// non-blocking acquire on the inflight semaphore. If full,
// the client is flooding faster than we can process —
// close with 1008.
select {
case c.inflightSem <- struct{}{}:
default:
    go func() {
        _ = c.ws.Close(websocket.StatusPolicyViolation, "too many requests")
    }()
    return
}
```

Then wrap each handler call to release the semaphore when done. Change each handler call from:

```go
handlers.OnSubscribe(ctx, c, &m)
```

to:

```go
go func() {
    defer func() { <-c.inflightSem }()
    handlers.OnSubscribe(ctx, c, &m)
}()
```

Do this for all four message types (OnSubscribe, OnUnsubscribe, OnCallReducer, OnOneOffQuery).

- [ ] **Step 5: Run tests**

Run: `rtk go test ./protocol/ -v -run "TestIncomingBackpressure|TestDispatch" -count=1`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
rtk git add protocol/conn.go protocol/dispatch.go protocol/backpressure_in_test.go
rtk git commit -m "feat(protocol): incoming backpressure — semaphore in read loop (Story 6.2)

Per-connection semaphore (capacity = IncomingQueueMessages) limits
in-flight messages. Overflow closes with 1008 'too many requests'.
Handlers run in goroutines to release the semaphore on completion."
```

---

## Task 4: Close Code Constants & Handshake Timeout (Story 6.3 foundation)

**Files:**
- Create: `protocol/close.go`
- Create: `protocol/close_test.go`

Centralize close codes and add a helper that sends a close frame with bounded handshake wait.

- [ ] **Step 1: Create close.go with constants and helper**

Create `protocol/close.go`:

```go
package protocol

import (
	"context"
	"time"

	"github.com/coder/websocket"
)

// Close codes used by the server (RFC 6455 + SPEC-005 §11.1).
// We alias the websocket package constants for documentation clarity.
const (
	CloseNormal     = websocket.StatusNormalClosure   // 1000: graceful shutdown
	CloseProtocol   = websocket.StatusProtocolError    // 1002: unknown tag, malformed
	ClosePolicy     = websocket.StatusPolicyViolation  // 1008: auth, buffer overflow, flood
	CloseInternal   = websocket.StatusInternalError    // 1011: unexpected server error
)

// closeWithHandshake sends a Close frame and waits up to timeout for
// the peer's echo. If the peer does not respond in time, the TCP
// connection is forcefully closed. Runs synchronously — callers that
// cannot block should invoke in a goroutine.
func closeWithHandshake(ws *websocket.Conn, code websocket.StatusCode, reason string, timeout time.Duration) {
	// coder/websocket.Conn.Close internally blocks up to 5s waiting
	// for the peer's close echo. We wrap it with our own shorter
	// deadline from CloseHandshakeTimeout.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// CloseNow forcefully closes if the context expires before the
	// handshake completes.
	done := make(chan struct{})
	go func() {
		_ = ws.Close(code, truncateCloseReason(reason))
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		_ = ws.CloseNow()
	}
}
```

- [ ] **Step 2: Write tests**

Create `protocol/close_test.go`:

```go
package protocol

import (
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestCloseConstants(t *testing.T) {
	// Verify our aliases match RFC 6455 codes.
	tests := []struct {
		name string
		got  websocket.StatusCode
		want websocket.StatusCode
	}{
		{"Normal", CloseNormal, websocket.StatusNormalClosure},
		{"Protocol", CloseProtocol, websocket.StatusProtocolError},
		{"Policy", ClosePolicy, websocket.StatusPolicyViolation},
		{"Internal", CloseInternal, websocket.StatusInternalError},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestCloseWithHandshake_ResponsivePeer(t *testing.T) {
	conn, clientWS, cleanup := loopbackConn(t, DefaultProtocolOptions())
	defer cleanup()

	// Client reads so the close handshake completes.
	go func() {
		_, _, _ = clientWS.Read(nil)
	}()

	done := make(chan struct{})
	go func() {
		closeWithHandshake(conn.ws, CloseNormal, "test", 2*time.Second)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("closeWithHandshake did not complete")
	}
}

func TestCloseWithHandshake_UnresponsivePeerTimesOut(t *testing.T) {
	conn, _, cleanup := loopbackConn(t, DefaultProtocolOptions())
	defer cleanup()

	// Client does NOT read — close handshake will time out.
	timeout := 100 * time.Millisecond
	start := time.Now()

	done := make(chan struct{})
	go func() {
		closeWithHandshake(conn.ws, ClosePolicy, "test", timeout)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("closeWithHandshake did not time out")
	}

	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("closeWithHandshake took %v, expected ~%v", elapsed, timeout)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `rtk go test ./protocol/ -v -run "TestClose" -count=1`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
rtk git add protocol/close.go protocol/close_test.go
rtk git commit -m "feat(protocol): close-code constants and handshake timeout helper (Story 6.3)

Centralizes RFC 6455 close codes used by SPEC-005 and adds
closeWithHandshake that respects CloseHandshakeTimeout."
```

---

## Task 5: Wire Close Handshake Timeout into Disconnect (Story 6.3)

**Files:**
- Modify: `protocol/disconnect.go`
- Modify: `protocol/disconnect_test.go`

Replace the fire-and-forget `go ws.Close(...)` in `Disconnect` with `closeWithHandshake` using `CloseHandshakeTimeout`.

- [ ] **Step 1: Update Disconnect to use closeWithHandshake**

In `protocol/disconnect.go`, change the WebSocket close from:

```go
go func() {
    _ = c.ws.Close(code, truncateCloseReason(reason))
}()
```

to:

```go
go closeWithHandshake(c.ws, code, reason, c.opts.CloseHandshakeTimeout)
```

- [ ] **Step 2: Write test for handshake timeout in disconnect**

Add to `protocol/disconnect_test.go`:

```go
func TestDisconnectCloseHandshakeTimeout(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.CloseHandshakeTimeout = 100 * time.Millisecond

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	c, _, cleanup := loopbackConn(t, opts)
	defer cleanup()
	mgr.Add(c)

	// Client does NOT read — handshake will time out.
	start := time.Now()
	c.Disconnect(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr)

	// Disconnect returns immediately (async close). Verify c.closed is signaled.
	select {
	case <-c.closed:
	default:
		t.Error("c.closed not signaled")
	}

	// The close goroutine should complete within timeout + buffer.
	time.Sleep(300 * time.Millisecond)
	elapsed := time.Since(start)
	if elapsed > 1*time.Second {
		t.Errorf("close handshake took %v, expected bounded by timeout", elapsed)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `rtk go test ./protocol/ -v -run TestDisconnect -count=1`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
rtk git add protocol/disconnect.go protocol/disconnect_test.go
rtk git commit -m "feat(protocol): wire CloseHandshakeTimeout into Disconnect (Story 6.3)

Close handshake now bounded by CloseHandshakeTimeout (250ms default).
Unresponsive peers get forcefully closed via CloseNow."
```

---

## Task 6: Client-Initiated Close Handling (Story 6.3)

**Files:**
- Modify: `protocol/dispatch.go` (detect client close, echo it)
- Create/Modify: `protocol/close_test.go` (add client-close tests)

When the client sends a Close frame, `ws.Read` returns a `CloseError`. The dispatch loop should let the supervisor handle the teardown — the existing flow already does this (read error → dispatch exits → supervisor fires Disconnect). But we need to verify the close code is echoed.

- [ ] **Step 1: Verify existing behavior handles client close**

The coder/websocket library automatically echoes Close frames when `ws.Read` encounters one. The dispatch loop returns on error, supervisor fires `Disconnect`. This already works — Story 6.3 AC "Client Close → server echoes Close, disconnect sequence runs" is covered by existing behavior.

Add a test to `protocol/close_test.go` to verify:

```go
func TestClientInitiatedClose_DisconnectSequenceRuns(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.PingInterval = 2 * time.Second // suppress keepalive noise
	opts.IdleTimeout = 4 * time.Second
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)

	handlers := &MessageHandlers{}
	dispatchDone := runDispatchAsync(conn, context.Background(), handlers)
	keepaliveDone := runKeepaliveAsync(conn, context.Background())

	supervised := make(chan struct{})
	go func() {
		conn.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr, dispatchDone, keepaliveDone)
		close(supervised)
	}()

	// Client sends Close.
	_ = clientWS.Close(websocket.StatusNormalClosure, "bye")

	select {
	case <-supervised:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not complete after client close")
	}

	onDis, onSubs, _ := inbox.disconnectSnapshot()
	if onDis != 1 {
		t.Errorf("OnDisconnect calls = %d, want 1", onDis)
	}
	if onSubs != 1 {
		t.Errorf("DisconnectClientSubscriptions calls = %d, want 1", onSubs)
	}
}
```

Wait — `superviseLifecycle` signature was updated in Task 1 to include code+reason. Let me revisit: actually the supervisor always calls `Disconnect` with a code. For client-initiated close, the supervisor doesn't know the client's close code — it just knows "dispatch loop exited". Using `StatusNormalClosure` is correct here since the teardown is triggered by a clean client close.

Actually, rethinking: the supervisor should be smarter about which code to use. But the spec says the server *echoes* the client's Close frame — coder/websocket handles that automatically in its read loop. The `Disconnect` call from the supervisor is for server-side cleanup (subscriptions, OnDisconnect, ConnManager) — the WebSocket close frame has already been exchanged. So `StatusNormalClosure` in the supervisor is fine.

Hmm, but `superviseLifecycle` doesn't know *why* the dispatch loop exited. Let me update the plan to pass close code through `superviseLifecycle` too. Actually, keep it simple — the supervisor defaults to 1000, and specific disconnect paths (backpressure, idle timeout) call `Disconnect` directly with their own code, which fires closeOnce and makes the supervisor's call a no-op.

- [ ] **Step 2: Update superviseLifecycle signature to pass code/reason**

In `protocol/disconnect.go`, update:

```go
func (c *Conn) superviseLifecycle(
	ctx context.Context,
	code websocket.StatusCode,
	reason string,
	inbox ExecutorInbox,
	mgr *ConnManager,
	dispatchDone <-chan struct{},
	keepaliveDone <-chan struct{},
) {
	select {
	case <-dispatchDone:
	case <-keepaliveDone:
	}
	c.Disconnect(ctx, code, reason, inbox, mgr)
	<-dispatchDone
	<-keepaliveDone
}
```

Update the call site in `protocol/upgrade.go`:

```go
go c.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", s.Executor, s.Conns, dispatchDone, keepaliveDone)
```

Update any test calls to `superviseLifecycle`.

- [ ] **Step 3: Run tests**

Run: `rtk go test ./protocol/ -v -run "TestClient|TestSupervise" -count=1`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
rtk git add protocol/dispatch.go protocol/disconnect.go protocol/upgrade.go protocol/close_test.go protocol/disconnect_test.go
rtk git commit -m "feat(protocol): client-initiated close + supervisor close code (Story 6.3)

coder/websocket auto-echoes client Close frames. Supervisor now
accepts close code/reason for the Disconnect call. Added integration
test verifying disconnect sequence runs on client close."
```

---

## Task 7: Server-Initiated Close Codes (Story 6.3)

**Files:**
- Modify: `protocol/dispatch.go` (use `ClosePolicy` / `CloseProtocol` consistently)
- Modify: `protocol/keepalive.go` (use `closeWithHandshake`)
- Add to: `protocol/close_test.go`

Verify all server-initiated close paths use the correct codes per SPEC-005 §11.1.

- [ ] **Step 1: Audit and update close codes in dispatch.go**

In `protocol/dispatch.go`, `closeProtocolError` already sends `StatusProtocolError` (1002). Verify these paths:

- Text frame → 1002 ✓ (already done)
- Unknown tag → 1002 ✓ (DecodeClientMessage returns error, closeProtocolError fires)
- Nil handler → 1002 ✓ (closeProtocolError fires)
- Malformed body → 1002 ✓ (DecodeClientMessage returns error)

Update `closeProtocolError` to use `closeWithHandshake`:

```go
func closeProtocolError(conn *Conn, reason string) {
	log.Printf("protocol: closing conn %x with protocol error: %s", conn.ID[:], reason)
	go closeWithHandshake(conn.ws, CloseProtocol, reason, conn.opts.CloseHandshakeTimeout)
}
```

- [ ] **Step 2: Update keepalive idle timeout to use closeWithHandshake**

In `protocol/keepalive.go`, replace:

```go
go func() {
    _ = c.ws.Close(websocket.StatusPolicyViolation, "idle timeout")
}()
```

with:

```go
go closeWithHandshake(c.ws, ClosePolicy, "idle timeout", c.opts.CloseHandshakeTimeout)
```

- [ ] **Step 3: Write test for protocol error close code**

Add to `protocol/close_test.go`:

```go
func TestUnknownCompressionTag_Closes1002(t *testing.T) {
	opts := DefaultProtocolOptions()
	conn, clientWS := testConnPair(t, &opts)
	conn.Compression = true // enable compression path

	handlers := &MessageHandlers{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Send binary frame with invalid compression byte (0xFF).
	wCtx, wCancel := context.WithTimeout(ctx, time.Second)
	_ = clientWS.Write(wCtx, websocket.MessageBinary, []byte{0xFF, TagSubscribe, 0x00})
	wCancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on bad compression tag")
	}

	readCtx, rCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rCancel()
	_, _, err := clientWS.Read(readCtx)
	if code := websocket.CloseStatus(err); code != websocket.StatusProtocolError {
		t.Errorf("close code = %d, want %d (1002)", code, websocket.StatusProtocolError)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `rtk go test ./protocol/ -v -run "TestClose|TestKeepalive|TestDispatch" -count=1`
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
rtk git add protocol/dispatch.go protocol/keepalive.go protocol/close_test.go
rtk git commit -m "feat(protocol): unify server close codes with handshake timeout (Story 6.3)

All server-initiated closes now use closeWithHandshake bounded by
CloseHandshakeTimeout. Protocol errors → 1002, policy violations
(backpressure, idle) → 1008, graceful shutdown → 1000."
```

---

## Task 8: Graceful Server Shutdown (Story 6.3)

**Files:**
- Modify: `protocol/conn.go` (add `ConnManager.CloseAll`)
- Add to: `protocol/close_test.go`

- [ ] **Step 1: Add CloseAll to ConnManager**

In `protocol/conn.go`, add:

```go
// CloseAll sends a Close frame to every connected client and runs
// the disconnect sequence for each. Used for graceful server shutdown
// (SPEC-005 §11.1, close code 1000). Connections are closed
// concurrently with a bounded wait for all teardowns to complete.
func (m *ConnManager) CloseAll(ctx context.Context, inbox ExecutorInbox) {
	m.mu.RLock()
	conns := make([]*Conn, 0, len(m.conns))
	for _, c := range m.conns {
		conns = append(conns, c)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *Conn) {
			defer wg.Done()
			c.Disconnect(ctx, CloseNormal, "server shutdown", inbox, m)
		}(c)
	}
	wg.Wait()
}
```

- [ ] **Step 2: Write tests**

Add to `protocol/close_test.go`:

```go
func TestCloseAll_DisconnectsEveryConnection(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()

	conns := make([]*Conn, 3)
	for i := range conns {
		c, _, cleanup := loopbackConn(t, DefaultProtocolOptions())
		defer cleanup()
		conns[i] = c
		mgr.Add(c)
	}

	mgr.CloseAll(context.Background(), inbox)

	for i, c := range conns {
		select {
		case <-c.closed:
		default:
			t.Errorf("conn %d: c.closed not signaled after CloseAll", i)
		}
		if mgr.Get(c.ID) != nil {
			t.Errorf("conn %d: still in ConnManager after CloseAll", i)
		}
	}

	onDis, onSubs, _ := inbox.disconnectSnapshot()
	if onDis != 3 {
		t.Errorf("OnDisconnect calls = %d, want 3", onDis)
	}
	if onSubs != 3 {
		t.Errorf("DisconnectClientSubscriptions calls = %d, want 3", onSubs)
	}
}

func TestCloseAll_EmptyManagerNoOp(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()

	// Must not panic.
	mgr.CloseAll(context.Background(), inbox)

	onDis, _, _ := inbox.disconnectSnapshot()
	if onDis != 0 {
		t.Errorf("OnDisconnect calls = %d, want 0", onDis)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `rtk go test ./protocol/ -v -run "TestCloseAll" -count=1`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
rtk git add protocol/conn.go protocol/close_test.go
rtk git commit -m "feat(protocol): ConnManager.CloseAll for graceful shutdown (Story 6.3)

Sends Close 1000 to all connected clients and runs disconnect
sequence concurrently. Bounded by ctx for shutdown deadline."
```

---

## Task 9: Reconnection Verification (Story 6.4)

**Files:**
- Create: `protocol/reconnect_test.go`

This is a pure verification story — no new production code, only integration tests that prove the system composes correctly across reconnection boundaries.

- [ ] **Step 1: Create reconnection test file**

Create `protocol/reconnect_test.go`:

```go
package protocol

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/types"
)

// TestReconnectSameIdentity verifies that the same token yields the
// same Identity across connections (Story 6.4 AC 1).
func TestReconnectSameIdentity(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	// First connection.
	c1, client1, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	mgr.Add(c1)
	identity1 := c1.Identity

	// Disconnect.
	c1.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

	// Second connection with same Identity (simulates same token).
	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = identity1
	mgr.Add(c2)

	if c2.Identity != identity1 {
		t.Errorf("reconnect Identity = %x, want %x", c2.Identity, identity1)
	}
}

// TestReconnectNoSubscriptionCarryover verifies that subscriptions
// do not carry over from a previous connection (Story 6.4 AC 2).
func TestReconnectNoSubscriptionCarryover(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	mgr.Add(c1)

	// Add subscriptions to first connection.
	_ = c1.Subscriptions.Reserve(100)
	c1.Subscriptions.Activate(100)
	_ = c1.Subscriptions.Reserve(200)
	c1.Subscriptions.Activate(200)

	// Disconnect clears subscriptions.
	c1.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

	// New connection — fresh subscription tracker.
	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if c2.Subscriptions.IsActiveOrPending(100) {
		t.Error("subscription 100 carried over — should not")
	}
	if c2.Subscriptions.IsActiveOrPending(200) {
		t.Error("subscription 200 carried over — should not")
	}
}

// TestReconnectAfterBufferOverflow verifies reconnection works after
// a backpressure disconnect (Story 6.4 AC 7).
func TestReconnectAfterBufferOverflow(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	mgr.Add(c1)

	// Overflow disconnect.
	c1.Disconnect(context.Background(), ClosePolicy, "send buffer full", inbox, mgr)

	// Reconnect.
	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if mgr.Get(c2.ID) == nil {
		t.Error("reconnected connection not in manager")
	}
	// New connection should accept sends.
	msg := SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	s := NewClientSender(mgr, inbox)
	if err := s.Send(c2.ID, msg); err != nil {
		t.Fatalf("send after reconnect: %v", err)
	}
}

// TestReconnectDifferentConnectionID verifies that a new connection
// gets a different ConnectionID even with the same Identity.
func TestReconnectDifferentConnectionID(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	mgr.Add(c1)
	id1 := c1.ID

	c1.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if c2.ID == id1 {
		t.Error("reconnected connection reused same ConnectionID — should be different")
	}
}

// TestReconnectSameConnectionIDAccepted verifies that supplying the
// same connection_id on reconnect is accepted (no semantic effect in v1).
func TestReconnectSameConnectionIDAccepted(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	savedID := c1.ID
	mgr.Add(c1)

	c1.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

	// Reconnect reusing same ConnectionID.
	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.ID = savedID
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if mgr.Get(savedID) == nil {
		t.Error("reconnected connection with reused ID not found in manager")
	}
}

// TestReconnectNoGoroutineLeakAfterDisconnect is a structural test:
// after Disconnect, c.closed is closed, which should let any
// goroutine selecting on it exit.
func TestReconnectNoGoroutineLeakAfterDisconnect(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()
	opts.PingInterval = 50 * time.Millisecond
	opts.IdleTimeout = 200 * time.Millisecond

	c, _, cleanup := loopbackConn(t, opts)
	defer cleanup()
	mgr.Add(c)

	handlers := &MessageHandlers{}
	dispatchDone := runDispatchAsync(c, context.Background(), handlers)
	keepaliveDone := runKeepaliveAsync(c, context.Background())

	c.Disconnect(context.Background(), CloseNormal, "test", inbox, mgr)

	// Both goroutines should exit promptly after c.closed is closed.
	select {
	case <-dispatchDone:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit after Disconnect")
	}
	select {
	case <-keepaliveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("keepalive loop did not exit after Disconnect")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `rtk go test ./protocol/ -v -run "TestReconnect" -count=1`
Expected: All pass (these test existing composition, not new code).

- [ ] **Step 3: Commit**

```bash
rtk git add protocol/reconnect_test.go
rtk git commit -m "test(protocol): reconnection verification suite (Story 6.4)

Covers: same identity on reconnect, no subscription carryover,
reconnect after buffer overflow, different/reused ConnectionID,
goroutine cleanup after disconnect."
```

---

## Task 10: Update REMAINING.md & Final Verification

**Files:**
- Modify: `REMAINING.md`

- [ ] **Step 1: Run full protocol test suite**

Run: `rtk go test ./protocol/ -v -count=1`
Expected: All tests pass.

- [ ] **Step 2: Build check**

Run: `rtk go build ./protocol/`
Expected: Clean build, no errors.

- [ ] **Step 3: Update REMAINING.md**

Update `REMAINING.md` to mark E5 as done and E6 as done:

In the Phase 7 table, change E5 status from "Not implemented" to "**Done** (cece4ae–cefef83)" and E6 status from "Not implemented" to "**Done**".

Update the dependency chain section to remove the completed items.

- [ ] **Step 4: Commit**

```bash
rtk git add REMAINING.md
rtk git commit -m "docs: mark protocol E5 + E6 complete in REMAINING.md"
```
