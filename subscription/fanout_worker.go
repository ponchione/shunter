package subscription

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/ponchione/shunter/types"
)

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
	SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []SubscriptionUpdate, memo *EncodingMemo) error
	// SendReducerResult delivers a ReducerCallResult to the caller client.
	SendReducerResult(connID types.ConnectionID, result *ReducerCallResult, memo *EncodingMemo) error
	// SendSubscriptionError delivers a SubscriptionError to a client.
	SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error
}

// FanOutWorker receives computed deltas from the evaluation loop and
// delivers them through the protocol layer. Runs on its own goroutine
// separate from the executor (SPEC-004 §8.1 / Story 6.1).
type FanOutWorker struct {
	inbox          <-chan FanOutMessage
	sender         FanOutSender
	mu             sync.RWMutex
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
			w.deliver(ctx, msg)
		}
	}
}

// SetConfirmedReads toggles the per-connection confirmed-read policy.
func (w *FanOutWorker) SetConfirmedReads(connID types.ConnectionID, enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if enabled {
		w.confirmedReads[connID] = true
	} else {
		delete(w.confirmedReads, connID)
	}
}

// RemoveClient clears all fan-out worker state for the given connection.
func (w *FanOutWorker) RemoveClient(connID types.ConnectionID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.confirmedReads, connID)
}

func (w *FanOutWorker) anyConfirmedRead(fanout CommitFanout) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for connID := range fanout {
		if w.confirmedReads[connID] {
			return true
		}
	}
	return false
}

func (w *FanOutWorker) deliver(ctx context.Context, msg FanOutMessage) {
	memo := NewEncodingMemo()

	// Confirmed-read gating (Story 6.4).
	if msg.TxDurable != nil && w.anyConfirmedRead(msg.Fanout) {
		select {
		case <-ctx.Done():
			return
		case <-msg.TxDurable:
		}
	}

	// Deliver subscription errors first (before updates).
	for connID, errs := range msg.Errors {
		for _, se := range errs {
			if err := w.sender.SendSubscriptionError(connID, se.SubscriptionID, se.Message); err != nil {
				w.handleSendError(connID, err)
			}
		}
	}

	// Extract caller's updates if present. Skip (not delete) caller
	// during iteration to avoid mutating the shared Fanout map.
	var callerUpdates []SubscriptionUpdate
	if msg.CallerConnID != nil {
		callerUpdates = msg.Fanout[*msg.CallerConnID]
	}

	// Deliver standalone TransactionUpdate to non-caller connections.
	for connID, updates := range msg.Fanout {
		if msg.CallerConnID != nil && connID == *msg.CallerConnID {
			continue
		}
		if err := w.sender.SendTransactionUpdate(connID, msg.TxID, updates, memo); err != nil {
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
		if err := w.sender.SendReducerResult(*msg.CallerConnID, &result, memo); err != nil {
			w.handleSendError(*msg.CallerConnID, err)
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
	w.mu.Lock()
	delete(w.confirmedReads, connID)
	w.mu.Unlock()
	select {
	case w.dropped <- connID:
	default:
		log.Printf("subscription: dropped client channel full, skipping conn %x", connID[:])
	}
}
