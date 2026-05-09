package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ponchione/shunter/schema"
)

// Server-to-client message types. TransactionUpdate is caller-bound;
// TransactionUpdateLight is the delta-only non-caller envelope.

// IdentityToken is the first server→client frame on every connection.
// Field order is identity, token, connection_id.
type IdentityToken struct {
	Identity     [32]byte
	Token        string
	ConnectionID [16]byte
}

// SubscribeSingleApplied is the server response to a SubscribeSingle.
// Rows is an encoded RowList for the subscribed table.
type SubscribeSingleApplied struct {
	RequestID                        uint32
	TotalHostExecutionDurationMicros uint64
	QueryID                          uint32
	TableName                        string
	Rows                             []byte // encoded RowList
}

// UnsubscribeSingleApplied is the server response to an UnsubscribeSingle.
// Rows is present only when HasRows is true.
type UnsubscribeSingleApplied struct {
	RequestID                        uint32
	TotalHostExecutionDurationMicros uint64
	QueryID                          uint32
	HasRows                          bool
	Rows                             []byte // encoded RowList; only present if HasRows
}

// SubscriptionError is the server-emitted failure envelope for subscription
// admission, evaluation, and removal paths.
type SubscriptionError struct {
	TotalHostExecutionDurationMicros uint64
	RequestID                        *uint32
	QueryID                          *uint32
	TableID                          *schema.TableID
	Error                            string
}

// SubscribeMultiApplied is the server response to a SubscribeMulti.
// Update is the merged initial snapshot for the query set.
type SubscribeMultiApplied struct {
	RequestID                        uint32
	TotalHostExecutionDurationMicros uint64
	QueryID                          uint32
	Update                           []SubscriptionUpdate
}

// UnsubscribeMultiApplied is the server response to an UnsubscribeMulti.
// Update carries rows that were live at unsubscribe time.
type UnsubscribeMultiApplied struct {
	RequestID                        uint32
	TotalHostExecutionDurationMicros uint64
	QueryID                          uint32
	Update                           []SubscriptionUpdate
}

// TransactionUpdate is the heavy caller-bound envelope. Non-callers
// receive `TransactionUpdateLight` instead. `Timestamp` and
// `TotalHostExecutionDuration` are populated from the executor seam in
// microseconds.
type TransactionUpdate struct {
	Status UpdateStatus
	// Timestamp is microseconds since Unix epoch.
	Timestamp          int64
	CallerIdentity     [32]byte
	CallerConnectionID [16]byte
	ReducerCall        ReducerCallInfo
	// TotalHostExecutionDuration is microseconds.
	TotalHostExecutionDuration int64
}

// TransactionUpdateLight is the delta-only envelope for non-caller subscribers.
type TransactionUpdateLight struct {
	RequestID uint32
	Update    []SubscriptionUpdate
}

// UpdateStatus is the two-arm tagged union carried by
// `TransactionUpdate.Status`. Implementations: `StatusCommitted`,
// `StatusFailed`.
type UpdateStatus interface {
	isUpdateStatus()
}

// StatusCommitted signals reducer success and carries the caller-visible delta.
type StatusCommitted struct {
	Update []SubscriptionUpdate
}

// StatusFailed signals reducer-side failure or pre-commit rejection.
// Error is a human-readable message; outcome-model collapses user-error,
// panic, and not-found into this single arm — see the decision doc.
type StatusFailed struct {
	Error string
}

func (StatusCommitted) isUpdateStatus() {}
func (StatusFailed) isUpdateStatus()    {}

// ReducerCallInfo mirrors the reference-side metadata embedded in every
// heavy `TransactionUpdate`.
type ReducerCallInfo struct {
	ReducerName string
	ReducerID   uint32
	Args        []byte
	RequestID   uint32
}

// OneOffQueryResponse is the server reply to a OneOffQuery.
// nil Error signals success; TotalHostExecutionDuration is in microseconds.
type OneOffQueryResponse struct {
	MessageID                  []byte
	Error                      *string
	Tables                     []OneOffTable
	TotalHostExecutionDuration int64
}

// OneOffTable mirrors reference `OneOffTable<F>` (v1.rs:669). Rows is an
// encoded F::List payload produced by EncodeRowList.
type OneOffTable struct {
	TableName string
	Rows      []byte
}

// UpdateStatus tag bytes on the wire.
const (
	updateStatusTagCommitted uint8 = 0
	updateStatusTagFailed    uint8 = 1
)

// EncodeServerMessage produces the uncompressed wire frame
// [tag byte] [BSATN body]. Compression envelope wrapping is Story 1.4.
func EncodeServerMessage(m any) ([]byte, error) {
	if err := validateServerMessageForEncode(m); err != nil {
		return nil, err
	}
	size, err := encodedServerMessageSize(m)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.Grow(size)
	switch msg := m.(type) {
	case IdentityToken:
		buf.WriteByte(TagIdentityToken)
		buf.Write(msg.Identity[:])
		if err := writeString(&buf, msg.Token); err != nil {
			return nil, err
		}
		buf.Write(msg.ConnectionID[:])
	case SubscribeSingleApplied:
		buf.WriteByte(TagSubscribeSingleApplied)
		writeAppliedHeader(&buf, msg.RequestID, msg.TotalHostExecutionDurationMicros, msg.QueryID)
		if err := writeString(&buf, msg.TableName); err != nil {
			return nil, err
		}
		if err := writeBytes(&buf, msg.Rows); err != nil {
			return nil, err
		}
	case UnsubscribeSingleApplied:
		buf.WriteByte(TagUnsubscribeSingleApplied)
		writeAppliedHeader(&buf, msg.RequestID, msg.TotalHostExecutionDurationMicros, msg.QueryID)
		if msg.HasRows {
			buf.WriteByte(1)
			if err := writeBytes(&buf, msg.Rows); err != nil {
				return nil, err
			}
		} else {
			buf.WriteByte(0)
		}
	case SubscriptionError:
		buf.WriteByte(TagSubscriptionError)
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
		writeOptionalUint32(&buf, msg.RequestID)
		writeOptionalUint32(&buf, msg.QueryID)
		writeOptionalTableID(&buf, msg.TableID)
		if err := writeString(&buf, msg.Error); err != nil {
			return nil, err
		}
	case TransactionUpdate:
		buf.WriteByte(TagTransactionUpdate)
		if err := writeUpdateStatus(&buf, msg.Status); err != nil {
			return nil, err
		}
		writeInt64(&buf, msg.Timestamp)
		buf.Write(msg.CallerIdentity[:])
		buf.Write(msg.CallerConnectionID[:])
		if err := writeReducerCallInfo(&buf, msg.ReducerCall); err != nil {
			return nil, err
		}
		writeInt64(&buf, msg.TotalHostExecutionDuration)
	case TransactionUpdateLight:
		buf.WriteByte(TagTransactionUpdateLight)
		writeUint32(&buf, msg.RequestID)
		if err := writeSubscriptionUpdates(&buf, msg.Update); err != nil {
			return nil, err
		}
	case OneOffQueryResponse:
		buf.WriteByte(TagOneOffQueryResponse)
		if err := writeBytes(&buf, msg.MessageID); err != nil {
			return nil, err
		}
		if err := writeOptionalString(&buf, msg.Error); err != nil {
			return nil, err
		}
		if err := writeOneOffTables(&buf, msg.Tables); err != nil {
			return nil, err
		}
		writeInt64(&buf, msg.TotalHostExecutionDuration)
	case SubscribeMultiApplied:
		buf.WriteByte(TagSubscribeMultiApplied)
		writeAppliedHeader(&buf, msg.RequestID, msg.TotalHostExecutionDurationMicros, msg.QueryID)
		if err := writeSubscriptionUpdates(&buf, msg.Update); err != nil {
			return nil, err
		}
	case UnsubscribeMultiApplied:
		buf.WriteByte(TagUnsubscribeMultiApplied)
		writeAppliedHeader(&buf, msg.RequestID, msg.TotalHostExecutionDurationMicros, msg.QueryID)
		if err := writeSubscriptionUpdates(&buf, msg.Update); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnknownMessageTag, m)
	}
	return buf.Bytes(), nil
}

// DecodeServerMessage parses a server frame into its concrete message type.
// Reserved tags are rejected.
func DecodeServerMessage(frame []byte) (uint8, any, error) {
	if len(frame) < 1 {
		return 0, nil, fmt.Errorf("%w: empty frame", ErrMalformedMessage)
	}
	tag := frame[0]
	body := frame[1:]
	switch tag {
	case TagIdentityToken:
		msg, err := decodeIdentityToken(body)
		return tag, msg, err
	case TagSubscribeSingleApplied:
		msg, err := decodeSubscribeSingleApplied(body)
		return tag, msg, err
	case TagUnsubscribeSingleApplied:
		msg, err := decodeUnsubscribeSingleApplied(body)
		return tag, msg, err
	case TagSubscriptionError:
		msg, err := decodeSubscriptionError(body)
		return tag, msg, err
	case TagTransactionUpdate:
		msg, err := decodeTransactionUpdate(body)
		return tag, msg, err
	case TagTransactionUpdateLight:
		msg, err := decodeTransactionUpdateLight(body)
		return tag, msg, err
	case TagOneOffQueryResponse:
		msg, err := decodeOneOffQueryResponse(body)
		return tag, msg, err
	case TagSubscribeMultiApplied:
		msg, err := decodeSubscribeMultiApplied(body)
		return tag, msg, err
	case TagUnsubscribeMultiApplied:
		msg, err := decodeUnsubscribeMultiApplied(body)
		return tag, msg, err
	default:
		return 0, nil, fmt.Errorf("%w: tag=%d", ErrUnknownMessageTag, tag)
	}
}

func decodeIdentityToken(body []byte) (IdentityToken, error) {
	var m IdentityToken
	if len(body) < 32 {
		return m, fmt.Errorf("%w: IdentityToken identity field", ErrMalformedMessage)
	}
	copy(m.Identity[:], body[0:32])
	token, off, err := readString(body, 32)
	if err != nil {
		return m, err
	}
	m.Token = token
	if len(body)-off < 16 {
		return m, fmt.Errorf("%w: IdentityToken connection_id field", ErrMalformedMessage)
	}
	copy(m.ConnectionID[:], body[off:off+16])
	off += 16
	if err := requireFullyConsumed(body, off, "IdentityToken"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeSubscribeSingleApplied(body []byte) (SubscribeSingleApplied, error) {
	var m SubscribeSingleApplied
	var off int
	var err error
	if m.RequestID, m.TotalHostExecutionDurationMicros, m.QueryID, off, err = readAppliedHeader(body); err != nil {
		return m, err
	}
	if m.TableName, off, err = readString(body, off); err != nil {
		return m, err
	}
	if m.Rows, off, err = readBytes(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "SubscribeSingleApplied"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeUnsubscribeSingleApplied(body []byte) (UnsubscribeSingleApplied, error) {
	var m UnsubscribeSingleApplied
	var off int
	var err error
	if m.RequestID, m.TotalHostExecutionDurationMicros, m.QueryID, off, err = readAppliedHeader(body); err != nil {
		return m, err
	}
	if len(body)-off < 1 {
		return m, fmt.Errorf("%w: UnsubscribeSingleApplied has_rows", ErrMalformedMessage)
	}
	switch body[off] {
	case 0:
		m.HasRows = false
	case 1:
		m.HasRows = true
	default:
		return m, fmt.Errorf("%w: UnsubscribeSingleApplied has_rows tag=%d", ErrMalformedMessage, body[off])
	}
	off++
	if m.HasRows {
		if m.Rows, off, err = readBytes(body, off); err != nil {
			return m, err
		}
	}
	if err := requireFullyConsumed(body, off, "UnsubscribeSingleApplied"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeSubscriptionError(body []byte) (SubscriptionError, error) {
	var m SubscriptionError
	var off int
	var err error
	if m.TotalHostExecutionDurationMicros, off, err = readUint64(body, 0); err != nil {
		return m, err
	}
	if m.RequestID, off, err = readOptionalUint32(body, off); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readOptionalUint32(body, off); err != nil {
		return m, err
	}
	if m.TableID, off, err = readOptionalTableID(body, off); err != nil {
		return m, err
	}
	if m.Error, off, err = readString(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "SubscriptionError"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeTransactionUpdate(body []byte) (TransactionUpdate, error) {
	var m TransactionUpdate
	status, off, err := readUpdateStatus(body, 0)
	if err != nil {
		return m, err
	}
	m.Status = status
	if m.Timestamp, off, err = readInt64(body, off); err != nil {
		return m, err
	}
	if len(body)-off < 32+16 {
		return m, fmt.Errorf("%w: TransactionUpdate caller fields truncated", ErrMalformedMessage)
	}
	copy(m.CallerIdentity[:], body[off:off+32])
	off += 32
	copy(m.CallerConnectionID[:], body[off:off+16])
	off += 16
	rci, off, err := readReducerCallInfo(body, off)
	if err != nil {
		return m, err
	}
	m.ReducerCall = rci
	if m.TotalHostExecutionDuration, off, err = readInt64(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "TransactionUpdate"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeTransactionUpdateLight(body []byte) (TransactionUpdateLight, error) {
	var m TransactionUpdateLight
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	ups, off, err := readSubscriptionUpdates(body, off)
	if err != nil {
		return m, err
	}
	m.Update = ups
	if err := requireFullyConsumed(body, off, "TransactionUpdateLight"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeOneOffQueryResponse(body []byte) (OneOffQueryResponse, error) {
	var m OneOffQueryResponse
	var off int
	var err error
	if m.MessageID, off, err = readBytes(body, 0); err != nil {
		return m, err
	}
	if m.Error, off, err = readOptionalString(body, off); err != nil {
		return m, err
	}
	if m.Tables, off, err = readOneOffTables(body, off); err != nil {
		return m, err
	}
	if m.TotalHostExecutionDuration, off, err = readInt64(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "OneOffQueryResponse"); err != nil {
		return m, err
	}
	return m, nil
}

func writeOneOffTables(buf *bytes.Buffer, tables []OneOffTable) error {
	count, err := checkedProtocolLen("OneOffTable count", len(tables))
	if err != nil {
		return err
	}
	writeUint32(buf, count)
	for _, t := range tables {
		if err := writeString(buf, t.TableName); err != nil {
			return err
		}
		if err := writeBytes(buf, t.Rows); err != nil {
			return err
		}
	}
	return nil
}

func readOneOffTables(body []byte, off int) ([]OneOffTable, int, error) {
	count, off, err := readUint32(body, off)
	if err != nil {
		return nil, off, err
	}
	if err := requireCountFitsRemaining("OneOffTable list", count, body, off, 8); err != nil {
		return nil, off, err
	}
	tables := make([]OneOffTable, 0, count)
	for i := uint32(0); i < count; i++ {
		var t OneOffTable
		if t.TableName, off, err = readString(body, off); err != nil {
			return nil, off, err
		}
		if t.Rows, off, err = readBytes(body, off); err != nil {
			return nil, off, err
		}
		tables = append(tables, t)
	}
	return tables, off, nil
}

func writeOptionalString(buf *bytes.Buffer, v *string) error {
	if v == nil {
		buf.WriteByte(0)
		return nil
	}
	buf.WriteByte(1)
	return writeString(buf, *v)
}

func readOptionalString(body []byte, off int) (*string, int, error) {
	present, off, err := readOptionalPresenceTag(body, off, "optional string")
	if err != nil {
		return nil, off, err
	}
	if !present {
		return nil, off, nil
	}
	s, off, err := readString(body, off)
	if err != nil {
		return nil, off, err
	}
	return &s, off, nil
}

func writeAppliedHeader(buf *bytes.Buffer, requestID uint32, durationMicros uint64, queryID uint32) {
	writeUint32(buf, requestID)
	writeUint64(buf, durationMicros)
	writeUint32(buf, queryID)
}

func readAppliedHeader(body []byte) (requestID uint32, durationMicros uint64, queryID uint32, off int, err error) {
	if requestID, off, err = readUint32(body, 0); err != nil {
		return 0, 0, 0, off, err
	}
	if durationMicros, off, err = readUint64(body, off); err != nil {
		return 0, 0, 0, off, err
	}
	if queryID, off, err = readUint32(body, off); err != nil {
		return 0, 0, 0, off, err
	}
	return requestID, durationMicros, queryID, off, nil
}

func decodeSubscribeMultiApplied(body []byte) (SubscribeMultiApplied, error) {
	var m SubscribeMultiApplied
	var off int
	var err error
	if m.RequestID, m.TotalHostExecutionDurationMicros, m.QueryID, off, err = readAppliedHeader(body); err != nil {
		return m, err
	}
	if m.Update, off, err = readSubscriptionUpdates(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "SubscribeMultiApplied"); err != nil {
		return m, err
	}
	return m, nil
}

func decodeUnsubscribeMultiApplied(body []byte) (UnsubscribeMultiApplied, error) {
	var m UnsubscribeMultiApplied
	var off int
	var err error
	if m.RequestID, m.TotalHostExecutionDurationMicros, m.QueryID, off, err = readAppliedHeader(body); err != nil {
		return m, err
	}
	if m.Update, off, err = readSubscriptionUpdates(body, off); err != nil {
		return m, err
	}
	if err := requireFullyConsumed(body, off, "UnsubscribeMultiApplied"); err != nil {
		return m, err
	}
	return m, nil
}

func writeUpdateStatus(buf *bytes.Buffer, s UpdateStatus) error {
	switch v := s.(type) {
	case StatusCommitted:
		buf.WriteByte(updateStatusTagCommitted)
		if err := writeSubscriptionUpdates(buf, v.Update); err != nil {
			return err
		}
	case StatusFailed:
		buf.WriteByte(updateStatusTagFailed)
		if err := writeString(buf, v.Error); err != nil {
			return err
		}
	case nil:
		return fmt.Errorf("%w: nil UpdateStatus", ErrMalformedMessage)
	default:
		return fmt.Errorf("%w: unknown UpdateStatus %T", ErrMalformedMessage, s)
	}
	return nil
}

func readUpdateStatus(body []byte, off int) (UpdateStatus, int, error) {
	if len(body)-off < 1 {
		return nil, off, fmt.Errorf("%w: UpdateStatus tag truncated", ErrMalformedMessage)
	}
	tag := body[off]
	off++
	switch tag {
	case updateStatusTagCommitted:
		ups, off, err := readSubscriptionUpdates(body, off)
		if err != nil {
			return nil, off, err
		}
		return StatusCommitted{Update: ups}, off, nil
	case updateStatusTagFailed:
		s, off, err := readString(body, off)
		if err != nil {
			return nil, off, err
		}
		return StatusFailed{Error: s}, off, nil
	default:
		return nil, off, fmt.Errorf("%w: UpdateStatus tag=%d", ErrMalformedMessage, tag)
	}
}

func writeReducerCallInfo(buf *bytes.Buffer, rci ReducerCallInfo) error {
	if err := writeString(buf, rci.ReducerName); err != nil {
		return err
	}
	writeUint32(buf, rci.ReducerID)
	if err := writeBytes(buf, rci.Args); err != nil {
		return err
	}
	writeUint32(buf, rci.RequestID)
	return nil
}

func readReducerCallInfo(body []byte, off int) (ReducerCallInfo, int, error) {
	var m ReducerCallInfo
	var err error
	if m.ReducerName, off, err = readString(body, off); err != nil {
		return m, off, err
	}
	if m.ReducerID, off, err = readUint32(body, off); err != nil {
		return m, off, err
	}
	if m.Args, off, err = readBytes(body, off); err != nil {
		return m, off, err
	}
	if m.RequestID, off, err = readUint32(body, off); err != nil {
		return m, off, err
	}
	return m, off, nil
}

// --- SubscriptionUpdate array + integer primitives ---

func writeSubscriptionUpdates(buf *bytes.Buffer, ups []SubscriptionUpdate) error {
	count, err := checkedProtocolLen("SubscriptionUpdate count", len(ups))
	if err != nil {
		return err
	}
	writeUint32(buf, count)
	for _, u := range ups {
		writeUint32(buf, u.QueryID)
		if err := writeString(buf, u.TableName); err != nil {
			return err
		}
		if err := writeBytes(buf, u.Inserts); err != nil {
			return err
		}
		if err := writeBytes(buf, u.Deletes); err != nil {
			return err
		}
	}
	return nil
}

func readSubscriptionUpdates(body []byte, off int) ([]SubscriptionUpdate, int, error) {
	count, off, err := readUint32(body, off)
	if err != nil {
		return nil, off, err
	}
	if err := requireCountFitsRemaining("SubscriptionUpdate list", count, body, off, 16); err != nil {
		return nil, off, err
	}
	ups := make([]SubscriptionUpdate, 0, count)
	for i := uint32(0); i < count; i++ {
		var u SubscriptionUpdate
		if u.QueryID, off, err = readUint32(body, off); err != nil {
			return nil, off, err
		}
		if u.TableName, off, err = readString(body, off); err != nil {
			return nil, off, err
		}
		if u.Inserts, off, err = readBytes(body, off); err != nil {
			return nil, off, err
		}
		if u.Deletes, off, err = readBytes(body, off); err != nil {
			return nil, off, err
		}
		ups = append(ups, u)
	}
	return ups, off, nil
}

func writeUint64(buf *bytes.Buffer, v uint64) {
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], v)
	buf.Write(tmp[:])
}

func writeOptionalUint32(buf *bytes.Buffer, v *uint32) {
	if v == nil {
		buf.WriteByte(0)
		return
	}
	buf.WriteByte(1)
	writeUint32(buf, *v)
}

func writeOptionalTableID(buf *bytes.Buffer, v *schema.TableID) {
	if v == nil {
		buf.WriteByte(0)
		return
	}
	buf.WriteByte(1)
	writeUint32(buf, uint32(*v))
}

func readUint64(body []byte, off int) (uint64, int, error) {
	if len(body)-off < 8 {
		return 0, off, fmt.Errorf("%w: uint64 truncated at offset %d", ErrMalformedMessage, off)
	}
	return binary.LittleEndian.Uint64(body[off : off+8]), off + 8, nil
}

func readOptionalUint32(body []byte, off int) (*uint32, int, error) {
	present, off, err := readOptionalPresenceTag(body, off, "optional uint32")
	if err != nil {
		return nil, off, err
	}
	if !present {
		return nil, off, nil
	}
	v, off, err := readUint32(body, off)
	if err != nil {
		return nil, off, err
	}
	return &v, off, nil
}

func readOptionalPresenceTag(body []byte, off int, label string) (bool, int, error) {
	if len(body)-off < 1 {
		return false, off, fmt.Errorf("%w: %s tag truncated at offset %d", ErrMalformedMessage, label, off)
	}
	tag := body[off]
	off++
	switch tag {
	case 0:
		return false, off, nil
	case 1:
		return true, off, nil
	default:
		return false, off, fmt.Errorf("%w: %s tag=%d", ErrMalformedMessage, label, tag)
	}
}

func readOptionalTableID(body []byte, off int) (*schema.TableID, int, error) {
	v, off, err := readOptionalUint32(body, off)
	if err != nil || v == nil {
		return nil, off, err
	}
	tableID := schema.TableID(*v)
	return &tableID, off, nil
}

func writeInt64(buf *bytes.Buffer, v int64) {
	writeUint64(buf, uint64(v))
}

func readInt64(body []byte, off int) (int64, int, error) {
	v, off, err := readUint64(body, off)
	return int64(v), off, err
}
