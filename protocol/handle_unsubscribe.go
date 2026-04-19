package protocol

import (
	"context"
	"fmt"
)

func handleUnsubscribe(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeSingleMsg,
	executor ExecutorInbox,
) {
	subID := msg.QueryID

	if !conn.Subscriptions.IsActive(subID) {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   subID,
			Error:     fmt.Sprintf("%v: id=%d", ErrSubscriptionNotFound, subID),
		})
		return
	}

	respCh := make(chan UnsubscribeCommandResponse, 1)
	if err := executor.UnregisterSubscription(ctx, UnregisterSubscriptionRequest{
		ConnID:         conn.ID,
		SubscriptionID: subID,
		RequestID:      msg.RequestID,
		SendDropped:    msg.SendDropped,
		ResponseCh:     respCh,
	}); err != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   subID,
			Error:     "executor unavailable: " + err.Error(),
		})
		return
	}

	_ = conn.Subscriptions.Remove(subID)
	watchUnsubscribeResponse(conn, respCh)
}
