package protocol

import "github.com/ponchione/shunter/types"

// SendSubscribeSingleApplied delivers a SubscribeSingleApplied message.
// single/multi variant admission-model slice (TD-140): wire-id admission
// bookkeeping is no longer maintained on the protocol connection —
// subscription.Manager.querySets is the single source of truth, and
// §9.4 ordering is preserved by the synchronous Reply closure invoked
// inside the executor main-loop goroutine plus per-connection
// OutboundCh FIFO. See docs/shunter-design-decisions.md.
func SendSubscribeSingleApplied(sender ClientSender, conn *Conn, msg *SubscribeSingleApplied) error {
	return sender.Send(conn.ID, *msg)
}

// SendUnsubscribeSingleApplied delivers an UnsubscribeSingleApplied
// message. single/multi variant (TD-140): per-connection admission
// bookkeeping is gone — subscription.Manager owns the authoritative
// set of live query IDs, so this is a straight transport push.
func SendUnsubscribeSingleApplied(sender ClientSender, conn *Conn, msg *UnsubscribeSingleApplied) error {
	return sender.Send(conn.ID, *msg)
}

// SendSubscribeMultiApplied delivers a SubscribeMultiApplied message.
// single/multi variant admission-model slice (TD-140): connection-level
// subscription tracking is gone on both Single and Multi paths —
// subscription.Manager owns admission, set-level teardown bookkeeping
// lives in the executor. This helper is a straight transport push.
func SendSubscribeMultiApplied(sender ClientSender, conn *Conn, msg *SubscribeMultiApplied) error {
	return sender.Send(conn.ID, *msg)
}

// SendUnsubscribeMultiApplied delivers an UnsubscribeMultiApplied
// message. Set-level teardown bookkeeping lives in the executor.
func SendUnsubscribeMultiApplied(sender ClientSender, conn *Conn, msg *UnsubscribeMultiApplied) error {
	return sender.Send(conn.ID, *msg)
}

// SendSubscriptionError delivers a SubscriptionError. single/multi variant
// (TD-140): per-connection admission bookkeeping is gone —
// subscription.Manager owns the authoritative set of live query IDs,
// and a failed Register never admits the id, so there is nothing to
// release here. SPEC-005 §8.4 reusability is still provided by the
// manager on failure.
func SendSubscriptionError(sender ClientSender, conn *Conn, msg *SubscriptionError) error {
	return sender.Send(conn.ID, *msg)
}

func optionalUint32(v uint32) *uint32 {
	return &v
}

// SendOneOffQueryResponse delivers a OneOffQueryResponse. No subscription
// state change.
func SendOneOffQueryResponse(sender ClientSender, connID types.ConnectionID, msg *OneOffQueryResponse) error {
	return sender.Send(connID, *msg)
}
