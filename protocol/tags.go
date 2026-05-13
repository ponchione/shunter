// Package protocol implements wire messages, codecs, transport lifecycle, and
// fan-out delivery adapters.
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
	// V2-only declared-read request messages. V1 connections reject these
	// tags as unsupported for the negotiated protocol.
	TagDeclaredQueryWithParameters         uint8 = 9
	TagSubscribeDeclaredViewWithParameters uint8 = 10
)

// v1 reserved tag space. Zero is never a message tag. Tags 128–255 are
// reserved for future negotiated protocol versions and remain fatal in v1.
const (
	TagReservedZero           uint8 = 0
	TagReservedExtensionStart uint8 = 128
	TagReservedExtensionEnd   uint8 = 255
)

// Server→client message tags. TagReducerCallResult is reserved.
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

// IsReservedV1Tag reports whether tag is in the protocol-wide v1 reserved
// space. Direction-specific retired tags, such as TagReducerCallResult on the
// server→client side, are handled by direction-specific policy.
func IsReservedV1Tag(tag uint8) bool {
	return tag == TagReservedZero || tag >= TagReservedExtensionStart
}

// IsReservedV1ServerTag reports whether tag is reserved for server→client
// frames in Shunter v1.
func IsReservedV1ServerTag(tag uint8) bool {
	return IsReservedV1Tag(tag) || tag == TagReducerCallResult
}
