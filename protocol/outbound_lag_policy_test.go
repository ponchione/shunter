package protocol

import "testing"

// TestShunterDefaultOutgoingBufferPolicy pins Shunter's independent retained
// memory policy. The byte ceiling is large enough for one maximum-sized
// uncompressed message and small enough to be a practical per-client bound.
func TestShunterDefaultOutgoingBufferPolicy(t *testing.T) {
	if DefaultOutgoingBufferMessages != 1024 {
		t.Fatalf("DefaultOutgoingBufferMessages = %d, want 1024", DefaultOutgoingBufferMessages)
	}
	opts := DefaultProtocolOptions()
	if opts.OutgoingBufferMessages != DefaultOutgoingBufferMessages {
		t.Fatalf("DefaultProtocolOptions().OutgoingBufferMessages = %d, want %d", opts.OutgoingBufferMessages, DefaultOutgoingBufferMessages)
	}
	if opts.MaxOutboundQueuedBytes != DefaultMaxOutboundQueuedBytes {
		t.Fatalf("MaxOutboundQueuedBytes = %d, want %d", opts.MaxOutboundQueuedBytes, DefaultMaxOutboundQueuedBytes)
	}
	if opts.MaxOutboundQueuedBytes <= int64(opts.MaxOutboundMessageSize) {
		t.Fatalf("MaxOutboundQueuedBytes = %d, want envelope headroom above MaxOutboundMessageSize %d", opts.MaxOutboundQueuedBytes, opts.MaxOutboundMessageSize)
	}
}
