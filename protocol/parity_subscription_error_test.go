package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ponchione/shunter/schema"
)

// TestParitySubscriptionErrorWireShape pins the byte-level wire shape
// of SubscriptionError against the reference envelope at
// `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`
// (`pub struct SubscriptionError`). Reference field order:
//
//	total_host_execution_duration_micros: u64
//	request_id: Option<u32>
//	query_id:   Option<u32>
//	table_id:   Option<TableId>   // TableId = u32
//	error:      Box<str>
//
// Option<T> is encoded as a one-byte presence tag followed by the
// value when present (matches writeOptionalUint32 / writeOptionalTableID
// here). Box<str> is a LE u32 length prefix followed by the UTF-8
// payload. The test constructs the reference byte shape by hand and
// compares against the protocol encoder output; it also round-trips
// the frame through the decoder to prove the field-order change is
// symmetric.
func TestParitySubscriptionErrorWireShape(t *testing.T) {
	requestID := uint32(0x11223344)
	queryID := uint32(0x55667788)
	tableID := schema.TableID(0x99AABBCC)
	const duration uint64 = 0x0102030405060708
	errMsg := "table not found"

	in := SubscriptionError{
		TotalHostExecutionDurationMicros: duration,
		RequestID:                        &requestID,
		QueryID:                          &queryID,
		TableID:                          &tableID,
		Error:                            errMsg,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagSubscriptionError)

	// total_host_execution_duration_micros: u64 (reference field 0)
	var durBuf [8]byte
	binary.LittleEndian.PutUint64(durBuf[:], duration)
	want.Write(durBuf[:])

	// request_id: Option<u32>
	want.WriteByte(1)
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], requestID)
	want.Write(u32Buf[:])

	// query_id: Option<u32>
	want.WriteByte(1)
	binary.LittleEndian.PutUint32(u32Buf[:], queryID)
	want.Write(u32Buf[:])

	// table_id: Option<TableId> (u32)
	want.WriteByte(1)
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(tableID))
	want.Write(u32Buf[:])

	// error: Box<str>
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(errMsg)))
	want.Write(u32Buf[:])
	want.WriteString(errMsg)

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("SubscriptionError wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d", tag, TagSubscriptionError)
	}
	got, ok := out.(SubscriptionError)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got.TotalHostExecutionDurationMicros != duration {
		t.Errorf("TotalHostExecutionDurationMicros = %d, want %d",
			got.TotalHostExecutionDurationMicros, duration)
	}
	if got.RequestID == nil || *got.RequestID != requestID {
		t.Errorf("RequestID = %v, want %d", got.RequestID, requestID)
	}
	if got.QueryID == nil || *got.QueryID != queryID {
		t.Errorf("QueryID = %v, want %d", got.QueryID, queryID)
	}
	if got.TableID == nil || *got.TableID != tableID {
		t.Errorf("TableID = %v, want %d", got.TableID, tableID)
	}
	if got.Error != errMsg {
		t.Errorf("Error = %q, want %q", got.Error, errMsg)
	}
}

// TestParitySubscriptionErrorWireShapeAllNoneOptions pins the byte shape
// when every optional field is absent. The reference encoding of
// Option<T>::None is a single 0 tag byte. Duration remains present as
// u64 (non-optional per reference).
func TestParitySubscriptionErrorWireShapeAllNoneOptions(t *testing.T) {
	errMsg := "generic failure"
	in := SubscriptionError{
		TotalHostExecutionDurationMicros: 0,
		Error:                            errMsg,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagSubscriptionError)
	// duration: u64 = 0
	want.Write(make([]byte, 8))
	// request_id: None
	want.WriteByte(0)
	// query_id: None
	want.WriteByte(0)
	// table_id: None
	want.WriteByte(0)
	// error
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(errMsg)))
	want.Write(u32Buf[:])
	want.WriteString(errMsg)

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("None-option SubscriptionError wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}
}
