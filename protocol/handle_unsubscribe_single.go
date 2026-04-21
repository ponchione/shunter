package protocol

import (
	"context"
	"log"
)

// handleUnsubscribeSingle processes an incoming UnsubscribeSingleMsg.
// The wire QueryID is forwarded to the executor via the set-based
// unsubscribe seam; the executor is responsible for producing the
// final UnsubscribeSingleApplied (or SubscriptionError) once the drop
// has been applied. The executor invokes the Reply closure
// synchronously on its own goroutine; the closure enqueues the result
// onto the connection's outbound channel.
func handleUnsubscribeSingle(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeSingleMsg,
	executor ExecutorInbox,
) {
	sender := connOnlySender{conn: conn}
	reply := func(resp UnsubscribeSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: unsubscribe SubscriptionError delivery failed for conn %x query_id=%s: %v", conn.ID[:], subscriptionErrorQueryIDForLog(resp.Error), err)
			}
		case resp.SingleApplied != nil:
			if err := SendUnsubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				log.Printf("protocol: UnsubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.SingleApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed UnsubscribeSetCommandResponse (req=%d query=%d)", msg.RequestID, msg.QueryID)
		}
	}
	if err := executor.UnregisterSubscriptionSet(ctx, UnregisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   msg.QueryID,
		RequestID: msg.RequestID,
		Variant:   SubscriptionSetVariantSingle,
		Reply:     reply,
	}); err != nil {
		sendError(conn, SubscriptionError{
			RequestID: optionalUint32(msg.RequestID),
			QueryID:   optionalUint32(msg.QueryID),
			Error:     "executor unavailable: " + err.Error(),
		})
		return
	}
}
