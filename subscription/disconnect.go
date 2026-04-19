package subscription

import "github.com/ponchione/shunter/types"

// DisconnectClient removes every subscription for the given connection
// (SPEC-004 §4.3). Drops registry entries plus pruning-index placements
// via dropSub; the querySets bucket for this ConnID is evicted wholesale
// at the end since every internal subID in it is being dropped.
func (m *Manager) DisconnectClient(connID types.ConnectionID) error {
	subs := m.registry.subscriptionsForConn(connID)
	for _, s := range subs {
		m.dropSub(connID, s)
	}
	delete(m.querySets, connID)
	return nil
}
