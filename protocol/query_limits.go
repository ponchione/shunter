package protocol

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/bsatn"
)

const (
	// DefaultSQLQueryMaxRows bounds rows returned by hosted one-off and declared
	// queries when the host does not provide a narrower limit.
	DefaultSQLQueryMaxRows = 100_000
	// DefaultSQLQueryMaxBytes bounds the encoded RowList payload returned by
	// hosted one-off and declared queries.
	DefaultSQLQueryMaxBytes = 64 << 20
)

// ErrSQLQueryResultLimit reports that a query exceeded a host-controlled result
// boundary. Client-supplied LIMIT clauses do not override this boundary.
var ErrSQLQueryResultLimit = errors.New("protocol: SQL query result limit exceeded")

// SQLQueryLimits bounds one-off and declared SQL query results. Zero values use
// the hosted defaults when passed through NormalizeSQLQueryLimits.
type SQLQueryLimits struct {
	MaxRows  int
	MaxBytes int
}

// NormalizeSQLQueryLimits validates limits and fills zero values with hosted
// defaults.
func NormalizeSQLQueryLimits(limits SQLQueryLimits) (SQLQueryLimits, error) {
	if limits.MaxRows < 0 {
		return SQLQueryLimits{}, fmt.Errorf("SQL query max rows must not be negative")
	}
	if limits.MaxBytes < 0 {
		return SQLQueryLimits{}, fmt.Errorf("SQL query max bytes must not be negative")
	}
	if limits.MaxRows == 0 {
		limits.MaxRows = DefaultSQLQueryMaxRows
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = DefaultSQLQueryMaxBytes
	}
	return limits, nil
}

func validateSQLQueryResultLimits(result SQLQueryResult, limits SQLQueryLimits) error {
	if limits.MaxRows > 0 && len(result.Rows) > limits.MaxRows {
		return fmt.Errorf("%w: rows=%d cap=%d", ErrSQLQueryResultLimit, len(result.Rows), limits.MaxRows)
	}
	if limits.MaxBytes <= 0 {
		return nil
	}
	size := uint64(4)
	for _, row := range result.Rows {
		rowBytes, err := bsatn.EncodedProductValueSizeForColumns(row, result.Columns)
		if err != nil {
			return err
		}
		size += 4 + uint64(rowBytes)
		if size > uint64(limits.MaxBytes) {
			return fmt.Errorf("%w: encoded_bytes>%d", ErrSQLQueryResultLimit, limits.MaxBytes)
		}
	}
	return nil
}
