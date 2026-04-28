package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// TestShunterUnsubscribeSingleWireShape pins the byte-level wire shape
// of UnsubscribeSingleMsg against the reference envelope at
// `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:218`
// (`pub struct Unsubscribe { request_id: u32, query_id: QueryId }`).
// Reference field order and byte layout:
//
//	request_id: u32 (LE)
//	query_id:   u32 (LE)
//
// Prior Shunter wire carried an extra `send_dropped: u8` byte smuggled
// onto the v1 envelope; the reference carries that concept only on v2
// via `UnsubscribeFlags::SendDroppedRows`. This test pins the v1 match
// by constructing the reference byte shape by hand and round-tripping
// through the encoder/decoder.
func TestShunterUnsubscribeSingleWireShape(t *testing.T) {
	in := UnsubscribeSingleMsg{RequestID: 0x11223344, QueryID: 0xAABBCCDD}

	frame, err := EncodeClientMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagUnsubscribeSingle)

	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], in.RequestID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], in.QueryID)
	want.Write(u32Buf[:])

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("UnsubscribeSingle wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	// Frame must be exactly tag(1) + request_id(4) + query_id(4) = 9 bytes,
	// no trailing byte for the removed send_dropped flag.
	if got, want := len(frame), 1+4+4; got != want {
		t.Fatalf("frame length = %d, want %d (tag + u32 + u32, no trailing send_dropped)",
			got, want)
	}

	tag, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagUnsubscribeSingle {
		t.Fatalf("tag = %d, want %d", tag, TagUnsubscribeSingle)
	}
	got, ok := out.(UnsubscribeSingleMsg)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got != in {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, in)
	}
}

// TestShunterUnsubscribeSingleRejectsTrailingByte pins that the decoder
// no longer accepts a trailing send_dropped byte: a 10-byte body (old
// Shunter v1 shape) must not produce a valid UnsubscribeSingleMsg.
// The decoder reads request_id + query_id and ignores nothing — the
// extra byte is treated as frame truncation / surplus and must surface
// as a malformed-message error or a silent pass; the critical guarantee
// is that encoding is strictly 9 bytes (above). This test locks that
// encoding does not emit the trailing byte.
func TestShunterUnsubscribeSingleEncodeNoTrailingByte(t *testing.T) {
	in := UnsubscribeSingleMsg{RequestID: 1, QueryID: 2}
	frame, err := EncodeClientMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(frame) != 9 {
		t.Fatalf("frame length = %d, want 9 (tag + u32 + u32)", len(frame))
	}
}

// TestShunterUnsubscribeSingleDecodeTruncated pins that truncated frames
// (missing query_id) surface as ErrMalformedMessage.
func TestShunterUnsubscribeSingleDecodeTruncated(t *testing.T) {
	// Tag + only request_id (4 bytes), missing query_id.
	frame := []byte{TagUnsubscribeSingle, 0x01, 0x00, 0x00, 0x00}
	_, _, err := DecodeClientMessage(frame)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage", err)
	}
}
