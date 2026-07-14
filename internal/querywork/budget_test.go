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
