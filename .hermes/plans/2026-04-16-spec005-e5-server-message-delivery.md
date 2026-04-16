# SPEC-005 Epic 5: Server Message Delivery — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the ClientSender interface, per-connection outbound write loop, and delivery helpers for all server→client message types.

**Architecture:** `ClientSender` wraps `ConnManager` and provides typed send methods. A per-connection `runOutboundWriter` goroutine drains `OutboundCh` → WebSocket. Response helpers (`SendSubscribeApplied`, etc.) coordinate subscription state transitions with delivery. `DeliverTransactionUpdate` and `DeliverReducerCallResult` translate fan-out payloads into wire messages.

**Tech Stack:** Go, `coder/websocket`, existing protocol codec/compression layer.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `protocol/sender.go` | `ClientSender` interface, `connManagerSender` impl, `ErrClientBufferFull`, `ErrConnNotFound` |
| `protocol/sender_test.go` | Unit tests for sender: encode+enqueue, compression, buffer-full, missing conn |
| `protocol/outbound.go` | `Conn.runOutboundWriter` goroutine — drain `OutboundCh` → ws.Write |
| `protocol/outbound_test.go` | Writer goroutine: FIFO order, exit on close, no leak |
| `protocol/send_responses.go` | `SendSubscribeApplied`, `SendUnsubscribeApplied`, `SendSubscriptionError`, `SendOneOffQueryResult` |
| `protocol/send_responses_test.go` | Subscription state transitions + delivery for each response type |
| `protocol/send_txupdate.go` | `DeliverTransactionUpdate` — fan-out→wire translation |
| `protocol/send_txupdate_test.go` | Single/multi conn delivery, skip disconnected, skip empty, buffer-full |
| `protocol/send_reducer_result.go` | `DeliverReducerCallResult` — caller-delta diversion + standalone delivery |
| `protocol/send_reducer_result_test.go` | Diversion logic, failed reducer, concurrent callers |

Existing files modified:
- `protocol/upgrade.go` — spawn `runOutboundWriter` alongside dispatch/keepalive goroutines

---

## Task 1: ClientSender interface + connManagerSender

**Files:**
- Create: `protocol/sender.go`
- Create: `protocol/sender_test.go`

- [ ] **Step 1: Write failing tests for sender**

```go
// sender_test.go
package protocol

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func testConn(compression bool) (*Conn, types.ConnectionID) {
	id := types.ConnectionID{1}
	opts := DefaultProtocolOptions()
	c := &Conn{
		ID:            id,
		Compression:   compression,
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, opts.OutgoingBufferMessages),
		opts:          &opts,
		closed:        make(chan struct{}),
	}
	return c, id
}

func TestSendEnqueuesFrame(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	msg := SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	if err := s.Send(id, msg); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-c.OutboundCh:
		if len(frame) == 0 {
			t.Fatal("empty frame")
		}
	default:
		t.Fatal("no frame enqueued")
	}
}

func TestSendWithCompressionWrapsEnvelope(t *testing.T) {
	c, id := testConn(true)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	msg := SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	if err := s.Send(id, msg); err != nil {
		t.Fatal(err)
	}
	frame := <-c.OutboundCh
	// Compression-enabled frames start with compression byte (0x00 or 0x01).
	if frame[0] != CompressionNone && frame[0] != CompressionGzip {
		t.Fatalf("expected compression envelope, got first byte %d", frame[0])
	}
}

func TestSendConnNotFound(t *testing.T) {
	mgr := NewConnManager()
	s := NewClientSender(mgr)
	id := types.ConnectionID{99}
	err := s.Send(id, SubscribeApplied{})
	if err == nil {
		t.Fatal("expected error for missing conn")
	}
}

func TestSendBufferFull(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	id := types.ConnectionID{1}
	c := &Conn{
		ID:            id,
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 1),
		opts:          &opts,
		closed:        make(chan struct{}),
	}
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	// Fill buffer.
	msg := SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	_ = s.Send(id, msg)

	// Second send should return buffer-full.
	err := s.Send(id, msg)
	if err != ErrClientBufferFull {
		t.Fatalf("expected ErrClientBufferFull, got %v", err)
	}
}

func TestSendTransactionUpdateTyped(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	update := &TransactionUpdate{TxID: 42, Updates: []SubscriptionUpdate{
		{SubscriptionID: 1, TableName: "t", Inserts: []byte{1}, Deletes: []byte{}},
	}}
	if err := s.SendTransactionUpdate(id, update); err != nil {
		t.Fatal(err)
	}
	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	tu, ok := msg.(TransactionUpdate)
	if !ok {
		t.Fatalf("expected TransactionUpdate, got %T", msg)
	}
	if tu.TxID != 42 {
		t.Fatalf("TxID = %d, want 42", tu.TxID)
	}
}

func TestSendReducerResultTyped(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	result := &ReducerCallResult{RequestID: 5, Status: 0, TxID: 99}
	if err := s.SendReducerResult(id, result); err != nil {
		t.Fatal(err)
	}
	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	rcr, ok := msg.(ReducerCallResult)
	if !ok {
		t.Fatalf("expected ReducerCallResult, got %T", msg)
	}
	if rcr.RequestID != 5 {
		t.Fatalf("RequestID = %d, want 5", rcr.RequestID)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestSend' -v -count=1`
Expected: compilation failure — `NewClientSender` not defined.

- [ ] **Step 3: Implement sender**

```go
// sender.go
package protocol

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/types"
)

// ErrClientBufferFull is returned when a non-blocking send to a
// connection's OutboundCh finds the channel full. The caller should
// trigger a disconnect (Epic 6).
var ErrClientBufferFull = errors.New("protocol: client outbound buffer full")

// ErrConnNotFound is returned when the target ConnectionID is not in
// the ConnManager (client disconnected between evaluation and delivery).
var ErrConnNotFound = errors.New("protocol: connection not found")

// ClientSender is the cross-subsystem contract for delivering server
// messages to connected clients (SPEC-005 §13). The fan-out worker
// (SPEC-004 E6) and executor response paths call these methods.
type ClientSender interface {
	// Send encodes msg and enqueues the frame on the connection's
	// outbound channel. Used for direct response messages
	// (SubscribeApplied, UnsubscribeApplied, SubscriptionError,
	// OneOffQueryResult).
	Send(connID types.ConnectionID, msg any) error
	// SendTransactionUpdate delivers a TransactionUpdate to one connection.
	SendTransactionUpdate(connID types.ConnectionID, update *TransactionUpdate) error
	// SendReducerResult delivers a ReducerCallResult to the calling connection.
	SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error
}

// NewClientSender returns a ClientSender backed by mgr for connection
// lookup and frame delivery.
func NewClientSender(mgr *ConnManager) ClientSender {
	return &connManagerSender{mgr: mgr}
}

type connManagerSender struct {
	mgr *ConnManager
}

func (s *connManagerSender) Send(connID types.ConnectionID, msg any) error {
	return s.enqueue(connID, msg)
}

func (s *connManagerSender) SendTransactionUpdate(connID types.ConnectionID, update *TransactionUpdate) error {
	return s.enqueue(connID, *update)
}

func (s *connManagerSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	return s.enqueue(connID, *result)
}

// enqueue encodes msg, wraps it in the connection's compression
// envelope, and does a non-blocking send to OutboundCh.
func (s *connManagerSender) enqueue(connID types.ConnectionID, msg any) error {
	conn := s.mgr.Get(connID)
	if conn == nil {
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	}

	frame, err := EncodeServerMessage(msg)
	if err != nil {
		return fmt.Errorf("encode server message: %w", err)
	}

	wrapped := EncodeFrame(frame[0], frame[1:], conn.Compression, CompressionNone)

	select {
	case conn.OutboundCh <- wrapped:
		return nil
	default:
		return fmt.Errorf("%w: %x", ErrClientBufferFull, connID[:])
	}
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestSend' -v -count=1`
Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```
feat(protocol): add ClientSender interface and connManagerSender impl

Story 5.1 — encode, compress, non-blocking enqueue to OutboundCh.
```

---

## Task 2: Per-connection outbound writer goroutine

**Files:**
- Create: `protocol/outbound.go`
- Create: `protocol/outbound_test.go`
- Modify: `protocol/upgrade.go` — spawn writer in HandleSubscribe lifecycle

- [ ] **Step 1: Write failing tests for outbound writer**

```go
// outbound_test.go
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

func TestOutboundWriterDeliversFrames(t *testing.T) {
	// Set up a real WebSocket pair so ws.Write works.
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
		// Keep handler alive until test completes.
		<-r.Context().Done()
	}))
	defer srv.Close()

	clientConn, _, err := websocket.Dial(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.CloseNow()
	<-ready

	opts := DefaultProtocolOptions()
	c := &Conn{
		OutboundCh: make(chan []byte, 8),
		ws:         serverConn,
		opts:       &opts,
		closed:     make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		c.runOutboundWriter(ctx)
		wg.Done()
	}()

	// Enqueue two frames.
	c.OutboundCh <- []byte{0x01, 0x02}
	c.OutboundCh <- []byte{0x03, 0x04}

	// Read them on the client side.
	for _, want := range [][]byte{{0x01, 0x02}, {0x03, 0x04}} {
		_, got, err := clientConn.Read(context.Background())
		if err != nil {
			t.Fatalf("client read: %v", err)
		}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	cancel()
	wg.Wait()
}

func TestOutboundWriterExitsOnClose(t *testing.T) {
	opts := DefaultProtocolOptions()
	c := &Conn{
		OutboundCh: make(chan []byte, 8),
		opts:       &opts,
		closed:     make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		c.runOutboundWriter(context.Background())
		close(done)
	}()

	// Close OutboundCh to signal shutdown.
	close(c.OutboundCh)

	select {
	case <-done:
		// OK — exited.
	case <-time.After(2 * time.Second):
		t.Fatal("writer goroutine did not exit after OutboundCh closed")
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestOutbound' -v -count=1`
Expected: compilation failure — `runOutboundWriter` not defined.

- [ ] **Step 3: Implement outbound writer**

```go
// outbound.go
package protocol

import (
	"context"
	"log"

	"github.com/coder/websocket"
)

// runOutboundWriter drains OutboundCh and writes each frame to the
// WebSocket as a binary message. Exits when OutboundCh is closed
// (disconnect teardown closes it), ctx is cancelled, or a write error
// occurs.
//
// FIFO order is guaranteed by the channel: frames enqueued first are
// dequeued and written first.
func (c *Conn) runOutboundWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-c.OutboundCh:
			if !ok {
				return
			}
			if err := c.ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
				log.Printf("protocol: outbound write failed for conn %x: %v", c.ID[:], err)
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestOutbound' -v -count=1`
Expected: PASS.

- [ ] **Step 5: Wire outbound writer into HandleSubscribe lifecycle**

In `protocol/upgrade.go`, in the `HandleSubscribe` method where goroutines are spawned (after `RunLifecycle` succeeds), add a goroutine for the outbound writer:

Find the block that spawns dispatch + keepalive + supervisor goroutines and add the writer:

```go
		// existing:
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
		// NEW: outbound writer goroutine drains OutboundCh → WebSocket.
		go c.runOutboundWriter(context.Background())
		go c.superviseLifecycle(context.Background(), s.Executor, s.Conns, dispatchDone, keepaliveDone)
```

The writer exits when `OutboundCh` is closed during `Disconnect`, which happens when the supervisor fires. No additional signal channel needed.

- [ ] **Step 6: Run full protocol test suite**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -v -count=1`
Expected: all existing + new tests PASS.

- [ ] **Step 7: Commit**

```
feat(protocol): add per-connection outbound writer goroutine

Story 5.1 — runOutboundWriter drains OutboundCh to WebSocket.
Wired into HandleSubscribe lifecycle.
```

---

## Task 3: Response message delivery helpers

**Files:**
- Create: `protocol/send_responses.go`
- Create: `protocol/send_responses_test.go`

- [ ] **Step 1: Write failing tests**

```go
// send_responses_test.go
package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestSendSubscribeAppliedActivatesSubscription(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(10)

	msg := &SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	if err := SendSubscribeApplied(s, c, msg); err != nil {
		t.Fatal(err)
	}

	if !c.Subscriptions.IsActive(10) {
		t.Fatal("subscription 10 should be active")
	}

	// Frame should be enqueued.
	select {
	case <-c.OutboundCh:
	default:
		t.Fatal("no frame enqueued")
	}
}

func TestSendSubscribeAppliedDiscardsAfterDisconnect(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(10)
	// Simulate disconnect: remove subscription before delivery.
	c.Subscriptions.Remove(10)

	msg := &SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	err := SendSubscribeApplied(s, c, msg)
	if err != nil {
		t.Fatal("should silently discard, not error")
	}
	// No frame should be sent.
	select {
	case <-c.OutboundCh:
		t.Fatal("frame should not be enqueued for removed subscription")
	default:
	}
}

func TestSendUnsubscribeAppliedRemovesSubscription(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(10)
	c.Subscriptions.Activate(10)

	msg := &UnsubscribeApplied{RequestID: 1, SubscriptionID: 10}
	if err := SendUnsubscribeApplied(s, c, msg); err != nil {
		t.Fatal(err)
	}

	if c.Subscriptions.IsActiveOrPending(10) {
		t.Fatal("subscription 10 should be removed")
	}
}

func TestSendSubscriptionErrorReleasesID(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(10)

	msg := &SubscriptionError{RequestID: 1, SubscriptionID: 10, Error: "bad predicate"}
	if err := SendSubscriptionError(s, c, msg); err != nil {
		t.Fatal(err)
	}

	if c.Subscriptions.IsActiveOrPending(10) {
		t.Fatal("subscription 10 should be released")
	}

	// ID should be immediately reusable.
	if err := c.Subscriptions.Reserve(10); err != nil {
		t.Fatalf("subscription 10 should be reusable: %v", err)
	}
}

func TestSendOneOffQueryResult(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	msg := &OneOffQueryResult{RequestID: 7, Status: 0, Rows: []byte{0x01}}
	if err := SendOneOffQueryResult(s, id, msg); err != nil {
		t.Fatal(err)
	}

	frame := <-c.OutboundCh
	_, decoded, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := decoded.(OneOffQueryResult)
	if !ok {
		t.Fatalf("expected OneOffQueryResult, got %T", decoded)
	}
	if result.RequestID != 7 {
		t.Fatalf("RequestID = %d, want 7", result.RequestID)
	}
}
```

Note: `testConn` is already defined in `sender_test.go` (same package), so these tests share it.

- [ ] **Step 2: Run tests, confirm failure**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestSendSubscribeApplied|TestSendUnsubscribe|TestSendSubscriptionError|TestSendOneOff' -v -count=1`
Expected: compilation failure — `SendSubscribeApplied` not defined.

- [ ] **Step 3: Implement response delivery helpers**

```go
// send_responses.go
package protocol

import "github.com/ponchione/shunter/types"

// SendSubscribeApplied delivers a SubscribeApplied message and
// transitions the subscription from pending → active. If the
// subscription was already removed (disconnect race), the result is
// silently discarded per SPEC-005 §9.1.
func SendSubscribeApplied(sender ClientSender, conn *Conn, msg *SubscribeApplied) error {
	if !conn.Subscriptions.IsActiveOrPending(msg.SubscriptionID) {
		return nil
	}
	if err := sender.Send(conn.ID, *msg); err != nil {
		return err
	}
	conn.Subscriptions.Activate(msg.SubscriptionID)
	return nil
}

// SendUnsubscribeApplied delivers an UnsubscribeApplied message and
// removes the subscription from the tracker.
func SendUnsubscribeApplied(sender ClientSender, conn *Conn, msg *UnsubscribeApplied) error {
	_ = conn.Subscriptions.Remove(msg.SubscriptionID)
	return sender.Send(conn.ID, *msg)
}

// SendSubscriptionError delivers a SubscriptionError and releases the
// subscription_id so it is immediately reusable (SPEC-005 §8.4).
func SendSubscriptionError(sender ClientSender, conn *Conn, msg *SubscriptionError) error {
	_ = conn.Subscriptions.Remove(msg.SubscriptionID)
	return sender.Send(conn.ID, *msg)
}

// SendOneOffQueryResult delivers a OneOffQueryResult. No subscription
// state change.
func SendOneOffQueryResult(sender ClientSender, connID types.ConnectionID, msg *OneOffQueryResult) error {
	return sender.Send(connID, *msg)
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestSendSubscribeApplied|TestSendUnsubscribe|TestSendSubscriptionError|TestSendOneOff' -v -count=1`
Expected: all PASS.

- [ ] **Step 5: Commit**

```
feat(protocol): add response message delivery helpers

Story 5.2 — SubscribeApplied/UnsubscribeApplied/SubscriptionError/
OneOffQueryResult delivery with subscription state transitions.
```

---

## Task 4: TransactionUpdate delivery

**Files:**
- Create: `protocol/send_txupdate.go`
- Create: `protocol/send_txupdate_test.go`

- [ ] **Step 1: Write failing tests**

```go
// send_txupdate_test.go
package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestDeliverTransactionUpdateSingleConn(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(1)
	c.Subscriptions.Activate(1)

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {
			{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}, Deletes: []byte{}},
		},
	}

	errs := DeliverTransactionUpdate(s, mgr, 42, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	tu := msg.(TransactionUpdate)
	if tu.TxID != 42 {
		t.Fatalf("TxID = %d, want 42", tu.TxID)
	}
	if len(tu.Updates) != 1 {
		t.Fatalf("Updates len = %d, want 1", len(tu.Updates))
	}
}

func TestDeliverTransactionUpdateMultiConn(t *testing.T) {
	c1, id1 := testConn(false)
	c2 := &Conn{
		ID:            types.ConnectionID{2},
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 256),
		opts:          c1.opts,
		closed:        make(chan struct{}),
	}
	id2 := c2.ID
	mgr := NewConnManager()
	mgr.Add(c1)
	mgr.Add(c2)
	s := NewClientSender(mgr)

	c1.Subscriptions.Reserve(1)
	c1.Subscriptions.Activate(1)
	c2.Subscriptions.Reserve(2)
	c2.Subscriptions.Activate(2)

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id1: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
		id2: {{SubscriptionID: 2, TableName: "t", Inserts: []byte{0x02}}},
	}

	errs := DeliverTransactionUpdate(s, mgr, 99, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Both connections should have a frame.
	select {
	case <-c1.OutboundCh:
	default:
		t.Fatal("c1 missing frame")
	}
	select {
	case <-c2.OutboundCh:
	default:
		t.Fatal("c2 missing frame")
	}
}

func TestDeliverTransactionUpdateSkipsDisconnected(t *testing.T) {
	mgr := NewConnManager()
	s := NewClientSender(mgr)

	// Connection not in manager — should be skipped.
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		{99}: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
	}

	errs := DeliverTransactionUpdate(s, mgr, 1, fanout)
	if len(errs) != 0 {
		t.Fatalf("disconnected conn should be skipped, not error: %v", errs)
	}
}

func TestDeliverTransactionUpdateSkipsEmptyUpdates(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {}, // empty
	}

	errs := DeliverTransactionUpdate(s, mgr, 1, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	select {
	case <-c.OutboundCh:
		t.Fatal("empty update should not send a frame")
	default:
	}
}

func TestDeliverTransactionUpdateBufferFull(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	id := types.ConnectionID{1}
	c := &Conn{
		ID:            id,
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 1),
		opts:          &opts,
		closed:        make(chan struct{}),
	}
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(1)
	c.Subscriptions.Activate(1)

	// Fill buffer.
	c.OutboundCh <- []byte{0xFF}

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
	}

	errs := DeliverTransactionUpdate(s, mgr, 1, fanout)
	if len(errs) != 1 {
		t.Fatalf("expected 1 buffer-full error, got %d", len(errs))
	}
	if !errors.Is(errs[0].Err, ErrClientBufferFull) {
		t.Fatalf("expected ErrClientBufferFull, got %v", errs[0].Err)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestDeliverTransaction' -v -count=1`
Expected: compilation failure — `DeliverTransactionUpdate` not defined.

- [ ] **Step 3: Implement TransactionUpdate delivery**

```go
// send_txupdate.go
package protocol

import "github.com/ponchione/shunter/types"

// DeliveryError pairs a connection ID with the error encountered
// during delivery. Used by callers to trigger disconnect for
// buffer-full connections.
type DeliveryError struct {
	ConnID types.ConnectionID
	Err    error
}

// DeliverTransactionUpdate sends a TransactionUpdate to every
// connection in fanout. Connections not found in the ConnManager are
// skipped (disconnected since evaluation). Empty update slices are
// skipped (no message sent). Buffer-full errors are collected and
// returned so the caller can trigger disconnects.
func DeliverTransactionUpdate(
	sender ClientSender,
	mgr *ConnManager,
	txID uint64,
	fanout map[types.ConnectionID][]SubscriptionUpdate,
) []DeliveryError {
	var errs []DeliveryError
	for connID, updates := range fanout {
		if len(updates) == 0 {
			continue
		}
		conn := mgr.Get(connID)
		if conn == nil {
			continue
		}
		msg := &TransactionUpdate{TxID: txID, Updates: updates}
		if err := sender.SendTransactionUpdate(connID, msg); err != nil {
			errs = append(errs, DeliveryError{ConnID: connID, Err: err})
		}
	}
	return errs
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestDeliverTransaction' -v -count=1`
Expected: all PASS.

- [ ] **Step 5: Commit**

```
feat(protocol): add TransactionUpdate delivery from fanout

Story 5.3 — DeliverTransactionUpdate translates CommitFanout entries
into per-connection wire messages. Skips disconnected/empty, collects
buffer-full errors.
```

---

## Task 5: ReducerCallResult delivery with caller-delta diversion

**Files:**
- Create: `protocol/send_reducer_result.go`
- Create: `protocol/send_reducer_result_test.go`

- [ ] **Step 1: Write failing tests**

```go
// send_reducer_result_test.go
package protocol

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestDeliverReducerResultEmbedsDelta(t *testing.T) {
	caller, callerID := testConn(false)
	other := &Conn{
		ID:            types.ConnectionID{2},
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 256),
		opts:          caller.opts,
		closed:        make(chan struct{}),
	}
	otherID := other.ID
	mgr := NewConnManager()
	mgr.Add(caller)
	mgr.Add(other)
	s := NewClientSender(mgr)

	caller.Subscriptions.Reserve(1)
	caller.Subscriptions.Activate(1)
	other.Subscriptions.Reserve(2)
	other.Subscriptions.Activate(2)

	result := &ReducerCallResult{RequestID: 5, Status: 0, TxID: 42}
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		callerID: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
		otherID:  {{SubscriptionID: 2, TableName: "t", Inserts: []byte{0x02}}},
	}

	errs := DeliverReducerCallResult(s, mgr, result, &callerID, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Caller should get ReducerCallResult with embedded updates.
	callerFrame := <-caller.OutboundCh
	_, callerMsg, err := DecodeServerMessage(callerFrame)
	if err != nil {
		t.Fatal(err)
	}
	rcr := callerMsg.(ReducerCallResult)
	if rcr.RequestID != 5 {
		t.Fatalf("RequestID = %d, want 5", rcr.RequestID)
	}
	if len(rcr.TransactionUpdate) != 1 {
		t.Fatalf("caller embedded updates = %d, want 1", len(rcr.TransactionUpdate))
	}

	// Other should get standalone TransactionUpdate.
	otherFrame := <-other.OutboundCh
	_, otherMsg, err := DecodeServerMessage(otherFrame)
	if err != nil {
		t.Fatal(err)
	}
	tu := otherMsg.(TransactionUpdate)
	if tu.TxID != 42 {
		t.Fatalf("other TxID = %d, want 42", tu.TxID)
	}

	// Caller should NOT have a second frame (no standalone TxUpdate).
	select {
	case <-caller.OutboundCh:
		t.Fatal("caller should not get standalone TransactionUpdate")
	default:
	}
}

func TestDeliverReducerResultFailedReducerEmptyUpdate(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	result := &ReducerCallResult{RequestID: 1, Status: 1, Error: "user error"}
	errs := DeliverReducerCallResult(s, mgr, result, &id, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	rcr := msg.(ReducerCallResult)
	if rcr.Status != 1 {
		t.Fatalf("Status = %d, want 1", rcr.Status)
	}
	if len(rcr.TransactionUpdate) != 0 {
		t.Fatal("failed reducer should have empty TransactionUpdate")
	}
}

func TestDeliverReducerResultNoCaller(t *testing.T) {
	// Non-reducer commit: no caller, just standalone delivery.
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(1)
	c.Subscriptions.Activate(1)

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
	}

	errs := DeliverReducerCallResult(s, mgr, nil, nil, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	// Should be TransactionUpdate, not ReducerCallResult.
	if _, ok := msg.(TransactionUpdate); !ok {
		t.Fatalf("expected TransactionUpdate for non-reducer commit, got %T", msg)
	}
}

func TestDeliverReducerResultCallerDisconnected(t *testing.T) {
	other := &Conn{
		ID:            types.ConnectionID{2},
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 256),
		opts: func() *ProtocolOptions {
			o := DefaultProtocolOptions()
			return &o
		}(),
		closed: make(chan struct{}),
	}
	mgr := NewConnManager()
	mgr.Add(other)
	s := NewClientSender(mgr)

	other.Subscriptions.Reserve(2)
	other.Subscriptions.Activate(2)

	callerID := types.ConnectionID{1} // not in manager
	result := &ReducerCallResult{RequestID: 5, Status: 0, TxID: 42}
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		callerID:  {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
		other.ID: {{SubscriptionID: 2, TableName: "t", Inserts: []byte{0x02}}},
	}

	errs := DeliverReducerCallResult(s, mgr, result, &callerID, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Other should still get TransactionUpdate.
	select {
	case <-other.OutboundCh:
	default:
		t.Fatal("other should have received TransactionUpdate")
	}
}

func TestDeliverReducerResultNotFoundStatus(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	result := &ReducerCallResult{RequestID: 1, Status: 3, TxID: 0, Error: "reducer not found"}
	errs := DeliverReducerCallResult(s, mgr, result, &id, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	rcr := msg.(ReducerCallResult)
	if rcr.Status != 3 {
		t.Fatalf("Status = %d, want 3", rcr.Status)
	}
	if rcr.TxID != 0 {
		t.Fatalf("TxID = %d, want 0 for not-found", rcr.TxID)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestDeliverReducer' -v -count=1`
Expected: compilation failure — `DeliverReducerCallResult` not defined.

- [ ] **Step 3: Implement ReducerCallResult delivery**

```go
// send_reducer_result.go
package protocol

import "github.com/ponchione/shunter/types"

// DeliverReducerCallResult handles the caller-delta diversion path
// (SPEC-005 §8.7). When a reducer commits:
//
//  1. The caller's subscription updates are extracted from fanout and
//     embedded in the ReducerCallResult.
//  2. The caller receives ReducerCallResult (not a standalone
//     TransactionUpdate).
//  3. All other connections receive standalone TransactionUpdate via
//     DeliverTransactionUpdate.
//
// When callerConnID is nil (non-reducer commit), result is ignored and
// all entries go through DeliverTransactionUpdate.
//
// When result.Status != 0 (failed/panic/not-found), the embedded
// TransactionUpdate is forced empty per SPEC-005 §8.7.
func DeliverReducerCallResult(
	sender ClientSender,
	mgr *ConnManager,
	result *ReducerCallResult,
	callerConnID *types.ConnectionID,
	fanout map[types.ConnectionID][]SubscriptionUpdate,
) []DeliveryError {
	if callerConnID == nil {
		// Non-reducer commit — pure standalone delivery.
		return DeliverTransactionUpdate(sender, mgr, result.TxID, fanout)
	}

	// Extract caller's updates from fanout before standalone delivery.
	callerUpdates := fanout[*callerConnID]
	delete(fanout, *callerConnID)

	var errs []DeliveryError

	// Deliver ReducerCallResult to caller.
	if result != nil {
		callerResult := *result
		if callerResult.Status == 0 {
			callerResult.TransactionUpdate = callerUpdates
		} else {
			callerResult.TransactionUpdate = nil
		}
		if err := sender.SendReducerResult(*callerConnID, &callerResult); err != nil {
			errs = append(errs, DeliveryError{ConnID: *callerConnID, Err: err})
		}
	}

	// Deliver standalone TransactionUpdate to everyone else.
	if result != nil && result.Status == 0 && len(fanout) > 0 {
		txErrs := DeliverTransactionUpdate(sender, mgr, result.TxID, fanout)
		errs = append(errs, txErrs...)
	}

	return errs
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -run 'TestDeliverReducer' -v -count=1`
Expected: all PASS.

- [ ] **Step 5: Run full protocol test suite**

Run: `cd /home/gernsback/source/shunter && rtk go test ./protocol/ -v -count=1`
Expected: all tests PASS.

- [ ] **Step 6: Commit**

```
feat(protocol): add ReducerCallResult delivery with caller-delta diversion

Story 5.4 — caller gets embedded updates in ReducerCallResult, other
connections get standalone TransactionUpdate. Failed/panic/not-found
reducers get empty embedded update.
```

---

## Verification

After all tasks complete:

- [ ] `rtk go test ./protocol/ -v -count=1` — full suite passes
- [ ] `rtk go test ./... -count=1` — no regressions in other packages
- [ ] `rtk go vet ./protocol/` — no vet warnings
