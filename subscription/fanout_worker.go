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
// Phase 1.5 outcome-model split (`docs/parity-phase1.5-outcome-model.md`):
//   - `SendTransactionUpdateHeavy` delivers the caller-bound envelope
//     with caller metadata and, for `CallerOutcomeCommitted`, the
//     caller's visible row delta.
//   - `SendTransactionUpdateLight` delivers the delta-only envelope to
//     non-callers whose rows were touched.
//
// Errors: implementations must return ErrSendBufferFull when the
// client's outbound buffer is full, and ErrSendConnGone when the
// target connection has already disconnected.
type FanOutSender interface {
	// SendTransactionUpdateHeavy delivers the caller-bound heavy envelope.
	SendTransactionUpdateHeavy(connID types.ConnectionID, outcome CallerOutcome, callerUpdates []SubscriptionUpdate, memo *EncodingMemo) error
	// SendTransactionUpdateLight delivers the non-caller delta-only envelope.
	SendTransactionUpdateLight(connID types.ConnectionID, requestID uint32, updates []SubscriptionUpdate, memo *EncodingMemo) error
	// SendSubscriptionError delivers a SubscriptionError to a client.
	SendSubscriptionError(connID types.ConnectionID, subErr SubscriptionError) error
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

func (w *FanOutWorker) anyConfirmedRead(fanout CommitFanout, callerConnID *types.ConnectionID, callerOutcome *CallerOutcome) bool {
	if len(fanout) > 0 {
		return true
	}
	if callerConnID != nil && callerOutcome != nil {
		return true
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	for connID := range fanout {
		if w.confirmedReads[connID] {
			return true
		}
	}
	if callerConnID != nil && callerOutcome != nil && w.confirmedReads[*callerConnID] {
		return true
	}
	return false
}

func (w *FanOutWorker) deliver(ctx context.Context, msg FanOutMessage) {
	memo := NewEncodingMemo()

	// Phase 1.5 CallReducerFlags::NoSuccessNotify: when the caller opted
	// out of the success echo and the outcome committed, suppress the
	// caller's heavy delivery entirely. Failure / out-of-energy
	// outcomes still flow so the caller observes non-success states.
	// Mirrors the reference behavior of dropping the caller from the
	// fan-out recipient set entirely in that case.
	callerSuppressed := msg.CallerConnID != nil && msg.CallerOutcome != nil &&
		msg.CallerOutcome.Kind == CallerOutcomeCommitted &&
		msg.CallerOutcome.Flags == CallerOutcomeFlagNoSuccessNotify

	// Compute the effective caller for downstream gating + delivery
	// decisions. When suppressed, treat the caller as absent so
	// confirmed-read gating does not block on a delivery that will not
	// happen and so the non-caller light loop skips the caller's
	// fanout entry as usual.
	effCallerConnID := msg.CallerConnID
	effCallerOutcome := msg.CallerOutcome
	if callerSuppressed {
		effCallerConnID = nil
		effCallerOutcome = nil
	}

	// Confirmed-read gating (Story 6.4). The heavy envelope is the
	// caller-visible commit confirmation, so it participates in
	// confirmed-read gating just like a non-caller light delivery.
	if msg.TxDurable != nil && w.anyConfirmedRead(msg.Fanout, effCallerConnID, effCallerOutcome) {
		select {
		case <-ctx.Done():
			return
		case <-msg.TxDurable:
		}
	}

	// Deliver subscription errors first (before updates).
	for connID, errs := range msg.Errors {
		for _, se := range errs {
			if err := w.sender.SendSubscriptionError(connID, se); err != nil {
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

	// Deliver TransactionUpdateLight to non-caller connections that had
	// row-touches. The light envelope carries the original caller's
	// request_id so non-callers can correlate their fanout updates with
	// the commit that produced them.
	var lightRequestID uint32
	if msg.CallerOutcome != nil {
		lightRequestID = msg.CallerOutcome.RequestID
	}
	for connID, updates := range msg.Fanout {
		if msg.CallerConnID != nil && connID == *msg.CallerConnID {
			continue
		}
		if err := w.sender.SendTransactionUpdateLight(connID, lightRequestID, updates, memo); err != nil {
			w.handleSendError(connID, err)
		}
	}

	// Deliver heavy TransactionUpdate to caller. Phase 1.5 invariant:
	// when CallerConnID is set the caller ALWAYS receives a heavy
	// envelope — success with possibly-empty update, failure, or
	// out-of-energy — so the caller never silently loses its outcome
	// even on empty changesets or no-active-subscription paths. The
	// NoSuccessNotify caller-echo opt-out (above) is the one exception:
	// effCallerConnID / effCallerOutcome are nil in that case.
	if effCallerConnID != nil && effCallerOutcome != nil {
		if err := w.sender.SendTransactionUpdateHeavy(*effCallerConnID, *effCallerOutcome, callerUpdates, memo); err != nil {
			w.handleSendError(*effCallerConnID, err)
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
