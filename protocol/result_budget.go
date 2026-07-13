package protocol

import (
	"fmt"
	"math"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

type encodedResultBudget struct {
	columns  []schema.ColumnSchema
	maxBytes int
	bytes    int
}

func newEncodedResultBudget(columns []schema.ColumnSchema, maxBytes int) (*encodedResultBudget, error) {
	budget := &encodedResultBudget{columns: columns, maxBytes: maxBytes, bytes: 4}
	if maxBytes > 0 && budget.bytes > maxBytes {
		return nil, budget.limitError(budget.bytes)
	}
	return budget, nil
}

func (b *encodedResultBudget) rowSize(row types.ProductValue) (int, error) {
	if b == nil {
		return 0, nil
	}
	return bsatn.EncodedProductValueSizeForColumns(row, b.columns)
}

func (b *encodedResultBudget) add(row types.ProductValue) (int, error) {
	if b == nil {
		return 0, nil
	}
	rowBytes, err := b.rowSize(row)
	if err != nil {
		return 0, err
	}
	if rowBytes > math.MaxInt-4 || b.bytes > math.MaxInt-(4+rowBytes) {
		return 0, b.limitError(math.MaxInt)
	}
	next := b.bytes + 4 + rowBytes
	if b.maxBytes > 0 && next > b.maxBytes {
		return 0, b.limitError(next)
	}
	b.bytes = next
	return rowBytes, nil
}

func (b *encodedResultBudget) replace(oldRowBytes int, row types.ProductValue) (int, error) {
	if b == nil {
		return 0, nil
	}
	rowBytes, err := b.rowSize(row)
	if err != nil {
		return 0, err
	}
	base := b.bytes - (4 + oldRowBytes)
	if rowBytes > math.MaxInt-4 || base > math.MaxInt-(4+rowBytes) {
		return 0, b.limitError(math.MaxInt)
	}
	next := base + 4 + rowBytes
	if b.maxBytes > 0 && next > b.maxBytes {
		return 0, b.limitError(next)
	}
	b.bytes = next
	return rowBytes, nil
}

func (b *encodedResultBudget) limitError(bytes int) error {
	return fmt.Errorf("%w: encoded_bytes=%d cap=%d", ErrSQLQueryResultLimit, bytes, b.maxBytes)
}

type oneOffResultCollector struct {
	rows    []types.ProductValue
	skipped int
	offset  int
	limit   int
	budget  *encodedResultBudget
	err     error
}

func newOneOffResultCollector(offset, limit int, budget *encodedResultBudget) *oneOffResultCollector {
	return &oneOffResultCollector{offset: offset, limit: limit, budget: budget}
}

func (c *oneOffResultCollector) Visit(row types.ProductValue) bool {
	if c.err != nil {
		return false
	}
	if c.skipped < c.offset {
		c.skipped++
		return true
	}
	if _, err := c.budget.add(row); err != nil {
		c.err = err
		return false
	}
	c.rows = append(c.rows, row)
	return !oneOffLimitReached(len(c.rows), c.limit)
}

func (c *oneOffResultCollector) Result() ([]types.ProductValue, error) {
	return c.rows, c.err
}
