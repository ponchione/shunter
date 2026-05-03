package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// TestShunterSubscriptionErrorWireShape pins SubscriptionError field order and
// option encoding.
func TestShunterSubscriptionErrorWireShape(t *testing.T) {
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

// TestShunterSubscriptionErrorWireShapeAllNoneOptions pins the byte shape
// when every optional field is absent. The reference encoding of
// Option<T>::None is a single 0 tag byte. Duration remains present as
// u64 (non-optional per reference).
func TestShunterSubscriptionErrorWireShapeAllNoneOptions(t *testing.T) {
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

// TestShunterSubscriptionErrorTransactionOriginWire pins eval-origin
// SubscriptionError encoding with request/query/table IDs absent.
func TestShunterSubscriptionErrorTransactionOriginWire(t *testing.T) {
	capture := &captureSender{}
	adapter := NewFanOutSenderAdapter(capture)

	errMsg := "predicate rewrite failed"
	const measuredDuration uint64 = 4242
	in := subscription.SubscriptionError{
		RequestID:                        55,
		SubscriptionID:                   77,
		Message:                          errMsg,
		TotalHostExecutionDurationMicros: measuredDuration,
	}
	if err := adapter.SendSubscriptionError(types.ConnectionID{}, in); err != nil {
		t.Fatalf("SendSubscriptionError: %v", err)
	}

	if len(capture.generic) != 1 {
		t.Fatalf("generic sends = %d, want 1", len(capture.generic))
	}
	frame, err := EncodeServerMessage(capture.generic[0])
	if err != nil {
		t.Fatalf("encode captured: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagSubscriptionError)
	var durBuf [8]byte
	binary.LittleEndian.PutUint64(durBuf[:], measuredDuration)
	want.Write(durBuf[:])
	want.WriteByte(0) // request_id: None
	want.WriteByte(0) // query_id: None
	want.WriteByte(0) // table_id: None
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(errMsg)))
	want.Write(u32Buf[:])
	want.WriteString(errMsg)

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("TransactionUpdate-origin SubscriptionError wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}
}

// captureSender records every Send() message so the adapter→wire byte
// shape can be inspected.
type captureSender struct {
	generic []any
}

func (c *captureSender) Send(_ types.ConnectionID, msg any) error {
	c.generic = append(c.generic, msg)
	return nil
}
func (c *captureSender) SendTransactionUpdate(types.ConnectionID, *TransactionUpdate) error {
	return nil
}
func (c *captureSender) SendTransactionUpdateLight(types.ConnectionID, *TransactionUpdateLight) error {
	return nil
}
