package protocol

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	if diff := cmp.Diff(in, got); diff != "" {
		t.Errorf("SubscribeSingleMsg round-trip mismatch (-want +got):\n%s", diff)
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

func TestUnsubscribeSingleRoundTrip(t *testing.T) {
	in := UnsubscribeSingleMsg{RequestID: 11, QueryID: 22}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(UnsubscribeSingleMsg)
	if diff := cmp.Diff(in, got); diff != "" {
		t.Errorf("UnsubscribeSingleMsg round-trip mismatch (-want +got):\n%s", diff)
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
	if diff := cmp.Diff(in, got); diff != "" {
		t.Errorf("CallReducerMsg round-trip mismatch (-want +got):\n%s", diff)
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

// TestOneOffQueryRoundTripSQL pins the SQL-string + 1c wire shape:
// SQL string query plus opaque `message_id` bytes.
func TestOneOffQueryRoundTripSQL(t *testing.T) {
	in := OneOffQueryMsg{
		MessageID:   []byte{0x05, 0x06},
		QueryString: "SELECT * FROM players WHERE level = 42",
	}
	frame, _ := EncodeClientMessage(in)
	_, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(OneOffQueryMsg)
	if diff := cmp.Diff(in, got); diff != "" {
		t.Errorf("OneOffQueryMsg round-trip mismatch (-want +got):\n%s", diff)
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

func TestDecodeClientMessageRejectsTrailingBytes(t *testing.T) {
	cases := []struct {
		name string
		msg  any
	}{
		{"SubscribeSingle", SubscribeSingleMsg{RequestID: 1, QueryID: 2, QueryString: "SELECT * FROM users"}},
		{"UnsubscribeSingle", UnsubscribeSingleMsg{RequestID: 3, QueryID: 4}},
		{"CallReducer", CallReducerMsg{ReducerName: "doit", Args: []byte{0x01}, RequestID: 5, Flags: CallReducerFlagsFullUpdate}},
		{"OneOffQuery", OneOffQueryMsg{MessageID: []byte{0x06}, QueryString: "SELECT * FROM users"}},
		{"SubscribeMulti", SubscribeMultiMsg{RequestID: 7, QueryID: 8, QueryStrings: []string{"SELECT * FROM users", "SELECT * FROM orders"}}},
		{"UnsubscribeMulti", UnsubscribeMultiMsg{RequestID: 9, QueryID: 10}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := EncodeClientMessage(tc.msg)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			frame = append(frame, 0xAA, 0xBB)
			_, _, err = DecodeClientMessage(frame)
			if !errors.Is(err, ErrMalformedMessage) {
				t.Fatalf("err = %v, want ErrMalformedMessage", err)
			}
		})
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
	if diff := cmp.Diff(orig.QueryStrings, got.QueryStrings); diff != "" {
		t.Fatalf("SubscribeMultiMsg query strings mismatch (-want +got):\n%s", diff)
	}
}

func TestSubscribeMultiDecodeRejectsImpossibleCountBeforeAllocation(t *testing.T) {
	var frame bytes.Buffer
	frame.WriteByte(TagSubscribeMulti)
	writeUint32(&frame, 1)
	writeUint32(&frame, 2)
	writeUint32(&frame, 1<<31)

	_, _, err := DecodeClientMessage(frame.Bytes())
	if !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage", err)
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
	got, ok := decoded.(UnsubscribeMultiMsg)
	if !ok {
		t.Fatalf("decoded type = %T, want UnsubscribeMultiMsg", decoded)
	}
	if diff := cmp.Diff(orig, got); diff != "" {
		t.Fatalf("UnsubscribeMultiMsg round-trip mismatch (-want +got):\n%s", diff)
	}
}
