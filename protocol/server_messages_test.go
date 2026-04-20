package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestInitialConnectionRoundTrip(t *testing.T) {
	var id [32]byte
	var conn [16]byte
	for i := range id {
		id[i] = byte(i)
	}
	for i := range conn {
		conn[i] = byte(0xa0 + i)
	}
	in := InitialConnection{Identity: id, ConnectionID: conn, Token: "abc.def.ghi"}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	if frame[0] != TagInitialConnection {
		t.Errorf("tag = %d, want TagInitialConnection", frame[0])
	}
	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagInitialConnection {
		t.Errorf("tag = %d, want TagInitialConnection", tag)
	}
	got := out.(InitialConnection)
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
		RequestID: 123,
		QueryID:   456,
		TableName: "players",
		Rows:      rows,
	}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(SubscribeSingleApplied)
	if got.RequestID != in.RequestID || got.QueryID != in.QueryID ||
		got.TableName != in.TableName {
		t.Errorf("field mismatch: got %+v, want %+v", got, in)
	}
	if !bytes.Equal(got.Rows, in.Rows) {
		t.Errorf("rows payload differs: got % x, want % x", got.Rows, in.Rows)
	}
}

func TestUnsubscribeSingleAppliedHasRowsFalse(t *testing.T) {
	in := UnsubscribeSingleApplied{RequestID: 1, QueryID: 2, HasRows: false}
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
}

func TestUnsubscribeSingleAppliedHasRowsTrue(t *testing.T) {
	rows := EncodeRowList([][]byte{{0xaa}})
	in := UnsubscribeSingleApplied{RequestID: 1, QueryID: 2, HasRows: true, Rows: rows}
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
}

func TestSubscriptionErrorRoundTrip(t *testing.T) {
	in := SubscriptionError{RequestID: 10, QueryID: 20, Error: "table not found"}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(SubscriptionError)
	if got != in {
		t.Errorf("got %+v, want %+v", got, in)
	}
}

func TestTransactionUpdateHeavyCommittedRoundTrip(t *testing.T) {
	rl := EncodeRowList([][]byte{{0x01}})
	in := TransactionUpdate{
		Status: StatusCommitted{Update: []SubscriptionUpdate{
			{SubscriptionID: 1, TableName: "a", Inserts: rl, Deletes: nil},
			{SubscriptionID: 2, TableName: "b", Inserts: nil, Deletes: rl},
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

func TestTransactionUpdateHeavyOutOfEnergyRoundTrip(t *testing.T) {
	in := TransactionUpdate{
		Status:      StatusOutOfEnergy{},
		ReducerCall: ReducerCallInfo{ReducerName: "doit", RequestID: 1},
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
	if _, ok := got.Status.(StatusOutOfEnergy); !ok {
		t.Fatalf("Status = %T, want StatusOutOfEnergy", got.Status)
	}
}

func TestTransactionUpdateLightRoundTrip(t *testing.T) {
	rl := EncodeRowList([][]byte{{0x01}})
	in := TransactionUpdateLight{
		RequestID: 7,
		Update: []SubscriptionUpdate{
			{SubscriptionID: 1, TableName: "a", Inserts: rl},
			{SubscriptionID: 2, TableName: "b", Deletes: rl},
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

func TestOneOffQueryResultSuccess(t *testing.T) {
	rl := EncodeRowList([][]byte{{0x07}, {0x08}})
	in := OneOffQueryResult{MessageID: []byte{0x05, 0x06}, Status: 0, Rows: rl}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(OneOffQueryResult)
	if !bytes.Equal(got.MessageID, in.MessageID) || got.Status != 0 {
		t.Errorf("field mismatch: %+v", got)
	}
	if !bytes.Equal(got.Rows, rl) {
		t.Errorf("rows differ")
	}
	if got.Error != "" {
		t.Errorf("Error should be empty on success, got %q", got.Error)
	}
}

func TestOneOffQueryResultError(t *testing.T) {
	in := OneOffQueryResult{MessageID: []byte{0x05, 0x06}, Status: 1, Error: "bad query"}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(OneOffQueryResult)
	if !bytes.Equal(got.MessageID, in.MessageID) || got.Status != 1 || got.Error != "bad query" {
		t.Errorf("field mismatch: %+v", got)
	}
	if len(got.Rows) != 0 {
		t.Errorf("Rows should be empty on error, got len %d", len(got.Rows))
	}
}

// TagReducerCallResult is reserved in Phase 1.5. The decoder must
// reject it as unknown so a future reintroduction cannot silently
// collide with the removed shape. See
// `docs/parity-phase1.5-outcome-model.md`.
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
		RequestID: 1,
		QueryID:   2,
		Update: []SubscriptionUpdate{
			{SubscriptionID: 10, TableName: "users", Inserts: []byte{0x01}},
			{SubscriptionID: 11, TableName: "orders", Inserts: []byte{0x02}},
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
	if got.RequestID != 1 || got.QueryID != 2 || len(got.Update) != 2 {
		t.Fatalf("decoded = %+v", got)
	}
	if got.Update[0].SubscriptionID != 10 || got.Update[0].TableName != "users" {
		t.Fatalf("update[0] = %+v", got.Update[0])
	}
	if got.Update[1].SubscriptionID != 11 || got.Update[1].TableName != "orders" {
		t.Fatalf("update[1] = %+v", got.Update[1])
	}
}

func TestUnsubscribeMultiAppliedRoundTrip(t *testing.T) {
	orig := UnsubscribeMultiApplied{
		RequestID: 5,
		QueryID:   9,
		Update: []SubscriptionUpdate{
			{SubscriptionID: 10, TableName: "users", Deletes: []byte{0x03}},
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
	if !ok || got.RequestID != 5 || got.QueryID != 9 || len(got.Update) != 1 {
		t.Fatalf("decoded = %+v", decoded)
	}
	if got.Update[0].SubscriptionID != 10 || got.Update[0].TableName != "users" {
		t.Fatalf("update[0] = %+v", got.Update[0])
	}
}
