package querywork

import (
	"context"
	"errors"
	"testing"
)

func TestBudgetChargesAcrossSharedContext(t *testing.T) {
	ctx := WithBudget(context.Background(), 2)
	if err := Charge(ctx); err != nil {
		t.Fatalf("first charge: %v", err)
	}
	if err := Charge(ctx); err != nil {
		t.Fatalf("second charge: %v", err)
	}
	if err := Charge(ctx); !errors.Is(err, ErrExhausted) {
		t.Fatalf("third charge = %v, want ErrExhausted", err)
	}
}

func TestMissingAndUnlimitedBudgetsDoNotExhaust(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background(), WithBudget(context.Background(), 0)} {
		for range 10 {
			if err := Charge(ctx); err != nil {
				t.Fatalf("Charge(%v): %v", ctx, err)
			}
		}
	}
}

func TestChargeNRejectsExpansionAtomically(t *testing.T) {
	ctx := WithBudget(context.Background(), 5)
	if err := ChargeN(ctx, 4); err != nil {
		t.Fatalf("ChargeN within limit: %v", err)
	}
	err := ChargeN(ctx, 2)
	var exhausted *ExhaustedError
	if !errors.As(err, &exhausted) || exhausted.Used != 6 || exhausted.Limit != 5 {
		t.Fatalf("ChargeN exhaustion = %#v, want used=6 limit=5", err)
	}
	if err := Charge(ctx); err != nil {
		t.Fatalf("failed ChargeN mutated budget: %v", err)
	}
}
