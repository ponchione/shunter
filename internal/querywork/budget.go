// Package querywork provides request-scoped execution work budgets shared by
// query evaluators in different runtime subsystems.
package querywork

import (
	"context"
	"errors"
	"fmt"
)

// ErrExhausted classifies a request that consumed its execution work budget.
var ErrExhausted = errors.New("query work budget exhausted")

type budgetKey struct{}

type budget struct {
	limit int
	used  int
}

// WithBudget returns a child context with an independent work budget. A
// non-positive limit is unlimited. Budgets are intentionally request-local and
// are not safe for concurrent use.
func WithBudget(ctx context.Context, limit int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, budgetKey{}, &budget{limit: limit})
}

// Charge consumes one work unit from the budget carried by ctx. Contexts
// without a budget are unlimited.
func Charge(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(budgetKey{}).(*budget)
	if b == nil || b.limit <= 0 {
		return nil
	}
	if b.used >= b.limit {
		return fmt.Errorf("%w: work=%d cap=%d", ErrExhausted, b.used+1, b.limit)
	}
	b.used++
	return nil
}
