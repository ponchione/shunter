package protocol

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/types"
)

// ErrSubscriptionNotActive is returned when a `TransactionUpdate` /
// `TransactionUpdateLight` fanout references a subscription_id that has
// not yet reached SubActive on the target connection. This is a
// protocol pipeline invariant violation: SubscribeApplied must be
// delivered before any update for that subscription.
var ErrSubscriptionNotActive = errors.New("protocol: subscription not active for transaction update")

// DeliveryError pairs a connection ID with the error encountered
// during delivery. Used by callers to trigger disconnect for
// buffer-full connections.
type DeliveryError struct {
	ConnID types.ConnectionID
	Err    error
}

// DeliverTransactionUpdateLight sends a non-caller `TransactionUpdateLight`
// to every connection in fanout (Phase 1.5). Connections not found in
// the ConnManager are skipped (disconnected since evaluation). Empty
// update slices are skipped. Buffer-full errors are collected so the
// caller can trigger disconnects.
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
		conn := mgr.Get(connID)
		if conn == nil {
			continue
		}
		if err := validateActiveSubscriptionUpdates(conn, updates); err != nil {
			errs = append(errs, DeliveryError{ConnID: connID, Err: err})
			continue
		}
		msg := &TransactionUpdateLight{RequestID: requestID, Update: updates}
		if err := sender.SendTransactionUpdateLight(connID, msg); err != nil {
			errs = append(errs, DeliveryError{ConnID: connID, Err: err})
		}
	}
	return errs
}

func validateActiveSubscriptionUpdates(conn *Conn, updates []SubscriptionUpdate) error {
	for _, update := range updates {
		if !conn.Subscriptions.IsActive(update.SubscriptionID) {
			return fmt.Errorf("%w: conn=%x subscription_id=%d", ErrSubscriptionNotActive, conn.ID[:], update.SubscriptionID)
		}
	}
	return nil
}
