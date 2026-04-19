package protocol

import "github.com/ponchione/shunter/types"

// SendSubscribeSingleApplied delivers a SubscribeSingleApplied message
// and transitions the subscription from pending → active. If the
// subscription was already removed (disconnect race), the result is
// silently discarded per SPEC-005 §9.1.
//
// The guard uses IsActiveOrPending because the only case we need to
// discard is "subscription removed by disconnect." The upstream
// contract guarantees exactly one call per subscription registration.
//
// If Send fails (e.g. ErrClientBufferFull), the error propagates to
// the caller. The tracker entry is removed so a late failed delivery
// cannot leave the subscription spuriously active after the result was
// never committed to the wire.
func SendSubscribeSingleApplied(sender ClientSender, conn *Conn, msg *SubscribeSingleApplied) error {
	if !conn.Subscriptions.IsPending(msg.QueryID) {
		return nil
	}
	conn.Subscriptions.Activate(msg.QueryID)
	if err := sender.Send(conn.ID, *msg); err != nil {
		_ = conn.Subscriptions.Remove(msg.QueryID)
		return err
	}
	return nil
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
