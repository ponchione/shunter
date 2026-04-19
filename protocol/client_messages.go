package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/types"
)

// SubscribeSingleMsg is the client-side single-envelope Subscribe
// message (SPEC-005 §7.1). QueryID mirrors reference
// `SubscribeSingle.query_id: QueryId` — a client-allocated identifier
// used to correlate the subscribe with its later Unsubscribe.
//
// Part of the Phase 2 Slice 2 variant split. SubscribeMultiMsg
// carries a list of queries under one QueryID; this type carries
// exactly one. Reference: SubscribeSingle at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:189.
type SubscribeSingleMsg struct {
	RequestID uint32
	QueryID   uint32
	Query     Query
}

// UnsubscribeSingleMsg is the client-side single-envelope Unsubscribe
// message (SPEC-005 §7.2). QueryID mirrors reference
// `Unsubscribe.query_id: QueryId`.
//
// Part of the Phase 2 Slice 2 variant split. UnsubscribeMultiMsg
// drops every query under a given QueryID; this type drops exactly
// one. Reference: Unsubscribe at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:218.
type UnsubscribeSingleMsg struct {
	RequestID   uint32
	QueryID     uint32
	SendDropped bool
}

// CallReducerMsg is the client-side CallReducer message (SPEC-005 §7.3).
// Args is the raw BSATN-encoded ProductValue; protocol does not
// validate argument types — that's the executor's job (SPEC-003).
//
// Flags is a single-byte discriminant matching the reference
// `CallReducerFlags` behavior.
// Only two values are defined today; see the constants below.
type CallReducerMsg struct {
	RequestID   uint32
	ReducerName string
	Args        []byte
	Flags       byte
}

// CallReducer flags (reference `CallReducerFlags`). The wire byte is a
// single u8 trailing `Args`. Values outside the defined set are
// rejected as malformed.
const (
	// CallReducerFlagsFullUpdate is the default: the caller is notified of
	// a successful reducer completion via the heavy `TransactionUpdate`
	// envelope regardless of whether the caller subscribed to any
	// relevant query.
	CallReducerFlagsFullUpdate byte = 0
	// CallReducerFlagsNoSuccessNotify opts the caller out of the success
	// caller-echo. On `StatusCommitted` the fan-out worker skips the
	// caller's heavy delivery entirely. Failure envelopes
	// (`StatusFailed`, `StatusOutOfEnergy`) are still delivered so the
	// caller observes non-success outcomes.
	CallReducerFlagsNoSuccessNotify byte = 1
)

// OneOffQueryMsg is the client-side OneOffQuery message (SPEC-005 §7.4).
type OneOffQueryMsg struct {
	RequestID  uint32
	TableName  string
	Predicates []Predicate
}

// SubscribeMultiMsg is the client-side SubscribeMulti message
// (SPEC-005 §7.1b). Reference: SubscribeMulti at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:203.
// Queries is a structured predicate list — the SQL-string form is
// deferred alongside OneOffQuery (see
// TestPhase2DeferralSubscribeMultiQueriesStructured).
type SubscribeMultiMsg struct {
	RequestID uint32
	QueryID   uint32
	Queries   []Query
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
	var buf bytes.Buffer
	switch msg := m.(type) {
	case SubscribeSingleMsg:
		buf.WriteByte(TagSubscribeSingle)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		if err := encodeQuery(&buf, msg.Query); err != nil {
			return nil, err
		}
	case UnsubscribeSingleMsg:
		buf.WriteByte(TagUnsubscribeSingle)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		if msg.SendDropped {
			buf.WriteByte(1)
		} else {
			buf.WriteByte(0)
		}
	case CallReducerMsg:
		buf.WriteByte(TagCallReducer)
		writeUint32(&buf, msg.RequestID)
		writeString(&buf, msg.ReducerName)
		writeBytes(&buf, msg.Args)
		buf.WriteByte(msg.Flags)
	case OneOffQueryMsg:
		buf.WriteByte(TagOneOffQuery)
		writeUint32(&buf, msg.RequestID)
		writeString(&buf, msg.TableName)
		if err := encodePredicates(&buf, msg.Predicates); err != nil {
			return nil, err
		}
	case SubscribeMultiMsg:
		buf.WriteByte(TagSubscribeMulti)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		writeUint32(&buf, uint32(len(msg.Queries)))
		for _, q := range msg.Queries {
			if err := encodeQuery(&buf, q); err != nil {
				return nil, err
			}
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

// DecodeClientMessage parses a wire frame into its concrete message
// type. The returned any is one of SubscribeSingleMsg, UnsubscribeSingleMsg,
// CallReducerMsg, OneOffQueryMsg, SubscribeMultiMsg, UnsubscribeMultiMsg
// — matching the tag byte.
func DecodeClientMessage(frame []byte) (uint8, any, error) {
	if len(frame) < 1 {
		return 0, nil, fmt.Errorf("%w: empty frame", ErrMalformedMessage)
	}
	tag := frame[0]
	body := frame[1:]
	switch tag {
	case TagSubscribeSingle:
		msg, err := decodeSubscribeSingle(body)
		return tag, msg, err
	case TagUnsubscribeSingle:
		msg, err := decodeUnsubscribeSingle(body)
		return tag, msg, err
	case TagCallReducer:
		msg, err := decodeCallReducer(body)
		return tag, msg, err
	case TagOneOffQuery:
		msg, err := decodeOneOffQuery(body)
		return tag, msg, err
	case TagSubscribeMulti:
		msg, err := decodeSubscribeMulti(body)
		return tag, msg, err
	case TagUnsubscribeMulti:
		msg, err := decodeUnsubscribeMulti(body)
		return tag, msg, err
	default:
		return 0, nil, fmt.Errorf("%w: tag=%d", ErrUnknownMessageTag, tag)
	}
}

// --- Per-message decoders ---

func decodeSubscribeSingle(body []byte) (SubscribeSingleMsg, error) {
	var m SubscribeSingleMsg
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	q, _, err := decodeQuery(body, off)
	if err != nil {
		return m, err
	}
	m.Query = q
	return m, nil
}

func decodeUnsubscribeSingle(body []byte) (UnsubscribeSingleMsg, error) {
	var m UnsubscribeSingleMsg
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if len(body)-off < 1 {
		return m, fmt.Errorf("%w: Unsubscribe send_dropped", ErrMalformedMessage)
	}
	m.SendDropped = body[off] != 0
	return m, nil
}

func decodeCallReducer(body []byte) (CallReducerMsg, error) {
	var m CallReducerMsg
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.ReducerName, off, err = readString(body, off); err != nil {
		return m, err
	}
	if m.Args, off, err = readBytes(body, off); err != nil {
		return m, err
	}
	if len(body)-off < 1 {
		return m, fmt.Errorf("%w: CallReducer flags byte truncated", ErrMalformedMessage)
	}
	m.Flags = body[off]
	switch m.Flags {
	case CallReducerFlagsFullUpdate, CallReducerFlagsNoSuccessNotify:
	default:
		return m, fmt.Errorf("%w: invalid CallReducer flags byte %d", ErrMalformedMessage, m.Flags)
	}
	return m, nil
}

func decodeOneOffQuery(body []byte) (OneOffQueryMsg, error) {
	var m OneOffQueryMsg
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.TableName, off, err = readString(body, off); err != nil {
		return m, err
	}
	preds, _, err := decodePredicates(body, off)
	if err != nil {
		return m, err
	}
	m.Predicates = preds
	return m, nil
}

func decodeSubscribeMulti(body []byte) (SubscribeMultiMsg, error) {
	var m SubscribeMultiMsg
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	count, off, err := readUint32(body, off)
	if err != nil {
		return m, err
	}
	m.Queries = make([]Query, 0, count)
	for i := uint32(0); i < count; i++ {
		q, next, qerr := decodeQuery(body, off)
		if qerr != nil {
			return m, qerr
		}
		off = next
		m.Queries = append(m.Queries, q)
	}
	return m, nil
}

func decodeUnsubscribeMulti(body []byte) (UnsubscribeMultiMsg, error) {
	var m UnsubscribeMultiMsg
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, _, err = readUint32(body, off); err != nil {
		return m, err
	}
	return m, nil
}

// --- Query / Predicate codecs ---

func encodeQuery(buf *bytes.Buffer, q Query) error {
	writeString(buf, q.TableName)
	return encodePredicates(buf, q.Predicates)
}

func decodeQuery(body []byte, off int) (Query, int, error) {
	var q Query
	var err error
	if q.TableName, off, err = readString(body, off); err != nil {
		return q, off, err
	}
	preds, off, err := decodePredicates(body, off)
	if err != nil {
		return q, off, err
	}
	q.Predicates = preds
	return q, off, nil
}

func encodePredicates(buf *bytes.Buffer, preds []Predicate) error {
	writeUint32(buf, uint32(len(preds)))
	for _, p := range preds {
		writeString(buf, p.Column)
		if err := bsatn.EncodeValue(buf, p.Value); err != nil {
			return err
		}
	}
	return nil
}

func decodePredicates(body []byte, off int) ([]Predicate, int, error) {
	count, off, err := readUint32(body, off)
	if err != nil {
		return nil, off, err
	}
	preds := make([]Predicate, 0, count)
	for i := uint32(0); i < count; i++ {
		var p Predicate
		if p.Column, off, err = readString(body, off); err != nil {
			return nil, off, err
		}
		v, n, err := decodeValue(body, off)
		if err != nil {
			return nil, off, err
		}
		off += n
		p.Value = v
		preds = append(preds, p)
	}
	return preds, off, nil
}

// decodeValue parses a BSATN value from body[off:], returning the Value
// and the number of bytes consumed.
func decodeValue(body []byte, off int) (types.Value, int, error) {
	if len(body)-off < 1 {
		return types.Value{}, 0, fmt.Errorf("%w: Value tag truncated", ErrMalformedMessage)
	}
	r := bytes.NewReader(body[off:])
	v, err := bsatn.DecodeValue(r)
	if err != nil {
		return types.Value{}, 0, fmt.Errorf("%w: %v", ErrMalformedMessage, err)
	}
	consumed := len(body[off:]) - r.Len()
	return v, consumed, nil
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
	s := string(body[off : off+int(n)])
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
