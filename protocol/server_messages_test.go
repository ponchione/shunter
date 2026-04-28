package protocol

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ponchione/shunter/schema"
)

func TestIdentityTokenRoundTrip(t *testing.T) {
	var id [32]byte
	var conn [16]byte
	for i := range id {
		id[i] = byte(i)
	}
	for i := range conn {
		conn[i] = byte(0xa0 + i)
	}
	in := IdentityToken{Identity: id, Token: "abc.def.ghi", ConnectionID: conn}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	if frame[0] != TagIdentityToken {
		t.Errorf("tag = %d, want TagIdentityToken", frame[0])
	}
	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagIdentityToken {
		t.Errorf("tag = %d, want TagIdentityToken", tag)
	}
	got := out.(IdentityToken)
	if got.Identity != in.Identity {
		t.Errorf("Identity mismatch: got % x, want % x", got.Identity, in.Identity)
	}
	if got.ConnectionID != in.ConnectionID {
		t.Errorf("ConnectionID mismatch: got % x, want % x", got.ConnectionID, in.ConnectionID)
	}
	if got.Token != in.Token {
		t.Errorf("Token mismatch: got %q, want %q", got.Token, in.Token)
	}
}

func TestSubscribeSingleAppliedRoundTrip(t *testing.T) {
	rows := EncodeRowList([][]byte{{0x01}, {0x02, 0x03}})
	in := SubscribeSingleApplied{
		RequestID:                        123,
		QueryID:                          456,
		TableName:                        "players",
		Rows:                             rows,
		TotalHostExecutionDurationMicros: 789,
	}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(SubscribeSingleApplied)
	if got.RequestID != in.RequestID || got.QueryID != in.QueryID ||
		got.TableName != in.TableName ||
		got.TotalHostExecutionDurationMicros != in.TotalHostExecutionDurationMicros {
		t.Errorf("field mismatch: got %+v, want %+v", got, in)
	}
	if !bytes.Equal(got.Rows, in.Rows) {
		t.Errorf("rows payload differs: got % x, want % x", got.Rows, in.Rows)
	}
}

func TestUnsubscribeSingleAppliedHasRowsFalse(t *testing.T) {
	in := UnsubscribeSingleApplied{RequestID: 1, QueryID: 2, HasRows: false, TotalHostExecutionDurationMicros: 33}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(UnsubscribeSingleApplied)
	if got.HasRows {
		t.Errorf("HasRows = true, want false")
	}
	if len(got.Rows) != 0 {
		t.Errorf("Rows should be empty when HasRows=false, got len %d", len(got.Rows))
	}
	if got.TotalHostExecutionDurationMicros != in.TotalHostExecutionDurationMicros {
		t.Errorf("TotalHostExecutionDurationMicros = %d, want %d", got.TotalHostExecutionDurationMicros, in.TotalHostExecutionDurationMicros)
	}
}

func TestUnsubscribeSingleAppliedHasRowsTrue(t *testing.T) {
	rows := EncodeRowList([][]byte{{0xaa}})
	in := UnsubscribeSingleApplied{RequestID: 1, QueryID: 2, HasRows: true, Rows: rows, TotalHostExecutionDurationMicros: 44}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(UnsubscribeSingleApplied)
	if !got.HasRows {
		t.Error("HasRows = false, want true")
	}
	if !bytes.Equal(got.Rows, rows) {
		t.Errorf("rows payload differs")
	}
	if got.TotalHostExecutionDurationMicros != in.TotalHostExecutionDurationMicros {
		t.Errorf("TotalHostExecutionDurationMicros = %d, want %d", got.TotalHostExecutionDurationMicros, in.TotalHostExecutionDurationMicros)
	}
}

func TestSubscriptionErrorRoundTrip(t *testing.T) {
	requestID := uint32(10)
	queryID := uint32(20)
	tableID := schema.TableID(30)
	in := SubscriptionError{RequestID: &requestID, QueryID: &queryID, TableID: &tableID, Error: "table not found"}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(SubscriptionError)
	if got.RequestID == nil || *got.RequestID != requestID {
		t.Fatalf("RequestID = %v, want %d", got.RequestID, requestID)
	}
	if got.QueryID == nil || *got.QueryID != queryID {
		t.Fatalf("QueryID = %v, want %d", got.QueryID, queryID)
	}
	if got.TableID == nil || *got.TableID != tableID {
		t.Fatalf("TableID = %v, want %d", got.TableID, tableID)
	}
	if got.Error != in.Error {
		t.Fatalf("Error = %q, want %q", got.Error, in.Error)
	}
}

func TestTransactionUpdateHeavyCommittedRoundTrip(t *testing.T) {
	rl := EncodeRowList([][]byte{{0x01}})
	in := TransactionUpdate{
		Status: StatusCommitted{Update: []SubscriptionUpdate{
			{QueryID: 1, TableName: "a", Inserts: rl, Deletes: nil},
			{QueryID: 2, TableName: "b", Inserts: nil, Deletes: rl},
		}},
		ReducerCall: ReducerCallInfo{ReducerName: "doit", ReducerID: 5, Args: []byte{0xCA}, RequestID: 7},
	}
	in.CallerConnectionID[0] = 0xAB
	in.CallerIdentity[0] = 0xCD
	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(TransactionUpdate)
	committed, ok := got.Status.(StatusCommitted)
	if !ok {
		t.Fatalf("Status = %T, want StatusCommitted", got.Status)
	}
	if len(committed.Update) != 2 {
		t.Fatalf("committed updates count = %d, want 2", len(committed.Update))
	}
	if got.ReducerCall.ReducerName != "doit" || got.ReducerCall.RequestID != 7 {
		t.Errorf("ReducerCall mismatch: %+v", got.ReducerCall)
	}
	if got.CallerConnectionID[0] != 0xAB || got.CallerIdentity[0] != 0xCD {
		t.Error("caller bytes not round-tripped")
	}
	if !bytes.Equal(committed.Update[0].Inserts, rl) {
		t.Error("update[0] inserts differ")
	}
	_ = in.Timestamp
}

func TestTransactionUpdateHeavyFailedRoundTrip(t *testing.T) {
	in := TransactionUpdate{
		Status:      StatusFailed{Error: "boom"},
		ReducerCall: ReducerCallInfo{ReducerName: "doit", RequestID: 3},
	}
	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(TransactionUpdate)
	failed, ok := got.Status.(StatusFailed)
	if !ok {
		t.Fatalf("Status = %T, want StatusFailed", got.Status)
	}
	if failed.Error != "boom" {
		t.Errorf("Error = %q, want 'boom'", failed.Error)
	}
}

func TestTransactionUpdateRejectsOutOfEnergyStatusTag(t *testing.T) {
	_, _, err := DecodeServerMessage([]byte{TagTransactionUpdate, 2})
	if err == nil {
		t.Fatal("DecodeServerMessage accepted retired OutOfEnergy status tag")
	}
	if !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage", err)
	}
}

func TestTransactionUpdateRejectsLegacyEnergyField(t *testing.T) {
	in := TransactionUpdate{
		Status:                     StatusFailed{Error: "boom"},
		ReducerCall:                ReducerCallInfo{ReducerName: "doit", RequestID: 3},
		TotalHostExecutionDuration: 77,
	}
	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	legacy := make([]byte, 0, len(frame)+16)
	legacy = append(legacy, frame[:len(frame)-8]...)
	legacy = append(legacy, make([]byte, 16)...)
	legacy = append(legacy, frame[len(frame)-8:]...)

	_, _, err = DecodeServerMessage(legacy)
	if err == nil {
		t.Fatal("DecodeServerMessage accepted legacy energy-bearing TransactionUpdate")
	}
	if !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage", err)
	}
}

func TestDecodeServerMessageRejectsTrailingBytes(t *testing.T) {
	requestID := uint32(10)
	queryID := uint32(20)
	tableID := schema.TableID(30)
	rows := EncodeRowList([][]byte{{0x01}})
	cases := []struct {
		name string
		msg  any
	}{
		{"IdentityToken", IdentityToken{Token: "token"}},
		{"SubscribeSingleApplied", SubscribeSingleApplied{RequestID: 1, QueryID: 2, TableName: "users", Rows: rows}},
		{"UnsubscribeSingleAppliedNoRows", UnsubscribeSingleApplied{RequestID: 3, QueryID: 4}},
		{"UnsubscribeSingleAppliedRows", UnsubscribeSingleApplied{RequestID: 5, QueryID: 6, HasRows: true, Rows: rows}},
		{"SubscriptionError", SubscriptionError{RequestID: &requestID, QueryID: &queryID, TableID: &tableID, Error: "boom"}},
		{"TransactionUpdate", TransactionUpdate{Status: StatusCommitted{}, ReducerCall: ReducerCallInfo{ReducerName: "doit"}}},
		{"TransactionUpdateLight", TransactionUpdateLight{RequestID: 7, Update: []SubscriptionUpdate{{QueryID: 8, TableName: "users", Inserts: rows}}}},
		{"OneOffQueryResponse", OneOffQueryResponse{MessageID: []byte{0x09}, Tables: []OneOffTable{{TableName: "users", Rows: rows}}}},
		{"SubscribeMultiApplied", SubscribeMultiApplied{RequestID: 11, QueryID: 12, Update: []SubscriptionUpdate{{QueryID: 13, TableName: "users", Inserts: rows}}}},
		{"UnsubscribeMultiApplied", UnsubscribeMultiApplied{RequestID: 14, QueryID: 15, Update: []SubscriptionUpdate{{QueryID: 16, TableName: "users", Deletes: rows}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := EncodeServerMessage(tc.msg)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			frame = append(frame, 0xAA, 0xBB)
			_, _, err = DecodeServerMessage(frame)
			if !errors.Is(err, ErrMalformedMessage) {
				t.Fatalf("err = %v, want ErrMalformedMessage", err)
			}
		})
	}
}

func TestDecodeServerMessageRejectsImpossibleArrayCountsBeforeAllocation(t *testing.T) {
	t.Run("one-off tables", func(t *testing.T) {
		var frame bytes.Buffer
		frame.WriteByte(TagOneOffQueryResponse)
		writeBytes(&frame, nil)
		writeOptionalString(&frame, nil)
		writeUint32(&frame, 1<<31)

		_, _, err := DecodeServerMessage(frame.Bytes())
		if !errors.Is(err, ErrMalformedMessage) {
			t.Fatalf("err = %v, want ErrMalformedMessage", err)
		}
	})

	t.Run("subscription updates", func(t *testing.T) {
		var frame bytes.Buffer
		frame.WriteByte(TagTransactionUpdateLight)
		writeUint32(&frame, 7)
		writeUint32(&frame, 1<<31)

		_, _, err := DecodeServerMessage(frame.Bytes())
		if !errors.Is(err, ErrMalformedMessage) {
			t.Fatalf("err = %v, want ErrMalformedMessage", err)
		}
	})
}

func TestTransactionUpdateLightRoundTrip(t *testing.T) {
	rl := EncodeRowList([][]byte{{0x01}})
	in := TransactionUpdateLight{
		RequestID: 7,
		Update: []SubscriptionUpdate{
			{QueryID: 1, TableName: "a", Inserts: rl},
			{QueryID: 2, TableName: "b", Deletes: rl},
		},
	}
	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(TransactionUpdateLight)
	if got.RequestID != 7 {
		t.Errorf("RequestID = %d, want 7", got.RequestID)
	}
	if len(got.Update) != 2 {
		t.Fatalf("Update count = %d, want 2", len(got.Update))
	}
}

func TestOneOffQueryResponseSuccess(t *testing.T) {
	rl := EncodeRowList([][]byte{{0x07}, {0x08}})
	in := OneOffQueryResponse{
		MessageID: []byte{0x05, 0x06},
		Tables:    []OneOffTable{{TableName: "users", Rows: rl}},
	}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(OneOffQueryResponse)
	if !bytes.Equal(got.MessageID, in.MessageID) {
		t.Errorf("MessageID mismatch: %+v", got)
	}
	if got.Error != nil {
		t.Errorf("Error should be nil on success, got %v", *got.Error)
	}
	if len(got.Tables) != 1 || got.Tables[0].TableName != "users" || !bytes.Equal(got.Tables[0].Rows, rl) {
		t.Errorf("Tables mismatch: %+v", got.Tables)
	}
}

func TestOneOffQueryResponseError(t *testing.T) {
	msg := "bad query"
	in := OneOffQueryResponse{MessageID: []byte{0x05, 0x06}, Error: &msg}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(OneOffQueryResponse)
	if !bytes.Equal(got.MessageID, in.MessageID) {
		t.Errorf("MessageID mismatch: %+v", got)
	}
	if got.Error == nil || *got.Error != "bad query" {
		t.Errorf("Error = %v, want %q", got.Error, "bad query")
	}
	if len(got.Tables) != 0 {
		t.Errorf("Tables should be empty on error, got len %d", len(got.Tables))
	}
}

// TagReducerCallResult is reserved in Phase 1.5. The decoder must
// reject it as unknown so a future reintroduction cannot silently
// collide with the removed shape. See
// `docs/parity-decisions.md#outcome-model`.
func TestTagReducerCallResultIsReserved(t *testing.T) {
	_, _, err := DecodeServerMessage([]byte{TagReducerCallResult})
	if err == nil {
		t.Fatal("DecodeServerMessage(TagReducerCallResult) succeeded, want unknown-tag error")
	}
	if !errors.Is(err, ErrUnknownMessageTag) {
		t.Errorf("err = %v, want ErrUnknownMessageTag", err)
	}
}

func TestDecodeServerMessageUnknownTag(t *testing.T) {
	_, _, err := DecodeServerMessage([]byte{99})
	if !errors.Is(err, ErrUnknownMessageTag) {
		t.Errorf("got %v, want ErrUnknownMessageTag", err)
	}
}

func TestDecodeServerMessageEmptyFrame(t *testing.T) {
	_, _, err := DecodeServerMessage(nil)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("got %v, want ErrMalformedMessage", err)
	}
}

func TestEncodeServerMessageUnknownType(t *testing.T) {
	type bogus struct{}
	_, err := EncodeServerMessage(bogus{})
	if !errors.Is(err, ErrUnknownMessageTag) {
		t.Errorf("got %v, want ErrUnknownMessageTag", err)
	}
}

func TestSubscribeMultiAppliedRoundTrip(t *testing.T) {
	orig := SubscribeMultiApplied{
		RequestID:                        1,
		QueryID:                          2,
		TotalHostExecutionDurationMicros: 55,
		Update: []SubscriptionUpdate{
			{QueryID: 10, TableName: "users", Inserts: []byte{0x01}},
			{QueryID: 11, TableName: "orders", Inserts: []byte{0x02}},
		},
	}
	frame, err := EncodeServerMessage(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	tag, decoded, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagSubscribeMultiApplied {
		t.Fatalf("tag = %d, want %d", tag, TagSubscribeMultiApplied)
	}
	got, ok := decoded.(SubscribeMultiApplied)
	if !ok {
		t.Fatalf("decoded type = %T", decoded)
	}
	if got.RequestID != 1 || got.QueryID != 2 || len(got.Update) != 2 || got.TotalHostExecutionDurationMicros != orig.TotalHostExecutionDurationMicros {
		t.Fatalf("decoded = %+v", got)
	}
	if got.Update[0].QueryID != 10 || got.Update[0].TableName != "users" {
		t.Fatalf("update[0] = %+v", got.Update[0])
	}
	if got.Update[1].QueryID != 11 || got.Update[1].TableName != "orders" {
		t.Fatalf("update[1] = %+v", got.Update[1])
	}
}

func TestUnsubscribeMultiAppliedRoundTrip(t *testing.T) {
	orig := UnsubscribeMultiApplied{
		RequestID:                        5,
		QueryID:                          9,
		TotalHostExecutionDurationMicros: 66,
		Update: []SubscriptionUpdate{
			{QueryID: 10, TableName: "users", Deletes: []byte{0x03}},
		},
	}
	frame, err := EncodeServerMessage(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	tag, decoded, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagUnsubscribeMultiApplied {
		t.Fatalf("tag = %d, want %d", tag, TagUnsubscribeMultiApplied)
	}
	got, ok := decoded.(UnsubscribeMultiApplied)
	if !ok || got.RequestID != 5 || got.QueryID != 9 || len(got.Update) != 1 || got.TotalHostExecutionDurationMicros != orig.TotalHostExecutionDurationMicros {
		t.Fatalf("decoded = %+v", decoded)
	}
	if got.Update[0].QueryID != 10 || got.Update[0].TableName != "users" {
		t.Fatalf("update[0] = %+v", got.Update[0])
	}
}
