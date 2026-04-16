package protocol

import (
	"bytes"
	"errors"
	"fmt"

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

func (a *FanOutSenderAdapter) SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []subscription.SubscriptionUpdate) error {
	encoded, err := encodeSubscriptionUpdates(updates)
	if err != nil {
		return fmt.Errorf("encode updates: %w", err)
	}
	msg := &TransactionUpdate{TxID: uint64(txID), Updates: encoded}
	return mapDeliveryError(a.sender.SendTransactionUpdate(connID, msg))
}

func (a *FanOutSenderAdapter) SendReducerResult(connID types.ConnectionID, result *subscription.ReducerCallResult) error {
	callerUpdates := result.TransactionUpdate
	pr, err := encodeReducerCallResult(result, callerUpdates)
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

func encodeSubscriptionUpdates(updates []subscription.SubscriptionUpdate) ([]SubscriptionUpdate, error) {
	out := make([]SubscriptionUpdate, len(updates))
	for i, su := range updates {
		eu, err := encodeSubscriptionUpdate(su)
		if err != nil {
			return nil, err
		}
		out[i] = eu
	}
	return out, nil
}

func encodeSubscriptionUpdate(su subscription.SubscriptionUpdate) (SubscriptionUpdate, error) {
	inserts, err := encodeRows(su.Inserts)
	if err != nil {
		return SubscriptionUpdate{}, fmt.Errorf("encode inserts: %w", err)
	}
	deletes, err := encodeRows(su.Deletes)
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

func encodeReducerCallResult(sr *subscription.ReducerCallResult, callerUpdates []subscription.SubscriptionUpdate) (*ReducerCallResult, error) {
	var encodedUpdates []SubscriptionUpdate
	if sr.Status == 0 {
		var err error
		encodedUpdates, err = encodeSubscriptionUpdates(callerUpdates)
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
