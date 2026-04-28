package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

// Tests in this file pin the fanout-aliasing: the
// per-query update slices produced by evaluate() are distributed to N
// subscribers in subscription/eval.go::evaluate. Before this slice each
// subscriber's SubscriptionUpdate carried the SAME backing slice for
// Inserts/Deletes, so a downstream consumer that mutated one
// subscriber's slice (replace/append) would silently corrupt every
// other subscriber's view of the same commit. The fix gives each
// subscriber an independent slice header for Inserts/Deletes; row
// payloads remain shared under the post-commit row-immutability
// contract.

func TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers(t *testing.T) {
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

	cs := simpleChangeset(1, []types.ProductValue{
		{types.NewUint64(42), types.NewString("orig")},
	}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	msg := <-inbox
	updA := msg.Fanout[connA]
	updB := msg.Fanout[connB]
	if len(updA) != 1 || len(updB) != 1 {
		t.Fatalf("want 1 update per subscriber, got A=%d B=%d", len(updA), len(updB))
	}
	if len(updA[0].Inserts) != 1 || len(updB[0].Inserts) != 1 {
		t.Fatalf("want 1 inserted row per subscriber, got A=%d B=%d",
			len(updA[0].Inserts), len(updB[0].Inserts))
	}
	if &updA[0].Inserts[0] == &updB[0].Inserts[0] {
		t.Fatal("Inserts share backing array across subscribers")
	}

	// Replace subscriber A's first insert wholesale. With the fix this
	// only mutates A's slice element; without the fix (shared backing
	// array) it would also overwrite B's slice element 0.
	updA[0].Inserts[0] = types.ProductValue{types.NewUint64(999), types.NewString("mutated")}
	if v := updB[0].Inserts[0][0].AsUint64(); v != 42 {
		t.Fatalf("subscriber B Inserts[0][0] = %v, want uint64(42)", v)
	}
	if v := updB[0].Inserts[0][1].AsString(); v != "orig" {
		t.Fatalf("subscriber B Inserts[0][1] = %q, want \"orig\"", v)
	}

	// Append onto subscriber A's Inserts. Without the fix this could
	// either spill into subscriber B's view (when len < cap of the
	// shared array) or stay isolated by accident (when len == cap and
	// append reallocates). With the fix subscriber B's len is always
	// independent of subscriber A's append.
	updA[0].Inserts = append(updA[0].Inserts, types.ProductValue{types.NewUint64(1), types.NewString("extra")})
	if got := len(updB[0].Inserts); got != 1 {
		t.Fatalf("subscriber B Inserts len after A append = %d, want 1", got)
	}
}

func TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers(t *testing.T) {
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

	cs := simpleChangeset(1, nil, []types.ProductValue{
		{types.NewUint64(42), types.NewString("gone")},
	})
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	msg := <-inbox
	updA := msg.Fanout[connA]
	updB := msg.Fanout[connB]
	if len(updA) != 1 || len(updB) != 1 {
		t.Fatalf("want 1 update per subscriber, got A=%d B=%d", len(updA), len(updB))
	}
	if len(updA[0].Deletes) != 1 || len(updB[0].Deletes) != 1 {
		t.Fatalf("want 1 deleted row per subscriber, got A=%d B=%d",
			len(updA[0].Deletes), len(updB[0].Deletes))
	}
	if &updA[0].Deletes[0] == &updB[0].Deletes[0] {
		t.Fatal("Deletes share backing array across subscribers")
	}

	updA[0].Deletes[0] = types.ProductValue{types.NewUint64(999), types.NewString("mutated")}
	if v := updB[0].Deletes[0][0].AsUint64(); v != 42 {
		t.Fatalf("subscriber B Deletes[0][0] = %v, want uint64(42)", v)
	}

	updA[0].Deletes = append(updA[0].Deletes, types.ProductValue{types.NewUint64(1)})
	if got := len(updB[0].Deletes); got != 1 {
		t.Fatalf("subscriber B Deletes len after A append = %d, want 1", got)
	}
}
