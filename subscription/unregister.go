package subscription

import "github.com/ponchione/shunter/types"

// Unregister removes a client from a subscription (SPEC-004 §4.2).
//
// v1 note: the optional "final delta" on unsubscribe is intentionally not
// emitted. The behavior is deferred rather than silently omitted; clients
// observe unsubscribe success and may resync on reconnect.
func (m *Manager) Unregister(connID types.ConnectionID, subID types.SubscriptionID) error {
	hash, last, ok := m.registry.removeSubscriber(connID, subID)
	if !ok {
		return ErrSubscriptionNotFound
	}
	if last {
		qs := m.registry.getQuery(hash)
		if qs != nil {
			RemoveSubscription(m.indexes, qs.predicate, hash)
		}
		m.registry.removeQueryState(hash)
	}
	return nil
}
