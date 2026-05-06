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
