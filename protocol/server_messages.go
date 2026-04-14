package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Server→client message types (SPEC-005 §8).

type InitialConnection struct {
	Identity     [32]byte
	ConnectionID [16]byte
	Token        string
}

type SubscribeApplied struct {
	RequestID      uint32
	SubscriptionID uint32
	TableName      string
	Rows           []byte // encoded RowList
}

type UnsubscribeApplied struct {
	RequestID      uint32
	SubscriptionID uint32
	HasRows        bool
	Rows           []byte // encoded RowList; only present if HasRows
}

type SubscriptionError struct {
	RequestID      uint32
	SubscriptionID uint32
	Error          string
}

type TransactionUpdate struct {
	TxID    uint64
	Updates []SubscriptionUpdate
}

type OneOffQueryResult struct {
	RequestID uint32
	Status    uint8  // 0 = success, 1 = error
	Rows      []byte // encoded RowList; present when Status == 0
	Error     string // present when Status == 1
}

type ReducerCallResult struct {
	RequestID         uint32
	Status            uint8 // 0=committed, 1=failed_user, 2=failed_panic, 3=not_found
	TxID              uint64
	Error             string
	Energy            uint64 // reserved, v1 encodes 0
	TransactionUpdate []SubscriptionUpdate
}

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
	case SubscribeApplied:
		buf.WriteByte(TagSubscribeApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.SubscriptionID)
		writeString(&buf, msg.TableName)
		writeBytes(&buf, msg.Rows)
	case UnsubscribeApplied:
		buf.WriteByte(TagUnsubscribeApplied)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.SubscriptionID)
		if msg.HasRows {
			buf.WriteByte(1)
			writeBytes(&buf, msg.Rows)
		} else {
			buf.WriteByte(0)
		}
	case SubscriptionError:
		buf.WriteByte(TagSubscriptionError)
		writeUint32(&buf, msg.RequestID)
		writeUint32(&buf, msg.SubscriptionID)
		writeString(&buf, msg.Error)
	case TransactionUpdate:
		buf.WriteByte(TagTransactionUpdate)
		writeUint64(&buf, msg.TxID)
		writeSubscriptionUpdates(&buf, msg.Updates)
	case OneOffQueryResult:
		buf.WriteByte(TagOneOffQueryResult)
		writeUint32(&buf, msg.RequestID)
		buf.WriteByte(msg.Status)
		if msg.Status == 0 {
			writeBytes(&buf, msg.Rows)
		} else {
			writeString(&buf, msg.Error)
		}
	case ReducerCallResult:
		buf.WriteByte(TagReducerCallResult)
		writeUint32(&buf, msg.RequestID)
		buf.WriteByte(msg.Status)
		writeUint64(&buf, msg.TxID)
		writeString(&buf, msg.Error)
		writeUint64(&buf, msg.Energy)
		writeSubscriptionUpdates(&buf, msg.TransactionUpdate)
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnknownMessageTag, m)
	}
	return buf.Bytes(), nil
}

// DecodeServerMessage parses a server frame back into the concrete
// message type. Provided for symmetry and client-side / test use.
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
	case TagSubscribeApplied:
		msg, err := decodeSubscribeApplied(body)
		return tag, msg, err
	case TagUnsubscribeApplied:
		msg, err := decodeUnsubscribeApplied(body)
		return tag, msg, err
	case TagSubscriptionError:
		msg, err := decodeSubscriptionError(body)
		return tag, msg, err
	case TagTransactionUpdate:
		msg, err := decodeTransactionUpdate(body)
		return tag, msg, err
	case TagOneOffQueryResult:
		msg, err := decodeOneOffQueryResult(body)
		return tag, msg, err
	case TagReducerCallResult:
		msg, err := decodeReducerCallResult(body)
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

func decodeSubscribeApplied(body []byte) (SubscribeApplied, error) {
	var m SubscribeApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.SubscriptionID, off, err = readUint32(body, off); err != nil {
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

func decodeUnsubscribeApplied(body []byte) (UnsubscribeApplied, error) {
	var m UnsubscribeApplied
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.SubscriptionID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if len(body)-off < 1 {
		return m, fmt.Errorf("%w: UnsubscribeApplied has_rows", ErrMalformedMessage)
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
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if m.SubscriptionID, off, err = readUint32(body, off); err != nil {
		return m, err
	}
	if m.Error, _, err = readString(body, off); err != nil {
		return m, err
	}
	return m, nil
}

func decodeTransactionUpdate(body []byte) (TransactionUpdate, error) {
	var m TransactionUpdate
	var off int
	var err error
	if m.TxID, off, err = readUint64(body, 0); err != nil {
		return m, err
	}
	ups, _, err := readSubscriptionUpdates(body, off)
	if err != nil {
		return m, err
	}
	m.Updates = ups
	return m, nil
}

func decodeOneOffQueryResult(body []byte) (OneOffQueryResult, error) {
	var m OneOffQueryResult
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
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

func decodeReducerCallResult(body []byte) (ReducerCallResult, error) {
	var m ReducerCallResult
	var off int
	var err error
	if m.RequestID, off, err = readUint32(body, 0); err != nil {
		return m, err
	}
	if len(body)-off < 1 {
		return m, fmt.Errorf("%w: ReducerCallResult status", ErrMalformedMessage)
	}
	m.Status = body[off]
	off++
	if m.TxID, off, err = readUint64(body, off); err != nil {
		return m, err
	}
	if m.Error, off, err = readString(body, off); err != nil {
		return m, err
	}
	if m.Energy, off, err = readUint64(body, off); err != nil {
		return m, err
	}
	ups, _, err := readSubscriptionUpdates(body, off)
	if err != nil {
		return m, err
	}
	m.TransactionUpdate = ups
	return m, nil
}

// --- SubscriptionUpdate array + uint64 primitives ---

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

func readUint64(body []byte, off int) (uint64, int, error) {
	if len(body)-off < 8 {
		return 0, off, fmt.Errorf("%w: uint64 truncated at offset %d", ErrMalformedMessage, off)
	}
	return binary.LittleEndian.Uint64(body[off : off+8]), off + 8, nil
}
