package protocol

import (
	"context"
	"log"
	"time"

	"github.com/ponchione/shunter/types"
)

// handleSubscribeMulti processes an incoming SubscribeMultiMsg. Every
// wire query is compiled against the schema; the first compile error
// aborts the entire batch and emits a SubscriptionError (atomic
// admission per SPEC-005 §7.1b). On success the N predicates are
// forwarded to the executor under a single QueryID via the set-based
// seam. The executor invokes the Reply closure synchronously on its
// own goroutine; the closure enqueues either a SubscribeMultiApplied
// or a SubscriptionError onto the connection's outbound channel.
// Synchronous dispatch here is what enforces ADR §9.4 FIFO between
// Applied and any subsequent fan-out.
//
// Receipt-timestamp seam: see handleSubscribeSingle.
func handleSubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMultiMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	receipt := time.Now()
	preds := make([]any, 0, len(msg.QueryStrings))
	hashIdentities := make([]*types.Identity, 0, len(msg.QueryStrings))
	for _, qs := range msg.QueryStrings {
		compiled, err := compileSQLQueryString(qs, sl, &conn.Identity, false, false)
		if err != nil {
			sendError(conn, SubscriptionError{
				TotalHostExecutionDurationMicros: elapsedMicros(receipt),
				RequestID:                        optionalUint32(msg.RequestID),
				QueryID:                          optionalUint32(msg.QueryID),
				Error:                            err.Error(),
			})
			return
		}
		preds = append(preds, compiled.Predicate)
		hashIdentities = append(hashIdentities, callerHashIdentity(conn, compiled))
	}

	sender := connOnlySender{conn: conn}
	reply := func(resp SubscriptionSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: SubscriptionError delivery failed for conn %x query_id=%s: %v", conn.ID[:], subscriptionErrorQueryIDForLog(resp.Error), err)
			}
		case resp.MultiApplied != nil:
			if err := SendSubscribeMultiApplied(sender, conn, resp.MultiApplied); err != nil {
				log.Printf("protocol: SubscribeMultiApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.MultiApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed SubscriptionSetCommandResponse (req=%d query=%d)", msg.RequestID, msg.QueryID)
		}
	}
	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:                  conn.ID,
		QueryID:                 msg.QueryID,
		RequestID:               msg.RequestID,
		Variant:                 SubscriptionSetVariantMulti,
		Predicates:              preds,
		PredicateHashIdentities: hashIdentities,
		Reply:                   reply,
		Receipt:                 receipt,
	}); submitErr != nil {
		sendError(conn, SubscriptionError{
			TotalHostExecutionDurationMicros: elapsedMicros(receipt),
			RequestID:                        optionalUint32(msg.RequestID),
			QueryID:                          optionalUint32(msg.QueryID),
			Error:                            "executor unavailable: " + submitErr.Error(),
		})
		return
	}
}
