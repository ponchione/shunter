package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ponchione/shunter/schema"
)

// Server→client message types (SPEC-005 §8).
//
// Phase 1.5 outcome-model decision is pinned in
// `docs/parity-phase1.5-outcome-model.md` and
// `protocol/parity_message_family_test.go`:
//   - `TransactionUpdate` is the heavy caller-bound envelope matching
//     `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`.
//   - `TransactionUpdateLight` is the delta-only envelope delivered to
//     non-callers whose subscribed rows were touched.
//   - `ReducerCallResult` is removed from the wire surface; `TagReducerCallResult`
//     stays reserved (unused) so the byte cannot silently be reallocated.
//   - `UpdateStatus` is a three-arm tagged union. `OutOfEnergy` is present for
//     shape parity but is never emitted by the Phase 1.5 executor.

type InitialConnection struct {
	Identity     [32]byte
	ConnectionID [16]byte
	Token        string
}

// SubscribeSingleApplied is the server response to a SubscribeSingle.
// Part of the Phase 2 Slice 2 variant split — SubscribeMultiApplied
// carries the merged delta for a multi-query set. Reference:
// SubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:317.
type SubscribeSingleApplied struct {
	RequestID                        uint32
	QueryID                          uint32
	TableName                        string
	Rows                             []byte // encoded RowList
	TotalHostExecutionDurationMicros uint64
}

// UnsubscribeSingleApplied is the server response to an UnsubscribeSingle.
// Part of the Phase 2 Slice 2 variant split — UnsubscribeMultiApplied
// carries the merged delta for a multi-query set. Reference:
// UnsubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:331.
type UnsubscribeSingleApplied struct {
	RequestID                        uint32
	QueryID                          uint32
	HasRows                          bool
	Rows                             []byte // encoded RowList; only present if HasRows
	TotalHostExecutionDurationMicros uint64
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
// parity. Measurement is deferred — emit sites populate 0 until a
// receipt-timestamp seam is plumbed through the admission and
// evaluation paths. Zero is a legal reference-side value so the wire
// shape is closed even though the value is not yet meaningful.
type SubscriptionError struct {
	TotalHostExecutionDurationMicros uint64
	RequestID                        *uint32
	QueryID                          *uint32
	TableID                          *schema.TableID
	Error                            string
}

// SubscribeMultiApplied is the server response to a SubscribeMulti.
// Update is a merged initial snapshot, one SubscriptionUpdate per
// (allocated internal SubscriptionID, table) pair, with Inserts
// populated and Deletes empty. Reference: SubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:380.
type SubscribeMultiApplied struct {
	RequestID                        uint32
	QueryID                          uint32
	Update                           []SubscriptionUpdate
	TotalHostExecutionDurationMicros uint64
}

// UnsubscribeMultiApplied is the server response to an UnsubscribeMulti.
// Update carries Deletes-populated entries for rows that were still
// live at unsubscribe time. Reference: UnsubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:394.
type UnsubscribeMultiApplied struct {
	RequestID                        uint32
	QueryID                          uint32
	Update                           []SubscriptionUpdate
	TotalHostExecutionDurationMicros uint64
}

// TransactionUpdate is the heavy caller-bound envelope (Phase 1.5).
// Non-callers receive `TransactionUpdateLight` instead. `Timestamp` and
// `TotalHostExecutionDuration` are populated from the executor seam;
// `EnergyQuantaUsed` remains zero because Shunter has no energy model —
// see the decision doc.
type TransactionUpdate struct {
	Status                     UpdateStatus
	CallerIdentity             [32]byte
	CallerConnectionID         [16]byte
	ReducerCall                ReducerCallInfo
	Timestamp                  int64 // nanoseconds since Unix epoch
	EnergyQuantaUsed           uint64
	TotalHostExecutionDuration int64 // nanoseconds
}

// TransactionUpdateLight is the delta-only envelope delivered to
// non-caller subscribers whose rows were touched (Phase 1.5).
type TransactionUpdateLight struct {
	RequestID uint32
	Update    []SubscriptionUpdate
}

// UpdateStatus is the three-arm tagged union carried by
// `TransactionUpdate.Status`. Implementations: `StatusCommitted`,
// `StatusFailed`, `StatusOutOfEnergy`.
type UpdateStatus interface {
	isUpdateStatus()
}

// StatusCommitted signals reducer success and carries the caller's
// visible row-delta slice (may be empty).
type StatusCommitted struct {
	Update []SubscriptionUpdate
}

// StatusFailed signals reducer-side failure or pre-commit rejection.
// Error is a human-readable message; Phase 1.5 collapses user-error,
// panic, and not-found into this single arm — see the decision doc.
type StatusFailed struct {
	Error string
}

// StatusOutOfEnergy is present for shape parity but is never emitted
// by the Phase 1.5 executor. Flipping this deferral is a Phase 3
// runtime-parity concern.
type StatusOutOfEnergy struct{}

func (StatusCommitted) isUpdateStatus()   {}
func (StatusFailed) isUpdateStatus()      {}
func (StatusOutOfEnergy) isUpdateStatus() {}

// ReducerCallInfo mirrors the reference-side metadata embedded in every
// heavy `TransactionUpdate`.
type ReducerCallInfo struct {
	ReducerName string
	ReducerID   uint32
	Args        []byte
	RequestID   uint32
}

// Phase 2 Slice 1c closes the remaining request/response correlation
// divergence by carrying the reference-style opaque `message_id` bytes.
type OneOffQueryResult struct {
	MessageID []byte
	Status    uint8  // 0 = success, 1 = error
	Rows      []byte // encoded RowList; present when Status == 0
	Error     string // present when Status == 1
}

// UpdateStatus tag bytes on the wire.
const (
	updateStatusTagCommitted   uint8 = 0
	updateStatusTagFailed      uint8 = 1
	updateStatusTagOutOfEnergy uint8 = 2
)

// EncodeServerMessage produces the uncompressed wire frame
// [tag byte] [BSATN body]. Compression envelope wrapping is Story 1.4.
func EncodeServerMessage(m any) ([]byte, error) {
	var buf bytes.Buffer
	switch msg := m.(type) {
	case InitialConnection:
		buf.WriteByte(TagInitialConnection)
		buf.Write(msg.Identity[:])
		buf.Write(msg.ConnectionID[:])
		writeString(&buf, msg.Token)
	case SubscribeSingleApplied:
		buf.WriteByte(TagSubscribeSingleApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		writeString(&buf, msg.TableName)
		writeBytes(&buf, msg.Rows)
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
	case UnsubscribeSingleApplied:
		buf.WriteByte(TagUnsubscribeSingleApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		if msg.HasRows {
			buf.WriteByte(1)
			writeBytes(&buf, msg.Rows)
		} else {
			buf.WriteByte(0)
		}
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
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
		buf.Write(msg.CallerIdentity[:])
		buf.Write(msg.CallerConnectionID[:])
		writeReducerCallInfo(&buf, msg.ReducerCall)
		writeInt64(&buf, msg.Timestamp)
		writeUint64(&buf, msg.EnergyQuantaUsed)
		writeInt64(&buf, msg.TotalHostExecutionDuration)
	case TransactionUpdateLight:
		buf.WriteByte(TagTransactionUpdateLight)
		writeUint32(&buf, msg.RequestID)
		writeSubscriptionUpdates(&buf, msg.Update)
	case OneOffQueryResult:
		buf.WriteByte(TagOneOffQueryResult)
		writeBytes(&buf, msg.MessageID)
		buf.WriteByte(msg.Status)
		if msg.Status == 0 {
			writeBytes(&buf, msg.Rows)
		} else {
			writeString(&buf, msg.Error)
		}
	case SubscribeMultiApplied:
		buf.WriteByte(TagSubscribeMultiApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		writeSubscriptionUpdates(&buf, msg.Update)
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
	case UnsubscribeMultiApplied:
		buf.WriteByte(TagUnsubscribeMultiApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.QueryID)
		writeSubscriptionUpdates(&buf, msg.Update)
		writeUint64(&buf, msg.TotalHostExecutionDurationMicros)
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnknownMessageTag, m)
	}
	return buf.Bytes(), nil
}

// DecodeServerMessage parses a server frame back into the concrete
// message type. Provided for symmetry and client-side / test use.
// The returned any is one of InitialConnection, SubscribeSingleApplied,
// UnsubscribeSingleApplied, SubscriptionError, TransactionUpdate,
// OneOffQueryResult, TransactionUpdateLight, SubscribeMultiApplied,
// UnsubscribeMultiApplied — matching the tag byte.
// TagReducerCallResult is reserved and rejected here — see
// `docs/parity-phase1.5-outcome-model.md`.
func DecodeServerMessage(frame []byte) (uint8, any, error) {
	if len(frame) < 1 {
		return 0, nil, fmt.Errorf("%w: empty frame", ErrMalformedMessage)
	}
	tag := frame[0]
	body := frame[1:]
	switch tag {
	case TagInitialConnection:
		msg, err := decodeInitialConnection(body)
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
	case TagOneOffQueryResult:
		msg, err := decodeOneOffQueryResult(body)
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

func decodeInitialConnection(body []byte) (InitialConnection, error) {
	var m InitialConnection
	if len(body) < 32+16 {
		return m, fmt.Errorf("%w: InitialConnection fixed fields", ErrMalformedMessage)
	}
	copy(m.Identity[:], body[0:32])
	copy(m.ConnectionID[:], body[32:48])
	s, _, err := readString(body, 48)
	if err != nil {
		return m, err
	}
	m.Token = s
	return m, nil
}

func decodeSubscribeSingleApplied(body []byte) (SubscribeSingleApplied, error) {
	var m SubscribeSingleApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if m.TableName, off, err = readString(body, off); err != nil {
		return m, err
	}
	if m.Rows, off, err = readBytes(body, off); err != nil {
		return m, err
	}
	if m.TotalHostExecutionDurationMicros, _, err = readUint64(body, off); err != nil {
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
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if len(body)-off < 1 {
		return m, fmt.Errorf("%w: UnsubscribeSingleApplied has_rows", ErrMalformedMessage)
	}
	m.HasRows = body[off] != 0
	off++
	if m.HasRows {
		if m.Rows, off, err = readBytes(body, off); err != nil {
			return m, err
		}
	}
	if m.TotalHostExecutionDurationMicros, _, err = readUint64(body, off); err != nil {
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
	if m.Timestamp, off, err = readInt64(body, off); err != nil {
		return m, err
	}
	if m.EnergyQuantaUsed, off, err = readUint64(body, off); err != nil {
		return m, err
	}
	if m.TotalHostExecutionDuration, _, err = readInt64(body, off); err != nil {
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
	ups, _, err := readSubscriptionUpdates(body, off)
	if err != nil {
		return m, err
	}
	m.Update = ups
	return m, nil
}

func decodeOneOffQueryResult(body []byte) (OneOffQueryResult, error) {
	var m OneOffQueryResult
	var off int
	var err error
	if m.MessageID, off, err = readBytes(body, 0); err != nil {
		return m, err
	}
	if len(body)-off < 1 {
		return m, fmt.Errorf("%w: OneOffQueryResult status", ErrMalformedMessage)
	}
	m.Status = body[off]
	off++
	if m.Status == 0 {
		if m.Rows, _, err = readBytes(body, off); err != nil {
			return m, err
		}
	} else {
		if m.Error, _, err = readString(body, off); err != nil {
			return m, err
		}
	}
	return m, nil
}

func decodeSubscribeMultiApplied(body []byte) (SubscribeMultiApplied, error) {
	var m SubscribeMultiApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if m.Update, off, err = readSubscriptionUpdates(body, off); err != nil {
		return m, err
	}
	if m.TotalHostExecutionDurationMicros, _, err = readUint64(body, off); err != nil {
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
	if m.QueryID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if m.Update, off, err = readSubscriptionUpdates(body, off); err != nil {
		return m, err
	}
	if m.TotalHostExecutionDurationMicros, _, err = readUint64(body, off); err != nil {
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
	case StatusOutOfEnergy:
		buf.WriteByte(updateStatusTagOutOfEnergy)
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
	case updateStatusTagOutOfEnergy:
		return StatusOutOfEnergy{}, off, nil
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
		writeUint32(buf, u.SubscriptionID)
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
		if u.SubscriptionID, off, err = readUint32(body, off); err != nil {
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
