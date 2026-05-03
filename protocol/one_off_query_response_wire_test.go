package protocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// TestShunterOneOffQueryResponseWireShapeSuccess pins the success wire shape
// for OneOffQueryResponse.
func TestShunterOneOffQueryResponseWireShapeSuccess(t *testing.T) {
	messageID := []byte{0xAA, 0xBB, 0xCC}
	tableName := "users"
	rows := EncodeRowList([][]byte{{0x01, 0x02}, {0x03}})
	const duration int64 = 0

	in := OneOffQueryResponse{
		MessageID: messageID,
		Tables: []OneOffTable{{
			TableName: tableName,
			Rows:      rows,
		}},
		TotalHostExecutionDuration: duration,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagOneOffQueryResponse)

	// message_id: Box<[u8]>
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(messageID)))
	want.Write(u32Buf[:])
	want.Write(messageID)

	// error: Option<Box<str>> = None
	want.WriteByte(0)

	// tables: Box<[OneOffTable]>
	binary.LittleEndian.PutUint32(u32Buf[:], 1)
	want.Write(u32Buf[:])
	// OneOffTable.table_name
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(tableName)))
	want.Write(u32Buf[:])
	want.WriteString(tableName)
	// OneOffTable.rows
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(rows)))
	want.Write(u32Buf[:])
	want.Write(rows)

	// total_host_execution_duration: i64
	var durBuf [8]byte
	binary.LittleEndian.PutUint64(durBuf[:], uint64(duration))
	want.Write(durBuf[:])

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("OneOffQueryResponse (success) wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagOneOffQueryResponse {
		t.Fatalf("tag = %d, want %d", tag, TagOneOffQueryResponse)
	}
	got, ok := out.(OneOffQueryResponse)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if !bytes.Equal(got.MessageID, messageID) {
		t.Errorf("MessageID = % x, want % x", got.MessageID, messageID)
	}
	if got.Error != nil {
		t.Errorf("Error = %v, want nil", got.Error)
	}
	if len(got.Tables) != 1 ||
		got.Tables[0].TableName != tableName ||
		!bytes.Equal(got.Tables[0].Rows, rows) {
		t.Errorf("Tables = %+v, want single %q entry", got.Tables, tableName)
	}
	if got.TotalHostExecutionDuration != duration {
		t.Errorf("TotalHostExecutionDuration = %d, want %d",
			got.TotalHostExecutionDuration, duration)
	}
}

// TestShunterOneOffQueryResponseWireShapeError pins the failure emission
// shape: `error: Some(msg), tables: []` — matching module_host.rs:2300.
func TestShunterOneOffQueryResponseWireShapeError(t *testing.T) {
	messageID := []byte{0x01, 0x02}
	errMsg := "bad query"
	const duration int64 = 0

	errStr := errMsg
	in := OneOffQueryResponse{
		MessageID:                  messageID,
		Error:                      &errStr,
		TotalHostExecutionDuration: duration,
	}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagOneOffQueryResponse)

	// message_id
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(messageID)))
	want.Write(u32Buf[:])
	want.Write(messageID)

	// error: Option<Box<str>> = Some(errMsg)
	want.WriteByte(1)
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(errMsg)))
	want.Write(u32Buf[:])
	want.WriteString(errMsg)

	// tables: empty Box<[OneOffTable]>
	binary.LittleEndian.PutUint32(u32Buf[:], 0)
	want.Write(u32Buf[:])

	// duration: i64 = 0
	want.Write(make([]byte, 8))

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("OneOffQueryResponse (error) wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := out.(OneOffQueryResponse)
	if got.Error == nil || *got.Error != errMsg {
		t.Errorf("Error = %v, want %q", got.Error, errMsg)
	}
	if len(got.Tables) != 0 {
		t.Errorf("Tables len = %d, want 0 on error", len(got.Tables))
	}
}

// TestShunterOneOffQueryResponseDurationNonZeroOnSuccess pins the
// receipt-timestamp seam at handleOneOffQuery entry: success emissions
// now carry a measured `TotalHostExecutionDuration` in microseconds.
// Before the seam the field always emitted 0 regardless of how long the
// scan took.
func TestShunterOneOffQueryResponseDurationNonZeroOnSuccess(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("users", 1, schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32})
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {{types.NewUint32(1)}},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x42},
		QueryString: "SELECT * FROM users",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	if result.TotalHostExecutionDuration == 0 {
		t.Fatal("TotalHostExecutionDuration = 0, want non-zero (receipt seam wired)")
	}
}

// TestShunterOneOffQueryResponseDurationNonZeroOnCompileFail pins the
// seam on the compile-short-circuit error path.
func TestShunterOneOffQueryResponseDurationNonZeroOnCompileFail(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("users", 1) // schema has no match for garbage query
	snap := &mockSnapshot{}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x43},
		QueryString: "THIS IS NOT SQL",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("Error = nil, want non-nil on compile-fail")
	}
	if result.TotalHostExecutionDuration == 0 {
		t.Fatal("TotalHostExecutionDuration = 0, want non-zero on compile-fail path")
	}
}

func TestHandleOneOffQuery_SQLTooLongRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("users", 1, schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32})
	stateAccess := snapshotPanicStateAccess{t: t}
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x44},
		QueryString: overlongSQLQuery(),
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("Error = nil, want non-nil on overlength SQL")
	}
	if !strings.Contains(*result.Error, "maximum allowed length") {
		t.Fatalf("Error = %q, want maximum allowed length message", *result.Error)
	}
	if result.TotalHostExecutionDuration == 0 {
		t.Fatal("TotalHostExecutionDuration = 0, want non-zero on overlength SQL path")
	}
}

type snapshotPanicStateAccess struct{ t *testing.T }

func (s snapshotPanicStateAccess) Snapshot() store.CommittedReadView {
	s.t.Fatal("Snapshot() called, want overlength SQL rejected before snapshot evaluation")
	return nil
}
