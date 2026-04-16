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
