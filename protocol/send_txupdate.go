package protocol

import (
	"errors"

	"github.com/ponchione/shunter/types"
)

// ErrSubscriptionNotActive remains defined temporarily for older tests
// that still reference the removed admission gate. The gate itself is no
// longer used in production delivery after TD-137 / TD-140.
var ErrSubscriptionNotActive = errors.New("protocol: subscription not active for transaction update")

// DeliveryError pairs a connection ID with the error encountered during
// delivery. Used by callers to trigger disconnect for buffer-full
// connections.
type DeliveryError struct {
	ConnID types.ConnectionID
	Err    error
}

// DeliverTransactionUpdateLight sends a non-caller TransactionUpdateLight
// to every connection in fanout (Phase 1.5). Connections not found in
// the ConnManager are skipped (disconnected since evaluation). Empty
// update slices are skipped. Buffer-full errors are collected so the
// caller can trigger disconnects.
//
// Phase 2 Slice 2 admission-model slice (TD-140): the former per-update
// IsActive(SubscriptionID) gate is gone — admission is owned by
// subscription.Manager.querySets, and fan-out enumerates only live
// subs. Transport-level guards in connOnlySender.Send (<-c.closed,
// ErrConnNotFound, ErrClientBufferFull) handle disconnect races.
// See docs/adr/2026-04-19-subscription-admission-model.md.
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
