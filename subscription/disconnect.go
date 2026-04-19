package subscription

import "github.com/ponchione/shunter/types"

// DisconnectClient removes every subscription for the given connection
// (SPEC-004 §4.3). Equivalent to calling Unregister for each, but allows
// an implementation to batch pruning index updates in the future.
func (m *Manager) DisconnectClient(connID types.ConnectionID) error {
	subs := m.registry.subscriptionsForConn(connID)
	for _, s := range subs {
		// Ignore not-found to keep DisconnectClient idempotent.
		_ = m.Unregister(connID, s)
	}
	delete(m.querySets, connID)
	return nil
}
