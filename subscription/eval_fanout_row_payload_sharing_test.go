package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

// Tests in this file pin the OI-006 row-payload sharing contract:
// `types.ProductValue` (itself `[]Value`) backing arrays are shared
// across subscribers of the same query for both
// `SubscriptionUpdate.Inserts` and `.Deletes`. Sharing is
// intentional under the post-commit row-immutability contract — a
// deep copy per subscriber would cost work proportional to row
// width × row count × subscriber count for no client-visible benefit
// under the contract.
//
// The OI-006 slice-header sub-hazard (closed 2026-04-20 in
// `eval_fanout_aliasing_test.go`) asserts the outer `[]ProductValue`
// is independent per subscriber so replace/append on one
// subscriber's outer slice does not leak. These tests assert the
// complement: the INNER `[]Value` backing array remains shared, so
// in-place `Value` mutation on one subscriber IS visible to peers.
// That is the hazard the post-commit row-immutability contract
// prevents — this file documents the hazard shape so a future
// change that claimed to make row-payload mutation safe would have
// to update these tests. See
// `docs/hardening-oi-006-row-payload-sharing.md`.

func TestEvalFanoutRowPayloadsSharedAcrossSubscribersForInserts(t *testing.T) {
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

	// Pin 1: row payload Value backing arrays are shared across
	// subscribers. The OI-006 slice-header fix gives each subscriber
	// an independent outer `[]types.ProductValue`, but the inner
	// `ProductValue` slice headers point at the same `[]Value`
	// backing array. A future change that deep-copied row payloads
	// per subscriber would break this identity assertion.
	if &updA[0].Inserts[0][0] != &updB[0].Inserts[0][0] {
		t.Fatal("row payload Value backing array unexpectedly independent across subscribers — " +
			"post-commit row-immutability contract said sharing is intentional; deep copy would " +
			"cost work proportional to row width × row count × subscriber count for no client-visible benefit")
	}

	// Pin 2: in-place Value mutation on subscriber A's row payload is
	// observable in subscriber B's view. Documents the hazard the
	// post-commit row-immutability contract prevents: any future
	// downstream consumer that mutates row contents in place (e.g.,
	// rewriting a column during bsatn-encoding) silently corrupts
	// every other subscriber's view of the same commit.
	updA[0].Inserts[0][1] = types.NewString("mutated")
	if got := updB[0].Inserts[0][1].AsString(); got != "mutated" {
		t.Fatalf("subscriber B Inserts[0][1] = %q, want %q — shared-payload hazard must be observable for the contract pin to be load-bearing", got, "mutated")
	}
}

func TestEvalFanoutRowPayloadsSharedAcrossSubscribersForDeletes(t *testing.T) {
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

	// Pin 1: identity on the Deletes side.
	if &updA[0].Deletes[0][0] != &updB[0].Deletes[0][0] {
		t.Fatal("row payload Value backing array unexpectedly independent across subscribers on Deletes")
	}

	// Pin 2: mutation-leaks hazard shape on the Deletes side.
	updA[0].Deletes[0][1] = types.NewString("mutated")
	if got := updB[0].Deletes[0][1].AsString(); got != "mutated" {
		t.Fatalf("subscriber B Deletes[0][1] = %q, want %q — shared-payload hazard must be observable", got, "mutated")
	}
}
