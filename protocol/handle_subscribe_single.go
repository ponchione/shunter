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

	respCh := make(chan SubscriptionSetCommandResponse, 1)
	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    msg.QueryID,
		RequestID:  msg.RequestID,
		Predicates: []any{pred},
		ResponseCh: respCh,
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
