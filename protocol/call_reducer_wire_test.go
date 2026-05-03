package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestShunterCallReducerWireShape pins CallReducer field order and flags.
func TestShunterCallReducerWireShape(t *testing.T) {
	const reducerName = "transfer"
	args := []byte{0xde, 0xad, 0xbe, 0xef}
	const requestID uint32 = 0x11223344
	const flags byte = CallReducerFlagsNoSuccessNotify

	in := CallReducerMsg{
		ReducerName: reducerName,
		Args:        args,
		RequestID:   requestID,
		Flags:       flags,
	}

	frame, err := EncodeClientMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagCallReducer)

	// reducer: Box<str> — LE u32 length, UTF-8 payload.
	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(reducerName)))
	want.Write(u32Buf[:])
	want.WriteString(reducerName)

	// args: Bytes — LE u32 length, raw payload.
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(args)))
	want.Write(u32Buf[:])
	want.Write(args)

	// request_id: u32.
	binary.LittleEndian.PutUint32(u32Buf[:], requestID)
	want.Write(u32Buf[:])

	// flags: u8.
	want.WriteByte(flags)

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("CallReducer wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagCallReducer {
		t.Fatalf("tag = %d, want %d", tag, TagCallReducer)
	}
	got, ok := out.(CallReducerMsg)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got.ReducerName != reducerName {
		t.Errorf("ReducerName = %q, want %q", got.ReducerName, reducerName)
	}
	if !bytes.Equal(got.Args, args) {
		t.Errorf("Args = % x, want % x", got.Args, args)
	}
	if got.RequestID != requestID {
		t.Errorf("RequestID = %d, want %d", got.RequestID, requestID)
	}
	if got.Flags != flags {
		t.Errorf("Flags = %d, want %d", got.Flags, flags)
	}
}

// TestShunterCallReducerWireShapeEmptyArgs pins the byte shape when args
// is empty. Reference encoding of `Bytes` with zero length is a LE u32
// zero followed by no payload.
func TestShunterCallReducerWireShapeEmptyArgs(t *testing.T) {
	const reducerName = "ping"
	const requestID uint32 = 1

	in := CallReducerMsg{
		ReducerName: reducerName,
		Args:        nil,
		RequestID:   requestID,
		Flags:       CallReducerFlagsFullUpdate,
	}

	frame, err := EncodeClientMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagCallReducer)

	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(reducerName)))
	want.Write(u32Buf[:])
	want.WriteString(reducerName)

	// args length = 0, no payload.
	binary.LittleEndian.PutUint32(u32Buf[:], 0)
	want.Write(u32Buf[:])

	binary.LittleEndian.PutUint32(u32Buf[:], requestID)
	want.Write(u32Buf[:])

	want.WriteByte(CallReducerFlagsFullUpdate)

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("empty-args CallReducer wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}
}
