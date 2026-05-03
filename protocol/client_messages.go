package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// SubscribeSingleMsg subscribes one SQL query under a client QueryID.
type SubscribeSingleMsg struct {
	RequestID   uint32
	QueryID     uint32
	QueryString string
}

// UnsubscribeSingleMsg removes one query from a client QueryID.
type UnsubscribeSingleMsg struct {
	RequestID uint32
	QueryID   uint32
}

// CallReducerMsg invokes a reducer with raw BSATN-encoded arguments.
type CallReducerMsg struct {
	ReducerName string
	Args        []byte
	RequestID   uint32
	Flags       byte
}

// CallReducer flags. Unknown wire values are rejected as malformed.
const (
	// CallReducerFlagsFullUpdate sends the normal success TransactionUpdate.
	CallReducerFlagsFullUpdate byte = 0
	// CallReducerFlagsNoSuccessNotify suppresses committed success echoes only.
	CallReducerFlagsNoSuccessNotify byte = 1
)

// OneOffQueryMsg executes one raw SQL query with an opaque message ID.
type OneOffQueryMsg struct {
	MessageID   []byte
	QueryString string
}

// DeclaredQueryMsg executes a module-owned QueryDeclaration by name. The
// server receives Name as the execution authority; it must not infer declared
// reads from matching raw SQL text.
type DeclaredQueryMsg struct {
	MessageID []byte
	Name      string
}

// SubscribeMultiMsg subscribes multiple SQL query strings under one QueryID.
type SubscribeMultiMsg struct {
	RequestID    uint32
	QueryID      uint32
	QueryStrings []string
}

// SubscribeDeclaredViewMsg subscribes to a module-owned ViewDeclaration by
// name. The wire carries the declaration name rather than the authored SQL.
type SubscribeDeclaredViewMsg struct {
	RequestID uint32
	QueryID   uint32
	Name      string
}

// UnsubscribeMultiMsg drops every query registered under the given
// QueryID in one call. Reference: UnsubscribeMulti at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:229.
type UnsubscribeMultiMsg struct {
	RequestID uint32
	QueryID   uint32
}

// EncodeClientMessage produces a wire frame: [tag byte] [BSATN body].
func EncodeClientMessage(m any) ([]byte, error) {
	if err := validateClientMessageForEncode(m); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	switch msg := m.(type) {
	case SubscribeSingleMsg:
		buf.WriteByte(TagSubscribeSingle)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		writeString(&buf, msg.QueryString)
	case UnsubscribeSingleMsg:
		buf.WriteByte(TagUnsubscribeSingle)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
	case CallReducerMsg:
		buf.WriteByte(TagCallReducer)
		writeString(&buf, msg.ReducerName)
		writeBytes(&buf, msg.Args)
		writeUint32(&buf, msg.RequestID)
		buf.WriteByte(msg.Flags)
	case OneOffQueryMsg:
		buf.WriteByte(TagOneOffQuery)
		writeBytes(&buf, msg.MessageID)
		writeString(&buf, msg.QueryString)
	case DeclaredQueryMsg:
		buf.WriteByte(TagDeclaredQuery)
		writeBytes(&buf, msg.MessageID)
		writeString(&buf, msg.Name)
	case SubscribeMultiMsg:
		buf.WriteByte(TagSubscribeMulti)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		writeUint32(&buf, uint32(len(msg.QueryStrings)))
		for _, qs := range msg.QueryStrings {
			writeString(&buf, qs)
		}
	case SubscribeDeclaredViewMsg:
		buf.WriteByte(TagSubscribeDeclaredView)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		writeString(&buf, msg.Name)
	case UnsubscribeMultiMsg:
		buf.WriteByte(TagUnsubscribeMulti)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnknownMessageTag, m)
	}
	return buf.Bytes(), nil
}

// DecodeClientMessage parses a wire frame into its concrete message
// type. The returned any is one of SubscribeSingleMsg, UnsubscribeSingleMsg,
// CallReducerMsg, OneOffQueryMsg, DeclaredQueryMsg, SubscribeMultiMsg,
// SubscribeDeclaredViewMsg, UnsubscribeMultiMsg — matching the tag byte.
func DecodeClientMessage(frame []byte) (uint8, any, error) {
	if len(frame) < 1 {
		return 0, nil, fmt.Errorf("%w: empty frame", ErrMalformedMessage)
	}
	tag := frame[0]
	msg, err := decodeClientMessageParts(tag, frame[1:])
	if err != nil {
		return 0, nil, err
	}
	return tag, msg, nil
}

func decodeClientMessageParts(tag uint8, body []byte) (any, error) {
	switch tag {
	case TagSubscribeSingle:
		return decodeSubscribeSingle(body)
	case TagUnsubscribeSingle:
		return decodeUnsubscribeSingle(body)
	case TagCallReducer:
		return decodeCallReducer(body)
	case TagOneOffQuery:
		return decodeOneOffQuery(body)
	case TagDeclaredQuery:
		return decodeDeclaredQuery(body)
	case TagSubscribeMulti:
		return decodeSubscribeMulti(body)
	case TagSubscribeDeclaredView:
		return decodeSubscribeDeclaredView(body)
	case TagUnsubscribeMulti:
		return decodeUnsubscribeMulti(body)
	default:
		return nil, fmt.Errorf("%w: tag=%d", ErrUnknownMessageTag, tag)
	}
}

// --- Per-message decoders ---

func decodeSubscribeSingle(body []byte) (SubscribeSingleMsg, error) {
	var m SubscribeSingleMsg
	var off int
	var err error
	if m.RequestID, m.QueryID, off, err = readRequestQueryID(body); err != nil {
		return m, err
	}
	if m.QueryString, off, err = readString(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "SubscribeSingle"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeUnsubscribeSingle(body []byte) (UnsubscribeSingleMsg, error) {
	var m UnsubscribeSingleMsg
	var off int
	var err error
	if m.RequestID, m.QueryID, off, err = readRequestQueryID(body); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "UnsubscribeSingle"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeCallReducer(body []byte) (CallReducerMsg, error) {
	var m CallReducerMsg
	var off int
	var err error
	if m.ReducerName, off, err = readString(body, 0); err != nil {
		return m, err
	}
	if m.Args, off, err = readBytes(body, off); err != nil {
		return m, err
	}
	if m.RequestID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if len(body)-off < 1 {
		return m, fmt.Errorf("%w: CallReducer flags byte truncated", ErrMalformedMessage)
	}
	m.Flags = body[off]
	off++
	if !validCallReducerFlags(m.Flags) {
		return m, fmt.Errorf("%w: invalid CallReducer flags byte %d", ErrMalformedMessage, m.Flags)
	}
	if err := requireFullyConsumed(body, off, "CallReducer"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeOneOffQuery(body []byte) (OneOffQueryMsg, error) {
	var m OneOffQueryMsg
	var off int
	var err error
	if m.MessageID, off, err = readBytes(body, 0); err != nil {
		return m, err
	}
	if m.QueryString, off, err = readString(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "OneOffQuery"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeDeclaredQuery(body []byte) (DeclaredQueryMsg, error) {
	var m DeclaredQueryMsg
	var off int
	var err error
	if m.MessageID, off, err = readBytes(body, 0); err != nil {
		return m, err
	}
	if m.Name, off, err = readString(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "DeclaredQuery"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeSubscribeMulti(body []byte) (SubscribeMultiMsg, error) {
	var m SubscribeMultiMsg
	var off int
	var err error
	if m.RequestID, m.QueryID, off, err = readRequestQueryID(body); err != nil {
		return m, err
	}
	count, off, err := readUint32(body, off)
	if err != nil {
		return m, err
	}
	if err := requireCountFitsRemaining("SubscribeMulti query_strings", count, body, off, 4); err != nil {
		return m, err
	}
	m.QueryStrings = make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		s, next, serr := readString(body, off)
		if serr != nil {
			return m, serr
		}
		off = next
		m.QueryStrings = append(m.QueryStrings, s)
	}
	if err := requireFullyConsumed(body, off, "SubscribeMulti"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeSubscribeDeclaredView(body []byte) (SubscribeDeclaredViewMsg, error) {
	var m SubscribeDeclaredViewMsg
	var off int
	var err error
	if m.RequestID, m.QueryID, off, err = readRequestQueryID(body); err != nil {
		return m, err
	}
	if m.Name, off, err = readString(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "SubscribeDeclaredView"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeUnsubscribeMulti(body []byte) (UnsubscribeMultiMsg, error) {
	var m UnsubscribeMultiMsg
	var off int
	var err error
	if m.RequestID, m.QueryID, off, err = readRequestQueryID(body); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "UnsubscribeMulti"); err != nil {
		return m, err
	}
	return m, nil
}

func readRequestQueryID(body []byte) (requestID uint32, queryID uint32, off int, err error) {
	if requestID, off, err = readUint32(body, 0); err != nil {
		return 0, 0, off, err
	}
	if queryID, off, err = readUint32(body, off); err != nil {
		return 0, 0, off, err
	}
	return requestID, queryID, off, nil
}

// --- Framing primitives ---

func writeUint32(buf *bytes.Buffer, v uint32) {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], v)
	buf.Write(tmp[:])
}

func writeString(buf *bytes.Buffer, s string) {
	writeUint32(buf, uint32(len(s)))
	buf.WriteString(s)
}

func writeBytes(buf *bytes.Buffer, b []byte) {
	writeUint32(buf, uint32(len(b)))
	buf.Write(b)
}

func readUint32(body []byte, off int) (uint32, int, error) {
	if len(body)-off < 4 {
		return 0, off, fmt.Errorf("%w: uint32 truncated at offset %d", ErrMalformedMessage, off)
	}
	return binary.LittleEndian.Uint32(body[off : off+4]), off + 4, nil
}

func readString(body []byte, off int) (string, int, error) {
	n, off, err := readUint32(body, off)
	if err != nil {
		return "", off, err
	}
	if uint64(n) > uint64(len(body)-off) {
		return "", off, fmt.Errorf("%w: string length %d exceeds remaining %d", ErrMalformedMessage, n, len(body)-off)
	}
	raw := body[off : off+int(n)]
	if !utf8.Valid(raw) {
		return "", off, fmt.Errorf("%w: invalid UTF-8 string at offset %d", ErrMalformedMessage, off)
	}
	s := string(raw)
	return s, off + int(n), nil
}

func readBytes(body []byte, off int) ([]byte, int, error) {
	n, off, err := readUint32(body, off)
	if err != nil {
		return nil, off, err
	}
	if uint64(n) > uint64(len(body)-off) {
		return nil, off, fmt.Errorf("%w: bytes length %d exceeds remaining %d", ErrMalformedMessage, n, len(body)-off)
	}
	out := make([]byte, n)
	copy(out, body[off:off+int(n)])
	return out, off + int(n), nil
}

func requireCountFitsRemaining(name string, count uint32, body []byte, off int, minPerItem int) error {
	if minPerItem <= 0 {
		panic("minPerItem must be positive")
	}
	remaining := len(body) - off
	if uint64(count) > uint64(remaining/minPerItem) {
		return fmt.Errorf("%w: %s count %d exceeds remaining %d", ErrMalformedMessage, name, count, remaining)
	}
	return nil
}

func requireFullyConsumed(body []byte, off int, msgName string) error {
	if off != len(body) {
		return fmt.Errorf("%w: %s trailing bytes at offset %d", ErrMalformedMessage, msgName, off)
	}
	return nil
}
