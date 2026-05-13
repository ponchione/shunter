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

// DeclaredQueryWithParametersMsg executes a module-owned QueryDeclaration by
// name with a BSATN product row of ordered declared-read parameters. It is a
// v2-only request message.
type DeclaredQueryWithParametersMsg struct {
	MessageID []byte
	Name      string
	Params    []byte
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

// SubscribeDeclaredViewWithParametersMsg subscribes to a module-owned
// ViewDeclaration by name with a BSATN product row of ordered declared-read
// parameters. It is a v2-only request message.
type SubscribeDeclaredViewWithParametersMsg struct {
	RequestID uint32
	QueryID   uint32
	Name      string
	Params    []byte
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
	size, err := encodedClientMessageSize(m)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.Grow(size)
	switch msg := m.(type) {
	case SubscribeSingleMsg:
		buf.WriteByte(TagSubscribeSingle)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		if err := writeString(&buf, msg.QueryString); err != nil {
			return nil, err
		}
	case UnsubscribeSingleMsg:
		buf.WriteByte(TagUnsubscribeSingle)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
	case CallReducerMsg:
		buf.WriteByte(TagCallReducer)
		if err := writeString(&buf, msg.ReducerName); err != nil {
			return nil, err
		}
		if err := writeBytes(&buf, msg.Args); err != nil {
			return nil, err
		}
		writeUint32(&buf, msg.RequestID)
		buf.WriteByte(msg.Flags)
	case OneOffQueryMsg:
		buf.WriteByte(TagOneOffQuery)
		if err := writeBytes(&buf, msg.MessageID); err != nil {
			return nil, err
		}
		if err := writeString(&buf, msg.QueryString); err != nil {
			return nil, err
		}
	case DeclaredQueryMsg:
		buf.WriteByte(TagDeclaredQuery)
		if err := writeBytes(&buf, msg.MessageID); err != nil {
			return nil, err
		}
		if err := writeString(&buf, msg.Name); err != nil {
			return nil, err
		}
	case DeclaredQueryWithParametersMsg:
		buf.WriteByte(TagDeclaredQueryWithParameters)
		if err := writeBytes(&buf, msg.MessageID); err != nil {
			return nil, err
		}
		if err := writeString(&buf, msg.Name); err != nil {
			return nil, err
		}
		if err := writeBytes(&buf, msg.Params); err != nil {
			return nil, err
		}
	case SubscribeMultiMsg:
		buf.WriteByte(TagSubscribeMulti)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		count, err := checkedProtocolLen("SubscribeMulti query string count", len(msg.QueryStrings))
		if err != nil {
			return nil, err
		}
		writeUint32(&buf, count)
		for _, qs := range msg.QueryStrings {
			if err := writeString(&buf, qs); err != nil {
				return nil, err
			}
		}
	case SubscribeDeclaredViewMsg:
		buf.WriteByte(TagSubscribeDeclaredView)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		if err := writeString(&buf, msg.Name); err != nil {
			return nil, err
		}
	case SubscribeDeclaredViewWithParametersMsg:
		buf.WriteByte(TagSubscribeDeclaredViewWithParameters)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		if err := writeString(&buf, msg.Name); err != nil {
			return nil, err
		}
		if err := writeBytes(&buf, msg.Params); err != nil {
			return nil, err
		}
	case UnsubscribeMultiMsg:
		buf.WriteByte(TagUnsubscribeMulti)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnknownMessageTag, m)
	}
	return buf.Bytes(), nil
}

// DecodeClientMessage parses a wire frame for the current protocol version
// into its concrete message type.
func DecodeClientMessage(frame []byte) (uint8, any, error) {
	return DecodeClientMessageForVersion(CurrentProtocolVersion, frame)
}

// DecodeClientMessageForVersion parses a wire frame for a negotiated protocol
// version into its concrete message type. V2 accepts all V1 request messages
// plus the V2-only parameterized declared-read request messages.
func DecodeClientMessageForVersion(version ProtocolVersion, frame []byte) (uint8, any, error) {
	if len(frame) < 1 {
		return 0, nil, fmt.Errorf("%w: empty frame", ErrMalformedMessage)
	}
	tag := frame[0]
	msg, err := decodeClientMessagePartsForVersion(version, tag, frame[1:])
	if err != nil {
		return 0, nil, err
	}
	return tag, msg, nil
}

func decodeClientMessageParts(tag uint8, body []byte) (any, error) {
	return decodeClientMessagePartsForVersion(CurrentProtocolVersion, tag, body)
}

func decodeClientMessagePartsForVersion(version ProtocolVersion, tag uint8, body []byte) (any, error) {
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
	case TagDeclaredQueryWithParameters:
		if version < ProtocolVersionV2 {
			return nil, fmt.Errorf("%w: tag=%d unsupported for %s", ErrUnknownMessageTag, tag, version)
		}
		return decodeDeclaredQueryWithParameters(body)
	case TagSubscribeMulti:
		return decodeSubscribeMulti(body)
	case TagSubscribeDeclaredView:
		return decodeSubscribeDeclaredView(body)
	case TagSubscribeDeclaredViewWithParameters:
		if version < ProtocolVersionV2 {
			return nil, fmt.Errorf("%w: tag=%d unsupported for %s", ErrUnknownMessageTag, tag, version)
		}
		return decodeSubscribeDeclaredViewWithParameters(body)
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

func decodeDeclaredQueryWithParameters(body []byte) (DeclaredQueryWithParametersMsg, error) {
	var m DeclaredQueryWithParametersMsg
	var off int
	var err error
	if m.MessageID, off, err = readBytes(body, 0); err != nil {
		return m, err
	}
	if m.Name, off, err = readString(body, off); err != nil {
		return m, err
	}
	if m.Params, off, err = readBytes(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "DeclaredQueryWithParameters"); err != nil {
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

func decodeSubscribeDeclaredViewWithParameters(body []byte) (SubscribeDeclaredViewWithParametersMsg, error) {
	var m SubscribeDeclaredViewWithParametersMsg
	var off int
	var err error
	if m.RequestID, m.QueryID, off, err = readRequestQueryID(body); err != nil {
		return m, err
	}
	if m.Name, off, err = readString(body, off); err != nil {
		return m, err
	}
	if m.Params, off, err = readBytes(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "SubscribeDeclaredViewWithParameters"); err != nil {
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

func writeString(buf *bytes.Buffer, s string) error {
	n, err := checkedProtocolLen("string", len(s))
	if err != nil {
		return err
	}
	writeUint32(buf, n)
	buf.WriteString(s)
	return nil
}

func writeBytes(buf *bytes.Buffer, b []byte) error {
	n, err := checkedProtocolLen("bytes", len(b))
	if err != nil {
		return err
	}
	writeUint32(buf, n)
	buf.Write(b)
	return nil
}

func checkedProtocolLen(label string, n int) (uint32, error) {
	if n < 0 || uint64(n) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("%w: %s length %d exceeds uint32", ErrMessageTooLarge, label, n)
	}
	return uint32(n), nil
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
