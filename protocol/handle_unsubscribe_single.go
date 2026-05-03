package protocol

import (
	"context"
	"time"
)

// handleUnsubscribeSingle removes one subscription for the client QueryID.
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
		recordProtocolMessage(conn.Observer, protocolUnsubscribeMetricKind(variant), "executor_rejected")
		return
	}
	recordProtocolMessage(conn.Observer, protocolUnsubscribeMetricKind(variant), "ok")
}

func protocolUnsubscribeMetricKind(variant SubscriptionSetVariant) string {
	if variant == SubscriptionSetVariantMulti {
		return "unsubscribe_multi"
	}
	return "unsubscribe_single"
}
