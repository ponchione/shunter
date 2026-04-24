package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// FanOutSenderAdapter wraps a ClientSender to implement
// subscription.FanOutSender. Converts subscription-domain types to
// protocol wire format before delivery.
//
// Phase 1.5 outcome-model split (`docs/parity-phase1.5-outcome-model.md`):
//   - Caller receives the heavy `TransactionUpdate` via
//     SendTransactionUpdateHeavy. Caller's visible row delta is carried
//     inside `StatusCommitted.Update` (or omitted for `StatusFailed` /
//     `StatusOutOfEnergy`).
//   - Non-callers whose rows were touched receive
//     `TransactionUpdateLight` via SendTransactionUpdateLight.
type FanOutSenderAdapter struct {
	sender ClientSender
}

func NewFanOutSenderAdapter(sender ClientSender) *FanOutSenderAdapter {
	return &FanOutSenderAdapter{sender: sender}
}

// SendTransactionUpdateHeavy delivers the caller's heavy
// `TransactionUpdate`. For `StatusCommitted` outcomes the caller's
// visible row delta is encoded into `StatusCommitted.Update`. For
// `StatusFailed` / `StatusOutOfEnergy` outcomes the update slice is
// ignored to match the reference wire contract.
func (a *FanOutSenderAdapter) SendTransactionUpdateHeavy(
	connID types.ConnectionID,
	outcome subscription.CallerOutcome,
	callerUpdates []subscription.SubscriptionUpdate,
	memo *subscription.EncodingMemo,
) error {
	msg, err := BuildTransactionUpdateHeavy(connID, outcome, callerUpdates, memo)
	if err != nil {
		return fmt.Errorf("encode caller outcome: %w", err)
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
		EnergyQuantaUsed:           energyQuantaU128LE(outcome.EnergyQuantaUsed),
		TotalHostExecutionDuration: outcome.TotalHostExecutionDuration,
	}, nil
}

// energyQuantaU128LE widens the subscription-domain u64 energy count to
// the reference-wire u128 little-endian layout. Shunter has no energy
// model so the value is always 0, but the wire width must match
// reference `EnergyQuanta { quanta: u128 }` (energy.rs:12).
func energyQuantaU128LE(quanta uint64) [16]byte {
	var out [16]byte
	binary.LittleEndian.PutUint64(out[:8], quanta)
	return out
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
		return fmt.Errorf("encode updates: %w", err)
	}
	msg := &TransactionUpdateLight{RequestID: requestID, Update: encoded}
	return mapDeliveryError(a.sender.SendTransactionUpdateLight(connID, msg))
}

// SendSubscriptionError delivers a post-commit evaluation-origin
// SubscriptionError. The evaluator routes here only via the fan-out
// worker after a TransactionUpdate-driven re-eval (see
// `subscription/eval.go::handleEvalError` and
// `subscription/fanout_worker.go::run`), so the error is never tied to
// a client Subscribe/Unsubscribe request.
//
// Reference `SubscriptionError` (v1.rs:350) sets both `request_id` and
// `query_id` to `None` in exactly this case — see
// `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:1998-2010`
// (v1) and `messages.rs:622-629` (Option propagates through
// `SubscriptionMessage`). Any per-connection diagnostic fields on
// `subscription.SubscriptionError` (`RequestID`, `SubscriptionID`,
// `QueryHash`, `Predicate`) are intentionally not on the wire; they
// stay for internal logging only.
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
		QueryID:   su.QueryID,
		TableName: su.TableName,
		Inserts:   inserts,
		Deletes:   deletes,
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

// encodeRows encodes each row to BSATN bytes. Row payloads are
// treated as READ-ONLY: OI-006 row-payload sharing contract governs
// `types.ProductValue` backing-array sharing across subscribers of
// the same query (pinned by subscription fanout row-payload sharing
// regression tests and `subscription/eval.go::evaluate`). `bsatn.EncodeProductValue`
// only reads `row` — any future change that mutated `row[i]` in place
// during encoding (for example, column-level normalization before
// serialization) would silently corrupt every other subscriber's
// view of the same commit. Preserve the read-only discipline at this
// seam.
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
	case subscription.CallerOutcomeOutOfEnergy:
		return StatusOutOfEnergy{}, nil
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
