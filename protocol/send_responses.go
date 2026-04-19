package protocol

import "github.com/ponchione/shunter/types"

// SendSubscribeSingleApplied delivers a SubscribeSingleApplied message.
// Phase 2 Slice 2 admission-model slice (TD-140): wire-id admission
// bookkeeping is no longer maintained on the protocol connection —
// subscription.Manager.querySets is the single source of truth, and
// §9.4 ordering is preserved by the synchronous Reply closure invoked
// inside the executor main-loop goroutine plus per-connection
// OutboundCh FIFO. See docs/adr/2026-04-19-subscription-admission-model.md.
func SendSubscribeSingleApplied(sender ClientSender, conn *Conn, msg *SubscribeSingleApplied) error {
	return sender.Send(conn.ID, *msg)
}

// SendUnsubscribeSingleApplied delivers an UnsubscribeSingleApplied
// message and removes the subscription from the tracker.
func SendUnsubscribeSingleApplied(sender ClientSender, conn *Conn, msg *UnsubscribeSingleApplied) error {
	_ = conn.Subscriptions.Remove(msg.QueryID)
	return sender.Send(conn.ID, *msg)
}

// SendSubscribeMultiApplied delivers a SubscribeMultiApplied message.
// Phase 2 Slice 2: connection-level subscription tracking stays on the
// Single path only; Multi admission bookkeeping lives in the executor's
// set registry (Task 7), so this helper is a straight transport push.
func SendSubscribeMultiApplied(sender ClientSender, conn *Conn, msg *SubscribeMultiApplied) error {
	return sender.Send(conn.ID, *msg)
}

// SendUnsubscribeMultiApplied delivers an UnsubscribeMultiApplied
// message. Set-level teardown bookkeeping lives in the executor.
func SendUnsubscribeMultiApplied(sender ClientSender, conn *Conn, msg *UnsubscribeMultiApplied) error {
	return sender.Send(conn.ID, *msg)
}

// SendSubscriptionError delivers a SubscriptionError and releases the
// subscription_id so it is immediately reusable (SPEC-005 §8.4).
func SendSubscriptionError(sender ClientSender, conn *Conn, msg *SubscriptionError) error {
	_ = conn.Subscriptions.Remove(msg.QueryID)
	return sender.Send(conn.ID, *msg)
}

// SendOneOffQueryResult delivers a OneOffQueryResult. No subscription
// state change.
func SendOneOffQueryResult(sender ClientSender, connID types.ConnectionID, msg *OneOffQueryResult) error {
	return sender.Send(connID, *msg)
}
