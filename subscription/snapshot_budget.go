package subscription

import (
	"fmt"
	"math"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/types"
)

type snapshotBudget struct {
	rowLimit  int
	byteLimit int
	rows      int
	bytes     int
}

func newSnapshotBudget(rowLimit, byteLimit int) *snapshotBudget {
	budget := &snapshotBudget{rowLimit: rowLimit, byteLimit: byteLimit}
	if byteLimit > 0 {
		budget.bytes = 4 // Empty RowList count prefix.
	}
	return budget
}

func (b *snapshotBudget) remainingRowLimit() int {
	if b == nil || b.rowLimit <= 0 {
		return 0
	}
	remaining := b.rowLimit - b.rows
	if remaining <= 0 {
		return 1 // Retain one-row detection once the aggregate cap is full.
	}
	return remaining
}

func (b *snapshotBudget) add(updates []SubscriptionUpdate) error {
	if b == nil {
		return nil
	}
	rows := b.rows
	bytes := b.bytes
	if b.byteLimit > 0 && bytes > b.byteLimit {
		return NewQuotaError(ErrSnapshotByteLimit, "snapshot_bytes", bytes, b.byteLimit)
	}
	for _, update := range updates {
		for _, batch := range [][]types.ProductValue{update.Inserts, update.Deletes} {
			for _, row := range batch {
				if rows == math.MaxInt {
					return NewQuotaError(ErrInitialRowLimit, "snapshot_rows", rows, b.rowLimit)
				}
				rows++
				if b.rowLimit > 0 && rows > b.rowLimit {
					return NewQuotaError(ErrInitialRowLimit, "snapshot_rows", rows, b.rowLimit)
				}
				if b.byteLimit <= 0 {
					continue
				}
				rowBytes := bsatn.EncodedProductValueSize(row)
				if len(update.Columns) > 0 {
					var err error
					rowBytes, err = bsatn.EncodedProductValueSizeForColumns(row, update.Columns)
					if err != nil {
						return fmt.Errorf("snapshot row size: %w", err)
					}
				}
				if rowBytes > math.MaxInt-4 || bytes > math.MaxInt-(4+rowBytes) {
					return NewQuotaError(ErrSnapshotByteLimit, "snapshot_bytes", math.MaxInt, b.byteLimit)
				}
				bytes += 4 + rowBytes
				if bytes > b.byteLimit {
					return NewQuotaError(ErrSnapshotByteLimit, "snapshot_bytes", bytes, b.byteLimit)
				}
			}
		}
	}
	b.rows = rows
	b.bytes = bytes
	return nil
}
