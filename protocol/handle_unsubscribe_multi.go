package protocol

import "context"

// handleUnsubscribeMulti processes an incoming UnsubscribeMultiMsg.
// The wire QueryID (shared across every predicate in the set) is
// forwarded to the executor via the set-based unsubscribe seam; the
// executor drops every internal subscription registered under
// (ConnID, QueryID) atomically. The executor invokes the Reply closure
// synchronously on its own goroutine; the closure enqueues either an
// UnsubscribeMultiApplied or a SubscriptionError onto the connection's
// outbound channel.
//
// Receipt-timestamp seam: see handleSubscribeSingle.
func handleUnsubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *UnsubscribeMultiMsg,
	executor ExecutorInbox,
) {
	handleUnsubscribeSet(ctx, conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantMulti, executor)
}
