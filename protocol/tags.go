// Package protocol implements the SPEC-005 wire protocol: message type
// tags, codecs, RowList row-batch encoding, compression envelope,
// transport lifecycle, and the ClientSender contract used by
// subscription fan-out delivery.
package protocol

// Client→server message tags (SPEC-005 §6).
const (
	TagSubscribe   uint8 = 1
	TagUnsubscribe uint8 = 2
	TagCallReducer uint8 = 3
	TagOneOffQuery uint8 = 4
)

// Server→client message tags (SPEC-005 §6).
//
// Phase 1.5 outcome-model decision (`docs/parity-phase1.5-outcome-model.md`):
//   - `TagTransactionUpdate` carries the heavy caller-bound envelope.
//   - `TagTransactionUpdateLight` carries the non-caller delta-only envelope.
//   - `TagReducerCallResult` is reserved. The former `ReducerCallResult` wire
//     type was removed when caller outcome moved into heavy `TransactionUpdate`.
//     The tag byte is intentionally not reused so a future reintroduction
//     cannot silently collide with the removed shape.
const (
	TagInitialConnection      uint8 = 1
	TagSubscribeApplied       uint8 = 2
	TagUnsubscribeApplied     uint8 = 3
	TagSubscriptionError      uint8 = 4
	TagTransactionUpdate      uint8 = 5
	TagOneOffQueryResult      uint8 = 6
	TagReducerCallResult      uint8 = 7 // RESERVED — formerly ReducerCallResult, removed Phase 1.5
	TagTransactionUpdateLight uint8 = 8
)
