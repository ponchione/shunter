package protocol

import "context"

// handleUnsubscribeMulti removes the subscription set for the client QueryID.
func handleUnsubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeMultiMsg,
	executor ExecutorInbox,
) {
	handleUnsubscribeSet(ctx, conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantMulti, executor)
}
