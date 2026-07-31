package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

// Tests in this file pin per-subscriber update slice headers while row
// payloads remain shared.

func evalTwoSubscriberFanout(t *testing.T, inserts, deletes []types.ProductValue) (SubscriptionUpdate, SubscriptionUpdate) {
	t.Helper()
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	connA := types.ConnectionID{1}
	connB := types.ConnectionID{2}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connA, QueryID: 10, Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet A: %v", err)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connB, QueryID: 11, Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet B: %v", err)
	}

	mgr.EvalAndBroadcast(types.TxID(1), simpleChangeset(1, inserts, deletes), nil, PostCommitMeta{})

	msg := <-inbox
	updA := msg.Fanout[connA]
	updB := msg.Fanout[connB]
	if len(updA) != 1 || len(updB) != 1 {
		t.Fatalf("want 1 update per subscriber, got A=%d B=%d", len(updA), len(updB))
	}
	return updA[0], updB[0]
}

func TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers(t *testing.T) {
	updA, updB := evalTwoSubscriberFanout(t, []types.ProductValue{
		{types.NewUint64(42), types.NewString("orig")},
	}, nil)
	if len(updA.Inserts) != 1 || len(updB.Inserts) != 1 {
		t.Fatalf("want 1 inserted row per subscriber, got A=%d B=%d",
			len(updA.Inserts), len(updB.Inserts))
	}
	if &updA.Inserts[0] == &updB.Inserts[0] {
		t.Fatal("Inserts share backing array across subscribers")
	}

	// Replace subscriber A's first insert wholesale. With the fix this
	// only mutates A's slice element; without the fix (shared backing
	// array) it would also overwrite B's slice element 0.
	updA.Inserts[0] = types.ProductValue{types.NewUint64(999), types.NewString("mutated")}
	if v := updB.Inserts[0][0].AsUint64(); v != 42 {
		t.Fatalf("subscriber B Inserts[0][0] = %v, want uint64(42)", v)
	}
	if v := updB.Inserts[0][1].AsString(); v != "orig" {
		t.Fatalf("subscriber B Inserts[0][1] = %q, want \"orig\"", v)
	}

	// Append onto subscriber A's Inserts. Without the fix this could
	// either spill into subscriber B's view (when len < cap of the
	// shared array) or stay isolated by accident (when len == cap and
	// append reallocates). With the fix subscriber B's len is always
	// independent of subscriber A's append.
	updA.Inserts = append(updA.Inserts, types.ProductValue{types.NewUint64(1), types.NewString("extra")})
	if got := len(updB.Inserts); got != 1 {
		t.Fatalf("subscriber B Inserts len after A append = %d, want 1", got)
	}
}

func TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers(t *testing.T) {
	updA, updB := evalTwoSubscriberFanout(t, nil, []types.ProductValue{
		{types.NewUint64(42), types.NewString("gone")},
	})
	if len(updA.Deletes) != 1 || len(updB.Deletes) != 1 {
		t.Fatalf("want 1 deleted row per subscriber, got A=%d B=%d",
			len(updA.Deletes), len(updB.Deletes))
	}
	if &updA.Deletes[0] == &updB.Deletes[0] {
		t.Fatal("Deletes share backing array across subscribers")
	}

	updA.Deletes[0] = types.ProductValue{types.NewUint64(999), types.NewString("mutated")}
	if v := updB.Deletes[0][0].AsUint64(); v != 42 {
		t.Fatalf("subscriber B Deletes[0][0] = %v, want uint64(42)", v)
	}

	updA.Deletes = append(updA.Deletes, types.ProductValue{types.NewUint64(1)})
	if got := len(updB.Deletes); got != 1 {
		t.Fatalf("subscriber B Deletes len after A append = %d, want 1", got)
	}
}
