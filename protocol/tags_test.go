package protocol

import (
	"errors"
	"testing"
)

func TestClientTagsDistinct(t *testing.T) {
	tags := []uint8{
		TagSubscribeSingle, TagUnsubscribeSingle, TagCallReducer, TagOneOffQuery,
		TagSubscribeMulti, TagUnsubscribeMulti, TagDeclaredQuery, TagSubscribeDeclaredView,
	}
	seen := map[uint8]bool{}
	for _, tag := range tags {
		if seen[tag] {
			t.Errorf("duplicate C2S tag value %d", tag)
		}
		seen[tag] = true
	}
	// Spec-pinned values (SPEC-005 §6).
	if TagSubscribeSingle != 1 || TagUnsubscribeSingle != 2 || TagCallReducer != 3 || TagOneOffQuery != 4 {
		t.Errorf("C2S tag values drifted from SPEC-005 §6")
	}
	if TagSubscribeMulti != 5 || TagUnsubscribeMulti != 6 ||
		TagDeclaredQuery != 7 || TagSubscribeDeclaredView != 8 {
		t.Errorf("Shunter-owned C2S tag values drifted")
	}
}

func TestServerTagsDistinct(t *testing.T) {
	tags := []uint8{
		TagIdentityToken, TagSubscribeSingleApplied, TagUnsubscribeSingleApplied,
		TagSubscriptionError, TagTransactionUpdate, TagOneOffQueryResponse,
		TagReducerCallResult, TagTransactionUpdateLight,
		TagSubscribeMultiApplied, TagUnsubscribeMultiApplied,
	}
	seen := map[uint8]bool{}
	for _, tag := range tags {
		if seen[tag] {
			t.Errorf("duplicate S2C tag value %d", tag)
		}
		seen[tag] = true
	}
	// Spec-pinned values.
	if TagIdentityToken != 1 || TagSubscribeSingleApplied != 2 || TagUnsubscribeSingleApplied != 3 ||
		TagSubscriptionError != 4 || TagTransactionUpdate != 5 || TagOneOffQueryResponse != 6 ||
		TagReducerCallResult != 7 || TagTransactionUpdateLight != 8 ||
		TagSubscribeMultiApplied != 9 || TagUnsubscribeMultiApplied != 10 {
		t.Errorf("Shunter-owned S2C tag values drifted")
	}
}

func TestReservedV1TagPolicy(t *testing.T) {
	if !IsReservedV1Tag(TagReservedZero) {
		t.Fatal("tag 0 must remain protocol-wide reserved")
	}
	if !IsReservedV1Tag(TagReservedExtensionStart) || !IsReservedV1Tag(TagReservedExtensionEnd) {
		t.Fatalf("reserved extension range = [%d,%d] not classified reserved", TagReservedExtensionStart, TagReservedExtensionEnd)
	}
	for _, tag := range []uint8{
		TagSubscribeSingle,
		TagSubscribeDeclaredView,
		TagIdentityToken,
		TagUnsubscribeMultiApplied,
	} {
		if IsReservedV1Tag(tag) {
			t.Fatalf("assigned tag %d classified protocol-wide reserved", tag)
		}
	}
	if !IsReservedV1ServerTag(TagReducerCallResult) {
		t.Fatal("server TagReducerCallResult must remain reserved")
	}
	if IsReservedV1ServerTag(TagSubscribeMultiApplied) {
		t.Fatal("server TagSubscribeMultiApplied classified reserved")
	}
}

func TestClientReservedAndUnassignedTagsRejected(t *testing.T) {
	cases := []struct {
		name string
		tag  uint8
	}{
		{"zero", TagReservedZero},
		{"unassigned_low", TagSubscribeDeclaredView + 1},
		{"extension_start", TagReservedExtensionStart},
		{"extension_end", TagReservedExtensionEnd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := DecodeClientMessage([]byte{tc.tag})
			if !errors.Is(err, ErrUnknownMessageTag) {
				t.Fatalf("DecodeClientMessage(tag=%d) err = %v, want ErrUnknownMessageTag", tc.tag, err)
			}
		})
	}
}

func TestServerReservedAndUnassignedTagsRejected(t *testing.T) {
	cases := []struct {
		name string
		tag  uint8
	}{
		{"zero", TagReservedZero},
		{"reducer_call_result", TagReducerCallResult},
		{"unassigned_low", TagUnsubscribeMultiApplied + 1},
		{"extension_start", TagReservedExtensionStart},
		{"extension_end", TagReservedExtensionEnd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := DecodeServerMessage([]byte{tc.tag})
			if !errors.Is(err, ErrUnknownMessageTag) {
				t.Fatalf("DecodeServerMessage(tag=%d) err = %v, want ErrUnknownMessageTag", tc.tag, err)
			}
		})
	}
}
