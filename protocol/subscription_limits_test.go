package protocol

import "testing"

func TestNormalizeSubscriptionLimits(t *testing.T) {
	got, err := NormalizeSubscriptionLimits(SubscriptionLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxQueriesPerSet != DefaultSubscriptionMaxQueriesPerSet {
		t.Fatalf("MaxQueriesPerSet = %d, want %d", got.MaxQueriesPerSet, DefaultSubscriptionMaxQueriesPerSet)
	}
	if _, err := NormalizeSubscriptionLimits(SubscriptionLimits{MaxQueriesPerSet: -1}); err == nil {
		t.Fatal("negative query limit succeeded")
	}
	if _, err := NormalizeSubscriptionLimits(SubscriptionLimits{MaxQueriesPerSet: int(MaxSubscribeMultiQueriesHard) + 1}); err == nil {
		t.Fatal("query limit above decoder ceiling succeeded")
	}
	got, err = NormalizeSubscriptionLimits(SubscriptionLimits{MaxQueriesPerSet: int(MaxSubscribeMultiQueriesHard)})
	if err != nil {
		t.Fatalf("decoder ceiling rejected: %v", err)
	}
	if got.MaxQueriesPerSet != int(MaxSubscribeMultiQueriesHard) {
		t.Fatalf("MaxQueriesPerSet = %d, want decoder ceiling", got.MaxQueriesPerSet)
	}
}
