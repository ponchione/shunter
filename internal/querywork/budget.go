// Package querywork provides request-scoped execution work budgets shared by
// query evaluators in different runtime subsystems.
package querywork

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// ErrExhausted classifies a request that consumed its execution work budget.
var ErrExhausted = errors.New("query work budget exhausted")

type budgetKey struct{}

type budget struct {
	limit int
	used  int
}

// ExhaustedError reports the attempted usage and configured limit for a
// depleted work budget.
type ExhaustedError struct {
	Used  int
	Limit int
}

func (e *ExhaustedError) Error() string {
	return fmt.Sprintf("%s: work=%d cap=%d", ErrExhausted, e.Used, e.Limit)
}

func (e *ExhaustedError) Unwrap() error { return ErrExhausted }

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
	return ChargeN(ctx, 1)
}

// ChargeN consumes n work units from the budget carried by ctx. It permits
// callers that know a Cartesian expansion's size to reject it before entering
// the materialization loop.
func ChargeN(ctx context.Context, n uint64) error {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(budgetKey{}).(*budget)
	if b == nil || b.limit <= 0 || n == 0 {
		return nil
	}
	remaining := b.limit - b.used
	if remaining < 0 || n > uint64(remaining) {
		attempted := math.MaxInt
		if n <= uint64(math.MaxInt-b.used) {
			attempted = b.used + int(n)
		}
		return &ExhaustedError{Used: attempted, Limit: b.limit}
	}
	b.used += int(n)
	return nil
}
