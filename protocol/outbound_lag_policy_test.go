package protocol

import "testing"

// TestShunterDefaultOutgoingBufferMatchesReference pins the outbound-lag
// decision recorded in
// `docs/shunter-design-decisions.md#outbound-lag-policy`: the per-connection outbound
// queue default matches the reference SpacetimeDB constant
// `CLIENT_CHANNEL_CAPACITY = 16 * KB` at
// `reference/SpacetimeDB/crates/core/src/client/client_connection.rs:657`.
func TestShunterDefaultOutgoingBufferMatchesReference(t *testing.T) {
	const referenceClientChannelCapacity = 16 * 1024
	if DefaultOutgoingBufferMessages != referenceClientChannelCapacity {
		t.Fatalf("DefaultOutgoingBufferMessages = %d, want %d (reference CLIENT_CHANNEL_CAPACITY)",
			DefaultOutgoingBufferMessages, referenceClientChannelCapacity)
	}
	if got := DefaultProtocolOptions().OutgoingBufferMessages; got != referenceClientChannelCapacity {
		t.Fatalf("DefaultProtocolOptions().OutgoingBufferMessages = %d, want %d",
			got, referenceClientChannelCapacity)
	}
}
