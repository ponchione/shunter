package protocol

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// FanOutSenderAdapter wraps a ClientSender to implement
// subscription.FanOutSender. Converts subscription-domain types to
// protocol wire format before delivery (SPEC-004 §8 / Story 6.1).
type FanOutSenderAdapter struct {
	sender ClientSender
}

func NewFanOutSenderAdapter(sender ClientSender) *FanOutSenderAdapter {
	return &FanOutSenderAdapter{sender: sender}
}

func (a *FanOutSenderAdapter) SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []subscription.SubscriptionUpdate, memo *subscription.EncodingMemo) error {
	encoded, err := encodeSubscriptionUpdatesMemoized(updates, memo)
	if err != nil {
		return fmt.Errorf("encode updates: %w", err)
	}
	msg := &TransactionUpdate{TxID: uint64(txID), Updates: encoded}
	return mapDeliveryError(a.sender.SendTransactionUpdate(connID, msg))
}

func (a *FanOutSenderAdapter) SendReducerResult(connID types.ConnectionID, result *subscription.ReducerCallResult, memo *subscription.EncodingMemo) error {
	callerUpdates := result.TransactionUpdate
	pr, err := encodeReducerCallResultMemoized(result, callerUpdates, memo)
	if err != nil {
		return fmt.Errorf("encode reducer result: %w", err)
	}
	return mapDeliveryError(a.sender.SendReducerResult(connID, pr))
}

func (a *FanOutSenderAdapter) SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error {
	return mapDeliveryError(a.sender.Send(connID, SubscriptionError{
		SubscriptionID: uint32(subID),
		Error:          message,
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

var encodeRowsUnmemoized = encodeRows
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
	inserts, err := encodeRowsMemoized(su.Inserts, memo)
	if err != nil {
		return SubscriptionUpdate{}, fmt.Errorf("encode inserts: %w", err)
	}
	deletes, err := encodeRowsMemoized(su.Deletes, memo)
	if err != nil {
		return SubscriptionUpdate{}, fmt.Errorf("encode deletes: %w", err)
	}
	return SubscriptionUpdate{
		SubscriptionID: uint32(su.SubscriptionID),
		TableName:      su.TableName,
		Inserts:        inserts,
		Deletes:        deletes,
	}, nil
}

func encodeRowsMemoized(rows []types.ProductValue, memo *subscription.EncodingMemo) ([]byte, error) {
	if len(rows) == 0 {
		return emptyEncodedRowList, nil
	}
	if memo == nil {
		return encodeRowsUnmemoized(rows)
	}
	key := memoizedRowListKey(rows)
	if cached, ok := memo.Get(key); ok {
		if payload, ok := cached.([]byte); ok {
			return payload, nil
		}
	}
	encoded, err := encodeRowsUnmemoized(rows)
	if err != nil {
		return nil, err
	}
	memo.Put(key, encoded)
	return encoded, nil
}

func memoizedRowListKey(rows []types.ProductValue) string {
	if len(rows) == 0 {
		return "binary-row-list:empty"
	}
	return fmt.Sprintf("binary-row-list:%x:%d", reflect.ValueOf(rows).Pointer(), len(rows))
}

func encodeSubscriptionUpdates(updates []subscription.SubscriptionUpdate) ([]SubscriptionUpdate, error) {
	return encodeSubscriptionUpdatesMemoized(updates, nil)
}

func encodeSubscriptionUpdate(su subscription.SubscriptionUpdate) (SubscriptionUpdate, error) {
	return encodeSubscriptionUpdateMemoized(su, nil)
}

func encodeRows(rows []types.ProductValue) ([]byte, error) {
	encoded := make([][]byte, len(rows))
	for i, row := range rows {
		var buf bytes.Buffer
		if err := bsatn.EncodeProductValue(&buf, row); err != nil {
			return nil, err
		}
		encoded[i] = buf.Bytes()
	}
	return EncodeRowList(encoded), nil
}

func encodeReducerCallResultMemoized(sr *subscription.ReducerCallResult, callerUpdates []subscription.SubscriptionUpdate, memo *subscription.EncodingMemo) (*ReducerCallResult, error) {
	var encodedUpdates []SubscriptionUpdate
	if sr.Status == 0 {
		var err error
		encodedUpdates, err = encodeSubscriptionUpdatesMemoized(callerUpdates, memo)
		if err != nil {
			return nil, err
		}
	}
	return &ReducerCallResult{
		RequestID:         sr.RequestID,
		Status:            sr.Status,
		TxID:              uint64(sr.TxID),
		Error:             sr.Error,
		Energy:            0, // v1: always zero (SPEC-005 §8.7)
		TransactionUpdate: encodedUpdates,
	}, nil
}

func encodeReducerCallResult(sr *subscription.ReducerCallResult, callerUpdates []subscription.SubscriptionUpdate) (*ReducerCallResult, error) {
	return encodeReducerCallResultMemoized(sr, callerUpdates, nil)
}
