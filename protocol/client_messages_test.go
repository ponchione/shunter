package protocol

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestSubscribeRoundTripEmptyPredicates(t *testing.T) {
	in := SubscribeMsg{
		RequestID: 42,
		QueryID:   7,
		Query: Query{
			TableName:  "accounts",
			Predicates: nil,
		},
	}
	frame, err := EncodeClientMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	if frame[0] != TagSubscribe {
		t.Errorf("tag = %d, want TagSubscribe", frame[0])
	}

	tag, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagSubscribe {
		t.Errorf("tag = %d, want TagSubscribe", tag)
	}
	got := out.(SubscribeMsg)
	if got.RequestID != in.RequestID || got.QueryID != in.QueryID {
		t.Errorf("ids mismatch: got %+v, want %+v", got, in)
	}
	if got.Query.TableName != in.Query.TableName {
		t.Errorf("table_name mismatch: got %q, want %q", got.Query.TableName, in.Query.TableName)
	}
	if len(got.Query.Predicates) != 0 {
		t.Errorf("expected 0 predicates, got %d", len(got.Query.Predicates))
	}
}

func TestSubscribeRoundTripMultiplePredicates(t *testing.T) {
	in := SubscribeMsg{
		RequestID: 1,
		QueryID:   2,
		Query: Query{
			TableName: "t",
			Predicates: []Predicate{
				{Column: "a", Value: types.NewUint64(100)},
				{Column: "b", Value: types.NewString("hello")},
				{Column: "c", Value: types.NewBool(true)},
			},
		},
	}
	frame, err := EncodeClientMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(SubscribeMsg)
	if len(got.Query.Predicates) != 3 {
		t.Fatalf("got %d predicates, want 3", len(got.Query.Predicates))
	}
	for i, want := range in.Query.Predicates {
		if got.Query.Predicates[i].Column != want.Column {
			t.Errorf("pred[%d].Column = %q, want %q", i, got.Query.Predicates[i].Column, want.Column)
		}
		if !got.Query.Predicates[i].Value.Equal(want.Value) {
			t.Errorf("pred[%d].Value mismatch", i)
		}
	}
}

func TestUnsubscribeRoundTripSendDroppedFalse(t *testing.T) {
	in := UnsubscribeMsg{RequestID: 11, QueryID: 22, SendDropped: false}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(UnsubscribeMsg)
	if got != in {
		t.Errorf("got %+v, want %+v", got, in)
	}
}

func TestUnsubscribeRoundTripSendDroppedTrue(t *testing.T) {
	in := UnsubscribeMsg{RequestID: 11, QueryID: 22, SendDropped: true}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(UnsubscribeMsg)
	if got != in {
		t.Errorf("got %+v, want %+v", got, in)
	}
}

func TestCallReducerRoundTrip(t *testing.T) {
	in := CallReducerMsg{
		RequestID:   99,
		ReducerName: "transfer",
		Args:        []byte{0xde, 0xad, 0xbe, 0xef},
		Flags:       CallReducerFlagsFullUpdate,
	}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(CallReducerMsg)
	if got.RequestID != in.RequestID || got.ReducerName != in.ReducerName {
		t.Errorf("ids mismatch: got %+v, want %+v", got, in)
	}
	if !bytes.Equal(got.Args, in.Args) {
		t.Errorf("args mismatch: got % x, want % x", got.Args, in.Args)
	}
	if got.Flags != in.Flags {
		t.Errorf("flags mismatch: got %d, want %d", got.Flags, in.Flags)
	}
}

// TestCallReducerFlagsNoSuccessNotifyRoundTrip pins that the NoSuccessNotify
// flag byte round-trips on the wire as a single trailing u8 after Args.
func TestCallReducerFlagsNoSuccessNotifyRoundTrip(t *testing.T) {
	in := CallReducerMsg{
		RequestID:   7,
		ReducerName: "fire_and_forget",
		Args:        []byte{0x01},
		Flags:       CallReducerFlagsNoSuccessNotify,
	}
	frame, _ := EncodeClientMessage(in)
	// Trailing byte of the frame must be the flags byte.
	if frame[len(frame)-1] != byte(CallReducerFlagsNoSuccessNotify) {
		t.Fatalf("trailing byte = %d, want %d (flags)", frame[len(frame)-1], CallReducerFlagsNoSuccessNotify)
	}
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(CallReducerMsg)
	if got.Flags != CallReducerFlagsNoSuccessNotify {
		t.Fatalf("Flags = %d, want %d (NoSuccessNotify)", got.Flags, CallReducerFlagsNoSuccessNotify)
	}
}

// TestCallReducerFlagsInvalidByteRejected pins that the decoder rejects
// flag bytes outside the defined range (0, 1). Matches reference
// impl_deserialize! behavior returning "invalid call reducer flag".
func TestCallReducerFlagsInvalidByteRejected(t *testing.T) {
	// Encode a valid message, mutate the trailing flags byte to 99.
	in := CallReducerMsg{RequestID: 1, ReducerName: "x", Args: nil, Flags: 0}
	frame, _ := EncodeClientMessage(in)
	frame[len(frame)-1] = 99
	_, _, err := DecodeClientMessage(frame)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for out-of-range flags byte", err)
	}
}

func TestCallReducerEmptyArgs(t *testing.T) {
	in := CallReducerMsg{RequestID: 1, ReducerName: "ping", Args: nil}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(CallReducerMsg)
	if len(got.Args) != 0 {
		t.Errorf("expected empty args, got % x", got.Args)
	}
}

func TestOneOffQueryRoundTrip(t *testing.T) {
	in := OneOffQueryMsg{
		RequestID: 5,
		TableName: "players",
		Predicates: []Predicate{
			{Column: "level", Value: types.NewUint32(42)},
		},
	}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(OneOffQueryMsg)
	if got.RequestID != in.RequestID || got.TableName != in.TableName {
		t.Errorf("field mismatch: got %+v, want %+v", got, in)
	}
	if len(got.Predicates) != 1 {
		t.Fatalf("predicate count = %d, want 1", len(got.Predicates))
	}
	if got.Predicates[0].Column != "level" || !got.Predicates[0].Value.Equal(types.NewUint32(42)) {
		t.Errorf("predicate mismatch: %+v", got.Predicates[0])
	}
}

func TestDecodeClientMessageUnknownTag(t *testing.T) {
	frame := []byte{99}
	_, _, err := DecodeClientMessage(frame)
	if !errors.Is(err, ErrUnknownMessageTag) {
		t.Errorf("got %v, want ErrUnknownMessageTag", err)
	}
}

func TestDecodeClientMessageEmptyFrame(t *testing.T) {
	_, _, err := DecodeClientMessage(nil)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("got %v, want ErrMalformedMessage", err)
	}
	_, _, err = DecodeClientMessage([]byte{})
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("empty: got %v, want ErrMalformedMessage", err)
	}
}

func TestDecodeClientMessageTruncatedBody(t *testing.T) {
	// Tag=TagSubscribe, but body stops after only 2 bytes of request_id.
	frame := []byte{TagSubscribe, 0x01, 0x02}
	_, _, err := DecodeClientMessage(frame)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("got %v, want ErrMalformedMessage", err)
	}
}

func TestEncodeClientMessageUnknownType(t *testing.T) {
	type bogus struct{}
	_, err := EncodeClientMessage(bogus{})
	if !errors.Is(err, ErrUnknownMessageTag) {
		t.Errorf("got %v, want ErrUnknownMessageTag", err)
	}
}
