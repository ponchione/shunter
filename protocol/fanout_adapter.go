package protocol

import (
	"errors"
	"fmt"
	"strconv"
	"unsafe"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// FanOutSenderAdapter converts subscription fan-out payloads to protocol wire
// messages before delivery.
type FanOutSenderAdapter struct {
	sender ClientSender
}

func NewFanOutSenderAdapter(sender ClientSender) *FanOutSenderAdapter {
	return &FanOutSenderAdapter{sender: sender}
}

// SendTransactionUpdateHeavy delivers the caller's heavy
// `TransactionUpdate`. For `StatusCommitted` outcomes the caller's
// visible row delta is encoded into `StatusCommitted.Update`. For
// `StatusFailed` outcomes the update slice is ignored.
func (a *FanOutSenderAdapter) SendTransactionUpdateHeavy(
	connID types.ConnectionID,
	outcome subscription.CallerOutcome,
	callerUpdates []subscription.SubscriptionUpdate,
	memo *subscription.EncodingMemo,
) error {
	msg, err := BuildTransactionUpdateHeavy(connID, outcome, callerUpdates, memo)
	if err != nil {
		return fmt.Errorf("%w: encode caller outcome: %v", subscription.ErrSendEncodeFailed, err)
	}
	return mapDeliveryError(a.sender.SendTransactionUpdate(connID, &msg))
}

// BuildTransactionUpdateHeavy is the canonical heavy-envelope assembler for
// committed caller responses. Both the protocol inbox adapter and the fan-out
// adapter route committed reducer results through this helper so the wire shape
// is derived from one path.
func BuildTransactionUpdateHeavy(
	connID types.ConnectionID,
	outcome subscription.CallerOutcome,
	callerUpdates []subscription.SubscriptionUpdate,
	memo *subscription.EncodingMemo,
) (TransactionUpdate, error) {
	status, err := buildUpdateStatus(outcome, callerUpdates, memo)
	if err != nil {
		return TransactionUpdate{}, err
	}
	return TransactionUpdate{
		Status:                     status,
		CallerIdentity:             outcome.CallerIdentity,
		CallerConnectionID:         connID,
		ReducerCall:                reducerCallInfoFrom(outcome),
		Timestamp:                  outcome.Timestamp,
		TotalHostExecutionDuration: outcome.TotalHostExecutionDuration,
	}, nil
}

// SendTransactionUpdateLight delivers the delta-only envelope to
// non-callers whose rows were touched.
func (a *FanOutSenderAdapter) SendTransactionUpdateLight(
	connID types.ConnectionID,
	requestID uint32,
	updates []subscription.SubscriptionUpdate,
	memo *subscription.EncodingMemo,
) error {
	encoded, err := encodeSubscriptionUpdatesMemoized(updates, memo)
	if err != nil {
		return fmt.Errorf("%w: encode updates: %v", subscription.ErrSendEncodeFailed, err)
	}
	msg := &TransactionUpdateLight{RequestID: requestID, Update: encoded}
	return mapDeliveryError(a.sender.SendTransactionUpdateLight(connID, msg))
}

// SendSubscriptionError delivers a post-commit evaluation error.
// Diagnostic fields stay internal and are not projected onto the wire.
func (a *FanOutSenderAdapter) SendSubscriptionError(connID types.ConnectionID, subErr subscription.SubscriptionError) error {
	return mapDeliveryError(a.sender.Send(connID, SubscriptionError{
		TotalHostExecutionDurationMicros: subErr.TotalHostExecutionDurationMicros,
		Error:                            subErr.Message,
	}))
}

// mapDeliveryError translates protocol-layer errors to subscription-layer
// sentinels so the fan-out worker can react without importing protocol.
func mapDeliveryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrClientBufferFull) {
		return fmt.Errorf("%w: %v", subscription.ErrSendBufferFull, err)
	}
	if errors.Is(err, ErrConnNotFound) {
		return fmt.Errorf("%w: %v", subscription.ErrSendConnGone, err)
	}
	return err
}

var encodeRowsUnmemoized = EncodeProductRows
var emptyEncodedRowList = EncodeRowList(nil)

func encodeSubscriptionUpdatesMemoized(updates []subscription.SubscriptionUpdate, memo *subscription.EncodingMemo) ([]SubscriptionUpdate, error) {
	out := make([]SubscriptionUpdate, len(updates))
	for i, su := range updates {
		eu, err := encodeSubscriptionUpdateMemoized(su, memo)
		if err != nil {
			return nil, err
		}
		out[i] = eu
	}
	return out, nil
}

func encodeSubscriptionUpdateMemoized(su subscription.SubscriptionUpdate, memo *subscription.EncodingMemo) (SubscriptionUpdate, error) {
	inserts, err := encodeRowsMemoized(su.Inserts, su.Columns, memo)
	if err != nil {
		return SubscriptionUpdate{}, fmt.Errorf("encode inserts: %w", err)
	}
	deletes, err := encodeRowsMemoized(su.Deletes, su.Columns, memo)
	if err != nil {
		return SubscriptionUpdate{}, fmt.Errorf("encode deletes: %w", err)
	}
	return SubscriptionUpdate{
		QueryID:   su.QueryID,
		TableName: su.TableName,
		Inserts:   inserts,
		Deletes:   deletes,
	}, nil
}

func encodeRowsMemoized(rows []types.ProductValue, columns []schema.ColumnSchema, memo *subscription.EncodingMemo) ([]byte, error) {
	if len(rows) == 0 {
		return emptyEncodedRowList, nil
	}
	encode := func() ([]byte, error) {
		if len(columns) == 0 {
			return encodeRowsUnmemoized(rows)
		}
		return EncodeProductRowsForColumns(rows, columns)
	}
	if memo == nil {
		return encode()
	}
	key := memoizedRowListKey(rows, columns)
	if cached, ok := memo.Get(key); ok {
		if payload, ok := cached.([]byte); ok {
			return payload, nil
		}
	}
	encoded, err := encode()
	if err != nil {
		return nil, err
	}
	memo.Put(key, encoded)
	return encoded, nil
}

func memoizedRowListKey(rows []types.ProductValue, columns []schema.ColumnSchema) string {
	if len(rows) == 0 {
		return "binary-row-list:empty"
	}
	buf := make([]byte, 0, 16+len(rows)*24)
	buf = append(buf, "binary-row-list"...)
	for _, col := range columns {
		buf = append(buf, '|')
		buf = strconv.AppendInt(buf, int64(col.Index), 10)
		buf = append(buf, ':')
		buf = strconv.AppendInt(buf, int64(col.Type), 10)
		if col.Nullable {
			buf = append(buf, '?')
		}
	}
	for _, row := range rows {
		buf = append(buf, ':')
		ptr := uintptr(unsafe.Pointer(unsafe.SliceData(row)))
		buf = strconv.AppendUint(buf, uint64(ptr), 16)
		buf = append(buf, '/')
		buf = strconv.AppendInt(buf, int64(len(row)), 10)
	}
	return string(buf)
}

func encodeSubscriptionUpdate(su subscription.SubscriptionUpdate) (SubscriptionUpdate, error) {
	return encodeSubscriptionUpdateMemoized(su, nil)
}

func buildUpdateStatus(
	outcome subscription.CallerOutcome,
	callerUpdates []subscription.SubscriptionUpdate,
	memo *subscription.EncodingMemo,
) (UpdateStatus, error) {
	switch outcome.Kind {
	case subscription.CallerOutcomeCommitted:
		encoded, err := encodeSubscriptionUpdatesMemoized(callerUpdates, memo)
		if err != nil {
			return nil, err
		}
		return StatusCommitted{Update: encoded}, nil
	case subscription.CallerOutcomeFailed:
		return StatusFailed{Error: outcome.Error}, nil
	default:
		return nil, fmt.Errorf("unknown CallerOutcome kind %d", outcome.Kind)
	}
}

func reducerCallInfoFrom(outcome subscription.CallerOutcome) ReducerCallInfo {
	return ReducerCallInfo{
		ReducerName: outcome.ReducerName,
		ReducerID:   outcome.ReducerID,
		Args:        outcome.Args,
		RequestID:   outcome.RequestID,
	}
}
