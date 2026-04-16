package protocol

import "github.com/ponchione/shunter/types"

// DeliverReducerCallResult handles the caller-delta diversion path
// (SPEC-005 §8.7). When a reducer commits:
//
//  1. The caller's subscription updates are extracted from fanout and
//     embedded in the ReducerCallResult.
//  2. The caller receives ReducerCallResult (not a standalone
//     TransactionUpdate).
//  3. All other connections receive standalone TransactionUpdate via
//     DeliverTransactionUpdate.
//
// When callerConnID is nil (non-reducer commit), result is ignored and
// all entries go through DeliverTransactionUpdate.
//
// When result.Status != 0 (failed/panic/not-found), the embedded
// TransactionUpdate is forced empty per SPEC-005 §8.7.
func DeliverReducerCallResult(
	sender ClientSender,
	mgr *ConnManager,
	result *ReducerCallResult,
	callerConnID *types.ConnectionID,
	fanout map[types.ConnectionID][]SubscriptionUpdate,
) []DeliveryError {
	if callerConnID == nil {
		// Non-reducer commit — pure standalone delivery.
		return DeliverTransactionUpdate(sender, mgr, result.TxID, fanout)
	}

	// Extract caller's entry and keep the input map unchanged so shared fanout
	// state can be safely reused by other delivery paths.
	callerUpdates := fanout[*callerConnID]

	var errs []DeliveryError

	// Deliver ReducerCallResult to caller.
	if result != nil {
		callerResult := *result
		callerResult.Energy = 0 // v1: always zero (SPEC-005 §8.7)
		if callerResult.Status == 0 {
			callerResult.TransactionUpdate = callerUpdates
		} else {
			callerResult.TransactionUpdate = nil
		}
		if err := sender.SendReducerResult(*callerConnID, &callerResult); err != nil {
			errs = append(errs, DeliveryError{ConnID: *callerConnID, Err: err})
		}
	}

	// Deliver standalone TransactionUpdate to everyone else.
	if result != nil && result.Status == 0 {
		nonCaller := make(map[types.ConnectionID][]SubscriptionUpdate, len(fanout))
		for connID, updates := range fanout {
			if connID == *callerConnID {
				continue
			}
			nonCaller[connID] = updates
		}
		if len(nonCaller) > 0 {
			txErrs := DeliverTransactionUpdate(sender, mgr, result.TxID, nonCaller)
			errs = append(errs, txErrs...)
		}
	}

	return errs
}
