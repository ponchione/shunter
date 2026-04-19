package protocol

import (
	"context"
)

// handleUnsubscribeSingle processes an incoming UnsubscribeSingleMsg.
// The wire QueryID is forwarded to the executor via the set-based
// unsubscribe seam; the executor is responsible for producing the
// final UnsubscribeSingleApplied (or SubscriptionError) once the drop
// has been applied. The legacy "reserved but unknown to executor"
// guard moves into the executor alongside the set registry.
func handleUnsubscribeSingle(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeSingleMsg,
	executor ExecutorInbox,
) {
	// Transitional: Task 2 exposes Reply; the watcher goroutine remains
	// until Task 3 removes it. The closure forwards the executor's reply
	// onto the existing buffered respCh so the watcher path is unchanged.
	respCh := make(chan UnsubscribeSetCommandResponse, 1)
	reply := func(resp UnsubscribeSetCommandResponse) { respCh <- resp }
	if err := executor.UnregisterSubscriptionSet(ctx, UnregisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   msg.QueryID,
		RequestID: msg.RequestID,
		Reply:     reply,
	}); err != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     "executor unavailable: " + err.Error(),
		})
		return
	}

	watchUnsubscribeSetResponse(conn, respCh, true /* single */, msg.RequestID, msg.QueryID)
}
