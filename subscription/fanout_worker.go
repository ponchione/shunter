package subscription

import (
	"context"
	"errors"
	"log"

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
	SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []SubscriptionUpdate) error
	// SendReducerResult delivers a ReducerCallResult to the caller client.
	SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error
	// SendSubscriptionError delivers a SubscriptionError to a client.
	SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error
}

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
