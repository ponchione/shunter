package protocol

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

// The pins in this file are the rolled-up canonical-contract layer for
// the rows-shape cluster decided in
// `docs/parity-phase2-slice4-rows-shape.md`. Individual byte-shape
// pins continue to live in `parity_applied_envelopes_test.go` and
// `parity_transaction_update_test.go`; this file adds:
//
//   - a cross-envelope field-order table with the decision-doc
//     rationale inlined,
//   - the missing byte-shape pin for `TransactionUpdateLight`,
//   - an explicit `writeSubscriptionUpdates` inner-layout pin, and
//   - a rowlist-format pin referencing SPEC-005 §3.4.
//
// If a future slice closes the wrapper-chain parity (SubscribeRows /
// DatabaseUpdate / TableUpdate / CompressableQueryUpdate / BsatnRowList
// together), every pin in this file will need to flip together, by
// design.

// TestParityRowsShapeEnvelopesFlatShape pins the current Shunter flat-
// rows shape across every envelope that carries subscription row data.
// See delta audit rows #1-#6 in
// `docs/parity-phase2-slice4-rows-shape.md`.
func TestParityRowsShapeEnvelopesFlatShape(t *testing.T) {
	cases := []struct {
		name     string
		envelope any
		fields   []string
	}{
		{
			name:     "SubscribeSingleApplied (delta #1)",
			envelope: SubscribeSingleApplied{},
			fields:   []string{"RequestID", "TotalHostExecutionDurationMicros", "QueryID", "TableName", "Rows"},
		},
		{
			name:     "UnsubscribeSingleApplied (delta #2)",
			envelope: UnsubscribeSingleApplied{},
			fields:   []string{"RequestID", "TotalHostExecutionDurationMicros", "QueryID", "HasRows", "Rows"},
		},
		{
			name:     "SubscribeMultiApplied (delta #3)",
			envelope: SubscribeMultiApplied{},
			fields:   []string{"RequestID", "TotalHostExecutionDurationMicros", "QueryID", "Update"},
		},
		{
			name:     "UnsubscribeMultiApplied (delta #4)",
			envelope: UnsubscribeMultiApplied{},
			fields:   []string{"RequestID", "TotalHostExecutionDurationMicros", "QueryID", "Update"},
		},
		{
			name:     "TransactionUpdateLight (delta #5)",
			envelope: TransactionUpdateLight{},
			fields:   []string{"RequestID", "Update"},
		},
		{
			name:     "StatusCommitted (delta #6)",
			envelope: StatusCommitted{},
			fields:   []string{"Update"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := msgFieldNames(c.envelope); !reflect.DeepEqual(got, c.fields) {
				t.Fatalf("%s fields = %v, want %v (see docs/parity-phase2-slice4-rows-shape.md)",
					c.name, got, c.fields)
			}
		})
	}
}

// TestParityTransactionUpdateLightWireShape pins the byte-level wire
// shape of TransactionUpdateLight. Reference envelope at
// `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:493`:
//
//	request_id : u32
//	update     : DatabaseUpdate<F>
//
// Shunter flattens `DatabaseUpdate { tables: Vec<TableUpdate> }` to
// `[]SubscriptionUpdate`. That inner divergence is delta #5 / #10 in
// `docs/parity-phase2-slice4-rows-shape.md` and is accepted as a
// documented divergence. This pin locks the current flat shape so
// accidental wire drift is loudly visible.
func TestParityTransactionUpdateLightWireShape(t *testing.T) {
	const requestID uint32 = 0x01020304
	rl := EncodeRowList([][]byte{{0xAA, 0xBB}})
	update := []SubscriptionUpdate{
		{SubscriptionID: 7, TableName: "users", Inserts: rl, Deletes: nil},
	}

	in := TransactionUpdateLight{RequestID: requestID, Update: update}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagTransactionUpdateLight)
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], requestID)
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
		t.Fatalf("TransactionUpdateLight wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagTransactionUpdateLight {
		t.Fatalf("tag = %d, want %d", tag, TagTransactionUpdateLight)
	}
	got, ok := out.(TransactionUpdateLight)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got.RequestID != requestID {
		t.Fatalf("RequestID = %d, want %d", got.RequestID, requestID)
	}
	if len(got.Update) != 1 || got.Update[0].SubscriptionID != 7 ||
		got.Update[0].TableName != "users" ||
		!bytes.Equal(got.Update[0].Inserts, rl) ||
		len(got.Update[0].Deletes) != 0 {
		t.Fatalf("Update mismatch: %+v", got.Update)
	}
}

// TestParitySubscriptionUpdateInnerLayout pins the SubscriptionUpdate
// inner wire layout as a canonical contract. Locks:
//   - the Shunter-local `SubscriptionID` field (delta #3 in
//     `docs/parity-phase2-slice4-rows-shape.md`; reference has no
//     per-TableUpdate subscription id), and
//   - the inserts-before-deletes field order (delta #7; reference
//     QueryUpdate is deletes-first).
//
// Both are intentional Shunter divergences carried forward by the
// decision doc until the wrapper-chain close lands as its own slice.
func TestParitySubscriptionUpdateInnerLayout(t *testing.T) {
	fields := msgFieldNames(SubscriptionUpdate{})
	want := []string{"SubscriptionID", "TableName", "Inserts", "Deletes"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscriptionUpdate fields = %v, want %v", fields, want)
	}

	inserts := []byte{0x01, 0x02}
	deletes := []byte{0x03}
	in := []SubscriptionUpdate{
		{SubscriptionID: 11, TableName: "rooms", Inserts: inserts, Deletes: deletes},
	}

	var got bytes.Buffer
	writeSubscriptionUpdates(&got, in)

	var wantBuf bytes.Buffer
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(in)))
	wantBuf.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], in[0].SubscriptionID)
	wantBuf.Write(u32Buf[:])
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(in[0].TableName)))
	wantBuf.Write(u32Buf[:])
	wantBuf.WriteString(in[0].TableName)
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(inserts)))
	wantBuf.Write(u32Buf[:])
	wantBuf.Write(inserts)
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(deletes)))
	wantBuf.Write(u32Buf[:])
	wantBuf.Write(deletes)

	if !bytes.Equal(got.Bytes(), wantBuf.Bytes()) {
		t.Fatalf("writeSubscriptionUpdates layout mismatch\n got: % x\nwant: % x",
			got.Bytes(), wantBuf.Bytes())
	}
}

// TestParityRowsShapeRowListFormatReference pins the per-row-length-
// prefix EncodeRowList layout as the canonical Shunter rows-data
// format. See SPEC-005 §3.4
// (`docs/decomposition/005-protocol/SPEC-005-protocol.md:132-143`) and
// delta #10 in `docs/parity-phase2-slice4-rows-shape.md`. The reference
// `BsatnRowList { size_hint: RowSizeHint, rows_data: Bytes }` layout
// is deliberately deferred to v2.
func TestParityRowsShapeRowListFormatReference(t *testing.T) {
	rows := [][]byte{{0x01, 0x02}, {0x03}}

	got := EncodeRowList(rows)

	var want bytes.Buffer
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(rows)))
	want.Write(u32Buf[:])
	for _, r := range rows {
		binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(r)))
		want.Write(u32Buf[:])
		want.Write(r)
	}

	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("EncodeRowList layout mismatch\n got: % x\nwant: % x",
			got, want.Bytes())
	}

	decoded, err := DecodeRowList(got)
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	if len(decoded) != len(rows) {
		t.Fatalf("decoded len = %d, want %d", len(decoded), len(rows))
	}
	for i := range rows {
		if !bytes.Equal(decoded[i], rows[i]) {
			t.Fatalf("row %d mismatch: got % x, want % x", i, decoded[i], rows[i])
		}
	}
}
