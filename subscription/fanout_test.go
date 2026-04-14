package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestReducerCallResultMatchesProtocolShape(t *testing.T) {
	result := ReducerCallResult{
		RequestID: 7,
		Status:    1,
		TxID:      types.TxID(9),
		Error:     "boom",
		Energy:    0,
		TransactionUpdate: []SubscriptionUpdate{{
			SubscriptionID: 10,
			TableID:        1,
		}},
	}
	if result.RequestID != 7 || result.Status != 1 || result.TxID != 9 {
		t.Fatalf("unexpected reducer result: %+v", result)
	}
	if len(result.TransactionUpdate) != 1 {
		t.Fatalf("transaction_update = %v, want 1 entry", result.TransactionUpdate)
	}
}
