package protocol

import "testing"

// TestPhase2Slice3DefaultOutgoingBufferMatchesReference pins the Phase 2
// Slice 3 decision recorded in
// `docs/parity-decisions.md#outbound-lag-policy`: the per-connection outbound
// queue default matches the reference SpacetimeDB constant
// `CLIENT_CHANNEL_CAPACITY = 16 * KB` at
// `reference/SpacetimeDB/crates/core/src/client/client_connection.rs:657`.
func TestPhase2Slice3DefaultOutgoingBufferMatchesReference(t *testing.T) {
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
