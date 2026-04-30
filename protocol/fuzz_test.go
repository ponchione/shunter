package protocol

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func FuzzDecodeRowList(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0, 0, 0, 0},
		{1, 0, 0, 0},
		{1, 0, 0, 0, 3, 0, 0, 0, 0xaa, 0xbb, 0xcc},
		{2, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0x42},
		{100, 0, 0, 0, 1, 0, 0, 0, 0x42},
		{0xff, 0xff, 0xff, 0xff},
		{1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff},
	} {
		f.Add(seed)
	}

	const maxRowListFuzzBytes = 64 << 10
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxRowListFuzzBytes {
			t.Skip("rowlist fuzz input above bounded local limit")
		}

		rows, err := DecodeRowList(data)
		if err != nil {
			if !errors.Is(err, ErrMalformedMessage) {
				t.Fatalf("DecodeRowList(%x) error = %v, want ErrMalformedMessage category", data, err)
			}
			return
		}

		encoded := EncodeRowList(rows)
		roundTrip, err := DecodeRowList(encoded)
		if err != nil {
			t.Fatalf("DecodeRowList(EncodeRowList(rows)) after accepting %x: %v", data, err)
		}
		if len(roundTrip) != len(rows) {
			t.Fatalf("row count after canonical round trip = %d, want %d for input %x", len(roundTrip), len(rows), data)
		}
		for i := range rows {
			if !bytes.Equal(roundTrip[i], rows[i]) {
				t.Fatalf("row %d after canonical round trip = %x, want %x for input %x", i, roundTrip[i], rows[i], data)
			}
		}
	})
}

func FuzzDecodeClientMessage(f *testing.F) {
	for _, seed := range fuzzClientMessageSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, frame []byte) {
		fuzzDecodeMessage(t, frame, DecodeClientMessage, EncodeClientMessage, "client")
	})
}

func FuzzDecodeServerMessage(f *testing.F) {
	for _, seed := range fuzzServerMessageSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, frame []byte) {
		fuzzDecodeMessage(t, frame, DecodeServerMessage, EncodeServerMessage, "server")
	})
}

func fuzzDecodeMessage(t *testing.T, frame []byte, decode func([]byte) (uint8, any, error), encode func(any) ([]byte, error), label string) {
	t.Helper()
	const maxMessageFuzzBytes = 64 << 10
	if len(frame) > maxMessageFuzzBytes {
		t.Skip(label + " message fuzz input above bounded local limit")
	}

	tag, msg, err := decode(frame)
	if err != nil {
		if !errors.Is(err, ErrMalformedMessage) && !errors.Is(err, ErrUnknownMessageTag) {
			t.Fatalf("Decode%sMessage(%x) error = %v, want protocol decode category", label, frame, err)
		}
		return
	}

	encoded, err := encode(msg)
	if err != nil {
		t.Fatalf("Encode%sMessage(%T) after accepting tag %d frame %x: %v", label, msg, tag, frame, err)
	}
	roundTag, roundMsg, err := decode(encoded)
	if err != nil {
		t.Fatalf("Decode%sMessage(Encode%sMessage(%T)) after accepting frame %x: %v", label, label, msg, frame, err)
	}
	if roundTag != tag {
		t.Fatalf("%s message canonical tag = %d, want %d for frame %x", label, roundTag, tag, frame)
	}
	if !reflect.DeepEqual(roundMsg, msg) {
		t.Fatalf("%s message canonical round trip = %#v, want %#v for frame %x", label, roundMsg, msg, frame)
	}
}

func fuzzClientMessageSeeds() [][]byte {
	var seeds [][]byte
	for _, msg := range []any{
		SubscribeSingleMsg{RequestID: 1, QueryID: 2, QueryString: "SELECT * FROM players"},
		UnsubscribeSingleMsg{RequestID: 3, QueryID: 4},
		CallReducerMsg{ReducerName: "insert_player", Args: []byte{0x01, 0x02}, RequestID: 5, Flags: CallReducerFlagsFullUpdate},
		CallReducerMsg{ReducerName: "fire_and_forget", Args: nil, RequestID: 6, Flags: CallReducerFlagsNoSuccessNotify},
		OneOffQueryMsg{MessageID: []byte("query"), QueryString: "SELECT * FROM players WHERE id = 1"},
		DeclaredQueryMsg{MessageID: []byte("declared"), Name: "recent_players"},
		SubscribeMultiMsg{RequestID: 7, QueryID: 8, QueryStrings: []string{"SELECT * FROM players", "SELECT * FROM teams"}},
		SubscribeDeclaredViewMsg{RequestID: 9, QueryID: 10, Name: "live_players"},
		UnsubscribeMultiMsg{RequestID: 11, QueryID: 12},
	} {
		seed, err := EncodeClientMessage(msg)
		if err != nil {
			panic(err)
		}
		seeds = append(seeds, seed)
	}
	return append(seeds,
		nil,
		[]byte{0xff},
		[]byte{TagCallReducer, 0, 0, 0, 0},
		[]byte{TagSubscribeMulti, 1, 0, 0, 0, 2, 0, 0, 0, 0xff, 0xff, 0xff, 0xff},
	)
}

func fuzzServerMessageSeeds() [][]byte {
	errText := "subscription failed"
	requestID := uint32(13)
	queryID := uint32(14)
	rows := EncodeRowList([][]byte{{0x01}, {0x02, 0x03}})
	var identity [32]byte
	var connID [16]byte
	for i := range identity {
		identity[i] = byte(i)
	}
	for i := range connID {
		connID[i] = byte(0xa0 + i)
	}

	var seeds [][]byte
	for _, msg := range []any{
		IdentityToken{Identity: identity, Token: "token", ConnectionID: connID},
		SubscribeSingleApplied{RequestID: 1, TotalHostExecutionDurationMicros: 2, QueryID: 3, TableName: "players", Rows: rows},
		UnsubscribeSingleApplied{RequestID: 4, TotalHostExecutionDurationMicros: 5, QueryID: 6, HasRows: true, Rows: rows},
		UnsubscribeSingleApplied{RequestID: 4, TotalHostExecutionDurationMicros: 5, QueryID: 6},
		SubscriptionError{TotalHostExecutionDurationMicros: 7, RequestID: &requestID, QueryID: &queryID, Error: errText},
		SubscribeMultiApplied{RequestID: 8, TotalHostExecutionDurationMicros: 9, QueryID: 10, Update: []SubscriptionUpdate{{QueryID: 10, TableName: "players", Inserts: rows}}},
		UnsubscribeMultiApplied{RequestID: 11, TotalHostExecutionDurationMicros: 12, QueryID: 13, Update: []SubscriptionUpdate{{QueryID: 13, TableName: "players", Deletes: rows}}},
		TransactionUpdate{Status: StatusCommitted{Update: []SubscriptionUpdate{{QueryID: 14, TableName: "players", Inserts: rows}}}, Timestamp: 15, CallerIdentity: identity, CallerConnectionID: connID, ReducerCall: ReducerCallInfo{ReducerName: "insert_player", Args: []byte{0x04}, RequestID: 16}, TotalHostExecutionDuration: 17},
		TransactionUpdate{Status: StatusFailed{Error: "boom"}, Timestamp: 18, CallerIdentity: identity, CallerConnectionID: connID, ReducerCall: ReducerCallInfo{ReducerName: "fail", RequestID: 19}, TotalHostExecutionDuration: 20},
		TransactionUpdateLight{RequestID: 21, Update: []SubscriptionUpdate{{QueryID: 22, TableName: "players", Inserts: rows}}},
		OneOffQueryResponse{MessageID: []byte("oneoff"), Error: &errText, TotalHostExecutionDuration: 23},
		OneOffQueryResponse{MessageID: []byte("oneoff-ok"), Tables: []OneOffTable{{TableName: "players", Rows: rows}}, TotalHostExecutionDuration: 24},
	} {
		seed, err := EncodeServerMessage(msg)
		if err != nil {
			panic(err)
		}
		seeds = append(seeds, seed)
	}
	return append(seeds,
		nil,
		[]byte{0xff},
		[]byte{TagTransactionUpdate, 2},
		[]byte{TagSubscribeSingleApplied, 1, 0, 0, 0},
	)
}
