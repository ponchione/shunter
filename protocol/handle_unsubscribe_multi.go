package protocol

import (
	"context"
)

// handleUnsubscribeMulti processes an incoming UnsubscribeMultiMsg.
// The wire QueryID (shared across every predicate in the set) is
// forwarded to the executor via the set-based unsubscribe seam; the
// executor drops every internal subscription registered under
// (ConnID, QueryID) atomically and produces the final
// UnsubscribeMultiApplied (or SubscriptionError) asynchronously.
func handleUnsubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeMultiMsg,
	executor ExecutorInbox,
) {
	respCh := make(chan UnsubscribeSetCommandResponse, 1)
	if err := executor.UnregisterSubscriptionSet(ctx, UnregisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    msg.QueryID,
		RequestID:  msg.RequestID,
		ResponseCh: respCh,
	}); err != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     "executor unavailable: " + err.Error(),
		})
		return
	}

	watchUnsubscribeSetResponse(conn, respCh, false /* single */, msg.RequestID, msg.QueryID)
}
