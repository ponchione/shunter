package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/schema"
)

func TestProtocolV1CompatibilityPinsTokenAndVersionNumbers(t *testing.T) {
	if ProtocolVersionV1 != 1 {
		t.Fatalf("ProtocolVersionV1 = %d, want 1", ProtocolVersionV1)
	}
	if MinSupportedProtocolVersion != 1 {
		t.Fatalf("MinSupportedProtocolVersion = %d, want 1", MinSupportedProtocolVersion)
	}
	if CurrentProtocolVersion != 1 {
		t.Fatalf("CurrentProtocolVersion = %d, want 1", CurrentProtocolVersion)
	}
	if SubprotocolV1 != "v1.bsatn.shunter" {
		t.Fatalf("SubprotocolV1 = %q, want v1.bsatn.shunter", SubprotocolV1)
	}

	token, ok := SubprotocolForVersion(1)
	if !ok || token != "v1.bsatn.shunter" {
		t.Fatalf("SubprotocolForVersion(1) = %q, %v; want v1.bsatn.shunter, true", token, ok)
	}
	version, ok := ProtocolVersionForSubprotocol("v1.bsatn.shunter")
	if !ok || version != 1 {
		t.Fatalf("ProtocolVersionForSubprotocol(v1 token) = %d, %v; want 1, true", version, ok)
	}
}

func TestProtocolV1CompatibilityPinsStableMessageTagAssignments(t *testing.T) {
	clientTags := []struct {
		name string
		got  uint8
		want uint8
	}{
		{name: "SubscribeSingle", got: TagSubscribeSingle, want: 1},
		{name: "UnsubscribeSingle", got: TagUnsubscribeSingle, want: 2},
		{name: "CallReducer", got: TagCallReducer, want: 3},
		{name: "OneOffQuery", got: TagOneOffQuery, want: 4},
		{name: "SubscribeMulti", got: TagSubscribeMulti, want: 5},
		{name: "UnsubscribeMulti", got: TagUnsubscribeMulti, want: 6},
		{name: "DeclaredQuery", got: TagDeclaredQuery, want: 7},
		{name: "SubscribeDeclaredView", got: TagSubscribeDeclaredView, want: 8},
	}
	for _, tag := range clientTags {
		if tag.got != tag.want {
			t.Fatalf("client tag %s = %d, want %d", tag.name, tag.got, tag.want)
		}
	}

	serverTags := []struct {
		name string
		got  uint8
		want uint8
	}{
		{name: "IdentityToken", got: TagIdentityToken, want: 1},
		{name: "SubscribeSingleApplied", got: TagSubscribeSingleApplied, want: 2},
		{name: "UnsubscribeSingleApplied", got: TagUnsubscribeSingleApplied, want: 3},
		{name: "SubscriptionError", got: TagSubscriptionError, want: 4},
		{name: "TransactionUpdate", got: TagTransactionUpdate, want: 5},
		{name: "OneOffQueryResponse", got: TagOneOffQueryResponse, want: 6},
		{name: "ReducerCallResultReserved", got: TagReducerCallResult, want: 7},
		{name: "TransactionUpdateLight", got: TagTransactionUpdateLight, want: 8},
		{name: "SubscribeMultiApplied", got: TagSubscribeMultiApplied, want: 9},
		{name: "UnsubscribeMultiApplied", got: TagUnsubscribeMultiApplied, want: 10},
	}
	for _, tag := range serverTags {
		if tag.got != tag.want {
			t.Fatalf("server tag %s = %d, want %d", tag.name, tag.got, tag.want)
		}
	}

	if !IsReservedV1Tag(TagReservedZero) {
		t.Fatal("TagReservedZero is not reserved in v1")
	}
	if !IsReservedV1Tag(TagReservedExtensionStart) || !IsReservedV1Tag(TagReservedExtensionEnd) {
		t.Fatal("extension tag range is not reserved in v1")
	}
	if !IsReservedV1ServerTag(TagReducerCallResult) {
		t.Fatal("retired server reducer-call result tag is not reserved in v1")
	}
}

func TestProtocolV1CompatibilityEncodesAndDecodesStableMessageFamilies(t *testing.T) {
	clientMessages := []struct {
		name string
		msg  any
		tag  uint8
	}{
		{
			name: "SubscribeSingle",
			msg:  SubscribeSingleMsg{RequestID: 1, QueryID: 2, QueryString: "SELECT * FROM messages"},
			tag:  TagSubscribeSingle,
		},
		{
			name: "UnsubscribeSingle",
			msg:  UnsubscribeSingleMsg{RequestID: 3, QueryID: 4},
			tag:  TagUnsubscribeSingle,
		},
		{
			name: "CallReducer",
			msg:  CallReducerMsg{ReducerName: "create_message", Args: []byte{0x01}, RequestID: 5, Flags: CallReducerFlagsFullUpdate},
			tag:  TagCallReducer,
		},
		{
			name: "OneOffQuery",
			msg:  OneOffQueryMsg{MessageID: []byte{0x06}, QueryString: "SELECT id FROM messages"},
			tag:  TagOneOffQuery,
		},
		{
			name: "SubscribeMulti",
			msg:  SubscribeMultiMsg{RequestID: 7, QueryID: 8, QueryStrings: []string{"SELECT * FROM messages"}},
			tag:  TagSubscribeMulti,
		},
		{
			name: "UnsubscribeMulti",
			msg:  UnsubscribeMultiMsg{RequestID: 9, QueryID: 10},
			tag:  TagUnsubscribeMulti,
		},
		{
			name: "DeclaredQuery",
			msg:  DeclaredQueryMsg{MessageID: []byte{0x0b}, Name: "recent_messages"},
			tag:  TagDeclaredQuery,
		},
		{
			name: "SubscribeDeclaredView",
			msg:  SubscribeDeclaredViewMsg{RequestID: 12, QueryID: 13, Name: "live_messages"},
			tag:  TagSubscribeDeclaredView,
		},
	}
	for _, tc := range clientMessages {
		t.Run("client/"+tc.name, func(t *testing.T) {
			frame, err := EncodeClientMessage(tc.msg)
			if err != nil {
				t.Fatalf("EncodeClientMessage(%s) returned error: %v", tc.name, err)
			}
			if len(frame) == 0 || frame[0] != tc.tag {
				t.Fatalf("EncodeClientMessage(%s) tag = %v, want %d", tc.name, frame, tc.tag)
			}
			tag, _, err := DecodeClientMessage(frame)
			if err != nil {
				t.Fatalf("DecodeClientMessage(%s) returned error: %v", tc.name, err)
			}
			if tag != tc.tag {
				t.Fatalf("DecodeClientMessage(%s) tag = %d, want %d", tc.name, tag, tc.tag)
			}
		})
	}

	rows := EncodeRowList([][]byte{{0x01}})
	update := []SubscriptionUpdate{{QueryID: 14, TableName: "messages", Inserts: rows}}
	requestID := uint32(15)
	queryID := uint32(16)
	tableID := schema.TableID(17)
	serverMessages := []struct {
		name string
		msg  any
		tag  uint8
	}{
		{
			name: "IdentityToken",
			msg:  IdentityToken{Token: "token"},
			tag:  TagIdentityToken,
		},
		{
			name: "SubscribeSingleApplied",
			msg:  SubscribeSingleApplied{RequestID: 18, QueryID: 19, TableName: "messages", Rows: rows},
			tag:  TagSubscribeSingleApplied,
		},
		{
			name: "UnsubscribeSingleApplied",
			msg:  UnsubscribeSingleApplied{RequestID: 20, QueryID: 21, HasRows: true, Rows: rows},
			tag:  TagUnsubscribeSingleApplied,
		},
		{
			name: "SubscriptionError",
			msg:  SubscriptionError{RequestID: &requestID, QueryID: &queryID, TableID: &tableID, Error: "permission denied"},
			tag:  TagSubscriptionError,
		},
		{
			name: "TransactionUpdate",
			msg:  TransactionUpdate{Status: StatusCommitted{Update: update}, ReducerCall: ReducerCallInfo{ReducerName: "create_message", RequestID: 22}},
			tag:  TagTransactionUpdate,
		},
		{
			name: "TransactionUpdateLight",
			msg:  TransactionUpdateLight{RequestID: 23, Update: update},
			tag:  TagTransactionUpdateLight,
		},
		{
			name: "OneOffQueryResponse",
			msg:  OneOffQueryResponse{MessageID: []byte{0x18}, Tables: []OneOffTable{{TableName: "messages", Rows: rows}}},
			tag:  TagOneOffQueryResponse,
		},
		{
			name: "SubscribeMultiApplied",
			msg:  SubscribeMultiApplied{RequestID: 24, QueryID: 25, Update: update},
			tag:  TagSubscribeMultiApplied,
		},
		{
			name: "UnsubscribeMultiApplied",
			msg:  UnsubscribeMultiApplied{RequestID: 26, QueryID: 27, Update: update},
			tag:  TagUnsubscribeMultiApplied,
		},
	}
	for _, tc := range serverMessages {
		t.Run("server/"+tc.name, func(t *testing.T) {
			frame, err := EncodeServerMessage(tc.msg)
			if err != nil {
				t.Fatalf("EncodeServerMessage(%s) returned error: %v", tc.name, err)
			}
			if len(frame) == 0 || frame[0] != tc.tag {
				t.Fatalf("EncodeServerMessage(%s) tag = %v, want %d", tc.name, frame, tc.tag)
			}
			tag, _, err := DecodeServerMessage(frame)
			if err != nil {
				t.Fatalf("DecodeServerMessage(%s) returned error: %v", tc.name, err)
			}
			if tag != tc.tag {
				t.Fatalf("DecodeServerMessage(%s) tag = %d, want %d", tc.name, tag, tc.tag)
			}
		})
	}
}

func TestProtocolCompiledAggregateOrderByIsObservable(t *testing.T) {
	sl := newMockSchema("messages", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint64},
	)
	compiled, err := CompileSQLQueryString("SELECT COUNT(*) AS n FROM messages ORDER BY n", sl, nil, SQLQueryValidationOptions{
		AllowProjection: true,
		AllowOrderBy:    true,
	})
	if err != nil {
		t.Fatalf("CompileSQLQueryString returned error: %v", err)
	}
	if compiled.SubscriptionAggregate() == nil {
		t.Fatal("SubscriptionAggregate = nil, want COUNT aggregate")
	}
	if !compiled.HasOrderBy() {
		t.Fatal("HasOrderBy = false, want source ORDER BY to remain observable")
	}
}

func TestProtocolV1CompatibilityRejectsEveryUnassignedOrReservedTag(t *testing.T) {
	for tag := 0; tag <= 255; tag++ {
		tag := uint8(tag)
		if isAssignedV1ClientTag(tag) {
			continue
		}
		_, _, err := DecodeClientMessage([]byte{tag})
		if !errors.Is(err, ErrUnknownMessageTag) {
			t.Fatalf("DecodeClientMessage(tag=%d) err = %v, want ErrUnknownMessageTag", tag, err)
		}
	}

	for tag := 0; tag <= 255; tag++ {
		tag := uint8(tag)
		if isAssignedV1ServerTag(tag) {
			continue
		}
		_, _, err := DecodeServerMessage([]byte{tag})
		if !errors.Is(err, ErrUnknownMessageTag) {
			t.Fatalf("DecodeServerMessage(tag=%d) err = %v, want ErrUnknownMessageTag", tag, err)
		}
	}
}

func isAssignedV1ClientTag(tag uint8) bool {
	switch tag {
	case TagSubscribeSingle,
		TagUnsubscribeSingle,
		TagCallReducer,
		TagOneOffQuery,
		TagSubscribeMulti,
		TagUnsubscribeMulti,
		TagDeclaredQuery,
		TagSubscribeDeclaredView:
		return true
	default:
		return false
	}
}

func isAssignedV1ServerTag(tag uint8) bool {
	switch tag {
	case TagIdentityToken,
		TagSubscribeSingleApplied,
		TagUnsubscribeSingleApplied,
		TagSubscriptionError,
		TagTransactionUpdate,
		TagOneOffQueryResponse,
		TagTransactionUpdateLight,
		TagSubscribeMultiApplied,
		TagUnsubscribeMultiApplied:
		return true
	default:
		return false
	}
}
