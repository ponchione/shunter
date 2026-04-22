package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestParityIdentityTokenWireShape pins the byte-level wire shape of
// IdentityToken against the reference envelope at
// `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:445`
// (`pub struct IdentityToken`). Reference field order:
//
//	identity:      Identity     (32 bytes)
//	token:         Box<str>     (LE u32 length + UTF-8)
//	connection_id: ConnectionId (16 bytes)
//
// The test constructs the reference byte shape by hand and compares
// against the encoder output; it also round-trips the frame through
// the decoder to prove the rename + field-order change is symmetric.
// Prior Shunter wire (pre-rename) used `identity, connection_id, token`
// under the type name `InitialConnection`.
func TestParityIdentityTokenWireShape(t *testing.T) {
	var identity [32]byte
	for i := range identity {
		identity[i] = byte(i + 1)
	}
	var connID [16]byte
	for i := range connID {
		connID[i] = byte(0xa0 + i)
	}
	const token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.x.y"

	in := IdentityToken{Identity: identity, Token: token, ConnectionID: connID}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagIdentityToken)
	want.Write(identity[:])

	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], uint32(len(token)))
	want.Write(u32Buf[:])
	want.WriteString(token)

	want.Write(connID[:])

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("IdentityToken wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}

	tag, out, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagIdentityToken {
		t.Fatalf("tag = %d, want %d", tag, TagIdentityToken)
	}
	got, ok := out.(IdentityToken)
	if !ok {
		t.Fatalf("decoded type = %T", out)
	}
	if got.Identity != identity {
		t.Errorf("Identity = % x, want % x", got.Identity, identity)
	}
	if got.Token != token {
		t.Errorf("Token = %q, want %q", got.Token, token)
	}
	if got.ConnectionID != connID {
		t.Errorf("ConnectionID = % x, want % x", got.ConnectionID, connID)
	}
}

// TestParityIdentityTokenWireShapeEmptyToken pins the byte shape when
// the token string is empty (the anonymous-connection case where the
// server has already handed the client its credentials out of band).
// Reference encoding of `Box<str>` with zero length is a LE u32 zero
// followed by no payload, so connection_id immediately follows.
func TestParityIdentityTokenWireShapeEmptyToken(t *testing.T) {
	var identity [32]byte
	for i := range identity {
		identity[i] = byte(0x10 + i)
	}
	var connID [16]byte
	for i := range connID {
		connID[i] = byte(0xb0 + i)
	}

	in := IdentityToken{Identity: identity, Token: "", ConnectionID: connID}

	frame, err := EncodeServerMessage(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var want bytes.Buffer
	want.WriteByte(TagIdentityToken)
	want.Write(identity[:])

	var u32Buf [4]byte
	binary.LittleEndian.PutUint32(u32Buf[:], 0)
	want.Write(u32Buf[:])

	want.Write(connID[:])

	if !bytes.Equal(frame, want.Bytes()) {
		t.Fatalf("empty-token IdentityToken wire shape mismatch\n got: % x\nwant: % x",
			frame, want.Bytes())
	}
}
