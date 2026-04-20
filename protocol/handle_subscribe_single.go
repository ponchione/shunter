package protocol

import (
	"context"
	"log"
)

// handleSubscribeSingle processes an incoming SubscribeSingleMsg. It
// resolves and validates the wire query against the schema, normalizes
// predicates, and submits the subscription to the executor via the
// set-based seam (len(Predicates)==1). The executor invokes the Reply
// closure synchronously on its own goroutine; the closure enqueues
// either a SubscribeSingleApplied or a SubscriptionError onto the
// connection's outbound channel. Synchronous dispatch here is what
// enforces ADR §9.4 FIFO between Applied and any subsequent fan-out.
func handleSubscribeSingle(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeSingleMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	compiled, err := compileSQLQueryString(msg.QueryString, sl)
	if err != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     err.Error(),
		})
		return
	}
	pred := compiled.Predicate

	sender := connOnlySender{conn: conn}
	reply := func(resp SubscriptionSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: SubscriptionError delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.Error.QueryID, err)
			}
		case resp.SingleApplied != nil:
			if err := SendSubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				log.Printf("protocol: SubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.SingleApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed SubscriptionSetCommandResponse (req=%d query=%d)", msg.RequestID, msg.QueryID)
		}
	}
	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    msg.QueryID,
		RequestID:  msg.RequestID,
		Variant:    SubscriptionSetVariantSingle,
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
}
