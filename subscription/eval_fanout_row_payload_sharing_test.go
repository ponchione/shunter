package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

// Tests in this file pin that fan-out copies outer update slices but shares
// immutable row payload backing arrays across subscribers.

func TestEvalFanoutRowPayloadsSharedAcrossSubscribersForInserts(t *testing.T) {
	updA, updB := evalTwoSubscriberFanout(t, []types.ProductValue{
		{types.NewUint64(42), types.NewString("orig")},
	}, nil)
	if len(updA.Inserts) != 1 || len(updB.Inserts) != 1 {
		t.Fatalf("want 1 inserted row per subscriber, got A=%d B=%d",
			len(updA.Inserts), len(updB.Inserts))
	}

	// Pin 1: row payload Value backing arrays are shared across
	// subscribers. The slice-header fix gives each subscriber
	// an independent outer `[]types.ProductValue`, but the inner
	// `ProductValue` slice headers point at the same `[]Value`
	// backing array. A future change that deep-copied row payloads
	// per subscriber would break this identity assertion.
	if &updA.Inserts[0][0] != &updB.Inserts[0][0] {
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
	updA.Inserts[0][1] = types.NewString("mutated")
	if got := updB.Inserts[0][1].AsString(); got != "mutated" {
		t.Fatalf("subscriber B Inserts[0][1] = %q, want %q — shared-payload hazard must be observable for the contract pin to be load-bearing", got, "mutated")
	}
}

func TestEvalFanoutRowPayloadsSharedAcrossSubscribersForDeletes(t *testing.T) {
	updA, updB := evalTwoSubscriberFanout(t, nil, []types.ProductValue{
		{types.NewUint64(42), types.NewString("gone")},
	})
	if len(updA.Deletes) != 1 || len(updB.Deletes) != 1 {
		t.Fatalf("want 1 deleted row per subscriber, got A=%d B=%d",
			len(updA.Deletes), len(updB.Deletes))
	}

	// Pin 1: identity on the Deletes side.
	if &updA.Deletes[0][0] != &updB.Deletes[0][0] {
		t.Fatal("row payload Value backing array unexpectedly independent across subscribers on Deletes")
	}

	// Pin 2: mutation-leaks hazard shape on the Deletes side.
	updA.Deletes[0][1] = types.NewString("mutated")
	if got := updB.Deletes[0][1].AsString(); got != "mutated" {
		t.Fatalf("subscriber B Deletes[0][1] = %q, want %q — shared-payload hazard must be observable", got, "mutated")
	}
}
