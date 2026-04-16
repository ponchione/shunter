# SPEC-004 E6: Fan-Out & Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the FanOutWorker goroutine that receives computed deltas from the evaluation loop and delivers them to connected clients through the protocol layer.

**Architecture:** FanOutWorker in subscription package reads FanOutMessages from a channel and delivers via a `FanOutSender` interface (defined in subscription, implemented by an adapter in protocol). The adapter handles encoding `subscription.SubscriptionUpdate` (ProductValue slices) to `protocol.SubscriptionUpdate` (wire-encoded bytes) and wraps `protocol.ClientSender`. This avoids an import cycle since protocol already imports subscription.

**Tech Stack:** Go, BSATN encoding, protocol RowList encoding, goroutines + channels

---

## Dependency Map

```
subscription/errors.go  ← Task 1 (add error sentinels)
subscription/fanout.go  ← Task 1 (add SubscriptionID to SubscriptionError)
subscription/eval.go    ← Task 1 (populate SubscriptionID in handleEvalError)
subscription/fanout_worker.go      ← Task 1, 3, 4, 5, 6, 7
subscription/fanout_worker_test.go ← Task 3, 4, 5, 6, 7, 8
protocol/fanout_adapter.go         ← Task 2
protocol/fanout_adapter_test.go    ← Task 2
subscription/manager.go             ← Task 8
```

## Import Constraint

**protocol → subscription** (already exists in handle_subscribe.go). **subscription → protocol is FORBIDDEN** (import cycle). The FanOutWorker uses a `FanOutSender` interface defined in subscription. The encoding adapter lives in protocol.

---

### Task 1: FanOutSender Interface + Error Sentinels + SubscriptionError Fix

**Files:**
- Create: `subscription/fanout_worker.go`
- Modify: `subscription/errors.go`
- Modify: `subscription/fanout.go`
- Modify: `subscription/eval.go`

- [ ] **Step 1: Add error sentinels to subscription/errors.go**

Add delivery error sentinels after the existing evaluation errors block:

```go
// Delivery errors (Story 6.1 / 6.3).
var (
	// ErrSendBufferFull — client outbound buffer is full, client should be dropped.
	ErrSendBufferFull = errors.New("subscription: client send buffer full")
	// ErrSendConnGone — connection not found, client already disconnected.
	ErrSendConnGone = errors.New("subscription: connection not found for delivery")
)
```

- [ ] **Step 2: Add SubscriptionID to SubscriptionError in fanout.go**

Change the existing `SubscriptionError` struct to include `SubscriptionID`:

```go
type SubscriptionError struct {
	SubscriptionID types.SubscriptionID
	QueryHash      QueryHash
	Predicate      string
	Message        string
}
```

- [ ] **Step 3: Update handleEvalError in eval.go to populate SubscriptionID**

Replace the existing `handleEvalError` method:

```go
func (m *Manager) handleEvalError(qs *queryState, err error, out map[types.ConnectionID][]SubscriptionError) {
	predRepr := fmt.Sprintf("%#v", qs.predicate)
	wrapped := fmt.Errorf("%w: %v", ErrSubscriptionEval, err)
	log.Printf("subscription: evaluation error for query %s predicate=%s: %v", qs.hash, predRepr, wrapped)
	for connID, subIDs := range qs.subscribers {
		for subID := range subIDs {
			out[connID] = append(out[connID], SubscriptionError{
				SubscriptionID: subID,
				QueryHash:      qs.hash,
				Predicate:      predRepr,
				Message:        wrapped.Error(),
			})
		}
		m.signalDropped(connID)
	}
}
```

- [ ] **Step 4: Create fanout_worker.go with FanOutSender interface**

Create `subscription/fanout_worker.go`:

```go
package subscription

import "github.com/ponchione/shunter/types"

// FanOutSender is the delivery contract used by the FanOutWorker to
// push encoded messages to connected clients. Implemented by a
// protocol-backed adapter wired at server startup (SPEC-004 §8 /
// Story 6.1).
//
// Errors: implementations must return ErrSendBufferFull when the
// client's outbound buffer is full, and ErrSendConnGone when the
// target connection has already disconnected.
type FanOutSender interface {
	// SendTransactionUpdate delivers a TransactionUpdate to one client.
	SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []SubscriptionUpdate) error
	// SendReducerResult delivers a ReducerCallResult to the caller client.
	SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error
	// SendSubscriptionError delivers a SubscriptionError to a client.
	SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error
}
```

- [ ] **Step 5: Run tests to verify nothing broke**

Run: `rtk go test ./subscription/... -count=1 -v 2>&1 | tail -5`
Expected: All 246 tests pass.

- [ ] **Step 6: Commit**

```bash
git add subscription/errors.go subscription/fanout.go subscription/eval.go subscription/fanout_worker.go
git commit -m "feat(subscription): FanOutSender interface and delivery error sentinels (Story 6.1)"
```

---

### Task 2: Encoding Adapter

**Files:**
- Create: `protocol/fanout_adapter.go`
- Create: `protocol/fanout_adapter_test.go`

- [ ] **Step 1: Write encoding tests in protocol/fanout_adapter_test.go**

```go
package protocol

import (
	"testing"

	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func TestEncodeSubscriptionUpdate_SingleInsertDelete(t *testing.T) {
	su := subscription.SubscriptionUpdate{
		SubscriptionID: 42,
		TableName:      "users",
		Inserts:        []types.ProductValue{{types.NewUint32(1)}},
		Deletes:        []types.ProductValue{{types.NewUint32(2)}},
	}
	pu, err := encodeSubscriptionUpdate(su)
	if err != nil {
		t.Fatal(err)
	}
	if pu.SubscriptionID != 42 {
		t.Fatalf("SubscriptionID = %d, want 42", pu.SubscriptionID)
	}
	if pu.TableName != "users" {
		t.Fatalf("TableName = %q, want %q", pu.TableName, "users")
	}
	// Decode RowLists to verify encoding
	insRows, err := DecodeRowList(pu.Inserts)
	if err != nil {
		t.Fatal(err)
	}
	if len(insRows) != 1 {
		t.Fatalf("Inserts row count = %d, want 1", len(insRows))
	}
	delRows, err := DecodeRowList(pu.Deletes)
	if err != nil {
		t.Fatal(err)
	}
	if len(delRows) != 1 {
		t.Fatalf("Deletes row count = %d, want 1", len(delRows))
	}
}

func TestEncodeSubscriptionUpdate_Empty(t *testing.T) {
	su := subscription.SubscriptionUpdate{
		SubscriptionID: 1,
		TableName:      "empty",
	}
	pu, err := encodeSubscriptionUpdate(su)
	if err != nil {
		t.Fatal(err)
	}
	insRows, err := DecodeRowList(pu.Inserts)
	if err != nil {
		t.Fatal(err)
	}
	if len(insRows) != 0 {
		t.Fatalf("Inserts row count = %d, want 0", len(insRows))
	}
}

func TestEncodeSubscriptionUpdate_MultiRow(t *testing.T) {
	su := subscription.SubscriptionUpdate{
		SubscriptionID: 7,
		TableName:      "data",
		Inserts: []types.ProductValue{
			{types.NewUint32(10), types.NewString("alice")},
			{types.NewUint32(20), types.NewString("bob")},
			{types.NewUint32(30), types.NewString("carol")},
		},
	}
	pu, err := encodeSubscriptionUpdate(su)
	if err != nil {
		t.Fatal(err)
	}
	insRows, err := DecodeRowList(pu.Inserts)
	if err != nil {
		t.Fatal(err)
	}
	if len(insRows) != 3 {
		t.Fatalf("Inserts row count = %d, want 3", len(insRows))
	}
}

func TestEncodeReducerCallResult_Committed(t *testing.T) {
	sr := &subscription.ReducerCallResult{
		RequestID: 5,
		Status:    0, // committed
		TxID:      types.TxID(100),
		Error:     "",
		Energy:    999, // should be zeroed by adapter
	}
	callerUpdates := []subscription.SubscriptionUpdate{{
		SubscriptionID: 1,
		TableName:      "t1",
		Inserts:        []types.ProductValue{{types.NewUint32(1)}},
	}}
	pr, err := encodeReducerCallResult(sr, callerUpdates)
	if err != nil {
		t.Fatal(err)
	}
	if pr.RequestID != 5 {
		t.Fatalf("RequestID = %d, want 5", pr.RequestID)
	}
	if pr.Status != 0 {
		t.Fatalf("Status = %d, want 0", pr.Status)
	}
	if pr.TxID != 100 {
		t.Fatalf("TxID = %d, want 100", pr.TxID)
	}
	if pr.Energy != 0 {
		t.Fatalf("Energy = %d, want 0 (v1)", pr.Energy)
	}
	if len(pr.TransactionUpdate) != 1 {
		t.Fatalf("TransactionUpdate len = %d, want 1", len(pr.TransactionUpdate))
	}
}

func TestEncodeReducerCallResult_Failed(t *testing.T) {
	sr := &subscription.ReducerCallResult{
		RequestID: 3,
		Status:    1, // failed
		TxID:      types.TxID(50),
		Error:     "panic",
	}
	callerUpdates := []subscription.SubscriptionUpdate{{
		SubscriptionID: 1,
		TableName:      "t1",
		Inserts:        []types.ProductValue{{types.NewUint32(1)}},
	}}
	pr, err := encodeReducerCallResult(sr, callerUpdates)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Status != 1 {
		t.Fatalf("Status = %d, want 1", pr.Status)
	}
	if pr.Error != "panic" {
		t.Fatalf("Error = %q, want %q", pr.Error, "panic")
	}
	// Failed status: TransactionUpdate forced empty per SPEC-005 §8.7
	if len(pr.TransactionUpdate) != 0 {
		t.Fatalf("TransactionUpdate len = %d, want 0 for failed status", len(pr.TransactionUpdate))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail (functions not defined)**

Run: `rtk go test ./protocol/... -run TestEncode -count=1 -v 2>&1 | tail -5`
Expected: FAIL — `encodeSubscriptionUpdate` undefined.

- [ ] **Step 3: Implement the encoding adapter in protocol/fanout_adapter.go**

```go
package protocol

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// FanOutSenderAdapter wraps a ClientSender to implement
// subscription.FanOutSender. Converts subscription-domain types to
// protocol wire format before delivery (SPEC-004 §8 / Story 6.1).
type FanOutSenderAdapter struct {
	sender ClientSender
}

// NewFanOutSenderAdapter creates an adapter backed by the given sender.
func NewFanOutSenderAdapter(sender ClientSender) *FanOutSenderAdapter {
	return &FanOutSenderAdapter{sender: sender}
}

func (a *FanOutSenderAdapter) SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []subscription.SubscriptionUpdate) error {
	encoded, err := encodeSubscriptionUpdates(updates)
	if err != nil {
		return fmt.Errorf("encode updates: %w", err)
	}
	msg := &TransactionUpdate{TxID: uint64(txID), Updates: encoded}
	return mapDeliveryError(a.sender.SendTransactionUpdate(connID, msg))
}

func (a *FanOutSenderAdapter) SendReducerResult(connID types.ConnectionID, result *subscription.ReducerCallResult) error {
	callerUpdates := result.TransactionUpdate
	pr, err := encodeReducerCallResult(result, callerUpdates)
	if err != nil {
		return fmt.Errorf("encode reducer result: %w", err)
	}
	return mapDeliveryError(a.sender.SendReducerResult(connID, pr))
}

func (a *FanOutSenderAdapter) SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error {
	return mapDeliveryError(a.sender.Send(connID, SubscriptionError{
		SubscriptionID: uint32(subID),
		Error:          message,
	}))
}

// mapDeliveryError translates protocol-layer errors to subscription-layer
// sentinels so the fan-out worker can react without importing protocol.
func mapDeliveryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrClientBufferFull) {
		return fmt.Errorf("%w: %v", subscription.ErrSendBufferFull, err)
	}
	if errors.Is(err, ErrConnNotFound) {
		return fmt.Errorf("%w: %v", subscription.ErrSendConnGone, err)
	}
	return err
}

// --- Encoding helpers ---

func encodeSubscriptionUpdates(updates []subscription.SubscriptionUpdate) ([]SubscriptionUpdate, error) {
	out := make([]SubscriptionUpdate, len(updates))
	for i, su := range updates {
		eu, err := encodeSubscriptionUpdate(su)
		if err != nil {
			return nil, err
		}
		out[i] = eu
	}
	return out, nil
}

func encodeSubscriptionUpdate(su subscription.SubscriptionUpdate) (SubscriptionUpdate, error) {
	inserts, err := encodeRows(su.Inserts)
	if err != nil {
		return SubscriptionUpdate{}, fmt.Errorf("encode inserts: %w", err)
	}
	deletes, err := encodeRows(su.Deletes)
	if err != nil {
		return SubscriptionUpdate{}, fmt.Errorf("encode deletes: %w", err)
	}
	return SubscriptionUpdate{
		SubscriptionID: uint32(su.SubscriptionID),
		TableName:      su.TableName,
		Inserts:        inserts,
		Deletes:        deletes,
	}, nil
}

func encodeRows(rows []types.ProductValue) ([]byte, error) {
	encoded := make([][]byte, len(rows))
	for i, row := range rows {
		var buf bytes.Buffer
		if err := bsatn.EncodeProductValue(&buf, row); err != nil {
			return nil, err
		}
		encoded[i] = buf.Bytes()
	}
	return EncodeRowList(encoded), nil
}

func encodeReducerCallResult(sr *subscription.ReducerCallResult, callerUpdates []subscription.SubscriptionUpdate) (*ReducerCallResult, error) {
	var encodedUpdates []SubscriptionUpdate
	if sr.Status == 0 {
		var err error
		encodedUpdates, err = encodeSubscriptionUpdates(callerUpdates)
		if err != nil {
			return nil, err
		}
	}
	return &ReducerCallResult{
		RequestID:         sr.RequestID,
		Status:            sr.Status,
		TxID:              uint64(sr.TxID),
		Error:             sr.Error,
		Energy:            0, // v1: always zero (SPEC-005 §8.7)
		TransactionUpdate: encodedUpdates,
	}, nil
}
```

- [ ] **Step 4: Run encoding tests**

Run: `rtk go test ./protocol/... -run TestEncode -count=1 -v 2>&1 | tail -10`
Expected: All 5 encoding tests pass.

- [ ] **Step 5: Write adapter integration test with mock ClientSender**

Add to `protocol/fanout_adapter_test.go`:

```go
type mockClientSender struct {
	mu      sync.Mutex
	calls   []senderCall
	sendErr error
}

type senderCall struct {
	method string
	connID types.ConnectionID
}

func (m *mockClientSender) Send(connID types.ConnectionID, msg any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "Send", connID: connID})
	return m.sendErr
}
func (m *mockClientSender) SendTransactionUpdate(connID types.ConnectionID, update *TransactionUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "SendTransactionUpdate", connID: connID})
	return m.sendErr
}
func (m *mockClientSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "SendReducerResult", connID: connID})
	return m.sendErr
}

func connID(b byte) types.ConnectionID {
	var id types.ConnectionID
	id[0] = b
	return id
}

func TestFanOutSenderAdapter_SendTransactionUpdate(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)

	err := adapter.SendTransactionUpdate(
		connID(1), types.TxID(100),
		[]subscription.SubscriptionUpdate{{
			SubscriptionID: 5,
			TableName:      "t1",
			Inserts:        []types.ProductValue{{types.NewUint32(42)}},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.calls) != 1 || mock.calls[0].method != "SendTransactionUpdate" {
		t.Fatalf("calls = %+v, want 1 SendTransactionUpdate", mock.calls)
	}
}

func TestFanOutSenderAdapter_BufferFull_MapsError(t *testing.T) {
	mock := &mockClientSender{sendErr: ErrClientBufferFull}
	adapter := NewFanOutSenderAdapter(mock)

	err := adapter.SendTransactionUpdate(
		connID(1), types.TxID(1),
		[]subscription.SubscriptionUpdate{{SubscriptionID: 1, TableName: "t"}},
	)
	if !errors.Is(err, subscription.ErrSendBufferFull) {
		t.Fatalf("err = %v, want ErrSendBufferFull", err)
	}
}

func TestFanOutSenderAdapter_ConnNotFound_MapsError(t *testing.T) {
	mock := &mockClientSender{sendErr: ErrConnNotFound}
	adapter := NewFanOutSenderAdapter(mock)

	err := adapter.SendTransactionUpdate(
		connID(1), types.TxID(1),
		[]subscription.SubscriptionUpdate{{SubscriptionID: 1, TableName: "t"}},
	)
	if !errors.Is(err, subscription.ErrSendConnGone) {
		t.Fatalf("err = %v, want ErrSendConnGone", err)
	}
}
```

(Add `"errors"` and `"sync"` to the import block at the top of the test file.)

- [ ] **Step 6: Run all adapter tests**

Run: `rtk go test ./protocol/... -run "TestEncode|TestFanOutSenderAdapter" -count=1 -v 2>&1 | tail -10`
Expected: All 8 tests pass.

- [ ] **Step 7: Run full protocol suite to verify no regressions**

Run: `rtk go test ./protocol/... -count=1 2>&1 | tail -3`
Expected: All 163+ tests pass.

- [ ] **Step 8: Commit**

```bash
git add protocol/fanout_adapter.go protocol/fanout_adapter_test.go
git commit -m "feat(protocol): FanOutSenderAdapter encoding bridge (Story 6.1/6.2)"
```

---

### Task 3: FanOutWorker Core Loop (Non-Caller Delivery)

**Files:**
- Modify: `subscription/fanout_worker.go`
- Create: `subscription/fanout_worker_test.go`

- [ ] **Step 1: Write test for basic non-caller delivery**

Create `subscription/fanout_worker_test.go`:

```go
package subscription

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

// mockFanOutSender records delivery calls for test assertions.
type mockFanOutSender struct {
	mu        sync.Mutex
	txCalls   []txCall
	resCalls  []resCall
	errCalls  []errCall
	sendErr   error // returned by all methods
}

type txCall struct {
	ConnID  types.ConnectionID
	TxID    types.TxID
	Updates []SubscriptionUpdate
}
type resCall struct {
	ConnID types.ConnectionID
	Result *ReducerCallResult
}
type errCall struct {
	ConnID types.ConnectionID
	SubID  types.SubscriptionID
	Msg    string
}

func (m *mockFanOutSender) SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []SubscriptionUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txCalls = append(m.txCalls, txCall{ConnID: connID, TxID: txID, Updates: updates})
	return m.sendErr
}
func (m *mockFanOutSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resCalls = append(m.resCalls, resCall{ConnID: connID, Result: result})
	return m.sendErr
}
func (m *mockFanOutSender) SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errCalls = append(m.errCalls, errCall{ConnID: connID, SubID: subID, Msg: message})
	return m.sendErr
}

func cid(b byte) types.ConnectionID {
	var id types.ConnectionID
	id[0] = b
	return id
}

func TestFanOutWorker_NonCallerDelivery(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	conn1, conn2 := cid(1), cid(2)
	inbox <- FanOutMessage{
		TxID: types.TxID(10),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1", Inserts: []types.ProductValue{{types.NewUint32(1)}}}},
			conn2: {{SubscriptionID: 2, TableName: "t2", Deletes: []types.ProductValue{{types.NewUint32(2)}}}},
		},
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 2 {
		t.Fatalf("txCalls = %d, want 2", len(mock.txCalls))
	}
	for _, c := range mock.txCalls {
		if c.TxID != 10 {
			t.Fatalf("TxID = %d, want 10", c.TxID)
		}
	}
	if len(mock.resCalls) != 0 {
		t.Fatalf("resCalls = %d, want 0 (no caller)", len(mock.resCalls))
	}
}

func TestFanOutWorker_ContextCancel(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit on context cancel")
	}
}

func TestFanOutWorker_ClosedInbox(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	done := make(chan struct{})
	go func() {
		w.Run(context.Background())
		close(done)
	}()

	close(inbox)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit on closed inbox")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker" -count=1 -v 2>&1 | tail -5`
Expected: FAIL — `NewFanOutWorker` undefined.

- [ ] **Step 3: Implement FanOutWorker core in fanout_worker.go**

Add to `subscription/fanout_worker.go` (below the existing FanOutSender interface):

```go
// FanOutWorker receives computed deltas from the evaluation loop and
// delivers them through the protocol layer. Runs on its own goroutine
// separate from the executor (SPEC-004 §8.1 / Story 6.1).
type FanOutWorker struct {
	inbox          <-chan FanOutMessage
	sender         FanOutSender
	confirmedReads map[types.ConnectionID]bool
	dropped        chan<- types.ConnectionID
}

// NewFanOutWorker creates a worker that reads from inbox and delivers
// via sender. Dropped client IDs are signaled on dropped (shared with
// the Manager's dropped channel so the executor drains one channel).
func NewFanOutWorker(inbox <-chan FanOutMessage, sender FanOutSender, dropped chan<- types.ConnectionID) *FanOutWorker {
	return &FanOutWorker{
		inbox:          inbox,
		sender:         sender,
		confirmedReads: make(map[types.ConnectionID]bool),
		dropped:        dropped,
	}
}

// Run is the main delivery loop. Blocks until ctx is cancelled or
// inbox is closed.
func (w *FanOutWorker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-w.inbox:
			if !ok {
				return
			}
			w.deliver(msg)
		}
	}
}

func (w *FanOutWorker) deliver(msg FanOutMessage) {
	// Deliver standalone TransactionUpdate to all connections.
	for connID, updates := range msg.Fanout {
		if err := w.sender.SendTransactionUpdate(connID, msg.TxID, updates); err != nil {
			w.handleSendError(connID, err)
		}
	}
}

func (w *FanOutWorker) handleSendError(connID types.ConnectionID, err error) {
	if errors.Is(err, ErrSendBufferFull) {
		w.markDropped(connID)
	} else if !errors.Is(err, ErrSendConnGone) {
		log.Printf("subscription: fanout delivery error for conn %x: %v", connID[:], err)
	}
}

func (w *FanOutWorker) markDropped(connID types.ConnectionID) {
	delete(w.confirmedReads, connID)
	select {
	case w.dropped <- connID:
	default:
		log.Printf("subscription: dropped client channel full, skipping conn %x", connID[:])
	}
}
```

(Add `"context"`, `"errors"`, and `"log"` to the import block.)

- [ ] **Step 4: Run tests**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker" -count=1 -v 2>&1 | tail -10`
Expected: All 3 tests pass.

- [ ] **Step 5: Run full subscription suite**

Run: `rtk go test ./subscription/... -count=1 2>&1 | tail -3`
Expected: All 246+ tests pass.

- [ ] **Step 6: Commit**

```bash
git add subscription/fanout_worker.go subscription/fanout_worker_test.go
git commit -m "feat(subscription): FanOutWorker core delivery loop (Story 6.1)"
```

---

### Task 4: Caller Diversion (ReducerCallResult Path)

**Files:**
- Modify: `subscription/fanout_worker.go`
- Modify: `subscription/fanout_worker_test.go`

- [ ] **Step 1: Write test for caller diversion**

Add to `subscription/fanout_worker_test.go`:

```go
func TestFanOutWorker_CallerDiversion(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller, other := cid(1), cid(2)
	callerResult := &ReducerCallResult{
		RequestID: 7,
		Status:    0,
		TxID:      types.TxID(20),
	}
	inbox <- FanOutMessage{
		TxID: types.TxID(20),
		Fanout: CommitFanout{
			caller: {{SubscriptionID: 1, TableName: "t1", Inserts: []types.ProductValue{{types.NewUint32(10)}}}},
			other:  {{SubscriptionID: 2, TableName: "t1", Inserts: []types.ProductValue{{types.NewUint32(20)}}}},
		},
		CallerConnID: &caller,
		CallerResult: callerResult,
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()

	// Caller gets ReducerCallResult, not TransactionUpdate.
	if len(mock.resCalls) != 1 {
		t.Fatalf("resCalls = %d, want 1", len(mock.resCalls))
	}
	if mock.resCalls[0].ConnID != caller {
		t.Fatalf("caller connID mismatch")
	}
	if mock.resCalls[0].Result.RequestID != 7 {
		t.Fatalf("RequestID = %d, want 7", mock.resCalls[0].Result.RequestID)
	}
	// Caller's updates embedded in the result
	if len(mock.resCalls[0].Result.TransactionUpdate) != 1 {
		t.Fatalf("caller TransactionUpdate len = %d, want 1", len(mock.resCalls[0].Result.TransactionUpdate))
	}

	// Other connection gets standalone TransactionUpdate.
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1", len(mock.txCalls))
	}
	if mock.txCalls[0].ConnID != other {
		t.Fatalf("non-caller connID mismatch")
	}
}

func TestFanOutWorker_CallerDiversion_FailedStatus(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller := cid(1)
	callerResult := &ReducerCallResult{
		RequestID: 3,
		Status:    1, // failed
		TxID:      types.TxID(30),
		Error:     "panic in reducer",
	}
	inbox <- FanOutMessage{
		TxID: types.TxID(30),
		Fanout: CommitFanout{
			caller: {{SubscriptionID: 1, TableName: "t1"}},
		},
		CallerConnID: &caller,
		CallerResult: callerResult,
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.resCalls) != 1 {
		t.Fatalf("resCalls = %d, want 1", len(mock.resCalls))
	}
	// Failed status: result delivered with error, no TransactionUpdate embedded.
	if mock.resCalls[0].Result.Status != 1 {
		t.Fatalf("Status = %d, want 1", mock.resCalls[0].Result.Status)
	}
	if mock.resCalls[0].Result.TransactionUpdate != nil {
		t.Fatalf("TransactionUpdate should be nil for failed status")
	}
	// Caller NOT in txCalls.
	if len(mock.txCalls) != 0 {
		t.Fatalf("txCalls = %d, want 0 (failed reducer, no standalone delivery)", len(mock.txCalls))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker_CallerDiversion" -count=1 -v 2>&1 | tail -5`
Expected: FAIL — caller not diverted, all connections get TransactionUpdate.

- [ ] **Step 3: Implement caller diversion in deliver()**

Replace the `deliver` method in `subscription/fanout_worker.go`:

```go
func (w *FanOutWorker) deliver(msg FanOutMessage) {
	// Extract caller if present — must happen before iterating fanout
	// so caller does NOT receive a standalone TransactionUpdate.
	var callerUpdates []SubscriptionUpdate
	if msg.CallerConnID != nil {
		callerUpdates = msg.Fanout[*msg.CallerConnID]
		delete(msg.Fanout, *msg.CallerConnID)
	}

	// Deliver standalone TransactionUpdate to non-caller connections.
	for connID, updates := range msg.Fanout {
		if err := w.sender.SendTransactionUpdate(connID, msg.TxID, updates); err != nil {
			w.handleSendError(connID, err)
		}
	}

	// Deliver ReducerCallResult to caller.
	if msg.CallerConnID != nil && msg.CallerResult != nil {
		result := *msg.CallerResult
		if result.Status == 0 {
			result.TransactionUpdate = callerUpdates
		} else {
			result.TransactionUpdate = nil
		}
		if err := w.sender.SendReducerResult(*msg.CallerConnID, &result); err != nil {
			w.handleSendError(*msg.CallerConnID, err)
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker" -count=1 -v 2>&1 | tail -10`
Expected: All 5 tests pass.

- [ ] **Step 5: Commit**

```bash
git add subscription/fanout_worker.go subscription/fanout_worker_test.go
git commit -m "feat(subscription): caller ReducerCallResult diversion (Story 6.2)"
```

---

### Task 5: Backpressure + Dropped Client Signaling

**Files:**
- Modify: `subscription/fanout_worker_test.go`

(Implementation already in Task 3 — handleSendError and markDropped. This task tests it.)

- [ ] **Step 1: Write backpressure tests**

Add to `subscription/fanout_worker_test.go`:

```go
func TestFanOutWorker_BufferFull_DropsClient(t *testing.T) {
	mock := &mockFanOutSender{sendErr: ErrSendBufferFull}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	conn1 := cid(1)
	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	// Dropped client should appear on channel.
	select {
	case id := <-dropped:
		if id != conn1 {
			t.Fatalf("dropped = %x, want %x", id[:], conn1[:])
		}
	case <-time.After(time.Second):
		t.Fatal("no dropped client signal")
	}
}

func TestFanOutWorker_ConnGone_Silent(t *testing.T) {
	mock := &mockFanOutSender{sendErr: ErrSendConnGone}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			cid(1): {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	// ConnGone should NOT appear on dropped channel.
	select {
	case id := <-dropped:
		t.Fatalf("unexpected dropped signal: %x", id[:])
	default:
	}
}

func TestFanOutWorker_MultipleSlowClients(t *testing.T) {
	callCount := 0
	mock := &mockFanOutSender{}
	// Override sendErr per-call: first call succeeds, second fails
	origSend := mock.SendTransactionUpdate
	_ = origSend // avoid unused

	// Use a sender that fails for specific connections.
	failConn := cid(2)
	sender := &selectiveFailSender{fail: failConn}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, sender, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			cid(1):   {{SubscriptionID: 1, TableName: "t1"}},
			failConn: {{SubscriptionID: 2, TableName: "t1"}},
			cid(3):   {{SubscriptionID: 3, TableName: "t1"}},
		},
	}

	// Only failConn should be dropped.
	select {
	case id := <-dropped:
		if id != failConn {
			t.Fatalf("dropped = %x, want %x", id[:], failConn[:])
		}
	case <-time.After(time.Second):
		t.Fatal("no dropped client signal")
	}

	time.Sleep(50 * time.Millisecond)
	_ = callCount
	// Verify other connections were still delivered to.
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.okCount < 2 {
		t.Fatalf("okCount = %d, want >= 2", sender.okCount)
	}
}

// selectiveFailSender fails with ErrSendBufferFull for a specific connID.
type selectiveFailSender struct {
	mu      sync.Mutex
	fail    types.ConnectionID
	okCount int
}

func (s *selectiveFailSender) SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []SubscriptionUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if connID == s.fail {
		return ErrSendBufferFull
	}
	s.okCount++
	return nil
}
func (s *selectiveFailSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	return nil
}
func (s *selectiveFailSender) SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error {
	return nil
}
```

- [ ] **Step 2: Run tests**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker_BufferFull|TestFanOutWorker_ConnGone|TestFanOutWorker_Multiple" -count=1 -v 2>&1 | tail -10`
Expected: All 3 tests pass (backpressure handling was implemented in Task 3).

- [ ] **Step 3: Commit**

```bash
git add subscription/fanout_worker_test.go
git commit -m "test(subscription): backpressure and dropped client verification (Story 6.3)"
```

---

### Task 6: Confirmed-Read Gating (TxDurable Wait)

**Files:**
- Modify: `subscription/fanout_worker.go`
- Modify: `subscription/fanout_worker_test.go`

- [ ] **Step 1: Write confirmed-read tests**

Add to `subscription/fanout_worker_test.go`:

```go
func TestFanOutWorker_FastRead_NoWait(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// TxDurable never signals — if worker waits, test will timeout.
	durableCh := make(chan types.TxID)
	inbox <- FanOutMessage{
		TxID:      types.TxID(1),
		TxDurable: durableCh,
		Fanout: CommitFanout{
			cid(1): {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	// Fast-read: delivery should happen without TxDurable signal.
	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1 (fast-read should not wait)", len(mock.txCalls))
	}
}

func TestFanOutWorker_ConfirmedRead_Waits(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)
	conn1 := cid(1)
	w.SetConfirmedReads(conn1, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	durableCh := make(chan types.TxID, 1)
	inbox <- FanOutMessage{
		TxID:      types.TxID(1),
		TxDurable: durableCh,
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	// No delivery yet — waiting for TxDurable.
	time.Sleep(50 * time.Millisecond)
	mock.mu.Lock()
	preCount := len(mock.txCalls)
	mock.mu.Unlock()
	if preCount != 0 {
		t.Fatalf("txCalls = %d before TxDurable, want 0", preCount)
	}

	// Signal durability.
	durableCh <- types.TxID(1)

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d after TxDurable, want 1", len(mock.txCalls))
	}
}

func TestFanOutWorker_NilTxDurable_Skips(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)
	conn1 := cid(1)
	w.SetConfirmedReads(conn1, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// TxDurable is nil — treat as already durable.
	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1 (nil TxDurable = already durable)", len(mock.txCalls))
	}
}

func TestFanOutWorker_SetConfirmedReads_Toggle(t *testing.T) {
	w := &FanOutWorker{confirmedReads: make(map[types.ConnectionID]bool)}
	conn1 := cid(1)

	w.SetConfirmedReads(conn1, true)
	if !w.confirmedReads[conn1] {
		t.Fatal("expected confirmed reads enabled")
	}

	w.SetConfirmedReads(conn1, false)
	if w.confirmedReads[conn1] {
		t.Fatal("expected confirmed reads disabled")
	}
}

func TestFanOutWorker_RemoveClient(t *testing.T) {
	w := &FanOutWorker{confirmedReads: make(map[types.ConnectionID]bool)}
	conn1 := cid(1)
	w.confirmedReads[conn1] = true

	w.RemoveClient(conn1)
	if _, ok := w.confirmedReads[conn1]; ok {
		t.Fatal("RemoveClient should clear confirmedReads entry")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker_FastRead|TestFanOutWorker_ConfirmedRead|TestFanOutWorker_NilTxDurable|TestFanOutWorker_SetConfirmedReads|TestFanOutWorker_RemoveClient" -count=1 -v 2>&1 | tail -10`
Expected: FAIL — SetConfirmedReads, RemoveClient, anyConfirmedRead not implemented.

- [ ] **Step 3: Add confirmed-read methods and gating to fanout_worker.go**

Add methods:

```go
// SetConfirmedReads toggles the per-connection confirmed-read policy.
// Accessed only from the fan-out goroutine — no mutex needed.
func (w *FanOutWorker) SetConfirmedReads(connID types.ConnectionID, enabled bool) {
	if enabled {
		w.confirmedReads[connID] = true
	} else {
		delete(w.confirmedReads, connID)
	}
}

// RemoveClient clears all fan-out worker state for the given connection.
func (w *FanOutWorker) RemoveClient(connID types.ConnectionID) {
	delete(w.confirmedReads, connID)
}

func (w *FanOutWorker) anyConfirmedRead(fanout CommitFanout) bool {
	for connID := range fanout {
		if w.confirmedReads[connID] {
			return true
		}
	}
	return false
}
```

Add gating at the top of `deliver()` (before the caller extraction):

```go
func (w *FanOutWorker) deliver(msg FanOutMessage) {
	// Confirmed-read gating (Story 6.4): wait for durability if any
	// client in this batch requires confirmed reads.
	if msg.TxDurable != nil && w.anyConfirmedRead(msg.Fanout) {
		<-msg.TxDurable
	}

	// ... rest of deliver unchanged
```

- [ ] **Step 4: Run tests**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker" -count=1 -v 2>&1 | tail -15`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add subscription/fanout_worker.go subscription/fanout_worker_test.go
git commit -m "feat(subscription): confirmed-read gating with TxDurable wait (Story 6.4)"
```

---

### Task 7: SubscriptionError Delivery

**Files:**
- Modify: `subscription/fanout_worker.go`
- Modify: `subscription/fanout_worker_test.go`

- [ ] **Step 1: Write error delivery test**

Add to `subscription/fanout_worker_test.go`:

```go
func TestFanOutWorker_SubscriptionErrorDelivery(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	conn1 := cid(1)
	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
		Errors: map[types.ConnectionID][]SubscriptionError{
			conn1: {
				{SubscriptionID: 5, QueryHash: QueryHash{1}, Message: "eval failed"},
				{SubscriptionID: 6, QueryHash: QueryHash{2}, Message: "type mismatch"},
			},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()

	// Errors delivered.
	if len(mock.errCalls) != 2 {
		t.Fatalf("errCalls = %d, want 2", len(mock.errCalls))
	}
	if mock.errCalls[0].SubID != 5 || mock.errCalls[1].SubID != 6 {
		t.Fatalf("errCalls SubIDs = %d, %d; want 5, 6", mock.errCalls[0].SubID, mock.errCalls[1].SubID)
	}
	// TransactionUpdate also delivered.
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1", len(mock.txCalls))
	}
}

func TestFanOutWorker_ErrorsDeliveredBeforeUpdates(t *testing.T) {
	// Track call order to verify errors come before updates.
	type call struct {
		kind string
		conn types.ConnectionID
	}
	var mu sync.Mutex
	var order []call

	sender := &orderTrackingSender{
		onTx: func(connID types.ConnectionID) {
			mu.Lock()
			order = append(order, call{kind: "tx", conn: connID})
			mu.Unlock()
		},
		onErr: func(connID types.ConnectionID) {
			mu.Lock()
			order = append(order, call{kind: "err", conn: connID})
			mu.Unlock()
		},
	}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, sender, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	conn1 := cid(1)
	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
		Errors: map[types.ConnectionID][]SubscriptionError{
			conn1: {{SubscriptionID: 5, Message: "boom"}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(order) < 2 {
		t.Fatalf("order len = %d, want >= 2", len(order))
	}
	if order[0].kind != "err" {
		t.Fatalf("first call = %q, want 'err' (errors before updates)", order[0].kind)
	}
	if order[1].kind != "tx" {
		t.Fatalf("second call = %q, want 'tx'", order[1].kind)
	}
}

type orderTrackingSender struct {
	onTx  func(types.ConnectionID)
	onErr func(types.ConnectionID)
}

func (s *orderTrackingSender) SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []SubscriptionUpdate) error {
	if s.onTx != nil {
		s.onTx(connID)
	}
	return nil
}
func (s *orderTrackingSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	return nil
}
func (s *orderTrackingSender) SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error {
	if s.onErr != nil {
		s.onErr(connID)
	}
	return nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker_SubscriptionError|TestFanOutWorker_ErrorsDelivered" -count=1 -v 2>&1 | tail -5`
Expected: FAIL — errors not delivered yet.

- [ ] **Step 3: Add error delivery to deliver() method**

Update the `deliver` method in `fanout_worker.go`. Insert error delivery after the confirmed-read gating, before caller extraction:

```go
func (w *FanOutWorker) deliver(msg FanOutMessage) {
	// Confirmed-read gating (Story 6.4).
	if msg.TxDurable != nil && w.anyConfirmedRead(msg.Fanout) {
		<-msg.TxDurable
	}

	// Deliver subscription errors first (before updates).
	for connID, errs := range msg.Errors {
		for _, se := range errs {
			if err := w.sender.SendSubscriptionError(connID, se.SubscriptionID, se.Message); err != nil {
				w.handleSendError(connID, err)
			}
		}
	}

	// Extract caller if present.
	var callerUpdates []SubscriptionUpdate
	if msg.CallerConnID != nil {
		callerUpdates = msg.Fanout[*msg.CallerConnID]
		delete(msg.Fanout, *msg.CallerConnID)
	}

	// Deliver standalone TransactionUpdate to non-caller connections.
	for connID, updates := range msg.Fanout {
		if err := w.sender.SendTransactionUpdate(connID, msg.TxID, updates); err != nil {
			w.handleSendError(connID, err)
		}
	}

	// Deliver ReducerCallResult to caller.
	if msg.CallerConnID != nil && msg.CallerResult != nil {
		result := *msg.CallerResult
		if result.Status == 0 {
			result.TransactionUpdate = callerUpdates
		} else {
			result.TransactionUpdate = nil
		}
		if err := w.sender.SendReducerResult(*msg.CallerConnID, &result); err != nil {
			w.handleSendError(*msg.CallerConnID, err)
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker" -count=1 -v 2>&1 | tail -15`
Expected: All tests pass.

- [ ] **Step 5: Run full subscription suite**

Run: `rtk go test ./subscription/... -count=1 2>&1 | tail -3`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add subscription/fanout_worker.go subscription/fanout_worker_test.go
git commit -m "feat(subscription): SubscriptionError delivery in fan-out loop (Story 6.3)"
```

---

### Task 8: Manager Wire-Up + Acceptance Test

**Files:**
- Modify: `subscription/manager.go`
- Modify: `subscription/fanout_worker_test.go`

- [ ] **Step 1: Add DroppedChanSend accessor to Manager**

Add to `subscription/manager.go`:

```go
// DroppedChanSend returns the write end of the dropped-client channel.
// Used to wire the FanOutWorker to the same channel the Manager's
// eval-error path writes to, so the executor drains one channel.
func (m *Manager) DroppedChanSend() chan<- types.ConnectionID { return m.dropped }
```

- [ ] **Step 2: Write acceptance test for full flow**

Add to `subscription/fanout_worker_test.go`:

```go
func TestFanOutWorker_Acceptance_FullFlow(t *testing.T) {
	// Full pipeline: Manager.EvalAndBroadcast → inbox → FanOutWorker → mock sender.
	// Uses existing test helpers: testSchema() (validate_test.go), simpleChangeset() (delta_view_test.go).
	mock := &mockFanOutSender{}
	fanoutCh := make(chan FanOutMessage, 64)

	s := testSchema() // fakeSchema: table 1 (cols: 0=KindUint64, 1=KindString, idx on 0)
	mgr := NewManager(s, s, WithFanOutInbox(fanoutCh))

	worker := NewFanOutWorker(fanoutCh, mock, mgr.DroppedChanSend())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Run(ctx)

	// Register AllRows subscription on table 1.
	conn1 := cid(1)
	_, err := mgr.Register(SubscriptionRegisterRequest{
		ConnID:         conn1,
		SubscriptionID: 10,
		Predicate:      AllRows{Table: 1},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a commit: one insert to table 1.
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(42), types.NewString("alice")}},
		nil,
	)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil)

	// Wait for fan-out delivery.
	time.Sleep(100 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1", len(mock.txCalls))
	}
	if mock.txCalls[0].ConnID != conn1 {
		t.Fatalf("connID mismatch")
	}
	if mock.txCalls[0].TxID != 1 {
		t.Fatalf("TxID = %d, want 1", mock.txCalls[0].TxID)
	}
	if len(mock.txCalls[0].Updates) == 0 {
		t.Fatal("no updates delivered")
	}
	if mock.txCalls[0].Updates[0].SubscriptionID != 10 {
		t.Fatalf("SubscriptionID = %d, want 10", mock.txCalls[0].Updates[0].SubscriptionID)
	}
}
```

- [ ] **Step 3: Run acceptance test**

Run: `rtk go test ./subscription/... -run "TestFanOutWorker_Acceptance" -count=1 -v 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 4: Run full subscription suite**

Run: `rtk go test ./subscription/... -count=1 2>&1 | tail -3`
Expected: All tests pass.

- [ ] **Step 5: Run full protocol suite**

Run: `rtk go test ./protocol/... -count=1 2>&1 | tail -3`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add subscription/manager.go subscription/fanout_worker_test.go
git commit -m "feat(subscription): Manager wire-up + fan-out acceptance test (Story 6.1)"
```

---

## Verification

After all tasks:

```bash
rtk go test ./subscription/... -count=1 -v 2>&1 | tail -5
rtk go test ./protocol/... -count=1 -v 2>&1 | tail -5
rtk go vet ./subscription/... ./protocol/...
```

All must pass.

---

## Files Summary

| File | Action | Task |
|------|--------|------|
| `subscription/errors.go` | Modify — add delivery error sentinels | 1 |
| `subscription/fanout.go` | Modify — add SubscriptionID to SubscriptionError | 1 |
| `subscription/eval.go` | Modify — populate SubscriptionID in handleEvalError | 1 |
| `subscription/fanout_worker.go` | Create — FanOutSender interface + FanOutWorker | 1, 3, 4, 6, 7 |
| `subscription/fanout_worker_test.go` | Create — comprehensive test suite | 3, 4, 5, 6, 7, 8 |
| `subscription/manager.go` | Modify — DroppedChanSend accessor | 8 |
| `protocol/fanout_adapter.go` | Create — encoding bridge + FanOutSenderAdapter | 2 |
| `protocol/fanout_adapter_test.go` | Create — encoding + adapter tests | 2 |

## Acceptance Criteria Coverage

| Criterion (from Stories 6.1–6.4) | Task |
|---|---|
| FanOutMessage received → delivered to correct clients | 3, 8 |
| Worker runs on separate goroutine from executor | 3 |
| Inbox channel bounded (default 64) | 8 (wiring) |
| Context cancellation → worker exits cleanly | 3 |
| Confirmed-read policy set/cleared per connection | 6 |
| Caller routed to ReducerCallResult path | 4 |
| DroppedClients channel available for executor drain | 5, 8 |
| Subscription boundaries preserved (multi-sub per table) | 3, 8 |
| Caller MUST NOT receive standalone TransactionUpdate | 4 |
| Failed reducer → empty TransactionUpdate in result | 4 |
| Buffer full → client disconnected, not blocked | 5 |
| Disconnected → connID on DroppedClients | 5 |
| Fan-out goroutine never blocks on slow client | 5 |
| All fast-read → no TxDurable wait | 6 |
| One confirmed-read → wait before all delivery | 6 |
| Nil TxDurable → no blocking | 6 |
