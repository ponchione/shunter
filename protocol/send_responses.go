package protocol

import "github.com/ponchione/shunter/types"

// SendSubscribeApplied delivers a SubscribeApplied message and
// transitions the subscription from pending → active. If the
// subscription was already removed (disconnect race), the result is
// silently discarded per SPEC-005 §9.1.
//
// The guard uses IsActiveOrPending because the only case we need to
// discard is "subscription removed by disconnect." The upstream
// contract guarantees exactly one call per subscription registration.
//
// If Send fails (e.g. ErrClientBufferFull), the error propagates to
// the caller, who is responsible for triggering a disconnect. The
// disconnect path calls RemoveAll, which cleans up the pending entry.
func SendSubscribeApplied(sender ClientSender, conn *Conn, msg *SubscribeApplied) error {
	if !conn.Subscriptions.IsActiveOrPending(msg.SubscriptionID) {
		return nil
	}
	if err := sender.Send(conn.ID, *msg); err != nil {
		return err
	}
	conn.Subscriptions.Activate(msg.SubscriptionID)
	return nil
}

// SendUnsubscribeApplied delivers an UnsubscribeApplied message and
// removes the subscription from the tracker.
func SendUnsubscribeApplied(sender ClientSender, conn *Conn, msg *UnsubscribeApplied) error {
	_ = conn.Subscriptions.Remove(msg.SubscriptionID)
	return sender.Send(conn.ID, *msg)
}

// SendSubscriptionError delivers a SubscriptionError and releases the
// subscription_id so it is immediately reusable (SPEC-005 §8.4).
func SendSubscriptionError(sender ClientSender, conn *Conn, msg *SubscriptionError) error {
	_ = conn.Subscriptions.Remove(msg.SubscriptionID)
	return sender.Send(conn.ID, *msg)
}

// SendOneOffQueryResult delivers a OneOffQueryResult. No subscription
// state change.
func SendOneOffQueryResult(sender ClientSender, connID types.ConnectionID, msg *OneOffQueryResult) error {
	return sender.Send(connID, *msg)
}
