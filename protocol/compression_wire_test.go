package protocol

import (
	"bytes"
	"compress/gzip"
	"errors"
	"testing"
)

// TestShunterCompressionTagByteValues pins the reference byte
// numbering: 0x00 none, 0x01 brotli (reserved, unsupported), 0x02
// gzip. Reference outcome matched:
// crates/client-api-messages/src/websocket/common.rs
// SERVER_MSG_COMPRESSION_TAG_{NONE,BROTLI,GZIP}.
func TestShunterCompressionTagByteValues(t *testing.T) {
	if CompressionNone != 0x00 {
		t.Errorf("CompressionNone = 0x%02x, want 0x00", CompressionNone)
	}
	if CompressionBrotli != 0x01 {
		t.Errorf("CompressionBrotli = 0x%02x, want 0x01",
			CompressionBrotli)
	}
	if CompressionGzip != 0x02 {
		t.Errorf("CompressionGzip = 0x%02x, want 0x02", CompressionGzip)
	}
}

// TestShunterCompressionGzipEnvelopeByte pins the over-the-wire
// byte sequence so a reference-compatible client sees gzip signaled as
// 0x02.
func TestShunterCompressionGzipEnvelopeByte(t *testing.T) {
	frame, err := WrapCompressed(TagTransactionUpdate, bytes.Repeat([]byte("b"), DefaultGzipMinBytes),
		CompressionGzip)
	if err != nil {
		t.Fatalf("WrapCompressed gzip: %v", err)
	}
	if len(frame) < 2 {
		t.Fatalf("frame too short: %d", len(frame))
	}
	if frame[0] != 0x02 {
		t.Errorf("compression byte = 0x%02x, want 0x02 (gzip)", frame[0])
	}
	if frame[1] != TagTransactionUpdate {
		t.Errorf("tag byte = 0x%02x, want 0x%02x",
			frame[1], TagTransactionUpdate)
	}
	gr, err := gzip.NewReader(bytes.NewReader(frame[2:]))
	if err != nil {
		t.Fatalf("gzip decode: %v", err)
	}
	defer gr.Close()
}

// TestShunterCompressionBrotliReservedRejected pins the deferral:
// brotli is recognized as a known tag but Shunter does not implement
// it. Server-side emit must reject it; decode must return a dedicated
// ErrBrotliUnsupported (distinct from ErrUnknownCompressionTag) so
// callers can distinguish "reserved-but-unimplemented" from "bogus
// byte".
func TestShunterCompressionBrotliReservedRejected(t *testing.T) {
	_, err := WrapCompressed(TagTransactionUpdate, []byte("body"),
		CompressionBrotli)
	if err == nil {
		t.Fatal("WrapCompressed brotli: want error, got nil")
	}
	if !errors.Is(err, ErrBrotliUnsupported) {
		t.Errorf("err = %v, want ErrBrotliUnsupported", err)
	}

	// A frame arriving with 0x01 from a peer must decode to the same
	// reserved-unsupported error so the dispatch loop can close with
	// a specific reason.
	frame := []byte{CompressionBrotli, TagSubscribeSingle, 0xAA}
	_, _, derr := UnwrapCompressed(frame)
	if !errors.Is(derr, ErrBrotliUnsupported) {
		t.Errorf("UnwrapCompressed brotli err = %v, want ErrBrotliUnsupported",
			derr)
	}
}

func TestShunterNegotiatedGzipSenderFrame(t *testing.T) {
	conn, id := testConn(true)
	mgr := NewConnManager()
	mgr.Add(conn)
	sender := NewClientSender(mgr, &fakeInbox{})

	msg := TransactionUpdateLight{
		RequestID: 42,
		Update: []SubscriptionUpdate{
			{QueryID: 7, TableName: "messages", Inserts: bytes.Repeat([]byte{0x42}, DefaultGzipMinBytes)},
		},
	}
	if err := sender.SendTransactionUpdateLight(id, &msg); err != nil {
		t.Fatalf("SendTransactionUpdateLight: %v", err)
	}

	frame := <-conn.OutboundCh
	if frame[0] != CompressionGzip {
		t.Fatalf("compression byte = 0x%02x, want 0x02 (gzip)", frame[0])
	}
	tag, decoded := decodeOutboundServerFrame(t, conn, frame)
	if tag != TagTransactionUpdateLight {
		t.Fatalf("tag = %d, want %d", tag, TagTransactionUpdateLight)
	}
	out := decoded.(TransactionUpdateLight)
	if out.RequestID != msg.RequestID {
		t.Fatalf("RequestID = %d, want %d", out.RequestID, msg.RequestID)
	}
}
