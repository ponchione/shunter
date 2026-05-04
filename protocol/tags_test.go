package protocol

import "testing"

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
