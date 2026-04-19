package protocol

import (
	"context"
)

// handleSubscribeMulti processes an incoming SubscribeMultiMsg. Every
// wire query is compiled against the schema; the first compile error
// aborts the entire batch and emits a SubscriptionError (atomic
// admission per SPEC-005 §7.1b). On success the N predicates are
// forwarded to the executor under a single QueryID via the set-based
// seam, and the async watcher emits either a SubscribeMultiApplied or a
// SubscriptionError on the connection's outbound channel.
func handleSubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMultiMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	preds := make([]any, 0, len(msg.Queries))
	for _, q := range msg.Queries {
		p, err := compileQuery(q, sl)
		if err != nil {
			sendError(conn, SubscriptionError{
				RequestID: msg.RequestID,
				QueryID:   msg.QueryID,
				Error:     err.Error(),
			})
			return
		}
		preds = append(preds, p)
	}

	respCh := make(chan SubscriptionSetCommandResponse, 1)
	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    msg.QueryID,
		RequestID:  msg.RequestID,
		Predicates: preds,
		ResponseCh: respCh,
	}); submitErr != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     "executor unavailable: " + submitErr.Error(),
		})
		return
	}

	watchSubscribeSetResponse(conn, respCh, false /* single */, msg.RequestID, msg.QueryID)
}
