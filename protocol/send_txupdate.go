package protocol

import (
	"errors"

	"github.com/ponchione/shunter/types"
)

// DeliveryError pairs a connection ID with the error encountered during
// delivery. Used by callers to trigger disconnect for buffer-full
// connections.
type DeliveryError struct {
	ConnID types.ConnectionID
	Err    error
}

// DeliverTransactionUpdateLight sends non-caller updates and collects
// buffer-full delivery errors. Missing connections and empty updates are skipped.
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
		msg := &TransactionUpdateLight{RequestID: requestID, Update: updates}
		if err := sender.SendTransactionUpdateLight(connID, msg); err != nil {
			if errors.Is(err, ErrConnNotFound) {
				continue
			}
			errs = append(errs, DeliveryError{ConnID: connID, Err: err})
		}
	}
	return errs
}
