package protocol

import (
	"context"
	"fmt"
)

func handleUnsubscribe(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeMsg,
	executor ExecutorInbox,
) {
	subID := msg.SubscriptionID

	if !conn.Subscriptions.IsActive(subID) {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          fmt.Sprintf("%v: id=%d", ErrSubscriptionNotFound, subID),
		})
		return
	}

	if err := executor.UnregisterSubscription(ctx, conn.ID, subID); err != nil {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          "executor unavailable: " + err.Error(),
		})
		return
	}

	_ = conn.Subscriptions.Remove(subID)
}
