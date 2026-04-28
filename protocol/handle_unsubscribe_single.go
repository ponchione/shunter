package protocol

import (
	"context"
	"time"
)

// handleUnsubscribeSingle processes an incoming UnsubscribeSingleMsg.
// The wire QueryID is forwarded to the executor via the set-based
// unsubscribe seam; the executor is responsible for producing the
// final UnsubscribeSingleApplied (or SubscriptionError) once the drop
// has been applied. The executor invokes the Reply closure
// synchronously on its own goroutine; the closure enqueues the result
// onto the connection's outbound channel.
//
// Receipt-timestamp seam: see handleSubscribeSingle.
func handleUnsubscribeSingle(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeSingleMsg,
	executor ExecutorInbox,
) {
	handleUnsubscribeSet(ctx, conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantSingle, executor)
}

func handleUnsubscribeSet(
	ctx context.Context,
	conn *Conn,
	requestID, queryID uint32,
	variant SubscriptionSetVariant,
	executor ExecutorInbox,
) {
	receipt := time.Now()
	if err := executor.UnregisterSubscriptionSet(ctx, UnregisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   queryID,
		RequestID: requestID,
		Variant:   variant,
		Reply:     makeUnsubscribeSetReply(conn, requestID, queryID, variant),
		Receipt:   receipt,
	}); err != nil {
		sendExecutorUnavailableError(conn, receipt, requestID, queryID, err)
		return
	}
}
