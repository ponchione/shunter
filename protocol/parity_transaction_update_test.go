package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestParityTransactionUpdateWireShape pins the byte-level Shunter-native
// wire shape of TransactionUpdate:
//
//	status:                     UpdateStatus     (tagged union)
//	timestamp:                  i64 µs since Unix epoch
//	caller_identity:            Identity         (32 bytes)
//	caller_connection_id:       ConnectionId     (16 bytes)
//	reducer_call:               ReducerCallInfo
//	total_host_execution_duration: i64 µs
//
// The test constructs the expected byte shape by hand and compares
// against EncodeServerMessage, then round-trips through
// DecodeServerMessage to prove the field-order change is symmetric.
func TestParityTransactionUpdateWireShape(t *testing.T) {
	var identity [32]byte
	for i := range identity {
		identity[i] = byte(0x10 + i)
	}
	var connID [16]byte
	for i := range connID {
		connID[i] = byte(0xA0 + i)
	}
	const timestamp int64 = 0x0102030405060708
	const duration int64 = 0x7766554433221100
	rci := ReducerCallInfo{
		ReducerName: "transfer",
		ReducerID:   0xCAFEBABE,
		Args:        []byte{0xDE, 0xAD, 0xBE, 0xEF},
		RequestID:   0x11223344,
	}
	rl := EncodeRowList([][]byte{{0x01}, {0x02, 0x03}})
	status := StatusCommitted{Update: []SubscriptionUpdate{
		{QueryID: 7, TableName: "users", Inserts: rl, Deletes: nil},
	}}

	in := TransactionUpdate{
		Status:                     status,
		Timestamp:                  timestamp,
		CallerIdentity:             identity,
		CallerConnectionID:         connID,
		ReducerCall:                rci,
		TotalHostExecutionDuration: duration,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagTransactionUpdate)

	// status: UpdateStatus — Committed tag (0) then SubscriptionUpdate
	// list: LE u32 count, then for each entry:
	// query_id (u32), table_name (Box<str>), inserts (Bytes),
	// deletes (Bytes).
	want.WriteByte(updateStatusTagCommitted)
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], 1)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], 7)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len("users")))
	want.Write(u32Buf[:])
	want.WriteString("users")
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(rl)))
	want.Write(u32Buf[:])
	want.Write(rl)
	binary.LittleEndian.PutUint32(u32Buf[:], 0)
	want.Write(u32Buf[:])

	// timestamp: i64 (reference field 1)
	var i64Buf [8]byte
	binary.LittleEndian.PutUint64(i64Buf[:], uint64(timestamp))
	want.Write(i64Buf[:])

	// caller_identity: 32 raw bytes
	want.Write(identity[:])

	// caller_connection_id: 16 raw bytes
	want.Write(connID[:])

	// reducer_call: ReducerCallInfo — reducer_name (Box<str>),
	// reducer_id (u32), args (Bytes), request_id (u32).
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(rci.ReducerName)))
	want.Write(u32Buf[:])
	want.WriteString(rci.ReducerName)
	binary.LittleEndian.PutUint32(u32Buf[:], rci.ReducerID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(rci.Args)))
	want.Write(u32Buf[:])
	want.Write(rci.Args)
	binary.LittleEndian.PutUint32(u32Buf[:], rci.RequestID)
	want.Write(u32Buf[:])

	// total_host_execution_duration: i64
	binary.LittleEndian.PutUint64(i64Buf[:], uint64(duration))
	want.Write(i64Buf[:])

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("TransactionUpdate wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagTransactionUpdate {
		t.Fatalf("tag = %d, want %d", tag, TagTransactionUpdate)
	}
	got, ok := out.(TransactionUpdate)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got.Timestamp != timestamp {
		t.Errorf("Timestamp = %d, want %d", got.Timestamp, timestamp)
	}
	if got.CallerIdentity != identity {
		t.Errorf("CallerIdentity mismatch")
	}
	if got.CallerConnectionID != connID {
		t.Errorf("CallerConnectionID mismatch")
	}
	if got.ReducerCall.ReducerName != rci.ReducerName ||
		got.ReducerCall.ReducerID != rci.ReducerID ||
		got.ReducerCall.RequestID != rci.RequestID ||
		!bytes.Equal(got.ReducerCall.Args, rci.Args) {
		t.Errorf("ReducerCall mismatch: got %+v, want %+v", got.ReducerCall, rci)
	}
	if got.TotalHostExecutionDuration != duration {
		t.Errorf("TotalHostExecutionDuration = %d, want %d", got.TotalHostExecutionDuration, duration)
	}
	committed, ok := got.Status.(StatusCommitted)
	if !ok {
		t.Fatalf("Status = %T, want StatusCommitted", got.Status)
	}
	if len(committed.Update) != 1 || committed.Update[0].QueryID != 7 ||
		committed.Update[0].TableName != "users" {
		t.Errorf("Status Update mismatch: %+v", committed.Update)
	}
}

// TestParityTransactionUpdateWireShapeFailed pins the byte shape when
// Status is the Failed arm — UpdateStatus tag byte is 1 followed by a
// Box<str> error message, and the rest of the envelope follows the
// reference field order.
func TestParityTransactionUpdateWireShapeFailed(t *testing.T) {
	const errMsg = "reducer panicked"
	const timestamp int64 = 42

	in := TransactionUpdate{
		Status:    StatusFailed{Error: errMsg},
		Timestamp: timestamp,
		ReducerCall: ReducerCallInfo{
			ReducerName: "doit",
			RequestID:   3,
		},
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagTransactionUpdate)

	// status: Failed tag + error Box<str>
	want.WriteByte(updateStatusTagFailed)
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(errMsg)))
	want.Write(u32Buf[:])
	want.WriteString(errMsg)

	// timestamp: i64
	var i64Buf [8]byte
	binary.LittleEndian.PutUint64(i64Buf[:], uint64(timestamp))
	want.Write(i64Buf[:])

	// caller_identity + caller_connection_id — zero bytes
	want.Write(make([]byte, 32))
	want.Write(make([]byte, 16))

	// reducer_call
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len("doit")))
	want.Write(u32Buf[:])
	want.WriteString("doit")
	binary.LittleEndian.PutUint32(u32Buf[:], 0)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], 0)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], 3)
	want.Write(u32Buf[:])

	// total_host_execution_duration (i64) — zero
	want.Write(make([]byte, 8))

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("Failed TransactionUpdate wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}
}
