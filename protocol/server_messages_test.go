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

func TestSubscribeAppliedRoundTrip(t *testing.T) {
	rows := EncodeRowList([][]byte{{0x01}, {0x02, 0x03}})
	in := SubscribeApplied{
		RequestID:      123,
		SubscriptionID: 456,
		TableName:      "players",
		Rows:           rows,
	}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(SubscribeApplied)
	if got.RequestID != in.RequestID || got.SubscriptionID != in.SubscriptionID ||
		got.TableName != in.TableName {
		t.Errorf("field mismatch: got %+v, want %+v", got, in)
	}
	if !bytes.Equal(got.Rows, in.Rows) {
		t.Errorf("rows payload differs: got % x, want % x", got.Rows, in.Rows)
	}
}

func TestUnsubscribeAppliedHasRowsFalse(t *testing.T) {
	in := UnsubscribeApplied{RequestID: 1, SubscriptionID: 2, HasRows: false}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(UnsubscribeApplied)
	if got.HasRows {
		t.Errorf("HasRows = true, want false")
	}
	if len(got.Rows) != 0 {
		t.Errorf("Rows should be empty when HasRows=false, got len %d", len(got.Rows))
	}
}

func TestUnsubscribeAppliedHasRowsTrue(t *testing.T) {
	rows := EncodeRowList([][]byte{{0xaa}})
	in := UnsubscribeApplied{RequestID: 1, SubscriptionID: 2, HasRows: true, Rows: rows}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(UnsubscribeApplied)
	if !got.HasRows {
		t.Error("HasRows = false, want true")
	}
	if !bytes.Equal(got.Rows, rows) {
		t.Errorf("rows payload differs")
	}
}

func TestSubscriptionErrorRoundTrip(t *testing.T) {
	in := SubscriptionError{RequestID: 10, SubscriptionID: 20, Error: "table not found"}
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

func TestTransactionUpdateEmptyUpdates(t *testing.T) {
	in := TransactionUpdate{TxID: 42, Updates: nil}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(TransactionUpdate)
	if got.TxID != in.TxID {
		t.Errorf("TxID mismatch")
	}
	if len(got.Updates) != 0 {
		t.Errorf("expected 0 updates, got %d", len(got.Updates))
	}
}

func TestTransactionUpdateMultipleUpdates(t *testing.T) {
	rl := EncodeRowList([][]byte{{0x01}})
	in := TransactionUpdate{
		TxID: 100,
		Updates: []SubscriptionUpdate{
			{SubscriptionID: 1, TableName: "a", Inserts: rl, Deletes: nil},
			{SubscriptionID: 2, TableName: "b", Inserts: nil, Deletes: rl},
			{SubscriptionID: 3, TableName: "c", Inserts: rl, Deletes: rl},
		},
	}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(TransactionUpdate)
	if got.TxID != 100 {
		t.Errorf("TxID = %d, want 100", got.TxID)
	}
	if len(got.Updates) != 3 {
		t.Fatalf("updates count = %d, want 3", len(got.Updates))
	}
	for i, want := range in.Updates {
		if got.Updates[i].SubscriptionID != want.SubscriptionID ||
			got.Updates[i].TableName != want.TableName {
			t.Errorf("update[%d] ids/name mismatch", i)
		}
		if !bytes.Equal(got.Updates[i].Inserts, want.Inserts) {
			t.Errorf("update[%d] inserts differ", i)
		}
		if !bytes.Equal(got.Updates[i].Deletes, want.Deletes) {
			t.Errorf("update[%d] deletes differ", i)
		}
	}
}

func TestOneOffQueryResultSuccess(t *testing.T) {
	rl := EncodeRowList([][]byte{{0x07}, {0x08}})
	in := OneOffQueryResult{RequestID: 5, Status: 0, Rows: rl}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(OneOffQueryResult)
	if got.RequestID != 5 || got.Status != 0 {
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
	in := OneOffQueryResult{RequestID: 5, Status: 1, Error: "bad query"}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(OneOffQueryResult)
	if got.Status != 1 || got.Error != "bad query" {
		t.Errorf("field mismatch: %+v", got)
	}
	if len(got.Rows) != 0 {
		t.Errorf("Rows should be empty on error, got len %d", len(got.Rows))
	}
}

func TestReducerCallResultCommittedWithUpdates(t *testing.T) {
	rl := EncodeRowList([][]byte{{0x42}})
	in := ReducerCallResult{
		RequestID: 7,
		Status:    0,
		TxID:      200,
		Energy:    0,
		TransactionUpdate: []SubscriptionUpdate{
			{SubscriptionID: 9, TableName: "x", Inserts: rl, Deletes: nil},
		},
	}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(ReducerCallResult)
	if got.RequestID != in.RequestID || got.Status != 0 || got.TxID != 200 {
		t.Errorf("field mismatch: %+v", got)
	}
	if got.Error != "" {
		t.Errorf("Error should be empty on committed, got %q", got.Error)
	}
	if len(got.TransactionUpdate) != 1 {
		t.Fatalf("updates count = %d, want 1", len(got.TransactionUpdate))
	}
	if !bytes.Equal(got.TransactionUpdate[0].Inserts, rl) {
		t.Errorf("updates[0].Inserts differ")
	}
}

func TestReducerCallResultFailedEmptyUpdates(t *testing.T) {
	in := ReducerCallResult{
		RequestID: 7,
		Status:    1, // failed_user
		TxID:      0,
		Error:     "reducer rejected input",
		// TransactionUpdate empty per spec when Status != 0
	}
	frame, _ := EncodeServerMessage(in)
	_, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(ReducerCallResult)
	if got.Status != 1 || got.Error != in.Error {
		t.Errorf("field mismatch: %+v", got)
	}
	if len(got.TransactionUpdate) != 0 {
		t.Errorf("updates should be empty on failure, got %d", len(got.TransactionUpdate))
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
