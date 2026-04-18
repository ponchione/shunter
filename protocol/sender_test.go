package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

func testConn(compression bool) (*Conn, types.ConnectionID) {
	id := types.ConnectionID{1}
	opts := DefaultProtocolOptions()
	c := &Conn{
		ID:            id,
		Compression:   compression,
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, opts.OutgoingBufferMessages),
		opts:          &opts,
		closed:        make(chan struct{}),
	}
	return c, id
}

func TestSendEnqueuesFrame(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	msg := SubscribeApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	if err := s.Send(id, msg); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-c.OutboundCh:
		if len(frame) == 0 {
			t.Fatal("empty frame")
		}
	default:
		t.Fatal("no frame enqueued")
	}
}

func TestSendWithCompressionWrapsEnvelope(t *testing.T) {
	c, id := testConn(true)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	msg := SubscribeApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	if err := s.Send(id, msg); err != nil {
		t.Fatal(err)
	}
	frame := <-c.OutboundCh
	// Compression-enabled frames start with compression byte (0x00 or 0x02).
	if frame[0] != CompressionNone && frame[0] != CompressionGzip {
		t.Fatalf("expected compression envelope, got first byte %d", frame[0])
	}
}

func TestSendConnNotFound(t *testing.T) {
	mgr := NewConnManager()
	s := NewClientSender(mgr, &fakeInbox{})
	id := types.ConnectionID{99}
	err := s.Send(id, SubscribeApplied{})
	if !errors.Is(err, ErrConnNotFound) {
		t.Fatalf("expected ErrConnNotFound, got %v", err)
	}
}

func TestSendBufferFull(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	id := types.ConnectionID{1}
	c := &Conn{
		ID:            id,
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 1),
		opts:          &opts,
		closed:        make(chan struct{}),
	}
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	// Fill buffer.
	msg := SubscribeApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	_ = s.Send(id, msg)

	// Second send should return buffer-full.
	err := s.Send(id, msg)
	if err == nil || !errors.Is(err, ErrClientBufferFull) {
		t.Fatalf("expected ErrClientBufferFull, got %v", err)
	}
}

func TestSendTransactionUpdateTypedHeavy(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	if err := c.Subscriptions.Reserve(1); err != nil {
		t.Fatal(err)
	}
	c.Subscriptions.Activate(1)

	update := &TransactionUpdate{
		Status: StatusCommitted{Update: []SubscriptionUpdate{
			{SubscriptionID: 1, TableName: "t", Inserts: []byte{1}, Deletes: []byte{}},
		}},
		ReducerCall: ReducerCallInfo{ReducerName: "x", RequestID: 9},
	}
	if err := s.SendTransactionUpdate(id, update); err != nil {
		t.Fatal(err)
	}
	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	tu, ok := msg.(TransactionUpdate)
	if !ok {
		t.Fatalf("expected TransactionUpdate, got %T", msg)
	}
	if tu.ReducerCall.RequestID != 9 {
		t.Fatalf("ReducerCall.RequestID = %d, want 9", tu.ReducerCall.RequestID)
	}
}

func TestSendTransactionUpdateTypedLight(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	if err := c.Subscriptions.Reserve(1); err != nil {
		t.Fatal(err)
	}
	c.Subscriptions.Activate(1)

	update := &TransactionUpdateLight{
		RequestID: 42,
		Update: []SubscriptionUpdate{
			{SubscriptionID: 1, TableName: "t", Inserts: []byte{1}, Deletes: []byte{}},
		},
	}
	if err := s.SendTransactionUpdateLight(id, update); err != nil {
		t.Fatal(err)
	}
	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	tu, ok := msg.(TransactionUpdateLight)
	if !ok {
		t.Fatalf("expected TransactionUpdateLight, got %T", msg)
	}
	if tu.RequestID != 42 {
		t.Fatalf("RequestID = %d, want 42", tu.RequestID)
	}
}
