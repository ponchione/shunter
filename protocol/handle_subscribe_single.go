package protocol

import (
	"context"
)

// handleSubscribeSingle processes an incoming SubscribeSingleMsg. It
// resolves and validates the wire query against the schema, normalizes
// predicates, and submits the subscription to the executor via the
// set-based seam (len(Predicates)==1). The async watcher emits either a
// SubscribeSingleApplied or a SubscriptionError on the connection's
// outbound channel.
func handleSubscribeSingle(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeSingleMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	pred, err := compileQuery(msg.Query, sl)
	if err != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     err.Error(),
		})
		return
	}

	// Transitional: Task 2 exposes Reply; the watcher goroutine remains
	// until Task 3 removes it. The closure forwards the executor's reply
	// onto the existing buffered respCh so the watcher path is unchanged.
	respCh := make(chan SubscriptionSetCommandResponse, 1)
	reply := func(resp SubscriptionSetCommandResponse) { respCh <- resp }
	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    msg.QueryID,
		RequestID:  msg.RequestID,
		Predicates: []any{pred},
		Reply:      reply,
	}); submitErr != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     "executor unavailable: " + submitErr.Error(),
		})
		return
	}

	watchSubscribeSetResponse(conn, respCh, true /* single */, msg.RequestID, msg.QueryID)
}
