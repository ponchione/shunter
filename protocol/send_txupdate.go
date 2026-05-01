package protocol

import (
	"github.com/ponchione/shunter/types"
)

// DeliveryError pairs a connection ID with the error encountered during
// delivery. Used by callers to trigger disconnect for buffer-full
// connections.
type DeliveryError struct {
	ConnID types.ConnectionID
	Err    error
}

// DeliverTransactionUpdateLight sends a non-caller TransactionUpdateLight
// to every connection in fanout (outcome-model). Connections not found in
// the ConnManager are skipped (disconnected since evaluation). Empty
// update slices are skipped. Buffer-full errors are collected so the
// caller can trigger disconnects.
//
// single/multi variant admission-model slice (TD-140): the former per-update
// active-subscription gate is gone — admission is owned by
// subscription.Manager.querySets, and fan-out enumerates only live
// client QueryIDs. Transport-level guards in connOnlySender.Send
// (<-c.closed, ErrConnNotFound, ErrClientBufferFull) handle disconnect races.
// See docs/shunter-design-decisions.md.
func DeliverTransactionUpdateLight(
	sender ClientSender,
	mgr *ConnManager,
	requestID uint32,
	fanout map[types.ConnectionID][]SubscriptionUpdate,
) []DeliveryError {
	var errs []DeliveryError
	for connID, updates := range fanout {
		if len(updates) == 0 {
			continue
		}
		if mgr.Get(connID) == nil {
			continue
		}
		msg := &TransactionUpdateLight{RequestID: requestID, Update: updates}
		if err := sender.SendTransactionUpdateLight(connID, msg); err != nil {
			errs = append(errs, DeliveryError{ConnID: connID, Err: err})
		}
	}
	return errs
}
