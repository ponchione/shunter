package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ponchione/shunter/schema"
)

// Server→client message types (SPEC-005 §8).
//
// Outcome-model decision:
//   - `TransactionUpdate` is the heavy caller-bound envelope.
//   - `TransactionUpdateLight` is the delta-only envelope delivered to
//     non-callers whose subscribed rows were touched.
//   - `ReducerCallResult` is removed from the wire surface; `TagReducerCallResult`
//     stays reserved (unused) so the byte cannot silently be reallocated.
//   - `UpdateStatus` is a two-arm tagged union: committed or failed.

// IdentityToken is the first server→client frame on every connection.
// Field order matches reference `IdentityToken` at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:445
// (`identity, token, connection_id`) — pinned by
// parity_identity_token_test.go against the reference byte shape.
// Renamed from `InitialConnection`; the prior type name and field
// order (`identity, connection_id, token`) were Shunter-local.
type IdentityToken struct {
	Identity     [32]byte
	Token        string
	ConnectionID [16]byte
}

// SubscribeSingleApplied is the server response to a SubscribeSingle.
// Part of the Phase 2 Slice 2 variant split — SubscribeMultiApplied
// carries the merged delta for a multi-query set. Reference:
// SubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:317
// (`request_id, total_host_execution_duration_micros, query_id, rows`).
// Duration sits at position 2 to match the reference byte shape — pinned
// by parity_applied_envelopes_test.go. TableName + Rows flatten the
// reference `SubscribeRows` wrapper; that rows-shape divergence is
// accepted as documented per
// `docs/parity-decisions.md#protocol-rows-shape` (Phase 2 Slice 4).
type SubscribeSingleApplied struct {
	RequestID                        uint32
	TotalHostExecutionDurationMicros uint64
	QueryID                          uint32
	TableName                        string
	Rows                             []byte // encoded RowList
}

// UnsubscribeSingleApplied is the server response to an UnsubscribeSingle.
// Part of the Phase 2 Slice 2 variant split — UnsubscribeMultiApplied
// carries the merged delta for a multi-query set. Reference:
// UnsubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:331
// (`request_id, total_host_execution_duration_micros, query_id, rows`).
// Duration sits at position 2 to match the reference byte shape — pinned
// by parity_applied_envelopes_test.go. HasRows + Rows diverges from the
// reference required `SubscribeRows` wrapper; that rows-shape
// divergence (including the Shunter-local `HasRows` optionality) is
// accepted as documented per
// `docs/parity-decisions.md#protocol-rows-shape` (Phase 2 Slice 4).
type UnsubscribeSingleApplied struct {
	RequestID                        uint32
	TotalHostExecutionDurationMicros uint64
	QueryID                          uint32
	HasRows                          bool
	Rows                             []byte // encoded RowList; only present if HasRows
}

// SubscriptionError is the server-emitted failure envelope for any
// subscription-lifecycle error. RequestID / QueryID now model the
// reference optional-field shape explicitly: subscribe/unsubscribe
// request paths populate them, while other producers may leave them nil.
// TableID mirrors the reference optional `table_id`; when present it
// narrows the drop scope to one return-table family rather than the
// entire subscription set.
//
// TotalHostExecutionDurationMicros mirrors the reference field of the
// same name (v1.rs:350) and is the first wire field for byte-shape
// parity. Live emit sites now populate a measured non-zero microsecond
// duration from the admission / evaluation receipt seam; the wire shape
// and the value semantics are both closed.
type SubscriptionError struct {
	TotalHostExecutionDurationMicros uint64
	RequestID                        *uint32
	QueryID                          *uint32
	TableID                          *schema.TableID
	Error                            string
}

// SubscribeMultiApplied is the server response to a SubscribeMulti.
// Update is a merged initial snapshot, one SubscriptionUpdate per emitted
// query/table family carrying the client QueryID, with Inserts populated and
// Deletes empty. Reference: SubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:380
// (`request_id, total_host_execution_duration_micros, query_id, update`).
// Duration sits at position 2 to match the reference byte shape — pinned
// by parity_applied_envelopes_test.go. Update flattens the reference
// `DatabaseUpdate` wrapper to `[]SubscriptionUpdate`; that rows-shape
// divergence is accepted as documented per
// `docs/parity-decisions.md#protocol-rows-shape` (Phase 2 Slice 4).
type SubscribeMultiApplied struct {
	RequestID                        uint32
	TotalHostExecutionDurationMicros uint64
	QueryID                          uint32
	Update                           []SubscriptionUpdate
}

// UnsubscribeMultiApplied is the server response to an UnsubscribeMulti.
// Update carries Deletes-populated entries for rows that were still
// live at unsubscribe time. Reference: UnsubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:394
// (`request_id, total_host_execution_duration_micros, query_id, update`).
// Duration sits at position 2 to match the reference byte shape — pinned
// by parity_applied_envelopes_test.go. Update flattens the reference
// `DatabaseUpdate` wrapper to `[]SubscriptionUpdate`; that rows-shape
// divergence is accepted as documented per
// `docs/parity-decisions.md#protocol-rows-shape` (Phase 2 Slice 4).
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

// TransactionUpdateLight is the delta-only envelope delivered to
// non-caller subscribers whose rows were touched (Phase 1.5). Reference:
// `pub struct TransactionUpdateLight<F>` at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:493
// (`request_id: u32, update: DatabaseUpdate<F>`). Byte-shape pin in
// parity_rows_shape_test.go. Update flattens the reference
// `DatabaseUpdate` wrapper to `[]SubscriptionUpdate`; that rows-shape
// divergence is accepted as documented per
// `docs/parity-decisions.md#protocol-rows-shape` (Phase 2 Slice 4).
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

// StatusCommitted signals reducer success and carries the caller's
// visible row-delta slice (may be empty). Reference: `Committed(DatabaseUpdate<F>)`
// variant of `UpdateStatus<F>` at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:526.
// Update flattens the reference `DatabaseUpdate` wrapper to
// `[]SubscriptionUpdate`; that rows-shape divergence is accepted as
// documented per `docs/parity-decisions.md#protocol-rows-shape` (Phase 2
// Slice 4). Byte-shape pin lives in parity_transaction_update_test.go.
type StatusCommitted struct {
	Update []SubscriptionUpdate
}

// StatusFailed signals reducer-side failure or pre-commit rejection.
// Error is a human-readable message; Phase 1.5 collapses user-error,
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

// OneOffQueryResponse is the server reply to a OneOffQuery. Field order
// matches reference `OneOffQueryResponse<F>` at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:654
// (`message_id, error: Option<Box<str>>, tables: Box<[OneOffTable]>,
// total_host_execution_duration: TimeDuration`) — pinned by
// parity_one_off_query_response_test.go against the reference byte shape.
// Renamed from `OneOffQueryResult`; the prior Status-byte + single-Rows
// + Error layout was Shunter-local. `nil Error` signals success and
// `Tables` carries the matched rows; on failure `Error` is non-nil and
// `Tables` is empty — matching module_host.rs:2290-2308.
//
// TotalHostExecutionDuration is the wire field from reference
// `TimeDuration` (i64 microseconds). Live emit sites populate a measured
// non-zero microsecond duration from the one-off receipt seam, matching
// the reference unit semantics.
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
	var buf bytes.Buffer
	switch msg := m.(type) {
	case IdentityToken:
		buf.WriteByte(TagIdentityToken)
		buf.Write(msg.Identity[:])
		writeString(&buf, msg.Token)
		buf.Write(msg.ConnectionID[:])
	case SubscribeSingleApplied:
		buf.WriteByte(TagSubscribeSingleApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
		writeUint32(&buf, msg.QueryID)
		writeString(&buf, msg.TableName)
		writeBytes(&buf, msg.Rows)
	case UnsubscribeSingleApplied:
		buf.WriteByte(TagUnsubscribeSingleApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
		writeUint32(&buf, msg.QueryID)
		if msg.HasRows {
			buf.WriteByte(1)
			writeBytes(&buf, msg.Rows)
		} else {
			buf.WriteByte(0)
		}
	case SubscriptionError:
		buf.WriteByte(TagSubscriptionError)
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
		writeOptionalUint32(&buf, msg.RequestID)
		writeOptionalUint32(&buf, msg.QueryID)
		writeOptionalTableID(&buf, msg.TableID)
		writeString(&buf, msg.Error)
	case TransactionUpdate:
		buf.WriteByte(TagTransactionUpdate)
		if err := writeUpdateStatus(&buf, msg.Status); err != nil {
			return nil, err
		}
		writeInt64(&buf, msg.Timestamp)
		buf.Write(msg.CallerIdentity[:])
		buf.Write(msg.CallerConnectionID[:])
		writeReducerCallInfo(&buf, msg.ReducerCall)
		writeInt64(&buf, msg.TotalHostExecutionDuration)
	case TransactionUpdateLight:
		buf.WriteByte(TagTransactionUpdateLight)
		writeUint32(&buf, msg.RequestID)
		writeSubscriptionUpdates(&buf, msg.Update)
	case OneOffQueryResponse:
		buf.WriteByte(TagOneOffQueryResponse)
		writeBytes(&buf, msg.MessageID)
		writeOptionalString(&buf, msg.Error)
		writeOneOffTables(&buf, msg.Tables)
		writeInt64(&buf, msg.TotalHostExecutionDuration)
	case SubscribeMultiApplied:
		buf.WriteByte(TagSubscribeMultiApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
		writeUint32(&buf, msg.QueryID)
		writeSubscriptionUpdates(&buf, msg.Update)
	case UnsubscribeMultiApplied:
		buf.WriteByte(TagUnsubscribeMultiApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
		writeUint32(&buf, msg.QueryID)
		writeSubscriptionUpdates(&buf, msg.Update)
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnknownMessageTag, m)
	}
	return buf.Bytes(), nil
}

// DecodeServerMessage parses a server frame back into the concrete
// message type. Provided for symmetry and client-side / test use.
// The returned any is one of IdentityToken, SubscribeSingleApplied,
// UnsubscribeSingleApplied, SubscriptionError, TransactionUpdate,
// OneOffQueryResponse, TransactionUpdateLight, SubscribeMultiApplied,
// UnsubscribeMultiApplied — matching the tag byte.
// TagReducerCallResult is reserved and rejected here — see
// `docs/parity-decisions.md#outcome-model`.
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
	return m, nil
}

func decodeSubscribeSingleApplied(body []byte) (SubscribeSingleApplied, error) {
	var m SubscribeSingleApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.TotalHostExecutionDurationMicros, off, err = readUint64(body, off); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if m.TableName, off, err = readString(body, off); err != nil {
		return m, err
	}
	if m.Rows, _, err = readBytes(body, off); err != nil {
		return m, err
	}
	return m, nil
}

func decodeUnsubscribeSingleApplied(body []byte) (UnsubscribeSingleApplied, error) {
	var m UnsubscribeSingleApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.TotalHostExecutionDurationMicros, off, err = readUint64(body, off); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if len(body)-off < 1 {
		return m, fmt.Errorf("%w: UnsubscribeSingleApplied has_rows", ErrMalformedMessage)
	}
	m.HasRows = body[off] != 0
	off++
	if m.HasRows {
		if m.Rows, _, err = readBytes(body, off); err != nil {
			return m, err
		}
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
	if m.Error, _, err = readString(body, off); err != nil {
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
	if off != len(body) {
		return m, fmt.Errorf("%w: TransactionUpdate trailing bytes at offset %d", ErrMalformedMessage, off)
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
	ups, _, err := readSubscriptionUpdates(body, off)
	if err != nil {
		return m, err
	}
	m.Update = ups
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
	if m.TotalHostExecutionDuration, _, err = readInt64(body, off); err != nil {
		return m, err
	}
	return m, nil
}

func writeOneOffTables(buf *bytes.Buffer, tables []OneOffTable) {
	writeUint32(buf, uint32(len(tables)))
	for _, t := range tables {
		writeString(buf, t.TableName)
		writeBytes(buf, t.Rows)
	}
}

func readOneOffTables(body []byte, off int) ([]OneOffTable, int, error) {
	count, off, err := readUint32(body, off)
	if err != nil {
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

func writeOptionalString(buf *bytes.Buffer, v *string) {
	if v == nil {
		buf.WriteByte(0)
		return
	}
	buf.WriteByte(1)
	writeString(buf, *v)
}

func readOptionalString(body []byte, off int) (*string, int, error) {
	if len(body)-off < 1 {
		return nil, off, fmt.Errorf("%w: optional string tag truncated at offset %d", ErrMalformedMessage, off)
	}
	present := body[off]
	off++
	if present == 0 {
		return nil, off, nil
	}
	s, off, err := readString(body, off)
	if err != nil {
		return nil, off, err
	}
	return &s, off, nil
}

func decodeSubscribeMultiApplied(body []byte) (SubscribeMultiApplied, error) {
	var m SubscribeMultiApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.TotalHostExecutionDurationMicros, off, err = readUint64(body, off); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if m.Update, _, err = readSubscriptionUpdates(body, off); err != nil {
		return m, err
	}
	return m, nil
}

func decodeUnsubscribeMultiApplied(body []byte) (UnsubscribeMultiApplied, error) {
	var m UnsubscribeMultiApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.TotalHostExecutionDurationMicros, off, err = readUint64(body, off); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if m.Update, _, err = readSubscriptionUpdates(body, off); err != nil {
		return m, err
	}
	return m, nil
}

func writeUpdateStatus(buf *bytes.Buffer, s UpdateStatus) error {
	switch v := s.(type) {
	case StatusCommitted:
		buf.WriteByte(updateStatusTagCommitted)
		writeSubscriptionUpdates(buf, v.Update)
	case StatusFailed:
		buf.WriteByte(updateStatusTagFailed)
		writeString(buf, v.Error)
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

func writeReducerCallInfo(buf *bytes.Buffer, rci ReducerCallInfo) {
	writeString(buf, rci.ReducerName)
	writeUint32(buf, rci.ReducerID)
	writeBytes(buf, rci.Args)
	writeUint32(buf, rci.RequestID)
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

func writeSubscriptionUpdates(buf *bytes.Buffer, ups []SubscriptionUpdate) {
	writeUint32(buf, uint32(len(ups)))
	for _, u := range ups {
		writeUint32(buf, u.QueryID)
		writeString(buf, u.TableName)
		writeBytes(buf, u.Inserts)
		writeBytes(buf, u.Deletes)
	}
}

func readSubscriptionUpdates(body []byte, off int) ([]SubscriptionUpdate, int, error) {
	count, off, err := readUint32(body, off)
	if err != nil {
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
	if len(body)-off < 1 {
		return nil, off, fmt.Errorf("%w: optional uint32 tag truncated at offset %d", ErrMalformedMessage, off)
	}
	present := body[off]
	off++
	if present == 0 {
		return nil, off, nil
	}
	v, off, err := readUint32(body, off)
	if err != nil {
		return nil, off, err
	}
	return &v, off, nil
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
