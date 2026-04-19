package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestSubscribeSingleRoundTripSQLEmpty(t *testing.T) {
	in := SubscribeSingleMsg{
		RequestID:   42,
		QueryID:     7,
		QueryString: "SELECT * FROM accounts",
	}
	frame, err := EncodeClientMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	if frame[0] != TagSubscribeSingle {
		t.Errorf("tag = %d, want TagSubscribeSingle", frame[0])
	}

	tag, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagSubscribeSingle {
		t.Errorf("tag = %d, want TagSubscribeSingle", tag)
	}
	got := out.(SubscribeSingleMsg)
	if got != in {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, in)
	}
}

func TestSubscribeSingleRoundTripSQLWithPredicates(t *testing.T) {
	in := SubscribeSingleMsg{
		RequestID:   1,
		QueryID:     2,
		QueryString: "SELECT * FROM t WHERE a = 100 AND b = 'hello' AND c = true",
	}
	frame, err := EncodeClientMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(SubscribeSingleMsg)
	if got.QueryString != in.QueryString {
		t.Errorf("QueryString mismatch: got %q, want %q", got.QueryString, in.QueryString)
	}
}

func TestUnsubscribeRoundTripSendDroppedFalse(t *testing.T) {
	in := UnsubscribeSingleMsg{RequestID: 11, QueryID: 22, SendDropped: false}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(UnsubscribeSingleMsg)
	if got != in {
		t.Errorf("got %+v, want %+v", got, in)
	}
}

func TestUnsubscribeRoundTripSendDroppedTrue(t *testing.T) {
	in := UnsubscribeSingleMsg{RequestID: 11, QueryID: 22, SendDropped: true}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(UnsubscribeSingleMsg)
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

func TestOneOffQueryRoundTripSQL(t *testing.T) {
	in := OneOffQueryMsg{
		RequestID:   5,
		QueryString: "SELECT * FROM players WHERE level = 42",
	}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(OneOffQueryMsg)
	if got != in {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, in)
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
	// Tag=TagSubscribeSingle, but body stops after only 2 bytes of request_id.
	frame := []byte{TagSubscribeSingle, 0x01, 0x02}
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

func TestSubscribeMultiRoundTripSQL(t *testing.T) {
	orig := SubscribeMultiMsg{
		RequestID: 42,
		QueryID:   7,
		QueryStrings: []string{
			"SELECT * FROM users",
			"SELECT * FROM orders WHERE id = 9",
		},
	}
	frame, err := EncodeClientMessage(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	tag, decoded, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagSubscribeMulti {
		t.Fatalf("tag = %d, want %d", tag, TagSubscribeMulti)
	}
	got, ok := decoded.(SubscribeMultiMsg)
	if !ok {
		t.Fatalf("decoded type = %T, want SubscribeMultiMsg", decoded)
	}
	if got.RequestID != orig.RequestID || got.QueryID != orig.QueryID {
		t.Fatalf("ids = %+v, want %+v", got, orig)
	}
	if len(got.QueryStrings) != 2 || got.QueryStrings[0] != orig.QueryStrings[0] || got.QueryStrings[1] != orig.QueryStrings[1] {
		t.Fatalf("query strings = %+v, want %+v", got.QueryStrings, orig.QueryStrings)
	}
}

func TestUnsubscribeMultiRoundTrip(t *testing.T) {
	orig := UnsubscribeMultiMsg{RequestID: 3, QueryID: 99}
	frame, err := EncodeClientMessage(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	tag, decoded, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagUnsubscribeMulti {
		t.Fatalf("tag = %d, want %d", tag, TagUnsubscribeMulti)
	}
	if got, ok := decoded.(UnsubscribeMultiMsg); !ok || got != orig {
		t.Fatalf("decoded = %+v, want %+v", decoded, orig)
	}
}
