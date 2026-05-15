package protocol

import (
	"testing"
	"time"
)

func TestDefaultProtocolOptions(t *testing.T) {
	opts := DefaultProtocolOptions()
	if opts.PingInterval != 15*time.Second {
		t.Errorf("PingInterval = %v, want 15s", opts.PingInterval)
	}
	if opts.IdleTimeout != 30*time.Second {
		t.Errorf("IdleTimeout = %v, want 30s", opts.IdleTimeout)
	}
	if opts.CloseHandshakeTimeout != 250*time.Millisecond {
		t.Errorf("CloseHandshakeTimeout = %v, want 250ms", opts.CloseHandshakeTimeout)
	}
	if opts.WriteTimeout != 5*time.Second {
		t.Errorf("WriteTimeout = %v, want 5s", opts.WriteTimeout)
	}
	if opts.DisconnectTimeout != 5*time.Second {
		t.Errorf("DisconnectTimeout = %v, want 5s", opts.DisconnectTimeout)
	}
	if opts.OutgoingBufferMessages != DefaultOutgoingBufferMessages {
		t.Errorf("OutgoingBufferMessages = %d, want %d", opts.OutgoingBufferMessages, DefaultOutgoingBufferMessages)
	}
	if opts.IncomingQueueMessages != 64 {
		t.Errorf("IncomingQueueMessages = %d, want 64", opts.IncomingQueueMessages)
	}
	if opts.MaxMessageSize != 4*1024*1024 {
		t.Errorf("MaxMessageSize = %d, want 4 MiB", opts.MaxMessageSize)
	}
}

func TestNormalizeProtocolOptionsFillsDefaults(t *testing.T) {
	opts, err := NormalizeProtocolOptions(ProtocolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if opts != DefaultProtocolOptions() {
		t.Fatalf("zero ProtocolOptions normalized to %#v, want %#v", opts, DefaultProtocolOptions())
	}
}

func TestNormalizeProtocolOptionsRejectsNegativeValues(t *testing.T) {
	if _, err := NormalizeProtocolOptions(ProtocolOptions{IncomingQueueMessages: -1}); err == nil {
		t.Fatal("expected negative IncomingQueueMessages to be rejected")
	}
	if _, err := NormalizeProtocolOptions(ProtocolOptions{WriteTimeout: -1}); err == nil {
		t.Fatal("expected negative WriteTimeout to be rejected")
	}
}

func TestGenerateConnectionIDNonZero(t *testing.T) {
	c := GenerateConnectionID()
	if c.IsZero() {
		t.Error("GenerateConnectionID returned zero value")
	}
}

func TestGenerateConnectionIDDistinct(t *testing.T) {
	a := GenerateConnectionID()
	b := GenerateConnectionID()
	if a == b {
		t.Error("two GenerateConnectionID calls must produce distinct values")
	}
}
