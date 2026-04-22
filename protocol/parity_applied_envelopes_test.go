package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestParitySubscribeSingleAppliedWireShape pins the byte-level wire
// shape of SubscribeSingleApplied against the reference envelope at
// `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:317`
// (`pub struct SubscribeApplied<F>`). Reference field order:
//
//	request_id:                        u32
//	total_host_execution_duration_micros: u64
//	query_id:                          QueryId (u32)
//	rows:                              SubscribeRows<F>
//
// Shunter flattens `SubscribeRows { table_id, table_name, table_rows }`
// to `TableName (Box<str>) + Rows (Bytes)`. That rows-shape divergence is
// documented and pinned separately; this test only pins the field order.
func TestParitySubscribeSingleAppliedWireShape(t *testing.T) {
	const requestID uint32 = 0x11223344
	const queryID uint32 = 0xAABBCCDD
	const duration uint64 = 0x0102030405060708
	const tableName = "players"
	rows := EncodeRowList([][]byte{{0x01, 0x02}, {0x03}})

	in := SubscribeSingleApplied{
		RequestID:                        requestID,
		TotalHostExecutionDurationMicros: duration,
		QueryID:                          queryID,
		TableName:                        tableName,
		Rows:                             rows,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagSubscribeSingleApplied)
	var u32Buf [4]byte
	var u64Buf [8]byte
	binary.LittleEndian.PutUint32(u32Buf[:], requestID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint64(u64Buf[:], duration)
	want.Write(u64Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], queryID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(tableName)))
	want.Write(u32Buf[:])
	want.WriteString(tableName)
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(rows)))
	want.Write(u32Buf[:])
	want.Write(rows)

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("SubscribeSingleApplied wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagSubscribeSingleApplied {
		t.Fatalf("tag = %d, want %d", tag, TagSubscribeSingleApplied)
	}
	got, ok := out.(SubscribeSingleApplied)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got.RequestID != requestID || got.QueryID != queryID ||
		got.TotalHostExecutionDurationMicros != duration ||
		got.TableName != tableName || !bytes.Equal(got.Rows, rows) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, in)
	}
}

// TestParityUnsubscribeSingleAppliedWireShape pins the byte-level wire
// shape of UnsubscribeSingleApplied against the reference envelope at
// `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:331`
// (`pub struct UnsubscribeApplied<F>`). Reference field order:
//
//	request_id:                        u32
//	total_host_execution_duration_micros: u64
//	query_id:                          QueryId (u32)
//	rows:                              SubscribeRows<F>
//
// Shunter models `rows` as `HasRows (u8) + optional Rows (Bytes)`; the
// reference required-rows shape is a documented future slice.
func TestParityUnsubscribeSingleAppliedWireShape(t *testing.T) {
	const requestID uint32 = 0x44332211
	const queryID uint32 = 0xDDCCBBAA
	const duration uint64 = 0x1020304050607080
	rows := EncodeRowList([][]byte{{0xAA}, {0xBB, 0xCC}})

	in := UnsubscribeSingleApplied{
		RequestID:                        requestID,
		TotalHostExecutionDurationMicros: duration,
		QueryID:                          queryID,
		HasRows:                          true,
		Rows:                             rows,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagUnsubscribeSingleApplied)
	var u32Buf [4]byte
	var u64Buf [8]byte
	binary.LittleEndian.PutUint32(u32Buf[:], requestID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint64(u64Buf[:], duration)
	want.Write(u64Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], queryID)
	want.Write(u32Buf[:])
	want.WriteByte(1)
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(rows)))
	want.Write(u32Buf[:])
	want.Write(rows)

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("UnsubscribeSingleApplied wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagUnsubscribeSingleApplied {
		t.Fatalf("tag = %d, want %d", tag, TagUnsubscribeSingleApplied)
	}
	got, ok := out.(UnsubscribeSingleApplied)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got.RequestID != requestID || got.QueryID != queryID ||
		got.TotalHostExecutionDurationMicros != duration ||
		!got.HasRows || !bytes.Equal(got.Rows, rows) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, in)
	}
}

// TestParityUnsubscribeSingleAppliedWireShapeNoRows pins the HasRows=false
// byte shape: duration still sits at position 2; the rows Bytes length
// is absent because HasRows=0.
func TestParityUnsubscribeSingleAppliedWireShapeNoRows(t *testing.T) {
	const requestID uint32 = 7
	const queryID uint32 = 42
	const duration uint64 = 0xCAFEBABEDEADBEEF

	in := UnsubscribeSingleApplied{
		RequestID:                        requestID,
		TotalHostExecutionDurationMicros: duration,
		QueryID:                          queryID,
		HasRows:                          false,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagUnsubscribeSingleApplied)
	var u32Buf [4]byte
	var u64Buf [8]byte
	binary.LittleEndian.PutUint32(u32Buf[:], requestID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint64(u64Buf[:], duration)
	want.Write(u64Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], queryID)
	want.Write(u32Buf[:])
	want.WriteByte(0)

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("UnsubscribeSingleApplied (no rows) wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}
}

// TestParitySubscribeMultiAppliedWireShape pins the byte-level wire
// shape of SubscribeMultiApplied against the reference envelope at
// `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:380`
// (`pub struct SubscribeMultiApplied<F>`). Reference field order:
//
//	request_id:                        u32
//	total_host_execution_duration_micros: u64
//	query_id:                          QueryId (u32)
//	update:                            DatabaseUpdate<F>
//
// Shunter flattens `DatabaseUpdate { tables: Vec<TableUpdate> }` to
// `[]SubscriptionUpdate`; that rows-shape divergence is a documented
// future slice.
func TestParitySubscribeMultiAppliedWireShape(t *testing.T) {
	const requestID uint32 = 0x01020304
	const queryID uint32 = 0x05060708
	const duration uint64 = 0x1112131415161718
	rl := EncodeRowList([][]byte{{0x01}})
	update := []SubscriptionUpdate{
		{SubscriptionID: 9, TableName: "users", Inserts: rl, Deletes: nil},
	}

	in := SubscribeMultiApplied{
		RequestID:                        requestID,
		TotalHostExecutionDurationMicros: duration,
		QueryID:                          queryID,
		Update:                           update,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagSubscribeMultiApplied)
	var u32Buf [4]byte
	var u64Buf [8]byte
	binary.LittleEndian.PutUint32(u32Buf[:], requestID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint64(u64Buf[:], duration)
	want.Write(u64Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], queryID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(update)))
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], update[0].SubscriptionID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(update[0].TableName)))
	want.Write(u32Buf[:])
	want.WriteString(update[0].TableName)
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(rl)))
	want.Write(u32Buf[:])
	want.Write(rl)
	binary.LittleEndian.PutUint32(u32Buf[:], 0)
	want.Write(u32Buf[:])

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("SubscribeMultiApplied wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagSubscribeMultiApplied {
		t.Fatalf("tag = %d, want %d", tag, TagSubscribeMultiApplied)
	}
	got, ok := out.(SubscribeMultiApplied)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got.RequestID != requestID || got.QueryID != queryID ||
		got.TotalHostExecutionDurationMicros != duration ||
		len(got.Update) != 1 || got.Update[0].SubscriptionID != 9 ||
		got.Update[0].TableName != "users" {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, in)
	}
}

// TestParityUnsubscribeMultiAppliedWireShape pins the byte-level wire
// shape of UnsubscribeMultiApplied against the reference envelope at
// `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:394`
// (`pub struct UnsubscribeMultiApplied<F>`). Same field order as
// SubscribeMultiApplied — duration at position 2.
func TestParityUnsubscribeMultiAppliedWireShape(t *testing.T) {
	const requestID uint32 = 0xAAAA5555
	const queryID uint32 = 0x5555AAAA
	const duration uint64 = 0xF0E0D0C0B0A09080
	rl := EncodeRowList([][]byte{{0xFE, 0xDC}})
	update := []SubscriptionUpdate{
		{SubscriptionID: 1, TableName: "orders", Inserts: nil, Deletes: rl},
	}

	in := UnsubscribeMultiApplied{
		RequestID:                        requestID,
		TotalHostExecutionDurationMicros: duration,
		QueryID:                          queryID,
		Update:                           update,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagUnsubscribeMultiApplied)
	var u32Buf [4]byte
	var u64Buf [8]byte
	binary.LittleEndian.PutUint32(u32Buf[:], requestID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint64(u64Buf[:], duration)
	want.Write(u64Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], queryID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(update)))
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], update[0].SubscriptionID)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(update[0].TableName)))
	want.Write(u32Buf[:])
	want.WriteString(update[0].TableName)
	binary.LittleEndian.PutUint32(u32Buf[:], 0)
	want.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(rl)))
	want.Write(u32Buf[:])
	want.Write(rl)

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("UnsubscribeMultiApplied wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagUnsubscribeMultiApplied {
		t.Fatalf("tag = %d, want %d", tag, TagUnsubscribeMultiApplied)
	}
	got, ok := out.(UnsubscribeMultiApplied)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got.RequestID != requestID || got.QueryID != queryID ||
		got.TotalHostExecutionDurationMicros != duration ||
		len(got.Update) != 1 || got.Update[0].SubscriptionID != 1 ||
		got.Update[0].TableName != "orders" {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, in)
	}
}
