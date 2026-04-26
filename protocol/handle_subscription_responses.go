package protocol

import (
	"log"
	"time"
)

func sendSubscribeCompileError(conn *Conn, receipt time.Time, requestID, queryID uint32, err error, sqlText string) {
	sendError(conn, SubscriptionError{
		TotalHostExecutionDurationMicros: elapsedMicros(receipt),
		RequestID:                        optionalUint32(requestID),
		QueryID:                          optionalUint32(queryID),
		Error:                            wrapSubscribeCompileErrorSQL(err, sqlText),
	})
}

func sendExecutorUnavailableError(conn *Conn, receipt time.Time, requestID, queryID uint32, err error) {
	sendError(conn, SubscriptionError{
		TotalHostExecutionDurationMicros: elapsedMicros(receipt),
		RequestID:                        optionalUint32(requestID),
		QueryID:                          optionalUint32(queryID),
		Error:                            "executor unavailable: " + err.Error(),
	})
}

func makeSubscribeSetReply(conn *Conn, requestID, queryID uint32, variant SubscriptionSetVariant) func(SubscriptionSetCommandResponse) {
	return func(resp SubscriptionSetCommandResponse) {
		sender := connOnlySender{conn: conn}
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: SubscriptionError delivery failed for conn %x query_id=%s: %v", conn.ID[:], subscriptionErrorQueryIDForLog(resp.Error), err)
			}
		case variant == SubscriptionSetVariantSingle && resp.SingleApplied != nil:
			if err := SendSubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				log.Printf("protocol: SubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.SingleApplied.QueryID, err)
			}
		case variant == SubscriptionSetVariantMulti && resp.MultiApplied != nil:
			if err := SendSubscribeMultiApplied(sender, conn, resp.MultiApplied); err != nil {
				log.Printf("protocol: SubscribeMultiApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.MultiApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed SubscriptionSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}
}

func makeUnsubscribeSetReply(conn *Conn, requestID, queryID uint32, variant SubscriptionSetVariant) func(UnsubscribeSetCommandResponse) {
	return func(resp UnsubscribeSetCommandResponse) {
		sender := connOnlySender{conn: conn}
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: unsubscribe SubscriptionError delivery failed for conn %x query_id=%s: %v", conn.ID[:], subscriptionErrorQueryIDForLog(resp.Error), err)
			}
		case variant == SubscriptionSetVariantSingle && resp.SingleApplied != nil:
			if err := SendUnsubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				log.Printf("protocol: UnsubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.SingleApplied.QueryID, err)
			}
		case variant == SubscriptionSetVariantMulti && resp.MultiApplied != nil:
			if err := SendUnsubscribeMultiApplied(sender, conn, resp.MultiApplied); err != nil {
				log.Printf("protocol: UnsubscribeMultiApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.MultiApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed UnsubscribeSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}
}
