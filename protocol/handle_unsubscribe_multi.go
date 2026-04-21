package protocol

import (
	"context"
	"log"
)

// handleUnsubscribeMulti processes an incoming UnsubscribeMultiMsg.
// The wire QueryID (shared across every predicate in the set) is
// forwarded to the executor via the set-based unsubscribe seam; the
// executor drops every internal subscription registered under
// (ConnID, QueryID) atomically. The executor invokes the Reply closure
// synchronously on its own goroutine; the closure enqueues either an
// UnsubscribeMultiApplied or a SubscriptionError onto the connection's
// outbound channel.
func handleUnsubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeMultiMsg,
	executor ExecutorInbox,
) {
	sender := connOnlySender{conn: conn}
	reply := func(resp UnsubscribeSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: unsubscribe SubscriptionError delivery failed for conn %x query_id=%s: %v", conn.ID[:], subscriptionErrorQueryIDForLog(resp.Error), err)
			}
		case resp.MultiApplied != nil:
			if err := SendUnsubscribeMultiApplied(sender, conn, resp.MultiApplied); err != nil {
				log.Printf("protocol: UnsubscribeMultiApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.MultiApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed UnsubscribeSetCommandResponse (req=%d query=%d)", msg.RequestID, msg.QueryID)
		}
	}
	if err := executor.UnregisterSubscriptionSet(ctx, UnregisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   msg.QueryID,
		RequestID: msg.RequestID,
		Variant:   SubscriptionSetVariantMulti,
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
