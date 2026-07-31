package subscription

import (
	"context"
	"math"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/types"
)

type deltaBudgetKey struct{}

type deltaBudget struct {
	rowLimit  int
	byteLimit int
	rows      int
	bytes     int
}

func withDeltaBudget(ctx context.Context, rowLimit, byteLimit int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, deltaBudgetKey{}, &deltaBudget{
		rowLimit:  rowLimit,
		byteLimit: byteLimit,
	})
}

func chargeDeltaRow(ctx context.Context, row types.ProductValue) error {
	return chargeDeltaRowN(ctx, row, 1)
}

func chargeDeltaRowN(ctx context.Context, row types.ProductValue, n uint64) error {
	if ctx == nil || n == 0 {
		return nil
	}
	budget, _ := ctx.Value(deltaBudgetKey{}).(*deltaBudget)
	if budget == nil {
		return nil
	}
	if n > uint64(math.MaxInt-budget.rows) {
		return NewQuotaError(ErrDeltaRowLimit, "delta_rows", math.MaxInt, budget.rowLimit)
	}
	rows := budget.rows + int(n)
	if budget.rowLimit > 0 && rows > budget.rowLimit {
		return NewQuotaError(ErrDeltaRowLimit, "delta_rows", rows, budget.rowLimit)
	}

	bytes := budget.bytes
	if budget.byteLimit > 0 {
		rowBytes := bsatn.EncodedProductValueSize(row)
		if rowBytes > math.MaxInt-4 {
			return NewQuotaError(ErrDeltaByteLimit, "delta_bytes", math.MaxInt, budget.byteLimit)
		}
		perRow := 4 + rowBytes
		if n > uint64(math.MaxInt/perRow) || int(n)*perRow > math.MaxInt-bytes {
			return NewQuotaError(ErrDeltaByteLimit, "delta_bytes", math.MaxInt, budget.byteLimit)
		}
		bytes += int(n) * perRow
		if bytes > budget.byteLimit {
			return NewQuotaError(ErrDeltaByteLimit, "delta_bytes", bytes, budget.byteLimit)
		}
	}
	budget.rows = rows
	budget.bytes = bytes
	return nil
}
