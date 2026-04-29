// Package protocol implements the SPEC-005 wire protocol: message type
// tags, codecs, RowList row-batch encoding, compression envelope,
// transport lifecycle, and the ClientSender contract used by
// subscription fan-out delivery.
package protocol

// Client→server message tags (SPEC-005 §6).
const (
	TagSubscribeSingle       uint8 = 1 // renamed from TagSubscribe (single/multi variant variant split)
	TagUnsubscribeSingle     uint8 = 2 // renamed from TagUnsubscribe (single/multi variant variant split)
	TagCallReducer           uint8 = 3
	TagOneOffQuery           uint8 = 4
	TagSubscribeMulti        uint8 = 5
	TagUnsubscribeMulti      uint8 = 6
	TagDeclaredQuery         uint8 = 7
	TagSubscribeDeclaredView uint8 = 8
)

// Server→client message tags (SPEC-005 §6).
//
// Outcome-model decision (`docs/shunter-design-decisions.md#outcome-model`):
//   - `TagTransactionUpdate` carries the heavy caller-bound envelope.
//   - `TagTransactionUpdateLight` carries the non-caller delta-only envelope.
//   - `TagReducerCallResult` is reserved. The former `ReducerCallResult` wire
//     type was removed when caller outcome moved into heavy `TransactionUpdate`.
//     The tag byte is intentionally not reused so a future reintroduction
//     cannot silently collide with the removed shape.
const (
	TagIdentityToken            uint8 = 1 // matches reference `IdentityToken` (v1.rs:445); renamed from TagInitialConnection
	TagSubscribeSingleApplied   uint8 = 2 // renamed from TagSubscribeApplied (single/multi variant variant split)
	TagUnsubscribeSingleApplied uint8 = 3 // renamed from TagUnsubscribeApplied (single/multi variant variant split)
	TagSubscriptionError        uint8 = 4
	TagTransactionUpdate        uint8 = 5
	TagOneOffQueryResponse      uint8 = 6 // matches reference `OneOffQueryResponse` (v1.rs:287/654); renamed from TagOneOffQueryResult
	TagReducerCallResult        uint8 = 7 // RESERVED — formerly ReducerCallResult, removed outcome-model
	TagTransactionUpdateLight   uint8 = 8
	TagSubscribeMultiApplied    uint8 = 9  // single/multi variant variant split
	TagUnsubscribeMultiApplied  uint8 = 10 // single/multi variant variant split
)
