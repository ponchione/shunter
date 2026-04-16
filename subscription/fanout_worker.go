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
